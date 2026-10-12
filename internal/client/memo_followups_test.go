package client

// The memo verbs' guards added after #239's round-1 audit (scope mail S3): each pins a mechanism a
// named mutant left green at `0d9ba01` — the cursor's margin and its `after` parameter, the paged
// listing, `memo-check`'s own instance routing — and the four behaviour fixes that round asked for
// (tolerant response decoding, the sub-second TTL, print-before-save, the derivation error). The
// stub listener and the synthetic host are `memo_test.go`'s.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/memowire"
)

// TestALateCommitterInsideTheMarginIsDelivered — the cursor's MARGIN (decision 6): ids and
// `created_at` are allocation order, not commit order, so a memo can become visible AFTER a newer
// one was delivered. Here memo 2 (T+1m) is delivered first; memo 1 (T+30s) commits late. The next
// check asks from `high_water − margin` = T−1m, so memo 1 is returned and delivered. RED with the
// margin removed (`after` = the high-water itself): memo 1 is older than T+1m and is LOST.
func TestALateCommitterInsideTheMarginIsDelivered(t *testing.T) {
	stub := newMemoStub(t, stubToken, &stubMemo{id: 2, scope: "alpha-notes", created: memoT0.Add(time.Minute), subject: "two"})
	h := newMemoHost(t, "alpha-notes")
	h.configure(t, "", uiLines(stub.srv.URL)...)
	if _, out, _ := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001"); blockCount(t, out) != 1 ||
		!strings.Contains(out, "subject: two") {
		t.Fatalf("POSITIVE CONTROL: the first check did not deliver memo 2:\n%s", out)
	}
	stub.mu.Lock()
	stub.memos = append([]*stubMemo{{id: 1, scope: "alpha-notes", created: memoT0.Add(30 * time.Second), subject: "one, committed late"}}, stub.memos...)
	stub.mu.Unlock()
	code, out, errOut := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001")
	if code != ExitOK || firstLine(errOut) != "memo-status=new" || blockCount(t, out) != 1 || !strings.Contains(out, "subject: one, committed late") {
		t.Fatalf("a memo that committed late, inside the margin, was LOST (exit %d, %q):\n%s", code, firstLine(errOut), out)
	}
}

// TestTheCheckSendsItsCursorSoAPrunedMemoIsNotReDelivered — the `after` parameter is what keeps a
// memo the seen-set has PRUNED from coming back. Memos at T and T+5m: after the first check the
// high-water is T+5m and memo 1 (T, older than `hw − margin − 1s`) is pruned from the seen-set. The
// next checks send `after` = T+3m, the listener returns memo 2 only (seen), and they are silent. RED
// with `after` never sent: the listener returns memo 1 again, the seen-set no longer holds it, and it
// is delivered AGAIN — on every check.
func TestTheCheckSendsItsCursorSoAPrunedMemoIsNotReDelivered(t *testing.T) {
	stub := newMemoStub(t, stubToken,
		&stubMemo{id: 1, scope: "alpha-notes", created: memoT0, subject: "one"},
		&stubMemo{id: 2, scope: "alpha-notes", created: memoT0.Add(5 * time.Minute), subject: "two"})
	h := newMemoHost(t, "alpha-notes")
	h.configure(t, "", uiLines(stub.srv.URL)...)
	if _, out, _ := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001"); blockCount(t, out) != 2 {
		t.Fatalf("POSITIVE CONTROL: the first check did not deliver both:\n%s", out)
	}
	for i := 2; i <= 3; i++ {
		code, out, errOut := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001")
		if code != ExitOK || out != "" || firstLine(errOut) != "memo-status=none" {
			t.Fatalf("check %d re-delivered a memo the seen-set had pruned (exit %d, %q):\n%s", i, code, firstLine(errOut), out)
		}
	}
}

// TestMemoReadPagesPastOneListingPage — `memo-read` without `--id` reads EVERY page: 250 memos is
// more than one page (`memoListPage` = 200), so the listing asks again from one second before the
// first page's last time and drops the ids it already holds. Memos 200 and 201 share ONE whole
// second at the page boundary — the case the one-second-early boundary exists for. RED with the loop
// stopped after the first page (`count=200`), and RED with the boundary moved to the last time itself
// (memo 201 is skipped: `count=249`).
func TestMemoReadPagesPastOneListingPage(t *testing.T) {
	base := time.Date(2000, 1, 2, 3, 0, 0, 0, time.UTC)
	var memos []*stubMemo
	for i := 1; i <= 250; i++ {
		at := base.Add(time.Duration(i) * time.Second)
		if i >= 201 {
			at = base.Add(time.Duration(i-1) * time.Second) // 201 shares memo 200's second; the rest follow on
		}
		memos = append(memos, &stubMemo{id: int64(i), scope: "alpha-notes", created: at, subject: fmt.Sprintf("memo %03d", i)})
	}
	stub := newMemoStub(t, stubToken, memos...)
	h := newMemoHost(t, "alpha-notes")
	h.configure(t, "", uiLines(stub.srv.URL)...)
	code, out, errOut := runCLI(t, "memo-read", "--scope", "alpha-notes")
	if code != ExitOK || blockCount(t, out) != 250 {
		t.Fatalf("memo-read of 250 memos: exit %d, count=%d (want 250) %q", code, blockCount(t, out), errOut)
	}
	for _, want := range []string{"subject: memo 001", "subject: memo 200", "subject: memo 201", "subject: memo 250"} {
		if n := strings.Count(out, want+"\n"); n != 1 {
			t.Errorf("%q appears %d times, want once", want, n)
		}
	}
	if n := stub.count(); n != 2 {
		t.Fatalf("POSITIVE CONTROL: %d listing request(s), want 2 (one per page)", n)
	}
}

// TestMemoCheckRoutesByScopeAcrossTwoInstances — `memo-check`'s OWN routing (#239 round 1): the
// sibling of `TestTheIdVerbsRouteByScopeAcrossTwoInstances` for the hook's verb, which reads its
// listener configuration itself rather than through `memoRoute`. A scope routed to the second
// instance is checked against THAT instance's listener, and its cursor lives under that instance.
// RED with the configuration read for the DEFAULT instance: B's scope is then asked of A's listener,
// which does not hold it (`scope-unreadable`).
func TestMemoCheckRoutesByScopeAcrossTwoInstances(t *testing.T) {
	a := newMemoStub(t, stubToken, &stubMemo{id: 17, scope: "alpha-notes", created: memoT0, subject: "from instance A"})
	b := newMemoStub(t, stubToken, &stubMemo{id: 17, scope: "beta-notes", created: memoT0, subject: "from instance B"})
	h := newMemoHost(t, "alpha-notes")
	if err := os.MkdirAll(filepath.Join(h.home, ".cache", "subsystem-store-second", "beta-notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.configure(t, "", append(uiLines(a.srv.URL), "CAIRN_URL=http://127.0.0.1:1", "CAIRN_TOKEN=t")...)
	h.configure(t, "second", append(uiLines(b.srv.URL), "CAIRN_URL=http://127.0.0.1:1", "CAIRN_TOKEN=t")...)
	if err := os.WriteFile(filepath.Join(h.cfgDir, RoutesFileName), []byte(`{"alpha-notes": "personal", "beta-notes": "second"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCLI(t, "memo-check", "--scope", "beta-notes", "--session", "s-0001")
	if code != ExitOK || firstLine(errOut) != "memo-status=new" || !strings.Contains(out, "subject: from instance B") {
		t.Fatalf("beta-notes (routed to `second`): exit %d, %q\n%s", code, errOut, out)
	}
	if a.count() != 0 || b.count() != 1 {
		t.Fatalf("beta-notes asked A %d time(s) and B %d; want 0 and 1", a.count(), b.count())
	}
	if _, err := os.Stat(filepath.Join(h.state, "cairn", "memo", "second", "s-0001.json")); err != nil {
		t.Fatalf("the cursor for beta-notes is not under the `second` instance: %v", err)
	}
	code, out, errOut = runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001")
	if code != ExitOK || firstLine(errOut) != "memo-status=new" || !strings.Contains(out, "subject: from instance A") {
		t.Fatalf("POSITIVE CONTROL: alpha-notes (the default instance): exit %d, %q\n%s", code, errOut, out)
	}
}

// fixedAnswer serves ONE fixed 200 answer to every request.
func fixedAnswer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, body) }))
	t.Cleanup(srv.Close)
	return srv
}

// wireMemoJSON is one wire memo's keys — `memo.Rendered`'s field names — without the braces.
const wireMemoJSON = `"ID": 7, "Scope": "alpha-notes", "Sender": "writer-a via agents@host-a (user)", ` +
	`"CreatedAt": "2000-01-02T03:04:05Z", "ExpiresAt": "2000-01-05", "Subject": "s", "Body": "b", "Tombstone": ""`

// TestAResponseFieldTheClientDoesNotKnowIsTolerated — `internal/memowire`'s decoding rule: a
// listener release that ADDS a response field (to the list, to a memo, to a send's or a retraction's
// answer) must not break an older client, so every verb still delivers. RED with unknown fields
// refused (S3's first `decodeStrict`): each of these is then exit 5 (reads) or 7 (writes).
func TestAResponseFieldTheClientDoesNotKnowIsTolerated(t *testing.T) {
	h := newMemoHost(t, "alpha-notes")
	list := fixedAnswer(t, `{"memos": [{`+wireMemoJSON+`, "Priority": "high"}], "next_page": null}`)
	h.configure(t, "", uiLines(list.URL)...)
	code, out, errOut := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001")
	if code != ExitOK || firstLine(errOut) != "memo-status=new" || blockCount(t, out) != 1 {
		t.Fatalf("memo-check over a list with an extra field: exit %d, %q\n%s", code, errOut, out)
	}
	if code, out, errOut = runCLI(t, "memo-read", "--scope", "alpha-notes"); code != ExitOK || blockCount(t, out) != 1 {
		t.Fatalf("memo-read over a list with an extra field: exit %d, %q", code, errOut)
	}
	one := fixedAnswer(t, `{"memo": {`+wireMemoJSON+`, "Priority": "high"}, "served_by": "a later listener"}`)
	h.configure(t, "", uiLines(one.URL)...)
	if code, out, errOut = runCLI(t, "memo-read", "--scope", "alpha-notes", "--id", "7"); code != ExitOK || blockCount(t, out) != 1 {
		t.Fatalf("memo-read --id over a memo with an extra field: exit %d, %q", code, errOut)
	}
	writes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == memowire.RetractPath {
			_, _ = fmt.Fprint(w, `{"retracted": 7, "at": "2000-01-02T03:05:00Z"}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"id": 7, "created_at": "2000-01-02T03:04:05Z", "expires_at": "2000-01-05"}`)
	}))
	defer writes.Close()
	h.configure(t, "", uiLines(writes.URL)...)
	if code, out, errOut = runCLI(t, "memo-send", "--scope", "alpha-notes", "--subject", "s", "--body", "b"); code != ExitOK ||
		!strings.Contains(out, "memo sent — id=7") {
		t.Fatalf("memo-send over an answer with an extra field: exit %d, %q", code, errOut)
	}
	if code, out, errOut = runCLI(t, "memo-retract", "--scope", "alpha-notes", "--id", "7"); code != ExitOK ||
		!strings.Contains(out, "memo retracted — id=7") {
		t.Fatalf("memo-retract over an answer with an extra field: exit %d, %q", code, errOut)
	}
}

// TestAResponseMissingAFieldTheClientReadsIsMalformed — the other half of the rule: tolerating
// EXTRA fields must not become tolerating MISSING ones, or a field renamed on the listener decodes
// as "" with no error. A memo without `Sender`, a list without `memos` and a `null` one are
// `malformed` (exit 5, nothing printed) for both read verbs; a send's answer without `created_at` is
// "whether it was stored is UNKNOWN" (exit 7). RED with the required-key check removed: the memo is
// delivered with an empty sender, a keyless list reads as `none`, and the send reports success.
func TestAResponseMissingAFieldTheClientReadsIsMalformed(t *testing.T) {
	h := newMemoHost(t, "alpha-notes")
	noSender := strings.Replace(wireMemoJSON, `"Sender": "writer-a via agents@host-a (user)", `, "", 1)
	if noSender == wireMemoJSON {
		t.Fatal("HARNESS: the Sender key was not removed from the fixture")
	}
	cases := []struct{ name, body string }{
		{"a memo without Sender", `{"memos": [{` + noSender + `}]}`},
		{"a list without memos", `{"items": [{` + wireMemoJSON + `}]}`},
		{"a memos that is null", `{"memos": null}`},
	}
	for i, tc := range cases {
		srv := fixedAnswer(t, tc.body)
		h.configure(t, "", uiLines(srv.URL)...)
		code, out, errOut := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-000"+strconv.Itoa(i))
		if code != ExitCorrupt || firstLine(errOut) != "memo-status=malformed" || out != "" {
			t.Errorf("memo-check, %s: exit %d, %q; want 5 malformed and nothing printed\n%s", tc.name, code, firstLine(errOut), out)
		}
		if code, out, _ := runCLI(t, "memo-read", "--scope", "alpha-notes"); code != ExitCorrupt || out != "" {
			t.Errorf("memo-read, %s: exit %d; want 5 and nothing printed", tc.name, code)
		}
	}
	srv := fixedAnswer(t, `{"memo": {`+noSender+`}}`)
	h.configure(t, "", uiLines(srv.URL)...)
	if code, out, _ := runCLI(t, "memo-read", "--scope", "alpha-notes", "--id", "7"); code != ExitCorrupt || out != "" {
		t.Errorf("memo-read --id over a memo without Sender: exit %d; want 5", code)
	}
	sent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"id": 7}`)
	}))
	defer sent.Close()
	h.configure(t, "", uiLines(sent.URL)...)
	if code, _, errOut := runCLI(t, "memo-send", "--scope", "alpha-notes", "--subject", "s", "--body", "b"); code != ExitWriteUnreachable ||
		!strings.Contains(errOut, "UNKNOWN") {
		t.Errorf("memo-send over an answer without created_at: exit %d, %q; want 7 UNKNOWN", code, errOut)
	}
}

// TestASubSecondTTLIsRefusedNotSentAsTheDefault — the wire carries whole seconds and 0 is the
// listener's DEFAULT (3 days), so an explicit TTL under one second is refused at 2 with NO request,
// rather than storing a memo for three days at exit 0. `--ttl 1s` is sent as 1, and no `--ttl` at all
// is still 0. RED with the refusal removed (`--ttl 500ms` exits 0 and the listener records 0).
func TestASubSecondTTLIsRefusedNotSentAsTheDefault(t *testing.T) {
	var mu sync.Mutex
	var got []int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req memowire.SendRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		got = append(got, req.TTLSeconds)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"id": 7, "created_at": "2000-01-02T03:04:05Z"}`)
	}))
	defer srv.Close()
	h := newMemoHost(t, "alpha-notes")
	h.configure(t, "", uiLines(srv.URL)...)
	send := func(ttl ...string) (int, string) {
		argv := []string{"memo-send", "--scope", "alpha-notes", "--subject", "s", "--body", "b"}
		if len(ttl) > 0 {
			argv = append(argv, "--ttl", ttl[0])
		}
		code, _, errOut := runCLI(t, argv...)
		return code, errOut
	}
	for _, ttl := range []string{"500ms", "0", "0s", "0d", "999ms"} {
		if code, errOut := send(ttl); code != ExitUsage || !strings.Contains(errOut, "less than one second") {
			t.Errorf("--ttl %s: exit %d, %q; want 2 naming the sub-second TTL", ttl, code, errOut)
		}
	}
	mu.Lock()
	refusedSent := len(got)
	mu.Unlock()
	if refusedSent != 0 {
		t.Fatalf("a refused TTL still sent %d request(s): %v", refusedSent, got)
	}
	if code, errOut := send("1s"); code != ExitOK {
		t.Fatalf("POSITIVE CONTROL: --ttl 1s: exit %d, %q", code, errOut)
	}
	if code, errOut := send(); code != ExitOK {
		t.Fatalf("POSITIVE CONTROL: no --ttl: exit %d, %q", code, errOut)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != 1 || got[1] != 0 {
		t.Fatalf("ttl_seconds on the wire: %v; want [1 0]", got)
	}
}

// cursorAtFirstWrite is a stdout that records, at its FIRST write, whether the cursor file exists.
type cursorAtFirstWrite struct {
	path    string
	writes  int
	existed bool
	buf     strings.Builder
}

func (w *cursorAtFirstWrite) Write(p []byte) (int, error) {
	if w.writes == 0 {
		_, err := os.Stat(w.path)
		w.existed = err == nil
	}
	w.writes++
	return w.buf.Write(p)
}

// TestTheBlockIsPrintedBeforeTheCursorIsSaved — delivery beats bookkeeping, in ORDER: when the block
// reaches stdout the cursor must not yet record it, so a process killed between the two re-delivers
// (a duplicate) rather than loses. Measured by a stdout that looks at the cursor file at its first
// write: a new session's cursor does not exist then, and does afterwards. RED with the save moved
// back before the print (S3's first order).
func TestTheBlockIsPrintedBeforeTheCursorIsSaved(t *testing.T) {
	stub := newMemoStub(t, stubToken, &stubMemo{id: 1, scope: "alpha-notes", created: memoT0, subject: "one"})
	h := newMemoHost(t, "alpha-notes")
	h.configure(t, "", uiLines(stub.srv.URL)...)
	path := filepath.Join(h.state, "cairn", "memo", DefaultAlias, "s-0001.json")
	stdout := &cursorAtFirstWrite{path: path}
	errFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer errFile.Close()
	code := Run(Env{Stdout: stdout, Stderr: errFile, Host: func() string { return "fixture-host-000000000000" }},
		[]string{"memo-check", "--scope", "alpha-notes", "--session", "s-0001"})
	errOut, _ := os.ReadFile(errFile.Name())
	if code != ExitOK || stdout.writes == 0 || blockCount(t, stdout.buf.String()) != 1 || firstLine(string(errOut)) != "memo-status=new" {
		t.Fatalf("POSITIVE CONTROL: exit %d, %d write(s), stderr %q\n%s", code, stdout.writes, errOut, stdout.buf.String())
	}
	if stdout.existed {
		t.Fatalf("the cursor %s already recorded the memo when the block was printed: a kill between them LOSES it", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("POSITIVE CONTROL: the cursor was not saved after the block: %v", err)
	}
}

// TestADerivationErrorIsSaidAfterTheNoScopeToken — a `--repo` whose scope cannot be derived because
// `git` FAILED (here: not on PATH) is still `no-scope` at exit 0 — the hook's contract — but the error
// is on stderr AFTER the token, so a broken `git` is not indistinguishable from "not a repository".
// RED with the error dropped (S3's first spelling).
func TestADerivationErrorIsSaidAfterTheNoScopeToken(t *testing.T) {
	newMemoHost(t, "alpha-notes")
	repo := t.TempDir()
	t.Setenv("PATH", t.TempDir()) // an empty directory: no `git`
	code, out, errOut := runCLI(t, "memo-check", "--repo", repo, "--session", "s-0001")
	if code != ExitOK || out != "" || firstLine(errOut) != "memo-status=no-scope" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want 0, nothing, memo-status=no-scope first", code, out, errOut)
	}
	if !strings.Contains(errOut, "git command failed") {
		t.Fatalf("the derivation error was dropped; stderr:\n%s", errOut)
	}
}

// renderedWireKeys is `memo.Rendered`'s field set AS THIS TEST LAST SAW IT, sorted — a literal, so
// that a field added to (or removed from) the type is a red test whose message carries the deploy
// order, rather than a change the reflection in `renderedKeys` absorbs without anyone reading the rule.
var renderedWireKeys = []string{"Body", "CreatedAt", "ExpiresAt", "ID", "Scope", "Sender", "Subject", "Tombstone"}

// TestTheMemoKeysAClientRequiresAreExactlyRenderedsFields — the relationship `internal/memowire`'s
// deploy-order rule rests on (#241 rounds 0 and 1): the keys a client REQUIRES of a wire memo are
// EXACTLY the keys the listener's encoder writes for `memo.Rendered` — no field optional, none
// required that the type lacks.
//
// The wire's keys are taken from the ENCODER (`json.Marshal` of a `memowire.MemoBody`, as the listener
// does), never from `renderedKeys`, so the expectation is not derived from the implementation. Then,
// key by key: a memo lacking THAT key is refused by BOTH decoding paths (`decodeOne`, `decodeList`),
// with an error naming the key. POSITIVE CONTROL: the complete memo decodes on both.
//
// RED with one field left out of the required set (`memo-required-keys-skip-a-rendered-field`).
func TestTheMemoKeysAClientRequiresAreExactlyRenderedsFields(t *testing.T) {
	const rule = "a client REQUIRES every field of memo.Rendered, so a field ADDED there must reach every LISTENER " +
		"before any client built with it is released, and one REMOVED must leave every client first — " +
		"internal/memowire's package doc is where that rule lives; re-read it, then update renderedWireKeys"
	full := memo.Rendered{ID: 7, Scope: "alpha-notes", Sender: "writer-a via agents@host-a (user)",
		CreatedAt: "2000-01-02T03:04:05Z", ExpiresAt: "2000-01-05", Subject: "kiln schedule", Body: "the second firing moves"}
	data, err := json.Marshal(memowire.MemoBody{Memo: full})
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Memo map[string]json.RawMessage `json:"memo"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	var encoded []string
	for k := range wire.Memo {
		encoded = append(encoded, k)
	}
	sort.Strings(encoded)
	if !reflect.DeepEqual(encoded, renderedWireKeys) {
		t.Fatalf("the listener's encoder writes the memo keys %v, and this test last saw %v.\n%s", encoded, renderedWireKeys, rule)
	}
	required := append([]string(nil), renderedKeys...)
	sort.Strings(required)
	if !reflect.DeepEqual(required, encoded) {
		t.Fatalf("the client requires %v of a wire memo, and the wire carries %v.\n%s", required, encoded, rule)
	}

	// POSITIVE CONTROL: the complete memo decodes on both paths.
	one, _ := json.Marshal(map[string]any{"memo": wire.Memo})
	list, _ := json.Marshal(map[string]any{"memos": []any{wire.Memo}})
	if got, err := decodeOne(one); err != nil || got != full {
		t.Fatalf("POSITIVE CONTROL FAILED: the complete memo did not decode as itself: %+v, %v", got, err)
	}
	if got, err := decodeList(list); err != nil || len(got) != 1 || got[0] != full {
		t.Fatalf("POSITIVE CONTROL FAILED: the complete list did not decode: %+v, %v", got, err)
	}
	for _, key := range encoded {
		short := map[string]json.RawMessage{}
		for k, v := range wire.Memo {
			if k != key {
				short[k] = v
			}
		}
		one, _ := json.Marshal(map[string]any{"memo": short})
		list, _ := json.Marshal(map[string]any{"memos": []any{short}})
		if _, err := decodeOne(one); err == nil || !strings.Contains(err.Error(), strconv.Quote(key)) {
			t.Errorf("a memo without %s decoded on the single-memo path (%v) — it is not required there.\n%s", key, err, rule)
		}
		if _, err := decodeList(list); err == nil || !strings.Contains(err.Error(), strconv.Quote(key)) {
			t.Errorf("a memo without %s decoded on the list path (%v) — it is not required there.\n%s", key, err, rule)
		}
	}
}
