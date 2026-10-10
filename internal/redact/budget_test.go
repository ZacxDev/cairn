package redact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

// 🔴 THE CLEAN-DAMAGE BUDGET (review round 5's governing finding). Rounds 4 and 5 each fixed the
// shapes the audit named and each paid for it somewhere nobody measured: the probes a fixer writes
// for a heuristic are the shapes the fixer thought of. So damage is measured HERE on text nobody
// wrote for the purpose, and PINNED, so that a later round which widens it fails a test instead of
// an audit:
//
//   - THIS REPOSITORY'S OWN TRACKED TEXT ([TestTheCleanDamageBudgetOverThisRepository]): every
//     line the redactor changes in a non-fixture file is ENUMERATED in `budget_data_test.go`, and
//     a damaged line that is not on the list fails. Fixture files — tests, `testdata`, and this
//     package, all written to hold credential-shaped text — are held to a count ceiling instead.
//   - THE GO STANDARD LIBRARY'S SOURCE ([TestTheCleanDamageCeilingOverTheGoStandardLibrary]):
//     about three million lines of code, tables, test vectors and vendored text, held to a
//     ceiling per rule.
//
// ⚠ WHAT "DAMAGED" COUNTS, so the number is not read as wider than it is: a line of the input that
// is not byte-identical in the output. It does NOT say the line was clean — this repository's
// tests are full of synthetic credentials, and the standard library's of real test keys, and
// redacting those is the redactor working. The budget pins the TOTAL so it cannot grow unseen; the
// enumerated list is where a reader can see which lines are which.
//
// ⚠ AND IT IS A TRIPWIRE ACROSS THE TREE: a change anywhere in the repository that adds a line the
// redactor would change fails this package's test. That is the point — it is the only signal that
// the redactor eats a new shape of ordinary text — and the failure prints the entry to add when
// the line really is credential-shaped.

var (
	budgetWrite = flag.Bool("redact.write-budget", false, "rewrite budget_data_test.go from the tree as it is")
	stdlibFull  = flag.Bool("redact.stdlib-full", false, "measure the whole Go standard library source, not the 1-in-8 sample")
)

// damagedLine is one input line the redactor changed.
type damagedLine struct {
	file  string
	line  int
	rules []string
	text  string
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
				d := damagedLine{file: file, line: i + 1, text: in[i]}
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

// TestTheCleanDamageBudgetOverThisRepository — see the file doc.
//
// The NEUTRAL files are held to the enumerated list [cleanBudget]: a damaged line whose content is
// not on it FAILS. An entry no line uses any more is only reported — an unrelated edit elsewhere in
// the tree may retire one — until more than [staleBudgetSlack] are unused in a complete tree, when
// the list is no longer a tight budget and must be regenerated. The FIXTURE files are held to
// [fixtureDamageCeiling].
func TestTheCleanDamageBudgetOverThisRepository(t *testing.T) {
	root := moduleRoot(t)
	rels, complete := repoFiles(t, root)
	files, lines := readCorpus(root, rels)
	r := r5redactor(t)

	// POSITIVE CONTROL: the instrument sees damage, and attributes it. A counter wired to nothing
	// passes every ceiling below with 0.
	control := "plain line\nDB_PASSWORD=" + r5val() + "\nanother plain line\n"
	if d := damagedLines(r, "control", []byte(control)); len(d) != 1 || d[0].line != 2 || len(d[0].rules) != 1 || d[0].rules[0] != "key-context" {
		t.Fatalf("control: one planted line gave %+v — the damage counter does not see damage", d)
	}
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
		writeBudget(t, clean, len(fixture))
		return
	}

	remaining := map[string]int{}
	for k, n := range cleanBudget {
		remaining[k] = n
	}
	over := 0
	for _, d := range clean {
		k := lineKey(d.text)
		if remaining[k] > 0 {
			remaining[k]--
			continue
		}
		over++
		t.Errorf("NOT IN THE BUDGET: %s:%d is changed by %v:\n    %s\n  If the redactor newly damages ordinary text, that is the regression this test exists for.\n"+
			"  If the line really is credential-shaped, add it: `go test ./internal/redact -run TestTheCleanDamageBudgetOverThisRepository -redact.write-budget`.",
			d.file, d.line, d.rules, d.text)
	}
	stale := 0
	for _, n := range remaining {
		stale += n
	}
	if stale > 0 {
		t.Logf("%d budget entries are no longer used (a line was edited away, or the redactor stopped damaging it)", stale)
		if complete && stale > staleBudgetSlack {
			t.Errorf("%d budget entries are unused, over the slack of %d: the budget is no longer tight — regenerate it with -redact.write-budget", stale, staleBudgetSlack)
		}
	}
	if len(fixture) > fixtureDamageCeiling {
		t.Errorf("fixture files: %d damaged lines, ceiling %d. A new test fixture holding credential-shaped text raises this honestly; "+
			"a redactor change that raises it is damage. Re-derive with -redact.write-budget and say which it was.", len(fixture), fixtureDamageCeiling)
	}
	if complete && len(fixture) < fixtureDamageCeiling/2 {
		t.Errorf("fixture files: %d damaged lines, under half the ceiling of %d — the ceiling no longer bounds anything; re-derive it", len(fixture), fixtureDamageCeiling)
	}
}

// staleBudgetSlack is how many unused entries [cleanBudget] may carry before the test calls it
// loose.
const staleBudgetSlack = 5

// writeBudget regenerates budget_data_test.go: the enumerated neutral lines, and the fixture
// ceiling at the measured count plus a tenth.
func writeBudget(t *testing.T, clean []damagedLine, fixtures int) {
	t.Helper()
	counts := map[string]int{}
	note := map[string]string{}
	for _, d := range clean {
		k := lineKey(d.text)
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
	b.WriteString("package redact\n\n")
	b.WriteString("// Code generated by `go test ./internal/redact -run TestTheCleanDamageBudgetOverThisRepository\n")
	b.WriteString("// -redact.write-budget`; see budget_test.go. Each key is 12 hex of SHA-256 over one damaged\n")
	b.WriteString("// line's content; the comment names a file that holds it and the rule(s) that changed it.\n\n")
	fmt.Fprintf(&b, "// cleanBudget: the %d lines of this repository's NEUTRAL files (not tests, test data or this\n", len(clean))
	b.WriteString("// package) that the redactor changes.\n")
	b.WriteString("var cleanBudget = map[string]int{\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "\t%q: %d, // %s\n", k, counts[k], note[k])
	}
	b.WriteString("}\n\n")
	b.WriteString("// fixtureDamageCeiling: the damaged lines of the FIXTURE files, measured, plus a tenth.\n")
	fmt.Fprintf(&b, "const fixtureDamageCeiling = %d // measured: %d\n", fixtures+fixtures/10, fixtures)
	if err := os.WriteFile("budget_data_test.go", []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote budget_data_test.go: %d neutral lines (%d distinct), fixture ceiling %d", len(clean), len(keys), fixtures+fixtures/10)
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
