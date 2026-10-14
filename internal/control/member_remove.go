package control

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// ErrNotAMember refuses a removal aimed at a user who holds no membership in that project.
//
// 🔴 THIS ONE IS NOT AN "EARLY REFUSAL" EITHER — `apply` DOES NOT ENFORCE IT, for
// `ErrWouldOrphanProject`'s reason. `apply`'s `member-removed` arm calls `dropMembership`,
// which deletes from two maps and is a no-op for a pair that is not there; it does not even
// check the project or the user EXISTS. Adding either rule to `apply` would govern REPLAY,
// and a journal already holding a hand-written removal of a non-member would stop loading
// whole. So the rule lives at `RemoveMember` and nowhere else.
//
// ⚠ WHY REFUSE AT ALL, WHEN `SetMember` ACCEPTS A RESTATED ROLE. A removal of nobody is a
// record that reads, in an audit of who left when, as a departure that never happened — and
// the likeliest way to produce one is a transposed or mistyped id, which a success would
// hide. The cost is a RETRY: a re-run after a timeout whose first attempt landed is refused
// here, and the refusal says that is what it means.
var ErrNotAMember = errors.New("control: that user is not a member of that project")

// MemberRemoval is a request to take one user out of one project.
type MemberRemoval struct {
	// ProjectID and UserID name an existing project and an existing user, and the user must
	// currently be a member of the project. IDs, not names, for `NewMembership`'s reason.
	ProjectID ID
	UserID    ID
	// Actor is recorded on the event. Optional, for `NewMembership.Actor`'s reason.
	Actor ID
	// At is the event's timestamp. Zero means now.
	At time.Time
}

// ResidualGrant is one LIVE grant that still gives the removed user authority over the
// project's objects after the removal.
type ResidualGrant struct {
	Grant Grant
	// Via is "" when the grant names the user THEMSELVES as its subject, and otherwise the
	// id of the OTHER project whose membership carries it — a grant whose subject is a
	// project reaches every member of that project at resolve time.
	Via ID
}

// ScopeReach is one of the project's scopes the user still reaches, and at what verbs.
type ScopeReach struct {
	ID    ID
	Name  string
	Verbs VerbSet
}

// MemberRemoved is what `RemoveMember` wrote, and what it deliberately left in place.
type MemberRemoved struct {
	ProjectID ID
	UserID    ID
	// Role is the role the user held until this removal.
	Role Role
	// Epoch is the journal epoch after the append — what an operator quotes when asking
	// whether a running pod has picked the change up yet.
	Epoch uint64

	// StillReaches is every scope OF THIS PROJECT the user still reaches after the removal,
	// as `Resolve` — the one predicate — answers it over the post-append model. Empty means
	// the removal took away every route they had to this project's scopes.
	StillReaches []ScopeReach
	// Grants is every live grant that produces `StillReaches`, sorted by grant id: a grant
	// naming the user as subject, or a grant naming another project they are still a member
	// of, whose OBJECT is this project or one of its scopes. NOT revoked here — see
	// `RemoveMember`.
	Grants []ResidualGrant
	// Credentials is every LIVE credential that authenticates as this user, sorted by id. A
	// credential binds to the PRINCIPAL, never to a project, so a removal changes what each
	// one reaches and not whether it authenticates.
	Credentials []Credential
}

// RemoveMember takes one user out of one project by appending ONE `member-removed` record.
//
// 🔴 WHY IT EXISTS, STATED AS THE GAP IT CLOSES. `EventMemberRemoved` has been in the closed
// event set since P3 and `apply` folds it, but nothing wrote one: `SetMember` declined to,
// naming two questions it had not answered. This function is those two answers.
//
// 🔴 THE LAST-OWNER QUESTION: REFUSED, BY `SetMember`'S OWN RULE, NOT A COPY. Removing a
// user is demoting them to no role at all, which is at least as orphaning as demoting them
// to `member` — after it nobody may administer the project and nobody may delete it. So
// `refuseOrphaning` is called with an empty next role, and its clauses carry over unchanged:
// removing one of TWO owners is the ordinary end of a handover and is accepted; removing the
// ONLY owner is `ErrWouldOrphanProject`, whose remedy (promote somebody else first) is the
// same two-step. ⚠ `Project.OwnerUserID` may still name a removed owner afterwards; nothing
// authorises from that field (`ProjectsManagedBy`'s comment says why), so it is left as the
// historical "created for" it already is after a demotion.
//
// 🔴 THE GRANTS QUESTION: REPORTED, NEVER REVOKED, AND THE SCOPE OF THAT ANSWER IS EXACT.
// `Resolve` has two sources and this record removes exactly one of them — membership. A
// live grant naming the user (a share-flow grant, a team-link redemption's project grant)
// is the OTHER source, and it keeps working after the removal. It is not revoked here for
// three reasons:
//   - it is a DIFFERENT DECISION. A grant can be a deliberate share that is meant to
//     outlive membership — somebody leaving a team who keeps read on one scope — and the
//     model keeps the two sources apart precisely so that one can change without the other.
//   - THE JOURNAL HAS NO UNDO. A revoked grant cannot be un-revoked; a re-grant mints a new
//     id and loses who granted it and when. A removal that silently revoked would make the
//     destructive direction the default one.
//   - ONE COMMAND, ONE DESTRUCTIVE DIRECTION — `SetMember`'s own reason for not removing.
//
// What it does instead is refuse to be SILENT: `StillReaches` is the residual authority as
// `Resolve` computes it, and `Grants` names every grant that produces it, INCLUDING one
// reaching the user through a DIFFERENT project they are still in, which a list of "grants
// whose subject is this user" would miss. `TestTheReportedResidueIsExactlyWhatResolveStillGrants`
// pins the two against each other.
//
// ⚠ AND THE SAME FOR CREDENTIALS. Every live credential of the user still authenticates;
// what it reaches is now whatever their other memberships and grants give. They are
// reported, not revoked: the user may be a member of other projects.
//
// ⚠ WHAT IT INHERITS: the model is read OUTSIDE `Append`'s `flock`, so the last-owner check
// has `refuseOrphaning`'s declared check-then-act window — two concurrent removals of the
// two owners of one project can both pass. Recoverable the same way, with `-set-member
// -member-role owner`.
func RemoveMember(ctx context.Context, s Store, req MemberRemoval) (MemberRemoved, error) {
	at := req.At
	if at.IsZero() {
		// For `SetMember`'s reason: `Store` is an interface, a backend is not required to
		// stamp, and `Event.validate` refuses a zero time from every writer.
		at = time.Now().UTC()
	}

	current, err := s.Model(ctx)
	if err != nil {
		return MemberRemoved{}, fmt.Errorf("reading the control journal: %w", err)
	}
	if _, known := current.Projects[req.ProjectID]; !known {
		// ⚠ UNLIKE `SetMember`'S, `apply` WOULD NOT REFUSE THIS — its `member-removed` arm
		// checks nothing. So this is the only thing between a mistyped id and a record that
		// names a project that does not exist.
		return MemberRemoved{}, fmt.Errorf(
			"%w: %s — a removal names its project by id, and `apply` does not check a "+
				"member-removed record's project exists, so this would be a record about nothing",
			ErrNoSuchProject, req.ProjectID)
	}
	if _, known := current.Users[req.UserID]; !known {
		return MemberRemoved{}, fmt.Errorf(
			"%w: %s — a removal names its user by id, not by provider subject or email",
			ErrNoSuchMember, req.UserID)
	}
	prior, isMember := current.Memberships[req.ProjectID][req.UserID]
	if !isMember {
		return MemberRemoved{}, fmt.Errorf(
			"%w: %s is not in project %s, so there is nothing to remove. If you have just run "+
				"this and it timed out, this refusal means the first attempt LANDED",
			ErrNotAMember, req.UserID, req.ProjectID)
	}
	if err := refuseOrphaning(current, req.ProjectID, req.UserID, prior.Role, ""); err != nil {
		return MemberRemoved{}, err
	}

	// ONE event, through `Append`, so `Event.validate` and `apply` judge it under the lock
	// exactly as they judge every other writer's.
	after, err := s.Append(ctx, Event{
		Kind: EventMemberRemoved, At: at, Actor: req.Actor,
		ProjectID: req.ProjectID, UserID: req.UserID,
	})
	if err != nil {
		return MemberRemoved{}, err
	}

	// The residue is read from `after` — the model `Append` produced under the lock — so it
	// describes the journal this record landed in, not the one read before it.
	return MemberRemoved{
		ProjectID:    req.ProjectID,
		UserID:       req.UserID,
		Role:         prior.Role,
		Epoch:        after.Epoch,
		StillReaches: stillReaches(after, req.ProjectID, req.UserID),
		Grants:       residualGrants(after, req.ProjectID, req.UserID),
		Credentials:  liveCredentialsOf(after, req.UserID),
	}, nil
}

// stillReaches is `Resolve`'s answer for the user, restricted to the project's own scopes.
//
// 🔴 IT ASKS `Resolve` RATHER THAN RECOMPUTING, because "what may this principal see" has
// one answer in this package and a second spelling here would be the thing that disagrees.
func stillReaches(m Model, project, user ID) []ScopeReach {
	auth := Resolve(m, Principal{Kind: KindUser, ID: user})
	var out []ScopeReach
	for _, scopeID := range m.ScopesIn(project) {
		if vs := auth.VerbsOn(scopeID); !vs.Empty() {
			out = append(out, ScopeReach{ID: scopeID, Name: m.Scopes[scopeID].DisplayName, Verbs: vs})
		}
	}
	return out
}

// residualGrants names the live grants that give `user` authority over `project`'s objects.
//
// ⚠ IT RESTATES `Resolve`'S SUBJECT SET — the user, plus every project they are a member
// of — because `Resolve` returns verbs and not the grants that produced them. A restatement
// can drift, so `TestTheReportedResidueIsExactlyWhatResolveStillGrants` asserts the union of
// these grants' expansions EQUALS `stillReaches`, in both directions.
func residualGrants(m Model, project, user ID) []ResidualGrant {
	inProject := map[ID]bool{}
	for _, scopeID := range m.ScopesIn(project) {
		inProject[scopeID] = true
	}
	via := map[subjectRef]ID{{KindUser, user}: ""}
	for other := range m.userProject[user] {
		via[subjectRef{KindProject, other}] = other
	}
	var out []ResidualGrant
	for _, g := range sortedGrants(m) {
		if !g.Live() {
			continue
		}
		through, mine := via[subjectRef{g.SubjectKind, g.SubjectID}]
		if !mine {
			continue
		}
		onThisProject := (g.ObjectKind == ObjectProject && g.ObjectID == project) ||
			(g.ObjectKind == ObjectScope && inProject[g.ObjectID])
		if onThisProject {
			out = append(out, ResidualGrant{Grant: g, Via: through})
		}
	}
	return out
}

// liveCredentialsOf lists every live credential that authenticates as `user`, sorted by id.
func liveCredentialsOf(m Model, user ID) []Credential {
	var out []Credential
	for _, c := range m.Credentials {
		if c.Live() && c.PrincipalKind == KindUser && c.PrincipalID == user {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
