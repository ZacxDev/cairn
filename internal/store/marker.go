package store

import "github.com/ZacxDev/cairn/internal/pytext"

// The reason tokens the two write-protocol advisories are keyed on. They are
// SEPARATE tokens for separate populations and their counts are never summed.
//
// 🔴 BOTH ARE "THE READER CANNOT SEE IT", AND THEY ARE STILL DIFFERENT
// QUANTITIES WITH DIFFERENT REMEDIES. An unreachable marker sits INSIDE a bullet
// that is read — the bullet renders, the marker declares nothing — and is fixed
// by PROMOTING that line to a bullet of its own. A dropped line sits in NO
// bullet: nothing about it is read, marker or otherwise, and it is fixed by
// restoring the bullet opening that used to sit above it. A single count
// covering both would send half the readers to the wrong remedy.
//
// 🔴 AND NEITHER IS A NEAR-MISS. A near-miss is a marker MIS-SPELLED where the
// parser looks; these are markers spelled CORRECTLY, or lost entirely, where it
// never looks.
const (
	ReasonUnreachableMarker = "unreachable-marker"
	ReasonDroppedLine       = "dropped-line"
)

// UnreachableMarker is one correctly-spelled openness marker sitting where NO
// reader looks: a bullet's line 2..n.
//
// 🔴 THE SHAPE THAT COST A REAL OPEN ACTION ITS BADGE. Measured in the field on
// the oracle's own store: one bullet carried a second, correctly-spelled marker
// several lines into its body. `BulletOpenness` reads a bullet's OPENING line and
// its pattern is anchored at position 0, so that declaration reached no surface at
// all — it had only ever raised a badge BY ACCIDENT, through a broken `RESOLVED —`
// sitting above it in the same section. Fixing the broken line would therefore have
// SILENCED a still-open action, which is the failure this marker exists to prevent,
// arriving through the fix for a different one.
type UnreachableMarker struct {
	// Offset is the 1-based index of the line WITHIN THE BULLET. Always >= 2 —
	// line 1 is what the parser already reads, so a marker there is reachable by
	// definition.
	Offset int

	// Line is the continuation line, VERBATIM — indentation and all. The report
	// quotes it, and a stripped copy would send a writer looking for a line as
	// typed.
	Line string

	// Openness is `open` or `resolved`: what this marker WOULD have declared had
	// it been at the head of a bullet. Derived by running the real parser, never
	// re-spelled.
	Openness string

	// ResolvedBy is the sha a `RESOLVED <sha>:` names, same normalisation as the
	// real parser.
	ResolvedBy string
}

// AsOpeningLine puts a CONTINUATION line into OPENING-line position, verbatim
// otherwise.
//
// 🔴 THIS IS THE WHOLE DERIVATION, AND IT IS DELIBERATELY THE ONLY NEW GRAMMAR.
// The marker vocabulary is NOT restated: the scanners below hand each line to
// `BulletOpenness` — the same function `ParseJournalBullets` calls for line 1 —
// and this normalisation exists solely because that pattern requires the
// `^[-*][ \t]+` bullet prefix, which a wrapped prose line does not have. A ledger
// that restated `OPEN|RESOLVED` could not catch what it was written for: the point
// is that this stays in step with the real pattern even if the pattern changes.
//
// A line that ALREADY opens with a bullet marker (a nested list item — the field
// case) is passed through with only its indentation removed, so nothing is
// manufactured; anything else is given the minimal `- ` prefix.
func AsOpeningLine(line string) string {
	stripped := pytext.StripWhitespace(line)
	if journalBullet.MatchString(stripped) {
		return stripped
	}
	return "- " + stripped
}

// LineOpenness is `(openness, resolvedBy)` for ANY line, opening or continuation.
//
// 🔴 THE ONE PLACE THAT ANSWERS "DOES THIS NON-OPENING LINE CARRY A MARKER".
// `JournalBullet.UnreachableMarkers` asks it of a bullet's lines 2..n, and
// `LineCarriesMarker` asks it of a line that reaches no bullet at all. Both had
// the same question and on the oracle only one of them had the answer: the
// dropped-line scanner shipped a hand-spelled copy that fired on `* OPEN:` (a
// bullet character the corpus does not use), missed `- OPEN:` and every dated
// `- 2000-01-02: OPEN: …`, and returned true for the prose `resolved upstream in
// 1.2.3.` — inert on the field shape and loose on everything else. Consolidating
// is what made that audible.
//
// A marker MID-line declares nothing, here as everywhere else, because the pattern
// is anchored at position 0.
func LineOpenness(line string) (openness, resolvedBy string) {
	return BulletOpenness(AsOpeningLine(line))
}

// LineMentionsMarker answers "does this line MENTION a declaration anywhere in
// it?". RANKING ONLY.
//
// The oracle's pattern is
//
//	(?:OPEN|RESOLVED)(?![A-Za-z0-9_])
//	(?:[ \t]*(?:[0-9a-fA-F]{7,40}(?![0-9a-fA-F])|PR#\d+|#\d+
//	          |\([^)]{1,30}\)|\[[^\]]{1,30}\]))*
//	[^A-Za-z0-9\n]{0,4}:
//
// case-insensitively, SEARCHED (not anchored) over the whole line.
//
// 🔴 THE COLON IS NOT OPTIONAL, and that is the whole difference from
// `nearMissMarker`'s shouted branch. That branch may skip the terminator because
// it is ANCHORED at a bullet head, where prose does not shout; unanchored over a
// whole line the same rule fires on `OPEN SOURCE`. Requiring the colon keeps
// `OPEN:` and `RESOLVED <sha>:` and rejects `OPEN SOURCE`, `RESOLVED_ADDR in the
// trace…` (the word-boundary guard) and `resolved upstream in 1.2.3.` (no colon
// follows the token or a ref form).
//
// 🔴 IT IS NOT A PARSE AND MUST NEVER GATE ONE. A marker mid-line declares nothing
// to any reader — that is deliberate and unchanged. This exists because a line that
// is ALREADY LOST is a different question from a line being parsed: the motivating
// field incident is a bullet whose `OPEN:` sat mid-line, so a signal anchored at
// position 0 is zero on the only shape ever observed in the wild. Ranking that as
// ordinary lost text, rather than as a lost DECLARATION, is what an audit called
// inert.
//
// ⚠ HAND-ROLLED FOR THE SAME REASON `nearMissMarker` IS — RE2 has no lookaround —
// AND IT REUSES THAT FUNCTION'S OWN WALK (`refRunThenColon`), which is exactly the
// pattern's tail. There is NO leading `\b`: the oracle's pattern has none either, so
// `REOPEN:` matches on both sides, at the `OPEN` inside it.
//
// ⚠ EXACTLY ONE NARROWING AGAINST THE ORACLE REMAINS, AND AN EARLIER VERSION OF THIS
// PARAGRAPH DECLARED ONE WHILE THREE WERE PRESENT. `_MARKER_ANYWHERE` is compiled
// `re.IGNORECASE` over the WHOLE pattern, and a differential sweep over 13,440
// generated lines found 3,620 lines the oracle matched and this did not — and NONE
// the other way — in three populations: `pr#` in the ref run (2,160), a non-ASCII
// decimal digit in a `#`/`PR#` reference (630), and U+017F (830). The first two are
// CLOSED — `refRunThenColon` now takes a `foldPR` flag and the digit run reads
// `unicode.IsDigit`; both fixes are in `refAtomEnds`, and the same sweep re-runs at
// 830 divergences, every one of them U+017F.
//
// The survivor is `hasFoldedPrefix`'S ASCII-ONLY FOLD: `re.I` on a `str` pattern also
// folds U+017F (long s) onto `s`, so a line spelling `reſolved:` matches on the oracle
// and not here. 🔴 IT HAS NO JUSTIFICATION BEYOND ITS COST, AND SAYING SO IS THE
// POINT — do not read a rationale into it. `hasFoldedPrefix` is shared with
// `markerAlternation` and `_UNMARKED_ACTION`'s transcription, so closing it means full
// Unicode simple-case-folding in three consumers whose oracles differ in whether they
// fold at all; that is a separate change with its own differential sweep. What makes
// the residue tolerable is scope, not merit: this flag may ONLY rank a dropped line's
// urgency, so a miss costs a word of emphasis and never a silent pass. CLOSING
// CONDITION: `hasFoldedPrefix` folds by `unicode.SimpleFold` and the sweep below
// reports 0 divergences across all three populations.
func LineMentionsMarker(line string) bool {
	rs := []rune(line)
	for i := range rs {
		for _, word := range []string{"open", "resolved"} {
			if !hasFoldedPrefix(rs[i:], word) {
				continue
			}
			end := i + len(word)
			// `(?![A-Za-z0-9_])` — the word-boundary guard that keeps
			// `RESOLVED_ADDR` and `OPENED` out.
			if end < len(rs) && isWordish(rs[end]) {
				continue
			}
			// `true`: the oracle's `re.IGNORECASE` covers the ref run too, unlike
			// `_NEAR_MISS_MARKER`'s scoped `(?i:…)`. See `refRunThenColon`.
			if refRunThenColon(rs, end, map[int]bool{}, true) {
				return true
			}
		}
	}
	return false
}

// LineCarriesMarker answers "does this DROPPED line carry an `OPEN:`/`RESOLVED:`
// declaration?".
//
// 🔴 IT RESTATES NO GRAMMAR. It asks `LineOpenness` — the real parser — and falls
// back to `LineMentionsMarker` for a declaration sitting mid-line.
//
// It may ONLY rank urgency. A dropped line is a finding on its own and this flag
// never gates whether one is reported, so a miss costs a word of emphasis, never a
// silent pass.
func LineCarriesMarker(line string) bool {
	openness, _ := LineOpenness(line)
	return openness != "" || LineMentionsMarker(line)
}

// UnreachableMarkers are the markers on this bullet's lines 2..n that WOULD have
// parsed at the head of a bullet.
//
// 🔴 A THIRD SHAPE, AND IT IS NOT A POPULATION. `OpennessPopulation` is untouched
// by this: that method answers "what did this bullet DECLARE", and a bullet whose
// only marker is out of reach declared nothing — which is precisely the finding.
// Folding this in would change the answer to a different question and silently move
// existing counts.
//
// 🔴 NOT SUPPRESSED WHEN THE BULLET ALREADY DECLARES ONE. The field case was
// exactly a bullet carrying two markers with the head one broken; a bullet with a
// good head marker AND a second one further down is two claims stored as one, which
// is worth saying either way.
//
// Blank lines contribute nothing, and FENCED regions are skipped for the same
// reason `ParseJournalBullets` skips them: a `- OPEN:` inside a code fence is sample
// text, and reporting it would send a writer to promote a line that is quoting
// something.
func (b JournalBullet) UnreachableMarkers() []UnreachableMarker {
	var out []UnreachableMarker
	inFence := false
	for i, line := range b.Lines {
		if i == 0 {
			continue
		}
		if IsFence(line) {
			inFence = !inFence
			continue
		}
		if inFence || pytext.StripWhitespace(line) == "" {
			continue
		}
		openness, sha := LineOpenness(line)
		if openness == "" {
			continue
		}
		out = append(out, UnreachableMarker{
			Offset:     i + 1,
			Line:       line,
			Openness:   openness,
			ResolvedBy: sha,
		})
	}
	return out
}

// truncRunes cuts `s` to at most `n` RUNES, which is what Python's `s[:n]` does on
// a `str`. Cutting bytes would split a multi-byte character and print a
// replacement glyph where the oracle prints the character.
func truncRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n])
}
