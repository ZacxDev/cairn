package capture

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The AGENT through the real pod half: the cursors it keeps are the ones the pod's compare-and-
// swap accepts, and what it ships lands once.

func podAgent(t *testing.T, e env, p *pod, maxRequest int, log *bytes.Buffer) (*Agent, *HTTPSink) {
	t.Helper()
	a := newAgent(t, e, oneInstance, &fakeRunner{}, log)
	s := p.sink("host-a", sinkTokenA, maxRequest)
	s.Log = log
	a.Sink = s
	return a, s
}

// lineOffsets is every complete line's start offset in a file, as decimal strings.
func lineOffsets(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	off := 0
	for _, l := range bytes.SplitAfter(data, []byte("\n")) {
		if len(l) == 0 || l[len(l)-1] != '\n' {
			break
		}
		out = append(out, strconv.Itoa(off))
		off += len(l)
	}
	return out
}

// assertWorldStoredOnce reads every stream and blob of the rich session back from the archive.
func assertWorldStoredOnce(t *testing.T, w world, e env, p *pod) {
	t.Helper()
	rich := w.Sessions["cc-rich"]
	sdir := filepath.Join(e.claude, rich.Project, rich.ID)
	streams := map[string]string{"main": filepath.Join(e.claude, rich.Project, rich.ID+".jsonl")}
	for _, sub := range []string{"ba0c6809460e5295", "df83d3cb21cb55e1"} {
		streams["subagent:"+sub] = filepath.Join(sdir, "subagents", "agent-"+sub+".jsonl")
	}
	for name, path := range streams {
		got, err := p.arc.ReadStream(rich.ID, name)
		if err != nil {
			t.Fatal(err)
		}
		want := lineOffsets(t, path)
		if strings.Join(srcs(got), ",") != strings.Join(want, ",") || len(want) == 0 {
			t.Fatalf("%s: stored srcs %v, want each of %d lines exactly once", name, srcs(got), len(want))
		}
	}
	blobs := 0
	_ = filepath.WalkDir(sdir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		rel, _ := filepath.Rel(sdir, path)
		stored, err := p.arc.ReadBlob(rich.ID, WireBlobName(filepath.ToSlash(rel)))
		if err != nil {
			t.Fatalf("blob %s was not stored: %v", rel, err)
		}
		if src, _ := os.ReadFile(path); (strings.HasSuffix(rel, ".jpg") || strings.HasSuffix(rel, ".pdf")) && !bytes.Equal(src, stored) {
			t.Fatalf("binary blob %s is not byte-identical", rel)
		}
		blobs++
		return nil
	})
	if blobs < 5 {
		t.Fatalf("only %d blobs were checked", blobs)
	}
}

// TestTheAgentShipsThroughThePodOnce: one run ships every stream and blob of a rich session —
// subagents, text and binary tool results, subagent metadata — and the second run sends NOTHING.
func TestTheAgentShipsThroughThePodOnce(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-rich", "cc-plain")
	p := newPod(t, podOpts{})
	var log bytes.Buffer
	a, s := podAgent(t, e, p, 0, &log)
	sum := mustRun(t, a)
	if sum.Refused != 0 || sum.Shipped != 2 {
		t.Fatalf("run 1: %+v\n%s", sum, log.String())
	}
	assertWorldStoredOnce(t, w, e, p)
	before := s.Requests
	if sum := mustRun(t, a); sum.Records != 0 || sum.Blobs != 0 || s.Requests != before {
		t.Fatalf("run 2 sent %d requests (%+v); an idle session must send none", s.Requests-before, sum)
	}
}

// TestALostAcknowledgementIsResumedNotDuplicated is clause (a) through the agent: the pod stores
// a request whose answer never arrives; the run refuses the session and advances nothing; the next
// run starts from the old cursor, the pod answers 409 naming where it is, and every record and
// blob lands EXACTLY once.
func TestALostAcknowledgementIsResumedNotDuplicated(t *testing.T) {
	for _, lost := range []int64{2, 5} { // a records request in the middle; a later one
		w := loadWorld(t)
		e := materialize(t, w, "cc-rich")
		p := newPod(t, podOpts{})
		p.lose.Store(lost)
		var log bytes.Buffer
		a, _ := podAgent(t, e, p, 8<<10, &log)
		if sum := mustRun(t, a); sum.Refused == 0 {
			t.Fatalf("lost answer %d: run 1 refused nothing — the lost acknowledgement did not reach the agent", lost)
		}
		if sum := mustRun(t, a); sum.Refused != 0 {
			t.Fatalf("lost answer %d: run 2 %+v\n%s", lost, sum, log.String())
		}
		if !strings.Contains(log.String(), "resume ") {
			t.Fatalf("lost answer %d: no resume was logged — the case did not exercise the 409\n%s", lost, log.String())
		}
		assertWorldStoredOnce(t, w, e, p)
	}
}

// TestOpencodeCursorsChainThroughThePod: two runs over an opencode session whose parts changed in
// between; the second run's `from` is the chain cursor the pod stored, so it is accepted.
func TestOpencodeCursorsChainThroughThePod(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "oc-root")
	root := w.Sessions["oc-root"].ID
	p := newPod(t, podOpts{})
	runner := &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList}
	var log bytes.Buffer
	a, _ := podAgent(t, e, p, 0, &log)
	a.Runner, a.ClaudeRoot, a.OpencodeDirs = runner, "", []string{"/work/alpha"}
	first := mustRun(t, a)
	runner.exports = exportsOf(w, "after")
	second := mustRun(t, a)
	if first.Refused+second.Refused != 0 || second.Records == 0 {
		t.Fatalf("runs: %+v, %+v\n%s", first, second, log.String())
	}
	got, err := p.arc.ReadStream(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	units, _, _ := parseExport(w.Opencode.Exports["before"][root])
	if len(got) <= len(units) {
		t.Fatalf("stored %d records; the second run's upserts did not land", len(got))
	}
}

// TestARecheckRefusalHoldsTheStreamAndTheRestShips: the pod's 422 holds ONE stream for good — the
// session's other streams still ship, the refusal is logged, and the next run sends nothing more of
// the held stream.
func TestARecheckRefusalHoldsTheStreamAndTheRestShips(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-rich")
	e.sink.recheck = "subagent:ba0c6809460e5295"
	var log bytes.Buffer
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &log)
	sum := mustRun(t, a)
	id := w.Sessions["cc-rich"].ID
	if sum.Refused != 1 || len(spooled(t, e, "personal", id, "main")) == 0 ||
		len(spooled(t, e, "personal", id, "subagent:df83d3cb21cb55e1")) == 0 {
		t.Fatalf("run 1: %+v — want the one refusal and the other streams shipped", sum)
	}
	st := a.State.Sessions[id].Streams["subagent:ba0c6809460e5295"]
	if st.Refused == "" || st.Offset != 0 || !strings.Contains(log.String(), "HOLD "+id+"/subagent:ba0c6809460e5295") {
		t.Fatalf("the refused stream is not held: %+v\n%s", st, log.String())
	}
	e.sink.recheck = "" // even a pod that would now accept it gets nothing: the hold is final
	if sum := mustRun(t, a); sum.Records != 0 || sum.Refused != 0 {
		t.Fatalf("run 2 resent the held stream: %+v", sum)
	}
}
