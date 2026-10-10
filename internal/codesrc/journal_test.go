package codesrc

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/arcs"
)

var (
	t0 = time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
	t1 = time.Date(2000, 1, 3, 3, 4, 5, 0, time.UTC)

	srcA = "git:github.com/example-org/example-repo@main"
	srcB = "git:git.example.com/team/sub-group/example-repo@trunk"
	srcC = "git:github.com/example-org/example-mono//services/widget@release/2.x"
)

func newJournal(t *testing.T) Journal {
	t.Helper()
	return Journal{Path: filepath.Join(t.TempDir(), "sources.jsonl")}
}

func lineCount(t *testing.T, j Journal) int {
	t.Helper()
	data, err := os.ReadFile(j.Path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(data, []byte("\n"))
}

func mustRead(t *testing.T, j Journal) Snapshot {
	t.Helper()
	s, err := j.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return s
}

// The record a Set writes, field by field, as LITERALS — including the stamp, which is the
// caller's principal and clock and never anything derived from the sources.
func TestSetAppendsOneLiteralRecord(t *testing.T) {
	j := newJournal(t)
	rec, err := j.Set("alpha-notes", []string{srcA, srcB}, RevisionNone, "reader-one", t0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := lineCount(t, j); n != 1 {
		t.Fatalf("lines = %d, want 1", n)
	}
	got, ok := mustRead(t, j).For("alpha-notes")
	if !ok {
		t.Fatal("the scope reads undeclared after a Set")
	}
	if !slices.Equal(got.Sources, []string{srcA, srcB}) || got.SetBy != "reader-one" ||
		got.SetAt != "2000-01-02T03:04:05Z" || got.Scope != "alpha-notes" || got.Schema != 1 {
		t.Fatalf("record = %+v", got)
	}
	if got.Revision != rec.Revision || got.Revision == RevisionNone || !strings.HasPrefix(got.Revision, "sha256:") {
		t.Fatalf("revision = %q (Set returned %q)", got.Revision, rec.Revision)
	}
}

// The revision is a LITERAL digest, computed independently (Python's `hashlib.sha256` over
// `json.dumps([scope, sources], separators=(",", ":"))`), so a fixture written outside Go can
// carry a valid one — never derived from `Revision` itself.
func TestTheRevisionIsALiteralDigest(t *testing.T) {
	for _, tc := range []struct {
		sources []string
		want    string
	}{
		{[]string{srcA}, "sha256:fa46c8eb492f986bfd14624ae9dc3d224657d90e4f90653bf8fadf40d1e90f16"},
		{[]string{}, "sha256:6b417d95f50e3d3984a21a9a7c4b7097af627e8713eabdbdbf6aa5d72ea38271"},
		{nil, "sha256:6b417d95f50e3d3984a21a9a7c4b7097af627e8713eabdbdbf6aa5d72ea38271"},
	} {
		if got := Revision("alpha-notes", tc.sources); got != tc.want {
			t.Errorf("Revision(alpha-notes, %q) = %s, want %s", tc.sources, got, tc.want)
		}
	}
}

// 🔴 THE SAME-REVISION RACE (the plan's F2), FORCED RATHER THAN HOPED FOR. Call one is held INSIDE
// its lock, between its compare and its append, while call two starts carrying the SAME revision.
// With the compare inside the lock, call two blocks on `flock`, re-reads after call one's append,
// and is refused. With the compare moved outside the lock, call two compares against the bytes
// before call one's append, passes, and lands too.
//
// The hold is a rendezvous with a deadline: call one waits for call two to reach ITS interleave, or
// for 300ms, whichever is first. Under the real code the deadline is what releases call one (call
// two is parked on the lock and cannot arrive). ⚠ THE KILL IS TIMED, NOT A RENDEZVOUS, for the
// mutant `codesrc-revision-compared-outside-the-lock` as written: its moved compare sits BEFORE the
// lock while `interleave` stays inside it, so call two never reaches the rendezvous; what kills it is
// that call two's pre-lock read runs within the 300ms hold — measured red on the line count (2 ≠ 1).
// A variant that moved `interleave` out with the compare WOULD rendezvous.
func TestTwoWritesCarryingOneRevisionLandExactlyOnce(t *testing.T) {
	j := newJournal(t)
	if _, err := j.Set("alpha-notes", []string{srcA}, RevisionNone, "reader-one", t0, nil); err != nil {
		t.Fatal(err)
	}
	base := mustRead(t, j).RevisionFor("alpha-notes")
	before := lineCount(t, j)

	firstIn := make(chan struct{})
	secondIn := make(chan struct{})
	var secondOnce sync.Once
	holdFirst := func() {
		close(firstIn)
		select {
		case <-secondIn:
		case <-time.After(300 * time.Millisecond):
		}
	}
	markSecond := func() { secondOnce.Do(func() { close(secondIn) }) }

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, errs[0] = j.Set("alpha-notes", []string{srcB}, base, "reader-one", t1, holdFirst)
	}()
	<-firstIn
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, errs[1] = j.Set("alpha-notes", []string{srcC}, base, "reader-two", t1, markSecond)
	}()
	wg.Wait()

	if added := lineCount(t, j) - before; added != 1 {
		t.Fatalf("two writes carrying one revision appended %d lines, want EXACTLY 1 — "+
			"the revision is being compared outside the lock", added)
	}
	if errs[0] != nil {
		t.Fatalf("the first writer (holding the lock) failed: %v", errs[0])
	}
	var stale *StaleRevisionError
	if !errors.As(errs[1], &stale) {
		t.Fatalf("the second writer got %v, want *StaleRevisionError", errs[1])
	}
	snap := mustRead(t, j)
	if stale.Current != snap.RevisionFor("alpha-notes") || stale.Current == base {
		t.Fatalf("the refusal carries %q, want the NEW current revision %q", stale.Current, snap.RevisionFor("alpha-notes"))
	}
	if got, _ := snap.For("alpha-notes"); !slices.Equal(got.Sources, []string{srcB}) {
		t.Fatalf("the fold shows %q, want the first call's list", got.Sources)
	}
}

// A sequential stale write — the plain lost-update shape — writes nothing. And a resubmission of
// an UNCHANGED list carrying the now-stale revision is refused the same way (T6's harmless no-op).
func TestAStaleRevisionWritesNothing(t *testing.T) {
	j := newJournal(t)
	if _, err := j.Set("alpha-notes", []string{srcA}, RevisionNone, "reader-one", t0, nil); err != nil {
		t.Fatal(err)
	}
	before := lineCount(t, j)
	for _, list := range [][]string{{srcB}, {srcA}} {
		_, err := j.Set("alpha-notes", list, RevisionNone, "reader-two", t1, nil)
		var stale *StaleRevisionError
		if !errors.As(err, &stale) {
			t.Fatalf("Set(%q) with a stale revision = %v, want *StaleRevisionError", list, err)
		}
	}
	if after := lineCount(t, j); after != before {
		t.Fatalf("a stale write appended %d line(s)", after-before)
	}
}

// LATEST WINS per scope. The two records' lists are distinct, so an earliest-wins fold is visible.
func TestTheFoldIsLatestWins(t *testing.T) {
	j := newJournal(t)
	r1, err := j.Set("alpha-notes", []string{srcA}, RevisionNone, "reader-one", t0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Set("alpha-notes", []string{srcB, srcC}, r1.Revision, "reader-two", t1, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := mustRead(t, j).For("alpha-notes")
	if !slices.Equal(got.Sources, []string{srcB, srcC}) || got.SetBy != "reader-two" {
		t.Fatalf("fold = %+v, want the LATER record", got)
	}
}

// A torn tail and a non-JSON line are skipped and COUNTED; the valid records around them still
// fold, and a scope whose LATEST line is damaged falls back to its previous record (T15).
func TestDamagedLinesAreSkippedAndCounted(t *testing.T) {
	j := newJournal(t)
	r1, err := j.Set("alpha-notes", []string{srcA}, RevisionNone, "reader-one", t0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Set("beta-notes", []string{srcB}, RevisionNone, "reader-one", t0, nil); err != nil {
		t.Fatal(err)
	}
	// A hand-edited LATER alpha line whose revision no longer matches its sources.
	forged := r1
	forged.Sources = []string{srcC}
	forgedLine, _ := json.Marshal(forged)
	f, err := os.OpenFile(j.Path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("this is not json\n")
	f.Write(append(forgedLine, '\n'))
	f.WriteString(`{"schema":1,"scope":"beta-no`) // torn
	f.Close()

	snap := mustRead(t, j)
	if snap.Skipped != 2 || !snap.TornTail || snap.Records != 2 || !snap.Damaged() {
		t.Fatalf("snapshot = skipped %d torn %v records %d, want 2 / true / 2", snap.Skipped, snap.TornTail, snap.Records)
	}
	if got, ok := snap.For("alpha-notes"); !ok || !slices.Equal(got.Sources, []string{srcA}) {
		t.Fatalf("alpha-notes = %+v, want its PREVIOUS valid record", got)
	}
	if got, ok := snap.For("beta-notes"); !ok || !slices.Equal(got.Sources, []string{srcB}) {
		t.Fatalf("beta-notes = %+v, want its record, untouched by the damage after it", got)
	}
}

// A torn tail is SEALED before the next append, so the fragment never glues onto the new record.
func TestATornTailIsSealedBeforeTheNextAppend(t *testing.T) {
	j := newJournal(t)
	if err := os.WriteFile(j.Path, []byte(`{"schema":1,"scope":"alpha-no`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Set("alpha-notes", []string{srcA}, RevisionNone, "reader-one", t0, nil); err != nil {
		t.Fatal(err)
	}
	snap := mustRead(t, j)
	if got, ok := snap.For("alpha-notes"); !ok || !slices.Equal(got.Sources, []string{srcA}) {
		t.Fatalf("the record after a torn tail did not fold: %+v", snap)
	}
	if snap.Skipped != 1 || snap.TornTail {
		t.Fatalf("skipped %d torn %v, want the sealed fragment counted once and no torn tail", snap.Skipped, snap.TornTail)
	}
}

// 🔴 INVARIANT GUARD, LABELLED AS ONE: no build writes an unknown field yet, so this pins the
// forward-compatibility rule (a newer build's record still folds) rather than a regression.
func TestAnUnknownFieldIsIgnoredNotRefused(t *testing.T) {
	j := newJournal(t)
	rec, err := j.Set("alpha-notes", []string{srcA}, RevisionNone, "reader-one", t0, nil)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	b, _ := json.Marshal(rec)
	json.Unmarshal(b, &m)
	m["note_from_a_newer_build"] = "x"
	m["set_by"] = "reader-two"
	line, _ := json.Marshal(m)
	f, _ := os.OpenFile(j.Path, os.O_APPEND|os.O_WRONLY, 0)
	f.Write(append(line, '\n'))
	f.Close()
	snap := mustRead(t, j)
	if got, _ := snap.For("alpha-notes"); got.SetBy != "reader-two" || snap.Skipped != 0 {
		t.Fatalf("a record with an unknown field did not fold: %+v skipped=%d", got, snap.Skipped)
	}
}

// The KEY is the folded name (Q17): two spellings reach one record; a renamed directory's NEW
// name reads undeclared while the old record survives; a recreated same-name scope reads the old.
func TestTheKeyIsTheFoldedName(t *testing.T) {
	j := newJournal(t)
	if _, err := j.Set(Key("Alpha-Notes"), []string{srcA}, RevisionNone, "reader-one", t0, nil); err != nil {
		t.Fatal(err)
	}
	snap := mustRead(t, j)
	if _, ok := snap.For("alpha-notes"); !ok {
		t.Fatal("alpha-notes does not read the record Alpha-Notes declared")
	}
	if _, ok := snap.For("gamma-notes"); ok {
		t.Fatal("a renamed directory's new name must read undeclared")
	}
	if _, ok := snap.Latest["alpha-notes"]; !ok {
		t.Fatal("the old record must survive in Read (it is history)")
	}
	// A recreated alpha-notes is the same key, so it inherits the record and its stamp.
	if got, ok := snap.For("alpha-notes"); !ok || got.SetBy != "reader-one" {
		t.Fatalf("a recreated same-name scope reads %+v, want the old record with its stamp", got)
	}
	if _, err := j.Set("Alpha-Notes", []string{srcA}, RevisionNone, "reader-one", t0, nil); err == nil {
		t.Fatal("Set accepted a scope that is not a key")
	}
}

// The three read states, each a literal. OFF lives at `FromEnv`; BROKEN and EMPTY at `Read`.
func TestTheThreeReadStates(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	t.Run("unset is off", func(t *testing.T) {
		for _, env := range []map[string]string{{}, {EnvJournal: ""}} {
			_, ok, err := FromEnv(root, func(k string) (string, bool) { v, ok := env[k]; return v, ok })
			if ok || err != nil {
				t.Fatalf("env %v: ok=%v err=%v, want off", env, ok, err)
			}
		}
	})
	t.Run("an absent file is empty and Missing", func(t *testing.T) {
		j, ok, err := FromEnv(root, func(k string) (string, bool) { return filepath.Join(outside, "absent.jsonl"), k == EnvJournal })
		if !ok || err != nil {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		snap := mustRead(t, j)
		if !snap.Missing || len(snap.Latest) != 0 || snap.Damaged() {
			t.Fatalf("snapshot = %+v, want Missing and empty", snap)
		}
	})
	t.Run("a directory at the path is unreadable", func(t *testing.T) {
		dir := filepath.Join(outside, "a-dir")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := Journal{Path: dir}.Read()
		var ue *JournalUnreadableError
		if !errors.As(err, &ue) {
			t.Fatalf("Read of a directory = %v, want *JournalUnreadableError", err)
		}
		want := "code sources journal unreadable: " + dir + " (read " + dir + ": is a directory) — this is NOT 'no sources declared'"
		if err.Error() != want {
			t.Fatalf("message = %q\nwant      %q", err.Error(), want)
		}
	})
}

// The two refusals: inside the store root names the SOURCES journal, and a symlink at the journal
// path is refused by the write (O_NOFOLLOW) — and by the read.
func TestTheRefusals(t *testing.T) {
	t.Run("inside the store root names the sources journal", func(t *testing.T) {
		root := t.TempDir()
		_, _, err := FromEnv(root, func(k string) (string, bool) { return filepath.Join(root, "sources.jsonl"), k == EnvJournal })
		var inside *arcs.InsideStoreError
		if !errors.As(err, &inside) {
			t.Fatalf("got %v, want *arcs.InsideStoreError", err)
		}
		msg := err.Error()
		if !strings.HasPrefix(msg, "the sources journal ($CAIRN_SOURCE_JOURNAL) ") || strings.Contains(msg, "arc journal") {
			t.Fatalf("the refusal must name the SOURCES journal and its variable, never the arc journal: %q", msg)
		}
	})
	t.Run("a symlink at the path is refused", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "elsewhere.jsonl")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "sources.jsonl")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		j := Journal{Path: link}
		_, err := j.Set("alpha-notes", []string{srcA}, RevisionNone, "reader-one", t0, nil)
		var ue *JournalUnreadableError
		if !errors.As(err, &ue) {
			t.Fatalf("Set through a symlink = %v, want a refusal", err)
		}
		if data, _ := os.ReadFile(target); len(data) != 0 {
			t.Fatalf("the write FOLLOWED the symlink: %q", data)
		}
		if _, err := j.Read(); !errors.As(err, &ue) {
			t.Fatalf("Read through a symlink = %v, want a refusal", err)
		}
	})
}

// An empty list is an explicit "undeclared, by <who> at <when>": it is written, and it reads as a
// record with no sources — distinct from no record at all.
func TestAnEmptyListIsARecordNotAnAbsence(t *testing.T) {
	j := newJournal(t)
	r1, _ := j.Set("alpha-notes", []string{srcA}, RevisionNone, "reader-one", t0, nil)
	if _, err := j.Set("alpha-notes", nil, r1.Revision, "reader-two", t1, nil); err != nil {
		t.Fatal(err)
	}
	got, ok := mustRead(t, j).For("alpha-notes")
	if !ok || got.Sources == nil || len(got.Sources) != 0 || got.SetBy != "reader-two" {
		t.Fatalf("cleared = %+v ok=%v", got, ok)
	}
	data, _ := os.ReadFile(j.Path)
	if !bytes.Contains(data, []byte(`"sources":[]`)) {
		t.Fatalf("the cleared record must carry sources: [] — %s", data)
	}
}
