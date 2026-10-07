package report

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/store"
)

// The cross-scope session answer's own guards. Every name, session id and date is synthetic.
//
// The world: `north-notes` and `south-notes` are readable, `east-notes` is NOT. `s-roam-01` wrote
// in all three — newest in the hidden one, so a walk that ignored `visible` would put `east-notes`
// FIRST, not merely add it. `s-hide-02` wrote ONLY in `east-notes`; `s-never-03` wrote nowhere.
// Bullet counts per scope are pairwise distinct (north 1, south 2, east 3) so a sum or a swap moves
// a number.
func sessionAcrossStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put := func(scope, service, nuance string) {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nservice: " + service + "\nscope: " + scope + "\n---\n\n" + store.NuanceHeading + "\n\n" + nuance
		if err := os.WriteFile(filepath.Join(dir, service+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("north-notes", "kettle",
		"- 2000-01-03: boiled [cairn: roam-bot/s-roam-01]\n- 2000-01-02: unsigned\n")
	put("south-notes", "ladle",
		"- 2000-01-07: stirred [cairn: roam-bot/s-roam-01]\n- 2000-01-05: ladled [cairn: other-bot/s-roam-01]\n")
	put("east-notes", "spoon",
		"- 2000-01-20: hidden one [cairn: roam-bot/s-roam-01]\n"+
			"- 2000-01-21: hidden two [cairn: hide-bot/s-hide-02]\n"+
			"- 2000-01-22: hidden three [cairn: hide-bot/s-hide-02]\n")
	return root
}

var northSouth = store.VisibleScopeSet([]string{"north-notes", "south-notes"})

func sessionAcrossSnapshot() *arcs.Snapshot {
	reg := func(home, slug, status string, members ...arcs.Member) arcs.Registration {
		return arcs.Registration{Schema: arcs.Schema, Home: home, Slug: slug, Status: status,
			ClosingKind: arcs.ClosingCheck, DeclaredScopes: []string{home}, Members: members,
			RegisteredBy: "fixture-registrar", RegisteredAt: "2000-01-06T07:08:09Z"}
	}
	regs := []arcs.Registration{
		reg("north-notes", "kettle-arc", arcs.StatusOpen, arcs.Member{Session: "s-roam-01", Role: "wrote"}),
		// Homed in the HIDDEN scope, naming both sessions: never listed for this caller.
		reg("east-notes", "spoon-arc", arcs.StatusClosed,
			arcs.Member{Session: "s-roam-01", Role: "originated"}, arcs.Member{Session: "s-hide-02", Role: "originated"}),
	}
	snap := arcs.Snapshot{Latest: map[arcs.Key]arcs.Registration{}}
	for _, r := range regs {
		snap.Latest[r.Key()] = r
	}
	return &snap
}

// TestTheSessionAcrossAnswerGroupsReadableScopesNewestFirstAndDropsTheHiddenOne: the aggregation,
// its order, and the narrowing, pinned as literal values. RED with `visible` replaced by
// `store.Unrestricted()` inside `SessionAcross` (east-notes leads the list, spoon-arc appears).
func TestTheSessionAcrossAnswerGroupsReadableScopesNewestFirstAndDropsTheHiddenOne(t *testing.T) {
	root := sessionAcrossStore(t)
	rep, err := SessionAcross(root, "s-roam-01", northSouth, sessionAcrossSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var scopes []string
	for _, s := range rep.Scopes {
		scopes = append(scopes, s.Scope)
	}
	if want := []string{"south-notes", "north-notes"}; !slices.Equal(scopes, want) {
		t.Fatalf("scopes %v, want %v (newest activity first, the unreadable east-notes absent)", scopes, want)
	}
	if got := rep.Bullets(); got != 3 {
		t.Errorf("bullets %d, want 3 (south 2 + north 1; the hidden one is not counted)", got)
	}
	if got := rep.Actors(); !slices.Equal(got, []string{"other-bot", "roam-bot"}) {
		t.Errorf("actors %v", got)
	}
	if f, l := rep.DateSpan(); f != "2000-01-03" || l != "2000-01-07" {
		t.Errorf("date span %s..%s, want 2000-01-03..2000-01-07 (the hidden 2000-01-20 is not readable)", f, l)
	}
	if rep.ScopesScanned != 2 || rep.Coverage.Bullets != 4 || rep.Coverage.Attributed != 3 {
		t.Errorf("coverage: %d scopes, %d of %d bullets attributed — want 2 scopes, 3 of 4",
			rep.ScopesScanned, rep.Coverage.Attributed, rep.Coverage.Bullets)
	}
	if want := []SessionArc{{Home: "north-notes", Slug: "kettle-arc", Status: arcs.StatusOpen,
		ClosingKind: arcs.ClosingCheck, Role: "wrote"}}; !reflect.DeepEqual(rep.Arcs, want) {
		t.Errorf("arcs %+v, want only the readable-home kettle-arc", rep.Arcs)
	}
	if !rep.Found() || rep.Partial() || !rep.ArcsConfigured {
		t.Errorf("found=%v partial=%v configured=%v", rep.Found(), rep.Partial(), rep.ArcsConfigured)
	}

	// POSITIVE CONTROL: the same id over an unrestricted set DOES see east-notes, first.
	wide, err := SessionAcross(root, "s-roam-01", store.Unrestricted(), sessionAcrossSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if len(wide.Scopes) != 3 || wide.Scopes[0].Scope != "east-notes" || len(wide.Arcs) != 2 {
		t.Fatalf("POSITIVE CONTROL FAILED: unrestricted, the hidden scope and arc are not seen (%+v) — the absence "+
			"above would then measure nothing", wide)
	}
}

// TestASessionSeenOnlyInAnUnreadableScopeIsTheSameAnswerAsANeverWrittenOne is the no-existence-
// oracle relation at the data layer: the two answers are DeepEqual once the asked-for id is set
// aside. RED if the walk reads the hidden scope (s-hide-02 then has scopes and an arc).
func TestASessionSeenOnlyInAnUnreadableScopeIsTheSameAnswerAsANeverWrittenOne(t *testing.T) {
	root := sessionAcrossStore(t)
	hidden, err := SessionAcross(root, "s-hide-02", northSouth, sessionAcrossSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	never, err := SessionAcross(root, "s-never-03", northSouth, sessionAcrossSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if hidden.Found() || never.Found() {
		t.Fatalf("found: hidden=%v never=%v — neither session wrote anything this caller can read", hidden.Found(), never.Found())
	}
	hidden.ID, never.ID = "", ""
	if !reflect.DeepEqual(hidden, never) {
		t.Errorf("a session seen only in an unreadable scope answers DIFFERENTLY from one never written:\n%+v\nvs\n%+v",
			hidden, never)
	}
	// POSITIVE CONTROL: the hidden session IS there for a caller who reads east-notes.
	east, err := SessionAcross(root, "s-hide-02", store.VisibleScopeSet([]string{"east-notes"}), sessionAcrossSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !east.Found() || east.Bullets() != 2 {
		t.Fatalf("POSITIVE CONTROL FAILED: s-hide-02 is not found by a reader of east-notes (%+v)", east)
	}
}

// TestASessionIdIsMatchedByteExact: ids are opaque — a case-folded spelling is a different session.
func TestASessionIdIsMatchedByteExact(t *testing.T) {
	rep, err := SessionAcross(sessionAcrossStore(t), "S-ROAM-01", northSouth, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Found() {
		t.Errorf("an upper-cased id matched %+v — session ids are never folded", rep.Scopes)
	}
	if rep.ArcsConfigured {
		t.Error("a nil snapshot reported arcs as configured")
	}
}
