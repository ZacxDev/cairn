package ui

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 S5 OF THE SCOPE-MAIL PLAN: THE MEMOS TAB AND ITS TWO WRITES, DRIVEN THROUGH THE REAL DISPATCHER
// OVER THE REAL `StoreSource`. Every name, id and date below is SYNTHETIC — this repository is public.
//
// ⚠ THE STORE HERE IS A FAKE, AND WHAT THAT DOES AND DOES NOT MEASURE IS STATED. `fakeMemos` asks the
// SAME `internal/memo` predicates `pgstore.MemoStore` asks, in the same order, so these tests measure
// what the UI renders and forwards over a store that behaves like the real one. Whether the REAL store
// refuses a forged send is measured against PostgreSQL in `cmd/cairn-ui/memotab_pgtest_test.go`, which
// drives this same handler through `cmd/cairn-ui`'s adapter.

// The memo world: alpha-notes is owned by writer-a (owner → read, write, admin); member-m is a
// MEMBER of its project (read, write — no admin); reader-r holds a READ grant on it and nothing else;
// outsider-o owns beta-notes and holds nothing on alpha-notes.
var (
	memoWriter   = control.DerivedID(control.PrefixUser, "memo-writer-a")
	memoMember   = control.DerivedID(control.PrefixUser, "memo-member-m")
	memoReader   = control.DerivedID(control.PrefixUser, "memo-reader-r")
	memoOutsider = control.DerivedID(control.PrefixUser, "memo-outsider-o")
	memoClock    = time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
)

// memoCookie is the session cookie every memo request carries; the CSRF token is derived from it.
const memoCookie = "a-session-cookie-the-memo-test-chose"

type memoPeople struct {
	model                           control.Model
	writer, member, reader, outside identity.Identity
}

func memoWorld(t *testing.T) memoPeople {
	t.Helper()
	at := memoClock
	user := func(id control.ID, n int, name string) control.Event {
		return control.Event{Kind: control.EventUserCreated, At: at, UserID: id, Provider: "fixture-provider",
			Subject: fmt.Sprintf("00000000-0000-4000-8000-0000000005%02d", n), DisplayName: name}
	}
	m, err := control.Replay([]control.Event{
		user(memoWriter, 1, "writer-a"),
		user(memoMember, 2, "member-m"),
		user(memoReader, 3, "reader-r"),
		user(memoOutsider, 4, "outsider-o"),
		{Kind: control.EventProjectCreated, At: at, ProjectID: browseProjectA, Name: "alpha", UserID: memoWriter},
		{Kind: control.EventMemberSet, At: at, ProjectID: browseProjectA, UserID: memoWriter, Role: control.RoleOwner},
		{Kind: control.EventMemberSet, At: at, ProjectID: browseProjectA, UserID: memoMember, Role: control.RoleMember},
		{Kind: control.EventScopeCreated, At: at, ScopeID: browseScopeA, DisplayName: "alpha-notes", ProjectID: browseProjectA},
		{Kind: control.EventProjectCreated, At: at, ProjectID: browseProjectB, Name: "beta", UserID: memoOutsider},
		{Kind: control.EventMemberSet, At: at, ProjectID: browseProjectB, UserID: memoOutsider, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: at, ScopeID: browseScopeB, DisplayName: "beta-notes", ProjectID: browseProjectB},
		{Kind: control.EventGranted, At: at, Actor: memoWriter, GrantID: control.DerivedID(control.PrefixGrant, "memo-read"),
			SubjectKind: control.KindUser, SubjectID: memoReader, ObjectKind: control.ObjectScope, ObjectID: browseScopeA,
			Verbs: control.NewVerbSet(control.VerbRead)},
	})
	if err != nil {
		t.Fatalf("building the memo world: %v", err)
	}
	as := func(id control.ID) identity.Identity {
		p, ok := m.PrincipalFor(control.KindUser, id)
		if !ok {
			t.Fatalf("the world does not hold %s", id)
		}
		return identity.Identity{Principal: p, Auth: control.Resolve(m, p)}
	}
	w := memoPeople{model: m, writer: as(memoWriter), member: as(memoMember), reader: as(memoReader), outside: as(memoOutsider)}
	// INSTRUMENT CONTROL: the authorities are what the assertions below assume.
	switch {
	case !memo.MaySend(w.writer.Auth, "alpha-notes") || !w.writer.Auth.VisibleScopes(control.VerbAdmin).Allows("alpha-notes"):
		t.Fatal("writer-a does not write and administer alpha-notes")
	case !memo.MaySend(w.member.Auth, "alpha-notes") || w.member.Auth.VisibleScopes(control.VerbAdmin).Allows("alpha-notes"):
		t.Fatal("member-m does not write alpha-notes WITHOUT admin")
	case !memo.MayRead(w.reader.Auth, "alpha-notes") || memo.MaySend(w.reader.Auth, "alpha-notes"):
		t.Fatal("reader-r does not READ ONLY alpha-notes")
	case memo.MayRead(w.outside.Auth, "alpha-notes"):
		t.Fatal("outsider-o reads alpha-notes")
	}
	return w
}

// fakeRow is one memo as a database row holds it.
type fakeRow struct {
	id               int64
	scope            string
	senderKind       control.Kind
	senderID         control.ID
	subject, body    string
	created, expires time.Time
	retracted        time.Time
	retractorKind    control.Kind
	retractorID      control.ID
}

// fakeMemos is a memo store that asks the predicates `pgstore.MemoStore` asks, in its order.
type fakeMemos struct {
	model    control.Model
	rows     []fakeRow
	next     int64
	sends    int // successful sends
	retracts int // successful retractions
	writes   int // every Send/Retract CALL, refused or not
	listErr  error
	// sendErr, when set, is what EVERY send answers, before any predicate — for the tests about what
	// the UI does WITH a store's refusal, which must not depend on this fake deciding one.
	sendErr error
}

func (f *fakeMemos) Model() control.Model { return f.model }

func (f *fakeMemos) stored(r fakeRow) memo.Stored {
	d := func(kind control.Kind, id control.ID) string {
		p, _ := f.model.PrincipalFor(kind, id)
		return p.Display
	}
	return memo.NewStored(memo.Fields{ID: r.id, Scope: r.scope, SenderKind: string(r.senderKind), SenderID: string(r.senderID),
		SenderDisplay: d(r.senderKind, r.senderID), Subject: r.subject, Body: r.body, CreatedAt: r.created, ExpiresAt: r.expires,
		RetractedAt: r.retracted, RetractorKind: string(r.retractorKind), RetractorID: string(r.retractorID),
		RetractorDisplay: d(r.retractorKind, r.retractorID)})
}

func (f *fakeMemos) All(_ context.Context, m control.Model, auth control.Authorization, scope string) ([]memo.Stored, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if !memo.MayRead(auth, scope) {
		return nil, memo.ErrNotFound
	}
	var rows []fakeRow
	for _, r := range f.rows {
		if r.scope == store.NormalizeRef(scope) {
			rows = append(rows, r)
		}
	}
	slices.SortFunc(rows, func(a, b fakeRow) int {
		if c := a.created.Compare(b.created); c != 0 {
			return c
		}
		return int(a.id - b.id)
	})
	out := make([]memo.Stored, 0, len(rows))
	for _, r := range rows {
		out = append(out, f.stored(r))
	}
	return out, nil
}

func (f *fakeMemos) Send(_ context.Context, caller control.Principal, auth control.Authorization, scope, subject, body string, ttl time.Duration) (int64, time.Time, error) {
	f.writes++
	if f.sendErr != nil {
		return 0, time.Time{}, f.sendErr
	}
	if !memo.MaySend(auth, scope) {
		return 0, time.Time{}, &memo.Refusal{Reason: memo.RefusedNoWrite}
	}
	scan, err := memo.NewScanner()
	if err != nil {
		return 0, time.Time{}, err
	}
	out, err := memo.CheckSend(subject, body, ttl, scan)
	if err != nil {
		return 0, time.Time{}, err
	}
	f.next++
	f.rows = append(f.rows, fakeRow{id: 100 + f.next, scope: store.NormalizeRef(scope), senderKind: caller.Kind, senderID: caller.ID,
		subject: out.Subject, body: out.Body, created: memoClock.Add(time.Duration(f.next) * time.Minute),
		expires: memoClock.Add(out.TTL)})
	f.sends++
	return 100 + f.next, memoClock, nil
}

func (f *fakeMemos) Retract(_ context.Context, caller control.Principal, auth control.Authorization, scope string, id int64) error {
	f.writes++
	if !memo.MayRead(auth, scope) {
		return memo.ErrNotFound
	}
	for i, r := range f.rows {
		if r.id != id || r.scope != store.NormalizeRef(scope) || !r.retracted.IsZero() {
			continue
		}
		if !memo.MayRetract(caller, auth, scope, string(r.senderKind), string(r.senderID)) {
			return &memo.Refusal{Reason: memo.RefusedNotPermitted}
		}
		f.rows[i].retracted, f.rows[i].subject, f.rows[i].body = memoClock.Add(time.Hour), "", ""
		f.rows[i].retractorKind, f.rows[i].retractorID = caller.Kind, caller.ID
		f.retracts++
		return nil
	}
	return memo.ErrNotFound
}

// memoRows is the world's three alpha-notes memos and one beta-notes memo, at pairwise-distinct times
// (the NEWEST is member-m's, so newest-first and oldest-first disagree on every position).
func memoRows() []fakeRow {
	exp := memoClock.Add(72 * time.Hour)
	return []fakeRow{
		{id: 11, scope: "alpha-notes", senderKind: control.KindUser, senderID: memoWriter,
			subject: "kiln schedule moves", body: "the second firing is now on the fourth", created: memoClock, expires: exp},
		{id: 12, scope: "alpha-notes", senderKind: control.KindUser, senderID: memoReader,
			subject: "from a reader", body: "sent while reader-r could still write", created: memoClock.Add(time.Minute), expires: exp},
		{id: 13, scope: "alpha-notes", senderKind: control.KindUser, senderID: memoMember,
			subject: "glaze batch ready", body: "batch seven is cured", created: memoClock.Add(2 * time.Minute), expires: exp},
		{id: 21, scope: "beta-notes", senderKind: control.KindUser, senderID: memoOutsider,
			subject: "beta only", body: "never shown on alpha", created: memoClock.Add(3 * time.Minute), expires: exp},
	}
}

func memoServer(t *testing.T, id identity.Identity, memos Memos) *Server {
	t.Helper()
	cfg := testConfig(t, staticAuth{id})
	cfg.Source = StoreSource{Root: arcsStore(t)}
	cfg.Memos = memos
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("the server did not build: %v", err)
	}
	return srv
}

// memoGet is a signed-in browser's GET: it carries the session cookie, so the page renders forms.
func memoGet(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: memoCookie})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	return rec
}

// memoPost is a form POST with the origin and CSRF token chosen by the caller ("same" = this server's).
func memoPost(srv *Server, path, origin, csrf string, form url.Values) *httptest.ResponseRecorder {
	form = cloneForm(form)
	if csrf != "" {
		form.Set(FieldCSRF, csrf)
	}
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	switch origin {
	case "":
	case "same":
		r.Header.Set("Origin", sameOriginOf(r))
	default:
		r.Header.Set("Origin", origin)
	}
	r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: memoCookie})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	return rec
}

// gatedPost is the request the tab's own forms make: same origin, this session's token.
func gatedPost(srv *Server, path string, form url.Values) *httptest.ResponseRecorder {
	return memoPost(srv, path, "same", identity.CSRFTokenFor(memoCookie), form)
}

func cloneForm(f url.Values) url.Values {
	out := url.Values{}
	for k, v := range f {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func sendForm(scope control.ID, subject, body string) url.Values {
	return url.Values{FieldScope: {string(scope)}, FieldMemoSubject: {subject}, FieldMemoBody: {body}, FieldTTLDays: {"3"}}
}

func retractForm(scope control.ID, id int64) url.Values {
	return url.Values{FieldScope: {string(scope)}, FieldMemoID: {fmt.Sprint(id)}}
}

// outcomeOf is the redirect's `outcome` and `reason`, after asserting it IS a 303 to the memos tab of
// `scope` — so a redirect that lands anywhere else cannot satisfy an outcome check.
func outcomeOf(t *testing.T, rec *httptest.ResponseRecorder, scope control.ID) (outcome, reason string) {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("answered %d %q, want a 303 back to the memos tab", rec.Code, rec.Body.String())
	}
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || u.Path != ScopePath || u.Query().Get(QueryID) != string(scope) || u.Query().Get(QueryTab) != TabMemos {
		t.Fatalf("redirected to %q, want the memos tab of %s", rec.Header().Get("Location"), scope)
	}
	return u.Query().Get(QueryOutcome), u.Query().Get(QueryMemoReason)
}

// TestTheMemoRowsAreBehindBothCrossSiteGates — the S5 test plan's first item: `POST /memo` and
// `POST /memo/retract` without an Origin, with a foreign one, without a CSRF token and with another
// session's token get the EXISTING refusals, each asserted by its OWN message (both gates answer 403,
// so a status alone would score one gate's refusal as the other's), and the store is never CALLED. The
// positive control is the same request with both gates satisfied. RED with either row declared
// `classPublic` (`ui-memo-send-row-declared-public`, `ui-memo-retract-row-declared-public`): a public
// row dispatches ahead of the chain and so ahead of the CSRF gate — the shape a future class exemption
// takes. The gates' own predicates are `TestACrossSiteStateChangeIsRefusedBeforeAuthentication`'s and
// the CSRF test's to pin.
func TestTheMemoRowsAreBehindBothCrossSiteGates(t *testing.T) {
	w := memoWorld(t)
	for _, row := range []struct {
		path string
		form url.Values
	}{
		{MemoPath, sendForm(browseScopeA, "gated", "a gated memo")},
		{MemoRetractPath, retractForm(browseScopeA, 11)},
	} {
		for _, arm := range []struct{ name, origin, csrf, want string }{
			{"no Origin header", "", identity.CSRFTokenFor(memoCookie), crossSiteRefusal},
			{"a foreign Origin", "https://evil.invalid", identity.CSRFTokenFor(memoCookie), crossSiteRefusal},
			{"no CSRF token", "same", "", csrfRefusal},
			{"another session's CSRF token", "same", identity.CSRFTokenFor("some-other-session-cookie"), csrfRefusal},
		} {
			t.Run(row.path+"/"+arm.name, func(t *testing.T) {
				memos := &fakeMemos{model: w.model, rows: memoRows()}
				rec := memoPost(memoServer(t, w.writer, memos), row.path, arm.origin, arm.csrf, row.form)
				if rec.Code != http.StatusForbidden || rec.Body.String() != arm.want {
					t.Errorf("answered %d %q, want 403 %q — the gate that must refuse this did not", rec.Code, rec.Body.String(), arm.want)
				}
				if memos.writes != 0 {
					t.Errorf("a REFUSED request reached the memo store (%d write call(s))", memos.writes)
				}
			})
		}
	}
	// POSITIVE CONTROL: both gates satisfied, the same world — the refusals above are about the gates.
	memos := &fakeMemos{model: w.model, rows: memoRows()}
	srv := memoServer(t, w.writer, memos)
	if out, _ := outcomeOf(t, gatedPost(srv, MemoPath, sendForm(browseScopeA, "gated", "a gated memo")), browseScopeA); out != memoOutcomeSent || memos.sends != 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: a gated send answered outcome %q and stored %d", out, memos.sends)
	}
	if out, _ := outcomeOf(t, gatedPost(srv, MemoRetractPath, retractForm(browseScopeA, 11)), browseScopeA); out != memoOutcomeRetracted || memos.retracts != 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: a gated retraction answered outcome %q and retracted %d", out, memos.retracts)
	}
}

// formsOf is every form ACTION on the page, and the memo ids each retract form names.
var (
	formActionRE = regexp.MustCompile(`<form [^>]*action="([^"]+)"`)
	retractIDRE  = regexp.MustCompile(`name="` + FieldMemoID + `" value="([0-9]+)"`)
)

func retractIDs(body string) []string {
	var out []string
	for _, m := range retractIDRE.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	slices.Sort(out)
	return out
}

func hasForm(body, action string) bool {
	for _, m := range formActionRE.FindAllStringSubmatch(body, -1) {
		if m[1] == action {
			return true
		}
	}
	return false
}

// TestAReadOnlyViewerSeesTheMemosTabButNoSendForm — the S5 test plan's second item, its RENDER half:
// a reader sees the tab, its panel and the memos, and no send form; a writer sees the form (the
// positive control, same scope, same store). RED with the form's condition dropped
// (`ui-memo-form-ignores-maysend`).
func TestAReadOnlyViewerSeesTheMemosTabButNoSendForm(t *testing.T) {
	w := memoWorld(t)
	for _, tc := range []struct {
		who      string
		id       identity.Identity
		wantForm bool
	}{
		{"reader-r (read only)", w.reader, false},
		{"member-m (read, write) — POSITIVE CONTROL", w.member, true},
		{"writer-a (owner) — POSITIVE CONTROL", w.writer, true},
	} {
		rec := memoGet(t, memoServer(t, tc.id, &fakeMemos{model: w.model, rows: memoRows()}), scopeTabURL(browseScopeA, TabMemos))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: the memos tab answered %d: %s", tc.who, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, `id="scope-memos"`) || !strings.Contains(body, `data-tab="memos"`) {
			t.Errorf("%s: the memos panel or its tab marker is missing", tc.who)
		}
		if !strings.Contains(body, `data-memo-id="11"`) {
			t.Errorf("%s: a memo of the scope is not listed — the tab is not reading the store", tc.who)
		}
		if got := hasForm(body, MemoPath); got != tc.wantForm {
			t.Errorf("%s: send form present=%v, want %v", tc.who, got, tc.wantForm)
		}
		if got := strings.Contains(body, `data-memo="read-only"`); got == tc.wantForm {
			t.Errorf("%s: the read-only note present=%v, want %v", tc.who, got, !tc.wantForm)
		}
	}
}

// TestAStoresNoWriteRefusalComesBackAsTheClosedSentenceOnTheTab — what THIS package does with a
// store's refusal of a send: the 303 back to the tab carries the closed reason, nothing else, and the
// tab renders that reason's one sentence. POSITIVE CONTROL: the same request against a store that
// accepts it answers `memo-sent`.
//
// ⚠ IT DOES NOT MEASURE WHO IS REFUSED, AND IT USED TO CLAIM TO (#242 round 0, D3). Its first half
// drove a read-only viewer's forged POST and asserted `no-write` — over `fakeMemos`, which calls
// `memo.MaySend` ITSELF, so the assertion was about the fake. The refusal here is HANDED to the
// handler (`sendErr`). That a real read-only viewer's forged send is refused is measured where the
// real store is: `cmd/cairn-ui`'s pgtest-tier `TestTheMemoTabsForgedSendIsRefusedByTheRealStore`;
// that the PAGE asks `memo.MaySend` is `TestAReadOnlyViewerSeesTheMemosTabButNoSendForm`.
func TestAStoresNoWriteRefusalComesBackAsTheClosedSentenceOnTheTab(t *testing.T) {
	w := memoWorld(t)
	memos := &fakeMemos{model: w.model, rows: memoRows(), sendErr: &memo.Refusal{Reason: memo.RefusedNoWrite}}
	before := len(memos.rows)
	srv := memoServer(t, w.writer, memos)
	out, reason := outcomeOf(t, gatedPost(srv, MemoPath, sendForm(browseScopeA, "refused", "a refused memo")), browseScopeA)
	if out != memoOutcomeRefused || reason != string(memo.RefusedNoWrite) {
		t.Errorf("a send the store refused as no-write answered outcome %q reason %q, want %q %q",
			out, reason, memoOutcomeRefused, memo.RefusedNoWrite)
	}
	if memos.writes != 1 || len(memos.rows) != before {
		t.Errorf("the send did not reach the store exactly once: %d call(s), %d → %d rows", memos.writes, before, len(memos.rows))
	}
	page := memoGet(t, srv, scopeTabURL(browseScopeA, TabMemos)+"&"+
		url.Values{QueryOutcome: {out}, QueryMemoReason: {reason}}.Encode())
	if want := "The memo was refused: " + memoReasons[memo.RefusedNoWrite] + "."; !strings.Contains(pageText(page.Body.String()), want) {
		t.Errorf("the refusal banner does not read %q", want)
	}
	// POSITIVE CONTROL: the same request, to a store that accepts it.
	pos := &fakeMemos{model: w.model, rows: memoRows()}
	if out, _ := outcomeOf(t, gatedPost(memoServer(t, w.writer, pos), MemoPath, sendForm(browseScopeA, "refused", "a refused memo")), browseScopeA); out != memoOutcomeSent || pos.sends != 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: an accepted send answered %q and stored %d", out, pos.sends)
	}
}

// The send form's two text fields as the page renders them, and its lifetime field.
var (
	draftSubjectRe = regexp.MustCompile(`<input type="text" id="memo-subject" name="memo_subject"[^>]* value="([^"]*)">`)
	draftBodyRe    = regexp.MustCompile(`(?s)<textarea id="memo-body" name="memo_body"[^>]*>(.*?)</textarea>`)
	draftTTLRe     = regexp.MustCompile(`<input type="number" id="memo-ttl" name="ttl_days"[^>]* value="([^"]*)">`)
)

// formDraft is what the send form on `body` holds: subject, text and lifetime, unescaped.
func formDraft(t *testing.T, body string) (subject, text, ttl string) {
	t.Helper()
	s, b, d := draftSubjectRe.FindStringSubmatch(body), draftBodyRe.FindStringSubmatch(body), draftTTLRe.FindStringSubmatch(body)
	if s == nil || b == nil || d == nil {
		t.Fatalf("the page carries no send form with all three fields (subject %v, text %v, ttl %v):\n%s", s != nil, b != nil, d != nil, body)
	}
	return html.UnescapeString(s[1]), html.UnescapeString(b[1]), html.UnescapeString(d[1])
}

// TestARefusedOverLongSendKeepsTheTypedTextInTheForm — #242 round 1: the form's `maxlength` counts
// UTF-16 units and the store counts UTF-8 BYTES, so non-ASCII text the form accepts is refused by the
// store. The refusal must not cost the text: the answer is the memos tab itself (422, `no-store`, no
// redirect) with the refusal's sentence and the form holding exactly what was typed — subject, text
// and the lifetime, which is 7 here because the form's default is 3.
//
// Two points on the dimension, each UNDER its field's `maxlength` in UTF-16 units and OVER the store's
// limit in bytes: a 61-character subject of two-byte letters (122 bytes > 120) and a 1400-character
// text of three-byte characters (4200 bytes > 4096). POSITIVE CONTROL, per row: the SAME number of
// ASCII characters is stored — so it is the bytes that refuse, and the form really does send.
//
// RED with the draft branch removed (`ui-memo-refusal-drops-the-draft`: a 303), and with either field
// not re-rendered (`ui-memo-draft-subject-not-rendered`, `ui-memo-draft-body-not-rendered`).
func TestARefusedOverLongSendKeepsTheTypedTextInTheForm(t *testing.T) {
	w := memoWorld(t)
	for _, tc := range []struct {
		name, subject, body string
		reason              memo.RefusalReason
		asciiSubject, ascii string
	}{
		{"subject", strings.Repeat("é", 61), "the kiln <b>cools</b> & \"rests\"\nuntil the fourth", memo.RefusedSubjectSize,
			strings.Repeat("e", 61), "the kiln cools until the fourth"},
		{"text", "kiln: second firing", strings.Repeat("語", 1400), memo.RefusedBodySize,
			"kiln: second firing", strings.Repeat("g", 1400)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if n := len(utf16.Encode([]rune(tc.subject))); n > memo.MaxSubjectBytes {
				t.Fatalf("the fixture's subject is %d UTF-16 units, over the form's own maxlength — it would not be typed", n)
			}
			if n := len(utf16.Encode([]rune(tc.body))); n > memo.MaxBodyBytes {
				t.Fatalf("the fixture's text is %d UTF-16 units, over the form's own maxlength — it would not be typed", n)
			}
			memos := &fakeMemos{model: w.model, rows: memoRows()}
			before := len(memos.rows)
			form := sendForm(browseScopeA, tc.subject, tc.body)
			form.Set(FieldTTLDays, "7")
			rec := gatedPost(memoServer(t, w.writer, memos), MemoPath, form)
			if rec.Code != http.StatusUnprocessableEntity || rec.Header().Get("Location") != "" {
				t.Fatalf("an over-long send answered %d Location=%q, want a 422 page and no redirect — a redirect drops the typed text",
					rec.Code, rec.Header().Get("Location"))
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("the re-rendered form is Cache-Control %q, want no-store: it carries the sender's draft", cc)
			}
			page := rec.Body.String()
			if want := "The memo was refused: " + memoReasons[tc.reason] + "."; !strings.Contains(pageText(page), want) {
				t.Errorf("the page does not carry the refusal %q", want)
			}
			if !strings.Contains(page, `id="scope-memos"`) || !strings.Contains(pageText(page), MemoLimitsNote) {
				t.Error("the page is not the memos tab with its byte-limits note")
			}
			subject, text, ttl := formDraft(t, page)
			if subject != tc.subject {
				t.Errorf("the form's subject is %q, want what was typed (%q)", subject, tc.subject)
			}
			if text != tc.body {
				t.Errorf("the form's text is %d bytes, want the %d typed", len(text), len(tc.body))
			}
			if ttl != "7" {
				t.Errorf("the form's lifetime is %q, want the 7 that was typed", ttl)
			}
			if len(memos.rows) != before || memos.sends != 0 {
				t.Errorf("the refused send stored a memo: %d → %d rows", before, len(memos.rows))
			}
			// POSITIVE CONTROL: as many ASCII characters fit, and are stored.
			pos := &fakeMemos{model: w.model, rows: memoRows()}
			if out, _ := outcomeOf(t, gatedPost(memoServer(t, w.writer, pos), MemoPath, sendForm(browseScopeA, tc.asciiSubject, tc.ascii)), browseScopeA); out != memoOutcomeSent || pos.sends != 1 {
				t.Fatalf("POSITIVE CONTROL FAILED: the ASCII memo of the same length answered %q and stored %d", out, pos.sends)
			}
		})
	}
	// A GET of the tab holds no draft: the form is empty and its lifetime is the default.
	subject, text, ttl := formDraft(t, memoGet(t, memoServer(t, w.writer, &fakeMemos{model: w.model}), scopeTabURL(browseScopeA, TabMemos)).Body.String())
	if subject != "" || text != "" || ttl != "3" {
		t.Errorf("a plain GET's form holds %q / %q / %q, want empty, empty and the default 3", subject, text, ttl)
	}
}

// TestOnlyTheClosedSetOfRefusalsKeepsTheDraft — `memoDraftKept`, over every reason the store has: the
// five a sender fixes by editing or waiting re-render the form with the draft; `secret` and
// `characters` REDIRECT and their answer does not carry the typed text anywhere (a suspected
// credential is never served back; text with a disguising code point is never emitted raw); `no-write`
// and `not-permitted` redirect too. A reason this table does not name fails it, so a new
// `memo.RefusalReason` is a decision taken here.
//
// The refusal is HANDED to the handler (`sendErr`), with a `secret` rule name, so the row is about
// what the UI does with each reason and not about whether this fake's scan fires.
// RED for `secret` under `ui-memo-draft-kept-for-a-suspected-secret`.
func TestOnlyTheClosedSetOfRefusalsKeepsTheDraft(t *testing.T) {
	w := memoWorld(t)
	const subject, body = "typed-subject-marker", "typed-text-marker"
	kept := map[memo.RefusalReason]bool{
		memo.RefusedSubjectSize: true, memo.RefusedBodySize: true, memo.RefusedTTL: true,
		memo.RefusedQuota: true, memo.RefusedLiveLimit: true,
		memo.RefusedSecret: false, memo.RefusedCharacters: false, memo.RefusedNoWrite: false, memo.RefusedNotPermitted: false,
	}
	if len(kept) != len(memoReasons) {
		t.Fatalf("this table names %d refusal reasons and memoReasons %d — decide whether the new one keeps the draft", len(kept), len(memoReasons))
	}
	for reason, want := range kept {
		if _, ok := memoReasons[reason]; !ok {
			t.Fatalf("%q is not a reason the tab has a sentence for", reason)
		}
		memos := &fakeMemos{model: w.model, sendErr: &memo.Refusal{Reason: reason, Rule: "github-token"}}
		rec := gatedPost(memoServer(t, w.writer, memos), MemoPath, sendForm(browseScopeA, subject, body))
		echoed := strings.Contains(rec.Body.String(), subject) || strings.Contains(rec.Body.String(), body) ||
			strings.Contains(rec.Header().Get("Location"), "typed-")
		switch {
		case want && (rec.Code != http.StatusUnprocessableEntity || !echoed):
			t.Errorf("%s: answered %d (draft present=%v), want the 422 form holding the draft", reason, rec.Code, echoed)
		case !want && (rec.Code != http.StatusSeeOther || echoed):
			t.Errorf("%s: answered %d (draft present=%v), want a 303 that carries none of the typed text", reason, rec.Code, echoed)
		}
	}
}

// TestAFailingMemoStoreIsA500AndNeverAnEmptyTab — a store error is the caller's, not a short list.
func TestAFailingMemoStoreIsA500AndNeverAnEmptyTab(t *testing.T) {
	w := memoWorld(t)
	rec := memoGet(t, memoServer(t, w.writer, &fakeMemos{model: w.model, listErr: errors.New("boom")}), scopeTabURL(browseScopeA, TabMemos))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "No memo on this scope") {
		t.Errorf("a failing memo store answered %d %q, want a 500 and no empty-tab copy", rec.Code, rec.Body.String())
	}
}

// TestAWriteNamingAScopeTheViewerCannotReadIsTheBrowseRefusal — the uniform miss on the two writes:
// a scope the viewer cannot read and a scope that does not exist get the SAME bytes, and the store is
// never called. POSITIVE CONTROL: the same scope id for its owner is a write.
func TestAWriteNamingAScopeTheViewerCannotReadIsTheBrowseRefusal(t *testing.T) {
	w := memoWorld(t)
	absent := control.DerivedID(control.PrefixScope, "no-such-memo-scope")
	for _, path := range []string{MemoPath, MemoRetractPath} {
		memos := &fakeMemos{model: w.model, rows: memoRows()}
		srv := memoServer(t, w.reader, memos)
		form := func(scope control.ID) url.Values {
			if path == MemoPath {
				return sendForm(scope, "s", "b")
			}
			return retractForm(scope, 21)
		}
		notYours := gatedPost(srv, path, form(browseScopeB))
		missing := gatedPost(srv, path, form(absent))
		if notYours.Code != http.StatusNotFound || notYours.Body.String() != browseRefusal ||
			missing.Code != notYours.Code || missing.Body.String() != notYours.Body.String() {
			t.Errorf("%s: not-yours answered %d %q, absent %d %q — want both 404 %q", path,
				notYours.Code, notYours.Body.String(), missing.Code, missing.Body.String(), browseRefusal)
		}
		if memos.writes != 0 {
			t.Errorf("%s: a write naming an unreadable scope reached the store", path)
		}
		owner := &fakeMemos{model: w.model, rows: memoRows()}
		if rec := gatedPost(memoServer(t, w.outside, owner), path, form(browseScopeB)); rec.Code != http.StatusSeeOther || owner.writes != 1 {
			t.Errorf("%s: POSITIVE CONTROL FAILED: beta's owner answered %d with %d store call(s)", path, rec.Code, owner.writes)
		}
	}
}

// TestTheRetractButtonFollowsMayRetractMemo — Q19's ONE rule decides the button: the sender (through a
// credential that reads the scope) and a scope admin who reads it. Per viewer, the EXACT set of memo
// ids carrying a retract form, over three live memos by three senders and one tombstone. RED with the
// button's predicate replaced by "can write" (`ui-memo-retract-button-asks-maysend`) and by "always"
// (`ui-memo-retract-button-ignores-the-rule`).
func TestTheRetractButtonFollowsMayRetractMemo(t *testing.T) {
	w := memoWorld(t)
	rows := memoRows()
	rows = append(rows, fakeRow{id: 14, scope: "alpha-notes", senderKind: control.KindUser, senderID: memoMember,
		created: memoClock.Add(-time.Minute), expires: memoClock.Add(time.Hour),
		retracted: memoClock, retractorKind: control.KindUser, retractorID: memoMember})
	for _, tc := range []struct {
		who  string
		id   identity.Identity
		want []string
	}{
		{"writer-a, scope admin: every LIVE memo, never the tombstone", w.writer, []string{"11", "12", "13"}},
		{"member-m, no admin: its own live memo only", w.member, []string{"13"}},
		{"reader-r, read only: its own memo (sender rule) only", w.reader, []string{"12"}},
	} {
		body := memoGet(t, memoServer(t, tc.id, &fakeMemos{model: w.model, rows: rows}), scopeTabURL(browseScopeA, TabMemos)).Body.String()
		if got := retractIDs(body); !slices.Equal(got, tc.want) {
			t.Errorf("%s: retract buttons on %v, want %v", tc.who, got, tc.want)
		}
	}
}

// TestMemoTextRendersAsTextAndThroughTheRenderSideReplacement — the S5 test plan's third item, plus
// decision 19's other half. A subject and a body carrying `<script>` render ESCAPED (gomponents' text
// node), and a zero-width space and a right-to-left override — which escaping leaves intact — render as
// U+FFFD (the render-side replacement), in every position on the page. RED with the body written
// through `g.Raw` (`ui-memo-body-rendered-raw`, also caught by the Raw ban) and with `memo.Render`
// passing the body through unsanitised (`memo-render-skips-the-body-replacement`).
func TestMemoTextRendersAsTextAndThroughTheRenderSideReplacement(t *testing.T) {
	w := memoWorld(t)
	const zwsp, rlo = "​", "‮"
	rows := []fakeRow{{id: 31, scope: "alpha-notes", senderKind: control.KindUser, senderID: memoWriter,
		subject: `<script>alert("subject")</script>`, body: "<script>alert(1)</script> hid" + zwsp + "den " + rlo + "txet",
		created: memoClock, expires: memoClock.Add(time.Hour)}}
	body := memoGet(t, memoServer(t, w.reader, &fakeMemos{model: w.model, rows: rows}), scopeTabURL(browseScopeA, TabMemos)).Body.String()
	// INSTRUMENT CONTROL: the memo is on the page at all, so the absences below are about its form.
	if !strings.Contains(body, `data-memo-id="31"`) {
		t.Fatalf("the hostile memo is not listed, so nothing below measures its rendering:\n%s", body)
	}
	for _, bad := range []string{`<script>alert`, zwsp, rlo} {
		if strings.Contains(body, bad) {
			t.Errorf("the page carries %q raw — memo text reached it without escaping or the replacement", bad)
		}
	}
	for _, want := range []string{
		`&lt;script&gt;alert(&#34;subject&#34;)&lt;/script&gt;`,
		`&lt;script&gt;alert(1)&lt;/script&gt; hid` + "�" + `den ` + "�" + `txet`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not carry the memo as escaped, replaced text %q", want)
		}
	}
}

// TestTheMemosTabIsNewestFirstWithTombstonesAndLapsedSenders — decisions 10, 12 and 15: rows newest
// first; a retracted memo shows its tombstone line and NO subject or body; a sender who can no longer
// write the scope is marked. The beta-notes memo never appears on alpha's tab.
func TestTheMemosTabIsNewestFirstWithTombstonesAndLapsedSenders(t *testing.T) {
	w := memoWorld(t)
	rows := memoRows()
	rows[0].retracted, rows[0].retractorKind, rows[0].retractorID = memoClock.Add(time.Hour), control.KindUser, memoWriter
	rows[0].subject, rows[0].body = "", ""
	body := memoGet(t, memoServer(t, w.writer, &fakeMemos{model: w.model, rows: rows}), scopeTabURL(browseScopeA, TabMemos)).Body.String()

	var order []string
	for _, m := range regexp.MustCompile(`data-memo-id="([0-9]+)"`).FindAllStringSubmatch(body, -1) {
		order = append(order, m[1])
	}
	if want := []string{"13", "12", "11"}; !slices.Equal(order, want) {
		t.Errorf("the tab lists %v, want newest first %v (and never beta's memo 21)", order, want)
	}
	item := func(id string) string {
		i := strings.Index(body, `data-memo-id="`+id+`"`)
		if i < 0 {
			t.Fatalf("memo %s is not on the page", id)
		}
		rest := body[i:]
		if j := strings.Index(rest, "</li>"); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	tomb := item("11")
	if !strings.Contains(tomb, `data-memo="tombstone"`) || !strings.Contains(pageText(tomb), "retracted by writer-a (user) at 2000-01-02T04:04:05Z") {
		t.Errorf("the retracted memo does not show its tombstone line:\n%s", tomb)
	}
	if strings.Contains(tomb, `data-memo="subject"`) || strings.Contains(tomb, `data-memo="body"`) {
		t.Errorf("the retracted memo still renders a subject or body:\n%s", tomb)
	}
	if !strings.Contains(item("12"), `data-memo="sender-lapsed"`) {
		t.Error("reader-r's memo is not marked — reader-r cannot write alpha-notes now (decision 12)")
	}
	if strings.Contains(item("13"), `data-memo="sender-lapsed"`) {
		t.Error("member-m's memo is marked lapsed, but member-m still writes alpha-notes")
	}
	if !strings.Contains(item("13"), "member-m (user)") || !strings.Contains(item("13"), "glaze batch ready") {
		t.Errorf("member-m's memo does not show its sender line and subject:\n%s", item("13"))
	}
}

// TestTheRetractionCopyIsOnTheTabWhole — "retraction stops further delivery only" (S5 row, decision
// 10), pinned as the WHOLE normalised sentence, because a guard on words is walkable by rewording.
func TestTheRetractionCopyIsOnTheTabWhole(t *testing.T) {
	w := memoWorld(t)
	body := memoGet(t, memoServer(t, w.writer, &fakeMemos{model: w.model, rows: memoRows()}), scopeTabURL(browseScopeA, TabMemos)).Body.String()
	const want = "Retracting a memo stops further delivery only: a session that was already shown it keeps what it " +
		"saw, and nothing already printed into a session or a transcript is recalled."
	if MemoRetractionNote != want {
		t.Errorf("the retraction note reads %q, want %q", MemoRetractionNote, want)
	}
	if !strings.Contains(pageText(body), want) {
		t.Error("the memos tab does not carry the retraction note")
	}
}

// TestTheMemoRowsAnswerHonestlyWithNoMemoStore — `NoInviteStore`'s ruling: with no store the tab says
// so and the two writes answer 501 with the same sentence.
func TestTheMemoRowsAnswerHonestlyWithNoMemoStore(t *testing.T) {
	w := memoWorld(t)
	srv := memoServer(t, w.writer, nil)
	rec := memoGet(t, srv, scopeTabURL(browseScopeA, TabMemos))
	if rec.Code != http.StatusOK || !strings.Contains(pageText(rec.Body.String()), NoMemoStore) {
		t.Errorf("the no-store tab answered %d without the sentence", rec.Code)
	}
	if hasForm(rec.Body.String(), MemoPath) {
		t.Error("the no-store tab renders a send form that can only fail")
	}
	for _, path := range []string{MemoPath, MemoRetractPath} {
		if rec := gatedPost(srv, path, retractForm(browseScopeA, 11)); rec.Code != http.StatusNotImplemented || rec.Body.String() != NoMemoStore {
			t.Errorf("%s with no store answered %d %q, want 501 %q", path, rec.Code, rec.Body.String(), NoMemoStore)
		}
	}
}

// TestTheMemoOutcomeBannerIsAClosedSet — nothing the address bar carries is reflected: an unknown
// outcome renders nothing, an unknown reason the generic line, and a rule name only from the confident
// set.
func TestTheMemoOutcomeBannerIsAClosedSet(t *testing.T) {
	for _, tc := range []struct {
		q    url.Values
		want string
	}{
		{url.Values{QueryOutcome: {memoOutcomeSent}}, "Memo sent."},
		{url.Values{QueryOutcome: {"<b>owned</b>"}}, ""},
		{url.Values{QueryOutcome: {memoOutcomeRefused}, QueryMemoReason: {"<b>owned</b>"}}, "The memo was refused."},
		{url.Values{QueryOutcome: {memoOutcomeRefused}, QueryMemoReason: {string(memo.RefusedSecret)}, QueryMemoRule: {"github-token"}},
			"The memo was refused: " + memoReasons[memo.RefusedSecret] + " (rule: github-token)."},
		{url.Values{QueryOutcome: {memoOutcomeRefused}, QueryMemoReason: {string(memo.RefusedSecret)}, QueryMemoRule: {"<b>owned</b>"}},
			"The memo was refused: " + memoReasons[memo.RefusedSecret] + "."},
	} {
		if got := memoOutcome(tc.q); got != tc.want {
			t.Errorf("memoOutcome(%v) = %q, want %q", tc.q, got, tc.want)
		}
	}
	// Every refusal reason the store can give has its own sentence.
	for _, r := range []memo.RefusalReason{memo.RefusedNoWrite, memo.RefusedQuota, memo.RefusedLiveLimit, memo.RefusedSubjectSize,
		memo.RefusedBodySize, memo.RefusedCharacters, memo.RefusedSecret, memo.RefusedTTL, memo.RefusedNotPermitted} {
		if memoReasons[r] == "" {
			t.Errorf("refusal reason %q has no sentence", r)
		}
	}
}

// TestAForgedOrStaleRetractionIsAnsweredByTheStoresRule — the retract write's two refusals, back on
// the tab: member-m (no admin) retracting writer-a's memo by hand is `not-permitted` and changes
// nothing; a second retraction of a memo already retracted (a stale page) is the `memo-gone` banner.
// POSITIVE CONTROL: the first retraction by the sender succeeds.
func TestAForgedOrStaleRetractionIsAnsweredByTheStoresRule(t *testing.T) {
	w := memoWorld(t)
	memos := &fakeMemos{model: w.model, rows: memoRows()}
	out, reason := outcomeOf(t, gatedPost(memoServer(t, w.member, memos), MemoRetractPath, retractForm(browseScopeA, 11)), browseScopeA)
	if out != memoOutcomeRefused || reason != string(memo.RefusedNotPermitted) || memos.retracts != 0 {
		t.Errorf("member-m retracting another's memo answered %q %q with %d retraction(s), want refused not-permitted and none",
			out, reason, memos.retracts)
	}
	srv := memoServer(t, w.writer, memos)
	if out, _ := outcomeOf(t, gatedPost(srv, MemoRetractPath, retractForm(browseScopeA, 11)), browseScopeA); out != memoOutcomeRetracted || memos.retracts != 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: the sender's retraction answered %q (%d)", out, memos.retracts)
	}
	if out, _ := outcomeOf(t, gatedPost(srv, MemoRetractPath, retractForm(browseScopeA, 11)), browseScopeA); out != memoOutcomeGone || memos.retracts != 1 {
		t.Errorf("a second retraction answered %q with %d retraction(s), want %q and still 1", out, memos.retracts, memoOutcomeGone)
	}
}
