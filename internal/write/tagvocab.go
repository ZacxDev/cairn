package write

import "slices"

// tagVocabulary is the CLOSED set of `tags:` front-matter values a WRITE may land.
// The axis is the technical DOMAIN an entry belongs to, which is deliberately a
// different question from `store.Kinds` (service/process/org/doc — what SHAPE of thing
// the entry describes); an entry carries one value from each, and neither set is the
// other's refinement.
//
// 🔴 IT WAS FOUR TERMS FOR ONE ROUND, AND THE FOURTH FALSIFIED THE SENTENCE ABOVE.
// `client-work` shipped in the first draft beside these three and is REMOVED: `infra`,
// `product` and `tooling` all answer "what KIND of work is this", while `client-work`
// answers "WHO is it for" — a second axis. Under the one-tag-per-entry rule
// (`HasTag`'s scalar operand) a set mixing two axes makes both unassertable, which is
// the identical objection this repository already records against putting the category
// axis into `kind:`. The "who" question was also already answered elsewhere: the SCOPE
// name carries it, so the fourth term largely restated the directory an entry lives in.
// Dropping a term is the direction that can refuse a write somebody used to be able to
// make, so it was taken on an operator decision with the live store re-tagged first.
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
//
// ⚠ AND THE BROWSER IS NOT A WRITER, WHICH IS WHY NO BROWSER-SIDE GATE EXISTS. Measured
// rather than assumed: every state-changing route in `internal/ui/routes.go`'s dispatch
// table is `POST /share`, `/unshare`, `/sign-in`, `/sign-out`, `/invite`,
// `/invite/revoke` or `/sign-in/github` — sharing, sessions and invitations. There is a
// `GET /entry`, and it is a READ page; no route creates, replaces or appends to an entry
// at all, and `internal/ui` imports neither `internal/write` nor any other entry writer
// (it reaches `internal/store`'s parsers only). So "every writer is
// gated" is true of the browser only VACUOUSLY. Saying so is the point: a reader who
// took it for a built gate would stop looking on the day that surface grows one. The
// route set is already pinned against a hand-written ledger there
// (`TestTheRouteLedgerMatchesTheDispatchTable`), so a new route cannot arrive unseen —
// but nothing makes it arrive through THIS function, and that is the open edge.
var tagVocabulary = []string{"infra", "product", "tooling"}

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
