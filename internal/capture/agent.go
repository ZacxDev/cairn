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

	// ListCap is the `-n` each `session list` is asked with; 0 is [SessionListCap]. Only a test
	// sets it, so the at-cap refusal can be reached without a million-row fixture.
	ListCap int
	// Now is the wall clock the opencode export skip reads ([Agent.exportSettled]); nil is
	// [time.Now].
	Now func() time.Time

	// fatal is the first error that ends the run's uploads loudly (a refused token).
	fatal error
	// dirty is whether the state holds a change no checkpoint has saved; saved is when it last was.
	dirty bool
	saved time.Time
	// sawDeleted is set when a write of the session being captured was answered 410 ([ErrDeleted]),
	// whichever of its failures the session reports; [Agent.isolate] turns it into the session's
	// final mark.
	sawDeleted bool
	// recurs is the first [Recurs] answer seen among ANY of the session's failed writes ([Agent.held]),
	// so a recurring refusal behind an ordinary first failure still counts.
	recurs Recurrence
}

func (a *Agent) listCap() int {
	if a.ListCap > 0 {
		return a.ListCap
	}
	return SessionListCap
}

func (a *Agent) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
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
	// Skipped counts sessions nothing was sent for because their instance is not taking uploads in
	// this run: those not read at all (already bound to it with nothing shipped), and those read and
	// routed to it only to find it stopped.
	Skipped int
	// WithdrawalPending counts sessions whose shipped prefix must be withdrawn and whose withdrawal
	// got no answer in this run; each is asked again on the next. Deleted counts sessions an instance
	// answered 410 for in this run — their transcript was deleted there, and they are never sent again.
	WithdrawalPending, Deleted int
	// Unresumable and NotOwner are the part of Refused whose session was refused for a reason that
	// RECURS on every run with nothing changed on either side ([Recurs]); `cairn-capture --run` exits
	// 3 when either is non-zero. Not printed: the run line's `refused=` is unchanged.
	Unresumable, NotOwner int
	// ExportSkipped counts opencode roots whose `opencode export` calls this run made none of
	// ([Agent.exportSettled]); printed as the summary's LAST field, `export-skipped=`, so the skip
	// working is visible in the journal and no field before it moved.
	ExportSkipped int
}

// Recurrence is [Recurs]'s answer.
type Recurrence int

const (
	// NotRecurring is every other failure: retried next run, or already marked so it is not.
	NotRecurring Recurrence = iota
	// RecursUnresumable is an [UnresumableError]: the instance holds the stream at a position no
	// run of this state can resume from.
	RecursUnresumable
	// RecursNotOwner is [ErrNotOwner]: another (owner, host) holds the root on the instance.
	RecursNotOwner
)

// Recurs is THE ONE classification of which session refusals fail a run (`cairn-capture --run`
// exits 3; the table is `internal/capture/README.md` § *`--run`'s exit status*): a refusal that the
// next run will meet again with nothing changed on either side. Typed, never matched on text.
//
// 🔴 EVERYTHING ELSE IS NotRecurring, DELIBERATELY. A network error, a 5xx, a 400, an unanswered
// withdrawal, an unlistable opencode project, an incomplete export and a 503/507 are retried next
// run and may not recur — failing the unit on one was measured in review (a single 502) and
// narrowed out on an operator decision. A 422 that HOLDS a stream (a [RecheckRefusal] naming a
// field) and a rewritten stream are permanent but marked ONCE and skipped silently after, so they
// would fail exactly one run — and the only state remedy would be wrong for both (clearing re-sends
// bytes the pod's re-check refused, or re-ships a rewritten file from its start).
func Recurs(err error) Recurrence {
	var ur *UnresumableError
	switch {
	case errors.As(err, &ur):
		return RecursUnresumable
	case errors.Is(err, ErrNotOwner):
		return RecursNotOwner
	}
	return NotRecurring
}

// String is the run's one summary line.
func (s Summary) String() string {
	return fmt.Sprintf("sessions=%d shipped=%d held=%d withdrawn=%d records=%d blobs=%d refused=%d skipped=%d withdrawal-pending=%d deleted=%d export-skipped=%d",
		s.Sessions, s.Shipped, s.Held, s.Withdrawn, s.Records, s.Blobs, s.Refused, s.Skipped, s.WithdrawalPending, s.Deleted,
		s.ExportSkipped)
}

func (a *Agent) logf(format string, args ...any) {
	if a.Log != nil {
		fmt.Fprintf(a.Log, format+"\n", args...)
	}
}

// Run captures every session once. It returns an error only for what ends the run's uploads — a
// session tree that cannot be listed, or an instance that refused the capture token
// ([FatalError]); every other failure is one session's, logged and counted in Refused, and the run
// goes on to every other session. Which of those refusals fail the RUN is [Recurs]'s answer, counted
// in [Summary] and read by the caller after the pass has completed and the state is saved.
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
	seen := map[string]bool{}
	for _, dir := range a.OpencodeDirs {
		raw, err := a.Runner.List(dir, a.listCap())
		if err == nil {
			var roots []listedRoot
			if roots, err = listRoots(raw, a.listCap()); err == nil {
				for _, root := range firstSeen(roots, seen) {
					a.isolate(root.ID, &sum, func() error { return a.runOpencode(root, &sum) })
				}
			}
		}
		if err != nil {
			// One unreadable project directory is refused and logged like one session; the rest
			// still run. A list at its `-n` cap ([ErrSessionListAtCap]) is refused WHOLE, here: no
			// root of it is captured, because a list that may be cut short is not read as complete.
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

// firstSeen is the roots of one `session list` not already processed in this pass, marking them.
//
// 🔴 A ROOT IS PROCESSED ONCE PER PASS, WHICHEVER DIRECTORY LISTED IT. `session list` filters by
// opencode's PROJECT, not by directory (measured: a base checkout and a worktree of it answered
// identical id sets), so two directories of one project — an explicit `-opencode-project` beside
// the worktree discovery names, or a worktree and a sandbox — list the same roots, and nothing
// short of asking opencode maps a directory to its project. Deduplicating the ROOTS is the
// mechanical form: a second visit would export it twice and count it twice.
func firstSeen(roots []listedRoot, seen map[string]bool) []listedRoot {
	var out []listedRoot
	for _, r := range roots {
		if !seen[r.ID] {
			seen[r.ID] = true
			out = append(out, r)
		}
	}
	return out
}

// isolate runs one session's capture and turns its failure into a logged refusal.
//
// 🔴 ONE SESSION'S FAILURE MUST NOT ABORT THE RUN. Returning the first error stopped every later
// session, on every 60-second run, for as long as the one bad session existed — a child export
// that fails, a blob a sink refuses (review round 1). The failed session's watermarks did not
// advance (each advances only after its own write), so the next run retries it.
func (a *Agent) isolate(root string, sum *Summary, run func() error) {
	before := a.sessionImage(root)
	a.sawDeleted, a.recurs = false, NotRecurring
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
		r := a.recurs
		if r == NotRecurring {
			r = Recurs(err)
		}
		switch r {
		case RecursUnresumable:
			sum.Unresumable++
		case RecursNotOwner:
			sum.NotOwner++
		}
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
	if r := Recurs(err); r != NotRecurring && a.recurs == NotRecurring {
		a.recurs = r
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
	if ss.Instance != "" && !dec.Held && dec.Instance != "" && ss.Instance != dec.Instance {
		// 🔴 THE DESTINATION IS ASKED BEFORE THE CURRENT HOLDER IS TOLD TO FORGET, AND ONLY A DEFINITE
		// "NOT DELETED HERE" LETS THE WITHDRAWAL GO AHEAD (#273's audit F1, closed in S10; #274's
		// audit made it "only"). A routing-TABLE edit can send a session BACK to an instance it was
		// once withdrawn from — and a deletion there is final for the root (410). Withdrawing first
		// deleted the prefix from the instance that held it, and the first upload to the other then
		// answered 410 and marked the session deleted: its capture ended EVERYWHERE, over an edit to
		// a table. The probe stores nothing ([Sink.Position]) and has three outcomes:
		//
		//   - a position (the CAS's own 409): the destination has NOT deleted this root — withdraw
		//     and move, as before;
		//   - 410: the session is HELD where it is, for good — no withdrawal, nothing more shipped,
		//     said once;
		//   - NO ANSWER, or any other: the withdrawal is DEFERRED to the next run. The first version
		//     of this probe withdrew anyway, so an unreachable destination that would have said 410
		//     still ended the capture everywhere.
		//
		// ⚠ THE TRADE, an agent decision open to operator review: while the destination cannot be
		// asked, the prefix stays on an instance the CURRENT table no longer routes its scopes to,
		// and nothing new of the session is shipped anywhere.
		_, perr := a.Sink.Position(Upload{Instance: dec.Instance, Root: root, Runtime: probeRuntime(ss), Declared: ss.Declared}, "main")
		switch {
		case errors.Is(perr, ErrDeleted):
			ss.Held, ss.HeldReason = true, "routed back to "+dec.Instance+", which deleted this session's transcript: kept on "+ss.Instance
			a.logf("HOLD %s: its routing answer now names instance %s, where this session's transcript was DELETED and "+
				"cannot be stored again — the prefix stays on %s, nothing is withdrawn and nothing more of the session is "+
				"shipped", root, dec.Instance, ss.Instance)
			sum.Held++
			return false, false, a.saveRouting(root)
		case perr != nil:
			var fatal *FatalError
			if errors.As(perr, &fatal) && a.fatal == nil {
				a.fatal = perr
			}
			sum.WithdrawalPending++
			return false, false, fmt.Errorf("its routing answer now names instance %s, which could not be asked whether it "+
				"deleted this session — so nothing is withdrawn from %s and nothing more of the session is sent this run "+
				"(asked again next run): %w", dec.Instance, ss.Instance, perr)
		}
	}
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

// probeRuntime is the runtime a position probe names: the session's own, or Claude Code's when the
// state predates the field — the probe is answered before the runtime is looked at either way.
func probeRuntime(ss *SessionState) string {
	if ss.Runtime == "opencode" {
		return "opencode"
	}
	return "claude"
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

// OpencodeSettle is how long after a root's listed `updated` its export must have begun before an
// unchanged `updated` may skip the next export ([Agent.exportSettled]).
//
// 🔴 WHY A WINDOW AND NOT THE ROOT'S `updated` ALONE: the listed `updated` does not move for every
// change an export would carry. Two shapes were measured on one host: a CHILD session updated
// after its root (2 of 459 children, by at most 39.8 s), and a root's OWN final assistant message
// whose `time.completed` lands after the root's listed `updated` (13 of 120 recent roots, by at most
// 135.4 s) — a message completing does not bump `updated`. An export that began at least this long
// after the root's last update saw every such change measured, with a wide margin; one that began
// sooner may not have, so the root is exported again until one has.
//
// ⚠ A change later than the window, with `updated` still unmoved, is beyond what was measured and
// not ruled out — [OpencodeMaxExportAge] is what bounds it.
const OpencodeSettle = 60 * time.Minute

// OpencodeMaxExportAge is the BACKSTOP: a root whose recorded export began longer ago than this is
// exported again whatever its `updated` says, so a change the skip rule cannot see — one landing
// later than [OpencodeSettle] with `updated` unmoved — is delayed by at most this, never lost. It
// costs one export per root per day.
const OpencodeMaxExportAge = 24 * time.Hour

// exportSettled reports whether this run may make NO `opencode export` call for a root: its
// listed `updated` is the one recorded at the last pass that exported and shipped the root and
// every child COMPLETELY ([SessionState.OpencodeUpdated]), and that pass's export began at least
// [OpencodeSettle] after it, and less than [OpencodeMaxExportAge] ago.
//
// 🔴 MEASURED COST, THE REASON IT EXISTS: one `opencode export` costs ~0.65 s whatever its size,
// and every root and every child was exported on every 60-second pass — at ~600 roots and ~460
// children, minutes of CPU per pass, forever, for sessions nobody had touched.
//
// 🔴 ANYTHING AMBIGUOUS EXPORTS: no record, a row with no `updated` (0), a different one, an
// unsettled export, an export older than the backstop, or a record that a pass which did not
// complete has cleared. A skip still
// folds every ledger, ROUTES and ships the ledger ([Agent.runOpencode]), so a read that moves the
// session, a routing-table edit and a pending withdrawal are all acted on exactly as before.
func (a *Agent) exportSettled(ss *SessionState, updated int64) bool {
	return updated > 0 && ss.OpencodeUpdated == updated &&
		ss.OpencodeExportedAt >= updated+OpencodeSettle.Milliseconds() &&
		a.now().UnixMilli()-ss.OpencodeExportedAt < OpencodeMaxExportAge.Milliseconds()
}

// opencodeComplete reports whether nothing of an opencode session is refused, held, pending or awaiting a
// re-check placeholder — and, for the streams this run exported, nothing is left unshipped.
func (ss *SessionState) opencodeComplete(exported []ocStream) bool {
	for _, st := range ss.Streams {
		if st.Refused != "" || len(st.Pending) > 0 || len(st.Rechecked) > 0 {
			return false
		}
	}
	for _, s := range exported {
		if len(diffUnits(s.all, ss.stream(s.name).Units)) > 0 {
			return false
		}
	}
	return true
}

func (a *Agent) runOpencode(lr listedRoot, sum *Summary) (err error) {
	root := lr.ID
	ss := a.State.session(root, "opencode")
	sum.Sessions++
	if ss.Held || ss.Deleted != "" {
		return nil
	}
	if a.skipUnread(ss) {
		sum.Skipped++
		return nil
	}
	skip := a.exportSettled(ss, lr.Updated)
	began := a.now().UnixMilli()
	var streams []ocStream
	var children []string
	// 🔴 THE RECORD IS WRITTEN ONLY BY A PASS THAT COMPLETED, AND CLEARED BY ANY THAT DID NOT —
	// an early return included (a refused export, a route that did not ship, a stopped instance, a
	// failed write). A skipped pass that completes keeps the record it was skipped on.
	completed := false
	defer func() {
		switch {
		case completed && !skip && lr.Updated > 0:
			ss.OpencodeUpdated, ss.OpencodeExportedAt = lr.Updated, began
		case !completed:
			ss.OpencodeUpdated, ss.OpencodeExportedAt = 0, 0
		}
	}()
	if skip {
		sum.ExportSkipped++
		// The children are the ones the recorded pass exported: every `child:<id>` stream.
		for name := range ss.Streams {
			if id, ok := strings.CutPrefix(name, "child:"); ok {
				children = append(children, id)
			}
		}
		sort.Strings(children)
	}
	// Export the root and, recursively, every child its `task` parts name.
	type job struct{ id, stream string }
	var queue []job
	if !skip {
		queue = []job{{root, "main"}}
	}
	seen := map[string]bool{root: true}
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

	ship, reset, err := a.route(root, ss, sum)
	if err != nil || !ship {
		return err
	}
	if skip && reset {
		// A MOVE reset every watermark, and this run exported nothing to re-ship: the cleared record
		// makes the next run export the whole session and ship it to the new instance.
		return nil
	}
	if a.stopped(ss.Instance) {
		sum.Skipped++
		return nil
	}
	defer func() { completed = err == nil && ss.opencodeComplete(streams) }()
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
