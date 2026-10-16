package control

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"
)

// Group federation: an OPT-IN mode in which an issuer this deployment trusts asserts a
// caller's group membership in a verified token, and the operator's group map — held in
// this journal and nowhere else — decides what those groups reach.
//
// 🔴 IT IS A DELIBERATE REVERSAL OF "NOTHING READS A GROUPS CLAIM", AND IT IS BOUNDED BY
// THREE RULES THAT ARE STRUCTURAL RATHER THAN DOCUMENTED:
//
//  1. THE CLAIM ONLY SELECTS. A claimed group with no live mapping contributes nothing; a
//     token cannot name a scope, a project or a verb — only a group, which reaches exactly
//     what the operator mapped it to. So the worst an issuer that lies can do is claim a
//     group the caller is not in, which reaches that group's mapped objects and no others.
//  2. NOTHING IS WRITTEN ON A READ. A federated principal is computed per request from the
//     claim and the map, so there is no account to provision, no row to clean up and no
//     read route that becomes a user-creation endpoint (the hazard
//     `UserByProviderSubject`'s comment refuses). `TestFederatedResolutionWritesNothing`
//     counts the journal's events across a battery of authentications.
//  3. ONE FUNCTION DECIDES. `ResolveFederated` computes the authority with the same
//     `addObject` expansion `Resolve` uses for a grant, so "which scopes does a project
//     object cover" has one answer — including the scopes created AFTER the mapping.

// GroupMapKey identifies one mapping: a (provider, group) pair reaching one object.
//
// ⚠ THE PROVIDER IS PART OF THE KEY FOR `User`'s REASON. A group name is the issuer's
// vocabulary, not a global one, so `ops` at one issuer and `ops` at another are two
// different groups — and the provider string here must equal the verifier's
// `CAIRN_OIDC_PROVIDER` namespace, which nothing in either program can check (the same
// two-halves-of-one-key caveat `-create-user -provider` carries).
type GroupMapKey struct {
	Provider   string
	Group      string
	ObjectKind ObjectKind
	ObjectID   ID
}

// GroupMap is one live mapping.
type GroupMap struct {
	GroupMapKey
	Verbs    VerbSet
	MappedBy ID
	MappedAt time.Time
}

// MaxGroupNameLen and MaxClaimedGroups bound a groups claim.
const (
	MaxGroupNameLen  = 40
	MaxClaimedGroups = 64
)

// groupName is the alphabet a group name must be spelled in.
//
// 🔴 NARROW ON PURPOSE, AND IT IS ONE EXPRESSION FOR BOTH SIDES OF THE MAP. The operator
// writes a group with `-map-group` and the issuer asserts one in a token; if the two were
// checked by different rules, a name one side accepts and the other cannot spell is a
// mapping that can never match — silently. Lowercase, digits and `-` only, so there is no
// case fold to get wrong and no separator (`/`, `:`, `,`) a renderer could split on.
var groupName = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// ErrGroupName refuses a group name outside `groupName`.
var ErrGroupName = errors.New("control: not a usable group name")

// ValidGroupName is THE predicate for a group name, used by the journal, by the operator
// mode and by the token-claim parser. See `groupName`.
func ValidGroupName(name string) error {
	if !groupName.MatchString(name) {
		return fmt.Errorf("%w: %q must match %s", ErrGroupName, name, groupName)
	}
	return nil
}

// ErrGroupsClaim refuses a claimed group set that is not a list of at most
// `MaxClaimedGroups` valid names.
var ErrGroupsClaim = errors.New("control: the groups claim is malformed")

// NormalizeClaimedGroups validates and canonicalises a claimed group list: every name
// valid, at most `MaxClaimedGroups` DISTINCT names, deduplicated and sorted.
//
// 🔴 ONE BAD NAME REFUSES THE WHOLE LIST, NEVER "THE GOOD ONES". Dropping the names that
// fail and keeping the rest would make a claim the issuer got wrong read as a smaller,
// valid one — a partial read nobody can see happened. A refusal is a 401, which is loud.
// The count bound is checked AFTER deduplication so a repeated name is not a refusal, and
// the list is sorted so two spellings of one set resolve identically.
func NormalizeClaimedGroups(groups []string) ([]string, error) {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		if err := ValidGroupName(g); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrGroupsClaim, err)
		}
		out = append(out, g)
	}
	slices.Sort(out)
	out = slices.Compact(out)
	if len(out) > MaxClaimedGroups {
		return nil, fmt.Errorf("%w: %d distinct groups, at most %d", ErrGroupsClaim, len(out), MaxClaimedGroups)
	}
	return out, nil
}

// FederationStatus is the distinguishable outcome of a federated resolution.
type FederationStatus string

const (
	// FederationNoGroups: the token claimed no group at all. Authenticated; reaches nothing.
	FederationNoGroups FederationStatus = "no-groups"
	// FederationGroupsUnmapped: the token claimed groups and NONE has a live mapping.
	// Authenticated; reaches nothing.
	FederationGroupsUnmapped FederationStatus = "groups-unmapped"
	// FederationMapped: at least one claimed group has a live mapping. What it reaches is
	// the union of every mapped claimed group's objects — which may still be no scope, if
	// a mapped project holds none.
	FederationMapped FederationStatus = "mapped"
)

// Federation reports how a federated principal's authority was computed.
//
// 🔴 IT IS DATA ABOUT THE CALLER'S OWN CLAIM, AND THAT IS WHAT MAKES IT SAFE TO SHOW THE
// CALLER. `Mapped`/`Unmapped` partition the groups THEY claimed; nothing here names a group
// they did not claim or an object they cannot reach, so serving it is a self-view rather
// than an enumeration API over the map.
type Federation struct {
	Provider string
	Subject  string
	Status   FederationStatus
	// Mapped and Unmapped partition the claimed groups, each sorted.
	Mapped   []string
	Unmapped []string
}

// Federation reports whether `ResolveFederated` produced this authority, and how.
func (a Authorization) Federation() (Federation, bool) {
	if a.federation == nil {
		return Federation{}, false
	}
	f := *a.federation
	f.Mapped = slices.Clone(f.Mapped)
	f.Unmapped = slices.Clone(f.Unmapped)
	return f, true
}

// PrefixFederated is the id prefix of a federated principal.
const PrefixFederated = "fed"

// ErrFederatedSubject refuses an empty provider or subject.
var ErrFederatedSubject = errors.New("control: a federated principal needs a provider and a subject")

// ResolveFederated computes a federated principal and its authority from a VERIFIED
// token's (provider, subject) and its claimed groups.
//
// 🔴 THE AUTHORITY IS ∪ OVER THE CLAIMED GROUPS OF EACH LIVE MAPPING'S OBJECT, AT THE
// MAPPING'S VERBS, AND NOTHING ELSE. No membership, no grant, no credential narrowing: a
// federated principal is not a subject of any of those (see `KindFederated`), so the only
// source is the map. Every group is consulted — a mutant that stops at the first mapped
// group, or that counts an unmapped one, is a row in `tests/control_mutants.py`.
//
// ⚠ THE PRINCIPAL ID IS DERIVED FROM (provider, subject), SO ONE CALLER IS ONE ID ACROSS
// REQUESTS AND RESTARTS — what an audit trail needs to correlate — and it names no journal
// row, because none exists. `Display` is `<provider>:<subject>`.
//
// It refuses (never "resolves to nothing") a malformed group list, through
// `NormalizeClaimedGroups`, so a caller cannot reach this function with a partial claim.
func ResolveFederated(m Model, provider, subject string, claimed []string) (Principal, Authorization, error) {
	if provider == "" || subject == "" {
		return Principal{}, Authorization{}, ErrFederatedSubject
	}
	groups, err := NormalizeClaimedGroups(claimed)
	if err != nil {
		return Principal{}, Authorization{}, err
	}

	a := Authorization{byScope: map[ID]VerbSet{}, names: map[ID]string{}, Epoch: m.Epoch}
	report := Federation{Provider: provider, Subject: subject, Mapped: []string{}, Unmapped: []string{}}
	for _, g := range groups {
		mapped := false
		for _, gm := range sortedGroupMaps(m) {
			if gm.Provider != provider || gm.Group != g {
				continue
			}
			mapped = true
			a.addObject(m, gm.ObjectKind, gm.ObjectID, gm.Verbs)
		}
		if mapped {
			report.Mapped = append(report.Mapped, g)
		} else {
			report.Unmapped = append(report.Unmapped, g)
		}
	}
	switch {
	case len(groups) == 0:
		report.Status = FederationNoGroups
	case len(report.Mapped) == 0:
		report.Status = FederationGroupsUnmapped
	default:
		report.Status = FederationMapped
	}
	a.finish(m)
	a.federation = &report

	display := provider + ":" + subject
	p := Principal{
		Kind:    KindFederated,
		ID:      DerivedID(PrefixFederated, provider+"\x00"+subject),
		Display: display,
	}
	return p, a, nil
}

// sortedGroupMaps iterates the map in a fixed order, for `sortedGrants`' reason.
func sortedGroupMaps(m Model) []GroupMap {
	out := make([]GroupMap, 0, len(m.GroupMaps))
	for _, gm := range m.GroupMaps {
		out = append(out, gm)
	}
	slices.SortFunc(out, func(x, y GroupMap) int {
		for _, c := range [][2]string{
			{x.Provider, y.Provider}, {x.Group, y.Group},
			{string(x.ObjectKind), string(y.ObjectKind)}, {string(x.ObjectID), string(y.ObjectID)},
		} {
			if c[0] != c[1] {
				if c[0] < c[1] {
					return -1
				}
				return 1
			}
		}
		return 0
	})
	return out
}

// UsersUnderProvider counts journal users created under `provider`.
//
// ⚠ IT EXISTS FOR ONE WARNING: with group federation on, every token from the verifier
// resolves federated, so a journal user under the same provider namespace is no longer
// reachable through that verifier. See `internal/identity/README.md`.
func (m Model) UsersUnderProvider(provider string) int {
	n := 0
	for _, u := range m.Users {
		if u.Provider == provider {
			n++
		}
	}
	return n
}

// The operator-mode refusals. Each is its own sentinel so a caller can branch and a test
// can watch THAT ONE fire.
var (
	// ErrNoSuchObject refuses a mapping to an object the journal does not hold.
	ErrNoSuchObject = errors.New("control: no such scope or project in this control journal")
	// ErrGroupAlreadyMapped refuses a second live mapping of one key.
	ErrGroupAlreadyMapped = errors.New("control: that group is already mapped to that object")
	// ErrGroupNotMapped refuses an unmap of a key with no live mapping.
	ErrGroupNotMapped = errors.New("control: that group has no live mapping to that object")
	// ErrGroupMapVerbs refuses a mapping that confers nothing.
	ErrGroupMapVerbs = errors.New("control: a group mapping needs at least one verb")
)

// GroupMapRequest is a request to map or unmap one (provider, group) → object.
type GroupMapRequest struct {
	Provider   string
	Group      string
	ObjectKind ObjectKind
	ObjectID   ID
	// Verbs is required for a map and must be empty for an unmap.
	Verbs VerbSet
	// Actor is recorded on the event. Optional.
	Actor ID
	// At is the event's timestamp. Zero means now.
	At time.Time
}

// GroupMapWritten is what `MapGroup` or `UnmapGroup` wrote.
type GroupMapWritten struct {
	Key   GroupMapKey
	Verbs VerbSet
	Epoch uint64
}

// MapGroup appends ONE `group-mapped` record.
//
// 🔴 IT WIDENS THE AUTHORITY OF EVERY CALLER WHOSE TOKEN CLAIMS THE GROUP, ON THEIR NEXT
// REQUEST, AND A PROJECT OBJECT WIDENS IT AGAIN EVERY TIME A SCOPE IS ADDED TO THE PROJECT.
// That is what mapping a group to a project means — it is how a reserved admin group is
// made to reach every scope, present and future, without editing the map — and it is the
// thing to check before running it.
//
// The early refusals below have sentinels; `apply` refuses the same events again under the
// journal's lock, so a concurrent writer cannot slip a duplicate past them.
func MapGroup(ctx context.Context, s Store, req GroupMapRequest) (GroupMapWritten, error) {
	return writeGroupMap(ctx, s, req, EventGroupMapped)
}

// UnmapGroup appends ONE `group-unmapped` record. It narrows on the next request of every
// caller claiming the group; no token is revoked, because none was issued here.
func UnmapGroup(ctx context.Context, s Store, req GroupMapRequest) (GroupMapWritten, error) {
	return writeGroupMap(ctx, s, req, EventGroupUnmapped)
}

func writeGroupMap(ctx context.Context, s Store, req GroupMapRequest, kind EventKind) (GroupMapWritten, error) {
	at := req.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if req.Provider == "" {
		return GroupMapWritten{}, fmt.Errorf("%w: the provider namespace is required — it must equal the verifier's provider", ErrFederatedSubject)
	}
	if err := ValidGroupName(req.Group); err != nil {
		return GroupMapWritten{}, err
	}
	if !req.ObjectKind.Valid() {
		return GroupMapWritten{}, fmt.Errorf("%w: unknown object kind %q", ErrNoSuchObject, req.ObjectKind)
	}
	if kind == EventGroupMapped && req.Verbs.Empty() {
		return GroupMapWritten{}, ErrGroupMapVerbs
	}
	current, err := s.Model(ctx)
	if err != nil {
		return GroupMapWritten{}, fmt.Errorf("reading the control journal: %w", err)
	}
	if err := current.checkObject(req.ObjectKind, req.ObjectID); err != nil {
		return GroupMapWritten{}, fmt.Errorf("%w: %v", ErrNoSuchObject, err)
	}
	key := GroupMapKey{Provider: req.Provider, Group: req.Group, ObjectKind: req.ObjectKind, ObjectID: req.ObjectID}
	_, live := current.GroupMaps[key]
	verbs := req.Verbs
	switch {
	case kind == EventGroupMapped && live:
		return GroupMapWritten{}, fmt.Errorf("%w: %q (provider %q) → %s %s; unmap it first to change its verbs",
			ErrGroupAlreadyMapped, req.Group, req.Provider, req.ObjectKind, req.ObjectID)
	case kind == EventGroupUnmapped && !live:
		return GroupMapWritten{}, fmt.Errorf("%w: %q (provider %q) → %s %s",
			ErrGroupNotMapped, req.Group, req.Provider, req.ObjectKind, req.ObjectID)
	case kind == EventGroupUnmapped:
		verbs = current.GroupMaps[key].Verbs
	}
	e := Event{
		Kind: kind, At: at, Actor: req.Actor,
		Provider: req.Provider, Group: req.Group, ObjectKind: req.ObjectKind, ObjectID: req.ObjectID,
	}
	if kind == EventGroupMapped {
		e.Verbs = req.Verbs
	}
	after, err := s.Append(ctx, e)
	if err != nil {
		return GroupMapWritten{}, err
	}
	return GroupMapWritten{Key: key, Verbs: verbs, Epoch: after.Epoch}, nil
}

// ObjectKindOfID infers an object's kind from its id prefix — `scp_…` is a scope, `prj_…`
// a project — so an operator types one id and cannot pair it with the wrong kind.
func ObjectKindOfID(id ID) (ObjectKind, bool) {
	switch {
	case MustPrefix(id, PrefixScope):
		return ObjectScope, true
	case MustPrefix(id, PrefixProject):
		return ObjectProject, true
	default:
		return "", false
	}
}
