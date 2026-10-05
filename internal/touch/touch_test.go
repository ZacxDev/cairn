package touch

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/write"
)

// All names, sessions and dates below are synthetic (year-2000 dates, `*-bot` actors).

const gadgetOne = "---\nservice: gadget-one\nscope: alpha-notes\n---\n\n" +
	"## What it is\nA synthetic entry. [cairn: alpha-bot/s-9001]\n\n" +
	"## Nuance / work-history\n" +
	"- 2000-01-05: alpha one [cairn: alpha-bot/s-0001]\n" +
	"- 2000-01-03: alpha two [cairn: alpha-bot/s-0001] [cb:0a1b2c3d]\n" +
	"- alpha undated [cairn: beta-bot/ses_example0000000000000000001]\n" +
	"- 2000-01-04: two in a run [cairn: alpha-bot/s-0002] [cairn: gamma-bot/s-0002]\n" +
	"- 2000-01-02: plain untrailered\n" +
	"- 2000-01-02: refused trailer [cairn: Alpha/s-0003]\n" +
	"- 2000-01-08: zeta [cairn: zeta-bot/s-0008]\n"

// CRLF throughout, and a `## Pointers` bullet carrying a trailer that must NOT count.
var gizmoTwo = strings.ReplaceAll("---\nservice: gizmo-two\nscope: alpha-notes\n---\n\n"+
	"## Pointers\n- see the runbook [cairn: alpha-bot/s-9002]\n\n"+
	"## Nuance / work-history\n"+
	"- 2000-01-01: crlf bullet [cairn: delta-bot/s-0001]\n"+
	"- 2000-01-06: quoted `[cairn: q-bot/s-9999]` mid prose\n"+
	"- 2000-01-06: wrapped [cairn: alpha-bot/s-0004]\n  continued later by hand\n"+
	"- refused [cairn: /s-0005]\n"+
	"- 2000-01-07: kept [cairn: x@y.invalid/s-0006] [cairn: epsilon-bot/s-0007]\n", "\n", "\r\n")

const widgetThree = "---\nservice: widget-three\nscope: alpha-notes\n---\n\n## What it is\nNo history.\n"
const doodadFour = "---\nservice: doodad-four\nscope: alpha-notes\n---\n\n## Nuance / work-history\n- 2000-01-09: never read [cairn: alpha-bot/s-0010]\n"

func writeFile(t testing.TB, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixtureStore builds the main world and returns its root and an index loaded over it,
// with `doodad-four.md` removed AFTER the load so it is named by the index and
// unreadable — the "store changed under the reader" shape.
func fixtureStore(t *testing.T) (string, *store.Index) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	writeFile(t, filepath.Join(dir, "gadget-one.md"), gadgetOne)
	writeFile(t, filepath.Join(dir, "gizmo-two.md"), gizmoTwo)
	writeFile(t, filepath.Join(dir, "widget-three.md"), widgetThree)
	writeFile(t, filepath.Join(dir, "doodad-four.md"), doodadFour)
	// Two entries the index REJECTS (no `service:`).
	writeFile(t, filepath.Join(dir, "broken-five.md"), "---\nscope: alpha-notes\n---\n")
	writeFile(t, filepath.Join(dir, "broken-six.md"), "---\nscope: alpha-notes\n---\n")
	// A rejected entry in ANOTHER scope, so a store-wide malformed count is not the
	// scope's.
	writeFile(t, filepath.Join(root, "beta-notes", "broken-eight.md"), "---\nscope: beta-notes\n---\n")
	writeFile(t, filepath.Join(root, "beta-notes", "other-seven.md"),
		"---\nservice: other-seven\nscope: beta-notes\n---\n\n## Nuance / work-history\n- x [cairn: alpha-bot/s-0001]\n")
	ix, err := store.LoadStore(root, "read", store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "doodad-four.md")); err != nil {
		t.Fatal(err)
	}
	return root, ix
}

func TestWritesCoverageCounters(t *testing.T) {
	root, ix := fixtureStore(t)
	res, err := Writes(root, ix, "alpha-notes")
	if err != nil {
		t.Fatal(err)
	}
	// Pairwise distinct on purpose, and distinct from len(Sessions)=5 and len(Edges)=8,
	// so a counter wired to the wrong quantity cannot pass.
	c := res.Coverage
	if c.Entries != 4 {
		t.Errorf("Coverage.Entries = %d, want 4", c.Entries)
	}
	if c.EntriesMalformed != 2 {
		t.Errorf("Coverage.EntriesMalformed = %d, want 2", c.EntriesMalformed)
	}
	if c.EntriesUnreadable != 1 {
		t.Errorf("Coverage.EntriesUnreadable = %d, want 1", c.EntriesUnreadable)
	}
	if c.Bullets != 12 {
		t.Errorf("Coverage.Bullets = %d, want 12", c.Bullets)
	}
	if c.Attributed != 7 {
		t.Errorf("Coverage.Attributed = %d, want 7", c.Attributed)
	}
	if c.MalformedTrailers != 3 {
		t.Errorf("Coverage.MalformedTrailers = %d, want 3", c.MalformedTrailers)
	}
	if len(res.Unreadable) != 1 {
		t.Fatalf("Unreadable = %v, want one error", res.Unreadable)
	}
	var unreadable *store.EntryUnreadableError
	if !errors.As(error(res.Unreadable[0]), &unreadable) ||
		!strings.Contains(unreadable.Error(), "doodad-four.md") {
		t.Errorf("Unreadable[0] = %v, want an EntryUnreadableError naming doodad-four.md", res.Unreadable[0])
	}
}

func TestWritesEdgesAndSessions(t *testing.T) {
	root, ix := fixtureStore(t)
	res, err := Writes(root, ix, "alpha-notes")
	if err != nil {
		t.Fatal(err)
	}
	if res.Scope != "alpha-notes" {
		t.Errorf("Scope = %q", res.Scope)
	}
	type pair struct{ ref, actor, session, date string }
	var got []pair
	for _, e := range res.Edges {
		if e.Scope != "alpha-notes" || len(e.CitationID) != 8 {
			t.Errorf("edge %+v: wrong scope or citation id", e)
		}
		got = append(got, pair{e.EntryRef, e.Actor, e.Session, e.Date})
	}
	// Index order (filename-sorted: gadget-one before gizmo-two), then file order,
	// then left to right in the run.
	want := []pair{
		{"gadget-one", "alpha-bot", "s-0001", "2000-01-05"},
		{"gadget-one", "alpha-bot", "s-0001", "2000-01-03"},
		{"gadget-one", "beta-bot", "ses_example0000000000000000001", ""},
		{"gadget-one", "alpha-bot", "s-0002", "2000-01-04"},
		{"gadget-one", "gamma-bot", "s-0002", "2000-01-04"},
		{"gadget-one", "zeta-bot", "s-0008", "2000-01-08"},
		{"gizmo-two", "delta-bot", "s-0001", "2000-01-01"},
		{"gizmo-two", "epsilon-bot", "s-0007", "2000-01-07"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("edges:\n got %v\nwant %v", got, want)
	}

	type sess struct {
		id          string
		actors      []string
		bullets     int
		first, last string
		undated     int
	}
	var gotS []sess
	for _, s := range res.Sessions {
		gotS = append(gotS, sess{s.ID, s.Actors, len(s.Bullets), s.FirstDate, s.LastDate, s.Undated})
	}
	wantS := []sess{
		{"s-0001", []string{"alpha-bot", "delta-bot"}, 3, "2000-01-01", "2000-01-05", 0},
		{"s-0002", []string{"alpha-bot", "gamma-bot"}, 1, "2000-01-04", "2000-01-04", 0},
		{"s-0007", []string{"epsilon-bot"}, 1, "2000-01-07", "2000-01-07", 0},
		{"s-0008", []string{"zeta-bot"}, 1, "2000-01-08", "2000-01-08", 0},
		{"ses_example0000000000000000001", []string{"beta-bot"}, 1, "", "", 1},
	}
	if !reflect.DeepEqual(gotS, wantS) {
		t.Errorf("sessions:\n got %v\nwant %v", gotS, wantS)
	}
	// A session's citation ids are the bullets' own, so they resolve through the store.
	ids := store.CitationIDsByStartLine(store.ExtractSections(gadgetOne, nuanceOnly)[store.NuanceHeading])
	if res.Sessions[0].Bullets[0].CitationID != ids[0] || res.Sessions[0].Bullets[1].CitationID != ids[1] {
		t.Errorf("s-0001 citation ids %v do not match the store's %v", res.Sessions[0].Bullets, ids)
	}
}

func TestWritesErrorsAreClassifiable(t *testing.T) {
	root, ix := fixtureStore(t)
	_, err := Writes(root, ix, "no-such-scope")
	var unknown *store.UnknownScopeError
	if !errors.As(err, &unknown) {
		t.Fatalf("unknown scope: got %v, want *store.UnknownScopeError", err)
	}
}

// TestWritesSeesOnlyTheNarrowedIndex: a scope the caller's index excludes answers
// exactly what an absent scope answers, and the positive control — the same scope,
// visible — has edges, so the refusal is not an empty store.
func TestWritesSeesOnlyTheNarrowedIndex(t *testing.T) {
	root, _ := fixtureStore(t)
	narrowed, err := store.LoadStore(root, "read", store.VisibleScopeSet([]string{"beta-notes"}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Writes(root, narrowed, "alpha-notes")
	var unknown *store.UnknownScopeError
	if !errors.As(err, &unknown) {
		t.Fatalf("narrowed-away scope: got %v, want *store.UnknownScopeError", err)
	}
	visible, err := Writes(root, narrowed, "beta-notes")
	if err != nil || len(visible.Edges) != 1 {
		t.Fatalf("positive control: beta-notes edges = %v, err = %v; want 1 edge", visible.Edges, err)
	}
}

// TestTheThreeEmptyStatesAreDistinct: "no entries", "entries but no trailers" and
// "could not read" each produce a different Coverage, so a caller can name them apart.
func TestTheThreeEmptyStatesAreDistinct(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty-notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "plain-notes", "quiet-one.md"),
		"---\nservice: quiet-one\nscope: plain-notes\n---\n\n## Nuance / work-history\n- 2000-01-02: a\n- 2000-01-03: b\n")
	writeFile(t, filepath.Join(root, "gone-notes", "lost-one.md"),
		"---\nservice: lost-one\nscope: gone-notes\n---\n\n## Nuance / work-history\n- 2000-01-02: a [cairn: alpha-bot/s-0001]\n")
	ix, err := store.LoadStore(root, "read", store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "gone-notes", "lost-one.md")); err != nil {
		t.Fatal(err)
	}
	cov := func(scope string) Coverage {
		res, err := Writes(root, ix, scope)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Edges) != 0 || len(res.Sessions) != 0 {
			t.Fatalf("%s: want no edges, got %v", scope, res.Edges)
		}
		return res.Coverage
	}
	if got := cov("empty-notes"); got != (Coverage{}) {
		t.Errorf("empty scope: %+v", got)
	}
	if got := cov("plain-notes"); got != (Coverage{Entries: 1, Bullets: 2}) {
		t.Errorf("untrailered scope: %+v", got)
	}
	if got := cov("gone-notes"); got != (Coverage{Entries: 1, EntriesUnreadable: 1}) {
		t.Errorf("unreadable scope: %+v", got)
	}
}

// TestADuplicateAppendLeavesNoEdge: a second session re-POSTing the same content gets
// `duplicate`, nothing is written, and only the first session has an edge.
func TestADuplicateAppendLeavesNoEdge(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "alpha-notes", "gadget-one.md")
	writeFile(t, path, "---\nservice: gadget-one\nscope: alpha-notes\n---\n\n## Nuance / work-history\n")
	st, _, _, err := write.AppendBullet(path, "the probe lies about readiness", "alpha-bot", "s-0001", "2000-01-02", nil)
	if err != nil || st != "appended" {
		t.Fatalf("first append: %s %v", st, err)
	}
	st, _, _, err = write.AppendBullet(path, "the probe lies about readiness", "beta-bot", "s-0002", "2000-01-03", nil)
	if err != nil || st != "duplicate" {
		t.Fatalf("second append: %s %v, want duplicate", st, err)
	}
	ix, err := store.LoadStore(root, "read", store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	res, err := Writes(root, ix, "alpha-notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sessions) != 1 || res.Sessions[0].ID != "s-0001" || res.Sessions[0].Actors[0] != "alpha-bot" ||
		res.Sessions[0].FirstDate != "2000-01-02" {
		t.Errorf("sessions = %+v, want only s-0001 by alpha-bot on 2000-01-02", res.Sessions)
	}
	if res.Coverage != (Coverage{Entries: 1, Bullets: 1, Attributed: 1}) {
		t.Errorf("coverage = %+v", res.Coverage)
	}
}
