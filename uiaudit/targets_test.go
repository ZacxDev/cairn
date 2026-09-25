package main

import (
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/ui"
)

// 🔴 ONE GUARD IN THIS FILE IS A REGRESSION GUARD AND THE REST ARE INVARIANT GUARDS, AND THE
// DIFFERENCE IS LABELLED PER TEST RATHER THAN CLAIMED FOR THE FILE.
//
// [Targets] is new code, so most of what is pinned here is an invariant no defect has
// violated. The exception is [TestAGuessedQueryParameterIsNotHowAPageIsReached], which pins a
// defect this harness's first draft SHIPPED: it expanded the share row into
// `/share?scope=<scope name>` and every target 404'd, because the parameter is a `control.ID`
// and not a name. That draft reported "62 axe violations across 6 rules" over a browser's own
// error page and exited 0. `red at the first draft, green at HEAD` — the matrix is in
// `README.md` with the rules it mis-attributed.

// TestAGuessedQueryParameterIsNotHowAPageIsReached is the regression guard.
//
// It pins the DESIGN, not the symptom: nothing in this package may synthesise a query
// parameter, because the only values that work are unguessable by construction. A future
// edit that reintroduced a per-path query table would make this test red — which is the
// point, since the symptom (a 404) is caught by a different guard in a different file and
// only after a browser has run.
func TestAGuessedQueryParameterIsNotHowAPageIsReached(t *testing.T) {
	targets, _, err := Targets(ui.DeclaredRouteLedger())
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range targets {
		if strings.Contains(tg.Path, "?") {
			t.Errorf("target %q carries a synthesised query parameter. The share row's %q is a "+
				"`control.ID` from crypto/rand, not a name — a guessed one 404s, and every other "+
				"signal in this harness will describe that error page as if it were the surface. "+
				"Reach it through ExpandLinks instead.", tg.Path, ui.QueryScope)
		}
	}
	// And the row that needs expansion must SAY so, or the walk never reads its links.
	var sawExpander bool
	for _, tg := range targets {
		if tg.Path == ui.SharePath {
			sawExpander = tg.ExpandLinks
		}
	}
	if !sawExpander {
		t.Errorf("%s must be marked ExpandLinks, or its per-scope pages are never reached at all", ui.SharePath)
	}
}

// TestExpandLinksAcceptsOnlyWhatTheSurfacePublishedForThisPage.
//
// 🔴 THE DECLINE LIST IS THE LOAD-BEARING HALF. `internal/ui/render.go`'s `safeHref`
// allowlists schemes, so an entry's `ref` can legitimately render an EXTERNAL link — and a
// harness that followed one would push somebody else's site through the hub. Each case
// below is a shape the expansion must decline, with the reason it is dangerous rather than
// merely irrelevant.
func TestExpandLinksAcceptsOnlyWhatTheSurfacePublishedForThisPage(t *testing.T) {
	from := Target{Path: ui.SharePath, PushURL: ui.SharePath, SignedIn: true, LedgerRow: "GET /share content", ExpandLinks: true}

	accepted, declined := ExpandLinks(from, []string{
		// The real shape: the share index's per-scope link.
		"/share?scope=scp_0000000000000000",
		"/share?scope=scp_1111111111111111",
		// A duplicate: one target, not two.
		"/share?scope=scp_0000000000000000",
		// Declined, each for its own reason.
		"https://example.invalid/share?scope=x", // absolute: would leave the origin entirely
		"//example.invalid/share?scope=x",       // protocol-relative: same, less obviously
		"/",                                     // a different path: already its own ledger row
		"/share",                                // the page itself: an infinite queue
		"mailto:nobody@example.invalid",         // a scheme `safeHref` may legitimately allow
	})

	var gotPaths []string
	for _, a := range accepted {
		gotPaths = append(gotPaths, a.Path)
		if a.SignedIn != from.SignedIn {
			t.Errorf("an expanded target must inherit its parent's session state; %q got SignedIn=%v", a.Path, a.SignedIn)
		}
		if !strings.Contains(a.LedgerRow, from.LedgerRow) {
			t.Errorf("an expanded target must stay attributable to the ledger row it came from; got %q", a.LedgerRow)
		}
		if a.ExpandLinks {
			t.Errorf("an expanded target must not itself expand, or the queue never drains; %q does", a.Path)
		}
	}
	want := "/share?scope=scp_0000000000000000|/share?scope=scp_1111111111111111"
	if strings.Join(gotPaths, "|") != want {
		t.Fatalf("accepted %v, want %v", gotPaths, strings.Split(want, "|"))
	}
	// FIVE, not six: a DUPLICATE is deduped rather than declined, and the distinction is
	// worth pinning. A duplicate is the same target twice (the share index could legitimately
	// render one scope in two places); a decline is a target the walk refuses to visit. Both
	// produce "not in the accepted set", which is exactly why counting them together would
	// hide a decline that should have been an accept.
	if len(declined) != 5 {
		t.Fatalf("want 5 declined hrefs, got %d: %v", len(declined), declined)
	}
	for _, d := range declined {
		if d == "/share?scope=scp_0000000000000000" {
			t.Error("the duplicate was DECLINED rather than deduped: a decline and a repeat are different facts")
		}
	}
}

// TestTheLedgerCARRIESEveryNotADocumentRow is the assertion the `notADocument` closing condition
// promised, and it is expressible only now that the auth change is merged.
//
// 🔴 IT REPLACES A TRANSCRIBED FIXTURE, AND DELETING THAT FIXTURE WAS THE POINT RATHER THAN TIDYING.
// While the auth change was unmerged this file carried `mergedLedger` — a hand-copied ledger used to
// predict the merged tree's behaviour. That was the right instrument then and is the wrong one now: a
// transcription is a copy, and a copy of a ledger is exactly what this module refuses everywhere
// else. The real `ui.DeclaredRouteLedger()` carries those rows today, so a prediction is replaced by
// a measurement and the fixture is gone.
//
// What it pins: every key in `notADocument` is a row the ledger actually declares. A key matching no
// row is inert — it would silently stop excluding anything, and the row it was meant to exclude would
// fall through to the `default` refusal, which reads as "somebody added a route" rather than "a
// constant was renamed". Both halves are one failure seen from opposite ends.
func TestTheLedgerCARRIESEveryNotADocumentRow(t *testing.T) {
	ledger := ui.DeclaredRouteLedger()
	if len(notADocument) == 0 {
		t.Fatal("`notADocument` is empty, so every assertion here passes vacuously")
	}
	for path, reason := range notADocument {
		if !hasRow(ledger, "GET "+path) {
			t.Errorf("`notADocument` carries %q but the ledger declares no `GET %s` row. A key that "+
				"matches nothing excludes nothing, and the route it was meant to exclude then falls "+
				"through to the unclassified-row refusal — which reads as a NEW route rather than a "+
				"renamed constant.\n  ledger: %v", path, path, ledger)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("`notADocument[%q]` has an empty reason; a skip with no reason cannot be told from "+
				"a row somebody gave up on", path)
		}
	}
	// And the two rows are the ones the constants name, so a constant repointed at a different route
	// fails here rather than silently changing what the walk skips.
	for _, want := range []string{ui.StylesheetPath, ui.OAuthCallbackPath} {
		if notADocument[want] == "" {
			t.Errorf("%q is not in `notADocument`; the walk would try to capture it as a page", want)
		}
	}
}

// TestTheREALLedgerIsFullyACCOUNTEDFor replaces the fixture-driven version of this check.
//
// ✅ THE MERGED TREE IT USED TO PREDICT IS NOW `main`, so this runs against the live ledger and the
// disjoint-file merge break it was written for is history rather than a forecast. It is kept because
// the NEXT route-adding change gets the same treatment for free.
func TestTheREALLedgerIsFullyACCOUNTEDFor(t *testing.T) {
	ledger := ui.DeclaredRouteLedger()
	targets, skipped, err := Targets(ledger)
	if err != nil {
		t.Fatalf("the live ledger must be fully accounted for, got: %v", err)
	}
	if err := LedgerAccounting(ledger, targets, skipped); err != nil {
		t.Fatal(err)
	}

	captured := map[string]bool{}
	for _, tg := range targets {
		captured[tg.Path] = true
	}
	for _, want := range []string{ui.RootPath, ui.SharePath, ui.SignInPath} {
		if !captured[want] {
			t.Errorf("%s must be captured", want)
		}
	}
	// 🔴 NO `notADocument` ROW MAY BE CAPTURED. The callback renders a refusal without a provider
	// code and a live flight cookie; the stylesheet is not a document at all.
	for never := range notADocument {
		if captured[never] {
			t.Errorf("%s was CAPTURED: a browser walk over it measures a non-document or an error page "+
				"and counts it, which is the false green this harness already shipped once", never)
		}
	}

	joined := strings.Join(skipped, "\n")
	// Both counts DERIVED from the ledger, never written down — a literal was wrong twice in this
	// file's history and both times it refused an honest tree.
	wantNonGET, wantNotDoc := 0, 0
	for _, row := range ledger {
		fields := strings.Fields(row)
		switch {
		case !strings.HasPrefix(row, "GET "):
			wantNonGET++
		case len(fields) >= 2 && notADocument[fields[1]] != "":
			wantNotDoc++
		}
	}
	if wantNotDoc == 0 {
		t.Fatal("the live ledger carries no `notADocument` GET row, so the skipped-with-a-reason " +
			"assertions below pass vacuously")
	}
	if n := strings.Count(joined, "not GET:"); n != wantNonGET {
		t.Errorf("the ledger has %d non-GET row(s); %d carried that reason:\n%s", wantNonGET, n, joined)
	}
	if n := strings.Count(joined, "not a document:"); n != wantNotDoc {
		t.Errorf("the ledger has %d not-a-document GET row(s); %d carried that reason:\n%s", wantNotDoc, n, joined)
	}
	t.Logf("live ledger: %d row(s) -> %d target(s), %d skip(s)", len(ledger), len(targets), len(skipped))
	for _, sk := range skipped {
		t.Logf("  skip %s", sk)
	}
}

// TestAPathClaimedByTwoClassesIsREFUSED.
//
// ⚠ AN INVARIANT GUARD, LABELLED. No path has ever been in two sets. It is pinned because the
// sets are hand-written and the failure is invisible: a row moved to `notADocument` without its
// old entry deleted keeps being captured, and the reason string added beside it is never printed
// — a declaration that reads as a decision and has no effect.
func TestAPathClaimedByTwoClassesIsREFUSED(t *testing.T) {
	saved := notADocument
	notADocument = map[string]string{ui.RootPath: "pretend the root is not a document"}
	defer func() { notADocument = saved }()

	_, _, err := Targets([]string{"GET / content"})
	if err == nil {
		t.Fatal("a path in both `plainGET` and `notADocument` was accepted: which class wins is then " +
			"whichever `case` the switch reaches first, and the losing declaration is inert")
	}
	if !strings.Contains(err.Error(), "plainGET") || !strings.Contains(err.Error(), "notADocument") {
		t.Fatalf("the refusal must name BOTH claiming classes; got %q", err)
	}
}

// TestAGETRowTheWalkWasNotToldAboutIsREFUSEDRatherThanCapturedBare.
//
// ⚠ AN INVARIANT GUARD, LABELLED. No ledger in this repository has ever carried an unhandled
// row. It exists because two changes are in flight that each ADD rows — a stylesheet route
// and two OAuth routes — and a walk that absorbed a new row silently would under-cover it
// while reporting success, which is the failure this whole derivation is designed against.
func TestAGETRowTheWalkWasNotToldAboutIsREFUSEDRatherThanCapturedBare(t *testing.T) {
	ledger := append(ui.DeclaredRouteLedger(), "GET /assets/unclassified.css")

	_, _, err := Targets(ledger)
	if err == nil {
		t.Fatal("a GET row in none of the three classes was absorbed silently: " +
			"the walk would report success over a surface it under-covered")
	}
	// 🔴 THE MESSAGE MUST NAME THE ROW, because the whole value of the refusal is that the
	// person who added the route learns which one it is without reading this file.
	if !strings.Contains(err.Error(), "GET /assets/unclassified.css") {
		t.Fatalf("the refusal must name the row it refused; got %q", err)
	}
	if !strings.Contains(err.Error(), "plainGET") || !strings.Contains(err.Error(), "linkExpanded") {
		t.Fatalf("the refusal must name BOTH remedies so the reader makes a decision rather than a guess; got %q", err)
	}
}

// TestTheWalkStateIsDerivedFromTheCLASSAndNeverFromThePATH pins the one thing a spelled
// guard would get wrong.
//
// ⚠ AN INVARIANT GUARD. A walk that decided "signed out" by looking for a path starting
// `/sign-` would be a guard on a WORD. `internal/ui/routes.go` says a class can only make a
// route LESS protected, so the class is the only authority on which state a row renders in —
// and a future public row named anything else must still be captured signed-out.
func TestTheWalkStateIsDerivedFromTheCLASSAndNeverFromThePATH(t *testing.T) {
	// A public row whose path shares no substring with the sign-in pair.
	ledger := []string{"GET /landing public", "GET / content"}
	saved := plainGET
	plainGET = map[string]bool{"/landing": true, "/": true}
	defer func() { plainGET = saved }()

	targets, _, err := Targets(ledger)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]Target{}
	for _, tg := range targets {
		byPath[tg.Path] = tg
	}
	if got := byPath["/landing"]; got.SignedIn {
		t.Errorf("a `public` row must be captured signed-OUT whatever its path is spelled; /landing got SignedIn=%v", got.SignedIn)
	}
	if got := byPath["/"]; !got.SignedIn {
		t.Errorf("a non-public row must be captured signed-IN; / got SignedIn=%v", got.SignedIn)
	}
}

// TestANonGETRowIsSkippedAndCOUNTED pins that the skip is visible.
//
// ⚠ AN INVARIANT GUARD. A walk that dropped non-GET rows silently would make
// [LedgerAccounting] unable to tell "handled" from "forgotten" — and the accounting is the
// only thing that notices a ledger that grew.
func TestANonGETRowIsSkippedAndCOUNTED(t *testing.T) {
	ledger := ui.DeclaredRouteLedger()
	targets, skipped, err := Targets(ledger)
	if err != nil {
		t.Fatal(err)
	}
	// ⚠ THE EXPECTATION COUNTS BOTH KINDS OF SKIP, AND COUNTING ONLY NON-GET ROWS WAS A DEFECT
	// THE MERGED TREE EXPOSED. On a ledger that carries a `notADocument` row this test read
	// "5 non-GET rows, 7 skips" and failed — not because the walk was wrong but because the
	// expectation was narrower than the thing it measured. A test that hardcodes which KINDS of
	// skip exist goes stale the moment a kind is added, which is the same failure mode as a
	// hardcoded path list.
	wantSkipped := 0
	for _, row := range ledger {
		fields := strings.Fields(row)
		switch {
		case !strings.HasPrefix(row, "GET "):
			wantSkipped++
		case len(fields) >= 2 && notADocument[fields[1]] != "":
			wantSkipped++
		}
	}
	if len(skipped) != wantSkipped {
		t.Fatalf("the ledger implies %d skip(s) (non-GET plus notADocument); the walk listed %d:\n%s",
			wantSkipped, len(skipped), strings.Join(skipped, "\n"))
	}
	if err := LedgerAccounting(ledger, targets, skipped); err != nil {
		t.Fatal(err)
	}
}

// TestLedgerAccountingCatchesAnUnaccountedRow is the mutation control on the accounting
// itself: a check that could not go red would be a count nobody reads.
func TestLedgerAccountingCatchesAnUnaccountedRow(t *testing.T) {
	ledger := ui.DeclaredRouteLedger()
	targets, skipped, err := Targets(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if err := LedgerAccounting(ledger, targets, skipped); err != nil {
		t.Fatalf("the honest case must pass: %v", err)
	}
	if len(skipped) == 0 {
		t.Fatal("the ledger carries no non-GET rows, so this control cannot run")
	}
	// Drop one skip and watch the count refuse. The mutation is the NARROWEST thing that can
	// be wrong: one row removed from one of the two accounted sets.
	if err := LedgerAccounting(ledger, targets, skipped[1:]); err == nil {
		t.Fatal("dropping a handled row left the accounting green: it is counting nothing")
	}
	// And the other direction: an extra row nobody handled.
	if err := LedgerAccounting(append(ledger, "GET /extra"), targets, skipped); err == nil {
		t.Fatal("a ledger row nobody handled left the accounting green: it cannot see a ledger that GREW")
	}
}

// TestTheUNKNOWNRowREFUSALSURVIVESTheThirdClass is the guard on the guard.
//
// 🔴 ADDING A CLASS IS EDITING THE THING THAT CATCHES AN UNCLASSIFIED ROW, AND THE OBVIOUS WAY TO
// GET IT WRONG IS TO MAKE `notADocument` THE DEFAULT. A `switch` whose new arm was written as
// `default:` — or whose `notADocument` lookup used a `map[string]bool` zero value the wrong way
// round — would absorb every future row silently WITH a reason string attached, which reads as a
// deliberate decision and is the worst version of this failure: coverage that documents itself.
//
// So this drives an unknown row against the ledger that now HAS a third class, and requires the
// refusal still to fire and still to name all three remedies.
func TestTheUNKNOWNRowREFUSALSURVIVESTheThirdClass(t *testing.T) {
	_, _, err := Targets(append(ui.DeclaredRouteLedger(), "GET /assets/logo.svg public"))
	if err == nil {
		t.Fatal("an unclassified GET row was absorbed after the third class was added: `notADocument` " +
			"has become a default, and every future row is now silently skipped WITH a reason that " +
			"reads as a decision somebody made")
	}
	for _, remedy := range []string{"plainGET", "linkExpanded", "notADocument"} {
		if !strings.Contains(err.Error(), remedy) {
			t.Errorf("the refusal must name the %q remedy so the reader makes a decision rather than a guess; got %q", remedy, err)
		}
	}
	if !strings.Contains(err.Error(), "WITH A REASON") {
		t.Errorf("the refusal must say that `notADocument` needs a REASON, or the next row gets an empty one; got %q", err)
	}
	if !strings.Contains(err.Error(), "GET /assets/logo.svg") {
		t.Errorf("the refusal must name the row it refused; got %q", err)
	}
}

// TestTheAccountingCatchesTWOERRORSTHATCANCEL is the case a count comparison cannot see.
//
// 🔴 RED AT BASE. The first `LedgerAccounting` compared `len(capturedRows) + len(skipped)` against
// `len(ledger)`. Feed it one row handled by BOTH arms and one handled by NEITHER and the arithmetic
// balances exactly — so it passed with a page missing, while its own docstring called it "the only
// thing that notices a ledger that grew". The existing control only DROPS a skip and APPENDS a row,
// which are the two cases a count does catch; this is the one it cannot.
//
// ⚠ Invariant-strength: [Targets]'s switch cannot currently produce a double-handled row. The check
// must not depend on that, because it is the thing that would notice if the switch changed.
func TestTheAccountingCatchesTWOERRORSTHATCANCEL(t *testing.T) {
	ledger := []string{"GET /a content", "GET /b content", "POST /c"}

	// /a is CAPTURED and also SKIPPED; /b is handled by neither. Counts: 1 captured row + 2 skips = 3
	// == len(ledger). A count-based accounting passes this.
	targets := []Target{{Path: "/a", PushURL: "/a", LedgerRow: "GET /a content"}}
	skipped := []string{
		"GET /a content (not a document: pretend)",
		"POST /c (not GET: reached by submitting a form, never navigated)",
	}

	if got := len(targets) + len(skipped); got != len(ledger) {
		t.Fatalf("this fixture must make the COUNTS balance, or it does not exercise the defect: "+
			"%d vs %d", got, len(ledger))
	}

	err := LedgerAccounting(ledger, targets, skipped)
	if err == nil {
		t.Fatal("a row handled by BOTH arms cancelled against a row handled by NEITHER, and the " +
			"accounting passed with a page missing — which is exactly what a count comparison does")
	}
	if !strings.Contains(err.Error(), "handled TWICE") {
		t.Errorf("the refusal should name the double-handling, since that is the half a count cannot "+
			"see; got %q", err)
	}

	// And the other half on its own: a row handled by neither, with nothing to cancel it.
	if err := LedgerAccounting(ledger, nil, []string{"POST /c (not GET: x)"}); err == nil {
		t.Error("a row handled by neither arm passed")
	} else if !strings.Contains(err.Error(), "NEITHER arm") {
		t.Errorf("the refusal should name the unhandled rows; got %q", err)
	}

	// 🔴 AND A ROW THE LEDGER DOES NOT DECLARE, which a count also cannot see: the total can be
	// right while the membership is wrong.
	bogus := []Target{{Path: "/z", PushURL: "/z", LedgerRow: "GET /z content"}}
	if err := LedgerAccounting(ledger, bogus, []string{
		"GET /a content (not a document: x)",
		"GET /b content (not a document: x)",
		"POST /c (not GET: x)",
	}); err == nil {
		t.Error("a target attributed to a row the ledger never declared was accounted for")
	} else if !strings.Contains(err.Error(), "does not declare") {
		t.Errorf("the refusal should name the undeclared row; got %q", err)
	}

	// 🔴 THE POSITIVE CONTROL. A check that refused every input would satisfy all three assertions
	// above while making the walk unrunnable — and the honest case includes the link-expansion shape,
	// where SEVERAL targets legitimately share one ledger row.
	honest := []Target{
		{Path: "/a", PushURL: "/a", LedgerRow: "GET /a content"},
		{Path: "/b?scope=x", PushURL: "/b?scope=x", LedgerRow: "GET /b content (link from /b)"},
		{Path: "/b?scope=y", PushURL: "/b?scope=y", LedgerRow: "GET /b content (link from /b)"},
		{Path: "/b", PushURL: "/b", LedgerRow: "GET /b content"},
	}
	if err := LedgerAccounting(ledger, honest, []string{"POST /c (not GET: x)"}); err != nil {
		t.Fatalf("the honest case — including several targets sharing one row via link expansion — "+
			"must pass: %v", err)
	}
}
