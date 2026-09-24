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

// mergedLedger is the ledger the auth change produces, transcribed from ITS `routes.go` rather
// than imagined — the two OAuth rows and the stylesheet row, with the classes that file gives
// them.
//
// 🔴 IT IS A FIXTURE RATHER THAN AN IMPORT BECAUSE THE ROWS DO NOT EXIST ON THIS BASE, AND THAT
// IS ALSO WHY THIS TEST MATTERS. Both changes are green on their own branches and touch ZERO
// files in common, so `git merge-tree` exits 0 and every per-branch gate stays green — and the
// merged tree is still red, because this walk's accounting refuses a `GET` row nobody classified.
// That is the disjoint-file merge break: one side widened the route ledger, the other added a
// consumer of it. This fixture is what makes the merged tree's answer knowable from HERE.
//
// ⚠ A TRANSCRIPTION IS A COPY AND CAN GO STALE. It stops being needed the moment the rows land,
// at which point the real `ui.DeclaredRouteLedger()` carries them and this fixture should be
// deleted rather than updated — the closing condition on `notADocument`'s literals is the same
// event.
var mergedLedger = []string{
	"GET / content",
	"GET /share content",
	"GET /sign-in public",
	"GET /sign-in/github/callback public",
	"GET /static/app.css public",
	"POST /share",
	"POST /sign-in public",
	"POST /sign-in/github public",
	"POST /sign-out",
	"POST /unshare",
}

// TestTheMERGEDLedgerIsFullyACCOUNTEDFor is the regression guard for the break the merged tree
// has.
//
// It asserts all five `GET` rows are classified, that the two new ones are SKIPPED WITH A REASON
// rather than captured, and — the half that matters — that the reason says WHY each is not a
// document. A skip with no reason is the thing `notADocument` exists to prevent.
func TestTheMERGEDLedgerIsFullyACCOUNTEDFor(t *testing.T) {
	targets, skipped, err := Targets(mergedLedger)
	if err != nil {
		t.Fatalf("the merged ledger must be fully accounted for, got: %v", err)
	}
	if err := LedgerAccounting(mergedLedger, targets, skipped); err != nil {
		t.Fatal(err)
	}

	captured := map[string]bool{}
	for _, tg := range targets {
		captured[tg.Path] = true
	}
	for _, want := range []string{"/", "/share", "/sign-in"} {
		if !captured[want] {
			t.Errorf("%s must still be captured on the merged ledger", want)
		}
	}
	// 🔴 NEITHER NEW ROW MAY BE CAPTURED. The callback renders a refusal without a provider
	// code and a live flight cookie; the stylesheet is not a document at all.
	for _, never := range []string{"/sign-in/github/callback", "/static/app.css"} {
		if captured[never] {
			t.Errorf("%s was CAPTURED: a browser walk over it measures a non-document or an error page "+
				"and counts it, which is the false green this harness already shipped once", never)
		}
	}

	joined := strings.Join(skipped, "\n")
	// Each skip must carry a REASON, and the reason must be about why it is not a document —
	// not merely that it was skipped.
	for _, want := range []string{
		"GET /sign-in/github/callback public (not a document: reachable only with a provider ?code=",
		"GET /static/app.css public (not a document: a text/css response and not a document",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the skip list must carry this row AND its reason:\n  want prefix: %q\n  got:\n%s", want, joined)
		}
	}
	// The non-GET rows are still skipped for the OTHER reason, which must remain a
	// DISTINGUISHABLE sentence — collapsing the two kinds of skip would lose the difference
	// between "a browser cannot usefully render this" and "a browser must not navigate this".
	//
	// ⚠ THE EXPECTED COUNT IS DERIVED FROM THE FIXTURE, NOT WRITTEN DOWN. A literal here was
	// wrong twice in this file's history — once at 28-vs-16 result lines and once at 4-vs-5
	// non-GET rows, both times refusing an honest tree. A count that can disagree with the
	// thing it counts is a second source of truth, so it is computed.
	wantNonGET := 0
	for _, row := range mergedLedger {
		if !strings.HasPrefix(row, "GET ") {
			wantNonGET++
		}
	}
	if n := strings.Count(joined, "not GET:"); n != wantNonGET {
		t.Errorf("the fixture has %d non-GET row(s); %d carried the non-GET reason:\n%s", wantNonGET, n, joined)
	}
	wantNotDoc := 0
	for _, row := range mergedLedger {
		fields := strings.Fields(row)
		if len(fields) >= 2 && fields[0] == "GET" && notADocument[fields[1]] != "" {
			wantNotDoc++
		}
	}
	if wantNotDoc == 0 {
		t.Fatal("the fixture contains no `notADocument` GET row, so every assertion above about " +
			"skipped-with-a-reason passed vacuously")
	}
	if n := strings.Count(joined, "not a document:"); n != wantNotDoc {
		t.Errorf("the fixture has %d not-a-document GET row(s); %d carried that reason:\n%s", wantNotDoc, n, joined)
	}
	t.Logf("merged ledger: %d row(s) -> %d target(s), %d skip(s)", len(mergedLedger), len(targets), len(skipped))
	for _, s := range skipped {
		t.Logf("  skip %s", s)
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
	ledger := append(ui.DeclaredRouteLedger(), "GET /assets/app.css")

	_, _, err := Targets(ledger)
	if err == nil {
		t.Fatal("a GET row in neither `plainGET` nor `linkExpanded` was absorbed silently: " +
			"the walk would report success over a surface it under-covered")
	}
	// 🔴 THE MESSAGE MUST NAME THE ROW, because the whole value of the refusal is that the
	// person who added the route learns which one it is without reading this file.
	if !strings.Contains(err.Error(), "GET /assets/app.css") {
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
	_, _, err := Targets(append(mergedLedger, "GET /assets/logo.svg public"))
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
