package api

import (
	"net/url"
	"slices"
	"strings"
)

// 🔴 THE ROUTE TABLES, AND THE DISPATCHER READS THEM. Nothing else in this package
// decides whether a path is a route: a route that is not in a table below is not
// dispatched, so a route that is not in the LEDGER cannot exist.
//
// The Python original arrived at this shape after two guards that tried to police
// the router's SPELLING were each defeated by an ordinary refactor while every test
// stayed green — a regex missed an underscore in a head, an AST walk missed
// `head = parts[0]` — so the guard moved from "what does the source look like" to
// "what does the code dispatch from".
//
// 🔴 AND FOR A NON-PYTHON IMPLEMENTATION THAT GUARD HAD TO BE REBUILT. The
// conformance suite reads the ORACLE's two dict literals by AST to prove the corpus
// addresses every declared route; there is no source for that reader to read here,
// and `tests/conformance/README.md` says so under "what this suite cannot see". What
// closes it on this side is `DeclaredRoutes` below, plus the Go test that compares its
// output against `tests/conformance/requests.json` — the same ledger claim, made from
// the same data file, failing when the set GROWS or SHRINKS.

// readArity counts the path components a read route takes INCLUDING the route name
// itself, so `recall/<scope>` is 2 and `snapshot` is 1.
type readRoute struct {
	arity   int
	handler func(*request, []string, url.Values) error
}

// writeRoute is keyed on `(method, head)` RATHER THAN ON THE HEAD ALONE.
// `POST .../bullets` and `PUT .../<ref>` are different operations on the same noun,
// so the METHOD is part of the route identity: keying on the head and branching on
// the verb inside the handler is the shape that lets a PUT reach an append. PATCH
// and DELETE appear in no row, which is how they stay refused — not by a separate
// rejecter they could be re-bound away from.
//
// `arity` counts path components including the head; `tail` is the fixed trailing
// components. The handler receives the components BETWEEN them, so
// `entry/<scope>/<ref>/bullets` hands over `(scope, ref)` and a request that spells
// the tail differently does not dispatch at all.
//
// ⚠ `PUT entry` IS **ONE** ROUTE CARRYING **TWO** OPERATIONS, AND THAT IS NOT THE
// SHAPE THIS COMMENT WARNS ABOUT. The warning is against branching on the METHOD
// inside a handler. Branching on the PRECONDITION is different in kind: RFC 9110 §13
// defines `If-Match` and `If-None-Match` as evaluated by the origin server for one
// target resource, and create-if-absent has no verb of its own in HTTP. Two rows
// would mean two URLs for one noun.
type writeRoute struct {
	arity   int
	tail    []string
	handler func(*request, []string, []byte) error
}

type writeKey struct {
	method string
	head   string
}

// readRoutes and writeRoutes are built in newServer, because every handler is a
// method on the server. The KEYS are declared here as the ledger, so the set is
// readable without reading the wiring.
var readHeads = []string{"recall", "search", "snapshot"}

var writeKeys = []writeKey{
	{"POST", "entry"},
	{"PUT", "entry"},
}

// safeReadMethods are the two methods every read head is reachable by, and the pair
// the ledger's `<METHOD> <head>` vocabulary expands over. It is a list rather than
// two `if`s because the ledger derives from it: adding a third safe method would have
// to appear in the declared set, which is the point.
var safeReadMethods = []string{"GET", "HEAD"}

// DeclaredRoutes is every `<METHOD> <head>` this server dispatches, in sorted order.
//
// 🔴 IT IS DERIVED FROM THE DISPATCH KEYS, NEVER RESTATED. A hand-written list beside
// the tables is a second spelling that goes stale in the direction nobody notices —
// the ledger reporting full coverage over a set that has grown.
//
// ⚠ WHAT IT CANNOT SEE, stated rather than assumed away: a route dispatched from
// anywhere other than these two tables. The handler in this package has no such
// branch today, and `TestEveryAPIPathReachesATable` is what keeps it that way by
// driving a path that matches no row and requiring the no-route answer.
func DeclaredRoutes() []string {
	out := make([]string, 0, len(readHeads)*len(safeReadMethods)+len(writeKeys))
	for _, head := range readHeads {
		for _, method := range safeReadMethods {
			out = append(out, method+" "+head)
		}
	}
	for _, key := range writeKeys {
		out = append(out, key.method+" "+key.head)
	}
	slices.Sort(out)
	return out
}

// pathComponents is the components of an `/api/v1/` path after the prefix, with
// empty segments dropped — so `/api/v1/` yields none and `/api/v1//recall` yields
// one.
func pathComponents(path string) []string {
	if !strings.HasPrefix(path, APIPrefix) {
		return nil
	}
	var out []string
	for _, part := range strings.Split(path[len(APIPrefix):], "/") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
