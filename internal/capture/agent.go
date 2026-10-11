package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ZacxDev/cairn/internal/client"
	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/transcript/scopeuse"
)

// Agent is one capture run's configuration.
type Agent struct {
	ClaudeRoot   string
	OpencodeDirs []string
	Runner       OpencodeRunner
	LedgerDir    string
	Routing      client.Routing
	Redactor     *redact.Redactor
	Sink         Sink
	State        *State
	Log          io.Writer
}

// Summary counts one run. It carries no content.
type Summary struct {
	Sessions, Shipped, Held, Withdrawn, Records, Blobs, Refused int
}

func (a *Agent) logf(format string, args ...any) {
	if a.Log != nil {
		fmt.Fprintf(a.Log, format+"\n", args...)
	}
}

// Run captures every session once.
func (a *Agent) Run() (Summary, error) {
	var sum Summary
	if a.ClaudeRoot != "" {
		sessions, err := discoverClaude(a.ClaudeRoot)
		if err != nil {
			return sum, err
		}
		for _, cs := range sessions {
			a.isolate(cs.Root, &sum, func() error { return a.runClaude(cs, &sum) })
		}
	}
	for _, dir := range a.OpencodeDirs {
		raw, err := a.Runner.List(dir)
		if err == nil {
			var roots []string
			if roots, err = listRoots(raw); err == nil {
				for _, root := range roots {
					a.isolate(root, &sum, func() error { return a.runOpencode(root, &sum) })
				}
			}
		}
		if err != nil {
			// One unreadable project directory is refused and logged like one session; the rest
			// still run.
			a.logf("refused opencode project %s: %v", dir, err)
			sum.Refused++
		}
	}
	return sum, nil
}

// isolate runs one session's capture and turns its failure into a logged refusal.
//
// 🔴 ONE SESSION'S FAILURE MUST NOT ABORT THE RUN. Returning the first error stopped every later
// session, on every 60-second run, for as long as the one bad session existed — a child export
// that fails, a blob a sink refuses (review round 1). The failed session's watermarks did not
// advance (each advances only after its own write), so the next run retries it.
func (a *Agent) isolate(root string, sum *Summary, run func() error) {
	if err := run(); err != nil {
		a.logf("refused %s: %v", root, err)
		sum.Refused++
	}
}

// safeID is the session-id grammar a ledger file name may be built from: ONE path component,
// tested as a component — `..` itself is refused, `a..b` is an ordinary name.
func safeID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`)
}

func (a *Agent) foldLedger(d *scopeuse.Deriver, id string) {
	if a.LedgerDir == "" || !safeID(id) {
		return
	}
	data, err := os.ReadFile(filepath.Join(a.LedgerDir, id+".jsonl"))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		d.LedgerUnreadable()
	default:
		d.Ledger(data)
	}
}

func union(a, b []string) []string {
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		set[s] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// route applies decision 16 to one session and reports whether to ship, and whether the ship
// must start from offset 0 (the watermarks were just reset by a move).
//
// 🔴 THE DECISION IS MADE OVER `V` AS IT STANDS *INCLUDING* THE RECORDS ABOUT TO SHIP, so a
// record that reads a scope on another instance never reaches the first one: the read moves the
// session BEFORE anything from it ships.
func (a *Agent) route(root string, ss *SessionState, sum *Summary) (ship bool, reset bool, err error) {
	dec := Decide(ss.V, a.Routing)
	if dec.Held {
		if ss.Instance != "" {
			if err := a.Sink.Withdraw(ss.Instance, root); err != nil {
				return false, false, err
			}
			a.logf("withdraw %s from %s: it is held now", root, ss.Instance)
			sum.Withdrawn++
			ss.Instance = ""
		}
		ss.Held, ss.HeldReason = true, dec.Reason
		a.logf("held %s: %s", root, dec.Reason)
		sum.Held++
		return false, false, nil
	}
	if ss.Instance != "" && ss.Instance != dec.Instance {
		if err := a.Sink.Withdraw(ss.Instance, root); err != nil {
			return false, false, err
		}
		a.logf("withdraw %s from %s: re-shipping it whole to %s", root, ss.Instance, dec.Instance)
		sum.Withdrawn++
		ss.reset()
		ss.Instance = dec.Instance
		return true, true, nil
	}
	ss.Instance = dec.Instance
	return true, false, nil
}

type pendingStream struct {
	name, path string
	lines      [][]byte
	starts     []int64
	end        int64
	fp         string
	fpLen      int
}

func splitWithOffsets(lines [][]byte, start int64) []int64 {
	starts := make([]int64, len(lines))
	off := start
	for i, l := range lines {
		starts[i] = off
		off += int64(len(l)) + 1
	}
	return starts
}

func (a *Agent) runClaude(cs claudeSession, sum *Summary) error {
	ss := a.State.session(cs.Root, "claude")
	sum.Sessions++
	if ss.Held {
		return nil
	}
	d := scopeuse.NewDeriver()
	d.Restore(ss.Evidence)

	// DERIVE: read every stream's new complete lines, and every new or changed blob.
	names := make([]string, 0, len(cs.Streams))
	for n := range cs.Streams {
		names = append(names, n)
	}
	sort.Strings(names)
	var pend []pendingStream
	for _, name := range names {
		st := ss.stream(name)
		if st.Refused != "" {
			continue
		}
		lines, end, fp, fpLen, err := readNewLines(cs.Streams[name], st, -1)
		if errors.Is(err, ErrRewritten) {
			st.Refused = "rewritten"
			a.logf("refused %s/%s: %v", cs.Root, name, err)
			sum.Refused++
			continue
		}
		if err != nil {
			return err
		}
		for _, l := range lines {
			feedClaudeLine(d, l)
		}
		pend = append(pend, pendingStream{name: name, path: cs.Streams[name], lines: lines,
			starts: splitWithOffsets(lines, st.Offset), end: end, fp: fp, fpLen: fpLen})
	}
	blobNames := make([]string, 0, len(cs.Blobs))
	for n := range cs.Blobs {
		blobNames = append(blobNames, n)
	}
	sort.Strings(blobNames)
	blobData := map[string][]byte{}
	blobStat := map[string]BlobState{}
	for _, n := range blobNames {
		info, err := os.Stat(cs.Blobs[n])
		if err != nil {
			return err
		}
		prev, seen := ss.Blobs[n]
		if seen && prev.Size == info.Size() && prev.MtimeNs == info.ModTime().UnixNano() {
			continue // unchanged since it shipped: one stat, no read (decision 5)
		}
		data, err := readFile(cs.Blobs[n])
		if err != nil {
			return err
		}
		blobData[n] = data
		blobStat[n] = BlobState{Size: info.Size(), MtimeNs: info.ModTime().UnixNano(), Digest: digest(data)}
		if !redact.IsBinary(data) {
			d.Content(string(data))
		}
	}
	a.foldLedger(d, cs.Root)
	res := d.Result()
	ss.Evidence = d.Evidence()
	ss.V = union(ss.V, res.V)

	ship, reset, err := a.route(cs.Root, ss, sum)
	if err != nil || !ship {
		return err
	}
	if reset {
		// A MOVE re-ships the whole session from offset 0 — but only up to what was DERIVED above,
		// so nothing un-derived can ship.
		for i := range pend {
			st := ss.stream(pend[i].name)
			lines, _, fp, fpLen, err := readNewLines(pend[i].path, st, pend[i].end)
			if err != nil {
				return err
			}
			pend[i].lines, pend[i].starts, pend[i].fp, pend[i].fpLen = lines, splitWithOffsets(lines, 0), fp, fpLen
		}
	}
	shippedAny := false
	for _, p := range pend {
		if len(p.lines) == 0 {
			continue
		}
		recs := make([]Record, 0, len(p.lines))
		for i, l := range p.lines {
			recs = append(recs, a.redactLine(strconv.FormatInt(p.starts[i], 10), l))
		}
		if err := a.Sink.Records(ss.Instance, cs.Root, p.name, recs); err != nil {
			return err
		}
		st := ss.stream(p.name)
		st.Offset, st.Fingerprint, st.FPLen = p.end, p.fp, p.fpLen
		sum.Records += len(recs)
		shippedAny = true
	}
	for _, n := range blobNames {
		data, read := blobData[n]
		if !read {
			if _, shipped := ss.Blobs[n]; shipped {
				continue // unchanged and already on this instance
			}
			// A MOVE reset the blob state: an unchanged blob must re-ship to the new instance.
			info, err := os.Stat(cs.Blobs[n])
			if err != nil {
				return err
			}
			if data, err = readFile(cs.Blobs[n]); err != nil {
				return err
			}
			blobStat[n] = BlobState{Size: info.Size(), MtimeNs: info.ModTime().UnixNano(), Digest: digest(data)}
		}
		if prev, ok := ss.Blobs[n]; ok && prev.Digest == blobStat[n].Digest {
			ss.Blobs[n] = blobStat[n] // touched, not changed
			continue
		}
		out, _ := a.Redactor.Blob(n, data)
		if err := a.Sink.Blob(ss.Instance, cs.Root, n, out); err != nil {
			return err
		}
		ss.Blobs[n] = blobStat[n]
		sum.Blobs++
		shippedAny = true
	}
	if shippedAny {
		sum.Shipped++
	}
	return nil
}

func feedClaudeLine(d *scopeuse.Deriver, line []byte) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	var rec map[string]any
	if err := dec.Decode(&rec); err != nil {
		d.Content(string(line))
		return
	}
	d.ClaudeRecord(rec)
}

// readFile is os.ReadFile, a variable only so a test can COUNT the reads (the stat-first rule).
var readFile = os.ReadFile

func (a *Agent) redactLine(src string, line []byte) Record {
	out, _ := a.Redactor.Record(line)
	return Record{Src: src, Rec: append([]byte(nil), out...)}
}

type ocStream struct {
	name    string
	changed []exportUnit
	all     []exportUnit
}

func (a *Agent) runOpencode(root string, sum *Summary) error {
	ss := a.State.session(root, "opencode")
	sum.Sessions++
	if ss.Held {
		return nil
	}
	// Export the root and, recursively, every child its `task` parts name.
	type job struct{ id, stream string }
	queue := []job{{root, "main"}}
	seen := map[string]bool{root: true}
	var streams []ocStream
	var children []string
	for len(queue) > 0 {
		j := queue[0]
		queue = queue[1:]
		raw, err := a.Runner.Export(j.id)
		if err != nil {
			return err
		}
		units, kids, err := parseExport(raw)
		if errors.Is(err, ErrIncompleteExport) {
			a.logf("refused opencode %s (%s): %d bytes that are not one complete JSON document — nothing shipped",
				root, j.stream, len(raw))
			sum.Refused++
			return nil
		}
		if err != nil {
			return err
		}
		st := ss.stream(j.stream)
		streams = append(streams, ocStream{name: j.stream, changed: diffUnits(units, st.Units), all: units})
		for _, k := range kids {
			if !seen[k] {
				seen[k] = true
				children = append(children, k)
				queue = append(queue, job{k, "child:" + k})
			}
		}
	}
	d := scopeuse.NewDeriver()
	d.Restore(ss.Evidence)
	for _, s := range streams {
		for _, u := range s.changed {
			if u.Part != nil {
				d.OpencodePart(u.Part)
			} else {
				d.Content(string(u.Raw))
			}
		}
	}
	a.foldLedger(d, root)
	for _, c := range children {
		a.foldLedger(d, c)
	}
	res := d.Result()
	ss.Evidence = d.Evidence()
	ss.V = union(ss.V, res.V)

	ship, reset, err := a.route(root, ss, sum)
	if err != nil || !ship {
		return err
	}
	shippedAny := false
	for _, s := range streams {
		todo := s.changed
		if reset {
			todo = s.all
		}
		if len(todo) == 0 {
			continue
		}
		recs := make([]Record, 0, len(todo))
		for _, u := range todo {
			recs = append(recs, a.redactLine(u.Key+"@"+digest(u.Raw)[:12], u.Raw))
		}
		if err := a.Sink.Records(ss.Instance, root, s.name, recs); err != nil {
			return err
		}
		st := ss.stream(s.name)
		for _, u := range todo {
			st.Units[u.Key] = digest(u.Raw)
		}
		sum.Records += len(recs)
		shippedAny = true
	}
	if shippedAny {
		sum.Shipped++
	}
	return nil
}
