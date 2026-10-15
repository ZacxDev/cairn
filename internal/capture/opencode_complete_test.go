package capture

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestADayOldExportIsExportedAgain is the BACKSTOP (operator decision): a settled, unchanged root
// whose recorded export began [OpencodeMaxExportAge] or more ago is exported again whatever its
// `updated` says, so a change the skip cannot see is delayed at most a day, never lost. Measured at
// three points: a minute short of the age (skips), exactly the age, and an hour past it (both
// export) — and the re-export refreshes the record, so the pass after it skips again.
func TestADayOldExportIsExportedAgain(t *testing.T) {
	day := OpencodeMaxExportAge.Milliseconds()
	for _, tc := range []struct {
		age    int64
		export bool
	}{{day - 60_000, false}, {day, true}, {day + 3_600_000, true}} {
		w := loadWorld(t)
		u := listedUpdated(t, w)
		_, runner, a := skipWorld(t, w)
		t0 := u + settle + 60_000
		a.Now = clockAt(t0)
		mustRun(t, a)
		a.Now = clockAt(t0 + tc.age)
		runner.calls = 0
		sum := mustRun(t, a)
		if exported := runner.calls > 0; exported != tc.export || (sum.ExportSkipped == 1) == tc.export {
			t.Fatalf("an export %d ms old: %d export call(s) (%+v); want exported=%v", tc.age, runner.calls, sum, tc.export)
		}
		if tc.export {
			runner.calls = 0
			if sum := mustRun(t, a); runner.calls != 0 || sum.ExportSkipped != 1 {
				t.Fatalf("the re-export at age %d ms did not refresh the record: %d call(s) next pass", tc.age, runner.calls)
			}
		}
	}
}

// TestAWriteThatFailsAfterStoringEverythingRecordsNothing: the session's own error decides, even
// when every unit was acknowledged and the state is clean — a pass that reported a failure is not
// a completed one.
func TestAWriteThatFailsAfterStoringEverythingRecordsNothing(t *testing.T) {
	w := loadWorld(t)
	u := listedUpdated(t, w)
	e, _, a := skipWorld(t, w)
	root := w.Sessions["oc-root"].ID
	a.Now = clockAt(u + settle + 60_000)
	e.sink.errAfterStore = errors.New("an answer lost after the store")
	if sum := mustRun(t, a); sum.Refused != 1 || sum.Records == 0 {
		t.Fatalf("the pass: %+v; want every record stored and the session refused", sum)
	}
	ss := a.State.Sessions[root]
	if !ss.opencodeComplete(nil) {
		t.Fatal("CONTROL: the state is not clean, so this test does not isolate the error clause")
	}
	if ss.OpencodeUpdated != 0 || ss.OpencodeExportedAt != 0 {
		t.Fatalf("a pass that returned an error left a skip record %d/%d", ss.OpencodeUpdated, ss.OpencodeExportedAt)
	}
}

// TestAHeldStreamRecordsNothing: a 422 naming a request field HOLDS the main stream — the session
// returns no error (the hold is marked, not retried) — and the pass still did not complete.
func TestAHeldStreamRecordsNothing(t *testing.T) {
	w := loadWorld(t)
	u := listedUpdated(t, w)
	e, _, a := skipWorld(t, w)
	root := w.Sessions["oc-root"].ID
	a.Now = clockAt(u + settle + 60_000)
	e.sink.fail = map[string]error{"main": &RecheckRefusal{Record: -1, Field: "stream"}}
	mustRun(t, a)
	ss := a.State.Sessions[root]
	if ss.Streams["main"].Refused == "" {
		t.Fatal("CONTROL: the main stream was not held")
	}
	if ss.OpencodeUpdated != 0 || ss.OpencodeExportedAt != 0 {
		t.Fatalf("a pass with a held stream left a skip record %d/%d", ss.OpencodeUpdated, ss.OpencodeExportedAt)
	}
}

// TestCompletenessIsEveryClause: opencodeComplete over states that differ from a complete one in
// exactly ONE respect — each is incomplete, and the untouched state is complete.
func TestCompletenessIsEveryClause(t *testing.T) {
	unit := exportUnit{Key: "prt:p1", Raw: []byte(`{"id":"p1"}`)}
	clean := func() *SessionState {
		return &SessionState{Streams: map[string]*StreamState{"main": {Units: map[string]string{"prt:p1": digest(unit.Raw)}}}}
	}
	exported := []ocStream{{name: "main", all: []exportUnit{unit}}}
	if !clean().opencodeComplete(exported) {
		t.Fatal("CONTROL: the clean state is not complete")
	}
	for name, spoil := range map[string]func(*SessionState){
		"refused":   func(ss *SessionState) { ss.Streams["main"].Refused = "held" },
		"pending":   func(ss *SessionState) { ss.Streams["main"].Pending = []PendingUnit{{Key: "prt:p1"}} },
		"rechecked": func(ss *SessionState) { ss.Streams["main"].Rechecked = map[string]string{"s": "d"} },
		"unshipped": func(ss *SessionState) { ss.Streams["main"].Units = map[string]string{} },
	} {
		ss := clean()
		spoil(ss)
		if ss.opencodeComplete(exported) {
			t.Errorf("a state %s in one stream reads as complete", name)
		}
	}
}

// TestTheSkipWindowsAreTheOperatorsDecision pins the two windows as LITERALS: every other skip test
// reads the constants, so a changed value would move them along with it.
func TestTheSkipWindowsAreTheOperatorsDecision(t *testing.T) {
	if OpencodeSettle != 60*time.Minute || OpencodeMaxExportAge != 24*time.Hour {
		t.Fatalf("settle %v, backstop %v; the operator decided 1h0m0s and 24h0m0s", OpencodeSettle, OpencodeMaxExportAge)
	}
}

// TestTheRunLinePrintsExportSkipped: the skip is visible in the journal — `export-skipped=` is the
// summary's LAST field (so no field before it moved), carrying the count, not a constant.
func TestTheRunLinePrintsExportSkipped(t *testing.T) {
	for _, n := range []int{0, 3} {
		line := Summary{ExportSkipped: n, Deleted: 7}.String()
		if !strings.HasSuffix(line, fmt.Sprintf(" deleted=7 export-skipped=%d", n)) {
			t.Fatalf("the run line %q does not print export-skipped=%d last", line, n)
		}
	}
}

// TestARootListedByTwoDirectoriesIsProcessedOnce: `session list` answers a PROJECT, so two
// directories of one project list the same roots; the pass exports and counts each root once.
func TestARootListedByTwoDirectoriesIsProcessedOnce(t *testing.T) {
	w := loadWorld(t)
	_, runner, a := skipWorld(t, w)
	sum := mustRun(t, a)
	once := runner.calls
	_, runner2, b := skipWorld(t, w)
	b.OpencodeDirs = []string{"/work/alpha", "/work/alpha-second-checkout"}
	sum2 := mustRun(t, b)
	if runner2.calls != once || sum2.Sessions != sum.Sessions || sum2.Records != sum.Records {
		t.Fatalf("two directories of one project: %d exports, %d sessions, %d records; one directory: %d, %d, %d",
			runner2.calls, sum2.Sessions, sum2.Records, once, sum.Sessions, sum.Records)
	}
	var out bytes.Buffer
	if err := b.DryRun(&out); err != nil || strings.Count(out.String(), "opencode ") != 1 {
		t.Fatalf("--dry-run over two directories of one project printed:\n%s(%v)", out.String(), err)
	}
}
