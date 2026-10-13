package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/transcript/archive"
)

// #273's audit, round 1, in the agent: a session deleted on its instance is never sent again (F2), a
// withdrawal reaches an instance that is not taking uploads (F3), and no proxy from the environment
// ever carries the capture token (F4). Synthetic throughout.

// TestASessionDeletedOnItsInstanceIsNeverSentAgain — F2, the agent half. The instance deleted the
// session's transcript (its owner pressed delete); the session keeps growing on the host. The next
// run's upload is answered 410, which is FINAL: the session is marked, said once, and no later run
// reads it or sends a request for it. RED before the fix: the stream was refused every run (a 409 it
// could not resume from), and — the broken promise — a stream or blob sent from the start was
// ACCEPTED, re-creating the session on the instance.
func TestASessionDeletedOnItsInstanceIsNeverSentAgain(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	s := w.Sessions["cc-plain"]
	path := filepath.Join(e.claude, s.Project, s.ID+".jsonl")
	p := newPod(t, podOpts{})
	var log bytes.Buffer
	a, _ := podAgent(t, e, p, 0, &log)
	mustRun(t, a)
	if m, _ := p.arc.ReadMeta(s.ID); m == nil {
		t.Fatal("control: run 1 stored nothing")
	}
	if ok, err := p.arc.Delete(s.ID, archive.ReasonOwnerDelete, nil); !ok || err != nil {
		t.Fatalf("the owner's delete: %v %v", ok, err)
	}
	// The session goes on: a new record, and a persisted tool result that did not exist before.
	appendTo(t, path, `{"type":"user","message":{"role":"user","content":"after the delete"}}`)
	results := filepath.Join(e.claude, s.Project, s.ID, "tool-results")
	if err := os.MkdirAll(results, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(results, "toolu_late01.txt"), []byte("synthetic output\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := mustRun(t, a)
	ss := a.State.Sessions[s.ID]
	if sum.Deleted != 1 || sum.Refused != 0 || ss.Deleted != "personal" {
		t.Fatalf("the run after the delete: %+v, marked %q — want deleted=1, refused=0, marked for personal\n%s", sum, ss.Deleted, log.String())
	}
	if m, _ := p.arc.ReadMeta(s.ID); m != nil {
		t.Fatal("the agent's next run re-created the deleted session on the instance")
	}
	if n := strings.Count(log.String(), "DELETED "+s.ID); n != 1 {
		t.Fatalf("the deletion was said %d times, want once:\n%s", n, log.String())
	}
	before := p.requests.Load()
	appendTo(t, path, `{"type":"user","message":{"role":"user","content":"and again"}}`)
	if sum := mustRun(t, a); p.requests.Load() != before || sum.Refused != 0 || sum.Deleted != 0 {
		t.Fatalf("a later run acted on a session deleted for good: %+v, %d request(s)", sum, p.requests.Load()-before)
	}
	if n := strings.Count(log.String(), "DELETED "+s.ID); n != 1 {
		t.Fatalf("the deletion was logged again on a later run (%d)", n)
	}
}

// stoppedInstance is an instance's worker listener that takes one run's uploads and then stops
// taking them: every upload is answered `status` (503, or 507), while the withdrawal route answers
// as the real one does.
type stoppedInstance struct {
	srv       *httptest.Server
	status    atomic.Int64 // 0: uploads are stored; else the status every upload gets
	withdraw  atomic.Int64 // the status the withdrawal route answers (200 unless set)
	withdrawn atomic.Int64
}

func newStoppedInstance(t *testing.T) *stoppedInstance {
	t.Helper()
	in := &stoppedInstance{}
	in.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/withdraw") {
			in.withdrawn.Add(1)
			if code := in.withdraw.Load(); code != 0 {
				rw.WriteHeader(int(code))
				return
			}
			_, _ = io.WriteString(rw, `{"deleted":true}`+"\n")
			return
		}
		switch in.status.Load() {
		case http.StatusServiceUnavailable:
			rw.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(rw, `{"refused":"capture-disarmed"}`+"\n")
		case http.StatusInsufficientStorage:
			rw.WriteHeader(http.StatusInsufficientStorage)
			_, _ = io.WriteString(rw, `{"refused":"quota","quota_bytes":1}`+"\n")
		default:
			var body struct {
				To string `json:"to"`
			}
			_ = json.Unmarshal(raw, &body)
			to, _ := json.Marshal(body.To)
			_, _ = io.WriteString(rw, `{"stored_to":`+string(to)+`,"seq_to":1}`+"\n")
		}
	}))
	t.Cleanup(in.srv.Close)
	return in
}

// TestAWithdrawalReachesAnInstanceThatIsNotTakingUploads — F3. An instance that answers 503
// (disarmed) or 507 (quota full) is stopped for UPLOADS for the rest of the run — and a session
// bound for it used to be skipped before it was even read, so a read that changed its routing answer
// was never derived and its shipped prefix never withdrawn, for as long as the instance stayed
// stopped. RED before the fix: `skipped=1 withdrawn=0`, and the instance saw no withdrawal. Now a
// session WITH a shipped prefix is derived and routed, and the withdrawal — which stores nothing —
// goes to the stopped instance. A withdrawal that gets no answer is counted `withdrawal-pending` and
// asked again; a session with nothing shipped is still skipped unread.
func TestAWithdrawalReachesAnInstanceThatIsNotTakingUploads(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusInsufficientStorage} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			w := loadWorld(t)
			e := materialize(t, w, "cc-plain")
			s := w.Sessions["cc-plain"]
			path := filepath.Join(e.claude, s.Project, s.ID+".jsonl")
			appendTo(t, path, strings.Replace(betaRead, "beta-notes", "alpha-notes", 1))
			in := newStoppedInstance(t)
			sinkFor := func() *HTTPSink {
				return &HTTPSink{Host: "host-a", Log: &bytes.Buffer{}, Endpoints: map[string]Endpoint{
					"personal": {URL: in.srv.URL, Token: sinkTokenA}, "client": {URL: in.srv.URL, Token: sinkTokenA}}}
			}
			var log bytes.Buffer
			a := newAgent(t, e, twoInstances, &fakeRunner{}, &log)
			a.Sink = sinkFor()
			if sum := mustRun(t, a); sum.Shipped != 1 || a.State.Sessions[s.ID].Instance != "personal" {
				t.Fatalf("control: run 1 = %+v, bound to %q", sum, a.State.Sessions[s.ID].Instance)
			}
			// The instance stops taking uploads, and the session reads the OTHER instance's scope.
			in.status.Store(int64(status))
			appendTo(t, path, betaRead)
			stopped := func() *HTTPSink { // a sink this run has already seen the instance refuse through
				sink := sinkFor()
				if _, err := sink.Records(Upload{Instance: "personal", Root: "s-probe", Runtime: "claude"}, "main",
					[]Record{{Src: "0", Rec: []byte(`{}`), End: "1"}}); err == nil || sink.Stopped("personal") == nil {
					t.Fatalf("control: the instance did not stop the sink (%v)", err)
				}
				return sink
			}
			// First the withdrawal gets NO answer: pending, the session stays bound, nothing held.
			in.withdraw.Store(http.StatusBadGateway)
			a.Sink = stopped()
			sum, err := a.Run()
			if err != nil || sum.WithdrawalPending != 1 || sum.Withdrawn != 0 || sum.Skipped != 0 || a.State.Sessions[s.ID].Instance != "personal" {
				t.Fatalf("a withdrawal with no answer: %+v (%v), bound to %q — want withdrawal-pending=1, still bound", sum, err,
					a.State.Sessions[s.ID].Instance)
			}
			if !strings.Contains(sum.String(), " withdrawal-pending=1 ") {
				t.Fatalf("the summary line does not say so: %s", sum)
			}
			// Then it is answered.
			in.withdraw.Store(0)
			before := in.withdrawn.Load()
			a.Sink = stopped()
			sum = mustRun(t, a)
			if sum.Withdrawn != 1 || sum.Held != 1 || sum.Skipped != 0 || sum.WithdrawalPending != 0 || in.withdrawn.Load() != before+1 {
				t.Fatalf("the stopped instance: %+v, %d withdrawal(s) reached it — want withdrawn=1 held=1 skipped=0\n%s",
					sum, in.withdrawn.Load()-before, log.String())
			}
			// CONTROL: a session with NOTHING shipped, bound for a stopped instance, is skipped unread.
			e2 := materialize(t, w, "cc-plain")
			b := newAgent(t, e2, oneInstance, &fakeRunner{}, &bytes.Buffer{})
			b.Sink = stopped()
			if sum := mustRun(t, b); sum.Skipped != 1 || sum.Sessions != 1 {
				t.Fatalf("an unshipped session on a stopped instance: %+v — want skipped=1", sum)
			}
		})
	}
}

// TestNoEnvironmentProxyEverCarriesTheCaptureToken — F4. With a plain-`http` worker URL (which
// `-allow-plaintext-host` admits), Go's default transport sends the WHOLE request to `HTTP_PROXY` in
// clear: the token and the transcript bytes, to whatever a stray variable names. The sink's own
// client uses no environment proxy.
//
// Go reads the proxy variables ONCE per process, so the measurement runs in a child: this test
// binary re-executed with `HTTP_PROXY` naming a recording listener. The POSITIVE CONTROL is in the
// same child — a default-transport client asked for the same URL DOES go to the proxy — so a zero
// for the sink is not "the proxy was never live". RED before the fix: the proxy received the sink's
// requests, bearer token and all.
func TestNoEnvironmentProxyEverCarriesTheCaptureToken(t *testing.T) {
	const worker = "http://mesh-host.example.invalid:8443"
	if os.Getenv("CAIRN_TEST_PROXY_CHILD") == "1" {
		// THE CHILD. First the control, through Go's default transport…
		if resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(worker + "/control"); err == nil {
			resp.Body.Close()
		}
		// …then the sink: one withdrawal and one upload.
		sink := &HTTPSink{Host: "host-a", Endpoints: map[string]Endpoint{"personal": {URL: worker, Token: sinkTokenA}}}
		_, _ = sink.Withdraw("personal", "s-0001")
		_, _ = sink.Records(Upload{Instance: "personal", Root: "s-0001", Runtime: "claude"}, "main",
			[]Record{{Src: "0", Rec: []byte(`{}`), End: "1"}})
		return
	}
	var mu sync.Mutex
	var seen []string
	proxy := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+" auth="+fmt.Sprint(r.Header.Get("Authorization") != ""))
		mu.Unlock()
		rw.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestNoEnvironmentProxyEverCarriesTheCaptureToken$", "-test.count=1")
	cmd.Env = append(os.Environ(), "CAIRN_TEST_PROXY_CHILD=1", "HTTP_PROXY="+proxy.URL, "http_proxy="+proxy.URL, "NO_PROXY=", "no_proxy=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the child failed: %v\n%s", err, out)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 || seen[0] != "GET /control auth=false" {
		t.Fatalf("POSITIVE CONTROL: the proxy never saw the default-transport request, so its zero would mean nothing: %v", seen)
	}
	if len(seen) != 1 {
		t.Fatalf("the environment's proxy received the capture sink's request(s): %v", seen[1:])
	}
}
