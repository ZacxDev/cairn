package archive

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// The arming preconditions AP1–AP3 of `claudedocs/plan-cairn-plugins.md` ("### Rollout"), each
// pinned by its own test. Every fixture is synthetic (sessions `s-00NN`, host `host-a`, year 2000);
// secret-shaped values are generated at run time.

// populated returns a rig whose directory holds everything the store itself writes: a committed
// session, a journal line from a retention sweep, and a staged frame.
func populated(t *testing.T) *rig {
	t.Helper()
	g := newRig(t, 1<<30)
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "1"), "main", recs(0, 1)); err != nil {
		t.Fatal(err)
	}
	g.clk.Add(91 * 24 * time.Hour)
	if deleted, err := g.a.Sweep(); err != nil || len(deleted) != 1 {
		t.Fatalf("POSITIVE CONTROL: the sweep deleted %v (%v), so no journal line exists", deleted, err)
	}
	if _, err := g.a.AppendRecords(ownerA, up("s-0002", "", "1"), "main", recs(0, 1)); err != nil {
		t.Fatal(err)
	}
	u := up("s-0003", "", "1")
	u.Frame = &Frame{Index: 0, Count: 2}
	if _, err := g.a.AppendRecords(ownerA, u, "main", []Record{{Src: "0", Rec: []byte(`{"half":`)}}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{journalName, sessionsDir, framesDir} {
		if _, err := os.Stat(filepath.Join(g.dir, p)); err != nil {
			t.Fatalf("POSITIVE CONTROL: the store's own directory lacks %s (%v)", p, err)
		}
	}
	return g
}

// TestOpenAcceptsAFreshAndItsOwnDirectoryAndNothingElse is AP1 at the store: an EMPTY directory and
// the store's OWN populated directory (a restart) open; a directory that also holds `cairn-ui`'s
// state — the control journal under the store's journal name, the browser session FILE under the
// store's `sessions` name, any other entry — is refused with ErrForeignEntry and left BYTE-
// UNCHANGED (a foreign `frames` is not removed by the open that refuses it).
//
// RED on the S3 head: `Open` checked nothing, so every foreign case opened.
func TestOpenAcceptsAFreshAndItsOwnDirectoryAndNothingElse(t *testing.T) {
	_ = newRig(t, 1<<30) // a fresh, empty directory opens: the rig would have failed otherwise
	g := populated(t)
	if _, err := Open(g.a.cfg); err != nil {
		t.Fatalf("the store's own directory was refused on reopen: %v", err)
	}

	// A control journal line: `kind`, never `event` (internal/control/journal.go).
	controlLine := `{"kind":"user-created","at":"2000-01-02T03:04:05Z","user_id":"usr_fixture","provider":"fixture",` +
		`"subject":"00000000-0000-4000-8000-000000000001","email":"fixture@notes.example.invalid"}` + "\n"
	ownLine := `{"event":"transcript-deleted","root":"s-0001","owner":"user:u_alpha","host":"host-a","reason":"retention",` +
		`"at":"2000-04-01T03:04:06Z"}` + "\n"
	for _, tc := range []struct {
		name  string
		plant func(dir string) error
		why   string
	}{
		{"the control journal under the store's journal name", func(d string) error {
			return os.WriteFile(filepath.Join(d, journalName), []byte(controlLine), 0o600)
		}, "line 1 is not a transcript deletion record"},
		{"a control line appended to the store's own journal", func(d string) error {
			return os.WriteFile(filepath.Join(d, journalName), []byte(ownLine+controlLine), 0o600)
		}, "line 2 is not a transcript deletion record"},
		{"a torn final journal line", func(d string) error {
			return os.WriteFile(filepath.Join(d, journalName), []byte(ownLine+`{"event":"transcript-del`), 0o600)
		}, "not terminated"},
		{"the browser session FILE under the store's sessions name", func(d string) error {
			return os.WriteFile(filepath.Join(d, sessionsDir), []byte(`{"sessions":[]}`+"\n"), 0o600)
		}, "a browser session table is a file"},
		{"a control journal under its own name", func(d string) error {
			return os.WriteFile(filepath.Join(d, "control.journal"), []byte(controlLine), 0o600)
		}, "not one of journal.jsonl, sessions/ or frames/"},
		{"a file inside sessions/", func(d string) error {
			if err := os.MkdirAll(filepath.Join(d, sessionsDir), 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(d, sessionsDir, "s-0001"), []byte("x"), 0o600)
		}, `it holds "s-0001"`},
		{"a foreign file inside frames/", func(d string) error {
			if err := os.MkdirAll(filepath.Join(d, framesDir), 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(d, framesDir, "keep-me.txt"), []byte("not a staged frame"), 0o600)
		}, `it holds "keep-me.txt"`},
		{"a symlink", func(d string) error {
			return os.Symlink(os.TempDir(), filepath.Join(d, sessionsDir))
		}, "a symlink"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newRig(t, 1<<30)
			dir := t.TempDir()
			if err := tc.plant(dir); err != nil {
				t.Fatal(err)
			}
			before := treeDigest(t, dir)
			cfg := g.a.cfg
			cfg.Dir = dir
			_, err := Open(cfg)
			if !errors.Is(err, ErrForeignEntry) || !strings.Contains(err.Error(), tc.why) {
				t.Fatalf("Open answered %v, want ErrForeignEntry saying %q", err, tc.why)
			}
			if after := treeDigest(t, dir); after != before {
				t.Fatal("the refusing open changed the directory")
			}
		})
	}
}

// pad builds a record of EXACTLY n bytes.
func pad(n int) []byte {
	head, tail := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"`, `"}]}}`
	return []byte(head + strings.Repeat("a", n-len(head)-len(tail)) + tail)
}

// sendFrames sends rec as `count` frames of at most `size` bytes and returns the last error.
func sendFrames(g *rig, root string, rec []byte, size int) error {
	count := (len(rec) + size - 1) / size
	var err error
	for i := 0; i < count; i++ {
		u := up(root, "", "1")
		u.Frame = &Frame{Index: i, Count: count}
		if _, err = g.a.AppendRecords(ownerA, u, "main", []Record{{Src: "0", Rec: rec[i*size : min(len(rec), (i+1)*size)]}}); err != nil {
			return err
		}
	}
	return err
}

// TestAnAssembledRecordOverTheCapIsRefusedBeforeItIsRead is AP2 at the store: a framed record of
// EXACTLY [MaxAssembledBytes] is stored (the boundary belongs to the record); one byte more is
// refused with TooLargeError on the frame that crosses, nothing is stored, the staged frames are
// gone and their bytes are back in the quota. An unframed record or blob over the cap is refused
// the same way.
//
// RED on the S3 head: no bound existed, so the over-cap record was stored.
func TestAnAssembledRecordOverTheCapIsRefusedBeforeItIsRead(t *testing.T) {
	g := newRig(t, 1<<30)
	if err := sendFrames(g, "s-0001", pad(MaxAssembledBytes), 1<<20); err != nil {
		t.Fatalf("POSITIVE CONTROL: a record of exactly the cap was refused: %v", err)
	}
	stored := g.a.Used()

	var tl *TooLargeError
	if err := sendFrames(g, "s-0002", pad(MaxAssembledBytes+1), 1<<20); !errors.As(err, &tl) || tl.Limit != MaxAssembledBytes {
		t.Fatalf("a record one byte over the cap answered %v, want TooLargeError{%d}", err, MaxAssembledBytes)
	}
	if m, _ := g.a.ReadMeta("s-0002"); m != nil {
		t.Fatal("the over-cap record created its session")
	}
	if g.a.Used() != stored {
		t.Fatalf("after the refusal the quota counts %d bytes, want the %d stored before it", g.a.Used(), stored)
	}
	if entries, _ := os.ReadDir(filepath.Join(g.dir, framesDir)); len(entries) != 0 {
		t.Fatalf("the refused record left %d staged frame file(s)", len(entries))
	}
	if _, err := g.a.AppendRecords(ownerA, up("s-0003", "", "1"), "main", []Record{{Src: "0", Rec: pad(MaxAssembledBytes + 1)}}); !errors.As(err, &tl) {
		t.Fatalf("an unframed record over the cap answered %v", err)
	}
	if _, err := g.a.PutBlob(ownerA, up("s-0003", "", "1"), "toolu_x.txt", make([]byte, MaxAssembledBytes+1)); !errors.As(err, &tl) {
		t.Fatalf("an unframed blob over the cap answered %v", err)
	}
	if roots, _ := g.a.Roots(); !slices.Equal(roots, []string{"s-0001"}) {
		t.Fatalf("stored roots %v, want only the at-cap control", roots)
	}
}

// assemblyBudget is what ONE assembly at the cap may allocate: under half the pod's 256 Mi limit,
// so two roots assembling at once still leave the rest of the process room.
const assemblyBudget = 100 << 20

// TestTheAssembledRecordCapFitsThePod measures what the FINAL frame of a record of exactly
// [MaxAssembledBytes] allocates — the staged read, the append, the compaction, the re-check walk
// and the gzip — with the collector's work excluded from nothing: TotalAlloc is every byte
// allocated, an UPPER bound on the peak live heap. Measured at 1, 2 and 4 MiB: 18.8×, 18.2×,
// 17.9× the record (≈72 MiB at the cap), linear. It fails if the cap is raised, or the assembly
// path grows, past [assemblyBudget].
func TestTheAssembledRecordCapFitsThePod(t *testing.T) {
	g := newRig(t, 1<<40)
	r := pad(MaxAssembledBytes)
	half := len(r)/2 + 1
	u := up("s-0001", "", "1")
	u.Frame = &Frame{Index: 0, Count: 2}
	if _, err := g.a.AppendRecords(ownerA, u, "main", []Record{{Src: "0", Rec: r[:half]}}); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	u.Frame = &Frame{Index: 1, Count: 2}
	if _, err := g.a.AppendRecords(ownerA, u, "main", []Record{{Src: "0", Rec: r[half:]}}); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	got := after.TotalAlloc - before.TotalAlloc
	if got < MaxAssembledBytes {
		t.Fatalf("POSITIVE CONTROL: the assembly allocated %d bytes, less than the record itself — the measurement saw nothing", got)
	}
	if got > assemblyBudget {
		t.Fatalf("one assembly at the %d-byte cap allocated %.1f MiB, over the %d MiB budget for a 256 Mi pod",
			MaxAssembledBytes, float64(got)/(1<<20), assemblyBudget>>20)
	}
	t.Logf("one assembly at the cap allocated %.1f MiB (%.1f× the record)", float64(got)/(1<<20), float64(got)/MaxAssembledBytes)
}

// TestASessionWithNoMetaIsSweptByRetention is AP3's first item: a session directory no upload
// committed (no `meta.json`) whose newest write is past the retention is swept, its bytes leave
// the quota, and the journal names the root with the reason and NO owner; one whose newest write
// is inside the retention is kept.
//
// RED on the S3 head: `Sweep` skipped every root without meta, forever.
func TestASessionWithNoMetaIsSweptByRetention(t *testing.T) {
	g := newRig(t, 1<<30)
	orphan := func(root string, at time.Time) {
		dir := filepath.Join(g.dir, sessionsDir, root)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, streamPrefix+"main"+streamSuffix)
		if err := os.WriteFile(p, []byte(strings.Repeat("z", 5000)), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, q := range []string{p, dir} {
			if err := os.Chtimes(q, at, at); err != nil {
				t.Fatal(err)
			}
		}
	}
	orphan("s-0001", clock0)
	g.clk.Add(90*24*time.Hour + time.Second)
	orphan("s-0002", g.clk.Now().Add(-time.Hour))
	// Reopen, so the orphans are counted against the quota exactly as after a restart.
	a, err := Open(g.a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if a.Used() != 10000 {
		t.Fatalf("POSITIVE CONTROL: the reopened store counts %d bytes, want the two orphans' 10000", a.Used())
	}
	deleted, err := a.Sweep()
	if err != nil || !slices.Equal(deleted, []string{"s-0001"}) {
		t.Fatalf("the sweep deleted %v (%v), want only the stale orphan", deleted, err)
	}
	if _, err := os.Stat(filepath.Join(g.dir, sessionsDir, "s-0001")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the stale orphan's directory survived (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(g.dir, sessionsDir, "s-0002")); err != nil {
		t.Fatalf("the fresh orphan was deleted (%v)", err)
	}
	j, _ := os.ReadFile(filepath.Join(g.dir, journalName))
	want := fmt.Sprintf(`{"event":"transcript-deleted","root":"s-0001","owner":"","host":"","reason":%q,"at":"2000-04-01T03:04:06Z"}`+"\n",
		ReasonRetentionNoMeta)
	if string(j) != want {
		t.Fatalf("the journal is\n%s\nwant\n%s", j, want)
	}
	if a.Used() != 5000+int64(len(want)) {
		t.Fatalf("after the sweep the quota counts %d bytes, want the fresh orphan's 5000 plus the journal's %d", a.Used(), len(want))
	}
	if _, err := Open(a.cfg); err != nil {
		t.Fatalf("the store refused its own directory after an uncommitted sweep: %v", err)
	}
}

// TestAbandonedFramesStopHoldingQuotaWithoutARestart is AP3's second item: a staged sequence idle
// for more than [FrameIdle] is discarded by the sweep and its bytes leave the quota; a sequence
// staged inside it is kept and still completes.
//
// RED on the S3 head: staged bytes were returned only by a restart (or by the same target's next
// frame).
func TestAbandonedFramesStopHoldingQuotaWithoutARestart(t *testing.T) {
	g := newRig(t, 1<<30)
	stage := func(root string, i int, chunk string) error {
		u := up(root, "", "1")
		u.Frame = &Frame{Index: i, Count: 2}
		_, err := g.a.AppendRecords(ownerA, u, "main", []Record{{Src: "0", Rec: []byte(chunk)}})
		return err
	}
	abandoned := `{"abandoned":"` + strings.Repeat("q", 3000)
	if err := stage("s-0001", 0, abandoned); err != nil {
		t.Fatal(err)
	}
	g.clk.Add(FrameIdle / 2)
	kept := `{"kept":`
	if err := stage("s-0002", 0, kept); err != nil {
		t.Fatal(err)
	}
	if got := g.a.Used(); got != int64(len(abandoned)+len(kept)) {
		t.Fatalf("POSITIVE CONTROL: %d staged bytes counted, want %d", got, len(abandoned)+len(kept))
	}
	g.clk.Add(FrameIdle/2 + time.Second)
	if _, err := g.a.Sweep(); err != nil {
		t.Fatal(err)
	}
	if got := g.a.Used(); got != int64(len(kept)) {
		t.Fatalf("after the sweep %d staged bytes are counted, want only the live sequence's %d", got, len(kept))
	}
	if entries, _ := os.ReadDir(filepath.Join(g.dir, framesDir)); len(entries) != 1 {
		t.Fatalf("%d staged frame file(s) remain, want 1", len(entries))
	}
	if err := stage("s-0002", 1, `"done"}`); err != nil {
		t.Fatalf("the live sequence no longer completes: %v", err)
	}
	if err := stage("s-0001", 1, `"x"}`); err == nil {
		t.Fatal("the abandoned sequence still completed after the sweep discarded it")
	}
}

// TestTheRecheckReadsNamesSrcAndCursors is AP3's third item: a runtime-generated 40-character
// token — which fits the blob-name, stream-id, session-id, scope and cursor patterns — in ANY
// request string is refused by the pod's re-check, naming the FIELD and never the value, and
// nothing is stored. The same uploads with clean values are stored (positive control).
//
// RED on the S3 head: only record and blob BYTES were re-checked, so every one of these stored.
func TestTheRecheckReadsNamesSrcAndCursors(t *testing.T) {
	tok := randToken(17)
	if len(tok) != 40 || !blobName.MatchString(tok) || !write40(tok) {
		t.Fatalf("POSITIVE CONTROL: the fixture %d-char token does not fit the name patterns", len(tok))
	}
	type call func(a *Archive, v string) error
	records := func(mod func(*Upload, *string, *string, *string)) call {
		return func(a *Archive, v string) error {
			u, stream, src := up("s-0001", "", "100"), "main", "0"
			mod(&u, &stream, &src, &v)
			_, err := a.AppendRecords(ownerA, u, stream, []Record{{Src: src, Rec: rec(1, "synthetic").Rec}})
			return err
		}
	}
	for _, tc := range []struct {
		field, clean string
		do           call
	}{
		{"blob name", "toolu_01AbCdEfGhIjKlMnOpQrStUv.txt", func(a *Archive, v string) error {
			_, err := a.PutBlob(ownerA, up("s-0001", "", "1"), v, []byte("synthetic blob"))
			return err
		}},
		{"record 0 src", "1200", records(func(_ *Upload, _, src, v *string) { *src = *v })},
		{"from", "", records(func(u *Upload, _, _, v *string) { u.From = *v })},
		{"to", "1300", records(func(u *Upload, _, _, v *string) { u.To = *v })},
		{"stream", "agent-0001", records(func(_ *Upload, stream, _, v *string) { *stream = "subagent:" + *v })},
		{"root", "s-0009", records(func(u *Upload, _, _, v *string) { u.Root = *v })},
		{"declared scope 0", "alpha-notes", records(func(u *Upload, _, _, v *string) { u.Declared = []string{*v} })},
	} {
		t.Run(tc.field, func(t *testing.T) {
			g := newRig(t, 1<<30)
			err := tc.do(g.a, tok)
			var rc *RecheckError
			if !errors.As(err, &rc) || rc.Field != tc.field || rc.Blob != "" {
				t.Fatalf("a token in the %s answered %v, want the re-check naming that field", tc.field, err)
			}
			if strings.Contains(err.Error(), tok) {
				t.Fatal("the refusal echoes the value")
			}
			if roots, _ := g.a.Roots(); len(roots) != 0 {
				t.Fatalf("a refused request stored %v", roots)
			}
			if err := tc.do(g.a, tc.clean); err != nil {
				t.Fatalf("POSITIVE CONTROL: the clean %s %q was refused: %v", tc.field, tc.clean, err)
			}
		})
	}
}

// write40 is the session-id class a root and a stream id are matched against.
func write40(s string) bool { return streamName.MatchString("subagent:"+s) && scopeName.MatchString(s) }
