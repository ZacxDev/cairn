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
			"command": "grep -r token ~/.cache/subsystem-store/alder-a/x.md", "workdir": "/home/u1"}, star},
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
		// Controls: a FILE under the scope, and a directory that is one argument of a command.
		testCacheRoot + "alder-t/x.md":        {"alder-t"},
		"ls ~/.cache/subsystem-store/alder-v": {"alder-v"},
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
		// Controls: a closing quote, and a glob in the file name, are not a rewritten component.
		`cat "$HOME/.cache/subsystem-store/alder-11/x.md"`: {"alder-11"},
		`cat ~/.cache/subsystem-store/alder-12/*.md`:       {"alder-12"},
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
		"grep -r secret ~/.cache/subsystem-store/alder-a1 ~/.cache":         star,
		"grep -r secret ~/.cache/subsystem-store/alder-a2 $HOME/.cache":     star,
		"grep -r secret ~/.cache/subsystem-store/alder-a3 /home/u1/.cache/": star,
		"grep -r secret ~/.cache/subsystem-store/alder-a4 ~":                star,
		"grep -r secret ~/.cache/subsystem-store/alder-a5 /home/u1":         star,
		"grep -r secret ~/.cache/subsystem-store/alder-a6 /":                star,
		`grep -r secret ~/.cache/subsystem-store/alder-a7 "$HOME"`:          star,
		"grep -r secret ~/.cache/subsystem-store/alder-a8 $XDG_CACHE_HOME":  star,
		"grep -r secret ~/.cache/subsystem-store/alder-a9 ${HOME}":          star,
		// Controls: a repo path is not an ancestor, nor is a flag or a word that says "cache".
		"grep -r secret ~/.cache/subsystem-store/alder-a0 /work/repo": {"alder-a0"},
		"grep -r --no-cache x ~/.cache/subsystem-store/alder-b0/x.md": {"alder-b0"},
		"echo cached; cat ~/.cache/subsystem-store/alder-b1/x.md":     {"alder-b1"},
	})
}

// TestF2CacheFlagAddsStar (🟡): the client's `--cache PATH` makes ANY directory a cache root, so an
// input carrying it is `*` whatever it names.
func TestF2CacheFlagAddsStar(t *testing.T) {
	star := []string{Star}
	checkStrings(t, map[string][]string{
		"cairn --cache /tmp/x/subsystem-store recall q": star,
		"cairn --cache=/tmp/x get alder-f1":             star,
		`cairn "--cache" /srv/copy ls`:                  star,
	})
	checkUnits(t, []unitCase{
		{"flag", "bash", map[string]any{"command": "cairn --cache /srv/copy/subsystem-store recall q"}, star},
	})
	for _, in := range []string{
		// T3, named in the package doc: a store copy under a non-cache parent read DIRECTLY.
		"cat /tmp/x/subsystem-store/birch-t/x.md",
		// Controls: flags that merely start with `--cache` or contain it.
		"git diff --cached",
		"pip install --no-cache-dir pkg",
		"npm ci --cache-min 9",
	} {
		if got := CacheScopes(in); len(got) != 0 {
			t.Errorf("CacheScopes(%q) = %v, want nothing", in, got)
		}
	}
}
