package ui_test

import (
	"context"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/api"
	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/codesrc"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/ui"
)

// TestAUIWrittenDeclarationIsWhatThePodServes is closing-condition part 4 of the scope-refs plan:
// the cross-binary SEAM, built the way each binary's `main` builds its half, over ONE journal and ONE
// store directory `alpha-notes`.
//
//   - THE UI HALF is `ui.New` over a CONTROL-JOURNAL model, in which `alpha-notes` has a RANDOM id
//     (`control.NewID`), signed in through the real exchange as the scope's owner, POSTing two sources
//     through the real dispatcher with both cross-site gates satisfied.
//   - THE POD HALF is `api.New` over a TOKEN-FILE projection (`cmd/cairn-server/main.go`'s call), in
//     which the same directory's id is DERIVED from its name. It GETs `alpha-notes`.
//
// 🔴 THE TWO IDS DIFFER BY CONSTRUCTION, AND THE TEST ASSERTS THAT PRECONDITION, so it cannot go green
// because the two fixtures happened to share an id. The record must therefore be keyed by the one
// identifier both authorities share — the folded directory NAME (`codesrc.Key`, the operator's Q17
// answer). RED under `ui-sources-keyed-by-control-id` (revision 3's design): the pod answers
// `sources-undeclared`, because the UI wrote under an id the pod has never heard of.
func TestAUIWrittenDeclarationIsWhatThePodServes(t *testing.T) {
	const (
		scopeName = "alpha-notes"
		token     = "seam-owner-credential-which-is-not-a-real-token-000"
		podToken  = "seam-pod-reader-credential-which-is-not-a-real-token"
		first     = "git:github.com/example-org/example-repo@main"
		second    = "git:git.example.com/team/sub-group/example-repo@trunk"
	)
	clock := time.Date(2000, 1, 3, 0, 0, 0, 0, time.UTC)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, scopeName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, scopeName, "gadget-one.md"), []byte("---\nservice: gadget-one\nscope: "+
		scopeName+"\n---\n\n## What it is\nsynthetic.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal, err := codesrc.ResolveJournalPath(root, filepath.Join(t.TempDir(), "sources.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	// ── the UI half: a control journal with a RANDOM scope id ──
	uiScope, err := control.NewID(control.PrefixScope)
	if err != nil {
		t.Fatal(err)
	}
	user := control.DerivedID(control.PrefixUser, "seam-owner")
	project := control.DerivedID(control.PrefixProject, "seam-project")
	store, err := control.OpenFileStore(filepath.Join(t.TempDir(), "control.journal"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(),
		control.Event{Kind: control.EventUserCreated, At: clock, UserID: user, Provider: "fixture-provider",
			Subject: "00000000-0000-4000-8000-000000000021", Email: "owner@notes.example.invalid"},
		control.Event{Kind: control.EventProjectCreated, At: clock, ProjectID: project, Name: "seam", UserID: user},
		control.Event{Kind: control.EventMemberSet, At: clock, ProjectID: project, UserID: user, Role: control.RoleOwner},
		control.Event{Kind: control.EventScopeCreated, At: clock, ScopeID: uiScope, DisplayName: scopeName, ProjectID: project},
		control.Event{Kind: control.EventCredentialIssued, At: clock, CredentialID: "crd_seam_owner",
			SubjectKind: control.KindUser, SubjectID: user, TokenHash: control.HashToken(token), Label: "seam-owner"},
	); err != nil {
		t.Fatal(err)
	}
	authority := control.NewCache(store, control.CacheOptions{})
	if err := authority.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	sessions, err := identity.OpenFileSessionStore(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	sessions.Now = func() time.Time { return clock }
	cookie, err := identity.NewCookieSession(sessions, authority)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := identity.NewMachineToken(authority)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := ui.AuthBackends(machine, nil, cookie)
	if err != nil {
		t.Fatal(err)
	}
	browser, err := ui.New(ui.Config{
		Auth: chain, Credentials: authority, Source: ui.StoreSource{Root: root},
		Sharing:  ui.ControlSharing{Authority: authority, Now: func() time.Time { return clock }},
		Sessions: sessions, Now: func() time.Time { return clock }, Log: io.Discard,
		SourceJournal: codesrc.Journal{Path: journal},
	})
	if err != nil {
		t.Fatal(err)
	}

	// ── the pod half: a token-file projection, the id DERIVED from the directory name ──
	pod, err := api.New(root, []authz.TokenRecord{{Token: podToken, Identity: "seam-reader", Scopes: []string{scopeName}}},
		[]netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, netid.NewRateLimiter(1000000, time.Minute, 15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	pod.Audit, pod.Warn = func(string) {}, func(string) {}
	pod.SourceJournal = journal

	// THE PRECONDITION: the two authorities name the directory by DIFFERENT ids.
	if derived := control.DerivedID(control.PrefixScope, scopeName); derived == uiScope {
		t.Fatalf("the UI's scope id %q equals the pod's derived id, so this test cannot see an id-keyed record", uiScope)
	}

	// Sign in through the real exchange, read the form's revision, POST two sources.
	signIn := url.Values{ui.FieldToken: {token}}
	req := httptest.NewRequest(http.MethodPost, ui.SignInPath, strings.NewReader(signIn.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://"+req.Host)
	rec := httptest.NewRecorder()
	browser.ServeHTTP(rec, req)
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == identity.SessionCookieName && c.Value != "" {
			session = c
		}
	}
	if rec.Code != http.StatusSeeOther || session == nil {
		t.Fatalf("sign-in: %d, cookie %v", rec.Code, session)
	}
	page := httptest.NewRequest(http.MethodGet, ui.SourcesPath+"?"+ui.QueryScope+"="+string(uiScope), nil)
	page.AddCookie(session)
	rec = httptest.NewRecorder()
	browser.ServeHTTP(rec, page)
	_, after, ok := strings.Cut(rec.Body.String(), `name="`+ui.FieldRevision+`" value="`)
	if rec.Code != http.StatusOK || !ok {
		t.Fatalf("the owner's edit page carries no form: %d\n%s", rec.Code, rec.Body.String())
	}
	revision, _, _ := strings.Cut(after, `"`)
	form := url.Values{
		ui.FieldCSRF: {identity.CSRFTokenFor(session.Value)}, ui.FieldScope: {string(uiScope)},
		ui.FieldSources: {first + "\n" + second}, ui.FieldRevision: {html.UnescapeString(revision)},
	}
	post := httptest.NewRequest(http.MethodPost, ui.SourcesPath, strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Origin", "https://"+post.Host)
	post.AddCookie(session)
	rec = httptest.NewRecorder()
	browser.ServeHTTP(rec, post)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the UI's save answered %d:\n%s", rec.Code, rec.Body.String())
	}

	// Over a real listener, the way `internal/client`'s tests reach a pod. (An in-memory
	// `httptest.NewRequest` answered 401 here; not chased, because a real connection is what a
	// deployed reader makes anyway.)
	podServer := httptest.NewServer(pod)
	defer podServer.Close()
	get, err := http.NewRequest(http.MethodGet, podServer.URL+"/api/v1/sources/"+scopeName, nil)
	if err != nil {
		t.Fatal(err)
	}
	get.Header.Set("Authorization", "Bearer "+podToken)
	resp, err := http.DefaultClient.Do(get)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	want := "\nsources=declared count=2\nsource=" + first + "\nsource=" + second + "\n"
	if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Store-Status") != "sources-declared" ||
		!strings.HasSuffix(string(body), want) {
		t.Fatalf("the pod must serve what the UI wrote: %d %s\n%s\nwant suffix %q",
			resp.StatusCode, resp.Header.Get("X-Store-Status"), body, want)
	}
}
