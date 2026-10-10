package control

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// A user's DISPLAY NAME: the operator-written answer `displayOf` prefers over the email and
// over `<provider>:<subject>`.
//
// 🔴 IT IS OPERATOR-WRITTEN AND NEVER SOURCED FROM A PROVIDER CLAIM. It reaches the journal
// through `user-created` (`cairn-server -create-user -display-name`) or `user-renamed`
// (`cairn-server -rename-user`), and both need write access to the journal. A JWT claim
// rendered in the browser header alone would make the header disagree with the audit line and
// every written bullet's ACTOR — two answers to "who is this", which `PrincipalFor` exists to
// make one.
//
// 🔴 THE ALPHABET IS THE SPOOFING RULE, NOT TASTE. The value lands in three places that each
// parse it differently: the audit line's `identity=` field (which maps every byte outside
// printable ASCII to `?`, every space to `_`, and truncates past [UserDisplayNameMax] bytes),
// `Principal.String`'s `<kind>:<id> (<display>)`, and a bullet's ACTOR. So the accepted set is
// `[A-Za-z0-9]` followed by `[A-Za-z0-9._-]`, at most 32 characters, and each exclusion closes
// one shape:
//
//   - no `:` — the `<provider>:<subject>` fallback cannot be impersonated (a display name of
//     `<provider>:<another user's subject>` would otherwise attribute one person's writes to
//     another in every reader that does not see the kind and id);
//   - no `@` — an email display cannot be impersonated;
//   - no `(`, `)`, space, control, format (bidi override) or non-ASCII character — the audit
//     line would rewrite them, so two distinct names could render as one there, and a
//     parenthesis could forge the `(<display>)` suffix of `Principal.String`;
//   - at most 32 (= `authz.MaxIdentityChars`, pinned by a test) — the audit field truncates
//     past it, so two names sharing a 32-character prefix would be one name there.
//
// A GitHub handle (`[A-Za-z0-9-]`, up to 39) fits unless it is longer than 32; such a handle is
// refused rather than cut, because a cut name is a name nobody chose.
//
// 🔴 AND IT IS UNIQUE AMONG USERS, CASE-INSENSITIVELY, AGAINST EVERY OTHER USER'S *RENDERED*
// DISPLAY — display name, else email, else `<provider>:<subject>`, compared as the audit line
// writes it — AND AGAINST EVERY DISPLAY ANOTHER USER HAS EVER RENDERED AS. Two users rendering the same
// string is two people one attribution, which is the hazard the alphabet exists for, arriving
// by duplication instead of by spelling; a released name reissued is the same hazard across
// time, because a bullet's ACTOR and an audit line are stored text. Case-insensitive because
// `Octocat` and `octocat` read as one person to a human scanning an audit stream; the alphabet
// is ASCII, so the fold is exact. Checked in `apply`, so it binds every writer, under
// `Append`'s lock.
//
// ⚠ WHAT IT DOES NOT COVER, DECLARED: (a) a PROJECT principal's display is its name, and a
// project may be named like a user's display name — `identity=` carries no kind, so the two
// are one string there. That class predates this field (a project may already be named like an
// email) and refusing it here would refuse the natural `-create-user -project octocat
// -display-name octocat`; `Principal.String` and the journal's ids still tell them apart.
// (b) A LATER user created with an email lacking `@` that equals an existing display name is
// not refused: `user-created`'s email is unvalidated free text, and adding a rule there would
// change how a journal written before this field replays.

// UserDisplayNameMax is the longest display name accepted, in characters (ASCII, so bytes).
const UserDisplayNameMax = 32

var userDisplayNameShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ErrUserDisplayNameShape refuses a display name outside the alphabet or over the cap.
var ErrUserDisplayNameShape = errors.New("control: not an acceptable user display name")

// ErrUserDisplayNameTaken refuses a display name another user already renders as.
var ErrUserDisplayNameTaken = errors.New("control: another user already displays as this name")

// ValidUserDisplayName is the SHAPE rule alone — the one `Event.validate` applies — exported
// so a command can refuse before it opens a journal. Uniqueness needs a model; see
// [RenameUser].
func ValidUserDisplayName(name string) error {
	return validUserDisplayName("", name)
}

func validUserDisplayName(kind EventKind, name string) error {
	prefix := ""
	if kind != "" {
		prefix = string(kind) + ": "
	}
	// Length first and by BYTES: a multi-byte value fails the alphabet anyway, and the audit
	// field this cap mirrors truncates by bytes.
	if len(name) > UserDisplayNameMax {
		return fmt.Errorf("%sdisplay_name is %d bytes, at most %d are accepted — the audit line's identity= "+
			"field truncates past that, so two longer names sharing a prefix would read as one: %w",
			prefix, len(name), UserDisplayNameMax, ErrUserDisplayNameShape)
	}
	if !userDisplayNameShape.MatchString(name) {
		// %q, so a control character in the refused value is ESCAPED in the message rather than
		// re-staged into an operator's terminal.
		return fmt.Errorf("%sdisplay_name %q is not [A-Za-z0-9] followed by [A-Za-z0-9._-] — no `:` or `@` "+
			"(either would let it be spelled like another user's email or <provider>:<subject>), and no "+
			"space, parenthesis, control or non-ASCII character (the audit line rewrites those, so two "+
			"names could render as one): %w", prefix, name, ErrUserDisplayNameShape)
	}
	return nil
}

// refuseTakenUserDisplayName is the uniqueness rule, asked by `apply` for both kinds that write
// a display name. `self` is excluded, so re-asserting a user's own name is not a clash.
//
// It refuses two things, each against a user OTHER than `self`:
//
//   - a name equal to their RENDERED display (`displayOf`) — one predicate over a display name,
//     an email and a `<provider>:<subject>` rather than three — compared as the audit line's
//     `identity=` field writes it, spaces as `_`: an `@`-less free-text email `wren example` is
//     `wren_example` there, which is a name the alphabet accepts. (Its other rewrites cannot
//     matter: `?` and the truncation marker's length are outside what the alphabet accepts.)
//   - a display they RELEASED by a rename — a display name, or an `@`-less email they rendered
//     as before their first one. A bullet's ACTOR and an audit line are stored text, so a
//     released display still attributes everything written under it; reissuing it to someone
//     else makes one string two people across time. The user who held it may take it back.
func (m *Model) refuseTakenUserDisplayName(self ID, name string) error {
	for id := range m.Users {
		if id == self {
			continue
		}
		other := displayOf(*m, KindUser, id)
		if strings.EqualFold(auditSpelling(other), name) {
			return fmt.Errorf("display name %q: user %s already displays as %q, and two users rendering one "+
				"string is one attribution for two people: %w", name, id, other, ErrUserDisplayNameTaken)
		}
	}
	if holder, held := m.heldDisplayNames[strings.ToLower(name)]; held && holder != self {
		return fmt.Errorf("display name %q: user %s held it before, and every bullet and audit line written "+
			"under it still reads as theirs, so it is not reissued to another user: %w",
			name, holder, ErrUserDisplayNameTaken)
	}
	return nil
}

// holdReleasedDisplay records what `user` renders as NOW — display name, else email, else
// `<provider>:<subject>` — as theirs, for [Model.refuseTakenUserDisplayName]'s history rule.
// `apply` calls it on a rename, before the new name replaces the old: that is the only moment
// a display is released, since a user's CURRENT display is covered by the comparison against
// every other user. A held value the alphabet can never spell (an `@` email, a
// `<provider>:<subject>`) is harmless and is not filtered out.
func (m *Model) holdReleasedDisplay(user ID) {
	if m.heldDisplayNames == nil {
		m.heldDisplayNames = map[string]ID{}
	}
	m.heldDisplayNames[strings.ToLower(auditSpelling(displayOf(*m, KindUser, user)))] = user
}

// auditSpelling is a display as the audit line's `identity=` field writes it, for the one
// rewrite a name in the alphabet can collide with: every space becomes `_`.
func auditSpelling(display string) string { return strings.ReplaceAll(display, " ", "_") }

// NewUserDisplayName is a request to set an existing user's display name.
type NewUserDisplayName struct {
	// UserID names an existing user (`usr_…`). Required.
	UserID ID
	// DisplayName is the new name. Required; see this file's header for the rules.
	DisplayName string
	// Actor is recorded on the event. Optional, for `NewMembership.Actor`'s reason.
	Actor ID
	// At is the event's timestamp. Zero means now.
	At time.Time
}

// UserRenamed is what [RenameUser] did, in enough detail for a caller to say so.
type UserRenamed struct {
	UserID ID
	// Previous is what the user displayed as BEFORE — the rendered display, so a first
	// rename reports the email (or `<provider>:<subject>`) it replaced.
	Previous string
	// Display is what the user displays as now — equal to the request's name.
	Display string
	// Epoch is the journal epoch after the append.
	Epoch uint64
}

// RenameUser appends a `user-renamed` record.
//
// ⚠ THE PRE-CHECKS ARE EARLY REFUSALS, NOT A SECOND AUTHORITY — `SetMember`'s standing.
// `Event.validate` and `apply` refuse the same things under `Append`'s lock for every writer;
// what these buy is a sentinel a command can branch on before anything is written.
//
// ⚠ THERE IS NO "CLEAR". A display name, once written, is replaced and never removed: an
// empty `display_name` is refused by `validate`, so the fallback to the email is reached only
// by a user who never had one. YAGNI until somebody needs it, and then it is a separate kind.
func RenameUser(ctx context.Context, s Store, req NewUserDisplayName) (UserRenamed, error) {
	at := req.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if err := ValidUserDisplayName(req.DisplayName); err != nil {
		return UserRenamed{}, err
	}
	current, err := s.Model(ctx)
	if err != nil {
		return UserRenamed{}, fmt.Errorf("reading the control journal: %w", err)
	}
	if _, known := current.Users[req.UserID]; !known {
		return UserRenamed{}, fmt.Errorf("%w: %s — a rename names its user by id", ErrNoSuchMember, req.UserID)
	}
	if err := current.refuseTakenUserDisplayName(req.UserID, req.DisplayName); err != nil {
		return UserRenamed{}, err
	}
	previous := displayOf(current, KindUser, req.UserID)
	after, err := s.Append(ctx, Event{
		Kind: EventUserRenamed, At: at, Actor: req.Actor,
		UserID: req.UserID, DisplayName: req.DisplayName,
	})
	if err != nil {
		return UserRenamed{}, err
	}
	return UserRenamed{
		UserID:   req.UserID,
		Previous: previous,
		Display:  displayOf(after, KindUser, req.UserID),
		Epoch:    after.Epoch,
	}, nil
}
