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
	"syscall"
	"time"

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

	// Checkpoint, when set, saves the state. It is called AT ONCE where a crash must not lose a
	// decision — before an opencode write leaves (its plan), before a re-check placeholder is sent
	// — and after a session whose state CHANGED, at most once per CheckpointEvery, with one last
	// save when the run ends with unsaved changes.
	//
	// 🔴 NOT AFTER EVERY SESSION. That was built first: an idle run over 861 sessions rewrote and
	// fsynced the whole state 861 times (measured: 4.7 s and 196 MB per run, every minute).
	Checkpoint      func() error
	CheckpointEvery time.Duration

	// fatal is the first error that ends the run's uploads loudly (a refused token).
	fatal error
	// dirty is whether the state holds a change no checkpoint has saved; saved is when it last was.
	dirty bool
	saved time.Time
	// sawDeleted is set when a write of the session being captured was answered 410 ([ErrDeleted]),
	// whichever of its failures the session reports; [Agent.isolate] turns it into the session's
	// final mark.
	sawDeleted bool
}

// checkpoint saves the state now.
func (a *Agent) checkpoint() error {
	if a.Checkpoint == nil {
		return nil
	}
	if err := a.Checkpoint(); err != nil {
		return err
	}
	a.dirty, a.saved = false, time.Now()
	return nil
}

// sessionImage is one session's state as it would be saved, for telling whether a run changed it.
func (a *Agent) sessionImage(root string) []byte {
	raw, _ := json.Marshal(a.State.Sessions[root])
	return raw
}

// stopped reports whether the instance a session is bound for is not taking uploads in this run.
// A session with no instance yet is bound for the sole instance when there is only one.
func (a *Agent) stopped(instance string) bool {
	s, ok := a.Sink.(interface{ Stopped(string) error })
	if !ok {
		return false
	}
	if instance == "" && !a.Routing.MultiInstance() && len(a.Routing.Instances) == 1 {
		instance = a.Routing.Instances[0].Alias
	}
	return instance != "" && s.Stopped(instance) != nil
}

// skipUnread reports whether a session may be skipped WITHOUT reading it: its instance is not taking
// uploads in this run AND nothing of the session is there.
//
// 🔴 A SESSION WITH A SHIPPED PREFIX IS STILL DERIVED AND ROUTED WHILE ITS INSTANCE IS STOPPED
// (#273's audit, F3). Skipping it — as every bound session was — froze its `V` for as long as the
// instance answered 503 or 507, so a read that moved its routing answer was never seen and the
// prefix was never withdrawn; and a quota that is FULL is a long-lived stop. Reading it costs only
// its new bytes (the watermarks hold); what is still never done for a stopped instance is the
// backlog nobody shipped — the measured megabytes — and any upload.
func (a *Agent) skipUnread(ss *SessionState) bool {
	return a.stopped(ss.Instance) && !ss.shipped()
}

// Summary counts one run. It carries no content.
type Summary struct {
	Sessions, Shipped, Held, Withdrawn, Records, Blobs, Refused int
	// Skipped counts sessions not read at all because their instance is not taking uploads.
	Skipped int
	// WithdrawalPending counts sessions whose shipped prefix must be withdrawn and whose withdrawal
	// got no answer in this run; each is asked again on the next. Deleted counts sessions an instance
	// answered 410 for in this run — their transcript was deleted there, and they are never sent again.
	WithdrawalPending, Deleted int
}

// String is the run's one summary line.
func (s Summary) String() string {
	return fmt.Sprintf("sessions=%d shipped=%d held=%d withdrawn=%d records=%d blobs=%d refused=%d skipped=%d withdrawal-pending=%d deleted=%d",
		s.Sessions, s.Shipped, s.Held, s.Withdrawn, s.Records, s.Blobs, s.Refused, s.Skipped, s.WithdrawalPending, s.Deleted)
}

func (a *Agent) logf(format string, args ...any) {
	if a.Log != nil {
		fmt.Fprintf(a.Log, format+"\n", args...)
	}
}

// Run captures every session once. It returns an error only for what ends the run's uploads — a
// session tree that cannot be listed, or an instance that refused the capture token
// ([FatalError]); every other failure is one session's, logged and counted in Refused.
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
	if a.dirty {
		if err := a.checkpoint(); err != nil && a.fatal == nil {
			a.fatal = fmt.Errorf("capture: the state could not be saved: %w", err)
		}
	}
	return sum, a.fatal
}

// isolate runs one session's capture and turns its failure into a logged refusal.
//
// 🔴 ONE SESSION'S FAILURE MUST NOT ABORT THE RUN. Returning the first error stopped every later
// session, on every 60-second run, for as long as the one bad session existed — a child export
// that fails, a blob a sink refuses (review round 1). The failed session's watermarks did not
// advance (each advances only after its own write), so the next run retries it.
func (a *Agent) isolate(root string, sum *Summary, run func() error) {
	before := a.sessionImage(root)
	a.sawDeleted = false
	err := run()
	switch ss := a.State.Sessions[root]; {
	case a.sawDeleted && ss != nil:
		// 🔴 410 IS FINAL, AND SAID ONCE (#273's audit, F2). The instance deleted this session's
		// transcript; the agent must not retry it every minute, and — what the deletion's owner was
		// promised — must not send it anything that would start the session again there. Saved at
		// once, so a kill cannot make the next run ask again.
		ss.Deleted = ss.Instance
		a.logf("DELETED %s: instance %s answered that this session's transcript was deleted there; nothing more of it "+
			"is read or sent", root, ss.Instance)
		sum.Deleted++
		if cerr := a.checkpoint(); cerr != nil && a.fatal == nil {
			a.fatal = fmt.Errorf("capture: the state could not be saved: %w", cerr)
		}
	case err != nil:
		a.logf("refused %s: %v", root, err)
		sum.Refused++
	}
	if !bytes.Equal(before, a.sessionImage(root)) {
		a.dirty = true
	}
	if a.dirty && time.Since(a.saved) >= a.CheckpointEvery {
		if err := a.checkpoint(); err != nil && a.fatal == nil {
			a.fatal = fmt.Errorf("capture: the state could not be saved: %w", err)
		}
	}
}

// rechecks is the half of an [Upload] that carries the pod's re-check refusals for one stream:
// which of recs it already refused (same `src`, same bytes), and the hook that records a new one
// in the state and SAVES it before the placeholder is sent.
func (a *Agent) rechecks(up *Upload, st *StreamState, recs []Record) {
	byDigest := map[string]string{}
	up.Rechecked = map[string]bool{}
	for _, r := range recs {
		byDigest[r.Src] = digest(r.Rec)
		if st.Rechecked[r.Src] == byDigest[r.Src] {
			up.Rechecked[r.Src] = true
		}
	}
	up.OnRecheck = func(src string) error {
		if st.Rechecked == nil {
			st.Rechecked = map[string]string{}
		}
		st.Rechecked[src] = byDigest[src]
		return a.checkpoint()
	}
}

// settled forgets the re-check refusals of records that are now stored (as placeholders).
func settled(st *StreamState, stored []Record) {
	for _, r := range stored {
		delete(st.Rechecked, r.Src)
	}
	if len(st.Rechecked) == 0 {
		st.Rechecked = nil
	}
}

// held is every failed write's first stop. A re-check refusal the sink could not answer with a
// placeholder — a 422 naming a request FIELD, where the name or cursor itself matched — HOLDS that
// stream or blob version for good: recorded in `mark`, logged, never resent. Any other error is
// returned for the session to report, and the next run retries it.
//
// 🔴 AND A REFUSED TOKEN IS RECORDED HERE, WHEREVER IT ARRIVES. A session reports its FIRST
// failure; a 401 on a later stream behind an ordinary error was masked and the run exited 0.
func (a *Agent) held(err error, mark *string, what string, sum *Summary) error {
	var fatal *FatalError
	if errors.As(err, &fatal) && a.fatal == nil {
		a.fatal = err
	}
	if errors.Is(err, ErrDeleted) {
		a.sawDeleted = true
	}
	var rc *RecheckRefusal
	if !errors.As(err, &rc) {
		return err
	}
	*mark = rc.Error()
	a.logf("HOLD %s: %v — held on this host for good and never resent", what, rc)
	sum.Refused++
	return nil
}

// offsetCursor is a Claude Code stream's cursor: its byte offset, "" for nothing written.
func offsetCursor(off int64) string {
	if off == 0 {
		return ""
	}
	return strconv.FormatInt(off, 10)
}

// chainCursor is an opencode stream's cursor after one more unit: a hash chain over every unit
// key and the digest of its REDACTED bytes, so it names a position and carries no content.
func chainCursor(prev, key string, rec []byte) string {
	return "c" + digest([]byte(prev + "\x00" + key + "\x00" + digest(rec)))[:32]
}

// blobCursor is a blob version's cursor: the digest of its REDACTED bytes.
func blobCursor(out []byte) string { return "b" + digest(out)[:32] }

// safeID is the session-id grammar a ledger file name may be built from: ONE path component,
// tested as a component — `..` itself is refused, `a..b` is an ordinary name.
func safeID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`)
}

// readLedger reads one ledger file the way the client writes it: a REGULAR file, never followed
// through a symlink (`O_NOFOLLOW`), opened `O_NONBLOCK` so a FIFO planted at the path cannot hang the
// run (#266's audit). Absent is [os.ErrNotExist] — zero records; anything else that is not a regular
// file is an error, which both callers turn into `*` (fail closed: a link or a pipe where the ledger
// goes is a ledger nobody can vouch for, not an empty one).
func readLedger(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return io.ReadAll(f)
}

func (a *Agent) foldLedger(d *scopeuse.Deriver, id string) {
	if a.LedgerDir == "" || !safeID(id) {
		return
	}
	data, err := readLedger(filepath.Join(a.LedgerDir, id+".jsonl"))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		d.LedgerUnreadable()
	default:
		d.Ledger(data)
	}
}

// LedgerStream is the stream a session's client read ledger ships as — the pod's
// `archive.LedgerStream`, spelled here because this package must not import the pod's store.
// `TestTheLedgerStreamNameIsThePods` pins the two equal.
const LedgerStream = "ledger"

// ledgerUnits is the client read ledger of `ids` — the root first, then each child id (decision 7:
// an opencode child's reads attribute to its ROOT, and this is where its id is mapped to it) — as
// units keyed `<id>@<byte offset>`, so an append-only file ships only its new lines.
//
// 🔴 ONLY COMPLETE LINES. A client appends one line per write; a line with no newline yet is a
// write in flight and waits for the next run, or it would ship half a record.
//
// 🔴 FAIL CLOSED, NEVER SILENT: a ledger that EXISTS and cannot be read ships one record naming `*`
// (the routing fold's `LedgerUnreadable`), and a line that is not one JSON value ships as a record
// naming `*` with its bytes DROPPED — the pod parses the ledger itself, and a record it could not
// store would stall the stream, while one it stores as `*` makes the session owner-only.
func (a *Agent) ledgerUnits(ids []string) []exportUnit {
	var out []exportUnit
	for _, id := range ids {
		if a.LedgerDir == "" || !safeID(id) {
			continue
		}
		data, err := readLedger(filepath.Join(a.LedgerDir, id+".jsonl"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			out = append(out, exportUnit{Key: id + "@unreadable", Raw: json.RawMessage(ledgerStar("unreadable"))})
			continue
		}
		for off := 0; off < len(data); {
			nl := bytes.IndexByte(data[off:], '\n')
			if nl < 0 {
				break
			}
			line := bytes.TrimSpace(data[off : off+nl])
			key := id + "@" + strconv.Itoa(off)
			off += nl + 1
			if len(line) == 0 {
				continue
			}
			if !json.Valid(line) {
				line = ledgerStar("unparsed")
			}
			out = append(out, exportUnit{Key: key, Raw: append(json.RawMessage(nil), line...)})
		}
	}
	return out
}

// ledgerStar is the record that stands for a ledger line or file the agent could not read.
func ledgerStar(why string) []byte {
	return []byte(`{"schema":1,"scope":"*","cairn_capture":"` + why + `"}`)
}

// shipLedger uploads the session's client read ledger as its [LedgerStream] (decision 3a), through
// the same plan-saved, resumable path as an opencode stream ([Agent.shipUnits]).
//
// 🔴 BEFORE THE TRANSCRIPT STREAMS: the pod's F1 asks "is the ledger EMPTY", so a transcript that
// names `cairn` uploaded ahead of its ledger is owner-only until the ledger lands.
func (a *Agent) shipLedger(up Upload, ids []string, ss *SessionState, sum *Summary) (bool, error) {
	units := a.ledgerUnits(ids)
	if len(units) == 0 {
		return false, nil
	}
	st := ss.stream(LedgerStream)
	if st.Refused != "" {
		return false, nil
	}
	n, err := a.shipUnits(up, ocStream{name: LedgerStream, all: units}, st)
	if n > 0 {
		sum.Records += n
	}
	if err != nil {
		err = a.held(err, &st.Refused, up.Root+"/"+LedgerStream, sum)
	}
	return n > 0, err
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
//
// 🔴 WHEN THE ANSWER IS NO LONGER THE INSTANCE HOLDING THE PREFIX, THE PREFIX IS WITHDRAWN FIRST
// (S6, clause m) — [Agent.withdraw] — and nothing is shipped anywhere until that instance has
// answered that the prefix is gone.
//
// 🔴 WHERE A SESSION IS BOUND IS SAVED BEFORE ITS FIRST UPLOAD LEAVES, NOT UP TO TWO SECONDS AFTER
// (S3b's audit, closed in S6). `Instance` used to be saved with the session's other changes, at
// most every [Agent.CheckpointEvery]: a run killed inside that window had shipped a prefix under a
// state that still said "nowhere", so when the routing answer later changed there was no instance
// to withdraw from and the prefix stayed where the rule says it must not be. Every change of
// `Instance` or `Held` made here is checkpointed AT ONCE, and a save that fails sends nothing.
func (a *Agent) route(root string, ss *SessionState, sum *Summary) (ship bool, reset bool, err error) {
	dec := Decide(ss.V, a.Routing)
	if ss.Instance != "" && (dec.Held || ss.Instance != dec.Instance) {
		gone, err := a.withdraw(root, ss, sum)
		if err != nil || !gone {
			return false, false, err
		}
		reset = !dec.Held
	}
	if dec.Held {
		ss.Held, ss.HeldReason = true, dec.Reason
		a.logf("held %s: %s", root, dec.Reason)
		sum.Held++
		return false, false, a.saveRouting(root)
	}
	if ss.Instance != dec.Instance {
		ss.Instance = dec.Instance
		if err := a.saveRouting(root); err != nil {
			return false, false, err
		}
	}
	return true, reset, nil
}

// saveRouting checkpoints a routing decision at once. A decision that cannot be saved is not acted
// on: the session's error for this run, and nothing of it is sent.
func (a *Agent) saveRouting(root string) error {
	if err := a.checkpoint(); err != nil {
		return fmt.Errorf("capture: where %s ships could not be saved, so nothing of it is sent: %w", root, err)
	}
	return nil
}

// withdraw asks the instance holding a session's prefix to forget it (decision 16), and reports
// whether the prefix is GONE from there — in which case the session's watermarks are reset and it
// is bound nowhere, saved at once.
//
//   - `{"deleted": true}`: the instance held it for this host's (owner, host) and deleted it — its
//     transcript, plugin outputs and ticket edges — or had already (a repeat after a lost
//     acknowledgement). Gone.
//   - `{"deleted": false}` when this agent has an ACKNOWLEDGED upload of the session on record: the
//     instance holds no such root for this (owner, host) — the token was reissued under another
//     host label, say. FINAL: the session is HELD on this host, loudly, and is never shipped
//     anywhere else — the prefix's presence elsewhere is surfaced rather than duplicated.
//   - `{"deleted": false}` when NO upload of it was ever acknowledged: the session was bound for the
//     instance and nothing landed (the instance was not taking uploads), so there is nothing to
//     withdraw. Gone. ⚠ That is read off this agent's own watermarks, which a kill can leave
//     unsaved — but then the instance DOES hold the prefix and answers `true`, so the two cases
//     cannot be confused in the direction that matters.
//   - an error (no answer): not done. The session stays bound where it was and the next run asks
//     again; a refused token is recorded as the run's fatal error.
func (a *Agent) withdraw(root string, ss *SessionState, sum *Summary) (gone bool, err error) {
	from := ss.Instance
	deleted, err := a.Sink.Withdraw(from, root)
	if err != nil {
		var fatal *FatalError
		if errors.As(err, &fatal) && a.fatal == nil {
			a.fatal = err
		}
		sum.WithdrawalPending++
		return false, fmt.Errorf("the prefix on %s could not be withdrawn (withdrawal pending — asked again next run), so "+
			"nothing more of this session is sent anywhere: %w", from, err)
	}
	if !deleted && ss.shipped() {
		ss.Held, ss.HeldReason = true, "prefix not withdrawn: not held by this owner and host on "+from
		a.logf("HOLD %s: PREFIX NOT WITHDRAWN: not held by this owner and host on instance %s — the session is held on "+
			"this host for good and is not shipped anywhere else; what %s holds of it must be removed there by hand",
			root, from, from)
		sum.Held++
		return false, a.saveRouting(root)
	}
	if deleted {
		a.logf("withdraw %s from %s: its routing answer changed", root, from)
		sum.Withdrawn++
	}
	ss.reset()
	ss.Instance = ""
	return true, a.saveRouting(root)
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
	if ss.Held || ss.Deleted != "" {
		return nil
	}
	// 🔴 BEFORE ANYTHING IS READ: an instance that is not taking uploads would have this session
	// read, derived and redacted on every run for nothing (measured at megabytes a second).
	if a.skipUnread(ss) {
		sum.Skipped++
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
	ss.Declared = union(ss.Declared, res.Declare)

	ship, reset, err := a.route(cs.Root, ss, sum)
	if err != nil || !ship {
		return err
	}
	if a.stopped(ss.Instance) {
		sum.Skipped++
		return nil
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
	// 🔴 ONE STREAM'S FAILURE DOES NOT STOP THE SESSION'S OTHER STREAMS: each advances on its own
	// acknowledgement, and the first error is the session's answer for this run.
	var failed error
	fail := func(err error) {
		if err != nil && failed == nil {
			failed = err
		}
	}
	up := Upload{Instance: ss.Instance, Root: cs.Root, Runtime: "claude", Declared: ss.Declared}
	shippedAny, err := a.shipLedger(up, []string{cs.Root}, ss, sum)
	fail(err)
	for _, p := range pend {
		if len(p.lines) == 0 {
			continue
		}
		st := ss.stream(p.name)
		recs := make([]Record, 0, len(p.lines))
		for i, l := range p.lines {
			r := a.redactLine(strconv.FormatInt(p.starts[i], 10), l)
			r.End = strconv.FormatInt(p.starts[i]+int64(len(l))+1, 10)
			recs = append(recs, r)
		}
		up.From = offsetCursor(st.Offset)
		a.rechecks(&up, st, recs)
		n, err := a.Sink.Records(up, p.name, recs)
		settled(st, recs[:n])
		// 🔴 THE ACKNOWLEDGED PREFIX IS COMMITTED EVEN WHEN THE WRITE FAILED: each record of it
		// answered 200, and its end is a line boundary.
		if n > 0 {
			st.Offset = p.end
			if n < len(recs) {
				st.Offset = p.starts[n]
			}
			st.Fingerprint, st.FPLen = p.fp, p.fpLen
			sum.Records += n
			shippedAny = true
		}
		if err != nil {
			fail(a.held(err, &st.Refused, cs.Root+"/"+p.name, sum))
		}
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
		prev, seen := ss.Blobs[n]
		next := blobStat[n]
		next.Cursor, next.Refused = prev.Cursor, ""
		if seen && prev.Digest == next.Digest {
			next.Refused = prev.Refused
			ss.Blobs[n] = next // touched, not changed
			continue
		}
		out := a.redactBlob(n, data)
		to := blobCursor(out)
		up.From = prev.Cursor
		up.Rechecked = map[string]bool{"": prev.Rechecked == next.Digest}
		up.OnRecheck = func(string) error {
			// Saved under the PREVIOUS version's size and mtime, so the next run reads the file again
			// and, finding this digest, sends the placeholder at once.
			prev.Rechecked, next.Rechecked = next.Digest, next.Digest
			ss.Blobs[n] = prev
			return a.checkpoint()
		}
		if prev.Rechecked == next.Digest {
			next.Rechecked = prev.Rechecked
		}
		if err := a.Sink.Blob(up, n, to, out); err != nil {
			if err = a.held(err, &next.Refused, cs.Root+" blob "+n, sum); err != nil {
				fail(err)
				continue
			}
			ss.Blobs[n] = next // held: the stored version is still prev.Cursor
			continue
		}
		next.Cursor = to
		ss.Blobs[n] = next
		sum.Blobs++
		shippedAny = true
	}
	if shippedAny {
		sum.Shipped++
	}
	return failed
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
	if redactionOff {
		return Record{Src: src, Rec: append([]byte(nil), line...)}
	}
	out, _ := a.Redactor.Record(line)
	return Record{Src: src, Rec: append([]byte(nil), out...)}
}

func (a *Agent) redactBlob(name string, data []byte) []byte {
	if redactionOff {
		return data
	}
	out, _ := a.Redactor.Blob(name, data)
	return out
}

type ocStream struct {
	name    string
	changed []exportUnit
	all     []exportUnit
}

func (a *Agent) runOpencode(root string, sum *Summary) error {
	ss := a.State.session(root, "opencode")
	sum.Sessions++
	if ss.Held || ss.Deleted != "" {
		return nil
	}
	if a.skipUnread(ss) {
		sum.Skipped++
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
	ss.Declared = union(ss.Declared, res.Declare)

	ship, _, err := a.route(root, ss, sum)
	if err != nil || !ship {
		return err
	}
	if a.stopped(ss.Instance) {
		sum.Skipped++
		return nil
	}
	var failed error
	up := Upload{Instance: ss.Instance, Root: root, Runtime: "opencode", Declared: ss.Declared}
	shippedAny, failed := a.shipLedger(up, append([]string{root}, children...), ss, sum)
	for _, s := range streams {
		st := ss.stream(s.name)
		if st.Refused != "" {
			continue
		}
		n, err := a.shipUnits(up, s, st)
		if n > 0 {
			sum.Records += n
			shippedAny = true
		}
		if err != nil {
			if err = a.held(err, &st.Refused, root+"/"+s.name, sum); err != nil && failed == nil {
				failed = err
			}
		}
	}
	if shippedAny {
		sum.Shipped++
	}
	return failed
}

// shipUnits sends every unit of one opencode stream whose version is not the one shipped, and
// commits the acknowledged prefix.
//
// 🔴 A PENDING WRITE IS SETTLED FIRST, AND NOTHING NEW IS SENT UNTIL IT IS. If an earlier write
// went unacknowledged, the instance is asked where the stream is ([Sink.Position] — it stores
// nothing) and the plan is settled against the answer ([StreamState.reconcile]). An instance that
// cannot be asked gets no new write, so there is only ever ONE plan and never one to evict.
//
// The write's own plan — each unit, its version, the cursor after it — is then put in the state
// AND SAVED before the request leaves. The upsert set is recomputed from the state HERE rather
// than taken from the derivation, so a move's reset (every unit again) and a settled position
// (only what the instance lacks) are the same computation.
func (a *Agent) shipUnits(up Upload, s ocStream, st *StreamState) (int, error) {
	if len(st.Pending) > 0 {
		at, err := a.Sink.Position(up, s.name)
		if err != nil {
			return 0, err
		}
		was := st.Cursor
		if !st.reconcile(at) {
			return 0, &UnresumableError{StoredTo: at}
		}
		if at != was {
			a.logf("resume %s/%s at %s: matched against a sent, unacknowledged write", up.Root, s.name, at)
		}
	}
	todo := diffUnits(s.all, st.Units)
	if len(todo) == 0 {
		return 0, nil
	}
	recs := make([]Record, 0, len(todo))
	plan := make([]PendingUnit, 0, len(todo))
	cur := st.Cursor
	for _, u := range todo {
		r := a.redactLine(u.Key+"@"+digest(u.Raw)[:12], u.Raw)
		cur = chainCursor(cur, u.Key, r.Rec)
		r.End = cur
		recs = append(recs, r)
		plan = append(plan, PendingUnit{Key: u.Key, Digest: digest(u.Raw), End: cur})
	}
	st.Pending = plan
	if err := a.checkpoint(); err != nil {
		return 0, fmt.Errorf("capture: the write's plan could not be saved, so it is not sent: %w", err)
	}
	up.From = st.Cursor
	a.rechecks(&up, st, recs)
	n, err := a.Sink.Records(up, s.name, recs)
	settled(st, recs[:n])
	if n > 0 {
		for _, done := range plan[:n] {
			st.Units[done.Key] = done.Digest
		}
		// The instance is known to be at this cursor; what is left of the plan still chains from it.
		st.Cursor, st.Pending = plan[n-1].End, plan[n:]
		if n == len(plan) {
			st.Pending = nil
		}
	}
	return n, err
}
