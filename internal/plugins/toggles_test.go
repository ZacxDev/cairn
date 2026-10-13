package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/arcs"
)

// Every value is SYNTHETIC: scopes `alpha-notes`/`beta-notes`, plugin `summary`, session `s-0001`.

const plug = "summary"

var (
	arcOne = arcs.Key{Home: "alpha-notes", Slug: "arc-one"}
	arcTwo = arcs.Key{Home: "beta-notes", Slug: "arc-two"}
	at2000 = time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339)
)

func knownAll(name string) bool {
	return name == "alpha-notes" || name == "beta-notes" || name == "gamma-notes"
}

// state builds a fold from records `level target value`.
func state(t *testing.T, rows ...[3]string) State {
	t.Helper()
	j := Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")}
	for _, r := range rows {
		if err := j.Append(Record{Plugin: plug, Level: Level(r[0]), Target: r[1], Value: r[2],
			By: By{Kind: "user", ID: "u-admin"}, At: at2000}); err != nil {
			t.Fatalf("append %v: %v", r, err)
		}
	}
	st, err := j.Read()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// foldCase asserts one of decision 10's cases with a LITERAL expectation.
func foldCase(t *testing.T, rows [][3]string, sub Subject, want bool) {
	t.Helper()
	if got := Effective(state(t, rows...), plug, sub, knownAll); got != want {
		t.Fatalf("Effective = %v, want %v", got, want)
	}
}

var both = []string{"alpha-notes", "beta-notes"}

// The toggle fold is decision 10's ten cases, ONE test each, so each is red under its OWN mutant
// (`plugins-*` rows in `tests/control_mutants.py`, by case number). The ON cases (2, 4) are what keep
// a blanket-OFF implementation from passing the OFF cases. Case 10 is
// `TestASessionLevelOnIsRefusedAndJournalsNothing`.

func TestFoldCase01NothingSetIsOff(t *testing.T) {
	foldCase(t, nil, Subject{Root: "s-0001", V: both}, false)
}

func TestFoldCase02EveryScopeOnIsOn(t *testing.T) {
	foldCase(t, [][3]string{{"scope", "alpha-notes", "on"}, {"scope", "beta-notes", "on"}}, Subject{Root: "s-0001", V: both}, true)
}

func TestFoldCase03AnArcOnWithOneScopeOffIsOff(t *testing.T) {
	foldCase(t, [][3]string{{"scope", "alpha-notes", "on"}, {"scope", "beta-notes", "off"}, {"arc", ArcTarget(arcOne), "on"}},
		Subject{Root: "s-0001", V: both, Arcs: []arcs.Key{arcOne}}, false)
}

func TestFoldCase04AnArcOnWithEveryScopeAllowedIsOn(t *testing.T) {
	foldCase(t, [][3]string{{"scope", "alpha-notes", "allowed"}, {"scope", "beta-notes", "allowed"}, {"arc", ArcTarget(arcOne), "on"}},
		Subject{Root: "s-0001", V: both, Arcs: []arcs.Key{arcOne}}, true)
}

func TestFoldCase05AnArcOnWithOneScopeAllowedAndOneOffIsOff(t *testing.T) {
	foldCase(t, [][3]string{{"scope", "alpha-notes", "allowed"}, {"scope", "beta-notes", "off"}, {"arc", ArcTarget(arcOne), "on"}},
		Subject{Root: "s-0001", V: both, Arcs: []arcs.Key{arcOne}}, false)
}

func TestFoldCase06EveryScopeOnWithAnArcOffIsOff(t *testing.T) {
	foldCase(t, [][3]string{{"scope", "alpha-notes", "on"}, {"scope", "beta-notes", "on"}, {"arc", ArcTarget(arcTwo), "off"}},
		Subject{Root: "s-0001", V: both, Arcs: []arcs.Key{arcOne, arcTwo}}, false)
}

func TestFoldCase07EveryScopeOnWithTheOwnersOptOutIsOff(t *testing.T) {
	foldCase(t, [][3]string{{"scope", "alpha-notes", "on"}, {"scope", "beta-notes", "on"}, {"session", "s-0001", "off"}},
		Subject{Root: "s-0001", V: both}, false)
}

// Case 8: a name the authority does not hold is OFF though its journal line says on, and so is `*`.
func TestFoldCase08AnUnknownScopeOrTheStarIsOff(t *testing.T) {
	foldCase(t, [][3]string{{"scope", "alpha-notes", "on"}, {"scope", "delta-notes", "on"}},
		Subject{Root: "s-0001", V: []string{"alpha-notes", "delta-notes"}}, false)
	foldCase(t, [][3]string{{"scope", "alpha-notes", "on"}}, Subject{Root: "s-0001", V: []string{"*", "alpha-notes"}}, false)
}

func TestFoldCase09TheEmptySetIsOff(t *testing.T) {
	foldCase(t, [][3]string{{"scope", "alpha-notes", "on"}, {"arc", ArcTarget(arcOne), "on"}},
		Subject{Root: "s-0001", Arcs: []arcs.Key{arcOne}}, false)
}

// TestTheFoldsControls are the controls beside the cases: a later write wins, an opt-out set back to
// unset restores ON, another session's opt-out and another plugin's ON change nothing.
func TestTheFoldsControls(t *testing.T) {
	foldCase(t, [][3]string{{"scope", "alpha-notes", "off"}, {"scope", "alpha-notes", "on"}}, Subject{Root: "s-0001", V: []string{"alpha-notes"}}, true)
	foldCase(t, [][3]string{{"scope", "alpha-notes", "on"}, {"session", "s-0001", "off"}, {"session", "s-0001", "unset"}},
		Subject{Root: "s-0001", V: []string{"alpha-notes"}}, true)
	foldCase(t, [][3]string{{"scope", "alpha-notes", "on"}, {"session", "s-0002", "off"}}, Subject{Root: "s-0001", V: []string{"alpha-notes"}}, true)
	other := state(t, [3]string{"scope", "alpha-notes", "on"})
	if Effective(other, "clickup", Subject{Root: "s-0001", V: []string{"alpha-notes"}}, knownAll) {
		t.Fatal("a toggle set for one plugin turned another ON")
	}
}

// TestASessionLevelOnIsRefusedAndJournalsNothing is case 10 at the journal: the level can only
// narrow, so its ON is refused BEFORE anything is written.
func TestASessionLevelOnIsRefusedAndJournalsNothing(t *testing.T) {
	if err := ValidValue(LevelSession, ValueOn); !errors.Is(err, ErrSessionOn) {
		t.Fatalf("ValidValue(session, on) = %v, want ErrSessionOn", err)
	}
	for _, ok := range []string{ValueOff, ValueUnset} {
		if err := ValidValue(LevelSession, ok); err != nil {
			t.Fatalf("ValidValue(session, %s) = %v, want nil (positive control)", ok, err)
		}
	}
	j := Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")}
	err := j.Append(Record{Plugin: plug, Level: LevelSession, Target: "s-0001", Value: ValueOn, By: By{Kind: "user", ID: "u-o"}, At: at2000})
	if err == nil {
		t.Fatal("a session-level ON was journaled")
	}
	if _, statErr := os.Stat(j.Path); !os.IsNotExist(statErr) {
		t.Fatalf("a refused toggle created or wrote the journal (%v)", statErr)
	}
	for level, bad := range map[Level]string{LevelScope: ValueUnset, LevelArc: ValueAllowed, "instance": ValueOn} {
		if ValidValue(level, bad) == nil {
			t.Fatalf("%s=%s was accepted", level, bad)
		}
	}
}

// TestTheJournalIsLastWriteWinsAndSealsATornTail: one line per append, a torn tail ignored on read
// and sealed (not glued) before the next append, and a line that does not validate counted and
// never applied.
func TestTheJournalIsLastWriteWinsAndSealsATornTail(t *testing.T) {
	j := Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")}
	rec := func(v string) Record {
		return Record{Plugin: plug, Level: LevelScope, Target: "alpha-notes", Value: v, By: By{Kind: "user", ID: "u-a"}, At: at2000}
	}
	if err := j.Append(rec(ValueOn)); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(j.Path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"plugin":"summary","level":"scope","target":"alpha-notes","value":"off"`)
	_ = f.Close()
	st, err := j.Read()
	if err != nil || !st.TornTail || st.Scope(plug, "alpha-notes") != ValueOn {
		t.Fatalf("read over a torn tail: torn=%v value=%q err=%v, want the tail ignored", st.TornTail, st.Scope(plug, "alpha-notes"), err)
	}
	if err := j.Append(rec(ValueAllowed)); err != nil {
		t.Fatal(err)
	}
	st, _ = j.Read()
	if st.Scope(plug, "alpha-notes") != ValueAllowed || st.Skipped != 1 || st.TornTail {
		t.Fatalf("after the seal: value=%q skipped=%d torn=%v, want allowed/1/false", st.Scope(plug, "alpha-notes"), st.Skipped, st.TornTail)
	}
	raw, _ := os.ReadFile(j.Path)
	if n := strings.Count(string(raw), "\n"); n != 3 {
		t.Fatalf("the journal holds %d lines, want 3 (two records and the sealed fragment)", n)
	}
	info, _ := os.Stat(j.Path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("the journal's mode is %v, want 0600", info.Mode().Perm())
	}
}
