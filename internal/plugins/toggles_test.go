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

// arcRecord journals one arc toggle; an `off` carries its member snapshot.
func arcRecord(t *testing.T, j Journal, k arcs.Key, value string, members ...string) {
	t.Helper()
	if err := j.Append(Record{Plugin: plug, Level: LevelArc, Target: ArcTarget(k), Value: value, Members: members,
		By: By{Kind: "user", ID: "u-admin"}, At: at2000}); err != nil {
		t.Fatal(err)
	}
}

// registry is an arc registry in which each arc lists the given sessions NOW.
func registry(members map[arcs.Key][]string) arcs.Snapshot {
	snap := arcs.Snapshot{Latest: map[arcs.Key]arcs.Registration{}}
	for k, ids := range members {
		reg := arcs.Registration{Home: k.Home, Slug: k.Slug}
		for _, id := range ids {
			reg.Members = append(reg.Members, arcs.Member{Session: id})
		}
		snap.Latest[k] = reg
	}
	return snap
}

// TestAnArcVetoOutlivesTheMembersRemovalAndAnArcOnIsReadLive — #270's audit, as S6 closed it. The
// two directions of an arc toggle are read against DIFFERENT member lists, deliberately:
//
//   - `off` (the veto) covers the arc's current members AND the members it listed when an admin set
//     it — so a WRITER of the home who re-registers the arc without a session does not lift the veto.
//     RED before the fix (membership read live only): the removed session went back ON.
//   - `on` is read against the arc's members as they are NOW — a session added later IS covered
//     (per-arc summaries stay continuous as an arc grows, operator decision O5). That is the stated
//     residual, pinned here so it cannot change silently in either direction.
func TestAnArcVetoOutlivesTheMembersRemovalAndAnArcOnIsReadLive(t *testing.T) {
	alpha := []string{"alpha-notes"}
	on := func(st State, snap arcs.Snapshot, root string) bool {
		return Effective(st, plug, Subject{Root: root, V: alpha, Arcs: ArcsFor(st, plug, snap, []string{root})}, knownAll)
	}
	scope := func(j Journal, value string) {
		if err := j.Append(Record{Plugin: plug, Level: LevelScope, Target: "alpha-notes", Value: value,
			By: By{Kind: "user", ID: "u-admin"}, At: at2000}); err != nil {
			t.Fatal(err)
		}
	}
	// THE VETO: alpha is ON for everybody; the admin vetoes the arc while it lists s-0001 and s-0002.
	veto := Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")}
	scope(veto, ValueOn)
	arcRecord(t, veto, arcOne, ValueOff, "s-0001", "s-0002")
	st, _ := veto.Read()
	listed := registry(map[arcs.Key][]string{arcOne: {"s-0001", "s-0002"}})
	if on(st, listed, "s-0001") || !on(st, listed, "s-0009") {
		t.Fatal("CONTROL: a vetoed member is ON, or a session the arc never listed is OFF")
	}
	// A writer of the home re-registers the arc WITHOUT s-0001. No admin acted.
	removed := registry(map[arcs.Key][]string{arcOne: {"s-0002"}})
	if on(st, removed, "s-0001") {
		t.Fatal("removing a session from an `off` arc lifted the admin's veto: it is ON again")
	}
	if got := st.ArcSnapshot(plug, arcOne); len(got) != 2 || got[0] != "s-0001" {
		t.Fatalf("ArcSnapshot = %v, want the veto's snapshot", got)
	}
	// A CHILD session id in the snapshot covers its root (the ids asked are the root and its children).
	if got := st.ArcsVetoing(plug, []string{"s-root", "s-0002"}); len(got) != 1 || got[0] != arcOne {
		t.Fatalf("a veto snapshot naming a child session does not cover its root: %v", got)
	}
	// A member ADDED to an `off` arc is vetoed too (the live half): narrowing is never lost.
	if on(st, registry(map[arcs.Key][]string{arcOne: {"s-0002", "s-0003"}}), "s-0003") {
		t.Fatal("a session added to an `off` arc is ON")
	}
	// The admin sets the arc back to `unset`: the veto, snapshot and all, is gone.
	arcRecord(t, veto, arcOne, ValueUnset)
	if st, _ = veto.Read(); !on(st, removed, "s-0001") || len(st.ArcsVetoing(plug, []string{"s-0001"})) != 0 {
		t.Fatal("an arc set back to `unset` still vetoes its old snapshot")
	}
	// Another plugin's veto covers nothing for this one.
	if got := st.ArcsVetoing("clickup", []string{"s-0001"}); len(got) != 0 {
		t.Fatalf("another plugin's vetoes: %v", got)
	}

	// `on` IS LIVE: alpha is only `allowed`; the admin turns the arc ON while it lists s-0001.
	live := Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")}
	scope(live, ValueAllowed)
	arcRecord(t, live, arcOne, ValueOn)
	st, _ = live.Read()
	if !on(st, registry(map[arcs.Key][]string{arcOne: {"s-0001"}}), "s-0001") {
		t.Fatal("POSITIVE CONTROL: a member of an `on` arc is not ON")
	}
	grown := registry(map[arcs.Key][]string{arcOne: {"s-0001", "s-0002"}})
	if !on(st, grown, "s-0002") {
		t.Fatal("a session added to an `on` arc is not covered — `on` must be read against the arc's members now")
	}
	// …and one REMOVED from an `on` arc is no longer covered: `on` keeps no snapshot.
	if on(st, registry(map[arcs.Key][]string{arcOne: {"s-0002"}}), "s-0001") {
		t.Fatal("a session removed from an `on` arc is still ON")
	}
	// The limits `on` keeps: the session must have touched the arc's HOME, and no scope may be off.
	beta := Subject{Root: "s-0002", V: []string{"beta-notes"}, Arcs: ArcsFor(st, plug, grown, []string{"s-0002"})}
	if Effective(st, plug, beta, knownAll) {
		t.Fatal("an `on` arc homed in alpha turned the plugin on for a session that touched only beta")
	}
}

// TestAMemberSnapshotIsAnArcRecordsAndIsSessionIds: the journal refuses a snapshot on a scope or
// session record, on an arc record that is not a veto, a value that is not a session id, and one past the arc registry's own bound — and
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
		"a snapshot on an arc `on`":      rec(LevelArc, ArcTarget(arcOne), ValueOn, []string{"s-0001"}),
		"a snapshot on an arc `unset`":   rec(LevelArc, ArcTarget(arcOne), ValueUnset, []string{"s-0001"}),
		"a member that is not an id":     rec(LevelArc, ArcTarget(arcOne), ValueOff, []string{"../s-0001"}),
		"more members than an arc holds": rec(LevelArc, ArcTarget(arcOne), ValueOff, many),
	} {
		if err := j.Append(r); err == nil {
			t.Errorf("%s was journaled", name)
		}
	}
	if _, err := os.Stat(j.Path); !os.IsNotExist(err) {
		t.Fatalf("a refused record left a journal behind: %v", err)
	}
	if err := j.Append(rec(LevelArc, ArcTarget(arcOne), ValueOff, []string{"s-0001"})); err != nil {
		t.Fatalf("POSITIVE CONTROL: a well-formed arc veto was refused: %v", err)
	}
}
