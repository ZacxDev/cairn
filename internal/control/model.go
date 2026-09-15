// Package control is cairn's control plane: who exists, what they may see, and
// how that was decided.
//
// 🔴 WHAT THIS PACKAGE REPLACES, STATED SO THE SCOPE IS NOT INFERRED. Authorization
// today is a hand-edited token file reloaded on SIGHUP: one row maps one credential
// to one identity and one literal scope allowlist. Every question this package
// exists to answer — invite a user, share a scope, move a scope into a project,
// revoke one person's access without rotating everybody's token — is MUTABLE,
// SELF-SERVICE, RUNTIME state that a file an operator edits by hand cannot carry.
//
// 🔴 AND IT DELIBERATELY STOPS AT THE LIBRARY BOUNDARY. Nothing here imports
// `net/http`, opens a socket or knows a server exists — the same shape
// `internal/report` has, and for the same reason: the pod, the CLI and (later) the
// UI must consult ONE authorization decision rather than three implementations of
// it that agree by review. Wiring this into `internal/api` is a separate change,
// because replacing the token path is a behaviour change and this is not.
//
// # The one property everything else is built to preserve
//
// A principal's authority is DERIVED, never enumerated beside the credential. A
// credential binds to a principal; what that principal may see is computed from
// membership and grants at the moment it is asked. The alternative — a credential
// row that also lists its scopes, which is what the token file does — is two
// structures holding one fact, and they drift. That is the one-rule-one-place
// failure this repository has already paid for in the reader.
package control

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"
)

// Verb is what a principal may DO to a scope. The set is closed at three.
//
// 🔴 THREE, NOT FOUR, AND `append` IS THE ONE DELIBERATELY ABSENT. The store
// enforces no distinction between appending a bullet and replacing an entry —
// `append`, `put` and `create` all mutate a curated file through the same write
// path — so a fourth verb would be SPELLED rather than STRUCTURAL: a word in the
// grant table that no call site can branch on, which is the guard shape this
// repository's rules name explicitly as walkable.
//
// ⚠ The asymmetry that makes starting narrow the safe direction: the journal is
// append-only, so a fourth verb LATER is a new event carrying it and costs nothing
// retroactive. A verb issued today and regretted tomorrow is in grants that cannot
// be edited, only tombstoned — so the expensive mistake is minting one early, and
// this comment is the record of that being a decision rather than an oversight.
type Verb string

const (
	// VerbRead sees a scope: its index, its entries, its search results, and its
	// bytes in a snapshot.
	VerbRead Verb = "read"
	// VerbWrite mutates entries within a scope.
	VerbWrite Verb = "write"
	// VerbAdmin shares a scope with somebody else, and revokes that sharing.
	VerbAdmin Verb = "admin"
)

// AllVerbs is the closed set, in the order every rendered list uses.
//
// Declared once and ranged over rather than restated at each site: a second
// spelling of "all the verbs" is how a new verb gets added to the model and missed
// by the matrix that is supposed to be measuring it.
var AllVerbs = []Verb{VerbRead, VerbWrite, VerbAdmin}

// Valid answers whether v is a verb this package defines.
//
// 🔴 EXHAUSTIVE, WITH NO DEFAULT ARM. The precedent is `store.classify`, whose
// docstring records that a default is how the last four rounds of that defect
// happened: an unmapped value silently taking the permissive branch. An unknown
// verb here is not a verb, and callers are expected to refuse rather than guess.
func (v Verb) Valid() bool {
	switch v {
	case VerbRead, VerbWrite, VerbAdmin:
		return true
	default:
		return false
	}
}

// Role is a user's standing inside a project.
type Role string

const (
	// RoleOwner created the project, and is the only role that can delete it or
	// transfer it. On the project's own scopes an owner is an admin.
	RoleOwner Role = "owner"
	// RoleAdmin may share the project's scopes and manage its membership.
	RoleAdmin Role = "admin"
	// RoleMember may read and write the project's scopes and nothing else.
	RoleMember Role = "member"
)

// roleVerbs is the ONE table mapping a role to what it may do to the project's own
// scopes.
//
// 🔴 OWNER AND ADMIN ARE IDENTICAL HERE, AND THAT IS NOT AN OVERSIGHT. The two
// differ at the PROJECT object — only an owner may delete or transfer — and that
// question is asked by `Model.MayAdministerProject`, not by a scope verb. Giving
// them different scope verbs to make the roles "look different" would put the
// owner/admin distinction in two places with no call site able to justify the
// second.
//
// 🔴 A ROLE ABSENT FROM THIS MAP GRANTS NOTHING. `resolve` reads it with the
// two-value form and treats a miss as the empty set, so a role string that reaches
// the journal without a mapping fails CLOSED. The map is the enumeration; there is
// no default arm anywhere that consults it.
var roleVerbs = map[Role]VerbSet{
	RoleOwner:  NewVerbSet(VerbRead, VerbWrite, VerbAdmin),
	RoleAdmin:  NewVerbSet(VerbRead, VerbWrite, VerbAdmin),
	RoleMember: NewVerbSet(VerbRead, VerbWrite),
}

// Valid answers whether r is a role this package defines.
func (r Role) Valid() bool {
	_, known := roleVerbs[r]
	return known
}

// Kind discriminates the two things that can hold authority.
//
// 🔴 A PROJECT IS A FIRST-CLASS SUBJECT, NOT SUGAR OVER ITS MEMBERS. Expanding
// "share with this project" into one grant per member at grant time would freeze
// the membership as it stood that day: every later join silently misses the share
// and every leave silently keeps it. Grants name the project; the expansion happens
// at RESOLVE time, against today's membership.
type Kind string

const (
	// KindUser is a person.
	KindUser Kind = "user"
	// KindProject is a project acting as a principal — the service-account case,
	// and the subject of a team share.
	KindProject Kind = "project"
)

// Valid answers whether k is a kind this package defines.
func (k Kind) Valid() bool { return k == KindUser || k == KindProject }

// ObjectKind discriminates what a grant is ABOUT.
type ObjectKind string

const (
	// ObjectScope is a grant over one scope.
	ObjectScope ObjectKind = "scope"
	// ObjectProject is a grant over every scope the project holds — including
	// scopes added to it AFTER the grant, which is the whole point of granting a
	// project rather than a list of scopes.
	ObjectProject ObjectKind = "project"
)

// Valid answers whether k is an object kind this package defines.
func (k ObjectKind) Valid() bool { return k == ObjectScope || k == ObjectProject }

// ID is an opaque, immutable handle. Its PREFIX names what it identifies, so a
// project id cannot be passed where a scope id is expected without the mistake
// being visible in any log line, error or journal record that carries it.
type ID string

// Entities.
//
// ⚠ Every one of these is DERIVED STATE, rebuilt by replaying the journal. Nothing
// mutates one in place; the journal is the authority and these are its projection.
// That is what makes "who granted whom what, when, and who revoked it" answerable
// after the fact rather than backfilled, which is the difference between validated
// access control and asserted access control.
type (
	// User is a person, identified by the identity provider that vouched for them.
	//
	// ⚠ `Provider`+`Subject` is the natural key and `Email` is NOT: an email is
	// mutable at the provider and is reused across providers, so keying on it
	// merges two people who share an address at two IdPs. Email is carried for
	// display and for invites only.
	User struct {
		ID        ID
		Provider  string
		Subject   string
		Email     string
		CreatedAt time.Time
	}

	// Project owns scopes and carries membership.
	//
	// `Solo` marks the project auto-created for a user at signup. It is a normal
	// project in every respect — the flag exists so the UI can present it as
	// "personal" rather than as a team, and so a deletion path can refuse to leave
	// a user with no project. NOTHING in authorization branches on it, which is
	// deliberate: a second class of project would be a second resolution path.
	Project struct {
		ID          ID
		Name        string
		OwnerUserID ID
		Solo        bool
		CreatedAt   time.Time
	}

	// Membership is one user's standing in one project.
	Membership struct {
		UserID    ID
		ProjectID ID
		Role      Role
		AddedAt   time.Time
	}

	// Scope is a directory of entries. Its ID is immutable; its NAME and its
	// project binding are not.
	//
	// 🔴 THE PATH ON DISK STAYS THE DISPLAY NAME. Renaming the directory to an
	// opaque id would cost the two things that make a local replica worth having:
	// it stops being human-browsable (people `cat` these files, and that is a
	// feature), and every rendered header would need an id-to-name lookup to say
	// anything legible. So the id is the identity for authorization and for sync
	// reconciliation, and the path is a PROJECTION a syncing client repairs when
	// it sees a known id at a new name.
	Scope struct {
		ID          ID
		DisplayName string
		ProjectID   ID
		CreatedAt   time.Time
	}

	// Grant is one act of sharing. Append-only: revoking writes a tombstone.
	//
	// 🔴 `RevokedAt` IS A TOMBSTONE, NOT A DELETE, AND THE TABLE IS THE AUTHORITY.
	// The current view is derived from these rows; the rows are what makes the
	// grant log answerable. A deleted row cannot be backfilled later, and without
	// it "who could see this, and when" has no answer at all.
	Grant struct {
		ID          ID
		SubjectKind Kind
		SubjectID   ID
		ObjectKind  ObjectKind
		ObjectID    ID
		Verbs       VerbSet
		GrantedBy   ID
		GrantedAt   time.Time
		RevokedAt   *time.Time
		RevokedBy   ID
	}

	// Credential is a bearer token's record. The token itself is never stored.
	//
	// 🔴 `NarrowedScopes` MAY ONLY INTERSECT, NEVER WIDEN — and that is enforced
	// in `resolve` by intersecting, not by validating at issue time. Validating at
	// issue time would be a claim about the authority as it stood THEN; a
	// credential whose principal has since lost a scope must stop seeing it, and
	// an intersection is the only spelling that keeps being true.
	//
	// ⚠ `nil` means NO NARROWING and an EMPTY non-nil slice means NOTHING IS
	// VISIBLE. They are opposites, exactly as `store.ScopeSet`'s nil-versus-
	// unrestricted asymmetry is, and for the same reason: conflating "nobody set
	// this" with "everything" is the failure the whole fail-closed direction
	// exists to make impossible.
	Credential struct {
		ID            ID
		PrincipalKind Kind
		PrincipalID   ID
		// TokenHash is the hex sha256 of the bearer token. See `HashToken`.
		TokenHash string
		// Label is what a human calls this credential in the UI. Never secret.
		Label          string
		NarrowedScopes []ID
		CreatedAt      time.Time
		RevokedAt      *time.Time
	}
)

// Live answers whether a grant is in force. A revoked grant stays in the model and
// stays out of every authority computation.
func (g Grant) Live() bool { return g.RevokedAt == nil }

// Live answers whether a credential may still authenticate.
func (c Credential) Live() bool { return c.RevokedAt == nil }

// Model is the whole control plane at one point in time.
//
// 🔴 IT CARRIES ITS OWN EPOCH, AND THAT IS WHAT MAKES STALENESS REPORTABLE RATHER
// THAN ASSUMED. A materialized copy of this served from a pod cannot know it is
// behind; a copy that carries the epoch it was built from can be COMPARED, which
// is the difference between bounding revocation lag and hoping about it.
//
// ⚠ A Model is a VALUE and is never mutated after construction. Replay builds one
// and hands it over; every reader gets the same consistent world. Nothing here is
// safe to mutate concurrently because nothing here is meant to be mutated at all.
type Model struct {
	// Epoch is the number of journal events applied to build this Model. It is
	// monotonic and it is meaningful only against the same journal.
	Epoch uint64
	// At is the timestamp of the LAST applied event — the model's own clock, not
	// the wall clock of whoever is reading it. Zero for an empty journal.
	At time.Time

	Users       map[ID]User
	Projects    map[ID]Project
	Scopes      map[ID]Scope
	Grants      map[ID]Grant
	Credentials map[ID]Credential
	// Memberships is keyed by project, then by user. Both directions are needed —
	// "who is in this project" for expanding a project grant, "what projects am I
	// in" for resolving a user — so both indexes are built once here rather than
	// scanned at every resolve.
	Memberships map[ID]map[ID]Membership
	userProject map[ID]map[ID]Membership
}

// NewModel returns an empty, usable Model. Every map is non-nil, so a caller never
// has to distinguish "no users" from "uninitialised".
func NewModel() Model {
	return Model{
		Users:       map[ID]User{},
		Projects:    map[ID]Project{},
		Scopes:      map[ID]Scope{},
		Grants:      map[ID]Grant{},
		Credentials: map[ID]Credential{},
		Memberships: map[ID]map[ID]Membership{},
		userProject: map[ID]map[ID]Membership{},
	}
}

// clone deep-copies a Model.
//
// 🔴 A STRUCT COPY OF A Model IS NOT A COPY, AND THIS FUNCTION EXISTS BECAUSE THE
// FIRST VERSION OF `FileStore.Append` GOT IT WRONG. Every field here is a map, so
// `next := current` copies six map HEADERS and leaves both names pointing at one
// set of buckets. `Append` validates a batch by applying it to what it believes is
// a scratch copy — and with a shallow copy that scratch IS the live cache, so a
// batch rejected at its third event leaves the first two permanently applied to the
// authority the process is serving, with no journal record of them. The model would
// then be WIDER than the file it claims to be a projection of, and a restart would
// silently "lose" grants that were never written.
//
// ⚠ The cost is paid once per append, never per request: reads share the cached
// Model and are promised (by this type's comment) not to mutate it.
func (m Model) clone() Model {
	// Start from a fully-initialised empty world, so a zero-value Model — whose
	// maps are all nil — clones into something `apply` can write to rather than
	// into another zero value that panics on first assignment.
	out := NewModel()
	out.Epoch = m.Epoch
	out.At = m.At
	maps.Copy(out.Users, m.Users)
	maps.Copy(out.Projects, m.Projects)
	maps.Copy(out.Scopes, m.Scopes)
	maps.Copy(out.Grants, m.Grants)
	maps.Copy(out.Credentials, m.Credentials)
	// The two membership indexes are maps OF maps, so `maps.Copy` at this level
	// would share the inner maps — the same shallow-copy trap one nesting level
	// down, reproducing the exact defect this function was written to fix. Walk
	// them.
	for k, inner := range m.Memberships {
		out.Memberships[k] = maps.Clone(inner)
	}
	for k, inner := range m.userProject {
		out.userProject[k] = maps.Clone(inner)
	}
	return out
}

// MembershipsOf returns the projects a user belongs to, keyed by project id.
//
// Returns a fresh map rather than the internal index, so a caller cannot mutate the
// Model by holding onto the result.
func (m Model) MembershipsOf(user ID) map[ID]Membership {
	out := map[ID]Membership{}
	maps.Copy(out, m.userProject[user])
	return out
}

// ScopesIn returns the ids of the scopes a project holds, sorted.
//
// Sorted because it feeds rendered output and grant expansion, and an unsorted map
// range would make the same world produce two different orders — which in this
// repository is the failure mode that reads as a stale cache.
func (m Model) ScopesIn(project ID) []ID {
	var out []ID
	for id, sc := range m.Scopes {
		if sc.ProjectID == project {
			out = append(out, id)
		}
	}
	sortIDs(out)
	return out
}

// MayAdministerProject answers the PROJECT-level question that no scope verb
// carries: may this user change the project itself — its membership, its name, its
// existence?
//
// 🔴 THIS IS WHERE OWNER AND ADMIN DIVERGE, AND IT IS THE ONLY PLACE. `roleVerbs`
// gives them identical authority over scopes on purpose; the difference is here,
// asked by one function, so the distinction cannot be spelled two ways.
func (m Model) MayAdministerProject(user, project ID) bool {
	ms, in := m.userProject[user][project]
	if !in {
		return false
	}
	return ms.Role == RoleOwner || ms.Role == RoleAdmin
}

// MayDeleteProject answers the narrower question only an owner may answer yes to.
func (m Model) MayDeleteProject(user, project ID) bool {
	ms, in := m.userProject[user][project]
	return in && ms.Role == RoleOwner
}

// ScopeByName resolves a display name to a scope, reporting ambiguity rather than
// picking.
//
// 🔴 IT REFUSES A DUPLICATE NAME RATHER THAN CHOOSING ONE. Display names are
// mutable and are NOT unique across projects — two projects may each hold a scope
// called `notes`, and that is legitimate. A resolver that silently picked the first
// would hand one project's caller the other project's scope id, which is a
// cross-tenant read dressed as a lookup. Callers that have a project in hand should
// use ScopeByNameIn.
func (m Model) ScopeByName(name string) (Scope, error) {
	var found []Scope
	for _, sc := range m.Scopes {
		if sc.DisplayName == name {
			found = append(found, sc)
		}
	}
	switch len(found) {
	case 0:
		return Scope{}, fmt.Errorf("no scope named %q", name)
	case 1:
		return found[0], nil
	default:
		sort.Slice(found, func(i, j int) bool { return found[i].ID < found[j].ID })
		ids := make([]string, 0, len(found))
		for _, sc := range found {
			ids = append(ids, string(sc.ID))
		}
		return Scope{}, fmt.Errorf(
			"scope name %q is ambiguous: %d scopes carry it (%v) — a display name is not unique across projects, so resolve it within a project",
			name, len(found), ids)
	}
}

// ScopeByNameIn resolves a display name within one project, where names ARE unique
// — `apply` refuses a create or a rename that would collide inside a project.
func (m Model) ScopeByNameIn(project ID, name string) (Scope, bool) {
	for _, sc := range m.Scopes {
		if sc.ProjectID == project && sc.DisplayName == name {
			return sc, true
		}
	}
	return Scope{}, false
}

func sortIDs(ids []ID) { slices.Sort(ids) }
