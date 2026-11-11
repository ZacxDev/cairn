package scopeuse

import "testing"

// #314 round 4: a RELATIVE word beside a named read. The grammar let a word with no `/` through as
// an argument, so `cat <named path>; grep -r x .cache` from the home directory derived the named
// scope while reading every scope — `*` on origin/main (the bare-substring rule), so a fail-open
// regression. Two rules close it: a word naming `cache`/`subsystem-store` that is not a clean cache
// path fails, and a recursive or name-list flag fails ([readsBeyondItsWords]). Every case uses only
// the API that predates the round (`CacheScopes`, `checkUnits`), so this file runs against ed3d8b3
// and shows each non-invariant row RED there. Scope names are distinct per row.

// TestF2RelativeAndRecursiveReadsAddStar (🔴): the review's shapes through BOTH runtimes, each beside
// a named read, and the positive controls that must stay named.
func TestF2RelativeAndRecursiveReadsAddStar(t *testing.T) {
	star := []string{Star}
	bash := func(name, cmd string, want ...string) unitCase {
		return unitCase{name, "bash", map[string]any{"command": cmd}, want}
	}
	const pre = "cat ~/.cache/subsystem-store/"
	checkUnits(t, []unitCase{
		bash("the .cache example", pre+"alder-z1/x.md; grep -r x .cache", star...),
		bash("grep -rn x subsystem-store", pre+"alder-z2/x.md; grep -rn x subsystem-store", star...),
		bash("grep -R x . (invariant: a dot word at ed3d8b3)", pre+"alder-z3/x.md; grep -R x .", star...),
		bash("grep --recursive x foo", pre+"alder-z4/x.md; grep --recursive x foo", star...),
		bash("grep -d recurse x foo", pre+"alder-z5/x.md; grep -d recurse x foo", star...),
		bash("ls -R", pre+"alder-z6/x.md; ls -R", star...),
		bash("ls -laR foo", pre+"alder-z7/x.md; ls -laR foo", star...),
		// Positive controls.
		bash("grep -n | cut", "grep -n 'a|b' ~/.cache/subsystem-store/alder-y1/x.md | cut -c1-80", "alder-y1"),
		bash("grep -c", "grep -c foo ~/.cache/subsystem-store/alder-y2/x.md", "alder-y2"),
		bash("ls -la", "ls -la ~/.cache/subsystem-store/alder-y3/x.md", "alder-y3"),
	})
}

// TestF2GrammarRejectsCacheWords: a word that, unquoted, names `cache` or `subsystem-store` (any case)
// and is not a clean cache path is `*`. No row carries a recursive flag or a dot word, so only this
// rule decides them.
func TestF2GrammarRejectsCacheWords(t *testing.T) {
	const p = " ~/.cache/subsystem-store/alder-c/x.md"
	cases := map[string][]string{}
	for _, in := range []string{
		"cat" + p + " .cache",
		"cat" + p + " subsystem-store",
		"head -n 5" + p + " .CACHE",
		"wc -l" + p + " Subsystem-Store",
		"cat" + p + " '.ca'che",            // the shell reads `.cache`
		"cat" + p + " subsystem-store-c1x", // another instance's root, relatively
		"cat" + p + " xcachex",
		// Over-poisoned on purpose: flags and words that only SAY cache. They derived the named scope
		// at ed3d8b3 and are `*` now.
		"grep --cached x" + p,
		"grep -c --no-cache x" + p,
		"echo cached; cat" + p,
	} {
		cases[in] = []string{Star}
	}
	// Controls: a relative word that names no cache stays an argument (the residual T3 names), and a
	// quoted pattern that only spells parts of the name is not the name.
	cases["cat"+p+" notes.md"] = []string{"alder-c"}
	cases["grep -c 'ca che'"+p] = []string{"alder-c"}
	cases["cat"+p+" ~/.cache/subsystem-store/birch-c/y.md"] = []string{"alder-c", "birch-c"}
	checkStrings(t, cases)
}

// TestF2GrammarRejectsRecursiveReads: a flag putting `grep`, `ls` or `wc` into a mode that reads
// files its words do not name is `*`, whole or abbreviated, quoted or not. No row names a cache word
// or a dot word, so only this rule decides them.
func TestF2GrammarRejectsRecursiveReads(t *testing.T) {
	const p = " ~/.cache/subsystem-store/alder-r/x.md"
	cases := map[string][]string{}
	for _, f := range []string{
		"-r", "-R", "-rn", "-nR", "--recursive", "--rec", "--dereference-recursive", "--der",
		"-d recurse", "-drecurse", "--directories=recurse", "--direc=recurse", "'-r'", "-'R'n",
		// Claude Code's `grep` is its embedded ugrep, where these recurse.
		"-2", "-n2", "--depth=3",
	} {
		cases["grep "+f+" x"+p+" foo"] = []string{Star}
	}
	for _, f := range []string{"-R", "-laR", "--recursive", "--recursiv"} {
		cases["ls "+f+p+" foo"] = []string{Star}
	}
	for _, f := range []string{"--files0-from=-", "--files0-from list", "--files0=list"} {
		cases["wc "+f+p] = []string{Star}
	}
	// Controls: the same commands' other flags, and the letters that are recursion for ONE command
	// only (`ls -r` reverses, `cut -d` is a delimiter).
	for _, in := range []string{
		"grep -c foo" + p, "grep -n 'a|b'" + p + " | cut -c1-80", "grep -i -e x" + p, "grep --color=never x" + p,
		"ls -la" + p, "ls -r" + p, "ls --reverse" + p, "wc -l" + p, "cut -d, -f1" + p, "tail -n 2" + p,
	} {
		cases[in] = []string{"alder-r"}
	}
	checkStrings(t, cases)
}
