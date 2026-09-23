package control

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// EventKind names one thing that happened. The set is closed and exhaustively
// handled by `apply`; an unknown kind fails the replay.
type EventKind string

const (
	EventUserCreated       EventKind = "user-created"
	EventProjectCreated    EventKind = "project-created"
	EventMemberSet         EventKind = "member-set"
	EventMemberRemoved     EventKind = "member-removed"
	EventScopeCreated      EventKind = "scope-created"
	EventScopeRenamed      EventKind = "scope-renamed"
	EventScopeMoved        EventKind = "scope-moved"
	EventGranted           EventKind = "granted"
	EventGrantRevoked      EventKind = "grant-revoked"
	EventCredentialIssued  EventKind = "credential-issued"
	EventCredentialRevoked EventKind = "credential-revoked"
)

// AllEventKinds is the closed set, declared once so a ledger test can assert it
// against `apply`'s switch rather than against a second hand-written list.
var AllEventKinds = []EventKind{
	EventUserCreated,
	EventProjectCreated,
	EventMemberSet,
	EventMemberRemoved,
	EventScopeCreated,
	EventScopeRenamed,
	EventScopeMoved,
	EventGranted,
	EventGrantRevoked,
	EventCredentialIssued,
	EventCredentialRevoked,
}

// Event is one durable record in the journal.
//
// 🔴 ONE FLAT STRUCT RATHER THAN A DISCRIMINATED UNION, AND THE TRADE IS STATED
// BECAUSE IT IS NOT FREE. A union would make "which fields does this kind carry"
// a compile-time fact; a flat struct makes it a validation fact, checked by
// `validate` and nowhere else. The flat shape is chosen because the journal is
// READ BY PEOPLE auditing a grant log — one stable line format, greppable by kind,
// with no nesting to walk — and because `encoding/json` round-trips it without a
// custom unmarshaller that would itself become a place to get the discrimination
// wrong. The cost is that `validate` is load-bearing; it is tested as such.
//
// 🔴 `NarrowedScopes` CARRIES NO `omitempty`, DELIBERATELY. With it, an EMPTY
// narrowing — "this credential sees nothing" — would be omitted from the line and
// read back as `nil`, which means "NO narrowing" and is its exact opposite. That is
// a silent WIDENING on a round trip through the durable format, in the one field
// whose whole purpose is to restrict. Without `omitempty` the two states are `[]`
// and `null`, and Go's decoder distinguishes them.
type Event struct {
	Kind  EventKind `json:"kind"`
	At    time.Time `json:"at"`
	Actor ID        `json:"actor,omitempty"`

	UserID       ID `json:"user_id,omitempty"`
	ProjectID    ID `json:"project_id,omitempty"`
	ScopeID      ID `json:"scope_id,omitempty"`
	GrantID      ID `json:"grant_id,omitempty"`
	CredentialID ID `json:"credential_id,omitempty"`

	Provider string `json:"provider,omitempty"`
	Subject  string `json:"subject,omitempty"`
	Email    string `json:"email,omitempty"`

	Name string `json:"name,omitempty"`
	Solo bool   `json:"solo,omitempty"`
	Role Role   `json:"role,omitempty"`

	DisplayName string `json:"display_name,omitempty"`

	SubjectKind Kind       `json:"subject_kind,omitempty"`
	SubjectID   ID         `json:"subject_id,omitempty"`
	ObjectKind  ObjectKind `json:"object_kind,omitempty"`
	ObjectID    ID         `json:"object_id,omitempty"`
	Verbs       VerbSet    `json:"verbs,omitempty"`

	TokenHash      string `json:"token_hash,omitempty"`
	Label          string `json:"label,omitempty"`
	NarrowedScopes []ID   `json:"narrowed_scopes"`
}

// validate checks an event's OWN shape — that the fields its kind requires are
// present and well-formed. It says nothing about whether the event is consistent
// with the model; that is `apply`'s job, and the split matters because a malformed
// line and a conflicting one are different operator problems with different fixes.
func (e Event) validate() error {
	need := func(name string, value string) error {
		if value == "" {
			return fmt.Errorf("%s: %s is required", e.Kind, name)
		}
		return nil
	}
	if e.At.IsZero() {
		return fmt.Errorf("%s: at is required — an authority record with no time cannot answer 'when', which is most of what it is for", e.Kind)
	}
	switch e.Kind {
	case EventUserCreated:
		return firstErr(
			need("user_id", string(e.UserID)),
			need("provider", e.Provider),
			need("subject", e.Subject))
	case EventProjectCreated:
		return firstErr(
			need("project_id", string(e.ProjectID)),
			need("name", e.Name),
			need("user_id", string(e.UserID)))
	case EventMemberSet:
		if err := firstErr(
			need("project_id", string(e.ProjectID)),
			need("user_id", string(e.UserID)),
			need("role", string(e.Role))); err != nil {
			return err
		}
		if !e.Role.Valid() {
			return fmt.Errorf("%s: unknown role %q — a role this build cannot map grants NOTHING, so replaying it would silently strip authority a newer build intends", e.Kind, e.Role)
		}
		return nil
	case EventMemberRemoved:
		return firstErr(
			need("project_id", string(e.ProjectID)),
			need("user_id", string(e.UserID)))
	case EventScopeCreated:
		return firstErr(
			need("scope_id", string(e.ScopeID)),
			need("display_name", e.DisplayName),
			need("project_id", string(e.ProjectID)))
	case EventScopeRenamed:
		return firstErr(
			need("scope_id", string(e.ScopeID)),
			need("display_name", e.DisplayName))
	case EventScopeMoved:
		return firstErr(
			need("scope_id", string(e.ScopeID)),
			need("project_id", string(e.ProjectID)))
	case EventGranted:
		if err := firstErr(
			need("grant_id", string(e.GrantID)),
			need("subject_id", string(e.SubjectID)),
			need("object_id", string(e.ObjectID))); err != nil {
			return err
		}
		if !e.SubjectKind.Valid() {
			return fmt.Errorf("%s: unknown subject_kind %q", e.Kind, e.SubjectKind)
		}
		if !e.ObjectKind.Valid() {
			return fmt.Errorf("%s: unknown object_kind %q", e.Kind, e.ObjectKind)
		}
		if e.Verbs.Empty() {
			return fmt.Errorf("%s: verbs is empty — a grant conferring nothing is indistinguishable from an absent one at every call site, so it is refused at the boundary rather than stored as a row that reads like access", e.Kind)
		}
		return nil
	case EventGrantRevoked:
		return need("grant_id", string(e.GrantID))
	case EventCredentialIssued:
		if err := firstErr(
			need("credential_id", string(e.CredentialID)),
			need("token_hash", e.TokenHash),
			need("subject_id", string(e.SubjectID))); err != nil {
			return err
		}
		if !e.SubjectKind.Valid() {
			return fmt.Errorf("%s: unknown subject_kind %q", e.Kind, e.SubjectKind)
		}
		// 🔴 A HEX DIGEST, NOT MERELY 64 CHARACTERS — AND THE LENGTH-ONLY VERSION THIS
		// REPLACES WAS A GUARD THE HAZARD WALKED AROUND. It read
		// `len(e.TokenHash) != HashHexLen` and its own message said a short hash "is the
		// shape a raw token takes", which is true and is not the shape that gets here: a
		// raw secret of exactly 64 characters PASSED and was persisted into the authority
		// journal verbatim. That is not a contrived width. `base64.RawURLEncoding` of 48
		// random bytes is exactly 64 characters, and 48 bytes is an entirely ordinary
		// choice for a machine-minted token — so the one input the field must never hold
		// had a natural spelling that cleared the check. `Event.validate` is the LAST
		// boundary before `WriteEvents` puts the value in an append-only, operator-readable
		// file with no undo, which is why the check belongs here rather than at each writer.
		//
		// 🔴 AND IT ACCEPTS BOTH CASES, WHICH IS A CORRECTION TO THIS GUARD'S FIRST DRAFT
		// RATHER THAN A CONCESSION. That draft required LOWERCASE and said the narrowing
		// "costs nothing that was ever alive"; that was measured false in the direction
		// that matters. It is a REPLAY check, so it fails the journal WHOLE: one
		// hand-written uppercase digest loads zero credentials and drops a pod's entire
		// control-plane authority, where the value it refuses could only ever have been a
		// single dead credential. `isHexDigest`'s own comment carries the alphabet
		// argument — a raw token is excluded by `-`, `_` and `g`..`z`, never by case — and
		// `apply` lowers the accepted value so that a record written in either spelling
		// authenticates.
		//
		// 🔴 THE MESSAGE REPORTS THE LENGTH AND NEVER THE VALUE. The whole premise of this
		// branch is that the field may be holding a live secret, so interpolating it would
		// re-stage that secret into the pod's stderr, the operator's scrollback and any
		// transcript capturing the run — the guard against writing a token to disk,
		// printing the token.
		if !isHexDigest(e.TokenHash) {
			return fmt.Errorf(
				"%s: token_hash is not a %d-character hex digest (it is %d character(s)) — the field holds the SHA-256 DIGEST of a credential and never the credential, and a value that is the right length but not hex is the shape a raw token takes when it is written to the field that was supposed to hold its digest. This journal is not a place a credential may ever land, and it is append-only: there is no undo. The value is deliberately not echoed here, because if that is what happened it is a live secret",
				e.Kind, HashHexLen, len(e.TokenHash))
		}
		return nil
	case EventCredentialRevoked:
		return need("credential_id", string(e.CredentialID))
	default:
		// 🔴 NO DEFAULT-ACCEPT ARM. An event kind this build does not know was
		// written by a build that knew more, and applying the ones around it while
		// skipping this one produces a model that is WRONG in an unknown direction
		// — quite possibly wider, if the skipped event was a revocation.
		return fmt.Errorf(
			"unknown event kind %q: this build defines %v — a journal written by a newer build is refused whole rather than replayed with the unrecognised records dropped, because a dropped revocation is a grant that keeps working",
			e.Kind, AllEventKinds)
	}
}

// apply folds one validated event into the model, in place.
//
// ⚠ IT MUTATES, AND IT IS THE ONLY THING THAT DOES. `Replay` owns the Model until
// it returns; nothing else in this package writes to one. Keeping the mutation to a
// single unexported function is what lets the type comment on Model promise that a
// published Model is immutable.
func (m *Model) apply(e Event) error {
	if err := e.validate(); err != nil {
		return err
	}
	switch e.Kind {
	case EventUserCreated:
		if _, exists := m.Users[e.UserID]; exists {
			return fmt.Errorf("user %s already exists", e.UserID)
		}
		// 🔴 TWO USER ROWS FOR ONE (provider, subject) IS AN AMBIGUITY THE
		// AUTHENTICATOR WOULD RESOLVE BY MAP ORDER. `UserByProviderSubject` scans
		// `m.Users` and returns the first match, and Go randomises map range order —
		// so a duplicated pair makes "who is this session" answer a DIFFERENT user id
		// on different requests inside one process, with a different authorization
		// each time. That is the same class as the credential-digest collision refused
		// below, and it is refused here for the same reason: there is no defined
		// precedence, so the journal declines to store the ambiguity rather than
		// letting the authenticator pick.
		//
		// ⚠ IT IS A UNIQUENESS RULE ON THE NATURAL KEY, WHICH `Event.validate` CANNOT
		// EXPRESS. `validate` sees one event and asks only whether its own fields are
		// present; whether the pair is already taken is a question about the model, so
		// it belongs here beside the other consistency checks.
		for id, u := range m.Users {
			if u.Provider == e.Provider && u.Subject == e.Subject {
				return fmt.Errorf(
					"user %s carries the same (provider, subject) pair as %s — the identity backends look a session up by exactly that pair, and two rows holding it make the answer depend on map iteration order",
					e.UserID, id)
			}
		}
		m.Users[e.UserID] = User{
			ID: e.UserID, Provider: e.Provider, Subject: e.Subject,
			Email: e.Email, CreatedAt: e.At,
		}

	case EventProjectCreated:
		if _, exists := m.Projects[e.ProjectID]; exists {
			return fmt.Errorf("project %s already exists", e.ProjectID)
		}
		if _, known := m.Users[e.UserID]; !known {
			return fmt.Errorf("project %s names owner %s, who does not exist", e.ProjectID, e.UserID)
		}
		m.Projects[e.ProjectID] = Project{
			ID: e.ProjectID, Name: e.Name, OwnerUserID: e.UserID,
			Solo: e.Solo, CreatedAt: e.At,
		}

	case EventMemberSet:
		if _, known := m.Projects[e.ProjectID]; !known {
			return fmt.Errorf("member-set names project %s, which does not exist", e.ProjectID)
		}
		if _, known := m.Users[e.UserID]; !known {
			return fmt.Errorf("member-set names user %s, who does not exist", e.UserID)
		}
		// AddedAt is preserved across a role change: the field answers "since when
		// has this person been in the project", and a promotion is not a join.
		added := e.At
		if prior, was := m.userProject[e.UserID][e.ProjectID]; was {
			added = prior.AddedAt
		}
		m.setMembership(Membership{
			UserID: e.UserID, ProjectID: e.ProjectID, Role: e.Role, AddedAt: added,
		})

	case EventMemberRemoved:
		m.dropMembership(e.UserID, e.ProjectID)

	case EventScopeCreated:
		if _, exists := m.Scopes[e.ScopeID]; exists {
			return fmt.Errorf("scope %s already exists", e.ScopeID)
		}
		if _, known := m.Projects[e.ProjectID]; !known {
			return fmt.Errorf("scope %s names project %s, which does not exist", e.ScopeID, e.ProjectID)
		}
		if other, clash := m.ScopeByNameIn(e.ProjectID, e.DisplayName); clash {
			return fmt.Errorf(
				"scope name %q is already used by %s in project %s — names are unique WITHIN a project so that a caller holding a project can resolve one without ambiguity",
				e.DisplayName, other.ID, e.ProjectID)
		}
		m.Scopes[e.ScopeID] = Scope{
			ID: e.ScopeID, DisplayName: e.DisplayName,
			ProjectID: e.ProjectID, CreatedAt: e.At,
		}

	case EventScopeRenamed:
		sc, known := m.Scopes[e.ScopeID]
		if !known {
			return fmt.Errorf("scope %s does not exist", e.ScopeID)
		}
		if other, clash := m.ScopeByNameIn(sc.ProjectID, e.DisplayName); clash && other.ID != sc.ID {
			return fmt.Errorf("scope name %q is already used by %s in project %s", e.DisplayName, other.ID, sc.ProjectID)
		}
		sc.DisplayName = e.DisplayName
		m.Scopes[e.ScopeID] = sc

	case EventScopeMoved:
		sc, known := m.Scopes[e.ScopeID]
		if !known {
			return fmt.Errorf("scope %s does not exist", e.ScopeID)
		}
		if _, known := m.Projects[e.ProjectID]; !known {
			return fmt.Errorf("scope %s cannot move to project %s, which does not exist", e.ScopeID, e.ProjectID)
		}
		if other, clash := m.ScopeByNameIn(e.ProjectID, sc.DisplayName); clash && other.ID != sc.ID {
			return fmt.Errorf(
				"scope %s cannot move to project %s: its name %q is already used there by %s",
				e.ScopeID, e.ProjectID, sc.DisplayName, other.ID)
		}
		sc.ProjectID = e.ProjectID
		m.Scopes[e.ScopeID] = sc

	case EventGranted:
		if _, exists := m.Grants[e.GrantID]; exists {
			return fmt.Errorf("grant %s already exists", e.GrantID)
		}
		if err := m.checkSubject(e.SubjectKind, e.SubjectID); err != nil {
			return err
		}
		if err := m.checkObject(e.ObjectKind, e.ObjectID); err != nil {
			return err
		}
		m.Grants[e.GrantID] = Grant{
			ID: e.GrantID, SubjectKind: e.SubjectKind, SubjectID: e.SubjectID,
			ObjectKind: e.ObjectKind, ObjectID: e.ObjectID, Verbs: e.Verbs,
			GrantedBy: e.Actor, GrantedAt: e.At,
		}

	case EventGrantRevoked:
		g, known := m.Grants[e.GrantID]
		if !known {
			return fmt.Errorf("grant %s does not exist", e.GrantID)
		}
		if !g.Live() {
			// Idempotent on purpose: a double revoke is an operator retrying a
			// thing they want to be true, and refusing it would make the safe
			// action the one that errors.
			break
		}
		at := e.At
		g.RevokedAt = &at
		g.RevokedBy = e.Actor
		m.Grants[e.GrantID] = g

	case EventCredentialIssued:
		if _, exists := m.Credentials[e.CredentialID]; exists {
			return fmt.Errorf("credential %s already exists", e.CredentialID)
		}
		if err := m.checkSubject(e.SubjectKind, e.SubjectID); err != nil {
			return err
		}
		// 🔴 THE DIGEST IS LOWERED HERE, AND EVERY USE BELOW READS THE NORMALISED
		// VALUE RATHER THAN THE RECORDED ONE. `validate` accepts either case
		// because case does not discriminate the hazard it guards (see
		// `isHexDigest`); this is the half that makes accepting it correct rather
		// than merely permissive, and there are TWO independent reasons, so
		// closing one does not remove the need for it:
		//
		//   - THE DUPLICATE REFUSAL BELOW IS A STRING COMPARE. Without lowering
		//     first, the SAME secret issued twice in two spellings is two rows
		//     this arm accepts — and the loop's own comment says two principals
		//     sharing a digest have no defined precedence at authentication time.
		//     A case difference must not be the thing that reaches that state.
		//   - `EqualHash` IS BYTE-EXACT and `HashToken` emits lowercase (`%x`), so
		//     an uppercase digest stored verbatim matches no presented token
		//     EVER. Lowering it turns a hand-appended credential that replayed
		//     clean and authenticated nobody into one that works — a fix, not
		//     merely compatibility.
		//
		// ⚠ THE JOURNAL LINE IS NOT REWRITTEN, AND CANNOT BE. The file is
		// append-only; this normalises what the MODEL holds, which is what
		// `Resolve` and `Authenticate` read.
		digest := normalizedDigest(e.TokenHash)
		// 🔴 A HASH COLLISION HERE IS A SHARED TOKEN, NOT A HASH FAILURE. sha256
		// does not collide by accident; two rows carrying one digest means one
		// secret was issued twice, and `Resolve` would then have to choose which
		// principal a request belongs to. There is no defined precedence, so the
		// journal refuses the second issue rather than storing an ambiguity the
		// authenticator would resolve arbitrarily.
		for id, c := range m.Credentials {
			if c.TokenHash == digest {
				return fmt.Errorf(
					"credential %s carries the same token digest as %s — one secret issued to two principals has no defined precedence at authentication time, so it is refused here rather than resolved arbitrarily there",
					e.CredentialID, id)
			}
		}
		m.Credentials[e.CredentialID] = Credential{
			ID: e.CredentialID, PrincipalKind: e.SubjectKind, PrincipalID: e.SubjectID,
			TokenHash: digest, Label: e.Label,
			// `copyIDs`, NOT `append([]ID(nil), …)` — the latter flattens a
			// non-nil empty narrowing into nil, which is its opposite. See the
			// function's own comment; this is the site that motivates it.
			NarrowedScopes: copyIDs(e.NarrowedScopes),
			CreatedAt:      e.At,
		}

	case EventCredentialRevoked:
		c, known := m.Credentials[e.CredentialID]
		if !known {
			return fmt.Errorf("credential %s does not exist", e.CredentialID)
		}
		if !c.Live() {
			break
		}
		at := e.At
		c.RevokedAt = &at
		m.Credentials[e.CredentialID] = c

	default:
		// Unreachable: `validate` refuses an unknown kind above. Restated rather
		// than omitted so a kind added to `validate` and forgotten here fails
		// loudly instead of being silently skipped.
		return fmt.Errorf("event kind %q validated but has no apply arm", e.Kind)
	}

	m.Epoch++
	m.At = e.At
	return nil
}

func (m *Model) setMembership(ms Membership) {
	if m.Memberships[ms.ProjectID] == nil {
		m.Memberships[ms.ProjectID] = map[ID]Membership{}
	}
	if m.userProject[ms.UserID] == nil {
		m.userProject[ms.UserID] = map[ID]Membership{}
	}
	m.Memberships[ms.ProjectID][ms.UserID] = ms
	m.userProject[ms.UserID][ms.ProjectID] = ms
}

func (m *Model) dropMembership(user, project ID) {
	delete(m.Memberships[project], user)
	delete(m.userProject[user], project)
	if len(m.Memberships[project]) == 0 {
		delete(m.Memberships, project)
	}
	if len(m.userProject[user]) == 0 {
		delete(m.userProject, user)
	}
}

func (m *Model) checkSubject(kind Kind, id ID) error {
	switch kind {
	case KindUser:
		if _, known := m.Users[id]; !known {
			return fmt.Errorf("subject user %s does not exist", id)
		}
	case KindProject:
		if _, known := m.Projects[id]; !known {
			return fmt.Errorf("subject project %s does not exist", id)
		}
	default:
		return fmt.Errorf("unknown subject kind %q", kind)
	}
	return nil
}

func (m *Model) checkObject(kind ObjectKind, id ID) error {
	switch kind {
	case ObjectScope:
		if _, known := m.Scopes[id]; !known {
			return fmt.Errorf("object scope %s does not exist", id)
		}
	case ObjectProject:
		if _, known := m.Projects[id]; !known {
			return fmt.Errorf("object project %s does not exist", id)
		}
	default:
		return fmt.Errorf("unknown object kind %q", kind)
	}
	return nil
}

// Replay folds a sequence of events into a Model.
//
// 🔴 IT FAILS WHOLE ON THE FIRST BAD EVENT, RETURNING NO MODEL. Returning a
// partially-replayed model alongside an error is the shape that gets used: a caller
// logs the error and serves the model, and the model is missing every event after
// the failure — which, if one of them was a revocation, is an authority WIDER than
// the journal describes. There is no partial answer here, on purpose.
func Replay(events []Event) (Model, error) {
	m := NewModel()
	for i, e := range events {
		if err := m.apply(e); err != nil {
			return Model{}, fmt.Errorf("event %d (%s): %w", i+1, e.Kind, err)
		}
	}
	return m, nil
}

// WriteEvents appends events to w, one JSON object per line.
//
// The format is newline-delimited JSON rather than one array, because the journal
// is APPENDED to: an array would have to be rewritten whole on every write, which
// turns "append a grant" into "rewrite the authority" and puts a truncation window
// in front of the one file that must never lose a revocation.
func WriteEvents(w io.Writer, events []Event) error {
	enc := json.NewEncoder(w)
	for i, e := range events {
		if err := e.validate(); err != nil {
			return fmt.Errorf("event %d: %w", i+1, err)
		}
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("event %d: %w", i+1, err)
		}
	}
	return nil
}

// ReadEvents parses a newline-delimited journal.
//
// ⚠ A BLANK LINE IS SKIPPED; ANYTHING ELSE UNPARSEABLE IS AN ERROR. Tolerating a
// malformed line would mean serving an authority with a hole in it, and a hole in
// an append-only log is indistinguishable from a revocation that never happened.
func ReadEvents(r io.Reader) ([]Event, error) {
	var out []Event
	sc := bufio.NewScanner(r)
	// The default 64 KiB token limit is well above any event this package writes,
	// but a journal line is operator-influenced (labels, names), so the cap is
	// raised rather than left to truncate a long line into a parse error that
	// names the wrong problem.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Bytes()
		if len(trimSpace(text)) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(text, &e); err != nil {
			return nil, fmt.Errorf("journal line %d: %w", line, err)
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func trimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
