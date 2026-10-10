package ui

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// Sharing is the control-plane half the share flow needs, as an interface for the
// same reason [Source] is one: the renderer's tests build a world without a journal
// on disk.
//
// 🔴 `Audience` AND `Revocable` ANSWER TWO DIFFERENT QUESTIONS AND THE DIFFERENCE IS
// THE WHOLE POINT OF THIS INTERFACE. "Who has access to this" is computed from
// [control.Resolve]; "which rows can I take back" is read from the grant table.
// Authority arrives TWO ways — membership in the project that owns the scope, and a
// live grant — so a page that answered the first question from the grant table would
// under-report every project member, and would do it silently: the list would be
// short, plausible, and wrong in the direction that tells somebody their notes are
// more private than they are. `TestTheAudienceIsComputedFromResolveNotFromGrantRows`
// is what measures that, with a member who holds no grant row at all.
type Sharing interface {
	// Administrable is the scopes this authority may share.
	//
	// 🔴 IT IS ON THE INTERFACE RATHER THAN OPEN-CODED IN THE HANDLER BECAUSE IT
	// DECIDES WHICH VERB CONFERS SHARING, AND THAT IS A POLICY. A handler calling
	// `auth.NamedScopes(control.VerbAdmin)` inline would put that decision at the
	// call site, where the next surface to need it would make it a second time —
	// and the two would disagree the first time either was edited.
	Administrable(auth control.Authorization) []control.NamedScope

	// Audience is every principal with ANY authority over this scope, computed from
	// [control.Resolve]. Ordered, so a page rendered twice reads the same.
	//
	// 🔴 ANY VERB, NOT `read` — AND THAT IS THE SAFE DIRECTION RATHER THAN A LOOSE ONE.
	// The three verbs are independent bits (`control.VerbSet`), so `write` without
	// `read` is representable and the shipped form can produce it: the checkboxes are
	// independent and only `read` is pre-ticked. Filtering on `read` would DROP a
	// principal who can modify the scope from the list somebody reads to decide who has
	// access — an under-report, which is the exact failure this whole seam exists
	// against. Each row renders its verb set, so a `write`-only principal is visible AND
	// distinguishable. ⚠ An earlier draft of this doc said "can READ" while the body
	// admitted any verb: a description NARROWER than its body, which reads as a promise
	// nothing keeps.
	Audience(scope control.ID) ([]Viewer, error)

	// Revocable is the live grant rows that REACH this scope — the grants naming it and the
	// project-wide grants naming the project that owns it (operator decision O-b) — which are
	// the only things this surface can take back. A viewer who holds authority by MEMBERSHIP
	// appears in `Audience` and NOT here, and the page says so in as many words.
	//
	// ⚠ IT IS VIEWER-INDEPENDENT AND SO IT DOES NOT SAY WHO MAY REVOKE A ROW. A scope admin who
	// is not the owning project's owner/admin sees project-wide rows they cannot take back; the
	// handler passes the rows through [Sharing.ForViewer] before rendering, which is what decides
	// the button and whether the project's name is shown.
	Revocable(scope control.ID) ([]GrantRow, error)

	// ForViewer marks, per row, whether THIS viewer may revoke it — by `mayRevokeGrant`, the
	// predicate `POST /unshare` runs, so the page never offers a button the write refuses
	// (round 2 🟡A of #214) — and blanks a project-wide row's project NAME for a viewer who is
	// not a member of that project (an outsider scope admin learns a grant exists, not which
	// project it is over).
	ForViewer(rows []GrantRow, viewer control.Principal, auth control.Authorization) []GrantRow

	// ProjectGrants is every live grant whose OBJECT is this project — "project-wide"
	// grants, which reach every scope the project owns. It performs NO authority check:
	// the Team page reaches it only for a project `Inviting.Invitable` returned, exactly as
	// `Inviting.Outstanding` is reached.
	//
	// 🔴 IT EXISTS SO A PROJECT-WIDE `reader` GRANT (what a team link's `reader` on a project
	// writes) CAN BE SEEN AND TAKEN BACK ON A PAGE (operator decision O-b), rather than only
	// with the control CLI.
	ProjectGrants(project control.ID) ([]GrantRow, error)

	// Candidates is who this actor may share with. See [ControlSharing.Candidates]
	// for the rule and why it is not "every user in the model".
	Candidates(actor control.Principal) ([]Subject, error)

	// Writable answers whether this deployment can record a share AT ALL, asked at
	// render time so the page can say so beside the controls rather than after
	// somebody has chosen a recipient.
	Writable() bool

	// ScopeOfGrant is the scope a grant names, for the redirect after a revoke.
	//
	// ⚠ IT IS NOT AN AUTHORITY CHECK AND MUST NEVER BE USED AS ONE. It answers for
	// any grant id, including one the caller may not touch, and the only thing it
	// decides is which page to land on afterwards. `Unshare` does the check, from
	// the grant row, itself.
	ScopeOfGrant(grant control.ID) (control.ID, bool)

	// Share records a grant and reports whether it is in force HERE yet.
	Share(ctx context.Context, actor control.Principal, auth control.Authorization,
		scope control.ID, subject Subject, verbs control.VerbSet) (Effect, error)

	// Unshare revokes one grant by id.
	Unshare(ctx context.Context, actor control.Principal, auth control.Authorization,
		grant control.ID) (Effect, error)
}

// Viewer is one principal that can see a scope, and HOW.
type Viewer struct {
	// Display is USER TEXT — an email or a project name out of the control plane.
	Display string
	// Kind is `user` or `project`.
	Kind control.Kind
	// Verbs is the rendered verb set, e.g. `read,write`.
	Verbs string
	// ByMembership is true when this principal reaches the scope through the project
	// that owns it rather than through a grant. It is what the page branches on to
	// explain why there is no revoke button beside the row.
	//
	// ⚠ IT IS NOT EXCLUSIVE. A principal can hold BOTH — a member who was also
	// granted the scope explicitly — and this field is true for them, because the
	// sentence it drives ("revoking every grant would not remove this one") is true
	// for them too.
	ByMembership bool
	// ByProjectGrant is true when this principal reaches the scope through a live grant over
	// the WHOLE project that owns it (directly, or as a member of a project that was granted
	// it). Like `ByMembership` it is not exclusive; it drives the "via a project-wide grant"
	// label, so the audience says HOW somebody reaches a scope whenever it is not a scope grant.
	ByProjectGrant bool
}

// Subject is a principal a scope may be shared WITH.
type Subject struct {
	Kind control.Kind
	ID   control.ID
	// Display is USER TEXT.
	Display string
}

// GrantRow is one revocable grant.
type GrantRow struct {
	ID control.ID
	// Subject is who it was granted to. USER TEXT in `Display`.
	Subject Subject
	Verbs   string
	// GrantedAt is rendered as an RFC3339 UTC instant, never as "3 days ago": a
	// relative time computed on the server is a claim about the READER's clock.
	GrantedAt string
	// ProjectWide is true for a grant over the whole PROJECT rather than one scope, and
	// Project is that project's display name (USER TEXT). Revoking one withdraws every scope
	// the project owns, and the row says so before anybody clicks.
	ProjectWide bool
	Project     string
	// ProjectID is the project a project-wide grant is over — ancillary, for where its revoke
	// lands afterwards; never an authority input.
	ProjectID control.ID
	// MayRevoke is whether the viewer this row was prepared for may take it back
	// ([Sharing.ForViewer]). False until that call: a row nobody asked about renders no button.
	MayRevoke bool
}

// Effect is how a write landed, carried out of the control plane unchanged.
//
// 🔴 IT IS RENDERED RATHER THAN SWALLOWED, AND `control.EffectDeferred`'s OWN COMMENT
// IS WHY: *"What a UI must render as 'revoked, effective by <time>' — the qualifier is
// not optional, and omitting it is the sharing dialog implying a guarantee the system
// cannot make."* That sentence was written in `internal/control` before any UI
// existed. This is the UI it was written for.
type Effect struct {
	// Immediate is true when this replica is already serving the written epoch.
	Immediate bool
	// EffectiveBy is when this replica is guaranteed to serve it, or "" when the
	// authority declares no bound — which the page renders as `unbounded`, never as
	// a date it does not have.
	EffectiveBy string
}

// ErrNotPermitted is what a write gets when the actor may not administer the scope.
//
// It is distinct from the HTTP refusal the handler produces because this interface
// has callers other than the handler — the tests — and a write path that relied on
// its one caller having checked first is a write path that is unauthorised by
// omission the day a second caller appears.
var ErrNotPermitted = errors.New("ui: the actor may not administer this scope")

// ErrNoSuchScope is what a read gets for a scope the authority does not hold. The
// handler renders it as the SAME uniform refusal an unauthorised scope gets, so the
// two are indistinguishable from outside — a distinguishable answer here would make
// the share page an existence oracle over every scope in the deployment.
var ErrNoSuchScope = errors.New("ui: no such scope")

// ControlSharing is [Sharing] over the real control plane.
//
// 🔴 IT HOLDS THE SAME `*control.Cache` THE AUTHENTICATION CHAIN DOES, AND MUST. The
// audience it computes and the authority a request was resolved against have to come
// out of one model, or the page can say "B can see this" about a model in which the
// viewer's own authority never existed.
type ControlSharing struct {
	Authority *control.Cache
	// Now is the clock the grant's timestamp is stamped from. Nil means
	// `time.Now().UTC()`.
	Now func() time.Time
}

func (s ControlSharing) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

// Administrable reads the caller's OWN authority and consults nothing else.
//
// 🔴 IT DOES NOT TOUCH THE MODEL, AND THAT IS THE POINT RATHER THAN AN OPTIMISATION.
// The `Authorization` was computed from the same model read that authenticated the
// request; re-deriving this list from `s.Authority.Model()` would answer from a model
// that may have refreshed since, so a page could offer a scope the caller's authority
// did not include. Narrowing must never come from a second read.
func (s ControlSharing) Administrable(auth control.Authorization) []control.NamedScope {
	return auth.NamedScopes(control.VerbAdmin)
}

// Audience answers WHO HAS ACCESS TO THIS, and it answers it by resolving every
// principal the model holds.
//
// 🔴 IT RESOLVES RATHER THAN READING GRANTS, AND THE SOURCE IS NOT THE THING TO
// REVISIT. The grant-table read would be O(grants) and is WRONG: a user who is a member
// of the project owning this scope reaches it with no grant row in existence.
// `control.Resolve` is the one function permitted to decide what a principal can see
// (`internal/control/README.md`), and "who has access to this" is that question asked of every
// principal instead of one.
//
// 🔴 THE COST IS `P x G x log G` AND THE MEASUREMENT AGREES WITH THAT MODEL — WHICH IS
// THE OPPOSITE OF WHAT THE PREVIOUS ROUND WROTE HERE, AND THE RETRACTION IS THE POINT.
// That round replaced the expression with four timings and concluded the cost was "worse
// than the expression implies … quadratic and not the log-linear the old expression reads
// as". Both halves were wrong. `P x G x log G` is not log-linear in the world's SIZE: when
// principals and grants grow together it is quadratic BY CONSTRUCTION, so the measurement
// confirmed the model rather than contradicting it, and the conclusion was drawn by
// reading a two-parameter expression as if one parameter were fixed.
//
// Re-derivable, via `BenchmarkAudience` in this package (`-bench BenchmarkAudience`), on
// one idle host — ratios are the claim, absolute times are not:
//
//	users x grants     ns/op        ratio to previous     P x G x log G predicts
//	10 x 10               19,963           —                       —
//	100 x 100          1,321,957         66.2x                   200x
//	300 x 300         12,194,371          9.2x                    11.1x
//	600 x 600         51,533,099          4.2x                     4.5x
//
// ⚠ THE `100x` THAT STOOD IN ROW 2 WAS THE COLUMN COMPUTED A SECOND WAY — bare `P x G`,
// with the log factor dropped — so a reader re-deriving it got 200 and concluded the table
// was broken. It is 200x, and that row is where the model over-predicts MOST (200x against
// 66.2x measured), not least.
//
// So the model OVER-predicts throughout and is a safe bound — by ~3x at the SMALLEST
// SIZES and by a few percent at the largest, which is the direction a bound should err in.
// (By STEP it reads the other way: the 3x lands on the ten-fold 10->100 step and the few
// percent on the 300->600 doubling. This table has already produced one reader who
// re-derived it and concluded it was broken, so the axis is spelled out.) `control.Resolve` sorts the
// whole grant table on every call (`sortedGrants`) and calls `ScopesIn` once per
// membership — that is where the `G log G` comes from, and it is why one page render at
// 600 principals allocates ~60 MB.
//
// ⚠ ACCEPTED FOR NOW, WITH THE NUMBERS RATHER THAN A SHRUG, because the deployments that
// exist hold single-digit principals — and there is no cache and no rate limiter on this
// surface, so the bound is worth knowing before either changes. The fix when it is needed
// is a per-epoch cache keyed on `Model.Epoch`, not a grant-table read.
func (s ControlSharing) Audience(scope control.ID) ([]Viewer, error) {
	m := s.Authority.Model()
	if _, known := m.Scopes[scope]; !known {
		return nil, ErrNoSuchScope
	}
	owner := m.Scopes[scope].ProjectID

	var out []Viewer
	for _, p := range principals(m) {
		auth := control.Resolve(m, p)
		verbs := auth.VerbsOn(scope)
		if verbs.Empty() {
			continue
		}
		out = append(out, Viewer{
			Display:        p.Display,
			Kind:           p.Kind,
			Verbs:          verbs.String(),
			ByMembership:   memberOf(m, p, owner),
			ByProjectGrant: projectGranted(m, p, owner),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Display < out[j].Display
	})
	return out, nil
}

// memberOf answers whether this principal reaches the owning project's scopes by
// MEMBERSHIP.
//
// ⚠ A PROJECT PRINCIPAL IS NEVER A MEMBER OF ITSELF, and that is the operator
// decision recorded in `internal/control/README.md` rather than an oversight here: a
// service account for a project reaches that project's scopes only if somebody
// granted it, so that a credential's reach always appears in the grant log. Answering
// `true` here for `p.ID == project` would put a "by membership" label on a row whose
// authority is in fact a grant, and the label is what decides whether the page offers
// a revoke button.
func memberOf(m control.Model, p control.Principal, project control.ID) bool {
	if p.Kind != control.KindUser || project == "" {
		return false
	}
	_, member := m.Memberships[project][p.ID]
	return member
}

// projectGranted answers whether a live grant over the WHOLE project reaches this principal —
// naming it directly, or naming a project it is a member of (the expansion `Resolve` does).
func projectGranted(m control.Model, p control.Principal, project control.ID) bool {
	if project == "" {
		return false
	}
	for _, g := range m.Grants {
		if !g.Live() || g.ObjectKind != control.ObjectProject || g.ObjectID != project {
			continue
		}
		if g.SubjectKind == p.Kind && g.SubjectID == p.ID {
			return true
		}
		if g.SubjectKind == control.KindProject && memberOf(m, p, g.SubjectID) {
			return true
		}
	}
	return false
}

// principals is every entity in the model that a request could be, sorted.
//
// It is built through `PrincipalFor` rather than from the entity rows directly, so
// the `Display` this page renders is byte-identical to the one an audit line carries
// — `internal/control`'s own rule about there being ONE constructor for a principal.
func principals(m control.Model) []control.Principal {
	var out []control.Principal
	for id := range m.Users {
		if p, known := m.PrincipalFor(control.KindUser, id); known {
			out = append(out, p)
		}
	}
	for id := range m.Projects {
		if p, known := m.PrincipalFor(control.KindProject, id); known {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Revocable is the live grant rows that reach this scope: the ones naming it, AND the
// project-wide ones naming the project that owns it.
//
// 🔴 THE PROJECT-WIDE ROWS ARE LISTED NOW, AND THE RULING THAT KEPT THEM OFF IS REVERSED BY
// AN OPERATOR DECISION (O-b), NOT FORGOTTEN. This said: "revoking it from a page about one
// scope would silently withdraw every other scope that project owns", so it listed scope
// grants only — and the page's own note ("Somebody who reaches this scope through membership
// … keeps it after every grant below is revoked") was then FALSE for every project-wide
// grantee, who kept access after every listed row was revoked while not being a member. Round
// 1 measured that. The "silently" half is answered instead by the row: a project-wide grant is
// labelled as one, names its project, and says that revoking it withdraws every scope there.
func (s ControlSharing) Revocable(scope control.ID) ([]GrantRow, error) {
	m := s.Authority.Model()
	sc, known := m.Scopes[scope]
	if !known {
		return nil, ErrNoSuchScope
	}
	var out []GrantRow
	for _, g := range m.Grants {
		if !g.Live() {
			continue
		}
		onScope := g.ObjectKind == control.ObjectScope && g.ObjectID == scope
		onOwner := g.ObjectKind == control.ObjectProject && g.ObjectID == sc.ProjectID && sc.ProjectID != ""
		if !onScope && !onOwner {
			continue
		}
		p, known := m.PrincipalFor(g.SubjectKind, g.SubjectID)
		if !known {
			// A grant whose subject the model no longer holds confers nothing
			// (`Resolve` never matches it), so it is not in the audience either. It
			// is skipped rather than rendered with a blank name: a row a reader
			// cannot identify is a row they cannot decide about.
			continue
		}
		out = append(out, grantRow(m, g, p.Display))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// grantRow projects one grant onto what a page renders, labelling a project-wide one.
func grantRow(m control.Model, g control.Grant, display string) GrantRow {
	row := GrantRow{
		ID:        g.ID,
		Subject:   Subject{Kind: g.SubjectKind, ID: g.SubjectID, Display: display},
		Verbs:     g.Verbs.String(),
		GrantedAt: g.GrantedAt.UTC().Format(time.RFC3339),
	}
	if g.ObjectKind == control.ObjectProject {
		row.ProjectWide = true
		row.Project = m.Projects[g.ObjectID].Name
		row.ProjectID = g.ObjectID
	}
	return row
}

// ForViewer decides each row's button and project name for one viewer — see the interface.
//
// 🔴 `viewer` IS `membershipActor(id)` AND `auth` IS `id.Auth`, while `POST /unshare` hands
// `Unshare` the ATTRIBUTION principal `id.Principal`. The two agree on every row because
// `mayRevokeGrant` refuses a project-wide grant outright when `auth.Narrowed()` — the one case in
// which `membershipActor(id)` and `id.Principal` differ — and its scope arm reads only `auth`.
func (s ControlSharing) ForViewer(rows []GrantRow, viewer control.Principal, auth control.Authorization) []GrantRow {
	m := s.Authority.Model()
	out := make([]GrantRow, len(rows))
	for i, row := range rows {
		g, known := m.Grants[row.ID]
		row.MayRevoke = known && mayRevokeGrant(m, viewer, auth, g)
		if row.ProjectWide {
			if _, member := m.RoleIn(row.ProjectID, viewer.ID); !member || viewer.Kind != control.KindUser {
				row.Project = ""
			}
		}
		out[i] = row
	}
	return out
}

// ProjectGrants is every live project-wide grant over `project`. No authority check — see
// the interface.
func (s ControlSharing) ProjectGrants(project control.ID) ([]GrantRow, error) {
	m := s.Authority.Model()
	var out []GrantRow
	for _, g := range m.Grants {
		if !g.Live() || g.ObjectKind != control.ObjectProject || g.ObjectID != project {
			continue
		}
		p, known := m.PrincipalFor(g.SubjectKind, g.SubjectID)
		if !known {
			continue
		}
		out = append(out, grantRow(m, g, p.Display))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Writable is the authority's capability, not this caller's permission.
func (s ControlSharing) Writable() bool { return s.Authority.Writable() }

// ScopeOfGrant answers for any grant the model holds, checking nothing.
func (s ControlSharing) ScopeOfGrant(grant control.ID) (control.ID, bool) {
	g, known := s.Authority.Model().Grants[grant]
	if !known || g.ObjectKind != control.ObjectScope {
		return "", false
	}
	return g.ObjectID, true
}

// Candidates is who this actor may share with.
//
// 🔴 IT IS THE ACTOR'S OWN COLLABORATORS, NOT EVERY USER IN THE MODEL, AND THAT IS A
// SECURITY DECISION RATHER THAN A UI ONE. A picker listing every user would make one
// scope's admin rights into a directory of everybody in the deployment — the same
// enumeration the uniform 401 and the opaque ids elsewhere in this surface exist to
// deny. The rule here is: the projects this actor belongs to, and the other people in
// them. Those are principals the actor can already name, so listing them tells them
// nothing they did not have.
//
// ⚠ THE COST IS REAL AND IT IS NOT HIDDEN: sharing with somebody you have no project
// in common with is NOT REACHABLE FROM THIS PAGE. That is a narrowing, it will want an
// invite flow to lift, and an invite flow is P6. A picker that quietly listed
// strangers would have been the easier build and the wrong one.
func (s ControlSharing) Candidates(actor control.Principal) ([]Subject, error) {
	m := s.Authority.Model()
	if actor.Kind != control.KindUser {
		// A project principal has no memberships, so it has no collaborators to
		// enumerate. An empty list rather than an error: the page renders "there is
		// nobody this credential can share with", which is the true answer.
		return nil, nil
	}

	seen := map[control.ID]struct{}{actor.ID: {}}
	var out []Subject
	for projectID, members := range m.Memberships {
		if _, mine := members[actor.ID]; !mine {
			continue
		}
		if p, known := m.PrincipalFor(control.KindProject, projectID); known {
			out = append(out, Subject{Kind: control.KindProject, ID: projectID, Display: p.Display})
		}
		for userID := range members {
			if _, already := seen[userID]; already {
				continue
			}
			seen[userID] = struct{}{}
			if p, known := m.PrincipalFor(control.KindUser, userID); known {
				out = append(out, Subject{Kind: control.KindUser, ID: userID, Display: p.Display})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Display < out[j].Display
	})
	return out, nil
}

// Share records a grant.
//
// 🔴 IT CONSULTS `Allows` ITSELF RATHER THAN TRUSTING ITS CALLER. The handler checks
// too, because it has to choose an HTTP status; this check is here because the
// interface is exported and a second caller that forgot would be authorised by
// omission.
//
// ⚠ AND THIS COMMENT USED TO END "Both call the SAME predicate, so this is one rule at two
// call sites rather than two rules" — RETRACTED, AND THE FIFTH SITE OF THAT RETRACTION.
// The two checks are NOT interchangeable: this one runs AFTER the form is validated, the
// handler's runs BEFORE it, and a request with no verb field answers 403 from the handler
// and 400 with the handler's check removed. An unauthorised caller learns which part of
// their request was malformed. `internal/ui/README.md` carries the table; the mutation row
// `ui-share-write-authority-check-removed-in-the-handler` is killable because of it.
//
// 🔴 AND IT CHECKS THE ACTOR'S AUTHORITY OVER THE SCOPE, NEVER OVER THE SUBJECT.
// There is no "may I share with this person" right in the model, and inventing one
// here would be a second authority this package decided by itself. What bounds the
// subject is `Candidates`, which the handler validates against.
func (s ControlSharing) Share(ctx context.Context, actor control.Principal, auth control.Authorization,
	scope control.ID, subject Subject, verbs control.VerbSet) (Effect, error) {
	if !auth.Allows(scope, control.VerbAdmin) {
		return Effect{}, ErrNotPermitted
	}
	if verbs.Empty() {
		// `Event.validate` refuses this too. Refusing here as well means the caller
		// gets a usable error rather than a journal-boundary message about a row it
		// never intended to write.
		return Effect{}, errors.New("ui: a share conferring no verb is not a share")
	}
	id, err := control.NewID(control.PrefixGrant)
	if err != nil {
		return Effect{}, err
	}
	res, err := s.Authority.ApplyNow(ctx, control.Event{
		Kind:        control.EventGranted,
		At:          s.now(),
		Actor:       actor.ID,
		GrantID:     id,
		SubjectKind: subject.Kind,
		SubjectID:   subject.ID,
		ObjectKind:  control.ObjectScope,
		ObjectID:    scope,
		Verbs:       verbs,
	})
	if err != nil {
		return Effect{}, err
	}
	return effectOf(res), nil
}

// Unshare revokes one grant.
//
// 🔴 THE AUTHORITY CHECKED IS OVER THE GRANT'S OBJECT, RESOLVED FROM THE GRANT ROW
// RATHER THAN TAKEN FROM THE REQUEST. A handler that passed the scope id alongside
// the grant id would let a caller with admin on scope A revoke a grant on scope B by
// naming A in the form — the grant id is the only thing that says which scope this
// write touches, so it is the only thing this check may read it from.
//
// 🔴 `ApplyNow`, NOT `Apply`, AND FOR REVOCATION THAT IS THE WHOLE DIFFERENCE. When
// it returns immediate, no subsequent read through THIS cache honours the revoked
// grant. It says nothing about another replica, and nothing at all about the copy
// already synced onto somebody's laptop — which is what [ReplicaHonesty] tells the
// operator, in the one place they are deciding to click it.
func (s ControlSharing) Unshare(ctx context.Context, actor control.Principal, auth control.Authorization,
	grant control.ID) (Effect, error) {
	m := s.Authority.Model()
	g, known := m.Grants[grant]
	if !known {
		return Effect{}, ErrNotPermitted
	}
	if !mayRevokeGrant(m, actor, auth, g) {
		// Refusing with the same error an unknown grant gets keeps the two
		// indistinguishable.
		return Effect{}, ErrNotPermitted
	}
	res, err := s.Authority.ApplyNow(ctx, control.Event{
		Kind:    control.EventGrantRevoked,
		At:      s.now(),
		Actor:   actor.ID,
		GrantID: grant,
	})
	if err != nil {
		return Effect{}, err
	}
	return effectOf(res), nil
}

// mayRevokeGrant is who may take a grant back, by the grant's OBJECT, read from the row.
//
//   - a SCOPE grant: `admin` on that scope in the request's own authorization — unchanged.
//   - a PROJECT-WIDE grant (operator decision O-b): MEMBERSHIP authority over the project —
//     `CanManageMembers` on the actor's own membership, the rule that lets them CREATE one
//     (`mayLink`'s project arm). It is not derived from scope admin: an outsider granted
//     `admin` on one scope must not withdraw a grant over the whole project.
//
// 🔴 A NARROWED CREDENTIAL TAKES BACK NO PROJECT-WIDE GRANT. Membership authority is not in an
// `Authorization`, so a narrowing cannot bound it — `membershipActor`'s argument, applied here
// from `auth.Narrowed()` because this method is handed the attribution principal
// (`handleUnshare` passes `id.Principal` for the journal's actor), not `membershipActor(id)`.
func mayRevokeGrant(m control.Model, actor control.Principal, auth control.Authorization, g control.Grant) bool {
	switch g.ObjectKind {
	case control.ObjectScope:
		return auth.Allows(g.ObjectID, control.VerbAdmin)
	case control.ObjectProject:
		if auth.Narrowed() || actor.Kind != control.KindUser {
			return false
		}
		role, member := m.RoleIn(g.ObjectID, actor.ID)
		return member && role.CanManageMembers()
	default:
		return false
	}
}

// effectOf projects a `control.WriteResult` onto what the page renders.
//
// ⚠ IT READS `Immediate()` RATHER THAN RE-DERIVING FROM THE EPOCHS. The derivation is
// `control.WriteResult`'s own documented invariant and re-spelling it here would be a
// second answer to "is this in force", which is the field a user is reading to decide
// whether they are done.
func effectOf(res control.WriteResult) Effect {
	out := Effect{Immediate: res.Immediate()}
	if !res.EffectiveBy.IsZero() {
		out.EffectiveBy = res.EffectiveBy.UTC().Format(time.RFC3339)
	}
	return out
}

var _ Sharing = ControlSharing{}
