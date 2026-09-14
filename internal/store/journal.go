package store

import (
	"regexp"
	"strings"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// The three headings the store's entry shape declares. Only the nuance heading is
// read by the write path; the other two are here because they are one vocabulary
// and splitting it across packages is how two readers come to disagree about what
// a section is called.
const (
	WhatHeading     = "## What it is"
	PointersHeading = "## Pointers"
	NuanceHeading   = "## Nuance / work-history"
)

// journalBullet matches a TOP-LEVEL journal bullet, which starts at COLUMN 0.
//
// Measured over the live corpus on the Python side: every bullet line is at indent
// 0 and every continuation line is at indent 2. So an indented `-` is a
// CONTINUATION (a nested list, or prose that happens to start with a dash), never
// a new bullet — folding the two together would split one bullet into several and
// report a history longer than the entry has.
var journalBullet = regexp.MustCompile(`^[-*][ \t]+`)

// IsFence answers "is this line a code-fence delimiter".
//
// 🔴 IMPORTED, NOT RE-SPELLED, IS THE RULE THIS MIRRORS. The Python write path
// imports the reader's private `_is_fence` rather than copying it, because a
// `## Nuance / work-history` written INSIDE a fence must not be mistaken for the
// real heading — exactly as the section extractor treats it. A second copy is the
// duplicated predicate that diverges the day one side learns a new fence spelling.
func IsFence(line string) bool {
	s := strings.TrimLeftFunc(line, isPyWhitespaceRune)
	return strings.HasPrefix(s, "```") || strings.HasPrefix(s, "~~~")
}

// isPyWhitespaceRune is the per-rune form of Python's argument-less
// `str.lstrip()`/`str.rstrip()`, which strip EVERY whitespace character rather
// than space and tab. Named for what it tests: an earlier spelling called it
// `isSpaceOrTab`, which is a claim two characters narrower than the code.
func isPyWhitespaceRune(r rune) bool {
	return pytext.IsSpace(r)
}

// JournalBullet is one top-level bullet of a nuance section, VERBATIM.
//
// 🔴 `Lines` IS A SLICE, NOT A STRING, because a real bullet is WRAPPED PROSE.
// Measured over the live corpus: 110 top-level bullets carried 250 continuation
// lines between them — a median bullet is 3 lines and the longest is 19. Any model
// that assumed one line per bullet would silently truncate most of the corpus, and
// a truncated bullet is exactly the thing a writer would fail to recognise as a
// near-duplicate of the line it is about to write.
//
// ⚠ IT USED TO CARRY ONLY `Lines`, ON THE GROUNDS THAT A FIELD NO CODE PATH READS IS
// A DECLARATION NOTHING HONOURS — and it said the date and the openness marker would
// "arrive with the renderer, which is the only thing that reports them". They have
// arrived: `internal/report`'s index row is the consumer, so the fields are here and
// the comment is updated rather than left contradicting the struct beside it.
type JournalBullet struct {
	Lines []string

	// Date is the ISO date the bullet is dated with, or "". ~44% of the oracle's
	// live corpus carries no date, so "" is an ordinary reading and not a parse
	// failure.
	Date string

	// Openness is OpennessOpen, OpennessResolved or "" — the bullet's DECLARED
	// marker. "" is by far the common reading and means only that nothing was
	// declared. 🔴 It does NOT mean "this bullet proposes no work".
	Openness string

	// ResolvedBy is the sha a `RESOLVED <sha>:` bullet names as having closed it, or
	// "". A `RESOLVED` with no sha parses fine and leaves this empty: the marker is
	// still worth having, it just cannot be verified — which is exactly the
	// distinction `PopulationUnverifiable` reports, so the field is branched on and
	// not merely stored.
	ResolvedBy string
}

// OpennessPopulation is WHICH of the six populations this bullet belongs to. Exactly
// one.
//
// 🔴 THE SINGLE SOURCE OF THE PRECEDENCE ORDER, and the reason it exists rather than
// each caller testing the fields. On the oracle a delta re-audit found one bullet
// counted TWICE in a writer-facing block — a line that is both a near-miss and an
// unmarked action rendered under both headings — because two surfaces each decided
// membership for itself. Every consumer branches on this.
//
// Precedence, most-certain first; an earlier case wins outright:
//
//	open          the writer declared `OPEN:`. Exact.
//	unverifiable  a `RESOLVED:` naming no sha; closed but unprovable.
//	resolved      a `RESOLVED <sha>:`. Nothing to report.
//	near-miss     no marker parsed, but the line looks like an attempt. Beats
//	              `unmarked` because "your write did not land" is actionable and
//	              specific, where "this reads like an open action" is a guess about
//	              the same line.
//	unmarked      no marker, and the prose matches the narrow floor.
//	none          everything else — the overwhelming majority.
//
// ⚠ ONLY `near-miss` > `unmarked` IS OBSERVABLE, and this says so rather than implying
// all five levels are load-bearing. `nearMissMarker` and `unmarkedAction` both
// self-suppress once `Openness` is set, so reordering `open`/`resolved`/`unverifiable`
// against them are EQUIVALENT mutants that no test can kill — measured as survivors on
// the oracle's own battery. The order is still written most-certain-first because that
// is what makes it readable; just do not count those levels as guards.
func (b JournalBullet) OpennessPopulation() string {
	switch b.Openness {
	case OpennessOpen:
		return PopulationOpen
	case OpennessResolved:
		if b.ResolvedBy != "" {
			return PopulationResolved
		}
		return PopulationUnverifiable
	}
	if nearMissMarker(b.FirstLine()) {
		return PopulationNearMiss
	}
	if unmarkedAction(b.Text()) {
		return PopulationUnmarked
	}
	return PopulationNone
}

// FirstLine is the bullet's opening line, or "" for a bullet with no lines (which
// ParseJournalBullets never produces, since a group is opened BY a line).
func (b JournalBullet) FirstLine() string {
	if len(b.Lines) == 0 {
		return ""
	}
	return b.Lines[0]
}

// Text is the whole bullet, lines rejoined with `\n` — the unit `unmarkedAction`
// searches, because that advisory is about the bullet's prose and a real bullet is
// WRAPPED prose.
func (b JournalBullet) Text() string { return strings.Join(b.Lines, "\n") }

// ParseJournalBullets groups a nuance-section body into top-level bullets.
//
// Order is preserved exactly as stored — this function makes NO claim about which
// bullet is newest. The store's convention is newest-first, but that is a
// convention a writer can break.
//
// Rules, each measured against the corpus on the Python side rather than assumed:
//
//   - A bullet starts at column 0. Every other non-blank line attaches to the
//     bullet above it, indented or not.
//   - Text BEFORE the first bullet is dropped from the bullet list. A caller must
//     not read an empty result as "the section is empty" — a non-empty body that
//     yields no bullets is its own state.
//   - 🔴 FENCED BLOCKS ARE SKIPPED. A `- ` line inside a fence is sample text, and
//     promoting it to a bullet invents history the entry does not have.
//   - Trailing blank lines are stripped from each bullet so a blank separator
//     cannot inflate a bullet's line count.
func ParseJournalBullets(body string) []JournalBullet {
	var groups [][]string
	inFence := false
	for _, line := range pytext.SplitLines(body) {
		if IsFence(line) {
			inFence = !inFence
			if len(groups) > 0 {
				groups[len(groups)-1] = append(groups[len(groups)-1], line)
			}
			continue
		}
		if !inFence && journalBullet.MatchString(line) {
			groups = append(groups, []string{line})
			continue
		}
		if len(groups) > 0 {
			groups[len(groups)-1] = append(groups[len(groups)-1], line)
		}
	}
	out := make([]JournalBullet, 0, len(groups))
	for _, group := range groups {
		for len(group) > 0 && pytext.StripWhitespace(group[len(group)-1]) == "" {
			group = group[:len(group)-1]
		}
		openness, resolvedBy := BulletOpenness(group[0])
		out = append(out, JournalBullet{
			Lines:      group,
			Date:       BulletDate(group[0]),
			Openness:   openness,
			ResolvedBy: resolvedBy,
		})
	}
	return out
}

// NuanceBlock returns the line index a new bullet is inserted AT and the body of
// the nuance section it points into, for the FIRST nuance heading.
//
// 🔴 ONE WALK, BECAUSE THE INSERTION SCOPE AND THE DEDUPE SCOPE MUST BE THE SAME
// SECTION. On the Python side they were not: insertion took the FIRST heading while
// the duplicate check read the section extractor, which CONCATENATES every block
// sharing a heading. An entry carrying the heading twice therefore answered
// `200 duplicate` — writing nothing — for a genuinely NEW bullet that merely
// matched one sitting in the SECOND section, a section this writer would never
// have inserted into. That is content loss in the direction the design says matters
// most, and it is silent: the response says the observation is already recorded.
//
// A heading is `#` at column 0, outside a fence, compared with trailing whitespace
// removed — because that is the parser every reader uses, and a writer that
// disagreed about where the section starts would insert into prose. The FIRST
// occurrence wins, and the next heading of ANY level ends the section.
//
// `ok` is false when the entry has no nuance heading at all, which is the one
// shape an append cannot be given a home in.
func NuanceBlock(lines []string) (insertAt int, body string, ok bool) {
	inFence := false
	start := -1
	var bodyLines []string
	for index, line := range lines {
		if IsFence(line) {
			inFence = !inFence
			if start >= 0 {
				bodyLines = append(bodyLines, line)
			}
			continue
		}
		if !inFence && strings.HasPrefix(line, "#") {
			if start >= 0 {
				break
			}
			if strings.TrimRightFunc(line, isPyWhitespaceRune) == NuanceHeading {
				start = index + 1
			}
			continue
		}
		if start >= 0 {
			bodyLines = append(bodyLines, line)
		}
	}
	if start < 0 {
		return 0, "", false
	}
	return start, strings.Trim(strings.Join(bodyLines, "\n"), "\n"), true
}
