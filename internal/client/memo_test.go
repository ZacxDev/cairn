package client

// The memo verbs (S3 of `claudedocs/plan-cairn-scope-mail.md`), against an in-process STUB of the
// client listener: the cursor, the status tokens, the two-instance routing of the id verbs, the
// configuration keys and the parser's refusals. What only the real listener and the real store can
// show — authority, the quota, the scan, revocation — is `tests/memo/e2e.sh`.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/memowire"
)

// stubMemo is one memo in the stub listener, with its TRUE creation time (sub-second); the stub
// sends it truncated to whole seconds, as `memo.Render` does.
type stubMemo struct {
	id        int64
	scope     string
	created   time.Time
	subject   string
	retracted bool
}

// memoStub is a client listener that answers the four routes over a fixed set of memos, counts
// requests, and checks the bearer token. `after` filters on the TRUE time, as the store does.
type memoStub struct {
	mu       sync.Mutex
	token    string
	memos    []*stubMemo
	requests int
	srv      *httptest.Server
}

func newMemoStub(t *testing.T, token string, memos ...*stubMemo) *memoStub {
	t.Helper()
	s := &memoStub{token: token, memos: memos}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *memoStub) count() int { s.mu.Lock(); defer s.mu.Unlock(); return s.requests }

func (s *memoStub) rendered(m *stubMemo) memo.Rendered {
	r := memo.Rendered{ID: m.id, Scope: m.scope, Sender: "writer-a via agents@host-a (user)",
		CreatedAt: m.created.UTC().Format(time.RFC3339), ExpiresAt: m.created.Add(72 * time.Hour).UTC().Format(time.DateOnly)}
	if m.retracted {
		r.Tombstone = "retracted by writer-a via agents@host-a (user) at " + m.created.Add(time.Minute).UTC().Format(time.RFC3339)
	} else {
		r.Subject, r.Body = m.subject, "body of "+m.subject
	}
	return r
}

func (s *memoStub) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	notFound := func() { w.WriteHeader(http.StatusNotFound); _, _ = fmt.Fprint(w, `{"error":"not-found"}`+"\n") }
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"unauthorized"}`+"\n")
		return
	}
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == memowire.MemosPath:
		var after time.Time
		if raw := q.Get("after"); raw != "" {
			after, _ = time.Parse(time.RFC3339Nano, raw)
		}
		body := memowire.ListBody{Memos: []memo.Rendered{}}
		held := false
		for _, m := range s.memos {
			if m.scope != q.Get("scope") {
				continue
			}
			held = true
			if m.created.After(after) && (!m.retracted || q.Get("tombstones") == "1") {
				body.Memos = append(body.Memos, s.rendered(m))
			}
		}
		// `limit` caps the page as the store does (oldest first): what makes `memo-read`'s paging
		// reachable at all.
		if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && len(body.Memos) > n {
			body.Memos = body.Memos[:n]
		}
		if !held {
			notFound()
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	case r.Method == http.MethodGet && r.URL.Path == memowire.MemoPath:
		id, _ := strconv.ParseInt(q.Get("id"), 10, 64)
		for _, m := range s.memos {
			if m.id == id && m.scope == q.Get("scope") {
				_ = json.NewEncoder(w).Encode(memowire.MemoBody{Memo: s.rendered(m)})
				return
			}
		}
		notFound()
	case r.Method == http.MethodPost && r.URL.Path == memowire.RetractPath:
		var req memowire.RetractRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		for _, m := range s.memos {
			if m.id == req.ID && m.scope == req.Scope && !m.retracted {
				m.retracted = true
				_ = json.NewEncoder(w).Encode(memowire.RetractedBody{Retracted: m.id})
				return
			}
		}
		notFound()
	case r.Method == http.MethodPost && r.URL.Path == memowire.MemosPath:
		var req memowire.SendRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		id := int64(len(s.memos) + 100)
		s.memos = append(s.memos, &stubMemo{id: id, scope: req.Scope, created: time.Now(), subject: req.Subject})
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(memowire.SentBody{ID: id, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	default:
		http.NotFound(w, r)
	}
}

// memoHost is one synthetic host: a HOME, a config dir, a state dir, and the cache scopes it holds.
// Every client-read environment name is pinned (a blank value reads as unset), so the operator's own
// configuration cannot reach a test.
type memoHost struct {
	home, cfgDir, state string
}

func newMemoHost(t *testing.T, cached ...string) memoHost {
	t.Helper()
	home := t.TempDir()
	h := memoHost{home: home, cfgDir: filepath.Join(home, "cfg"), state: filepath.Join(home, "state")}
	for _, name := range EnvReadsWithAliases() {
		t.Setenv(name, "")
	}
	t.Setenv(HomeEnv, home)
	t.Setenv(ConfigEnv, filepath.Join(h.cfgDir, "env"))
	t.Setenv(StateHomeEnv, h.state)
	for _, scope := range cached {
		if err := os.MkdirAll(filepath.Join(home, ".cache", "subsystem-store", scope), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

// configure writes one instance's env file ("" alias = the default instance).
func (h memoHost) configure(t *testing.T, alias string, lines ...string) {
	t.Helper()
	path := filepath.Join(h.cfgDir, "env")
	if alias != "" {
		path = filepath.Join(h.cfgDir, InstanceDirName, alias+InstanceSuffix)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

const stubToken = "memo-test-agent-credential-0000000000000000"

func uiLines(url string) []string {
	return []string{EnvUIURL + "=" + url, EnvUIToken + "=" + stubToken}
}

var memoT0 = time.Date(2000, 1, 2, 3, 4, 5, 250_000_000, time.UTC)

// firstLine is stderr's first line — where `memo-status=` must be.
func firstLine(s string) string { line, _, _ := strings.Cut(s, "\n"); return line }

// blockCount parses a block's `count=` (0 when stdout is empty).
func blockCount(t *testing.T, out string) int {
	t.Helper()
	if out == "" {
		return 0
	}
	head := firstLine(out)
	i := strings.Index(head, " count=")
	if !strings.HasPrefix(head, "<<<cairn-memo untrusted nonce=") || i < 0 {
		t.Fatalf("stdout is not a memo block:\n%s", out)
	}
	n, err := strconv.Atoi(strings.TrimSuffix(head[i+len(" count="):], ">>>"))
	if err != nil {
		t.Fatalf("unreadable count in %q", head)
	}
	return n
}

// TestTheCheckDeliversEachMemoOncePerSession — the cursor (decision 6, S3 test plan): a new session
// sees every live memo in one block; its SECOND check prints zero bytes, exits 0 and reports `none`;
// ANOTHER session sees them again. A memo that arrives later is shown once to the first session.
// RED with the cursor never saved (every check re-delivers) and with the seen-set ignored.
func TestTheCheckDeliversEachMemoOncePerSession(t *testing.T) {
	stub := newMemoStub(t, stubToken,
		&stubMemo{id: 1, scope: "alpha-notes", created: memoT0, subject: "one"},
		&stubMemo{id: 2, scope: "alpha-notes", created: memoT0.Add(time.Minute), subject: "two"})
	h := newMemoHost(t, "alpha-notes")
	h.configure(t, "", uiLines(stub.srv.URL)...)

	code, out, errOut := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001")
	if code != ExitOK || firstLine(errOut) != "memo-status=new" || blockCount(t, out) != 2 {
		t.Fatalf("first check: exit %d, stderr %q, count %d\n%s", code, errOut, blockCount(t, out), out)
	}
	code, out, errOut = runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001")
	if code != ExitOK || out != "" || firstLine(errOut) != "memo-status=none" {
		t.Fatalf("second check of the same session: exit %d, stdout %d bytes, stderr %q", code, len(out), errOut)
	}
	code, out, errOut = runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0002")
	if code != ExitOK || firstLine(errOut) != "memo-status=new" || blockCount(t, out) != 2 {
		t.Fatalf("a new session: exit %d, stderr %q, count %d", code, errOut, blockCount(t, out))
	}
	stub.mu.Lock()
	stub.memos = append(stub.memos, &stubMemo{id: 3, scope: "alpha-notes", created: memoT0.Add(2 * time.Minute), subject: "three"})
	stub.mu.Unlock()
	_, out, _ = runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001")
	if blockCount(t, out) != 1 || !strings.Contains(out, "subject: three") {
		t.Fatalf("the later memo was not delivered alone to s-0001:\n%s", out)
	}
	if _, out, _ = runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001"); out != "" {
		t.Fatalf("the later memo was delivered twice:\n%s", out)
	}
}

// TestTheSeenSetOutlivesTheWholeSecondTruncation — the wire carries whole seconds, so a memo whose
// TRUE time is just past `high_water − margin` arrives with a time up to one second EARLIER. Here
// memo 1 is at T+0.25s and memo 2 at T+120.5s: after the first check the high-water is T+120s, the
// next `after` is T+0s, and memo 1 (true T+0.25s) is returned AGAIN — with wire time T+0s, which is
// NOT after `hw − margin`. A prune at `hw − margin` forgets it and shows it twice; the prune at
// `hw − margin − 1s` keeps it. RED with the extra second removed.
func TestTheSeenSetOutlivesTheWholeSecondTruncation(t *testing.T) {
	base := time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
	stub := newMemoStub(t, stubToken,
		&stubMemo{id: 1, scope: "alpha-notes", created: base.Add(250 * time.Millisecond), subject: "one"},
		&stubMemo{id: 2, scope: "alpha-notes", created: base.Add(memo.CursorMargin + 500*time.Millisecond), subject: "two"})
	h := newMemoHost(t, "alpha-notes")
	h.configure(t, "", uiLines(stub.srv.URL)...)
	if _, out, _ := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001"); blockCount(t, out) != 2 {
		t.Fatalf("POSITIVE CONTROL: the first check did not deliver both:\n%s", out)
	}
	code, out, errOut := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001")
	if code != ExitOK || out != "" || firstLine(errOut) != "memo-status=none" {
		t.Fatalf("a memo at the margin's edge was delivered TWICE (exit %d, %q):\n%s", code, errOut, out)
	}
}

// TestACorruptCursorIsReadAsAbsentAndRewritten — a cursor that is not JSON is treated as absent,
// the backlog is delivered as ONE block, the file is REWRITTEN, and the next check is silent —
// never a crash. The stderr line says what happened, after the status token.
func TestACorruptCursorIsReadAsAbsentAndRewritten(t *testing.T) {
	stub := newMemoStub(t, stubToken, &stubMemo{id: 1, scope: "alpha-notes", created: memoT0, subject: "one"})
	h := newMemoHost(t, "alpha-notes")
	h.configure(t, "", uiLines(stub.srv.URL)...)
	path := filepath.Join(h.state, "cairn", "memo", DefaultAlias, "s-0001.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001")
	if code != ExitOK || blockCount(t, out) != 1 || firstLine(errOut) != "memo-status=new" ||
		!strings.Contains(errOut, "is corrupt; treated as absent and rewritten") {
		t.Fatalf("corrupt cursor: exit %d, count %d, stderr %q", code, blockCount(t, out), errOut)
	}
	data, err := os.ReadFile(path)
	if err != nil || !json.Valid(data) {
		t.Fatalf("the cursor was not rewritten: %q (%v)", data, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("the cursor is mode %v, want 0600", info.Mode().Perm())
	}
	if _, out, _ := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001"); out != "" {
		t.Fatalf("after the rewrite the memo was delivered again:\n%s", out)
	}
}

// TestAnUnwritableStateDirStillDeliversAndSaysTheNextCheckReDelivers — delivery beats bookkeeping:
// the block is printed at exit 0 with a stderr line naming the cost, and the next check DOES
// re-deliver (stated, not hidden).
func TestAnUnwritableStateDirStillDeliversAndSaysTheNextCheckReDelivers(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes, so an unwritable state dir cannot be built")
	}
	stub := newMemoStub(t, stubToken, &stubMemo{id: 1, scope: "alpha-notes", created: memoT0, subject: "one"})
	h := newMemoHost(t, "alpha-notes")
	h.configure(t, "", uiLines(stub.srv.URL)...)
	if err := os.MkdirAll(h.state, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(h.state, 0o700) })
	for i := 0; i < 2; i++ {
		code, out, errOut := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001")
		if code != ExitOK || blockCount(t, out) != 1 || firstLine(errOut) != "memo-status=new" ||
			!strings.Contains(errOut, "the cursor was NOT saved") || !strings.Contains(errOut, "the next check re-delivers") {
			t.Fatalf("check %d with an unwritable state dir: exit %d, count %d, stderr %q", i+1, code, blockCount(t, out), errOut)
		}
	}
}

// TestEveryStatusTokenHasACaseThatProducesIt — each token of the closed set, with its exit code
// (decision 7), and the request counter as the positive control for the quiet cases: `no-scope`
// sends NOTHING (a repo that is not a repo; a scope no local cache holds; no cache at all), while
// `scope-unreadable` is a scope the cache holds and the listener refuses — the counter moves to 1.
func TestEveryStatusTokenHasACaseThatProducesIt(t *testing.T) {
	seen := map[string]bool{}
	check := func(name, wantToken string, wantCode int, argv ...string) string {
		t.Helper()
		code, out, errOut := runCLI(t, append([]string{"memo-check", "--session", "s-0001"}, argv...)...)
		if firstLine(errOut) != memoStatusPrefix+wantToken || code != wantCode {
			t.Errorf("%s: exit %d, first stderr line %q; want %d and memo-status=%s", name, code, firstLine(errOut), wantCode, wantToken)
		}
		seen[wantToken] = true
		return out
	}

	stub := newMemoStub(t, stubToken, &stubMemo{id: 1, scope: "alpha-notes", created: memoT0, subject: "one"},
		&stubMemo{id: 2, scope: "gamma-notes", created: memoT0, subject: "elsewhere"})
	h := newMemoHost(t, "alpha-notes", "beta-notes")
	h.configure(t, "", uiLines(stub.srv.URL)...)

	if out := check("a directory that is not a repository", "no-scope", ExitOK, "--repo", t.TempDir()); out != "" {
		t.Errorf("no-scope printed %q", out)
	}
	check("a scope no local cache holds", "no-scope", ExitOK, "--scope", "gamma-notes")
	if n := stub.count(); n != 0 {
		t.Fatalf("no-scope sent %d request(s); it must send none", n)
	}
	check("beta-notes: cached, refused by the listener", "scope-unreadable", ExitUnreachableNoCache, "--scope", "beta-notes")
	if n := stub.count(); n != 1 {
		t.Fatalf("POSITIVE CONTROL: scope-unreadable moved the request counter to %d, want 1", n)
	}
	check("a new memo", "new", ExitOK, "--scope", "alpha-notes")
	check("nothing new", "none", ExitOK, "--scope", "alpha-notes")

	h.configure(t, "", EnvUIURL+"=http://127.0.0.1:1", EnvUIToken+"="+stubToken)
	check("a listener that does not answer", "unreachable", ExitUnreachableNoCache, "--scope", "alpha-notes")

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `{"memos": [{"ID": "x"}]}`) }))
	defer bad.Close()
	h.configure(t, "", uiLines(bad.URL)...)
	check("an answer that is not a list of memos", "malformed", ExitCorrupt, "--scope", "alpha-notes")

	h.configure(t, "", EnvUIURL+"="+stub.srv.URL)
	beforeUnconfigured := stub.count()
	check("no CAIRN_UI_TOKEN", "unconfigured", ExitUnreachableNoCache, "--scope", "alpha-notes")
	if n := stub.count() - beforeUnconfigured; n != 0 {
		// The line that separates `unconfigured` from `refused`: a host with no key SENDS NOTHING,
		// even with the listener's URL in hand, so nothing counts toward the listener's lockout.
		t.Fatalf("unconfigured sent %d request(s) to the listener; it must send none", n)
	}
	h.configure(t, "", EnvUIToken+"="+stubToken)
	check("no CAIRN_UI_URL", "unconfigured", ExitUnreachableNoCache, "--scope", "alpha-notes")
	h.configure(t, "", EnvUIURL+"="+stub.srv.URL, EnvUIToken+"=memo-test-a-token-the-listener-never-issued")
	before401 := stub.count()
	check("a credential the listener refuses", "refused", ExitUnreachableNoCache, "--scope", "alpha-notes")
	if n := stub.count() - before401; n != 1 {
		t.Fatalf("POSITIVE CONTROL: refused moved the request counter by %d, want 1 (it is a SENT credential)", n)
	}

	// No cache at all.
	if err := os.RemoveAll(filepath.Join(h.home, ".cache")); err != nil {
		t.Fatal(err)
	}
	h.configure(t, "", uiLines(stub.srv.URL)...)
	before := stub.count()
	check("no cache at all", "no-scope", ExitOK, "--scope", "alpha-notes")
	if stub.count() != before {
		t.Fatalf("no-scope with no cache sent a request")
	}
	for _, token := range memo.Statuses() {
		if !seen[token] {
			t.Errorf("no case produced memo-status=%s", token)
		}
	}
}

// TestUnconfiguredNamesEachMissingKeySeparately — one key present does not make the other optional,
// and the line names WHICH is missing.
func TestUnconfiguredNamesEachMissingKeySeparately(t *testing.T) {
	h := newMemoHost(t, "alpha-notes")
	for _, tc := range []struct {
		lines   []string
		missing string
	}{
		{[]string{EnvUIURL + "=http://127.0.0.1:1"}, "CAIRN_UI_TOKEN not set"},
		{[]string{EnvUIToken + "=" + stubToken}, "CAIRN_UI_URL not set"},
	} {
		h.configure(t, "", tc.lines...)
		code, _, errOut := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001")
		if code != ExitUnreachableNoCache || firstLine(errOut) != "memo-status=unconfigured" || !strings.Contains(errOut, tc.missing) {
			t.Errorf("%v: exit %d, stderr %q; want unconfigured naming %q", tc.lines, code, errOut, tc.missing)
		}
	}
}

// TestTheStatusTokenIsTheFirstStderrLineEvenWithADeprecationWarning — `Run` warns about deprecated
// variables BEFORE parsing; for `memo-check` that warning would be the first stderr line and the
// hook would read `malformed`. It is held until the token is out. RED with the hold removed.
func TestTheStatusTokenIsTheFirstStderrLineEvenWithADeprecationWarning(t *testing.T) {
	stub := newMemoStub(t, stubToken, &stubMemo{id: 1, scope: "alpha-notes", created: memoT0, subject: "one"})
	h := newMemoHost(t, "alpha-notes")
	h.configure(t, "", uiLines(stub.srv.URL)...)
	t.Setenv("SUBSYSTEM_STORE_URL", "http://127.0.0.1:1")
	_, _, errOut := runCLI(t, "memo-check", "--scope", "alpha-notes", "--session", "s-0001")
	if firstLine(errOut) != "memo-status=new" || !strings.Contains(errOut, "SUBSYSTEM_STORE_URL") {
		t.Fatalf("stderr:\n%s\nwant the status token first and the deprecation warning after it", errOut)
	}
}

// TestTheParserRefusesTheMemoVerbsMisuse — exit 2, each: `--session` with no value or outside the
// session grammar; the id verbs without `--scope`/`--repo` (the `--repo .` default does not count);
// `memo-retract` without `--id` or with a non-integer one; `memo-send` with both or neither body
// flag. `--statuses` prints the closed table and needs no `--session`.
func TestTheParserRefusesTheMemoVerbsMisuse(t *testing.T) {
	newMemoHost(t)
	for _, argv := range [][]string{
		{"memo-check", "--scope", "alpha-notes", "--session"},
		{"memo-check", "--scope", "alpha-notes", "--session", "has space"},
		{"memo-check", "--scope", "alpha-notes", "--session", "-leading-dash"},
		{"memo-check", "--scope", "alpha-notes"},
		{"memo-check", "--scope", "alpha-notes", "--session", "s-1", "--timeout", "0"},
		{"memo-read", "--id", "17"},
		{"memo-read"},
		{"memo-retract", "--id", "17"},
		{"memo-retract", "--scope", "alpha-notes"},
		{"memo-retract", "--scope", "alpha-notes", "--id", "seventeen"},
		{"memo-read", "--scope", "alpha-notes", "--id", "0"},
		{"memo-send", "--scope", "alpha-notes", "--subject", "s"},
		{"memo-send", "--scope", "alpha-notes", "--subject", "s", "--body", "b", "--body-file", "f"},
		{"memo-send", "--scope", "alpha-notes", "--body", "b"},
		{"memo-send", "--scope", "alpha-notes", "--subject", "s", "--body", "b", "--ttl", "soon"},
	} {
		if code, _, errOut := runCLI(t, argv...); code != ExitUsage {
			t.Errorf("%q: exit %d, want 2 (%s)", argv, code, errOut)
		}
	}
	code, out, _ := runCLI(t, "memo-check", "--statuses")
	if code != ExitOK || out != strings.Join(memo.Statuses(), "\n")+"\n" {
		t.Fatalf("--statuses: exit %d, %q", code, out)
	}
}

// TestTheIdVerbsRouteByScopeAcrossTwoInstances — round 1 🔴3: TWO listeners, one per instance, each
// holding a memo with id 17 in a scope routed to it. `memo-read --scope <A's scope> --id 17` is A's
// memo and `--scope <B's scope> --id 17` is B's; `memo-retract` retracts only the routed one (the
// other still reads live); and A's scope with an id only B holds is `not-found`. RED with the id
// verbs routed by the DEFAULT instance instead of by scope.
func TestTheIdVerbsRouteByScopeAcrossTwoInstances(t *testing.T) {
	a := newMemoStub(t, stubToken, &stubMemo{id: 17, scope: "alpha-notes", created: memoT0, subject: "from instance A"})
	b := newMemoStub(t, stubToken,
		&stubMemo{id: 17, scope: "beta-notes", created: memoT0, subject: "from instance B"},
		&stubMemo{id: 23, scope: "beta-notes", created: memoT0, subject: "only on B"})
	h := newMemoHost(t, "alpha-notes")
	h.configure(t, "", append(uiLines(a.srv.URL), "CAIRN_URL=http://127.0.0.1:1", "CAIRN_TOKEN=t")...)
	h.configure(t, "second", append(uiLines(b.srv.URL), "CAIRN_URL=http://127.0.0.1:1", "CAIRN_TOKEN=t")...)
	if err := os.WriteFile(filepath.Join(h.cfgDir, RoutesFileName), []byte(`{"alpha-notes": "personal", "beta-notes": "second"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	read := func(scope, id string) (int, string) {
		code, out, _ := runCLI(t, "memo-read", "--scope", scope, "--id", id)
		return code, out
	}
	if code, out := read("alpha-notes", "17"); code != ExitOK || !strings.Contains(out, "subject: from instance A") {
		t.Fatalf("alpha-notes 17: exit %d\n%s", code, out)
	}
	if code, out := read("beta-notes", "17"); code != ExitOK || !strings.Contains(out, "subject: from instance B") {
		t.Fatalf("beta-notes 17: exit %d\n%s", code, out)
	}
	if code, out := read("alpha-notes", "23"); code != ExitUnreachableNoCache || out != "" {
		t.Fatalf("alpha-notes 23 (only B holds 23): exit %d, want 3 not-found\n%s", code, out)
	}
	if code, out, errOut := runCLI(t, "memo-retract", "--scope", "beta-notes", "--id", "17"); code != ExitOK || !strings.Contains(out, "instance=second") {
		t.Fatalf("retracting beta-notes 17: exit %d %q %q", code, out, errOut)
	}
	if _, out := read("beta-notes", "17"); !strings.Contains(out, "retracted by") {
		t.Fatalf("B's 17 was not retracted:\n%s", out)
	}
	if _, out := read("alpha-notes", "17"); !strings.Contains(out, "subject: from instance A") {
		t.Fatalf("A's 17 was retracted by a retraction routed to B:\n%s", out)
	}
	// The LISTING (no --id) shows B's scope with full bodies AND the retracted memo as its tombstone
	// (decision 10) — one block, count=2.
	code, out, _ := runCLI(t, "memo-read", "--scope", "beta-notes")
	if code != ExitOK || blockCount(t, out) != 2 || !strings.Contains(out, "|   body: body of only on B") ||
		!strings.Contains(out, "|   retracted by writer-a") || strings.Contains(out, "from instance B") {
		t.Fatalf("the listing of beta-notes: exit %d\n%s", code, out)
	}
	// POSITIVE CONTROL: an unrouted scope refuses at 11 before any request.
	before := a.count() + b.count()
	if code, _, _ := runCLI(t, "memo-read", "--scope", "gamma-notes", "--id", "17"); code != ExitUnrouted || a.count()+b.count() != before {
		t.Fatalf("an unrouted scope answered %d after %d request(s)", code, a.count()+b.count()-before)
	}
}

// TestTheSendAndRetractClassifyTheListenersAnswer — the write bucket: a send that lands prints the
// id and instance at exit 0; the listener's refusal is 6 naming the reason and the rule (never text);
// 401 is 6; an unreachable listener is 7; and an invalid-UTF-8 body is refused HERE at 6 without a
// request (JSON would silently replace the bytes).
func TestTheSendAndRetractClassifyTheListenersAnswer(t *testing.T) {
	stub := newMemoStub(t, stubToken)
	h := newMemoHost(t, "alpha-notes")
	h.configure(t, "", uiLines(stub.srv.URL)...)
	code, out, errOut := runCLI(t, "memo-send", "--scope", "alpha-notes", "--subject", "s", "--body", "b", "--ttl", "2d")
	if code != ExitOK || !strings.Contains(out, "memo sent — id=100 scope=alpha-notes instance=personal") {
		t.Fatalf("send: exit %d %q %q", code, out, errOut)
	}
	refuse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"refused","reason":"secret","rule":"github-token"}`+"\n")
	}))
	defer refuse.Close()
	h.configure(t, "", uiLines(refuse.URL)...)
	code, _, errOut = runCLI(t, "memo-send", "--scope", "alpha-notes", "--subject", "s", "--body", "b")
	if code != ExitWriteRefused || !strings.Contains(errOut, "REFUSED [secret] (rule github-token)") {
		t.Fatalf("refused send: exit %d %q", code, errOut)
	}
	h.configure(t, "", EnvUIURL+"="+stub.srv.URL, EnvUIToken+"=memo-test-a-token-the-listener-never-issued")
	if code, _, errOut = runCLI(t, "memo-send", "--scope", "alpha-notes", "--subject", "s", "--body", "b"); code != ExitWriteRefused ||
		!strings.Contains(errOut, "REFUSED [unauthorized]") {
		t.Fatalf("a refused credential: exit %d %q", code, errOut)
	}
	if code, _, _ = runCLI(t, "memo-retract", "--scope", "alpha-notes", "--id", "5"); code != ExitWriteRefused {
		t.Fatalf("a refused credential on retract: exit %d", code)
	}
	h.configure(t, "", EnvUIURL+"=http://127.0.0.1:1", EnvUIToken+"="+stubToken)
	if code, _, _ = runCLI(t, "memo-send", "--scope", "alpha-notes", "--subject", "s", "--body", "b"); code != ExitWriteUnreachable {
		t.Fatalf("an unreachable listener: exit %d, want 7", code)
	}
	h.configure(t, "", uiLines(stub.srv.URL)...)
	before := stub.count()
	bodyFile := filepath.Join(t.TempDir(), "body")
	if err := os.WriteFile(bodyFile, []byte("bad \xff byte"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut = runCLI(t, "memo-send", "--scope", "alpha-notes", "--subject", "s", "--body-file", bodyFile); code != ExitWriteRefused ||
		!strings.Contains(errOut, "[characters]") || stub.count() != before {
		t.Fatalf("invalid UTF-8: exit %d %q after %d request(s)", code, errOut, stub.count()-before)
	}
	if code, _, errOut = runCLI(t, "memo-retract", "--scope", "alpha-notes", "--id", "100"); code != ExitOK {
		t.Fatalf("retracting the sent memo: exit %d %q", code, errOut)
	}
	if code, _, errOut = runCLI(t, "memo-retract", "--scope", "alpha-notes", "--id", "100"); code != ExitWriteRefused || !strings.Contains(errOut, "[not-found]") {
		t.Fatalf("retracting it again: exit %d %q", code, errOut)
	}
}

// TestTheTTLFlagSpellings — `--ttl` takes Go durations and whole days; the value reaches the wire in
// seconds, and the listener's ceiling (not this parser) refuses a long one.
func TestTheTTLFlagSpellings(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 0, "3d": 72 * time.Hour, "90m": 90 * time.Minute, "72h": 72 * time.Hour, "31d": 31 * 24 * time.Hour} {
		if got, ok := parseMemoTTL(in); !ok || got != want {
			t.Errorf("parseMemoTTL(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"soon", "3.5d", "d", "99999d"} {
		if _, ok := parseMemoTTL(in); ok {
			t.Errorf("parseMemoTTL(%q) accepted", in)
		}
	}
}

// TestMemoSendHelpSaysWhatTheScanCannotCatch — `memo-send --help` states the secret scan's limits
// (decision 11), the same claim the `cairn-memo` skill and `memo.CheckSend`'s doc make, so a sender
// who reads only `--help` does not take a clean send for a clean memo. 🔴 PINNED AS ONE WHOLE
// NORMALISED STRING, LITERALLY, and never read from the constant: a guard on a few WORDS is walked
// by a reword, and a test reading the constant follows any edit to it. A reword fails here — pay it.
// RED before the note existed (the help carried the one-line Help and the flags only).
func TestMemoSendHelpSaysWhatTheScanCannotCatch(t *testing.T) {
	const want = "The send-time secret scan refuses only a credential recognisable by its own shape " +
		"(a PEM private key, a vendor-prefixed token, a JWT, an Authorization or Bearer value, " +
		"a password in a URL). Everything else goes through: a password in prose, a short or " +
		"unnamed token, an unprefixed hex secret, any novel format. Never put a secret in a " +
		"memo; if one gets through, `cairn memo-retract` it at once."
	var stdout strings.Builder
	if code := Run(Env{Stdout: &stdout, Stderr: os.Stderr}, []string{"memo-send", "--help"}); code != ExitOK {
		t.Fatalf("memo-send --help: exit %d", code)
	}
	got := strings.Join(strings.Fields(stdout.String()), " ")
	if !strings.Contains(got, want) {
		t.Fatalf("memo-send --help does not state the scan's limits; got:\n%s", stdout.String())
	}
	// POSITIVE CONTROL for the normalisation: the verb's own one-line Help is found the same way.
	if !strings.Contains(got, "send a short memo to the scope (writes to the memo listener)") {
		t.Fatalf("the help text was not read at all:\n%s", stdout.String())
	}
}
