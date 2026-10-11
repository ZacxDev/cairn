package archive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/redact"
)

// Every fixture here is synthetic: sessions `s-0001`…, hosts `host-a`/`host-b`, owners
// `user:u_alpha`/`user:u_beta`, scopes `alpha-notes`/`beta-notes`, year-2000 clocks. Secrets the
// re-check must refuse are GENERATED AT RUN TIME (the plan's decision 6: a committed
// credential-shaped value is itself a leakscan finding).

var (
	ownerA = Uploader{Owner: "user:u_alpha", Host: "host-a"}
	ownerB = Uploader{Owner: "user:u_beta", Host: "host-a"}
	hostB  = Uploader{Owner: "user:u_alpha", Host: "host-b"}
	clock0 = time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type rig struct {
	t   *testing.T
	a   *Archive
	dir string
	clk *fakeClock
}

func newRig(t *testing.T, quota int64) *rig {
	t.Helper()
	red, err := redact.New(redact.SelfTestKey(99), nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	clk := &fakeClock{now: clock0}
	a, err := Open(Config{Dir: dir, Retention: 90 * 24 * time.Hour, Quota: quota, Recheck: red, Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, a: a, dir: dir, clk: clk}
}

func up(root, from, to string, declared ...string) Upload {
	return Upload{Root: root, Runtime: "claude", From: from, To: to, Declared: declared}
}

// rec is a synthetic Claude Code `assistant` record carrying `text`.
func rec(n int, text string) Record {
	return Record{Src: fmt.Sprint(n * 100), Rec: []byte(fmt.Sprintf(
		`{"type":"assistant","uuid":"00000000-0000-4000-8000-%012d","timestamp":"2000-01-02T03:04:05Z",`+
			`"message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`, n, text))}
}

func recs(from, to int) []Record {
	var out []Record
	for i := from; i < to; i++ {
		out = append(out, rec(i, fmt.Sprintf("synthetic turn %d", i)))
	}
	return out
}

// randToken is a GitHub-classic-shaped token, generated now: `ghp_` and 36 alphanumerics.
func randToken(seed uint64) string {
	rng := rand.New(rand.NewPCG(seed, seed^0x5eed))
	const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 36)
	for i := range b {
		b[i] = alnum[rng.IntN(len(alnum))]
	}
	return "ghp_" + string(b)
}

// treeDigest hashes every path, mode and byte under dir — "unchanged" is asserted on this.
func treeDigest(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		info, _ := d.Info()
		fmt.Fprintf(h, "%s %v\n", rel, info.Mode())
		if d.Type().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			h.Write(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (g *rig) readBack(root, stream string) []StoredRecord {
	g.t.Helper()
	got, err := g.a.ReadStream(root, stream)
	if err != nil {
		g.t.Fatal(err)
	}
	return got
}

// TestAResumedUploadLandsEveryRecordExactlyOnce is clause (a)'s relationship at the store: an
// upload interrupted after the pod stored it but before the agent saw the answer is RETRIED; the
// retry gets 409 naming the stored position, the agent resumes from there, and every record is
// stored exactly once, in order, with server-assigned seqs 1…N.
func TestAResumedUploadLandsEveryRecordExactlyOnce(t *testing.T) {
	g := newRig(t, 1<<30)
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "300"), "main", recs(0, 3)); err != nil {
		t.Fatal(err)
	}
	// The lost acknowledgement: the agent never saw "300" and retries from "".
	_, err := g.a.AppendRecords(ownerA, up("s-0001", "", "300"), "main", recs(0, 3))
	var stale *StaleError
	if !errors.As(err, &stale) || stale.StoredTo != "300" {
		t.Fatalf("a retried upload from a stale position answered %v, want a stale refusal naming 300", err)
	}
	st, err := g.a.AppendRecords(ownerA, up("s-0001", stale.StoredTo, "500"), "main", recs(3, 5))
	if err != nil || st.SeqTo != 5 || st.StoredTo != "500" {
		t.Fatalf("resuming from the named position answered %+v %v", st, err)
	}
	got := g.readBack("s-0001", "main")
	if len(got) != 5 {
		t.Fatalf("stored %d records, want exactly 5 (every record once)", len(got))
	}
	for i, r := range got {
		if r.Seq != int64(i+1) || r.Src != fmt.Sprint(i*100) || !bytes.Equal(r.Rec, rec(i, fmt.Sprintf("synthetic turn %d", i)).Rec) {
			t.Fatalf("record %d is seq %d src %s rec %s", i, r.Seq, r.Src, r.Rec)
		}
	}
}

// TestTwoConcurrentUploadsFromOnePositionOneWins: the compare-and-swap under contention — exactly
// one 200, one stale refusal naming the winner's position (clause a).
func TestTwoConcurrentUploadsFromOnePositionOneWins(t *testing.T) {
	for round := 0; round < 20; round++ {
		g := newRig(t, 1<<30)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = g.a.AppendRecords(ownerA, up("s-0001", "", fmt.Sprintf("to-%d", i)), "main", recs(i*10, i*10+2))
			}()
		}
		wg.Wait()
		won, lost := 0, 0
		var stale *StaleError
		for _, err := range errs {
			switch {
			case err == nil:
				won++
			case errors.As(err, &stale):
				lost++
			default:
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if won != 1 || lost != 1 {
			t.Fatalf("round %d: %d won and %d lost, want exactly one of each", round, won, lost)
		}
		if got := g.readBack("s-0001", "main"); len(got) != 2 || stale.StoredTo == "" {
			t.Fatalf("round %d: %d records stored, stale named %q", round, len(got), stale.StoredTo)
		}
	}
}

// TestAFramedRecordIsStoredAsOneRecordByteIdentical: a 3 MB record sent as three frames is stored
// as ONE record whose bytes equal the source; the non-final frames store nothing.
func TestAFramedRecordIsStoredAsOneRecordByteIdentical(t *testing.T) {
	g := newRig(t, 1<<30)
	big := rec(1, strings.Repeat("lorem ipsum dolor ", 3<<20/18))
	src := big.Rec
	cut := []int{0, len(src) / 3, 2 * len(src) / 3, len(src)}
	for i := 0; i < 3; i++ {
		u := up("s-0001", "", "100")
		u.Frame = &Frame{Index: i, Count: 3}
		st, err := g.a.AppendRecords(ownerA, u, "main", []Record{{Src: "100", Rec: src[cut[i]:cut[i+1]]}})
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if i < 2 {
			if !st.Pending {
				t.Fatalf("frame %d was not staged as pending: %+v", i, st)
			}
			if m, _ := g.a.ReadMeta("s-0001"); m != nil {
				t.Fatalf("a non-final frame created the session")
			}
		}
	}
	got := g.readBack("s-0001", "main")
	if len(got) != 1 || !bytes.Equal(got[0].Rec, src) {
		t.Fatalf("stored %d record(s); the first is %d bytes against a %d-byte source", len(got), len(got[0].Rec), len(src))
	}
}

// TestAMissingMiddleFrameStoresNothing: frames 0 and 2 of 3 — the final frame is refused, nothing
// is stored, and the staged bytes are returned to the quota.
func TestAMissingMiddleFrameStoresNothing(t *testing.T) {
	g := newRig(t, 1<<30)
	src := rec(1, strings.Repeat("x", 3000)).Rec
	send := func(i int) error {
		u := up("s-0001", "", "100")
		u.Frame = &Frame{Index: i, Count: 3}
		_, err := g.a.AppendRecords(ownerA, u, "main", []Record{{Src: "100", Rec: src[i*1000 : min(len(src), (i+1)*1000)]}})
		return err
	}
	if err := send(0); err != nil {
		t.Fatal(err)
	}
	var inv *InvalidError
	if err := send(2); !errors.As(err, &inv) || !strings.Contains(inv.Reason, "missing or out of order") {
		t.Fatalf("a final frame after a missing middle one answered %v", err)
	}
	if m, _ := g.a.ReadMeta("s-0001"); m != nil {
		t.Fatal("a missing middle frame stored something")
	}
	if used := g.a.Used(); used != 0 {
		t.Fatalf("%d staged bytes are still counted against the quota", used)
	}
}

// TestARestartDiscardsStagedFramesAndCountsWhatIsStored: a reopened archive counts the committed
// bytes already on disk against the quota, and drops frames a previous process staged (nothing can
// continue them).
func TestARestartDiscardsStagedFramesAndCountsWhatIsStored(t *testing.T) {
	g := newRig(t, 1<<30)
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "100"), "main", recs(0, 2)); err != nil {
		t.Fatal(err)
	}
	u := up("s-0002", "", "100")
	u.Frame = &Frame{Index: 0, Count: 2}
	if _, err := g.a.AppendRecords(ownerA, u, "main", []Record{{Src: "0", Rec: []byte(`{"half":`)}}); err != nil {
		t.Fatal(err)
	}
	committed, err := treeBytes(filepath.Join(g.dir, sessionsDir))
	if err != nil || committed == 0 {
		t.Fatalf("POSITIVE CONTROL: %d committed bytes (%v)", committed, err)
	}
	again, err := Open(g.a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if again.Used() != committed {
		t.Fatalf("the reopened archive counts %d bytes, want the %d committed (staged frames dropped)", again.Used(), committed)
	}
	if _, err := os.Stat(filepath.Join(g.dir, framesDir)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the staged frames survived a reopen (%v)", err)
	}
}

// TestTheFirstUploadFixesTheOwnerAndHost is decision 15's ownership: owner B's first upload of a
// root owner A holds is refused with the ONE uniform error and A's directory is byte-unchanged;
// so is owner A from ANOTHER host; A continuing from its own host succeeds.
func TestTheFirstUploadFixesTheOwnerAndHost(t *testing.T) {
	g := newRig(t, 1<<30)
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "100"), "main", recs(0, 1)); err != nil {
		t.Fatal(err)
	}
	before := treeDigest(t, g.dir)
	for _, who := range []Uploader{ownerB, hostB} {
		// From the CORRECT stored position: the refusal must be ownership, not a stale cursor.
		_, err := g.a.AppendRecords(who, up("s-0001", "100", "200"), "main", recs(1, 2))
		if !errors.Is(err, ErrNotOwner) {
			t.Fatalf("%+v answered %v, want ErrNotOwner", who, err)
		}
		// And from a WRONG one: still ownership, so the stored position is not disclosed.
		if _, err := g.a.AppendRecords(who, up("s-0001", "", "200"), "main", recs(1, 2)); !errors.Is(err, ErrNotOwner) {
			t.Fatalf("%+v from a wrong position answered %v — a stale answer would name the owner's position", who, err)
		}
		if _, err := g.a.PutBlob(who, up("s-0001", "", "v1"), "toolu_x.txt", []byte("text\n")); !errors.Is(err, ErrNotOwner) {
			t.Fatalf("%+v blob answered %v", who, err)
		}
	}
	if after := treeDigest(t, g.dir); after != before {
		t.Fatal("a refused upload changed the owner's directory")
	}
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "100", "200"), "main", recs(1, 2)); err != nil {
		t.Fatalf("the owner continuing from its own host: %v", err)
	}
	m, _ := g.a.ReadMeta("s-0001")
	if m.Owner != ownerA.Owner || m.Host != ownerA.Host {
		t.Fatalf("meta names %s on %s", m.Owner, m.Host)
	}
}

// TestThePodRefusesARecordTheTableMatches is clause (c) at the store, on a RECORD: one record in a
// batch carries a runtime-generated token, the request is refused naming that record's index, and
// NOTHING in it is stored — not the clean records beside it, not a session directory. The same
// batch with a clean value is stored (the positive control).
func TestThePodRefusesARecordTheTableMatches(t *testing.T) {
	g := newRig(t, 1<<30)
	token := randToken(1)
	batch := recs(0, 3)
	batch[1] = rec(1, "the header was Authorization: token "+token)
	_, err := g.a.AppendRecords(ownerA, up("s-0001", "", "300"), "main", batch)
	var re *RecheckError
	if !errors.As(err, &re) || re.Record != 1 || re.Blob != "" {
		t.Fatalf("a record carrying a token answered %v, want a re-check refusal naming record 1", err)
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), token[4:12]) {
		t.Fatal("the refusal carries the value")
	}
	if entries, _ := os.ReadDir(filepath.Join(g.dir, sessionsDir)); len(entries) != 0 {
		t.Fatalf("a refused request left %d session director(ies)", len(entries))
	}
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "300"), "main", recs(0, 3)); err != nil {
		t.Fatalf("POSITIVE CONTROL: the same batch with clean values was refused: %v", err)
	}
	if got := g.readBack("s-0001", "main"); len(got) != 3 {
		t.Fatalf("the clean batch stored %d records", len(got))
	}
}

// TestThePodRefusesATextBlobTheTableMatches is clause (c) on a BLOB: a persisted text tool result
// carrying a token is refused naming the blob and nothing is stored; the same blob clean is stored
// byte-identical; a BINARY blob (a PNG signature) ships as it is (O12) even with the token inside.
func TestThePodRefusesATextBlobTheTableMatches(t *testing.T) {
	g := newRig(t, 1<<30)
	token := randToken(2)
	dirty := []byte("export GITHUB_TOKEN=" + token + "\nexport LOG_LEVEL=debug\n")
	_, err := g.a.PutBlob(ownerA, up("s-0001", "", "v1"), "toolu_env.txt", dirty)
	var re *RecheckError
	if !errors.As(err, &re) || re.Blob != "toolu_env.txt" {
		t.Fatalf("a text blob carrying a token answered %v, want a re-check refusal naming the blob", err)
	}
	if m, _ := g.a.ReadMeta("s-0001"); m != nil {
		t.Fatal("a refused blob created the session")
	}
	clean := []byte("export LOG_LEVEL=debug\n")
	if _, err := g.a.PutBlob(ownerA, up("s-0001", "", "v1"), "toolu_env.txt", clean); err != nil {
		t.Fatalf("POSITIVE CONTROL: a clean text blob was refused: %v", err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("tEXtComment\x00"+token)...)
	if _, err := g.a.PutBlob(ownerA, up("s-0001", "", "v1"), "toolu_shot.png", png); err != nil {
		t.Fatalf("a binary blob was refused (O12: binary ships): %v", err)
	}
	for name, want := range map[string][]byte{"toolu_env.txt": clean, "toolu_shot.png": png} {
		got, err := g.a.ReadBlob("s-0001", name)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("blob %s is not byte-identical to what was sent (%v)", name, err)
		}
	}
}

// TestTheAgentsRedactedOutputIsAccepted ties the re-check to the redactor it re-runs: the S1
// corpus's items, redacted under a HOST key, are every one accepted by a pod re-checking under its
// own key. Before `internal/redact` was made quiet on its own markers this refused the redacted
// output of every planted position — the re-check would have stalled exactly the sessions that
// needed redaction. Positive control: the same items UNREDACTED are refused.
func TestTheAgentsRedactedOutputIsAccepted(t *testing.T) {
	g := newRig(t, 1<<30)
	c := redact.NewCorpus(3)
	host, err := redact.New(redact.SelfTestKey(3), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Every record in ONE request: the re-check refuses a whole request on any hit, so one accepted
	// request is every record accepted — and one fsync instead of one per item.
	var redacted, raw []Record
	refusedRaw, blobs := 0, 0
	for i, it := range c.Items {
		if it.Blob {
			out, _ := host.Blob(it.Name, it.Data)
			blobs++
			if _, err := g.a.PutBlob(ownerA, up("s-0002", "", fmt.Sprint(i)), fmt.Sprintf("toolu_raw%d.txt", i), it.Data); err != nil {
				refusedRaw++
			}
			if _, err := g.a.PutBlob(ownerA, up("s-0001", "", fmt.Sprint(i)), fmt.Sprintf("toolu_red%d.txt", i), out); err != nil {
				t.Fatalf("item %d (blob %s): the agent's redacted output was refused: %v", i, it.Name, err)
			}
			continue
		}
		out, _ := host.Record(it.Data)
		redacted = append(redacted, Record{Src: fmt.Sprint(i), Rec: out})
		raw = append(raw, Record{Src: fmt.Sprint(i), Rec: it.Data})
	}
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "end"), "main", redacted); err != nil {
		t.Fatalf("the agent's redacted records (%d) were refused: %v", len(redacted), err)
	}
	var re *RecheckError
	if _, err := g.a.AppendRecords(ownerA, up("s-0002", "", "end"), "main", raw); !errors.As(err, &re) {
		t.Fatalf("POSITIVE CONTROL: the %d UNREDACTED records were not refused (%v), so the acceptance above proves nothing", len(raw), err)
	}
	if blobs == 0 || refusedRaw == 0 {
		t.Fatalf("POSITIVE CONTROL: %d of %d UNREDACTED blobs refused", refusedRaw, blobs)
	}
}

// TestTheQuotaRefusesAndRetentionFreesIt: at the quota a NEW upload is refused (named); after the
// retention sweep frees space the same retry succeeds — bytes delayed, never dropped.
func TestTheQuotaRefusesAndRetentionFreesIt(t *testing.T) {
	g := newRig(t, 1<<30)
	// Lower-case letters only: poorly compressible, and no entropy-rule token (that needs three
	// character classes), so the re-check passes and only the quota can refuse.
	rng := rand.New(rand.NewPCG(4, 4))
	noise := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = "abcdefghijklmnopqrstuvwxyz"[rng.IntN(26)]
		}
		return string(b)
	}
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "1"), "main", []Record{rec(1, noise(3500))}); err != nil {
		t.Fatal(err)
	}
	// The quota leaves room for less than the next upload, and for more than it once s-0001 is gone.
	quota := g.a.Used() + 2500
	g.a.cfg.Quota = quota
	g.clk.Add(89 * 24 * time.Hour)
	big := []Record{rec(2, noise(6000))}
	_, err := g.a.AppendRecords(ownerA, up("s-0002", "", "1"), "main", big)
	var q *QuotaError
	if !errors.As(err, &q) || q.Quota != quota || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("an upload over the quota answered %v, want the named quota refusal", err)
	}
	if m, _ := g.a.ReadMeta("s-0002"); m != nil {
		t.Fatal("a quota-refused upload created its session")
	}
	g.clk.Add(1*24*time.Hour + time.Second)
	if deleted, err := g.a.Sweep(); err != nil || !slices.Equal(deleted, []string{"s-0001"}) {
		t.Fatalf("the sweep deleted %v (%v)", deleted, err)
	}
	if _, err := g.a.AppendRecords(ownerA, up("s-0002", "", "1"), "main", big); err != nil {
		t.Fatalf("the retry after retention freed space: %v", err)
	}
}

// TestRetentionSweepsAtItsBoundary: a session whose last upload is retention − 1 s old is kept,
// one at retention + 1 s is deleted, and the deletion is journaled without content.
func TestRetentionSweepsAtItsBoundary(t *testing.T) {
	g := newRig(t, 1<<30)
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "1"), "main", recs(0, 1)); err != nil {
		t.Fatal(err)
	}
	g.clk.Add(90*24*time.Hour - time.Second)
	if deleted, _ := g.a.Sweep(); len(deleted) != 0 {
		t.Fatalf("a session at retention − 1 s was swept: %v", deleted)
	}
	g.clk.Add(time.Second)
	if deleted, _ := g.a.Sweep(); len(deleted) != 0 {
		t.Fatalf("a session at EXACTLY the retention was swept (the boundary belongs to the session): %v", deleted)
	}
	g.clk.Add(time.Second)
	if deleted, _ := g.a.Sweep(); !slices.Equal(deleted, []string{"s-0001"}) {
		t.Fatalf("a session at retention + 1 s was not swept: %v", deleted)
	}
	if _, err := os.Stat(filepath.Join(g.dir, sessionsDir, "s-0001")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the swept session's directory is still there (%v)", err)
	}
	j, err := os.ReadFile(filepath.Join(g.dir, journalName))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"event":"transcript-deleted","root":"s-0001","owner":"user:u_alpha","host":"host-a","reason":"retention",` +
		`"at":"2000-04-01T03:04:06Z"}` + "\n"
	if string(j) != want {
		t.Fatalf("the journal is\n%s\nwant\n%s", j, want)
	}
	if g.a.Used() != int64(len(want)) {
		t.Fatalf("after the sweep the quota counts %d bytes, want the journal's %d", g.a.Used(), len(want))
	}
}

// TestThePodDerivesStarNotTheDeclarationAlone: an agent that declares NOTHING still gets `*` in
// the pod's own set, and one that declares `beta-notes` keeps both — the pod's set is never the
// agent's declaration alone (decision 3: "so an old or lying agent cannot shrink `V`"). Until
// `scopeuse` is wired, `*` is the fail-closed answer.
func TestThePodDerivesStarNotTheDeclarationAlone(t *testing.T) {
	g := newRig(t, 1<<30)
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "1"), "main", recs(0, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "2", "beta-notes"), "child:ses_0002", recs(1, 2)); err != nil {
		t.Fatal(err)
	}
	m, _ := g.a.ReadMeta("s-0001")
	if !slices.Equal(m.Derived, []string{Star}) || !slices.Equal(m.Declared, []string{"beta-notes"}) ||
		!slices.Equal(m.Children, []string{"ses_0002"}) {
		t.Fatalf("meta derived=%v declared=%v children=%v", m.Derived, m.Declared, m.Children)
	}
}

// TestResolveDirRefusesInsideTheStoreRoot: the store root itself, a directory under it, and a
// symlink outside that resolves into it are each refused; a sibling directory is accepted.
func TestResolveDirRefusesInsideTheStoreRoot(t *testing.T) {
	base := t.TempDir()
	store := filepath.Join(base, "store")
	inside := filepath.Join(store, ".transcripts")
	outside := filepath.Join(base, "transcripts")
	link := filepath.Join(base, "link")
	for _, d := range []string{store, inside, outside} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(inside, link); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{store, inside, link} {
		if _, err := ResolveDir(store, d); err == nil || !strings.Contains(err.Error(), "INSIDE the store root") {
			t.Fatalf("%s answered %v", d, err)
		}
	}
	if _, err := ResolveDir(store, filepath.Join(base, "absent")); err == nil {
		t.Fatal("a missing directory was accepted")
	}
	if got, err := ResolveDir(store, outside); err != nil || got == "" {
		t.Fatalf("a sibling directory was refused: %v", err)
	}
}

// TestUploadsAreValidated: each malformed field is its own refusal, and none creates a session.
func TestUploadsAreValidated(t *testing.T) {
	g := newRig(t, 1<<30)
	cases := map[string]func() error{
		"a root that is not a session id": func() error {
			_, err := g.a.AppendRecords(ownerA, up("../etc", "", "1"), "main", recs(0, 1))
			return err
		},
		"the ledger stream (S11's)": func() error {
			_, err := g.a.AppendRecords(ownerA, up("s-0001", "", "1"), "ledger", recs(0, 1))
			return err
		},
		"a stream outside the vocabulary": func() error {
			_, err := g.a.AppendRecords(ownerA, up("s-0001", "", "1"), "subagent:../x", recs(0, 1))
			return err
		},
		"an unknown runtime": func() error {
			u := up("s-0001", "", "1")
			u.Runtime = "other"
			_, err := g.a.AppendRecords(ownerA, u, "main", recs(0, 1))
			return err
		},
		"an empty to": func() error {
			_, err := g.a.AppendRecords(ownerA, up("s-0001", "", ""), "main", recs(0, 1))
			return err
		},
		"a declared scope that is no name": func() error {
			_, err := g.a.AppendRecords(ownerA, up("s-0001", "", "1", "alpha notes"), "main", recs(0, 1))
			return err
		},
		"a record that is not JSON": func() error {
			_, err := g.a.AppendRecords(ownerA, up("s-0001", "", "1"), "main", []Record{{Src: "0", Rec: []byte("{not json")}})
			return err
		},
		"no records": func() error {
			_, err := g.a.AppendRecords(ownerA, up("s-0001", "", "1"), "main", nil)
			return err
		},
		"a blob name with a slash": func() error {
			_, err := g.a.PutBlob(ownerA, up("s-0001", "", "1"), "a/b.txt", []byte("x"))
			return err
		},
		"a one-frame sequence": func() error {
			u := up("s-0001", "", "1")
			u.Frame = &Frame{Index: 0, Count: 1}
			_, err := g.a.AppendRecords(ownerA, u, "main", recs(0, 1))
			return err
		},
	}
	for name, run := range cases {
		var inv *InvalidError
		if err := run(); !errors.As(err, &inv) {
			t.Errorf("%s: answered %v, want an InvalidError", name, err)
		}
	}
	if roots, _ := g.a.Roots(); len(roots) != 0 {
		t.Fatalf("refused uploads created %v", roots)
	}
}
