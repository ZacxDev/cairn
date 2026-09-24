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
// authentication (see [Server.ServeHTTP]); the state-changing rows behind it are
// `POST /sign-out`, `POST /share` and `POST /unshare` — `POST /sign-in` is `classPublic`
// and dispatches ahead of the chain; `handleSignOut` then revokes `sha256(X)` for a
// caller-chosen X, which is nothing, and the two share rows authorise from `id.Auth`
// rather than from the cookie, so a caller who chose their own cookie gains no authority
// by it. ⚠ THAT ENUMERATION READ "the only state-changing row is `POST /sign-out`" until
// the share flow added two. The impact argument is unchanged; the LIST it rests on was
// stale, and the list is the half a reader would have checked. The CSRF property itself is untouched, because it defends against a CROSS-SITE
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

// handleSignInForm renders the way in. It is PUBLIC and it reads nothing.
func (s *Server) handleSignInForm(w http.ResponseWriter, r *http.Request, _ identity.Identity) {
	s.renderSignIn(w, http.StatusOK, "")
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
		s.renderSignIn(w, http.StatusUnauthorized, signInRefused)
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
		s.renderSignIn(w, http.StatusUnauthorized, signInRefused)
		return
	}
	presented := r.PostFormValue(FieldToken)
	principal, _, err := s.credentials.Authenticate(presented)
	if err != nil {
		// No token, no digest, no reason — and no principal, because there is not one.
		// What IS recorded is the client, because a refusal nobody can attribute is a
		// refusal nobody can act on, and this surface is reachable from the internet.
		if s.limiter != nil && s.limiter.RecordFailure(client) {
			s.logf("sign-in refused: %s — LOCKOUT TRIGGERED (client identity %s)",
				client, peerState(trusted))
		} else {
			s.logf("sign-in refused: %s (client identity %s)", client, peerState(trusted))
		}
		s.renderSignIn(w, http.StatusUnauthorized, signInRefused)
		return
	}

	s.openSession(w, r, principal, "credential from "+client+" (client identity "+peerState(trusted)+")")
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
func (s *Server) openSession(w http.ResponseWriter, r *http.Request, principal control.Principal, via string) {
	if old, err := r.Cookie(identity.SessionCookieName); err == nil && old.Value != "" {
		if err := s.sessions.Revoke(old.Value); err != nil {
			// A revocation that failed must not be followed by a successful sign-in:
			// the browser would end up holding a NEW session while the OLD one stayed
			// live, which is the fixation hole this call exists to close.
			s.logf("sign-in aborted: the previous session could not be revoked: %v", err)
			s.renderSignIn(w, http.StatusInternalServerError, signInRefused)
			return
		}
	}

	id, err := identity.NewSessionID()
	if err != nil {
		s.logf("sign-in aborted: no session id could be generated: %v", err)
		s.renderSignIn(w, http.StatusInternalServerError, signInRefused)
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
		s.renderSignIn(w, http.StatusInternalServerError, signInRefused)
		return
	}

	http.SetCookie(w, identity.SessionCookie(id, rec.ExpiresAt))
	// The principal's DISPLAY, which is what every audit line in this system carries,
	// and nothing about the credential or the session.
	s.logf("sign-in: a session was opened for %s via %s", principal, via)
	// 303, not 302: the browser must follow it with a GET. A 302 leaves the method
	// up to the client, and a client that re-POSTs to `/` gets the uniform 401.
	http.Redirect(w, r, RootPath, http.StatusSeeOther)
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
// cannot differ between the form's own 200 and any of the five refusals that land here.
//
// 🔴 THE BUTTON IS RENDERED IFF A PROVIDER IS WIRED, AND THAT IS DERIVED FROM THE SERVER
// RATHER THAN PASSED IN. A boolean parameter here would be a value five call sites could get
// wrong, and the one that got it wrong would render a button whose route answers 501.
func (s *Server) renderSignIn(w http.ResponseWriter, code int, message string) {
	var b strings.Builder
	if err := SignInPage(message, s.oauth != nil).Render(&b); err != nil {
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
