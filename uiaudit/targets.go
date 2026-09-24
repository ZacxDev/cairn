package main

import (
	"fmt"
	"io"
	"mime"
	"net/http"
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

// notADocument is the THIRD class: a `GET` row a browser walk must not capture as a page,
// mapped to the REASON it is not one.
//
// 🔴 A REASON RATHER THAN A BOOLEAN, BECAUSE "SKIPPED" IS THE THING THAT NEEDS JUSTIFYING.
// A `map[string]bool` here would make the walk log say a row was skipped and nothing about
// why, and the next person to read it cannot tell a deliberate exclusion from a row somebody
// gave up on. The reason is printed with the skip and is what makes
// `captured + skipped == len(ledger)` an accounting rather than an excuse.
//
// 🔴 AND THE ALTERNATIVE — PUTTING THESE IN `plainGET` — IS THE FALSE GREEN THIS HARNESS
// ALREADY SHIPPED ONCE, which is why neither is there:
//
//   - The OAuth CALLBACK is reachable only with a provider `?code=` AND a live single-use
//     flight cookie. Navigated bare it renders a refusal. Capturing that refusal would run
//     axe, the layout script and the digest over an ERROR PAGE and count it as a page —
//     byte-for-byte the defect that produced "62 axe violations across 6 rules" over
//     browser error pages at exit 0. The document-status gate in `browser.go` would now
//     catch it as a failed walk, which is better than a false green and still worse than
//     not navigating a row that cannot answer.
//   - The STYLESHEET is a `text/css` response, not a document. axe, the layout smells and
//     the a11y digest are all meaningless on it, and a screenshot of a stylesheet is noise
//     in a pixel diff that is already advisory. It gets a non-browser check instead — see
//     [StylesheetCheck], which is the one thing about that row a walk can usefully assert.
//
// ⚠ THE TWO KEYS ARE LITERALS RATHER THAN `ui.OAuthCallbackPath` AND `ui.StylesheetPath`
// FOR EXACTLY ONE REASON: those constants do not exist on this branch's base. They arrive
// with the auth change, and a literal here is the second spelling of a route that
// `internal/ui/routes.go` warns about. **Closing condition:** once that change is merged,
// replace both literals with the constants and add the assertion that the ledger contains
// them — which is a compile-time claim the moment the constants exist, and is not expressible
// before. Until then a key that matches no row is inert, which is why writing them early is
// safe rather than speculative.
var notADocument = map[string]string{
	"/static/app.css": "a text/css response and not a document — axe, the layout smells and the " +
		"a11y digest are all meaningless on a stylesheet, and its screenshot is noise in the pixel diff; " +
		"checked over plain HTTP instead",
	"/sign-in/github/callback": "reachable only with a provider ?code= AND a live single-use flight " +
		"cookie, so navigated bare it renders a refusal — capturing that would measure an error page and " +
		"count it as a page",
}

// classesFor is how many of the three sets claim a path, and it exists so that a path in two
// of them is a REFUSAL rather than a silent win for whichever `case` the switch reaches first.
//
// ⚠ AN INVARIANT GUARD, LABELLED. No path has ever been in two sets. It is pinned because the
// sets are hand-written and the failure would be invisible: a row moved from `plainGET` to
// `notADocument` without deleting the first entry keeps being captured, and the reason string
// added alongside it is simply never printed — a declaration that reads as a decision and has
// no effect.
func classesFor(path string) []string {
	var in []string
	if plainGET[path] {
		in = append(in, "plainGET")
	}
	if linkExpanded[path] {
		in = append(in, "linkExpanded")
	}
	if notADocument[path] != "" {
		in = append(in, "notADocument")
	}
	return in
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

		// A path claimed by two classes is a refusal, not a race between `case` arms.
		if in := classesFor(path); len(in) > 1 {
			return nil, nil, fmt.Errorf(
				"ledger row %q is in %d classes (%s); exactly one must claim it, or which one wins is "+
					"whichever `case` this switch reaches first and the losing declaration is inert",
				row, len(in), strings.Join(in, ", "))
		}

		switch {
		case notADocument[path] != "":
			// 🔴 SKIPPED WITH ITS REASON, AND COUNTED. This arm is what keeps
			// `captured + skipped == len(ledger)` true for a row that must not be captured,
			// so the `default` below stays a refusal about rows nobody has classified rather
			// than becoming a catch-all for rows a browser cannot render.
			skipped = append(skipped, row+" (not a document: "+notADocument[path]+")")
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
				"ledger row %q is a GET this walk has not been told how to reach. Add it to `plainGET` "+
					"if the path as written is the page a user sees; to `linkExpanded` if the page "+
					"publishes further targets as links (a %q query parameter, say); or to `notADocument` "+
					"WITH A REASON if a browser must not capture it at all. Left to a default it would be "+
					"captured however it happens to render and still count as a page",
				row, ui.QueryScope)
		}
	}
	return targets, skipped, nil
}

// StylesheetPath is the `notADocument` row that gets a check anyway.
//
// ⚠ A LITERAL FOR THE SAME REASON THE `notADocument` KEY IS — see that comment. It is derived
// from nothing here, so [StylesheetCheck] is a no-op on a ledger that has no such row.
const StylesheetPath = "/static/app.css"

// StylesheetCheck asserts the one thing about a stylesheet route a walk can usefully assert:
// that it answers 200, with `Content-Type: text/css`, and a non-empty body.
//
// 🔴 IT IS PLAIN HTTP, NOT A BROWSER, AND THAT IS THE POINT OF SEPARATING IT. A stylesheet is
// not a document; every browser-side collector this harness runs is meaningless on one. But the
// route is now a REAL BLOCKING SUBRESOURCE of every page, so a 404 or a wrong content-type
// there is a genuine first-party network finding — one that would otherwise show up only as a
// page that renders unstyled, which no assertion in this harness looks at.
//
// 🔴 IT IS GATED ON THE LEDGER, NEVER RUN UNCONDITIONALLY. On a tree whose ledger has no
// stylesheet row the check would be asking about a path the surface does not serve, and its
// failure would be a fact about the harness. `hasRow` is the gate, and it reads the same ledger
// the walk derives from — so the check appears exactly when the route does.
//
// ⚠ AND `Content-Type` IS COMPARED ON ITS MEDIA TYPE, NOT AS A WHOLE STRING. A conforming
// server may append `; charset=utf-8`, so a whole-string comparison would fail on a correct
// response — the guard would be spelled rather than structural, in the direction that refuses
// the honest tree.
func StylesheetCheck(ledger []string, base string) (skipped bool, err error) {
	if !hasRow(ledger, "GET "+StylesheetPath) {
		return true, nil
	}
	resp, err := http.Get(strings.TrimRight(base, "/") + StylesheetPath)
	if err != nil {
		return false, fmt.Errorf("%s: %w", StylesheetPath, err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return false, fmt.Errorf("%s: reading the body: %w", StylesheetPath, readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%s answered %d, not 200 — it is a blocking subresource of every page, "+
			"so every page renders unstyled and no browser-side collector here would say so",
			StylesheetPath, resp.StatusCode)
	}
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if media != "text/css" {
		return false, fmt.Errorf("%s answered Content-Type %q (media type %q), not text/css — a conforming "+
			"browser refuses a stylesheet served under the wrong type, so the page renders unstyled with a 200",
			StylesheetPath, resp.Header.Get("Content-Type"), media)
	}
	if len(body) == 0 {
		return false, fmt.Errorf("%s answered 200 text/css with an EMPTY body, which is a stylesheet that "+
			"styles nothing and is indistinguishable from a working one at the status line", StylesheetPath)
	}
	return false, nil
}

// hasRow answers whether the ledger declares an exact `<METHOD> <path>` row, ignoring the
// classes a row may carry after it.
func hasRow(ledger []string, want string) bool {
	for _, row := range ledger {
		if fields := strings.Fields(row); len(fields) >= 2 && fields[0]+" "+fields[1] == want {
			return true
		}
	}
	return false
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
