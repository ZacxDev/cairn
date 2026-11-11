package scopeuse

import (
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// corpusUnit is one tool input the F2 tests feed: a tool name and its input (a map, or the raw
// strings of a [CacheScopes] call, all shell lines).
type corpusUnit struct {
	tool  string
	input any
}

// recording, while non-nil, collects every unit [checkStrings] and [checkUnits] feed.
var recording *[]corpusUnit

func record(tool string, input any) {
	if recording != nil {
		*recording = append(*recording, corpusUnit{tool, input})
	}
}

// f2RowTests are the tests whose rows [TestF2NeverNarrowerThanMain] re-reads as its corpus.
// [TestF2PropertyCorpusIsEveryF2Test] holds this list to the package's `TestF2…` functions, so a
// new F2 test whose rows the property never saw turns it red.
var f2RowTests = map[string]func(*testing.T){
	"TestF2NamedCacheReadAddsThatScope":       TestF2NamedCacheReadAddsThatScope,
	"TestF2UnattributableCacheReadAddsStar":   TestF2UnattributableCacheReadAddsStar,
	"TestF2NonCacheMentionsAddStar":           TestF2NonCacheMentionsAddStar,
	"TestF2DecidesOverTheWholeInput":          TestF2DecidesOverTheWholeInput,
	"TestF2ScopeDirectoryStringAddsStar":      TestF2ScopeDirectoryStringAddsStar,
	"TestF2ShellRewrittenPathAddsStar":        TestF2ShellRewrittenPathAddsStar,
	"TestF2CdFamilyWordAddsStar":              TestF2CdFamilyWordAddsStar,
	"TestF2AncestorArgumentAddsStar":          TestF2AncestorArgumentAddsStar,
	"TestF2CacheFlagFollowsMain":              TestF2CacheFlagFollowsMain,
	"TestF2AllowlistRejectsTheRoundTwoShapes": TestF2AllowlistRejectsTheRoundTwoShapes,
	"TestF2AllowlistKeepsProvableReadsNamed":  TestF2AllowlistKeepsProvableReadsNamed,
	"TestF2GrammarRejectsMetacharacters":      TestF2GrammarRejectsMetacharacters,
	"TestF2GrammarRejectsQuotedPaths":         TestF2GrammarRejectsQuotedPaths,
	"TestF2GrammarRejectsOtherPaths":          TestF2GrammarRejectsOtherPaths,
	"TestF2GrammarRejectsDotWords":            TestF2GrammarRejectsDotWords,
	"TestF2GrammarRejectsCommands":            TestF2GrammarRejectsCommands,
	"TestF2WorkdirMustBeOutsideTheCache":      TestF2WorkdirMustBeOutsideTheCache,
	"TestF2WorkdirMustNotBeAnAncestor":        TestF2WorkdirMustNotBeAnAncestor,
	"TestF2OnlyABashDescriptionIsFreeText":    TestF2OnlyABashDescriptionIsFreeText,
	"TestF2UnprovableShapesAddStar":           TestF2UnprovableShapesAddStar,
	"TestF2RelativeAndRecursiveReadsAddStar":  TestF2RelativeAndRecursiveReadsAddStar,
	"TestF2GrammarRejectsCacheWords":          TestF2GrammarRejectsCacheWords,
	"TestF2FlagAllowlist":                     TestF2FlagAllowlist,
	"TestF2MustBeStar":                        TestF2MustBeStar,
	"TestF2EveryMentionInsideACleanPath":      TestF2EveryMentionInsideACleanPath,
	"TestF2NoMentionAddsNothing":              TestF2NoMentionAddsNothing,
	"TestF2MustStayNamed":                     TestF2MustStayNamed,
}

// f2NotRowTests are the `TestF2…` functions that are not row tables: this file's own two, and
// ones that build sessions rather than single inputs.
var f2NotRowTests = []string{
	"TestF2NeverNarrowerThanMain", "TestF2PropertyCorpusIsEveryF2Test", "TestF2DirectoryChangeChainAddsStar",
	"TestF2CacheReadAddsItsScope",
}

// TestF2PropertyCorpusIsEveryF2Test: the ledger of the property's corpus, failing when the set of
// `TestF2…` functions in this package grows OR shrinks without the lists above moving with it.
func TestF2PropertyCorpusIsEveryF2Test(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v %v", files, err)
	}
	var found []string
	fs := token.NewFileSet()
	for _, f := range files {
		af, err := parser.ParseFile(fs, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range af.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "TestF2") {
				found = append(found, fn.Name.Name)
			}
		}
	}
	var want []string
	for n := range f2RowTests {
		want = append(want, n)
	}
	want = append(want, f2NotRowTests...)
	sort.Strings(found)
	sort.Strings(want)
	if !slices.Equal(found, want) {
		t.Fatalf("TestF2 functions %v\nbut the property ledger lists %v", found, want)
	}
}

// mainF2 is origin/main's F2, copied inline and NOT shared with the code under test: a tool input
// is `*` when any string of it contains `subsystem-store` under `strings.ToLower`.
func mainF2(ss []string) bool {
	for _, s := range ss {
		if strings.Contains(strings.ToLower(s), "subsystem-store") {
			return true
		}
	}
	return false
}

// TestF2NeverNarrowerThanMain (🔴): THE INVARIANT, over every row of every F2 test plus a seeded
// random corpus of shell lines mixing cache paths, flags, metacharacters, `cd`, `tar`, `find`, case
// changes and non-ASCII bytes, each also inside bash/Read/Grep/Task inputs with working
// directories and descriptions. For each input:
//
//   - main's predicate false → new F2 contributes NOTHING (no `*`, no scope), and the Deriver's F2
//     is false — identical to main;
//   - main's predicate true → new F2 is exactly `[*]`, OR a non-empty set of valid scopes with no
//     `*`, each spelled in the input as `.cache/subsystem-store/<scope>/`, for an input the grammar
//     accepts ([provenScopes]); never nothing.
//
// It also pins the collection seam: [inputStrings] yields exactly the strings main's
// `eachString(input, …)` fed its predicate. And it carries its positive controls: the corpus must
// produce every one of the three outcomes, in numbers, or the property was never exercised.
func TestF2NeverNarrowerThanMain(t *testing.T) {
	var corpus []corpusUnit
	recording = &corpus
	for name, f := range f2RowTests {
		t.Run(name, f)
	}
	recording = nil
	rows := len(corpus)
	corpus = append(corpus, randomCorpus(rand.New(rand.NewSource(314)), 4000)...)

	var none, star, named int
	for _, u := range corpus {
		var ss []string
		var roles []role
		var collected []string
		if raw, ok := u.input.([]string); ok {
			ss, collected = raw, raw
		} else {
			ss, roles = inputStrings(u.tool, u.input)
			eachString(u.input, func(s string) { collected = append(collected, s) })
		}
		if a, b := slices.Sorted(slices.Values(ss)), slices.Sorted(slices.Values(collected)); !slices.Equal(a, b) {
			t.Fatalf("%s %#v: inputStrings %q, main's eachString %q", u.tool, u.input, a, b)
		}
		lowers := make([]string, len(ss))
		for k, s := range ss {
			lowers[k] = asciiLower(s)
		}
		var got []string
		eachCacheRead(ss, lowers, roles, func(v string) { got = append(got, v) })
		d := NewDeriver()
		d.toolInputStrings(ss, roles)
		f2 := d.Result().F2

		if !mainF2(ss) {
			if len(got) != 0 || f2 {
				t.Errorf("%s %#v: main adds nothing, F2 added %v (F2=%v)", u.tool, u.input, got, f2)
			}
			none++
			continue
		}
		if slices.Equal(got, []string{Star}) {
			if !f2 {
				t.Errorf("%s %#v: F2 emitted * but the Deriver's F2 is false", u.tool, u.input)
			}
			star++
			continue
		}
		if len(got) == 0 || f2 {
			t.Errorf("%s %#v: main is *, F2 gave %v (F2=%v): neither * nor named scopes", u.tool, u.input, got, f2)
			continue
		}
		for _, sc := range got {
			spelled := false
			for _, s := range ss {
				spelled = spelled || strings.Contains(s, ".cache/"+CacheRootName+"/"+sc+"/")
			}
			if sc == Star || !ValidScope(sc) || !spelled {
				t.Errorf("%s %#v: main is *, F2 named %v: %q is not a valid scope the input spells", u.tool, u.input, got, sc)
			}
		}
		if _, ok := provenScopes(ss, lowers, roles); !ok {
			t.Errorf("%s %#v: F2 named %v for an input the grammar rejects", u.tool, u.input, got)
		}
		named++
	}
	t.Logf("%d row inputs + %d random: %d add nothing (main: nothing), %d `*`, %d named (main: `*`)",
		rows, len(corpus)-rows, none, star, named)
	if rows < 300 || none < 200 || star < 1000 || named < 100 {
		t.Fatalf("the corpus did not exercise the property: %d rows, %d nothing, %d `*`, %d named", rows, none, star, named)
	}
}

// randomCorpus returns n random tool inputs built from fragments that sit on every side of F2's
// rules: clean cache paths and near-misses, every allowed command and some that are not, allowed
// and disallowed flags, `cd`/`tar`/`find`, separators and metacharacters; then perturbed by a case
// flip, a non-ASCII rune (the Kelvin sign lowers to `k` under strings.ToLower only), or an invalid
// byte.
func randomCorpus(r *rand.Rand, n int) []corpusUnit {
	cmds := []string{
		"cat", "cat -n", "grep -n x", "grep -c 'a|b'", "grep -r x", "grep -m1 x", "grep -m 2 x",
		"grep --index -l x", "grep -e x", "ls", "ls -R", "ls -la", "head -n 3", "tail -5", "wc -l",
		"wc --files0-from=-", "cut -c1-9", "nl", "uniq -c", "tr a b", "echo", "printf x", "cd", "pushd",
		"tar -C ~/.cache -czf /tmp/s.tgz", "find ~/.cache -path", "sed -n 1p", "kubectl -n", "xargs cat",
		"bash -c", "cairn --cache /tmp/x",
	}
	args := []string{
		"~/.cache/subsystem-store/alder/a.md", "/home/u1/.cache/subsystem-store/birch/b.md",
		"/root/.cache/subsystem-store/cedar/c.md", "~/.cache/subsystem-store/alder/subsystem-store.md",
		"~/.cache/subsystem-store", "~/.cache/subsystem-store/", "~/.cache/subsystem-store/alder",
		"~/.cache/subsystem-store/*/a.md", "~/.cache/subsystem-store-other/alder/a.md",
		"~/.cache/subsystem-store.lock", "~/.cache/subsystem-store/al.der/a.md",
		"~/.cache/subsystem-store/alder/../birch/b.md", "$HOME/.cache/subsystem-store/alder/a.md",
		"~/.config/subsystem-store/ui-token", "apps/subsystem-store/x.yaml", "subsystem-store",
		"SUBSYSTEM-STORE", "Subsystem-Store", "subsystem-stores", "'*subsystem-store*'", "~/.cache",
		".cache", ".", "..", "-", "~", "x.md", "notes", `"$W"`, "$(mktemp -d)", "{} +", "-exec cat",
		"/work/repo", "../sibling", "-R", "-r", "-n", "--index", "-A3", "-A 3", "-e x", "-m 5", "'a b'",
		"-d recurse", "--count",
	}
	seps := []string{" && ", "; ", " | ", "\n", " || ", " > /tmp/o; ", " & ", " "}
	dirs := []string{"/work/repo", "/home/u1/work/repo", "/home/u1", "/", "/home/u1/.cache/subsystem-store/alder",
		"/work/subsystem-store", "/srv/xcache", "~/.cache", "/root/w"}
	descs := []string{"Read the note", "see subsystem-store", "cd ~/.cache", "$X > y", "~/.cache/subsystem-store/alder/a.md"}
	pick := func(xs []string) string { return xs[r.Intn(len(xs))] }
	line := func() string {
		var b strings.Builder
		for c, nc := 0, 1+r.Intn(3); c < nc; c++ {
			if c > 0 {
				b.WriteString(pick(seps))
			}
			b.WriteString(pick(cmds))
			for a, na := 0, r.Intn(4); a < na; a++ {
				b.WriteString(" " + pick(args))
			}
		}
		s := b.String()
		if r.Intn(8) == 0 {
			s = `x="$(` + s + `)"`
		}
		switch r.Intn(10) {
		case 0: // flip the case of one letter
			if i := r.Intn(len(s)); s[i] >= 'a' && s[i] <= 'z' {
				s = s[:i] + string(s[i]-'a'+'A') + s[i+1:]
			}
		case 1: // a non-ASCII rune
			i := r.Intn(len(s) + 1) // every fragment is ASCII, so every index is a rune boundary
			s = s[:i] + "K" + s[i:]
		case 2: // an invalid byte
			i := r.Intn(len(s) + 1)
			s = s[:i] + "\xff" + s[i:]
		}
		return s
	}
	var out []corpusUnit
	for k := 0; k < n; k++ {
		switch r.Intn(5) {
		case 0:
			out = append(out, corpusUnit{"", []string{line()}})
		case 1:
			out = append(out, corpusUnit{"", []string{line(), line()}})
		case 2:
			in := map[string]any{"command": line()}
			if r.Intn(2) == 0 {
				in["workdir"] = pick(dirs)
			}
			if r.Intn(2) == 0 {
				in["description"] = pick(descs)
			}
			out = append(out, corpusUnit{pick([]string{"bash", "Bash"}), in})
		case 3:
			out = append(out, corpusUnit{"Read", map[string]any{"file_path": pick(args)}})
		default:
			out = append(out, corpusUnit{pick([]string{"Grep", "Glob", "Task"}), map[string]any{
				"pattern": pick(args), "path": pick(dirs), "description": pick(descs),
				"nested": []any{line(), map[string]any{"k": pick(args)}},
			}})
		}
	}
	return out
}
