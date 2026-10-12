package capture

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Guards for #260's audit round 1.

// TestAnOpencodeStreamSurvivesALostAckWhenUnitsChange (finding 1): the pod stores a request whose
// answer is lost; before the next run opencode MUTATES units already sent. The chain cursor of
// what the pod holds can no longer be recomputed from the export — it is matched against the
// plan persisted BEFORE the request was sent, and the stream moves on. Later runs are idle.
func TestAnOpencodeStreamSurvivesALostAckWhenUnitsChange(t *testing.T) {
	matched := 0
	defer func() {
		if !t.Failed() && matched == 0 {
			t.Fatal("no case was resumed from a persisted plan — the mutation between runs did not reach a sent unit")
		}
	}()
	for _, lost := range []int64{1, 2} {
		w := loadWorld(t)
		e := materialize(t, w, "oc-root")
		root := w.Sessions["oc-root"].ID
		p := newPod(t, podOpts{})
		p.lose.Store(lost)
		runner := &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList}
		var log bytes.Buffer
		a, s := podAgent(t, e, p, 2<<10, &log) // a small request bound: the stream is several requests
		a.Runner, a.ClaudeRoot, a.OpencodeDirs = runner, "", []string{"/work/alpha"}
		if sum := mustRun(t, a); sum.Refused == 0 {
			t.Fatalf("lost %d: run 1 refused nothing — the lost answer did not reach the agent", lost)
		}
		storedAfterLoss, _ := p.arc.ReadStream(root, "main")
		if len(storedAfterLoss) == 0 {
			t.Fatalf("lost %d: nothing was stored before the loss", lost)
		}
		runner.exports = exportsOf(w, "after")
		for run := 2; run <= 3; run++ {
			if sum := mustRun(t, a); sum.Refused != 0 {
				t.Fatalf("lost %d: run %d refused %d — the stream is wedged\n%s", lost, run, sum.Refused, log.String())
			}
		}
		// Every unit of the CURRENT export is the last stored version of its key, and no version
		// was stored twice.
		got, err := p.arc.ReadStream(root, "main")
		if err != nil {
			t.Fatal(err)
		}
		last, seen := map[string]string{}, map[string]bool{}
		for _, r := range got {
			if seen[r.Src] {
				t.Fatalf("lost %d: unit version %s was stored twice", lost, r.Src)
			}
			seen[r.Src] = true
			key, _, _ := strings.Cut(r.Src, "@")
			last[key] = r.Src
		}
		units, _, _ := parseExport(w.Opencode.Exports["after"][root])
		for _, u := range units {
			if want := u.Key + "@" + digest(u.Raw)[:12]; last[u.Key] != want {
				t.Fatalf("lost %d: unit %s is stored as %q, want %q", lost, u.Key, last[u.Key], want)
			}
		}
		before := s.Requests
		if sum := mustRun(t, a); sum.Records != 0 || s.Requests != before {
			t.Fatalf("lost %d: an idle run sent %d requests", lost, s.Requests-before)
		}
		if strings.Contains(log.String(), "matched against a sent, unacknowledged write") {
			matched++
		}
		if st := a.State.Sessions[root].Streams["main"]; len(st.Pending) != 0 {
			t.Fatalf("lost %d: a plan of %d units is still pending after the stream settled", lost, len(st.Pending))
		}
	}
}

// TestTheSinkNeverFollowsARedirect (finding 2): a 3xx is an error; the token and the body go
// nowhere but the configured URL.
func TestTheSinkNeverFollowsARedirect(t *testing.T) {
	var elsewhere atomic.Int64
	other := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"stored_to":"x","seq_to":1}`))
	}))
	defer other.Close()
	front := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.Redirect(rw, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer front.Close()
	s := &HTTPSink{Host: "host-a", Endpoints: map[string]Endpoint{"personal": {URL: front.URL, Token: sinkTokenA}}}
	if _, err := s.Records(up(""), "main", synth(1, 50)); err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("a redirect answered %v, want an error naming the 307", err)
	}
	if elsewhere.Load() != 0 {
		t.Fatal("the request — token and body — followed the redirect")
	}
}

// TestALineThatIsJSONNullShips (finding 5): `null` is a JSON value the wire reads as "no rec", so
// it ships wrapped, bytes exact, instead of refusing its stream 400 on every run.
func TestALineThatIsJSONNullShips(t *testing.T) {
	p := newPod(t, podOpts{})
	recs := []Record{{Src: "0", Rec: []byte("null"), End: "5"}, {Src: "5", Rec: []byte(" null "), End: "12"}}
	if _, err := p.sink("host-a", sinkTokenA, 0).Records(up(""), "main", recs); err != nil {
		t.Fatal(err)
	}
	got := p.stored("main")
	if len(got) != 2 || string(got[0].Rec) != `{"cairn_capture":"not-json","parts":["null"]}` ||
		string(got[1].Rec) != `{"cairn_capture":"not-json","parts":[" null "]}` {
		t.Fatalf("stored %d records: %v", len(got), srcs(got))
	}
}

// TestATokenFileMayBeASymlinkToAPrivateFile (finding 7): secret managers expose a secret as a
// symlink; the regular-file and mode checks apply to the TARGET.
func TestATokenFileMayBeASymlinkToAPrivateFile(t *testing.T) {
	dir := t.TempDir()
	private, open := filepath.Join(dir, "private"), filepath.Join(dir, "open")
	_ = os.WriteFile(private, []byte(sinkTokenA+"\n"), 0o600)
	_ = os.WriteFile(open, []byte(sinkTokenA+"\n"), 0o640)
	_ = os.Symlink(private, filepath.Join(dir, "to-private"))
	_ = os.Symlink(open, filepath.Join(dir, "to-open"))
	_ = os.Symlink(dir, filepath.Join(dir, "to-dir"))
	if tok, err := ReadTokenFile(filepath.Join(dir, "to-private")); err != nil || tok != sinkTokenA {
		t.Fatalf("a symlink to a 0600 file: %v", err)
	}
	for _, name := range []string{"to-open", "to-dir"} {
		if _, err := ReadTokenFile(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// TestARefusedTokenSurfacesBehindAnEarlierFailure (finding 4): a 401 on a LATER stream of a
// session whose earlier stream failed for another reason still ends the run loudly.
func TestARefusedTokenSurfacesBehindAnEarlierFailure(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-rich")
	e.sink.fail = map[string]error{
		"main":                      errors.New("a 500, say"),
		"subagent:ba0c6809460e5295": &FatalError{Instance: "personal", Status: 401},
	}
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &bytes.Buffer{})
	_, err := a.Run()
	var fatal *FatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("Run returned %v; the 401 was masked by the earlier failure", err)
	}
}

// TestABlobTheRecheckRefusesByNameIsHeldAndNotResent (finding 6): a 422 naming a request FIELD
// cannot be answered with a placeholder — the name itself matched — so that blob version is held,
// the hold survives into the state, and the next run does not send it again.
func TestABlobTheRecheckRefusesByNameIsHeldAndNotResent(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-rich")
	const blob = "tool-results/toolu_HG0xYsNZHTy9e4GaPzWkVuJ4.txt"
	e.sink.fail = map[string]error{"blob:" + blob: &RecheckRefusal{Record: -1, Field: "blob name"}}
	var log bytes.Buffer
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &log)
	sum := mustRun(t, a)
	id := w.Sessions["cc-rich"].ID
	if st := a.State.Sessions[id].Blobs[blob]; st.Refused == "" || sum.Refused != 1 {
		t.Fatalf("the refused blob is not held in the state: %+v (%+v)", st, sum)
	}
	first := e.sink.attempts["blob:"+blob]
	if sum := mustRun(t, a); sum.Refused != 0 || e.sink.attempts["blob:"+blob] != first {
		t.Fatalf("run 2 resent the held blob (%d → %d attempts)", first, e.sink.attempts["blob:"+blob])
	}
	if _, ok := e.sink.blobs["personal/"+id+"/"+blob]; ok {
		t.Fatal("the held blob was stored")
	}
}

// TestAStoppedInstanceIsNotReReadEveryRun (finding 3): once an instance is not taking uploads the
// sessions bound for it are skipped BEFORE they are read and redacted — one refusal line, not one
// per session.
func TestAStoppedInstanceIsNotReReadEveryRun(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-rich", "cc-plain", "cc-hook-recall")
	p := newPod(t, podOpts{disarmed: true})
	var log bytes.Buffer
	a, s := podAgent(t, e, p, 0, &log)
	sum := mustRun(t, a)
	if s.Requests != 1 || sum.Refused != 1 || sum.Skipped != 2 {
		t.Fatalf("%d requests, %+v — want ONE probing request, one refusal and two sessions skipped unread\n%s",
			s.Requests, sum, log.String())
	}
	if n := strings.Count(log.String(), "capture-disarmed"); n != 1 {
		t.Fatalf("%d log lines name the stop, want one per run:\n%s", n, log.String())
	}
}

// TestAKilledRunKeepsItsPlan (finding 1, the kill): the process dies after a request left and
// before anything it learned was saved. The state on disk is the one checkpointed BEFORE the
// request — and it already holds the write's plan, so the next process is not wedged by a mutated
// unit either.
func TestAKilledRunKeepsItsPlan(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "oc-root")
	root := w.Sessions["oc-root"].ID
	p := newPod(t, podOpts{})
	runner := &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList}
	var log bytes.Buffer
	a, _ := podAgent(t, e, p, 0, &log)
	a.Runner, a.ClaudeRoot, a.OpencodeDirs = runner, "", []string{"/work/alpha"}
	path := filepath.Join(t.TempDir(), "state.json")
	var onDisk [][]byte // the state file after each checkpoint
	a.Checkpoint = func() error {
		if err := a.State.Save(path); err != nil {
			return err
		}
		raw, _ := os.ReadFile(path)
		onDisk = append(onDisk, raw)
		return nil
	}
	mustRun(t, a) // the pod stores everything
	if len(onDisk) < 2 {
		t.Fatalf("%d checkpoints: the plan was not saved before the request left", len(onDisk))
	}
	// THE KILL: what survives is the checkpoint taken before the request, not the run's end.
	if err := os.WriteFile(path, onDisk[0], 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Sessions[root].Streams["main"]; got.Cursor != "" || len(got.Pending) == 0 {
		t.Fatalf("the surviving state: cursor %q, a pending plan of %d units — want no cursor and the plan", got.Cursor, len(got.Pending))
	}
	next, _ := podAgent(t, e, p, 0, &log)
	next.State = st
	next.Runner, next.ClaudeRoot, next.OpencodeDirs = &fakeRunner{exports: exportsOf(w, "after"), list: w.Opencode.SessionList}, "", []string{"/work/alpha"}
	if sum := mustRun(t, next); sum.Refused != 0 || sum.Records == 0 {
		t.Fatalf("the run after the kill: %+v\n%s", sum, log.String())
	}
}

// TestAResetKeepsABlobsRefusalAsItKeepsAStreams: a move to another instance re-ships everything
// EXCEPT what was refused for good — a stream's refusal and a blob's alike.
func TestAResetKeepsABlobsRefusalAsItKeepsAStreams(t *testing.T) {
	ss := &SessionState{
		Streams: map[string]*StreamState{"main": {Offset: 9, Refused: "rewritten", Pending: []PendingUnit{{Key: "k"}}}},
		Blobs: map[string]BlobState{"held.txt": {Size: 1, Digest: "d", Cursor: "b1", Refused: "refused"},
			"shipped.txt": {Size: 2, Digest: "e", Cursor: "b2"}},
	}
	ss.reset()
	if st := ss.Streams["main"]; st.Refused == "" || st.Offset != 0 || st.Pending != nil {
		t.Fatalf("stream after reset: %+v", st)
	}
	if b, ok := ss.Blobs["held.txt"]; !ok || b.Refused == "" || b.Cursor != "" || len(ss.Blobs) != 1 {
		t.Fatalf("blobs after reset: %+v", ss.Blobs)
	}
}
