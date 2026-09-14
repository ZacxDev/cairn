package report

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ZacxDev/cairn/internal/pytext"
	"github.com/ZacxDev/cairn/internal/store"
)

// --- Search tuning. Every number here is gated by the oracle's labelled
// query→expected-hunk fixture corpus; none of them is taste. -------------------------

const (
	// fuzzyFloor — below this, a `difflib` ratio is not evidence of a typo. 0.82 and not
	// 0.80 because 0.80 is exactly where a one-character SUBSTITUTION in a five-letter
	// word lands — and so does the ordinary English pair `probe`/`prone`, which is not a
	// typo of anything. Every typo class in the fixture set clears 0.88.
	fuzzyFloor = 0.82

	// minInexactLen — 🔴 ONE length rule, not three. A token shorter than this must match
	// EXACTLY: no prefix rule, no substring rule, no fuzzy rule. Short tokens are where
	// every inexact rule turns into noise (`pod` prefixes `podman`, `pgo`, `podinfo`), and
	// a reader cannot tell a noisy hit from a real one once it is on the screen.
	minInexactLen = 4

	// What an inexact match is WORTH, so a weak hit prints as visibly weak. Ordered, and
	// each strictly below 1.0 — an exact token match must always outrank them.
	prefixStrength    = 0.92
	substringStrength = 0.85
)

// The two values of a hunk's `basis`, sharing no spelling, printed per hunk:
//
//	line        this block itself cleared the threshold.
//	entry-name  the ENTRY's ref/aliases cleared it and no block did, so the entry's best
//	            block is shown as the worked example. Without this value a name-only hit
//	            is indistinguishable from a no-match.
const (
	BasisLine      = "line"
	BasisEntryName = "entry-name"
)

// pySpaceClass and pyNonSpaceClass are `\s` and `\S` FOR A PYTHON `str` PATTERN, spelled
// out as code points.
//
// 🔴 GO's `\s` IS `[\t\n\f\r ]` — FIVE CHARACTERS — AND PYTHON's IS TWENTY-NINE. A
// transcribed `\s` would silently stop matching on a NO-BREAK SPACE, an OGHAM SPACE MARK
// or any of the four C0 information separators, which is a heading that stops being a
// heading and a bullet that stops being a bullet. The set is `pytext.IsSpace`'s, measured
// exhaustively against the pinned interpreter; it is written twice here because a regexp
// character class cannot call a function, and the two spellings are the same 29 code
// points in the same order so a reader can diff them by eye.
const (
	pySpaceClass    = `[\t\n\v\f\r \x{1c}-\x{1f}\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]`
	pyNonSpaceClass = `[^\t\n\v\f\r \x{1c}-\x{1f}\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]`
)

// headingRe and bulletRe are `entryBlocks`' OWN notions of a heading and a bullet.
//
// ⚠ `headingRe` IS NARROWER THAN `store.HeadingBlocks`' `#`-PREFIX TEST, AND BOTH ARE THE
// ORACLE'S. Seven hashes match neither `#{1,6}` nor a following space, and `## ` with
// nothing after it fails `(.*\S)` — so each is ordinary text HERE and a real heading
// THERE. Two parsers, two questions; unifying them would be a behaviour change in output
// that is pinned byte for byte.
//
// ⚠ `\z` RATHER THAN `$`: Python's `$` also matches just before a trailing newline, and Go's
// `$` (without `(?m)`) does not. The input is one line from `splitlines`, which can carry
// no line break at all, so the two are the same here — `\z` states which reading is meant.
var (
	headingRe = regexp.MustCompile(`^(#{1,6})` + pySpaceClass + `+(.*` + pyNonSpaceClass + `)` + pySpaceClass + `*\z`)
	bulletRe  = regexp.MustCompile(`^` + pySpaceClass + `*[-*+]` + pySpaceClass + `+` + pyNonSpaceClass)
)

// Tokenize is lowercase alphanumeric runs, in order. THE one tokenizer.
//
// Punctuation is a separator, so `rate-limit`, `rate_limit` and `rate limit` all tokenize
// identically — which is why the compound handling below only has to solve the
// CONCATENATED spelling (`ratelimit`) and not four punctuation variants.
//
// 🔴 THE LOWERCASE IS `pytext.Lower`, NOT `strings.ToLower`, and the difference is one
// code point that is reachable from a request: U+0130 lowercases to TWO code points in
// Python and one in Go.
func Tokenize(text string) []string {
	lowered := pytext.Lower(text)
	var out []string
	start := -1
	for i, r := range lowered {
		alnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if alnum {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			out = append(out, lowered[start:i])
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, lowered[start:])
	}
	return out
}

// candidateTokens is every token a unit's TEXT offers as a match candidate: its own
// tokens, PLUS every adjacent pair joined WITHIN one clause.
//
// 🔴 THE JOIN IS THE COMPOUND-TERM FIX, and it is deliberate rather than a lowered cutoff.
// `ratelimit` never clears a fuzzy threshold against `rate` or `limit` — it is not a typo
// of either — so the corpus side grows the concatenation and the match becomes EXACT
// (1.00) instead of fuzzy-and-arguable. The other direction (`rate limit` searched against
// a corpus that writes `ratelimit`) is covered by pairStrength's prefix and substring rungs.
//
// 🔴 IT TAKES TEXT AND NOT TOKENS, because the clause boundary is exactly the information
// tokenizing throws away — and the join is unsafe without it. A bullet of the shape "drain
// that node, port-forward the socket" joins `node`+`port` across the comma and scores a
// PERFECT 1.00 for `nodeport` in an entry that never says the word — two facts glued into
// a term neither of them spells. A 1.00 with no evidence behind it is the worst shape this
// scorer can emit, because it out-ranks every genuine match on the page. Taking text also
// removes the bypass: a caller cannot tokenize first and lose the guard.
//
// Adjacent PAIRS only. Triples were not added: nothing in the corpus needs them, and each
// extra join widens the false-match surface.
//
// Plain tokens first, then the joins — scoreUnit scans in order and stops at 1.00, so this
// keeps a whole-token exact hit cheaper than a joined one.
func candidateTokens(text string) []string {
	var plain, joined []string
	for _, clause := range splitClauses(text) {
		toks := Tokenize(clause)
		plain = append(plain, toks...)
		for i := 0; i+1 < len(toks); i++ {
			joined = append(joined, toks[i]+toks[i+1])
		}
	}
	return append(plain, joined...)
}

// splitClauses is `re.split(r"[,;:!?()\[\]]|\.(?=\s|$)", text)`.
//
// The punctuation that ENDS a compound instead of spelling one. `-`, `_` and a plain space
// are deliberately ABSENT: those are the three ways the store actually writes one compound
// term, and Tokenize already folds them together.
//
// 🔴 A `.` COUNTS ONLY AT A SENTENCE END — followed by whitespace or end-of-text. A dotted
// identifier (`activity.events`, `nginx.conf`, a dotted config key) is a single term whose
// halves must keep joining, and a rule that broke on every `.` would silently stop
// reaching them. That trailing condition is a lookahead, which is why this is a scan
// rather than a regexp.
func splitClauses(text string) []string {
	var out []string
	runes := []rune(text)
	start := 0
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		isBreak := false
		switch r {
		case ',', ';', ':', '!', '?', '(', ')', '[', ']':
			isBreak = true
		case '.':
			isBreak = i+1 == len(runes) || pytext.IsSpace(runes[i+1])
		}
		if !isBreak {
			continue
		}
		out = append(out, string(runes[start:i]))
		start = i + 1
	}
	return append(out, string(runes[start:]))
}

// pairStrength is how strongly one query token `q` matches one candidate token `t`, in
// [0, 1]. Each rung is strictly below the one above, so an exact match always wins and a
// weak hit PRINTS as weak:
//
//	1.00  identical
//	0.92  the candidate EXTENDS the query   (`postgres` → `postgresql`)
//	0.85  the candidate CONTAINS the query  (`limit` → `ratelimit`)
//	ratio a `difflib` ratio ≥ fuzzyFloor    (`conection` → `connection`, 0.95)
//	0.00  otherwise
//
// 🔴 THE TWO MIDDLE RUNGS ARE DIRECTIONAL, AND THAT IS THE WHOLE POINT. They ask whether
// the candidate spells MORE than the query, never the reverse. A symmetric rule ("either
// is a prefix of the other") scores a candidate that is a FRAGMENT of the query just as
// highly, and because scoreUnit is a mean over the QUERY's tokens, a single-token query
// then takes FULL coverage from one incidental short word: `logrotate` scores 0.85 off a
// bare `rotate`, `kubeconfig` 0.92 off `kube`. The symmetric form put a MAJORITY of every
// above-threshold hunk on the screen for queries whose word appeared nowhere in them, each
// wearing a 0.85 or 0.92 that nothing in the entry justified.
//
// 🔴 Tokens shorter than minInexactLen take the first rung or nothing. ONE length rule
// guarding all three inexact rungs, not three. A length RATIO floor was considered as a
// SECOND guard and REJECTED on measurement: once the rungs are directional it moves almost
// nothing, while at 0.5 it refuses `rate` → `ratelimit` (4/9), a case this design names as
// motivating.
//
// The `difflib` call is gated on a LENGTH WINDOW as well as on the floor: a ratio can only
// reach fuzzyFloor when the lengths are close, so the window changes no answer and keeps a
// full-store scan in the millisecond range.
//
// ⚠ THE LENGTHS ARE CODE POINTS, NOT BYTES. `len(q)` in Python counts characters, and
// tokens are `[a-z0-9]+` so the two agree for every token this scorer is handed — but the
// comparison is written in code points because the day a tokenizer widens is the day a
// byte length silently changes which rung fires.
func pairStrength(q, t string) float64 {
	if q == t {
		return 1.0
	}
	qr, tr := []rune(q), []rune(t)
	if len(qr) < minInexactLen || len(tr) < minInexactLen {
		return 0.0
	}
	if strings.HasPrefix(t, q) {
		return prefixStrength
	}
	if strings.Contains(t, q) {
		return substringStrength
	}
	if abs(len(qr)-len(tr)) > 2 {
		return 0.0
	}
	r := ratio(qr, tr)
	if r >= fuzzyFloor {
		return r
	}
	return 0.0
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// scoreUnit is COVERAGE of the query by the unit: the mean best strength per query token.
//
// 🔴 A MEAN AND NOT A MAX, which is the whole reason an absent term is observable.
// `nginx zzzz` against a block that says `nginx` scores 0.50 and does not clear the default
// threshold, so a query whose second word appears nowhere returns nothing rather than the
// first word's hits wearing the second word's authority. A max would have made every
// multi-token query as loose as its loosest word.
//
// An empty query scores 0 everywhere rather than matching everything: "the user asked for
// nothing" and "everything matches" are different answers and only one of them is honest.
//
// 🔴 THE CANDIDATE SIDE IS TEXT, NOT TOKENS. candidateTokens needs the clause boundaries to
// decide what it may join, so handing it a pre-tokenized sequence would silently disable
// that guard at whichever call site did it. There is one way in, and it carries the
// punctuation.
func scoreUnit(queryTokens []string, unitText string) float64 {
	if len(queryTokens) == 0 {
		return 0.0
	}
	cands := candidateTokens(unitText)
	if len(cands) == 0 {
		return 0.0
	}
	total := 0.0
	for _, q := range queryTokens {
		best := 0.0
		for _, t := range cands {
			s := pairStrength(q, t)
			if s > best {
				best = s
				if best == 1.0 {
					break
				}
			}
		}
		total += best
	}
	return total / float64(len(queryTokens))
}

// Block is one searchable unit of an entry body: a bullet with its continuation lines, or a
// paragraph, ALWAYS under a named section.
//
// 🔴 THE UNIT IS A BLOCK, NOT A LINE, and that is what makes multi-token queries work at
// all. The store is written in bullets that wrap: `nginx` on the first line and
// `rate-limit` on its continuation is ONE fact, and a per-line scorer gives each half 0.50
// and returns nothing — the exact "returns NOTHING" failure a per-line prototype produced.
type Block struct {
	Section string
	// Start is the 1-based line number of the block's first line, in the entry FILE.
	Start int
	Lines []string
}

func (b Block) End() int     { return b.Start + len(b.Lines) - 1 }
func (b Block) Text() string { return strings.Join(b.Lines, "\n") }

// entryBlocks splits an entry body into searchable blocks, each under its section.
//
// 🔴 EVERYTHING BEFORE THE FIRST HEADING IS SKIPPED, and that is the front-matter exclusion
// — expressed as a property of the OUTPUT rather than as a second front-matter parser. A
// hunk has to name its section, so text with no section cannot be a hunk; front matter,
// which carries no `##`, falls out for free. That matters: a prototype that folded slug
// tokens into every line ranked the `---` fence line first.
//
// Headings themselves are not blocks — a query matching the literal words
// "Nuance / work-history" would otherwise hit every entry in the store.
//
// Fenced code is kept whole: a fence's contents are not bullets even when they begin with
// `-`, and splitting a snippet mid-command emits a fragment that reads like a complete
// instruction.
func entryBlocks(text string) []Block {
	var blocks []Block
	section := ""
	haveSection := false
	var cur []string
	start := 0
	inFence := false

	flush := func() {
		if len(cur) > 0 && haveSection {
			lines := make([]string, len(cur))
			copy(lines, cur)
			blocks = append(blocks, Block{Section: section, Start: start, Lines: lines})
		}
		cur = nil
		start = 0
	}

	for i, line := range pytext.SplitLines(text) {
		n := i + 1
		if store.IsFence(line) {
			inFence = !inFence
			if len(cur) == 0 {
				start = n
			}
			cur = append(cur, line)
			continue
		}
		if inFence {
			cur = append(cur, line)
			continue
		}
		if headingRe.MatchString(line) {
			flush()
			// `heading.group(0).strip()` — the pattern is anchored at both ends, so group 0
			// IS the whole line and the strip is `str.strip()`.
			section, haveSection = pytext.StripWhitespace(line), true
			continue
		}
		if pytext.StripWhitespace(line) == "" {
			flush()
			continue
		}
		if bulletRe.MatchString(line) {
			flush()
		}
		if len(cur) == 0 {
			start = n
		}
		cur = append(cur, line)
	}
	flush()
	return blocks
}

// Hunk is one match, carrying EVERYTHING needed to read it safely on its own.
//
// 🔴 THE LABELS TRAVEL WITH THE CONTENT. The caveat prints once per invocation and
// sensitivity is a per-entry fact, but hunk output INTERLEAVES entries — so a `scope/ref` or
// a `sensitivity=` printed once at the top would end up describing somebody else's lines by
// the time a reader reaches them. Every hunk therefore restates its scope, ref, file, line,
// section and sensitivity.
type Hunk struct {
	Scope    string
	Ref      string
	Filename string

	Sensitivity string
	// DeclaredSensitivity is carried onto the HUNK and not just the entry: hunk output
	// interleaves entries, so an override noted anywhere but on the hunk itself is an
	// override the reader never sees next to the lines it governs.
	DeclaredSensitivity string

	Section string
	Start   int
	Lines   []string
	Score   float64
	Basis   string

	// NameScore is how well the query matched this entry's ref/aliases. A TIE-BREAK ONLY
	// — it is deliberately NOT folded into Score, which stays a claim about the printed
	// lines and nothing else.
	//
	// 🔴 It exists because a one-word query for a subsystem's NAME hits that subsystem's
	// own entry and half a dozen passing mentions elsewhere, ALL at 1.00, and an
	// alphabetical tie-break then puts the passing mentions first — "ranks noise above
	// signal" with a perfect-looking score beside it. Adding it to Score instead would have
	// printed a 1.15, or a number nothing on the screen explains.
	NameScore float64
}

func (h Hunk) End() int { return h.Start + len(h.Lines) - 1 }

// SearchReport is one deterministic answer to "where does the index say anything about X?".
type SearchReport struct {
	Status    string
	Scope     string
	StoreRoot string
	Query     string
	Threshold float64
	Context   int

	Hunks []Hunk
	// TotalHits is hunks that cleared the threshold, BEFORE MaxHits. The truncation
	// discriminator.
	TotalHits int
	MaxHits   int

	EntriesSearched int
	ScopesSearched  []string
	KnownScopes     []string

	// BestBelow is the `(ref, score)` of the best hunk that did NOT clear the threshold.
	//
	// 🔴 THIS IS WHAT MAKES A ZERO READABLE. An empty result cannot distinguish two
	// mechanisms: "the query matched nothing anywhere" and "the best candidate scored 0.50
	// against a threshold of 0.60" are different facts with different next actions — lower
	// the threshold, or rephrase — and a searcher that prints the same blank for both has
	// diagnosed nothing.
	BestBelow *BestBelow
	Malformed []store.MalformedEntry
	// MalformedElsewhere is empty under all-scopes, which searches everything the (narrowed)
	// index holds.
	MalformedElsewhere []store.MalformedEntry
}

// BestBelow is the near miss, with its score already rounded the way the oracle rounds it.
type BestBelow struct {
	Ref   string
	Score float64
}

func (r SearchReport) Omitted() int {
	if n := r.TotalHits - len(r.Hunks); n > 0 {
		return n
	}
	return 0
}

// Label is the searched scopes, in prose. ONE spelling, shared with the caveat.
func (r SearchReport) Label() string { return ScopeLabel(r.ScopesSearched) }

// Caveat hands the label over UNQUOTED — CaveatText owns the backticks, so quoting here too
// would print them twice, which is exactly the kind of near-miss a second spelling of one
// string produces.
//
// 🔴 AN EMPTY BADGE SET, AND THAT IS A CLAIM ABOUT THIS RENDERER RATHER THAN A SHORTCUT:
// search prints Hunks — matched excerpts — and never an index row, so no badge can appear
// in its output and no badge explanation can apply to it. If search ever grows an
// index-row view, compute the set here. It is `noBadges()` and not `nil`, and the
// difference is the whole contract — see noBadges.
func (r SearchReport) Caveat() string { return CaveatText(r.Label(), noBadges()) }

// Search finds HUNKS matching a query. READ-ONLY, nothing is spawned.
//
// 🔴 IT SHELLS OUT TO NOTHING, and that is a measurement rather than a preference. The
// whole store is tens of kilobytes across tens of files; a pure scan of it takes
// single-digit milliseconds, so an external matcher would buy no speed while costing a
// binary that is not guaranteed on PATH and an output format nobody pinned.
//
// 🔴 THE ALL-SCOPES PATH IS THE REASON VISIBILITY IS AN INDEX FILTER AND NOT A PER-SCOPE
// REFUSAL CHECK. `all_scopes=1` names NO scope, so there is nothing for such a check to
// refuse — it would search the CONTENT of every scope in the store and report hits from
// scopes the caller cannot name. Narrowing the index instead makes the store-wide search
// store-wide over what the caller may see and nothing else.
func Search(storeRoot string, opts SearchOptions, visible store.ScopeSet) (SearchReport, error) {
	index, err := store.LoadStore(storeRoot, "searched", visible)
	if err != nil {
		return SearchReport{}, err
	}

	base := SearchReport{
		Scope:       store.NormalizeRef(opts.Scope),
		StoreRoot:   storeRoot,
		Query:       opts.Query,
		Threshold:   opts.Threshold,
		Context:     opts.Context,
		MaxHits:     opts.MaxHits,
		KnownScopes: index.Scopes(),
	}

	var scopes []string
	switch {
	case opts.AllScopes:
		scopes = index.Scopes()
	case !index.HasScope(opts.Scope):
		// Asked of the index's own scope set rather than by catching the loader's error:
		// there is ONE place an unknown scope is turned into an error in this package
		// (Recall's), and a second copy of that catch is a second place for the two to
		// disagree about what an unknown scope means.
		out := base
		out.Status = StatusScopeAbsent
		out.Malformed = index.MalformedIn(opts.Scope)
		out.MalformedElsewhere = index.MalformedOutside([]string{opts.Scope})
		return out, nil
	default:
		scopes = []string{store.NormalizeRef(opts.Scope)}
	}

	// Derived AFTER `scopes` is settled, because all-scopes changes what "here" means: with
	// it, every reject in the (narrowed) index is in the searched set and nothing is
	// elsewhere. Deriving it once above would have reported a store-wide scan's own broken
	// entries as somebody else's problem.
	searchedSet := map[string]bool{}
	for _, s := range scopes {
		searchedSet[store.NormalizeRef(s)] = true
	}
	var bad []store.MalformedEntry
	for _, m := range index.Malformed {
		if searchedSet[m.Scope] {
			bad = append(bad, m)
		}
	}
	badElsewhere := index.MalformedOutside(scopes)

	queryTokens := Tokenize(opts.Query)
	var cleared, below []Hunk
	searched := 0
	for _, sc := range scopes {
		entries, entriesErr := index.Entries(sc)
		if entriesErr != nil {
			return SearchReport{}, entriesErr
		}
		ordered := make([]store.Entry, len(entries))
		copy(ordered, entries)
		sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Ref() < ordered[j].Ref() })
		for _, entry := range ordered {
			searched++
			hits, misses, hunkErr := entryHunks(storeRoot, entry, queryTokens, opts)
			if hunkErr != nil {
				return SearchReport{}, hunkErr
			}
			cleared = append(cleared, hits...)
			below = append(below, misses...)
		}
	}

	// THE ONE ordering site for hunks: score descending, then the entry-name tie-break, then
	// scope/ref/line ascending so two runs over an unchanged store produce identical bytes.
	sort.SliceStable(cleared, func(i, j int) bool {
		a, b := cleared[i], cleared[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.NameScore != b.NameScore {
			return a.NameScore > b.NameScore
		}
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		if a.Ref != b.Ref {
			return a.Ref < b.Ref
		}
		return a.Start < b.Start
	})

	out := base
	// 🔴 `search-unreadable` OUTRANKS `search-no-match`, AND THE DISCRIMINATOR IS
	// "NOTHING WAS SEARCHED", not "nothing cleared". A query that ran over zero readable
	// entries produced a zero that says nothing about the query, and the no-match branch
	// would have printed "searched 0 entries … nothing cleared the threshold" — technically
	// true, and read by everyone as "the store has nothing on this".
	switch {
	case searched == 0 && len(bad) > 0:
		out.Status = StatusSearchUnreadable
	case len(cleared) > 0:
		out.Status = StatusSearchHit
	default:
		out.Status = StatusSearchNoMatch
	}
	if opts.AllScopes {
		out.Scope = "(all scopes)"
	}
	out.Hunks = cleared
	if opts.MaxHits < len(out.Hunks) {
		out.Hunks = out.Hunks[:opts.MaxHits]
	}
	out.TotalHits = len(cleared)
	out.EntriesSearched = searched
	out.ScopesSearched = scopes
	out.Malformed = bad
	out.MalformedElsewhere = badElsewhere
	if worst := worstBelow(below); worst != nil {
		out.BestBelow = &BestBelow{Ref: worst.Ref, Score: pyRound(worst.Score, 3)}
	}
	return out, nil
}

// worstBelow is `max(below, default=None, key=lambda h: (h.score, -h.start))` — the FIRST
// element achieving the maximum, which is what CPython's `max` returns and therefore what
// decides the reported near miss when two sub-threshold hunks tie.
func worstBelow(below []Hunk) *Hunk {
	var best *Hunk
	for i := range below {
		h := &below[i]
		if best == nil || h.Score > best.Score || (h.Score == best.Score && -h.Start > -best.Start) {
			best = h
		}
	}
	return best
}

// entryHunks is every hunk ONE entry offers, split into (cleared, below).
//
// Stage 2 of the two-stage scorer. Stage 1 — does the entry qualify at all — is the
// caller's, because it needs the NAME score and the best block score together.
func entryHunks(storeRoot string, entry store.Entry, queryTokens []string, opts SearchOptions) (cleared, below []Hunk, err error) {
	recalled, err := ReadEntry(storeRoot, entry)
	if err != nil {
		return nil, nil, err
	}
	path := filepath.Join(storeRoot, entry.Scope, entry.Filename)
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		// ⚠ THE ORACLE DOES NOT GUARD THIS SECOND READ, so an OSError here escapes as an
		// unhandled read failure rather than as the reader's own sentence. Wrapped in the
		// SAME sentence ReadEntry above would have produced, because the honest statement
		// about a file that vanished between two reads is "the store was not fully read" —
		// and a bare error would reach the route's generic 500 instead of the 503 that says
		// the store was not read.
		return nil, nil, store.EntryUnreadable(path, readErr)
	}
	text := store.DecodeReplace(data)
	raw := pytext.SplitLines(text)

	nameParts := append([]string{entry.Ref(), entry.Slug}, entry.Aliases...)
	nameScore := scoreUnit(queryTokens, strings.Join(nameParts, " "))

	makeHunk := func(block Block, score float64, basis string) Hunk {
		lines, start := block.Lines, block.Start
		if opts.Context != ContextBullet {
			// `?context=N` — N RAW lines either side of the block, clamped to the file.
			// Deliberately raw: the caller asked for a window, and a window that quietly
			// snapped back to a bullet would not be one.
			lo := block.Start - opts.Context
			if lo < 1 {
				lo = 1
			}
			hi := block.End() + opts.Context
			if hi > len(raw) {
				hi = len(raw)
			}
			lines, start = windowLines(raw, lo, hi), lo
		}
		return Hunk{
			Scope:               entry.Scope,
			Ref:                 entry.Ref(),
			Filename:            entry.Filename,
			Sensitivity:         recalled.Sensitivity,
			DeclaredSensitivity: recalled.DeclaredSensitivity,
			Section:             block.Section,
			Start:               start,
			Lines:               lines,
			Score:               score,
			Basis:               basis,
			NameScore:           nameScore,
		}
	}

	type scoredBlock struct {
		score float64
		block Block
	}
	blocks := entryBlocks(text)
	scored := make([]scoredBlock, 0, len(blocks))
	for _, b := range blocks {
		scored = append(scored, scoredBlock{scoreUnit(queryTokens, b.Text()), b})
	}
	for _, sb := range scored {
		if sb.score >= opts.Threshold {
			cleared = append(cleared, makeHunk(sb.block, sb.score, BasisLine))
		}
	}
	if len(cleared) > 0 {
		return cleared, nil, nil
	}

	// `max(scored, default=None, key=lambda sb: (sb[0], -sb[1].start))` — first maximal.
	var best *scoredBlock
	for i := range scored {
		sb := &scored[i]
		if best == nil || sb.score > best.score ||
			(sb.score == best.score && -sb.block.Start > -best.block.Start) {
			best = sb
		}
	}
	if best == nil || (best.score <= 0.0 && nameScore < opts.Threshold) {
		// 🔴 A ZERO-SCORING BLOCK IS NOT A NEAR MISS. Reporting it as one would make
		// BestBelow say "the closest candidate scored 0.00", which reads as a weak match and
		// is really an ABSENT TERM — the two need different next actions (lower the
		// threshold vs. rephrase), so they must not collapse into one sentence.
		return nil, nil, nil
	}
	if nameScore >= opts.Threshold {
		// 🔴 THE NAME-ONLY HIT. The entry IS the answer — its ref or an alias is what was
		// searched for — and no single block happened to clear. Emitting nothing here is
		// what made a per-line prototype return NOTHING for a query that named an entry
		// outright. The basis says which selector fired, so a name hit is never mistaken for
		// a content hit.
		return []Hunk{makeHunk(best.block, nameScore, BasisEntryName)}, nil, nil
	}
	return nil, []Hunk{makeHunk(best.block, best.score, BasisLine)}, nil
}

// windowLines is `raw[lo-1:hi]` with 1-based inclusive bounds, copied so a hunk never aliases the
// file's line slice.
func windowLines(raw []string, lo, hi int) []string {
	if lo > hi {
		return nil
	}
	out := make([]string, hi-lo+1)
	copy(out, raw[lo-1:hi])
	return out
}

// pyRound is CPython's `round(float, ndigits)`.
//
// 🔴 IT IS "FORMAT CORRECTLY TO N PLACES, THEN PARSE BACK", NOT `math.Round(x*1000)/1000`.
// The multiply-and-divide form introduces its own rounding error and disagrees with CPython
// on ordinary inputs; CPython formats with a correctly-rounded decimal conversion and
// re-parses. Go's `strconv` does the same conversion with the same tie rule (ties to even),
// which is why this composition is the port rather than an approximation.
//
// It matters because the value is then formatted AGAIN with two decimals, and a double
// rounding is exactly where the two implementations would part company on a `.xx5` boundary.
func pyRound(x float64, digits int) float64 {
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(x, 'f', digits, 64), 64)
	if err != nil {
		// Unreachable: FormatFloat's output always parses. Returning the input unrounded
		// is the degradation that keeps a report coming rather than failing a read over a
		// number's last decimal place.
		return x
	}
	return rounded
}
