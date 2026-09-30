package write

import "slices"

// tagVocabulary is the CLOSED set of `tags:` front-matter values a WRITE may land.
// The axis is the technical domain an entry belongs to, which is deliberately a
// different question from `store.Kinds` (service/process/org/doc — what SHAPE of thing
// the entry describes); an entry carries one value from each, and neither set is the
// other's refinement.
//
// 🔴 IT LIVES IN THE WRITER'S PACKAGE AND NOT BESIDE `store.Kinds`, AND THE REASON IS A
// MEASURED OUTAGE RATHER THAN TASTE. `store.parseTagsField` is the READER. A refusal
// there makes the entry MALFORMED, and a malformed entry is out of the index, out of
// `--ref`, out of `--search` AND UNWRITABLE — every write route resolves its target
// THROUGH the index (`resolveWritable`), so `PUT` and `POST .../bullets` answer 404 for
// it. A vocabulary check in the reader would therefore take every entry already
// carrying an off-vocabulary tag and make it unreadable and unrepairable in the same
// stroke: a store-wide outage caused by the guard, not by the data.
//
// 🔴 AND THE SPLIT IS ENFORCED BY THE COMPILER RATHER THAN BY THIS COMMENT.
// `internal/write` imports `internal/store`, so a back-import is an import CYCLE and
// the reader CANNOT reach this list. That is the whole reason the vocabulary is not a
// sibling of `Kinds` in `internal/store`: a constant the reader can see is a constant a
// later edit can wire into `parseTagsField`, and the refusal that edit produces is the
// outage above. It is also unexported, so no consumer of this package can grow a second
// copy of the check — `internal/client` imports `write.BulletTextMax` and could
// otherwise have grown a client-side pre-check, which `tests/parity/` would then need
// in BOTH clients to stay green.
//
// ⚠ SORTED, AND THAT IS PART OF THE REFUSAL'S CONTRACT RATHER THAN TIDINESS. The
// refusal message joins this slice with `|`; `lib/entry_shape.py`'s `TAG_VOCABULARY`
// joins its own tuple the same way; `tests/conformance/` compares the two servers'
// response bytes for a `PUT` carrying an off-vocabulary tag. Reordering or extending
// one side alone is a RED corpus, not a review comment —
// `tests/test_tag_vocabulary.py` is the cheaper red that says which side moved.
var tagVocabulary = []string{"client-work", "infra", "product", "tooling"}

// tagOutsideVocabulary returns the FIRST tag that is not in `tagVocabulary`.
//
// 🔴 IT TAKES THE FOLDED SET, NEVER THE RAW FRONT MATTER, so `tags: [Infra]` and
// `tags: [infra]` are the same write. `store.parseTagsField` has already lowercased,
// folded through `NormalizeRef`, deduped and SORTED by the time this sees it — which is
// also what makes "the first offender" deterministic: one body cannot name one tag on
// this run and a different one on the next, and two servers handed one body name the
// same tag. There is no separate operand normaliser here for the same reason `HasTag`
// has none: one fold, applied once, on the side that owns the spelling.
//
// ⚠ ONE TAG REPORTED, NOT THE SET. An operator fixes the front matter one line at a
// time and the next write re-runs this check, so a list adds bytes to the refusal
// without adding a decision to it.
func tagOutsideVocabulary(tags []string) (string, bool) {
	for _, tag := range tags {
		if !slices.Contains(tagVocabulary, tag) {
			return tag, true
		}
	}
	return "", false
}
