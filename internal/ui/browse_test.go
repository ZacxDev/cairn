package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/control/tokenfile"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/store"
)

// browseClock is the fixtures' instant. Year 2000, which is what `tests/leakscan.py`
// allows and what makes a synthetic date unambiguous. See `fixtures_test.go`.
var browseClock = time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)

// The two-scope world these guards need, and every name in it is INVENTED. This
// repository is public and was extracted from a private one.
var (
	browseReader   = control.DerivedID(control.PrefixUser, "browse-reader")
	browseOutsider = control.DerivedID(control.PrefixUser, "browse-outsider")
	browseProjectA = control.DerivedID(control.PrefixProject, "browse-a")
	browseProjectB = control.DerivedID(control.PrefixProject, "browse-b")
	browseScopeA   = control.DerivedID(control.PrefixScope, "alpha-notes")
	browseScopeB   = control.DerivedID(control.PrefixScope, "beta-notes")
)

// twoScopeWorld is one model holding TWO scopes in two projects, and two principals: one
// who reads scope A only, and one who reads scope B only.
//
// 🔴 TWO PRINCIPALS RATHER THAN ONE, BECAUSE THE POSITIVE CONTROL IS HALF THE GUARD. A
// refusal test with one principal cannot distinguish "this handler refuses what it should"
// from "this handler refuses everything" — and a handler that refused everything would
// satisfy every authority assertion in this file. The second principal is what makes the
// SAME id render 200, which is the only thing that turns the refusal into a measurement.
func twoScopeWorld(t *testing.T) (readsA, readsB identity.Identity) {
	t.Helper()
	at := browseClock
	m, err := control.Replay([]control.Event{
		{Kind: control.EventUserCreated, At: at, UserID: browseReader,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000031",
			Email: "reader@notes.example.invalid"},
		{Kind: control.EventUserCreated, At: at, UserID: browseOutsider,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000032",
			Email: "outsider@notes.example.invalid"},
		{Kind: control.EventProjectCreated, At: at, ProjectID: browseProjectA, Name: "alpha", UserID: browseReader},
		{Kind: control.EventMemberSet, At: at, ProjectID: browseProjectA, UserID: browseReader, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: at, ScopeID: browseScopeA, DisplayName: "alpha-notes", ProjectID: browseProjectA},
		{Kind: control.EventProjectCreated, At: at, ProjectID: browseProjectB, Name: "beta", UserID: browseOutsider},
		{Kind: control.EventMemberSet, At: at, ProjectID: browseProjectB, UserID: browseOutsider, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: at, ScopeID: browseScopeB, DisplayName: "beta-notes", ProjectID: browseProjectB},
	})
	if err != nil {
		t.Fatalf("building the two-scope world: %v", err)
	}
	resolve := func(id control.ID) identity.Identity {
		p, known := m.PrincipalFor(control.KindUser, id)
		if !known {
			t.Fatalf("the world does not hold principal %s", id)
		}
		return identity.Identity{Principal: p, Auth: control.Resolve(m, p)}
	}
	readsA, readsB = resolve(browseReader), resolve(browseOutsider)

	// INSTRUMENT CONTROL: the two authorities really are disjoint. Without this the
	// refusals below could be measuring a world in which nobody reads anything.
	if !readsA.Auth.Allows(browseScopeA, control.VerbRead) || readsA.Auth.Allows(browseScopeB, control.VerbRead) {
		t.Fatal("principal A does not read exactly scope A, so the refusal/permit pair below is not the pair " +
			"this test is named for")
	}
	if !readsB.Auth.Allows(browseScopeB, control.VerbRead) || readsB.Auth.Allows(browseScopeA, control.VerbRead) {
		t.Fatal("principal B does not read exactly scope B")
	}
	return readsA, readsB
}

// twoScopeStore writes the two scopes to disk, with one entry each.
//
// The entry bodies are synthetic and each carries a word the OTHER scope's entry does
// not, which is what makes the search-narrowing guard below measurable.
func twoScopeStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(scope, name, body string) {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("building the store: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("writing %s/%s: %v", scope, name, err)
		}
	}
	write("alpha-notes", "runbook.md", strings.Join([]string{
		"---",
		"service: runbook",
		"scope: alpha-notes",
		"aliases:",
		"  - rollout",
		"---",
		"",
		"## What it is",
		"",
		"The alpha rollout runbook.",
		"",
		store.PointersHeading,
		"",
		"See the deployment notes.",
		"",
		store.NuanceHeading,
		"",
		"- 2000-06-01 OPEN: the " + onlyInAlpha + " step is still unautomated",
		"  and nobody has claimed it.",
		"- 2000-05-01 OPEN the near miss, no colon after the marker",
		"- 2000-04-01 an ordinary bullet with no marker at all",
		"",
	}, "\n"))
	write("beta-notes", "ledger.md", strings.Join([]string{
		"---",
		"service: ledger",
		"scope: beta-notes",
		"---",
		"",
		"## What it is",
		"",
		"The beta ledger.",
		"",
		store.NuanceHeading,
		"",
		"- 2000-06-01 the " + onlyInBeta + " reconciliation runs nightly.",
		"",
	}, "\n"))
	return root
}

// The two discriminating words. Each appears in exactly ONE scope's entry BODY and in no
// ref, alias or heading — so a hit on one is a hit on a bullet, which is the property the
// search guard is about: `report.Search` finds text inside a line item, and a
// title-matching stand-in would not.
const (
	onlyInAlpha = "quarrying"
	onlyInBeta  = "kilnwork"
)

// browseServer wires the REAL `StoreSource` over a store on disk, so these guards measure
// the narrowing seam rather than a fixture that returns whatever it was handed.
func browseServer(t *testing.T, root string, id identity.Identity) *Server {
	t.Helper()
	cfg := testConfig(t, staticAuth{id})
	cfg.Source = StoreSource{Root: root}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	return srv
}

func getAs(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// TestTheBrowsePagesRefuseAnotherPrincipalsScopeWithTheSameBytesAsAnAbsentOne is the
// authority guard, in BOTH directions.
//
// 🔴 THE REFUSAL FOR "not yours" MUST BE BYTE-IDENTICAL TO THE REFUSAL FOR "does not
// exist", AND THAT IS NOT A STYLE RULE. A 404-for-unknown beside a 403-for-somebody-
// else's turns these pages into an existence oracle over every scope in the deployment:
// a caller holding one scope could enumerate the rest by watching which status came back.
// `scopeRefusal`'s comment records the same ruling for the share flow, and scope ids are
// unguessable by construction (`control.NewID` uses `crypto/rand`) precisely so this
// refusal is worth something.
//
// 🔴 AND THE POSITIVE CONTROL IS WHAT MAKES IT A MEASUREMENT. The SAME scope id that is
// refused for principal A is rendered 200 for principal B, and the same entry ref with it.
// Without that pair, a handler that refused every request would pass this test completely.
func TestTheBrowsePagesRefuseAnotherPrincipalsScopeWithTheSameBytesAsAnAbsentOne(t *testing.T) {
	readsA, readsB := twoScopeWorld(t)
	root := twoScopeStore(t)

	srvA := browseServer(t, root, readsA)

	// An id that exists in the model and belongs to somebody else.
	notYoursScope := getAs(t, srvA, ScopePath+"?"+QueryID+"="+string(browseScopeB))
	// An id that exists nowhere at all, minted the same way a real one is so it is
	// indistinguishable in shape.
	absent := control.DerivedID(control.PrefixScope, "no-such-scope-anywhere")
	if absent == browseScopeA || absent == browseScopeB {
		t.Fatal("the absent id collides with a real one, so the comparison below is vacuous")
	}
	absentScope := getAs(t, srvA, ScopePath+"?"+QueryID+"="+string(absent))

	if notYoursScope.Code != http.StatusNotFound {
		t.Errorf("GET %s for a scope the caller may not read answered %d, want 404: %s",
			ScopePath, notYoursScope.Code, notYoursScope.Body.String())
	}
	if notYoursScope.Code != absentScope.Code || notYoursScope.Body.String() != absentScope.Body.String() {
		t.Errorf("the refusals DIFFER: somebody else's scope answered %d %q, an absent one answered %d %q. "+
			"Any difference between them is an existence oracle over every scope in the deployment.",
			notYoursScope.Code, notYoursScope.Body.String(), absentScope.Code, absentScope.Body.String())
	}

	// The same pair for the entry page, including the case that only the entry page has:
	// a ref that IS real, in a scope the caller may not read.
	notYoursEntry := getAs(t, srvA, EntryPath+"?"+url.Values{
		QueryScope: []string{string(browseScopeB)}, QueryRef: []string{"ledger"},
	}.Encode())
	absentEntry := getAs(t, srvA, EntryPath+"?"+url.Values{
		QueryScope: []string{string(absent)}, QueryRef: []string{"ledger"},
	}.Encode())
	// And a ref that does not exist, in a scope the caller CAN read — the fourth way to
	// miss, and the one a handler is most likely to answer differently.
	absentRef := getAs(t, srvA, EntryPath+"?"+url.Values{
		QueryScope: []string{string(browseScopeA)}, QueryRef: []string{"no-such-entry"},
	}.Encode())
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"a real ref in somebody else's scope": notYoursEntry,
		"a real ref in an absent scope":       absentEntry,
		"an absent ref in the caller's scope": absentRef,
	} {
		if rec.Code != notYoursScope.Code || rec.Body.String() != notYoursScope.Body.String() {
			t.Errorf("%s answered %d %q; the scope refusal is %d %q. All four ways to miss must be one answer.",
				name, rec.Code, rec.Body.String(), notYoursScope.Code, notYoursScope.Body.String())
		}
	}

	// 🔴 THE POSITIVE CONTROL. The caller's OWN scope and entry render, so the refusals
	// above are not a handler that refuses everything.
	ownScope := getAs(t, srvA, ScopePath+"?"+QueryID+"="+string(browseScopeA))
	if ownScope.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED: the caller's OWN scope answered %d, want 200: %s. Every refusal "+
			"above is then satisfied by a handler that refuses everything.", ownScope.Code, ownScope.Body.String())
	}
	if !strings.Contains(ownScope.Body.String(), "alpha-notes") {
		t.Error("the caller's own scope page does not name the scope, so its 200 may not be the scope page")
	}
	ownEntry := getAs(t, srvA, EntryPath+"?"+url.Values{
		QueryScope: []string{string(browseScopeA)}, QueryRef: []string{"runbook"},
	}.Encode())
	if ownEntry.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED: the caller's OWN entry answered %d, want 200: %s",
			ownEntry.Code, ownEntry.Body.String())
	}
	if !strings.Contains(ownEntry.Body.String(), onlyInAlpha) {
		t.Error("the caller's own entry page does not carry its bullet text, so its 200 may not be the entry page")
	}

	// 🔴 AND THE MIRROR: the SAME id that A is refused renders 200 for B. This is the
	// control that proves the refusal is about the CALLER and not about the id.
	srvB := browseServer(t, root, readsB)
	mirror := getAs(t, srvB, ScopePath+"?"+QueryID+"="+string(browseScopeB))
	if mirror.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED: scope B answered %d for the principal who OWNS it, want 200: %s. "+
			"A's refusal for the same id is then a fact about the id, not about A.", mirror.Code, mirror.Body.String())
	}
	if strings.Contains(mirror.Body.String(), "alpha-notes") {
		t.Error("B's scope page names scope A, which B cannot read")
	}

	// 🔴 AND THE ROOT PAGE, WHICH IS WHERE A LOST NARROWING IS ACTUALLY VISIBLE. This
	// assertion is here because of a MEASURED mutant survival, not because it seemed
	// thorough: replacing `scopeSetOf(named)` with `store.Unrestricted()` in
	// `StoreSource.Visible` — the exact shape of forgetting the narrowing — left every
	// assertion above GREEN. The reason is worth writing down, because it is a second
	// guard doing work nobody credited it with: an unnarrowed load returns scope B's
	// directory, but `scopeIDsByFoldedName` is built from the AUTHORITY's `NamedScopes`,
	// so B's card gets an EMPTY id, and `pickScope` refuses an empty id. The scope page
	// therefore still refused — while the ROOT page listed the other tenant's scope name,
	// its entry refs and its bullet counts as an unlinked card. The refusal held and the
	// data leaked anyway, which is exactly the shape a refusal-only guard cannot see.
	rootAsA := getAs(t, srvA, RootPath)
	if rootAsA.Code != http.StatusOK {
		t.Fatalf("the root page answered %d for A, want 200; the narrowing assertions below are about a refusal",
			rootAsA.Code)
	}
	body := rootAsA.Body.String()
	if !strings.Contains(body, "alpha-notes") {
		t.Fatal("POSITIVE CONTROL FAILED: A's root page does not name A's OWN scope, so its silence about " +
			"B's below is a page with nothing on it rather than a narrowed one")
	}
	for _, leaked := range []string{"beta-notes", "ledger", onlyInBeta} {
		if strings.Contains(body, leaked) {
			t.Errorf("A's root page carries %q, which belongs to a scope A cannot read. The per-scope refusal "+
				"can still hold while this leaks: an unnarrowed load gives the foreign scope an empty id, so "+
				"its card renders UNLINKED — refused if you click it, and fully legible on the page.", leaked)
		}
	}

	t.Logf("authority: %s refused %d for 4 misses (identical bytes), 200 for A's own scope and entry; "+
		"the same id answered 200 for B; A's root page names alpha-notes and none of "+
		"[beta-notes ledger %s]", browseRefusal, notYoursScope.Code, onlyInBeta)
}

// TestSearchIsNarrowedByTheCallersAuthorityWithANonVacuousZero is the search guard, and
// it reports a PAIR rather than a zero.
//
// 🔴 A ZERO ALONE IS INDISTINGUISHABLE FROM A SEARCH WIRED TO NOTHING. The query below
// has exactly one hit in the whole store and that hit lives in scope B; run as A it must
// return nothing, and run as B it must return the hit. Only the second half says the
// engine is connected — without it, a `Search` that returned an empty report
// unconditionally would pass.
//
// ⚠ AND THE WORD IS IN A BULLET, NOT IN A REF OR A TITLE. `report.Search` scores lines,
// so a fixture whose discriminator was the entry's NAME would be satisfied by a
// title-matching stand-in and would say nothing about whether the real engine is behind
// the box.
func TestSearchIsNarrowedByTheCallersAuthorityWithANonVacuousZero(t *testing.T) {
	readsA, readsB := twoScopeWorld(t)
	root := twoScopeStore(t)
	src := StoreSource{Root: root}

	// INSTRUMENT CONTROL: the discriminating word really is in exactly one scope's file
	// and in no ref or heading, so the zero below is about authority and not about a word
	// that was never in the store.
	beta, err := os.ReadFile(filepath.Join(root, "beta-notes", "ledger.md"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	alpha, err := os.ReadFile(filepath.Join(root, "alpha-notes", "runbook.md"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	if !strings.Contains(string(beta), onlyInBeta) || strings.Contains(string(alpha), onlyInBeta) {
		t.Fatalf("INSTRUMENT CONTROL FAILED: %q is not in exactly one scope's file, so neither half of the "+
			"pair below measures narrowing", onlyInBeta)
	}

	asA, err := src.Search(readsA.Auth, onlyInBeta)
	if err != nil {
		t.Fatalf("searching as A: %v", err)
	}
	asB, err := src.Search(readsB.Auth, onlyInBeta)
	if err != nil {
		t.Fatalf("searching as B: %v", err)
	}

	if len(asB.Hits) == 0 {
		t.Fatalf("POSITIVE CONTROL FAILED: %q returned NO hit for the principal who can read the scope it "+
			"lives in. The zero for A below is then indistinguishable from a search wired to nothing. "+
			"(searched %v, best below %q)", onlyInBeta, asB.ScopesSearched, asB.BestBelow)
	}
	if len(asA.Hits) != 0 {
		t.Errorf("%q returned %d hit(s) for a principal who cannot read the only scope it appears in: %+v",
			onlyInBeta, len(asA.Hits), asA.Hits)
	}
	if got := asA.ScopesSearched; len(got) != 1 || got[0] != "alpha-notes" {
		t.Errorf("A's search walked %v, want exactly [alpha-notes]. The narrowing is an INDEX filter — "+
			"`report.Search`'s own ruling — so a wider set here means the engine read a scope A cannot see, "+
			"whether or not it reported a hit from it.", got)
	}
	if got := asB.ScopesSearched; len(got) != 1 || got[0] != "beta-notes" {
		t.Errorf("B's search walked %v, want exactly [beta-notes]", got)
	}

	// The hit is a BULLET hit and it carries the scope id, which is what lets the page
	// link it. A hit with no id renders as unlinkable text.
	hit := asB.Hits[0]
	if hit.ScopeID != browseScopeB {
		t.Errorf("the hit carries scope id %q, want %q; the page builds the entry link from it and an empty "+
			"one renders a result nobody can open", hit.ScopeID, browseScopeB)
	}
	if !strings.Contains(strings.Join(hit.Lines, "\n"), onlyInBeta) {
		t.Errorf("the hit's lines do not contain the query word, so this is not the hunk the query matched: %v", hit.Lines)
	}

	t.Logf("search narrowing: %q -> %d hit(s) as the principal who can read beta-notes, %d as the one who "+
		"cannot; scopes walked %v vs %v", onlyInBeta, len(asB.Hits), len(asA.Hits), asB.ScopesSearched, asA.ScopesSearched)
}

// TestTheRootPageSearchBoxDrivesTheEngineThroughTheRoute drives `?q=` through the real
// dispatcher, so the guard above is not only about a function nobody routes to.
func TestTheRootPageSearchBoxDrivesTheEngineThroughTheRoute(t *testing.T) {
	_, readsB := twoScopeWorld(t)
	root := twoScopeStore(t)
	srv := browseServer(t, root, readsB)

	hitPage := getAs(t, srv, RootPath+"?"+url.Values{QueryQuery: []string{onlyInBeta}}.Encode())
	if hitPage.Code != http.StatusOK {
		t.Fatalf("GET /?q= answered %d, want 200: %s", hitPage.Code, hitPage.Body.String())
	}
	if !strings.Contains(hitPage.Body.String(), onlyInBeta) {
		t.Error("the search page does not carry the matched line, so the `?q=` parameter reached no engine")
	}

	// A word that is in no entry at all: the page must say so in words rather than render
	// a blank, which is the whole reason `BestBelow` is carried onto the view.
	missPage := getAs(t, srv, RootPath+"?"+url.Values{QueryQuery: []string{"zzzznothingmatchesthis"}}.Encode())
	if missPage.Code != http.StatusOK {
		t.Fatalf("a no-match search answered %d, want 200", missPage.Code)
	}
	if !strings.Contains(missPage.Body.String(), "Nothing") {
		t.Error("a search with no match rendered no explanation; an empty result that says nothing cannot " +
			"be told from a search that did not run")
	}

	// An EMPTY `?q=` is not a search. It is how a reader clears one.
	cleared := getAs(t, srv, RootPath+"?"+QueryQuery+"=")
	if cleared.Code != http.StatusOK {
		t.Fatalf("GET /?q= (empty) answered %d, want 200", cleared.Code)
	}
	if strings.Contains(cleared.Body.String(), "Clear the search") {
		t.Error("an empty query rendered the results card. Submitting a blank box is how a reader clears a " +
			"search, and it must land on the ordinary root page.")
	}
	t.Logf("search through the route: a hit page carries the matched line, a miss page explains itself, an "+
		"empty %s renders the ordinary root", QueryQuery)
}

// TestScopeIdsAreAddressableOnATokenFileDeployment measures the claim the browse links
// rest on, on the deployment shape where it is least obvious.
//
// 🔴 THIS REPOSITORY HAS ALREADY SHIPPED A GUARD THAT WAS STRUCTURALLY UNREACHABLE ON A
// TOKEN-FILE DEPLOYMENT AND ONLY NOTICED VIA A SKIPPED TEST, so "scope ids exist there
// too" is measured rather than assumed. `tokenfile.Source` mints `scopeID(name)` for every
// scope it projects, so a `?id=` link is reachable against a token file exactly as it is
// against a control journal — which is not true of the SHARE flow, where the same
// deployment grants no `admin` verb to anybody and the index legitimately publishes
// nothing.
//
// ⚠ IT ASSERTS THE IDS ARE PRESENT AND DISTINCT, NOT THEIR VALUES. The derivation is
// `control.DerivedID` over the folded name and pinning a literal here would be a second
// spelling of it.
func TestScopeIdsAreAddressableOnATokenFileDeployment(t *testing.T) {
	root := twoScopeStore(t)
	// A BARE (legacy) row: one credential, unrestricted over whatever scopes the store
	// holds. Synthetic, and it is not a real token — see `fixtures_test.go` for the rule.
	const fixtureToken = "fixture-token-not-a-real-credential"
	src := tokenfile.Source{
		StoreRoot: root,
		Records:   func() []authz.TokenRecord { return []authz.TokenRecord{authz.LegacyRecord(fixtureToken)} },
		Now:       func() time.Time { return browseClock },
	}
	m, err := src.Model(t.Context())
	if err != nil {
		t.Fatalf("projecting the token file: %v", err)
	}

	p, auth, err := control.Authenticate(m, fixtureToken)
	if err != nil {
		t.Fatalf("resolving the token file's own credential: %v", err)
	}
	named := auth.NamedScopes(control.VerbRead)
	if len(named) == 0 {
		t.Fatal("the token-file projection named NO readable scope, so this test measures nothing about ids")
	}

	seen := map[control.ID]string{}
	for _, n := range named {
		if n.ID == "" {
			t.Errorf("scope %q has an EMPTY id on a token-file deployment, so `%s?%s=` cannot address it and "+
				"its card renders unlinked", n.Name, ScopePath, QueryID)
			continue
		}
		if other, dup := seen[n.ID]; dup {
			t.Errorf("scopes %q and %q share id %q, so one link opens the other's page", n.Name, other, n.ID)
		}
		seen[n.ID] = n.Name
	}

	// And the id is addressable END TO END, not merely present: the page renders.
	srv := browseServer(t, root, identity.Identity{Principal: p, Auth: auth})
	reached := 0
	for _, n := range named {
		rec := getAs(t, srv, ScopePath+"?"+QueryID+"="+string(n.ID))
		if rec.Code != http.StatusOK {
			t.Errorf("%s?%s=%s (scope %q, token-file deployment) answered %d, want 200: %s",
				ScopePath, QueryID, n.ID, n.Name, rec.Code, rec.Body.String())
			continue
		}
		if !strings.Contains(rec.Body.String(), n.Name) {
			t.Errorf("the page for scope %q does not name it, so its 200 may be a different page", n.Name)
		}
		reached++
	}
	if reached == 0 {
		t.Fatal("NO scope page was reached on the token-file deployment, so every assertion above is vacuous")
	}
	t.Logf("token-file deployment: %d scope(s) projected with distinct non-empty ids, %d addressable at %s?%s=",
		len(named), reached, ScopePath, QueryID)
}

// TestTheGeneratedStylesheetHasNoColourSchemePreference is the STRUCTURAL form of
// "dark always".
//
// 🔴 IT ASSERTS THE STATE AND NOT A SPELLING. A guard reading "the stylesheet contains
// `oklch(0.21`" would pass a tree that had re-added a light branch under a different
// token, and a guard reading "it contains the word dark" is walkable by anybody who names
// a class `dark-mode`. The state a reader on a light-mode machine depends on is that
// NOTHING in the served bytes switches on their system preference — so the assertion is
// that the query is absent, in the GENERATED output rather than in the source, because
// the generated output is what `//go:embed` ships.
//
// ⚠ IT IS AN INVARIANT GUARD FOR THE REDUCED-MOTION HALF AND A REGRESSION GUARD FOR THE
// COLOUR HALF. The colour query was REAL in the tree before this change and is measured
// gone; `prefers-reduced-motion` was never a defect and is asserted PRESENT only so this
// test cannot be satisfied by a stylesheet that lost every media query at once, which is
// what a broken generator would produce.
func TestTheGeneratedStylesheetHasNoColourSchemePreference(t *testing.T) {
	if len(stylesheet) == 0 {
		t.Fatal("the embedded stylesheet is EMPTY, so an absence in it says nothing")
	}
	if n := strings.Count(stylesheet, "prefers-color-scheme"); n != 0 {
		t.Errorf("the served stylesheet carries %d `prefers-color-scheme` block(s). This surface is dark "+
			"ALWAYS by operator decision: there is no light palette and no toggle, so a reader's system "+
			"preference must not change what they get. Delete the query rather than inverting it.", n)
	}
	// POSITIVE CONTROL ON THE SCAN: a media query that MUST be there is there, so the
	// zero above is a fact about the colour query and not about a lookup that finds
	// nothing, an empty file, or a generator that emitted no `@media` at all.
	if n := strings.Count(stylesheet, "prefers-reduced-motion"); n == 0 {
		t.Fatal("POSITIVE CONTROL FAILED: the stylesheet carries NO `prefers-reduced-motion` block either, " +
			"so the zero above is indistinguishable from a scan that cannot see a media query")
	}
	// And the palette is really in the theme rather than behind some other condition: the
	// dark surface colour is declared unconditionally.
	if !strings.Contains(stylesheet, "--color-surface:") {
		t.Error("the stylesheet declares no `--color-surface`, so every page resolves it to nothing and the " +
			"absence of a media query above is an absence of a theme")
	}
	// 🔴 THE LOG PRINTS THE MEASURED COUNT, NOT THE EXPECTED ONE. A draft of this line
	// hard-coded the `0` and printed "0 prefers-color-scheme blocks" on the very run that
	// had just FAILED reporting one — a log line that contradicts the assertion beside it
	// is worse than no log, because the log is what gets pasted into a report.
	t.Logf("dark-always: %d prefers-color-scheme block(s), %d prefers-reduced-motion block(s) as the "+
		"positive control, over %d bytes of generated stylesheet",
		strings.Count(stylesheet, "prefers-color-scheme"),
		strings.Count(stylesheet, "prefers-reduced-motion"), len(stylesheet))
}

// TestAnEntryRefIsEncodedOnTheWayOutAndMatchedOnTheWayIn is the guard on the one piece of
// USER TEXT that reaches a URL position on this surface.
//
// 🔴 A REF IS A FILENAME STEM OUT OF A FILE SOMEBODY ELSE WROTE, NOT A MINTED ID. It can
// carry `&`, `#`, a space, a `+` or a percent sequence, every one of which changes what a
// query string MEANS if it is concatenated rather than encoded. The round trip is the
// claim: the link a page renders for an entry must be a link that resolves back to that
// entry and to no other.
func TestAnEntryRefIsEncodedOnTheWayOutAndMatchedOnTheWayIn(t *testing.T) {
	scope := control.DerivedID(control.PrefixScope, "encode-fixture")
	hostile := []string{
		`plain`,
		`with space`,
		`amp&ref=other`,
		`hash#frag`,
		`plus+sign`,
		`percent%2Fencoded`,
		`quote"and<angle>`,
	}
	for _, ref := range hostile {
		href := entryHref(scope, ref)
		u, err := url.Parse(href)
		if err != nil {
			t.Errorf("entryHref(%q) produced %q, which is not a parseable URL", ref, href)
			continue
		}
		if u.Path != EntryPath {
			t.Errorf("entryHref(%q) points at path %q, want %q; the ref escaped its parameter", ref, u.Path, EntryPath)
		}
		q := u.Query()
		if got := q.Get(QueryRef); got != ref {
			t.Errorf("entryHref(%q) round-tripped to %q", ref, got)
		}
		if got := q.Get(QueryScope); got != string(scope) {
			t.Errorf("entryHref(%q) round-tripped scope %q, want %q — the ref injected a second parameter",
				ref, got, scope)
		}
		// NEGATIVE CONTROL on the hazard itself: the naive concatenation this function
		// exists instead of really does break for the `&` case, so the round trip above
		// is not trivially true of any implementation.
		if strings.Contains(ref, "&") {
			naive := EntryPath + "?" + QueryScope + "=" + string(scope) + "&" + QueryRef + "=" + ref
			nu, parseErr := url.Parse(naive)
			if parseErr == nil && nu.Query().Get(QueryRef) == ref {
				t.Errorf("NEGATIVE CONTROL FAILED: naive concatenation ALSO round-trips %q, so the encoding "+
					"above is not what makes the round trip hold", ref)
			}
		}
	}
	t.Logf("ref encoding: %d ref(s) round-tripped through entryHref, including the `&` case a naive "+
		"concatenation loses", len(hostile))
}
