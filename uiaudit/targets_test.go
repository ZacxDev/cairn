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

	ledger := ui.DeclaredRouteLedger()
	accepted, declined, bounded := ExpandLinks(from, []string{
		// The real shape: the share index's per-scope link.
		"/share?scope=scp_0000000000000000",
		"/share?scope=scp_1111111111111111",
		// A duplicate: one target, not two.
		"/share?scope=scp_0000000000000000",
		// Declined, each for its own reason.
		"https://example.invalid/share?scope=x", // absolute: would leave the origin entirely
		"//example.invalid/share?scope=x",       // protocol-relative: same, less obviously
		"/share",                                // the page itself, with no query: an infinite queue
		"mailto:nobody@example.invalid",         // a scheme `safeHref` may legitimately allow
		"/nowhere?x=1",                          // a query, relative — and NO declared GET row
	}, ledger)

	var gotPaths []string
	for _, a := range accepted {
		gotPaths = append(gotPaths, a.Path)
		if a.SignedIn != from.SignedIn {
			t.Errorf("an expanded target must inherit its parent's session state; %q got SignedIn=%v", a.Path, a.SignedIn)
		}
		if !strings.Contains(a.LedgerRow, from.LedgerRow) {
			t.Errorf("an expanded target must stay attributable to the ledger row it came from; got %q", a.LedgerRow)
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
	// Two accepted, under the per-page bound: nothing here is a bounded target, and saying
	// so is what keeps `bounded` from silently absorbing a decline.
	if bounded != 0 {
		t.Errorf("bounded=%d over %d accepted target(s) and a cap of %d; a bounded count where nothing was "+
			"capped means a decline was folded into the wrong number", bounded, len(accepted), MaxExpansionsPerPage)
	}
}

// TestExpandLinksIsBOUNDEDPerPageAndSaysSoRatherThanTruncatingSilently is the guard on the
// cap, and the cap exists because the unbounded version was MEASURED and failed.
//
// 🔴 A SCOPE PAGE PUBLISHES ONE LINK PER ENTRY. The fixture store carries 126, so the first
// unbounded walk captured 650 pages, spent fifteen minutes doing it, and then built a
// 260-page payload that `Validate` refused with `too many pages: 260 (max 200)` — the hub's
// own cap, reached because the push arithmetic is `targets × pushed viewports`.
//
// 🔴 AND THE BOUND IS APPLIED AFTER THE SORT, WHICH IS THE ONLY ORDER THAT MAKES IT
// DETERMINISTIC. Capping as the hrefs arrive takes whichever ones the DOM listed first, so
// "which entry pages did this walk look at" becomes a function of render order — and the hub
// matches its P2 pixel diff on `url`, so a set that moved between runs would make every page
// "new" on half of them and the diff would never settle.
func TestExpandLinksIsBOUNDEDPerPageAndSaysSoRatherThanTruncatingSilently(t *testing.T) {
	ledger := ui.DeclaredRouteLedger()
	from := Target{
		Path: ui.ScopePath, PushURL: ui.ScopePath, SignedIn: true,
		LedgerRow: "GET " + ui.ScopePath + " content", ExpandLinks: true,
	}
	// Published in DESCENDING order, so a cap applied before the sort keeps the LAST refs
	// alphabetically and a cap applied after keeps the first. The two answers are different
	// sets, which is what makes this a measurement rather than a restatement.
	var hrefs []string
	for _, ref := range []string{"zulu", "yankee", "xray", "whisky", "victor", "uniform", "tango"} {
		hrefs = append(hrefs, "/entry?ref="+ref+"&scope=scp_0000000000000000")
	}
	if len(hrefs) <= MaxExpansionsPerPage {
		t.Fatalf("the fixture publishes %d href(s) against a cap of %d, so nothing is capped and this test "+
			"measures nothing", len(hrefs), MaxExpansionsPerPage)
	}

	accepted, declined, bounded := ExpandLinks(from, hrefs, ledger)
	if len(accepted) != MaxExpansionsPerPage {
		t.Fatalf("accepted %d target(s) against a cap of %d", len(accepted), MaxExpansionsPerPage)
	}
	if want := len(hrefs) - MaxExpansionsPerPage; bounded != want {
		t.Errorf("bounded=%d, want %d. The count is what the walk PRINTS, and a bounded walk that reports "+
			"nothing reads as a complete one — which is the under-coverage this whole derivation refuses.",
			bounded, want)
	}
	if len(declined) != 0 {
		t.Errorf("%d href(s) were DECLINED: every one here is a well-formed relative link to a declared row, "+
			"so a decline means the acceptance test rejected something it should have bounded instead: %v",
			len(declined), declined)
	}
	// 🔴 THE KEPT SET IS THE ALPHABETICALLY FIRST, WHICH IS WHAT SAYS THE SORT RAN BEFORE THE
	// CAP. The fixture publishes them in descending order, so a pre-sort cap would keep
	// zulu/yankee/xray/whisky.
	var got []string
	for _, a := range accepted {
		got = append(got, a.Path)
	}
	want := []string{
		"/entry?ref=tango&scope=scp_0000000000000000",
		"/entry?ref=uniform&scope=scp_0000000000000000",
		"/entry?ref=victor&scope=scp_0000000000000000",
		"/entry?ref=whisky&scope=scp_0000000000000000",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the bounded set is %v, want %v. A different set means the cap was applied BEFORE the sort, "+
			"so which pages this walk looks at depends on DOM order and moves between runs.", got, want)
	}

	// SECOND RUN, SAME INPUT IN A DIFFERENT ORDER: the bound is deterministic over the SET
	// and not over the order it arrived in.
	shuffled := append([]string{hrefs[3], hrefs[0], hrefs[6]}, hrefs[1], hrefs[2], hrefs[4], hrefs[5])
	again, _, _ := ExpandLinks(from, shuffled, ledger)
	var gotAgain []string
	for _, a := range again {
		gotAgain = append(gotAgain, a.Path)
	}
	if strings.Join(gotAgain, "|") != strings.Join(want, "|") {
		t.Errorf("the same hrefs in a different order produced %v, want %v — the sample is order-dependent",
			gotAgain, want)
	}
	t.Logf("expansion bound: %d href(s) -> %d target(s) kept (sorted-first), %d bounded, %d declined; the "+
		"same set in a different order keeps the same four", len(hrefs), len(accepted), bounded, len(declined))
}

// TestExpandLinksFollowsALinkACROSSRowsAndOnlyToADeclaredOne is the guard on the widening
// the browse pages needed.
//
// 🔴 THE OLD RULE WAS `u.Path == from.Path` AND IT WOULD HAVE SILENTLY UNDER-COVERED THE
// NEW PAGES RATHER THAN FAILING. `GET /` publishes `/scope?id=…` and `GET /scope?id=…`
// publishes `/entry?…` — links ACROSS ledger rows — so a same-path rule declines every one
// of them, the walk captures the two new rows only in their parameterless form, and the run
// reports success over pages no reader ever sees. That is the exact failure mode
// `linkExpanded`'s own comment records from the first draft of this file, one step along.
//
// ⚠ AND THE WIDENING IS BOUNDED BY THE LEDGER, WHICH IS THE HALF THAT KEEPS IT FROM BEING A
// CRAWLER. `/nowhere?x=1` above is relative, same-origin and carries a query, and it is
// still declined — because the path is not a row this server declares.
func TestExpandLinksFollowsALinkACROSSRowsAndOnlyToADeclaredOne(t *testing.T) {
	ledger := ui.DeclaredRouteLedger()
	from := Target{
		Path: ui.RootPath, PushURL: ui.RootPath, SignedIn: true,
		LedgerRow: "GET / content", ExpandLinks: true,
	}
	accepted, declined, bounded := ExpandLinks(from, []string{
		"/scope?id=scp_0000000000000000",
		"/entry?ref=runbook&scope=scp_0000000000000000",
		"/",                                  // no query: already its own row, and an infinite queue
		"https://tracker.invalid/issue/4711", // a task ref, which `safeHref` legitimately permits
	}, ledger)

	if len(accepted) != 2 {
		t.Fatalf("accepted %d target(s), want 2 (%v); a same-path rule accepts NEITHER, which is the "+
			"regression this test is named for", len(accepted), accepted)
	}
	byPath := map[string]Target{}
	for _, a := range accepted {
		byPath[a.Path] = a
	}

	// 🔴 ATTRIBUTION IS TO THE ROW THE HREF ADDRESSES, NOT TO THE PUBLISHING PAGE.
	// `LedgerAccounting` reads `parentRow` off this string to decide which row a capture
	// discharges, so attributing a `/scope` capture to `GET / content` would leave
	// `GET /scope content` handled by NEITHER arm — which that function refuses, correctly,
	// because the row really would be uncaptured.
	scope := byPath["/scope?id=scp_0000000000000000"]
	if got := parentRow(scope.LedgerRow); got != "GET "+ui.ScopePath+" content" {
		t.Errorf("the discovered scope page is attributed to row %q; it addresses %s and must discharge "+
			"THAT row", got, ui.ScopePath)
	}
	// And it expands in turn, which is the only way an ENTRY page is ever reached.
	if !scope.ExpandLinks {
		t.Errorf("the discovered %s page does not expand, so no entry page is reachable by any route this "+
			"walk has. The queue's `enqueued` set is what keeps that terminating.", ui.ScopePath)
	}
	entry := byPath["/entry?ref=runbook&scope=scp_0000000000000000"]
	if got := parentRow(entry.LedgerRow); got != "GET "+ui.EntryPath+" content" {
		t.Errorf("the discovered entry page is attributed to row %q, want the %s row", got, ui.EntryPath)
	}
	if entry.ExpandLinks {
		t.Errorf("%s is not in `linkExpanded` — an entry page publishes only its breadcrumb and its task "+
			"refs, and a task ref is external — so a discovered one must not expand", ui.EntryPath)
	}

	if len(declined) != 2 {
		t.Fatalf("want 2 declined hrefs, got %d: %v", len(declined), declined)
	}
	if bounded != 0 {
		t.Errorf("bounded=%d over 2 accepted target(s) and a cap of %d", bounded, MaxExpansionsPerPage)
	}
	t.Logf("cross-row expansion: %s -> %v, each attributed to its own ledger row; %d declined",
		ui.RootPath, []string{scope.Path, entry.Path}, len(declined))
}

// TestOnlyTheHubsOwnTwoViewportsAreEverPushed pins the boundary between what the walk
// MEASURES and what it SENDS.
//
// 🔴 THE HUB'S SET IS CLOSED AND THIS PROGRAM CANNOT WIDEN IT. `Validate` refuses a page
// whose viewport is outside `{mobile, desktop}` — the server's contract — and the hub
// matches its P2 pixel diff on `url`+`viewport`, so a page pushed under a name it has never
// stored would be "new" on every run and the diff would say nothing forever. Capturing five
// widths locally is what measures a responsive layout; pushing five would be a wire-contract
// change this repository does not own.
func TestOnlyTheHubsOwnTwoViewportsAreEverPushed(t *testing.T) {
	if len(Viewports) != 5 {
		t.Fatalf("the walk declares %d viewport(s), want 5. The five are named in `Viewports` with what "+
			"each one is for; changing the count is a decision, not a tidy-up.", len(Viewports))
	}
	var pushed, local []string
	widths := map[int]bool{}
	for _, vp := range Viewports {
		if widths[vp.Width] {
			t.Errorf("two viewports share width %d, so one of them measures nothing the other does not", vp.Width)
		}
		widths[vp.Width] = true
		if vp.Push {
			pushed = append(pushed, vp.Name)
		} else {
			local = append(local, vp.Name)
		}
	}
	if strings.Join(pushed, ",") != Mobile.Name+","+Desktop.Name {
		t.Errorf("the pushed set is %v; the hub's closed set is {%s, %s} and `Validate` REFUSES anything "+
			"else. A third pushed viewport is a 400 on the whole push.", pushed, Mobile.Name, Desktop.Name)
	}
	if len(local) == 0 {
		t.Fatal("no viewport is local-only, so this test's distinction is vacuous and the extra widths " +
			"bought nothing")
	}

	// 🔴 AND THE FILTER IS MEASURED THROUGH `BuildPayload`, NOT INFERRED FROM THE FIELD. A
	// `Push` flag nothing branches on is a declaration, not a guard — and this repository's
	// own rule is that a field existing in a struct is not a gate, only a BRANCH on it is.
	var captures []*Capture
	for _, vp := range Viewports {
		captures = append(captures, &Capture{
			Target:     Target{Path: "/", PushURL: "/", LedgerRow: "GET / content"},
			Viewport:   vp,
			Screenshot: []byte("\x89PNG\r\n\x1a\n-fixture"),
			AxeJSON:    []byte(`{"testEngine":{"name":"axe-core"},"violations":[]}`),
			Layout:     &PushLayout{InnerWidth: vp.Width},
		})
	}
	payload, files, err := BuildPayload("fixture", captures)
	if err != nil {
		t.Fatalf("building the payload: %v", err)
	}
	if len(payload.Pages) != len(pushed) {
		t.Errorf("BuildPayload emitted %d page(s) from %d capture(s); only the %d pushed viewport(s) may "+
			"reach the wire", len(payload.Pages), len(captures), len(pushed))
	}
	for _, pg := range payload.Pages {
		if pg.Viewport != Mobile.Name && pg.Viewport != Desktop.Name {
			t.Errorf("a page carrying viewport %q reached the payload; the server refuses it with a 400 on "+
				"the WHOLE push", pg.Viewport)
		}
	}
	// The server's own shape check, run over what was built — the same call `main` makes
	// before uploading, so this is the real refusal and not a restatement of it.
	if err := payload.Validate(files); err != nil {
		t.Errorf("the payload built from a five-width walk does not satisfy the server's shape rules: %v", err)
	}
	t.Logf("viewports: %d captured (%v local-only), %d pushed (%v), payload carries %d page(s)",
		len(Viewports), local, len(pushed), pushed, len(payload.Pages))
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
	// ⚠ BOTH STYLESHEET ROWS, because `internal/ui` serves two — the content-hashed path every
	// page links and the unversioned path kept for URLs already issued. A list naming only one
	// would leave the other falling through to the unclassified-row refusal, which reads as
	// "somebody added a route" rather than "a second spelling of the same asset".
	for _, want := range []string{ui.StylesheetPath, ui.StylesheetHashedPath, ui.OAuthCallbackPath} {
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
	// ⚠ THE PATH IS THE SIGN-IN ROW AND NOT THE ROOT, AND THE SWAP IS NOT COSMETIC. This
	// case needs a path that is ALREADY in exactly one class so that adding a second makes
	// two; the root moved from `plainGET` to `linkExpanded` when it started publishing scope
	// cards, so overriding `notADocument` with it would still produce a two-class refusal —
	// naming a different pair, and the assertion below would fail for a reason that has
	// nothing to do with what this test measures.
	saved := notADocument
	notADocument = map[string]string{ui.SignInPath: "pretend the sign-in page is not a document"}
	defer func() { notADocument = saved }()

	_, _, err := Targets([]string{"GET " + ui.SignInPath + " public"})
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
	saved, savedExpand := plainGET, linkExpanded
	plainGET = map[string]bool{"/landing": true, "/": true}
	// ⚠ `linkExpanded` IS EMPTIED FOR THE DURATION, because the root is in it now and a path
	// claimed by two classes is a REFUSAL — which is the previous test's subject, not this
	// one's. Overriding only `plainGET` would make this case fail on that refusal and say
	// nothing about whether the walk state is derived from the class.
	linkExpanded = map[string]bool{}
	defer func() { plainGET, linkExpanded = saved, savedExpand }()

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
