package store

import (
	"unicode/utf8"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// RequirementsHeading is the FOURTH canonical section: what the entry's subsystem was
// SAID to do, with open/met state.
//
// 🔴 IT IS A SECTION AND NOT A NEW `kind:`, AND NOT A FIRST-CLASS OBJECT. The `Kinds`
// enum is closed and asserted, so a fifth kind would move every kind-keyed table; a
// first-class requirement with its own routes and its own store would be a tracker, and
// the store is deliberately not one. A heading is additive: `HeadingBlocks` and
// `ExtractSections` are generic over headings, so an entry that has never heard of this
// one parses exactly as it did before.
//
// ⚠ IT IS DECLARED HERE RATHER THAN BESIDE THE OTHER THREE IN `journal.go` FOR ONE
// REASON ONLY — `journal.go`'s block is the vocabulary the WRITE path reads, and nothing
// writes a requirement yet. Moving it there the day a writer exists is correct; putting
// it there today would say the write path knows about it, which is a claim no code makes.
const RequirementsHeading = "## Requirements"

// The three answers `BulletProvenance` gives. 🔴 ABSENT IS A DECIDED ANSWER, NOT AN
// ACCIDENT — the whole feature being asked for is "who said this", so a requirement whose
// author nobody recorded has to be distinguishable from one nobody asked about. A caller
// that folded absent into inferred would be asserting an attribution the file does not
// make, which is the one direction that manufactures provenance.
const (
	ProvenanceOperator = "operator"
	ProvenanceInferred = "inferred"
	ProvenanceAbsent   = ""
)

// Requirement is one bullet of the requirements section: a JournalBullet — the SAME
// bullet, with the SAME openness vocabulary — plus who stated it.
//
// 🔴 IT EMBEDS `JournalBullet` RATHER THAN RE-DECLARING ITS FIELDS, because the openness
// state machine is the thing being reused and a parallel struct is how two readers come
// to disagree about what `RESOLVED <sha>:` means. `OpennessPopulation`, `FirstLine`,
// `Text` and `Date` are all inherited and none of them is re-spelled here.
type Requirement struct {
	JournalBullet

	// Provenance is ProvenanceOperator, ProvenanceInferred or ProvenanceAbsent — read
	// from a prefix AFTER the openness marker, never from inside it.
	Provenance string
}

// IsMet answers "has this requirement been met", in the two-state vocabulary the journal
// already has and with no third state invented.
//
// 🔴 IT READS `OpennessPopulation`, NOT `Openness`, so it cannot disagree with the badge
// beside it. `PopulationUnverifiable` — a `RESOLVED:` naming no sha — counts as MET: the
// writer closed it, and the only thing missing is the means to check the closure, which is
// exactly the distinction that population exists to report separately. Folding it into
// "not met" would reopen an action somebody finished.
func (r Requirement) IsMet() bool {
	switch r.OpennessPopulation() {
	case PopulationResolved, PopulationUnverifiable:
		return true
	}
	return false
}

// IsOpen answers "is this requirement still outstanding" — DECLARED open, exactly.
//
// ⚠ IT IS NOT `!IsMet()`, AND THE GAP BETWEEN THEM IS THE POINT. A bullet carrying no
// marker at all is neither open nor met; it is unstated, and both of these return false
// for it. A `!IsMet()` spelling would silently promote every unmarked bullet to an open
// requirement and inflate the badge with work nobody declared.
func (r Requirement) IsOpen() bool {
	return r.OpennessPopulation() == PopulationOpen
}

// ParseRequirements groups a REQUIREMENTS-section body into requirement bullets.
//
// 🔴 IT TAKES THE SECTION BODY, NOT THE ENTRY, AND THAT IS THE SECTION BOUNDARY GUARD.
// The claim a requirement makes is "the operator said this subsystem should do X", and a
// bullet saying the same words under `## Nuance / work-history` is a note about history,
// not a stated requirement. The two are told apart by WHICH SECTION THEY SIT IN and by
// nothing else — no keyword, no shape — so the only way this function can be wrong is if
// a caller hands it the wrong body. Callers get it from
// `ExtractSections(text, …)[RequirementsHeading]`, which is a lookup that cannot silently
// widen.
//
// Bullet grouping is `ParseJournalBullets` verbatim: same column-0 rule, same fence
// skipping, same continuation handling, same trailing-blank strip. A second grouper here
// would be the duplicated predicate that diverges the day one side learns a new fence
// spelling — and it would diverge invisibly, because both would look right on the simple
// case everyone tests.
func ParseRequirements(body string) []Requirement {
	bullets := ParseJournalBullets(body)
	out := make([]Requirement, 0, len(bullets))
	for _, b := range bullets {
		out = append(out, Requirement{
			JournalBullet: b,
			Provenance:    BulletProvenance(b.FirstLine()),
		})
	}
	return out
}

// BulletProvenance reads `(operator)` / `(inferred)` from the run immediately AFTER the
// bullet's openness marker, and returns ProvenanceAbsent for everything else.
//
// 🔴 AFTER THE MARKER, INSIDE THE TEXT — NEVER AS A CAPTURE GROUP INSIDE `journalOpenness`.
// That regex is anchored, terminator-exact, and among the most heavily mutation-tested
// expressions in the store; it also decides `MarkerSpan`, which every surface that renders
// a badge uses to strip the prefix it must not print. Widening it to carry an optional
// parenthetical would change what counts as a marker for EVERY journal bullet in every
// entry, to add a field only this section reads. So the marker is read first and this
// starts where the marker ended:
//
//   - OPEN: (operator) the share page should name the project
//     ^ MarkerSpan ends here
//
// ⚠ SO A LINE WITH NO PARSED MARKER HAS NO PROVENANCE, BY CONSTRUCTION. `MarkerSpan` is 0
// for every line `BulletOpenness` refuses, and position 0 is the `-`, which is not `(`.
// That is the intended narrowing and the safe direction: a near-miss bullet's whole
// finding is that its marker did not parse, and attaching an author to it would dress a
// failed write up as a recorded one.
//
// 🔴 THE MATCH IS THE WHOLE PARENTHESISED WORD, NOT A PREFIX. `(operator-ish)` and
// `(operators)` are ProvenanceAbsent, not operator — a guard that accepted a prefix would
// be walkable by writing anything starting with the right letters, and provenance is
// precisely the claim that must not be manufacturable by accident.
func BulletProvenance(firstLine string) string {
	word, _ := bulletProvenance(firstLine)
	return word
}

// ProvenanceSpan is the byte offset in `firstLine` immediately PAST the parenthetical
// [BulletProvenance] read — the openness marker, the optional whitespace after it and the
// `(operator)` / `(inferred)` word together — or 0 when the line declares no provenance.
//
// 🔴 IT EXISTS SO A RENDERER THAT REPLACES THE PARENTHETICAL WITH A BADGE CAN REMOVE IT BY
// OFFSET RATHER THAN BY SPELLING ITS OWN `\(operator\)`. That is the same division of labour
// [MarkerSpan] already has with the openness marker, and for the same reason: a surface that
// re-spelled the grammar would be a second parser, free to disagree with
// `BulletProvenance` about what counts as provenance — and it would disagree invisibly,
// because both look right on `- OPEN: (operator) …`.
//
// 🔴 0 IS "NO PROVENANCE HERE", NEVER "STRIP NOTHING BUT THE MARKER". It is 0 on exactly the
// lines `BulletProvenance` answers ProvenanceAbsent for — a near miss (`MarkerSpan` is 0), a
// declared marker with no parenthetical, and `(operator-ish)` — so a caller that cuts at this
// offset when it is non-zero and at `MarkerSpan` otherwise removes text on precisely the
// lines a badge replaces it on, and on no others.
//
// ⚠ AND IT IS NEVER LESS THAN `MarkerSpan(firstLine)` WHEN IT IS NON-ZERO, because the scan
// starts where the marker ended. A caller may therefore use it in place of the marker span
// rather than in addition to it.
func ProvenanceSpan(firstLine string) int {
	_, span := bulletProvenance(firstLine)
	return span
}

// bulletProvenance is the ONE scan both exported spellings read, so the word and the offset
// cannot disagree about which run of bytes the provenance is. Two functions each doing their
// own scan is the duplicated predicate that renders a badge for `(inferred)` while cutting
// the bytes of something else.
func bulletProvenance(firstLine string) (string, int) {
	span := MarkerSpan(firstLine)
	if span == 0 {
		return ProvenanceAbsent, 0
	}
	rest := firstLine[span:]
	// The separator between the marker and the parenthetical is optional whitespace,
	// read with Python's 29-code-point `\s` rather than Go's five — the same rule every
	// other transcription in this package uses, so a non-breaking space cannot mean one
	// thing here and another in `BulletDate`.
	//
	// Counted in BYTES rather than code points because the offset is what the caller cuts
	// at; `pytext.IsSpace` still decides membership per RUNE, so the two questions stay
	// separate.
	gap := 0
	for _, r := range rest {
		if !pytext.IsSpace(r) {
			break
		}
		gap += utf8.RuneLen(r)
	}
	for _, candidate := range []string{ProvenanceOperator, ProvenanceInferred} {
		if n := matchParenthesizedWord(rest[gap:], candidate); n != 0 {
			return candidate, span + gap + n
		}
	}
	return ProvenanceAbsent, 0
}

// matchParenthesizedWord is `\(<word>\)` at position 0, exactly — case-sensitive, and
// with the closing paren REQUIRED so the word cannot be a prefix of a longer one. It
// returns the match's LENGTH IN BYTES, or 0 for no match.
//
// Case-sensitive on purpose: `(Operator)` is ProvenanceAbsent. The two spellings are
// schema tokens, not prose, and folding them would be the first step toward accepting
// `(OPERATOR)`, `(operator — via chat)` and everything else a writer might reach for,
// each of which is a different claim that nothing would then be able to tell apart.
//
// ⚠ BYTE INDEXING IS SAFE HERE ONLY BECAUSE `(`, `)` AND BOTH WORDS ARE ASCII, AND THAT IS THE
// WHOLE ARGUMENT: a multi-byte rune can satisfy the LENGTH check — where the rune-indexed
// version this replaced would not — but can never satisfy the byte-exact EQUALITY, so it is
// refused either way, one line later. The offset is what a caller cutting the text needs, and
// a bool could not give it one.
//
// ⚠ AN EARLIER VERSION CITED A PAIR-COUNT FROM AN AD-HOC DIFFERENTIAL AGAINST THE RUNE FORM,
// AND THE FIGURE IS DELETED RATHER THAN CORRECTED. The harness was never committed, so nobody
// could re-derive it — and an independent rebuild of the same sweep got a different total,
// which is exactly what an uncheckable number is worth. Either commit the sweep as a test or
// state the argument; this states the argument. `TestProvenanceIsTheWholeParenthesizedWord`
// and `requirements-provenance-accepts-a-prefix` in `tests/control_mutants.py` are the
// checkable guards on the behaviour that argument is about.
func matchParenthesizedWord(s string, word string) int {
	if len(s) < len(word)+2 || s[0] != '(' {
		return 0
	}
	if s[1:1+len(word)] != word || s[1+len(word)] != ')' {
		return 0
	}
	return len(word) + 2
}
