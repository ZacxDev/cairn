package scopeuse

import (
	"slices"
	"testing"
)

// #314 round 6: F2 is MAIN + ALLOWLIST. The bare-substring rule origin/main ships — a tool input
// any string of which contains `subsystem-store` (any case) is `*` — decides first, unchanged; the
// one exception is an input the allowlist grammar proves reads only files of named scopes, every
// occurrence of the name inside one of its clean cache paths. So F2 is never narrower than main:
// an input without the name adds nothing (the 18fb0a8 `cd` rule over-poisoned those), and every
// other input is `*` unless proven. Every case uses only the API that predates the round
// (`CacheScopes`, `checkStrings`, `checkUnits`, `ClaudeRecord`, `OpencodePart`), so this file runs
// against 18fb0a8 and shows each non-invariant row RED there.

// TestF2MustBeStar (🔴): the round-3 audit's shapes. Each names `subsystem-store`, so main is `*`,
// and none passes the grammar.
func TestF2MustBeStar(t *testing.T) {
	star := []string{Star}
	checkStrings(t, map[string][]string{
		// ugrep's `--index` is not on grep's flag allowlist.
		"cat ~/.cache/subsystem-store/alpha/a.md && grep --index -l needle": star,
		"tar -C ~/.cache -czf /tmp/s.tgz subsystem-store":                   star,
		"find ~/.cache -path '*subsystem-store*' -exec cat {} +":            star,
		`x="$(cd ~/.cache && cd subsystem-store && cat */a.md)"`:            star,
		"kubectl -n subsystem-store get pods":                               star,
		"cat ~/.config/subsystem-store/ui-token":                            star,
		// An attached digit: ugrep's `-NUM` recursion is one letter away. (Invariant: already `*`
		// at 18fb0a8, whose recursive-flag denylist held every digit.)
		"grep -m1 x ~/.cache/subsystem-store/alpha/a.md": star,
	})
}

// TestF2EveryMentionInsideACleanPath (🔴): an input the grammar accepts is still `*` when ONE
// occurrence of the name lies outside every clean cache path it accepted — a bare word, a pattern,
// a skipped description, a working directory.
func TestF2EveryMentionInsideACleanPath(t *testing.T) {
	star := []string{Star}
	// Rows e1–e3 were already `*` at 18fb0a8 (its cache-word rule): invariant rows, held because
	// that rule is gone and only the occurrence count decides them now.
	checkStrings(t, map[string][]string{
		"echo subsystem-store": star,
		"cat ~/.cache/subsystem-store/alder-e1/x.md subsystem-store":       star,
		"grep -c subsystem-store ~/.cache/subsystem-store/alder-e2/x.md":   star,
		"cat ~/.cache/subsystem-store/alder-e3/x.md; echo SUBSYSTEM-STORE": star,
		// Control: the name again INSIDE an accepted path is covered by it.
		"cat ~/.cache/subsystem-store/dunlin-e/subsystem-store-notes.md": {"dunlin-e"},
	})
	checkUnits(t, []unitCase{
		{"mention in a bash description", "bash", map[string]any{
			"command": "cat ~/.cache/subsystem-store/alder-e4/x.md", "description": "see subsystem-store"}, star},
		{"mention in a workdir", "bash", map[string]any{
			"command": "cat ~/.cache/subsystem-store/alder-e5/x.md", "workdir": "/work/subsystem-store"}, star},
		{"control: Read of a clean path", "Read", map[string]any{
			"file_path": "/home/u1/.cache/subsystem-store/alder-e6/x.md"}, []string{"alder-e6"}},
	})
}

// TestF2NoMentionAddsNothing (🔴): an input that does not contain `subsystem-store` is not `*` on
// main, so F2 adds nothing for it — whatever else it does. 18fb0a8's directory-change rule made
// these `*` (the over-poisoning); main's blind spots stay in T3.
func TestF2NoMentionAddsNothing(t *testing.T) {
	// `grep -r x .`, `ls -R` and the `cat` row added nothing at 18fb0a8 too: invariant rows.
	var cases []unitCase
	for _, cmd := range []string{
		"cd ..", "cd -", `cd "$W"`, "cd $(mktemp -d)", "cd ../sibling && make", "grep -r x .", "ls -R",
		"cd ~/.cache", "cd", "pushd ~/.cache && ls", "cairn --cache=/tmp/x get alder-n1",
		"cat /tmp/x/notes1/a.md",
	} {
		cases = append(cases, unitCase{cmd, "bash", map[string]any{"command": cmd, "description": "Move"}, []string{}})
	}
	checkUnits(t, cases)
}

// TestF2MustStayNamed: the positive controls, through BOTH runtimes, with literal sets. A rule that
// starred every mention would pass every test above; it fails here.
func TestF2MustStayNamed(t *testing.T) {
	bash := func(cmd string, want ...string) unitCase {
		return unitCase{cmd, "bash", map[string]any{"command": cmd}, want}
	}
	checkUnits(t, []unitCase{
		bash("cat ~/.cache/subsystem-store/alder/x.md", "alder"),
		bash("grep -c 'OTEL_HTTP' ~/.cache/subsystem-store/alder/x.md", "alder"),
		bash("grep -n 'a|b|c' /home/u1/.cache/subsystem-store/alder/n.md | cut -c1-180", "alder"),
		bash("grep -m 5 -n foo ~/.cache/subsystem-store/alder/x.md", "alder"),
		{"Read file_path", "Read", map[string]any{"file_path": "/home/u1/.cache/subsystem-store/alder/x.md"}, []string{"alder"}},
		{"opencode bash with workdir and description", "bash", map[string]any{
			"command": "cat ~/.cache/subsystem-store/alder/x.md", "workdir": "/home/u1/work/repo",
			"description": "read the note"}, []string{"alder"}},
		bash("cat ~/.cache/subsystem-store/alder/x.md ~/.cache/subsystem-store/birch/y.md", "alder", "birch"),
	})
}

// TestF2DirectoryChangeChainAddsStar: the round-5 chain, call by call in ONE session, through both
// runtimes. Its second call names `subsystem-store`, so main is `*` for the session, and so is F2
// — no directory-change rule needed.
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
