package archive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/redact"
)

// Plugin outputs: the caps' running totals, and the deletion cascade. Synthetic throughout — sessions
// `s-0001`/`s-0002`, plugins `summary`/`clickup`, lines of filler.

func outputLine(plugin string, kib int) []byte {
	return []byte(`{"plugin":"` + plugin + `","body":"` + strings.Repeat("y", kib<<10) + `"}`)
}

// TestTheOutputTotalsFollowDeletionsAndARestart — #270's audit, closed in S6. The caps are checked
// against a RUNNING TOTAL that the archive counts off the disk once, when it opens (`outputUsage` has
// that ONE caller; as first built, every write that would cross a cap re-read every session's
// `outputs.jsonl` before refusing). The totals stay TRUE without rescanning: a deleted session's
// outputs come out of them — the next write is accepted — and a restart counts what is on disk, so
// the next write is refused exactly where it should be.
func TestTheOutputTotalsFollowDeletionsAndARestart(t *testing.T) {
	const quota = 400 << 10 // one plugin's share is 20 KiB
	g := newRig(t, quota)
	for _, root := range []string{"s-0001", "s-0002"} {
		if _, err := g.a.AppendRecords(ownerA, up(root, "", "100"), "main", recs(0, 1)); err != nil {
			t.Fatal(err)
		}
	}
	// Two 9 KiB lines fit the 20 KiB share; the third does not, however often it is tried.
	for i := 0; i < 2; i++ {
		if err := g.a.AppendOutput("s-0001", "summary", outputLine("summary", 9)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	var q *QuotaError
	for i := 0; i < 3; i++ {
		err := g.a.AppendOutput("s-0002", "summary", outputLine("summary", 9))
		if !errors.As(err, &q) || q.Share != SharePlugin || q.Quota != quota/OutputPluginShareDivisor {
			t.Fatalf("refused write %d: %v — want the plugin's share (%d bytes)", i, err, quota/OutputPluginShareDivisor)
		}
	}
	// Another plugin is not refused by this one's share (the totals are per plugin).
	if err := g.a.AppendOutput("s-0002", "clickup", outputLine("clickup", 9)); err != nil {
		t.Fatalf("another plugin was refused by the first one's share: %v", err)
	}
	// A DELETION takes exactly that session's outputs out of the totals.
	if ok, err := g.a.Delete("s-0001", ReasonOwnerDelete, nil); err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
	for i := 0; i < 2; i++ {
		if err := g.a.AppendOutput("s-0002", "summary", outputLine("summary", 9)); err != nil {
			t.Fatalf("after the session holding its outputs was deleted, write %d was still refused: %v", i, err)
		}
	}
	if err := g.a.AppendOutput("s-0002", "summary", outputLine("summary", 9)); !errors.As(err, &q) {
		t.Fatalf("the share is not enforced after a deletion: %v", err)
	}
	// A RESTART counts what is on disk: `summary` is at its share, `clickup` is not.
	red, err := redact.New(redact.SelfTestKey(99), nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Open(Config{Dir: g.dir, Retention: 90 * 24 * time.Hour, Quota: quota, Recheck: red, Now: g.clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	if err := again.AppendOutput("s-0002", "summary", outputLine("summary", 9)); !errors.As(err, &q) || q.Share != SharePlugin {
		t.Fatalf("after a restart the plugin's share is not what the disk holds: %v", err)
	}
	if err := again.AppendOutput("s-0002", "clickup", outputLine("clickup", 9)); err != nil {
		t.Fatalf("after a restart another plugin's write was refused: %v", err)
	}
}

// TestTheQuotaRefusalSaysWhichLimit: the three limits an output write can hit are told apart by
// `QuotaError.Share`, and a capture upload's refusal is always the instance's.
func TestTheQuotaRefusalSaysWhichLimit(t *testing.T) {
	const quota = 400 << 10
	g := newRig(t, quota)
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "100"), "main", recs(0, 1)); err != nil {
		t.Fatal(err)
	}
	var q *QuotaError
	// All plugins together: four at 19 KiB (76 KiB of the 80 KiB all-plugins share), then a fifth.
	for _, p := range []string{"p-1", "p-2", "p-3", "p-4"} {
		if err := g.a.AppendOutput("s-0001", p, outputLine(p, 19)); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.a.AppendOutput("s-0001", "p-5", outputLine("p-5", 9)); !errors.As(err, &q) || q.Share != ShareOutputs ||
		q.Quota != quota/OutputTotalShareDivisor {
		t.Fatalf("a fifth plugin's write: %v — want all plugins' share (%d bytes)", err, quota/OutputTotalShareDivisor)
	}
	// The instance: a binary blob that leaves 2 KiB, then a 1 KiB output from a plugin with room.
	fill := make([]byte, quota-g.a.Used()-(2<<10))
	for i := range fill {
		fill[i] = byte(i * 7)
	}
	if _, err := g.a.PutBlob(ownerA, up("s-0001", "", "v1"), "fill.bin", fill); err != nil {
		t.Fatal(err)
	}
	if err := g.a.AppendOutput("s-0001", "p-5", outputLine("p-5", 1)); err != nil {
		t.Fatalf("control: a 1 KiB output with 2 KiB of instance room was refused: %v", err)
	}
	if err := g.a.AppendOutput("s-0001", "p-5", outputLine("p-5", 2)); !errors.As(err, &q) || q.Share != ShareInstance || q.Quota != quota {
		t.Fatalf("an output past the INSTANCE quota: %v (share %q) — want the instance quota", err, q.Share)
	}
	if _, err := g.a.PutBlob(ownerA, up("s-0001", "", "v1"), "more.bin", fill[:8<<10]); !errors.As(err, &q) || q.Share != ShareInstance {
		t.Fatalf("a capture upload past the quota: %v — want the instance quota", err)
	}
}

// TestTheDeletionCascadeTakesEverythingDerived is clause (l) at the archive: one deletion removes the
// session's directory — its streams, its blobs, and `outputs.jsonl`, every summary AND ticket edge in
// it — discards a continuation-frame sequence staged for the root (kept OUTSIDE the directory),
// returns every byte to the quota, and journals the fact: which root, whose, why, when — no content.
// A second session is untouched. Mutant `plugins-delete-keeps-outputs`.
func TestTheDeletionCascadeTakesEverythingDerived(t *testing.T) {
	g := newRig(t, 1<<30)
	before := g.a.Used()
	for _, root := range []string{"s-0001", "s-0002"} {
		if _, err := g.a.AppendRecords(ownerA, up(root, "", "100"), "main", recs(0, 1)); err != nil {
			t.Fatal(err)
		}
		if _, err := g.a.PutBlob(ownerA, up(root, "", "v1"), "toolu_x.txt", []byte("synthetic output\n")); err != nil {
			t.Fatal(err)
		}
		for plugin, line := range map[string]string{
			"summary": `{"plugin":"summary","type":"summary","body":{"text":"a synthetic summary of ` + root + `"}}`,
			"clickup": `{"plugin":"clickup","type":"ticket-edge","body":{"system":"clickup","ticket_id":"abc123"}}`,
		} {
			if err := g.a.AppendOutput(root, plugin, []byte(line)); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A staged frame sequence for s-0001: frame 0 of 2 of its next record.
	framed := up("s-0001", "100", "200")
	framed.Frame = &Frame{Index: 0, Count: 2}
	if st, err := g.a.AppendRecords(ownerA, framed, "main", []Record{{Src: "100", Rec: []byte(`{"type":"user","mess`)}}); err != nil || !st.Pending {
		t.Fatalf("staging a frame: %+v %v", st, err)
	}
	staged, _ := os.ReadDir(filepath.Join(g.dir, "frames"))
	if len(staged) != 1 {
		t.Fatalf("control: %d staged frame files, want 1", len(staged))
	}
	g.clk.Add(time.Minute)
	ok, err := g.a.Delete("s-0001", ReasonOwnerDelete, func(m *Meta) bool { return m.Owner == ownerA.Owner })
	if err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(g.dir, "sessions", "s-0001")); !os.IsNotExist(err) {
		t.Fatalf("the session's directory survived its deletion: %v", err)
	}
	if g.a.HasOutputs("s-0001") {
		t.Fatal("the deleted session's outputs — its summary and its ticket edge — survived it")
	}
	if staged, _ := os.ReadDir(filepath.Join(g.dir, "frames")); len(staged) != 0 {
		t.Fatalf("%d staged frame file(s) of the deleted session are still on disk", len(staged))
	}
	raw, err := os.ReadFile(filepath.Join(g.dir, "journal.jsonl"))
	want := `{"event":"transcript-deleted","root":"s-0001","owner":"user:u_alpha","host":"host-a","reason":"owner-delete","at":"2000-01-02T03:05:05Z"}` + "\n"
	if err != nil || string(raw) != want {
		t.Fatalf("the journal holds\n%s(%v)\nwant\n%s", raw, err, want)
	}
	// Nothing of s-0002 moved, and the quota holds exactly it plus the journal line.
	if m, _ := g.a.ReadMeta("s-0002"); m == nil || !g.a.HasOutputs("s-0002") {
		t.Fatal("deleting s-0001 touched s-0002")
	}
	onDisk, err := treeBytes(g.dir)
	if err != nil || g.a.Used() != onDisk || g.a.Used() <= before {
		t.Fatalf("the quota counts %d bytes and the directory holds %d (%v)", g.a.Used(), onDisk, err)
	}
	// A predicate that says no deletes nothing; neither does a root that is not stored.
	if ok, err := g.a.Delete("s-0002", ReasonOwnerDelete, func(*Meta) bool { return false }); ok || err != nil {
		t.Fatalf("a refused delete answered %v %v", ok, err)
	}
	if ok, err := g.a.Delete("s-never", ReasonOwnerDelete, nil); ok || err != nil {
		t.Fatalf("deleting a root that is not stored answered %v %v", ok, err)
	}
	if m, _ := g.a.ReadMeta("s-0002"); m == nil {
		t.Fatal("a refused delete removed the session")
	}
}

// TestAWithdrawalIsOwnerAndHostBoundAndIdempotent is `Archive.Withdraw`'s truth table (decision 16):
// true for the caller's own (owner, host)'s root — deleted — and again on a repeat; false, deleting
// nothing, for another owner's root, the same owner's from another host label, a root never stored,
// and an id that is no session id; and true for a root the retention sweep already removed for the
// same uploader (the prefix is gone, which is what the agent asked).
func TestAWithdrawalIsOwnerAndHostBoundAndIdempotent(t *testing.T) {
	g := newRig(t, 1<<30)
	for root, who := range map[string]Uploader{"s-0001": ownerA, "s-0002": ownerB, "s-0003": hostB, "s-0004": ownerA} {
		if _, err := g.a.AppendRecords(who, up(root, "", "100"), "main", recs(0, 1)); err != nil {
			t.Fatal(err)
		}
	}
	for name, root := range map[string]string{"another owner's root": "s-0002", "the same owner's root from another host": "s-0003",
		"a root never stored": "s-never", "an id that is not a session id": "../s-0001"} {
		if deleted, err := g.a.Withdraw(ownerA, root); deleted || err != nil {
			t.Errorf("%s: Withdraw = %v, %v — want false", name, deleted, err)
		}
	}
	for _, root := range []string{"s-0001", "s-0002", "s-0003", "s-0004"} {
		if m, _ := g.a.ReadMeta(root); m == nil {
			t.Fatalf("a refused withdrawal deleted %s", root)
		}
	}
	for i := 0; i < 3; i++ {
		if deleted, err := g.a.Withdraw(ownerA, "s-0001"); !deleted || err != nil {
			t.Fatalf("the uploader's own withdrawal, call %d: %v, %v — want true every time", i+1, deleted, err)
		}
	}
	if m, _ := g.a.ReadMeta("s-0001"); m != nil {
		t.Fatal("the withdrawn root is still stored")
	}
	raw, _ := os.ReadFile(filepath.Join(g.dir, "journal.jsonl"))
	if n := strings.Count(string(raw), "\n"); n != 1 || !strings.Contains(string(raw), `"reason":"withdrawn"`) {
		t.Fatalf("three withdrawals of one root journaled %d line(s):\n%s", n, raw)
	}
	// It stays false for everyone who never held it.
	for name, who := range map[string]Uploader{"another host": hostB, "another owner": ownerB} {
		if deleted, _ := g.a.Withdraw(who, "s-0001"); deleted {
			t.Errorf("%s was answered true for a root it never held", name)
		}
	}
	// Retention removes s-0004; its uploader's withdrawal afterwards is true — the prefix is gone.
	g.clk.Add(91 * 24 * time.Hour)
	if _, err := g.a.Sweep(); err != nil {
		t.Fatal(err)
	}
	if deleted, err := g.a.Withdraw(ownerA, "s-0004"); !deleted || err != nil {
		t.Fatalf("a root retention already removed for this uploader: %v, %v — want true", deleted, err)
	}
	if deleted, _ := g.a.Withdraw(ownerA, "s-0002"); deleted {
		t.Fatal("a root retention removed for ANOTHER owner was answered true")
	}
}

// TestADeletionIsFinalForItsRoot is `Archive.gone` (#273's audit, F2) at the archive, per reason and
// across a restart. Every deletion of a COMMITTED session — the owner's, a withdrawal, retention —
// is final for its root: its own uploader is refused `ErrDeleted` on records, blobs and frames,
// anybody else the uniform `ErrNotOwner`, and nothing is created. A directory no upload ever
// committed (`retention-uncommitted`) held no session, and its root may still begin.
func TestADeletionIsFinalForItsRoot(t *testing.T) {
	g := newRig(t, 1<<30)
	for _, root := range []string{"s-owner", "s-withdrawn", "s-swept"} {
		if _, err := g.a.AppendRecords(ownerA, up(root, "", "100"), "main", recs(0, 1)); err != nil {
			t.Fatal(err)
		}
	}
	// A directory with no meta: created, never committed.
	uncommitted := filepath.Join(g.dir, "sessions", "s-uncommitted")
	if err := os.MkdirAll(uncommitted, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(uncommitted, clock0, clock0); err != nil { // its clock is its own mtime
		t.Fatal(err)
	}
	if ok, err := g.a.Delete("s-owner", ReasonOwnerDelete, nil); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := g.a.Withdraw(ownerA, "s-withdrawn"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	g.clk.Add(91 * 24 * time.Hour)
	if swept, err := g.a.Sweep(); err != nil || len(swept) != 2 {
		t.Fatalf("sweep: %v %v — want s-swept and s-uncommitted", swept, err)
	}
	check := func(a *Archive, when string) {
		t.Helper()
		for _, root := range []string{"s-owner", "s-withdrawn", "s-swept"} {
			if _, err := a.AppendRecords(ownerA, up(root, "", "100"), "subagent:late01", recs(0, 1)); !errors.Is(err, ErrDeleted) {
				t.Errorf("%s: a new stream of %s: %v, want ErrDeleted", when, root, err)
			}
			if _, err := a.PutBlob(ownerA, up(root, "", "v1"), "toolu_new.txt", []byte("synthetic\n")); !errors.Is(err, ErrDeleted) {
				t.Errorf("%s: a new blob of %s: %v, want ErrDeleted", when, root, err)
			}
			framed := up(root, "", "100")
			framed.Frame = &Frame{Index: 0, Count: 2}
			if _, err := a.AppendRecords(ownerA, framed, "main", []Record{{Src: "0", Rec: []byte(`{"a":`)}}); !errors.Is(err, ErrDeleted) {
				t.Errorf("%s: a first frame of %s: %v, want ErrDeleted", when, root, err)
			}
			for name, who := range map[string]Uploader{"another host": hostB, "another owner": ownerB} {
				if _, err := a.AppendRecords(who, up(root, "", "100"), "main", recs(0, 1)); !errors.Is(err, ErrNotOwner) {
					t.Errorf("%s: %s uploading %s: %v, want the uniform ErrNotOwner", when, name, root, err)
				}
			}
			if m, _ := a.ReadMeta(root); m != nil {
				t.Errorf("%s: %s is stored again", when, root)
			}
			if _, err := os.Stat(filepath.Join(g.dir, "sessions", root)); !os.IsNotExist(err) {
				t.Errorf("%s: %s's directory exists again (%v)", when, root, err)
			}
		}
	}
	check(g.a, "in the process that deleted them")
	red, err := redact.New(redact.SelfTestKey(99), nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Open(Config{Dir: g.dir, Retention: 90 * 24 * time.Hour, Quota: 1 << 30, Recheck: red, Now: g.clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	check(again, "after a restart")
	// The uncommitted directory's root, and one never seen, may begin.
	for _, root := range []string{"s-uncommitted", "s-fresh"} {
		if _, err := again.AppendRecords(ownerA, up(root, "", "100"), "main", recs(0, 1)); err != nil {
			t.Errorf("a first upload of %s was refused: %v", root, err)
		}
	}
}

// TestOneUnreadableOutputsFileDoesNotStopTheStoreOpening: a session whose `outputs.jsonl` cannot be
// scanned (here, one line past the bound) is counted at its on-disk size against no plugin — the
// archive OPENS, the bytes still weigh on the all-plugins share, and the session can be deleted.
func TestOneUnreadableOutputsFileDoesNotStopTheStoreOpening(t *testing.T) {
	const quota = 4 << 20 // all plugins' share is ~819 KiB
	g := newRig(t, quota)
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "100"), "main", recs(0, 1)); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(g.dir, "sessions", "s-0001", "outputs.jsonl")
	if err := os.WriteFile(broken, []byte(strings.Repeat("x", 800<<10)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	red, err := redact.New(redact.SelfTestKey(99), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Open(Config{Dir: g.dir, Retention: 90 * 24 * time.Hour, Quota: quota, Recheck: red, Now: g.clk.Now})
	if err != nil {
		t.Fatalf("one unreadable outputs.jsonl refused the whole store: %v", err)
	}
	var q *QuotaError
	if err := a.AppendOutput("s-0001", "summary", outputLine("summary", 30)); !errors.As(err, &q) || q.Share != ShareOutputs {
		t.Fatalf("the unreadable file's bytes are not counted against all plugins' share: %v", err)
	}
	if ok, err := a.Delete("s-0001", ReasonOwnerDelete, nil); !ok || err != nil {
		t.Fatalf("the session with the unreadable file cannot be deleted: %v %v", ok, err)
	}
}

// TestDeletingASessionWhoseOutputsBecameUnreadableLeavesNoPhantomBytes — S6 audit F4. A plugin's lines
// are counted per plugin as they are appended; the file then becomes unreadable (here: one line past
// the scanner's bound, written behind the archive's back) and the session is deleted. The plugin's
// share must be EMPTY again. Red before: the deletion re-read the file, failed, subtracted its size
// against no plugin, and the plugin's total kept every byte — a share full of nothing.
func TestDeletingASessionWhoseOutputsBecameUnreadableLeavesNoPhantomBytes(t *testing.T) {
	const quota = 400 << 10 // one plugin's share is 20 KiB
	g := newRig(t, quota)
	for _, root := range []string{"s-0001", "s-0002"} {
		if _, err := g.a.AppendRecords(ownerA, up(root, "", "100"), "main", recs(0, 1)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := g.a.AppendOutput("s-0001", "summary", outputLine("summary", 9)); err != nil {
			t.Fatal(err)
		}
	}
	var q *QuotaError
	if err := g.a.AppendOutput("s-0002", "summary", outputLine("summary", 9)); !errors.As(err, &q) || q.Share != SharePlugin {
		t.Fatalf("POSITIVE CONTROL: the plugin is not at its share: %v", err)
	}
	f, err := os.OpenFile(filepath.Join(g.dir, "sessions", "s-0001", "outputs.jsonl"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(strings.Repeat("x", MaxOutputLine+10) + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if g.a.ScanOutputs("s-0001", func([]byte) error { return nil }) == nil {
		t.Fatal("POSITIVE CONTROL: the outputs file is still readable; the test would prove nothing")
	}
	if ok, err := g.a.Delete("s-0001", ReasonOwnerDelete, nil); !ok || err != nil {
		t.Fatalf("delete: %v %v", ok, err)
	}
	for i := 0; i < 2; i++ {
		if err := g.a.AppendOutput("s-0002", "summary", outputLine("summary", 9)); err != nil {
			t.Fatalf("write %d after the deletion: %v — the deleted session's bytes are still charged to the plugin", i, err)
		}
	}
}

// TestConcurrentDeletionsDoNotDriftTheQuotasCount — the journal append measures the journal's size
// before and after its own line, so it must be alone while it does: two roots' deletions hold two
// different root locks. With the journal's own lock removed, concurrent deletions each count the
// other's line and `Used()` drifts UP — away from what a restart counts off the disk.
func TestConcurrentDeletionsDoNotDriftTheQuotasCount(t *testing.T) {
	for round := 0; round < 4; round++ {
		g := newRig(t, 1<<30)
		const n = 48
		var roots []string
		for i := 0; i < n; i++ {
			root := fmt.Sprintf("s-%04d", i)
			roots = append(roots, root)
			if _, err := g.a.AppendRecords(ownerA, up(root, "", "100"), "main", recs(0, 1)); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for _, root := range roots {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if ok, err := g.a.Delete(root, ReasonOwnerDelete, nil); !ok || err != nil {
					t.Errorf("delete %s: %v %v", root, ok, err)
				}
			}()
		}
		close(start)
		wg.Wait()
		red, err := redact.New(redact.SelfTestKey(99), nil)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Open(Config{Dir: g.dir, Retention: 90 * 24 * time.Hour, Quota: 1 << 30, Recheck: red, Now: g.clk.Now})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := g.a.Used(), again.Used(); got != want || want == 0 {
			t.Fatalf("round %d: after %d concurrent deletions the running count is %d bytes and the disk holds %d", round, n, got, want)
		}
	}
}

// TestAWithdrawalDropsFramesStagedForARootThatNeverCommitted — a record still being reassembled from
// continuation frames is that session's content, staged OUTSIDE any session directory. A withdrawal
// of a root with nothing committed answers `false` (nothing of the caller's was deleted here) and
// still discards those frames NOW, so they stop holding quota — not after the idle sweep.
func TestAWithdrawalDropsFramesStagedForARootThatNeverCommitted(t *testing.T) {
	g := newRig(t, 1<<30)
	u := up("s-0001", "", "100")
	u.Frame = &Frame{Index: 0, Count: 3}
	if _, err := g.a.AppendRecords(ownerA, u, "main", []Record{{Src: "100", Rec: []byte(strings.Repeat("x", 1000))}}); err != nil {
		t.Fatal(err)
	}
	if g.a.Used() == 0 {
		t.Fatal("POSITIVE CONTROL: a staged frame is not counted against the quota")
	}
	if deleted, err := g.a.Withdraw(ownerA, "s-0001"); deleted || err != nil {
		t.Fatalf("withdrawing a root with nothing committed answered %v %v, want false", deleted, err)
	}
	if used := g.a.Used(); used != 0 {
		t.Fatalf("%d bytes of the withdrawn root's staged frames are still held", used)
	}
}
