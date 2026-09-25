package store

import (
	"regexp"
	"time"
	"unicode"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// The two openness markers a journal bullet may DECLARE.
const (
	OpennessOpen     = "open"
	OpennessResolved = "resolved"
)

// The populations OpennessPopulation partitions bullets into. Every consumer
// branches on THESE rather than on the raw fields, because a predicate duplicated
// across call sites regenerates the same bug at every site — on the oracle two
// surfaces once counted ONE bullet twice, each having decided membership for itself.
const (
	PopulationOpen         = "open"
	PopulationResolved     = "resolved"
	PopulationUnverifiable = "unverifiable"
	PopulationNearMiss     = "near-miss"
	PopulationUnmarked     = "unmarked"
	PopulationNone         = "none"
)

// journalOpenness is `- [YYYY-MM-DD: ]OPEN: …` / `- [YYYY-MM-DD: ]RESOLVED <sha>: …`
// — the marker, as a PREFIX rather than as a phrase.
//
// 🔴 WHY THIS IS SCHEMA AND NOT A PROSE DETECTOR. A bullet proposing a one-line
// remedy went on being served as an open action for weeks after the remedy landed,
// and nothing could have noticed, because "this is not applied yet" was a claim made
// only in prose. The obvious repair — grep the prose for remedy words — is a guard on
// WORDS, and a guard on words is walkable by rewording: a writer who says "the
// endpoint is already correct" walks past it, silently. A prefix a writer must TYPE
// cannot be walked by rewording the sentence after it.
//
// `RESOLVED` takes the sha that closed it so the claim is checkable
// (`git cat-file -e <sha>`) rather than being a second unverifiable assertion.
//
// ⚠ TRANSCRIBED DIRECTLY, LOOKAHEAD-FREE, SO IT IS ONE REGEXP HERE TOO. The oracle's
// pattern uses no lookaround, so RE2 expresses it exactly; the two below do use
// lookaround and are hand-rolled for that reason.
var journalOpenness = regexp.MustCompile(
	`^[-*][ \t]+` +
		`(?:\d{4}-\d{2}-\d{2}:[ \t]+)?` + // the optional leading date
		`(OPEN|RESOLVED)` +
		`(?:[ \t]+([0-9a-fA-F]{7,40}))?` + // RESOLVED carries the closing sha
		`:`, // exact terminator, no fuzz
)

// journalDatePrefix is the SHAPE half of `- YYYY-MM-DD…`. The oracle's pattern ends
// in `(?=[:,)\]\s]|$)`, which RE2 cannot express, so the lookahead is applied by the
// caller against the rune that follows — and it has to be, because it is what makes
// `- 2000-01-0212:` not a dated bullet.
//
// The date is OPTIONAL on a bullet: measured over the oracle's live corpus, 62 of 110
// top-level bullets carry one and 48 do not, so a parser that required one would drop
// 44% of the corpus on the floor and call the result a complete read.
var journalDatePrefix = regexp.MustCompile(`^[-*][ \t]+(\d{4}-\d{2}-\d{2})`)

// BulletOpenness is `(openness, resolvedBy)` for one bullet's FIRST line.
//
// It takes the first line only, but 🔴 THAT IS NOT WHAT ENFORCES IT — the guard is
// that the pattern is anchored at position 0 with no multiline flag, so a marker on a
// continuation line is unreachable whether this is passed one line or the whole joined
// bullet. Stated because a mutation battery on the oracle proved it: passing the joined
// text is an EQUIVALENT mutant and survives, and an earlier docstring claimed the
// argument was the protection. The narrower argument is kept as defence in depth — if
// the pattern ever gains a multiline flag, passing one line is what stops markers being
// invented out of wrapped prose.
//
// The sha is lowercased so `RESOLVED B83BFB58:` and `RESOLVED b83bfb58:` are one claim.
func BulletOpenness(firstLine string) (openness, resolvedBy string) {
	m := journalOpenness.FindStringSubmatch(firstLine)
	if m == nil {
		return "", ""
	}
	marker := OpennessResolved
	if m[1] == "OPEN" {
		marker = OpennessOpen
	}
	if m[2] == "" {
		return marker, ""
	}
	return marker, pytext.Lower(m[2])
}

// BulletDate is the bullet's ISO date, or "" — VALIDATED, not just shaped.
//
// `2000-13-45` matches the shape and is not a date; returning it would put a
// nonexistent day into a recency claim and into any arithmetic done on it. The parse is
// the check, so what comes back is always a real date.
func BulletDate(firstLine string) string {
	loc := journalDatePrefix.FindStringSubmatchIndex(firstLine)
	if loc == nil {
		return ""
	}
	// The oracle's lookahead: the date must be followed by end-of-string or one of
	// `[:,)\]\s]`. `\s` is Python's 29-code-point set, which is `pytext.IsSpace` — NOT
	// Go's `\s`.
	rest := firstLine[loc[1]:]
	if rest != "" {
		r := []rune(rest)[0]
		switch r {
		case ':', ',', ')', ']':
		default:
			if !pytext.IsSpace(r) {
				return ""
			}
		}
	}
	date := firstLine[loc[2]:loc[3]]
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return ""
	}
	return date
}

// unmarkedAction is the retrospective advisory — DELIBERATELY NARROW, and a FLOOR
// rather than a list. `\bFIX\s*[(:]|\bnot (yet )?addressed\b`, case-insensitive, on
// the oracle.
//
// 🔴 IT IS HAND-ROLLED BECAUSE BOTH `\b` AND `\s` DIFFER BETWEEN THE TWO ENGINES, and
// each difference is a silent widening. Go's `\b` is ASCII-only while Python's is
// defined against a Unicode `\w`, so `…ñFIX(` is a match in Go and not in Python; Go's
// `\s` is `[\t\n\f\r ]` while Python's is 29 code points, so `FIX (` is a match in
// Python and not in Go. See `pytext.IsWordChar` and `pytext.IsSpace`, both of which are
// measured against the pinned interpreter over every code point.
//
// 🔴 RECALL IS UNKNOWN AND MUST BE REPORTED AS SUCH. This finds bullets that HAPPEN to
// be phrased the two ways already observed; it is never evidence that an entry has no
// open actions. The marker above is the mechanism, this is a net under it for bullets
// written before the marker existed.
func unmarkedAction(text string) bool {
	rs := []rune(text)
	for i := range rs {
		if !atWordBoundary(rs, i) {
			continue
		}
		if matchFixParen(rs, i) || matchNotAddressed(rs, i) {
			return true
		}
	}
	return false
}

// atWordBoundary is Python's `\b` immediately BEFORE position i: a transition between
// a `\w` rune and a non-`\w` one, with the text edges counting as non-`\w`. Only the
// "the next rune starts a word" direction is needed, because both patterns open with a
// word character.
func atWordBoundary(rs []rune, i int) bool {
	if i >= len(rs) || !pytext.IsWordChar(rs[i]) {
		return false
	}
	return i == 0 || !pytext.IsWordChar(rs[i-1])
}

func matchFixParen(rs []rune, i int) bool {
	if !hasFoldedPrefix(rs[i:], "fix") {
		return false
	}
	j := i + 3
	for j < len(rs) && pytext.IsSpace(rs[j]) {
		j++
	}
	return j < len(rs) && (rs[j] == '(' || rs[j] == ':')
}

func matchNotAddressed(rs []rune, i int) bool {
	j := i
	if !hasFoldedPrefix(rs[j:], "not ") {
		return false
	}
	j += 4
	if hasFoldedPrefix(rs[j:], "yet ") {
		j += 4
	}
	if !hasFoldedPrefix(rs[j:], "addressed") {
		return false
	}
	// The pattern's trailing `\b`: the word must END here.
	j += 9
	return j >= len(rs) || !pytext.IsWordChar(rs[j])
}

// hasFoldedPrefix is a case-insensitive prefix test over an ASCII-lowercase `want`.
// ASCII folding is the whole of `re.I` for these patterns: every character in them is
// an ASCII letter or a space, and CPython's `re.I` on a `str` pattern additionally
// folds the two Kelvin/long-s style relatives of `k` and `s` — neither of which appears
// in `FIX`, `not`, `yet` or `addressed`.
func hasFoldedPrefix(rs []rune, want string) bool {
	w := []rune(want)
	if len(rs) < len(w) {
		return false
	}
	for i, c := range w {
		g := rs[i]
		if g >= 'A' && g <= 'Z' {
			g += 'a' - 'A'
		}
		if g != c {
			return false
		}
	}
	return true
}

// nearMissMarker answers "did this bullet TRY to carry a marker and miss the grammar?".
//
// 🔴 THE SILENT FAILURE IS THE SAME CLASS AS THE BUG THE MARKER EXISTS FOR, which is
// why this exists at all. Shapes that parse as NO MARKER: `- 2000-01-02 OPEN: …` (the
// date is not followed by `:`), `- 2000-01-02: RESOLVED abc1234 (repo): …` (a
// parenthetical before the colon), `- 2000-01-02: **OPEN:** …` (emphasis), `OPEN : …`
// (a space before the colon), `RESOLVED PR#505: …` (a non-sha reference). A writer
// follows the protocol, the `🔴 N OPEN` badge disappears — which LOOKS like success —
// and the claim is discarded.
//
// Deliberately NOT fixed by loosening the marker pattern: a lenient marker starts
// matching prose, and INVENTING a marker is worse than missing one, because a false
// `RESOLVED` closes an action nobody closed. So the strict grammar stands and the
// near-misses are REPORTED.
//
// 🔴 TWO BRANCHES, BECAUSE ON THE ORACLE THREE ROUNDS PROVED NEITHER ALONE IS ENOUGH.
// (a) SHOUTED — an all-caps marker needs no terminator, because prose does not shout.
// (b) sentence-cased — then a `:` IS required, optionally after a sha / PR reference /
// parenthetical, because sentence case IS ordinary English: "Open questions remain" must
// not fire while "Open:" must. Requiring the terminator in BOTH was the subtler failure:
// it demanded the exact character whose OMISSION is the likeliest way to miss the
// grammar.
//
// 🔴 THE GUARD ON (a) IS "not followed by `[A-Za-z0-9_]`", NOT "not followed by a
// lowercase letter", and the difference is a whole class of false positive: the narrower
// form let through every all-caps identifier a real bullet quotes — `OPENSSL_CONF`,
// `OPEN_MAX`, `RESOLVED_ADDR`, `OPENED` — each raising a "marker did not parse" advisory
// about a correct sentence, which is how a loud path gets ignored.
//
// ⚠ HAND-ROLLED, AND THE ENUMERATION IS THE TRANSCRIPTION. The oracle's pattern has
// three lookaheads and a `*` loop over a variable-length atom; RE2 has no lookaround, so
// the optional prefixes are enumerated and the loop is a memoized walk. The memo is not
// an optimisation for its own sake: `{7,40}` inside a `*` loop is exponentially ambiguous
// on the oracle when the trailing `:` never arrives — measured there at 0.028 s for 64
// hex characters and NO RETURN IN 30 SECONDS for three 40-character shas, hanging the
// caller with no output. The oracle closed that with a lookahead on the hex atom; keying
// the walk on the position makes it linear in states here, which closes the same hole by
// construction.
//
// ⚠ ITS OWN NARROWING LEDGER, BECAUSE IT DOES NOT INHERIT `LineMentionsMarker`'S. The
// two share `refRunThenColon`, `refAtomEnds` and `hasFoldedPrefix`, and a round that
// converged a SHARED helper wrote that the fix landed "for BOTH callers" — which was
// false, because this transcription has grammar the other one does not (the `^[-*][ \t]+`
// prefix, the optional date, the shouted branch) and each of those can carry a narrowing
// of its own. It did: the date prefix's `\d{4}-\d{2}-\d{2}` stayed ASCII-only in
// `looksISODate` through that round, 1,020 divergent lines in the committed sweep's
// corpus, while the comment one function over read as though it were closed. Read a
// convergence claim as being about the RUN it was measured on, never about a caller.
//
// The ledger, measured by `internal/store/markersweep_test.go` over the corpus
// `tests/marker_corpus.py` generates:
//
//	`\d` in the date prefix (`looksISODate`)    CLOSED — `unicode.IsDigit`
//	`\d` in the ref atoms (`refAtomEnds`)       CLOSED — `unicode.IsDigit`
//	`PR#` folding, the class folds              N/A — `_NEAR_MISS_MARKER` carries no
//	                                            `re.I` outside `(?i:OPEN|RESOLVED)`,
//	                                            so ASCII IS the faithful reading here
//	`hasFoldedPrefix`'s ASCII-only fold          OPEN, 211 divergences, all
//	                                            oracle-wider — the SAME residual
//	                                            `LineMentionsMarker` declares, because
//	                                            `(?i:…)` folds U+017F too
//
// So this function has exactly ONE residual and it is the shared one; there is no
// second, and the closing condition is `marker.go`'s.
func nearMissMarker(firstLine string) bool {
	rs := []rune(firstLine)
	if len(rs) == 0 || (rs[0] != '-' && rs[0] != '*') {
		return false
	}
	// `[ \t]+` — at least one. Every split point of the run is tried, because the
	// groups that follow can also match a space or a tab and a greedy engine would
	// backtrack into them.
	run := 1
	for run < len(rs) && (rs[run] == ' ' || rs[run] == '\t') {
		run++
	}
	for i := 2; i <= run; i++ {
		for _, after := range datePrefixEnds(rs, i) {
			// `[^A-Za-z0-9]{0,4}` — `**`, quotes, stray punctuation.
			p := after
			for k := 0; ; k++ {
				if markerAlternation(rs, p) {
					return true
				}
				if k == 4 || p >= len(rs) || isASCIIAlnum(rs[p]) {
					break
				}
				p++
			}
		}
	}
	return false
}

// datePrefixEnds is every position the optional `(?:\d{4}-\d{2}-\d{2}[^A-Za-z]{0,3})?`
// group can end at, starting with `i` — the group being skipped entirely, which is
// always allowed.
func datePrefixEnds(rs []rune, i int) []int {
	ends := []int{i}
	if !looksISODate(rs, i) {
		return ends
	}
	p := i + 10
	ends = append(ends, p)
	for k := 0; k < 3 && p < len(rs) && !isASCIILetter(rs[p]); k++ {
		p++
		ends = append(ends, p)
	}
	return ends
}

// looksISODate is `\d{4}-\d{2}-\d{2}` at `i`.
//
// 🔴 `\d` IS THE UNICODE `Nd` CATEGORY HERE TOO, AND READING IT AS `[0-9]` WAS A
// NARROWING THE ROUND THAT CONVERGED `refAtomEnds` WALKED PAST. `_NEAR_MISS_MARKER` is
// not compiled `re.ASCII`, so its date prefix takes every decimal digit in Unicode,
// exactly as its `PR#\d+` does. An ASCII-only reading made `nearMissMarker` blind to
// every dated bullet written in a non-ASCII numeral system. MEASURED by the committed
// sweep (`internal/store/markersweep_test.go`) with this line still ASCII-only: 1,020
// lines matched on the oracle and not here, every one of them carrying an
// Arabic-Indic date, and none the other way.
//
// 🔴 THE DENOMINATOR IS DELIBERATELY ABSENT, NOT CORRECTED. It read "of 10,816"
// and was stale IN THE COMMIT THAT WROTE IT — six corpus rows were appended by a
// sibling edit in the same change — the third number in this arc to go stale the
// moment another line moved. Correcting the digits regenerates the class. The corpus
// size is `corpus_lines` in `internal/store/testdata/marker_oracle_sweep.json`, and
// BOTH clients re-derive it from the generator and fail on a mismatch, so a prose
// copy duplicates a machine-checked fact and is the only copy nothing checks. The
// numerator stays because it is a MUTATION measurement — reproducible only by
// reverting this function — and no artifact in the tree carries it.
//
// ⚠ THE HYPHENS STAY ASCII. `-` is a literal in the pattern, not a class, so no
// Unicode dash is accepted on either side.
func looksISODate(rs []rune, i int) bool {
	if i+10 > len(rs) {
		return false
	}
	for offset, want := range []byte("dddd-dd-dd") {
		c := rs[i+offset]
		if want == '-' {
			if c != '-' {
				return false
			}
			continue
		}
		if !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

// markerAlternation is the `(?: shouted | sentence-cased-with-terminator )` group.
func markerAlternation(rs []rune, p int) bool {
	for _, word := range []string{"OPEN", "RESOLVED"} {
		if hasExactPrefix(rs[p:], word) {
			end := p + len(word)
			if end >= len(rs) || !isWordish(rs[end]) {
				return true
			}
		}
	}
	for _, word := range []string{"open", "resolved"} {
		if !hasFoldedPrefix(rs[p:], word) {
			continue
		}
		// `false`: `_NEAR_MISS_MARKER` scopes `(?i:…)` to the marker word alone, so
		// everything after it — the `PR#` literal AND the two character classes — is
		// read case-sensitively. See `refRunThenColon`.
		if refRunThenColon(rs, p+len(word), map[int]bool{}, false) {
			return true
		}
	}
	return false
}

// refRunThenColon is `(?:[ \t]*(?:<ref>))*[^A-Za-z0-9\n]{0,4}:` — memoized on the
// position, which is the whole state, so the walk cannot blow up.
//
// 🔴 `ignoreCase` IS NOT A CONVENIENCE FLAG: IT IS *WHETHER THE ORACLE PATTERN THIS
// CALLER TRANSCRIBES WAS COMPILED `re.IGNORECASE` AS A WHOLE*, AND A SHARED WALK WITH
// ONE ANSWER WAS WRONG FOR ONE OF THEM. `_MARKER_ANYWHERE` is `re.IGNORECASE` over the
// WHOLE pattern; `_NEAR_MISS_MARKER` puts only the marker word inside a scoped
// `(?i:OPEN|RESOLVED)` and leaves everything after it OUTSIDE.
//
// 🔴 IT GOVERNS THREE THINGS, NOT ONE, AND AN EARLIER VERSION OF THIS PARAGRAPH NAMED
// ONLY THE FIRST — under the name `foldPR`, which is how the other two stayed invisible:
//
//	(1) the `PR#` LITERAL — `pr#`, `Pr#` and `pR#` match on `_MARKER_ANYWHERE` and not
//	    on `_NEAR_MISS_MARKER`. Passing `false` from both (a single `hasExactPrefix`)
//	    made `LineMentionsMarker` narrower than its oracle on every lowercase spelling:
//	    540 divergent lines in the committed sweep's corpus, all of them `pr#`.
//	(2) `[^A-Za-z0-9\n]{0,4}` — the terminator run, in `terminatorColon`. Under `re.I`
//	    that NEGATED class also excludes the four non-ASCII runes that fold onto an
//	    ASCII letter, so the oracle's run cannot span them and an ASCII-only reading
//	    here spans them happily. That direction is WIDER than the oracle.
//	(3) `[A-Za-z0-9_]` — the word-boundary lookahead, at `LineMentionsMarker`'s own
//	    call site rather than in this walk. Same four runes, same widening: `OPENſ:`
//	    matched here and not on the oracle.
//
// See `foldsToASCIILetter` for the enumeration and how it was measured.
//
// `seen` stays one map per top-level call because `ignoreCase` is constant within one.
func refRunThenColon(rs []rune, p int, seen map[int]bool, ignoreCase bool) bool {
	if seen[p] {
		return false
	}
	seen[p] = true
	if terminatorColon(rs, p, ignoreCase) {
		return true
	}
	// `[ \t]*` is taken maximally: every ref atom below begins with a character that
	// is neither a space nor a tab, so giving whitespace back can never help.
	q := p
	for q < len(rs) && (rs[q] == ' ' || rs[q] == '\t') {
		q++
	}
	for _, next := range refAtomEnds(rs, q, ignoreCase) {
		if refRunThenColon(rs, next, seen, ignoreCase) {
			return true
		}
	}
	return false
}

// refAtomEnds is every position one `<ref>` atom can end at, starting from q:
// `[0-9a-fA-F]{7,40}(?![0-9a-fA-F])`, `PR#\d+`, `#\d+`, `\([^)]{1,30}\)` or
// `\[[^\]]{1,30}\]`.
//
// `ignoreCase` folds the `PR` literal here, and only that — see `refRunThenColon` for
// the other two things the same flag governs, and for which caller wants which. The hex
// atom is already both cases by its own class, and MEASURED to gain nothing under
// `re.I`: no non-ASCII rune folds into `[0-9a-fA-F]`.
func refAtomEnds(rs []rune, q int, ignoreCase bool) []int {
	var ends []int
	// The hex atom's lookahead forces the WHOLE maximal hex run to be consumed, so the
	// run's length is what decides: 7..40 matches, anything else does not.
	hex := q
	for hex < len(rs) && isHexDigit(rs[hex]) {
		hex++
	}
	if n := hex - q; n >= 7 && n <= 40 {
		ends = append(ends, hex)
	}
	// `PR#\d+` and `#\d+`, with every length of the digit run — a greedy engine would
	// backtrack into them, and a later atom can begin with a digit.
	//
	// 🔴 `\d` IS A UNICODE CLASS IN BOTH ORACLE PATTERNS, NOT `[0-9]`. Neither is
	// compiled with `re.ASCII`, so CPython's `\d` on a `str` pattern is category `Nd`
	// — every decimal digit in Unicode, of which ASCII is one block of about 700.
	// `unicode.IsDigit` is that same category, so THIS RUN transcribes the oracle for
	// both callers rather than a narrower reading of it.
	//
	// 🔴 AND THAT IS A CLAIM ABOUT THIS RUN, NOT ABOUT EITHER CALLER. An earlier
	// version of this paragraph said the fix landed "for BOTH callers: the narrowing
	// was shared, so `nearMissMarker` carried it too" — the sharing is real and the
	// conclusion was false. `_NEAR_MISS_MARKER` has a SECOND `\d` run this atom cannot
	// reach, the optional date prefix `\d{4}-\d{2}-\d{2}`; it is transcribed in
	// `looksISODate`, which that round did not touch and which stayed ASCII-only.
	// MEASURED at the commit that wrote the sentence: `- ٢٠٠٠-٠١-٠٤: OPEN:` matched
	// `_NEAR_MISS_MARKER` and not `nearMissMarker`, one of 1,020 such lines in the
	// committed sweep's corpus, all oracle-wider and none the other way. Both runs read
	// `unicode.IsDigit` now — but the claim is per-RUN either way, because "a shared
	// helper was fixed" says nothing about the other places the same class is spelled.
	digitsFrom := -1
	switch {
	case ignoreCase && hasFoldedPrefix(rs[q:], "pr#"):
		digitsFrom = q + 3
	case !ignoreCase && hasExactPrefix(rs[q:], "PR#"):
		digitsFrom = q + 3
	case q < len(rs) && rs[q] == '#':
		digitsFrom = q + 1
	}
	if digitsFrom >= 0 {
		for j := digitsFrom; j < len(rs) && unicode.IsDigit(rs[j]); j++ {
			ends = append(ends, j+1)
		}
	}
	// `\([^)]{1,30}\)` and `\[[^\]]{1,30}\]` — the inner class excludes the closer, so
	// the closer is the FIRST one and there is at most one way to match each.
	for _, pair := range [][2]rune{{'(', ')'}, {'[', ']'}} {
		if q >= len(rs) || rs[q] != pair[0] {
			continue
		}
		for m := 1; m <= 30 && q+m < len(rs); m++ {
			if rs[q+m] == pair[1] {
				ends = append(ends, q+m+1)
				break
			}
		}
	}
	return ends
}

// terminatorColon is `[^A-Za-z0-9\n]{0,4}:`.
//
// ⚠ THE NEGATED CLASS NARROWS UNDER `re.I`, WHICH IS THE OPPOSITE OF THE INTUITION.
// `re.IGNORECASE` widens `[A-Za-z0-9]`, so NEGATING it takes those extra runes AWAY:
// on `_MARKER_ANYWHERE` this run cannot span `ſ`, while on `_NEAR_MISS_MARKER` — no
// flags — it can. Reading it ASCII-only for both made `LineMentionsMarker` WIDER than
// its oracle, which is the direction that manufactures a declaration nobody typed.
func terminatorColon(rs []rune, p int, ignoreCase bool) bool {
	for k := 0; ; k++ {
		if p < len(rs) && rs[p] == ':' {
			return true
		}
		if k == 4 || p >= len(rs) || rs[p] == '\n' ||
			isASCIIAlnum(rs[p]) || (ignoreCase && foldsToASCIILetter(rs[p])) {
			return false
		}
		p++
	}
}

func hasExactPrefix(rs []rune, want string) bool {
	w := []rune(want)
	if len(rs) < len(w) {
		return false
	}
	for i, c := range w {
		if rs[i] != c {
			return false
		}
	}
	return true
}

func isASCIILetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }
func isASCIIAlnum(r rune) bool  { return isASCIILetter(r) || (r >= '0' && r <= '9') }
func isWordish(r rune) bool     { return isASCIIAlnum(r) || r == '_' }

// foldsToASCIILetter is the complete set of NON-ASCII runes that CPython's
// `re.IGNORECASE` folds into an ASCII letter, so that `[A-Za-z0-9_]` matches them and
// `[^A-Za-z0-9\n]` does NOT.
//
// 🔴 A CHARACTER CLASS CHANGES MEANING UNDER `re.I`, NOT JUST A LITERAL — AND THAT IS
// THE HALF THE `foldPR` ROUND MISSED. It converged the `PR` LITERAL and left the two
// CLASSES around it ASCII-only, which made this walk WIDER than `_MARKER_ANYWHERE`:
// the word-boundary lookahead stopped guarding and the terminator run spanned runes
// the oracle's negated class excludes. Wider is the dangerous direction — it
// manufactures a declaration nobody typed.
//
// MEASURED on the committed sweep's corpus, by reverting each site in turn:
//
//	both ASCII (the state before this)  7 lines wider than the oracle
//	terminator run ASCII only           3 — `OPEN ſ:`, `RESOLVED abc1234 K:`, `OPEN ſſſ:`
//	boundary lookahead ASCII only       0 — see `LineMentionsMarker`, which declares
//	                                    that site as UNREACHABLE rather than as a guard
//
// 🔴 ENUMERATED BY MEASUREMENT, NOT BY REASONING ABOUT UNICODE. Every codepoint below
// U+110000 was tested against `re.compile("[A-Za-z0-9_]", re.I)` on the pinned
// interpreter (CPython 3.12.14); FOUR matched outside ASCII and these are they. Note
// there is no digit relative: `0-9` gains nothing under `re.I`, and neither does
// `[0-9a-fA-F]` — the hex atom was already correct.
//
// ⚠ IT DOES NOT APPLY TO `_NEAR_MISS_MARKER`. That pattern is compiled with NO flags
// and scopes its fold to `(?i:OPEN|RESOLVED)`, so its `[A-Za-z0-9_]`, `[^A-Za-z0-9\n]`
// and `[^A-Za-z]` are literally ASCII. Which caller gets which is the `ignoreCase`
// argument threaded through `refRunThenColon`.
func foldsToASCIILetter(r rune) bool {
	// ⚠ SPELLED AS ESCAPES ON PURPOSE. U+212A KELVIN SIGN renders identically to an
	// ASCII `K` in almost every font, so a literal here reads as a no-op clause and is
	// exactly the sort of thing a later "simplification" deletes; U+0130/U+0131 have
	// the same problem against `I` and `i`.
	switch r {
	case '\u0130', // LATIN CAPITAL LETTER I WITH DOT ABOVE
		'\u0131', // LATIN SMALL LETTER DOTLESS I
		'\u017f', // LATIN SMALL LETTER LONG S
		'\u212a': // KELVIN SIGN
		return true
	}
	return false
}

// isWordishUnder is `[A-Za-z0-9_]`, read the way the oracle pattern's flags read it.
func isWordishUnder(r rune, ignoreCase bool) bool {
	return isWordish(r) || (ignoreCase && foldsToASCIILetter(r))
}
func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
