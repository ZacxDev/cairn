package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	text, ok := entryText(path)
	if !ok {
		return "", false
	}
	body := ExtractSections(text, []string{NuanceHeading})[NuanceHeading]
	if body == "" {
		return "", false
	}
	return body, true
}

// entryText is the WHOLE entry file, and whether any scanner may open it at all.
//
// 🔴 THE GATE AND THE READ, SPELLED ONCE FOR EVERY SCANNER. `nuanceBody` carried both
// inline until a scanner appeared that needs the whole file rather than one section
// (`ScanEntryShape`, which has to see headings the nuance body cannot contain). A
// second copy of the gate is the shape this file refuses everywhere else: the FIFO and
// character-device hazards `nuanceBody` documents above are properties of the OPEN, not
// of the section extraction, so a scanner that reads the file by a different route
// inherits none of the protection. Every measurement in that comment applies verbatim
// to every caller of this function.
func entryText(path string) (string, bool) {
	if !scannerReadsPath(path) {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return DecodeReplace(data), true
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

// The entry's SHAPE, as opposed to whether its front matter parses.
//
// 🔴 WHY A SHAPE CHECK EXISTS AT ALL. `validate` returned `OK` at exit 0 for an entry
// with all three spine headings renamed, for one with no headings at all, and for one
// whose sections were all empty. Only a missing `service:` went red. So the spine every
// consumer depends on was enforced by NOTHING.
//
// 🔴 AND IT IS NOT COSMETIC. A reader computes an entry's bullet count and its
// `🔴 N OPEN` badge from `ExtractSections(...)[NuanceHeading]`, so a heading that is
// renamed — or given a trailing colon, or shifted off column 0 — yields an empty body,
// and the index row renders an entry with genuine open actions as a well-formed empty
// one. The reader names a missing heading ON READ; this catches the same thing ON
// WRITE, in the turn that wrote it.
//
// 🔴 WHICH HEADINGS, AND WHY STILL NOT `WhatHeading`. The checked set is
// `ShapeHeadings` — the set whose absence makes a NUMBER wrong. `WhatHeading` IS read
// and surfaced in every printed body, but it feeds no bullet count and no index badge,
// so a missing one cannot turn a parse failure into a well-formed empty entry the way a
// missing nuance heading can. Flagging it would report a convention with no numeric
// consequence beside two whose consequence is measured, and a writer cannot tell those
// apart in a list.

// ShapeHeadings is the set of headings whose absence makes a COUNT or a BADGE wrong on
// the read path. The oracle's spelling is `entry_shape.SHAPE_HEADINGS`, and it is the
// same pair in the same order — the report interpolates it, so the ORDER is part of the
// printed contract the parity gate compares.
//
// ⚠ A `var`, NOT A `const`, ONLY BECAUSE GO HAS NO SLICE CONSTANTS. It is never
// written; the oracle's is a tuple for exactly this reason.
var ShapeHeadings = []string{PointersHeading, NuanceHeading}

// The four DISJOINT kinds of shape finding. Never summed — see ShapeFinding.
const (
	ShapeAbsent     = "absent"
	ShapeRenamed    = "renamed"
	ShapeDuplicated = "duplicated"
	ShapeEmpty      = "empty"
)

// ShapeInventoryShown caps the heading inventory printed beside an ABSENT finding. A
// bound, not a filter — the remainder is always counted in the line.
const ShapeInventoryShown = 6

// headingKey is the LOOSE form, used ONLY to pair a heading with the schema one it
// missed. The oracle's spelling is `entry_shape._heading_key`.
//
// 🔴 IT NEVER ACCEPTS A HEADING. `ExtractSections` matches the exact string and keeps
// doing so; folding `## Pointers` and `## pointers` together there would quietly widen
// what the store is allowed to look like, which is the opposite of what this check is
// for. This exists so the report can say "you wrote `## pointers`" instead of "the
// section is absent" — the difference between a finding a writer can act on in one edit
// and one that sends them looking for prose that is already on disk.
//
// Folds exactly three near-misses: the `#` level, the case and surrounding whitespace,
// and a trailing colon. Whitespace RUNS collapse to one space, mirroring the oracle's
// `re.sub(r"\s+", " ", …)`, so `##  Nuance  /  work-history` pairs rather than reading
// as a heading nothing can match.
func headingKey(heading string) string {
	s := strings.TrimLeft(heading, "#")
	s = pytext.StripWhitespace(s)
	s = strings.TrimRight(s, ":")
	s = pytext.StripWhitespace(s)
	return strings.ToLower(collapseWhitespace(s))
}

// collapseWhitespace is `re.sub(r"\s+", " ", s)` over the oracle's `\s` class.
//
// ⚠ IT IS `pytext.IsSpace`, NOT `unicode.IsSpace`, because the oracle's `\s` on a `str`
// pattern is Unicode-aware and this comparison must agree with it — a heading separated
// by a NO-BREAK SPACE must fold the same way on both clients or the parity gate reports
// a divergence in a report nobody can read.
func collapseWhitespace(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if pytext.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return b.String()
}

// ShapeFinding is one way an entry's spine departs from the schema. FOUR DISJOINT
// KINDS.
//
// 🔴 THEY ARE NEVER SUMMED, for the same reason OpenAction's populations are not: they
// are different facts with different remedies. `renamed` and `absent` are the same
// missing section reported at different resolutions — the writer typed something we can
// show them, or they did not — and collapsing them would throw away the only half that
// is actionable. `duplicated` is a section that IS parsed, twice, silently merged.
// `empty` is a section present and unfilled, which ExtractSections tracks separately
// from absent precisely so this can.
//
// 🔴 `duplicated` AND `empty` ARE NOT MUTUALLY EXCLUSIVE, and neither branch in
// ScanEntryShape excludes the other: a heading written twice whose merged body is still
// blank is BOTH, and reporting one of the two would hand the writer half a remedy.
type ShapeFinding struct {
	Filename string

	// Heading is the SCHEMA heading this finding is about, always — never the typo.
	Heading string

	Kind string

	// Found is, for `renamed`, the near-miss heading(s) actually written; for
	// `absent`, the file's whole heading inventory, so the writer sees what they
	// wrote instead.
	Found []string

	// Count is, for `duplicated`, how many times the exact heading appears.
	Count int
}

// ScanEntryShape reports each entry's spine against ShapeHeadings. READ-ONLY.
//
// Tolerant in exactly the way the sibling scanners are, and gated the same way: a path
// the loader's own table refuses is never opened (`entryText`), and a file that cannot
// be read contributes nothing rather than failing, because this runs BESIDE the parse
// check and never in front of it — a malformed file's own rejection is the finding that
// matters.
//
// 🔴 IT REUSES `ExtractSections` AND `HeadingInventory` AND PARSES NOTHING ITSELF. Both
// are views over the one `HeadingBlocks` walker, so this cannot come to a different
// conclusion about what a heading is than the reader does — which would be the worst
// possible defect in a checker whose entire job is to predict what the reader will see.
func ScanEntryShape(paths []string) []ShapeFinding {
	var out []ShapeFinding
	for _, p := range paths {
		text, ok := entryText(p)
		if !ok {
			continue
		}
		name := filepath.Base(p)
		present := ExtractSections(text, ShapeHeadings)
		headings := HeadingInventory(text)
		for _, h := range ShapeHeadings {
			body, isPresent := present[h]
			if !isPresent {
				var near []string
				for _, x := range headings {
					if headingKey(x) == headingKey(h) {
						near = append(near, x)
					}
				}
				kind := ShapeAbsent
				found := headings
				if len(near) > 0 {
					kind = ShapeRenamed
					found = near
				}
				out = append(out, ShapeFinding{
					Filename: name, Heading: h, Kind: kind, Found: found,
				})
				continue
			}
			// 🔴 BOTH REMAINING KINDS CAN BE TRUE OF ONE HEADING AT ONCE — a
			// duplicated heading whose merged body is still empty — so neither
			// branch excludes the other and neither is an `else`.
			n := 0
			for _, x := range headings {
				if x == h {
					n++
				}
			}
			if n > 1 {
				out = append(out, ShapeFinding{
					Filename: name, Heading: h, Kind: ShapeDuplicated, Count: n,
				})
			}
			if pytext.StripWhitespace(body) == "" {
				out = append(out, ShapeFinding{
					Filename: name, Heading: h, Kind: ShapeEmpty,
				})
			}
		}
	}
	return out
}

// OpenAction is one bullet `validate` is reporting as unfinished business.
//
// 🔴 FOUR POPULATIONS, AND THEY MUST NEVER BE ADDED TOGETHER INTO ONE NUMBER. A `OPEN:`
// bullet is a claim the WRITER made and is exact, while an unmarked one is this tool's
// guess from two measured phrasings and has unknown recall. Merging them would let a
// floor masquerade as a count.
type OpenAction struct {
	Filename string

	// Declared records that the writer typed `OPEN:`. Exact — not a guess about the
	// prose.
	Declared bool

	Date      string
	FirstLine string

	// NearMiss records that the bullet tried to declare a marker and missed the
	// grammar. A THIRD population, never folded into either other one: it is not an
	// open action and not a guess about one, it is a write that did not land.
	NearMiss bool

	// UnverifiableClosure records a `RESOLVED:` that names no sha, so its claim
	// cannot be checked. A FOURTH population. Not a problem — closing an action is
	// the outcome this whole design wants — but the sha is what separates "closed,
	// and here is the commit" from an assertion.
	UnverifiableClosure bool
}

// ScanOpenActions reads each entry's journal and collects its unfinished business.
// READ-ONLY.
//
// Deliberately tolerant, exactly as the sibling scanners are: a file no scanner may
// open, one that cannot be read, or one with no nuance section contributes nothing.
// This runs BESIDE the parse check, never in front of it.
func ScanOpenActions(paths []string) []OpenAction {
	var out []OpenAction
	for _, p := range paths {
		body, ok := nuanceBody(p)
		if !ok {
			continue
		}
		name := filepath.Base(p)
		for _, b := range ParseJournalBullets(body) {
			// 🔴 ONE BRANCH, ON THE SINGLE PRECEDENCE SOURCE. Re-deriving
			// membership from the individual predicates here is what let a bullet
			// be both a near-miss and an unmarked action on one surface while
			// being one thing on another — the duplicated predicate
			// `OpennessPopulation` exists to remove.
			pop := b.OpennessPopulation()
			if pop == PopulationNone || pop == PopulationResolved {
				continue
			}
			out = append(out, OpenAction{
				Filename:            name,
				Declared:            pop == PopulationOpen,
				NearMiss:            pop == PopulationNearMiss,
				UnverifiableClosure: pop == PopulationUnverifiable,
				Date:                b.Date,
				FirstLine:           b.FirstLine(),
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
// divergence, and the parity world seeds a scope carrying a dropped line, an
// out-of-reach marker, a renamed heading, a duplicated-and-empty heading and every
// open-action population precisely so a one-sided edit is RED rather than invisible.
//
// 🔴 EVERY FINDING SET IS A REQUIRED PARAMETER, NOT A VARIADIC OR AN OPTION STRUCT, and
// that is the point of the signature. A caller that never wired a scanner would
// otherwise print a confident `entry shape: … each present exactly once` over files
// nothing examined — the reassuring zero from an instrument wired to nothing, arriving
// through a forgotten argument instead of an empty directory. Required, a missed call
// site is a compile error here and a `TypeError` on the oracle.
//
// 🔴 THE ORDER IS LOAD-BEARING, IN BOTH ADJACENT PAIRS:
//
//   - `entry shape:` COMES FIRST OF ALL. A renamed nuance heading makes every one of
//     the three blocks below read an empty section, so their zeros are facts about a
//     section the parser never reached. Read in the other order, `open actions: 0
//     declared` is simply false.
//   - `dropped lines:` COMES BEFORE `open actions` AND `marker reachability:`. A
//     dropped line is content NO reader reaches, so neither of those scans ever sees
//     it — a zero printed above a `🔴 N DROPPED LINE(S)` reads as a reassurance it
//     cannot support.
func ValidationAdvisoryLines(
	nScanned int,
	shape []ShapeFinding,
	dropped []DroppedLineFinding,
	openActions []OpenAction,
	unreachable []UnreachableMarkerFinding,
) []string {
	if nScanned == 0 {
		return []string{fmt.Sprintf(
			"write-protocol advisories: NOT CHECKED — 0 entry file(s) scanned, so a "+
				"zero here would be a zero over nothing. This withholds ALL FOUR "+
				"blocks — entry shape, dropped lines, open actions and marker "+
				"reachability. [%s] [%s]",
			ReasonDroppedLine, ReasonUnreachableMarker)}
	}
	out := entryShapeBlock(nScanned, shape)
	out = append(out, droppedLinesBlock(nScanned, dropped)...)
	out = append(out, openActionsBlock(nScanned, openActions)...)
	return append(out, reachabilityBlock(nScanned, unreachable)...)
}

// entryShapeBlock is the SHAPE advisory. Prints on every path that SCANNED something.
//
// 🔴 IT PRINTS ITS DENOMINATOR EVEN WHEN IT FINDS NOTHING, AND IT PRINTS WHICH HEADINGS
// IT LOOKED FOR. A reassuring zero is indistinguishable from an instrument wired to
// nothing unless it carries the size of what it looked at — and here it must also carry
// the SET it looked at, because a reader who assumes the third spine heading was checked
// would take this zero as a claim about a heading nothing examined.
//
// 🔴 THE FOUR KINDS ARE RENDERED SEPARATELY AND NEVER SUMMED — see ShapeFinding. Each
// KIND names its own remedy, and `renamed`'s ("you wrote this, the schema says that") is
// the only one a writer can act on in a single edit.
func entryShapeBlock(nScanned int, shape []ShapeFinding) []string {
	quoted := make([]string, 0, len(ShapeHeadings))
	for _, h := range ShapeHeadings {
		quoted = append(quoted, "`"+h+"`")
	}
	spine := strings.Join(quoted, ", ")
	var renamed, absent, duplicated, empty []ShapeFinding
	for _, s := range shape {
		switch s.Kind {
		case ShapeRenamed:
			renamed = append(renamed, s)
		case ShapeAbsent:
			absent = append(absent, s)
		case ShapeDuplicated:
			duplicated = append(duplicated, s)
		case ShapeEmpty:
			empty = append(empty, s)
		}
	}
	if len(shape) == 0 {
		return []string{"", fmt.Sprintf(
			"entry shape: %d entry file(s) checked for %s — each present exactly once "+
				"and non-empty. 🔴 `%s` is NOT checked here: the reader DOES surface "+
				"it, but it feeds no count and no badge, so its absence changes nothing "+
				"this zero is about — `subsystem_recall` names a missing one under that "+
				"entry's own body instead.",
			nScanned, spine, WhatHeading)}
	}
	out := []string{"", fmt.Sprintf(
		"entry shape across %d entry file(s), checked for %s (NOT `%s`, which feeds no "+
			"count and no badge):", nScanned, spine, WhatHeading)}
	if len(renamed) > 0 {
		out = append(out, fmt.Sprintf(
			"  🔴 %d section(s) RENAMED — the heading is close but not exact, so NO "+
				"reader reaches the section. Matching is exact-string after a "+
				"right-strip, case-sensitive, at column 0. On that entry's index row "+
				"this reads as `0 nuance` with no `OPEN` badge: PARSE FAILURE, not an "+
				"empty entry.", len(renamed)))
		for _, s := range renamed {
			wrote := make([]string, 0, len(s.Found))
			for _, h := range s.Found {
				wrote = append(wrote, "`"+h+"`")
			}
			out = append(out, fmt.Sprintf("    %s: `%s` is written as %s",
				s.Filename, s.Heading, strings.Join(wrote, ", ")))
		}
	}
	if len(absent) > 0 {
		out = append(out, fmt.Sprintf(
			"  🔴 %d section(s) ABSENT — the heading is not in the file at all, under "+
				"any spelling this tool can pair with it. Whatever the entry says on "+
				"that subject is invisible to every default read. Read the inventory "+
				"below against the schema heading: a section retitled far enough that "+
				"no folding pairs it lands here rather than under RENAMED.", len(absent)))
		for _, s := range absent {
			shown := s.Found
			rest := 0
			if len(shown) > ShapeInventoryShown {
				rest = len(shown) - ShapeInventoryShown
				shown = shown[:ShapeInventoryShown]
			}
			inventory := "(none at all)"
			if len(shown) > 0 {
				q := make([]string, 0, len(shown))
				for _, h := range shown {
					q = append(q, "`"+h+"`")
				}
				inventory = strings.Join(q, ", ")
			}
			if rest > 0 {
				inventory += fmt.Sprintf(", … %d more", rest)
			}
			out = append(out, fmt.Sprintf(
				"    %s: no `%s`; the file's headings are %s",
				s.Filename, s.Heading, inventory))
		}
	}
	if len(duplicated) > 0 {
		out = append(out, fmt.Sprintf(
			"  🔴 %d heading(s) DUPLICATED — the sections silently MERGE into one body "+
				"and anything written under a heading BETWEEN them is dropped from the "+
				"read entirely. Fold them into one section.", len(duplicated)))
		for _, s := range duplicated {
			out = append(out, fmt.Sprintf("    %s: `%s` appears %d times",
				s.Filename, s.Heading, s.Count))
		}
	}
	if len(empty) > 0 {
		out = append(out, fmt.Sprintf(
			"  ⚠ %d section(s) PRESENT AND EMPTY — the heading is there with nothing "+
				"under it. Not a parse failure and not the same as absent: the reader "+
				"finds the section and prints a blank.", len(empty)))
		for _, s := range empty {
			out = append(out, fmt.Sprintf("    %s: `%s`", s.Filename, s.Heading))
		}
	}
	return append(out,
		"  (Advisory, and it changes no verdict: the loader accepts a file whose "+
			"sections it cannot find, which is precisely the silent failure this block "+
			"exists to make loud. Fix the heading, not the exit code.)")
}

// openActionsBlock is the UNFINISHED-BUSINESS advisory. Prints on every path that
// SCANNED something.
//
// 🔴 IT PRINTS ITS DENOMINATOR EVEN WHEN IT FINDS NOTHING, like every sibling. "0
// declared across 29 entry file(s)" is a reading; a blank space is not.
//
// 🔴 THE FOUR POPULATIONS ARE RENDERED SEPARATELY AND NEVER SUMMED — see OpenAction.
// `Declared` is exact and the unmarked guess is a FLOOR with unknown recall; one total
// over both would let the floor masquerade as a count.
func openActionsBlock(nScanned int, openActions []OpenAction) []string {
	// 🔴 FOUR INDEPENDENT FILTERS, NOT A `switch`, MIRRORING THE ORACLE'S FOUR LIST
	// COMPREHENSIONS EXACTLY. `ScanOpenActions` sets exactly one flag from one
	// `OpennessPopulation`, so a `switch` would be equivalent over every input either
	// scanner can produce — and that is the whole trap: a hand-built record carrying
	// two flags lands in TWO lists on the oracle and in one here, which is a
	// divergence nothing in this file would show. The populations are disjoint because
	// the SCANNER makes them so; the renderer must not be the second place that
	// decides it.
	var declared, near, unverifiable, guessed []OpenAction
	for _, a := range openActions {
		if a.Declared {
			declared = append(declared, a)
		}
		if a.NearMiss {
			near = append(near, a)
		}
		if a.UnverifiableClosure {
			unverifiable = append(unverifiable, a)
		}
		if !a.Declared && !a.NearMiss && !a.UnverifiableClosure {
			guessed = append(guessed, a)
		}
	}
	if len(openActions) == 0 {
		return []string{"", fmt.Sprintf(
			"open actions: 0 declared across %d entry file(s), 0 "+
				"attempted-but-unparsed, 0 `RESOLVED:` naming no sha, and 0 unmarked "+
				"bullets matched the two phrasings this tool can recognise. 🔴 The last "+
				"of those is a FLOOR with unknown recall, not a clean bill of health — "+
				"an unfinished action phrased any other way is invisible here.",
			nScanned)}
	}
	out := []string{"", fmt.Sprintf("open actions across %d entry file(s):", nScanned)}
	quote := func(rows []OpenAction) {
		for _, a := range rows {
			out = append(out, fmt.Sprintf("    %s: %s",
				a.Filename, truncRunes(a.FirstLine, AdvisoryQuoteMax)))
		}
	}
	if len(declared) > 0 {
		out = append(out, fmt.Sprintf(
			"  🔴 %d declared `OPEN:` — exact, the writer said so. Re-check against the "+
				"repo; if it landed, rewrite as `RESOLVED <sha>:`.", len(declared)))
		quote(declared)
	}
	if len(near) > 0 {
		out = append(out, fmt.Sprintf(
			"  🔴 %d bullet(s) look like an ATTEMPTED marker that did not parse — they "+
				"declare nothing and show no badge. Fix the line: the marker follows "+
				"`YYYY-MM-DD: `, is upper-case, carries no emphasis or parenthetical, "+
				"and ends in `:`.", len(near)))
		quote(near)
	}
	if len(guessed) > 0 {
		out = append(out, fmt.Sprintf(
			"  ⚠ %d unmarked bullet(s) that READ like an open action. AT LEAST this "+
				"many — two measured phrasings, unknown recall.", len(guessed)))
		quote(guessed)
	}
	if len(unverifiable) > 0 {
		out = append(out, fmt.Sprintf(
			"  ⚠ %d `RESOLVED:` bullet(s) name no sha, so the closure cannot be "+
				"checked. Not a defect — closing is the point — but `RESOLVED <sha>:` "+
				"is what makes it verifiable rather than asserted.", len(unverifiable)))
		quote(unverifiable)
	}
	return append(out,
		"  (Advisory. None of this changes the verdict: an entry with unfinished "+
			"business is still well-formed, and failing it here would be a red gate "+
			"nobody could turn green by fixing the file.)")
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
		return []string{"", fmt.Sprintf(
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
	out := []string{"", fmt.Sprintf(
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
