package store

import (
	"crypto/sha256"
	"encoding/hex"
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

// BulletMarkerSpan is how many BYTES of `line` the top-level LIST MARKER occupies —
// the `-`/`*` and the whitespace run after it — or 0 for a line that opens no bullet.
//
// 🔴 SAME REASON AS [MarkerSpan], ONE LEVEL SHALLOWER: a surface that renders a bullet
// as an `<li>` must not ALSO print the `- ` the list item already means, and the only
// spelling of "what a bullet marker is" that cannot drift from `ParseJournalBullets` is
// the pattern `ParseJournalBullets` groups on. A `strings.TrimPrefix(line, "- ")` at a
// call site is narrower than this in two ways that both matter — it misses `* ` and it
// misses a TAB — so it would leave the marker on screen for exactly the lines whose
// spelling is already unusual.
//
// It is 0 for an INDENTED dash, which is a continuation line and not a bullet. That is
// the same narrowing `journalBullet`'s own comment records, and it is the safe direction:
// a continuation line renders verbatim.
func BulletMarkerSpan(line string) int {
	loc := journalBullet.FindStringIndex(line)
	if loc == nil {
		return 0
	}
	return loc[1]
}

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

	// StartLine is the 0-based index, WITHIN THE SECTION BODY this bullet was parsed
	// from, of the line that OPENED it.
	//
	// 🔴 IT EXISTS SO A RENDERER NEED NOT RE-DECIDE WHAT A BULLET IS. `AGENTS.md`:
	// entry structure comes from this package's parsers, never from a new markdown
	// reader. A caller that wants to annotate bullet openings while emitting a body
	// VERBATIM would otherwise have to re-detect them, and the detection is not
	// trivial: a `- ` line INSIDE A FENCE is sample text, not a bullet (see the
	// `IsFence` branch below), and text before the first bullet is dropped. A second
	// copy of that rule would be wrong about fences on its first day.
	//
	// ⚠ IT IS AN INDEX INTO THE BODY, NOT INTO THE FILE. The body is what
	// `ExtractSections` returned, so a caller holding the file must not use this
	// against file line numbers. Named as a claim because an off-by-a-section is
	// silent: it would annotate the wrong lines rather than fail.
	//
	// ⚠ AND IT IS NOT A LENGTH. `len(Lines)` can exceed the distance to the next
	// bullet's StartLine, because trailing blank lines are stripped from `Lines`
	// while remaining in the body. A caller spanning bullets must walk StartLines,
	// never `StartLine + len(Lines)`.
	//
	// ⚠ AND IT IS AN INDEX INTO `pytext.SplitLines(body)`, WHICH BREAKS ON **TEN**
	// CHARACTERS — not on "\n". A consumer that re-splits the body with
	// `strings.Split(text, "\n")` gets a DIFFERENT list, and this index then names
	// the wrong line with no error anywhere. MEASURED on `"- a\rb\n- c\n"`:
	// `pytext.SplitLines` gives `["- a", "b", "- c"]` and the StartLines are
	// `[0, 2]`; `strings.Split(…, "\n")` gives `["- a\rb", "- c", ""]`, so StartLine
	// 0 points MID-LINE and StartLine 2 points at `""` — the second bullet would get
	// no token at all. ⚠ THE WRONG SPLITTER IS ALREADY IN THIS TREE:
	// `internal/ui/render.go`'s `inlineCode` splits its text on "\n". Both
	// renderers that emit section bodies use `pytext.SplitLines`, which is why the
	// index aligns TODAY; it is an alignment, not a property.
	//
	// 🔴 AND THE FIXTURE CASE DOES NOT CATCH A CONSUMER — THIS COMMENT SAID IT DID,
	// AND THAT IS RETRACTED RATHER THAN SOFTENED. It read: "the fixture case
	// `a-NON-newline-line-break-inside-a-bullet` is what catches a consumer that
	// picks the other one." That case pins THIS PACKAGE'S OWN StartLines (0 and 2 on
	// `"- a\rb\n- c\n"`) against the Python answers, so it catches the PARSER
	// changing splitter. It never runs a consumer. Measured at `902be517`: swapping
	// `internal/report`'s emission splitter to `strings.Split(body, "\n")` left
	// `go test ./... -count=1` at 21 ok, 0 FAIL — that fixture replay included.
	// What catches a consumer is
	// `report.TestTheRENDERERSOwnSplitterIsWhatPlacesTheToken`, which hands
	// `RenderText` a body directly; a body that arrived through `ExtractSections`
	// cannot see the difference, because that function re-joins with "\n".
	StartLine int
}

// CitationID is the opaque per-bullet token the read surfaces print, as exactly 8
// lowercase hex characters.
//
// 🔴 WHAT IT IS FOR. "Was this recalled bullet actually used?" has no answer today:
// every available proxy is a SPELLED one, walkable by rewording, and the obvious one
// is saturated by a MANDATE rather than by use: a downstream consumer's resume flow
// requires its report to echo what it recalled, so "a printed ref reappears later" fires
// 451/454 = 99.3% and measures COMPLIANCE, not use. A token that exists nowhere else in a
// corpus turns that question into a match.
//
// 🔴 AND THAT UNIQUENESS DECAYS, BY THE SAME MECHANISM THAT DISQUALIFIED THE OLD PROXY.
// Declared here rather than left for a reader to discover, because it is the FOUNDING
// rationale one paragraph up. [CitationToken] is stripped off the END of a submitted line
// before `write.ContentHash` is taken, so a verbatim echo is still recognised as the same
// bullet; a token quoted INSIDE new prose is not a trailer, is not stripped, and is stored
// VERBATIM by `write.RenderBullet` / the oracle's `render_bullet`. Measured end to end
// against the oracle's own functions: `- 2000-01-02: … [cb:7a54575e]` read, then
// `building on [cb:7a54575e], …` submitted, stores a bullet whose PROSE carries
// `[cb:7a54575e]` forever and which on the next read ALSO carries its own id. So:
//
//   - A CONSUMER MUST TAKE THE **LAST** `[cb:…]` ON A LINE. A first-match regexp reads the
//     quoted id — a different bullet's. Both renderers emit the same bytes, so no
//     comparison between them can surface the mistake. Deriving ids from the body with
//     [CitationIDsByStartLine] avoids the question entirely, and is what both renderers do.
//
//   - "NOWHERE ELSE IN A CORPUS" IS TRUE UNTIL THE FIRST SUCH WRITE, AND NOT AFTER. A
//     corpus search for a token then cannot tell a USE from a QUOTATION, which is exactly
//     the saturation the 99.3% proxy was rejected for. Nothing here bounds it and nothing
//     is proposed: `write.TestACitationTokenInProseIsNotStripped` makes the mid-prose token
//     deliberately permanent, because stripping it would make two different bullets hash
//     alike. The honest statement is that the signal degrades with use.
//
// 🔴 IT IS NOW PRINTED, AND THE CLOSING CONDITION THAT SAID SO IS MET. This comment read
// "NOT PRINTED BY ANY SURFACE AT THIS COMMIT" and named the condition that would retire
// that sentence: an `internal/report/testdata/reader_fixtures.json` row carrying `[cb:`.
// There are such rows. BOTH text renderers append [CitationToken] to every surfaced
// section line that OPENS a bullet — `internal/report`'s `RecallReport.RenderText` and
// `lib/subsystem_recall.py`'s `render_text`, which is also the oracle pod's — so this
// method is on the read path of every recall and its guards are no longer contract pins
// over a dead payload. ⚠ WHAT IS STILL NOT PRINTED: the oracle's `--json` payload carries
// `sections` as RAW bodies and is deliberately unannotated, and no browser surface reads
// `StartLine`. (That cross-reference read "`report_json`'s own note" and pointed at
// nothing — the function carried no such note; it is now written at its `"sections"` key
// in `lib/subsystem_recall.py`.)
//
// 🔴 DERIVED FROM THE BULLET'S OWN BYTES, WHICH IS WHAT MAKES CROSS-LANGUAGE AGREEMENT
// REACHABLE RATHER THAN DISCIPLINED. `sha256` over `Text()` — `Lines` joined with "\n"
// — first 8 hex characters. Pinned by `internal/store/testdata/citation_ids.json`,
// which the PYTHON side generates and this package's test replays, so neither
// implementation can move without one of them going red.
//
// 🔴 TWO NORMALISATIONS, NAMED, BECAUSE AN EARLIER DRAFT OF THIS COMMENT SAID "No
// normalisation" AND THAT WAS FALSE. Both are in the id whether anyone wants them or
// not, and both are KEPT:
//
//  1. THE LINE TERMINATOR. `ParseJournalBullets` splits with `pytext.SplitLines`, which
//     strips the terminator, and `Text()` re-imposes "\n". So THIRTEEN byte-distinct
//     spellings of one bullet — LF, CRLF, a lone CR, \v, \f, \x1c, \x1d, \x1e, U+0085,
//     U+2028, U+2029, no final terminator at all, and a trailing blank line — all yield
//     ONE id. MEASURED: all thirteen give `3a98d5a7` for `- one`.
//  2. THE DECODE FORM, which is a property of the CALLER rather than of this method.
//     `Lines` hold whatever string the loader produced, and `store.DecodeReplace` is
//     what the reader uses — so the id is a function of the entry's bytes AS A
//     `replace` DECODE SEES THEM. `lib/subsystem_resolver.py` coerces to the same form
//     before hashing (see `_citation_hash_bytes` there), which is what makes the pod's
//     `surrogateescape` decode answer the same id. ⚠ A caller that parsed bullets out
//     of RAW file bytes would get a different id for a malformed entry;
//     `internal/write` does exactly that, and it is safe only because it never calls
//     this method — it hashes `BulletContent`, not a citation id.
//
// ⚠ SO DO NOT "RESTORE BYTE-EXACTNESS" BY HASHING THE RAW SLICE. The behaviour above is
// desirable — a CRLF entry and an LF entry naming the same bullet should name it with
// the same id — but it is a CONTRACT: every previously printed id stops resolving the
// day somebody hashes the unsplit bytes instead.
//
// ⚠ THE INVALIDATION SET IS WIDER THAN "EDITING A BULLET", AND EVERY MEMBER IS MEASURED.
// An id names a parsed bullet's bytes, so it moves on: a TRAILING-WHITESPACE change
// (`- one` is `3a98d5a7`, `- one   ` is `7adcabe5`); UNICODE FORM (NFC `- café` is
// `60bec6cc`, NFD `83a886e0` — two visually identical bullets, two ids); ADDING OR
// REMOVING an `OPEN:`/`RESOLVED <sha>:` marker, which is part of the opening line's
// bytes; the WRITE TRAILER, since ` [cairn: <actor>/<session>]` lives inside `Lines[0]`
// and an id is therefore partly a function of the writing SESSION; and FIXING THE
// RECORDED `dropped-lines` DEFECT, which moves a NEIGHBOUR's id — lines currently
// absorbed into the preceding bullet would leave its `Lines`, so a bullet nobody
// touched loses its id.
//
// ⚠ THAT LAST MEMBER NOW NAMES ITS BODIES, BECAUSE THE FIRST NUMBER IT WAS GIVEN
// ARRIVED WITHOUT ONE. Measured here, in Go: over `"- first\nstray line\n- second\n"`
// the first bullet is `608a5be6`, and over `"- first\n- second\n"` — the same bullet
// after the absorbed line leaves — it is `6b4d8edb`. `0aa5fc48`'s commit message
// paired `6b4d8edb` with `ad08c42d`; that second value is RETRACTED as
// unreproducible, and the retraction and its sweeps are recorded once, beside
// `lib/subsystem_resolver.py`'s copy of this list.
//
// 🔴 AND THE ID IS **NOT SCOPED**. Two byte-identical bullets in different entries, or
// in different scopes, get the SAME id, deterministically — `write.AppendBullet` dedupes
// within ONE file only, so the corpus does not forbid it. The arithmetic below models
// RANDOM collisions only; it says nothing about duplicated text, which collides with
// probability 1.
//
// 🔴 AND THE ESCAPE HATCH THIS PARAGRAPH USED TO OFFER IS GONE. It read: the framing "a
// token that exists nowhere else in a corpus" "therefore holds for the PROSE a bullet
// carries and not for a duplicate of it". That is the one reading the citation echo
// breaks — a writer who quotes `[cb:…]` INSIDE new prose puts the token in the prose, and
// nothing strips it there. So the framing holds for NEITHER a duplicate NOR the prose;
// the decay paragraph above the derivation states the honest version and nothing bounds
// it. Two independent failure modes of one sentence, which is why it is retracted rather
// than narrowed again.
//
// ⚠ 8 HEX, NOT 4, AND THE REASON IS ARITHMETIC. At 16 bits a 3,129-bullet corpus
// collides with probability ≈1 (birthday: ~50% by ~300 bullets); 32 bits puts it near
// 0.1% corpus-wide. Do not shorten it to fit a column. ⚠ THE 3,129 IS A DIFFERENT
// POPULATION FROM THE 110 THIS STRUCT'S OWN COMMENT COUNTS — three sizes are in play
// across the two implementations and none is a re-measurement of another; the
// reconciliation is written out once, in `citation_id`'s docstring in
// `lib/subsystem_resolver.py`.
func (b JournalBullet) CitationID() string {
	// `Text()` rather than a second `strings.Join`: ONE definition of "the bullet's
	// bytes", which is the rule the Python twin's docstring already states. Two
	// definitions on the one function where a divergence is a contract break is how
	// the next edit moves one of them.
	sum := sha256.Sum256([]byte(b.Text()))
	return hex.EncodeToString(sum[:])[:8]
}

// CitationToken is the rendered spelling of one citation id — the LEADING SPACE is part
// of it, because every caller appends it to a line that already ends in content.
//
// 🔴 ONE SPELLING OF THE RENDERED TOKEN, AND THE REASON IS A SEAM RATHER THAN TIDINESS.
// `internal/write`'s `citationTokenPattern` (`\[cb:[0-9a-f]{8}\]`) is what STRIPS this
// trailer before a bullet's `ContentHash` is taken, and the echo it strips is measured
// rather than feared — a downstream consumer's resume flow requires its report to echo
// what it recalled, firing on 451 of 454 runs (99.3%). A format that drifted from that
// pattern would leave the token in the hash, and the damage is the silent kind: the
// re-POST stops matching the bullet on disk, appends a near-duplicate, and that duplicate
// carries a `[cb:…]` in its PROSE naming the bullet it was copied FROM. The two cannot be
// derived from one another (a format string is not a regexp), so what binds them is a
// test that renders through this function and strips through that one —
// `TestTheRenderedCitationTokenIsStrippedByTheWritePath`.
//
// ⚠ 14 BYTES PER ANNOTATED LINE, and that is the whole cost of the feature: 1 space + 4
// for `[cb:` + 8 hex + 1 for `]`. It counts against `BulletTextMax` on a re-POST rather
// than being exempt; the reason is at `write.BulletContent`.
func CitationToken(id string) string { return " [cb:" + id + "]" }

// CitationIDsByStartLine maps a SECTION BODY's bullet-opening line index to that bullet's
// [JournalBullet.CitationID].
//
// 🔴 IT PARSES FOR IDS ONLY — THE CALLER STILL EMITS THE BODY VERBATIM, AND THAT
// SEPARATION IS THE WHOLE DESIGN RATHER THAN A STYLE CHOICE. The obvious implementation
// of an annotated read is to walk [ParseJournalBullets] and print each bullet's `Lines`;
// that is RULED OUT, because this parser has a recorded `dropped-lines` defect — text
// BEFORE the first bullet never enters the bullet list, and a bullet whose opening line
// was lost or indented is ABSORBED into the bullet above it. Today those are an advisory
// validator finding and nothing is lost. Under a re-emission they become SILENT DELETION
// on the read surface: measured history is 7 versions across two entries carrying dropped
// lines for 2–8 days with that validator green throughout. So this function hands back
// POSITIONS, never content, and a renderer that uses it cannot lose a byte it was given.
//
// 🔴 A LINE WITH NO ENTRY HERE IS NOT EVIDENCE ITS BULLET WAS NOT PRINTED. Every body line
// is printed either way; the map only says which ones carry an id. Four shapes are absent
// from it BY DESIGN — prose, a blank, an INDENTED dash (a continuation, see
// `journalBullet`), and a dash INSIDE A FENCE (sample text) — and a fifth, the
// `dropped-lines` population above, is absent by DEFECT. Measured over a real 344-entry
// store, by surfaced section: `## Pointers` 1,589 of 2,635 body lines carry an id and 15
// are pre-first-bullet prose; `## Nuance / work-history` 3,375 of 14,094; `## What it is`
// 7 of 1,274, because that section is prose and 1,258 of its lines precede any bullet;
// `## Requirements` 7 of 7. ⚠ THOSE ARE ONE READING OF A LIVE STORE AND WILL NOT
// RE-DERIVE — the store is not in this repository, and the nuance pair moved 3,367/14,086
// -> 3,375/14,094 between two measurements an hour apart while this comment was being
// written. What is STABLE, and what the sentence actually rests on, is the last clause:
// ZERO column-0 dash lines in any of the four sections parsed to no bullet, at both
// readings. So missing ids are a DEGRADATION IN COVERAGE, never in content, and anyone
// reading a gap as "that bullet was withheld" has the direction backwards.
//
// ⚠ THE KEYS ARE INDICES INTO `pytext.SplitLines(body)`, WHICH BREAKS ON **TEN**
// CHARACTERS — not on "\n". A caller that re-splits with `strings.Split(body, "\n")`
// annotates the wrong line, silently; the worked measurement is at
// [JournalBullet.StartLine]. Split with `pytext.SplitLines` or do not use this map.
//
// ⚠ AND IT IS PER SECTION BODY, NOT PER FILE — `StartLine` is an index into whatever
// `ExtractSections` returned, so handing this a whole entry file annotates lines that
// merely share an offset with a bullet.
//
// Collisions are impossible: `ParseJournalBullets` records one start index per group, in
// lockstep with the groups, so no two bullets claim one line.
func CitationIDsByStartLine(body string) map[int]string {
	bullets := ParseJournalBullets(body)
	out := make(map[int]string, len(bullets))
	for _, b := range bullets {
		out[b.StartLine] = b.CitationID()
	}
	return out
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
	// 🔴 PARALLEL TO `groups`, APPENDED IN LOCKSTEP WITH IT. A map keyed on the
	// opening LINE would be wrong wherever two bullets open identically, which the
	// corpus does not forbid; the index is recorded where the group is created, so
	// the two slices cannot disagree about which bullet is which.
	var starts []int
	inFence := false
	for i, line := range pytext.SplitLines(body) {
		if IsFence(line) {
			inFence = !inFence
			if len(groups) > 0 {
				groups[len(groups)-1] = append(groups[len(groups)-1], line)
			}
			continue
		}
		if !inFence && journalBullet.MatchString(line) {
			groups = append(groups, []string{line})
			starts = append(starts, i)
			continue
		}
		if len(groups) > 0 {
			groups[len(groups)-1] = append(groups[len(groups)-1], line)
		}
	}
	out := make([]JournalBullet, 0, len(groups))
	for gi, group := range groups {
		for len(group) > 0 && pytext.StripWhitespace(group[len(group)-1]) == "" {
			group = group[:len(group)-1]
		}
		openness, resolvedBy := BulletOpenness(group[0])
		out = append(out, JournalBullet{
			Lines:      group,
			Date:       BulletDate(group[0]),
			Openness:   openness,
			ResolvedBy: resolvedBy,
			StartLine:  starts[gi],
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
