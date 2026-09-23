package client

import (
	"os"
	"path/filepath"
	"strings"
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
// what is shared here: `anchoredNames` does the enumeration, `anchoredGlob` layers the one
// pattern walk that needs it, and each caller keeps its own predicate beside its own reasons.
// The class is named once, here, rather than three times in three comments.
//
// ⚠ `filepath.Glob` HAS NO `QuoteMeta`, WHICH IS WHY THIS IS A WALK AND NOT AN ESCAPE. There
// is no supported way to spell "this part of the pattern is literal", and hand-escaping the
// anchor would have to reproduce `Match`'s own grammar — a second copy of the rule, which is
// the shape this file exists to remove.

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

// hasGlobMeta is `filepath.Match`'s own metacharacter set on this platform.
//
// ⚠ `\` IS IN IT BECAUSE IT IS `Match`'s ESCAPE CHARACTER everywhere except Windows, where
// `Match` treats it as the separator and escaping is disabled. This client is built for the
// same platforms the rest of the port is; the set is stated here so a reader does not have to
// re-derive it from `path/filepath`'s source.
func hasGlobMeta(part string) bool {
	return strings.ContainsAny(part, `*?[\`)
}

// anchoredGlob is `Path(anchor).glob(pattern)`: `anchor` is a literal directory path and ONLY
// `pattern` is interpreted, component by component, exactly as the oracle does it.
//
// The returned paths are in `filepath.Glob`'s own order — parent directories in the order they
// were matched, and names within each in `os.ReadDir`'s sorted order — so a caller that used to
// sort or index into `Glob`'s result gets the same sequence.
//
// ⚠ A COMPONENT WITH NO METACHARACTER IS RESOLVED BY `Lstat`, NOT BY ENUMERATING ITS PARENT,
// AND THE DIFFERENCE IS OBSERVABLE. `filepath.Glob` splits its argument at the LAST separator
// and only reads the directory it ends up with, so `<repo>/claudedocs/handoff-*.md` never reads
// `<repo>` at all — it lists `<repo>/claudedocs`. A walk that enumerated every component would
// therefore find nothing where `<repo>` is searchable but not readable (mode `--x`), which both
// the old code and the oracle's `_PreciseSelector` handle fine. Same results for ordinary paths
// is the requirement; this is the branch that keeps it.
func anchoredGlob(anchor, pattern string) []string {
	dirs := []string{anchor}
	parts := strings.Split(filepath.ToSlash(pattern), "/")
	for i, part := range parts {
		last := i == len(parts)-1
		var next []string
		for _, dir := range dirs {
			next = appendAnchoredMatches(next, dir, part, last)
		}
		dirs = next
	}
	return dirs
}

// appendAnchoredMatches is one component of `anchoredGlob`'s walk.
//
// `last` decides the kind check: an INTERMEDIATE component must resolve to a directory, because
// the walk has to descend through it, and `os.Stat` is what asks — it FOLLOWS a symlink, which
// is what `Glob` does when it reads a matched middle component as a directory, and what the
// oracle's `dironly` selectors do. A FINAL component is kept whatever it is, for the same
// reason `Glob` keeps it: deciding is the caller's job.
func appendAnchoredMatches(out []string, dir, part string, last bool) []string {
	usable := func(child string) bool {
		if last {
			return true
		}
		info, err := os.Stat(child)
		return err == nil && info.IsDir()
	}
	if !hasGlobMeta(part) {
		child := filepath.Join(dir, part)
		if _, err := os.Lstat(child); err != nil || !usable(child) {
			return out
		}
		return append(out, child)
	}
	for _, name := range anchoredNames(dir, func(name string) bool {
		// 🔴 A `Match` ERROR CANNOT HAPPEN HERE AND IS STILL CHECKED. `part` comes from a
		// pattern a caller wrote, not from a path a caller was handed — that is the whole
		// separation this file draws — but a future caller could pass an ill-formed one, and
		// "no match" is the answer `Glob` gives for it too.
		ok, err := filepath.Match(part, name)
		return err == nil && ok
	}) {
		child := filepath.Join(dir, name)
		if usable(child) {
			out = append(out, child)
		}
	}
	return out
}
