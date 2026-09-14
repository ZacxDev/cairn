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

	"github.com/ZacxDev/cairn/internal/pytext"
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
// 🔴 `pytext.Lower`, NOT `strings.ToLower`, AND THE COMMENT THAT USED TO SIT HERE
// DENIED THE BUG IT WAS STANDING ON. It said U+0130's expansion "reaches the same
// folded string here, because the combining mark is outside the class and collapses
// away — checked for that code point specifically". The check was real and it was only
// ever run on the character ALONE, where the resulting dash is at the end of the string
// and `strings.Trim` removes it. In the middle of a string that dash SURVIVES, because
// U+0307 is outside `[a-z0-9.-]` and therefore becomes a SEPARATOR:
//
//	İ    -> oracle "i"     old Go "i"     (agreed — the trailing dash is trimmed)
//	İa   -> oracle "i-a"   old Go "ia"    (DIVERGED)
//	aİb  -> oracle "ai-b"  old Go "aib"   (DIVERGED)
//	İİ   -> oracle "i-i"   old Go "ii"    (DIVERGED)
//
// A ref that folds differently resolves to a different entry or to none, so this is a
// create/alias collision rather than a cosmetic difference.
//
// ⚠ AND "NOT REACHABLE FROM A URL" IS NOT "NOT REACHABLE". The old note reasoned that
// `SAFE_PATH_COMPONENT` is ASCII so a non-ASCII ref cannot be named in a path — true, and
// irrelevant, because this fold is also applied to entry FRONT MATTER: `service:`,
// `scope:`/`repo:` and every `aliases:` entry go through it, and those arrive in a `PUT`
// BODY. `EntryFromMapping` is the caller that makes it reachable.
//
// ⚠ ONE DIVERGENCE, MEASURED OVER THE WHOLE CODE-POINT RANGE rather than argued: U+0130
// is the only code point whose `str.lower()` is not a single code point on the pinned
// interpreter, and no code point lowers in Go but not in Python. See `pytext.Lower`.
//
// ⚠ `pytext.StripWhitespace` REPLACED `strings.TrimSpace` IN THE SAME CHANGE AND IS
// **NOT** A FIX — labelled so it is not counted as one, and so nobody "simplifies" it
// back. The two predicates genuinely differ (`str.isspace()` is true for U+001C..U+001F,
// `unicode.IsSpace` is not), but the difference is UNOBSERVABLE here for a structural
// reason: any character this strip would have removed is outside `[a-z0-9.-]`, so
// `nonSlug` turns it into a leading or trailing `-` that the closing `strings.Trim`
// removes anyway. Checked at three points — leading, trailing, and a whole string of it.
// It is changed because the oracle's line is `raw.strip().lower()` and porting one half of
// it faithfully while leaving the other on Go's predicate is how the next caller of this
// function inherits a difference nobody decided on.
func NormalizeRef(raw string) string {
	s := pytext.Lower(pytext.StripWhitespace(raw))
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
