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

	// Revocable is the live grant rows naming this scope, which are the only thing
	// this surface can take back. A viewer who holds authority by MEMBERSHIP appears
	// in `Audience` and NOT here, and the page says so in as many words — the
	// alternative is a revoke button that cannot work.
	Revocable(scope control.ID) ([]GrantRow, error)

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
//	100 x 100          1,321,957         66.2x                   100x
//	300 x 300         12,194,371          9.2x                   11.1x
//	600 x 600         51,533,099          4.2x                    4.5x
//
// So the model slightly OVER-predicts and is a safe bound. `control.Resolve` sorts the
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
			Display:      p.Display,
			Kind:         p.Kind,
			Verbs:        verbs.String(),
			ByMembership: memberOf(m, p, owner),
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

// Revocable is the live grant rows naming this scope.
//
// ⚠ IT IS DELIBERATELY NARROWER THAN THE AUDIENCE AND THE PAGE SAYS SO. A grant
// naming the OWNING PROJECT as its object also confers this scope; it is not listed
// here, because revoking it from a page about one scope would silently withdraw every
// other scope that project owns. A wider revocation is a decision somebody makes on a
// page about the project, not a side effect of a button on this one.
func (s ControlSharing) Revocable(scope control.ID) ([]GrantRow, error) {
	m := s.Authority.Model()
	if _, known := m.Scopes[scope]; !known {
		return nil, ErrNoSuchScope
	}
	var out []GrantRow
	for _, g := range m.Grants {
		if !g.Live() || g.ObjectKind != control.ObjectScope || g.ObjectID != scope {
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
		out = append(out, GrantRow{
			ID:        g.ID,
			Subject:   Subject{Kind: g.SubjectKind, ID: g.SubjectID, Display: p.Display},
			Verbs:     g.Verbs.String(),
			GrantedAt: g.GrantedAt.UTC().Format(time.RFC3339),
		})
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
// omission. Both call the SAME predicate, so this is one rule at two call sites
// rather than two rules.
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
	if g.ObjectKind != control.ObjectScope || !auth.Allows(g.ObjectID, control.VerbAdmin) {
		// A grant over a PROJECT is refused here rather than authorised against the
		// project, because this surface has no project page and `Revocable` never
		// offers one. Refusing with the same error an unauthorised caller gets keeps
		// the two indistinguishable.
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
