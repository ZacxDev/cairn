package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
	"github.com/ZacxDev/cairn/internal/transcriptwire"
)

// The `transcript` verb (S8 of the transcripts plan) against a stub client listener that answers
// through the REAL `transcript.Store.Answer` — the one function both real listeners serve through —
// over a real archive, as the session's owner. Everything is SYNTHETIC.

const transcriptStubSession = "s-cli-01"

var transcriptStubRecords = []string{
	`{"type":"user","message":{"role":"user","content":"a synthetic request"}}`,
	`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_c1","name":"Bash","input":{"command":"ls"}}]}}`,
	`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_c1","content":"alpha beta"}]},"toolUseResult":{"stdout":"alpha beta"}}`,
	`{"type":"attachment","attachment":{"type":"hook_success"}}`,
}

type transcriptStub struct {
	srv      *httptest.Server
	store    *transcript.Store
	viewer   transcript.Viewer
	mu       sync.Mutex
	requests []string
}

func newTranscriptStub(t *testing.T) *transcriptStub {
	t.Helper()
	a := transcripttest.Open(t)
	transcripttest.Append(t, a, transcripttest.Session{Root: transcriptStubSession, Owner: "user:usr_clireader"}, "main",
		transcriptStubRecords)
	s := &transcriptStub{store: transcript.NewStore(a, t.TempDir()),
		viewer: transcript.Viewer{Principal: control.Principal{Kind: control.KindUser, ID: "usr_clireader"}}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r.URL.RequestURI())
		s.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+stubToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"error":"unauthorized"}`+"\n")
			return
		}
		route, ok := strings.CutPrefix(r.URL.Path, transcriptwire.ClientPathPrefix)
		if !ok || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		code, body, _ := s.store.Answer(s.viewer, route, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *transcriptStub) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.requests) }

// direct is what the listener answers for `route` with `q`, asked without the client.
func (s *transcriptStub) direct(t *testing.T, route string, q url.Values) string {
	t.Helper()
	code, body, err := s.store.Answer(s.viewer, route, q)
	if code != http.StatusOK || err != nil {
		t.Fatalf("the control: %s answered %d (%v)", route, code, err)
	}
	return string(body)
}

func transcriptHost(t *testing.T) (*transcriptStub, memoHost) {
	t.Helper()
	stub := newTranscriptStub(t)
	h := newMemoHost(t)
	h.configure(t, "", uiLines(stub.srv.URL)...)
	return stub, h
}

// TestTheTranscriptVerbPrintsTheListenersAnswerVerbatim — each action prints exactly the body the
// listener answers, on stdout and nothing else there, at exit 0, with its flags sent as the read
// API's parameters. RED before S8: `transcript` was an unknown subcommand (exit 2).
func TestTheTranscriptVerbPrintsTheListenersAnswerVerbatim(t *testing.T) {
	stub, _ := transcriptHost(t)
	for _, tc := range []struct {
		argv  []string
		route string
		q     url.Values
	}{
		{[]string{"transcript", "skeleton", "--session", transcriptStubSession}, transcriptwire.RouteSkeleton, url.Values{}},
		{[]string{"transcript", "records", "--session", transcriptStubSession, "--view", "raw", "--from-seq", "2", "--max-bytes", "4096"},
			transcriptwire.RouteRecords, url.Values{"view": {"raw"}, "from_seq": {"2"}, "max_bytes": {"4096"}}},
		{[]string{"transcript", "records", "--session", transcriptStubSession, "--tool", "Bash", "--class", "tool-result"},
			transcriptwire.RouteRecords, url.Values{"tool": {"Bash"}, "class": {"tool-result"}}},
		{[]string{"transcript", "tool", "--session", transcriptStubSession, "--seq", "2", "--id", "toolu_c1", "--range", "0-5"},
			transcriptwire.RouteTool, url.Values{"seq": {"2"}, "id": {"toolu_c1"}, "range": {"0-5"}}},
	} {
		tc.q.Set(transcriptwire.ParamSession, transcriptStubSession)
		code, out, stderr := runCLI(t, tc.argv...)
		if want := stub.direct(t, tc.route, tc.q); code != ExitOK || out != want || stderr != "" {
			t.Errorf("%v: exit %d\nstdout %q\nwant   %q\nstderr %q", tc.argv, code, out, want, stderr)
		}
	}
	if code, out, _ := runCLI(t, "transcript", "records", "--session", transcriptStubSession); code != ExitOK ||
		strings.Contains(out, `"toolUseResult":{`) || !strings.Contains(out, `"class":"duplicate"`) {
		t.Errorf("the default view is not the conversation view (exit %d): %s", code, out)
	}
}

// TestTheTranscriptVerbsExitsAreTheReadBucket — no new exit code: a miss is 3 ("nothing was read"),
// the listener's 400 is 2 (the command line must change), a refused credential and an unconfigured
// instance are 3; and every usage refusal the client can decide is 2 with NO request made.
func TestTheTranscriptVerbsExitsAreTheReadBucket(t *testing.T) {
	stub, h := transcriptHost(t)
	for name, tc := range map[string]struct {
		argv     []string
		code     int
		reaches  bool
		inStderr string
	}{
		"a session not stored":            {[]string{"transcript", "skeleton", "--session", "s-cli-99"}, ExitUnreachableNoCache, true, "not-found"},
		"a record with no call":           {[]string{"transcript", "tool", "--session", transcriptStubSession, "--seq", "1"}, ExitUnreachableNoCache, true, "not-found"},
		"a view the listener refuses":     {[]string{"transcript", "records", "--session", transcriptStubSession, "--view", "rawr"}, ExitUsage, true, "view"},
		"no action":                       {[]string{"transcript", "--session", transcriptStubSession}, ExitUsage, false, ""},
		"an unknown action":               {[]string{"transcript", "summary", "--session", transcriptStubSession}, ExitUsage, false, "one action"},
		"no session":                      {[]string{"transcript", "skeleton"}, ExitUsage, false, "--session"},
		"a session that is no id":         {[]string{"transcript", "skeleton", "--session", "../x"}, ExitUsage, false, "not a session id"},
		"a flag the action does not take": {[]string{"transcript", "skeleton", "--session", transcriptStubSession, "--seq", "2"}, ExitUsage, false, "does not take --seq"},
		"--id on records":                 {[]string{"transcript", "records", "--session", transcriptStubSession, "--id", "x"}, ExitUsage, false, "does not take --id"},
		"tool without --seq":              {[]string{"transcript", "tool", "--session", transcriptStubSession}, ExitUsage, false, "requires --seq"},
	} {
		before := stub.count()
		code, out, stderr := runCLI(t, tc.argv...)
		reached := stub.count() > before
		if code != tc.code || reached != tc.reaches || out != "" || !strings.Contains(stderr, tc.inStderr) {
			t.Errorf("%s: exit %d (want %d), reached the listener %v (want %v), stdout %q, stderr %q",
				name, code, tc.code, reached, tc.reaches, out, stderr)
		}
	}
	h.configure(t, "", EnvUIURL+"="+stub.srv.URL, EnvUIToken+"=not-the-token")
	if code, _, stderr := runCLI(t, "transcript", "skeleton", "--session", transcriptStubSession); code != ExitUnreachableNoCache ||
		!strings.Contains(stderr, "HTTP 401") {
		t.Errorf("a refused credential: exit %d %q", code, stderr)
	}
	h.configure(t, "")
	if code, _, stderr := runCLI(t, "transcript", "skeleton", "--session", transcriptStubSession); code != ExitUnreachableNoCache ||
		!strings.Contains(stderr, EnvUIURL) {
		t.Errorf("an unconfigured instance: exit %d %q", code, stderr)
	}
}

// TestATranscriptReadRecordsStarOnlyWhenServed — a served read records `*` (its class); a miss and a
// usage refusal record nothing, because nothing was printed. RED with the record moved before the
// request.
func TestATranscriptReadRecordsStarOnlyWhenServed(t *testing.T) {
	_, h := transcriptHost(t)
	const session = "ses-transcript-ledger"
	t.Setenv(ClaudeSessionEnv, session)
	t.Setenv(OpencodeSessionEnv, "")
	if code, _, _ := runCLI(t, "transcript", "skeleton", "--session", "s-cli-99"); code != ExitUnreachableNoCache {
		t.Fatalf("the miss: exit %d", code)
	}
	if got := readLedgerFile(t, ledgerPath(h.state, session)); len(got) != 0 {
		t.Errorf("a miss recorded %+v", got)
	}
	if code, _, _ := runCLI(t, "transcript", "skeleton", "--session", transcriptStubSession); code != ExitOK {
		t.Fatalf("the served read: exit %d", code)
	}
	got := readLedgerFile(t, ledgerPath(h.state, session))
	if len(got) != 1 || got[0].Scope != LedgerAllScopes || got[0].Verb != "transcript" {
		t.Errorf("a served read recorded %+v, want one `*` record", got)
	}
}
