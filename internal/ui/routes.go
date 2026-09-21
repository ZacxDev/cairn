package ui

import (
	"net/http"
	"slices"

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

// routes is the dispatch table. It is a package-level var rather than a method so
// that `DeclaredRoutes` derives from the same data the dispatcher uses — there is
// no second copy to disagree with.
//
// 🔴 EVERY ROW HERE IS AUTHENTICATED. `HealthPath` below is the one unauthenticated
// path and it is deliberately NOT a row: it is answered before the chain runs, it
// says nothing but "ok", and it is not in the ledger because it serves no content.
// That mirrors `internal/api`, where the health path is likewise outside
// `DeclaredRoutes()`.
//
// 🔴 ONE ROW, AND THE SECOND ONE WAS DELETED FOR A REASON WORTH KEEPING. This table
// briefly held `GET /` beside `GET /entries`: the first rendered the page with NO
// store read, the second over the scopes the credential may see. The first was a
// measured lie. [Page] renders "No scope is visible to this credential. That is an
// authority answer, not an empty store." whenever it is handed an empty slice — and
// the handler behind `GET /` handed it `nil` without ever calling [Source.Visible],
// so an operator with authority over every scope was told, in a sentence whose whole
// purpose is to distinguish an authority answer from an empty one, that they had
// none. A page that has not asked may not answer. The page that asks is the page the
// browser opens.
var routes = map[routeKey]handler{
	{"GET", "/"}: (*Server).handlePage,
}

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
// strongest available claim is that the ledger and the dispatcher read one map:
// `TestTheRouteLedgerMatchesTheDispatchTable` makes it, and a nix check printing
// the same list from the binary would have restated it rather than added to it.
func DeclaredRoutes() []string {
	out := make([]string, 0, len(routes))
	for key := range routes {
		out = append(out, key.method+" "+key.path)
	}
	slices.Sort(out)
	return out
}
