// Package presence holds WHERE a live session is running — host label, tmux target, hotkey —
// for the one identity that pushed it, and the queue of terminal-bell rings aimed at it.
//
// It is slice S2 of `claudedocs/plan-cairn-arcs-presence.md`: the store and the agent API. The
// browser's read-only badges (S4) are `internal/ui/presence.go`. Everything here is EPHEMERAL and in memory (decision 2): a restart
// loses at most one push interval of presence and any pending ring, and both self-heal. ⚠ It
// therefore assumes ONE `cairn-ui` replica (the plan's open question P2).
//
// 🔴 ONE OWNER PREDICATE, AND EVERY SURFACE ASKS IT. [Store.For] is the only way to read a row
// on behalf of a viewer, and the ring path ([Service.Ring]) reaches the queue only through it.
// The predicate's clauses — owner equality on `(Kind, ID)`, an unexpired row, an UN-narrowed
// viewer — live in [visible] and nowhere else, so another owner's presence, expired presence
// and no presence are one answer by construction rather than by three call sites agreeing.
//
// 🔴 NARROWED IS `control.Authorization.Narrowed()`, READ OFF THE VIEWER'S OWN IDENTITY. On the
// bearer path `identity.MachineToken` hands through the `Authorization` `control.Authenticate`
// produced, which carries the bit from the SAME match that authenticated the credential; on the
// cookie path `identity.CookieSession` re-resolves with `control.Resolve`, which never sets it —
// and a session cannot be minted from a narrowed credential (`internal/ui`'s sign-in refusal,
// pinned by `TestANarrowedCredentialCannotSignIn`). The plan's first draft derived the bit from
// the credential ROW instead, because the accessor did not exist when it was written; see its
// decision 11 for what was built.
//
// 🔴 ONLY `cmd/cairn-ui` AND `internal/ui` IMPORT THIS PACKAGE, AND THAT IS PINNED AS A LEDGER.
// `TestOnlyTheBrowserProgramImportsPresence` fails when an importer is added (or removed): a
// presence token authenticates nothing but the two agent routes because nothing else parses one.
// `internal/ui` (S4) only READS presence, for its badges, through [Store.For] alone. The pod's
// dependency set is `internal/depspolicy`'s ban, unchanged here.
package presence

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// DefaultTTL is how long a pushed host set stays visible after its push (decision 2, O3's
// "~3 min"). The host side pushes every 60 s ± 1 s, so two missed pushes are tolerated.
const DefaultTTL = 3 * time.Minute

// Owner is the stable key of whoever pushed presence: `(Principal.Kind, Principal.ID)`.
//
// 🔴 NEVER `Display` AND NEVER THE WHOLE `control.Principal`. `Display` is an email and the
// email is MUTABLE; `CredentialID` is set on the bearer path only, so a whole-struct comparison
// would make one person two owners depending on how they arrived.
type Owner struct {
	Kind control.Kind
	ID   control.ID
}

// String renders the owner as the `-presence-owner` flag and the token file spell it.
func (o Owner) String() string { return string(o.Kind) + ":" + string(o.ID) }

// ParseOwner reads `<kind>:<id>`, the one spelling of an owner in a flag or a token row.
func ParseOwner(s string) (Owner, error) {
	kind, id, ok := strings.Cut(s, ":")
	if !ok || id == "" {
		return Owner{}, fmt.Errorf("presence owner %q is not <kind>:<id>", s)
	}
	o := Owner{Kind: control.Kind(kind), ID: control.ID(id)}
	if !o.Kind.Valid() {
		return Owner{}, fmt.Errorf("presence owner %q names kind %q, which is not %q or %q",
			s, kind, control.KindUser, control.KindProject)
	}
	if strings.ContainsAny(id, " \t\r\n:") {
		return Owner{}, fmt.Errorf("presence owner %q has an id carrying whitespace or a second ':'", s)
	}
	return o, nil
}

// OwnerOf is the owner key a viewer would match, for callers that must name it (the wall, the
// mint). It is NOT a visibility check — that is [Store.For]'s alone.
func OwnerOf(p control.Principal) Owner { return Owner{Kind: p.Kind, ID: p.ID} }

// Row is one pane as the host reported it — the wire row of decision 8, decoded and validated.
type Row struct {
	Session string
	Runtime string
	Target  string
	Label   string
	Hotkey  string
	// LastActivity is the ledger record's time as sent (RFC 3339) or "" — kept as the host
	// spelled it for display, with [Row.activity] the parsed instant the target pick compares.
	LastActivity string
	activity     time.Time
}

// Located is one row as STORED: the wire row plus what the UI added and the host never sends.
type Located struct {
	Row
	Owner    Owner
	Host     string
	PushedAt time.Time
	Expires  time.Time
}

// Presence is the answer to "where is this session running", for a viewer allowed to know.
type Presence struct {
	// Target is the row a ring is aimed at (decision 7).
	Target Located
	// AlsoOn lists the OTHER hosts presenting the same session, sorted, for an "also on" note.
	AlsoOn []string
}

type hostKey struct {
	owner Owner
	host  string
}

type hostSet struct {
	pushedAt time.Time
	rows     []Row
}

// Store is the in-memory presence table, keyed `(owner, host)` with each value the host's
// WHOLE current set (decision 6): a push replaces it, so a closed pane disappears on the next
// push rather than at TTL, and a push can never touch another host's rows.
type Store struct {
	// TTL defaults to [DefaultTTL].
	TTL time.Duration
	// Now defaults to `time.Now`.
	Now func() time.Time

	mu    sync.Mutex
	hosts map[hostKey]hostSet
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) ttl() time.Duration {
	if s.TTL > 0 {
		return s.TTL
	}
	return DefaultTTL
}

// Replace installs `rows` as the whole current set for `(owner, host)`. An empty slice clears
// it. The caller has already checked the body's host against the token's — [Agent] does, and
// refuses a mismatch before this is reached, so nothing is written for a 400.
func (s *Store) Replace(owner Owner, host string, rows []Row) {
	kept := append([]Row(nil), rows...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hosts == nil {
		s.hosts = map[hostKey]hostSet{}
	}
	s.hosts[hostKey{owner, host}] = hostSet{pushedAt: s.now(), rows: kept}
}

// visible is THE OWNER PREDICATE: may `viewer` see a row owned by `owner` that expires at
// `expires`, at instant `now`. Every read on a viewer's behalf goes through it.
//
// 🔴 THREE CLAUSES, EACH WITH ITS OWN RED TEST (`TestTheOwnerPredicateIsARelationship`): the
// viewer is not narrowed, the owner is the viewer's `(Kind, ID)` — BOTH halves, because a
// project and a user can carry the same id string — and the row has not expired.
func visible(viewer identity.Identity, owner Owner, expires, now time.Time) bool {
	if !viewer.Valid() || viewer.Auth.Narrowed() {
		return false
	}
	if owner != OwnerOf(viewer.Principal) {
		return false
	}
	return now.Before(expires)
}

// For answers where `session` is running, for `viewer` — or false, which is the SAME answer
// for another owner's presence, expired presence, a narrowed viewer and no presence at all.
func (s *Store) For(viewer identity.Identity, session string) (Presence, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	ttl := s.ttl()
	var found []Located
	for key, set := range s.hosts {
		expires := set.pushedAt.Add(ttl)
		if !visible(viewer, key.owner, expires, now) {
			continue
		}
		for _, r := range set.rows {
			if r.Session != session {
				continue
			}
			found = append(found, Located{Row: r, Owner: key.owner, Host: key.host,
				PushedAt: set.pushedAt, Expires: expires})
		}
	}
	if len(found) == 0 {
		return Presence{}, false
	}
	sort.Slice(found, func(i, j int) bool { return before(found[i], found[j]) })
	p := Presence{Target: found[0]}
	for _, other := range found[1:] {
		p.AlsoOn = append(p.AlsoOn, other.Host)
	}
	sort.Strings(p.AlsoOn)
	return p, true
}

// before orders candidate rows so the TARGET sorts first (decision 7): the newest
// `last_activity` wins; an empty one sorts OLDEST; a tie — including two empties — goes to the
// byte-wise smaller host label.
func before(a, b Located) bool {
	if !a.activity.Equal(b.activity) {
		return a.activity.After(b.activity)
	}
	return a.Host < b.Host
}
