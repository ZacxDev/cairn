// Package racegate lets a package run a DECLARED set of its slow tests outside `-race`, and
// refuses — as a test — when the declaration's premise stops holding.
//
// 🔴 THE PREMISE, AT THE SCOPE IT CARRIES. The race detector reports a data race only between two
// goroutines it watched touch the same memory. A test that never starts a second goroutine, in a
// package whose own code starts none, gives it nothing to compare: under `-race` it costs 5–20×
// its plain run time and can report no race of OURS. That is the whole licence, and `Check` holds
// both halves of it: the package's NON-test code has no `go` statement, no channel and no
// `sync`/`sync/atomic` import, and no exempt test's OWN body starts a goroutine, calls
// `Parallel()`, or calls a declared concurrent helper.
//
// ⚠ WHAT `Check` DELIBERATELY DOES NOT CATCH: transitive reach — an exempt test calling a NEW
// helper that starts a goroutine, or an imported package becoming concurrent; concurrent use of a
// shared Redactor stays covered by the `measureCorpus` tests and the importers' race runs.
//
// HOW A PACKAGE USES IT: one `TestMain` calling `Main` with the package's exempt list, and one
// test calling `Check` with the same list plus its known concurrent functions. Three runs then see
// the list:
//
//   - `go test -race` SKIPS the exempt tests (via `-test.skip`), and says so on stdout;
//   - `CAIRN_RACE_EXEMPT_ONLY=1 go test` (no `-race`) runs EXACTLY the exempt tests (via
//     `-test.run`) and prints `racegate: exempt-only <N>` so the caller can count them back;
//   - any other plain `go test` runs everything, as before.
//
// The `go` CI job runs the first over `./...` and the second over every package importing this
// one, and the second step refuses unless the `--- PASS` names equal the declared names. So no
// test is dropped: a name is either outside the list (runs under `-race`) or inside it (runs in
// the exempt-only step), and both runs read the same Go slice. To put the exempt tests back under
// the detector, pass a `-skip` of your own (`go test -race -skip '^$'`): a caller's `-skip` wins.
//
// ⚠ PASS `-count=1` WHEN THE VARIABLE CHANGES BETWEEN RUNS. It is read in `TestMain` before
// `m.Run`, which is outside the window where `go test` records the environment a test read, so
// its cache does not key on it — measured: a plain run after an exempt-only one replayed the
// exempt-only output, `racegate:` line and all.
//
// ⚠ IT IS TEST SUPPORT AND NOTHING IN A SERVING PATH IMPORTS IT — the same position as
// `internal/testcookie`: `depspolicy.ImportGraph` reads `pkg.Imports`, which excludes test imports.
package racegate

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
)

// OnlyEnv, set to "1" on a run WITHOUT `-race`, restricts the run to the exempt tests.
const OnlyEnv = "CAIRN_RACE_EXEMPT_ONLY"

// flagAccess is the two `testing` flags `plan` reads and writes, behind an interface so the
// decision is testable without re-executing a test binary.
type flagAccess interface {
	get(name string) string
	set(name, value string) error
}

type realFlags struct{}

func (realFlags) get(name string) string {
	if f := flag.Lookup(name); f != nil {
		return f.Value.String()
	}
	return ""
}

func (realFlags) set(name, value string) error { return flag.Set(name, value) }

// Main is a package's whole `TestMain` body: `os.Exit(racegate.Main(m, raceExempt))`. A refusal
// (a malformed list, or exempt-only requested under `-race`) exits 2 — "could not vouch" — before
// any test runs.
func Main(m *testing.M, exempt []string) int {
	flag.Parse()
	note, err := plan(Enabled, os.Getenv(OnlyEnv) == "1", exempt, realFlags{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "racegate: REFUSING:", err)
		return 2
	}
	if note != "" {
		fmt.Println(note)
	}
	return m.Run()
}

// Pattern is the anchored `-test.run`/`-test.skip` expression matching exactly the names given,
// at the top level only (it has no `/`, so no subtest level is constrained).
func Pattern(names []string) string {
	return "^(" + strings.Join(names, "|") + ")$"
}

// plan validates the list and sets the one flag the mode needs. It returns the line to print.
func plan(race, only bool, exempt []string, fl flagAccess) (string, error) {
	if err := validate(exempt); err != nil {
		return "", err
	}
	switch {
	case race && only:
		return "", fmt.Errorf("%s=1 under -race: the exempt tests are the ones that must run WITHOUT the detector", OnlyEnv)
	case only:
		if fl.get("test.run") != "" {
			return "", fmt.Errorf("%s=1 with -run %q: the run set is the declared list, not a second one", OnlyEnv, fl.get("test.run"))
		}
		if err := fl.set("test.run", Pattern(exempt)); err != nil {
			return "", err
		}
		return fmt.Sprintf("racegate: exempt-only %d: %s", len(exempt), strings.Join(exempt, " ")), nil
	case race:
		if s := fl.get("test.skip"); s != "" {
			// The caller's own -skip wins; the exempt tests then run UNDER -race, which is the
			// safe direction (more detector coverage, never less).
			return fmt.Sprintf("racegate: -skip %q given: the %d exempt test(s) are NOT skipped", s, len(exempt)), nil
		}
		if err := fl.set("test.skip", Pattern(exempt)); err != nil {
			return "", err
		}
		return fmt.Sprintf("racegate: -race skips %d declared single-goroutine test(s), run without it by %s=1: %s",
			len(exempt), OnlyEnv, strings.Join(exempt, " ")), nil
	}
	return "", nil
}

// validate refuses an empty list, a duplicate, and any name that is not a plain Go test
// identifier — the last because the name is spliced into a regular expression, where a `.` or a
// `|` would widen the set the two runs agree on.
func validate(exempt []string) error {
	if len(exempt) == 0 {
		return fmt.Errorf("the exempt list is empty; a package with nothing to exempt does not call racegate.Main")
	}
	seen := map[string]bool{}
	for _, n := range exempt {
		if !isTestName(n) {
			return fmt.Errorf("%q is not a top-level Test identifier", n)
		}
		if seen[n] {
			return fmt.Errorf("%q is listed twice", n)
		}
		seen[n] = true
	}
	return nil
}

func isTestName(n string) bool {
	if !strings.HasPrefix(n, "Test") {
		return false
	}
	for _, r := range n {
		if !(r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}
