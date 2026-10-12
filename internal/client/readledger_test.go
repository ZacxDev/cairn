package client

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// THE CLIENT READ LEDGER (S11 of `claudedocs/plan-cairn-plugins.md`, decision 3a). What these
// rows pin, and why each one exists:
//
//   - a SERVED call records the scope it RESOLVED — a bare `recall` in a repository records the
//     scope `--repo` derived, not the empty flag (mutant
//     `client-ledger-records-requested-not-resolved-scope`);
//   - a session id in the environment is what turns the write on, and a served call with one
//     writes a record (mutant `client-ledger-write-skipped`);
//   - a write that FAILS changes nothing the caller can see — stdout, stderr and the exit code are
//     byte-identical to a run with no session id (mutant `client-ledger-write-visible-in-output`);
//   - a refused call records nothing; `sync` records nothing; the verbs whose output names scopes
//     this client cannot enumerate record exactly one `*`.
//
// The other end of the file is `internal/transcript/scopeuse.(*Deriver).Ledger`;
// `TestTheLedgerFileIsWhatTheDeriverReads` holds the two together.

type ledgerKey struct{ Verb, Instance, Scope string }

// ledgerHost is a one-instance host whose table routes `alpha-notes` and `beta-notes` to it and
// `ghost-scope` to an instance it does not have (an UNROUTED scope, exit 11), a cache holding both, a repository named
// `alpha-notes` as the working directory, a scratch `XDG_STATE_HOME`, and NO session id.
func ledgerHost(t *testing.T) (home, state string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CAIRN_MIRROR_ROOT", "")
	t.Setenv("SUBSYSTEM_STORE_URL", "http://127.0.0.1:1")
	t.Setenv("SUBSYSTEM_STORE_TOKEN", "x")
	t.Setenv(ClaudeSessionEnv, "")
	t.Setenv(OpencodeSessionEnv, "")
	state = filepath.Join(home, "state")
	t.Setenv(StateHomeEnv, state)
	dir := configuredHost(t, filepath.Join(home, "config"))
	writeTable(t, dir, `{"alpha-notes": "`+DefaultAlias+`", "beta-notes": "`+DefaultAlias+`", "ghost-scope": "no-such-instance"}`)
	cache := filepath.Join(home, ".cache", "subsystem-store")
	seedCache(t, cache, "alpha-notes", "one")
	seedCache(t, cache, "beta-notes", "two")
	repo := filepath.Join(home, "repos", "alpha-notes")
	gitInit(t, repo)
	t.Chdir(repo)
	return home, state
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q", dir)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func ledgerPath(state, session string) string {
	return filepath.Join(state, "cairn", "read-ledger", session+".jsonl")
}

// readLedgerFile is every record of one ledger file; a missing file is nil.
func readLedgerFile(t *testing.T, path string) []LedgerRecord {
	t.Helper()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []LedgerRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec LedgerRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("a ledger line that does not parse: %q: %v", sc.Text(), err)
		}
		out = append(out, rec)
	}
	return out
}

func keys(recs []LedgerRecord) []ledgerKey {
	var out []ledgerKey
	for _, r := range recs {
		out = append(out, ledgerKey{r.Verb, r.Instance, r.Scope})
	}
	return out
}

// TestTheLedgerRecordsTheScopeEachServedCallResolved is the S11 test plan's table: for each verb,
// exactly the records for the scopes the call RESOLVED and touched.
//
// RED before S11: no ledger existed, so every row with a non-empty `want` fails (the bare-recall row
// is the one the requested-not-resolved mutant fails — it records the empty `--scope`).
func TestTheLedgerRecordsTheScopeEachServedCallResolved(t *testing.T) {
	_, state := ledgerHost(t)
	const session = "ses-ledger-0001"
	t.Setenv(ClaudeSessionEnv, session)
	path := ledgerPath(state, session)
	star := LedgerAllScopes
	for _, tc := range []struct {
		argv []string
		code int
		want []ledgerKey
	}{
		// 🔴 THE BARE CALL: no `--scope`, so the scope is the one `--repo .` derived.
		{[]string{"recall", "--no-sync"}, ExitOK, []ledgerKey{{"recall", DefaultAlias, "alpha-notes"}}},
		{[]string{"recall", "--no-sync", "--scope", "beta-notes"}, ExitOK, []ledgerKey{{"recall", DefaultAlias, "beta-notes"}}},
		{[]string{"search", "synthetic", "--no-sync", "--scope", "beta-notes"}, -1, []ledgerKey{{"search", DefaultAlias, "beta-notes"}}},
		{[]string{"search", "synthetic", "--no-sync", "--all-scopes"}, -1, []ledgerKey{{"search", DefaultAlias, star}}},
		{[]string{"sessions", "--no-sync", "--scope", "beta-notes"}, -1, []ledgerKey{{"sessions", DefaultAlias, "beta-notes"}}},
		// `validate` with no `--scope` validates EVERY held scope (whatever `--repo` derives), and
		// prints a line for each: one record per scope it touched.
		{[]string{"validate", "--no-sync"}, ExitOK, []ledgerKey{
			{"validate", DefaultAlias, "alpha-notes"}, {"validate", DefaultAlias, "beta-notes"}}},
		{[]string{"validate", "--no-sync", "--scope", "beta-notes"}, ExitOK, []ledgerKey{{"validate", DefaultAlias, "beta-notes"}}},
		{[]string{"ls-entries", "--no-sync"}, ExitOK, []ledgerKey{
			{"ls-entries", DefaultAlias, "alpha-notes"}, {"ls-entries", DefaultAlias, "beta-notes"}}},
		// Content verbs whose output names scopes this client does not enumerate: ONE `*`.
		{[]string{"doctor", "--no-sync"}, -1, []ledgerKey{{"doctor", "", star}}},
		{[]string{"routes"}, ExitOK, []ledgerKey{{"routes", "", star}}},
		// `sync` prints only banners: nothing (it fails here — the pod is a closed port).
		{[]string{"sync"}, -1, nil},
		// REFUSED calls: a bad flag, an unroutable scope, a scope the cache does not hold.
		{[]string{"recall", "--no-sync", "--bogus-flag"}, ExitUsage, nil},
		{[]string{"recall", "--no-sync", "--scope", "ghost-scope"}, ExitUnrouted, nil},
		{[]string{"append", "--scope", "ghost-scope", "--ref", "one", "--text", "x", "--session", "s"}, ExitUnrouted, nil},
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		code, _, stderr := runCLI(t, tc.argv...)
		if tc.code >= 0 && code != tc.code {
			t.Fatalf("%v: exit %d, want %d\n%s", tc.argv, code, tc.code, stderr)
		}
		got := keys(readLedgerFile(t, path))
		if !slices.Equal(got, tc.want) {
			t.Errorf("%v: the ledger holds %v, want %v", tc.argv, got, tc.want)
		}
	}
}

// TestTheWriteAndPodVerbsRecordWhatTheyTouched: the writes record the scope they wrote, the arc
// listings record `*` (they print arcs homed in other scopes), and a write the POD refuses records
// nothing. Through the real pod handler (`arcPod`).
func TestTheWriteAndPodVerbsRecordWhatTheyTouched(t *testing.T) {
	tsrv := arcPod(t, filepath.Join(t.TempDir(), "journal.jsonl"))
	home := arcClientHost(t, tsrv.URL)
	state := filepath.Join(home, "state")
	t.Setenv(StateHomeEnv, state)
	t.Setenv(OpencodeSessionEnv, "")
	const session = "ses-ledger-0002"
	t.Setenv(ClaudeSessionEnv, session)
	path := ledgerPath(state, session)
	payload := filepath.Join(home, "arc.json")
	if err := os.WriteFile(payload, []byte(`{"schema":1,"status":"open","members":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(home, "entry.md")
	if err := os.WriteFile(entry, []byte("---\nservice: gadget-two\nscope: alpha-notes\n---\n\n## What it is\nsynthetic.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	star := LedgerAllScopes
	for _, tc := range []struct {
		argv []string
		code int
		want []ledgerKey
	}{
		{[]string{"append", "--scope", "alpha-notes", "--ref", "gadget-one", "--text", "x", "--session", "s-1"}, ExitOK,
			[]ledgerKey{{"append", DefaultAlias, "alpha-notes"}}},
		{[]string{"create", "--scope", "alpha-notes", "--ref", "gadget-two", "--file", entry}, ExitOK,
			[]ledgerKey{{"create", DefaultAlias, "alpha-notes"}}},
		// The POD refuses this one (the entry exists): nothing was touched, nothing recorded.
		{[]string{"create", "--scope", "alpha-notes", "--ref", "gadget-two", "--file", entry}, -1, nil},
		{[]string{"arc-register", "--scope", "alpha-notes", "--slug", "gadget-rollout", "--from", payload}, ExitOK,
			[]ledgerKey{{"arc-register", DefaultAlias, "alpha-notes"}}},
		{[]string{"arcs", "--scope", "alpha-notes"}, ExitOK, []ledgerKey{{"arcs", DefaultAlias, star}}},
		{[]string{"arc-show", "--scope", "alpha-notes", "--slug", "gadget-rollout"}, ExitOK, []ledgerKey{{"arc-show", DefaultAlias, star}}},
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		code, out, stderr := runCLI(t, tc.argv...)
		if tc.code >= 0 && code != tc.code {
			t.Fatalf("%v: exit %d, want %d\n%s\n%s", tc.argv, code, tc.code, out, stderr)
		}
		if tc.code < 0 && code == ExitOK {
			t.Fatalf("%v: the fixture meant this call to be refused, it exited 0\n%s", tc.argv, out)
		}
		if got := keys(readLedgerFile(t, path)); !slices.Equal(got, tc.want) {
			t.Errorf("%v: the ledger holds %v, want %v", tc.argv, got, tc.want)
		}
	}
}

// TestARecordCarriesTheDeclaredFields pins the record's shape (decision 3a) and the modes.
func TestARecordCarriesTheDeclaredFields(t *testing.T) {
	_, state := ledgerHost(t)
	t.Setenv(OpencodeSessionEnv, "ses_opencode0003") // the SECOND variable alone also turns it on
	before := time.Now().UTC().Add(-time.Second)
	if code, _, stderr := runCLI(t, "recall", "--no-sync"); code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	path := ledgerPath(state, "ses_opencode0003")
	recs := readLedgerFile(t, path)
	if len(recs) != 1 {
		t.Fatalf("want one record, got %v", recs)
	}
	r := recs[0]
	at, err := time.Parse(time.RFC3339, r.At)
	if r.Schema != LedgerSchema || r.Session != "ses_opencode0003" || r.Client != "go" || r.ClientVersion == "" ||
		err != nil || at.Before(before.Truncate(time.Second)) {
		t.Fatalf("a record missing a declared field: %+v (at: %v)", r, err)
	}
	raw, _ := os.ReadFile(path)
	if want := `{"schema":1,"session":"ses_opencode0003","verb":"recall","instance":"personal","scope":"alpha-notes","client":"go",`; !strings.HasPrefix(string(raw), want) {
		t.Fatalf("the wire order moved — the oracle writes these keys in this order:\n%s", raw)
	}
	for p, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s: mode %v, want %v (%v)", p, info.Mode().Perm(), want, err)
		}
	}
	// CLAUDE_CODE_SESSION_ID WINS when both are set.
	t.Setenv(ClaudeSessionEnv, "ses-claude-0003")
	runCLI(t, "recall", "--no-sync")
	if got := readLedgerFile(t, ledgerPath(state, "ses-claude-0003")); len(got) != 1 {
		t.Fatalf("with both set, the Claude Code id names the file: %v", got)
	}
	if got := readLedgerFile(t, path); len(got) != 1 {
		t.Fatalf("…and nothing more is appended under the opencode id: %v", got)
	}
}

// TestNoSessionIdWritesNothing: with no session id no file — nor directory — is created. The
// control is the same call WITH an id, which does write (so the absence is not a broken fixture).
func TestNoSessionIdWritesNothing(t *testing.T) {
	_, state := ledgerHost(t)
	for _, bad := range []string{"", "../escape", "a/b", ".hidden"} {
		t.Setenv(ClaudeSessionEnv, bad)
		if code, _, stderr := runCLI(t, "recall", "--no-sync"); code != ExitOK {
			t.Fatalf("exit %d\n%s", code, stderr)
		}
		if _, err := os.Stat(state); !os.IsNotExist(err) {
			t.Fatalf("session id %q: the state directory exists (%v) — a call with no usable session id wrote", bad, err)
		}
	}
	t.Setenv(ClaudeSessionEnv, "ses-ledger-0004")
	runCLI(t, "recall", "--no-sync")
	if got := readLedgerFile(t, ledgerPath(state, "ses-ledger-0004")); len(got) != 1 {
		t.Fatalf("the positive control: a session id writes one record, got %v", got)
	}
}

// TestAFailedLedgerWriteIsInvisible: with the ledger directory read-only — or the state home a
// FILE, or the ledger file a dangling symlink — stdout, stderr and the exit code are byte-identical
// to a run with no session id, for a served call AND a refused one.
func TestAFailedLedgerWriteIsInvisible(t *testing.T) {
	_, state := ledgerHost(t)
	const session = "ses-ledger-0005"
	argvs := [][]string{{"recall", "--no-sync"}, {"ls-entries", "--no-sync"}, {"recall", "--no-sync", "--scope", "ghost-scope"}}
	type outcome struct {
		code        int
		out, errOut string
	}
	baseline := map[int]outcome{}
	t.Setenv(ClaudeSessionEnv, "")
	for i, argv := range argvs {
		c, o, e := runCLI(t, argv...)
		baseline[i] = outcome{c, o, e}
	}
	dir := filepath.Dir(ledgerPath(state, session))
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	breakers := map[string]func(t *testing.T){
		"read-only directory": func(t *testing.T) {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(dir, 0o700) })
		},
		"state home is a file": func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "not-a-dir")
			if err := os.WriteFile(file, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv(StateHomeEnv, file)
		},
		"ledger file is a dangling symlink": func(t *testing.T) {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, ledgerPath(state, session)); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Remove(ledgerPath(state, session)) })
		},
	}
	for name, breakIt := range breakers {
		t.Run(name, func(t *testing.T) {
			breakIt(t)
			t.Setenv(ClaudeSessionEnv, session)
			for i, argv := range argvs {
				c, o, e := runCLI(t, argv...)
				if got := (outcome{c, o, e}); got != baseline[i] {
					t.Errorf("%v: a failed ledger write is visible:\n  with: %+v\n  without: %+v", argv, got, baseline[i])
				}
			}
			if info, err := os.Lstat(ledgerPath(state, session)); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
				t.Errorf("the breaker did not break the write — the ledger holds bytes, so this row measured nothing")
			}
		})
	}
	// The dangling link's target was NOT created: `O_NOFOLLOW` refused it rather than following.
	if _, err := os.Stat(elsewhere); !os.IsNotExist(err) {
		t.Fatalf("the append FOLLOWED a symlink planted at the ledger's name: %v", err)
	}
}

// TestTheLedgerFileIsWhatTheDeriverReads: the agent's parser (`scopeuse`) reads `scope` and nothing
// else; this pins that what the client writes is one valid JSON object per line carrying it.
func TestTheLedgerFileIsWhatTheDeriverReads(t *testing.T) {
	_, state := ledgerHost(t)
	t.Setenv(ClaudeSessionEnv, "ses-ledger-0006")
	runCLI(t, "recall", "--no-sync", "--scope", "beta-notes")
	runCLI(t, "search", "synthetic", "--no-sync", "--all-scopes")
	raw, err := os.ReadFile(ledgerPath(state, "ses-ledger-0006"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("one line per record: %q", raw)
	}
	var scopes []string
	for _, l := range lines {
		var rec struct {
			Scope *string `json:"scope"`
		}
		if err := json.Unmarshal([]byte(l), &rec); err != nil || rec.Scope == nil {
			t.Fatalf("a line the deriver would read as `*`: %q (%v)", l, err)
		}
		scopes = append(scopes, *rec.Scope)
	}
	if !slices.Equal(scopes, []string{"beta-notes", "*"}) {
		t.Fatalf("scopes %v", scopes)
	}
}
