package transcript

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// mineRoots is the listing's roots, in order.
func mineRoots(t *testing.T, s *Store, viewer Viewer) []string {
	t.Helper()
	res, err := s.Mine(viewer)
	if err != nil {
		t.Fatalf("Mine: %v", err)
	}
	var out []string
	for _, o := range res.Sessions {
		out = append(out, o.Root)
	}
	return out
}

// TestMySessionsListsOwnershipNotVisibility is the S7 relationship (clause g, the listing half):
// over [visBuild]'s world — every session owned by `owner`, among them the owner-only s-solo-02,
// s-star-03 and s-lost-07 — plus two sessions `every` uploaded itself:
//
//   - the owner lists all eight of its own, the owner-only ones included, and NOT `every`'s;
//   - `every` reads every scope of s-full-01, s-only-04, s-tree-05, s-decl-06 and s-both-08 and OPENS
//     each (INSTRUMENT CONTROL, asserted below through [Store.View]) — and lists none of them: it
//     lists exactly its own two;
//   - `one` and `none` own nothing and list nothing, though `one` opens s-only-04.
//
// RED under `my-sessions-lists-another-owner` (the listing asks [Store.View] instead of the owner
// arm): `every` then lists the owner's five shared sessions.
func TestMySessionsListsOwnershipNotVisibility(t *testing.T) {
	w := visBuild(t)
	everyKey := string(control.KindUser) + ":" + string(visID(control.PrefixUser, "every"))
	for _, root := range []string{"s-every-11", "s-every-12"} {
		transcripttest.Append(t, w.arc, transcripttest.Session{Root: root, Owner: everyKey}, "main",
			[]string{`{"type":"user","message":{"role":"user","content":"every's own session"}}`})
	}
	s := NewStore(w.arc, w.store)

	// INSTRUMENT CONTROL: `every` really can open the owner's shared sessions, so a listing that went by
	// visibility WOULD list them — the refusal below is ownership's, not the predicate's.
	for _, root := range []string{"s-full-01", "s-only-04", "s-tree-05", "s-decl-06", "s-both-08"} {
		if !w.sees(t, w.viewers["every"], root) {
			t.Fatalf("INSTRUMENT CONTROL: every cannot open %s, so its absence below proves nothing", root)
		}
	}
	if !w.sees(t, w.viewers["one"], "s-only-04") {
		t.Fatal("INSTRUMENT CONTROL: one cannot open s-only-04")
	}

	want := map[string][]string{
		"owner": {"s-both-08", "s-decl-06", "s-full-01", "s-lost-07", "s-only-04", "s-solo-02", "s-star-03", "s-tree-05"},
		"every": {"s-every-11", "s-every-12"},
		"one":   nil,
		"wrote": nil,
		"none":  nil,
	}
	for name, roots := range want {
		got := mineRoots(t, s, w.viewers[name])
		// Every fixture upload is at one clock, so the order is the root-id tie-break; the clock case
		// is TestMySessionsAreNewestUploadFirst.
		if !slices.Equal(got, roots) {
			t.Errorf("%s lists %v, want exactly %v — its OWN sessions, not every session it can open", name, got, roots)
		}
	}
}

// TestMySessionsRefusesANarrowedOwner: the owner's NARROWED credential lists nothing — not the
// owner-only sessions the narrowing withheld, and not the shared ones either (the listing is the
// owner arm, which a narrowed credential never takes). Its un-narrowed credential is the control.
// RED under `transcript-narrowed-owner-arm`, the predicate's own row, since the arm is one function.
func TestMySessionsRefusesANarrowedOwner(t *testing.T) {
	w := visBuild(t)
	s := NewStore(w.arc, w.store)
	owner := w.viewers["owner"]
	if got := mineRoots(t, s, owner); len(got) != 8 {
		t.Fatalf("CONTROL: the un-narrowed owner lists %v, want its eight sessions", got)
	}
	narrowed := owner
	narrowed.Auth = control.Narrow(owner.Auth, []control.ID{visID(control.PrefixScope, "alpha-notes")}, nil)
	if !narrowed.Auth.Narrowed() {
		t.Fatal("CONTROL: the narrowed credential does not report itself narrowed")
	}
	if got := mineRoots(t, s, narrowed); len(got) != 0 {
		t.Errorf("a NARROWED credential of the owner lists %v; it must list nothing", got)
	}
}

// TestMySessionsAreNewestUploadFirst orders by the LAST accepted upload, not the first and not the
// id: three sessions created in one order and last appended to in another, with ids sorting in a
// third. Then a session nobody committed (a bare directory) and one whose meta does not parse: the
// first is no session, the second is counted as unreadable and listed for nobody.
func TestMySessionsAreNewestUploadFirst(t *testing.T) {
	var mu sync.Mutex
	now := time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	tick := func() { mu.Lock(); now = now.Add(time.Hour); mu.Unlock() }
	adir := t.TempDir()
	red, err := redact.New(redact.SelfTestKey(41), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := archive.Open(archive.Config{Dir: adir, Retention: transcripttest.Retention, Quota: 1 << 30, Recheck: red, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	m := visModel(t, nil)
	owner := visViewer(t, m, "owner")
	key := string(control.KindUser) + ":" + string(owner.Principal.ID)
	rec := `{"type":"user","message":{"role":"user","content":"synthetic"}}`
	// Created b, c, a; last appended c, a, b — so newest-last-upload is b, a, c, and newest-created is a.
	for _, root := range []string{"s-b", "s-c", "s-a"} {
		transcripttest.Append(t, a, transcripttest.Session{Root: root, Owner: key}, "main", []string{rec})
		tick()
	}
	for _, root := range []string{"s-c", "s-a", "s-b"} {
		transcripttest.Append(t, a, transcripttest.Session{Root: root, Owner: key}, "main", []string{rec})
		tick()
	}
	s := NewStore(a, t.TempDir())
	if got, want := mineRoots(t, s, owner), []string{"s-b", "s-a", "s-c"}; !slices.Equal(got, want) {
		t.Errorf("the listing is %v, want %v — newest LAST upload first", got, want)
	}

	// A bare directory and a corrupt meta, in the archive's own layout (`sessions/<root>`).
	dir := filepath.Join(adir, "sessions")
	if _, err := os.Stat(filepath.Join(dir, "s-a", "meta.json")); err != nil {
		t.Fatalf("INSTRUMENT CONTROL: the layout is not sessions/<root>/meta.json: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "s-bare"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "s-torn"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "s-torn", "meta.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := s.Mine(owner)
	if err != nil {
		t.Fatalf("one corrupt meta failed the whole listing: %v", err)
	}
	if res.Unreadable != 1 || len(res.Sessions) != 3 {
		t.Errorf("Mine = %d sessions, %d unreadable; want the three, and the corrupt one counted", len(res.Sessions), res.Unreadable)
	}
}

// TestNoArchiveOwnsNothing: a deployment with no transcript store lists nothing for anybody, and
// says so with no error — the S4 convention that such a deployment serves what it would with
// nothing stored.
func TestNoArchiveOwnsNothing(t *testing.T) {
	m := visModel(t, nil)
	for name, s := range map[string]*Store{"nil store": nil, "nil archive": NewStore(nil, t.TempDir())} {
		res, err := s.Mine(visViewer(t, m, "owner"))
		if err != nil || len(res.Sessions) != 0 || res.Unreadable != 0 {
			t.Errorf("%s: Mine = %+v, %v; want nothing and no error", name, res, err)
		}
	}
}
