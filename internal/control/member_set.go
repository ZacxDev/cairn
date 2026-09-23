package control

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNoSuchProject refuses a membership aimed at a project this journal does not hold.
//
// ⚠ AN EARLY REFUSAL, NOT A SECOND AUTHORITY — the same standing as
// `ErrNoSuchPrincipal`. `Model.apply` refuses the same batch under `Append`'s `flock`,
// and that is the authoritative rule for every writer including one that never comes
// through this function. What the sentinel buys is a caller that can BRANCH: it is what
// `cmd/cairn-server -set-member` reads to print "this journal holds no project with id …"
// instead of relaying a sentence about a batch that would not replay.
var ErrNoSuchProject = errors.New("control: no such project in this control journal")

// ErrNoSuchMember refuses a membership aimed at a user this journal does not hold.
//
// ⚠ SEPARATE FROM `ErrNoSuchPrincipal` DELIBERATELY, AND THE DISTINCTION IS THE CALLER'S
// REMEDY RATHER THAN THE MODEL'S RULE. `ErrNoSuchPrincipal` means "nothing can
// authenticate as this" and its fix is usually `-create-user`; this means "that person
// exists nowhere in this journal", whose fix is the same command but whose failure reads
// differently when the operator has just typed a project id into the user field — which,
// with two id-shaped flags on one command line, is the mistake to expect.
var ErrNoSuchMember = errors.New("control: no such user in this control journal")

// ErrWouldOrphanProject refuses a change that would leave a project with no owner.
//
// 🔴 THIS ONE IS NOT AN "EARLY REFUSAL" — `apply` DOES NOT ENFORCE IT, AND THAT
// ASYMMETRY IS A DECISION RATHER THAN AN OVERSIGHT. Every other refusal in this file
// restates a rule the model already holds; there is no last-owner rule anywhere in
// `apply`, measured, so this is the only place the question is asked.
//
// 🔴 WHY IT IS NOT IN `apply`, WHICH IS WHERE A CORRECTNESS RULE WOULD GO. `apply` is
// replay. A rule added there does not only govern new writes — it governs every journal
// that already exists, so a journal somewhere already containing an owner demotion would
// stop replaying entirely and the authority it describes would become unreadable. That is
// a breaking change to a durable, append-only format with no migration path and no
// undo, in exchange for refusing a state an operator has to go out of their way to
// create. The pre-check refuses the path people actually take; `apply` keeps replaying
// what is already written.
//
// 🔴 WHAT THE ORPHANED STATE ACTUALLY COSTS, AND IT DIFFERS BY TARGET ROLE — an earlier
// version of this paragraph gave one answer for both and was FALSE for one of them.
// `MayAdministerProject` is true for an owner OR AN ADMIN; `MayDeleteProject` only for an
// owner. So demoting the last owner to `member` leaves a project nobody may administer and
// nobody may delete, while demoting them to `admin` leaves one that is merely UNDELETABLE
// — they can still change its membership. Both are refused; the message says which.
//
// ⚠ AND IT IS RECOVERABLE THROUGH THIS VERY COMMAND, which the old wording denied by
// claiming it needed "direct write access to the journal file". `-set-member -member-role
// owner` restores an owner at rc 0 — MEASURED — because this operator path is not itself
// gated by `MayAdministerProject`. The claim that matters is narrower and still holds: no
// AUTHORISED IN-PRODUCT surface can repair it, because the predicates above are what such
// a surface would ask.
//
// ⚠ NEITHER PREDICATE HAS A NON-TEST CALLER TODAY — measured tree-wide. That does not make
// the rule premature: it makes the rule the thing that is true before the surface exists,
// and it is why the cost above is written as what those predicates WOULD answer rather
// than as an outage anybody can currently observe.
//
// ⚠ AND THERE IS NO OVERRIDE FLAG, WHICH IS A JUDGEMENT ABOUT THIS PARTICULAR REFUSAL
// RATHER THAN A HOUSE RULE. `claude/RULES.md`'s objection to an unclearable gate applies
// where there is no other route; here there is an obvious two-step one — promote somebody
// else to owner, then demote — so the refusal names it. A flag would be taken by reflex
// and the thing on the other side is an unadministrable project.
var ErrWouldOrphanProject = errors.New(
	"control: that would leave the project with no owner")

// NewMembership is a request to put a user in a project at a role, or to change the role
// they already hold.
type NewMembership struct {
	// ProjectID and UserID name an existing project and an existing user. Both required,
	// and both must already be in the journal.
	//
	// ⚠ A PROJECT CANNOT BE A MEMBER OF ANYTHING, INCLUDING ITSELF. `EventMemberSet`
	// carries a `user_id` and nothing else, so there is no shape here for a project
	// principal — which is the same ruling `internal/control/README.md` records from the
	// other side ("a project is not a member of itself"), and the reason a service account
	// reaches a scope only through an explicit grant.
	ProjectID ID
	UserID    ID
	// Role is `owner`, `admin` or `member`. Required, and validated by `Event.validate`
	// as well as here — an unknown role grants NOTHING, so replaying one would silently
	// strip authority a newer build intends.
	Role Role
	// Actor is who is doing this, recorded on the event. Optional: an operator running
	// the CLI against the journal file has no principal to name, and a fabricated one
	// would be worse than an absent one in a log whose whole purpose is attribution.
	Actor ID
	// At is the event's timestamp. Zero means now.
	At time.Time
}

// MembershipSet is what happened, in enough detail for a caller to say so.
//
// 🔴 IT REPORTS THE PREVIOUS ROLE BECAUSE THE INTERESTING OUTCOMES ARE NOT "OK". An
// operator running this command wants to know whether they JOINED somebody or PROMOTED
// them, and whether the command they just re-ran changed anything at all — three
// different sentences that a bare success cannot distinguish.
type MembershipSet struct {
	ProjectID ID
	UserID    ID
	// Role is the role now held — equal to the request's.
	Role Role
	// Previous is the role held before this call, and is the zero `Role` when
	// `WasMember` is false. Read the pair, never `Previous` alone: "" is also what a
	// malformed role would look like.
	Previous Role
	// WasMember reports whether the user was already in the project.
	WasMember bool
	// Epoch is the journal's event count AFTER this append, taken from the Model
	// `Append` returns rather than read back in a second call — the same value
	// `-create-user` and `-issue-credential` print, and for the same reason: it is what
	// an operator quotes when asking whether a running pod has picked the change up yet.
	Epoch uint64
}

// Changed reports whether this call moved anything.
//
// ⚠ A FALSE HERE IS A SUCCESS, NOT A NO-OP REFUSAL, and the event was still appended.
// `SetMember` deliberately does not refuse a request that restates the current role: the
// journal is append-only and a record that an operator ASSERTED this role at this time is
// itself information, and refusing would make a retry after a timeout fail where the first
// attempt may or may not have landed. Idempotent in EFFECT, not in the record.
func (m MembershipSet) Changed() bool {
	return !m.WasMember || m.Previous != m.Role
}

// SetMember puts a user in a project at a role, or changes the role they hold.
//
// 🔴 WHY IT EXISTS, STATED AS THE DEFECT IT CLOSES. `EventMemberSet` has been in the
// closed event set since P3 and `ProvisionUser` emits one — for the project it creates,
// at `RoleOwner`, for the user it just created. Nothing else wrote one. So a SECOND
// person could not be put in a project by any path in this repository: measured,
// `cairn-server -create-user` with an existing `-project` name creates a NEW project with
// its caller as owner rather than joining the named one, which means two users are never
// co-members and `ui.Candidates`' co-membership narrowing finds nobody to share with. The
// browser surface's share flow had a candidate list that only an operator with a text
// editor could populate.
//
// 🔴 AND HAND-APPENDING WAS THE DOCUMENTED REMEDY, WHICH IS THE SHAPE THIS REPOSITORY HAS
// ALREADY BEEN BURNED BY. `IssueCredential`'s own history: an operator told to hand-write
// a `credential-issued` record was not told that `token_hash` is a digest, and a
// 64-character raw secret was accepted and persisted into an append-only authority. A
// schema an operator has to guess at is a hazard whatever the field; this function is the
// same answer one event over.
//
// ⚠ WHAT IT DELIBERATELY DOES NOT DO. It does not REMOVE: `EventMemberRemoved` exists,
// has no writer, and is not written here — removal has its own last-owner question and its
// own answer about what happens to grants that named the person, and folding it in would
// make one command with two destructive directions. It does not GRANT: membership confers
// authority over the project's OWN scopes through `roleVerbs`, and a scope shared from
// elsewhere still needs its grant. And it does not create either party.
func SetMember(ctx context.Context, s Store, req NewMembership) (MembershipSet, error) {
	at := req.At
	if at.IsZero() {
		// `FileStore.Append` stamps a zero `At` too, so this is belt-and-braces there —
		// but `Store` is an interface, a backend is not required to stamp, and
		// `Event.validate` refuses a zero time from every writer.
		at = time.Now().UTC()
	}

	current, err := s.Model(ctx)
	if err != nil {
		return MembershipSet{}, fmt.Errorf("reading the control journal: %w", err)
	}

	if _, known := current.Projects[req.ProjectID]; !known {
		return MembershipSet{}, fmt.Errorf(
			"%w: %s — a membership names its project by id, and `apply` refuses a "+
				"member-set naming a project that does not exist, so this would be a batch "+
				"that cannot replay rather than a membership nobody can see",
			ErrNoSuchProject, req.ProjectID)
	}
	if _, known := current.Users[req.UserID]; !known {
		return MembershipSet{}, fmt.Errorf(
			"%w: %s — a membership names its user by id, not by provider subject or "+
				"email, and an id that names nobody would be refused by `apply` for the "+
				"same reason",
			ErrNoSuchMember, req.UserID)
	}
	if !req.Role.Valid() {
		// Restating `Event.validate`'s rule, for the same reason as the two above: a
		// caller that can branch gets to say which flag was wrong.
		return MembershipSet{}, fmt.Errorf(
			"unknown role %q — a role this build cannot map grants NOTHING, so replaying "+
				"it would silently strip authority a newer build intends", req.Role)
	}

	prior, wasMember := current.Memberships[req.ProjectID][req.UserID]
	if err := refuseOrphaning(current, req, prior); err != nil {
		return MembershipSet{}, err
	}

	// ONE BATCH, for the reason `ProvisionUser` and `IssueCredential` both state: it is a
	// single event today, so a half-applied membership is not a state this path can
	// produce — but the next thing anybody adds here is a set-plus-grant, and `Append`
	// validates a whole batch against a CLONE before a byte reaches disk.
	after, err := s.Append(ctx, Event{
		Kind: EventMemberSet, At: at, Actor: req.Actor,
		ProjectID: req.ProjectID, UserID: req.UserID, Role: req.Role,
	})
	if err != nil {
		return MembershipSet{}, err
	}

	out := MembershipSet{
		ProjectID: req.ProjectID,
		UserID:    req.UserID,
		Role:      req.Role,
		WasMember: wasMember,
		Epoch:     after.Epoch,
	}
	if wasMember {
		out.Previous = prior.Role
	}
	return out, nil
}

// refuseOrphaning is the last-owner rule, in one place so the condition is stated once.
//
// 🔴 TWO CLAUSES, NOT THREE — AND AN EARLIER COMMENT HERE CLAIMED THREE AND THAT "EACH
// CLAUSE MATTERS". THAT WAS FALSE, MEASURED BY MUTATION, AND THE RETRACTION IS KEPT
// RATHER THAN TIDIED AWAY. The dropped clause was `!wasMember`, and it was not merely
// untested: it is EQUIVALENT. `prior` comes from a map lookup, so a non-member's value is
// the zero `Membership` whose `Role` is `""` — and `"" != RoleOwner` already returns nil
// one clause later. Deleting it leaves both packages' suites fully green because there is
// no behaviour behind it. The old comment justified it with "promoting a member to admin
// in a project whose owner is somebody else", which is the case the SECOND clause handles.
//
// What is left, and what each one actually buys:
//
//   - `prior.Role != RoleOwner` — the user must currently BE an owner. ⚠ ITS OWN CONTROL
//     IS NARROW AND SAYS SO: dropping it is observable only in a project that ALREADY has
//     no owner, where changing a member's role would then be refused with a message
//     naming them as "the only owner" of a project they do not own.
//     `TestAnAlreadyOrphanedProjectStillAcceptsAMembershipChange` is that case.
//   - `req.Role == RoleOwner` — re-asserting owner on the sole owner changes nothing and
//     must not be refused. `TestReassertingOwnerOnTheSoleOwnerIsNotRefused`.
//   - the loop's `userID != req.UserID` — demoting one of TWO owners is the ordinary end
//     of a handover. `TestASECONDOwnerMakesTheDemotionLEGITIMATE`.
//
// 🔴 IT IS A CHECK-THEN-ACT AND THE WINDOW IS REAL, NOT THEORETICAL — DECLARED HERE
// BECAUSE THE ALTERNATIVE IS TO IMPLY A GUARANTEE THIS DOES NOT GIVE. `SetMember` reads
// the model OUTSIDE the `flock` that `Append` takes, and `apply` carries no last-owner
// rule to re-check under it, so two concurrent demotions of a two-owner project can each
// pass this against the same pre-state and both land. MEASURED: reproduced on the second
// attempt of a naive two-process race, leaving a project with zero owners. This is the
// same window `internal/control/README.md` already declares for
// `checkScopeNamesAreFree`, and it is declared rather than closed for the same reason: the
// `Writer` interface takes events, not a caller-supplied precondition, so closing it means
// either a rule in `apply` — which would make an already-written journal unreplayable, see
// `ErrWouldOrphanProject` — or a new seam in `Store`. Neither belongs in the change that
// introduces the command. **The operator-facing consequence is bounded**: the state is
// recoverable with one `-set-member -member-role owner`, which is the remedy the refusal
// names anyway.
func refuseOrphaning(m Model, req NewMembership, prior Membership) error {
	if prior.Role != RoleOwner || req.Role == RoleOwner {
		return nil
	}
	for userID, ms := range m.Memberships[req.ProjectID] {
		if userID != req.UserID && ms.Role == RoleOwner {
			return nil
		}
	}
	// 🔴 THE COST IS STATED PER TARGET ROLE, BECAUSE ONE SENTENCE FOR BOTH WAS FALSE FOR
	// ONE OF THEM. An earlier version said "a project whose membership nobody may change
	// and which nobody may delete" for every demotion. `MayAdministerProject` returns true
	// for an ADMIN, so the first half is wrong when the target role is `admin` — and the
	// `-member-role` flag's own help text says so one file over, which is exactly the
	// contradiction `AGENTS.md` warns leads a maintainer to delete the guard.
	cost := "whose membership nobody may change and which nobody may delete"
	if req.Role == RoleAdmin {
		cost = "that nobody may delete — an admin may still change its membership, " +
			"but `MayDeleteProject` is true only for an owner"
	}
	return fmt.Errorf(
		"%w: %s is the only owner of %s, so making them %q leaves a project %s. Promote "+
			"somebody else to owner first, then re-run this — that is a supported two-step "+
			"and it is also how this state is recovered if it is ever reached another way",
		ErrWouldOrphanProject, req.UserID, req.ProjectID, req.Role, cost)
}
