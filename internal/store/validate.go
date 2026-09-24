package store

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// The WRITE-PROTOCOL advisories: content a reader cannot reach.
//
// 🔴 THESE TWO CHECKS ARE WHY `validate` IS THE POST-WRITE CHECK AND NOT ONLY A
// PARSE CHECK. The `N of M entry file(s) parse` line answers "would the loader
// accept these files?", and a file can pass that while holding text NO reader will
// ever surface. `dropped lines:` is the half that means content is ALREADY LOST.
//
// 🔴 NEITHER MOVES THE VERDICT, AND THAT IS NOT TIMIDITY. `validate` answers one
// question and the write protocol branches on its EXIT CODE to mean "write
// NOTHING". Failing here would stop a session recording anything into an entry
// whose only defect is that an OLDER write lost a line — which makes the store
// lossier, not safer. They are reported loudly and change no code.

// UnreachableMarkerFinding is one bullet carrying a correctly-spelled marker where
// no parser looks, attributed to the file it was found in.
//
// 🔴 DELIBERATELY NOT AN OPENNESS POPULATION. `OpennessPopulation` partitions
// bullets by a reading of their OPENING line; this is a fact about lines 2..n, and
// the bullet it is about is usually `none` — a bullet that declared nothing.
type UnreachableMarkerFinding struct {
	Filename string

	// BulletFirstLine is the bullet's OPENING line — what a reader will search
	// the file for.
	BulletFirstLine string

	// Offset is the 1-based line index WITHIN the bullet. Always >= 2.
	Offset int

	// Line is the continuation line carrying the marker, verbatim.
	Line string

	// Openness is `open` or `resolved` — what it would have declared at a
	// bullet's head.
	Openness string
}

// DroppedLineFinding is one nuance line that belongs to no parsed bullet.
//
// 🔴 MEASURED IN THE FIELD, NOT HYPOTHETICAL. Over every committed version of every
// entry file in a live store of several hundred entries, seven versions carried
// dropped lines — two entries whose whole bullet block had been indented, each
// broken for days. One of them held a `OPEN:` that raised no badge for the whole of
// that window, and would still raise none, because every marker reader anchors at
// position 0; see `LineCarriesMarker`.
type DroppedLineFinding struct {
	Filename string

	// Offset is the 1-based line index within the nuance section BODY.
	Offset int

	// Line is the dropped line, verbatim.
	Line string

	// CarriesMarker records that the line looks like it declared
	// `OPEN:`/`RESOLVED:`. It changes the URGENCY and nothing else: a dropped
	// line is lost content either way, but a dropped DECLARATION is an open
	// action the store is actively failing to report. It is never counted as an
	// open action — the bullet it belonged to no longer exists, so there is
	// nothing to declare it ON.
	CarriesMarker bool
}

// nuanceBody is the nuance-section body of one entry file, and whether it has one.
//
// Deliberately tolerant: a file that cannot be read, or has no nuance section,
// yields false rather than an error. Both scanners run BESIDE the parse check,
// never in front of it — a malformed file's own rejection is the finding that
// matters, and an advisory computed from its half-parsed body would bury it.
func nuanceBody(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	body := ExtractSections(DecodeReplace(data), []string{NuanceHeading})[NuanceHeading]
	if body == "" {
		return "", false
	}
	return body, true
}

// ScanUnreachableMarkers finds markers typed into a bullet's BODY, where the marker
// parser never reads. READ-ONLY.
//
// 🔴 IT IS A SEPARATE WALK FROM ANY OPEN-ACTION SCAN BY CHOICE. Merging them would
// mean deciding what to do with a bullet that declared nothing on its opening line,
// and the answer there is "report it, but never as an open action" — a second
// population inside a function whose whole contract is a one-branch precedence.
func ScanUnreachableMarkers(paths []string) []UnreachableMarkerFinding {
	var out []UnreachableMarkerFinding
	for _, p := range paths {
		body, ok := nuanceBody(p)
		if !ok {
			continue
		}
		name := filepath.Base(p)
		for _, b := range ParseJournalBullets(body) {
			for _, m := range b.UnreachableMarkers() {
				out = append(out, UnreachableMarkerFinding{
					Filename:        name,
					BulletFirstLine: b.FirstLine(),
					Offset:          m.Offset,
					Line:            m.Line,
					Openness:        m.Openness,
				})
			}
		}
	}
	return out
}

// ScanDroppedLines finds nuance lines present on disk that reach no bullet.
// READ-ONLY.
//
// 🔴 IT ASKS THE READER'S OWN PARSER AND DERIVES NOTHING ITSELF. The set of
// reachable lines is taken from `ParseJournalBullets`' output rather than re-deduced
// from a bullet pattern here, so this check cannot drift away from what consumers
// actually see. Re-deriving it would be a duplicated predicate with the checker's
// worst failure mode: blessing lines the reader drops.
//
// 🔴 WHAT IT STRUCTURALLY CANNOT SEE, stated because the gap is the whole reason to
// read this twice. A bullet that loses its opening line while ANOTHER bullet sits
// above it is not detectable — `ParseJournalBullets` appends every non-bullet line
// to the bullet above, so the orphaned tail is absorbed into it and inherits its
// date, and the resulting file is BYTE-IDENTICAL to one where that bullet
// legitimately wrapped. No check can separate the two, and this one does not pretend
// to: it covers the case where the drop is decidable — text before the first bullet,
// which includes every entry whose NEWEST bullet lost its head, the store being
// newest-first.
func ScanDroppedLines(paths []string) []DroppedLineFinding {
	var out []DroppedLineFinding
	for _, p := range paths {
		body, ok := nuanceBody(p)
		if !ok {
			continue
		}
		name := filepath.Base(p)
		lines := pytext.SplitLines(body)
		// 🔴 REACHABILITY IS KEYED ON (OFFSET, LINE), NOT ON THE LINE ALONE. A
		// set of strings masks an orphan whose text is byte-identical to any
		// line inside any bullet of the same file — and a read-modify-write
		// race, which is the shape that produces a decapitated bullet in the
		// first place, is exactly what duplicates a block.
		type at struct {
			offset int
			line   string
		}
		reachable := map[at]bool{}
		cursor := 0
		for _, b := range ParseJournalBullets(body) {
			for _, ln := range b.Lines {
				// `ParseJournalBullets` preserves order and never reorders or
				// rewrites a line, so a forward scan re-attaches each bullet
				// line to its own offset. Trailing blanks it stripped are
				// simply not looked for; the skip below treats them as
				// reachable anyway.
				for cursor < len(lines) && lines[cursor] != ln {
					cursor++
				}
				if cursor < len(lines) {
					reachable[at{cursor + 1, ln}] = true
					cursor++
				}
			}
		}
		inFence := false
		for i, line := range lines {
			offset := i + 1
			// 🔴 FENCES ARE SKIPPED, as in the sibling scanner and in
			// `ParseJournalBullets` itself. A fenced snippet BEFORE the first
			// bullet is sample text, and reporting it would hand the operator
			// the remedy "restore the bullet opening line" for content that
			// never had one.
			if IsFence(line) {
				inFence = !inFence
				continue
			}
			if inFence || pytext.StripWhitespace(line) == "" ||
				reachable[at{offset, line}] {
				continue
			}
			out = append(out, DroppedLineFinding{
				Filename:      name,
				Offset:        offset,
				Line:          line,
				CarriesMarker: LineCarriesMarker(line),
			})
		}
	}
	return out
}

// AdvisoryQuoteMax is the longest a quoted line runs before it is cut. A finding
// names a FILE and a LINE NUMBER; the quote is there to recognise it by, and an
// entry may hold a 4,000-character bullet.
const AdvisoryQuoteMax = 120

// ValidationAdvisoryLines renders the two write-protocol advisory blocks, as lines.
// UNPREFIXED.
//
// Each client prefixes every line with its own `cairn: <scope>: `, because
// `validate` with no `--scope` walks every scope the cache holds and an unprefixed
// block would not say which one it is about.
//
// 🔴 THE DROPPED-LINE BLOCK COMES FIRST, deliberately. A dropped line is content NO
// reader reaches, so the marker scan never sees it — a `0 out-of-reach` printed
// above a `🔴 N DROPPED LINE(S)` is a fact about text the parser never got to, and
// reads as a reassurance it cannot support.
//
// 🔴 EVERY BLOCK PRINTS ITS DENOMINATOR EVEN WHEN IT FINDS NOTHING. A bare zero is
// indistinguishable from a scanner wired to nothing, and each of these has a SECOND
// way to be vacuous that the zero must not hide: both read only the nuance heading,
// so an entry whose heading is renamed contributes zero to both for a reason neither
// block can state.
//
// 🔴 AND WHEN NOTHING WAS CHECKED THE BLOCKS DO NOT PRINT AT ALL — one `NOT CHECKED`
// line prints instead. "0 across 0 entry file(s)" is the reassuring zero from an
// instrument that walked nothing, and it must not render anywhere near a
// clean-looking count.
//
// 🔴 THIS IS ONE HALF OF A PAIR KEPT BYTE-IDENTICAL BY `tests/parity/`. The oracle's
// spelling is `entry_shape.validation_advisory_lines`. Changing either alone IS a
// divergence, and the parity world seeds a scope carrying both a dropped line and an
// out-of-reach marker precisely so a one-sided edit is RED rather than invisible.
func ValidationAdvisoryLines(
	nFiles int,
	dropped []DroppedLineFinding,
	unreachable []UnreachableMarkerFinding,
) []string {
	if nFiles == 0 {
		return []string{fmt.Sprintf(
			"dropped lines / marker reachability: NOT CHECKED — 0 entry file(s) "+
				"to read, so a zero here would be a zero over nothing. [%s] [%s]",
			ReasonDroppedLine, ReasonUnreachableMarker)}
	}
	out := droppedLinesBlock(nFiles, dropped)
	return append(out, reachabilityBlock(nFiles, unreachable)...)
}

// droppedLinesBlock is the DROPPED-LINE advisory — the half that means content is
// ALREADY LOST.
//
// 🔴 THE ZERO CARRIES ITS OWN BLIND SPOT IN WORDS. This check is knowingly PARTIAL —
// the absorbed-tail case is undecidable, see `ScanDroppedLines` — so a bare "0
// dropped" would read as "no bullet has lost its head", which is a claim it cannot
// make. Saying which half was checked is the difference between a measurement and a
// reassurance.
func droppedLinesBlock(nFiles int, dropped []DroppedLineFinding) []string {
	if len(dropped) == 0 {
		return []string{fmt.Sprintf(
			"dropped lines: 0 across %d entry file(s) [%s] — every non-blank "+
				"`%s` line reaches a bullet some reader will surface. 🔴 PARTIAL "+
				"BY CONSTRUCTION: this sees text before the FIRST bullet. A bullet "+
				"that lost its opening line while another bullet sat above it is "+
				"absorbed into that one and is byte-identical to a legitimate wrap "+
				"— no check can see it, and this zero is not a claim about that "+
				"case.",
			nFiles, ReasonDroppedLine, NuanceHeading)}
	}
	marked := 0
	for _, d := range dropped {
		if d.CarriesMarker {
			marked++
		}
	}
	out := []string{fmt.Sprintf(
		"🔴 %d DROPPED LINE(S) across %d entry file(s) [%s] — present in the file, "+
			"inside NO bullet, so EVERY reader skips them: `--ref`, `--search`, the "+
			"digest and every openness count. `parse_journal_bullets` drops text "+
			"that precedes the first bullet. The cause is almost always a lost or "+
			"indented bullet OPENING line; the fix is to restore it, NOT to delete "+
			"the text.",
		len(dropped), nFiles, ReasonDroppedLine)}
	if marked > 0 {
		plural := ""
		if marked == 1 {
			plural = "s"
		}
		out = append(out, fmt.Sprintf(
			"  🔴 %d of them look%s like a `OPEN:`/`RESOLVED:` DECLARATION. That is "+
				"an open action the store is actively failing to report — it raises "+
				"no badge and is counted in no openness total, because the bullet "+
				"that would carry it no longer exists.",
			marked, plural))
	}
	for _, d := range dropped {
		flag := ""
		if d.CarriesMarker {
			flag = "  ← looks like a DECLARATION"
		}
		out = append(out,
			fmt.Sprintf("    %s: nuance line %d%s", d.Filename, d.Offset, flag),
			"      "+truncRunes(pytext.StripWhitespace(d.Line), AdvisoryQuoteMax))
	}
	out = append(out,
		"  🔴 RESTORE FROM HISTORY, NOT FROM MEMORY. The entry's previous version "+
			"is one `git log -p -- <file>` away in the store's own history. "+
			"Reconstructing the opening line by hand invents a date and an author "+
			"the store never had.",
		advisoryFooter)
	return out
}

// reachabilityBlock is the MARKER-REACHABILITY advisory.
//
// 🔴 ITS OWN BLOCK, BECAUSE IT IS ITS OWN SHAPE. Every remedy a near-miss advisory
// names — "fix the LINE", "rewrite as `RESOLVED <sha>:`" — is wrong here. A marker
// on a continuation line is spelled correctly; the edit it needs is to be PROMOTED
// to a bullet of its own.
func reachabilityBlock(nFiles int, unreachable []UnreachableMarkerFinding) []string {
	if len(unreachable) == 0 {
		return []string{"", fmt.Sprintf(
			"marker reachability: 0 out-of-reach marker(s) across %d entry file(s) "+
				"[%s] — every `OPEN:`/`RESOLVED:` found is on a bullet's OPENING "+
				"line, where the parser reads.",
			nFiles, ReasonUnreachableMarker)}
	}
	out := []string{"", fmt.Sprintf(
		"🔴 %d MARKER(S) OUT OF REACH across %d entry file(s) [%s] — spelled "+
			"CORRECTLY, on a bullet's CONTINUATION line, where NO reader looks. The "+
			"marker pattern is anchored at position 0 of a bullet's OPENING line, so "+
			"this declares NOTHING: it raises neither the `OPEN` badge nor "+
			"`NEAR-MISS`. 🔴 It is NOT a near-miss and is NOT counted as one — a "+
			"near-miss is mis-spelled where the parser looks and is fixed by editing "+
			"the line; this is fixed by PROMOTING the line to a top-level bullet of "+
			"its own.",
		len(unreachable), nFiles, ReasonUnreachableMarker)}
	for _, u := range unreachable {
		out = append(out,
			fmt.Sprintf("    %s: line %d of the bullet opening", u.Filename, u.Offset),
			"      bullet: "+truncRunes(u.BulletFirstLine, AdvisoryQuoteMax),
			fmt.Sprintf("      marker: %s   (would declare `%s`)",
				truncRunes(pytext.StripWhitespace(u.Line), AdvisoryQuoteMax),
				u.Openness))
	}
	out = append(out,
		"  🔴 BEFORE FIXING ANY MARKER ABOVE IT IN THE SAME SECTION, re-check this "+
			"one against the store's history. Such a bullet can have raised its "+
			"badge only BY ACCIDENT, through a broken `RESOLVED —` sitting above it "+
			"— so repairing that line would SILENCE a still-open action.",
		advisoryFooter)
	return out
}

// advisoryFooter is the sentence BOTH blocks end on, written once because it is one
// claim: these findings change no verdict, and the reason is the same for both.
const advisoryFooter = "  (Advisory. It changes no verdict: the loader accepts the " +
	"file, and the write protocol branches on this command's exit code to mean " +
	"'write NOTHING'.)"
