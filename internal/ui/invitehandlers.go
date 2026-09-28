package ui

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/invite"
)

// inviteRefusal is what a caller gets for a project they may not invite into AND for a
// project that does not exist.
//
// 🔴 ONE ANSWER FOR BOTH, WHICH IS `scopeRefusal`'s RULE ONE OBJECT OVER. A 404 for an
// unknown project beside a 403 for somebody else's would turn this page into an existence
// oracle over every project in the deployment: a caller who manages one could enumerate
// the rest by watching which status came back. Project ids are unguessable by construction
// (`control.NewID` uses `crypto/rand`) and this refusal is what keeps that worth something.
const inviteRefusal = "no such project, or it is not yours to invite into"

// inviteWriteRefusal is the uniform refusal for both write routes. Same discipline as
// `shareWriteRefusal`: it names no reason, so a caller cannot tell an unknown project from
// one they may not touch, nor an unknown invitation from one that is no longer open.
const inviteWriteRefusal = "that invitation cannot be recorded"

// roleRefusal is what a caller gets for a role they may not confer.
//
// 🔴 IT DISCRIMINATES, AND THAT IS CORRECT HERE FOR `ReadOnlyAuthority`'s REASON RATHER
// THAN AN EXCEPTION TO THE UNIFORM-REFUSAL RULE. Everything the rule protects is a fact
// about somebody ELSE — which projects exist, which invitations exist, which credentials
// are valid. This sentence is a fact about the CALLER'S OWN standing in a project they
// have already proved they manage, so it is identical for every caller in that state and
// discloses nothing they could not read off the page they submitted. The alternative is a
// person who picked `owner` from a list getting "that invitation cannot be recorded" and
// having no way to learn that the list offered them a value their role cannot hand out.
//
// ⚠ IT DOES NOT NAME THE ROLE THEY HOLD OR THE ROLE THEY ASKED FOR. Both are in the
// operator's log line instead — not for secrecy, but because a page that echoed the
// submitted role would be reflecting caller-chosen text into a sentence the page presents
// as its own, which is the shape `outcomeFrom` refuses for the share flow's banner.
const roleRefusal = "that role cannot be conferred by the role you hold in this project"

// NoInviteStore is what the invite flow says on a deployment that has no store to hold an
// invitation.
//
// 🔴 IT IS SAID ON THE PAGE RATHER THAN REFUSED AT THE ROUTE, WHICH IS `ReadOnlyAuthority`'s
// RULING AND NOT `refuseUnconfiguredOAuth`'s. The two look interchangeable and are not: the
// OAuth rows are reached by a BUTTON that `SignInPage` withholds when the provider is nil,
// so a 501 there is only ever seen by somebody driving the route directly. `GET /invite` is
// reached by a LINK IN THE HEADER OF EVERY PAGE, unconditionally — see `shell` — so a 501
// there is a dead link in the frame of the whole surface. The read therefore answers the
// page and says this; the two WRITES, which nothing links, answer 501 with this sentence.
//
// ⚠ IT NAMES THE CONFIGURATION RATHER THAN THE SYMPTOM, for the reason `ReadOnlyAuthority`
// gives: it discriminates nothing, and the alternative is an operator who has not wired a
// database hunting a permission problem that does not exist.
const NoInviteStore = "This deployment has no invitation store, so no invitation can be " +
	"minted or redeemed here: cairn-ui was started without a database to hold them."

// InviteHonesty is the notice the invite flow carries on every shape of its page.
//
// 🔴 IT IS PINNED AS ONE NORMALISED STRING, WHICH IS `ReplicaHonesty`'s RULING AND FOR THE
// SAME REASON: a test asserting the page contains "token" or "once" passes against a
// reworded notice that has quietly dropped a clause, and the clause most worth dropping is
// always the one that makes the product sound weakest. `TestTheInviteHonestyNoticeIsPinnedWhole`
// compares the rendered text against this constant with entities resolved and whitespace
// collapsed. A cosmetic reword FAILS it; that is the price of a machine-readable claim.
//
// 🔴 EVERY CLAUSE IS A PROPERTY MEASURED SOMEWHERE ELSE IN THIS TREE, WHICH IS WHY THERE
// ARE EXACTLY THESE THREE:
//
//   - "shown once and cannot be recovered" — `invite.NewToken` returns the token and only
//     its SHA-256 digest is stored, and `invite.Store`'s own doc says this package has no
//     function that can produce it again from anything persisted.
//   - "anyone who holds the link can use it" — the token IS the authority
//     (`internal/invite`'s package doc), and redemption may PROVISION a principal the
//     control plane has never seen, which is the operator decision `ControlInviting.Redeem`
//     records.
//   - "revoking it stops it being redeemed and takes nothing back from somebody who
//     already has" — `invite.Store.Revoke` acts only on an OPEN invitation, and the
//     authority a redeemed one conferred is a `member-set` record on an append-only
//     journal. Revoking the invitation does not revoke the membership.
//
// ⚠ WHAT IT DOES NOT SAY, DELIBERATELY: it gives no lifetime. `invite.DefaultTTL` is seven
// days and a minting caller may pass another, so a number here would be a promise about a
// value this sentence does not own. The bound that IS known travels per invitation instead,
// as the expiry rendered on its own row.
const InviteHonesty = "An invitation is a link, and the link is the authority: it is shown " +
	"once and cannot be recovered, anyone who holds it can use it to join this project, and " +
	"revoking it stops it being redeemed but takes nothing back from somebody who has " +
	"already joined."

// inviteOutcomeRevoked is the one outcome a redirect into this flow may carry.
//
// 🔴 A CODE FROM A CLOSED SET RATHER THAN A SENTENCE FROM THE QUERY STRING, which is the
// share flow's ruling and the reason is identical: a redirect target is something anybody
// can put in a link, so a reflected sentence would let an attacker choose the text of a
// banner the page presents as its OWN. There is exactly one code because there is exactly
// one write that redirects — the MINT cannot, since its whole answer is a value that
// survives no hop. See `handleInvite`.
const inviteOutcomeRevoked = "revoked"

// handleInvitePage renders the invite flow: the index with no `?project=`, one project's
// page with it.
//
// 🔴 THE NARROWING IS `Invitable`, AND `Outstanding` IS REACHED ONLY THROUGH IT. That is not
// a stylistic ordering — `Inviting.Outstanding` performs NO authority check and its own
// comment says so in as many words, because re-deriving the narrowing per call would be the
// second model read `Invitable` argues against. So the list this function walks IS the
// authority check, and a project that is not in it is refused before `Outstanding` is
// called at all.
func (s *Server) handleInvitePage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	view := InviteView{
		Viewer: id.Principal.Display,
		// The same derivation `handlePage` and `handleSharePage` use, and for the same
		// reason: the token comes from the COOKIE on this request, so a caller
		// authenticated by a bearer header renders no forms.
		CSRF:    csrfTokenFor(r),
		Outcome: inviteOutcomeFrom(r),
		NoStore: s.inviting == nil,
	}
	if s.inviting == nil {
		// Nothing to ask. The page says so — see [NoInviteStore] for why this is a page
		// rather than a refusal.
		s.renderInvite(w, view)
		return
	}

	// 🔴 POPULATED BEFORE THE BRANCH BELOW, NOT ONLY ON THE INDEX, WHICH IS THE CORRECTION
	// `handleSharePage` ALREADY CARRIES. `InvitePage` renders the index whenever the
	// project's NAME is empty, and a project the authority cannot name has an empty one —
	// so filling this only on the index branch would leave a path on which the page renders
	// "No project is yours to invite into. That is an authority answer, not an empty
	// control plane." to a caller who had just proved they manage one.
	view.Projects = s.inviting.Invitable(id.Principal)

	project := control.ID(r.URL.Query().Get(QueryProject))
	if project == "" {
		s.renderInvite(w, view)
		return
	}
	chosen, found := pickProject(view.Projects, project)
	if !found {
		writePlain(w, http.StatusNotFound, inviteRefusal)
		return
	}
	rows, err := s.inviting.Outstanding(project)
	if err != nil {
		// The reason does not reach the wire: a store error can carry a DSN, a table name
		// or a driver's own text, each a fact about the deployment rather than about the
		// request. It goes to the operator's log, which is where this surface puts verdicts.
		s.logf("the invitations for a project could not be read: %v", err)
		writePlain(w, http.StatusInternalServerError, "the invitations could not be read")
		return
	}
	view.Project = chosen
	view.Outstanding = inviteRows(rows, s.now())
	s.renderInvite(w, view)
}

// handleInvite mints one invitation and renders its link ONCE.
//
// # 🔴 IT DOES NOT REDIRECT, AND THAT IS THE ONE PLACE THIS FLOW BREAKS THE HOUSE PATTERN
//
// Every other write on this surface is a POST-redirect-GET, because a write that renders its
// own answer is a write a refresh repeats (`redirectToScope`). This one cannot be, and the
// reason is the value it produces: `invite.NewToken` returns the token exactly once and only
// its digest is stored, so there is nothing to render on the far side of a hop. The two ways
// to keep the redirect were both weighed and refused:
//
//   - PUT THE TOKEN IN THE REDIRECT URL. Refused outright. It is a bearer capability that can
//     create a principal, and a query parameter lands in browser history, in the referrer the
//     next hop receives and in every access log on the way — which is the argument
//     `inviteTokenField` makes about the same value, and the reason the token is a form field
//     everywhere else.
//   - STASH IT SERVER-SIDE, KEYED BY SESSION, AND REDIRECT. Refused as a worse trade: it is a
//     new table of live capabilities with its own expiry, its own single-use question and its
//     own restart behaviour, bought to avoid a refresh that mints a spare invitation.
//
// ⚠ SO THE ACCEPTED COST, NAMED RATHER THAN DISCOVERED: a browser reload of this response
// re-submits the form and mints a SECOND invitation. Browsers prompt before doing it, the
// extra invitation is listed on the project's page and is revocable, and an invitation grants
// nothing until it is redeemed (`internal/invite`'s package doc). The response carries
// `Cache-Control: no-store` — see [writeHTMLNoStore] — because its body is the capability.
func (s *Server) handleInvite(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	if s.inviting == nil {
		s.refuseWithoutInviteStore(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		writePlain(w, http.StatusBadRequest, inviteWriteRefusal)
		return
	}
	project := control.ID(r.PostFormValue(FieldProject))
	role := control.Role(r.PostFormValue(FieldRole))

	// 🔴 THIS READ IS FOR THE PROJECT'S DISPLAY NAME AND IS NOT THE AUTHORITY CHECK. `Mint`
	// does the check itself, from the model, and it is the only one that counts — a gate
	// here would be a second place the rule lives and the second place is what drifts.
	// Looking the name up BEFORE the write rather than after is deliberate: after a
	// successful mint a concurrent membership change could remove the project from this
	// list, and the page would then render an invitation with no name on it.
	chosen, _ := pickProject(s.inviting.Invitable(id.Principal), project)

	token, inv, err := s.inviting.Mint(r.Context(), id.Principal, project, role, 0)
	if err != nil {
		s.refuseInviteWrite(w, err, "mint")
		return
	}
	// 🔴 THE PROJECT RENDERED IS THE ONE THE STORE RECORDED, NEVER THE ONE THE FORM NAMED.
	// `Mint` returns the `invite.Invite` it created, so `inv.ProjectID` is what was written;
	// the form value only found a display name. They agree today because `Mint` writes what
	// it was given, and reading the stored value is what keeps that an observation rather
	// than an assumption.
	if chosen.ID != inv.ProjectID {
		chosen = control.NamedProject{ID: inv.ProjectID}
	}
	s.logf("an invitation was minted: project=%s role=%s expires=%s by=%s",
		inv.ProjectID, inv.Role, inv.ExpiresAt.UTC().Format(time.RFC3339), inv.Inviter)

	view := InviteView{
		Viewer:  id.Principal.Display,
		CSRF:    csrfTokenFor(r),
		NoStore: false,
		Project: chosen,
		Minted: &MintedInvite{
			// 🔴 A PATH AND A QUERY, NEVER AN ABSOLUTE URL, BECAUSE THIS PROCESS DOES NOT
			// KNOW ITS OWN EXTERNAL ORIGIN. The scheme and host a reader must send are
			// configuration — the identical reason `OAuthCallbackPath` is spelled as a path
			// and its full URL is not — and the only candidate available here is a `Host`
			// header a proxy chooses. A page that guessed it would hand somebody a link to
			// the wrong hostname, which for a single-use capability means a link they cannot
			// re-issue. [MintedInvite.Link] says so on the page instead.
			Link:    JoinPath + "?" + url.Values{inviteTokenField: []string{token}}.Encode(),
			Role:    inv.Role,
			Expires: inv.ExpiresAt.UTC().Format(time.RFC3339),
		},
	}
	s.renderInviteNoStore(w, view)
}

// handleInviteRevoke withdraws an outstanding invitation.
//
// 🔴 IT AUTHORISES FROM THE INVITATION'S OWN STORED ROW AND NOT FROM THE PROJECT THE FORM
// NAMES — `ControlInviting.Revoke` resolves the project from the digest for exactly the
// reason `handleUnshare` does not take a scope: a caller who manages project A must not be
// able to revoke an invitation into project B by naming A in the form.
//
// ⚠ THE FORM DOES CARRY A PROJECT, AND IT IS ANCILLARY IN `ScopeOfGrant`'s SENSE — the
// redirect needs somewhere to land and nothing else reads it. It is never an authority
// input: `Revoke` has already refused or succeeded by the time it is used. The worst a
// wrong value can do is land the caller on a project page that answers the uniform 404,
// which is a page they could have asked for by typing.
func (s *Server) handleInviteRevoke(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	if s.inviting == nil {
		s.refuseWithoutInviteStore(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		writePlain(w, http.StatusBadRequest, inviteWriteRefusal)
		return
	}
	digest := r.PostFormValue(FieldDigest)
	if digest == "" {
		writePlain(w, http.StatusBadRequest, inviteWriteRefusal)
		return
	}
	if err := s.inviting.Revoke(r.Context(), id.Principal, digest); err != nil {
		s.refuseInviteWrite(w, err, "revoke")
		return
	}
	q := url.Values{}
	if project := r.PostFormValue(FieldProject); project != "" {
		q.Set(QueryProject, project)
	}
	q.Set(QueryOutcome, inviteOutcomeRevoked)
	http.Redirect(w, r, InvitePath+"?"+q.Encode(), http.StatusSeeOther)
}

// handleJoinPage is what an invitation LINK opens, and it is the only PUBLIC page this flow
// has.
//
// # 🔴 IT DOES NOT LOOK THE TOKEN UP, AND THAT IS WHAT MAKES A PUBLIC ROW SAFE
//
// Every token renders the same page. A page that resolved the token would answer differently
// for one that exists and one that does not — on an UNAUTHENTICATED route, at whatever rate a
// caller cares to drive it — which is an oracle over other people's invitations. That is
// `flights.start`'s ruling restated one layer up: the token is resolved exactly once, at the
// OAuth callback, where it is being redeemed anyway and where its refusal is
// indistinguishable from every other reason a sign-in did not complete.
//
// ⚠ THE COST IS A UX ONE AND IT IS A REVISITABLE DECISION RATHER THAN A FORCED ONE. A dead,
// expired or already-used link shows "sign in to accept" and then a generic refusal at the
// far end, and the page cannot name the project or the role somebody is being invited to. An
// operator who would rather show those has to accept that the page becomes an oracle, or
// design a second mechanism that reveals them without resolving the token on a public route.
//
// ⚠ AND IT DOES REFLECT THE TOKEN, INTO A HIDDEN FORM FIELD, WHICH IS NOT THE SHAPE
// `outcomeFrom` REFUSES. That rule is about caller-chosen text rendered as a SENTENCE the
// page presents as its own; this is the visitor's own credential going back into the request
// that spends it, in a quoted attribute value gomponents escapes — the same position, and the
// same reasoning, as the search box keeping what the reader typed.
func (s *Server) handleJoinPage(w http.ResponseWriter, r *http.Request, _ identity.Identity) {
	// `FormValue` rather than `URL.Query().Get`: a GET's form values ARE its query, and
	// reading it this way keeps one spelling for a field that is posted on the next hop.
	token := r.URL.Query().Get(inviteTokenField)
	// 🔴 THE PROVIDER PREDICATE IS THE SAME ONE THE SIGN-IN PAGE ASKS, NOT A NEW CHECK.
	// `providerArmed` is per-render deliberately (see its comment): a deployment whose key
	// set has never been fetched re-arms without a restart, so a value sampled anywhere else
	// would leave this page refusing after the provider came back.
	s.render(w, JoinPage(token, s.providerArmed()))
}

// refuseWithoutInviteStore is what the two invite WRITES answer on a deployment with no
// store. The READ answers a page instead — see [NoInviteStore].
//
// ⚠ 501 RATHER THAN 404 OR 403, AND THE DIFFERENCE IS WHICH FACT IS BEING REPORTED. A 404
// would say the path is not a route, and it is one — it is in `DeclaredRoutes()`, which this
// repository publishes. A 403 would say the caller is not permitted, which sends an operator
// hunting a permission problem that does not exist. 501 says the route exists and this
// deployment does not implement it. Same ruling as `refuseUnconfiguredOAuth`.
func (s *Server) refuseWithoutInviteStore(w http.ResponseWriter) {
	s.logf("an invitation write was refused: this deployment has no invite store")
	writePlain(w, http.StatusNotImplemented, NoInviteStore)
}

// refuseInviteWrite maps a write failure onto a status, and it is the ONE place that mapping
// lives so the two write routes cannot answer differently — `refuseWrite`'s reason, and the
// `what` argument is only for the log line that attributes the refusal to a route.
func (s *Server) refuseInviteWrite(w http.ResponseWriter, err error, what string) {
	s.logf("an invitation %s was refused: %v", what, err)
	switch {
	case errors.Is(err, ErrRoleNotConferrable):
		// 403 with the cause. See [roleRefusal] for why this one discriminates and the
		// others do not.
		writePlain(w, http.StatusForbidden, roleRefusal)
	case errors.Is(err, ErrNotInvitable), errors.Is(err, invite.ErrNotRedeemable):
		// 🔴 ONE STATUS AND ONE SENTENCE FOR BOTH, WHICH IS THE POINT RATHER THAN A
		// SHORTCUT. `ErrNotInvitable` is "no such project, or not yours";
		// `ErrNotRedeemable` is "no such invitation, or it is no longer open" — and
		// `ControlInviting.Revoke` already collapses an unknown digest into the second so
		// that a revoke cannot be used to ask whether a digest exists. Answering them
		// differently here would rebuild the oracle one layer up.
		writePlain(w, http.StatusForbidden, inviteWriteRefusal)
	default:
		writePlain(w, http.StatusInternalServerError, "the invitation could not be recorded")
	}
}

func (s *Server) renderInvite(w http.ResponseWriter, view InviteView) {
	s.render(w, InvitePage(view))
}

// renderInviteNoStore is the mint response, and it is a separate function ONLY so the
// `no-store` header cannot be forgotten by the ordinary render path.
func (s *Server) renderInviteNoStore(w http.ResponseWriter, view InviteView) {
	var b strings.Builder
	if err := InvitePage(view).Render(&b); err != nil {
		writePlain(w, http.StatusInternalServerError, "the page could not be rendered")
		return
	}
	writeHTMLNoStore(w, http.StatusOK, b.String())
}

// pickProject finds a project by id in the caller's narrowed list.
//
// 🔴 AN EMPTY ID MATCHES NOTHING, for `pickScope`'s reason: a project the authority could
// not name would otherwise be the one case where `?project=` with nothing after it resolves
// to a page.
func pickProject(projects []control.NamedProject, id control.ID) (control.NamedProject, bool) {
	for _, p := range projects {
		if p.ID != "" && p.ID == id {
			return p, true
		}
	}
	return control.NamedProject{}, false
}

// inviteRows projects the store's invitations onto what the page renders, at ONE instant.
//
// 🔴 THE CLOCK IS PASSED IN AND READ ONCE, NOT PER ROW. `invite.Invite.StateAt` is a
// function of a time, so calling `time.Now()` per row would let a page render two
// invitations that expired in the same second in two different states — and the boundary is
// closed at `ExpiresAt`, so that is one clock tick wide rather than hypothetical.
func inviteRows(rows []invite.Invite, now time.Time) []InviteRow {
	out := make([]InviteRow, 0, len(rows))
	for _, inv := range rows {
		out = append(out, InviteRow{
			Digest: inv.Digest,
			Short:  shortDigest(inv.Digest),
			Role:   inv.Role,
			State:  string(inv.StateAt(now)),
			// RFC3339 in UTC, which is the spelling every other instant on this surface
			// uses (`GrantRow.GrantedAt`, `Effect.EffectiveBy`). A locale-formatted date
			// would be a second spelling of an instant on one page.
			Created: inv.CreatedAt.UTC().Format(time.RFC3339),
			Expires: inv.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

// shortDigestChars is how much of a digest the page shows.
//
// ⚠ A PREFIX OF A DIGEST IS NOT A CREDENTIAL, which is what makes rendering one safe, and
// `internal/pgstore`'s own comment makes the same argument about the same kind of value: a
// digest cannot be inverted to the token a redeemer must present, and a prefix of one is
// strictly less. What it buys is that an administrator looking at two rows can tell which is
// which, and can match a row against a log line — the full digest is in the revoke form's
// hidden field either way, so shortening this hides nothing from anyone reading the source.
const shortDigestChars = 12

func shortDigest(digest string) string {
	if len(digest) <= shortDigestChars {
		return digest
	}
	return digest[:shortDigestChars]
}

// inviteOutcomeFrom renders the banner for a redirect that arrived from a revoke.
//
// 🔴 EVERY BRANCH RETURNS A SENTENCE WRITTEN HERE AND NO CALLER-SUPPLIED TEXT REACHES THE
// PAGE — `outcomeFrom`'s rule, with one fewer moving part because this flow has no
// effective-by instant to re-format. An unrecognised code renders nothing at all.
//
// ⚠ THE SENTENCE SAYS WHAT REVOKING DOES *NOT* DO, and that is not padding. An
// administrator who revokes an invitation somebody has already redeemed has changed nothing
// about their access, and a bare "Revoked." invites exactly the wrong conclusion. The same
// claim is in [InviteHonesty] above the list; this is the one place a reader is looking
// immediately after acting.
func inviteOutcomeFrom(r *http.Request) string {
	if r.URL.Query().Get(QueryOutcome) != inviteOutcomeRevoked {
		return ""
	}
	return "Revoked. That link can no longer be redeemed. Anybody who already joined with " +
		"it keeps their membership."
}
