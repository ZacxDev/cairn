package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/codesrc"
	"github.com/ZacxDev/cairn/internal/refaudit"
)

// withAuditor installs the auditor the way `cmd/cairn` does (a field-for-field copy into
// `refaudit.Execute`). A test file may import `internal/refaudit`; the non-test graph may not.
func withAuditor(t *testing.T) {
	t.Helper()
	SetAuditRunner(func(req AuditRequest) (string, string, int) {
		entries := make([]refaudit.EntryInput, 0, len(req.Entries))
		for _, e := range req.Entries {
			entries = append(entries, refaudit.EntryInput{Ref: e.Ref, Text: e.Text, Refs: e.Refs})
		}
		return refaudit.Execute(refaudit.Run{SourcesStatus: req.SourcesStatus, SourcesDetail: req.SourcesDetail,
			JournalAbsent: req.JournalAbsent, Sources: req.Sources, Entries: entries, EntriesDetail: req.EntriesDetail,
			Hosts: req.Hosts, GitHubAPI: req.GitHubAPI, Token: req.Token, MirrorRoot: req.MirrorRoot,
			Timeout: req.Timeout, JSON: req.JSON, Label: req.Label})
	})
	t.Cleanup(func() { SetAuditRunner(nil) })
}

// TestTheAuditEnvNamesAreTheLibrarysOwn: the client's ledger spells the two auditor variables as
// literals (the env-read ledger reads its constants from source), so they are pinned to the library's.
func TestTheAuditEnvNamesAreTheLibrarysOwn(t *testing.T) {
	if AuditHostsEnv != refaudit.EnvHosts || AuditGitHubAPIEnv != refaudit.EnvGitHubAPI {
		t.Fatalf("%q/%q vs the library's %q/%q", AuditHostsEnv, AuditGitHubAPIEnv, refaudit.EnvHosts, refaudit.EnvGitHubAPI)
	}
}

// TestAuditRefsCouldNotLookIsTenForEveryScopeLevelReason: an undeclared scope, an unconfigured pod
// and an UNREADABLE journal (the pod's 503 `store-unreachable`) each exit 10 naming its reason — never
// 0, never 2, and never the plain read's 3 (decision 1: the auditor follows `arcsCheck`). POSITIVE
// CONTROL: the same host's plain `sources` verb exits 3 on the 503, so the 10 is the auditor's mapping.
func TestAuditRefsCouldNotLookIsTenForEveryScopeLevelReason(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "sources.jsonl")
	if _, err := (codesrc.Journal{Path: journal}).Set("alpha-notes", []string{srcFirst}, codesrc.RevisionNone,
		"scope-admin", time.Date(2000, 1, 3, 0, 0, 0, 0, time.UTC), nil); err != nil {
		t.Fatal(err)
	}
	withAuditor(t)
	declared := sourcesPod(t, journal)
	arcClientHost(t, declared.URL)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	code, out, errOut := runCLI(t, "audit-refs", "--scope", "beta-notes")
	if code != refaudit.ExitCouldNotLook || !strings.HasPrefix(out, "sources=undeclared\n") ||
		!strings.Contains(out, "could-not-look reason=sources=undeclared\n") {
		t.Fatalf("undeclared: %d\n%s\n%s", code, out, errOut)
	}

	off := sourcesPod(t, "")
	t.Setenv("CAIRN_URL", off.URL)
	code, out, _ = runCLI(t, "audit-refs", "--scope", "alpha-notes")
	if code != refaudit.ExitCouldNotLook || !strings.HasPrefix(out, "sources-unconfigured\n") {
		t.Fatalf("unconfigured: %d\n%s", code, out)
	}

	dir := filepath.Join(t.TempDir(), "sources.jsonl")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	broken := sourcesPod(t, dir)
	t.Setenv("CAIRN_URL", broken.URL)
	code, out, errOut = runCLI(t, "audit-refs", "--scope", "alpha-notes")
	if code != refaudit.ExitCouldNotLook || !strings.HasPrefix(out, "sources-unreadable\n") ||
		!strings.Contains(errOut, "answered HTTP 503") {
		t.Fatalf("unreadable journal: %d\n%s\n%s", code, out, errOut)
	}
	if code, _, _ := runCLI(t, "sources", "--scope", "alpha-notes"); code != ExitUnreachableNoCache {
		t.Fatalf("POSITIVE CONTROL: the plain read exits %d on the same 503, want 3", code)
	}
}

// TestAuditRefsRefusesABadHostMapAsUsage: a malformed $CAIRN_AUDIT_HOSTS is a command-line-class
// problem (2) found before any fetch of a remote, and an ssh base is refused (T1: https and file only).
func TestAuditRefsRefusesABadHostMapAsUsage(t *testing.T) {
	withAuditor(t)
	tsrv := sourcesPod(t, "")
	arcClientHost(t, tsrv.URL)
	for _, bad := range []string{"github.com", "github.com=ssh://git@github.com", "=https://x.example"} {
		t.Setenv(AuditHostsEnv, bad)
		if code, _, errOut := runCLI(t, "audit-refs", "--scope", "alpha-notes"); code != ExitUsage ||
			!strings.Contains(errOut, AuditHostsEnv) {
			t.Fatalf("%q: %d %s", bad, code, errOut)
		}
	}
}

// TestABinaryWithoutTheAuditorRefusesLoudly: `audit-refs` with no runner installed is an error naming
// the omission — never an exit 0 that printed nothing.
func TestABinaryWithoutTheAuditorRefusesLoudly(t *testing.T) {
	SetAuditRunner(nil)
	tsrv := sourcesPod(t, "")
	arcClientHost(t, tsrv.URL)
	code, out, errOut := runCLI(t, "audit-refs", "--scope", "alpha-notes")
	if code == ExitOK || out != "" || !strings.Contains(errOut, "built without the auditor") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
}
