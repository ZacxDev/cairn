package scopeuse

import "testing"

// #314 round 4: a RELATIVE word beside a named read. The grammar let a word with no `/` through as
// an argument, so `cat <named path>; grep -r x .cache` from the home directory derived the named
// scope while reading every scope — `*` on origin/main (the bare-substring rule), so a fail-open
// regression. Since #314 round 6 three rules close it: a word naming `cache` fails, an occurrence of
// `subsystem-store` outside every accepted clean cache path makes the input `*`
// ([provenScopes]), and a flag not on its command's allowlist fails ([commandFlags]). Scope names
// are distinct per row.

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
// and is not a clean cache path is `*` — the `cache` rows by the cache-word rule, the
// `subsystem-store` rows by the occurrence count. No row carries a disallowed flag or a dot word.
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

// TestF2FlagAllowlist: every flag of an allowed command must be on THAT command's allowlist
// ([commandFlags]); an unknown one is `*`, whole, abbreviated or quoted. The recursive and name-list
// modes are the reason (`grep -r`, ugrep's `-NUM`/`--depth`/`--index`, `ls -R`, `wc
// --files0-from`), but the rule is the list, not those. No row names a cache word or a dot word.
func TestF2FlagAllowlist(t *testing.T) {
	const p = " ~/.cache/subsystem-store/alder-r/x.md"
	cases := map[string][]string{}
	for _, f := range []string{
		"-r", "-R", "-rn", "-nR", "--recursive", "--rec", "--dereference-recursive", "--der",
		"-d recurse", "-drecurse", "--directories=recurse", "--direc=recurse", "'-r'", "-'R'n",
		// Claude Code's `grep` is its embedded ugrep, where these recurse or index.
		"-2", "-n2", "--depth=3", "--index",
		// Attached digits fail, so does a non-numeric count, and so does a flag left waiting.
		"-m1", "-A3", "-B2", "-C1", "-m x", "-ie", "-f pats", "--color=never", "-z", "--", "-",
	} {
		cases["grep "+f+" x"+p+" foo"] = []string{Star}
	}
	cases["grep x"+p+" -e"] = []string{Star}
	for _, f := range []string{"-R", "-laR", "-lR", "--recursive", "--recursiv", "--color"} {
		cases["ls "+f+p+" foo"] = []string{Star}
	}
	for _, f := range []string{"--files0-from=-", "--files0-from list", "--files0=list", "--files0-from"} {
		cases["wc "+f+p] = []string{Star}
	}
	for _, in := range []string{
		"cat -x" + p, "tail -f" + p, "head --follow" + p, "printf -v v" + p, "echo -x" + p,
		"cut --output-delimiter=, -f1" + p, "uniq --zero-terminated" + p, "nl -x" + p, "tr -x a b" + p,
	} {
		cases[in] = []string{Star}
	}
	// Controls: each command's allowed flags, and the letters that are recursion for ONE command
	// only (`ls -r` reverses, `cut -d` is a delimiter).
	for _, in := range []string{
		"grep -c foo" + p, "grep -n 'a|b'" + p + " | cut -c1-80", "grep -i -e x" + p, "grep -m 5 -n foo" + p,
		"grep -A 3 -B 2 -C 1 foo" + p, "grep -inwx foo" + p, "grep -lLhHoEFsqvc foo" + p,
		"grep --count --line-number --ignore-case --invert-match --word-regexp --line-regexp " +
			"--only-matching --no-filename --with-filename --extended-regexp --fixed-strings " +
			"--files-with-matches --files-without-match --quiet --silent --no-messages foo" + p,
		"ls -la" + p, "ls -r" + p, "ls --reverse" + p, "ls -1aAdFhilnrSt" + p, "wc -l" + p, "wc -lwcmL" + p,
		"wc --lines --words --bytes --chars --max-line-length" + p, "cut -d, -f1" + p, "cut -c 1-9" + p,
		"tail -n 2" + p, "tail -5" + p, "head -20" + p, "head --lines=3" + p, "head -n5" + p,
		"cat -nA" + p, "cat --number" + p, "echo -n x; cat" + p, "nl -ba" + p, "uniq -c" + p,
		"cat" + p + " | tr -d a",
	} {
		cases[in] = []string{"alder-r"}
	}
	checkStrings(t, cases)
}
