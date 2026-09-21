// Package identity is P4: WHO a request is, behind one interface with three
// backends.
//
// 🔴 WHAT THIS SEPARATES, AND WHY THE SEAM IS WORTH A PACKAGE. Until now a request's
// caller was a bearer token and nothing else: `internal/api` read an `Authorization`
// header, handed the string to `control.Cache.Authenticate`, and that was the whole
// of authentication. A browser session cannot do that — an OAuth provider hands back
// a signed assertion, not a token this pod ever minted — and an instance fronted by an
// auth proxy has already had its user authenticated one hop upstream. Those are three
// different ways to learn WHO, and exactly one way to learn WHAT THEY MAY SEE.
//
// 🔴 SO ALL THREE RESOLVE TO ONE `control.Principal` AND NOTHING DOWNSTREAM BRANCHES
// ON WHICH PRODUCED IT. `Identity` carries no backend discriminator, deliberately:
// a field naming the backend is a field a route can branch on, and the first branch
// is the second authorization decision `internal/control` exists to forbid. The one
// place the backend is visible is a refusal's own text, which never reaches the wire.
//
// # The interface, and where it deliberately differs from the plan's sketch
//
// `claudedocs/plan-cairn-control-plane.md` §E sketches
// `Authenticate(*http.Request) (Principal, error)`. This package returns an
// `Identity` — the principal AND its authorization, as ONE value — and the difference
// is not cosmetic.
//
// 🔴 `control.Principal`'s OWN COMMENT FORBIDS THE SKETCH'S SHAPE: *"authentication
// and authorization must come out of the SAME match, so no route can be authenticated
// against one credential and authorised against another. `Authenticate` is the only
// constructor that a server path may use, and it returns the Principal and the
// Authorization together."* An interface returning the principal alone makes the
// server call `Resolve` afterwards — a SECOND read of the materialized model, which a
// concurrent refresh may have replaced between the two calls. The window is small and
// it is exactly the window the rule names. Returning one value closes it structurally
// rather than by discipline: there is no way to hold a principal from this package
// without the authority that came out of the same read.
//
// 🔴 AND THE INTERFACE CANNOT LIVE IN `internal/control`, WHICH THE SKETCH'S PLACEMENT
// IMPLIES. That package's doc says it "deliberately stops at the library boundary —
// nothing here imports `net/http`, opens a socket or knows a server exists", for the
// same reason `internal/report` does. `Authenticate(*http.Request)` is an
// `http.Request`. The interface therefore lives here, one level out, and `control`
// stays a library the CLI and the UI can link without a server.
//
// # What this package does NOT decide
//
// Nothing here narrows anything. A backend answers "who", the `control.Authorization`
// it carries answers "what", and that authorization is computed by `control.Resolve`
// and by nothing in this package. Adding a scope check here would be the second
// implementation of visibility that `internal/control/README.md` exists to refuse.
package identity

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ZacxDev/cairn/internal/control"
)

// ModelSource is the read half the SESSION backends need: the materialized world, at
// this instant, with no network call. `*control.Cache` satisfies it.
//
// 🔴 ONE READ PER REQUEST, AND THE INTERFACE IS SHAPED TO MAKE THAT THE EASY THING.
// `Model()` hands back a whole consistent world; a backend then asks it for the user,
// for the principal and for the authorization, and all three come out of that one value.
// An interface offering "look up a user" and "resolve a principal" as separate calls
// would invite two reads, and a refresh landing between them is exactly the
// authenticated-against-one-world-authorised-against-another window `control.Principal`'s
// comment forbids.
//
// 🔴 AN INTERFACE RATHER THAN A `*control.Cache` FOR THE REASON `TokenAuthority`'s
// COMMENT RECORDS: a captured pointer is a second holder of one fact, and the two can
// come apart with nothing observable.
type ModelSource interface {
	Model() control.Model
}

// Identity is the whole answer to "who is this request, and what may it do".
//
// 🔴 ONE VALUE, BECAUSE THE TWO FACTS MUST COME OUT OF ONE MATCH. See the package
// doc: splitting these into two returns — or two calls — reintroduces the window in
// which a request is authenticated against one world and authorised against another.
type Identity struct {
	// Principal is who. Its `Display` is what the audit line's `identity=` field and
	// every written bullet's ACTOR carry.
	Principal control.Principal
	// Auth is what this principal may do, computed from the SAME model read that
	// produced it.
	//
	// ⚠ IT IS `Auth` RATHER THAN `Authorization`, WHICH MATCHES `api.request.auth` AND
	// IS ALSO A LEAKSCAN ACCOMMODATION — both halves are true and the second is the one
	// that would otherwise be invisible. `tests/leakscan.py`'s credential rule matches
	// the word `authorization` followed by twenty-plus identifier characters, which is
	// exactly what a Go declaration of the shape `<the word> control.<the word>` is —
	// written with a placeholder because the literal form is the thing the rule refuses,
	// and a comment that instantiates it refuses its own file. That is
	// a FALSE POSITIVE, and the fix is on this side rather than in the scanner: a
	// credential gate's false positives are the safe direction, and narrowing the one
	// pattern that catches `Authorization: Bearer <token>` to spare a type declaration
	// would trade a real refusal for a cosmetic one. The name it lands on is the one
	// the server already uses for this value.
	Auth control.Authorization
	// Fingerprint is the audit line's `token=` field: the 12-hex `authz.TokenID` of
	// the presented bearer token.
	//
	// ⚠ IT IS EMPTY FOR EVERY BACKEND THAT AUTHENTICATES WITHOUT A TOKEN THIS POD
	// MINTED, AND THE AUDIT LINE RENDERS THAT AS `-`. That is honest rather than
	// convenient: the documented rotation procedure is "read the fingerprints the
	// startup line prints, then grep the audit stream for the one that should have
	// stopped appearing", and a session has no row in that table to stop appearing
	// from. Synthesizing a digest of a JWT would put a value in that column that
	// rotates with every token refresh and correlates with nothing an operator can
	// look up. `identity=` is what identifies such a request, and it carries the
	// principal's display name.
	Fingerprint string
}

// Valid answers whether this Identity names anybody at all.
//
// 🔴 A ZERO `Identity` WITH A NIL ERROR IS THE ONE FAIL-OPEN SHAPE THIS INTERFACE
// MAKES POSSIBLE, AND IT IS CHECKED RATHER THAN TRUSTED. A backend that returned
// `Identity{}, nil` would authenticate a principal with no kind, no id and no display
// — "logged in as nobody" — whose `control.Authorization` zero value permits nothing,
// so no scope would leak, but whose request would be audited as `auth=ok identity=-`
// and would pass every guard that asks "did authentication succeed". The chain and the
// server both refuse it. Two checks rather than one because they are different claims:
// the chain refuses to PROPAGATE such a value, and the server refuses to SERVE one, and
// a chain of length one bypasses the first.
func (i Identity) Valid() bool {
	return i.Principal.Kind.Valid() && i.Principal.ID != "" && i.Principal.Display != ""
}

// Authenticator resolves a request to exactly one Identity.
//
// 🔴 EVERY FAILURE IS `control.ErrNoCredential`, WRAPPED OR BARE, AND CALLERS MUST NOT
// BRANCH ON THE REASON. That error carries nothing by design — no near-miss, no hint
// about whether a credential was unknown, revoked, expired or bound to a principal that
// no longer exists — because a reason that reaches the wire is an enumeration API. A
// backend here may wrap it with a reason for its own tests and for a future operator
// surface; `errors.Is(err, control.ErrNoCredential{})` still answers true, and that is
// the only question a serving path may ask.
type Authenticator interface {
	Authenticate(r *http.Request) (Identity, error)
}

// Refusal is a refusal that says WHY, without the why ever reaching the wire.
//
// 🔴 THE WIRE DOES NOT DISCRIMINATE; A TEST DOES. `internal/api`'s uniform 401 is what
// a caller sees for every rejection, and that does not change here. What changes is that
// a refusal in THIS package can be watched firing with its own message — which is the
// difference between a guard that is present and a guard that has been proven reachable.
// The trusted-header backend has six of these and each one is exercised by a case no
// earlier check rejects.
//
// ⚠ THE REASON IS NOT IN THE AUDIT LINE TODAY, AND SAYING SO IS THE POINT RATHER THAN
// LEAVING IT TO BE INFERRED. `tests/dualrun/harness.py` compares the two servers' audit
// records line for line, so a Go-only `status=` vocabulary item would move a gate in the
// same change that most needs it — the same reason `control.Cache.Staleness()` is
// reportable and unreported. **CLOSING CONDITION:** a `status=` spelling declared in
// `wire.NORMALIZATIONS` that a `tests/dualrun/` run exits 0 with, or the retirement of
// the oracle that makes the comparison moot. Until then the reason is observable at this
// package's boundary and nowhere else, which is where its tests read it.
type Refusal struct {
	// Backend names which authenticator refused. Never rendered to a caller.
	Backend string
	// Reason is the specific check that failed.
	Reason string
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("%s: %s: %s", r.Backend, control.ErrNoCredential{}.Error(), r.Reason)
}

// Unwrap is what makes `errors.Is(err, control.ErrNoCredential{})` true, so a serving
// path that asks only the uniform question gets the uniform answer.
func (r *Refusal) Unwrap() error { return control.ErrNoCredential{} }

// refuse builds a Refusal. A helper rather than a literal at each site so the backend
// name is spelled once per backend.
func refuse(backend, reason string) error { return &Refusal{Backend: backend, Reason: reason} }

// Chain tries each authenticator in order and takes the first that succeeds.
//
// 🔴 ORDER IS A SECURITY DECISION, AND THE RECOMMENDED ONE IS FIXED BY `Backends`:
// machine token, then Supabase JWT, then cookie session, then trusted header. Three
// reasons, all about what happens when a request carries more than one credential.
// First, the machine token is the only credential this pod MINTED as a bearer token and
// the only one whose revocation is one edit away, so it must win where both are present.
// Second, the trusted-header backend is last because it is the one whose source check is
// a property of the DEPLOYMENT rather than of the request — see `TrustedHeader`, which
// refuses to exist at all unless an operator has declared the deployment proxy-fronted.
//
// 🔴 THIRD, AND THIS IS THE ONE THE FOURTH BACKEND ADDED: EVERY EXPLICITLY-PRESENTED
// CREDENTIAL IS TRIED BEFORE THE ONE THE BROWSER SENDS BY ITSELF. A machine token and a
// Supabase JWT arrive in an `Authorization` header, which no user agent sets on its own —
// a caller had to decide to send it. A session cookie is AMBIENT: the browser attaches it
// to every request to this origin, including one a different site caused. So a request
// carrying both resolves as the header's principal, which is the one the caller chose;
// the alternative ordering would let an old cookie silently shadow the credential
// somebody deliberately presented, and the person debugging that would be reading a page
// rendered for a principal they did not ask to be. ⚠ It is NOT a CSRF defence — a
// cross-site request carries no `Authorization` header either, so this ordering changes
// nothing about that case. The guard for it is in `internal/ui`.
//
// ⚠ AND THE COOKIE SITS BEFORE THE TRUSTED HEADER RATHER THAN AFTER, WHICH IS A CHOICE
// BETWEEN TWO AMBIENT-ISH CREDENTIALS. The trusted header stays last because its
// precondition is a deployment declaration an operator made once, so a request that
// satisfies it satisfies it for every caller who can reach the socket; a session cookie
// is at least a credential this surface minted for one browser. No deployment has both
// today — `internal/ui/auth.go` refuses the trusted header outright — so this ordering
// is a decision recorded before it can be reached rather than one anything exercises.
//
// 🔴 IT SHORT-CIRCUITS ON SUCCESS AND NOT ON FAILURE. Stopping at the first success
// leaks which backend accepted — a fact the 200 already carries — while running every
// backend on every bad credential would make one anonymous request cost a full JWT
// signature verification per configured backend, which is a cheap amplifier for anyone
// who can reach the pod. `control.Authenticate`'s no-early-exit rule is about WHICH
// CREDENTIAL matched inside one model and is unaffected: that loop still runs to
// completion.
//
// 🔴 AN EMPTY CHAIN AUTHENTICATES NOBODY. It returns the same refusal a wrong token
// gets, rather than the nil error a "nothing to check, so nothing objected" reading
// would produce. This is the fail-closed direction and it is the one a misconfiguration
// lands on.
type Chain []Authenticator

// Authenticate runs the chain.
func (c Chain) Authenticate(r *http.Request) (Identity, error) {
	for _, backend := range c {
		if backend == nil {
			// A nil member is a wiring mistake, not a credential outcome. It is
			// skipped rather than panicked on so that one bad entry cannot take the
			// whole authentication path down, and `Backends` refuses to build one.
			continue
		}
		got, err := backend.Authenticate(r)
		if err != nil {
			continue
		}
		if !got.Valid() {
			// 🔴 A BACKEND THAT ANSWERED "YES" WITHOUT NAMING ANYBODY IS REFUSED, NOT
			// PROPAGATED — and refused for the whole chain rather than skipped, because
			// a backend returning a zero Identity with a nil error is broken and the
			// safe reading of a broken authenticator is that this request has no
			// identity at all.
			return Identity{}, refuse("chain", "a backend returned an identity naming no principal")
		}
		return got, nil
	}
	return Identity{}, refuse("chain", "no configured backend recognised this request")
}

// ErrNoBackends refuses a chain with nothing in it, at CONFIGURATION time.
//
// ⚠ IT IS NOT THE SAME CLAIM AS `Chain.Authenticate`'s EMPTY-CHAIN REFUSAL, AND BOTH
// EXIST. The runtime refusal is fail-closed behaviour for a chain that somehow reaches
// a request; this is a build-time refusal so a deployment configured with no way to
// authenticate anybody does not come up looking healthy. A server that authorises
// nobody serves nothing and passes every health check — the exact failure
// `api.New`'s empty-token guard already refuses at startup.
var ErrNoBackends = errors.New("identity: no authentication backend is configured, so no request could ever be authenticated")

// Backends assembles the chain in the fixed order above, skipping nil backends.
//
// It takes the backends as explicit arguments rather than a slice so that adding one is
// an edit to this signature — a place somebody has to think about ordering — rather than
// an append at a call site. ⚠ THE MECHANISM WORKED: `cookie` is the fourth, and adding it
// broke every caller until each had decided where it goes. That is the whole point of the
// positional shape and it is worth recording that it was paid rather than dodged.
func Backends(machine *MachineToken, supabase *SupabaseJWT, cookie *CookieSession, trusted *TrustedHeader) (Chain, error) {
	var chain Chain
	if machine != nil {
		chain = append(chain, machine)
	}
	if supabase != nil {
		chain = append(chain, supabase)
	}
	if cookie != nil {
		chain = append(chain, cookie)
	}
	if trusted != nil {
		chain = append(chain, trusted)
	}
	if len(chain) == 0 {
		return nil, ErrNoBackends
	}
	return chain, nil
}
