package ui

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/netid"
)

// The two form fields and the one header this surface reads from a request body.
//
// 🔴 `FieldToken` IS `type="password"` IN THE RENDERED FORM AND THE VALUE IS NEVER
// ECHOED BACK. A refused sign-in re-renders an EMPTY form with a fixed sentence; putting
// the submitted value back in the field — which is what an ordinary form does to be
// helpful — would write a bearer credential into a page, into the browser's back/forward
// cache, and into any transcript that captures the response.
const (
	FieldToken = "token"
	FieldCSRF  = "csrf"
	// HeaderCSRF is accepted beside the form field so a non-browser caller that holds a
	// session can present the token without constructing a form body. It is read only
	// if the field is absent.
	HeaderCSRF = "X-Cairn-CSRF"
)

// The two cross-site refusals. They are DIFFERENT strings on purpose: the gates are two
// independent claims, and a test that could not tell them apart would report a kill by
// the wrong guard as a kill.
//
// ⚠ NEITHER NAMES WHAT WAS WRONG BEYOND ITS OWN CLASS. "your Origin header said
// evil.example" would be a reflection of attacker-controlled text into a response, and
// "the token should have been X" is the whole secret.
const (
	crossSiteRefusal = "cross-site request refused"
	csrfRefusal      = "csrf token missing or invalid"
)

// signInRefused is the ONE thing a failed sign-in says, for every reason it can fail.
//
// 🔴 A REFUSAL THAT DISCRIMINATES IS AN ENUMERATION API — the same ruling
// `internal/api`'s uniform 401 makes, one layer up where a human reads it. "no such
// credential" and "that credential is revoked" are different facts about the token
// table, and the second one confirms a guess.
const signInRefused = "That credential was not accepted."

// sameOrigin answers whether a state-changing request came from this origin.
//
// 🔴 IT COMPARES `Origin` AGAINST `Host`, WHICH SOUNDS CIRCULAR AND IS NOT. Both are
// supplied by the client, so neither is trustworthy on its own — but a browser sets
// `Origin` from the page that MADE the request and `Host` from the URL it was sent to,
// and it will not let a page lie about the first. An attacker's page at `evil.invalid`
// posting here therefore sends `Origin: https://evil.invalid` with our `Host`, and the
// two disagree. A non-browser attacker can set both to anything, but a non-browser
// attacker also has no victim's cookie to ride, which is the entire premise of CSRF.
//
// 🔴 A MISSING `Origin` IS REFUSED, WHICH IS THE FAIL-CLOSED DIRECTION AND HAS A COST.
// Browsers send `Origin` on every state-changing request, so the header's absence means
// a client this surface was not built for — and `curl` is such a client. The cost is
// that a script driving this surface must set the header; the alternative is a gate that
// any request can skip by omitting one line, which is not a gate.
//
// ⚠ IT COMPARES HOST-AND-PORT AND DELIBERATELY IGNORES THE SCHEME. This process cannot
// know whether it is behind a TLS-terminating proxy — every signal that would tell it is
// a header the client controls, which is the `TrustedHeader` hazard in miniature — so a
// scheme comparison would either be wrong behind a proxy or would be trusting
// `X-Forwarded-Proto`. Host-and-port is the part a cross-origin attacker cannot match.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Host == r.Host
}

// csrfTokenFor derives the token a page must carry, from the cookie on THIS request.
//
// An empty string for a request with no session cookie: there is no session, so there is
// no token, and rendering a well-formed-looking one would be rendering a value that
// cannot validate.
func csrfTokenFor(r *http.Request) string {
	cookie, err := r.Cookie(identity.SessionCookieName)
	if err != nil || cookie.Value == "" {
		return ""
	}
	return identity.CSRFTokenFor(cookie.Value)
}

// csrfTokenValid is gate (6). See [Server.ServeHTTP] for why it runs after the chain.
//
// 🔴 IT READS THE COOKIE DIRECTLY RATHER THAN THE `identity.Identity`, AND THAT IS
// FORCED. `identity.Identity` carries no backend discriminator and no session id — by
// design, because a field naming the backend is a field a route can branch on — so the
// only thing that can bind a submitted token to the session it belongs to is the cookie
// itself.
//
// ⚠ THE CONSEQUENCE THIS COMMENT USED TO STATE WAS WRONG, AND THE CORRECTION IS RECORDED
// RATHER THAN SWAPPED IN. It read: "a request authenticated by the machine-token backend
// with no cookie has no token and is refused on every state-changing row." It is not
// refused. The expected token is derived from the cookie ON THIS REQUEST, which the caller
// chooses — so such a caller sends any value X as the cookie plus `identity.CSRFTokenFor(X)`
// as the token and passes this gate.
//
// 🔴 THE IMPACT IS NIL, AND SAYING WHY IS THE POINT — "no impact" alone is how a wrong
// claim gets replaced by an unexamined one. The gate is reachable only AFTER
// authentication (see [Server.ServeHTTP]) — the `classPublic` POST rows dispatch ahead of the
// chain and never reach it — and every authenticated state-changing row behind it decides
// from the IDENTITY the chain resolved (`id.Auth`, `id.Principal`, or the whole `id` handed to
// a predicate), never from the cookie, so a caller who chose their own cookie gains no
// authority by it. Sign-out is the one row that reads the cookie, and it revokes
// `sha256(X)` for a caller-chosen X, which is nothing. A narrowed bearer caller who reaches a
// row this way is still narrowed: the invite rows require a real membership through
// `membershipActor(id)`, and `POST /ring` asks `presence.Store.For(id, …)`, which refuses a
// narrowed viewer outright.
// 🔴 THE ENUMERATION OF ROWS THAT STOOD HERE IS DELETED, AS THIS PARAGRAPH SAID IT WOULD BE ON
// ITS THIRD STALENESS. It went stale when the share flow added two rows and again when the
// invite flow added two more; the bell (`POST /ring`) would have been the third. A list of
// rows maintained by hand in a comment is a ledger with no gate — `ui.DeclaredRoutes()`
// filtered on `stateChanging` is the authority. The impact argument above is what survived
// every movement, so it is all that is kept. The CSRF property itself is untouched, because it defends against a CROSS-SITE
// attacker riding a victim's cookie, and such an attacker can neither READ a `HttpOnly`
// cookie nor SET a `__Host-` one for this origin.
//
// So the honest claim is narrower than a session binding: this gate binds a submitted
// token to THE COOKIE ON THIS REQUEST. For a browser that did not choose its cookie that
// is a session binding; for a caller who did, it is a self-consistency check — and that is
// sound, because the gate's job is refusing CROSS-SITE requests and a caller choosing its
// own cookie is not cross-site.
//
// 🔴 THE REAL CONSTRAINT ON A LATER PHASE SURVIVES THE CORRECTION: a header-authenticated
// caller with NO SESSION cannot perform a state change that needs a SESSION-BOUND token,
// since the only token it can compute is bound to a cookie value no session exists for.
// It is written here rather than discovered there.
func csrfTokenValid(r *http.Request) bool {
	cookie, err := r.Cookie(identity.SessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	presented := r.PostFormValue(FieldCSRF)
	if presented == "" {
		presented = r.Header.Get(HeaderCSRF)
	}
	return identity.CSRFTokenValid(cookie.Value, presented)
}

// handleSignInForm renders the way in. It is PUBLIC, and the one thing it reads is the
// return-to value, which it validates and carries into both forms as a hidden field.
//
// 🔴 A BROWSER THAT IS ALREADY SIGNED IN AND ARRIVES WITH A `next` IS SENT STRAIGHT THERE.
// That is the shape of a stale tab: a page redirected here while the session was expired, the
// person signed in in another tab, and this one is reloaded. Without `next` the page renders as
// it always did — a signed-in person may want the form to switch credentials, and a redirect
// with nowhere particular to go would take that away. The identity is resolved through the
// SAME chain gate (4) uses; this row is public, so the dispatcher handed it the zero value.
//
// 🔴 A `next` WHOSE PATH IS `/sign-in` ITSELF IS SENT TO `/`, NOT FOLLOWED. `safeNext` accepts
// `/sign-in` (the loop check was deleted — see its comment), so without this a signed-in browser
// at `/sign-in?next=/sign-in?next=…/scope` was redirected once per nesting level: measured 1, 21
// and 140 hops at depths 1, 21 and 140, and a 140-deep value is ~1,966 bytes, under
// `maxNextLen` — a browser gives up with "too many redirects" long before. A `next` naming the
// sign-in page carries no destination a SIGNED-IN person can use, so the answer is the default
// landing, in one hop. `/` rather than rendering the form, because the form would carry that
// same `next` and a completed sign-in would land straight back on this branch.
// The PATH is compared DECODED, as the dispatcher routes it, so `/sign%2Din?next=…` is caught
// too; a value `url.Parse` refuses cannot be routed to this row at all.
// `TestNoUnauthenticatedRequestShapeLoops` walks a 140-deep target.
func (s *Server) handleSignInForm(w http.ResponseWriter, r *http.Request, _ identity.Identity) {
	next := requestedNext(r)
	if next != "" {
		if id, err := s.auth.Authenticate(r); err == nil && id.Valid() {
			if u, perr := url.Parse(next); perr == nil && u.Path == SignInPath {
				next = RootPath
			}
			http.Redirect(w, r, next, http.StatusSeeOther)
			return
		}
	}
	s.renderSignIn(w, http.StatusOK, "", next)
}

// handleSignIn exchanges a presented credential for a session. It is one of TWO doors into
// [Server.openSession], which is where a session is actually minted and where the fixation
// guard lives — the other is `handleOAuthCallback`.
//
// ⚠ THIS COMMENT SAID "IT IS THE ONE PLACE A SESSION IS MINTED" AND CARRIED THE FIXATION
// PARAGRAPH ITSELF. Both were true of a surface with one door; the mint moved to
// `openSession` on the day the second arrived, and a copy of the paragraph here would be a
// second claim about code this function no longer contains.
//
// ⚠ AND THE REVOCATION RUNS ONLY AFTER THE CREDENTIAL IS ACCEPTED — which is a property of
// the ORDER OF THESE TWO CALLS and not of `openSession`, so it is stated here. Revoking on a FAILED
// attempt would make an unauthenticated cross-site POST into a denial-of-service: anyone
// who can make a victim's browser submit this form with a junk token could sign them out
// at will. Gate (2) already refuses that request, so this is defence in depth — but the
// ordering costs nothing and the other ordering is a live hazard if gate (2) is ever
// relaxed.
func (s *Server) handleSignIn(w http.ResponseWriter, r *http.Request, _ identity.Identity) {
	// The return-to value, from the posted body only and validated, carried into every
	// re-render below so a refused attempt keeps it, and into `openSession` as the landing.
	next := requestedNext(r)

	// 🔴 THE CLIENT IS RESOLVED AND METERED BEFORE THE CREDENTIAL IS READ, and the reason
	// is narrower than the one written here first. WHAT IT BUYS: a locked-out client causes
	// NO credential work — no SHA-256 over the presented value, no authority read — which
	// is the entire point of throttling a path whose cost is exactly that work.
	//
	// ⚠ TWO REASONS WERE CLAIMED HERE AND BOTH ARE RETRACTED, recorded rather than swapped
	// because a mutation SURVIVED against them. (1) "A lockout checked after the token is
	// one a valid credential walks through" — false: moving the check below
	// `Authenticate` still refuses, because the check still precedes the session mint. The
	// mutant was measured surviving the test written to catch it. (2) "An attacker who
	// guesses a token mid-run must not get the failure record wiped" — false for THIS
	// limiter: `netid.RateLimiter.RecordSuccess` is a deliberate no-op, and its own comment
	// explains that a success-resets-counter design is the defect, so there is no record to
	// wipe. `internal/api` states reason (1) for itself; it does not transfer here
	// unexamined, and this is what examining it produced.
	//
	// 🔴 `netid.ResolveClient` IS REUSED RATHER THAN REIMPLEMENTED, because the trust
	// boundary is the whole difficulty and it is already decided there: the header is
	// read ONLY from a peer inside the allowlist, and every other peer is keyed on its
	// own address. A local re-implementation is how a surface ends up trusting a
	// forgeable header from anybody.
	client, trusted, ok := netid.ResolveClient(r.Header, r.RemoteAddr, s.trustedProxies)
	if !ok {
		// 🔴 FAIL CLOSED, AND COUNT NOTHING. There is no bucket to count into, and the
		// alternative — one shared key for every unidentifiable request — is the failure
		// `internal/netid` exists to avoid: a single abuser locks out everybody.
		s.logf("sign-in refused: no client identity could be resolved from peer %q", r.RemoteAddr)
		s.renderSignIn(w, http.StatusUnauthorized, signInRefused, next)
		return
	}
	if s.limiter != nil && s.limiter.LockedOut(client) {
		// ⚠ THE BODY IS THE SAME SENTENCE AS EVERY OTHER REFUSAL, AND THE COST IS REAL
		// AND ACCEPTED. `signInRefused`'s own comment rules that a refusal which
		// discriminates is an enumeration API; a lockout that announced itself would be a
		// second thing a failed sign-in can say. So a human who mistyped five times sees
		// no hint that waiting is the remedy — the reason goes to the OPERATOR's log,
		// which is where the pod puts its own `locked-out` verdict. Relaxing this is a
		// decision about that ruling, not about this handler.
		s.logf("sign-in refused: %s is locked out (client identity %s)",
			client, peerState(trusted))
		s.renderSignIn(w, http.StatusUnauthorized, signInRefused, next)
		return
	}
	// refuse is the ONE failure exit for a presented credential, so a rejected token and a
	// narrowed one cannot drift apart in status, body or lockout accounting. `reason` goes to
	// the operator's log only and is a constant chosen here — never caller text.
	refuse := func(reason string) {
		// No token, no digest — and no principal, because for a rejected token there is
		// not one and for a narrowed one naming it would put a real account beside a
		// refusal. What IS recorded is the client, because a refusal nobody can attribute
		// is a refusal nobody can act on, and this surface is reachable from the internet.
		if s.limiter != nil && s.limiter.RecordFailure(client) {
			s.logf("sign-in refused: %s%s — LOCKOUT TRIGGERED (client identity %s)",
				reason, client, peerState(trusted))
		} else {
			s.logf("sign-in refused: %s%s (client identity %s)", reason, client, peerState(trusted))
		}
		s.renderSignIn(w, http.StatusUnauthorized, signInRefused, next)
	}

	presented := r.PostFormValue(FieldToken)
	principal, auth, err := s.credentials.Authenticate(presented)
	if err != nil {
		refuse("")
		return
	}
	// 🔴 A narrowed credential mints no session, and gets the uniform, lockout-counted
	// refusal — the rules and their reasons: `internal/ui/README.md`, "What a session can be minted from".
	if auth.Narrowed() {
		refuse("the credential is narrowed — ")
		return
	}

	s.openSession(w, r, principal, "credential from "+client+" (client identity "+peerState(trusted)+")", next)
}

// openSession is the ONE place a session is minted, and it is one place because there are
// now TWO doors into it.
//
// 🔴 A SECOND COPY OF THIS WOULD BE A SECOND PLACE TO FORGET THE REVOKE-BEFORE-MINT
// ORDERING, AND THE COPY THAT FORGOT WOULD BE THE ONE ON THE NEWER DOOR. The fixation guard,
// the store write and the cookie are four lines each and every one of them is load-bearing;
// `handleSignIn` and `handleOAuthCallback` reach this function with a principal and nothing
// else, so the two doors cannot diverge in what a session IS.
//
// 🔴 AND "A PRINCIPAL AND NOTHING ELSE" IS WHY NEITHER DOOR MAY PASS ONE THAT ARRIVED ON A
// NARROWED CREDENTIAL. The session row carries no authority; `identity.CookieSession`
// resolves the principal's FULL authority on every request. `handleSignIn` refuses a
// narrowed credential before reaching here; `handleOAuthCallback`'s principal comes from a
// provider identity, which has no credential and therefore no narrowing to lose.
//
// 🔴 THE OLD SESSION IS REVOKED BEFORE THE NEW ONE IS MINTED, WHICH IS THE FIXATION GUARD
// AND IS STRONGER THAN "THE ID CHANGES". Session fixation is an attacker planting a session
// id in a victim's browser and waiting for the victim to authenticate it. A fresh id on
// sign-in defeats it for the browser that signs in — but the attacker still HOLDS the planted
// id, and if that id is a live session of their own they keep it. Revoking the presented one
// closes that: whatever session this browser arrived with stops working, for everybody
// holding it, at the moment somebody signs in over it.
//
// ⚠ `via` NAMES THE DOOR AND GOES ONLY TO THE LOG. It is composed by the caller out of
// values the caller already logs — never a value from the request body — because this line
// is the one an operator reads to tell a token sign-in from a provider one, and a refusal
// that reflected caller text into a log is how a log becomes unreadable.
func (s *Server) openSession(w http.ResponseWriter, r *http.Request, principal control.Principal, via, next string) {
	if old, err := r.Cookie(identity.SessionCookieName); err == nil && old.Value != "" {
		if err := s.sessions.Revoke(old.Value); err != nil {
			// A revocation that failed must not be followed by a successful sign-in:
			// the browser would end up holding a NEW session while the OLD one stayed
			// live, which is the fixation hole this call exists to close.
			s.logf("sign-in aborted: the previous session could not be revoked: %v", err)
			s.renderSignIn(w, http.StatusInternalServerError, signInRefused, next)
			return
		}
	}

	id, err := identity.NewSessionID()
	if err != nil {
		s.logf("sign-in aborted: no session id could be generated: %v", err)
		s.renderSignIn(w, http.StatusInternalServerError, signInRefused, next)
		return
	}
	now := s.now()
	rec := identity.Session{
		Digest:    identity.SessionDigest(id),
		Kind:      principal.Kind,
		Principal: principal.ID,
		IssuedAt:  now,
		ExpiresAt: now.Add(s.ttl),
	}
	if err := s.sessions.Create(rec); err != nil {
		// The cookie is NOT set on this path. A browser holding a cookie for a session
		// the store does not have would be refused on every request with no way to tell
		// why, which is indistinguishable from a broken login.
		s.logf("sign-in aborted: the session could not be stored: %v", err)
		s.renderSignIn(w, http.StatusInternalServerError, signInRefused, next)
		return
	}

	http.SetCookie(w, identity.SessionCookie(id, rec.ExpiresAt))
	// The principal's DISPLAY, which is what every audit line in this system carries,
	// and nothing about the credential or the session.
	s.logf("sign-in: a session was opened for %s via %s", principal, via)
	// 303, not 302: the browser must follow it with a GET. A 302 leaves the method
	// up to the client, and a client that re-POSTs to `/` gets the uniform 401.
	//
	// 🔴 `next` ARRIVES VALIDATED, AND THIS FUNCTION DOES NOT VALIDATE IT AGAIN ON PURPOSE.
	// Each door validates where it READS the value — `requestedNext` for the form, `safeNext`
	// on the flight's value in the callback — and each of those is a guard a test can reach
	// and watch go red. A second check here would be one no test could reach, which this
	// repository calls a defect when it is not labelled; a third door must validate where it
	// reads, as these two do.
	http.Redirect(w, r, landingFor(next), http.StatusSeeOther)
}

// handleSignOut revokes the session and clears the cookie, in that order.
//
// 🔴 THE REVOCATION IS THE LOGOUT AND THE COOKIE IS HOUSEKEEPING. If the store write
// fails this answers 500 and does NOT clear the cookie: a browser told to forget a
// credential that is still live would leave the user believing they had signed out while
// the session kept working, which is precisely the client-held-JWT failure this design
// was chosen to avoid.
func (s *Server) handleSignOut(w http.ResponseWriter, r *http.Request, _ identity.Identity) {
	cookie, err := r.Cookie(identity.SessionCookieName)
	if err == nil && cookie.Value != "" {
		if err := s.sessions.Revoke(cookie.Value); err != nil {
			s.logf("sign-out failed: the session could not be revoked: %v", err)
			writePlain(w, http.StatusInternalServerError, "the session could not be revoked")
			return
		}
	}
	http.SetCookie(w, identity.ClearedSessionCookie())
	s.logf("sign-out: a session was revoked")
	http.Redirect(w, r, SignInPath, http.StatusSeeOther)
}

// renderSignIn is the ONE place the sign-in page is rendered, so the button's presence
// cannot differ between the form's own 200 and every refusal that lands here.
//
// ⚠ THIS SENTENCE CARRIED A COUNT ("the five refusals") AND THE COUNT WENT STALE IN THE SAME
// CHANGE THAT WROTE IT — there are now at least ten call sites, across the credential path, the
// provider start row and the callback. The number is not restated: it is `git grep
// 'renderSignIn('` and nothing here can keep it true. What matters is the property, which does
// not move when a caller is added: every one of them renders through this function, so the
// button's presence and the page's shape are decided in ONE place.
//
// 🔴 THE BUTTON IS RENDERED IFF THE PROVIDER DOOR CAN WORK RIGHT NOW, AND THAT IS DERIVED
// FROM THE SERVER RATHER THAN PASSED IN. A boolean parameter here would be a value six call
// sites could get wrong, and the one that got it wrong would render a button whose route
// answers 501 or 503. `providerArmed` folds in BOTH questions — is a provider configured, and
// has its key set ever been fetched — so a page rendered during a provider outage offers the
// door that still works and not the one that does not.
//
// ⚠ `next` ARRIVES VALIDATED, for the reason `openSession` states: every caller passes the
// value its door read through `safeNext`, or "".
func (s *Server) renderSignIn(w http.ResponseWriter, code int, message, next string) {
	var b strings.Builder
	if err := SignInPage(message, s.providerArmed(), next).Render(&b); err != nil {
		writePlain(w, http.StatusInternalServerError, "the page could not be rendered")
		return
	}
	writeHTML(w, code, b.String())
}

// logf is the ONE writer of operational lines, so there is one place to audit against
// the "no secret reaches a log" claim.
func (s *Server) logf(format string, args ...any) {
	fmt.Fprintf(s.log, "cairn-ui: "+format+"\n", args...)
}

// peerState names WHERE a client identity came from, because the two are different
// evidence and a log line that omits which is unreadable after the fact.
//
// 🔴 "trusted-header" MEANS THE PEER WAS IN THE ALLOWLIST AND THE HEADER WAS READ.
// "peer-address" means it was not, so the TCP peer is the identity. An operator
// debugging a lockout needs to know which, because behind a proxy every peer-address
// line is the PROXY and means the allowlist is wrong.
func peerState(trusted bool) string {
	if trusted {
		return "trusted-header"
	}
	return "peer-address"
}
