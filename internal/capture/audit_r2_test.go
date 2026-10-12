package capture

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Guards for #260's audit round 2.

// TestAnIdleRunSavesNothing (finding 1): the state is saved when a session's state CHANGED, not
// after every session — an idle run over many sessions writes nothing, and a run in which one
// session grew saves a bounded number of times.
func TestAnIdleRunSavesNothing(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w) // every Claude Code session of the world
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &bytes.Buffer{})
	saves := 0
	a.Checkpoint = func() error { saves++; return nil }
	mustRun(t, a)
	if saves == 0 {
		t.Fatal("control: the first run saved nothing")
	}
	saves = 0
	if sum := mustRun(t, a); saves != 0 || sum.Sessions < 8 {
		t.Fatalf("an idle run over %d sessions saved the state %d times, want 0", sum.Sessions, saves)
	}
	plain := w.Sessions["cc-plain"]
	appendTo(t, filepath.Join(e.claude, plain.Project, plain.ID+".jsonl"), `{"type":"user","message":{"role":"user","content":"more"}}`)
	saves = 0
	if sum := mustRun(t, a); saves != 1 || sum.Records != 1 {
		t.Fatalf("a run in which ONE session grew saved %d times (%+v), want exactly 1", saves, sum)
	}
	// A backlog: with the saves spaced out, ten changed sessions cost the first save and the one
	// when the run ends — not ten.
	b := newAgent(t, materialize(t, w), oneInstance, &fakeRunner{}, &bytes.Buffer{})
	saves = 0
	b.Checkpoint, b.CheckpointEvery = func() error { saves++; return nil }, time.Hour
	if sum := mustRun(t, b); saves != 2 || sum.Shipped < 8 {
		t.Fatalf("a first run over %d sessions saved %d times, want 2 (the first change, and the end)", sum.Sessions, saves)
	}
}

// bumpExport returns the export with its session `info` changed — a unit ALREADY sent, mutated.
func bumpExport(t *testing.T, raw []byte, n int) []byte {
	t.Helper()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var info map[string]json.RawMessage
	if err := json.Unmarshal(doc["info"], &info); err != nil {
		t.Fatal(err)
	}
	info["title"] = json.RawMessage(fmt.Sprintf(`"synthetic title %d"`, n))
	doc["info"], _ = json.Marshal(info)
	out, _ := json.Marshal(doc)
	return out
}

// TestALostAckSurvivesManyFailedRuns (finding 2): the pod stores a write whose answer is lost and
// is then unreachable for N runs, the export changing before each. The one plan the pod can hold
// is never evicted and no new write is planned over it, so the first run that reaches the pod
// resumes. N is measured at the old eviction bound (16) and past it (30).
func TestALostAckSurvivesManyFailedRuns(t *testing.T) {
	for _, n := range []int{16, 30} {
		w := loadWorld(t)
		e := materialize(t, w, "oc-root")
		root := w.Sessions["oc-root"].ID
		p := newPod(t, podOpts{})
		p.lose.Store(1)
		runner := &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList}
		var log bytes.Buffer
		a, _ := podAgent(t, e, p, 0, &log)
		a.Runner, a.ClaudeRoot, a.OpencodeDirs = runner, "", []string{"/work/alpha"}
		mustRun(t, a)
		stored, _ := p.arc.ReadStream(root, "main")
		if len(stored) == 0 {
			t.Fatalf("N=%d: the lost write was not stored", n)
		}
		p.down.Store(true)
		for i := 0; i < n; i++ {
			runner.exports = map[string][]byte{}
			for id, raw := range exportsOf(w, "after") {
				runner.exports[id] = raw
			}
			runner.exports[root] = bumpExport(t, runner.exports[root], i)
			if sum := mustRun(t, a); sum.Refused == 0 {
				t.Fatalf("N=%d: failed run %d refused nothing", n, i)
			}
		}
		if plans := a.State.Sessions[root].Streams["main"].Pending; len(plans) == 0 {
			t.Fatalf("N=%d: the plan the pod may hold was dropped", n)
		}
		p.down.Store(false)
		if sum := mustRun(t, a); sum.Refused != 0 || sum.Records == 0 {
			t.Fatalf("N=%d: the run after recovery: %+v — the stream is wedged\n%s", n, sum, lastLines(log.String(), 3))
		}
		if sum := mustRun(t, a); sum.Refused != 0 || sum.Records != 0 {
			t.Fatalf("N=%d: the run after that is not idle: %+v", n, sum)
		}
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// TestARefusedRecordsBytesAreNotResentByALaterRun (finding 3): the pod's re-check refuses a
// record (422); the placeholder request's answer is LOST. The refusal was SAVED before that
// request left, so the next run sends the placeholder at once — 0 requests of that run carry the
// refused bytes. The same for a blob.
func TestARefusedRecordsBytesAreNotResentByALaterRun(t *testing.T) {
	redactionOff = true // the agent sends what the pod's table matches: a real re-check refusal
	defer func() { redactionOff = false }()
	for _, kind := range []string{"record", "blob"} {
		secret := liveToken()
		root := t.TempDir()
		proj := filepath.Join(root, "projects", "-work-alpha")
		if err := os.MkdirAll(filepath.Join(proj, "s-0002", "tool-results"), 0o700); err != nil {
			t.Fatal(err)
		}
		line, blob := "export COLOUR=green", "COLOUR=green"
		if kind == "record" {
			line = "export TOKEN=" + secret
		} else {
			blob = "TOKEN=" + secret
		}
		lines := `{"type":"user","message":{"role":"user","content":"first"}}` + "\n" +
			`{"type":"user","message":{"role":"user","content":"` + line + `"}}` + "\n" +
			`{"type":"user","message":{"role":"user","content":"last"}}` + "\n"
		if kind == "blob" { // a blob travels base64: that is what a request body carries of it
			secret = base64.StdEncoding.EncodeToString([]byte(blob + "\n"))
		}
		_ = os.WriteFile(filepath.Join(proj, "s-0002.jsonl"), []byte(lines), 0o600)
		_ = os.WriteFile(filepath.Join(proj, "s-0002", "tool-results", "toolu_0009.txt"), []byte(blob+"\n"), 0o600)
		e := env{root: root, claude: filepath.Join(root, "projects"), ledger: filepath.Join(root, "ledger")}

		p := newPod(t, podOpts{})
		var log, sent bytes.Buffer
		a, s := podAgent(t, e, p, 0, &log)
		s.Client = &http.Client{Transport: recordingTransport{&sent}}
		// The request count at the FIRST save that holds the refusal: it must be the 422's own
		// request — saved before the placeholder request, which is the next one.
		var savedAt int64
		a.Checkpoint = func() error {
			ss := a.State.Sessions["s-0002"]
			if savedAt == 0 && ss != nil && (len(ss.Streams["main"].Rechecked) > 0 || ss.Blobs["tool-results/toolu_0009.txt"].Rechecked != "") {
				savedAt = p.requests.Load()
			}
			return nil
		}
		p.loseAfter422.Store(true) // the placeholder request: stored, its answer lost
		if sum := mustRun(t, a); sum.Refused == 0 {
			t.Fatalf("%s: run 1 refused nothing — the lost answer did not reach the agent\n%s", kind, log.String())
		}
		if n := strings.Count(sent.String(), secret); n != 1 {
			t.Fatalf("%s: run 1 sent the refused bytes %d times, want once", kind, n)
		}
		if savedAt == 0 || savedAt != p.at422.Load() {
			t.Fatalf("%s: the refusal was first saved at request %d; the 422 was request %d — it must be saved before the placeholder leaves",
				kind, savedAt, p.at422.Load())
		}
		sent.Reset()
		if sum := mustRun(t, a); sum.Refused != 0 {
			t.Fatalf("%s: run 2: %+v\n%s", kind, sum, log.String())
		}
		if strings.Contains(sent.String(), secret) {
			t.Fatalf("%s: run 2 sent the refused bytes again", kind)
		}
		got, _ := p.arc.ReadStream("s-0002", "main")
		b, _ := p.arc.ReadBlob("s-0002", "toolu_0009.txt")
		if len(got) != 3 || bytes.Contains(got[1].Rec, []byte("refused-by-recheck")) != (kind == "record") ||
			bytes.HasPrefix(b, []byte("cairn-capture: this file was omitted")) != (kind == "blob") {
			t.Fatalf("%s: stored %d records; blob %q", kind, len(got), b)
		}
		if st := a.State.Sessions["s-0002"].Streams["main"]; len(st.Rechecked) != 0 {
			t.Fatalf("%s: a stored record's refusal is still in the state: %v", kind, st.Rechecked)
		}
	}
}

// TestAStoppedInstancesSessionsAreNotOpened (finding 4a): the stop check is BEFORE the read — the
// sessions skipped for a stopped instance have none of their files opened.
func TestAStoppedInstancesSessionsAreNotOpened(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-rich", "cc-plain", "cc-hook-recall")
	p := newPod(t, podOpts{disarmed: true})
	a, _ := podAgent(t, e, p, 0, &bytes.Buffer{})
	var opened []string
	openStream = func(path string) (*os.File, error) { opened = append(opened, path); return os.Open(path) }
	defer func() { openStream = os.Open }()
	sum := mustRun(t, a)
	probe := opened
	if sum.Skipped != 2 || len(probe) == 0 {
		t.Fatalf("control: %+v, %d files opened by the probing session", sum, len(probe))
	}
	for _, path := range opened {
		if !strings.Contains(path, strings.TrimSuffix(filepath.Base(probe[0]), ".jsonl")) {
			t.Fatalf("a skipped session's file was opened: %s", path)
		}
	}
	// The next run: the instance's stop is per run, so one session probes again and the rest stay shut.
	opened = nil
	a.Sink = p.sink("host-a", sinkTokenA, 0)
	mustRun(t, a)
	for _, path := range opened {
		if !strings.Contains(path, strings.TrimSuffix(filepath.Base(probe[0]), ".jsonl")) {
			t.Fatalf("run 2 opened a skipped session's file: %s", path)
		}
	}
}

// TestASessionRoutedToAStoppedInstanceIsNotSent (finding 4a, the second check): with two
// instances a new session's instance is known only after it is read; it is then skipped before
// anything is redacted or sent — one refusal for the run, not one per session.
func TestASessionRoutedToAStoppedInstanceIsNotSent(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-rich", "cc-plain")
	p := newPod(t, podOpts{disarmed: true})
	var log bytes.Buffer
	a, s := podAgent(t, e, p, 0, &log)
	a.Routing = twoInstances
	sum := mustRun(t, a)
	if s.Requests != 1 || sum.Refused != 1 || sum.Skipped != 1 || strings.Count(log.String(), "capture-disarmed") != 1 {
		t.Fatalf("%d requests, %+v\n%s", s.Requests, sum, log.String())
	}
}

// TestAnOpencodeSessionBoundForAStoppedInstanceIsNotExported (finding 4a, opencode): once the
// instance is stopped, an opencode session bound for it is skipped before `opencode export` runs.
func TestAnOpencodeSessionBoundForAStoppedInstanceIsNotExported(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	p := newPod(t, podOpts{disarmed: true})
	runner := &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList}
	a, _ := podAgent(t, e, p, 0, &bytes.Buffer{})
	a.Runner, a.OpencodeDirs = runner, []string{"/work/alpha"}
	if sum := mustRun(t, a); runner.calls != 0 || sum.Skipped != 1 {
		t.Fatalf("%d exports ran for a stopped instance (%+v)", runner.calls, sum)
	}
}

// TestTheAcknowledgedPrefixIsCommitted (finding 4b): request 1 of a write is answered 200 and the
// pod then fails. The state holds exactly the request-1 prefix, and the next run sends only the
// remainder — no 409, no resume. Claude Code and opencode.
func TestTheAcknowledgedPrefixIsCommitted(t *testing.T) {
	w := loadWorld(t)
	// Claude Code.
	e := materialize(t, w, "cc-plain")
	plain := w.Sessions["cc-plain"]
	path := filepath.Join(e.claude, plain.Project, plain.ID+".jsonl")
	for i := 0; i < 40; i++ {
		appendTo(t, path, fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"line %d %s"}}`, i, strings.Repeat("lorem ", 60)))
	}
	p := newPod(t, podOpts{})
	p.failFrom.Store(2)
	var log bytes.Buffer
	a, _ := podAgent(t, e, p, 4<<10, &log)
	if sum := mustRun(t, a); sum.Refused != 1 || sum.Records == 0 {
		t.Fatalf("claude run 1: %+v — want a refusal AND the acknowledged prefix counted", sum)
	}
	stored, _ := p.arc.ReadStream(plain.ID, "main")
	meta, _ := p.arc.ReadMeta(plain.ID)
	st := a.State.Sessions[plain.ID].Streams["main"]
	if len(stored) == 0 || offsetCursor(st.Offset) != meta.Streams["main"].StoredTo {
		t.Fatalf("claude: the state is at %d, the pod at %q (%d records)", st.Offset, meta.Streams["main"].StoredTo, len(stored))
	}
	p.failFrom.Store(0)
	if sum := mustRun(t, a); sum.Refused != 0 || strings.Contains(log.String(), "resume ") {
		t.Fatalf("claude run 2: %+v — the remainder was not sent cleanly\n%s", sum, log.String())
	}
	if got, _ := p.arc.ReadStream(plain.ID, "main"); strings.Join(srcs(got), ",") != strings.Join(lineOffsets(t, path), ",") {
		t.Fatal("claude: not every line is stored exactly once")
	}

	// opencode.
	e = materialize(t, w, "oc-root")
	root := w.Sessions["oc-root"].ID
	p = newPod(t, podOpts{})
	p.failFrom.Store(2)
	log.Reset()
	a, _ = podAgent(t, e, p, 2<<10, &log)
	a.Runner, a.ClaudeRoot, a.OpencodeDirs = &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList}, "", []string{"/work/alpha"}
	if sum := mustRun(t, a); sum.Refused != 1 || sum.Records == 0 {
		t.Fatalf("opencode run 1: %+v", sum)
	}
	meta, _ = p.arc.ReadMeta(root)
	ost := a.State.Sessions[root].Streams["main"]
	stored, _ = p.arc.ReadStream(root, "main")
	if ost.Cursor == "" || ost.Cursor != meta.Streams["main"].StoredTo || len(ost.Units) != len(stored) {
		t.Fatalf("opencode: state cursor %q with %d units, pod at %q with %d records", ost.Cursor, len(ost.Units),
			meta.Streams["main"].StoredTo, len(stored))
	}
	p.failFrom.Store(0)
	if sum := mustRun(t, a); sum.Refused != 0 || strings.Contains(log.String(), "resume ") {
		t.Fatalf("opencode run 2: %+v\n%s", sum, log.String())
	}
}

// TestStaleStateTempFilesAreCleaned: a save that was killed leaves `.state-*` beside the state.
func TestStaleStateTempFilesAreCleaned(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	_ = os.WriteFile(filepath.Join(dir, ".state-123"), []byte("{"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "host.key"), []byte("k"), 0o600)
	CleanStateTemps(path)
	if _, err := os.Stat(filepath.Join(dir, ".state-123")); !os.IsNotExist(err) {
		t.Fatal("the stale temp file is still there")
	}
	if _, err := os.Stat(filepath.Join(dir, "host.key")); err != nil {
		t.Fatal("a file that is not a state temp was removed")
	}
}

// TestPositionAsksAndStoresNothing: the probe that settles a pending write names where a stream
// is — "" for one never written — and leaves the instance exactly as it was.
func TestPositionAsksAndStoresNothing(t *testing.T) {
	p := newPod(t, podOpts{})
	s := p.sink("host-a", sinkTokenA, 0)
	if at, err := s.Position(up(""), "main"); err != nil || at != "" {
		t.Fatalf("a stream never written: %q, %v", at, err)
	}
	if meta, _ := p.arc.ReadMeta(sinkRoot); meta != nil {
		t.Fatal("a probe of a session never uploaded created it")
	}
	recs := synth(3, 50)
	if _, err := s.Records(up(""), "main", recs); err != nil {
		t.Fatal(err)
	}
	for _, from := range []string{"", recs[2].End} { // whatever the caller believes, the probe stores nothing
		if at, err := s.Position(up(from), "main"); err != nil || at != recs[2].End {
			t.Fatalf("a stream stored to %q: probe from %q answered %q, %v", recs[2].End, from, at, err)
		}
	}
	if got := p.stored("main"); len(got) != 3 {
		t.Fatalf("a probe stored something: %d records", len(got))
	}
}
