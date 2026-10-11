package scopeuse

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

type world struct {
	Sessions map[string]struct {
		Runtime string `json:"runtime"`
		ID      string `json:"id"`
		Parent  string `json:"parent"`
	} `json:"sessions"`
	Claude struct {
		Files map[string]map[string]string `json:"files"`
	} `json:"claude"`
	Ledgers  map[string]string `json:"ledgers"`
	Opencode struct {
		Exports map[string]map[string]json.RawMessage `json:"exports"`
	} `json:"opencode"`
}

func loadWorld(t *testing.T) world {
	t.Helper()
	raw, err := os.ReadFile("../testdata/synthetic_world.json")
	if err != nil {
		t.Fatalf("the synthetic world is missing: %v", err)
	}
	var w world
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	return w
}

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v map[string]any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// derive runs the Deriver over one named fixture session: every stream, and its ledger file
// when the world has one. `inputsOnly` is the control reader that sees tool INPUTS and nothing
// else.
func derive(t *testing.T, w world, name string, inputsOnly bool) Result {
	t.Helper()
	s, ok := w.Sessions[name]
	if !ok {
		t.Fatalf("no fixture session %s", name)
	}
	d := NewDeriver()
	feedClaude := func(rec map[string]any) {
		if !inputsOnly {
			d.ClaudeRecord(rec)
			return
		}
		msg, _ := rec["message"].(map[string]any)
		blocks, _ := msg["content"].([]any)
		for _, b := range blocks {
			if bm, ok := b.(map[string]any); ok && bm["type"] == "tool_use" {
				eachString(bm["input"], d.ToolInput)
			}
		}
	}
	switch s.Runtime {
	case "claude":
		n := 0
		for path, f := range w.Claude.Files {
			if !strings.HasSuffix(path, ".jsonl") || !strings.Contains(path, s.ID) {
				continue
			}
			for _, line := range strings.SplitAfter(f["utf8"], "\n") {
				if line != "" {
					feedClaude(decode(t, []byte(line)))
					n++
				}
			}
		}
		if n == 0 {
			t.Fatalf("session %s fed no records — the instrument read nothing", name)
		}
	case "opencode":
		for _, id := range []string{s.ID, w.Sessions["oc-child"].ID} {
			doc := decode(t, w.Opencode.Exports["before"][id])
			for _, m := range doc["messages"].([]any) {
				for _, p := range m.(map[string]any)["parts"].([]any) {
					d.OpencodePart(p.(map[string]any))
				}
			}
		}
	}
	if l, ok := w.Ledgers[s.ID+".jsonl"]; ok {
		d.Ledger([]byte(l))
	}
	return d.Result()
}

// TestTheVisibilityInputOfEveryFixtureSession pins R_header ∪ L ∪ F per session with LITERAL
// expectations — never derived from the implementation.
func TestTheVisibilityInputOfEveryFixtureSession(t *testing.T) {
	w := loadWorld(t)
	cases := map[string][]string{
		// explicit --scope, a `cd … &&` chain, a `--repo` search and a header-less ls-entries:
		// the headers carry the RESOLVED scope and the ledger agrees.
		"cc-rich": {"alpha-notes"},
		// The ledger, not the line, is the read record: bare calls in chains and wrappers.
		"cc-ledgered-bare": {"alpha-notes", "beta-notes"},
		// THE routing fixture: wrote alpha-notes, read beta-notes.
		"cc-routing-two-instances": {"alpha-notes", "beta-notes"},
		// An unrelated session: nothing.
		"cc-plain": {},
		// opencode: the header names alpha-notes; inputs name the program and NO ledger file
		// exists for these sessions, so F1 adds `*`.
		"oc-root": {"*", "alpha-notes"},
	}
	for name, want := range cases {
		got := derive(t, w, name, false).V
		if !slices.Equal(got, want) && !(len(got) == 0 && len(want) == 0) {
			t.Errorf("%s: V = %v, want %v", name, got, want)
		}
	}
}

// TestAHookAttachmentHeaderCounts: a recall rendered into a HOOK attachment, with no command line
// anywhere, still adds its scope. Control: a reader of tool INPUTS only misses it.
func TestAHookAttachmentHeaderCounts(t *testing.T) {
	w := loadWorld(t)
	if got := derive(t, w, "cc-hook-recall", false).V; !slices.Equal(got, []string{"beta-notes"}) {
		t.Fatalf("V = %v, want [beta-notes]", got)
	}
	if got := derive(t, w, "cc-hook-recall", true).V; len(got) != 0 {
		t.Fatalf("control: an inputs-only reader found %v — then the hook case proves nothing", got)
	}
}

// TestTheAllScopesHeaderAddsStar: a store-wide search renders `scope=(all scopes)`, which names
// no scope, so it is `*` (mutant scopeuse-all-scopes-header-names-a-scope).
func TestTheAllScopesHeaderAddsStar(t *testing.T) {
	w := loadWorld(t)
	if got := derive(t, w, "cc-hook-allscopes", false).V; !slices.Equal(got, []string{"*"}) {
		t.Fatalf("V = %v, want [*]", got)
	}
	if got := HeaderScopes("subsystem-recall: status=search-hit scope=(all scopes) query='x'"); !slices.Equal(got, []string{"*"}) {
		t.Fatalf("HeaderScopes = %v", got)
	}
}

// TestTheScopelessHeaderFormAddsStar: the `renderer.go` "all N entry files … MALFORMED" form carries
// NO `scope=` field at all (mutant scopeuse-scopeless-header-dropped).
func TestTheScopelessHeaderFormAddsStar(t *testing.T) {
	w := loadWorld(t)
	if got := derive(t, w, "cc-unreadable-header", false).V; !slices.Equal(got, []string{"*", "alpha-notes"}) {
		t.Fatalf("V = %v, want [* alpha-notes]", got)
	}
	if got := HeaderScopes("subsystem-recall: scope-unreadable: all 2 entry files under `x` are MALFORMED"); !slices.Equal(got, []string{"*"}) {
		t.Fatalf("HeaderScopes = %v", got)
	}
}

// TestF1EmptyLedgerAddsStar: a program-naming input and an EMPTY ledger is `*` (mutant
// scopeuse-fallback-empty-ledger-trusted); F1 is about emptiness, not per call.
func TestF1EmptyLedgerAddsStar(t *testing.T) {
	w := loadWorld(t)
	r := derive(t, w, "cc-f1-empty-ledger", false)
	if !r.F1 || !slices.Equal(r.V, []string{"*"}) {
		t.Fatalf("F1=%v V=%v, want F1 and [*]", r.F1, r.V)
	}
	if got := derive(t, w, "cc-f1-uppercase", false).V; !slices.Equal(got, []string{"*"}) {
		t.Fatalf("`CAIRN recall` with an empty ledger: V = %v, want [*] (case-insensitive)", got)
	}
	// The same session with ONE ledger record: that record's scope, and no `*`.
	d := NewDeriver()
	d.ToolInput("cairn ls-entries")
	d.Ledger([]byte(`{"schema":1,"scope":"beta-notes"}` + "\n"))
	if r := d.Result(); r.F1 || !slices.Equal(r.V, []string{"beta-notes"}) {
		t.Fatalf("one ledger record: F1=%v V=%v, want no F1 and [beta-notes]", r.F1, r.V)
	}
	// F1 does not fire on an unrelated session with an empty ledger.
	d = NewDeriver()
	d.ToolInput("ls -la")
	d.Ledger(nil)
	if r := d.Result(); r.F1 || len(r.V) != 0 {
		t.Fatalf("an unrelated session: F1=%v V=%v", r.F1, r.V)
	}
}

// TestF2CacheReadAddsStar: a file-tool read of the client cache root (mutant
// scopeuse-cache-path-read-ignored).
func TestF2CacheReadAddsStar(t *testing.T) {
	w := loadWorld(t)
	r := derive(t, w, "cc-f2-cache-read", false)
	if !r.F2 || !slices.Equal(r.V, []string{"*"}) {
		t.Fatalf("F2=%v V=%v, want F2 and [*]", r.F2, r.V)
	}
}

func TestTheLedgerFailsClosed(t *testing.T) {
	d := NewDeriver()
	d.Ledger([]byte("not json\n" + `{"scope":"../etc"}` + "\n" + `{"verb":"recall"}` + "\n" + `{"scope":"*"}` + "\n"))
	if r := d.Result(); !slices.Equal(r.V, []string{"*"}) {
		t.Fatalf("malformed/invalid ledger lines: V = %v, want [*]", r.V)
	}
}
