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
	return pytext.StripWhitespace(string(r)) == ""
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
// ⚠ IT CARRIES ONLY `Lines`, AND THAT IS DELIBERATE RATHER THAN INCOMPLETE. The
// reader's bullet also carries a parsed DATE and an OPENNESS marker (`OPEN:` /
// `RESOLVED <sha>:`); nothing on the write path branches on either, and a field in
// a struct that no code path reads is a declaration nothing honours. They arrive
// with the renderer, which is the only thing that reports them.
type JournalBullet struct {
	Lines []string
}

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
		out = append(out, JournalBullet{Lines: group})
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
