package client

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/doctor"
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
//
// 🔴 IT WRITES A `routes.json`, AND THAT IS THE WHOLE DIFFERENCE BETWEEN THIS FIXTURE AND A
// FIXTURE THAT MEASURES NOTHING. A one-instance host with NO table cannot separate "the label
// asks the instance COUNT" from "the label asks the TABLE": both predicates answer the same on
// it, so a gate that consulted `routing.Routes` was byte-identical to the correct one on every
// case built here. Measured at `d57f46b`: `instanceLabel` mutated to
// `if routing.Routes == nil && !routing.MultiInstance()` — the historical defect
// `Routing.MultiInstance`'s own comment names — SURVIVED at 54 PASS / 0 FAIL, byte-identical
// to the unmutated run, and so did the same edit to `doctorInstance`'s gate. A table present
// on a host with one instance is row 2 of `AliasFor`'s three-row table, and it is the only
// world in which the two questions have different answers.
//
// ⚠ THE TABLE MUST ROUTE `alpha-notes` TO AN ALIAS THIS HOST HAS. Routing it anywhere else is
// row 3, which REFUSES at exit 11 before any label is computed — a different claim, and one
// that would hide this one.
func oneInstanceHost(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CAIRN_MIRROR_ROOT", "")
	// Pinned at a closed port so `sync` — the one verb here that is not `--no-sync`able —
	// fails fast and locally instead of reaching whatever the operator's shell configured.
	t.Setenv("SUBSYSTEM_STORE_URL", "http://127.0.0.1:1")
	t.Setenv("SUBSYSTEM_STORE_TOKEN", "x")
	dir := configuredHost(t, filepath.Join(home, "config"))
	writeTable(t, dir, `{"alpha-notes": "`+DefaultAlias+`"}`)
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
	// output. `cairn[`, the caveat's clause, the `[personal] ` line prefix and a `personal/`
	// row name are the four strings that can only appear when something labelled.
	//
	// 🔴 AND THE HOST HAS A TABLE, WHICH IS WHAT MAKES THE FIRST HALF OF THAT SENTENCE
	// MEASURABLE. The docstring used to claim both halves while `oneInstanceHost` wrote no
	// `routes.json` — so "asked the table instead" was unreachable and only "labelled
	// unconditionally" was ever caught. See `oneInstanceHost` for the measurement.
	//
	// ⚠ THE INVOCATIONS BELOW COVER `instanceLabel`'S SIX CALL SITES, AND VERBS DO NOT MAP ONTO
	// THEM ONE-TO-ONE — WHICH IS WHY THERE ARE EIGHT OF THEM AND NOT SIX. An earlier draft of
	// this comment said "ALL SIX LABELLING VERBS, BECAUSE THE PREDICATE IS REACHED FROM SIX
	// PLACES", which reads as a 6↔6 correspondence and is false in both directions: `recall`,
	// scoped `validate` and `search` share ONE site, and `validate` alone reaches TWO. The
	// mapping, spelled out because nothing checks it mechanically:
	//
	//	verbs.go   Sync              <- `sync`
	//	verbs.go   LsEntries         <- `ls-entries`
	//	cli.go     doctorInstance    <- `doctor`
	//	routes.go  readInstance      <- `recall`, `validate`, `search` (all SCOPED)
	//	routes.go  defaultInstance   <- `validate` with NO scope and a non-repo `--repo`
	//	routes.go  bannerFor         <- `routes --check`
	//
	// 🔴 THE LAST TWO ROWS ARE WHY THE LIST GREW, AND THE FIRST OF THEM WAS A MEASURED HOLE.
	// Verified at b28787b8: mutating `defaultInstance`'s return to an unconditional
	// `DefaultAlias` label — the no-scope `cairn validate` path, which is what the command does
	// with no arguments outside a repo — SURVIVED the whole `./internal/client` package at rc 0.
	// `bannerFor` was unreached by the same reasoning and is now covered too; it needs
	// `--check`, because without it `Routes` returns before any banner is printed.
	//
	// ⚠ `doctor` IS THE ONE WHOSE ALIAS IS NOT IN ITS PROSE — it prefixes its ROW NAMES, the one
	// mechanism a machine consumer of `doctor --json` parses and the one a forbidden-substring
	// check does not see, which is why it gets an assertion of its own below.
	//
	// ⚠ IT IS AN INVARIANT GUARD ON THE `ls-entries`/`validate` HALF: those verbs printed
	// nothing labelled before this change either. It is REGRESSION coverage on `recall`,
	// which at the previous commit did not reach a rendered report at all here — but that is
	// a statement about the two-instance host, so the honest label for this test as a whole
	// is INVARIANT.
	home := oneInstanceHost(t)
	// A directory that is not a git repository, so `RepoScope` yields "" and `Validate` takes
	// its `defaultInstance` arm. `--repo .` would derive THIS checkout's scope and take the
	// `readInstance` arm instead, which is the arm three other rows already cover.
	plain := filepath.Join(home, "not-a-repo")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, invocation := range []struct {
		name string
		run  func(Env, Options) (int, error)
		with func(o *Options)
	}{
		{"ls-entries", LsEntries, nil},
		{"validate", Validate, func(o *Options) { o.Scope = "alpha-notes" }},
		{"recall", func(e Env, o Options) (int, error) { return Report(e, o, false) },
			func(o *Options) { o.Scope = "alpha-notes" }},
		{"search", func(e Env, o Options) (int, error) { return Report(e, o, true) },
			func(o *Options) { o.Scope, o.Query = "alpha-notes", "synthetic" }},
		{"sync", Sync, nil},
		{"doctor", Doctor, nil},
		{"validate (no scope)", Validate, func(o *Options) { o.Scope, o.Repo = "", plain }},
		{"routes --check", Routes, func(o *Options) { o.Check = true }},
	} {
		opts := readOpts()
		if invocation.with != nil {
			invocation.with(&opts)
		}
		_, stdout, stderr := capture(t, invocation.run, opts)
		for _, forbidden := range []string{"cairn[", "instance ONLY", "[personal]"} {
			if strings.Contains(stdout+stderr, forbidden) {
				t.Fatalf("%s: a one-instance host must not print %q:\n%s\n%s",
					invocation.name, forbidden, stdout, stderr)
			}
		}
		if invocation.name != "doctor" {
			continue
		}
		// 🔴 THE ROW NAME, NOT A SUBSTRING OF THE WHOLE REPORT. `doctor`'s alias lives in the
		// NAME column (`<alias>/<check>`), and a detail field may legitimately quote a path
		// containing the word — so the assertion is on the NAME column alone.
		//
		// 🔴 AND THE COLUMN IS FOUND BY STRIPPING THE RENDERER'S OWN MARKER, NOT BY
		// `strings.TrimSpace`. The previous spelling was
		// `strings.HasPrefix(strings.TrimSpace(line), DefaultAlias+"/")`, which reaches the
		// name on an `OK` row — marker `"  "` — and on NO other state, because `"🔴"`, `"⚠ "`
		// and `"· "` are not whitespace. Measured at b28787b8: this fixture renders 2 OK rows
		// and 5 non-OK ones, so the guard inspected 2 of 7 and a fixture change leaving no OK
		// row would have emptied it in silence. `doctor.Markers()` is the table `Render`
		// itself writes from.
		rows, nonOK := 0, 0
		for _, line := range strings.Split(stdout, "\n") {
			name, state := doctorRow(line)
			if name == "" {
				continue
			}
			rows++
			if state != doctor.OK {
				nonOK++
			}
			if strings.HasPrefix(name, DefaultAlias+"/") {
				t.Fatalf("doctor: a one-instance host must not prefix its row names: %q\n%s",
					line, stdout)
			}
		}
		// 🔴 THE POSITIVE CONTROL, BECAUSE THE ASSERTION ABOVE IS A REASSURING ZERO OTHERWISE.
		// "No row was prefixed" and "no row was read" are the same observation from a loop that
		// matched nothing, and the second one is what the `TrimSpace` spelling was one fixture
		// change away from. The non-OK count is the half that pins the marker handling: revert
		// `doctorRow` to the old spelling and this line goes red at `nonOK == 0`.
		if rows < 2 || nonOK == 0 {
			t.Fatalf("the row predicate read %d row(s), %d of them non-OK — it must inspect "+
				"EVERY state, not whichever this fixture happens to produce:\n%s",
				rows, nonOK, stdout)
		}
	}
}

// doctorRow splits one line of a rendered `doctor` report into its NAME and STATE columns, and
// returns `("", "")` for a line that is not a check row.
//
// 🔴 IT STRIPS THE MARKER THE RENDERER WROTE, WHICH IS THE WHOLE REASON IT EXISTS. `Render`
// emits `"<marker> <name>  <state> <detail>"`, and the four markers are neither the same width
// nor all whitespace — so both obvious spellings are wrong for some states and right for
// others: a fixed rune offset (`"🔴"` is ONE rune, the other three are two) and a `TrimSpace`
// (which reaches the name on `OK` rows only). The state is checked against `doctor.States` so a
// footer or legend line cannot be mistaken for a row.
func doctorRow(line string) (name, state string) {
	for _, marker := range doctor.Markers() {
		rest, found := strings.CutPrefix(line, marker)
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			continue
		}
		for _, known := range doctor.States {
			if fields[1] == known {
				return fields[0], known
			}
		}
	}
	return "", ""
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

// =============================================================================
// `search --all-scopes` — THE FAN-OUT.
//
// 🔴 THESE THREE EXIST BECAUSE `searchEveryInstance` SHIPPED WITH MEASURED-ZERO COVERAGE.
// Verified at 0c187d76: inserting `if true { return ExitOK, nil }` as its first statement — a
// fan-out that searches nothing and reports success — compiled and left this package at 51
// PASS / 0 FAIL, byte-identical to the unmutated run. The prose in `tests/parity/README.md`
// pointed a reader here for exactly this behaviour while nothing here referenced it.
//
// They are the Go spelling of the oracle's `TestSearchAcrossInstances`, ported as CLAIMS
// rather than as Python: every instance is searched and each section names its own; an
// UNREGISTERED scope does not refuse a search that named no scope; and an instance that could
// not be read is LOUD and non-zero.
//
// ⚠ EACH ONE ASSERTS OUTPUT AND NOT ONLY AN EXIT CODE, WHICH IS WHAT THE MUTANT ABOVE FORCES.
// `report.ExitFor` answers 0 for `search-hit` AND for `search-no-match`, so a fan-out that
// returned `ExitOK` having searched nothing is indistinguishable from a successful one by its
// code alone — the case the oracle's first test compares stdout for.
//
// 🔴 THE MATRIX, MEASURED AT TWO POINTS RATHER THAN ONE. RED at `d8b858a` (the commit before
// the routing landed) — all three refuse at exit 11 with `RefuseUnportedMultiInstance`, the
// file header's one reason, so they are regression coverage and not invariant guards. RED
// again at HEAD under the no-op fan-out above, each failing on its OWN assertion. GREEN at
// HEAD unmutated. The two reds are different defects, which is why both were run.
// =============================================================================

// fanOutOpts is `search --all-scopes <query>` with no scope named — the invocation the whole
// path exists for.
func fanOutOpts(query string) Options {
	opts := readOpts()
	opts.Query = query
	opts.AllScopes = true
	return opts
}

func searchRun(e Env, o Options) (int, error) { return Report(e, o, true) }

func TestAllScopesFANSOUTToEveryInstanceAndLABELSEachSection(t *testing.T) {
	// 🔴 THE HITS, NOT THE BANNERS, ARE WHAT PROVE THE SECOND STORE WAS READ. `instance.Alias`
	// reaches the banner directly, so a fan-out that walked the instance list while searching
	// ONE cache N times would print both labels over one store's content. The discriminator is
	// each section's own `store:` line and its own scope — `gamma-notes` exists ONLY under the
	// secondary root.
	home := twoInstanceHost(t)
	code, stdout, stderr := capture(t, searchRun, fanOutOpts("synthetic"))
	if code != ExitOK {
		t.Fatalf("both instances are readable, so the fan-out exits 0, got %d\n%s", code, stderr)
	}
	for _, alias := range []string{"personal", "secondary"} {
		if !strings.Contains(stdout, "cairn["+alias+"]: cached") {
			t.Fatalf("every section's banner must name its instance (%q missing):\n%s",
				alias, stdout)
		}
	}
	// Each section read ITS OWN cache root, which is the half a label check cannot see.
	for _, root := range []string{
		filepath.Join(home, ".cache", "subsystem-store"),
		filepath.Join(home, ".cache", "subsystem-store-secondary"),
	} {
		if !strings.Contains(stdout, "  store: "+root) {
			t.Fatalf("a section must name the cache it searched (%q missing):\n%s", root, stdout)
		}
	}
	// …and each store's OWN entry is in the hits, so "searched" is not "listed".
	for _, hit := range []string{"alpha-notes/one", "gamma-notes/two"} {
		if !strings.Contains(stdout, hit) {
			t.Fatalf("the fan-out must report %q, which lives on one instance only:\n%s",
				hit, stdout)
		}
	}
	if strings.Contains(stdout, "status=search-no-match") {
		t.Fatalf("the query matches both stores; a no-match section means one was not searched:\n%s",
			stdout)
	}
}

func TestAllScopesDoesNOTRequireThisReposScopeToBeRegistered(t *testing.T) {
	// 🔴 `--all-scopes` NAMES NO SCOPE, SO ROUTING IT IS A REFUSAL ABOUT A QUESTION NOBODY
	// ASKED — and it is the shape a naive "route every read" implementation produces. The
	// dispatch in `Report` therefore sits ABOVE `ScopeOrReason`; moving it below, or asking
	// the table first, makes this invocation exit 11 with an `*UnroutedScope`.
	//
	// ⚠ EXIT 0 ALONE IS NOT THE CLAIM. A fan-out that returned success without searching would
	// satisfy a returncode check, so the labelled sections are asserted here too — the oracle's
	// row checks only the code, and this port is deliberately the stronger one.
	twoInstanceHost(t)
	writeTable(t, filepath.Join(os.Getenv("HOME"), "config"),
		`{"alpha-notes": "personal", "gamma-notes": "secondary"}`)

	opts := fanOutOpts("synthetic")
	opts.Scope = "never-registered"
	code, stdout, stderr := capture(t, searchRun, opts)
	if code != ExitOK {
		t.Fatalf("an unregistered --scope must not refuse a fan-out, got %d\n%s", code, stderr)
	}
	for _, alias := range []string{"personal", "secondary"} {
		if !strings.Contains(stdout, "cairn["+alias+"]: cached") {
			t.Fatalf("the fan-out must still have searched instance %q:\n%s", alias, stdout)
		}
	}
	// The negative control on the refusal itself: the SAME unregistered scope on a search that
	// is NOT `--all-scopes` does refuse, so the row above is about the fan-out and not about
	// this host having no routing rules.
	routed := opts
	routed.AllScopes = false
	sink, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	var discard bytes.Buffer
	_, runErr := searchRun(Env{Stdout: &discard, Stderr: sink,
		Host: func() string { return "fixture-host-000000000000" }}, routed)
	var unrouted *UnroutedScope
	if !errors.As(runErr, &unrouted) {
		t.Fatalf("a SCOPED read of an unregistered scope must still refuse, got %v", runErr)
	}
}

func TestAnUNREADInstanceMakesTheFanOutLOUDAndNonZero(t *testing.T) {
	// 🔴 A SILENT PARTIAL MAKES "found nothing" A LIE. The hits that WERE found are still
	// printed — discarding real answers is its own harm — but the run exits non-zero and NAMES
	// the instance it could not read.
	//
	// ⚠ THE INSTANCE IS UNREAD BECAUSE IT HAS NO CACHE UNDER `--no-sync`, WHERE THE ORACLE'S
	// ROW POINTS ITS SECOND INSTANCE AT A CLOSED PORT. Both arrive at the SAME branch — the
	// `state.ExitHint != 0` arm of the walk — and this file runs offline by design (see the
	// header). What is NOT measured here is the network failure itself; that is the parity
	// harness's claim.
	home := twoInstanceHost(t)
	if err := os.RemoveAll(filepath.Join(home, ".cache", "subsystem-store-secondary")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := capture(t, searchRun, fanOutOpts("synthetic"))
	if code != ExitUnreachableNoCache {
		t.Fatalf("an unread instance makes the fan-out exit %d, got %d\n%s",
			ExitUnreachableNoCache, code, stderr)
	}
	if !strings.Contains(stderr, "PARTIAL") {
		t.Fatalf("the partial must be LOUD on stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "secondary") {
		t.Fatalf("…and must NAME the instance it could not read:\n%s", stderr)
	}
	// The hits that were found are still printed — the other half of the contract.
	if !strings.Contains(stdout, "alpha-notes/one") {
		t.Fatalf("a partial fan-out still prints what the instances that ANSWERED hold:\n%s",
			stdout)
	}
}

func TestAFanOutREFUSESAnExplicitSharedCache(t *testing.T) {
	// 🔴 THE DAMAGE IS SILENT AND IT IS TO THE CACHE. Two snapshots unpacked into one root
	// interleave their scopes while `.sync-stamp` dates whichever synced last — a store that
	// can say neither what it holds nor how old it is. All FOUR fan-out verbs refuse BEFORE the
	// walk, and all four are listed here because the refusal is per-call-site.
	//
	// 🔴 THE COUNT USED TO READ "three", WHICH WAS AN UNDERCOUNT RATHER THAN A PHRASING — AND
	// THE MISSING ROW WAS THE UNCOVERED ONE. `refuseSharedCache` has FIVE call sites (`sync`,
	// `ls-entries`, `search --all-scopes`, `doctor`, `routes --check`); this map held four, and
	// the one it omitted was the call inside `searchEveryInstance`, the function that shipped
	// with measured-zero coverage. A comment that counts the call sites is a claim about which
	// ones are exercised — count them (`grep -c refuseSharedCache`) rather than restating this
	// number when a sixth appears.
	//
	// ⚠ REGRESSION COVERAGE ONLY FOR THE FOUR READ VERBS: `routes --check` already refused
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
		// The fifth call site, and the reason the count above moved. It needs `--all-scopes`
		// and a query of its own, because `Report` dispatches the fan-out on the flag.
		"search --all-scopes": func(e Env, o Options) (int, error) {
			o.AllScopes, o.Query = true, "synthetic"
			return Report(e, o, true)
		},
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
