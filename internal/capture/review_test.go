package capture

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/client"
)

// Tests for the findings of review rounds 0 and 1 on S2.

// TestAScopeRoutedToAnotherInstanceIsHeldOnAOneInstanceHost (F1): a table that names an alias this
// host has no config for says the scope lives ELSEWHERE; it must not ship to the sole instance.
func TestAScopeRoutedToAnotherInstanceIsHeldOnAOneInstanceHost(t *testing.T) {
	one := client.Routing{Instances: []client.Instance{{Alias: "personal"}},
		Routes: map[string]string{"beta-notes": "client"}, RoutesSource: "the test's table"}
	if d := Decide([]string{"beta-notes"}, one); !d.Held {
		t.Fatalf("an explicitly-routed, unconfigured scope shipped: %+v", d)
	}
	// Control: a scope the table says NOTHING about still ships to the sole instance.
	if d := Decide([]string{"gamma-notes"}, one); d.Held || d.Instance != "personal" {
		t.Fatalf("an unnamed scope on a one-instance host: %+v", d)
	}
	w := loadWorld(t)
	e := materialize(t, w, "cc-hook-recall") // reads beta-notes
	a := newAgent(t, e, one, &fakeRunner{}, &bytes.Buffer{})
	if sum := mustRun(t, a); sum.Held != 1 || e.sink.has("personal", w.Sessions["cc-hook-recall"].ID) {
		t.Fatalf("the agent shipped it: %+v", sum)
	}
}

// TestOneSessionsFailureDoesNotAbortTheRun (F2): a sink refusal for one session, a child export
// that fails for another — each is refused and logged, and every other session still ships.
func TestOneSessionsFailureDoesNotAbortTheRun(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain", "cc-ledgered-bare", "oc-root")
	bad := w.Sessions["cc-plain"].ID
	e.sink.failRoot = bad
	ex := exportsOf(w, "before")
	delete(ex, w.Sessions["oc-child"].ID) // the child's export fails
	var log bytes.Buffer
	a := newAgent(t, e, oneInstance, &fakeRunner{exports: ex, list: w.Opencode.SessionList}, &log)
	a.OpencodeDirs = []string{"/work/alpha"}
	sum, err := a.Run()
	if err != nil {
		t.Fatalf("the run aborted: %v", err)
	}
	if sum.Refused != 2 || !strings.Contains(log.String(), "refused "+bad) ||
		!strings.Contains(log.String(), "refused "+w.Sessions["oc-root"].ID) {
		t.Fatalf("refusals: %+v\n%s", sum, log.String())
	}
	if !e.sink.has("personal", w.Sessions["cc-ledgered-bare"].ID) {
		t.Fatal("a healthy session after the failing one did not ship")
	}
}

// TestABlobNameWithDotsInsideShips (F2): `..` is refused as a path COMPONENT only; `out..txt` is an
// ordinary name.
func TestABlobNameWithDotsInsideShips(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-rich")
	rich := w.Sessions["cc-rich"]
	dir := filepath.Join(e.claude, rich.Project, rich.ID, "tool-results")
	if err := os.WriteFile(filepath.Join(dir, "out..txt"), []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &bytes.Buffer{})
	if sum := mustRun(t, a); sum.Refused != 0 {
		t.Fatalf("refused: %+v", sum)
	}
	if _, ok := e.sink.blobs["personal/"+rich.ID+"/tool-results/out..txt"]; !ok {
		t.Fatal("a blob named out..txt did not ship")
	}
	if !safeID("a..b") || safeID("..") || safeID("a/b") {
		t.Fatal("safeID must test the component, not a substring")
	}
}

func writeLedger(t *testing.T, e env, id, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.ledger, id+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestVOnlyGrowsWhenTheLedgerShrinks (F3): V is the UNION of every run's evidence. A ledger that
// is deleted after a run (anything on the host can) must not shrink V or move the session.
func TestVOnlyGrowsWhenTheLedgerShrinks(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	s := w.Sessions["cc-plain"]
	writeLedger(t, e, s.ID, `{"schema":1,"scope":"beta-notes"}`+"\n")
	a := newAgent(t, e, twoInstances, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	if !e.sink.has("client", s.ID) {
		t.Fatal("run 1 did not route the beta-notes reader to the client instance")
	}
	if err := os.Remove(filepath.Join(e.ledger, s.ID+".jsonl")); err != nil {
		t.Fatal(err)
	}
	appendTo(t, filepath.Join(e.claude, s.Project, s.ID+".jsonl"), `{"type":"mode","mode":"normal","sessionId":"x"}`)
	mustRun(t, a)
	if got := a.State.Sessions[s.ID].V; !slices.Equal(got, []string{"beta-notes"}) {
		t.Fatalf("V shrank to %v when the ledger was deleted", got)
	}
	if len(e.sink.withdrawn["client"]) != 0 {
		t.Fatal("a deleted ledger moved the session")
	}
}

// TestAChildSessionsLedgerCounts (F3): opencode child ids' ledger files are folded into the root.
func TestAChildSessionsLedgerCounts(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "oc-root")
	writeLedger(t, e, w.Sessions["oc-child"].ID, `{"schema":1,"scope":"beta-notes"}`+"\n")
	a := newAgent(t, e, oneInstance, &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList},
		&bytes.Buffer{})
	a.ClaudeRoot = ""
	a.OpencodeDirs = []string{"/work/alpha"}
	mustRun(t, a)
	// With the child's ledger: alpha-notes (header) and beta-notes (ledger), and NO `*` — a
	// non-empty ledger keeps F1 quiet. Without it the program-naming inputs trip F1.
	if got := a.State.Sessions[w.Sessions["oc-root"].ID].V; !slices.Equal(got, []string{"alpha-notes", "beta-notes"}) {
		t.Fatalf("V = %v, want [alpha-notes beta-notes]", got)
	}
}

// TestAnUnreadableLedgerFailsClosed (F3): a ledger that EXISTS but cannot be read is `*`, never
// "no ledger".
func TestAnUnreadableLedgerFailsClosed(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	s := w.Sessions["cc-plain"]
	if err := os.Mkdir(filepath.Join(e.ledger, s.ID+".jsonl"), 0o700); err != nil { // a read fails
		t.Fatal(err)
	}
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	if got := a.State.Sessions[s.ID].V; !slices.Equal(got, []string{"*"}) {
		t.Fatalf("V = %v, want [*]", got)
	}
}

// TestAnUnchangedBlobIsNotReread (F4): the second run stats every blob and reads none.
func TestAnUnchangedBlobIsNotReread(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-rich")
	reads := 0
	orig := readFile
	readFile = func(p string) ([]byte, error) { reads++; return orig(p) }
	defer func() { readFile = orig }()
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	if reads == 0 {
		t.Fatal("positive control: run 1 read no blob")
	}
	reads = 0
	mustRun(t, a)
	if reads != 0 {
		t.Fatalf("run 2 re-read %d unchanged blob(s)", reads)
	}
	// A CHANGED blob (size moves) is read and re-shipped.
	rich := w.Sessions["cc-rich"]
	matches, _ := filepath.Glob(filepath.Join(e.claude, rich.Project, rich.ID, "tool-results", "*.txt"))
	appendTo(t, matches[0], "more")
	if sum := mustRun(t, a); reads != 1 || sum.Blobs != 1 {
		t.Fatalf("a changed blob: %d read(s), %d shipped", reads, sum.Blobs)
	}
}

// TestADuplicateKeyCannotSmuggleASecretThroughTheAgent (F5): S1's duplicate-key fix, end to end.
func TestADuplicateKeyCannotSmuggleASecretThroughTheAgent(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	s := w.Sessions["cc-plain"]
	tok := "ghp_" + strings.Repeat("Zq9x", 9)
	appendTo(t, filepath.Join(e.claude, s.Project, s.ID+".jsonl"),
		`{"type":"user","sessionId":"x","message":{"role":"user","content":"ok"},"note":"`+tok+`","note":"ok"}`)
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	for _, r := range spooled(t, e, "personal", s.ID, "main") {
		if bytes.Contains(r.Rec, []byte(tok)) {
			t.Fatal("a secret in the FIRST of two duplicate members reached the sink")
		}
	}
}

// TestANonJSONLineShipsByteIdentical (F6): no U+FFFD substitution on the way out.
func TestANonJSONLineShipsByteIdentical(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	s := w.Sessions["cc-plain"]
	line := "\xff\xfe not json \x80"
	appendTo(t, filepath.Join(e.claude, s.Project, s.ID+".jsonl"), line)
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	recs := spooled(t, e, "personal", s.ID, "main")
	if got := recs[len(recs)-1].Rec; !bytes.Equal(got, []byte(line)) {
		t.Fatalf("the non-JSON line changed: %q", got)
	}
}

// TestARewritePastTheFingerprintIsRefused (F7): the byte before the watermark must still be the
// newline that ended the last shipped line, even far past the 4 KiB head.
func TestARewritePastTheFingerprintIsRefused(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-rich")
	rich := w.Sessions["cc-rich"]
	path := filepath.Join(e.claude, rich.Project, rich.ID+".jsonl")
	var log bytes.Buffer
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &log)
	mustRun(t, a)
	full, _ := os.ReadFile(path)
	if len(full) < 2*FingerprintBytes {
		t.Fatal("fixture: the main stream is too short to rewrite past the fingerprint")
	}
	// Move the last line boundary: the shipped region's final newline becomes a space, and the
	// file grows, so neither the head fingerprint nor the size check can see it.
	rewritten := append([]byte(nil), full...)
	rewritten[len(rewritten)-1] = ' '
	rewritten = append(rewritten, []byte("\n"+`{"type":"mode","mode":"normal","sessionId":"x"}`+"\n")...)
	if err := os.WriteFile(path, rewritten, 0o600); err != nil {
		t.Fatal(err)
	}
	if sum := mustRun(t, a); sum.Refused != 1 || !strings.Contains(log.String(), "refused "+rich.ID+"/main") {
		t.Fatalf("a rewrite past the fingerprint was resumed: %+v %s", sum, log.String())
	}
}
