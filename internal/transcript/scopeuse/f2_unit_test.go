package scopeuse

import (
	"slices"
	"testing"
)

// #314 round 1: F2 decides over ONE TOOL INPUT WHOLE, and its fail-closed checks are structural
// rather than a list of spellings. Every case uses only the API that predates the round
// (`CacheScopes` of one string, `ClaudeRecord`, `OpencodePart`), so the same file runs against the
// commit before it and shows each case RED there. Scope names are distinct per case and from every
// constant the code names.

// viaClaude feeds input as a Claude Code `tool_use` block's input and returns Declare — V without
// F1's `*`, so an input naming `cairn` with no ledger cannot pass a case on F1's account.
func viaClaude(tool string, input map[string]any) []string {
	d := NewDeriver()
	d.ClaudeRecord(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role": "assistant",
			"content": []any{map[string]any{
				"type": "tool_use", "id": "toolu_01", "name": tool, "input": input,
			}},
		},
	})
	return d.Result().Declare
}

// viaOpencode feeds input as an opencode tool part's `state.input` and returns Declare.
func viaOpencode(tool string, input map[string]any) []string {
	d := NewDeriver()
	d.OpencodePart(map[string]any{
		"type": "tool", "tool": tool, "callID": "call_01",
		"state": map[string]any{"status": "completed", "input": input, "output": "ok"},
	})
	return d.Result().Declare
}

type unitCase struct {
	name, tool string
	input      map[string]any
	want       []string
}

// checkUnits runs every case through BOTH runtimes' record shapes: one input is one unit whichever
// client wrote it.
func checkUnits(t *testing.T, cases []unitCase) {
	t.Helper()
	for _, c := range cases {
		record(c.tool, c.input)
		if got := viaClaude(c.tool, c.input); !slices.Equal(got, c.want) {
			t.Errorf("%s (claude tool_use %s %v): Declare = %v, want %v", c.name, c.tool, c.input, got, c.want)
		}
		if got := viaOpencode(c.tool, c.input); !slices.Equal(got, c.want) {
			t.Errorf("%s (opencode part %s %v): Declare = %v, want %v", c.name, c.tool, c.input, got, c.want)
		}
	}
}

func checkStrings(t *testing.T, cases map[string][]string) {
	t.Helper()
	for in, want := range cases {
		record("", []string{in})
		if got := CacheScopes(in); !slices.Equal(got, want) {
			t.Errorf("CacheScopes(%q) = %v, want %v", in, got, want)
		}
	}
}

const testCacheRoot = "/home/u1/.cache/subsystem-store/"

// TestF2DecidesOverTheWholeInput (🔴): a `..`, or an ancestor argument, in ANOTHER string of the
// same input poisons the read a string names. Fed string by string these derived the named scope.
func TestF2DecidesOverTheWholeInput(t *testing.T) {
	star := []string{Star}
	checkUnits(t, []unitCase{
		// The review's two cases: opencode's per-call working directory, and a Glob root.
		{"workdir + .. command", "bash", map[string]any{
			"command": "cat ../birch-w/secret.md", "workdir": testCacheRoot + "alder-w", "description": "Show it"}, star},
		{"glob root + .. pattern", "Glob", map[string]any{
			"pattern": "../birch-g/**/*.md", "path": testCacheRoot + "alder-g"}, star},
		// The same, with the root ONE level down, so only the whole-input `..` rule decides.
		{"subdir workdir + .. command", "bash", map[string]any{
			"command": "cat ../../birch-u/x.md", "workdir": testCacheRoot + "alder-u/entries"}, star},
		{"grep path + .. glob", "Grep", map[string]any{
			"pattern": "secret", "path": testCacheRoot + "alder-h/entries", "glob": "../../birch-h/*.md"}, star},
		{"named read + ancestor workdir", "bash", map[string]any{
			"command": "grep -n token ~/.cache/subsystem-store/alder-a/x.md", "workdir": "/home/u1"}, star},
		// Positive controls: a named read stays named when nothing else in its input poisons it.
		{"file read", "Read", map[string]any{"file_path": testCacheRoot + "alder-r/x.md"}, []string{"alder-r"}},
		{"named read + repo workdir", "bash", map[string]any{
			"command": "cat ~/.cache/subsystem-store/alder-o/x.md", "workdir": "/work/repo",
			"description": "Show the note"}, []string{"alder-o"}},
		{"git range, no cache", "bash", map[string]any{"command": "git log main..HEAD", "workdir": "/work/repo"}, []string{}},
		{"cd, no cache", "Bash", map[string]any{"command": "cd /work/repo && make", "description": "Build"}, []string{}},
	})
}

// TestF2ScopeDirectoryStringAddsStar (🔴): a string that is NOTHING BUT a scope directory is a
// working directory or a recursive root, never a single-file read — even when the input's other
// strings look harmless.
func TestF2ScopeDirectoryStringAddsStar(t *testing.T) {
	star := []string{Star}
	checkUnits(t, []unitCase{
		{"workdir", "bash", map[string]any{"command": "cat notes.md", "workdir": testCacheRoot + "alder-d"}, star},
		{"workdir with slash", "bash", map[string]any{"command": "ls", "workdir": testCacheRoot + "alder-e/"}, star},
		{"glob root", "Glob", map[string]any{"pattern": "**/*.md", "path": "~/.cache/subsystem-store/alder-f"}, star},
	})
	checkStrings(t, map[string][]string{
		testCacheRoot + "alder-s": star,
		// Control: a FILE under the scope, read by an allowed command.
		"cat " + testCacheRoot + "alder-t/x.md": {"alder-t"},
	})
}

// TestF2ShellRewrittenPathAddsStar (🟡): a cache path whose token holds a byte the shell rewrites is
// `*` — each of these is `..` to the shell, and none is a `..` component as written.
func TestF2ShellRewrittenPathAddsStar(t *testing.T) {
	star := []string{Star}
	checkStrings(t, map[string][]string{
		`cat ~/.cache/subsystem-store/alder-1/.\./birch-1/x.md`:         star,
		`cat ~/.cache/subsystem-store/alder-2/.''./birch-2/x.md`:        star,
		`cat ~/.cache/subsystem-store/alder-3/"."./birch-3/x.md`:        star,
		`cat ~/.cache/subsystem-store/alder-4/.{.,}/birch-4/x.md`:       star,
		`cat ~/.cache/subsystem-store/alder-5/$'\x2e\x2e'/birch-5/x.md`: star,
		`cat ~/.cache/subsystem-store/alder-6/..\/birch-6/x.md`:         star,
		"cat ~/.cache/subsystem-store/alder-7/.\\\n./birch-7/x.md":      star, // backslash-newline
		"cat ~/.cache/subsystem-store/alder-8/.`printf .`/birch-8/x.md": star,
		`cat ~/.cache/subsystem-store/alder-9/.*/birch-9/x.md`:          star, // `..` to a pre-5.2 bash
		`cat ~/.cache/subsystem-store/alder-10/.?/birch-10/x.md`:        star,
		// Control: the same read with nothing rewritten.
		`cat ~/.cache/subsystem-store/alder-11/x.md`: {"alder-11"},
	})
}

// TestF2CdFamilyWordAddsStar (🟡): a cd-family word ANYWHERE in an input naming the cache, in any
// quoting, is `*`. The `builtin`/`command` rows were `*` before the round too (the old pattern
// matched a `cd` after whitespace): invariant guards, not regression rows.
func TestF2CdFamilyWordAddsStar(t *testing.T) {
	star := []string{Star}
	checkStrings(t, map[string][]string{
		`\cd ~/.cache/subsystem-store/alder-c1 && cat x.md`:                      star,
		`"cd" ~/.cache/subsystem-store/alder-c2 && cat x.md`:                     star,
		`cd "$(dirname ~/.cache/subsystem-store/alder-c3/x.md)" && cat y.md`:     star,
		`c''d ~/.cache/subsystem-store/alder-c4 && cat x.md`:                     star,
		`popd; cat ~/.cache/subsystem-store/alder-c5/x.md`:                       star,
		`cat "$(readlink -f ~/.cache/subsystem-store/alder-c6/x.md)"`:            star,
		`/usr/bin/realpath ~/.cache/subsystem-store/alder-c7/x.md | xargs cat`:   star,
		`builtin cd ~/.cache/subsystem-store/alder-c8; cat x.md`:                 star,
		`command pushd ~/.cache/subsystem-store/alder-c9 >/dev/null && cat x.md`: star,
	})
}

// TestF2AncestorArgumentAddsStar (🟡): another argument that is an ancestor of the cache root reads
// every scope under it.
func TestF2AncestorArgumentAddsStar(t *testing.T) {
	star := []string{Star}
	checkStrings(t, map[string][]string{
		"grep -c secret ~/.cache/subsystem-store/alder-a1 ~/.cache":         star,
		"grep -c secret ~/.cache/subsystem-store/alder-a2 $HOME/.cache":     star,
		"grep -c secret ~/.cache/subsystem-store/alder-a3 /home/u1/.cache/": star,
		"grep -c secret ~/.cache/subsystem-store/alder-a4 ~":                star,
		"grep -c secret ~/.cache/subsystem-store/alder-a5 /home/u1":         star,
		"grep -c secret ~/.cache/subsystem-store/alder-a6 /":                star,
		`grep -c secret ~/.cache/subsystem-store/alder-a7 "$HOME"`:          star,
		"grep -c secret ~/.cache/subsystem-store/alder-a8 $XDG_CACHE_HOME":  star,
		"grep -c secret ~/.cache/subsystem-store/alder-a9 ${HOME}":          star,
		// Controls: a flag, or a word, that is not a path is not an ancestor. (A word that says
		// "cache" is `*` now, whatever it is: TestF2GrammarRejectsCacheWords; so is a flag not on
		// the command's allowlist: TestF2FlagAllowlist.)
		"grep -c -i x ~/.cache/subsystem-store/alder-b0/x.md":   {"alder-b0"},
		"echo done; cat ~/.cache/subsystem-store/alder-b1/x.md": {"alder-b1"},
	})
}

// TestF2CacheFlagFollowsMain (🟡): the client's `--cache PATH` makes ANY directory a cache root,
// but F2 has no rule of its own for it any more: an input naming `subsystem-store` is `*` (the
// flag never passes the grammar — it names `cache`), and one that does not adds nothing, as on
// origin/main. The second half is T3 (the package doc), not a guarantee.
func TestF2CacheFlagFollowsMain(t *testing.T) {
	star := []string{Star}
	checkStrings(t, map[string][]string{
		"cairn --cache /tmp/x/subsystem-store recall q": star,
		"cat /tmp/x/subsystem-store/birch-t/x.md":       star,
		"cairn --cache=/tmp/x get alder-f1":             {},
		`cairn "--cache" /srv/copy ls`:                  {},
		"git diff --cached":                             {},
		"pip install --no-cache-dir pkg":                {},
	})
	checkUnits(t, []unitCase{
		{"flag", "bash", map[string]any{"command": "cairn --cache /srv/copy/subsystem-store recall q"}, star},
	})
}

// #314 round 3: F2 is an ALLOWLIST. A named scope is derived only when the whole input provably
// reads files of those scopes and nothing else under the cache; every other input naming the cache
// is `*`. Each test below pins ONE rule of the grammar ([shellLineProvable], [safeDir],
// [keyRole]) and carries a positive control the rule must leave named.

// TestF2AllowlistRejectsTheRoundTwoShapes (🔴): the round-2 audit's shapes, through BOTH runtimes.
// Each derived a named scope at b842b7e.
func TestF2AllowlistRejectsTheRoundTwoShapes(t *testing.T) {
	star := []string{Star}
	bash := func(name, cmd, workdir string) unitCase {
		in := map[string]any{"command": cmd}
		if workdir != "" {
			in["workdir"] = workdir
		}
		return unitCase{name, "bash", in, star}
	}
	checkUnits(t, []unitCase{
		bash("quoted dots from a subdir workdir", `cat .''./.''./birch-p1/x.md`, testCacheRoot+"alder-p1/entries"),
		bash("escaped dots from a subdir workdir", `cat .\./.\./birch-p2/x.md`, testCacheRoot+"alder-p2/entries"),
		bash("workdir ending /.", `cat .''./birch-p3/x.md`, testCacheRoot+"alder-p3/."),
		bash("workdir ending //", `cat .''./birch-p4/x.md`, testCacheRoot+"alder-p4//"),
		bash("workdir ending /./", `cat .''./birch-p5/x.md`, testCacheRoot+"alder-p5/./"),
		bash("~/.cache/.", "grep -n token ~/.cache/subsystem-store/alder-p6/x.md ~/.cache/.", ""),
		bash("cache glob", "grep -n token ~/.cache/subsystem-store/alder-p7/x.md /home/u1/.cache/*", ""),
		bash("double slash home", "grep -n token ~/.cache/subsystem-store/alder-p8/x.md /home//u1", ""),
		bash("~/.", "grep -n token ~/.cache/subsystem-store/alder-p9/x.md ~/.", ""),
		bash("/.", "grep -n token ~/.cache/subsystem-store/alder-p10/x.md /.", ""),
		bash("parameter expansion", "grep -n token ~/.cache/subsystem-store/alder-p11/x.md ${HOME:-x}", ""),
		bash(". from a home workdir", "grep -n token "+testCacheRoot+"alder-p12/x.md .", "/home/u1/."),
	})
}

// TestF2AllowlistKeepsProvableReadsNamed: the positive controls, through BOTH runtimes, with
// literal sets. A grammar that rejected everything would pass every `*` test above; it fails here.
func TestF2AllowlistKeepsProvableReadsNamed(t *testing.T) {
	bash := func(name, cmd string, want ...string) unitCase {
		return unitCase{name, "bash", map[string]any{"command": cmd}, want}
	}
	checkUnits(t, []unitCase{
		bash("cat", "cat ~/.cache/subsystem-store/alder-n1/x.md", "alder-n1"),
		bash("grep, quoted pattern", "grep -c 'OTEL_HTTP' ~/.cache/subsystem-store/alder-n2/x.md", "alder-n2"),
		bash("grep | cut", "grep -n 'a|b|c' "+testCacheRoot+"alder-n3/n.md | cut -c1-180", "alder-n3"),
		bash("two scopes", "cat ~/.cache/subsystem-store/alder-n4/x.md ~/.cache/subsystem-store/birch-n4/y.md",
			"alder-n4", "birch-n4"),
		{"Read file_path", "Read", map[string]any{"file_path": testCacheRoot + "alder-n5/x.md"}, []string{"alder-n5"}},
		{"bash + workdir + description", "bash", map[string]any{
			"command": "cat ~/.cache/subsystem-store/alder-n6/x.md", "workdir": "/home/u1/work/repo",
			"description": "read the note"}, []string{"alder-n6"}},
	})
}

// TestF2GrammarRejectsMetacharacters: any shell metacharacter outside single quotes is `*`; the
// five command separators are not.
func TestF2GrammarRejectsMetacharacters(t *testing.T) {
	const p = " ~/.cache/subsystem-store/alder-m/x.md"
	cases := map[string][]string{}
	for _, in := range []string{
		"cat" + p + " $X", "cat" + p + ` "x"`, "cat" + p + ` x\y`, "cat" + p + " x*", "cat" + p + " x?",
		"cat" + p + " [x]", "cat" + p + " {x,y}", "cat" + p + " > o", "cat" + p + " < i", "cat" + p + " &",
		"cat" + p + " `x`", "cat" + p + " (x)", "cat" + p + " a~b", "cat" + p + " #x",
		"cat" + p + " !x", "cat" + p + " ^x", "cat" + p + " x\r", "cat" + p + " |& wc",
		"cat" + p + " 'unterminated",
		"cat" + p + " ~", // invariant: already `*` at b842b7e (an ancestor word)
	} {
		cases[in] = []string{Star}
	}
	for _, in := range []string{
		"cat" + p + " | wc", "cat" + p + " || echo no", "cat" + p + " && echo ok", "cat" + p + "; echo ok",
		"cat" + p + "\necho ok", "cat" + p + "\tx",
	} {
		cases[in] = []string{"alder-m"}
	}
	checkStrings(t, cases)
}

// TestF2GrammarRejectsQuotedPaths: a single-quoted segment is literal text, unless it holds `/` or
// `..` — then it could be a path argument, and the input is `*`.
func TestF2GrammarRejectsQuotedPaths(t *testing.T) {
	checkStrings(t, map[string][]string{
		// q1 and q4 are invariant rows, already `*` at b842b7e (a quoted `..` component).
		"grep x ~/.cache/subsystem-store/alder-q1/x.md '..'":   {Star},
		"grep x ~/.cache/subsystem-store/alder-q2/x.md '/tmp'": {Star},
		"grep x ~/.cache/subsystem-store/alder-q3/x.md a'/'b":  {Star},
		"grep x ~/.cache/subsystem-store/alder-q4/x.md x'..'y": {Star},
		"grep 'a b|c' ~/.cache/subsystem-store/alder-q5/x.md":  {"alder-q5"},
		"grep a'b'c ~/.cache/subsystem-store/alder-q6/x.md":    {"alder-q6"},
		"grep '' ~/.cache/subsystem-store/alder-q7/x.md":       {"alder-q7"},
		"grep 'x . y' ~/.cache/subsystem-store/alder-q8/x.md":  {"alder-q8"},
	})
}

// TestF2GrammarRejectsOtherPaths: every word holding `/` must be ONE clean, unquoted file path
// under the default cache root — any other path, however harmless it looks, is `*`.
func TestF2GrammarRejectsOtherPaths(t *testing.T) {
	const p = "grep x ~/.cache/subsystem-store/alder-o/x.md "
	cases := map[string][]string{}
	for _, w := range []string{
		"/tmp/x", "/work/repo", "--file=/tmp/x", "a/b", "~/x", testCacheRoot + "alder-o",
		testCacheRoot + "alder-o/", testCacheRoot + "alder-o//x.md", testCacheRoot + "alder-o/./x.md",
		testCacheRoot + "alder-o/../birch-o/x.md", // invariant: `*` at b842b7e

		"/tmp/h1/.cache/subsystem-store/alder-o/x.md", "/home/u1/.CACHE/subsystem-store/alder-o/x.md",
		"~/.cache/subsystem-store/alder-o/x.md'x'", // invariant: `*` at b842b7e
		"/home/u%1/.cache/subsystem-store/alder-o/x.md",
	} {
		cases[p+w] = []string{Star}
	}
	cases[p+"/root/.cache/subsystem-store/birch-o/.hidden"] = []string{"alder-o", "birch-o"}
	cases[p+"/home/u1/.cache/subsystem-store/birch-o/sub/x@1+2.md"] = []string{"alder-o", "birch-o"}
	checkStrings(t, cases)
}

// TestF2GrammarRejectsDotWords: a word that is `.` or `..`, once unquoted, is a directory the
// command reads against an unknown working directory.
func TestF2GrammarRejectsDotWords(t *testing.T) {
	checkStrings(t, map[string][]string{
		"grep -n x ~/.cache/subsystem-store/alder-d1/x.md .":    {Star},
		"grep -n x ~/.cache/subsystem-store/alder-d2/x.md ..":   {Star}, // invariant: `*` at b842b7e
		"grep -n x ~/.cache/subsystem-store/alder-d3/x.md '.'":  {Star},
		"grep -n x ~/.cache/subsystem-store/alder-d4/x.md .''":  {Star},
		"grep -n x ~/.cache/subsystem-store/alder-d5/x.md .'.'": {Star},
		// Controls: a word that merely holds dots is an argument.
		"grep -n x.y ~/.cache/subsystem-store/alder-d6/x.md ...": {"alder-d6"},
		"grep -n .x ~/.cache/subsystem-store/alder-d7/x.md":      {"alder-d7"},
	})
}

// TestF2GrammarRejectsCommands: only a command that reads the files it is given and runs nothing
// else is provable; `cd`, `xargs`, `sed`, a shell, `find` … each turn text into a path or a program.
func TestF2GrammarRejectsCommands(t *testing.T) {
	const p = " ~/.cache/subsystem-store/alder-k/x.md"
	cases := map[string][]string{
		"echo" + p + " | sed s,alder-k.x.md,, | xargs grep -r t": {Star},
		"cat" + p + "; cd; grep -r t .cache":                     {Star}, // invariant: `*` at b842b7e (a cd word)
		"cat" + p + " | bash":                                    {Star},
		"cat" + p + " && find x -type f":                         {Star},
		"sed -n 1,40p" + p:                                       {Star},
		"awk 1" + p:                                              {Star},
		"X=1 cat" + p:                                            {Star},
		p[1:]:                                                    {Star}, // the path itself, run
		"'ba'sh" + p:                                             {Star},
	}
	for _, c := range []string{"cat", "cut -c1-9", "grep x", "head", "ls -la", "nl", "tail -n 3", "wc -l", "uniq"} {
		cases[c+p] = []string{"alder-k"}
	}
	cases["echo"+p+" | tr a b"] = []string{"alder-k"}
	cases["printf x"+p] = []string{"alder-k"}
	cases["c'a't"+p] = []string{"alder-k"}
	checkStrings(t, cases)
}

// TestF2WorkdirMustBeOutsideTheCache: a working directory that is the cache, under it, or names a
// cache is `*` — the input's relative words resolve inside it.
func TestF2WorkdirMustBeOutsideTheCache(t *testing.T) {
	star := []string{Star}
	cmd := "grep -n t ~/.cache/subsystem-store/alder-w1/x.md birch-w1"
	var cases []unitCase
	// The first and the fourth to sixth rows below were already `*` at b842b7e (a scope-directory
	// string, an ancestor word): invariant rows. The second and third were named there.
	for _, wd := range []string{
		"/home/u1/.cache/subsystem-store/birch-w1", "/home/u1/.cache/subsystem-store",
		"/srv/h1/.cache/subsystem-store/birch-w1/e", "/home/u1/.cache", "/srv/xcache",
	} {
		cases = append(cases, unitCase{"workdir " + wd, "bash", map[string]any{"command": cmd, "workdir": wd}, star})
	}
	cases = append(cases,
		unitCase{"glob path in the cache", "Glob", map[string]any{"pattern": "x", "path": testCacheRoot + "alder-w2"}, star},
		unitCase{"control: repo workdir", "bash", map[string]any{"command": cmd, "workdir": "/srv/h1/repo"}, []string{"alder-w1"}})
	checkUnits(t, cases)
}

// TestF2WorkdirMustNotBeAnAncestor: a working directory a home directory, and so its cache, lies
// under is `*` — `grep -r t u1` from `/home` reads every scope. Since #314 round 4 the recursive
// flag is `*` by itself ([commandFlags]), so the command here is NON-recursive and only
// this rule decides; it stays the second fence for a host `grep` whose modes the grammar does not
// know.
func TestF2WorkdirMustNotBeAnAncestor(t *testing.T) {
	star := []string{Star}
	cmd := "grep -n t ~/.cache/subsystem-store/alder-v1/x.md u1"
	var cases []unitCase
	// Only `/home/u1/.` and `home/u1` were named at b842b7e; the rest were already `*` there (an
	// ancestor word): invariant rows, held so the allowlist keeps them.
	for _, wd := range []string{"/", "/home", "/home/u1", "/root", "/Users", "/Users/u1", "/home/u1/.", "/home/u1/", "home/u1", "~"} {
		cases = append(cases, unitCase{"workdir " + wd, "bash", map[string]any{"command": cmd, "workdir": wd}, star})
	}
	cases = append(cases,
		unitCase{"control: under the home", "bash", map[string]any{"command": cmd, "workdir": "/home/u1/w"}, []string{"alder-v1"}},
		unitCase{"control: under /root", "bash", map[string]any{"command": cmd, "workdir": "/root/w"}, []string{"alder-v1"}})
	checkUnits(t, cases)
}

// TestF2OnlyABashDescriptionIsFreeText: a bash tool's `description` is never run, so the grammar
// skips it — and NOTHING else: not the command beside it, not another tool's `description`.
func TestF2OnlyABashDescriptionIsFreeText(t *testing.T) {
	checkUnits(t, []unitCase{
		{"bash command is read", "bash", map[string]any{
			"command": "cat ~/.cache/subsystem-store/alder-f1/x.md > /tmp/o", "description": "read it"}, []string{Star}},
		{"another tool's description is read", "Task", map[string]any{
			"description": "cat ~/.cache/subsystem-store/alder-f2/x.md $X"}, []string{Star}},
		{"another key is read", "bash", map[string]any{
			"command": "cat ~/.cache/subsystem-store/alder-f3/x.md", "comment": "$X > y"}, []string{Star}},
		{"control: a bash description is free", "bash", map[string]any{
			"command": "cat ~/.cache/subsystem-store/alder-f4/x.md", "description": `see "$X" > y`}, []string{"alder-f4"}},
		// A description is skipped, so a cache path there is not an accepted one: `*` (main's rule).
		{"a description naming a read is `*`", "bash", map[string]any{
			"command": "wc -l ~/.cache/subsystem-store/alder-f5/x.md", "description": "~/.cache/subsystem-store/birch-f5/y.md"},
			[]string{Star}},
		// Another tool's description is a shell line: a metacharacter there poisons the read in
		// another key, which a mention-count alone would not.
		{"another tool's description beside a read", "Task", map[string]any{
			"prompt": "cat ~/.cache/subsystem-store/alder-f6/x.md", "description": "$X > y"}, []string{Star}},
	})
}

// TestF2UnprovableShapesAddStar: shapes that DERIVED a named scope before the allowlist and are
// `*` now — the over-poisoning it accepts. Each row is a real read of one scope that the grammar
// cannot prove, so it hides the session from that scope's readers too; it is listed so that a
// change here is deliberate.
func TestF2UnprovableShapesAddStar(t *testing.T) {
	star := []string{Star}
	checkStrings(t, map[string][]string{
		"sed -n 1,40p /home/u1/.cache/subsystem-store/ember-u1/nix.md":               star,
		`sed -n '/x/,$p' ~/.cache/subsystem-store/ember-u2/nix.md`:                   star,
		`F=/home/u1/.cache/subsystem-store/ember-u3/a.md; cat "$F"`:                  star,
		"cat $HOME/.cache/subsystem-store/ember-u4/x.md":                             star,
		`cat "${HOME}/.cache/subsystem-store/ember-u5/a.md"`:                         star,
		"cat $XDG_CACHE_HOME/subsystem-store/ember-u6/x.md":                          star,
		"cat ~/.CACHE/SUBSYSTEM-STORE/Ember-u7/x.md":                                 star,
		"ls ~/.cache/subsystem-store/ember-u8":                                       star,
		"/tmp/h1/.cache/subsystem-store/ember-u9/x.md":                               star,
		testCacheRoot + "ember-u10/x.md":                                             star,
		"cat ~/.cache/subsystem-store/ember-u11/*.md":                                star,
		"$(cat ~/.cache/subsystem-store/ember-u12/a.md)":                             star,
		`cat '/home/u1/.cache/subsystem-store/ember-u13/x.md'`:                       star,
		"cat ~/.cache/subsystem-store/ember-u14/x.md > /tmp/out.txt":                 star,
		"cat ~/.cache/subsystem-store/ember-u15/x.md 2>/dev/null":                    star,
		"git log main..HEAD; cat ~/.cache/subsystem-store/ember-u17/x.md":            star,
		"grep -r secret ~/.cache/subsystem-store/ember-u18 /work/repo":               star,
		"grep -r secret ~/.cache/subsystem-store/ember-u19/x.md /work/repo/notes.md": star,
	})
}
