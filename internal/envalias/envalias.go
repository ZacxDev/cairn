// Package envalias is THE one definition of the `SUBSYSTEM_STORE_*` → `CAIRN_*`
// environment-variable rename, and of the deprecation window both names live in.
//
// 🔴 THERE ARE TWO SPELLINGS OF THIS LEDGER AND THAT IS PACKAGING, NOT DUPLICATION.
// `packages.cairn` installs the Python client script and `lib/` under `libexec` and
// nothing else, so the Python side CANNOT import a file under `internal/`. The second
// spelling is `lib/env_aliases.py`, and `tests/test_env_aliases.py` pins the two against
// each other — failing when the pair set GROWS *or* SHRINKS, and comparing the rendered
// warnings as WHOLE NORMALISED STRINGS rather than by keyword, because a guard on words
// is walkable by rewording. That two-spellings-plus-a-gate shape is the precedent
// `AGENTS.md` already blesses for `server/Dockerfile` against `flake.nix`'s `serverEnv`.
//
// # What the window is anchored to
//
// The old names keep working until the PYTHON CLIENT (`packages.cairn`) is retired —
// the arc's P8 milestone. That is an anchor a reader can CHECK: `packages.cairn` either
// exists in `flake.nix` or it does not. A date could not be checked, there is no semver
// here to hang it on (`flake.nix`: `version = self.shortRev`), and `tests/leakscan.py`
// refuses a dated stamp in this tree anyway.
//
// # 🔴 TWO PAIRS ARE NOT THE MECHANICAL PREFIX SWAP, AND ONE OF THEM WAS A LIVE COLLISION
//
// The obvious rule — strip `SUBSYSTEM_STORE_`, prepend `CAIRN_` — is WRONG twice, and it
// is written down here because it is exactly the kind of assumption that gets re-derived:
//
//   - `SUBSYSTEM_STORE_HOST` is the POD'S LISTEN ADDRESS (`--host`, default `0.0.0.0`).
//     `CAIRN_HOST` ALREADY EXISTS AND MEANS SOMETHING ELSE: it is the human-readable
//     machine LABEL, the first entry of `host_identity.HOST_LABEL_ENV`, read by
//     `host_label()` and rendered into client output. The mechanical swap would make a
//     pod try to bind to an operator's machine label, AND let a listen address hijack the
//     label that lands in rendered output. Hence `CAIRN_LISTEN_HOST`.
//   - `CAIRN_ROOT` would sit beside the live, client-side `CAIRN_CACHE_ROOT` and
//     `CAIRN_MIRROR_ROOT` and read as a third member of that family, when it is the POD's
//     store root and belongs to neither. Hence `CAIRN_STORE_ROOT`.
//
// # The resolution rules, which are the same in both spellings
//
//  1. The NEW name wins. The old name is read only when the new one is absent or blank.
//  2. An old name that is PRESENT AND NON-BLANK warns once per process — including when
//     it is shadowed by the new one, because "I set the new name" and "the old one is no
//     longer reachable by anything" are different claims and an operator wants both.
//     A blank value is how a caller UNSETS an alias; it changes no resolution, so it is
//     not a deprecation and does not warn.
//  3. Warnings are emitted sorted by NEW name. `tests/parity/harness.py` compares the two
//     clients' stderr BYTE-FOR-BYTE, so "whatever order the lookups happened in" is not
//     an option: the order has to be a property of the ledger, not of the call sequence.
//
// ⚠ ONE MEASURED BEHAVIOUR CHANGE, NAMED RATHER THAN LEFT TO BE FOUND. `Value` treats a
// PRESENT BUT EMPTY value as absent. Every Go call site already did (`envOr`, `envInt`,
// `netid.LimiterSettings`, `authz.LoadTokens` all test `!= ""`), and so did the Python
// client's `load_config`. The oracle's `main()` did NOT: `os.environ.get("SUBSYSTEM_STORE_ROOT",
// DEFAULT_STORE)` RETURNS `""` for an exported-empty variable, and `int(os.environ.get(
// "SUBSYSTEM_STORE_PORT", …))` raised `ValueError` on one. Routing both through this rule
// NARROWS the oracle toward what Go already did — it removes a divergence rather than
// creating one — and no conformance row sends an empty value for either.
package envalias

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// Pair is one rename: the name to use, and the name that still works.
type Pair struct {
	New string
	Old string
}

// Ledger is every renamed variable, SORTED BY NEW NAME.
//
// 🔴 THE ORDER IS LOAD-BEARING, NOT COSMETIC — see rule 3 in the package doc.
// `TestTheLedgerIsSortedByNewName` pins it, so a pair appended in the wrong place is a
// red test rather than a stderr diff discovered by the parity harness.
var Ledger = []Pair{
	{New: "CAIRN_CONFIG", Old: "SUBSYSTEM_STORE_CONFIG"},
	{New: "CAIRN_FAILURE_WINDOW_S", Old: "SUBSYSTEM_STORE_FAILURE_WINDOW_S"},
	{New: "CAIRN_LISTEN_HOST", Old: "SUBSYSTEM_STORE_HOST"},
	{New: "CAIRN_LOCKOUT_S", Old: "SUBSYSTEM_STORE_LOCKOUT_S"},
	{New: "CAIRN_MAX_FAILURES", Old: "SUBSYSTEM_STORE_MAX_FAILURES"},
	{New: "CAIRN_PORT", Old: "SUBSYSTEM_STORE_PORT"},
	{New: "CAIRN_STORE_ROOT", Old: "SUBSYSTEM_STORE_ROOT"},
	{New: "CAIRN_TOKEN", Old: "SUBSYSTEM_STORE_TOKEN"},
	{New: "CAIRN_TOKEN_FILE", Old: "SUBSYSTEM_STORE_TOKEN_FILE"},
	{New: "CAIRN_TRUSTED_PROXIES", Old: "SUBSYSTEM_STORE_TRUSTED_PROXIES"},
	{New: "CAIRN_URL", Old: "SUBSYSTEM_STORE_URL"},
}

// RemovalAnchor is WHEN the old names stop being read, in the only terms this repo can
// state it: a milestone a reader can check, never a date.
const RemovalAnchor = "the Python client (packages.cairn) is retired"

// envWarningFormat and fileWarningFormat are the two pinned warning texts.
//
// 🔴 ONE TEXT PER DESTINATION, USED WHETHER OR NOT THE NEW NAME SHADOWS THE OLD ONE.
// A second "…and it is being ignored because you also set X" wording would double the
// strings the cross-language gate has to pin and double the ways the two spellings can
// drift, for a sentence that is already true in both cases as written: "read only when
// … is unset" SAYS what happened when the new name is set.
//
// 🔴 `tests/test_env_aliases.py` EXTRACTS THESE TWO CONSTANTS FROM THIS FILE BY TEXT and
// compares the rendered result against `lib/env_aliases.py`'s. Editing one of them alone
// is a red test, which is the whole reason they are named constants rather than inline
// format strings at the two call sites below.
const (
	envWarningFormat = "$%s is a deprecated alias for $%s and is read only when $%s is unset. " +
		"Both are accepted until " + RemovalAnchor + "."
	fileWarningFormat = "%s in %s is a deprecated alias for %s and is read only when %s is absent " +
		"from that file. Both are accepted until " + RemovalAnchor + "."
)

// olds maps an old name to its replacement, for the reverse lookup `Deprecations` needs.
var olds = func() map[string]string {
	m := make(map[string]string, len(Ledger))
	for _, p := range Ledger {
		m[p.Old] = p.New
	}
	return m
}()

// news maps a new name to the old name it replaced.
var news = func() map[string]string {
	m := make(map[string]string, len(Ledger))
	for _, p := range Ledger {
		m[p.New] = p.Old
	}
	return m
}()

// Old is the deprecated spelling of `newName`, or "" if there is not one.
//
// It returns "" rather than panicking so that a caller passing a name that was never
// renamed — `CAIRN_ROUTES`, `CAIRN_UI_PORT` — gets plain single-name behaviour from
// `Value` instead of a crash. `TestEveryConvertedCallSiteNamesALedgerEntry` is what
// catches a typo'd name, because a typo here would otherwise read as "no alias".
func Old(newName string) string { return news[newName] }

// Value is the resolved value of `newName` over `env`: the new name if it is present and
// non-blank, else the old name, else "".
//
// Rule 1 of the package doc, and the ONLY place it is written. Every call site goes
// through this — none open-codes a fallback — because a predicate duplicated across call
// sites regenerates the same bug at every site.
func Value(env map[string]string, newName string) string {
	return ValueFrom(func(k string) string { return env[k] }, newName)
}

// ValueFrom is `Value` over an arbitrary getter, and is where the precedence rule actually
// lives — `Value`, `OSValue` and every wrapper below delegate to it.
//
// 🔴 IT EXISTS BECAUSE A SECOND SPELLING OF THE RULE ALREADY ESCAPED ONCE. `internal/client`
// resolves its config path through an injectable `func(string) string`, not a map, and the
// first cut of this package offered only the map form — so that call site kept a plain
// single-name lookup, the routing layer stopped seeing `SUBSYSTEM_STORE_CONFIG`, and a
// two-instance world silently collapsed to one. `tests/parity/harness.py` is what caught it,
// by diffing the two clients' bytes; nothing in the Go tree noticed. The remedy is the one
// the rules name: not a second patch, but one predicate every shape delegates to.
func ValueFrom(get func(string) string, newName string) string {
	if v := get(newName); strings.TrimSpace(v) != "" {
		return v
	}
	if old := news[newName]; old != "" {
		return get(old)
	}
	return ""
}

// Resolving wraps a getter so every lookup through it resolves aliases.
//
// For a call site that holds a `func(string) string` and passes it around — rather than
// calling `ValueFrom` at each read — so the alias rule cannot be lost at one of the reads.
func Resolving(get func(string) string) func(string) string {
	return func(newName string) string { return ValueFrom(get, newName) }
}

// ValueOr is `Value` with a default for "neither is set".
func ValueOr(env map[string]string, newName, fallback string) string {
	if v := Value(env, newName); v != "" {
		return v
	}
	return fallback
}

// Lookup is `Value` plus "was it set at all", for a caller that must distinguish an
// explicit empty string from an unset variable (`envInt`'s "not a number" refusal needs
// to know which, or a blank `CAIRN_PORT` exits 78 instead of taking the default).
func Lookup(env map[string]string, newName string) (string, bool) {
	v := Value(env, newName)
	return v, v != ""
}

// Environ is `os.Environ()` as a map, so the process environment can be fed to the same
// pure functions a test feeds a literal map to.
func Environ() map[string]string {
	out := map[string]string{}
	for _, entry := range os.Environ() {
		if key, value, found := strings.Cut(entry, "="); found {
			out[key] = value
		}
	}
	return out
}

// OSValue is `Value` over the process environment.
func OSValue(newName string) string { return ValueFrom(os.Getenv, newName) }

// OSValueOr is `ValueOr` over the process environment.
func OSValueOr(newName, fallback string) string {
	if v := OSValue(newName); v != "" {
		return v
	}
	return fallback
}

// OSDeprecations is `Deprecations` over the process environment.
func OSDeprecations() []string { return Deprecations(Environ()) }

// Deprecations is one warning line per OLD name that is present and non-blank in `env`,
// sorted by new name.
//
// It is a pure function of `env`: no process state, no emission. `WarnOnce` is what adds
// the once-per-process rule, and separating them is what lets a test assert the ORDER
// (rule 3) without reaching into package state.
func Deprecations(env map[string]string) []string {
	var present []Pair
	for old, newName := range olds {
		if strings.TrimSpace(env[old]) != "" {
			present = append(present, Pair{New: newName, Old: old})
		}
	}
	sort.Slice(present, func(i, j int) bool { return present[i].New < present[j].New })
	lines := make([]string, 0, len(present))
	for _, p := range present {
		lines = append(lines, EnvWarning(p))
	}
	return lines
}

// EnvWarning is the pinned text for an environment variable.
func EnvWarning(p Pair) string {
	return fmt.Sprintf(envWarningFormat, p.Old, p.New, p.New)
}

// FileWarning is the pinned text for a config-FILE key.
//
// 🔴 IT NAMES THE FILE, NOT `$VAR`. `SUBSYSTEM_STORE_URL=` inside
// `~/.config/subsystem-store/env` is not an exported variable, and telling an operator to
// "unset $SUBSYSTEM_STORE_URL" when the string lives in a file they have to EDIT sends
// them looking in the wrong place.
func FileWarning(p Pair, path string) string {
	return fmt.Sprintf(fileWarningFormat, p.Old, path, p.New, p.New)
}

// FileDeprecations is `Deprecations` for the keys of one config file.
func FileDeprecations(fromFile map[string]string, path string) []string {
	var present []Pair
	for old, newName := range olds {
		if strings.TrimSpace(fromFile[old]) != "" {
			present = append(present, Pair{New: newName, Old: old})
		}
	}
	sort.Slice(present, func(i, j int) bool { return present[i].New < present[j].New })
	lines := make([]string, 0, len(present))
	for _, p := range present {
		lines = append(lines, FileWarning(p, path))
	}
	return lines
}

// warned is the once-per-process-per-OLD-NAME set behind `WarnOnce`.
//
// Keyed on the WHOLE LINE rather than on the old name, so that the same variable
// deprecated in the environment AND named as a key in a config file produces both
// warnings — they say different things and send the operator to different places — while
// two lookups of the same variable produce one.
var (
	warnMu sync.Mutex
	warned = map[string]bool{}
)

// WarnOnce emits each line in `lines` through `emit`, at most once per process.
//
// The caller supplies `emit` because the destination differs and the format differs with
// it: the CLIs write bare lines to stderr, the pods write `subsystem-store-api: <line>`
// through their reload-safe sanitiser. A package that wrote to `os.Stderr` itself would
// have to grow a knob for the prefix, and then the prefix would be a second thing to keep
// in step across two languages.
func WarnOnce(lines []string, emit func(string)) {
	for _, line := range lines {
		warnMu.Lock()
		seen := warned[line]
		warned[line] = true
		warnMu.Unlock()
		if !seen {
			emit(line)
		}
	}
}

// ResetWarnedForTest clears the once-per-process set.
//
// Exported because `WarnOnce`'s whole contract is process-global state, and a test that
// could not clear it would be order-dependent: the second test to assert an emission
// would see nothing and pass for the wrong reason.
func ResetWarnedForTest() {
	warnMu.Lock()
	warned = map[string]bool{}
	warnMu.Unlock()
}
