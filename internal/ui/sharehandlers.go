package ui

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// scopeRefusal is what a caller gets for a scope they may not administer AND for a
// scope that does not exist.
//
// 🔴 ONE ANSWER FOR BOTH, WHICH IS THE SAME RULE THE UNIFORM 401 FOLLOWS ONE LEVEL UP.
// A 404 for an unknown scope beside a 403 for somebody else's would turn this page
// into an existence oracle over every scope in the deployment: a caller with admin on
// one scope could enumerate the rest by watching which status came back. Scope ids are
// unguessable by construction (`control.NewID` uses `crypto/rand` for exactly this
// reason) and this refusal is what keeps that worth something.
const scopeRefusal = "no such scope, or it is not yours to share"

// shareWriteRefusal is the uniform refusal for both write routes. Same discipline: it
// names no reason, so a caller cannot tell an unknown grant from one they may not
// touch from a subject they may not share with.
const shareWriteRefusal = "that share cannot be recorded"

// The closed set of outcomes a redirect may carry.
//
// 🔴 THE OUTCOME IS A CODE FROM THIS SET, NEVER A SENTENCE FROM THE QUERY STRING. A
// write redirects to a GET so a refresh does not repeat it, and the message has to
// survive that hop — but a redirect target is something anybody can put in a link. A
// reflected sentence would let an attacker choose the text of a banner the page
// presents as its OWN ("Your access was suspended, call this number"), which the HTML
// escaper does not touch because the text is not markup. A code maps to a sentence
// written here; an unknown code renders nothing at all.
const (
	QueryOutcome     = "outcome"
	QueryEffectiveBy = "by"

	outcomeShared  = "shared"
	outcomeRevoked = "revoked"

	// effectNow is the spelling `by` carries when the write is already in force here.
	effectNow = "now"
)

// handleSharePage renders the share flow: the index with no `?scope=`, one scope's
// page with it.
func (s *Server) handleSharePage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	view := ShareView{
		Viewer: id.Principal.Display,
		// The same derivation `handlePage` uses, and for the same reason: the token
		// comes from the COOKIE on this request, so a caller authenticated by a bearer
		// header renders no forms. See [Server.handlePage].
		CSRF:     csrfTokenFor(r),
		Outcome:  outcomeFrom(r),
		ReadOnly: !s.sharing.Writable(),
	}

	// 🔴 `Administrable` IS POPULATED ON BOTH BRANCHES, NOT JUST THE INDEX, AND THAT IS
	// A CORRECTION RATHER THAN THOROUGHNESS. [SharePage] renders the index whenever the
	// scope's NAME is empty, and `namedScope` returns an empty one for a scope the
	// authority cannot name. Filling this field only on the index branch therefore left
	// one path — unreachable today, behind the `Allows` check below — on which the page
	// would render "No scope is administrable by this credential. That is an authority
	// answer, not an empty store." to a caller who had just proved they administer one.
	// That is precisely the shape `TestEveryContentRouteConsultsTheAuthority` exists to
	// refuse, one level along: a sentence about AUTHORITY rendered by a path that did
	// not ask. Asking on both branches makes the fallback true instead of merely rare.
	view.Administrable = s.sharing.Administrable(id.Auth)

	scope := control.ID(r.URL.Query().Get(QueryScope))
	if scope == "" {
		s.renderShare(w, view)
		return
	}

	// 🔴 THE AUTHORITY CHECK IS BEFORE EVERY READ BELOW, AND IT IS THE REQUEST'S OWN
	// `Authorization` RATHER THAN A FRESH RESOLVE. That is the property
	// `control.Principal`'s comment requires: authenticated against one model,
	// authorised against the same one.
	if !id.Auth.Allows(scope, control.VerbAdmin) {
		writePlain(w, http.StatusNotFound, scopeRefusal)
		return
	}

	audience, err := s.sharing.Audience(scope)
	if err != nil {
		// `ErrNoSuchScope` cannot normally reach here — the authority check above
		// already required the scope to be in this caller's authorization, and
		// `Resolve` only admits scopes the model holds. It is answered with the SAME
		// refusal anyway, because the window is real: the cache can refresh between
		// the chain and this call, and a caller must not learn from a 500 that a scope
		// they could see a moment ago has just been deleted.
		if errors.Is(err, ErrNoSuchScope) {
			writePlain(w, http.StatusNotFound, scopeRefusal)
			return
		}
		writePlain(w, http.StatusInternalServerError, "the authority could not be read")
		return
	}
	revocable, err := s.sharing.Revocable(scope)
	if err != nil {
		if errors.Is(err, ErrNoSuchScope) {
			writePlain(w, http.StatusNotFound, scopeRefusal)
			return
		}
		writePlain(w, http.StatusInternalServerError, "the authority could not be read")
		return
	}
	candidates, err := s.sharing.Candidates(id.Principal)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the authority could not be read")
		return
	}

	view.Scope = s.namedScope(id.Auth, scope)
	view.Audience = audience
	view.Revocable = revocable
	view.Candidates = candidates
	s.renderShare(w, view)
}

// namedScope is the scope's (id, name) pair taken from the CALLER'S AUTHORITY.
//
// 🔴 THE NAME COMES FROM THE AUTHORIZATION, NOT FROM THE MODEL. Both hold it and they
// are built from one read, so they agree today — and the authorization is the one
// that cannot name a scope this caller may not see. Reading the model here would put a
// display name on the page through a path that has not been narrowed, which is the
// shape `Authorization.NamedScopes`'s own comment refuses.
//
// ⚠ IT RETURNS A ZERO NAME IF THE AUTHORITY DOES NOT HOLD ONE, AND [SharePage] READS AN
// EMPTY NAME AS "render the index". That is a safe degradation rather than a wrong
// page: the caller sees the list they may administer instead of a scope page with a
// blank heading. It is unreachable behind the `Allows` check above, which requires the
// scope to be in `byScope`, and `Resolve` drops any scope it cannot name.
func (s *Server) namedScope(auth control.Authorization, scope control.ID) control.NamedScope {
	for _, named := range auth.NamedScopes(control.VerbAdmin) {
		if named.ID == scope {
			return named
		}
	}
	return control.NamedScope{}
}

func (s *Server) renderShare(w http.ResponseWriter, view ShareView) {
	var b strings.Builder
	if err := SharePage(view).Render(&b); err != nil {
		writePlain(w, http.StatusInternalServerError, "the page could not be rendered")
		return
	}
	writeHTML(w, http.StatusOK, b.String())
}

// handleShare records a grant.
//
// It runs behind gates (2) and (6) — same origin and a per-session CSRF token — which
// it inherits from the METHOD rather than declaring, so neither can be forgotten here.
// See [Server.ServeHTTP].
func (s *Server) handleShare(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	if err := r.ParseForm(); err != nil {
		writePlain(w, http.StatusBadRequest, shareWriteRefusal)
		return
	}
	scope := control.ID(r.PostFormValue(FieldScope))
	if !id.Auth.Allows(scope, control.VerbAdmin) {
		writePlain(w, http.StatusForbidden, shareWriteRefusal)
		return
	}

	// 🔴 THE SUBJECT IS VALIDATED AGAINST `Candidates`, NOT ACCEPTED FROM THE FORM. The
	// `select` in the rendered page constrains a browser and nothing else; this request
	// may carry any id at all. Without this, a caller with admin on one scope could
	// share it with any principal whose id they could guess — which is what makes the
	// unguessable ids load-bearing rather than cosmetic, and is not a property to rest
	// a share flow on.
	candidates, err := s.sharing.Candidates(id.Principal)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the authority could not be read")
		return
	}
	subject, ok := pick(candidates, control.ID(r.PostFormValue(FieldSubject)))
	if !ok {
		writePlain(w, http.StatusForbidden, shareWriteRefusal)
		return
	}

	// `r.PostForm[...]`, not `PostFormValue`: a checkbox group repeats one name and
	// the single-value read would take the first box only. See `FieldVerb`.
	var verbs []control.Verb
	for _, raw := range r.PostForm[FieldVerb] {
		verbs = append(verbs, control.Verb(raw))
	}
	set := control.NewVerbSet(verbs...)
	if set.Empty() {
		// `NewVerbSet` DROPS an unknown verb rather than refusing, so an empty set here
		// is both "no box was ticked" and "every box carried a verb this build does not
		// define". Both are refused, and the refusal is uniform: distinguishing them
		// would report which verbs this build knows.
		writePlain(w, http.StatusBadRequest, shareWriteRefusal)
		return
	}

	effect, err := s.sharing.Share(r.Context(), id.Principal, id.Auth, scope, subject, set)
	if err != nil {
		s.refuseWrite(w, err)
		return
	}
	s.redirectToScope(w, r, scope, outcomeShared, effect)
}

// handleUnshare revokes a grant.
//
// 🔴 IT DOES NOT TAKE A SCOPE. The grant id alone says which scope this write touches,
// and `Sharing.Unshare` resolves the authority check from the grant row for exactly
// that reason — a scope taken from the form beside it would let a caller with admin on
// scope A authorise a revocation on scope B. The redirect afterwards needs a scope, so
// it is read from the grant's own row through the same read.
func (s *Server) handleUnshare(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	if err := r.ParseForm(); err != nil {
		writePlain(w, http.StatusBadRequest, shareWriteRefusal)
		return
	}
	grant := control.ID(r.PostFormValue(FieldGrant))
	if grant == "" {
		writePlain(w, http.StatusBadRequest, shareWriteRefusal)
		return
	}
	scope, known := s.sharing.ScopeOfGrant(grant)

	effect, err := s.sharing.Unshare(r.Context(), id.Principal, id.Auth, grant)
	if err != nil {
		s.refuseWrite(w, err)
		return
	}
	if !known {
		// The revoke succeeded, so `Unshare` did resolve the grant; only this
		// ancillary lookup missed it, which a concurrent refresh can cause. Redirect
		// to the index rather than to a scope page addressed by an empty id.
		http.Redirect(w, r, SharePath, http.StatusSeeOther)
		return
	}
	s.redirectToScope(w, r, scope, outcomeRevoked, effect)
}

// refuseWrite maps a write failure onto a status, and it is the ONE place that
// mapping lives so the two write routes cannot answer differently.
func (s *Server) refuseWrite(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, control.ErrAuthorityReadOnly):
		// 501: the request was well-formed and permitted, and this deployment has no
		// way to carry it out. A 403 here would send an operator hunting a permission
		// problem that does not exist.
		//
		// 🔴 THIS ARM IS UNREACHABLE THROUGH THE ONE READ-ONLY AUTHORITY THIS TREE HAS,
		// AND SAYING SO IS BETTER THAN LETTING IT READ AS COVERAGE. `tokenfile.Source`
		// grants no `admin` verb to anybody, so the authority check above refuses first
		// and this arm never runs on a token-file deployment — the condition is
		// announced on the PAGE instead ([ReadOnlyAuthority]), where it IS reachable.
		// The arm is kept because it is the correct answer for an authority that
		// confers admin and cannot write, which is one new `control.Source` away, and
		// `TestAReadOnlyWriteIsRefusedWithTheCauseRatherThanAPermission` pins the
		// MAPPING through the real dispatcher so it cannot rot unnoticed.
		writePlain(w, http.StatusNotImplemented, ReadOnlyAuthority)
	case errors.Is(err, ErrNotPermitted):
		writePlain(w, http.StatusForbidden, shareWriteRefusal)
	default:
		writePlain(w, http.StatusInternalServerError, "the share could not be recorded")
	}
}

// redirectToScope is the POST-redirect-GET hop, carrying a CODE and a validated
// instant. See the outcome constants for why neither is free text.
func (s *Server) redirectToScope(w http.ResponseWriter, r *http.Request, scope control.ID, outcome string, effect Effect) {
	q := url.Values{}
	q.Set(QueryScope, string(scope))
	q.Set(QueryOutcome, outcome)
	switch {
	case effect.Immediate:
		q.Set(QueryEffectiveBy, effectNow)
	case effect.EffectiveBy != "":
		q.Set(QueryEffectiveBy, effect.EffectiveBy)
	}
	http.Redirect(w, r, SharePath+"?"+q.Encode(), http.StatusSeeOther)
}

// pick finds a candidate by id. A linear scan over a list the caller is about to be
// shown; there is no index worth maintaining for it.
func pick(candidates []Subject, id control.ID) (Subject, bool) {
	for _, c := range candidates {
		if c.ID == id {
			return c, true
		}
	}
	return Subject{}, false
}

// outcomeFrom renders the banner for a redirect that arrived from a write.
//
// 🔴 EVERY BRANCH RETURNS A SENTENCE WRITTEN HERE, AND THE ONLY CALLER-SUPPLIED TEXT
// THAT REACHES THE PAGE IS AN INSTANT THIS FUNCTION RE-FORMATTED FROM A PARSE. An
// unparseable `by` is not echoed and not reported — it degrades to the unbounded
// sentence, which is the true statement when nothing reliable is known about when the
// write lands.
//
// 🔴 AND THE DEFERRED BRANCH ALWAYS CARRIES ITS QUALIFIER, WHICH IS
// `control.EffectDeferred`'s STATED REQUIREMENT: *"the qualifier is not optional, and
// omitting it is the sharing dialog implying a guarantee the system cannot make."*
func outcomeFrom(r *http.Request) string {
	q := r.URL.Query()
	var what string
	switch q.Get(QueryOutcome) {
	case outcomeShared:
		what = "Shared."
	case outcomeRevoked:
		what = "Revoked."
	default:
		// Includes the empty case: an ordinary page load carries no outcome.
		return ""
	}
	switch by := q.Get(QueryEffectiveBy); {
	case by == effectNow:
		return what + " It is in force on this replica now."
	case by == "":
		return what + " The authority declares no bound on when this replica will serve it."
	default:
		at, err := time.Parse(time.RFC3339, by)
		if err != nil {
			return what + " The authority declares no bound on when this replica will serve it."
		}
		return what + " It is not in force on this replica yet; effective by " +
			at.UTC().Format(time.RFC3339) + "."
	}
}
