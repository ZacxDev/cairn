package ui

import (
	"net/http"
	"slices"
	"strings"

	"github.com/ZacxDev/cairn/internal/identity"
)

// 🔴 THE ROUTE LEDGER, AND THE DISPATCHER READS IT — the same shape as
// `api.DeclaredRoutes`, and for the same reason. A path that is not a key below is
// not dispatched, so a route that is not in the ledger cannot exist. A hand-written
// list beside the table is a second spelling that goes stale in the direction
// nobody notices: the ledger reporting full coverage over a set that has grown.
//
// ⚠ THIS IS A DIFFERENT LEDGER FROM THE POD'S, AND NEITHER MOVES THE OTHER.
// `api.DeclaredRoutes()` is the SERVED CONTRACT the conformance corpus replays;
// this one is a browser surface with no corpus. Adding a row here does not touch
// `tests/conformance/requests.json`, and adding one there does not touch this.
type routeKey struct {
	method string
	path   string
}

type handler func(*Server, http.ResponseWriter, *http.Request, identity.Identity)

// routeClass is what a row DECLARES about itself. There are exactly two classes and
// both had to be invented to add the sign-in flow; everything else a route is subject
// to is DERIVED from the method instead — see [Server.ServeHTTP].
//
// 🔴 A CLASS IS A HOLE IN A GUARD BY CONSTRUCTION, WHICH IS WHY THERE ARE ONLY TWO AND
// WHY NEITHER OF THEM IS `csrf`. A per-row opt-in to a security check is a check
// somebody can forget to opt a new row into, and the forgetting is silent. So the CSRF
// token requirement and the same-origin requirement are not classes: they are asked of
// every state-changing request the dispatcher sees, derived from the METHOD, and a new
// row inherits them without anybody remembering. What a class can only do is make a
// route LESS protected, so each one is spelled into the hand-written ledger in
// `routes_test.go` and adding a row means writing its class out by hand.
type routeClass uint8

const (
	// classPublic is dispatched BEFORE the authentication chain runs. Exactly the
	// sign-in pair carries it, because a surface whose only way in is behind its own
	// authentication has no way in.
	classPublic routeClass = 1 << iota
	// classContent renders an answer about the caller's authority, so it MUST consult AN
	// authority before rendering — which one is per route, declared in
	// `contentAuthority` in `routes_test.go`, and a content route missing from that map
	// fails. ⚠ This said "MUST consult `Source.Visible`" while the share flow's content
	// route consults `Sharing` instead: a class doc naming one authority, three lines from
	// where somebody adds the next row.
	classContent
)

type route struct {
	handle handler
	class  routeClass
}

// routes is the dispatch table. It is a package-level var rather than a method so
// that `DeclaredRoutes` derives from the same data the dispatcher uses — there is
// no second copy to disagree with.
//
// 🔴 `HealthPath` IS THE ONE PATH THAT IS NOT A ROW. It is answered before the chain
// runs, it says nothing but "ok", and it is not in the ledger because it serves no
// content. That mirrors `internal/api`, where the health path is likewise outside
// `DeclaredRoutes()`.
//
// 🔴 ONE ROW WAS DELETED HERE FOR A REASON WORTH KEEPING. This table briefly held
// `GET /` beside `GET /entries`: the first rendered the page with NO store read, the
// second over the scopes the credential may see. The first was a measured lie. [Page]
// renders "No scope is visible to this credential. That is an authority answer, not an
// empty store." whenever it is handed an empty slice — and the handler behind `GET /`
// handed it `nil` without ever calling [Source.Visible], so an operator with authority
// over every scope was told, in a sentence whose whole purpose is to distinguish an
// authority answer from an empty one, that they had none. A page that has not asked may
// not answer.
//
// 🔴 AND THE SIGN-IN PAIR IS PUBLIC, WHICH NARROWS A PROPERTY PHASE A HAD. Before this
// change every path that was not `/healthz` answered the same uniform 401, so an
// unauthenticated caller could not tell a route from a typo. `GET /sign-in` answers 200
// to anybody, so the URL space is now mappable TO THE EXTENT OF THE PUBLIC ROWS — two
// paths, both of which a sign-in flow has to advertise anyway. The property still holds
// in full for every authenticated row, and `TestEveryServedPathComesFromTheLedger`
// probes non-public paths for exactly that reason. This is a stated narrowing, not an
// accident: a sign-in page nobody can reach is not a sign-in page.
// 🔴 AND THE SHARE FLOW IS THREE ROWS ON FIXED PATHS, WITH THE SCOPE IN A QUERY
// PARAMETER RATHER THAN IN THE PATH. `routes` is an EXACT-MATCH map, so a path
// parameter (`/share/{scope}`) would mean a prefix match in the dispatcher — and a
// prefix match is a second way for a request to reach a handler, one that
// `TestEveryServedPathComesFromTheLedger` structurally cannot probe, because there is
// no longer a finite set of paths to probe. The query parameter keeps every served
// path a literal key in this map, which is the property the whole ledger rests on.
var routes = map[routeKey]route{
	{"GET", "/"}:          {(*Server).handlePage, classContent},
	{"GET", "/share"}:     {(*Server).handleSharePage, classContent},
	{"POST", "/share"}:    {(*Server).handleShare, 0},
	{"POST", "/unshare"}:  {(*Server).handleUnshare, 0},
	{"GET", "/sign-in"}:   {(*Server).handleSignInForm, classPublic},
	{"POST", "/sign-in"}:  {(*Server).handleSignIn, classPublic},
	{"POST", "/sign-out"}: {(*Server).handleSignOut, 0},
}

// SignInPath and SignOutPath are spelled once and read by the dispatcher, by the
// renderer's form actions and by the redirects. A form posting to a literal string
// would be a second spelling of a route, which is how a form outlives its handler.
const (
	SignInPath  = "/sign-in"
	SignOutPath = "/sign-out"
	RootPath    = "/"
	// SharePath answers the share flow's read AND its grant write, split by method.
	SharePath = "/share"
	// UnsharePath is a SEPARATE path rather than an action field on `SharePath`,
	// deliberately. A hidden `action=revoke` would make the difference between
	// granting and revoking a value inside a form body — something chosen by whoever
	// gets one request past both cross-site gates, rather than something the route
	// decides. Two paths make the two writes two rows in the ledger, which is where
	// somebody reads them.
	UnsharePath = "/unshare"
)

// QueryScope is the one query parameter this surface reads.
const QueryScope = "scope"

// The share flow's form fields, spelled once for the renderer and the handlers.
//
// ⚠ `FieldVerb` IS SINGULAR AND REPEATS. A checkbox group posts one name many times,
// and `r.PostForm[FieldVerb]` is the only read that sees all of them:
// `PostFormValue` returns the FIRST, which would silently narrow every multi-verb
// grant to whichever box the browser happened to serialise first.
const (
	FieldScope   = "scope"
	FieldSubject = "subject"
	FieldVerb    = "verb"
	FieldGrant   = "grant"
)

// HealthPath is the readiness probe: before authentication, before rate limiting,
// and outside the ledger. A readiness probe broken by a security guard is how the
// guard gets deleted.
const HealthPath = "/healthz"

const healthBody = "ok"

// DeclaredRoutes is every `<METHOD> <path>` this server dispatches, sorted.
//
// ⚠ WHAT IT CANNOT SEE, stated rather than assumed away: a route dispatched from
// anywhere other than `routes`. `TestEveryServedPathComesFromTheLedger` is what
// keeps that true — it drives a path in no row and requires the no-route answer.
//
// ⚠ AND IT IS READ IN ONE PLACE PLUS A LOG LINE, WHICH IS WEAKER THAN THE POD'S
// EQUIVALENT ON PURPOSE. `api.DeclaredRoutes()` is checked against an external
// corpus and read back out of the running binary because the corpus builder is
// Python and cannot see a compiled program. This surface has no corpus, so the
// strongest available claim is that the ledger and the dispatcher read one map.
func DeclaredRoutes() []string {
	out := make([]string, 0, len(routes))
	for key := range routes {
		out = append(out, key.method+" "+key.path)
	}
	slices.Sort(out)
	return out
}

// DeclaredRouteLedger is [DeclaredRoutes] with each row's CLASS spelled out, and it is
// the one the hand-written expectation is compared against.
//
// 🔴 TWO VIEWS OF ONE MAP, NEVER TWO COPIES, AND THE TEST PINS THEM TO EACH OTHER. A
// classed ledger is what makes "this row is public" a thing somebody has to write down
// when they add the row; a plain one is what `cmd/cairn-ui` counts at startup. They
// derive from the same `routes` map, and `TestTheRouteLedgerMatchesTheDispatchTable`
// asserts that stripping the classes off this one reproduces the other exactly — so the
// two cannot drift even though there are two functions.
func DeclaredRouteLedger() []string {
	out := make([]string, 0, len(routes))
	for key, r := range routes {
		line := key.method + " " + key.path
		if classes := classNames(r.class); len(classes) > 0 {
			line += " " + strings.Join(classes, ",")
		}
		out = append(out, line)
	}
	slices.Sort(out)
	return out
}

// classNames renders a class set, sorted by the constants' own order so the ledger's
// spelling cannot depend on anything but the bits.
func classNames(c routeClass) []string {
	var out []string
	if c&classPublic != 0 {
		out = append(out, "public")
	}
	if c&classContent != 0 {
		out = append(out, "content")
	}
	return out
}
