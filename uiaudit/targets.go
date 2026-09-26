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

// 🔴 THE TWO VALUES COME FROM THE CONSUMER, NOT FROM A PREFERENCE HERE, AND THAT IS WHY THEY ARE
// THESE TWO. The upstream hub's own native crawl captures at 390 and 1440, and it matches its P2
// diff on `url`+`viewport` — so a producer capturing at any other pair would be diffed against
// pages rendered at widths its own screenshots were never taken at. Verified against a real ingested
// run: the service stored `width=390` on the three mobile rows and `width=1440` on the three desktop
// rows, i.e. it records the width it derives from the viewport NAME rather than anything this
// harness sends. Changing either number silently changes what the diff compares.
//
// ⚠ THE COUNT SATISFIES THIS REPOSITORY'S TWO-POINTS RULE; THE VALUES DO NOT COME FROM IT. Two
// widths is "measure at ≥2 points"; WHICH two is the consumer's contract. Both facts, because
// either alone reads as arbitrary.
//
// 390 is the phone width this surface has never been rendered at; 1440
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
//     in a pixel diff that is already advisory. It gets NO check here either — `internal/ui`'s
//     own `TestTheStylesheetIsServedAsItsOwnRoute` already asserts far more about it than this
//     package could, as a build refusal rather than an advisory tick.
//
// ✅ THE KEYS ARE THE CONSTANTS, AND THE CLOSING CONDITION THAT GOT THEM THERE IS DISCHARGED.
// They were string literals while the auth change was unmerged, because the constants did not
// exist on this branch's base — and a literal is the second spelling of a route that
// `internal/ui/routes.go` warns about. The stated condition was "once that change is merged,
// replace both literals with the constants and add the assertion that the ledger contains
// them". It is merged; both are done. `TestTheLedgerCARRIESEveryNotADocumentRow` is that
// assertion, and it is a compile-time claim now in a way it could not have been before.
//
// ⚠ THE STYLESHEET IS TWO ROWS NOW, AND BOTH KEYS ARE REQUIRED RATHER THAN ONE COVERING BOTH.
// `internal/ui` serves the stylesheet at a CONTENT-HASHED path — the one every page links, whose
// spelling changes with the theme — and keeps the unversioned path so URLs already loose in the
// world do not 404. Two ledger rows, so two keys: a row in none of the three classes is a
// refusal by construction, which is how this map found out about the second one. The hashed
// key is `ui.StylesheetHashedPath`, a package VARIABLE rather than a constant, and writing its
// current value down here as a literal would silently stop excluding anything on the next theme
// change — `TestTheLedgerCARRIESEveryNotADocumentRow` is what would catch that, and only
// because it compares against the live ledger.
var notADocument = map[string]string{
	ui.StylesheetPath: "a text/css response and not a document — axe, the layout smells and the " +
		"a11y digest are all meaningless on a stylesheet, and `internal/ui`'s own " +
		"TestTheStylesheetIsServedAsItsOwnRoute already asserts far more about it than a browser walk " +
		"could, as a build refusal rather than an advisory tick. This is the UNVERSIONED row, which no " +
		"page links: it is served so that URLs issued before a deploy keep answering",
	ui.StylesheetHashedPath: "a text/css response and not a document, for every reason the unversioned " +
		"row gives — this is the CONTENT-HASHED row, the one the pages actually link, so a browser walk " +
		"does fetch it as a subresource and `Browser.onEvent` is what watches that fetch succeed",
	ui.OAuthCallbackPath: "reachable only with a provider ?code= AND a live single-use flight " +
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
			// ⚠ THE REASON SAYS "REACHABLE BY A FORM", NOT "EXERCISED". Of the non-GET rows, only the
			// sign-in and sign-out POSTs are actually submitted by this walk; `POST /share`,
			// `POST /unshare` and the OAuth start are reached by NOTHING here — the first two need an
			// `admin` grant the token-file deployment does not issue, and the third would leave the
			// origin. An earlier wording claimed all of them were form-reachable, which read as
			// coverage. What is true of every one of them is that a browser must not NAVIGATE it:
			// `sameOrigin` refuses a state-changing request with no `Origin`, so a direct navigation
			// measures a refusal rather than a page.
			skipped = append(skipped, row+" (not GET: a state-changing row; navigating it directly would "+
				"be refused for want of an Origin, so the walk never does — see README residual 7 for "+
				"which of these are actually submitted)")
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

// 🔴 THERE IS DELIBERATELY NO HTTP CHECK ON THE STYLESHEET ROUTE HERE, AND THE DELETED ONE IS WORTH
// A SENTENCE SO NOBODY ADDS IT BACK. A `StylesheetCheck` stood here asserting 200, `text/css` and a
// non-empty body. It was a STRICT SUBSET of `internal/ui`'s own
// `TestTheStylesheetIsServedAsItsOwnRoute`, which asserts the exact `Content-Type` including
// charset, `X-Content-Type-Options: nosniff`, BYTE-EQUALITY with the stylesheet constant, a size
// floor, that no page carries an inline `<style>`, that every page links the route, and that an
// unauthenticated caller can reach it — as a hard failure in root `go test ./...`, so in the `go`
// job and all three nix derivations. That is a BUILD REFUSAL; this would have been an advisory
// tick in a `continue-on-error` job. `doc.go` states the rule it broke: a second spelling of a
// gate that already exists.
//
// ⚠ AND ITS JUSTIFYING COMMENT WAS FALSE WITHIN THIS PACKAGE. It claimed no browser-side
// collector here looks at a failed stylesheet fetch. `Browser.onEvent` records any subresource
// response with `Status >= 400` as a first-party network event — which is precisely the thing a
// network zero is asserted to mean. The walk was already watching.
//
// ⚠ A LOCAL `StylesheetPath` CONST USED TO LIVE HERE AND IS GONE: `ui.StylesheetPath` exists now,
// and two spellings of one route is the thing this file keeps warning about.

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
// 🔴 IT COMPARES SETS, AND THE COUNT COMPARISON IT REPLACES WAS SATISFIABLE WITH A PAGE MISSING. The
// first version asserted `len(capturedRows) + len(skipped) == len(ledger)`, which checks neither
// disjointness nor membership: one row handled by BOTH arms, plus another handled by NEITHER,
// balances exactly and passes. Two errors cancelling is the characteristic failure of counting
// arguments — and the docstring called this "the only thing that notices a ledger that grew", which
// is a claim a count cannot support.
//
// ⚠ INVARIANT-STRENGTH TODAY, and labelled as such: [Targets]'s switch takes exactly one arm per
// row, so no tree has produced a double-handled row. The point is that this check must not DEPEND on
// that — it is the thing that would notice if the switch changed, so deriving its soundness from the
// switch makes it circular.
//
// Three claims, each failing for its own reason: nothing is handled twice; nothing is handled that
// the ledger does not declare; nothing the ledger declares goes unhandled.
func LedgerAccounting(ledger []string, targets []Target, skipped []string) error {
	declared := make(map[string]bool, len(ledger))
	for _, row := range ledger {
		declared[row] = true
	}

	// how[row] records which arm claimed it. Several TARGETS may share one row — the link expansion
	// produces exactly that — so a row is recorded once however many targets carry it.
	how := make(map[string]string, len(ledger))

	for _, tg := range targets {
		row := parentRow(tg.LedgerRow)
		if !declared[row] {
			return fmt.Errorf("target %q claims ledger row %q, which the ledger does not declare. A "+
				"captured page attributed to a row nobody declared is a page nothing accounts for",
				tg.Path, row)
		}
		if prior, seen := how[row]; seen && prior != "captured" {
			return fmt.Errorf("ledger row %q was handled TWICE (%s, then captured)", row, prior)
		}
		how[row] = "captured"
	}

	for _, sk := range skipped {
		row := reasonlessRow(sk)
		if !declared[row] {
			return fmt.Errorf("skip %q names ledger row %q, which the ledger does not declare", sk, row)
		}
		if prior, seen := how[row]; seen {
			return fmt.Errorf("ledger row %q was handled TWICE (%s, then skipped). A count-based "+
				"accounting would let this cancel against a row handled by NEITHER arm and pass with a "+
				"page missing", row, prior)
		}
		how[row] = "skipped"
	}

	var unhandled []string
	for _, row := range ledger {
		if _, ok := how[row]; !ok {
			unhandled = append(unhandled, row)
		}
	}
	if len(unhandled) > 0 {
		sort.Strings(unhandled)
		return fmt.Errorf("the ledger declares %d row(s); %d were handled, and these were handled by "+
			"NEITHER arm: %v", len(ledger), len(how), unhandled)
	}
	return nil
}

// parentRow strips the suffix [ExpandLinks] appends, so a discovered target is attributed to the
// ledger row it descends from rather than to a string the ledger has never seen.
func parentRow(row string) string {
	if i := strings.Index(row, " (link from "); i >= 0 {
		return row[:i]
	}
	return row
}

// reasonlessRow strips the parenthesised reason from a skip line.
//
// ⚠ IT CUTS AT THE FIRST " (" AND THAT IS SAFE ONLY BECAUSE A LEDGER ROW CANNOT CONTAIN ONE: a row is
// `<METHOD> <path>[ <classes>]`, and both the method and the path are constrained — `internal/ui`'s
// `routes` map keys are literal paths, and a class name comes from `classNames`. If a row could ever
// contain " (", this would truncate it and the membership check above would reject a row that IS
// declared, which fails LOUDLY rather than silently. That is the direction to fail in, but it is
// worth knowing the assumption is there.
func reasonlessRow(skip string) string {
	if i := strings.Index(skip, " ("); i >= 0 {
		return skip[:i]
	}
	return skip
}
