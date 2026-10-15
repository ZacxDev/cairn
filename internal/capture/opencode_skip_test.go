package capture

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// listedUpdated is the synthetic world's one `session list` row's `updated`, read rather than
// restated so a regenerated world moves it.
func listedUpdated(t *testing.T, w world) int64 {
	t.Helper()
	var rows []struct {
		Updated int64 `json:"updated"`
	}
	if err := json.Unmarshal(w.Opencode.SessionList, &rows); err != nil || len(rows) != 1 || rows[0].Updated == 0 {
		t.Fatalf("the world's session list is not one row with an `updated`: %v", err)
	}
	return rows[0].Updated
}

// withUpdated is the world's session list with its one row's `updated` replaced.
func withUpdated(t *testing.T, w world, updated int64) []byte {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(w.Opencode.SessionList, &rows); err != nil {
		t.Fatal(err)
	}
	rows[0]["updated"] = updated
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func clockAt(ms int64) func() time.Time { return func() time.Time { return time.UnixMilli(ms) } }

// skipWorld is an opencode-only agent over the synthetic world's root and its child.
func skipWorld(t *testing.T, w world) (env, *fakeRunner, *Agent) {
	t.Helper()
	e := materialize(t, w, "oc-root")
	runner := &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList}
	a := newAgent(t, e, oneInstance, runner, &bytes.Buffer{})
	a.ClaudeRoot, a.OpencodeDirs = "", []string{"/work/alpha"}
	return e, runner, a
}

var settle = OpencodeSettle.Milliseconds()

// TestAnUnchangedSettledRootIsNotExportedAgain: the second pass over a root whose listed `updated`
// is unchanged, exported once at least the settle window after it, makes ZERO `opencode export`
// calls — for the root AND its child.
func TestAnUnchangedSettledRootIsNotExportedAgain(t *testing.T) {
	w := loadWorld(t)
	u := listedUpdated(t, w)
	_, runner, a := skipWorld(t, w)
	a.Now = clockAt(u + settle + 60_000)
	if sum := mustRun(t, a); sum.Records == 0 || runner.calls < 2 || sum.ExportSkipped != 0 {
		t.Fatalf("the first pass: %+v, %d exports — want records shipped, the root and its child exported", sum, runner.calls)
	}
	runner.calls = 0
	sum := mustRun(t, a)
	if runner.calls != 0 || sum.ExportSkipped != 1 || sum.Refused != 0 {
		t.Fatalf("the second, unchanged, settled pass made %d export call(s) (%+v); want 0 and one skip", runner.calls, sum)
	}
}

// TestAChangedUpdatedIsExportedAgain: the listed `updated` moved, so the root is exported again
// even though the record is settled.
func TestAChangedUpdatedIsExportedAgain(t *testing.T) {
	w := loadWorld(t)
	u := listedUpdated(t, w)
	_, runner, a := skipWorld(t, w)
	a.Now = clockAt(u + settle + 60_000)
	mustRun(t, a)
	runner.calls, runner.list = 0, withUpdated(t, w, u+1)
	if sum := mustRun(t, a); runner.calls < 2 || sum.ExportSkipped != 0 {
		t.Fatalf("a moved `updated` made %d export call(s) (%+v); want the root and its child exported", runner.calls, sum)
	}
}

// TestARowWithNoUpdatedIsAlwaysExported: a row whose `updated` is missing or not an integer never
// skips — ambiguous fails toward reading.
func TestARowWithNoUpdatedIsAlwaysExported(t *testing.T) {
	for _, val := range []any{nil, "soon", 1.5} {
		w := loadWorld(t)
		u := listedUpdated(t, w)
		var rows []map[string]any
		_ = json.Unmarshal(w.Opencode.SessionList, &rows)
		if val == nil {
			delete(rows[0], "updated")
		} else {
			rows[0]["updated"] = val
		}
		list, _ := json.Marshal(rows)
		_, runner, a := skipWorld(t, w)
		runner.list = list
		a.Now = clockAt(u + settle + 60_000)
		mustRun(t, a)
		// No record is written from a row with no `updated`: there is nothing to compare next time.
		if ss := a.State.Sessions[w.Sessions["oc-root"].ID]; ss.OpencodeUpdated != 0 || ss.OpencodeExportedAt != 0 {
			t.Fatalf("updated=%v: a record %d/%d was written from a row with no `updated`", val, ss.OpencodeUpdated, ss.OpencodeExportedAt)
		}
		runner.calls = 0
		if sum := mustRun(t, a); runner.calls == 0 || sum.ExportSkipped != 0 {
			t.Fatalf("updated=%v: %d export call(s) on the second pass (%+v); want an export", val, runner.calls, sum)
		}
	}
}

// TestAZeroUpdatedNeverSettles: the comparison itself refuses a listed `updated` of 0 even against
// a record that would otherwise match — a state file is an operator-editable input, and one
// carrying `oc_updated` 0 with an export time must not turn every row without `updated` into a
// skip. (The run never WRITES such a record: TestARowWithNoUpdatedIsAlwaysExported.)
func TestAZeroUpdatedNeverSettles(t *testing.T) {
	a := &Agent{}
	if a.exportSettled(&SessionState{OpencodeUpdated: 0, OpencodeExportedAt: 946684800000}, 0) {
		t.Fatal("a listed `updated` of 0 matched a hand-written record of 0 and skipped the export")
	}
	if !a.exportSettled(&SessionState{OpencodeUpdated: 1, OpencodeExportedAt: 1 + settle}, 1) {
		t.Fatal("control: a matching, settled record did not skip")
	}
}

// TestAnUnchangedButUnsettledRootIsExportedAgain: the export ran inside the settle window after
// the root's `updated`, so a child may have changed after it; the root is exported again — and
// once an export has run past the window, the next pass skips.
func TestAnUnchangedButUnsettledRootIsExportedAgain(t *testing.T) {
	w := loadWorld(t)
	u := listedUpdated(t, w)
	_, runner, a := skipWorld(t, w)
	a.Now = clockAt(u + settle/2)
	mustRun(t, a)
	runner.calls = 0
	if sum := mustRun(t, a); runner.calls < 2 || sum.ExportSkipped != 0 {
		t.Fatalf("an unsettled record skipped: %d export call(s) (%+v)", runner.calls, sum)
	}
	// Boundary: an export that began EXACTLY the window after `updated` is settled.
	a.Now = clockAt(u + settle)
	mustRun(t, a)
	runner.calls = 0
	if sum := mustRun(t, a); runner.calls != 0 || sum.ExportSkipped != 1 {
		t.Fatalf("an export begun exactly %v after `updated` did not settle: %d call(s) (%+v)", OpencodeSettle, runner.calls, sum)
	}
}

// TestAChildChangedAfterItsRootInsideTheWindowIsCaptured: the measured shape — a child updated
// AFTER its root, whose own listed `updated` therefore does not move. Exported first 30 s after the
// root's update; the child changes; the next pass, still inside the window, exports again and ships
// the child's change.
func TestAChildChangedAfterItsRootInsideTheWindowIsCaptured(t *testing.T) {
	w := loadWorld(t)
	u := listedUpdated(t, w)
	e, runner, a := skipWorld(t, w)
	root, child := w.Sessions["oc-root"].ID, w.Sessions["oc-child"].ID
	// The child's export is re-encoded ONCE before the first pass, so the edit below changes the
	// bytes of exactly one part rather than the encoding of every unit.
	var doc map[string]any
	if err := json.Unmarshal(runner.exports[child], &doc); err != nil {
		t.Fatal(err)
	}
	runner.exports[child], _ = json.Marshal(doc)
	a.Now = clockAt(u + 30_000)
	mustRun(t, a)
	before := len(spooled(t, e, "personal", root, "child:"+child))
	if before == 0 {
		t.Fatal("the child was not shipped on the first pass")
	}
	msgs := doc["messages"].([]any)
	part := msgs[len(msgs)-1].(map[string]any)["parts"].([]any)[0].(map[string]any)
	part["synthetic_late_edit"] = "changed after the root"
	runner.exports[child], _ = json.Marshal(doc)
	a.Now = clockAt(u + 60_000)
	mustRun(t, a)
	after := spooled(t, e, "personal", root, "child:"+child)
	if len(after) != before+1 || !strings.HasPrefix(after[len(after)-1].Src, "prt:") {
		t.Fatalf("the child's late change: %d records before, %d after; want exactly one more, a part", before, len(after))
	}
}

// boundSkipWorld is a two-instance agent whose session has shipped to personal and been recorded
// as settled. The root's own ledger names its read, so `V` is not F1's `*` (which HOLDS a session
// on a two-instance host).
func boundSkipWorld(t *testing.T) (world, env, *fakeRunner, *Agent, *bytes.Buffer) {
	t.Helper()
	w := loadWorld(t)
	u := listedUpdated(t, w)
	e := materialize(t, w, "oc-root")
	root := w.Sessions["oc-root"].ID
	runner := &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList}
	var log bytes.Buffer
	a := newAgent(t, e, twoInstances, runner, &log)
	a.ClaudeRoot, a.OpencodeDirs = "", []string{"/work/alpha"}
	a.Now = clockAt(u + settle + 60_000)
	if err := os.WriteFile(filepath.Join(e.ledger, root+".jsonl"),
		[]byte(ledgerLine(root, "sessions", "alpha-notes")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, a)
	if ss := a.State.Sessions[root]; ss == nil || ss.Instance != "personal" || ss.Held || ss.OpencodeUpdated != u {
		t.Fatalf("the first pass did not bind the session to personal with a record: %+v\n%s", ss, log.String())
	}
	runner.calls = 0
	return w, e, runner, a, &log
}

// TestASkippedRootIsStillRoutedOnANewRead: a skip makes no export call, but the session's ledgers
// are still folded and its routing still decided — a read of a client scope recorded in a CHILD's
// ledger after the last export HOLDS the session (it wrote a personal scope), exactly as without
// the skip.
func TestASkippedRootIsStillRoutedOnANewRead(t *testing.T) {
	w, e, runner, a, log := boundSkipWorld(t)
	root, child := w.Sessions["oc-root"].ID, w.Sessions["oc-child"].ID
	if err := os.WriteFile(filepath.Join(e.ledger, child+".jsonl"),
		[]byte(ledgerLine(child, "sessions", "beta-notes")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := mustRun(t, a)
	ss := a.State.Sessions[root]
	if runner.calls != 0 || sum.ExportSkipped != 1 || !ss.Held || sum.Held != 1 {
		t.Fatalf("a child's read of a client scope under a skip: %d export call(s), held %v (%+v); want 0 calls and HELD\n%s",
			runner.calls, ss.Held, sum, log.String())
	}
}

// TestASkippedRootStillMovesOnATableEdit: a routing-TABLE edit between passes moves a skipped
// session — the prefix is withdrawn from personal — and, because the move reset every watermark
// while this pass exported nothing, the record is cleared and the NEXT pass exports the whole
// session and ships it to client.
func TestASkippedRootStillMovesOnATableEdit(t *testing.T) {
	w, e, runner, a, log := boundSkipWorld(t)
	root := w.Sessions["oc-root"].ID
	a.Routing.Routes = map[string]string{"alpha-notes": "client", "beta-notes": "client"}
	sum := mustRun(t, a)
	ss := a.State.Sessions[root]
	if runner.calls != 0 || sum.ExportSkipped != 1 || ss.Instance != "client" || len(e.sink.withdrawn["personal"]) != 1 {
		t.Fatalf("a table edit under a skip: %d export call(s), instance %q, withdrawn %v (%+v)\n%s",
			runner.calls, ss.Instance, e.sink.withdrawn, sum, log.String())
	}
	if ss.OpencodeUpdated != 0 || ss.OpencodeExportedAt != 0 {
		t.Fatalf("a move kept the skip record %d/%d; the next pass would ship nothing to the new instance",
			ss.OpencodeUpdated, ss.OpencodeExportedAt)
	}
	sum = mustRun(t, a)
	if runner.calls < 2 || sum.ExportSkipped != 0 || len(spooled(t, e, "client", root, "main")) == 0 {
		t.Fatalf("after the move the session was not exported and shipped to client (%d calls, %+v)", runner.calls, sum)
	}
}

// TestARefusedPassClearsTheRecord: a pass that did not complete — a write refused — leaves no
// record, so the next pass exports even with `updated` unchanged and settled.
func TestARefusedPassClearsTheRecord(t *testing.T) {
	w := loadWorld(t)
	u := listedUpdated(t, w)
	e, runner, a := skipWorld(t, w)
	root := w.Sessions["oc-root"].ID
	a.Now = clockAt(u + settle + 60_000)
	e.sink.failRoot = root
	if sum := mustRun(t, a); sum.Refused == 0 {
		t.Fatalf("the failing sink refused nothing: %+v", sum)
	}
	if ss := a.State.Sessions[root]; ss.OpencodeUpdated != 0 || ss.OpencodeExportedAt != 0 {
		t.Fatalf("a refused pass left a skip record %d/%d", ss.OpencodeUpdated, ss.OpencodeExportedAt)
	}
	e.sink.failRoot = ""
	runner.calls = 0
	if sum := mustRun(t, a); runner.calls < 2 || sum.Refused != 0 || sum.Records == 0 {
		t.Fatalf("after a refused pass: %d export call(s), %+v; want a full export that ships", runner.calls, sum)
	}
}
