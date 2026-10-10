package redact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"go/format"
	"hash/fnv"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// 🔴 THE CLEAN-DAMAGE BUDGET (review round 5's governing finding). Rounds 4 and 5 each fixed the
// shapes the audit named and each paid for it somewhere nobody measured: the probes a fixer writes
// for a heuristic are the shapes the fixer thought of. So damage is measured HERE on text nobody
// wrote for the purpose, and PINNED, so that a later round which widens it fails a test instead of
// an audit.
//
// 🔴 WHAT `go test` PINS IS A FROZEN CORPUS, NOT THE LIVE TREE (review round 6). The round-6 budget
// read this repository's tracked files and the toolchain's GOROOT, so it went red for reasons that
// had nothing to do with the redactor: a documentation edit anywhere in the tree that added a
// changed line, deleting three retired docs ("6 budget entries are unused"), and go1.26 (entropy
// 1063 against a ceiling of 845 — a different standard library under the same sampling rule, with
// the redactor unchanged). A gate that goes red on an unrelated edit trains everyone to regenerate
// it blind, which is worse than no gate. So:
//
//   - [TestTheCleanDamageBudgetOverTheFrozenCorpus] — the DEFAULT, and the gate. Two committed
//     files under `testdata/` ([frozenCorpusFiles]): a sample of this repository's neutral text and
//     code, and a sample of the Go standard library's source (go1.25.14, under its BSD license,
//     reproduced in the file) — a hash sample PLUS the files densest in tokens the entropy rule
//     considers, because a hash sample alone was MEASURED too thin: lowering the rule's bit floor
//     from 3.5 to 3.0 changed no line of a 25,869-line hash sample. Every line the redactor changes
//     there is ENUMERATED in `budget_data_test.go`, keyed by the line AND its redacted form
//     ([damageKey]), and the match is EXACT in both directions: a changed line not on the list
//     fails (damage widened), and an entry no line uses fails (damage narrowed — which may be a fix,
//     or a test key the redactor stopped taking; regenerate and say which). It reads nothing but
//     those two files, so its answer is the same on every tree and every toolchain.
//   - [TestTheCleanDamageBudgetOverThisRepository] and [TestTheCleanDamageCeilingOverTheGoStandardLibrary]
//     — the LIVE sweeps, OPT-IN behind `-redact.live-budget` and SKIPPED otherwise, so they never
//     fail `go test ./...`. They are how a human measures text the frozen corpus does not hold (a
//     new doc, a new toolchain); their lists live in `budget_live_data_test.go`.
//
// ⚠ WHAT "DAMAGED" COUNTS, so the number is not read as wider than it is: a line of the input that
// is not byte-identical in the output. It does NOT say the line was clean — the standard library's
// test keys and vectors are in the Go half, and redacting those is the redactor working. The list
// pins the TOTAL so it cannot move unseen, and names which lines are which.
//
// ⚠ AND WHAT THE FROZEN CORPUS CANNOT SEE: text unlike its 41,477 lines — no `claudedocs/` prose
// at all, by choice. That is the trade for a deterministic gate, and the opt-in sweeps are the
// instrument for it. Whether CI should run them is an open question recorded in the plan (T1), not
// a job here.

var (
	budgetWrite = flag.Bool("redact.write-budget", false, "rewrite the budget data file of the budget test that runs (frozen: budget_data_test.go; live: budget_live_data_test.go)")
	liveSweep   = flag.Bool("redact.live-budget", false, "also run the LIVE clean-damage sweeps over this repository's tree and the toolchain's standard library (skipped by default)")
	stdlibFull  = flag.Bool("redact.stdlib-full", false, "with -redact.live-budget: measure the whole Go standard library source, not the 1-in-8 sample")
)

// damagedLine is one input line the redactor changed.
type damagedLine struct {
	file  string
	line  int
	rules []string
	text  string
	out   string // the line as redacted; "" for a multi-line collapse
}

var budgetMarker = regexp.MustCompile(`\[redacted:([a-z0-9/-]+):[0-9a-f]{8}\]`)

// damagedLines redacts one file's text as [Redactor.Text] and returns the lines that changed.
// When the output has a different number of lines (a private-key block collapses) the comparison
// falls back to a multiset: an input line with no identical output line left is damaged.
func damagedLines(r *Redactor, file string, data []byte) []damagedLine {
	red, hits := r.Text(data)
	if len(hits) == 0 {
		return nil
	}
	in, out := strings.Split(string(data), "\n"), strings.Split(string(red), "\n")
	var res []damagedLine
	if len(in) == len(out) {
		for i := range in {
			if in[i] != out[i] {
				d := damagedLine{file: file, line: i + 1, text: in[i], out: out[i]}
				for _, m := range budgetMarker.FindAllStringSubmatch(out[i], -1) {
					d.rules = append(d.rules, m[1])
				}
				res = append(res, d)
			}
		}
		return res
	}
	have := map[string]int{}
	for _, l := range out {
		have[l]++
	}
	for i, l := range in {
		if have[l] > 0 {
			have[l]--
			continue
		}
		res = append(res, damagedLine{file: file, line: i + 1, text: l, rules: []string{"(multi-line)"}})
	}
	return res
}

// lineKey names a damaged line by its CONTENT, so moving or re-indenting a file's other lines does
// not move the budget: 12 hex of SHA-256 over the line.
func lineKey(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:6])
}

// damageKey names a damaged line by its content AND what the redactor made of it, so a change in
// HOW an already-damaged line is redacted (one more span, a different rule) moves the frozen
// budget too. Measured necessary: lowering [MinEntropyToken] to 16 added an entropy span to a line
// that was already damaged, and a key over the input alone passed it.
func damageKey(d damagedLine) string { return lineKey(d.text + "\x00" + d.out) }

// liveKey is the live sweep's key: the input line only, so its list survives a marker change.
func liveKey(d damagedLine) string { return lineKey(d.text) }

// corpusFile is one text file of a corpus.
type corpusFile struct {
	rel  string
	data []byte
}

// readCorpus reads the text files among rels (relative to root): not binary by the redactor's own
// rule, no NUL, at most 4 MiB.
func readCorpus(root string, rels []string) (files []corpusFile, lines int) {
	for _, rel := range rels {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || len(data) > 4<<20 || bytes.IndexByte(data, 0) >= 0 || IsBinary(data) {
			continue
		}
		files = append(files, corpusFile{rel, data})
		lines += bytes.Count(data, []byte("\n")) + 1
	}
	return files, lines
}

// measureCorpus redacts every file (in parallel — a Redactor holds no mutable state) and returns
// the damaged lines in file order.
func measureCorpus(r *Redactor, files []corpusFile) []damagedLine {
	results := make([][]damagedLine, len(files))
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				results[i] = damagedLines(r, files[i].rel, files[i].data)
			}
		}()
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()
	var out []damagedLine
	for _, res := range results {
		out = append(out, res...)
	}
	return out
}

// moduleRoot walks up from the package directory to the directory holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the package directory")
		}
		dir = parent
	}
}

// repoFiles lists the repository's files. TRACKED files when git can say (`complete`); otherwise —
// a source tree with no `.git`, as inside a nix build, which also carries only part of the tree —
// every file under the root.
func repoFiles(t *testing.T, root string) (rels []string, complete bool) {
	t.Helper()
	if out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output(); err == nil && len(out) > 0 {
		for _, f := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
			rels = append(rels, f)
		}
		sort.Strings(rels)
		return rels, true
	}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// `vendor` is what a nix build fetches beside the source (the module cache, vendored):
			// third-party text this repository does not track, measured nowhere else.
			if n := d.Name(); n == ".git" || n == ".claude" || n == "node_modules" || n == ".direnv" || n == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if rel, err := filepath.Rel(root, p); err == nil && d.Type().IsRegular() {
			rels = append(rels, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(rels)
	return rels, false
}

// fixtureFile: a file WRITTEN to hold credential-shaped text — a test, test data, or this package
// (whose sources and README spell the shapes the rules read). Everything else is the neutral part
// of the tree: source, documentation, configuration, workflows.
func fixtureFile(rel string) bool {
	return strings.HasPrefix(rel, "internal/redact/") || strings.HasSuffix(rel, "_test.go") ||
		strings.HasPrefix(rel, "tests/") || strings.Contains(rel, "/testdata/") || strings.HasPrefix(rel, "testdata/")
}

// frozenCorpusFiles are the frozen corpus, relative to the package directory. Both must also be
// in `flake.nix`'s `onlyGo` filter, or the sandbox build reads a tree without them — which this
// test refuses (below) rather than passing over an empty corpus.
var frozenCorpusFiles = []string{"testdata/budget_corpus_repo.txt", "testdata/budget_corpus_go.txt"}

// frozenFloorFiles, frozenFloorLines: the corpus as committed holds 197 sections and 41,477 lines;
// a reader that finds fewer has narrowed, and its zero would be a zero about nothing.
const frozenFloorFiles, frozenFloorLines = 190, 40000

var sectionHeader = regexp.MustCompile(`^=== file (\S+) ([0-9]+) ===$`)

// readFrozenCorpus parses one corpus file: a preamble, then sections of `=== file <name> <n> ===`,
// exactly n bytes, and one newline. A malformed section is a FAILURE, never a skip.
func readFrozenCorpus(t *testing.T, rel string) []corpusFile {
	t.Helper()
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("the frozen corpus is missing (%v) — in a nix build, %s is not in flake.nix's onlyGo filter", err, rel)
	}
	i := bytes.Index(data, []byte("\n=== file "))
	if i < 0 {
		t.Fatalf("%s: no section", rel)
	}
	data = data[i+1:]
	var out []corpusFile
	for len(data) > 0 {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			t.Fatalf("%s: a section header with no newline", rel)
		}
		m := sectionHeader.FindSubmatch(data[:nl])
		if m == nil {
			t.Fatalf("%s: not a section header: %q", rel, data[:min(nl, 80)])
		}
		n, _ := strconv.Atoi(string(m[2]))
		body := data[nl+1:]
		if len(body) < n+1 || body[n] != '\n' {
			t.Fatalf("%s: section %s is not %d bytes and a newline", rel, m[1], n)
		}
		out = append(out, corpusFile{rel: string(m[1]), data: append([]byte(nil), body[:n]...)})
		data = body[n+1:]
	}
	return out
}

// TestTheCleanDamageBudgetOverTheFrozenCorpus — see the file doc. EXACT against [frozenBudget].
func TestTheCleanDamageBudgetOverTheFrozenCorpus(t *testing.T) {
	r := r5redactor(t)
	budgetControl(t, r)
	var files []corpusFile
	for _, f := range frozenCorpusFiles {
		files = append(files, readFrozenCorpus(t, f)...)
	}
	lines := 0
	for _, f := range files {
		lines += bytes.Count(f.data, []byte("\n")) + 1
	}
	if len(files) < frozenFloorFiles || lines < frozenFloorLines {
		t.Fatalf("control: only %d sections / %d lines in the frozen corpus — the reader narrowed", len(files), lines)
	}
	damaged := measureCorpus(r, files)
	perRule := map[string]int{}
	for _, d := range damaged {
		for _, rule := range d.rules {
			perRule[rule]++
		}
	}
	t.Logf("frozen corpus: %d sections, %d lines; damaged lines: %d %v", len(files), lines, len(damaged), perRule)
	// CONTROL: the Go half's test vectors are there to be taken. A counter that never moves passes
	// an exact match against an empty list.
	if perRule["entropy"] == 0 {
		t.Fatal("control: the entropy rule changed no line of the frozen corpus — the counter is wired to nothing")
	}
	if *budgetWrite {
		writeFrozenBudget(t, damaged)
		return
	}
	checkEnumerated(t, damaged, damageKey, frozenBudget, "-run TestTheCleanDamageBudgetOverTheFrozenCorpus -redact.write-budget", true)
}

// budgetControl: the instrument sees damage, and attributes it. A counter wired to nothing passes
// every budget with 0.
func budgetControl(t *testing.T, r *Redactor) {
	t.Helper()
	control := "plain line\nDB_PASSWORD=" + r5val() + "\nanother plain line\n"
	if d := damagedLines(r, "control", []byte(control)); len(d) != 1 || d[0].line != 2 || len(d[0].rules) != 1 || d[0].rules[0] != "key-context" {
		t.Fatalf("control: one planted line gave %+v — the damage counter does not see damage", d)
	}
}

// checkEnumerated holds damaged lines to an enumerated multiset of line keys. A line not on it
// fails; unused entries fail when exact (the frozen corpus), and are only counted otherwise.
func checkEnumerated(t *testing.T, damaged []damagedLine, key func(damagedLine) string, budget map[string]int, regen string, exact bool) (stale int) {
	t.Helper()
	remaining := map[string]int{}
	for k, n := range budget {
		remaining[k] = n
	}
	for _, d := range damaged {
		k := key(d)
		if remaining[k] > 0 {
			remaining[k]--
			continue
		}
		t.Errorf("NOT IN THE BUDGET: %s:%d is changed by %v:\n    %s\n  If the redactor newly damages ordinary text, that is the regression this test exists for.\n"+
			"  If the line really is credential-shaped, add it: `go test ./internal/redact %s`.", d.file, d.line, d.rules, d.text, regen)
	}
	for _, n := range remaining {
		stale += n
	}
	if stale > 0 && exact {
		t.Errorf("%d budget entries match no damaged line: the redactor stopped changing them. A fix narrows damage; so does a test key it stopped taking — regenerate (`go test ./internal/redact %s`) and say which", stale, regen)
	}
	return stale
}

// TestTheCleanDamageBudgetOverThisRepository — the LIVE sweep; see the file doc. Opt-in.
//
// The NEUTRAL files are held to the enumerated list [liveBudget]: a damaged line whose content is
// not on it FAILS. An entry no line uses any more is only reported until more than
// [staleBudgetSlack] are unused in a complete tree. The FIXTURE files are held to
// [liveFixtureCeiling].
func TestTheCleanDamageBudgetOverThisRepository(t *testing.T) {
	if !*liveSweep {
		t.Skip("SKIPPED — the live-tree sweep is opt-in (-redact.live-budget); the frozen corpus is the gate")
	}
	root := moduleRoot(t)
	all, complete := repoFiles(t, root)
	// The frozen corpus is measured by its own test, exactly; counting it again here would put
	// its 631 lines against the fixture ceiling.
	var rels []string
	for _, rel := range all {
		if !strings.HasPrefix(rel, "internal/redact/testdata/budget_corpus_") {
			rels = append(rels, rel)
		}
	}
	files, lines := readCorpus(root, rels)
	r := r5redactor(t)
	budgetControl(t, r)
	floorFiles, floorLines := 400, 200000
	if !complete {
		// A filtered source tree (a nix build): only the Go sources and a few fixtures.
		floorFiles, floorLines = 100, 50000
	}
	if len(files) < floorFiles || lines < floorLines {
		t.Fatalf("control: only %d text files / %d lines found under %s (complete=%v) — the walk narrowed", len(files), lines, root, complete)
	}

	damaged := measureCorpus(r, files)
	var clean, fixture []damagedLine
	for _, d := range damaged {
		if fixtureFile(d.file) {
			fixture = append(fixture, d)
		} else {
			clean = append(clean, d)
		}
	}
	t.Logf("tree: %d text files, %d lines (tracked list: %v); damaged lines: %d in neutral files, %d in fixture files",
		len(files), lines, complete, len(clean), len(fixture))

	if *budgetWrite {
		writeLiveBudget(t, clean, len(fixture))
		return
	}

	stale := checkEnumerated(t, clean, liveKey, liveBudget, "-run TestTheCleanDamageBudgetOverThisRepository -redact.live-budget -redact.write-budget", false)
	if stale > 0 {
		t.Logf("%d budget entries are no longer used (a line was edited away, or the redactor stopped damaging it)", stale)
		if complete && stale > staleBudgetSlack {
			t.Errorf("%d budget entries are unused, over the slack of %d: the budget is no longer tight — regenerate it with -redact.live-budget -redact.write-budget", stale, staleBudgetSlack)
		}
	}
	if len(fixture) > liveFixtureCeiling {
		t.Errorf("fixture files: %d damaged lines, ceiling %d. A new test fixture holding credential-shaped text raises this honestly; "+
			"a redactor change that raises it is damage. Re-derive with -redact.write-budget and say which it was.", len(fixture), liveFixtureCeiling)
	}
	if complete && len(fixture) < liveFixtureCeiling/2 {
		t.Errorf("fixture files: %d damaged lines, under half the ceiling of %d — the ceiling no longer bounds anything; re-derive it", len(fixture), liveFixtureCeiling)
	}
}

// staleBudgetSlack is how many unused entries [liveBudget] may carry before the live sweep calls
// it loose.
const staleBudgetSlack = 5

// enumerate renders damaged lines as the body of a `map[string]int` literal: key, count, and a
// comment naming one file that holds the line and the rule(s) that changed it.
func enumerate(damaged []damagedLine, key func(damagedLine) string) (body string, distinct int) {
	counts := map[string]int{}
	note := map[string]string{}
	for _, d := range damaged {
		k := key(d)
		counts[k]++
		if note[k] == "" {
			rules := append([]string(nil), d.rules...)
			sort.Strings(rules)
			note[k] = fmt.Sprintf("%s (%s)", d.file, strings.Join(slices.Compact(rules), ","))
		}
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if note[keys[i]] != note[keys[j]] {
			return note[keys[i]] < note[keys[j]]
		}
		return keys[i] < keys[j]
	})
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "\t%q: %d, // %s\n", k, counts[k], note[k])
	}
	return b.String(), len(keys)
}

const generatedPreamble = "// line's content; the comment names a file that holds it and the rule(s) that changed it.\n\n"

// writeGo writes generated Go source gofmt-ed, so a regenerated file is never a gofmt diff.
func writeGo(name, src string) error {
	out, err := format.Source([]byte(src))
	if err != nil {
		return err
	}
	return os.WriteFile(name, out, 0o644)
}

// writeFrozenBudget regenerates budget_data_test.go from the frozen corpus.
func writeFrozenBudget(t *testing.T, damaged []damagedLine) {
	t.Helper()
	body, distinct := enumerate(damaged, damageKey)
	var b strings.Builder
	b.WriteString("package redact\n\n")
	b.WriteString("// Code generated by `go test ./internal/redact -run TestTheCleanDamageBudgetOverTheFrozenCorpus\n")
	b.WriteString("// -redact.write-budget`; see budget_test.go. Each key is 12 hex of SHA-256 over one damaged line's\n")
	b.WriteString("// content AND its redacted form (damageKey); the comment names a file that holds it and the rule(s)\n")
	b.WriteString("// that changed it.\n\n")
	fmt.Fprintf(&b, "// frozenBudget: the %d lines of the FROZEN corpus (testdata/budget_corpus_*.txt) that the\n", len(damaged))
	b.WriteString("// redactor changes. Matched EXACTLY, in both directions.\n")
	b.WriteString("var frozenBudget = map[string]int{\n" + body + "}\n")
	if err := writeGo("budget_data_test.go", b.String()); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote budget_data_test.go: %d lines (%d distinct)", len(damaged), distinct)
}

// writeLiveBudget regenerates budget_live_data_test.go: the enumerated neutral lines of the live
// tree, and the fixture ceiling at the measured count plus a tenth.
func writeLiveBudget(t *testing.T, clean []damagedLine, fixtures int) {
	t.Helper()
	body, distinct := enumerate(clean, liveKey)
	var b strings.Builder
	b.WriteString("package redact\n\n")
	b.WriteString("// Code generated by `go test ./internal/redact -run TestTheCleanDamageBudgetOverThisRepository\n")
	b.WriteString("// -redact.live-budget -redact.write-budget`; see budget_test.go. OPT-IN: read only by the live\n")
	b.WriteString("// sweep, which `go test` skips by default. Each key is 12 hex of SHA-256 over one damaged\n")
	b.WriteString(generatedPreamble)
	fmt.Fprintf(&b, "// liveBudget: the %d lines of this repository's NEUTRAL files (not tests, test data or this\n", len(clean))
	b.WriteString("// package) that the redactor changes.\n")
	b.WriteString("var liveBudget = map[string]int{\n" + body + "}\n\n")
	b.WriteString("// liveFixtureCeiling: the damaged lines of the FIXTURE files, measured, plus a tenth.\n")
	fmt.Fprintf(&b, "const liveFixtureCeiling = %d // measured: %d\n", fixtures+fixtures/10, fixtures)
	if err := writeGo("budget_live_data_test.go", b.String()); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote budget_live_data_test.go: %d neutral lines (%d distinct), fixture ceiling %d", len(clean), distinct, fixtures+fixtures/10)
}

// stdlibMeasured are the per-rule counts over the SAMPLED Go standard library source (every file
// whose path hashes to 0 mod 8) at [stdlibMeasuredAt], and they are the CEILINGS, exactly, when the
// test runs under that toolchain — the pinned one (`go.mod`, `flake.nix`). Under any other version
// the source differs, so each ceiling is padded by 3% + 2 and the run says so.
//
// ⚠ EXACT, BECAUSE A PADDED CEILING WAS MEASURED BLIND: with 3% of headroom, deleting the entropy
// rule's whole identifier test moved the sampled entropy count from 819 to 836 and passed.
//
// ⚠ READ THE ENTROPY ROW FOR WHAT IT IS: almost all of it is the library's own test keys,
// certificates and vectors — random base64 and hex, which the rule exists to take.
var stdlibMeasured = map[string]int{
	"lines":          858, // damaged lines, of 354,088
	"entropy":        819,
	"key-context":    1,
	"netrc-password": 0,
	"pgpass":         0,
}

const stdlibMeasuredAt = "go1.25.14"

// TestTheCleanDamageCeilingOverTheGoStandardLibrary — see the file doc. It SKIPS, saying so and
// with a count of zero lines measured, when the toolchain's source tree is not on disk.
func TestTheCleanDamageCeilingOverTheGoStandardLibrary(t *testing.T) {
	if !*liveSweep {
		t.Skip("SKIPPED — the GOROOT sweep is opt-in (-redact.live-budget): its answer depends on the toolchain, and the frozen corpus's Go half is the gate")
	}
	src := filepath.Join(runtime.GOROOT(), "src")
	if out, err := exec.Command("go", "env", "GOROOT").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		src = filepath.Join(strings.TrimSpace(string(out)), "src")
	}
	if st, err := os.Stat(filepath.Join(src, "fmt", "print.go")); err != nil || st.IsDir() {
		t.Skipf("SKIPPED — 0 lines measured: no Go standard library source under %q", src)
	}
	var rels []string
	filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		rel = filepath.ToSlash(rel)
		h := fnv.New32a()
		h.Write([]byte(rel))
		if *stdlibFull || h.Sum32()%8 == 0 {
			rels = append(rels, rel)
		}
		return nil
	})
	sort.Strings(rels)
	files, lines := readCorpus(src, rels)
	r := r5redactor(t)
	damaged := measureCorpus(r, files)
	got := map[string]int{"lines": len(damaged)}
	for _, d := range damaged {
		for _, rule := range d.rules {
			got[rule]++
		}
	}
	var names []string
	for k := range got {
		names = append(names, k)
	}
	sort.Strings(names)
	var parts []string
	for _, k := range names {
		parts = append(parts, fmt.Sprintf("%s=%d", k, got[k]))
	}
	t.Logf("%s, %s: %d text files, %d lines (full=%v); damaged %s", runtime.Version(), src, len(files), lines, *stdlibFull, strings.Join(parts, " "))
	if *stdlibFull {
		return // a measurement run: the ceilings are the sample's
	}
	// CONTROLS: the sample is not empty, and the counter moved (the library's test keys are there).
	if len(files) < 500 || lines < 150000 {
		t.Fatalf("control: only %d files / %d lines sampled under %s — the walk narrowed", len(files), lines, src)
	}
	if got["entropy"] == 0 {
		t.Fatal("control: the entropy rule changed no line of the standard library's test vectors — the counter is wired to nothing")
	}
	exact := runtime.Version() == stdlibMeasuredAt
	if !exact {
		t.Logf("⚠ %s is not %s: the ceilings are padded by 3%% + 2, and a small widening can pass here", runtime.Version(), stdlibMeasuredAt)
	}
	for _, k := range []string{"lines", "key-context", "netrc-password", "pgpass", "entropy"} {
		ceiling := stdlibMeasured[k]
		if !exact {
			ceiling += ceiling*3/100 + 2
		}
		if got[k] > ceiling {
			t.Errorf("%s: %d over the sampled standard library, ceiling %d — the redactor damages more ordinary code than it did", k, got[k], ceiling)
		}
	}
}
