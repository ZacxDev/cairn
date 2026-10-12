package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/codesrc"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/control/tokenfile"
	"github.com/ZacxDev/cairn/internal/identity"
)

// The code-sources edit flow (S4 of the scope-refs plan), driven through the share rig: a real
// control journal, a real store, real sessions and the real dispatcher. Rowan ADMINISTERS
// `quarry-notes` and is a plain MEMBER (read and write, no admin) of `commons-notes`.

const (
	srcGitHub = "git:github.com/example-org/example-repo@main"
	srcOther  = "git:git.example.com/team/sub-group/example-repo@trunk"
)

// sourcesRig is the share rig with a sources journal configured outside its store.
func sourcesRig(t *testing.T) (*shareRig, string) {
	t.Helper()
	rig := newShareRig(t)
	path := filepath.Join(t.TempDir(), "sources.jsonl")
	rig.srv.sourceJournal = codesrc.Journal{Path: path}
	return rig, path
}

// journalLines is the journal's complete lines; an absent file is zero lines.
func journalLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func lastRecord(t *testing.T, path string) codesrc.Record {
	t.Helper()
	lines := journalLines(t, path)
	if len(lines) == 0 {
		t.Fatal("the journal holds no line")
	}
	var rec codesrc.Record
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func sourcesPagePath(scope control.ID) string {
	return SourcesPath + "?" + QueryScope + "=" + string(scope)
}

// formRevision reads the hidden revision field off a rendered edit page.
func formRevision(t *testing.T, body string) string {
	t.Helper()
	marker := `name="` + FieldRevision + `" value="`
	_, rest, ok := strings.Cut(body, marker)
	if !ok {
		t.Fatalf("the page carries no revision field:\n%s", body)
	}
	value, _, _ := strings.Cut(rest, `"`)
	return html.UnescapeString(value)
}

func saveForm(scope control.ID, text, revision string) url.Values {
	return url.Values{FieldScope: {string(scope)}, FieldSources: {text}, FieldRevision: {revision}}
}

// TestAnAdminsSaveAppendsOneLineStampedByTheServerNotTheForm: a valid POST appends EXACTLY one
// line, stamped with the signed-in principal and the UI's clock, in canonical form and order, under
// the scope's NAME key. A forged `set_by` (and `set_at`) form field is IGNORED — the mutant
// `ui-sources-set-by-taken-from-the-form` reads it and goes red here.
func TestAnAdminsSaveAppendsOneLineStampedByTheServerNotTheForm(t *testing.T) {
	rig, path := sourcesRig(t)
	rowan := rig.signIn(shareRowanToken)

	page := rowan.get(sourcesPagePath(shareScope))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `action="`+SourcesPath+`"`) {
		t.Fatalf("an admin's edit page must carry the form: %d\n%s", page.Code, page.Body.String())
	}
	rev := formRevision(t, page.Body.String())
	if rev != codesrc.RevisionNone {
		t.Fatalf("an undeclared scope's form carries revision %q, want %q", rev, codesrc.RevisionNone)
	}

	form := saveForm(shareScope, srcGitHub+"\r\n"+srcOther+"\r\n", rev)
	form.Set("set_by", "forged-admin")
	form.Set("set_at", "2000-01-01T00:00:00Z")
	rec := rowan.post(SourcesPath, form)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/scope/sources?outcome=saved&scope="+string(shareScope) {
		t.Fatalf("a valid save must 303 to the edit page with the saved outcome: %d %q\n%s",
			rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if n := len(journalLines(t, path)); n != 1 {
		t.Fatalf("one save appended %d line(s), want exactly 1", n)
	}
	got := lastRecord(t, path)
	want := codesrc.Record{Schema: 1, Scope: shareScopeName, Sources: []string{srcGitHub, srcOther},
		SetBy: rigPrincipalDisplay(t, rig, shareRowanToken), SetAt: "2000-06-01T12:00:00Z",
		Revision: codesrc.Revision(shareScopeName, []string{srcGitHub, srcOther})}
	if got.Scope != want.Scope || strings.Join(got.Sources, ",") != strings.Join(want.Sources, ",") ||
		got.SetBy != want.SetBy || got.SetAt != want.SetAt || got.Revision != want.Revision {
		t.Fatalf("record %+v\nwant   %+v", got, want)
	}
	if got.SetBy == "forged-admin" || got.SetAt == "2000-01-01T00:00:00Z" {
		t.Fatal("the stamp was taken from the FORM")
	}

	saved := rowan.get(rec.Header().Get("Location"))
	if !strings.Contains(saved.Body.String(), `data-sources="outcome"`) ||
		!strings.Contains(saved.Body.String(), srcGitHub+"\n"+srcOther) {
		t.Fatalf("the saved page must say so and show the list:\n%s", saved.Body.String())
	}
}

// rigPrincipalDisplay is what the chain resolves a token's principal's display to — the value the
// stamp must carry, read through the same authority rather than restated.
func rigPrincipalDisplay(t *testing.T, rig *shareRig, token string) string {
	t.Helper()
	p, _, err := rig.authority.Authenticate(token)
	if err != nil {
		t.Fatal(err)
	}
	if p.Display == "" {
		t.Fatal("the fixture principal has no display, so the stamp assertion would compare empty strings")
	}
	return p.Display
}

// TestAStaleSaveWritesNothingAndShowsTheCurrentList is T6: the second of two saves carrying one
// revision appends NOTHING (line count before = after), answers 409, and re-renders the CURRENT list
// with the CURRENT revision — so a resubmit from that page lands. A resubmission of the now-current
// list under the old revision is refused the same way.
func TestAStaleSaveWritesNothingAndShowsTheCurrentList(t *testing.T) {
	rig, path := sourcesRig(t)
	rowan := rig.signIn(shareRowanToken)
	if rec := rowan.post(SourcesPath, saveForm(shareScope, srcGitHub, codesrc.RevisionNone)); rec.Code != http.StatusSeeOther {
		t.Fatalf("first save: %d %s", rec.Code, rec.Body.String())
	}
	before := len(journalLines(t, path))
	for _, text := range []string{srcOther, srcGitHub} {
		rec := rowan.post(SourcesPath, saveForm(shareScope, text, codesrc.RevisionNone))
		if rec.Code != http.StatusConflict {
			t.Fatalf("a stale save (%q) answered %d, want 409:\n%s", text, rec.Code, rec.Body.String())
		}
		if after := len(journalLines(t, path)); after != before {
			t.Fatalf("a stale save appended: %d line(s) before, %d after", before, after)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `data-sources="problem"`) || !strings.Contains(body, ">"+srcGitHub+"</textarea>") {
			t.Fatalf("a stale save must re-render the CURRENT list with the problem said:\n%s", body)
		}
		if got := formRevision(t, body); got != codesrc.Revision(shareScopeName, []string{srcGitHub}) {
			t.Fatalf("the re-rendered form carries revision %q, not the current one", got)
		}
	}
}

// TestAnInvalidLineIsRefusedByItsLineNumberWithTheTextKept: nothing is written, the refusal names
// the FORM's line (blank lines counted, so it is the line the user sees) and its rule, and the
// textarea keeps what was typed.
func TestAnInvalidLineIsRefusedByItsLineNumberWithTheTextKept(t *testing.T) {
	rig, path := sourcesRig(t)
	rowan := rig.signIn(shareRowanToken)
	typed := srcGitHub + "\n\ngit:github.com/lonely@main\n"
	rec := rowan.post(SourcesPath, saveForm(shareScope, typed, codesrc.RevisionNone))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an invalid line answered %d, want 422:\n%s", rec.Code, rec.Body.String())
	}
	body := html.UnescapeString(rec.Body.String())
	want := "Line 3 (git:github.com/lonely@main) " + codesrc.RuleRepoPath + ". Nothing was saved."
	if !strings.Contains(body, want) {
		t.Fatalf("the refusal must read %q:\n%s", want, body)
	}
	if !strings.Contains(body, ">"+typed+"</textarea>") {
		t.Fatalf("the typed text must be kept in the textarea:\n%s", body)
	}
	if lines := journalLines(t, path); lines != nil {
		t.Fatalf("a refused save wrote %d line(s)", len(lines))
	}
}

// TestAnEmptyTextareaRecordsAnExplicitUndeclared: `sources: []`, stamped — clearing is history too.
func TestAnEmptyTextareaRecordsAnExplicitUndeclared(t *testing.T) {
	rig, path := sourcesRig(t)
	rowan := rig.signIn(shareRowanToken)
	if rec := rowan.post(SourcesPath, saveForm(shareScope, srcGitHub, codesrc.RevisionNone)); rec.Code != http.StatusSeeOther {
		t.Fatalf("seed: %d", rec.Code)
	}
	rev := codesrc.Revision(shareScopeName, []string{srcGitHub})
	if rec := rowan.post(SourcesPath, saveForm(shareScope, "\r\n", rev)); rec.Code != http.StatusSeeOther {
		t.Fatalf("clearing answered %d:\n%s", rec.Code, rec.Body.String())
	}
	lines := journalLines(t, path)
	if len(lines) != 2 || !strings.Contains(lines[1], `"sources":[]`) {
		t.Fatalf("clearing must append one `sources: []` record: %q", lines)
	}
	page := rowan.get(ScopePath + "?" + QueryID + "=" + string(shareScope))
	if !strings.Contains(page.Body.String(), `data-sources="undeclared"`) {
		t.Fatalf("a cleared scope reads undeclared on its page:\n%s", page.Body.String())
	}
}

// TestTheSaveIsBehindBothCrossSiteGatesAndTheAdminCheck: no CSRF token → 403; a cross-site Origin →
// refused; a principal who READS and WRITES the scope but does not ADMINISTER it → the one refusal,
// byte-identical to a scope that does not exist — and none of them writes. POSITIVE CONTROL: the same
// admin, both gates satisfied, lands (so the refusals are not a handler refusing everything).
func TestTheSaveIsBehindBothCrossSiteGatesAndTheAdminCheck(t *testing.T) {
	rig, path := sourcesRig(t)
	rowan := rig.signIn(shareRowanToken)

	raw := func(origin string, withCSRF bool) *httptest.ResponseRecorder {
		form := saveForm(shareScope, srcGitHub, codesrc.RevisionNone)
		if withCSRF {
			form.Set(FieldCSRF, csrfFor(rowan))
		}
		req := httptest.NewRequest(http.MethodPost, SourcesPath, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
		req.AddCookie(rowan.cookie)
		rec := httptest.NewRecorder()
		rig.srv.ServeHTTP(rec, req)
		return rec
	}
	if rec := raw("https://example.com", false); rec.Code != http.StatusForbidden {
		t.Fatalf("no CSRF token: %d, want 403", rec.Code)
	}
	if rec := raw("https://attacker.example", true); rec.Code != http.StatusForbidden {
		t.Fatalf("a cross-site Origin: %d, want 403", rec.Code)
	}

	member := rowan.post(SourcesPath, saveForm(shareOtherScope, srcGitHub, codesrc.RevisionNone))
	absent := rowan.post(SourcesPath, saveForm(control.ID("scp_does_not_exist"), srcGitHub, codesrc.RevisionNone))
	if member.Code != http.StatusNotFound || member.Body.String() != sourcesRefusal ||
		absent.Code != member.Code || absent.Body.String() != member.Body.String() {
		t.Fatalf("a non-admin and an absent scope must get one refusal: %d %q / %d %q",
			member.Code, member.Body.String(), absent.Code, absent.Body.String())
	}
	// The read-only view must SHOW the list: seed `commons-notes` through the one writer with a source
	// the page's help text cannot contain (the help text quotes `srcGitHub`, which would make a
	// Contains vacuous — the S4 audit's measured trap).
	distinct := "git:github.com/distinct-org/distinct-repo@feature"
	if _, err := rig.srv.sourceJournal.Set(shareOtherScopeName, []string{distinct}, codesrc.RevisionNone,
		"scope-admin", shareClock, nil); err != nil {
		t.Fatal(err)
	}
	readOnlyPage := rowan.get(sourcesPagePath(shareOtherScope))
	if !strings.Contains(readOnlyPage.Body.String(), "<code>"+distinct+"</code>") {
		t.Fatalf("a reader who is not an admin must see the current list:\n%s", readOnlyPage.Body.String())
	}
	if readOnlyPage.Code != http.StatusOK || strings.Contains(readOnlyPage.Body.String(), `action="`+SourcesPath+`"`) ||
		!strings.Contains(readOnlyPage.Body.String(), `data-sources="read-only"`) {
		t.Fatalf("a reader who is not an admin sees the page and no form: %d\n%s", readOnlyPage.Code, readOnlyPage.Body.String())
	}
	if n := len(journalLines(t, path)); n != 1 {
		t.Fatalf("a refused save wrote: %d line(s), want only the seeded one", n)
	}

	if ok := rowan.post(SourcesPath, saveForm(shareScope, srcGitHub, codesrc.RevisionNone)); ok.Code != http.StatusSeeOther {
		t.Fatalf("POSITIVE CONTROL: the admin's gated save answered %d", ok.Code)
	}
}

// csrfFor is the token the browser fixture's post helper sends.
func csrfFor(b *browser) string { return identity.CSRFTokenFor(b.cookie.Value) }

// TestATokenFileDeploymentRefusesEverySave: the token-file projection confers `admin` on nobody
// (`tokenfile/source.go`), so a save is the refusal for every principal — the designed state. The
// page still shows the declaration, read-only.
func TestATokenFileDeploymentRefusesEverySave(t *testing.T) {
	root := buildShareStore(t)
	tokenPath := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenPath, []byte(readOnlyToken+" operator "+shareScopeName+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tokens, err := authz.LoadTokens(tokenPath, map[string]string{}, func(string) {})
	if err != nil || len(tokens) == 0 {
		t.Fatalf("loading the token table: %v", err)
	}
	authority := control.NewCache(tokenfile.Source{StoreRoot: root, Records: func() []authz.TokenRecord { return tokens }},
		control.CacheOptions{})
	if err := authority.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	rig := newShareRigWithAuthority(t, authority)
	path := filepath.Join(t.TempDir(), "sources.jsonl")
	rig.srv.sourceJournal = codesrc.Journal{Path: path}
	caller := rig.signIn(readOnlyToken)
	scope := control.DerivedID(control.PrefixScope, shareScopeName)
	page := caller.get(sourcesPagePath(scope))
	if page.Code != http.StatusOK || strings.Contains(page.Body.String(), `action="`+SourcesPath+`"`) {
		t.Fatalf("the token-file reader sees the page and no form: %d\n%s", page.Code, page.Body.String())
	}
	rec := caller.post(SourcesPath, saveForm(scope, srcGitHub, codesrc.RevisionNone))
	if rec.Code != http.StatusNotFound || rec.Body.String() != sourcesRefusal {
		t.Fatalf("a token-file save must be the refusal: %d %q", rec.Code, rec.Body.String())
	}
	if lines := journalLines(t, path); lines != nil {
		t.Fatalf("a token-file save wrote: %q", lines)
	}
}

// TestOnlyAGitHubSourceIsLinked: exactly ONE href in the block, the literal GitHub tree URL; a
// self-hosted host renders as text. A branch carrying `#` is escaped so it cannot end the path.
func TestOnlyAGitHubSourceIsLinked(t *testing.T) {
	rec := &codesrc.Record{Sources: []string{srcGitHub, srcOther}, SetBy: "scope-admin", SetAt: "2000-01-03T00:00:00Z"}
	var b bytes.Buffer
	if err := sourcesBlockNode(Scope{ID: shareScope, Name: shareScopeName}, SourcesBlock{Record: rec}).Render(&b); err != nil {
		t.Fatal(err)
	}
	body := b.String()
	if n := strings.Count(body, "href="); n != 1 {
		t.Fatalf("%d href(s) in the block, want exactly 1:\n%s", n, body)
	}
	if !strings.Contains(body, `href="https://github.com/example-org/example-repo/tree/main"`) {
		t.Fatalf("the one href is not the literal GitHub tree URL:\n%s", body)
	}
	if !strings.Contains(body, "<code>"+srcOther+"</code>") {
		t.Fatalf("the self-hosted source must render as text:\n%s", body)
	}
	if got, ok := sourceHref("git:github.com/example-org/example-repo@issue#12"); !ok ||
		got != "https://github.com/example-org/example-repo/tree/issue%2312" {
		t.Fatalf("a `#` branch: %q %v", got, ok)
	}
}

// TestTheScopePageShowsTheBlockOnlyWhenConfigured: with a journal, the scope page carries the block
// (the list, the stamp, and the edit link for an admin only); without one, no block at all.
func TestTheScopePageShowsTheBlockOnlyWhenConfigured(t *testing.T) {
	rig, _ := sourcesRig(t)
	rowan := rig.signIn(shareRowanToken)
	if rec := rowan.post(SourcesPath, saveForm(shareScope, srcGitHub, codesrc.RevisionNone)); rec.Code != http.StatusSeeOther {
		t.Fatalf("seed: %d", rec.Code)
	}
	admin := rowan.get(ScopePath + "?" + QueryID + "=" + string(shareScope)).Body.String()
	if !strings.Contains(admin, `id="scope-sources"`) || !strings.Contains(admin, "<code>"+srcGitHub+"</code>") ||
		!strings.Contains(admin, `data-sources="set-by"`) || !strings.Contains(admin, `data-sources="edit"`) {
		t.Fatalf("an admin's scope page must carry the block with its edit link:\n%s", admin)
	}
	member := rowan.get(ScopePath + "?" + QueryID + "=" + string(shareOtherScope)).Body.String()
	if !strings.Contains(member, `id="scope-sources"`) || strings.Contains(member, `data-sources="edit"`) {
		t.Fatalf("a non-admin's scope page carries the block and no edit link:\n%s", member)
	}

	off := newShareRig(t).signIn(shareRowanToken)
	if page := off.get(ScopePath + "?" + QueryID + "=" + string(shareScope)).Body.String(); strings.Contains(page, `id="scope-sources"`) {
		t.Fatalf("with no journal the block must be absent:\n%s", page)
	}
	edit := off.get(sourcesPagePath(shareScope))
	if edit.Code != http.StatusOK || !strings.Contains(pageText(edit.Body.String()), normalizeSpace(NoSourceJournal)) ||
		strings.Contains(edit.Body.String(), `action="`+SourcesPath+`"`) {
		t.Fatalf("the off state's edit page says so and renders no form: %d\n%s", edit.Code, edit.Body.String())
	}
	if rec := off.post(SourcesPath, saveForm(shareScope, srcGitHub, codesrc.RevisionNone)); rec.Code != http.StatusNotImplemented ||
		rec.Body.String() != NoSourceJournal {
		t.Fatalf("the off state's save is 501 with the sentence: %d %q", rec.Code, rec.Body.String())
	}
}

// TestAnUnreadableJournalIsSaidAndNeverShownAsUndeclared: a DIRECTORY at the journal path is the
// broken state — the edit page answers 503 with the error, the scope page says it could not be read,
// and neither renders "declares no code sources".
func TestAnUnreadableJournalIsSaidAndNeverShownAsUndeclared(t *testing.T) {
	rig := newShareRig(t)
	dir := filepath.Join(t.TempDir(), "sources.jsonl")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	rig.srv.sourceJournal = codesrc.Journal{Path: dir}
	rowan := rig.signIn(shareRowanToken)
	if rec := rowan.get(sourcesPagePath(shareScope)); rec.Code != http.StatusServiceUnavailable ||
		rec.Body.String() != sourcesUnreadable || strings.Contains(rec.Body.String(), dir) {
		t.Fatalf("the edit page over a broken journal: %d %q", rec.Code, rec.Body.String())
	}
	page := rowan.get(ScopePath + "?" + QueryID + "=" + string(shareScope)).Body.String()
	if !strings.Contains(page, `data-sources="unreadable"`) || strings.Contains(page, `data-sources="undeclared"`) {
		t.Fatalf("the scope page must say unreadable, never undeclared:\n%s", page)
	}
}
