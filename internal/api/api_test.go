package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/write"
)

// ---------------------------------------------------------------------------
// The route ledger, which is the blind spot the conformance suite cannot close
// for a non-Python implementation.
// ---------------------------------------------------------------------------

// repoRoot walks up to the directory holding `go.mod`.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}

// corpusRoutes is every `<METHOD> <head>` the conformance request list ADDRESSES,
// derived from the declared target exactly as `cases.addressed_routes` derives it.
//
// 🔴 THIS FUNCTION IS THE GO SIDE OF A LEDGER THE SUITE CANNOT BUILD ITSELF.
// `tests/conformance/cases.declared_routes` reads the ORACLE's two dispatch tables by
// AST and fails when the declared set grows or shrinks against what the corpus
// addresses — and `tests/conformance/README.md` states plainly that this closure is
// "not closed for a non-Python implementation … so P1 has to add the equivalent ledger
// on the Go side". This is it, and it reads the SAME data file rather than a copy of
// it: a suite that replays a recorded list is structurally blind to a route added after
// the list was written, in either language.
func corpusRoutes(t *testing.T) []string {
	t.Helper()
	path := filepath.Join(repoRoot(t), "tests", "conformance", "requests.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the request list is the ledger's other half: %v", err)
	}
	var corpus struct {
		Cases []struct {
			ID            string `json:"id"`
			Method        string `json:"method"`
			Target        string `json:"target"`
			NegativeRoute bool   `json:"negative_route"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) == 0 {
		// 🔴 THE POSITIVE CONTROL ON THE READER ITSELF. A ledger that discovers NOTHING
		// reports full coverage over an empty set, which is exactly the reassuring zero
		// this guard exists to avoid.
		t.Fatal("the request list parsed to zero cases: this ledger would then agree " +
			"with anything")
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range corpus.Cases {
		// A `negative_route` row exists to pin what a NON-route answers, so it
		// contributes no coverage — the same exclusion the Python side makes, and for
		// the same reason.
		if c.NegativeRoute || c.Method == "" || c.Target == "" {
			continue
		}
		path, _, _ := strings.Cut(c.Target, "?")
		if !strings.HasPrefix(path, APIPrefix) {
			continue
		}
		parts := pathComponents(path)
		if len(parts) == 0 {
			continue
		}
		name := c.Method + " " + parts[0]
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func TestTheRouteLedgerMatchesTheConformanceCorpus(t *testing.T) {
	declared := DeclaredRoutes()
	addressed := corpusRoutes(t)
	if len(declared) == 0 {
		t.Fatal("this server declares no route at all")
	}
	// 🔴 IT FAILS WHEN THE SET GROWS **AND** WHEN IT SHRINKS. A route the corpus never
	// addresses is a public, internet-reachable endpoint nothing replays; a corpus row
	// for a route this server no longer serves records a 404 as if it were the contract.
	for _, name := range declared {
		if !slices.Contains(addressed, name) {
			t.Fatalf("this server declares %q and the conformance request list never "+
				"addresses it. A suite that replays a recorded list is STRUCTURALLY "+
				"BLIND to a route added after the list was written — add a row per "+
				"principal shape, then regenerate.", name)
		}
	}
	for _, name := range addressed {
		if !slices.Contains(declared, name) {
			t.Fatalf("the conformance request list addresses %q and this server does "+
				"not dispatch it. The ledger fails when the set SHRINKS as well as "+
				"when it grows.", name)
		}
	}
}

func TestEveryDeclaredRouteIsActuallyDISPATCHED(t *testing.T) {
	// 🔴 THE LEDGER AND THE WIRING, PINNED BEHAVIOURALLY RATHER THAN BY THE
	// CONSTRUCTION-TIME CHECK. `New` calls `checkLedger`, but nothing outside this
	// package can hand `New` a wrong wiring — so a mutation that DELETES that call
	// SURVIVED every test here, measured. What cannot be walked is the server's own
	// answer: a declared route that nothing dispatches answers the no-route 404, and a
	// dispatched route the ledger does not name shows up in `DeclaredRoutes` for the
	// corpus ledger to reject.
	h := newHarness(t)
	// One request per declared route, at its own arity, with a credential that may see
	// the scope. None of them may be the no-route answer.
	targets := map[string]string{
		"recall":   "/api/v1/recall/alpha-notes",
		"search":   "/api/v1/search/alpha-notes?q=x",
		"snapshot": "/api/v1/snapshot",
		"entry":    "/api/v1/entry/alpha-notes/gadget-one",
	}
	for _, route := range DeclaredRoutes() {
		method, head, _ := strings.Cut(route, " ")
		target, known := targets[head]
		if !known {
			t.Fatalf("this test has no request shape for the declared route %q — a route "+
				"was added and the behavioural half of the ledger was not", route)
		}
		if method == "POST" {
			target += "/bullets"
		}
		got := h.do(t, method, target, wideToken, nil, "{}")
		if got.status == 404 && got.body == "no such endpoint\n" {
			t.Fatalf("%s is DECLARED and answers the no-route 404: the ledger names a "+
				"route nothing dispatches", route)
		}
		if got.status == 405 {
			t.Fatalf("%s is DECLARED and answers the wrong-method 405", route)
		}
	}
	// The CONTROL on that loop: a head the ledger does NOT name must answer exactly the
	// no-route 404 the assertion above rejects, or the loop would pass for any answer.
	control := h.do(t, "GET", "/api/v1/raw_dump", wideToken, nil, "")
	if control.status != 404 || control.body != "no such endpoint\n" {
		t.Fatalf("the control failed: an undeclared head must answer the no-route 404, "+
			"got %d %q", control.status, control.body)
	}
}

func TestTheLedgerGuardCanGoRed(t *testing.T) {
	// 🔴 THE NEGATIVE CONTROL ON THE GUARD ABOVE. `checkLedger` is the mechanism that
	// keeps the wiring and the declared set in step; feeding it a wiring the ledger does
	// not name must be an error, or the agreement test is comparing a set with itself.
	srv := &Server{
		readRoutes:  map[string]readRoute{"recall": {}, "search": {}, "snapshot": {}, "raw_dump": {}},
		writeRoutes: map[writeKey]writeRoute{{"POST", "entry"}: {}, {"PUT", "entry"}: {}},
	}
	err := srv.checkLedger()
	if err == nil {
		t.Fatal("a route that is dispatched but not declared must refuse to start")
	}
	if !strings.Contains(err.Error(), "raw_dump") {
		t.Fatalf("the refusal must name the route: %v", err)
	}
	// …and the other direction: a ledger entry nothing dispatches.
	srv = &Server{
		readRoutes:  map[string]readRoute{"recall": {}, "search": {}},
		writeRoutes: map[writeKey]writeRoute{{"POST", "entry"}: {}, {"PUT", "entry"}: {}},
	}
	if err := srv.checkLedger(); err == nil {
		t.Fatal("a declared route nothing dispatches must refuse to start")
	}
	// The POSITIVE control: the real wiring passes, so a red above is the mutation and
	// not the harness.
	if _, err := newTestServer(t); err != nil {
		t.Fatalf("the real wiring must pass its own ledger check: %v", err)
	}

	// ⚠ EQUIVALENT MUTANT, MEASURED AND RECORDED: deleting the `checkLedger()` CALL in
	// `New` survives this file. Nothing outside this package can hand `New` a wrong
	// wiring, so the call is a construction-time belt over a property
	// `TestEveryDeclaredRouteIsActuallyDISPATCHED` already pins from the outside — it
	// asks the running server whether every declared route answers something other than
	// the no-route 404. The call stays because it turns a wiring mistake into a refusal
	// to START rather than into a 404 in production, which is the better failure; but it
	// is not what this file's green rests on, and saying so is cheaper than a future
	// reader re-deriving it from a surviving mutant.
}

// ---------------------------------------------------------------------------
// An end-to-end harness over a real store.
// ---------------------------------------------------------------------------

const wideToken = "wide-reader-token-wide-reader-token-wide-rd"
const narrowToken = "narrow-reader-token-narrow-reader-token-nar"
const legacyToken = "legacy-token-legacy-token-legacy-token-lega"

type harness struct {
	srv   *Server
	root  string
	tsrv  *httptest.Server
	clock time.Time
}

func newTestServer(t *testing.T) (*Server, error) {
	t.Helper()
	root := buildFixtureStore(t)
	tokens := []authz.TokenRecord{
		{Token: wideToken, Identity: "wide-reader", Scopes: []string{"alpha-notes", "beta-notes"}},
		{Token: narrowToken, Identity: "narrow-reader", Scopes: []string{"beta-notes"}},
		authz.LegacyRecord(legacyToken),
	}
	return New(root, tokens, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")},
		netid.NewRateLimiter(1000000, time.Minute, 15*time.Minute))
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	srv, err := newTestServer(t)
	if err != nil {
		t.Fatal(err)
	}
	srv.Audit = func(string) {}
	clock := time.Date(2000, 1, 5, 0, 0, 0, 0, time.UTC)
	srv.Now = func() time.Time { return clock }
	h := &harness{srv: srv, root: srv.StoreRoot, clock: clock}
	h.tsrv = httptest.NewServer(srv)
	t.Cleanup(h.tsrv.Close)
	return h
}

func buildFixtureStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put := func(scope, name, body string) {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entry := func(service, scope string) string {
		return "---\nservice: " + service + "\nscope: " + scope + "\n---\n\n" +
			"## What it is\nsynthetic.\n\n## Nuance / work-history\n- 2000-01-02: a note.\n"
	}
	put("alpha-notes", "gadget-one.md", entry("gadget-one", "alpha-notes"))
	put("beta-notes", "widget-three.md", entry("widget-three", "beta-notes"))
	if err := os.WriteFile(filepath.Join(root, ".seed-stamp"),
		[]byte("2000-01-04T00:00:00Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

type reply struct {
	status  int
	headers http.Header
	body    string
}

// do issues one request with the conformance configuration's client-IP header, which
// is what makes the loopback peer a trusted proxy.
func (h *harness) do(t *testing.T, method, target, token string, headers map[string]string, body string) reply {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, h.tsrv.URL+target, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(netid.ClientIPHeader, "203.0.113.7")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return reply{status: resp.StatusCode, headers: resp.Header, body: string(payload)}
}

// raw issues a request over a bare socket and returns the whole response as it
// arrived, head and body.
//
// 🔴 IT EXISTS BECAUSE Go's HTTP CLIENT DOES NOT HAND BACK EVERYTHING THE SERVER SENT.
// `Connection` is a hop-by-hop field and the transport consumes it, so `resp.Header`
// reports it ABSENT on a response that carried it — MEASURED: a test asserting on
// `resp.Header.Get("Connection")` failed for every 401 while the conformance suite,
// which reads the wire, records `Connection: close` on all of them. Reading the parsed
// view would have been a test of the CLIENT, not of the server — the same
// "parsing a tool's output makes its format a dependency you did not pin" hazard the
// rest of this repository is built against. So the assertion reads the bytes.
func (h *harness) raw(t *testing.T, requestLines ...string) string {
	t.Helper()
	address := strings.TrimPrefix(h.tsrv.URL, "http://")
	conn, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte(strings.Join(requestLines, "\r\n"))); err != nil {
		t.Fatal(err)
	}
	// 🔴 READ THE HEAD, THEN EXACTLY `Content-Length` BYTES — NEVER TO EOF. A 200 and
	// the health probe deliberately KEEP their connection, so reading to EOF blocks
	// until a deadline and the test times out instead of failing on the thing it
	// measures. MEASURED: `io.ReadAll` here turned the keep-alive rows of the
	// close-on-refusal table into a ten-second timeout with no verdict at all, which
	// reads as a broken harness rather than as the assertion it was.
	reader := bufio.NewReader(conn)
	var head strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("the response head ended early: %v (so far: %q)", err, head.String())
		}
		head.WriteString(line)
		if line == "\r\n" {
			break
		}
	}
	length := 0
	for _, line := range strings.Split(head.String(), "\r\n") {
		name, value, found := strings.Cut(line, ":")
		if found && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				t.Fatalf("unreadable Content-Length %q", value)
			}
			length = parsed
		}
	}
	body := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(reader, body); err != nil {
			t.Fatalf("the body was shorter than its declared length: %v", err)
		}
	}
	return head.String() + string(body)
}

// rawRequest is the common shape: one request, one credential, read to EOF. Extra
// lines are appended verbatim, so a caller can add a precondition header and a body.
func (h *harness) rawRequest(t *testing.T, method, target, token string, extra ...string) string {
	t.Helper()
	lines := []string{
		method + " " + target + " HTTP/1.1",
		"Host: cairn.invalid",
		netid.ClientIPHeader + ": 203.0.113.7",
	}
	if token != "" {
		lines = append(lines, "Authorization: Bearer "+token)
	}
	lines = append(lines, extra...)
	lines = append(lines, "", "")
	return h.raw(t, lines...)
}

func TestHealthIsAnsweredBeforeEverything(t *testing.T) {
	h := newHarness(t)

	got := h.do(t, "GET", HealthPath, "", nil, "")
	if got.status != 200 || got.body != "ok\n" {
		t.Fatalf("got %d %q", got.status, got.body)
	}
	// 🔴 IT SAYS NOTHING BUT `ok`: no version, no scope count, no store revision. It is
	// unauthenticated, so anything it reveals is public.
	for _, header := range []string{"X-Store-Status", "X-Store-Snapshot", "X-Store-Revision", "X-Store-Entries"} {
		if got.headers.Get(header) != "" {
			t.Fatalf("health must reveal nothing, and it set %s", header)
		}
	}
	if got.headers.Get("Server") != "subsystem-store" {
		t.Fatalf("the banner must carry no version, got %q", got.headers.Get("Server"))
	}

	// 🔴 IT MUST NOT REQUIRE A CLIENT-IP HEADER THE KUBELET HAS NO REASON TO SEND. A
	// readiness probe broken by a security guard is how the guard gets deleted.
	req, _ := http.NewRequest("GET", h.tsrv.URL+HealthPath, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health with no client IP: got %d", resp.StatusCode)
	}

	// A valid credential buys NOTHING extra here.
	authed := h.do(t, "GET", HealthPath, wideToken, nil, "")
	if authed.body != got.body || authed.headers.Get("Content-Length") != got.headers.Get("Content-Length") {
		t.Fatal("an authenticated probe must get the same answer")
	}

	// …and a WRITE verb at the same path is a wrong method, not a health check.
	post := h.do(t, "POST", HealthPath, wideToken, nil, "")
	if post.status != 405 || post.body != "read-only\n" {
		t.Fatalf("got %d %q", post.status, post.body)
	}
}

func TestTheUniform401(t *testing.T) {
	h := newHarness(t)
	// 🔴 EVERY REJECTION IS BYTE-IDENTICAL, because an error that discriminates is an
	// enumeration API. Gathered as a SET and compared to each other — a per-answer
	// assertion would keep passing if one of them grew a distinguishing header.
	shapes := map[string]reply{
		"no token":             h.do(t, "GET", "/api/v1/recall/alpha-notes", "", nil, ""),
		"a wrong token":        h.do(t, "GET", "/api/v1/recall/alpha-notes", "wrong-cred", nil, ""),
		"a non-API path":       h.do(t, "GET", "/", "", nil, ""),
		"the prefix, no slash": h.do(t, "GET", "/api/v1", wideToken, nil, ""),
		"an operator's guess":  h.do(t, "GET", "/metrics", wideToken, nil, ""),
		"an unhandled verb":    h.do(t, "OPTIONS", "/", "", nil, ""),
		"an invented verb":     h.do(t, "FROBNICATE", "/", "", nil, ""),
		"a write with no credential": h.do(t, "POST",
			"/api/v1/entry/alpha-notes/gadget-one/bullets", "", nil, `{"text":"x","session":"s"}`),
		"an unauthenticated snapshot": h.do(t, "GET", "/api/v1/snapshot", "", nil, ""),
	}
	var reference string
	for name, got := range shapes {
		if got.status != 401 {
			t.Fatalf("%s: got %d, want 401", name, got.status)
		}
		if got.body != "unauthorized\n" {
			t.Fatalf("%s: body %q", name, got.body)
		}
		// The header NAME spelling is part of what a port must reproduce: the corpus
		// records the names a response actually carried.
		if values := got.headers.Values("WWW-Authenticate"); len(values) != 1 ||
			values[0] != `Bearer realm="subsystem-store"` {
			t.Fatalf("%s: WWW-Authenticate %v", name, values)
		}
		fingerprint := fmt.Sprintf("%d|%s|%s", got.status, got.body, headerFingerprint(got.headers))
		if reference == "" {
			reference = fingerprint
			continue
		}
		if fingerprint != reference {
			t.Fatalf("%s is DISTINGUISHABLE from the uniform 401:\n got: %s\nref: %s",
				name, fingerprint, reference)
		}
	}
	// 🔴 THE HEADER NAME SPELLINGS ARE READ OFF THE WIRE, NOT OUT OF THE PARSED
	// RESPONSE, AND A MUTATION SWEEP IS WHY. Go's `Header.Set` rewrites a name to
	// `Title-Case-Per-Hyphen`, which turns `WWW-Authenticate` into `Www-Authenticate`
	// and `ETag` into `Etag`; the conformance corpus records the names a response
	// actually carried, so those are a real difference. But `Header.Values` looks up
	// through the SAME canonicaliser, so the assertion above cannot see it —
	// MEASURED: replacing this package's `setHeader` with `h.Set` SURVIVED every
	// assertion in this file until the check below read the bytes.
	wire := h.rawRequest(t, "GET", "/api/v1/recall/alpha-notes", "")
	if !strings.Contains(wire, "WWW-Authenticate: ") {
		t.Fatalf("the wire does not carry `WWW-Authenticate:` in that spelling:\n%s", wire)
	}
	if strings.Contains(wire, "Www-Authenticate") {
		t.Fatalf("the canonicalised spelling reached the wire:\n%s", wire)
	}

	// 🔴 A DUPLICATED CLIENT-IP HEADER IS REFUSED RATHER THAN GUESSED AT, because which
	// one is the client is unanswerable and picking one is how a caller smuggles a value
	// past a proxy that appends rather than overwrites.
	req, _ := http.NewRequest("GET", h.tsrv.URL+"/api/v1/recall/alpha-notes", nil)
	req.Header.Add(netid.ClientIPHeader, "203.0.113.7")
	req.Header.Add(netid.ClientIPHeader, "203.0.113.99")
	req.Header.Set("Authorization", "Bearer "+wideToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("two client-IP headers: got %d, want the uniform 401", resp.StatusCode)
	}
}

// headerFingerprint is the sorted header set, excluding the two fields that are
// allowed to differ per response: the moment it was generated, and nothing else.
func headerFingerprint(h http.Header) string {
	var parts []string
	for name, values := range h {
		if name == "Date" {
			continue
		}
		parts = append(parts, name+"="+strings.Join(values, ","))
	}
	slices.Sort(parts)
	return strings.Join(parts, ";")
}

func TestTheNonAPIPathIsNotCountedAsAFailedAuth(t *testing.T) {
	// 🔴 A CORRECTION RATHER THAN AN OVERSIGHT. Counting a wrong PATH as a failed auth
	// measured as a legitimate client HOLDING THE RIGHT TOKEN locking itself out for
	// fifteen minutes by requesting five ordinary wrong paths. The specification says
	// five failed AUTHS per minute, and a request to a path that never reaches the token
	// check is not one.
	srv, err := newTestServer(t)
	if err != nil {
		t.Fatal(err)
	}
	srv.Audit = func(string) {}
	// A limiter with a threshold of two, so five wrong paths would lock out twice over.
	srv.Limiter = netid.NewRateLimiter(2, time.Minute, 15*time.Minute)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	h := &harness{srv: srv, root: srv.StoreRoot, tsrv: ts}

	for i := 0; i < 5; i++ {
		if got := h.do(t, "GET", "/favicon.ico", wideToken, nil, ""); got.status != 401 {
			t.Fatalf("a non-API path is the uniform 401, got %d", got.status)
		}
	}
	if got := h.do(t, "GET", "/api/v1/snapshot", wideToken, nil, ""); got.status != 200 {
		t.Fatalf("the valid credential must still work: got %d %q", got.status, got.body)
	}
	// The CONTROL: a WRONG CREDENTIAL *is* counted, so the limiter is wired to
	// something. Two failures reach the threshold; the third request is the lockout.
	for i := 0; i < 2; i++ {
		h.do(t, "GET", "/api/v1/snapshot", "wrong-cred", nil, "")
	}
	if got := h.do(t, "GET", "/api/v1/snapshot", wideToken, nil, ""); got.status != 401 {
		t.Fatalf("the control failed: a wrong credential was not counted either, so "+
			"this test cannot see the difference it was written for (got %d)", got.status)
	}
}

func TestThePreconditionLadder(t *testing.T) {
	h := newHarness(t)
	entryPath := filepath.Join(h.root, "alpha-notes", "gadget-one.md")
	original, err := os.ReadFile(entryPath)
	if err != nil {
		t.Fatal(err)
	}
	revision := write.EntryRevision(original)
	target := "/api/v1/entry/alpha-notes/gadget-one"
	valid := "---\nservice: gadget-one\nscope: alpha-notes\n---\n\n## Nuance / work-history\n- x\n"

	// 🔴 THE LADDER IS ORDERED, AND THE ORDER IS THE CONTRACT. A request carrying two
	// problems gets ONE answer, and which one it gets is what the corpus records.
	cases := []struct {
		name             string
		headers          map[string]string
		body             string
		wantStatus       int
		wantStatusHeader string
		wantBodyHas      string
	}{
		{
			name: "NEITHER header is 428 — an optional precondition is no precondition",
			body: "nope\n", wantStatus: 428, wantStatusHeader: "precondition-required",
			wantBodyHas: "precondition required: send If-Match",
		},
		{
			name:    "BOTH headers is a contradiction, refused rather than resolved",
			headers: map[string]string{"If-Match": `"` + revision + `"`, "If-None-Match": "*"},
			body:    "nope\n", wantStatus: 400, wantStatusHeader: "bad-request",
			wantBodyHas: "contradictory on a PUT",
		},
		{
			name:    "`If-Match: *` turns the guard off while looking like it is on",
			headers: map[string]string{"If-Match": "*"},
			body:    "nope\n", wantStatus: 400, wantStatusHeader: "bad-request",
			wantBodyHas: "If-Match: * is refused",
		},
		{
			name:    "a header present that names NO entity-tag is not a precondition",
			headers: map[string]string{"If-Match": ","},
			body:    "nope\n", wantStatus: 400, wantStatusHeader: "bad-request",
			wantBodyHas: "If-Match names no entity-tag",
		},
		{
			name:    "a LIST on If-None-Match is the lost update spelled backwards",
			headers: map[string]string{"If-None-Match": `"aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb"`},
			body:    "nope\n", wantStatus: 400, wantStatusHeader: "bad-request",
			wantBodyHas: "only `If-None-Match: *` is supported",
		},
		{
			name:    "a stale If-Match is 412 and the file is untouched",
			headers: map[string]string{"If-Match": `"0000000000000000"`},
			body:    valid, wantStatus: 412, wantStatusHeader: "precondition-failed",
			wantBodyHas: "the entry has changed since that revision",
		},
		{
			name:    "a create over an existing entry is 412 with a DISTINCT token",
			headers: map[string]string{"If-None-Match": "*"},
			body:    valid, wantStatus: 412, wantStatusHeader: "already-exists",
			wantBodyHas: "already exists",
		},
		{
			name:    "a correct precondition and invalid bytes is 422, nothing written",
			headers: map[string]string{"If-Match": `"` + revision + `"`},
			body:    "not an entry at all\n", wantStatus: 422, wantStatusHeader: "entry-shape",
			wantBodyHas: "the index loader would reject these bytes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := h.do(t, "PUT", target, wideToken, tc.headers, tc.body)
			if got.status != tc.wantStatus {
				t.Fatalf("got %d %q, want %d", got.status, got.body, tc.wantStatus)
			}
			if got.headers.Get("X-Store-Status") != tc.wantStatusHeader {
				t.Fatalf("X-Store-Status: got %q want %q",
					got.headers.Get("X-Store-Status"), tc.wantStatusHeader)
			}
			if !strings.Contains(got.body, tc.wantBodyHas) {
				t.Fatalf("body %q does not carry %q", got.body, tc.wantBodyHas)
			}
			// 🔴 NOT ONE OF THESE MAY HAVE WRITTEN A BYTE.
			after, err := os.ReadFile(entryPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(original) {
				t.Fatal("a refused precondition must leave the entry untouched")
			}
		})
	}

	// 🔴 THE 412 PAIR CARRIES ITS REMEDY IN A HEADER, BECAUSE THE STATUS CODE CANNOT.
	// A caller told only "412" cannot tell "somebody moved it, re-sync and re-apply"
	// from "it is already there, you wanted a replace", and one of those two is an
	// infinite retry.
	stale := h.do(t, "PUT", target, wideToken, map[string]string{"If-Match": `"0000000000000000"`}, valid)
	taken := h.do(t, "PUT", target, wideToken, map[string]string{"If-None-Match": "*"}, valid)
	if stale.headers.Get("X-Store-Status") == taken.headers.Get("X-Store-Status") {
		t.Fatal("the two 412s must be distinguishable by a client")
	}
	// The stale one carries the CURRENT revision, so the client can retry.
	if stale.headers.Get("ETag") != `"`+revision+`"` {
		t.Fatalf("a client told only `no` cannot retry: ETag %q", stale.headers.Get("ETag"))
	}

	t.Run("a correct precondition replaces, answers 200 and returns the new ETag", func(t *testing.T) {
		got := h.do(t, "PUT", target, wideToken, map[string]string{"If-Match": `"` + revision + `"`}, valid)
		if got.status != 200 || got.body != "replaced\n" {
			t.Fatalf("got %d %q", got.status, got.body)
		}
		if got.headers.Get("ETag") != `"`+write.EntryRevision([]byte(valid))+`"` {
			t.Fatalf("ETag %q", got.headers.Get("ETag"))
		}
		// 🔴 AND THE NAME IS SPELLED `ETag` ON THE WIRE, NOT `Etag`. Go's canonical
		// form is the latter and the parsed lookup above cannot tell them apart, so
		// this reads the bytes — see the note in TestTheUniform401.
		wire := h.rawRequest(t, "PUT", target, wideToken,
			"If-Match: \""+write.EntryRevision([]byte(valid))+"\"",
			"Content-Length: "+strconv.Itoa(len(valid)), "", valid)
		if !strings.Contains(wire, "ETag: ") {
			t.Fatalf("the wire does not carry an `ETag:` header:\n%s", wire)
		}
		// 🔴 A 200 KEEPS ITS CONNECTION; EVERY OTHER CODE CLOSES IT.
		if got.headers.Get("Connection") != "" {
			t.Fatalf("a 200 must not close: %q", got.headers.Get("Connection"))
		}
	})

	t.Run("a create answers 201, which is the only way to tell it from a replace", func(t *testing.T) {
		created := "---\nservice: newcomer-five\nscope: beta-notes\n---\n\n## Nuance / work-history\n- x\n"
		got := h.do(t, "PUT", "/api/v1/entry/beta-notes/newcomer-five", wideToken,
			map[string]string{"If-None-Match": "*"}, created)
		if got.status != 201 || got.body != "created\n" {
			t.Fatalf("got %d %q", got.status, got.body)
		}
		if got.headers.Get("X-Store-Status") != "created" {
			t.Fatalf("X-Store-Status %q", got.headers.Get("X-Store-Status"))
		}
		// ⚠ A 201 TAKES THE CLOSE BRANCH TOO, and that is a wasted handshake on a verb
		// that runs once per entry rather than a defect — the value of the rule is that
		// it is unconditional. Asserted on the WIRE, because the client consumes the
		// header: see `harness.raw`.
		second := strings.Replace(created, "newcomer-five", "second-newcomer", 1)
		wire := h.rawRequest(t, "PUT", "/api/v1/entry/beta-notes/second-newcomer", wideToken,
			"If-None-Match: *",
			"Content-Length: "+strconv.Itoa(len(second)),
			"", second)
		if !strings.Contains(wire, "201 Created") {
			t.Fatalf("the raw control did not reach the create path:\n%s", wire)
		}
		if !strings.Contains(wire, "Connection: close") {
			t.Fatalf("the close-on-not-200 rule is deliberately unconditional:\n%s", wire)
		}
	})

	t.Run("a ref that normalizes away is SAID rather than 404'd", func(t *testing.T) {
		// The asymmetry with the append route — where the same shape is a 404, because
		// the allowlist answers first — is real and is recorded rather than smoothed over.
		got := h.do(t, "PUT", "/api/v1/entry/beta-notes/___", wideToken,
			map[string]string{"If-None-Match": "*"}, "nope\n")
		if got.status != 400 || !strings.Contains(got.body, "normalize to a non-empty slug") {
			t.Fatalf("got %d %q", got.status, got.body)
		}
	})
}

func TestALegacyTokenMayNotWriteButMayStillRead(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		method, target, body string
		headers              map[string]string
	}{
		{"POST", "/api/v1/entry/alpha-notes/gadget-one/bullets", `{"text":"x","session":"s"}`, nil},
		{"PUT", "/api/v1/entry/beta-notes/legacy-create", "nope\n", map[string]string{"If-None-Match": "*"}},
	} {
		got := h.do(t, tc.method, tc.target, legacyToken, tc.headers, tc.body)
		if got.status != 403 {
			t.Fatalf("%s %s: got %d, want 403", tc.method, tc.target, got.status)
		}
		if got.headers.Get("X-Store-Status") != "legacy-cannot-write" {
			t.Fatalf("X-Store-Status %q", got.headers.Get("X-Store-Status"))
		}
		if !strings.Contains(got.body, "`<token> <identity> <scopes>` row") {
			t.Fatalf("the refusal must name the row shape that fixes it: %q", got.body)
		}
	}
	// 🔴 READS FROM A LEGACY TOKEN ARE UNCHANGED, AND IT IS UNRESTRICTED. The refusal
	// is on the write ROUTES only; a bare row reads a scope no allowlist names.
	if got := h.do(t, "GET", "/api/v1/snapshot", legacyToken, nil, ""); got.status != 200 {
		t.Fatalf("a legacy token must still read: got %d %q", got.status, got.body)
	}

	// 🔴 ORDER: ROUTE FIRST, THEN THE LEGACY REFUSAL. Refusing before the route lookup
	// would answer 403 to a legacy caller's write verb at a READ route, which is a
	// wrong-method and must stay one.
	if got := h.do(t, "POST", "/api/v1/recall/alpha-notes", legacyToken, nil, "{}"); got.status != 405 {
		t.Fatalf("a write verb at a read route is a wrong method even for a legacy "+
			"token: got %d", got.status)
	}
}

func TestARefusedScopeAnswersWhatANeverExistedScopeAnswers(t *testing.T) {
	h := newHarness(t)
	// 🔴 THE ENUMERATION PROPERTY ON EVERY WRITE ROUTE. A scope OUTSIDE the caller's
	// allowlist, a scope that has never existed, a ref that resolves to nothing and an
	// entry the loader could not parse all answer the SAME bytes with the SAME headers.
	pairs := []struct {
		name            string
		refused, absent reply
	}{
		{
			name: "append",
			refused: h.do(t, "POST", "/api/v1/entry/alpha-notes/gadget-one/bullets",
				narrowToken, nil, `{"text":"x","session":"s"}`),
			absent: h.do(t, "POST", "/api/v1/entry/ghost-void/gadget-one/bullets",
				narrowToken, nil, `{"text":"x","session":"s"}`),
		},
		{
			name: "replace",
			refused: h.do(t, "PUT", "/api/v1/entry/alpha-notes/gadget-one", narrowToken,
				map[string]string{"If-Match": `"0000000000000000"`}, "nope\n"),
			absent: h.do(t, "PUT", "/api/v1/entry/ghost-void/gadget-one", narrowToken,
				map[string]string{"If-Match": `"0000000000000000"`}, "nope\n"),
		},
		{
			name: "create",
			refused: h.do(t, "PUT", "/api/v1/entry/alpha-notes/newcomer", narrowToken,
				map[string]string{"If-None-Match": "*"}, "nope\n"),
			absent: h.do(t, "PUT", "/api/v1/entry/ghost-void/newcomer", narrowToken,
				map[string]string{"If-None-Match": "*"}, "nope\n"),
		},
	}
	for _, pair := range pairs {
		t.Run(pair.name, func(t *testing.T) {
			if pair.refused.status != 404 {
				t.Fatalf("a refused scope must be 404, got %d %q", pair.refused.status, pair.refused.body)
			}
			if pair.refused.status != pair.absent.status ||
				pair.refused.body != pair.absent.body ||
				headerFingerprint(pair.refused.headers) != headerFingerprint(pair.absent.headers) {
				t.Fatalf("a refusal is DISTINGUISHABLE from an absence:\n%+v\n%+v",
					pair.refused, pair.absent)
			}
		})
	}
	// …and an unresolvable ref in a scope the caller CAN write is the same answer.
	unknownRef := h.do(t, "POST", "/api/v1/entry/beta-notes/ghost-ref/bullets", wideToken,
		nil, `{"text":"x","session":"s"}`)
	if unknownRef.status != 404 || unknownRef.body != pairs[0].refused.body {
		t.Fatalf("an unresolvable ref must answer the same 404: %+v", unknownRef)
	}
}

func TestAppendAttributesFromTheTokenAndDiscardsABodyActor(t *testing.T) {
	h := newHarness(t)
	got := h.do(t, "POST", "/api/v1/entry/alpha-notes/gadget-one/bullets", wideToken, nil,
		`{"actor":"somebody-else","session":"conf-1","text":"the suite appended this bullet."}`)
	if got.status != 200 {
		t.Fatalf("got %d %q", got.status, got.body)
	}
	// 🔴 THE ACTOR COMES FROM THE TOKEN AND THE BODY CANNOT SUPPLY IT. This is the whole
	// of the attribution guarantee, and it is structural: the render function takes the
	// actor as a parameter the request cannot populate.
	want := "- 2000-01-05: the suite appended this bullet. [cairn: wide-reader/conf-1]\n"
	if got.body != want {
		t.Fatalf("\n got: %q\nwant: %q", got.body, want)
	}
	if strings.Contains(got.body, "somebody-else") {
		t.Fatal("a client-supplied actor must be DISCARDED, not honoured")
	}
	if got.headers.Get("X-Store-Status") != "appended" {
		t.Fatalf("X-Store-Status %q", got.headers.Get("X-Store-Status"))
	}
	// 🔴 THE IDEMPOTENCY CRITERION AS A RELATIONSHIP BETWEEN TWO REQUESTS: the identical
	// text under a DIFFERENT session answers `duplicate` and writes nothing, because the
	// content hash covers neither the date, the actor nor the session.
	again := h.do(t, "POST", "/api/v1/entry/alpha-notes/gadget-one/bullets", wideToken, nil,
		`{"session":"conf-2","text":"the suite appended this bullet."}`)
	if again.headers.Get("X-Store-Status") != "duplicate" {
		t.Fatalf("a re-POST under a different session must be a duplicate: %q",
			again.headers.Get("X-Store-Status"))
	}
	if again.body != want {
		t.Fatalf("the duplicate must echo the STORED line: %q", again.body)
	}
	if again.headers.Get("X-Cairn-Bullet") != got.headers.Get("X-Cairn-Bullet") {
		t.Fatal("the content hash must be the same for the same content")
	}
}

func TestAMalformedBodyIsAnsweredAndNotDropped(t *testing.T) {
	// 🔴 THE HALF OF TWO ORACLE-SPECIFIC CORPUS CASES THAT **IS** A CONTRACT. Their
	// bodies quote CPython's own JSON diagnostic, which is why the corpus asserts them
	// against the oracle only; the status, the header, the message PREFIX and the fact
	// that the connection survives are the same for every implementation and are pinned
	// here. See `tests/conformance/requests.json` for the reclassification and its reason.
	h := newHarness(t)
	for _, body := range []string{"{not json", strings.Repeat("[", 50), "[]", "42"} {
		got := h.do(t, "POST", "/api/v1/entry/alpha-notes/gadget-one/bullets", wideToken, nil, body)
		if got.status != 400 {
			t.Fatalf("body %.12q: got %d %q, want 400", body, got.status, got.body)
		}
		if got.headers.Get("X-Store-Status") != "bad-request" {
			t.Fatalf("body %.12q: X-Store-Status %q", body, got.headers.Get("X-Store-Status"))
		}
		if !strings.HasPrefix(got.body, "bad request: ") {
			t.Fatalf("body %.12q: %q", body, got.body)
		}
	}
	// A body that is valid JSON but not an object gets the OTHER refusal, which is a
	// different sentence and must not be folded into the parser's.
	got := h.do(t, "POST", "/api/v1/entry/alpha-notes/gadget-one/bullets", wideToken, nil, "[]")
	if !strings.Contains(got.body, "the body must be a JSON object") {
		t.Fatalf("`not JSON` and `not an object` are different refusals: %q", got.body)
	}
	// …and the server is still serving, which is the property a dropped connection
	// would break.
	if alive := h.do(t, "GET", HealthPath, "", nil, ""); alive.status != 200 {
		t.Fatal("the server stopped answering after a malformed body")
	}
}

func TestQueryParametersNeverSilentlyDefault(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		target      string
		wantBodyHas string
	}{
		{"/api/v1/recall/alpha-notes?limit=abc", "limit must be an integer, got 'abc'"},
		{"/api/v1/recall/alpha-notes?limit=0", "limit must be an int >= 1, got 0"},
		{"/api/v1/recall/alpha-notes?page=0", "page must be an int >= 1, got 0"},
		{"/api/v1/recall/alpha-notes?mode=telepathy",
			"mode must be one of ('digest', 'list', 'full'), got 'telepathy'"},
		{"/api/v1/search/beta-notes", "q is required and must be non-empty"},
		{"/api/v1/search/beta-notes?q=%20", "q is required and must be non-empty"},
		{"/api/v1/search/beta-notes?q=x&threshold=warm", "threshold must be a number, got 'warm'"},
		{"/api/v1/search/beta-notes?q=x&threshold=2", "threshold must be a number in [0, 1], got 2.0"},
		{"/api/v1/search/beta-notes?q=x&max_hits=0", "max-hits must be an int >= 1, got 0"},
		{"/api/v1/snapshot?scope=a.b", "invalid scope"},
		{"/api/v1/recall/%2e%2e", "invalid path component"},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			got := h.do(t, "GET", tc.target, wideToken, nil, "")
			if got.status != 400 {
				t.Fatalf("got %d %q, want 400", got.status, got.body)
			}
			if got.body != "bad request: "+tc.wantBodyHas+"\n" {
				t.Fatalf("\n got: %q\nwant: %q", got.body, "bad request: "+tc.wantBodyHas+"\n")
			}
			if got.headers.Get("X-Store-Status") != "bad-request" {
				t.Fatalf("X-Store-Status %q", got.headers.Get("X-Store-Status"))
			}
		})
	}
	// 🔴 A REPEATED PARAMETER TAKES THE LAST VALUE, and that choice is a contract a
	// port must reproduce rather than an accident of the parser.
	last := h.do(t, "GET", "/api/v1/recall/alpha-notes?limit=0&limit=1", wideToken, nil, "")
	if last.status == 400 {
		t.Fatalf("the LAST value wins, so this must not be the limit refusal: %q", last.body)
	}
}

func TestABadPercentEscapeIsREFUSEDAndNotSILENTLYDEFAULTED(t *testing.T) {
	// 🔴 THE ONE DIVERGENCE THAT WAS MEASURED RATHER THAN REVIEWED, AND IT WAS IN THE
	// SILENT-DEFAULT DIRECTION. `url.Values`/`r.URL.Query()` DROPS a pair whose escape it
	// cannot read and discards the error, so `?limit=%zz` reached the handler as NO
	// `limit` at all and the default was used — while the oracle's `parse_qs` yields
	// `['%zz']` and the parameter validator answers `400 … got '%zz'`. A caller who
	// typo'd an escape would have been told their setting took effect, which is exactly
	// what "a query parameter NEVER silently defaults" forbids.
	//
	// The corpus cannot see this: its runner builds targets from the declared list and
	// never sends a malformed escape. See `query.go` for the side-by-side measurement.
	h := newHarness(t)
	for _, tc := range []struct{ target, want string }{
		{"/api/v1/recall/alpha-notes?limit=%zz", "limit must be an integer, got '%zz'"},
		{"/api/v1/recall/alpha-notes?limit=%2", "limit must be an integer, got '%2'"},
		{"/api/v1/search/beta-notes?q=x&threshold=%zz", "threshold must be a number, got '%zz'"},
	} {
		got := h.do(t, "GET", tc.target, wideToken, nil, "")
		if got.status != 400 {
			t.Fatalf("%s: got %d %q — a bad escape must be REFUSED, not defaulted",
				tc.target, got.status, got.body)
		}
		if got.body != "bad request: "+tc.want+"\n" {
			t.Fatalf("%s:\n got: %q\nwant: %q", tc.target, got.body, "bad request: "+tc.want+"\n")
		}
	}
	// …and a VALID escape still decodes, so the fix is not "refuse everything with a
	// percent in it". `%31` is `1`.
	if got := h.do(t, "GET", "/api/v1/recall/alpha-notes?limit=%31", wideToken, nil, ""); got.status == 400 {
		t.Fatalf("a valid escape must decode: %q", got.body)
	}
}

func TestTheQueryParserMatchesTheOracleOnTheShapesThatDiffer(t *testing.T) {
	// The unit-level pair, spelled with the answers CPython gives — measured with
	// `parse_qs(q, keep_blank_values=True)` on 3.12 and written out by hand, never
	// derived from this package's own output.
	for _, tc := range []struct {
		raw  string
		key  string
		want []string
	}{
		{"limit=%zz", "limit", []string{"%zz"}},
		{"limit=%2", "limit", []string{"%2"}},
		{"limit=1&limit=2", "limit", []string{"1", "2"}},
		{"q=%20", "q", []string{" "}},
		{"foo", "foo", []string{""}},
		{"limit=", "limit", []string{""}},
		// A percent-escaped multi-byte character is ONE character, which is why the
		// byte decode comes before the UTF-8 decode.
		{"a=%C3%A9", "a", []string{"\u00e9"}},
		{"a=x+y", "a", []string{"x y"}},
		// A lone `%` and an escape at the very end are left literal, as the oracle
		// leaves them.
		{"a=100%", "a", []string{"100%"}},
	} {
		got := parseQuery(tc.raw)[tc.key]
		if len(got) != len(tc.want) || strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Fatalf("parseQuery(%q)[%q] = %q, want %q", tc.raw, tc.key, got, tc.want)
		}
	}
	// The CONTROL on the claim: Go's own parser must DISAGREE on the first two shapes,
	// or this function and this test are solving a problem that does not exist.
	for _, raw := range []string{"limit=%zz", "limit=%2"} {
		if _, err := url.ParseQuery(raw); err == nil {
			t.Fatalf("the control failed: url.ParseQuery(%q) no longer errors, so the "+
				"divergence this parser exists for may have gone away — re-measure "+
				"before deleting it", raw)
		}
	}
	if _, err := url.ParseQuery("limit=1"); err != nil {
		t.Fatalf("the control's other half: a well-formed query must not error: %v", err)
	}
}

func TestAnUnknownRouteIsA404AndNeverA405(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		method, target string
		wantStatus     int
		wantBody       string
	}{
		// A head with no row. Spelled with an underscore on purpose: that spelling
		// defeated a regex-based route guard on the oracle.
		{"GET", "/api/v1/raw_dump", 404, "no such endpoint\n"},
		{"GET", "/api/v1/", 404, "no such endpoint\n"},
		{"GET", "/api/v1/recall", 404, "no such endpoint\n"},
		{"GET", "/api/v1/recall/alpha-notes/extra", 404, "no such endpoint\n"},
		// 🔴 `entry` IS A WRITE HEAD ONLY. A GET at it is a 404 from the read table,
		// not a 405.
		{"GET", "/api/v1/entry/alpha-notes/gadget-one", 404, "no such endpoint\n"},
		// PATCH and DELETE appear in no write row, which is how they stay refused —
		// not by a separate rejecter they could be re-bound away from.
		{"PATCH", "/api/v1/entry/alpha-notes/gadget-one", 405, "read-only\n"},
		{"DELETE", "/api/v1/entry/alpha-notes/gadget-one", 405, "read-only\n"},
		{"POST", "/api/v1/recall/alpha-notes", 405, "read-only\n"},
		{"PUT", "/api/v1/snapshot", 405, "read-only\n"},
		// The fixed TAIL is part of the route identity: a request that spells it
		// differently does not dispatch at all.
		{"POST", "/api/v1/entry/alpha-notes/gadget-one/comments", 405, "read-only\n"},
	} {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			got := h.do(t, tc.method, tc.target, wideToken, nil, "{}")
			if got.status != tc.wantStatus || got.body != tc.wantBody {
				t.Fatalf("got %d %q, want %d %q", got.status, got.body, tc.wantStatus, tc.wantBody)
			}
			if tc.wantStatus == 405 && got.headers.Get("Allow") != "GET, HEAD" {
				t.Fatalf("Allow %q", got.headers.Get("Allow"))
			}
		})
	}
}

func TestHEADCarriesEveryHeaderAndNoBody(t *testing.T) {
	h := newHarness(t)
	get := h.do(t, "GET", "/api/v1/snapshot", wideToken, nil, "")
	head := h.do(t, "HEAD", "/api/v1/snapshot", wideToken, nil, "")
	if head.status != get.status {
		t.Fatalf("HEAD %d vs GET %d", head.status, get.status)
	}
	if head.body != "" {
		t.Fatalf("a HEAD sends no body, got %q", head.body)
	}
	// 🔴 A HEAD MUST REPORT THE LENGTH ITS GET WOULD HAVE SENT. A HEAD that
	// understates it is how a client truncates a report it never sees the rest of; the
	// naive implementation — no body, so report no length — is what this kills.
	if head.headers.Get("Content-Length") != get.headers.Get("Content-Length") {
		t.Fatalf("HEAD Content-Length %q, GET %q",
			head.headers.Get("Content-Length"), get.headers.Get("Content-Length"))
	}
	for _, name := range []string{"X-Store-Status", "X-Store-Entries", "X-Store-Snapshot", "Content-Type"} {
		if head.headers.Get(name) != get.headers.Get(name) {
			t.Fatalf("%s: HEAD %q, GET %q", name, head.headers.Get(name), get.headers.Get(name))
		}
	}
}

func TestTheReportRoutesRenderAndStillRefuseFirst(t *testing.T) {
	h := newHarness(t)
	// ⚠ THIS TEST USED TO PIN THE P1a STATE — a 501 with `X-Store-Status:
	// not-implemented` — so a missing renderer could not be mistaken for a working one.
	// P1b implemented it, so the assertion moved with the behaviour rather than being
	// deleted: what still has to hold is that the LADDER in front of the renderer is
	// unchanged, which is the half a working renderer could quietly swallow.
	got := h.do(t, "GET", "/api/v1/recall/alpha-notes", wideToken, nil, "")
	if got.status != 200 {
		t.Fatalf("got %d %q", got.status, got.body)
	}
	if got.headers.Get("X-Store-Status") != "recalled" || got.headers.Get("X-Store-Exit") != "0" {
		t.Fatalf("X-Store-Status %q X-Store-Exit %q",
			got.headers.Get("X-Store-Status"), got.headers.Get("X-Store-Exit"))
	}
	// 🔴 `X-Store-Revision` IS THE HEADER P1b ADDED, and it is the ONE header on this route
	// that is not derived from the narrowed index — it is read off `<scope>/.git/HEAD`. A
	// report answered without it is a report whose scope cannot be quoted as `scope@sha`,
	// and its ABSENCE is what a P1a-era port shipped.
	if got.headers.Get("X-Store-Revision") != "unknown" {
		t.Fatalf("X-Store-Revision %q, want `unknown` for a scope that is not a git repo",
			got.headers.Get("X-Store-Revision"))
	}
	if !strings.Contains(got.body, "subsystem-recall: status=recalled scope=alpha-notes") {
		t.Fatalf("body %q", got.body)
	}
	// …and the REFUSALS on those routes come FIRST: an unauthorised caller must never reach
	// the renderer, and a bad parameter must not be silently defaulted by it.
	if unauth := h.do(t, "GET", "/api/v1/recall/alpha-notes", "", nil, ""); unauth.status != 401 {
		t.Fatalf("authentication precedes the renderer: got %d", unauth.status)
	}
	if bad := h.do(t, "GET", "/api/v1/recall/alpha-notes?limit=0", wideToken, nil, ""); bad.status != 400 {
		t.Fatalf("parameter validation precedes the renderer: got %d %q", bad.status, bad.body)
	}
	// 🔴 AND THE REFUSED SCOPE IS STILL INDISTINGUISHABLE FROM AN ABSENT ONE ON THE HEADER
	// THE RENDERER DOES NOT CONTROL. `narrow-reader` may not see `alpha-notes`; a scope that
	// never existed answers the same. The single licence to differ is the scope NAME, which
	// a report echoes.
	refused := h.do(t, "GET", "/api/v1/recall/alpha-notes", narrowToken, nil, "")
	absent := h.do(t, "GET", "/api/v1/recall/ghost-void", narrowToken, nil, "")
	if refused.headers.Get("X-Store-Revision") != absent.headers.Get("X-Store-Revision") {
		t.Fatalf("a refused scope's revision must match an absent one's: %q vs %q",
			refused.headers.Get("X-Store-Revision"), absent.headers.Get("X-Store-Revision"))
	}
	if refused.headers.Get("X-Store-Status") != "scope-absent" {
		t.Fatalf("a refused scope answers what an absent one answers: %q",
			refused.headers.Get("X-Store-Status"))
	}
}

func TestAuditFieldCannotForgeARecordBoundary(t *testing.T) {
	// 🔴 THE AUDIT LINE IS A LOG-INJECTION SINK REACHED BEFORE AUTH. The request path is
	// percent-decoded, so `%0a` becomes a REAL NEWLINE — and a caller who can forge a
	// line can keep any fingerprint alive forever (blocking rotation), fabricate an
	// `auth=ok` from an address of their choice, and drown the auth-failure alert.
	var lines []string
	srv, err := newTestServer(t)
	if err != nil {
		t.Fatal(err)
	}
	srv.Audit = func(line string) { lines = append(lines, line) }
	ts := httptest.NewServer(srv)
	defer ts.Close()
	h := &harness{srv: srv, root: srv.StoreRoot, tsrv: ts}

	h.do(t, "GET", "/api/v1/x%0astore-api%20audit%20token=forged%20auth=ok", wideToken, nil, "")
	if len(lines) == 0 {
		t.Fatal("the request produced no audit line at all, so this test is vacuous")
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "\n\r") {
			t.Fatalf("a forged line boundary reached the sink: %q", line)
		}
		if strings.Contains(line, "audit token=forged") {
			t.Fatalf("a forged FIELD reached the sink unescaped: %q", line)
		}
	}
	// The positive control: an ordinary path is readable in the line, so the escaping is
	// not simply eating everything.
	lines = nil
	h.do(t, "GET", "/api/v1/snapshot", wideToken, nil, "")
	if len(lines) != 1 || !strings.Contains(lines[0], "path=/api/v1/snapshot") {
		t.Fatalf("an ordinary path must be legible: %v", lines)
	}
	if !strings.Contains(lines[0], "identity=wide-reader") ||
		!strings.Contains(lines[0], "token="+authz.TokenID(wideToken)) {
		t.Fatalf("the line must name WHICH credential matched: %q", lines[0])
	}
	if strings.Contains(lines[0], wideToken) {
		t.Fatalf("the audit line must never carry the token itself: %q", lines[0])
	}
}

func TestTheSnapshotIsNarrowedButFreshnessIsStoreWide(t *testing.T) {
	h := newHarness(t)
	wide := h.do(t, "GET", "/api/v1/snapshot", wideToken, nil, "")
	narrow := h.do(t, "GET", "/api/v1/snapshot", narrowToken, nil, "")
	if wide.headers.Get("X-Store-Entries") != "2" || narrow.headers.Get("X-Store-Entries") != "1" {
		t.Fatalf("the member count must follow the allowlist: wide=%q narrow=%q",
			wide.headers.Get("X-Store-Entries"), narrow.headers.Get("X-Store-Entries"))
	}
	// ⚠ THE FRESHNESS BLOCK IS STORE-WIDE, AND IT IS A RESIDUAL COUNT LEAK RATHER THAN
	// an oversight: it carries a total `entry-files=` over scopes the caller cannot name.
	// That is deliberate — it is the freshness guarantee the whole snapshot design rests
	// on, and scoping it would make a caller's view of staleness a function of its own
	// allowlist — but it IS why the byte-identity claim is about a REFUSED scope versus
	// an ABSENT one and never about two different stores.
	if wide.headers.Get("X-Store-Snapshot") != narrow.headers.Get("X-Store-Snapshot") {
		t.Fatalf("the freshness header is store-wide by design: wide=%q narrow=%q",
			wide.headers.Get("X-Store-Snapshot"), narrow.headers.Get("X-Store-Snapshot"))
	}
	if !strings.Contains(wide.headers.Get("X-Store-Snapshot"), "entry-files=2") {
		t.Fatalf("X-Store-Snapshot %q", wide.headers.Get("X-Store-Snapshot"))
	}
	if wide.headers.Get("Content-Type") != "application/gzip" {
		t.Fatalf("Content-Type %q", wide.headers.Get("Content-Type"))
	}
}

func TestTheStoreWasNotReadIsA503AndNotAnEmptyAnswer(t *testing.T) {
	// 🔴 THE STATE THIS WHOLE DESIGN EXISTS TO KEEP SEPARATE. "Reached the store and
	// there is genuinely nothing recorded" and "could not read the store at all" MUST NOT
	// render alike, because a 200 is a claim that the store was read.
	srv, err := newTestServer(t)
	if err != nil {
		t.Fatal(err)
	}
	srv.Audit = func(string) {}
	srv.StoreRoot = filepath.Join(srv.StoreRoot, "does-not-exist")
	ts := httptest.NewServer(srv)
	defer ts.Close()
	h := &harness{srv: srv, root: srv.StoreRoot, tsrv: ts}

	for _, tc := range []struct{ method, target, body string }{
		{"GET", "/api/v1/snapshot", ""},
		{"POST", "/api/v1/entry/alpha-notes/gadget-one/bullets", `{"text":"x","session":"s"}`},
	} {
		got := h.do(t, tc.method, tc.target, wideToken, nil, tc.body)
		if got.status != 503 {
			t.Fatalf("%s %s: got %d %q, want 503", tc.method, tc.target, got.status, got.body)
		}
		if got.headers.Get("X-Store-Status") != "store-unreachable" ||
			got.headers.Get("X-Store-Exit") != "3" {
			t.Fatalf("%s: headers %v", tc.target, got.headers)
		}
	}
	// …and health still answers, because it is the kubelet's probe and says nothing
	// about the store.
	if got := h.do(t, "GET", HealthPath, "", nil, ""); got.status != 200 {
		t.Fatal("health must not depend on the store")
	}
}

func TestEveryRefusalClosesItsConnectionAndA200DoesNot(t *testing.T) {
	// 🔴 A REJECTED REQUEST NEVER KEEPS ITS CONNECTION. If framing was ever mis-read the
	// socket is already untrustworthy, and reusing it is the smuggling primitive itself.
	//
	// Asserted on the WIRE rather than on the parsed response — `Connection` is
	// hop-by-hop and Go's client consumes it, so a parsed-view assertion would pass
	// whether or not the server sent the header at all. Setting the flag alone closes
	// the socket but tells the PEER nothing, and a pooling proxy that only saw the
	// close keeps its pool entry and discovers it on the next use — so the HEADER is
	// the part being pinned here.
	h := newHarness(t)
	for _, tc := range []struct {
		name, method, target, token string
		wantClose                   bool
	}{
		{"a 401", "GET", "/api/v1/recall/alpha-notes", "", true},
		{"a 404", "GET", "/api/v1/raw_dump", wideToken, true},
		{"a 405", "PATCH", "/api/v1/entry/alpha-notes/gadget-one", wideToken, true},
		{"a 400", "GET", "/api/v1/snapshot?scope=a.b", wideToken, true},
		{"a 200", "GET", "/api/v1/snapshot", wideToken, false},
		{"health", "GET", HealthPath, "", false},
	} {
		wire := h.rawRequest(t, tc.method, tc.target, tc.token)
		head, _, found := strings.Cut(wire, "\r\n\r\n")
		if !found {
			t.Fatalf("%s: no header/body boundary in:\n%q", tc.name, wire)
		}
		closed := strings.Contains(head, "Connection: close")
		if closed != tc.wantClose {
			t.Fatalf("%s: want close=%v, head was:\n%s", tc.name, tc.wantClose, head)
		}
	}
}

func TestVisibleScopesDefaultsToTheEmptySet(t *testing.T) {
	// 🔴 AN INVARIANT GUARD, LABELLED AS ONE: no bug ever set this field wrong, because
	// the per-request value's ZERO VALUE is the empty set and unrestricted is reachable
	// only by asking for it. What it pins is that the property survives a refactor — the
	// day a route runs before authorization, it must see NOTHING rather than everything.
	var zero store.ScopeSet
	if zero.Unrestricted {
		t.Fatal("the zero ScopeSet must not be unrestricted")
	}
	if zero.Allows("alpha-notes") {
		t.Fatal("the zero ScopeSet must allow nothing")
	}
	rq := &request{}
	if rq.visible.Unrestricted || rq.visible.Allows("alpha-notes") {
		t.Fatal("a request that has not authenticated must see nothing")
	}
}
