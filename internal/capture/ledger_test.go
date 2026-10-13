package capture

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/transcript/archive"
)

// THE CLIENT READ LEDGER'S UPLOAD (S11, decision 3a): the agent ships `<session>.jsonl` — the root's
// and every opencode child's — as the root's `ledger` stream, new complete lines only, before the
// transcript streams. RED before S11: no `ledger` stream was ever sent.

func ledgerLine(session, verb, scope string) string {
	return `{"schema":1,"session":"` + session + `","verb":"` + verb + `","instance":"personal","scope":"` + scope +
		`","client":"go","client_version":"(devel)","at":"2000-01-02T03:04:05Z"}`
}

func ledgerScopes(t *testing.T, recs []Record) []string {
	t.Helper()
	var out []string
	for _, r := range recs {
		var rec struct {
			Scope string `json:"scope"`
		}
		if err := json.Unmarshal(r.Rec, &rec); err != nil {
			t.Fatalf("a shipped ledger record is not JSON: %q", r.Rec)
		}
		out = append(out, rec.Scope)
	}
	return out
}

// TestTheLedgerShipsAsTheRootsLedgerStream: the lines ship once, complete lines only; a later
// line ships alone; a line that is not JSON ships as `*` with its bytes dropped.
func TestTheLedgerShipsAsTheRootsLedgerStream(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	id := w.Sessions["cc-plain"].ID
	path := filepath.Join(e.ledger, id+".jsonl")
	if err := os.WriteFile(path, []byte(ledgerLine(id, "sessions", "beta-notes")+"\n"+
		ledgerLine(id, "recall", "gamma-notes")), 0o600); err != nil { // the second has no newline yet
		t.Fatal(err)
	}
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	got := spooled(t, e, "personal", id, LedgerStream)
	if !slices.Equal(ledgerScopes(t, got), []string{"beta-notes"}) {
		t.Fatalf("run 1 shipped ledger scopes %v, want only the COMPLETE line's [beta-notes]", ledgerScopes(t, got))
	}
	// The write in flight completes, and a garbage line follows it.
	appendTo(t, path, "", "not json at all, and a secret-shaped tail would be dropped with it")
	mustRun(t, a)
	got = spooled(t, e, "personal", id, LedgerStream)
	if !slices.Equal(ledgerScopes(t, got), []string{"beta-notes", "gamma-notes", "*"}) {
		t.Fatalf("run 2: ledger scopes %v, want [beta-notes gamma-notes *] — each line once", ledgerScopes(t, got))
	}
	if strings.Contains(string(got[2].Rec), "not json") {
		t.Fatalf("an unparsed ledger line shipped its bytes: %s", got[2].Rec)
	}
	mustRun(t, a)
	if n := len(spooled(t, e, "personal", id, LedgerStream)); n != 3 {
		t.Fatalf("an unchanged ledger re-shipped: %d records", n)
	}
}

// TestAChildLedgerShipsUnderItsRoot is decision 3a's OPENCODE_SESSION_ID mapping: inside an opencode
// subagent the operator's plugin exports the CHILD's id, so the child's reads land in
// `<child>.jsonl`; the agent maps that child to its root (decision 7's recorded child ids) and ships
// it in the ROOT's `ledger` stream — and the routing set counts it.
func TestAChildLedgerShipsUnderItsRoot(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "oc-root")
	root, child := w.Sessions["oc-root"].ID, w.Sessions["oc-child"].ID
	if err := os.WriteFile(filepath.Join(e.ledger, child+".jsonl"),
		[]byte(ledgerLine(child, "sessions", "beta-notes")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList}
	a := newAgent(t, e, oneInstance, runner, &bytes.Buffer{})
	a.ClaudeRoot, a.OpencodeDirs = "", []string{"/work/alpha"}
	mustRun(t, a)
	got := spooled(t, e, "personal", root, LedgerStream)
	if !slices.Equal(ledgerScopes(t, got), []string{"beta-notes"}) || len(got) != 1 ||
		!strings.HasPrefix(got[0].Src, child+"@0@") {
		t.Fatalf("the child's ledger under the root: %v (srcs %v)", ledgerScopes(t, got), srcsOf(got))
	}
	if !slices.Contains(a.State.Sessions[root].V, "beta-notes") {
		t.Fatalf("the root's routing set %v does not count the child's read", a.State.Sessions[root].V)
	}
	if n := len(spooled(t, e, "personal", child, LedgerStream)); n != 0 {
		t.Fatalf("the child id shipped a ledger of its own (%d records) — it has no root of its own on the pod", n)
	}
}

func srcsOf(recs []Record) []string {
	var out []string
	for _, r := range recs {
		out = append(out, r.Src)
	}
	return out
}

// TestTheLedgerReachesThePodsFold: through the REAL pod, the uploaded ledger lands in the archive's
// `ledger` stream and the pod folds its scope into `meta.json` — the agent's half of clause (n).
func TestTheLedgerReachesThePodsFold(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	id := w.Sessions["cc-plain"].ID
	if err := os.WriteFile(filepath.Join(e.ledger, id+".jsonl"),
		[]byte(ledgerLine(id, "sessions", "beta-notes")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := newPod(t, podOpts{})
	var log bytes.Buffer
	a, _ := podAgent(t, e, p, 0, &log)
	if sum := mustRun(t, a); sum.Refused != 0 {
		t.Fatalf("%+v\n%s", sum, log.String())
	}
	m, err := p.arc.ReadMeta(id)
	if err != nil || m == nil {
		t.Fatalf("no meta: %v", err)
	}
	if !slices.Equal(m.Ledger, []string{"beta-notes"}) || m.LedgerRecords != 1 {
		t.Fatalf("the pod's ledger fold: %v / %d records\n%s", m.Ledger, m.LedgerRecords, log.String())
	}
}

// TestTheLedgerStreamNameIsThePods pins the one name spelled in two packages.
func TestTheLedgerStreamNameIsThePods(t *testing.T) {
	if LedgerStream != archive.LedgerStream {
		t.Fatalf("the agent ships %q, the pod folds %q", LedgerStream, archive.LedgerStream)
	}
}
