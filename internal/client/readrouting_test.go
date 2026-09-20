package client

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/report"
)

// THE READ VERBS ROUTE — declared difference 8's own gate, on the Go side.
//
// 🔴 EVERY TEST IN THIS FILE IS RED AT THE COMMIT BEFORE THE ROUTING LANDED, AND RED FOR THE
// SAME ONE REASON: the read verbs refused a multi-instance host outright at exit 11
// (`RefuseUnportedMultiInstance`), so none of them reached a banner, a cache or a rendered
// line. That is what makes them regression coverage rather than invariant guards — except
// where a test says otherwise about itself.
//
// ⚠ THEY RUN OFFLINE, WITH `--no-sync`. A fan-out that fetched would need two pods, which is
// the parity harness's job; what is measured here is WHICH cache each instance reads, whether
// its banner and its lines carry the alias, and that a ONE-instance host is untouched. The
// bytes against the oracle are the parity gate's claim, not this file's.

// twoInstanceHost builds a host with the default instance plus `secondary`, each with a cache
// root holding one scope and a stamp, and points `$HOME` at it so `CacheRootFor` resolves the
// sibling-directory layout for real rather than through an injected path.
func twoInstanceHost(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	// 🔴 CLEARED, NOT INHERITED. The operator running this suite may have a real
	// `CAIRN_MIRROR_ROOT`, and `doctor` reports a row about it — a test that read the
	// ambient value would pass or fail depending on whose machine it ran on.
	t.Setenv("CAIRN_MIRROR_ROOT", "")
	configDir := configuredHost(t, filepath.Join(home, "config"))
	addInstance(t, configDir, "secondary")
	seedCache(t, filepath.Join(home, ".cache", "subsystem-store"), "alpha-notes", "one")
	seedCache(t, filepath.Join(home, ".cache", "subsystem-store-secondary"), "gamma-notes", "two")
	return home
}

// oneInstanceHost is the same world with NO `instances/` directory — every host today.
func oneInstanceHost(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CAIRN_MIRROR_ROOT", "")
	configuredHost(t, filepath.Join(home, "config"))
	seedCache(t, filepath.Join(home, ".cache", "subsystem-store"), "alpha-notes", "one")
	return home
}

// seedCache writes one entry and a stamp, so `--no-sync` resolves to `cached` rather than to
// "no cache" — the only state that lets a read verb get as far as printing anything.
func seedCache(t *testing.T, root, scope, ref string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, scope), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nservice: " + ref + "\nscope: " + scope + "\n---\n\n" +
		"## What it is\n\nsynthetic.\n\n## Pointers\n\n- none\n\n" +
		"## Nuance / work-history\n\n- 2000-01-01: synthetic.\n"
	if err := os.WriteFile(filepath.Join(root, scope, ref+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, SyncStamp),
		[]byte("synced=2000-01-01T00:00:00Z\nrevision=abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// capture runs one verb with its own stdout buffer and a real file for stderr, because `Env`
// takes an `*os.File` there. Returns `(code, stdout, stderr)`.
func capture(t *testing.T, run func(Env, Options) (int, error), opts Options) (int, string, string) {
	t.Helper()
	var out bytes.Buffer
	errFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer errFile.Close()
	code, runErr := run(Env{
		Stdout: &out,
		Stderr: errFile,
		// Pinned, because the host line is the one line of a rendered report that is
		// deliberately machine-dependent and this repository is PUBLIC.
		Host: func() string { return "fixture-host-000000000000" },
	}, opts)
	if runErr != nil {
		t.Fatalf("the verb returned an error rather than an exit code: %v", runErr)
	}
	written, err := os.ReadFile(errFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	return code, out.String(), string(written)
}

func readOpts() Options {
	// `Mode` is what the PARSER defaults, so a test that left it empty would refuse at the
	// reader's own option ladder before reaching anything this file measures.
	return Options{
		Cache:   DefaultCacheRoot(),
		Timeout: DefaultTimeout,
		Repo:    ".",
		NoSync:  true,
		Mode:    report.DefaultMode,
	}
}

func TestLsEntriesWALKSEveryInstanceAndPrefixesTheLINE(t *testing.T) {
	// 🔴 THE PREFIX IS A `[alias] ` ON THE LINE, NOT A THIRD PATH SEGMENT, and that is the
	// half a "does the alias appear" check cannot see. A consumer splits these on `/`
	// expecting exactly two parts; `alias/scope/entry.md` would re-point every such split at
	// the wrong field while containing the alias just as happily.
	twoInstanceHost(t)
	code, stdout, stderr := capture(t, LsEntries, readOpts())
	if code != ExitOK {
		t.Fatalf("a readable two-instance host exits 0, got %d\n%s", code, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	want := []string{"[personal] alpha-notes/one.md", "[secondary] gamma-notes/two.md"}
	if len(lines) != len(want) {
		t.Fatalf("both instances must be walked; got %d line(s):\n%s", len(lines), stdout)
	}
	for i, line := range lines {
		if line != want[i] {
			t.Fatalf("line %d: got %q, want %q", i+1, line, want[i])
		}
		// The structural half: everything after the prefix still splits into exactly two.
		_, path, found := strings.Cut(line, "] ")
		if !found || strings.Count(path, "/") != 1 {
			t.Fatalf("the path half must stay `<scope>/<entry>.md`: %q", line)
		}
	}
	// Each instance's banner names its own alias, so "which store was that" is answerable.
	for _, alias := range []string{"personal", "secondary"} {
		if !strings.Contains(stderr, "cairn["+alias+"]: cached") {
			t.Fatalf("the banner must name instance %q:\n%s", alias, stderr)
		}
	}
}

func TestAOneInstanceHostIsUNLABELLEDOnEveryReadVerb(t *testing.T) {
	// 🔴 THE COMPATIBILITY GUARANTEE, ASSERTED AT THE VERB RATHER THAN AT THE RENDERER. The
	// label is gated on the instance COUNT, so a gate that asked the routing table instead —
	// or that labelled unconditionally — would change the bytes of every existing host's
	// output. `cairn[` and the caveat's clause are the two strings that can only appear when
	// something labelled.
	//
	// ⚠ IT IS AN INVARIANT GUARD ON THE `ls-entries`/`validate` HALF: those verbs printed
	// nothing labelled before this change either. It is REGRESSION coverage on `recall`,
	// which at the previous commit did not reach a rendered report at all here — but that is
	// a statement about the two-instance host, so the honest label for this test as a whole
	// is INVARIANT.
	oneInstanceHost(t)
	for name, verb := range map[string]func(Env, Options) (int, error){
		"ls-entries": LsEntries,
		"validate":   Validate,
		"recall":     func(e Env, o Options) (int, error) { return Report(e, o, false) },
	} {
		opts := readOpts()
		if name == "recall" || name == "validate" {
			opts.Scope = "alpha-notes"
		}
		_, stdout, stderr := capture(t, verb, opts)
		for _, forbidden := range []string{"cairn[", "instance ONLY", "[personal]"} {
			if strings.Contains(stdout+stderr, forbidden) {
				t.Fatalf("%s: a one-instance host must not print %q:\n%s\n%s",
					name, forbidden, stdout, stderr)
			}
		}
	}
}

func TestRecallROUTESItsScopeAndCARRIESTheCaveatsInstanceClause(t *testing.T) {
	// 🔴 THE TWO HALVES ARE INDEPENDENT AND BOTH ARE ASSERTED. Routing decides WHICH cache is
	// read — `gamma-notes` exists only under the `secondary` root, so a client that read the
	// default instance answers `scope-absent` for it — and the caveat clause is what tells the
	// reader which store the answer came from. A port that routed without labelling would pass
	// the first assertion and hand back a confident digest that names no instance.
	twoInstanceHost(t)
	dir := filepath.Join(os.Getenv("HOME"), "config")
	writeTable(t, dir, `{"alpha-notes": "personal", "gamma-notes": "secondary"}`)

	opts := readOpts()
	opts.Scope = "gamma-notes"
	code, stdout, stderr := capture(t, func(e Env, o Options) (int, error) {
		return Report(e, o, false)
	}, opts)
	if code != ExitOK {
		t.Fatalf("the routed scope is readable, so this exits 0, got %d\n%s", code, stderr)
	}
	// The routed store was read: the entry that lives ONLY on `secondary` is in the digest.
	if !strings.Contains(stdout, "two") {
		t.Fatalf("the SECONDARY instance's cache must be the one read:\n%s", stdout)
	}
	if strings.Contains(stdout, "status=scope-absent") {
		t.Fatalf("`scope-absent` means the DEFAULT instance was read:\n%s", stdout)
	}
	if !strings.Contains(stdout, "cairn[secondary]: cached") {
		t.Fatalf("the banner must name the routed instance:\n%s", stdout)
	}
	if !strings.Contains(stdout, "this run read the `secondary` instance ONLY") {
		t.Fatalf("the caveat must carry the multi-instance clause:\n%s", stdout)
	}
	// And the negative control on the routing half: the OTHER scope still resolves to the
	// default instance, so "it read secondary" is not "it always reads secondary".
	opts.Scope = "alpha-notes"
	_, stdout, stderr = capture(t, func(e Env, o Options) (int, error) {
		return Report(e, o, false)
	}, opts)
	if !strings.Contains(stdout, "cairn[personal]: cached") {
		t.Fatalf("a scope routed to the DEFAULT instance must read it:\n%s\n%s", stdout, stderr)
	}
	if !strings.Contains(stdout, "this run read the `personal` instance ONLY") {
		t.Fatalf("…and carry ITS alias in the caveat:\n%s", stdout)
	}
}

func TestValidateROUTESAndDoctorWALKS(t *testing.T) {
	// 🔴 TWO VERBS WITH TWO SHAPES, AND THE DIFFERENCE IS THE POINT. `validate` is a claim
	// about the BYTES of one cache, so it routes to one instance; `doctor` takes no scope, so
	// it walks every instance and names each row `<alias>/<check>` — a merged verdict would
	// let a healthy instance mask a broken one.
	twoInstanceHost(t)
	dir := filepath.Join(os.Getenv("HOME"), "config")
	writeTable(t, dir, `{"alpha-notes": "personal", "gamma-notes": "secondary"}`)

	opts := readOpts()
	opts.Scope = "gamma-notes"
	code, stdout, stderr := capture(t, Validate, opts)
	if code != ExitOK {
		t.Fatalf("a clean routed scope validates at 0, got %d\n%s", code, stderr)
	}
	// It found the file, which it can only have done in the SECONDARY cache.
	if !strings.Contains(stdout, "gamma-notes: 1 of 1 entry file(s) parse") {
		t.Fatalf("`validate` must read the ROUTED cache:\n%s", stdout)
	}
	if !strings.Contains(stderr, "cairn[secondary]: cached") {
		t.Fatalf("…and name it in the banner:\n%s", stderr)
	}

	// `doctor` walks both. `--no-sync` keeps it off the network; the row NAMES are what is
	// being measured, not the states.
	_, stdout, _ = capture(t, Doctor, readOpts())
	for _, want := range []string{"personal/reader-resolution", "secondary/reader-resolution"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("`doctor` must report a row per instance, named after it (%q):\n%s",
				want, stdout)
		}
	}
	// 🔴 AND THE MIRROR BELONGS TO THE DEFAULT INSTANCE ALONE. It is the pre-cutover local
	// store this host migrated FROM, so there is exactly one of it however many instances are
	// configured; reporting it under each would state one fact N times and make N-1 rows a
	// claim about a directory that instance has nothing to do with.
	//
	// ⚠ THE ROW EXISTS ON BOTH — `Collect` always emits one — SO THE ASSERTION IS ON ITS
	// CONTENT. A row-name check would pass for a `doctor` that reported the mirror twice.
	mirror := t.TempDir()
	t.Setenv("CAIRN_MIRROR_ROOT", mirror)
	_, stdout, _ = capture(t, Doctor, readOpts())
	if !strings.Contains(stdout, "personal/frozen-mirror") ||
		!strings.Contains(stdout, mirror) {
		t.Fatalf("the default instance's row must name the configured mirror:\n%s", stdout)
	}
	secondary := ""
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, "secondary/frozen-mirror") {
			secondary = line
		}
	}
	if secondary == "" {
		t.Fatalf("every instance gets a frozen-mirror ROW:\n%s", stdout)
	}
	if !strings.Contains(secondary, "no mirror is configured") || strings.Contains(secondary, mirror) {
		t.Fatalf("a non-default instance must NOT claim the default one's mirror: %q", secondary)
	}
}

func TestAFanOutREFUSESAnExplicitSharedCache(t *testing.T) {
	// 🔴 THE DAMAGE IS SILENT AND IT IS TO THE CACHE. Two snapshots unpacked into one root
	// interleave their scopes while `.sync-stamp` dates whichever synced last — a store that
	// can say neither what it holds nor how old it is. All three fan-out verbs refuse BEFORE
	// the walk, and all three are listed here because the refusal is per-call-site.
	//
	// ⚠ REGRESSION COVERAGE ONLY FOR THE THREE READ VERBS: `routes --check` already refused
	// (`refuseSharedCache` predates this change) and is included as the control that the
	// mechanism itself works, not as new coverage.
	twoInstanceHost(t)
	// `routes --check` needs a TABLE to grade — with none it refuses at 11 before the cache
	// question is reached, which would make its row here measure the wrong refusal.
	writeTable(t, filepath.Join(os.Getenv("HOME"), "config"),
		`{"alpha-notes": "personal", "gamma-notes": "secondary"}`)
	for name, verb := range map[string]func(Env, Options) (int, error){
		"sync":       Sync,
		"ls-entries": LsEntries,
		"doctor":     Doctor,
		"routes":     Routes,
	} {
		opts := readOpts()
		opts.Cache = filepath.Join(t.TempDir(), "shared")
		opts.CacheExplicit = true
		opts.Check = true // only `routes` reads it
		code, _, stderr := capture(t, verb, opts)
		if code != ExitUsage {
			t.Fatalf("%s: a fan-out with an explicit --cache is exit %d, got %d\n%s",
				name, ExitUsage, code, stderr)
		}
		if !strings.Contains(stderr, "would make them share one directory") {
			t.Fatalf("%s: the refusal must say WHY:\n%s", name, stderr)
		}
	}
	// The negative control: with no explicit `--cache` the same verbs do NOT refuse, so the
	// assertion above is about `--cache` and not about the fan-out existing at all.
	code, _, stderr := capture(t, LsEntries, readOpts())
	if code == ExitUsage {
		t.Fatalf("without --cache there is nothing to refuse:\n%s", stderr)
	}
}
