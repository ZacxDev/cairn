package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/codesrc"
	"github.com/ZacxDev/cairn/internal/snapshot"
)

// 🔴 THE SOURCES ROUTE (`claudedocs/plan-cairn-scope-refs.md`, decision 10), MEASURED OVER THE
// SERVED ANSWER WITH LITERAL BODIES. These are the CONTRACT WITNESSES for a Go-only route — the
// corpus goldens are change detectors — and they are the ONLY witnesses for the two states the
// one-boot corpus cannot send beside the authorised rows: the unconfigured 200 and the 503.

const (
	srcPrimary   = "git:github.com/example-org/example-repo@main"
	srcSecondary = "git:git.example.com/team/sub-group/example-repo@trunk"
)

// withSourceJournal configures a journal OUTSIDE the fixture store, as `cmd/cairn-server` would
// after `codesrc.ResolveJournalPath`, and seeds it through the ONE writer.
func withSourceJournal(t *testing.T, h *harness, seed map[string][]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sources.jsonl")
	h.srv.SourceJournal = path
	j := codesrc.Journal{Path: path}
	for scope, srcs := range seed {
		if _, err := j.Set(scope, srcs, codesrc.RevisionNone, "scope-admin", time.Date(2000, 1, 3, 0, 0, 0, 0, time.UTC), nil); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// bodyAfterFreshness strips the freshness preamble every report route carries — its own contract,
// pinned elsewhere — so the literal below is the sources answer and nothing else.
func bodyAfterFreshness(t *testing.T, h *harness, body string) string {
	t.Helper()
	_, prose := snapshot.Freshness(h.root)
	rest, ok := strings.CutPrefix(body, prose+"\n\n")
	if !ok {
		t.Fatalf("the body does not open with the freshness preamble:\n%s", body)
	}
	return rest
}

const sourcesDeclaredBody = "cairn-sources: status=sources-declared scope=alpha-notes\n" +
	"  provenance: a declaration is set in the browser by an admin of the scope and is that admin's word — the pod checks its grammar, never that the code exists\n" +
	"  journal=present\n" +
	"  set_by=scope-admin set_at=2000-01-03T00:00:00Z revision=sha256:%s\n" +
	"\n" +
	"sources=declared count=2\n" +
	"source=git:github.com/example-org/example-repo@main\n" +
	"source=git:git.example.com/team/sub-group/example-repo@trunk\n"

// The authorised read: 200, the canonical list IN DECLARATION ORDER, the stamp, and exit 0.
func TestAReaderGetsTheDeclaredSourcesInOrder(t *testing.T) {
	h := newHarness(t)
	withSourceJournal(t, h, map[string][]string{"alpha-notes": {srcPrimary, srcSecondary}})
	got := h.do(t, "GET", "/api/v1/sources/alpha-notes", wideToken, nil, "")
	if got.status != 200 || got.headers.Get("X-Store-Status") != "sources-declared" || got.headers.Get("X-Store-Exit") != "0" {
		t.Fatalf("GET: %d %s exit=%s\n%s", got.status, got.headers.Get("X-Store-Status"), got.headers.Get("X-Store-Exit"), got.body)
	}
	rev := strings.TrimPrefix(codesrc.Revision("alpha-notes", []string{srcPrimary, srcSecondary}), "sha256:")
	want := strings.Replace(sourcesDeclaredBody, "%s", rev, 1)
	if body := bodyAfterFreshness(t, h, got.body); body != want {
		t.Fatalf("body:\n%s\nwant:\n%s", body, want)
	}
}

// The state matrix, each a literal: unconfigured; a scope with no record over a journal that does
// not exist yet (`journal=absent`); and a scope with no record over a journal that does.
func TestTheSourcesStatesAreEachALiteral(t *testing.T) {
	t.Run("unconfigured", func(t *testing.T) {
		h := newHarness(t)
		got := h.do(t, "GET", "/api/v1/sources/alpha-notes", wideToken, nil, "")
		want := "cairn-sources: status=sources-unconfigured scope=alpha-notes\n\n" +
			"CODE SOURCES ARE NOT CONFIGURED ON THIS POD — it was started without $CAIRN_SOURCE_JOURNAL, so no declaration can be shown. This is the designed off state, NOT 'this scope declares no sources'.\n"
		if got.status != 200 || got.headers.Get("X-Store-Status") != "sources-unconfigured" || got.headers.Get("X-Store-Exit") != "0" {
			t.Fatalf("%d %s", got.status, got.headers.Get("X-Store-Status"))
		}
		if body := bodyAfterFreshness(t, h, got.body); body != want {
			t.Fatalf("body:\n%q\nwant:\n%q", body, want)
		}
	})
	t.Run("absent journal", func(t *testing.T) {
		h := newHarness(t)
		h.srv.SourceJournal = filepath.Join(t.TempDir(), "never-written.jsonl")
		got := h.do(t, "GET", "/api/v1/sources/beta-notes", wideToken, nil, "")
		want := "cairn-sources: status=sources-undeclared scope=beta-notes\n" +
			"  provenance: a declaration is set in the browser by an admin of the scope and is that admin's word — the pod checks its grammar, never that the code exists\n" +
			"  journal=absent\n\nsources=undeclared\n"
		if got.status != 200 || got.headers.Get("X-Store-Status") != "sources-undeclared" {
			t.Fatalf("%d %s", got.status, got.headers.Get("X-Store-Status"))
		}
		if body := bodyAfterFreshness(t, h, got.body); body != want {
			t.Fatalf("body:\n%q\nwant:\n%q", body, want)
		}
	})
	t.Run("present journal, no record", func(t *testing.T) {
		h := newHarness(t)
		withSourceJournal(t, h, map[string][]string{"alpha-notes": {srcPrimary}})
		got := h.do(t, "GET", "/api/v1/sources/beta-notes", wideToken, nil, "")
		want := "cairn-sources: status=sources-undeclared scope=beta-notes\n" +
			"  provenance: a declaration is set in the browser by an admin of the scope and is that admin's word — the pod checks its grammar, never that the code exists\n" +
			"  journal=present\n\nsources=undeclared\n"
		if body := bodyAfterFreshness(t, h, got.body); got.status != 200 || body != want {
			t.Fatalf("%d body:\n%q\nwant:\n%q", got.status, body, want)
		}
	})
}

// 🔴 THE BROKEN STATE: a DIRECTORY at the configured journal path is "could not look" — arcs'
// `storeUnreachable` answer EXACTLY: 503, `X-Store-Status: store-unreachable`, `X-Store-Exit: 3`,
// `text/plain`, and the `*JournalUnreadableError` text as the body. Never a 200 that reads as
// "undeclared", and no new wire token.
func TestAnUnreadableSourcesJournalIsTheStoreUnreachable503(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(t.TempDir(), "sources.jsonl")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	h.srv.SourceJournal = dir
	got := h.do(t, "GET", "/api/v1/sources/alpha-notes", wideToken, nil, "")
	want := "code sources journal unreadable: " + dir + " (read " + dir + ": is a directory) — this is NOT 'no sources declared'\n"
	if got.status != 503 || got.headers.Get("X-Store-Status") != "store-unreachable" ||
		got.headers.Get("X-Store-Exit") != "3" || got.headers.Get("Content-Type") != "text/plain; charset=utf-8" ||
		got.body != want {
		t.Fatalf("got %d status=%q exit=%q type=%q body=%q\nwant 503 store-unreachable 3 text/plain %q",
			got.status, got.headers.Get("X-Store-Status"), got.headers.Get("X-Store-Exit"),
			got.headers.Get("Content-Type"), got.body, want)
	}
}

// 🔴 REFUSED EQUALS ABSENT: the narrow reader cannot read `alpha-notes`, which HAS a declaration,
// and gets — header for header, `X-Store-Revision` included (alpha-notes carries a `.git/HEAD`) —
// exactly what it gets for a scope that never existed. POSITIVE CONTROL: the same credential reads
// `beta-notes`' declaration, so a handler that refused everything cannot pass.
func TestAScopeTheCallerCannotReadAnswersExactlyLikeAbsence(t *testing.T) {
	h := newHarness(t)
	withSourceJournal(t, h, map[string][]string{"alpha-notes": {srcPrimary}, "beta-notes": {srcSecondary}})
	refused := h.do(t, "GET", "/api/v1/sources/alpha-notes", narrowToken, nil, "")
	absent := h.do(t, "GET", "/api/v1/sources/ghost-void", narrowToken, nil, "")
	if refused.status != 200 || refused.headers.Get("X-Store-Status") != "scope-absent" {
		t.Fatalf("refused: %d %s\n%s", refused.status, refused.headers.Get("X-Store-Status"), refused.body)
	}
	if strings.Contains(refused.body, "example-repo") {
		t.Fatalf("the refused answer leaks the hidden declaration:\n%s", refused.body)
	}
	// The bodies differ ONLY by the path scope's own name, which the caller sent.
	if strings.ReplaceAll(refused.body, "alpha-notes", "ghost-void") != absent.body {
		t.Fatalf("refused differs from absent:\n%s\n---\n%s", refused.body, absent.body)
	}
	// Content-Length is NOT compared: the two path scopes differ in length, and the body
	// comparison above (with the caller's own scope name substituted) already covers it.
	for _, name := range []string{"X-Store-Status", "X-Store-Exit", "X-Store-Revision", "Content-Type"} {
		if refused.headers.Get(name) != absent.headers.Get(name) {
			t.Fatalf("%s: refused %q, absent %q", name, refused.headers.Get(name), absent.headers.Get(name))
		}
	}
	control := h.do(t, "GET", "/api/v1/sources/beta-notes", narrowToken, nil, "")
	if control.headers.Get("X-Store-Status") != "sources-declared" || !strings.Contains(control.body, "\nsource="+srcSecondary+"\n") {
		t.Fatalf("control: the narrow reader must read beta-notes' declaration:\n%s", control.body)
	}
}

// 🔴 A HIDDEN SCOPE'S DAMAGED LINE CHANGES NOTHING ON THE WIRE. The narrow reader may read
// `beta-notes` and not `alpha-notes`; a hand-damaged alpha line (a record whose revision no longer
// matches its sources) and a torn tail are appended, and the narrow reader's `beta-notes` answer is
// BYTE-IDENTICAL before and after — no flag, no count, no state flip. RED under round 0's design
// (`damaged=yes` and a fixed sentence on every configured answer). CONTROL: the damage is real —
// the pod's warn sink receives the counts.
func TestAHiddenScopesDamagedLineChangesNothingOnTheWire(t *testing.T) {
	h := newHarness(t)
	var warned []string
	h.srv.Warn = func(line string) { warned = append(warned, line) }
	path := withSourceJournal(t, h, map[string][]string{"alpha-notes": {srcPrimary}, "beta-notes": {srcSecondary}})
	before := h.do(t, "GET", "/api/v1/sources/beta-notes", narrowToken, nil, "")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	forged := `{"schema":1,"scope":"alpha-notes","sources":["` + srcSecondary + `"],"set_by":"x","set_at":"2000-01-04T00:00:00Z","revision":"sha256:00"}` + "\n"
	if _, err := f.WriteString(forged + `{"schema":1,"scope":"alpha-no`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	after := h.do(t, "GET", "/api/v1/sources/beta-notes", narrowToken, nil, "")
	if before.status != 200 || before.body != after.body {
		t.Fatalf("a hidden scope's damage changed the narrow reader's answer:\n--- before\n%s\n--- after\n%s", before.body, after.body)
	}
	for _, name := range []string{"X-Store-Status", "X-Store-Exit", "Content-Length"} {
		if before.headers.Get(name) != after.headers.Get(name) {
			t.Fatalf("%s moved: %q -> %q", name, before.headers.Get(name), after.headers.Get(name))
		}
	}
	if strings.Contains(after.body, "damaged") {
		t.Fatalf("the body speaks of damage:\n%s", after.body)
	}
	if len(warned) == 0 || !strings.Contains(warned[len(warned)-1], "1 unreadable record(s) skipped, torn tail: true") {
		t.Fatalf("control: the damage must reach the pod log with its counts, got %q", warned)
	}
}

// 🔴 READ-ONLY: the pod never needs to write the journal. A file the pod's user cannot write (mode
// 0444) still answers — the read opens `O_RDONLY` — and is byte-identical afterwards.
func TestAReadOnlyJournalStillAnswers(t *testing.T) {
	h := newHarness(t)
	path := withSourceJournal(t, h, map[string][]string{"alpha-notes": {srcPrimary}})
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	got := h.do(t, "GET", "/api/v1/sources/alpha-notes", wideToken, nil, "")
	if got.status != 200 || got.headers.Get("X-Store-Status") != "sources-declared" {
		t.Fatalf("a read-only journal: %d %s\n%s", got.status, got.headers.Get("X-Store-Status"), got.body)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("a GET changed the journal")
	}
}

// 🔴 T9'S PIN 3 — `internal/api` CANNOT BECOME A SECOND WRITER. This package's non-test source
// holds ZERO call sites of any method named `Set`, which is the only spelling `codesrc.Journal.Set`
// can take. ⚠ SCOPED TO `internal/api`: it walks this directory only, so a write reached through
// another package (or from `cmd/cairn-server`) is outside what it sees. ⚠ AN INVARIANT GUARD, LABELLED AS ONE: measured 0 before this route existed, and the route
// did not change it. It is a GROW guard on a set of size zero: any `.Set(` call — a journal write or
// anything else — is refused and must be argued for here. A method value (`f := j.Set`) is caught
// too, because it is the selector, not the call, that is counted.
func TestThePodHasNoCallSiteOfJournalSet(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var walked int
	var sites []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		walked++
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Set" {
				sites = append(sites, fset.Position(sel.Pos()).String())
			}
			return true
		})
	}
	// Positive control on the walk: it must have read the package's real files.
	if walked < 3 {
		t.Fatalf("walked %d non-test file(s); the glob is not reading internal/api", walked)
	}
	if len(sites) != 0 {
		t.Fatalf("internal/api has %d selector(s) named Set — the pod must never write the sources journal: %v", len(sites), sites)
	}
}
