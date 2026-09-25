package ui

import (
	"context"
	"html"
	"io"
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
)

// The share world. SYNTHETIC in every part — see `fixtures_test.go` for the rule this
// repository applies to fixtures and why.
//
// 🔴 THE TWO USERS SHARE THE `commons` PROJECT AND NEITHER IS A MEMBER OF THE OTHER'S.
// That is not scene-setting: `ControlSharing.Candidates` offers only principals the
// actor already shares a project with, so a fixture where the two had nothing in common
// could not exercise the share flow at all. Rowan owns `quarry` and its scope; wren has
// no authority over it whatsoever until the grant under test.
var (
	shareRowan     = control.DerivedID(control.PrefixUser, "share-rowan")
	shareWren      = control.DerivedID(control.PrefixUser, "share-wren")
	shareQuarry    = control.DerivedID(control.PrefixProject, "share-quarry")
	shareCommons   = control.DerivedID(control.PrefixProject, "share-commons")
	shareScope     = control.DerivedID(control.PrefixScope, "share-quarry-notes")
	shareScopeName = "quarry-notes"
	// A second scope in a project rowan does NOT administer, for the tests that need
	// two scopes to tell an authority check from a blanket refusal.
	shareOtherScope     = control.DerivedID(control.PrefixScope, "share-commons-notes")
	shareOtherScopeName = "commons-notes"

	// A principal in NEITHER project, so it is a real subject that is not a candidate
	// — the only shape that can measure the candidate check. See the test that uses it.
	shareStranger        = control.DerivedID(control.PrefixUser, "share-stranger")
	shareStrangerProject = control.DerivedID(control.PrefixProject, "share-elsewhere")

	shareRowanToken = "fixture-rowan-credential-which-is-not-a-real-token"
	shareWrenToken  = "fixture-wren-credential-which-is-not-a-real-token"

	shareClock = time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
)

// readOnlyToken is the credential the token-file rig authenticates. SYNTHETIC, and long
// enough to clear `authz.MinTokenChars` — the loader refuses a short token as guessable.
const readOnlyToken = "fixture-token-file-credential-which-is-not-a-real-token-0000"

// shareWorld is the events the fixture journal holds before any test acts.
func shareWorld() []control.Event {
	at := shareClock
	return []control.Event{
		{Kind: control.EventUserCreated, At: at, UserID: shareRowan,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000011",
			Email: "rowan@notes.example.invalid"},
		{Kind: control.EventUserCreated, At: at, UserID: shareWren,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000012",
			Email: "wren@notes.example.invalid"},

		{Kind: control.EventProjectCreated, At: at, ProjectID: shareQuarry, Name: "quarry", UserID: shareRowan},
		{Kind: control.EventMemberSet, At: at, ProjectID: shareQuarry, UserID: shareRowan, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: at, ScopeID: shareScope,
			DisplayName: shareScopeName, ProjectID: shareQuarry},

		// The project the two have in common. Rowan is a MEMBER here — read and write,
		// no admin — which is what makes `shareOtherScope` a scope rowan can SEE and
		// cannot ADMINISTER. Several tests below turn on exactly that distinction, and
		// a scope rowan could not see at all would be a weaker probe: it would be
		// refused by a narrowing rather than by the sharing authority check.
		{Kind: control.EventProjectCreated, At: at, ProjectID: shareCommons, Name: "commons", UserID: shareWren},
		{Kind: control.EventMemberSet, At: at, ProjectID: shareCommons, UserID: shareWren, Role: control.RoleOwner},
		{Kind: control.EventMemberSet, At: at, ProjectID: shareCommons, UserID: shareRowan, Role: control.RoleMember},
		{Kind: control.EventScopeCreated, At: at, ScopeID: shareOtherScope,
			DisplayName: shareOtherScopeName, ProjectID: shareCommons},

		// The stranger, with their own project and no membership either user shares.
		{Kind: control.EventUserCreated, At: at, UserID: shareStranger,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000013",
			Email: "stranger@notes.example.invalid"},
		{Kind: control.EventProjectCreated, At: at, ProjectID: shareStrangerProject,
			Name: "elsewhere", UserID: shareStranger},
		{Kind: control.EventMemberSet, At: at, ProjectID: shareStrangerProject,
			UserID: shareStranger, Role: control.RoleOwner},

		{Kind: control.EventCredentialIssued, At: at, CredentialID: "crd_share_rowan",
			SubjectKind: control.KindUser, SubjectID: shareRowan,
			TokenHash: control.HashToken(shareRowanToken), Label: "fixture-rowan"},
		{Kind: control.EventCredentialIssued, At: at, CredentialID: "crd_share_wren",
			SubjectKind: control.KindUser, SubjectID: shareWren,
			TokenHash: control.HashToken(shareWrenToken), Label: "fixture-wren"},
	}
}

// shareRig is a whole running surface: a real control journal, a real store on disk, a
// real session table, and the real dispatcher.
//
// 🔴 NOTHING IN IT IS STUBBED, AND THAT IS THE POINT RATHER THAN THOROUGHNESS FOR ITS
// OWN SAKE. The clause this file exists to satisfy is "a scope granted from one user to
// another, SERVED THROUGH THE BROWSER". A rig with a fixture `Source` would prove the
// authority moved and say nothing about whether the page the second user loads lists
// the scope — and a defect that lives in the seam between the control plane and the
// store reader is exactly the kind this repository has already shipped twice.
type shareRig struct {
	t         *testing.T
	srv       *Server
	authority *control.Cache
	storeRoot string
}

func newShareRig(t *testing.T) *shareRig {
	t.Helper()
	return newShareRigWithAuthority(t, openShareJournal(t))
}

// openShareJournal builds the writable authority: a real `control.FileStore` with the
// fixture world appended.
func openShareJournal(t *testing.T) *control.Cache {
	t.Helper()
	store, err := control.OpenFileStore(filepath.Join(t.TempDir(), "control.journal"))
	if err != nil {
		t.Fatalf("opening the control journal: %v", err)
	}
	if _, err := store.Append(context.Background(), shareWorld()...); err != nil {
		t.Fatalf("seeding the control journal: %v", err)
	}
	cache := control.NewCache(store, control.CacheOptions{})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing the control journal: %v", err)
	}
	return cache
}

func newShareRigWithAuthority(t *testing.T, authority *control.Cache) *shareRig {
	t.Helper()
	rig := &shareRig{t: t, authority: authority, storeRoot: buildShareStore(t)}

	sessions := mustSessions(t)
	// 🔴 ONE CLOCK FOR THE STORE AND THE SERVER, OR EVERY SESSION IS BORN EXPIRED. The
	// server stamps a session's expiry from `Config.Now` and the store decides whether
	// it is live from its OWN clock; leaving the store's at the wall clock while the
	// server mints at the fixture instant made every sign-in succeed and every
	// subsequent request answer 401 — measured, not reasoned about.
	sessions.Now = func() time.Time { return shareClock }
	cookie, err := identity.NewCookieSession(sessions, authority)
	if err != nil {
		t.Fatalf("the cookie backend did not build: %v", err)
	}
	machine, err := identity.NewMachineToken(authority)
	if err != nil {
		t.Fatalf("the machine-token backend did not build: %v", err)
	}
	chain, err := AuthBackends(machine, nil, cookie)
	if err != nil {
		t.Fatalf("the chain did not build: %v", err)
	}
	srv, err := New(Config{
		Auth:        chain,
		Credentials: authority,
		Source:      StoreSource{Root: rig.storeRoot},
		Sharing:     ControlSharing{Authority: authority, Now: func() time.Time { return shareClock }},
		Sessions:    sessions,
		Now:         func() time.Time { return shareClock },
		Log:         io.Discard,
	})
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	rig.srv = srv
	return rig
}

// buildShareStore writes a real store: one directory per scope, one entry in each.
func buildShareStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for scope, service := range map[string]string{
		shareScopeName:      "gadget-one",
		shareOtherScopeName: "widget-three",
	} {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nservice: " + service + "\nscope: " + scope +
			"\n---\n\n## Nuance / work-history\n- 2000-01-02: a note.\n"
		if err := os.WriteFile(filepath.Join(dir, service+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// browser is one signed-in caller, holding its cookie the way a browser would.
type browser struct {
	rig    *shareRig
	cookie *http.Cookie
}

// signIn drives the REAL sign-in exchange rather than synthesising a session.
func (r *shareRig) signIn(token string) *browser {
	r.t.Helper()
	form := url.Values{}
	form.Set(FieldToken, token)
	req := httptest.NewRequest(http.MethodPost, SignInPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://"+req.Host)
	rec := httptest.NewRecorder()
	r.srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		r.t.Fatalf("sign-in answered %d, want 303; the rest of this test would measure an unauthenticated "+
			"caller. Body: %q", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == identity.SessionCookieName && c.Value != "" {
			return &browser{rig: r, cookie: c}
		}
	}
	r.t.Fatal("sign-in set no session cookie, so no request below would carry a session")
	return nil
}

func (b *browser) get(path string) *httptest.ResponseRecorder {
	b.rig.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(b.cookie)
	rec := httptest.NewRecorder()
	b.rig.srv.ServeHTTP(rec, req)
	return rec
}

// post sends a state-changing request with BOTH cross-site gates satisfied, so what it
// measures is the handler rather than gate (2) or gate (6).
func (b *browser) post(path string, form url.Values) *httptest.ResponseRecorder {
	b.rig.t.Helper()
	form.Set(FieldCSRF, identity.CSRFTokenFor(b.cookie.Value))
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://"+req.Host)
	req.AddCookie(b.cookie)
	rec := httptest.NewRecorder()
	b.rig.srv.ServeHTTP(rec, req)
	return rec
}

// sharePath is the share page for one scope.
func sharePath(scope control.ID) string {
	return SharePath + "?" + QueryScope + "=" + string(scope)
}

// TestAScopeSharedFromOneUserToAnotherIsServedThroughTheBrowser is THE CLAUSE — the
// arc's closing condition names this flow by name, and this is it end to end.
//
// 🔴 IT MEASURES THE BEFORE STATE FIRST, AND THAT IS WHAT MAKES THE AFTER STATE
// EVIDENCE. "Wren can see the scope" is satisfied by a surface that shows every scope
// to everybody; only "wren could NOT see it, then rowan shared it, then wren could"
// distinguishes a working share from a broken authority. The before read is therefore
// an assertion and not a setup step.
//
// ⚠ IT IS A NEW-FEATURE GUARD, NOT A REGRESSION TEST, AND LABELLING IT IS THIS
// REPOSITORY'S RULE. There is no pre-change tree on which it fails for the right
// reason: before this commit the route it drives did not exist, so it fails with a 401
// from the ledger rather than by observing a broken share. What it pins going forward
// is the whole path — journal write, cache materialization, `Resolve`, the store
// narrowing and the rendered page.
func TestAScopeSharedFromOneUserToAnotherIsServedThroughTheBrowser(t *testing.T) {
	rig := newShareRig(t)
	rowan := rig.signIn(shareRowanToken)
	wren := rig.signIn(shareWrenToken)

	// BEFORE: wren's own page does not carry the scope, and wren's own authority does
	// not reach it. Both are asserted — the page could omit it for a rendering reason
	// and the authority is the thing under test.
	before := wren.get(RootPath)
	if before.Code != http.StatusOK {
		t.Fatalf("wren's page answered %d before the share; the comparison below needs a served page", before.Code)
	}
	if strings.Contains(before.Body.String(), shareScopeName) {
		t.Fatalf("wren's page ALREADY lists %q before any share was recorded, so this test cannot tell a "+
			"working share from a surface that shows every scope to everybody", shareScopeName)
	}

	// The share page rowan actually uses. Wren must be offered as a candidate, or the
	// flow below is driving a form the browser would never render.
	page := rowan.get(sharePath(shareScope))
	if page.Code != http.StatusOK {
		t.Fatalf("rowan's share page answered %d, want 200: %s", page.Code, page.Body.String())
	}
	if !strings.Contains(page.Body.String(), "wren@notes.example.invalid") {
		t.Fatal("wren is not offered on rowan's share page, so the POST below drives a form no browser " +
			"would have rendered and this test would pass against an unusable UI")
	}

	// THE SHARE.
	form := url.Values{}
	form.Set(FieldScope, string(shareScope))
	form.Set(FieldSubject, string(shareWren))
	form.Set(FieldVerb, string(control.VerbRead))
	wrote := rowan.post(SharePath, form)
	if wrote.Code != http.StatusSeeOther {
		t.Fatalf("the share answered %d, want 303: %s", wrote.Code, wrote.Body.String())
	}

	// AFTER: wren's page carries the scope AND its entry. The entry matters — a page
	// listing a scope heading with no entries would mean the authority moved and the
	// store narrowing did not.
	after := wren.get(RootPath)
	if after.Code != http.StatusOK {
		t.Fatalf("wren's page answered %d after the share: %s", after.Code, after.Body.String())
	}
	if !strings.Contains(after.Body.String(), shareScopeName) {
		t.Errorf("wren's page does NOT list %q after rowan shared it. The grant is in the journal and the "+
			"cache was materialized before the redirect, so a miss here is the seam between the control "+
			"plane and the store reader.\n%s", shareScopeName, after.Body.String())
	}
	if !strings.Contains(after.Body.String(), "gadget-one") {
		t.Errorf("wren's page lists the scope but not its entry %q; the authority moved and the store "+
			"narrowing did not follow it", "gadget-one")
	}

	// And the audience rowan now sees names wren, which is the same fact read back
	// through the page that recorded it.
	audience := rowan.get(sharePath(shareScope))
	if !strings.Contains(audience.Body.String(), "wren@notes.example.invalid") {
		t.Error("rowan's share page does not list wren in the audience after sharing with them")
	}

	t.Logf("share flow: wren 0 scopes before, %q + entry after; audience names wren", shareScopeName)
}

// TestTheAudienceIsComputedFromResolveNotFromGrantRows is the guard on the one
// instruction this flow was built under.
//
// 🔴 A GRANT-ROW LISTING UNDER-REPORTS EVERY PROJECT MEMBER, AND IT UNDER-REPORTS IN
// THE DIRECTION THAT TELLS SOMEBODY THEIR NOTES ARE MORE PRIVATE THAN THEY ARE.
// Authority arrives two ways — `control.Resolve`'s own comment enumerates them — so a
// page that answered "who has access to this" from `Model.Grants` would show a short,
// plausible, wrong list. Rowan reaches `quarry-notes` through OWNERSHIP of the project
// that holds it, with no grant row naming rowan anywhere in the journal; this test
// asserts rowan is in the audience and that the grant table is genuinely empty, so the
// two cannot both be satisfied by an implementation reading grants.
//
// 🔴 MUTATION-CHECKED: replacing `Audience`'s body with a loop over `m.Grants` makes
// the first assertion below fail with `audience=[]`, which is the defect this test
// exists for. The second assertion is what makes the first non-vacuous — without it,
// an implementation that read grants AND happened to have a grant row would pass.
func TestTheAudienceIsComputedFromResolveNotFromGrantRows(t *testing.T) {
	authority := openShareJournal(t)
	sharing := ControlSharing{Authority: authority, Now: func() time.Time { return shareClock }}

	// INSTRUMENT CONTROL: the grant table is EMPTY for this scope, so anything the
	// audience reports about it came from somewhere other than a grant row.
	revocable, err := sharing.Revocable(shareScope)
	if err != nil {
		t.Fatalf("reading the grant rows: %v", err)
	}
	if len(revocable) != 0 {
		t.Fatalf("the fixture already holds %d grant row(s) on %s, so an audience computed FROM grant rows "+
			"could pass this test. The whole point is a viewer with no grant.", len(revocable), shareScope)
	}

	audience, err := sharing.Audience(shareScope)
	if err != nil {
		t.Fatalf("reading the audience: %v", err)
	}
	var rowan *Viewer
	for i := range audience {
		if audience[i].Display == "rowan@notes.example.invalid" {
			rowan = &audience[i]
		}
	}
	if rowan == nil {
		t.Fatalf("rowan is NOT in the audience for %s, and rowan OWNS the project that holds it. That is "+
			"exactly the under-report a grant-row listing produces: authority arrives two ways and this "+
			"list saw one of them. audience=%+v", shareScopeName, audience)
	}
	if !rowan.ByMembership {
		t.Error("rowan is in the audience but is not marked as reaching it by membership, so the page would " +
			"offer a revoke button for authority no grant confers and no button can remove")
	}
	if rowan.Verbs != "read,write,admin" {
		t.Errorf("rowan's verbs are %q, want the owner role's full set; a narrowed answer here would mean "+
			"the membership branch resolved a role it does not hold", rowan.Verbs)
	}

	// Wren must NOT be in it — a test that only asserts presence passes against an
	// implementation that returns every principal in the model.
	for _, v := range audience {
		if v.Display == "wren@notes.example.invalid" {
			t.Errorf("wren is in the audience for %s with no membership and no grant; this list is wider "+
				"than the authority", shareScopeName)
		}
	}
	t.Logf("audience for %s: %d viewer(s) from Resolve, 0 grant rows in the table", shareScopeName, len(audience))
}

// TestTheReplicaHonestyNoticeIsPinnedWhole pins the notice as ONE NORMALISED STRING on
// every shape of the share page.
//
// 🔴 WHOLE-STRING, NOT KEYWORD, AND THE NEGATIVE CONTROL BELOW IS WHAT MAKES THAT A
// MEASUREMENT. A guard asserting the page mentions "cache" survives a reword that has
// quietly dropped the clause about entries already copied onto somebody's machine —
// the clause that makes the product sound weakest and is therefore the one a
// well-meaning edit removes. The cost is that any cosmetic reword reds this test, which
// is the intended cost: what this surface promises is a claim, and a claim gets a gate.
//
// ⚠ AN INVARIANT GUARD, NOT A REGRESSION TEST. No prior tree had a notice for it to
// catch the loss of.
func TestTheReplicaHonestyNoticeIsPinnedWhole(t *testing.T) {
	rig := newShareRig(t)
	rowan := rig.signIn(shareRowanToken)

	want := normalizeSpace(ReplicaHonesty)
	if want == "" {
		t.Fatal("ReplicaHonesty is EMPTY, so every comparison below is vacuous")
	}

	for _, page := range []struct {
		name string
		path string
	}{
		{"the share index", SharePath},
		{"one scope's share page", sharePath(shareScope)},
	} {
		rec := rowan.get(page.path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s answered %d, so the notice below was not measured", page.name, rec.Code)
			continue
		}
		if got := pageText(rec.Body.String()); !strings.Contains(got, want) {
			t.Errorf("%s does not carry the replica-honesty notice as a whole string.\nwant: %q\n"+
				"The notice is pinned entire rather than by keyword because a reword that drops one "+
				"clause is exactly what this guard is for. If the wording changed on purpose, change "+
				"`ReplicaHonesty` and this test together.", page.name, want)
		}
	}

	// 🔴 NEGATIVE CONTROL: the comparison can FAIL. Without it a `Contains` that always
	// matched — an empty needle, a normaliser that returned "" — would report both
	// pages green while measuring nothing.
	rec := rowan.get(sharePath(shareScope))
	mutated := strings.Replace(want, "does not recall entries already copied", "recalls entries already copied", 1)
	if mutated == want {
		t.Fatal("NEGATIVE CONTROL FAILED TO BUILD: the clause it inverts is not in the notice, so the " +
			"control below asserts nothing about the comparison")
	}
	if strings.Contains(pageText(rec.Body.String()), mutated) {
		t.Error("NEGATIVE CONTROL FAILED: the page matched a notice with one clause INVERTED, so the " +
			"comparison above cannot tell the real notice from a reworded one")
	}

	// And the sign-in page must NOT carry it: a notice on every page is a notice
	// nobody reads, and this one qualifies an authority answer that page does not make.
	plain := httptest.NewRecorder()
	rig.srv.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, SignInPath, nil))
	if strings.Contains(pageText(plain.Body.String()), want) {
		t.Error("the sign-in page carries the replica-honesty notice; it makes no authority claim to qualify")
	}
}

// normalizeSpace collapses every run of whitespace to one space and trims.
//
// It exists because the pinned string lives in Go source wrapped across lines while the
// page renders it as one run of text — comparing the two without normalising would pin
// the SOURCE FORMATTING of a constant, which is a guard that reds on `gofmt`.
func normalizeSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// pageText is what a READER sees: the body with HTML entities resolved and whitespace
// collapsed.
//
// 🔴 THE UNESCAPE IS NECESSARY AND IT WEAKENS THE COMPARISON IN ONE NAMED DIRECTION,
// WHICH IS WHY IT IS SAID HERE. gomponents escapes the apostrophes in the notice, so a
// comparison against the raw body would be pinning `&#39;` — the ESCAPER's current
// spelling — rather than the sentence, and would red on an escaper change that harmed
// nobody. Resolving entities first pins the sentence. What it gives up: text that is
// entity-escaped in the SOURCE could in principle unescape into a match. Nothing on
// this page can supply that — the share page renders no entry content, and every
// string on it comes from the control plane or from this package — but the direction
// of the weakening is stated rather than left for somebody to discover.
func pageText(body string) string { return normalizeSpace(html.UnescapeString(body)) }

// TestTheSharePageRefusesAScopeThisCallerCannotAdminister pins the authority check AND
// that its refusal does not discriminate.
//
// 🔴 THE UNKNOWN SCOPE AND THE UNAUTHORISED ONE MUST ANSWER IDENTICALLY, STATUS AND
// BODY. A 404 for one beside a 403 for the other turns this page into an existence
// oracle: a caller with admin on any scope could probe for others. Rowan can SEE
// `commons-notes` (a reader in that project) and cannot ADMINISTER it, which makes it a
// sharper probe than a scope rowan cannot see at all.
func TestTheSharePageRefusesAScopeThisCallerCannotAdminister(t *testing.T) {
	rig := newShareRig(t)
	rowan := rig.signIn(shareRowanToken)

	// POSITIVE CONTROL: the page does serve SOMETHING, so the refusals below are about
	// the scope rather than about a page that refuses everybody.
	if ok := rowan.get(sharePath(shareScope)); ok.Code != http.StatusOK {
		t.Fatalf("rowan cannot open the share page for a scope rowan owns (%d); every refusal below would "+
			"then be meaningless", ok.Code)
	}

	unauthorised := rowan.get(sharePath(shareOtherScope))
	unknown := rowan.get(sharePath("scp_this-id-names-nothing"))
	if unauthorised.Code != http.StatusNotFound {
		t.Errorf("a scope rowan may read but not administer answered %d, want 404", unauthorised.Code)
	}
	if unknown.Code != unauthorised.Code || unknown.Body.String() != unauthorised.Body.String() {
		t.Errorf("an UNKNOWN scope answered %d %q and an UNAUTHORISED one answered %d %q. They must be "+
			"indistinguishable, or this page enumerates the scopes of a deployment for anybody who can "+
			"administer one.", unknown.Code, unknown.Body.String(), unauthorised.Code, unauthorised.Body.String())
	}

	// The write path takes the same ruling.
	form := url.Values{}
	form.Set(FieldScope, string(shareOtherScope))
	form.Set(FieldSubject, string(shareWren))
	form.Set(FieldVerb, string(control.VerbRead))
	if wrote := rowan.post(SharePath, form); wrote.Code != http.StatusForbidden {
		t.Errorf("sharing a scope rowan cannot administer answered %d, want 403: %s",
			wrote.Code, wrote.Body.String())
	}

	// 🔴 AND THE SAME REQUEST WITH NO VERB, WHICH IS WHAT DISTINGUISHES THE HANDLER'S
	// AUTHORITY CHECK FROM THE ONE INSIDE `ControlSharing.Share`. The two look redundant
	// and are not: the handler's runs BEFORE the form is validated, so an unauthorised
	// caller is refused without learning anything about their input. Delete it and this
	// request reaches the verb validation instead — measured **400** where the authorised
	// answer is **403** — which tells a caller who may not touch this scope that their
	// verb field was the problem. That is a small disclosure and an exact discriminator,
	// and it is why the mutation row for this check is a KILLABLE one rather than the
	// EQUIVALENT it was first labelled. The label came first, the measurement second, and
	// the measurement won.
	noVerb := url.Values{}
	noVerb.Set(FieldScope, string(shareOtherScope))
	noVerb.Set(FieldSubject, string(shareWren))
	if wrote := rowan.post(SharePath, noVerb); wrote.Code != http.StatusForbidden {
		t.Errorf("sharing a scope rowan cannot administer, with NO verb, answered %d, want 403. A 400 here "+
			"means the authority check ran AFTER the form validation, so an unauthorised caller learns "+
			"which part of their request was malformed.", wrote.Code)
	}
}

// TestTheSubjectIsValidatedAgainstCandidatesRatherThanAcceptedFromTheForm pins the one
// check a rendered `select` cannot make.
//
// 🔴 THE REFUSED SUBJECT IS A REAL PRINCIPAL, WHICH IS WHAT MAKES THIS A TEST. Posting
// a nonexistent id would be refused by `Event.validate` at the journal boundary, so a
// surface with NO candidate check at all would still look safe against that probe. The
// subject here is a user this control plane holds, with their own project, who shares
// no project with rowan — which is precisely the principal `Candidates` withholds and
// precisely the one a caller must not be able to widen a scope to by typing an id.
func TestTheSubjectIsValidatedAgainstCandidatesRatherThanAcceptedFromTheForm(t *testing.T) {
	rig := newShareRig(t)
	rowan := rig.signIn(shareRowanToken)

	// INSTRUMENT CONTROL: the id under test really is a principal this model holds, so
	// the refusal below is the candidate check and not "no such subject".
	sharing := ControlSharing{Authority: rig.authority}
	if _, known := rig.authority.Model().Users[shareStranger]; !known {
		t.Fatal("the fixture does not hold the user this test posts, so the refusal below would be " +
			"about an unknown id rather than about the candidate check")
	}
	candidates, err := sharing.Candidates(control.Principal{Kind: control.KindUser, ID: shareRowan})
	if err != nil {
		t.Fatalf("reading candidates: %v", err)
	}
	for _, c := range candidates {
		if c.ID == shareStranger {
			t.Fatalf("the fixture offers %s as a candidate, so posting it cannot measure the check", shareStranger)
		}
	}

	form := url.Values{}
	form.Set(FieldScope, string(shareScope))
	form.Set(FieldSubject, string(shareStranger))
	form.Set(FieldVerb, string(control.VerbRead))
	rec := rowan.post(SharePath, form)
	if rec.Code != http.StatusForbidden {
		t.Errorf("sharing with a real principal that is NOT a candidate answered %d, want 403. The `select` "+
			"in the page constrains a browser and nothing else; without this check, admin on one scope "+
			"lets a caller widen it to any principal whose id they can name.", rec.Code)
	}

	// POSITIVE CONTROL: a real candidate IS accepted, so the refusal above is about the
	// subject rather than a write path that refuses everything.
	form.Set(FieldSubject, string(shareWren))
	if ok := rowan.post(SharePath, form); ok.Code != http.StatusSeeOther {
		t.Fatalf("POSITIVE CONTROL FAILED: sharing with a real candidate answered %d, want 303: %s",
			ok.Code, ok.Body.String())
	}
}

// TestAGrantWithNoVerbIsRefused pins the boundary `Event.validate` also holds, at the
// place a person can reach it.
func TestAGrantWithNoVerbIsRefused(t *testing.T) {
	rig := newShareRig(t)
	rowan := rig.signIn(shareRowanToken)

	for _, arm := range []struct {
		name  string
		verbs []string
	}{
		{"no box ticked", nil},
		// `control.NewVerbSet` DROPS an unknown verb rather than refusing, so a form
		// carrying only unknown verbs reaches the write path with an EMPTY set — the
		// one input that turns a silent narrowing into a grant conferring nothing.
		{"only a verb this build does not define", []string{"teleport"}},
	} {
		form := url.Values{}
		form.Set(FieldScope, string(shareScope))
		form.Set(FieldSubject, string(shareWren))
		for _, v := range arm.verbs {
			form.Add(FieldVerb, v)
		}
		if rec := rowan.post(SharePath, form); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: answered %d, want 400", arm.name, rec.Code)
		}
	}
}

// TestEveryTickedVerbReachesTheGrant pins the multi-value read.
//
// 🔴 `PostFormValue` RETURNS THE FIRST VALUE ONLY, AND A CHECKBOX GROUP POSTS ONE NAME
// MANY TIMES. A handler using it would silently record a read-only grant for somebody
// who ticked read AND write — a narrowing nothing reports, on the field that decides
// what another person can do to your notes.
func TestEveryTickedVerbReachesTheGrant(t *testing.T) {
	rig := newShareRig(t)
	rowan := rig.signIn(shareRowanToken)

	form := url.Values{}
	form.Set(FieldScope, string(shareScope))
	form.Set(FieldSubject, string(shareWren))
	form.Add(FieldVerb, string(control.VerbRead))
	form.Add(FieldVerb, string(control.VerbWrite))
	if rec := rowan.post(SharePath, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("the share answered %d: %s", rec.Code, rec.Body.String())
	}

	sharing := ControlSharing{Authority: rig.authority}
	rows, err := sharing.Revocable(shareScope)
	if err != nil {
		t.Fatalf("reading the grant rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want exactly one grant row, got %d", len(rows))
	}
	if rows[0].Verbs != "read,write" {
		t.Errorf("the recorded grant carries %q, want \"read,write\". Both boxes were posted; a single-value "+
			"read of the form would record only the first.", rows[0].Verbs)
	}
}

// TestRevokingRemovesTheViewerFromTheAudience is the round trip, and it is what makes
// the share flow a flow rather than a one-way widening.
func TestRevokingRemovesTheViewerFromTheAudience(t *testing.T) {
	rig := newShareRig(t)
	rowan := rig.signIn(shareRowanToken)
	wren := rig.signIn(shareWrenToken)

	form := url.Values{}
	form.Set(FieldScope, string(shareScope))
	form.Set(FieldSubject, string(shareWren))
	form.Set(FieldVerb, string(control.VerbRead))
	if rec := rowan.post(SharePath, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("the share answered %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(wren.get(RootPath).Body.String(), shareScopeName) {
		t.Fatal("wren cannot see the scope after the share, so the revocation below would prove nothing")
	}

	sharing := ControlSharing{Authority: rig.authority}
	rows, err := sharing.Revocable(shareScope)
	if err != nil || len(rows) != 1 {
		t.Fatalf("want one revocable grant, got %d (%v)", len(rows), err)
	}

	revoke := url.Values{}
	revoke.Set(FieldGrant, string(rows[0].ID))
	if rec := rowan.post(UnsharePath, revoke); rec.Code != http.StatusSeeOther {
		t.Fatalf("the revoke answered %d, want 303: %s", rec.Code, rec.Body.String())
	}

	if body := wren.get(RootPath).Body.String(); strings.Contains(body, shareScopeName) {
		t.Errorf("wren STILL sees %q after the grant was revoked. `ApplyNow` materializes before returning, "+
			"so this cache is serving the written epoch and no read through it may honour the grant.",
			shareScopeName)
	}
	audience, err := sharing.Audience(shareScope)
	if err != nil {
		t.Fatalf("reading the audience: %v", err)
	}
	for _, v := range audience {
		if v.Display == "wren@notes.example.invalid" {
			t.Error("wren is still in the audience after the revoke")
		}
	}
}

// TestARevokeIsAuthorisedFromTheGrantRatherThanFromTheForm pins that the scope a
// revocation touches is read from the grant and not from anything the caller sends.
//
// 🔴 THE FORM CARRIES NO SCOPE AT ALL, SO THIS TEST HAS TO BUILD THE HAZARD FROM THE
// OTHER SIDE: wren grants a scope wren owns, then ROWAN — who administers a different
// scope and has no authority over wren's — tries to revoke it by id. A handler that
// authorised from the caller's own admin rights rather than from the grant's object
// would allow it.
func TestARevokeIsAuthorisedFromTheGrantRatherThanFromTheForm(t *testing.T) {
	rig := newShareRig(t)
	rowan := rig.signIn(shareRowanToken)
	wren := rig.signIn(shareWrenToken)

	// Wren owns `commons` and its scope, so wren may share it. Rowan is only a reader
	// there and administers `quarry-notes` instead.
	form := url.Values{}
	form.Set(FieldScope, string(shareOtherScope))
	form.Set(FieldSubject, string(shareRowan))
	form.Set(FieldVerb, string(control.VerbWrite))
	if rec := wren.post(SharePath, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("wren's share answered %d: %s", rec.Code, rec.Body.String())
	}

	sharing := ControlSharing{Authority: rig.authority}
	rows, err := sharing.Revocable(shareOtherScope)
	if err != nil || len(rows) != 1 {
		t.Fatalf("want one grant on %s, got %d (%v)", shareOtherScopeName, len(rows), err)
	}

	revoke := url.Values{}
	revoke.Set(FieldGrant, string(rows[0].ID))
	if rec := rowan.post(UnsharePath, revoke); rec.Code != http.StatusForbidden {
		t.Errorf("rowan revoked a grant on a scope rowan does not administer (answered %d, want 403). The "+
			"grant id is the only thing that says which scope a revocation touches, so it is the only "+
			"thing the authority check may read it from.", rec.Code)
	}

	// POSITIVE CONTROL: the grant's own administrator CAN revoke it, so the refusal
	// above is about authority rather than a revoke path that refuses everything.
	if ok := wren.post(UnsharePath, revoke); ok.Code != http.StatusSeeOther {
		t.Fatalf("POSITIVE CONTROL FAILED: wren could not revoke a grant on wren's own scope (%d): %s",
			ok.Code, ok.Body.String())
	}
}

// TestAReadOnlyDeploymentSaysSoOnThePageRatherThanAtTheClick is the deployment
// `cairn-ui` has shipped with until now: an authority projected from a token file,
// which has no journal to append to.
//
// 🔴 THE FIRST DRAFT OF THIS TEST SKIPPED, AND THE SKIP IS WHY THE FEATURE CHANGED. It
// drove a share against the projection and asserted a 501; the projection grants NO
// `admin` verb to anybody — `internal/control/tokenfile` says so in as many words, "the
// token file has no sharing to administer" — so the authority check refused first and
// the assertion was never reached. A skip nobody counts is a pass. What the surface
// does now is announce the condition on the PAGE, where it IS reachable, and that is
// what this test measures.
func TestAReadOnlyDeploymentSaysSoOnThePageRatherThanAtTheClick(t *testing.T) {
	root := buildShareStore(t)
	tokenPath := filepath.Join(t.TempDir(), "tokens")
	// `<token> <identity> <comma-separated scopes>`, and the token must be at least
	// `authz.MinTokenChars` long — a short one is refused as guessable, which is a
	// property of the loader rather than of this test.
	if err := os.WriteFile(tokenPath,
		[]byte(readOnlyToken+" operator "+shareScopeName+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tokens, err := authz.LoadTokens(tokenPath, map[string]string{}, func(string) {})
	if err != nil {
		t.Fatalf("loading the token table: %v", err)
	}
	if len(tokens) == 0 {
		t.Fatal("the token table is empty, so the rig below would authenticate nobody")
	}
	authority := control.NewCache(tokenfile.Source{
		StoreRoot: root,
		Records:   func() []authz.TokenRecord { return tokens },
	}, control.CacheOptions{})
	if err := authority.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing the projection: %v", err)
	}

	// INSTRUMENT CONTROL: the projection really is read-only, so the sentence below is
	// the page reporting a fact rather than a constant somebody rendered unconditionally.
	if authority.Writable() {
		t.Fatal("the token-file projection reports itself WRITABLE, so this test measures nothing about " +
			"the read-only path")
	}

	rig := newShareRigWithAuthority(t, authority)
	caller := rig.signIn(readOnlyToken)

	// The READS still work. A deployment that cannot record a share can still answer
	// "who has access to this", and withholding that would be a worse surface, not a safer one.
	index := caller.get(SharePath)
	if index.Code != http.StatusOK {
		t.Fatalf("the share index answered %d against a read-only authority; the reads need no journal: %s",
			index.Code, index.Body.String())
	}
	if !strings.Contains(pageText(index.Body.String()), normalizeSpace(ReadOnlyAuthority)) {
		t.Errorf("the share index does not say this deployment cannot record a share. An operator who "+
			"mounted a token file instead of a journal learns it at the first click otherwise, and the "+
			"refusal they see then reads as a permission problem.\n%s", index.Body.String())
	}

	// 🔴 AND A WRITABLE DEPLOYMENT MUST NOT CARRY IT — a banner rendered unconditionally
	// is a banner that says nothing, and it would pass the assertion above forever.
	writable := newShareRig(t).signIn(shareRowanToken).get(SharePath)
	if strings.Contains(pageText(writable.Body.String()), normalizeSpace(ReadOnlyAuthority)) {
		t.Error("a deployment WITH a control journal also renders the read-only notice, so the assertion " +
			"above is about a constant rather than about the authority")
	}
}

// TestAReadOnlyWriteIsRefusedWithTheCauseRatherThanAPermission pins the STATUS MAPPING
// for `control.ErrAuthorityReadOnly`.
//
// ⚠ IT DRIVES THE CONDITION THROUGH A FIXTURE, AND THAT IS STATED BECAUSE IT IS A
// WEAKER CLAIM THAN THE TEST ABOVE. No authority in this tree is both read-only and
// able to confer `admin`, so the arm is unreachable end to end today — see
// `refuseWrite`. What this measures is that IF a write ever returns that sentinel, the
// answer is a 501 naming the configuration and not a 403 naming permissions. It is the
// mapping, through the real dispatcher, and it is not evidence that the condition
// occurs.
func TestAReadOnlyWriteIsRefusedWithTheCauseRatherThanAPermission(t *testing.T) {
	sharing := benignSharing()
	sharing.err = control.ErrAuthorityReadOnly
	// The fixture admits the subject and the scope, so the refusal below is the write
	// rather than the candidate or authority checks.
	cfg := testConfig(t, staticAuth{authorisedIdentity(t)})
	cfg.Sharing = sharing
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}

	form := url.Values{}
	form.Set(FieldScope, string(fixtureNamedScope.ID))
	form.Set(FieldSubject, string(sharing.candidates[0].ID))
	form.Set(FieldVerb, string(control.VerbRead))
	rec := driveForm(t, srv, SharePath, form)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("a write refused with ErrAuthorityReadOnly answered %d, want 501: %s",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "control journal") {
		t.Errorf("the refusal is %q and does not name the configuration that caused it; an operator "+
			"reading it would go looking for a permission problem", rec.Body.String())
	}

	// POSITIVE CONTROL: with the sentinel removed the same request SUCCEEDS, so the 501
	// above is the mapping rather than a write path that refuses everything.
	sharing.err = nil
	if ok := driveForm(t, srv, SharePath, form); ok.Code != http.StatusSeeOther {
		t.Fatalf("POSITIVE CONTROL FAILED: the same request without the sentinel answered %d, want 303: %s",
			ok.Code, ok.Body.String())
	}
}

// authorisedIdentity is the dispatch fixtures' principal carrying REAL admin authority
// over `fixtureNamedScope`, resolved from a real model rather than hand-built.
//
// 🔴 A HAND-BUILT `control.Authorization` IS NOT POSSIBLE FROM OUTSIDE `control`, AND
// THAT IS THE PACKAGE WORKING. Its map is unexported precisely so no test and no
// surface can mint authority that `Resolve` did not compute; this helper therefore
// builds a one-scope world and resolves it, which is the same path a request takes.
func authorisedIdentity(t *testing.T) identity.Identity {
	t.Helper()
	at := shareClock
	owner := control.DerivedID(control.PrefixUser, "fixture-owner")
	project := control.DerivedID(control.PrefixProject, "fixture-project")
	m, err := control.Replay([]control.Event{
		{Kind: control.EventUserCreated, At: at, UserID: owner,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000021",
			Email: "owner@notes.example.invalid"},
		{Kind: control.EventProjectCreated, At: at, ProjectID: project, Name: "fixture", UserID: owner},
		{Kind: control.EventMemberSet, At: at, ProjectID: project, UserID: owner, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: at, ScopeID: fixtureNamedScope.ID,
			DisplayName: fixtureNamedScope.Name, ProjectID: project},
	})
	if err != nil {
		t.Fatalf("building the authorised world: %v", err)
	}
	p, known := m.PrincipalFor(control.KindUser, owner)
	if !known {
		t.Fatal("the world does not hold its own owner")
	}
	auth := control.Resolve(m, p)
	if !auth.Allows(fixtureNamedScope.ID, control.VerbAdmin) {
		t.Fatal("the resolved authority does not administer the fixture scope, so every write below would " +
			"be refused by the authority check rather than by the arm under test")
	}
	return identity.Identity{Principal: p, Auth: auth}
}

// driveForm posts a form past both cross-site gates against a server whose
// authentication is a fixture, so what it measures is the handler.
//
// The CSRF token is derived from a cookie value this request chooses. That is not a
// hole: `csrfTokenValid`'s own comment records that the gate binds a token to THE
// COOKIE ON THIS REQUEST, which for a caller that chose its own cookie is a
// self-consistency check — and a caller choosing its own cookie is not cross-site.
func driveForm(t *testing.T, srv *Server, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	const cookieValue = "fixture-session-value-which-is-not-a-real-session"
	body := url.Values{}
	for k, v := range form {
		body[k] = v
	}
	body.Set(FieldCSRF, identity.CSRFTokenFor(cookieValue))
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://"+req.Host)
	req.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: cookieValue})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// TestTheOutcomeBannerRendersOnlySentencesWrittenHere pins that the redirect's message
// is a CODE and never reflected text.
//
// 🔴 A REFLECTED SENTENCE IS NOT AN XSS BUG AND IS STILL AN ATTACK. The escaper turns
// markup into text; it has nothing to say about a link that makes this page present an
// attacker's sentence as its OWN ("Your access was suspended, call this number"). The
// only caller-supplied value that may reach the banner is an instant, and only after
// this code has parsed and re-formatted it.
func TestTheOutcomeBannerRendersOnlySentencesWrittenHere(t *testing.T) {
	rig := newShareRig(t)
	rowan := rig.signIn(shareRowanToken)

	hostile := "Your access was suspended, call 555-0100"
	rec := rowan.get(sharePath(shareScope) + "&" + QueryOutcome + "=" + url.QueryEscape(hostile) +
		"&" + QueryEffectiveBy + "=" + url.QueryEscape(hostile))
	if rec.Code != http.StatusOK {
		t.Fatalf("the page answered %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "suspended") {
		t.Errorf("a sentence from the query string reached the page. The banner must render a sentence "+
			"written in the handler, chosen by a CODE from a closed set.\n%s", rec.Body.String())
	}

	// POSITIVE CONTROL: a real outcome code DOES render, so the absence above is the
	// closed set working rather than a banner that never renders at all.
	genuine := rowan.get(sharePath(shareScope) + "&" + QueryOutcome + "=" + outcomeShared +
		"&" + QueryEffectiveBy + "=" + effectNow)
	if !strings.Contains(genuine.Body.String(), "in force on this replica now") {
		t.Errorf("POSITIVE CONTROL FAILED: a legitimate outcome code rendered no banner, so the assertion "+
			"above is indistinguishable from a page with no banner at all.\n%s", genuine.Body.String())
	}

	// And a DEFERRED outcome must carry its qualifier — `control.EffectDeferred`'s own
	// comment makes that mandatory rather than stylistic.
	deferred := rowan.get(sharePath(shareScope) + "&" + QueryOutcome + "=" + outcomeRevoked +
		"&" + QueryEffectiveBy + "=" + url.QueryEscape("2000-06-01T12:05:00Z"))
	body := deferred.Body.String()
	if !strings.Contains(body, "not in force on this replica yet") || !strings.Contains(body, "2000-06-01T12:05:00Z") {
		t.Errorf("a deferred outcome rendered without its effective-by qualifier. Omitting it is the "+
			"sharing dialog implying a guarantee the system cannot make.\n%s", body)
	}
}
