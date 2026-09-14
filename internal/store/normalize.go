// Package store is the Go port of the reader `lib/subsystem_resolver.py` and
// the index half of `lib/subsystem_recall.py`: ref folding, front matter, entry
// validation, the index, ref resolution, the path classifier and the journal
// bullet parser.
//
// 🔴 IT HOLDS NO RENDERING, AND THAT SPLIT IS THE P1a/P1b SEAM. `render_text`
// and `render_search` are the largest and most byte-sensitive part of the reader
// and they land in `internal/report` at P1b. Everything in this package is what
// the WRITE routes and `/snapshot` need — which is why P1a could not skip it:
// `PUT`'s 422 quotes the index loader's own refusal sentence, and `POST
// .../bullets` resolves its target through the same index a read does.
package store

import (
	"regexp"
	"strings"
)

// Kinds is the kind enum from the store's own schema. A trailing dot-segment is
// a kind ONLY if it appears here — otherwise it is part of the slug, which is
// what keeps a dotted slug (`forgejo.example.com`) or a component like
// `values.yaml` intact.
var Kinds = []string{"service", "process", "org", "doc"}

// 🔴 `_` IS FOLDED BY THIS CLASS AND NOWHERE ELSE, exactly as the Python side
// records: `_` is outside `[a-z0-9.-]`, so an explicit `_`→`-` replacement
// beside this would be an unkillable mutant reading as the guard that
// implements the rule.
var (
	nonSlug = regexp.MustCompile(`[^a-z0-9.-]`)
	dashRun = regexp.MustCompile(`-{2,}`)
)

// NormalizeRef folds a ref/slug/alias/path-component to its canonical form:
// lowercase, every character outside `[a-z0-9.-]` to `-`, runs collapsed, ends
// trimmed. Applied identically on read and write and to aliases before
// comparing.
//
// `.` SURVIVES — it is inside the character class. That is what lets kind
// qualification (`<slug>.<kind>`) and dotted slugs work at all.
//
// Returns "" for input that normalizes away entirely; every caller treats an
// empty result as "not a ref", never as a wildcard.
//
// ⚠ ONE MEASURED RESIDUAL AGAINST THE PYTHON ORACLE, stated rather than left to
// be found. `str.lower()` in Python may expand one code point into several
// (U+0130 LATIN CAPITAL LETTER I WITH DOT ABOVE lowercases to `i` + U+0307)
// while `strings.ToLower` maps it to a single `i`. Both then reach the same
// folded string here, because the combining mark is outside the class and
// collapses away — checked for that code point specifically. The residual is
// that a FUTURE Unicode special-casing pair could diverge; nothing in this
// store's charset (`SAFE_PATH_COMPONENT` is ASCII) can reach it, because a
// scope or ref that is not ASCII cannot be named in a URL path at all.
func NormalizeRef(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = nonSlug.ReplaceAllString(s, "-")
	s = dashRun.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// SplitKind splits an ALREADY-NORMALIZED ref into (slug, kind). `kind` is ""
// when the ref carries none.
//
// Used for BOTH index filenames and incoming refs, on purpose — one splitter,
// so a ref and the filename it should reach can never disagree about where the
// kind boundary is. A leading-dot ref (`.process`) has no slug, so it is not a
// kind qualification either, which is why `head` must be non-empty.
func SplitKind(ref string) (slug, kind string) {
	i := strings.LastIndex(ref, ".")
	if i <= 0 {
		return ref, ""
	}
	head, tail := ref[:i], ref[i+1:]
	for _, k := range Kinds {
		if tail == k {
			return head, tail
		}
	}
	return ref, ""
}
