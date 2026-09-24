package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/ZacxDev/cairn/internal/ui"
)

// 🔴 THE WALK IS DERIVED FROM `ui.DeclaredRouteLedger()` AND FROM NOTHING ELSE, AND A
// HARDCODED PATH LIST WOULD ALREADY BE STALE. Two changes are in flight that each add
// rows to that ledger. A list written here would under-cover them silently — the worst
// direction, because a walk that captures fewer pages than the surface serves still
// reports a green run over everything it happened to look at.
//
// It is derived at COMPILE time rather than by parsing a log line, because a nested
// module under `github.com/ZacxDev/cairn/` may import `internal/ui` (Go's internal rule
// is about the import path's prefix, not about the module boundary). So a row added to
// `internal/ui/routes.go` reaches this program without anybody editing this file, and a
// row REMOVED stops appearing here for the same reason.

// Viewport is a capture width. The two values are the hub's own two, spelled as its
// wire strings so the push cannot disagree with the server's closed set.
type Viewport struct {
	Name          string // The hub's wire value: "mobile" | "desktop"
	Width, Height int
}

// The two widths. 390 is the phone width this surface has never been rendered at; 1440
// is the desktop one the hub's native crawl uses.
var (
	Mobile  = Viewport{Name: "mobile", Width: 390, Height: 844}
	Desktop = Viewport{Name: "desktop", Width: 1440, Height: 900}
)

// Viewports is the capture matrix, in a fixed order so a push's page order is stable
// across runs — the hub matches P2 diff pages on `url`+`viewport`, and a stable order
// keeps a run's report readable beside the previous one.
var Viewports = []Viewport{Mobile, Desktop}

// Target is one page to capture: a path to navigate and whether it is captured with a
// session or without one.
type Target struct {
	// Path is what the browser navigates, query parameter included.
	Path string
	// PushURL is the STABLE identity the hub matches diffs on. It is the same string
	// as Path deliberately: a label that varied run to run (a port, a temp dir) would
	// make every page "new" on every push and the diff would never say anything.
	PushURL string
	// SignedIn says which state this page is captured in. It is DERIVED from the
	// ledger's class, never chosen per path.
	SignedIn bool
	// LedgerRow is the ledger line this target came from, carried so the walk log can
	// attribute every capture to a row and so the accounting below can be checked.
	LedgerRow string
	// ExpandLinks says this page publishes further targets as links — see [ExpandLinks].
	ExpandLinks bool
}

// linkExpanded is the set of ledger PATHS whose page publishes further targets as LINKS,
// and whose query parameter must therefore never be guessed.
//
// 🔴 THE PARAMETER IS A `control.ID`, NOT A NAME, AND GUESSING IT PRODUCED A WALK OVER
// TWELVE 404 PAGES THAT REPORTED SUCCESS. Measured on this tree, not imagined: the first
// draft of this file expanded `GET /share` into one target per scope NAME read off the store
// (`/share?scope=alpha-notes`). `handleSharePage` reads that parameter as a
// `control.ID` — a `crypto/rand` value, unguessable by construction and deliberately so,
// because a 404-for-unknown beside a 403-for-somebody-else's would make the page an
// existence oracle over every scope in the deployment. So every one of those targets 404'd,
// and the run reported "62 axe violations across 6 rules" over a browser's own error page:
// `document-title`, `html-has-lang`, `landmark-one-main`, `page-has-heading-one`, `region`,
// none of which is a fact about cairn. THAT is what under-coverage looks like from the
// inside — not a missing page, a present one that is the wrong page.
//
// The fix is not a better guess. It is to read the values the surface PUBLISHES:
// `GET /share` with no parameter renders the index, the index renders one link per
// administrable scope, and [ExpandLinks] turns those hrefs into targets. A deployment with
// more administrable scopes is covered without anybody editing this file, and a walk can no
// longer address a page the surface never offered.
//
// ⚠ AND ON THE TOKEN-FILE DEPLOYMENT THE INDEX PUBLISHES NOTHING, WHICH IS CORRECT RATHER
// THAN A GAP. `tokenfile.Source` grants no `admin` verb, so the page renders "No scope is
// administrable by this credential" — the state `internal/ui/README.md` describes when
// `cairn-ui` runs against a token file rather than a control journal. The walk therefore
// captures the index and no per-scope page, and says so. Reaching the per-scope page needs a
// journal-backed world, which is named in `README.md` as a declared gap.
var linkExpanded = map[string]bool{
	ui.SharePath: true,
}

// plainGET is the set of ledger paths captured exactly as the ledger spells them.
var plainGET = map[string]bool{
	ui.RootPath:   true,
	ui.SignInPath: true,
}

// Targets derives the walk's BASE targets from the route ledger.
//
// The accounting it returns is load-bearing, not decoration: `captured + skipped` must
// equal the ledger's length, and the caller prints all three. A ledger that grew by a row
// nobody handled shows up as a refusal with the row's own spelling in the message.
func Targets(ledger []string) (targets []Target, skipped []string, err error) {
	for _, row := range ledger {
		fields := strings.Fields(row)
		if len(fields) < 2 {
			return nil, nil, fmt.Errorf("ledger row %q is not %q", row, "<METHOD> <path> [classes]")
		}
		method, path := fields[0], fields[1]
		classes := map[string]bool{}
		if len(fields) > 2 {
			for _, c := range strings.Split(fields[2], ",") {
				classes[c] = true
			}
		}

		// 🔴 SKIPPED BY METHOD, NEVER BY PATH. A state-changing row is reached by
		// CLICKING a form on a captured page, which is how the sign-in POST is
		// exercised — navigating to it directly would be a request with no `Origin`,
		// and `sameOrigin` refuses exactly that. So the walk never navigates a non-GET
		// row, and the rows it declines are listed rather than dropped.
		if method != "GET" {
			skipped = append(skipped, row+" (not GET: reached by submitting a form, never navigated)")
			continue
		}

		// 🔴 DERIVED FROM THE CLASS, NEVER FROM THE PATH. `classPublic` is the only
		// thing that says "this row renders before the authentication chain", so it is
		// the only thing that may decide which state a page is captured in. A path
		// spelling ("anything starting /sign-") would be a guard on a WORD that a new
		// row can walk past by being named differently.
		signedIn := !classes["public"]

		switch {
		case linkExpanded[path], plainGET[path]:
			// Both classes are navigated bare. The difference is only that a
			// link-expanded page's own hrefs become further targets, which [ExpandLinks]
			// discovers after this page has been rendered — it cannot be known here.
			targets = append(targets, Target{
				Path: path, PushURL: path, SignedIn: signedIn, LedgerRow: row,
				ExpandLinks: linkExpanded[path],
			})
		default:
			// The refusal the comment on `linkExpanded` exists for.
			return nil, nil, fmt.Errorf(
				"ledger row %q is a GET this walk has not been told how to reach: add it to `plainGET` "+
					"if the path as written is the page a user sees, or to `linkExpanded` if the page "+
					"publishes further targets as links (a %q query parameter, say). Left to a default it "+
					"would be captured however it happens to render and still count as a page",
				row, ui.QueryScope)
		}
	}
	return targets, skipped, nil
}

// ExpandLinks turns the hrefs a link-expanded page rendered into further targets.
//
// 🔴 ONLY SAME-PATH, SAME-ORIGIN, RELATIVE HREFS, AND THE NARROWING IS WHAT KEEPS THE WALK
// FROM BECOMING A CRAWLER. `internal/ui/render.go`'s `safeHref` allowlists the schemes that
// may reach an href, so an entry's `ref` can legitimately render an external link — and a
// harness that followed one would be sending somebody else's site through the hub. So an
// expansion is accepted only when it is a relative href whose PATH equals the page it came
// from: the share index's per-scope links, and nothing else. Anything else is returned as a
// declined href so the log says what it saw rather than dropping it.
func ExpandLinks(from Target, hrefs []string) (targets []Target, declined []string) {
	seen := map[string]bool{}
	for _, href := range hrefs {
		u, err := url.Parse(href)
		if err != nil || u.IsAbs() || u.Host != "" || u.Path != from.Path || u.RawQuery == "" {
			declined = append(declined, href)
			continue
		}
		p := u.Path + "?" + u.RawQuery
		if seen[p] {
			continue
		}
		seen[p] = true
		targets = append(targets, Target{
			Path: p, PushURL: p, SignedIn: from.SignedIn,
			LedgerRow: from.LedgerRow + " (link from " + from.Path + ")",
		})
	}
	// Sorted so a push's page order does not depend on render order.
	sort.Slice(targets, func(i, j int) bool { return targets[i].Path < targets[j].Path })
	return targets, declined
}

// LedgerAccounting is the check the caller prints and the walk refuses on.
//
// ⚠ IT IS AN INVARIANT GUARD, LABELLED AS ONE. No bug in this repository has ever
// violated it; it exists so that a FUTURE row added to the ledger and to neither set
// above cannot be absorbed silently. Counting it as regression coverage would be a claim
// about a defect that never happened.
func LedgerAccounting(ledger []string, targets []Target, skipped []string) error {
	rows := map[string]bool{}
	for _, t := range targets {
		rows[t.LedgerRow] = true
	}
	handled := len(rows) + len(skipped)
	if handled != len(ledger) {
		return fmt.Errorf("the ledger has %d row(s); the walk accounted for %d (%d captured row(s) + %d skipped)",
			len(ledger), handled, len(rows), len(skipped))
	}
	return nil
}
