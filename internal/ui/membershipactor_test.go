package ui

import (
	"context"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// bearerDo drives one request authenticated by a bearer token — the machine-token backend,
// which is how a narrowed credential reaches this surface. It also carries a cookie value the
// CALLER chose (no session behind it) and, on a state change, the CSRF token derived from
// it — exactly what `csrfTokenValid`'s comment says such a caller can do.
func (l *live) bearerDo(method, path, bearer string, form url.Values) *httptest.ResponseRecorder {
	l.t.Helper()
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Host = testHost
	r.Header.Set("Authorization", "Bearer "+bearer)
	// The cookie rides GETs too: a page renders its forms (and the share page its candidate
	// `select`) only when a CSRF token can be derived from a cookie on the request.
	const chosen = "a-cookie-value-the-caller-chose"
	r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: chosen})
	if method != http.MethodGet {
		r.Header.Set("Origin", "https://"+testHost)
		r.Header.Set(HeaderCSRF, identity.CSRFTokenFor(chosen))
	}
	rec := httptest.NewRecorder()
	l.srv.ServeHTTP(rec, r)
	return rec
}

var narrowedBearers = []string{narrowedToOneToken, narrowedToNoneToken, narrowedToAllToken}

// seededInvite is a real invitation into the fixture user's project, minted directly by the
// service as the UN-narrowed principal, so the page and revoke rows have something to show
// and something to withdraw.
func seededInvite(t *testing.T, authority *control.Cache) (ControlInviting, *memInvites, string) {
	t.Helper()
	store := newMemInvites()
	inviting := ControlInviting{Authority: authority, Invites: store, Now: func() time.Time { return fixtureClock }}
	owner, ok := authority.Model().PrincipalFor(control.KindUser, fixtureUser)
	if !ok {
		t.Fatal("precondition: the fixture user is not in the model")
	}
	_, inv, err := inviting.Mint(context.Background(), owner, fixtureProject, control.RoleMember, 0)
	if err != nil {
		t.Fatalf("precondition: the owner could not mint: %v", err)
	}
	return inviting, store, inv.Digest
}

// TestANarrowedBearerSeesNoInvitations covers `GET /invite?project=` — the page that lists a
// project's invitations through `Outstanding`, which performs NO authority check of its own
// and is reached only through `Invitable`.
func TestANarrowedBearerSeesNoInvitations(t *testing.T) {
	authority := narrowedWorld(t)
	inviting, _, _ := seededInvite(t, authority)
	path := InvitePath + "?" + url.Values{QueryProject: {string(fixtureProject)}}.Encode()

	// POSITIVE CONTROL: the un-narrowed bearer sees the project page and the invitation on it.
	rec := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodGet, path, testCredential, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED: the un-narrowed bearer's project page answered %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "quarry") {
		t.Fatal("POSITIVE CONTROL FAILED: the un-narrowed project page does not name the project")
	}
	for _, bearer := range narrowedBearers {
		rec := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodGet, path, bearer, nil)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), inviteRefusal) {
			t.Fatalf("A NARROWED BEARER WAS SHOWN ITS OWNER'S PROJECT INVITATIONS (status %d); want the "+
				"uniform 404 %q:\n%s", rec.Code, inviteRefusal, rec.Body.String())
		}
	}
}

// TestANarrowedBearerCannotRevokeAnInvitation covers `POST /invite/revoke`.
func TestANarrowedBearerCannotRevokeAnInvitation(t *testing.T) {
	authority := narrowedWorld(t)
	for _, bearer := range narrowedBearers {
		inviting, store, digest := seededInvite(t, authority)
		rec := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodPost, InviteRevokePath, bearer,
			url.Values{FieldDigest: {digest}, FieldProject: {string(fixtureProject)}})
		if rec.Code == http.StatusSeeOther || !store.rows[digest].RevokedAt.IsZero() {
			t.Fatalf("A NARROWED BEARER REVOKED ITS OWNER'S INVITATION (status %d)", rec.Code)
		}
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), inviteWriteRefusal) {
			t.Fatalf("a narrowed bearer's revoke answered %d, want the uniform 403 %q", rec.Code, inviteWriteRefusal)
		}
	}
	// POSITIVE CONTROL: the un-narrowed bearer CAN revoke, so the refusals are about the narrowing.
	inviting, store, digest := seededInvite(t, authority)
	rec := newLiveOver(t, authority, inviting, nil, nil).bearerDo(http.MethodPost, InviteRevokePath, testCredential,
		url.Values{FieldDigest: {digest}, FieldProject: {string(fixtureProject)}})
	if rec.Code != http.StatusSeeOther || store.rows[digest].RevokedAt.IsZero() {
		t.Fatalf("POSITIVE CONTROL FAILED: the un-narrowed bearer could not revoke (status %d)", rec.Code)
	}
}

// TestANarrowedAdminBearerIsOfferedNoShareCandidates covers the share flow's candidate list,
// which is membership-derived: a caller narrowed to `quarry-notes` (where it holds admin) must
// not be shown — or be able to share with — a collaborator from `kiln`, a project its
// narrowing has nothing to do with.
func TestANarrowedAdminBearerIsOfferedNoShareCandidates(t *testing.T) {
	authority := narrowedWorld(t)
	page := SharePath + "?" + url.Values{QueryScope: {string(fixtureScope)}}.Encode()
	share := url.Values{FieldScope: {string(fixtureScope)}, FieldSubject: {string(fixtureCollaborator)},
		FieldVerb: {string(control.VerbRead)}}

	// POSITIVE CONTROL: un-narrowed, the collaborator IS offered, and the share passes the
	// subject check (whatever the read-only fixture authority then answers, it is not the
	// subject refusal).
	rec := newLiveOver(t, authority, nil, nil, nil).bearerDo(http.MethodGet, page, testCredential, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), string(fixtureCollaborator)) {
		t.Fatalf("POSITIVE CONTROL FAILED: the un-narrowed share page (status %d) does not offer the collaborator",
			rec.Code)
	}

	// The narrowed admin bearer: subset and equal-to-full both hold admin on quarry-notes.
	// (Narrowed-to-nothing cannot reach the scope page at all, which the admin check decides.)
	for _, bearer := range []string{narrowedToOneToken, narrowedToAllToken} {
		rec := newLiveOver(t, authority, nil, nil, nil).bearerDo(http.MethodGet, page, bearer, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("precondition: a narrowed bearer with admin on quarry-notes got %d on its share page", rec.Code)
		}
		if strings.Contains(rec.Body.String(), string(fixtureCollaborator)) ||
			strings.Contains(rec.Body.String(), string(fixtureProjectKiln)) {
			t.Fatal("A NARROWED BEARER WAS OFFERED SHARE CANDIDATES FROM ITS OWNER'S MEMBERSHIPS (a collaborator " +
				"from `kiln`, which its narrowing excludes)")
		}
		rec = newLiveOver(t, authority, nil, nil, nil).bearerDo(http.MethodPost, SharePath, bearer, share)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), shareWriteRefusal) {
			t.Fatalf("a narrowed bearer's share with a membership-derived collaborator answered %d, want the "+
				"uniform 403 %q", rec.Code, shareWriteRefusal)
		}
	}
}

// TestEveryMembershipDecisionActsAsMembershipActor is the LEDGER for the rule
// `membershipActor` states: every call into `Inviting` or `Sharing` that hands over a
// `control.Principal` passes `membershipActor(id)`, except the named exemptions below.
//
// 🔴 IT DERIVES WHICH METHODS TAKE AN ACTOR FROM THE INTERFACES, NOT FROM A LIST HERE, so a
// method added to either interface with a principal parameter is covered on the day it lands.
// And it compares the WHOLE set of (handler, method, actor) triples against a literal, so it
// fails when a site bypasses the helper, when a new site appears, and when one disappears.
// A battery row mutating `membershipActor`'s body cannot see a call site that never calls it;
// this can.
func TestEveryMembershipDecisionActsAsMembershipActor(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var parsed []*ast.File
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", f, err)
		}
		parsed = append(parsed, af)
	}

	// Which parameter of which interface method is a `control.Principal`.
	actorParam := map[string]map[string]int{} // field name on Server -> method -> index
	for iface, field := range map[string]string{"Inviting": "inviting", "Sharing": "sharing"} {
		actorParam[field] = map[string]int{}
		for _, af := range parsed {
			ast.Inspect(af, func(n ast.Node) bool {
				ts, ok := n.(*ast.TypeSpec)
				if !ok || ts.Name.Name != iface {
					return true
				}
				it, ok := ts.Type.(*ast.InterfaceType)
				if !ok {
					return false
				}
				for _, m := range it.Methods.List {
					ft, ok := m.Type.(*ast.FuncType)
					if !ok {
						continue
					}
					idx := 0
					for _, p := range ft.Params.List {
						n := len(p.Names)
						if n == 0 {
							n = 1
						}
						if astText(fset, p.Type) == "control.Principal" {
							for _, name := range m.Names {
								actorParam[field][name.Name] = idx
							}
						}
						idx += n
					}
				}
				return false
			})
		}
	}
	// POSITIVE CONTROL on the derivation itself: the methods known to take an actor today.
	for _, want := range []string{"inviting.Invitable", "inviting.Mint", "inviting.Revoke",
		"inviting.RedeemFor", "sharing.Candidates", "sharing.Share", "sharing.Unshare"} {
		parts := strings.SplitN(want, ".", 2)
		if _, ok := actorParam[parts[0]][parts[1]]; !ok {
			t.Fatalf("the interface scan did not find %s taking a control.Principal — the instrument is broken, "+
				"and an empty ledger below would mean nothing", want)
		}
	}

	var got []string
	for _, af := range parsed {
		for _, decl := range af.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				inner, ok := sel.X.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				methods, known := actorParam[inner.Sel.Name]
				if !known {
					return true
				}
				idx, takesActor := methods[sel.Sel.Name]
				if !takesActor || idx >= len(call.Args) {
					return true
				}
				got = append(got, fn.Name.Name+" "+inner.Sel.Name+"."+sel.Sel.Name+" "+astText(fset, call.Args[idx]))
				return true
			})
		}
	}
	sort.Strings(got)

	want := []string{
		"handleInvite inviting.Invitable membershipActor(id)",
		"handleInvite inviting.Mint membershipActor(id)",
		"handleInvitePage inviting.Invitable membershipActor(id)",
		"handleInviteRevoke inviting.Revoke membershipActor(id)",
		// EXEMPT: the principal comes from a provider identity with no credential behind it,
		// so there is no narrowing to lose.
		"handleOAuthCallback inviting.RedeemFor principal",
		// EXEMPT: the actor here is ATTRIBUTION (the journal's `actor`), and the authority is
		// checked against the narrowed `id.Auth` passed beside it.
		"handleShare sharing.Candidates membershipActor(id)",
		"handleShare sharing.Share id.Principal",
		"handleSharePage sharing.Candidates membershipActor(id)",
		"handleUnshare sharing.Unshare id.Principal",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the membership-actor ledger moved. Every actor-taking Inviting/Sharing call must pass "+
			"membershipActor(id) unless it is a named exemption; a NEW site must be added here deliberately.\n"+
			"--- got\n%s\n--- want\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func astText(fset *token.FileSet, n ast.Node) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, n); err != nil {
		return "<unprintable>"
	}
	return b.String()
}
