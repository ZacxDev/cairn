package capture

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// F1's `*` — "the session's inputs name the program and its read ledger is EMPTY" — is a property of
// ONE RUN: the ledger line that answers it lands later. These tests pin that a capture pass which
// runs BEFORE that line lands does not freeze `*` into the session's grow-only state, from where
// `route` used to write it into `Declared` (uploaded to the pod as `declared_scopes`, grow-only
// there too) and keep the session owner-only, on the default instance, for good (#309).
//
// The fixture `cc-f1-empty-ledger` is a Claude Code session whose tool input names `cairn`, whose
// ledger file is EMPTY and whose transcript names no scope, on a TWO-instance host — the only case
// in which `*` makes a session owner-only ([Decide]).

// f1World is run 1 of every case below: the F1 session, shipped once with its ledger still empty.
func f1World(t *testing.T) (env, *Agent, string, string, *bytes.Buffer) {
	t.Helper()
	w := loadWorld(t)
	e := materialize(t, w, "cc-f1-empty-ledger")
	id := w.Sessions["cc-f1-empty-ledger"].ID
	ledger := filepath.Join(e.ledger, id+".jsonl")
	if b, err := os.ReadFile(ledger); err != nil || len(bytes.TrimSpace(b)) != 0 {
		t.Fatalf("the fixture: the session's ledger must exist and be EMPTY (F1), got %q, %v", b, err)
	}
	var log bytes.Buffer
	a := newAgent(t, e, twoInstances, &fakeRunner{}, &log)
	mustRun(t, a)
	ss := a.State.Sessions[id]
	if ss == nil || !ss.Evidence.NamesProgram || ss.Held || ss.Instance != "personal" ||
		len(e.sink.records["personal/"+id+"/main"]) == 0 {
		t.Fatalf("the fixture: run 1 must ship the F1 session to personal: %+v\n%s", ss, log.String())
	}
	return e, a, id, ledger, &log
}

// TestAnF1PassDoesNotFreezeStarIntoTheSession (Case A, the regression): run 1 sees F1 and ships the
// session to the default instance; run 2 sees the ledger line that names the session's one real
// read. After run 2 the session's declared scopes carry no `*`, and it is routed by that scope:
//
//   - alpha-notes (the default instance's): it stays on personal, declared [alpha-notes];
//   - beta-notes (the other instance's): it MOVES — withdrawn from personal, re-shipped to client
//     through the existing move path, declared [beta-notes].
//
// RED on origin/main: run 1 wrote F1's `*` into the persisted `V`, `route` then wrote it into
// `Declared`, and run 2 kept the session on personal owner-only (`*` declared) in both arms.
func TestAnF1PassDoesNotFreezeStarIntoTheSession(t *testing.T) {
	for _, tc := range []struct{ scope, instance string }{
		{"alpha-notes", "personal"},
		{"beta-notes", "client"},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			e, a, id, ledger, log := f1World(t)
			ss := a.State.Sessions[id]
			if slices.Contains(ss.Declared, "*") || slices.Contains(e.sink.declared["personal/"+id], "*") {
				t.Errorf("run 1 DECLARED F1's `*` (state %v, upload %v): the pod's grow-only declared_scopes would "+
					"keep the session owner-only after its ledger lands", ss.Declared, e.sink.declared["personal/"+id])
			}
			if slices.Contains(ss.V, "*") {
				t.Errorf("run 1 PERSISTED F1's `*` in the session's grow-only V %v: every later run routes on it", ss.V)
			}

			if err := os.WriteFile(ledger, []byte(ledgerLine(id, "recall", tc.scope)+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			mustRun(t, a)
			ss = a.State.Sessions[id]
			if ss.Held || ss.Instance != tc.instance {
				t.Fatalf("run 2: bound to %q (held %v), want %s — the session must be routed by the scope its "+
					"ledger names, not by F1's `*` from run 1\n%s", ss.Instance, ss.Held, tc.instance, log.String())
			}
			if !slices.Equal(ss.Declared, []string{tc.scope}) {
				t.Fatalf("run 2: the session declares %v, want [%s]", ss.Declared, tc.scope)
			}
			up, shipped := e.sink.declared[tc.instance+"/"+id]
			if !shipped || !slices.Equal(up, []string{tc.scope}) {
				t.Fatalf("run 2: the upload to %s declared %v (shipped %v), want [%s]", tc.instance, up, shipped, tc.scope)
			}
			moved := slices.Contains(e.sink.withdrawn["personal"], id)
			if moved != (tc.instance != "personal") {
				t.Fatalf("run 2: withdrawn from personal = %v, want %v", moved, tc.instance != "personal")
			}
			if tc.instance != "personal" {
				full, _ := os.ReadFile(filepath.Join(e.claude, "-work-alpha", id+".jsonl"))
				if got := len(e.sink.records[tc.instance+"/"+id+"/main"]); got != lineCount(full) {
					t.Fatalf("run 2: the move re-shipped %d of %d main records to %s", got, lineCount(full), tc.instance)
				}
			}
		})
	}
}

// TestAnF1PassDoesNotFreezeStarForAnOpencodeSession — Case A at the opencode site, which persists
// `V` and routes separately from the Claude Code one (`agent.go` spells both). The root's headers
// name alpha-notes and nothing is ledgered, so run 1 is F1; run 2's root ledger names alpha-notes.
//
// RED on origin/main: run 2's upload still declared `*`.
func TestAnF1PassDoesNotFreezeStarForAnOpencodeSession(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "oc-root")
	var log bytes.Buffer
	a := newAgent(t, e, twoInstances, &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList}, &log)
	a.ClaudeRoot = ""
	a.OpencodeDirs = []string{"/work/alpha"}
	mustRun(t, a)
	root := w.Sessions["oc-root"].ID
	ss := a.State.Sessions[root]
	if ss == nil || !ss.Evidence.NamesProgram || ss.Instance != "personal" || !e.sink.has("personal", root) {
		t.Fatalf("the fixture: run 1 must ship the F1 opencode root to personal: %+v\n%s", ss, log.String())
	}
	if slices.Contains(ss.Declared, "*") || slices.Contains(ss.V, "*") {
		t.Errorf("run 1 froze F1's `*`: V %v, declared %v", ss.V, ss.Declared)
	}
	if err := os.WriteFile(filepath.Join(e.ledger, root+".jsonl"),
		[]byte(ledgerLine(root, "recall", "alpha-notes")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, a)
	ss = a.State.Sessions[root]
	if ss.Instance != "personal" || !slices.Equal(ss.Declared, []string{"alpha-notes"}) ||
		!slices.Equal(e.sink.declared["personal/"+root], []string{"alpha-notes"}) {
		t.Fatalf("run 2: bound to %q, declares %v, uploaded %v — want personal, [alpha-notes], [alpha-notes]\n%s",
			ss.Instance, ss.Declared, e.sink.declared["personal/"+root], log.String())
	}
}

// TestALedgeredStarIsStillDeclared (Case B) is an INVARIANT GUARD, not regression coverage: a `*`
// the LEDGER records — the client could not enumerate what the verb read — is a real visibility
// statement, so after the F1 run the session is still placed on the default instance and declares
// `*`, exactly as before the fix. Green on origin/main and after.
func TestALedgeredStarIsStillDeclared(t *testing.T) {
	e, a, id, ledger, log := f1World(t)
	if err := os.WriteFile(ledger, []byte(ledgerLine(id, "routes", "*")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, a)
	ss := a.State.Sessions[id]
	if ss.Held || ss.Instance != "personal" || !slices.Contains(ss.Declared, "*") ||
		!slices.Contains(e.sink.declared["personal/"+id], "*") {
		t.Fatalf("a ledgered `*`: bound to %q, declares %v, uploaded %v — want personal with `*` declared\n%s",
			ss.Instance, ss.Declared, e.sink.declared["personal/"+id], log.String())
	}
}

// TestACrossInstanceSessionAfterF1IsStillOwnerOnly (Case C, #308) is an INVARIANT GUARD: once the
// ledger lands naming scopes on BOTH instances, the session is placed on the default instance
// OWNER-ONLY and `*` IS declared — the only thing keeping the other instance's bytes from a reader
// who could not see them there. Nothing is moved. Green on origin/main and after.
func TestACrossInstanceSessionAfterF1IsStillOwnerOnly(t *testing.T) {
	e, a, id, ledger, log := f1World(t)
	body := ledgerLine(id, "recall", "alpha-notes") + "\n" + ledgerLine(id, "recall", "beta-notes") + "\n"
	if err := os.WriteFile(ledger, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, a)
	ss := a.State.Sessions[id]
	if ss.Held || ss.Instance != "personal" || !slices.Contains(ss.Declared, "*") ||
		!slices.Contains(e.sink.declared["personal/"+id], "*") {
		t.Fatalf("cross-instance after F1: bound to %q, declares %v, uploaded %v — want personal, owner-only\n%s",
			ss.Instance, ss.Declared, e.sink.declared["personal/"+id], log.String())
	}
	if e.sink.has("client", id) || slices.Contains(e.sink.withdrawn["personal"], id) {
		t.Fatal("a cross-instance session placed owner-only was moved or duplicated")
	}
	if !strings.Contains(strings.Join(ss.V, ","), "beta-notes") {
		t.Fatalf("the fixture: V %v does not hold the client scope", ss.V)
	}
}

// TestDecideSaysWhyItIsOwnerOnly pins [DecideRun]'s cause on the cases that decide whether `*` is
// declared. The row that matters most is F1 beside ONE recorded scope of the OTHER instance: F1's
// `*` forces the default instance this run, but the session's recorded scopes do not disagree, so
// it is not cross-instance and must not declare `*` (that would re-freeze #309 in another shape).
func TestDecideSaysWhyItIsOwnerOnly(t *testing.T) {
	for _, tc := range []struct {
		name     string
		v        []string
		f1       bool
		instance string
		by       OwnerOnlyCause
	}{
		{"F1 alone", nil, true, "personal", OwnerOnlyUnledgered},
		{"F1 and a scope of the other instance", []string{"beta-notes"}, true, "personal", OwnerOnlyUnledgered},
		{"a recorded star", []string{"*", "beta-notes"}, false, "personal", OwnerOnlyStar},
		{"a recorded star under F1", []string{"*"}, true, "personal", OwnerOnlyStar},
		{"two instances", []string{"alpha-notes", "beta-notes"}, false, "personal", OwnerOnlyCrossInstance},
		{"two instances under F1", []string{"beta-notes", "alpha-notes"}, true, "personal", OwnerOnlyCrossInstance},
		{"one scope, no F1", []string{"beta-notes"}, false, "client", NotOwnerOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := DecideRun(tc.v, tc.f1, Repo{}, twoInstances, RepoRules{})
			if d.Held || d.Instance != tc.instance || d.OwnerOnlyBy != tc.by || d.OwnerOnly != (tc.by != NotOwnerOnly) {
				t.Fatalf("%+v, want instance %s, cause %d", d, tc.instance, tc.by)
			}
			if want := tc.by == OwnerOnlyStar || tc.by == OwnerOnlyCrossInstance; d.DeclaresStar() != want {
				t.Fatalf("DeclaresStar() = %v, want %v (%+v)", d.DeclaresStar(), want, d)
			}
		})
	}
	// One instance: F1 changes nothing, as `*` never did there.
	if d := DecideRun(nil, true, Repo{}, oneInstance, RepoRules{}); d.OwnerOnly || d.Instance != "personal" {
		t.Fatalf("one instance under F1: %+v, want personal, not owner-only", d)
	}
}
