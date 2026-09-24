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
// 🔴 THE PATH'S KIND IS DECIDED BEFORE IT IS OPENED, THROUGH THE LOADER'S OWN
// TABLE. An `err != nil` check is NOT tolerance for a path that is not a regular
// file, and measuring it was the point: reading a FIFO returns no error — it BLOCKS
// until somebody writes, and `validate` never returns. A character device does not
// error either; `os.ReadFile` grows its buffer until the runtime dies of
// `fatal error: out of memory`, which no `err` can catch. Measured on this tree at
// the CLI, both clients, against a cache holding one such path: the FIFO wedged
// until a `timeout 20` killed it (124), and a symlink to `/dev/zero` under an
// address-space limit exited 2 here and 1 on the oracle — in each case abandoning
// every scope after the bad one, where the same tree one commit earlier reported the
// file as malformed and exited 5.
//
// `LoadIndex` never had that exposure: it refuses `other`, `link-to-other`,
// `broken-link`, `directory` and `link-to-dir` before `open()`, for exactly this
// reason (a fifo measured wedging a request thread for 25s). This gate is that same
// table — `loaderEntryActions`, not a fresh predicate — so the advisories read
// precisely the paths the loader reads and no others.
//
// ⚠ THE RESIDUAL IS THE LOADER'S RESIDUAL, AND IT IS NOT EMPTY. The kinds the table
// TAKES are `regular-file`, `link-to-file`, `indeterminate` and `absent`; on the last
// three the read can still fail, and `err != nil` is what turns that into
// "contributes nothing". What it does NOT cover is a REGULAR file whose read is
// unbounded — a `/proc` file reached through a symlink, say. That is `LoadIndex`'s
// own RESIDUAL LEDGER, unchanged here and not widened: this function is now exactly
// as exposed as the reader beside it, which is the property worth having. It is not a
// claim that nothing can fail.
//
// Deliberately tolerant otherwise: a file with no nuance section yields false. Both
// scanners run BESIDE the parse check, never in front of it — a malformed file's own
// rejection is the finding that matters, and an advisory computed from its
// half-parsed body would bury it.
//
// ⚠ EACH ENTRY IS READ THREE TIMES PER `validate` — once by `LoadIndex` and once by
// each scanner — AND THAT IS A DECISION, NOT AN OVERSIGHT. Measured on this tree over
// a synthetic cache of 300 entries carrying 30 bullets apiece: 36 ms end to end here
// and 118 ms on the oracle, process start included. Caching the body would put mutable
// state into two functions whose whole contract is READ-ONLY and independent, to save a
// fraction of a tenth of a second on a store an order of magnitude larger than any real
// one. The re-read also has one honest property a cache would remove: each scanner sees
// the file as it is when IT runs, so a body that changed mid-command cannot be reported
// under offsets taken from an earlier read.
func nuanceBody(path string) (string, bool) {
	// `ActionFor` rather than a map index, so the KIND comes from the loader's own
	// table and never from a predicate spelled here.
	//
	// 🔴 THE ERROR IS SWALLOWED, AND AN EARLIER FORM OF THIS COMMENT CLAIMED THE
	// OPPOSITE — it said an unmapped kind was "the same BUG it is in the loader
	// rather than a silent zero value". It is not. `LoadIndex` RETURNS `ActionFor`'s
	// error (see `load.go`); the line below folds it into `("", false)`, which is
	// byte-for-byte what a bare map index would have produced, because a zero-value
	// `Action` is not `Take` either. What `ActionFor` buys here is the SHARED TABLE,
	// not the raised error.
	//
	// ⚠ AND THE TWO CLIENTS DIVERGE ON THAT BRANCH. `lib/entry_shape.py`'s
	// `_nuance_body` calls `action_for(...) != TAKE`, and `action_for` raises
	// `AssertionError` on an unmapped kind with nothing catching it — so the oracle
	// ABORTS where this returns "no nuance section". UNREACHABLE TODAY in both:
	// `TestTheActionTablesAreTotal` here and `TestClassifierIsTotal` on the Python
	// side pin the kind set against the loader table two-way, so no unmapped kind can
	// exist to reach either branch. Recorded rather than closed — closing it means
	// giving this function an error return that both call sites would discard, which
	// is the same swallow one frame further out.
	//
	// ⚠ SIBLING ASYMMETRY ON THE SAME TABLE, for whoever adds a third action:
	// `LoadIndex` branches `action == Refuse`, both scanners branch `action != Take`.
	// Identical today only because the table holds no row that is neither — a `Skip`
	// would be READ by the loader and SKIPPED here.
	if !scannerReadsPath(path) {
		return "", false
	}
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
// 🔴 WHAT IT STRUCTURALLY CANNOT SEE — THREE CASES, NOT ONE, and an earlier version
// of this paragraph named only the first. The printed zero names all three
// (`droppedLinesBlock`), because a caveat the operator never reads is not a caveat.
// Each is pinned by an invariant guard in `validate_test.go` and its oracle twin.
//
//  1. ABSORBED TAIL. A bullet that loses its opening line while ANOTHER bullet sits
//     above it is not detectable — `ParseJournalBullets` appends every non-bullet
//     line to the bullet above, so the orphaned tail is absorbed into it and inherits
//     its date, and the file is BYTE-IDENTICAL to one where that bullet legitimately
//     wrapped. No check can separate the two.
//  2. UNCLOSED FENCE. `IsFence` toggles, so an odd count leaves everything after the
//     last fence marked fenced — and fenced lines are deliberately skipped as sample
//     text (see below). A bullet swallowed that way yields NO dropped-line finding
//     and NO bullet, so its `OPEN:` is reported by nothing at all. This is the one
//     case where the zero is actively misleading rather than merely partial.
//  3. DUPLICATED nuance HEADING. `ExtractSections` concatenates same-named sections
//     into one body, so an orphan under the SECOND heading is absorbed by a bullet
//     from the FIRST, and the offsets reported here index the concatenation rather
//     than the file.
//
// What it DOES cover is the case where the drop is decidable — text before the first
// bullet, which includes every entry whose NEWEST bullet lost its head, the store
// being newest-first.
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
	nScanned int,
	dropped []DroppedLineFinding,
	unreachable []UnreachableMarkerFinding,
) []string {
	if nScanned == 0 {
		return []string{fmt.Sprintf(
			"dropped lines / marker reachability: NOT CHECKED — 0 entry file(s) "+
				"scanned, so a zero here would be a zero over nothing. [%s] [%s]",
			ReasonDroppedLine, ReasonUnreachableMarker)}
	}
	out := droppedLinesBlock(nScanned, dropped)
	return append(out, reachabilityBlock(nScanned, unreachable)...)
}

// scannerReadsPath is the ONE predicate deciding whether the two advisory scanners
// OPEN a path, and therefore the one that decides the denominator they print.
//
// 🔴 IT IS THE DENOMINATOR *AND* THE GATE, SPELLED ONCE, BECAUSE AS TWO THINGS IT WAS
// WRONG. The advisories used to be handed `len(EntryFileNames(...))` — an unfiltered
// listing — while the scanners read only the kinds the loader TAKES. MEASURED on a
// scope holding one real entry beside a FIFO: `dropped lines: 0 across 2 entry
// file(s) — every non-blank … line reaches a bullet some reader will surface`, over a
// file nothing had opened. That is the reassuring zero the `NOT CHECKED` branch above
// exists to prevent, arriving through the numerator's own count instead of through an
// empty directory.
func scannerReadsPath(path string) bool {
	action, err := ActionFor(ClassifyPath(path), loaderEntryActions)
	return err == nil && action == Take
}

// ScannedEntryCount is how many of `paths` the advisory scanners will actually open —
// the denominator `ValidationAdvisoryLines` must be given.
//
// ⚠ IT IS NOT `len(paths)`, AND THE GAP IS THE POINT. A FIFO, a dangling symlink, a
// directory or a device in a scope directory is listed as an entry file and REFUSED
// before `open()`, so it appears in the parse line above the advisories (as malformed)
// and must not appear in their denominator.
func ScannedEntryCount(paths []string) int {
	n := 0
	for _, path := range paths {
		if scannerReadsPath(path) {
			n++
		}
	}
	return n
}

// droppedLinesBlock is the DROPPED-LINE advisory — the half that means content is
// ALREADY LOST.
//
// 🔴 THE ZERO CARRIES ITS OWN BLIND SPOT IN WORDS. This check is knowingly PARTIAL —
// the absorbed-tail case is undecidable, see `ScanDroppedLines` — so a bare "0
// dropped" would read as "no bullet has lost its head", which is a claim it cannot
// make. Saying which half was checked is the difference between a measurement and a
// reassurance.
func droppedLinesBlock(nScanned int, dropped []DroppedLineFinding) []string {
	if len(dropped) == 0 {
		return []string{fmt.Sprintf(
			"dropped lines: 0 across %d entry file(s) scanned [%s] — every non-blank "+
				"`%s` line reaches a bullet some reader will surface. 🔴 PARTIAL "+
				"BY CONSTRUCTION, IN THREE WAYS, and this zero is a claim about "+
				"none of them: (1) ABSORBED TAIL — a bullet that lost its opening "+
				"line while another bullet sat above it is absorbed into that one "+
				"and is byte-identical to a legitimate wrap, so no check can "+
				"separate them; (2) UNCLOSED FENCE — an odd number of ``` or ~~~ "+
				"makes every line after it fenced, and fenced lines are skipped as "+
				"sample text, so a whole bullet can be swallowed with its `OPEN:` "+
				"and reported nowhere; (3) DUPLICATED `%s` HEADING — the sections "+
				"are concatenated into one body, so text under the second is "+
				"absorbed by a bullet from the first and any offset printed here "+
				"would index the concatenation rather than the file.",
			nScanned, ReasonDroppedLine, NuanceHeading, NuanceHeading)}
	}
	marked := 0
	for _, d := range dropped {
		if d.CarriesMarker {
			marked++
		}
	}
	out := []string{fmt.Sprintf(
		"🔴 %d DROPPED LINE(S) across %d entry file(s) scanned [%s] — present in "+
			"the file, inside NO bullet, so EVERY reader skips them: `--ref`, "+
			"`--search`, the "+
			"digest and every openness count. `parse_journal_bullets` drops text "+
			"that precedes the first bullet. The cause is almost always a lost or "+
			"indented bullet OPENING line; the fix is to restore it, NOT to delete "+
			"the text.",
		len(dropped), nScanned, ReasonDroppedLine)}
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
func reachabilityBlock(nScanned int, unreachable []UnreachableMarkerFinding) []string {
	if len(unreachable) == 0 {
		return []string{"", fmt.Sprintf(
			"marker reachability: 0 out-of-reach marker(s) across %d entry "+
				"file(s) scanned [%s] — every `OPEN:`/`RESOLVED:` found is on a "+
				"bullet's OPENING line, where the parser reads.",
			nScanned, ReasonUnreachableMarker)}
	}
	out := []string{"", fmt.Sprintf(
		"🔴 %d MARKER(S) OUT OF REACH across %d entry file(s) scanned [%s] — "+
			"spelled CORRECTLY, on a bullet's CONTINUATION line, where NO reader "+
			"looks. The "+
			"marker pattern is anchored at position 0 of a bullet's OPENING line, so "+
			"this declares NOTHING: it raises neither the `OPEN` badge nor "+
			"`NEAR-MISS`. 🔴 It is NOT a near-miss and is NOT counted as one — a "+
			"near-miss is mis-spelled where the parser looks and is fixed by editing "+
			"the line; this is fixed by PROMOTING the line to a top-level bullet of "+
			"its own.",
		len(unreachable), nScanned, ReasonUnreachableMarker)}
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
