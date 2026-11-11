package scopeuse

import (
	"slices"
	"testing"
)

// #314 round 5: a DIRECTORY CHANGE in an earlier call. Claude Code's Bash working directory
// persists between calls, so `cd ~/.cache`, `cd subsystem-store`, `cd alder`, then `cat x.md`
// reads a scope no single call names as a cache path — `*` on origin/main (the bare-substring
// rule), nothing or only the named scope at f029df8, so a fail-open regression. Every case uses
// only the API that predates the round (`CacheScopes`, `checkUnits`, `ClaudeRecord`,
// `OpencodePart`), so this file runs against f029df8 and shows each `*` row RED there. Scope names
// are distinct per row.

// TestF2DirectoryChangeAddsStar (🔴): a `cd`/`pushd` whose argument names the cache, or is not a
// plain path this can show stays outside it, is `*` through BOTH runtimes, with no named read in
// the input. The controls change directory to plain non-cache paths and add nothing.
func TestF2DirectoryChangeAddsStar(t *testing.T) {
	star := []string{Star}
	bash := func(cmd string, want ...string) unitCase {
		if want == nil {
			want = []string{}
		}
		return unitCase{cmd, "bash", map[string]any{"command": cmd, "description": "Move"}, want}
	}
	checkUnits(t, []unitCase{
		bash("cd ~/.cache", star...),
		bash("cd subsystem-store", star...),
		bash("cd .cache/subsystem-store", star...), // invariant: a cache root with no scope at f029df8
		bash("builtin cd SUBSYSTEM-STORE", star...),
		bash(`cd "$D"`, star...),
		bash("cd", star...),
		bash("cd ~", star...),
		bash("cd -", star...),
		bash("pushd ~/.cache && ls", star...),
		bash("git status; cd .Cache", star...),
		bash("command pushd ..", star...),
		bash("ls\ncd $(dirname x)", star...),
		// Controls: plain non-cache directories add nothing.
		bash("cd /work/repo && make"),
		bash("cd src"),
		bash("pushd /tmp/build"),
		bash("cd ~/work/repo-d1 && git status"),
		bash("echo cd ~ && make"),
		// Named controls stay named.
		bash("cat ~/.cache/subsystem-store/alder-d2/x.md", "alder-d2"),
		bash("cd /work/repo && cat ~/.cache/subsystem-store/alder-d3/x.md", star...), // cd is not a provable command
	})
	// The bash `description` is free text, never run.
	checkUnits(t, []unitCase{{"description only", "bash",
		map[string]any{"command": "make", "description": "cd ~/.cache then build"}, []string{}}})
}

// TestF2DirectoryChangeChainAddsStar (🔴): the review's chain, call by call in ONE session, through
// both runtimes: no call names a cache path, and the session must still be `*`.
func TestF2DirectoryChangeChainAddsStar(t *testing.T) {
	for _, tail := range []string{"cat x.md", "cat ~/.cache/subsystem-store/birch-d4/y.md x.md"} {
		cmds := []string{"cd ~/.cache", "cd subsystem-store", "cd alder-d5", tail}
		cd := NewDeriver()
		od := NewDeriver()
		for k, c := range cmds {
			in := map[string]any{"command": c}
			cd.ClaudeRecord(map[string]any{"type": "assistant", "message": map[string]any{
				"role": "assistant", "content": []any{map[string]any{
					"type": "tool_use", "id": "toolu_0" + string(rune('a'+k)), "name": "Bash", "input": in,
				}}}})
			od.OpencodePart(map[string]any{"type": "tool", "tool": "bash", "callID": "call_0" + string(rune('a'+k)),
				"state": map[string]any{"status": "completed", "input": in, "output": "ok"}})
		}
		for rt, d := range map[string]*Deriver{"claude": cd, "opencode": od} {
			if got := d.Result().Declare; !slices.Contains(got, Star) {
				t.Errorf("%s chain %q: Declare = %v, want it to hold %q", rt, cmds, got, Star)
			}
		}
	}
}

// TestF2DirectoryChangeStrings: the same rule on one raw line ([CacheScopes]), over the spellings
// the lexer has to read — quotes, `\`, prefixes, separators — and controls that only look like it.
func TestF2DirectoryChangeStrings(t *testing.T) {
	star := []string{Star}
	cases := map[string][]string{
		`cd '.ca'che`:            star,
		`c\d ~`:                  star,
		`"pushd"`:                star,
		`x=1 cd -P src`:          star,
		`{ cd ~/x-d6/../..; }`:   star,
		`if cd "$H"; then :; fi`: star,
		`make || cd /`:           star,
		`(cd /home/u1) && ls`:    star,
		`pushd +1`:               star,
		`cd src extra`:           {},
		`cd src/`:                {},
		`cd /opt/app-d7/`:        {},
		`grep cd notes.txt`:      {},
		`echo 'cd ~'`:            {},
		`abcd ~`:                 {},
	}
	checkStrings(t, cases)
}
