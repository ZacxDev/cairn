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

// TestAnArcSpeaksOnlyForSessionsThatTouchedItsHome — #270 round 1, both halves: (a) an admin of an
// UNRELATED scope (gamma) cannot turn a plugin ON — or OFF — for a session that only touched other
// scopes by listing it in an arc homed in gamma; (b) once an arc homed in alpha is ON, a session a
// mere writer of alpha adds to it goes ON only if it touched alpha, and not one that touched beta
// alone. RED before the fix: (a) ON, (a') vetoed, (b) ON.
func TestAnArcSpeaksOnlyForSessionsThatTouchedItsHome(t *testing.T) {
	arcGamma := arcs.Key{Home: "gamma-notes", Slug: "arc-g"}
	// (a) beta is only `allowed`; an arc homed in gamma (outside V) is ON.
	foldCase(t, [][3]string{{"scope", "beta-notes", "allowed"}, {"arc", ArcTarget(arcGamma), "on"}},
		Subject{Root: "s-0001", V: []string{"beta-notes"}, Arcs: []arcs.Key{arcGamma}}, false)
	// (a') nor can it VETO: beta is `on`, the gamma arc says `off`.
	foldCase(t, [][3]string{{"scope", "beta-notes", "on"}, {"arc", ArcTarget(arcGamma), "off"}},
		Subject{Root: "s-0001", V: []string{"beta-notes"}, Arcs: []arcs.Key{arcGamma}}, true)
	// (b) arc-one (home alpha) is ON; a session that touched beta alone, added as a member, stays OFF…
	foldCase(t, [][3]string{{"scope", "beta-notes", "allowed"}, {"arc", ArcTarget(arcOne), "on"}},
		Subject{Root: "s-0009", V: []string{"beta-notes"}, Arcs: []arcs.Key{arcOne}}, false)
	// …and one that touched alpha (allowed) goes ON: the home's admin set the arc, inside its own scope.
	foldCase(t, [][3]string{{"scope", "alpha-notes", "allowed"}, {"arc", ArcTarget(arcOne), "on"}},
		Subject{Root: "s-0010", V: []string{"alpha-notes"}, Arcs: []arcs.Key{arcOne}}, true)
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

// arcRecord journals one arc toggle with its member snapshot.
func arcRecord(t *testing.T, j Journal, k arcs.Key, value string, members ...string) {
	t.Helper()
	if err := j.Append(Record{Plugin: plug, Level: LevelArc, Target: ArcTarget(k), Value: value, Members: members,
		By: By{Kind: "user", ID: "u-admin"}, At: at2000}); err != nil {
		t.Fatal(err)
	}
}

// TestAnArcToggleCoversExactlyTheMembersAnAdminSaw — #270's audit, closed in S6. An arc toggle speaks
// for the member list SNAPSHOTTED when an admin of the arc's home set it, and for nothing the arc's
// registration says later: a session a WRITER of the home adds afterwards is not covered — in either
// direction — until an admin sets the toggle again. "It touched the home" did not close that: a
// trailer naming a session id is self-declared, by the same writer.
//
// RED before the fix (membership read live, so every session the arc lists NOW was covered): the
// session added later went ON, and — the veto half — OFF.
func TestAnArcToggleCoversExactlyTheMembersAnAdminSaw(t *testing.T) {
	alpha := []string{"alpha-notes"}
	on := func(st State, root string) bool {
		return Effective(st, plug, Subject{Root: root, V: alpha, Arcs: st.ArcsCovering(plug, []string{root})}, knownAll)
	}
	j := Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")}
	if err := j.Append(Record{Plugin: plug, Level: LevelScope, Target: "alpha-notes", Value: ValueAllowed,
		By: By{Kind: "user", ID: "u-admin"}, At: at2000}); err != nil {
		t.Fatal(err)
	}
	// The admin sets the arc ON while it lists s-0001 only.
	arcRecord(t, j, arcOne, ValueOn, "s-0001")
	st, _ := j.Read()
	if !on(st, "s-0001") {
		t.Fatal("POSITIVE CONTROL: the member the admin saw is not ON")
	}
	if on(st, "s-0002") {
		t.Fatal("a session that was not a member when the admin set the arc ON is ON — a writer of the home can add sessions to an ON arc")
	}
	if got, set := st.ArcMembers(plug, arcOne); !set || len(got) != 1 || got[0] != "s-0001" {
		t.Fatalf("ArcMembers = %v, %v — want the snapshot [s-0001]", got, set)
	}
	// The admin sets it again, now seeing both: both are covered.
	arcRecord(t, j, arcOne, ValueOn, "s-0001", "s-0002")
	if st, _ = j.Read(); !on(st, "s-0001") || !on(st, "s-0002") {
		t.Fatal("after the admin set the toggle again with both members, one of them is not ON")
	}
	// A CHILD session id in the snapshot covers its root (the ids asked are the root and its children).
	if got := st.ArcsCovering(plug, []string{"s-root", "s-0002"}); len(got) != 1 || got[0] != arcOne {
		t.Fatalf("a snapshot naming a child session does not cover its root: %v", got)
	}
	// THE VETO HALF: alpha is ON for everybody; the admin vetoes the arc while it lists s-0001.
	veto := Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")}
	if err := veto.Append(Record{Plugin: plug, Level: LevelScope, Target: "alpha-notes", Value: ValueOn,
		By: By{Kind: "user", ID: "u-admin"}, At: at2000}); err != nil {
		t.Fatal(err)
	}
	arcRecord(t, veto, arcOne, ValueOff, "s-0001")
	if st, _ = veto.Read(); on(st, "s-0001") || !on(st, "s-0002") {
		t.Fatalf("an arc veto snapshotting [s-0001]: s-0001 ON=%v (want off), s-0002 ON=%v (want on — it was never vetoed)",
			on(st, "s-0001"), on(st, "s-0002"))
	}
	// A record with NO snapshot covers nobody, and `unset` covers nobody whatever it carries.
	bare := Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")}
	if err := bare.Append(Record{Plugin: plug, Level: LevelScope, Target: "alpha-notes", Value: ValueAllowed,
		By: By{Kind: "user", ID: "u-admin"}, At: at2000}); err != nil {
		t.Fatal(err)
	}
	arcRecord(t, bare, arcOne, ValueOn)
	arcRecord(t, bare, arcTwo, ValueUnset, "s-0001")
	if st, _ = bare.Read(); on(st, "s-0001") || len(st.ArcsCovering(plug, []string{"s-0001"})) != 0 {
		t.Fatal("an arc record with no member snapshot (or an unset one) covers a session")
	}
	// Another plugin's arc toggle covers nothing for this one.
	if got := st.ArcsCovering("clickup", []string{"s-0001"}); len(got) != 0 {
		t.Fatalf("another plugin's arcs: %v", got)
	}
}

// TestAMemberSnapshotIsAnArcRecordsAndIsSessionIds: the journal refuses a snapshot on a scope or
// session record, a value that is not a session id, and one past the arc registry's own bound — and
// journals nothing for any of them.
func TestAMemberSnapshotIsAnArcRecordsAndIsSessionIds(t *testing.T) {
	j := Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")}
	rec := func(level Level, target, value string, members []string) Record {
		return Record{Plugin: plug, Level: level, Target: target, Value: value, Members: members, By: By{Kind: "user", ID: "u-admin"}, At: at2000}
	}
	many := make([]string, arcs.MaxMembers+1)
	for i := range many {
		many[i] = "s-" + strings.Repeat("0", 4) + string(rune('a'+i%26)) + strings.Repeat("x", i%7)
	}
	for name, r := range map[string]Record{
		"a snapshot on a scope record":   rec(LevelScope, "alpha-notes", ValueOn, []string{"s-0001"}),
		"a snapshot on a session record": rec(LevelSession, "s-0001", ValueOff, []string{"s-0001"}),
		"a member that is not an id":     rec(LevelArc, ArcTarget(arcOne), ValueOn, []string{"../s-0001"}),
		"more members than an arc holds": rec(LevelArc, ArcTarget(arcOne), ValueOn, many),
	} {
		if err := j.Append(r); err == nil {
			t.Errorf("%s was journaled", name)
		}
	}
	if _, err := os.Stat(j.Path); !os.IsNotExist(err) {
		t.Fatalf("a refused record left a journal behind: %v", err)
	}
	if err := j.Append(rec(LevelArc, ArcTarget(arcOne), ValueOn, []string{"s-0001"})); err != nil {
		t.Fatalf("POSITIVE CONTROL: a well-formed arc record was refused: %v", err)
	}
}

// TestSettledIsOffForEverySuperset pins `Settled` against `Effective` by exhaustion over a small
// world: whenever it says a set is settled, the fold is OFF for that set AND for every superset of
// it — the property the store-walk skip rests on. And it is not vacuous: it settles the cases the
// skip exists for, and does NOT settle the one that was skipped wrongly (an ON arc covering the
// session whose home the set does not hold yet).
func TestSettledIsOffForEverySuperset(t *testing.T) {
	scopes := []string{"alpha-notes", "beta-notes", "gamma-notes"}
	values := []string{ValueOff, ValueAllowed, ValueOn}
	arcVals := []string{ValueUnset, ValueOn, ValueOff}
	settledCount, unsettledOff := 0, 0
	for mask := 0; mask < 27*9; mask++ {
		j := Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")}
		m := mask
		for _, sc := range scopes {
			if err := j.Append(Record{Plugin: plug, Level: LevelScope, Target: sc, Value: values[m%3], By: By{Kind: "user", ID: "u"}, At: at2000}); err != nil {
				t.Fatal(err)
			}
			m /= 3
		}
		for _, k := range []arcs.Key{arcOne, arcTwo} {
			arcRecord(t, j, k, arcVals[m%3], "s-0001")
			m /= 3
		}
		st, _ := j.Read()
		sub := func(v []string) Subject {
			return Subject{Root: "s-0001", V: v, Arcs: st.ArcsCovering(plug, []string{"s-0001"})}
		}
		for set := 1; set < 8; set++ {
			var v []string
			for i, sc := range scopes {
				if set&(1<<i) != 0 {
					v = append(v, sc)
				}
			}
			if !Settled(st, plug, sub(v), knownAll) {
				if !Effective(st, plug, sub(v), knownAll) {
					unsettledOff++
				}
				continue
			}
			settledCount++
			for super := set; super < 8; super++ {
				if super&set != set {
					continue
				}
				var sv []string
				for i, sc := range scopes {
					if super&(1<<i) != 0 {
						sv = append(sv, sc)
					}
				}
				if Effective(st, plug, sub(sv), knownAll) {
					t.Fatalf("Settled said %v is OFF for every superset, and %v is ON (journal mask %d)", v, sv, mask)
				}
			}
		}
	}
	if settledCount == 0 || unsettledOff == 0 {
		t.Fatalf("the sweep is vacuous: %d settled sets, %d sets OFF and not settled", settledCount, unsettledOff)
	}
	// The case that was skipped wrongly, by name: beta allowed, alpha allowed, an ON arc homed in alpha.
	j := Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")}
	for _, sc := range []string{"alpha-notes", "beta-notes"} {
		_ = j.Append(Record{Plugin: plug, Level: LevelScope, Target: sc, Value: ValueAllowed, By: By{Kind: "user", ID: "u"}, At: at2000})
	}
	arcRecord(t, j, arcOne, ValueOn, "s-0001")
	st, _ := j.Read()
	stored := Subject{Root: "s-0001", V: []string{"beta-notes"}, Arcs: st.ArcsCovering(plug, []string{"s-0001"})}
	if Effective(st, plug, stored, knownAll) || Settled(st, plug, stored, knownAll) {
		t.Fatal("over the stored half {beta} the fold must be OFF and NOT settled: the walk can add the arc's home")
	}
	if Settled(st, plug, Subject{Root: "s-0001"}, knownAll) {
		t.Fatal("an empty set is settled")
	}
}
