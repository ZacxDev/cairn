package clientapi_test

// The client listener's OWN guards, measured without a database: the route ledger,
// authentication (machine tokens only, one refusal body), the failed-credential lockout,
// revocation through a forced `control.Cache.Refresh`, what reaches the wire, and request
// shapes refused before the store is reached. The handler runs over a REAL journal-backed
// `control.Cache` and the REAL `identity.MachineToken`; only the store is a stub, because every
// AUTHORISATION guard is the store's and is measured against the real one in the Postgres tier
// (`cmd/cairn-ui/clientapi_pgtest_test.go`).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/clientapi"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/pgstore"
)

var t0 = time.Date(2000, 2, 3, 4, 5, 6, 0, time.UTC)

// A synthetic world: writer-a owns a project holding alpha-notes, and holds two credentials —
// agents@host-a and agents@host-b, each narrowed to alpha-notes and read,write (decision 17).
const (
	uWriter = control.ID("usr_capiwriter")
	pNotes  = control.ID("prj_capinotes")
	sAlpha  = control.ID("scp_capialpha")

	tokA = "clientapi-test-agent-host-a-credential-000000"
	tokB = "clientapi-test-agent-host-b-credential-000000"
	// garbage is a well-formed bearer the authority never issued.
	garbage = "clientapi-test-garbage-never-issued-0000000000"
)

// stubStore records whether the store was reached at all, and answers with `stored`. It also
// RECORDS what the handler asked it — the list query and the send's TTL — because a parameter the
// handler parses and then drops is invisible to every test that looks only at the answer: before
// these fields existed, the list route's `after`, `limit` and `tombstones` had no test that could
// fail (S2 round 1).
type stubStore struct {
	mu        sync.Mutex
	calls     int
	stored    []memo.Stored
	lastQuery *pgstore.MemoQuery
	lastTTL   *time.Duration
}

func (s *stubStore) hit()   { s.mu.Lock(); s.calls++; s.mu.Unlock() }
func (s *stubStore) n() int { s.mu.Lock(); defer s.mu.Unlock(); return s.calls }

func (s *stubStore) Send(_ context.Context, _ control.Principal, _ control.Authorization, _, _, _ string, ttl time.Duration) (int64, time.Time, error) {
	s.hit()
	s.mu.Lock()
	s.lastTTL = &ttl
	s.mu.Unlock()
	return 7, t0, nil
}
func (s *stubStore) List(_ context.Context, _ control.Model, _ control.Authorization, q pgstore.MemoQuery) ([]memo.Stored, error) {
	s.hit()
	s.mu.Lock()
	s.lastQuery = &q
	s.mu.Unlock()
	return s.stored, nil
}
func (s *stubStore) Get(context.Context, control.Model, control.Authorization, string, int64) (memo.Stored, error) {
	s.hit()
	if len(s.stored) == 0 {
		return memo.Stored{}, memo.ErrNotFound
	}
	return s.stored[0], nil
}
func (s *stubStore) Retract(context.Context, control.Principal, control.Authorization, string, int64) error {
	s.hit()
	return nil
}

type rig struct {
	journal *control.FileStore
	cache   *control.Cache
	store   *stubStore
	h       *clientapi.Handler
}

func newRig(t *testing.T, limiter *netid.RateLimiter) *rig {
	t.Helper()
	fs, err := control.OpenFileStore(filepath.Join(t.TempDir(), "control.journal"))
	if err != nil {
		t.Fatal(err)
	}
	rw := control.NewVerbSet(control.VerbRead, control.VerbWrite)
	cred := func(id control.ID, tok, label string) []control.Event {
		return []control.Event{
			{Kind: control.EventCredentialIssued, At: t0, CredentialID: id, SubjectKind: control.KindUser, SubjectID: uWriter,
				TokenHash: control.HashToken(tok), Label: label, NarrowedScopes: []control.ID{sAlpha}},
			{Kind: control.EventCredentialVerbsNarrowed, At: t0, CredentialID: id, Verbs: rw},
		}
	}
	events := []control.Event{
		{Kind: control.EventUserCreated, At: t0, UserID: uWriter, Provider: "p", Subject: "c1", DisplayName: "writer-a"},
		{Kind: control.EventProjectCreated, At: t0, ProjectID: pNotes, Name: "notes", UserID: uWriter},
		{Kind: control.EventMemberSet, At: t0, ProjectID: pNotes, UserID: uWriter, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: t0, ScopeID: sAlpha, DisplayName: "alpha-notes", ProjectID: pNotes},
	}
	events = append(events, cred("crd_capi_a", tokA, "agents@host-a")...)
	events = append(events, cred("crd_capi_b", tokB, "agents@host-b")...)
	if _, err := fs.Append(context.Background(), events...); err != nil {
		t.Fatal(err)
	}
	cache := control.NewCache(fs, control.CacheOptions{})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	machine, err := identity.NewMachineToken(cache)
	if err != nil {
		t.Fatal(err)
	}
	if limiter == nil {
		limiter = netid.NewRateLimiter(1000, time.Minute, time.Minute)
	}
	st := &stubStore{}
	h, err := clientapi.New(clientapi.Config{Auth: machine, Model: cache.Model, Memos: st, Limiter: limiter})
	if err != nil {
		t.Fatal(err)
	}
	return &rig{journal: fs, cache: cache, store: st, h: h}
}

type answer struct {
	status int
	body   string
	ctype  string
}

// do serves one request from `peer` with the given headers.
func (r *rig) do(method, target, body, peer string, headers map[string]string) answer {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = peer + ":40000"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	return answer{rec.Code, rec.Body.String(), rec.Header().Get("Content-Type")}
}

func bearer(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

const peerA, peerB = "192.0.2.10", "192.0.2.20"

// TestTheClientRouteLedgerMatchesTheDispatchTable — `ClientRoutes()` against a HAND-WRITTEN list,
// failing on GROW or SHRINK: adding a row is adding an endpoint an agent credential reaches. Every
// row also DISPATCHES (the store is reached), a ledger path under another method is 405, and a path
// off the ledger is 404 — so the ledger cannot list a row the dispatcher does not serve.
func TestTheClientRouteLedgerMatchesTheDispatchTable(t *testing.T) {
	want := []string{
		"GET /client/v1/memo",
		"GET /client/v1/memos",
		"POST /client/v1/memo/retract",
		"POST /client/v1/memos",
	}
	if got := clientapi.ClientRoutes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ClientRoutes() = %q\nwant          %q\nA row added or removed here is an endpoint an agent credential reaches; "+
			"change this list in the same commit and say why.", got, want)
	}
	r := newRig(t, nil)
	r.store.stored = []memo.Stored{memo.NewStored(memo.Fields{ID: 1, Scope: "alpha-notes", SenderKind: "user",
		SenderID: string(uWriter), SenderDisplay: "writer-a", Subject: "s", Body: "b", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)})}
	bodies := map[string]string{
		"POST /client/v1/memos":        `{"scope":"alpha-notes","subject":"s","body":"b"}`,
		"POST /client/v1/memo/retract": `{"scope":"alpha-notes","id":1}`,
	}
	for _, row := range want {
		method, path, _ := strings.Cut(row, " ")
		before := r.store.n()
		got := r.do(method, path+"?scope=alpha-notes&id=1", bodies[row], peerA, bearer(tokA))
		if got.status >= 300 || r.store.n() != before+1 {
			t.Errorf("%s: answered %d %q and reached the store %d time(s) — a ledger row the dispatcher does not serve",
				row, got.status, got.body, r.store.n()-before)
		}
	}
	if got := r.do(http.MethodDelete, clientapi.MemosPath, "", peerA, bearer(tokA)); got.status != http.StatusMethodNotAllowed {
		t.Errorf("DELETE on a ledger path answered %d, want 405", got.status)
	}
	if got := r.do(http.MethodGet, "/client/v1/memos/", "", peerA, bearer(tokA)); got.status != http.StatusNotFound {
		t.Errorf("a path off the ledger answered %d, want 404", got.status)
	}
}

// TestOnlyAMachineTokenAuthenticatesAndEveryRefusalIsOneBody — decision 3 and T12: no credential, a
// garbage bearer, a cookie, a provider-JWT-shaped bearer and a Basic header are each 401 with
// `UnauthorizedBody`, byte for byte (status, content type and body), and none reaches the store.
// POSITIVE CONTROL: the issued machine token answers 200.
//
// ⚠ AN INVARIANT GUARD AT THIS LEVEL, LABELLED AS ONE. `Config.Auth` is a `*identity.MachineToken`,
// so no cookie or JWT backend can be handed to this handler and these refusals cannot go red here
// without changing the type. What CAN go red — `cmd/cairn-ui` wiring the browser's chain into the
// listener — is `TestACookieSignedInNextDoorIsGarbageToTheClientListener` in the Postgres tier,
// which signs a real browser session in and presents its cookie.
func TestOnlyAMachineTokenAuthenticatesAndEveryRefusalIsOneBody(t *testing.T) {
	r := newRig(t, nil)
	target := clientapi.MemosPath + "?scope=alpha-notes"
	if got := r.do(http.MethodGet, target, "", peerA, bearer(tokA)); got.status != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL: the issued machine token answered %d %q", got.status, got.body)
	}
	reached := r.store.n()
	jwtShaped := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIwMDAwMDAwMC0wMDAwLTQwMDAtODAwMC0wMDAwMDAwMDAwOTEifQ.c2lnbmF0dXJl"
	for name, h := range map[string]map[string]string{
		"no credential":        nil,
		"a garbage bearer":     bearer(garbage),
		"a session cookie":     {"Cookie": identity.SessionCookieName + "=clientapi-test-session-id-000000000000000000"},
		"a JWT-shaped bearer":  bearer(jwtShaped),
		"a Basic header":       {"Authorization": "Basic d3JpdGVyLWE6cGFzc3dvcmQ="},
		"an empty bearer":      {"Authorization": "Bearer "},
		"the token, no scheme": {"Authorization": tokA + "x"},
	} {
		got := r.do(http.MethodGet, target, "", peerA, h)
		want := answer{http.StatusUnauthorized, clientapi.UnauthorizedBody, "application/json"}
		if got != want {
			t.Errorf("%s: %+v, want %+v", name, got, want)
		}
	}
	if r.store.n() != reached {
		t.Fatalf("a refused request reached the store %d time(s)", r.store.n()-reached)
	}
}

// TestFailedCredentialsLockTheClientOut — the `netid` lockout on this listener's own limiter: after
// N failures from one client, even the VALID token from that client is refused (the same 401 body),
// while another client's valid token still answers 200. RED with the `LockedOut` check removed, and
// RED with the failure not recorded.
func TestFailedCredentialsLockTheClientOut(t *testing.T) {
	const n = 3
	r := newRig(t, netid.NewRateLimiter(n, time.Minute, time.Hour))
	target := clientapi.MemosPath + "?scope=alpha-notes"
	if got := r.do(http.MethodGet, target, "", peerA, bearer(tokA)); got.status != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL: before any failure the valid token answered %d", got.status)
	}
	for i := 0; i < n; i++ {
		r.do(http.MethodGet, target, "", peerA, bearer(garbage))
	}
	got := r.do(http.MethodGet, target, "", peerA, bearer(tokA))
	if got.status != http.StatusUnauthorized || got.body != clientapi.UnauthorizedBody {
		t.Fatalf("after %d failures the locked-out client's VALID token answered %d %q, want the 401", n, got.status, got.body)
	}
	if got := r.do(http.MethodGet, target, "", peerB, bearer(tokA)); got.status != http.StatusOK {
		t.Fatalf("another client's valid token answered %d — the lockout is not per client", got.status)
	}
}

// TestARevokedCredentialIsRefusedAfterOneForcedRefresh — the S2 test plan's forced revocation, and
// O9's "a revoked one → 401, byte-identical to a random token's; the other keeps working": revoke
// agents@host-a in the journal, call `control.Cache.Refresh` directly — no waiting for the refresh
// interval — and the next request with it is the garbage token's 401, byte for byte, while
// agents@host-b still answers 200. RED with the handler remembering a credential it has already
// authenticated. ⚠ BEFORE the refresh the revoked token still answers 200: revocation latency is one
// refresh (decision 16), asserted here so nobody reads the forced refresh as the production timing.
func TestARevokedCredentialIsRefusedAfterOneForcedRefresh(t *testing.T) {
	r := newRig(t, nil)
	target := clientapi.MemosPath + "?scope=alpha-notes"
	if got := r.do(http.MethodGet, target, "", peerA, bearer(tokA)); got.status != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL: agents@host-a answered %d before revocation", got.status)
	}
	if _, err := control.RevokeCredential(context.Background(), r.journal, "crd_capi_a", uWriter, t0); err != nil {
		t.Fatal(err)
	}
	if got := r.do(http.MethodGet, target, "", peerA, bearer(tokA)); got.status != http.StatusOK {
		t.Fatalf("before the refresh the revoked token answered %d — the test's premise (one refresh of latency) moved", got.status)
	}
	if err := r.cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	revoked := r.do(http.MethodGet, target, "", peerA, bearer(tokA))
	random := r.do(http.MethodGet, target, "", peerA, bearer(garbage))
	if revoked.status != http.StatusUnauthorized || revoked != random {
		t.Fatalf("after one refresh the revoked credential answered %+v; a random token answers %+v — want the same 401", revoked, random)
	}
	if got := r.do(http.MethodGet, target, "", peerB, bearer(tokB)); got.status != http.StatusOK {
		t.Fatalf("the OTHER credential of the same principal answered %d after its sibling was revoked", got.status)
	}
}

// TestTheWireCarriesRenderedTextNeverStored — decision 19 / Guard C at the listener: a memo whose
// stored subject, body and credential label carry U+200B, a variation selector (U+E0100) and U+FE0F
// is served by the list and the one-memo routes, and the RAW response bytes carry none of those code
// points — neither as UTF-8 nor as a JSON `\u` escape — and DO carry U+FFFD; decoded, the sender
// line still names `via <label>`. RED with the listener encoding `memo.Stored` (no exported field, so
// `{}`: the rendered fields this test requires are absent). The same property against a row planted
// by SQL in a real database is `TestTheListenerServesRenderedTextForARowPlantedBySQL`.
func TestTheWireCarriesRenderedTextNeverStored(t *testing.T) {
	r := newRig(t, nil)
	// Each planted code point, with BOTH spellings a JSON encoder could give it: raw UTF-8, and the
	// `\u` escape (a surrogate pair for the astral one) — so an encoder that escapes them is caught
	// as well as one that writes them raw.
	hostile := map[string]string{"\u200b": `\u200b`, "\U000E0100": `\udb40\udd00`, "\ufe0f": `\ufe0f`}
	r.store.stored = []memo.Stored{memo.NewStored(memo.Fields{ID: 41, Scope: "alpha-notes", SenderKind: "user",
		SenderID: string(uWriter), SenderDisplay: "writer-a", CredentialLabel: "agents@host-a\u200b",
		Subject: "deploy\u200bnotes", Body: "use the\U000E0100 rename\ufe0f next", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)})}
	for _, target := range []string{clientapi.MemosPath + "?scope=alpha-notes", clientapi.MemoPath + "?scope=alpha-notes&id=41"} {
		got := r.do(http.MethodGet, target, "", peerA, bearer(tokA))
		if got.status != http.StatusOK {
			t.Fatalf("%s answered %d %q", target, got.status, got.body)
		}
		raw := []byte(got.body)
		for cp, esc := range hostile {
			if bytes.Contains(raw, []byte(cp)) || bytes.Contains(bytes.ToLower(raw), []byte(esc)) {
				t.Errorf("%s: the raw response carries U+%04X: %s", target, []rune(cp)[0], raw)
			}
		}
		if !bytes.Contains(raw, []byte("\ufffd")) && !bytes.Contains(bytes.ToLower(raw), []byte(`\ufffd`)) {
			t.Errorf("%s: the raw response carries no U+FFFD — nothing was rendered: %s", target, raw)
		}
		var one memo.Rendered
		if strings.HasPrefix(target, clientapi.MemosPath) {
			var lb clientapi.ListBody
			if err := json.Unmarshal(raw, &lb); err != nil || len(lb.Memos) != 1 {
				t.Fatalf("%s: decoding %s: %v", target, raw, err)
			}
			one = lb.Memos[0]
		} else {
			var mb clientapi.MemoBody
			if err := json.Unmarshal(raw, &mb); err != nil {
				t.Fatalf("%s: decoding %s: %v", target, raw, err)
			}
			one = mb.Memo
		}
		if one.ID != 41 || one.Subject != "deploy\ufffdnotes" || !strings.Contains(one.Sender, " via agents@host-a\ufffd ") {
			t.Errorf("%s: the rendered fields are missing or wrong: %+v", target, one)
		}
	}
}

// TestMalformedRequestsAreRefusedBeforeTheStore — a request whose SHAPE is wrong is a 400 that never
// reaches the store, and its detail echoes nothing the caller sent.
func TestMalformedRequestsAreRefusedBeforeTheStore(t *testing.T) {
	r := newRig(t, nil)
	for _, tc := range []struct {
		name, method, target, body string
	}{
		{"list without a scope", http.MethodGet, clientapi.MemosPath, ""},
		{"list with limit 0", http.MethodGet, clientapi.MemosPath + "?scope=alpha-notes&limit=0", ""},
		{"list with a limit past the cap", http.MethodGet, clientapi.MemosPath + "?scope=alpha-notes&limit=201", ""},
		{"list with a 21-digit limit", http.MethodGet, clientapi.MemosPath + "?scope=alpha-notes&limit=100000000000000000000", ""},
		{"list with an unparseable after", http.MethodGet, clientapi.MemosPath + "?scope=alpha-notes&after=yesterday", ""},
		{"list with tombstones=2", http.MethodGet, clientapi.MemosPath + "?scope=alpha-notes&tombstones=2", ""},
		{"get without an id", http.MethodGet, clientapi.MemoPath + "?scope=alpha-notes", ""},
		{"get with id 0", http.MethodGet, clientapi.MemoPath + "?scope=alpha-notes&id=0", ""},
		{"get with a non-numeric id", http.MethodGet, clientapi.MemoPath + "?scope=alpha-notes&id=seventeen", ""},
		{"get without a scope", http.MethodGet, clientapi.MemoPath + "?id=1", ""},
		{"send with an unknown field", http.MethodPost, clientapi.MemosPath, `{"scope":"alpha-notes","subject":"s","body":"b","from":"someone-else"}`},
		{"send with no scope", http.MethodPost, clientapi.MemosPath, `{"subject":"s","body":"b"}`},
		{"send with two objects", http.MethodPost, clientapi.MemosPath, `{"scope":"alpha-notes"}{"scope":"alpha-notes"}`},
		// 🔴 A LITERAL, NOT `clientapi.MaxRequestBody`: a size derived from the constant moves WITH
		// it, so raising the bound to a megabyte kept this row green. 65536 is the bound as
		// documented; `TestTheBodyBoundIsALiteral64KiBWithAPositiveControl` pins the other edge.
		{"send past the body bound", http.MethodPost, clientapi.MemosPath, `{"scope":"alpha-notes","body":"` + strings.Repeat("x", 65536) + `"}`},
		// `{…}}`: one object, then a stray closing brace. `dec.More()` reports false before a `}`,
		// so the "ONE JSON object" check let this through until `decode` required end of input.
		{"send with a trailing brace", http.MethodPost, clientapi.MemosPath, `{"scope":"alpha-notes","subject":"s","body":"b"}}`},
		{"send with trailing garbage", http.MethodPost, clientapi.MemosPath, `{"scope":"alpha-notes","subject":"s","body":"b"} x`},
		{"retract without an id", http.MethodPost, clientapi.RetractPath, `{"scope":"alpha-notes"}`},
		{"retract with a string id", http.MethodPost, clientapi.RetractPath, `{"scope":"alpha-notes","id":"1"}`},
	} {
		got := r.do(tc.method, tc.target, tc.body, peerA, bearer(tokA))
		if got.status != http.StatusBadRequest {
			t.Errorf("%s: answered %d %q, want 400", tc.name, got.status, got.body)
		}
		if strings.Contains(got.body, "someone-else") || strings.Contains(got.body, "seventeen") {
			t.Errorf("%s: the refusal echoes the caller's input: %q", tc.name, got.body)
		}
	}
	if r.store.n() != 0 {
		t.Fatalf("malformed requests reached the store %d time(s)", r.store.n())
	}
}

// TestTheListRouteHandsTheStoreEveryParameter — the list route's parameters, asserted on the QUERY
// the store receives rather than on the answer, which a stub store cannot vary. Every value is
// distinct from its zero AND from the default the store would substitute (`limit` 17, not the cap
// 200; `after` a sub-second instant, not the zero time), so a handler that drops a parameter, or
// hardcodes its default, cannot satisfy the comparison by coincidence. RED under each of: `after`
// parsed and not assigned; `limit` parsed and not assigned; `IncludeRetracted` defaulting to true;
// `tombstones=1` ignored. The same four against the REAL store are
// `TestTheListRouteParametersReachTheRealStore` (Postgres tier).
func TestTheListRouteHandsTheStoreEveryParameter(t *testing.T) {
	r := newRig(t, nil)
	after := time.Date(2000, 2, 3, 4, 5, 6, 500_000_000, time.UTC)
	for _, tc := range []struct {
		target string
		want   pgstore.MemoQuery
	}{
		{clientapi.MemosPath + "?scope=alpha-notes", pgstore.MemoQuery{Scope: "alpha-notes"}},
		{clientapi.MemosPath + "?scope=alpha-notes&tombstones=0", pgstore.MemoQuery{Scope: "alpha-notes"}},
		{clientapi.MemosPath + "?scope=alpha-notes&after=2000-02-03T04:05:06.5Z&limit=17&tombstones=1",
			pgstore.MemoQuery{Scope: "alpha-notes", After: after, Limit: 17, IncludeRetracted: true}},
		{clientapi.MemosPath + "?scope=alpha-notes&limit=1", pgstore.MemoQuery{Scope: "alpha-notes", Limit: 1}},
		{clientapi.MemosPath + "?scope=alpha-notes&after=2000-02-03T04:05:06.5Z", pgstore.MemoQuery{Scope: "alpha-notes", After: after}},
	} {
		r.store.lastQuery = nil
		got := r.do(http.MethodGet, tc.target, "", peerA, bearer(tokA))
		if got.status != http.StatusOK {
			t.Fatalf("%s: answered %d %q", tc.target, got.status, got.body)
		}
		if r.store.lastQuery == nil {
			t.Fatalf("%s: the store was never asked", tc.target)
		}
		q := *r.store.lastQuery
		if q.Scope != tc.want.Scope || !q.After.Equal(tc.want.After) || q.Limit != tc.want.Limit ||
			q.IncludeRetracted != tc.want.IncludeRetracted {
			t.Errorf("%s: the store was asked %+v, want %+v", tc.target, q, tc.want)
		}
	}
}

// TestTheSendTTLIsClampedWithoutOverflow — `ttlFrom`'s clamp, measured through the route. A
// `ttl_seconds` past `memo.MaxTTL` reaches the store as one second past it (which `memo.CheckSend`
// refuses as `ttl`), a hugely negative one as -1 s, and nothing in between overflows. RED with the
// clamp removed: `time.Duration(math.MaxInt64) * time.Second` wraps, and the store is handed a value
// that is neither the caller's nor a refusal.
func TestTheSendTTLIsClampedWithoutOverflow(t *testing.T) {
	r := newRig(t, nil)
	if memo.MaxTTL != 30*24*time.Hour {
		t.Fatalf("memo.MaxTTL moved to %v: the literal 2592000 rows below no longer sit on its edge", memo.MaxTTL)
	}
	for _, tc := range []struct {
		seconds string
		want    time.Duration
	}{
		{"0", 0},
		{"3600", time.Hour},
		{"2592000", memo.MaxTTL},
		{"2592001", memo.MaxTTL + time.Second},
		{"9223372036854775807", memo.MaxTTL + time.Second},
		{"-9223372036854775807", -time.Second},
		{"-60", -time.Minute},
	} {
		r.store.lastTTL = nil
		got := r.do(http.MethodPost, clientapi.MemosPath,
			`{"scope":"alpha-notes","subject":"s","body":"b","ttl_seconds":`+tc.seconds+`}`, peerA, bearer(tokA))
		if got.status != http.StatusCreated || r.store.lastTTL == nil {
			t.Fatalf("ttl_seconds=%s: answered %d %q", tc.seconds, got.status, got.body)
		}
		if *r.store.lastTTL != tc.want {
			t.Errorf("ttl_seconds=%s reached the store as %v, want %v", tc.seconds, *r.store.lastTTL, tc.want)
		}
	}
}

// TestTheBodyBoundIsALiteral64KiBWithAPositiveControl — the other edge of the bound the malformed
// table pins from above: a send body just UNDER 64 KiB is decoded and reaches the store, so the
// refusal there is the bound and not a decoder that refuses everything large.
func TestTheBodyBoundIsALiteral64KiBWithAPositiveControl(t *testing.T) {
	r := newRig(t, nil)
	body := `{"scope":"alpha-notes","subject":"s","body":"` + strings.Repeat("x", 65536-64) + `"}`
	if len(body) >= 65536 {
		t.Fatalf("the positive control is %d bytes, not under the bound", len(body))
	}
	if got := r.do(http.MethodPost, clientapi.MemosPath, body, peerA, bearer(tokA)); got.status != http.StatusCreated {
		t.Fatalf("a %d-byte send answered %d %q, want it decoded and handed to the store", len(body), got.status, got.body)
	}
}
