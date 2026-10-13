package client

import (
	"os"
	"sort"

	"github.com/ZacxDev/cairn/internal/envalias"
	"github.com/ZacxDev/cairn/internal/hostid"
)

// THE CLIENT'S ENVIRONMENT-READ LEDGER (S3 of `claudedocs/plan-cairn-scope-mail.md`, round 5
// 🟡4a-iii): every environment variable this package and `cmd/cairn` read BY NAME is a constant
// listed in [EnvReads], and every read goes through ONE function, [getenv], or — for `$HOME` —
// through [homeDir].
//
// 🔴 WHY A LEDGER RATHER THAN A LIST IN A TEST. The scope-mail live probe (`tests/memo/live-probe.sh`)
// must run every client call with NONE of the operator's configuration reachable, and its isolation
// test decoys "every name the client reads". Before this file two of those names were not
// enumerable from code at all — `CAIRN_MIRROR_ROOT` was a string literal inside `MirrorRoot`, and
// `CAIRN_UI_URL`/`CAIRN_UI_TOKEN` did not exist — so the plan's "read from code" was a claim with no
// mechanism behind it. `TestEveryEnvironmentReadIsInTheLedger` (envreads_test.go) is the mechanism:
// it parses this package and `cmd/cairn` and fails on a read the ledger does not name (a new
// `os.Getenv("…")` anywhere, or `getenv` handed anything but a ledger constant) AND on a ledger name
// nothing reads, so the list cannot GROW or SHRINK away from the code. The probe's isolation test
// builds its decoys from [EnvReadsWithAliases] through `tests/memo/memotool env-reads`, never from a
// typed copy.
//
// WHAT THE LEDGER DECIDED, for each of the three "is this a read?" cases S2 carried over:
//
//   - `os.UserHomeDir()` IS one: it reads `$HOME` (on every platform this client ships for), and
//     `$HOME` decides the cache root, the default config path and the cursor's default state
//     directory. So [HomeEnv] is a ledger name and [homeDir] is its one read site.
//   - `reposcope`'s `os.Environ()` is NOT a read by name: `runGit` hands the WHOLE environment to
//     `git`, which reads its own (`GIT_DIR`, `GIT_CONFIG_*`, …) to answer `--repo`. It is a declared
//     WHOLE-ENVIRONMENT SITE in the test, not a ledger name — enumerating git's environment here
//     would be a copy of git's documentation that nothing could keep true. A caller who needs no git
//     influence passes `--scope`, which runs no git at all.
//   - `envalias.OSDeprecations()` is NOT a read that changes behaviour: it scans for the OLD
//     spellings of names already in the ledger to WARN, and every one of those is in
//     [EnvReadsWithAliases]. It too is a declared whole-environment site.
//
// ⚠ WHAT IS OUTSIDE IT, NAMED: `internal/hostid`'s [hostid.HostLabelEnv] (a display label in a
// rendered header, never configuration) is read by a package this one imports; it is listed in
// [EnvReadsWithAliases] so the probe's decoys cover it, and is otherwise not this ledger's. Go's
// `net/http` reads the proxy variables, and `crypto/x509` the CA variables — the probe passes those
// THROUGH on purpose (they change how the network is reached, never which configuration is read).
const (
	// HomeEnv is `$HOME`, read ONLY through [homeDir] (`os.UserHomeDir`).
	HomeEnv = "HOME"
	// MirrorRootEnv names the frozen pre-cutover store `doctor` checks (see [MirrorRoot]).
	MirrorRootEnv = "CAIRN_MIRROR_ROOT"
	// EnvUIURL and EnvUIToken are the memo listener's base URL and the agent's own narrowed journal
	// credential (scope-mail decisions 8 and 16): keys of the SAME per-instance env file as
	// [EnvURL]/[EnvToken], read through the same precedence. New names with NO alias — the alias
	// ledger (`internal/envalias`) does not move.
	EnvUIURL   = "CAIRN_UI_URL"
	EnvUIToken = "CAIRN_UI_TOKEN"
	// StateHomeEnv is where the memo cursor lives (decision 6): `$XDG_STATE_HOME/cairn/memo/…`,
	// defaulting to `~/.local/state` when unset — and the client read ledger beside it,
	// `…/cairn/read-ledger/` (`readledger.go`, whose two session-id names are in this ledger too).
	StateHomeEnv = "XDG_STATE_HOME"
	// The `audit-refs` verb's reads (S5 of the scope-refs plan). AuditHostsEnv is the host → base-URL
	// map and AuditGitHubAPIEnv the PR check's REST base — the SAME spellings as `refaudit.EnvHosts` /
	// `refaudit.EnvGitHubAPI`, pinned equal by `TestTheAuditEnvNamesAreTheLibrarysOwn` (the library
	// reads no environment itself). GHTokenEnv/GitHubTokenEnv are the PR check's credential, first set
	// wins, never printed, logged or sent to the pod. CacheHomeEnv is where the mirrors live:
	// `$XDG_CACHE_HOME/cairn/mirrors`, `~/.cache` when unset.
	AuditHostsEnv     = "CAIRN_AUDIT_HOSTS"
	AuditGitHubAPIEnv = "CAIRN_AUDIT_GITHUB_API"
	GHTokenEnv        = "GH_TOKEN"
	GitHubTokenEnv    = "GITHUB_TOKEN"
	CacheHomeEnv      = "XDG_CACHE_HOME"
)

// EnvReads is every environment variable the client reads BY NAME, sorted — the ledger.
func EnvReads() []string {
	out := []string{ConfigEnv, EnvURL, EnvToken, RoutesEnv, MirrorRootEnv, EnvUIURL, EnvUIToken,
		AuditHostsEnv, AuditGitHubAPIEnv, GHTokenEnv, GitHubTokenEnv, CacheHomeEnv,
		StateHomeEnv, HomeEnv,
		ClaudeSessionEnv, OpencodeSessionEnv}
	sort.Strings(out)
	return out
}

// EnvReadsWithAliases is [EnvReads] plus every deprecated spelling `internal/envalias` still
// resolves for one of them, plus the display-label names `internal/hostid` reads — every name a
// process environment could use to influence a client run. Sorted, no duplicates. The probe's
// isolation test exports a decoy for EACH of these.
func EnvReadsWithAliases() []string {
	seen := map[string]bool{}
	for _, name := range EnvReads() {
		seen[name] = true
		if old := envalias.OldName(name); old != "" {
			seen[old] = true
		}
	}
	for _, name := range hostid.HostLabelEnv {
		seen[name] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// getenv is THE process-environment read in this package: the value of a LEDGER name, with its
// deprecated alias resolved by `internal/envalias` (a name with no alias reads as itself; a blank
// value reads as unset). `TestEveryEnvironmentReadIsInTheLedger` refuses a call whose argument is
// not a constant [EnvReads] names.
func getenv(name string) string { return envalias.OSValue(name) }

// homeDir is THE `$HOME` read in this package, and the one place `os.UserHomeDir` is called.
func homeDir() (string, error) { return os.UserHomeDir() }
