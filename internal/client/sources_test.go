package client

import (
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/api"
	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/codesrc"
	"github.com/ZacxDev/cairn/internal/netid"
)

// The `sources` verb (S3 of the scope-refs plan), driven through the REAL pod handler.

const (
	srcFirst  = "git:github.com/example-org/example-repo@main"
	srcSecond = "git:git.example.com/team/sub-group/example-repo@trunk"
)

// sourcesPod serves a synthetic store with `alpha-notes` and `beta-notes` through `api.New`.
// `journal` is the pod's `SourceJournal`: "" is the off state. The credential reads both scopes.
func sourcesPod(t *testing.T, journal string) *httptest.Server {
	t.Helper()
	root := t.TempDir()
	for _, scope := range []string{"alpha-notes", "beta-notes"} {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "gadget-one.md"), []byte("---\nservice: gadget-one\n"+
			"scope: "+scope+"\n---\n\n## What it is\nsynthetic.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, err := api.New(root, []authz.TokenRecord{
		{Token: arcWriterToken, Identity: "arc-writer", Scopes: []string{"alpha-notes", "beta-notes"}},
	}, []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
		netid.NewRateLimiter(1000000, time.Minute, 15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	srv.Audit, srv.Warn = func(string) {}, func(string) {}
	srv.SourceJournal = journal
	tsrv := httptest.NewServer(srv)
	t.Cleanup(tsrv.Close)
	return tsrv
}

// afterPreamble is the body with the pod's freshness preamble removed, so the literals below are
// the sources answer alone. The preamble is the report routes' own contract (`snapshot.Freshness`),
// pinned elsewhere.
func afterPreamble(t *testing.T, out string) string {
	t.Helper()
	_, rest, ok := strings.Cut(out, "\n\ncairn-sources: ")
	if !ok {
		t.Fatalf("no sources answer after a preamble:\n%s", out)
	}
	return "cairn-sources: " + rest
}

// TestTheSourcesVerbPrintsEachStateAsALiteral: the canonical list in declaration order,
// `sources=undeclared`, and the unconfigured sentence — each a LITERAL, each exit 0, each the
// pod's body VERBATIM (the property that keeps `report.SourcesReport.RenderText` the only renderer).
func TestTheSourcesVerbPrintsEachStateAsALiteral(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "sources.jsonl")
	if _, err := (codesrc.Journal{Path: journal}).Set("alpha-notes", []string{srcFirst, srcSecond},
		codesrc.RevisionNone, "scope-admin", time.Date(2000, 1, 3, 0, 0, 0, 0, time.UTC), nil); err != nil {
		t.Fatal(err)
	}
	tsrv := sourcesPod(t, journal)
	arcClientHost(t, tsrv.URL)

	rev := codesrc.Revision("alpha-notes", []string{srcFirst, srcSecond})
	code, out, errOut := runCLI(t, "sources", "--scope", "alpha-notes")
	want := "cairn-sources: status=sources-declared scope=alpha-notes\n" +
		"  provenance: a declaration is set in the browser by an admin of the scope and is that admin's word — the pod checks its grammar, never that the code exists\n" +
		"  journal=present\n" +
		"  set_by=scope-admin set_at=2000-01-03T00:00:00Z revision=" + rev + "\n" +
		"\n" +
		"sources=declared count=2\n" +
		"source=" + srcFirst + "\n" +
		"source=" + srcSecond + "\n"
	pod, _ := podBody(t, tsrv.URL, "/api/v1/sources/alpha-notes")
	if code != ExitOK || out != pod || afterPreamble(t, out) != want {
		t.Fatalf("declared: exit %d\n%s\n--- want (after the preamble)\n%s\n--- pod\n%s\n%s", code, out, want, pod, errOut)
	}

	code, out, errOut = runCLI(t, "sources", "--scope", "beta-notes")
	wantUndeclared := "cairn-sources: status=sources-undeclared scope=beta-notes\n" +
		"  provenance: a declaration is set in the browser by an admin of the scope and is that admin's word — the pod checks its grammar, never that the code exists\n" +
		"  journal=present\n" +
		"\n" +
		"sources=undeclared\n"
	if code != ExitOK || afterPreamble(t, out) != wantUndeclared {
		t.Fatalf("undeclared: exit %d\n%q\nwant\n%q\n%s", code, afterPreamble(t, out), wantUndeclared, errOut)
	}

	off := sourcesPod(t, "")
	t.Setenv("CAIRN_URL", off.URL)
	code, out, errOut = runCLI(t, "sources", "--scope", "alpha-notes")
	wantOff := "cairn-sources: status=sources-unconfigured scope=alpha-notes\n" +
		"\n" +
		"CODE SOURCES ARE NOT CONFIGURED ON THIS POD — it was started without $CAIRN_SOURCE_JOURNAL, so no declaration can be shown. This is the designed off state, NOT 'this scope declares no sources'.\n"
	if code != ExitOK || afterPreamble(t, out) != wantOff {
		t.Fatalf("unconfigured: exit %d\n%q\nwant\n%q\n%s", code, afterPreamble(t, out), wantOff, errOut)
	}
}

// TestAnUnreadableSourcesJournalIsAPlainReadsExit3: the pod's 503 `store-unreachable` for a
// journal it cannot read is "nothing was read" — 3, the plain-read code (decision 1) — and never a
// 0 that prints nothing, nor the auditor's 10. POSITIVE CONTROL on the same host: a readable pod
// answers 0, so the 3 is the 503's and not a client that cannot reach anything.
func TestAnUnreadableSourcesJournalIsAPlainReadsExit3(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sources.jsonl")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	good := sourcesPod(t, filepath.Join(t.TempDir(), "never-written.jsonl"))
	arcClientHost(t, good.URL)
	if code, out, errOut := runCLI(t, "sources", "--scope", "alpha-notes"); code != ExitOK ||
		!strings.Contains(out, "  journal=absent\n") {
		t.Fatalf("positive control: a readable (absent) journal answers 0 with journal=absent: %d\n%s\n%s", code, out, errOut)
	}
	broken := sourcesPod(t, dir)
	t.Setenv("CAIRN_URL", broken.URL)
	code, out, errOut := runCLI(t, "sources", "--scope", "alpha-notes")
	if code != ExitUnreachableNoCache || out != "" || !strings.Contains(errOut, "answered HTTP 503") ||
		!strings.Contains(errOut, "code sources journal unreadable") {
		t.Fatalf("an unreadable journal must exit 3 naming the 503: %d\nstdout=%q\n%s", code, out, errOut)
	}
}

// TestTheSourcesVerbTakesItsFlagsAndNothingElse: `--scope`/`--repo` only. 🔴 In particular no
// `--no-sync` (nothing is cached to read) and nothing that could carry a declaration — the browser
// surface is the ONLY writer (decision 5), so a `--file`/`--text` here would be a second one.
func TestTheSourcesVerbTakesItsFlagsAndNothingElse(t *testing.T) {
	for _, argv := range [][]string{{"sources", "--scope", "alpha-notes"}, {"sources", "--repo", "."}} {
		if _, _, err := Parse(argv); err != nil {
			t.Fatalf("%v must parse: %v", argv, err)
		}
	}
	for _, argv := range [][]string{
		{"sources", "--no-sync"},
		{"sources", "--scope", "alpha-notes", "--file", "x"},
		{"sources", "--scope", "alpha-notes", "--text", srcFirst},
		{"sources", "--scope", "alpha-notes", srcFirst},
	} {
		if _, _, err := Parse(argv); err == nil {
			t.Fatalf("%v must be refused", argv)
		}
	}
}
