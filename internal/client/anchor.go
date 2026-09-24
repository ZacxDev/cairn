package client

import (
	"os"
)

// 🔴 ONE RULE, IN ONE PLACE: **AN ANCHOR IS A PATH, NEVER PART OF A PATTERN.**
//
// Every site in this package that used to build a `filepath.Glob` argument by joining a
// directory the OPERATOR named — a cache root, a repo path, a cache's own basename — onto a
// pattern was wrong in the same way: `filepath.Match` interprets `*`, `?`, `[` and `\`
// wherever they appear, so a directory called `wid[get` is not matched, it is PARSED. An
// unterminated `[` is `ErrBadPattern`, which `Glob` returns with a nil match slice; every one
// of those call sites discarded the error, so the defect surfaced as an EMPTY RESULT — a
// false claim of absence rather than a failure. A `*` or `?` in the anchor is worse in a
// quieter way: no error at all, and a match set computed against some OTHER directory.
//
// The Python oracle never had any of it. `Path(anchor).glob(pattern)` treats the anchor
// literally by construction, so each of these was also a silent cross-client divergence that
// the parity gate could not see, because no world it built named a directory with a
// metacharacter in it. (`tests/parity/harness.py` now builds one — see its world root.)
//
// 🔴 THE THREE REMAINING SITES DO **NOT** SHARE A PREDICATE, AND FORCING ONE WOULD HAVE BEEN
// THE ORIGINAL MISTAKE AGAIN. `Focus` matches a fixed GLOB pattern against names, `ReapOrphans`
// matches a literal PREFIX, and `Put` matches one exact name plus a `<ref>.*.md` family. What
// they share is the MECHANISM — enumerate the anchor, then decide per entry NAME — so that is
// what is shared here: `anchoredNames` does the enumeration and each caller keeps its own
// predicate beside its own reasons. The class is named once, here, rather than three times in
// three comments.
//
// 🔴 THERE IS DELIBERATELY NO GENERAL `Path(anchor).glob(pattern)` WALKER IN THIS FILE, AND ONE
// WAS WRITTEN AND THEN DELETED TO PUT THAT SENTENCE HERE. `anchoredGlob` walked a pattern
// component by component so that an INTERMEDIATE component could itself be a wildcard. It had
// exactly one production caller — `Focus` — and `Focus`'s only patterns are `HandoffGlobs`,
// both of which have a metacharacter-free directory prefix (`claudedocs/`). For every input the
// program can actually present, the walk therefore collapsed to a single `anchoredNames` call
// against `filepath.Join(repo, "claudedocs")`, which is the shape the other two sites already
// use. The multi-component-wildcard branch had no caller and could not get one from
// `HandoffGlobs`, so its two test rows were the only thing exercising ~55 lines of production
// code — a generality nobody had asked for, pinned by tests written to cover it. The
// precondition the collapse rests on is not left to a comment:
// `TestHandoffGlobsKeepTheLiteralDIRECTORYPrefixThatFocusJOINS` fails if a pattern with a
// metacharacter before its last `/` is ever added, because `Focus` would then narrow silently.
//
// ⚠ `filepath.Glob` HAS NO `QuoteMeta`, WHICH IS WHY EVERY SITE ENUMERATES AND NONE ESCAPES.
// There is no supported way to spell "this part of the pattern is literal", and hand-escaping
// the anchor would have to reproduce `Match`'s own grammar — a second copy of the rule, which
// is the shape this file exists to remove.

// anchoredNames is the entry names directly under `dir` for which `keep` is true, in
// `os.ReadDir` order — which is sorted by name.
//
// ⚠ THE READ ERROR IS DISCARDED, AND THAT IS THE PRE-EXISTING BEHAVIOUR KEPT ON PURPOSE. Every
// caller replaced a `filepath.Glob` that swallowed its own error, and the oracle's `Path.glob`
// yields no paths rather than raising on an unreadable directory. Surfacing it here would be a
// divergence, not an improvement.
func anchoredNames(dir string, keep func(name string) bool) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if keep(entry.Name()) {
			out = append(out, entry.Name())
		}
	}
	return out
}
