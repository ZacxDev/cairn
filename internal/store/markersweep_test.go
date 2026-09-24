package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// 🔴 THE SWEEP THE NARROWING LEDGER IN `marker.go` POINTS AT. It used to point at
// nothing: that comment closed with *"the sweep below reports 0 divergences across all
// three populations"* and no sweep existed in the tree, in that file or any other — the
// figures came from a script in somebody's scratchpad. Half a closing condition nobody
// can evaluate is worse than none, because it reads as evidence.
//
// WHAT IT MEASURES. `LineMentionsMarker` and `nearMissMarker` are HAND-ROLLED
// transcriptions of two CPython regexes (RE2 has no lookaround, so neither can be
// compiled). Nothing about a transcription is checked by a compiler; a differential run
// over shapes chosen to hit the places the two grammars differ is the only instrument
// that can see a silent narrowing, and a narrowing here is exactly what round 1 and
// round 2 each found one of.
//
// HOW THE ORACLE'S ANSWERS GET HERE. `internal/store/testdata/marker_oracle_sweep.json`
// carries
// the corpus AXES, the SHA-256 of the assembled corpus and the oracle's verdicts as two
// bit-strings. `tests/marker_corpus.py` writes it and `tests/test_marker_oracle_sweep.py`
// re-derives every one of those verdicts from the LIVE `lib/subsystem_resolver` patterns
// on every pytest run — so the fixture cannot drift away from the regexes it claims to
// record. This side assembles the SAME corpus from the SAME axes and checks the SHA-256
// before reading a single verdict: two assemblers in two languages is the seam that
// drifts silently, and without the hash a comfortable zero here would be a zero over a
// corpus the Python side never saw.
//
// ⚠ THE FIXTURE IS NOT A GOLDEN OF THIS PACKAGE'S BEHAVIOUR. It records what CPython
// answers, never what Go answers, so nothing here can agree with a snapshot of itself.

type markerSweepFixture struct {
	Axes struct {
		Prefixes    []string `json:"prefixes"`
		Dates       []string `json:"dates"`
		Words       []string `json:"words"`
		Refs        []string `json:"refs"`
		Terminators []string `json:"terminators"`
		Tail        string   `json:"tail"`
		Extra       []string `json:"extra"`
	} `json:"axes"`
	CorpusLines  int    `json:"corpus_lines"`
	CorpusSHA256 string `json:"corpus_sha256"`
	Anywhere     string `json:"oracle_marker_anywhere"`
	NearMiss     string `json:"oracle_near_miss_marker"`
}

// loadMarkerSweep reads the fixture and rebuilds the corpus from its axes, in the ONE
// order `tests/marker_corpus.py` documents. The nesting is load-bearing: change it and
// the SHA-256 check below fires, which is the point of having one.
func loadMarkerSweep(t *testing.T) (markerSweepFixture, []string) {
	t.Helper()
	// `testdata/`, beside the package, because the `nix build` tier compiles from an
	// allowlisted source copy that does not carry `tests/fixtures/` — measured as a
	// RED sandbox build against a green dev-host one. The path is named in
	// `flake.nix`'s `onlyGo` filter, beside `reader_fixtures.json`, for that reason.
	path := filepath.Join("testdata", "marker_oracle_sweep.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the sweep fixture is unreadable, so this file measures NOTHING: %v", err)
	}
	var fx markerSweepFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("the sweep fixture does not parse: %v", err)
	}
	var lines []string
	for _, prefix := range fx.Axes.Prefixes {
		for _, date := range fx.Axes.Dates {
			for _, word := range fx.Axes.Words {
				for _, ref := range fx.Axes.Refs {
					for _, term := range fx.Axes.Terminators {
						lines = append(lines,
							prefix+date+word+ref+term+fx.Axes.Tail)
					}
				}
			}
		}
	}
	lines = append(lines, fx.Axes.Extra...)

	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	if got := hex.EncodeToString(sum[:]); got != fx.CorpusSHA256 {
		t.Fatalf("this side assembled a DIFFERENT corpus from the same axes:\n"+
			"  go     sha256 %s over %d lines\n"+
			"  python sha256 %s over %d lines\n"+
			"Every verdict below would then be compared against lines nobody ran. "+
			"Re-read the assembly order in tests/marker_corpus.py.",
			got, len(lines), fx.CorpusSHA256, fx.CorpusLines)
	}
	if len(fx.Anywhere) != len(lines) || len(fx.NearMiss) != len(lines) {
		t.Fatalf("verdict strings are %d/%d long over %d lines",
			len(fx.Anywhere), len(fx.NearMiss), len(lines))
	}
	if len(lines) < 1000 {
		t.Fatalf("a %d-line corpus is not a sweep; the axes shrank", len(lines))
	}
	return fx, lines
}

// markerSweepDivergence is one line the two sides disagree about.
type markerSweepDivergence struct {
	line   string
	oracle bool
}

// runMarkerSweep compares one predicate against one bit-string and returns the lines
// they disagree about.
func runMarkerSweep(lines []string, bits string, fn func(string) bool) []markerSweepDivergence {
	var out []markerSweepDivergence
	for i, line := range lines {
		want := bits[i] == '1'
		if fn(line) != want {
			out = append(out, markerSweepDivergence{line: line, oracle: want})
		}
	}
	return out
}

// 🔴 THE POPULATION LABEL IS A SET, NOT A FIRST MATCH, AND THE FIRST DRAFT OF THIS FILE
// GOT IT WRONG IN A WAY THAT PRODUCED A CONFIDENT FALSE DIAGNOSIS. A single corpus line
// can carry several of these axes at once — `- ٢٠٠٠-٠١-٠٤: OPEN pr#12:` carries two —
// and an `if/else if` chain attributes every such line to whichever arm happens to come
// first. Measured: 48 divergences caused by the Arabic-Indic DATE were reported under
// `pr#`, a convergence that was not broken at all and could not have caused them. So
// each line is labelled with every axis it carries, and the label is a diagnosis aid,
// never a causal claim.

// hasFoldingRune is a DIAGNOSIS LABEL: does this line contain one of the four
// non-ASCII runes CPython's `re.IGNORECASE` folds onto an ASCII letter (see
// `foldsToASCIILetter`)?
//
// 🔴 IT IS NOT THE LEDGER'S GATE, AND USING IT AS ONE WAS A SPELLED GUARD. Asking
// whether a line CONTAINS one of four runes passes for any divergence merely SPELLED
// with one, whatever caused it — so a narrowing introduced anywhere on a line
// carrying a long s was absorbed into the declared bucket and the sweep stayed green.
// MEASURED on this corpus: drop the `ignoreCase &&` guard from `terminatorColon` — a
// plausible "simplify" edit on code the previous round introduced — and
// `- Open ſ: ...` diverges from `_NEAR_MISS_MARKER` in the NARROW direction; with the
// gate spelled, the ledger printed `211 on the declared residual, 0 outside it` and
// PASSED. `divergenceIsTheDeclaredFold` is the gate now; this is only what the failure
// message prints, so a human reading one can see which axes were in play.
func hasFoldingRune(line string) bool {
	return strings.ContainsAny(line, "İıſK")
}

// reIFoldsOnto pairs each of `foldsToASCIILetter`'s four runes with the ASCII letter
// CPython's `re.IGNORECASE` folds it onto.
//
// ⚠ SPELLED AS ESCAPES, for the reason `foldsToASCIILetter` gives: U+212A renders
// identically to an ASCII `K` in almost every font, and U+0130/U+0131 to `I`/`i`.
//
// 🔴 THE TARGETS ARE A CLAIM ABOUT CPYTHON, SO THEY ARE MEASURED THERE, NOT ARGUED
// HERE — AND THE MAP ITSELF IS READ OUT OF THIS FILE AND COMPARED THERE, BECAUSE A
// MEASUREMENT AGAINST A SECOND LITERAL IS BLIND TO THIS ONE.
// `tests/test_marker_oracle_sweep.py`'s
// `test_the_GO_fold_target_map_is_pinned_TWO_WAY_against_CPython` sweeps `re.I` on the
// pinned interpreter for each rune's target and requires THIS map — parsed from this
// source file — to equal it, in both directions. Before that existed the Python side
// compared CPython against a dict spelled in the Python file, so a wrong or missing
// target here was invisible to it: MEASURED by mutation, three of these four entries
// SURVIVED every test in the tree.
//
// ⚠ ONLY THE U+017F ENTRY IS REACHABLE FROM THE WALK, AND SAYING SO IS THE POINT.
// `asReadUnderReI` respells the MARKER WORD, and the only letter of `open`/`resolved`
// any of these four folds onto is the `s` of `resolved`. U+0130, U+0131 and U+212A fold
// onto `i`/`i`/`k`, which appear in neither word, so no corpus row can exercise them
// however it is spelled — do not read the map's completeness as coverage. What they are
// for is the two-way pin above: the SET is a claim about the interpreter, and a fifth
// rune, or a moved target, has to fail SOMETHING.
//
// ⚠ `unicode.SimpleFold` IS NOT A SUBSTITUTE. Go's simple-fold orbit connects U+017F
// to `s` and U+212A to `k`, but U+0130 and U+0131 have no simple fold at all — their
// CaseFolding entries are full/Turkic — so a SimpleFold-based widening would silently
// fail to attribute a divergence spelled with either of them, and fail CLOSED-looking:
// the divergence would be reported as undeclared, which is at least the safe direction.
var reIFoldsOnto = map[rune]rune{
	'\u0130': 'i', // LATIN CAPITAL LETTER I WITH DOT ABOVE
	'\u0131': 'i', // LATIN SMALL LETTER DOTLESS I
	'\u017f': 's', // LATIN SMALL LETTER LONG S
	'\u212a': 'k', // KELVIN SIGN
}

// reIFoldedPrefix is `hasFoldedPrefix` as the ORACLE reads a marker word: the ASCII
// fold PLUS the four runes above. It is the declared residual, removed.
func reIFoldedPrefix(rs []rune, want string) bool {
	w := []rune(want)
	if len(rs) < len(w) {
		return false
	}
	for i, c := range w {
		g := rs[i]
		if a, ok := reIFoldsOnto[g]; ok {
			g = a
		} else if g >= 'A' && g <= 'Z' {
			g += 'a' - 'A'
		}
		if g != c {
			return false
		}
	}
	return true
}

// asReadUnderReI respells a line with the DECLARED RESIDUAL REMOVED: a folding rune that
// sits inside a MARKER WORD — a run that folds onto `open` or `resolved` — becomes the
// ASCII letter `re.I` reads it as. Nowhere else.
//
// 🔴 THE SCOPE IS THE FIX, AND RESPELLING THE WHOLE LINE WAS WRONG IN A WAY NO COUNT ON
// THE COMMITTED CORPUS COULD SEE. This function used to say the substitution was "the
// WHOLE of the difference … at the marker word, at the `[A-Za-z0-9_]` lookahead and
// inside the negated `[^A-Za-z0-9\n]` run alike". That is true of `_MARKER_ANYWHERE`,
// compiled `re.IGNORECASE`, and FALSE of `_NEAR_MISS_MARKER`, which carries no flags and
// scopes its fold to `(?i:OPEN|RESOLVED)` — so its two character classes are literally
// ASCII and its terminator run spans `ſ` quite happily (`foldsToASCIILetter`'s own
// comment says this, and is right). Respelling that `ſ` to an `s` STOPPED the run, so a
// divergence whose only cause WAS the declared fold stopped vanishing and got reported as
// `1 divergence(s) outside the ONE declared residual` — a false alarm on the one clause
// that exists to catch a real narrowing. MEASURED on `- Reſolved ſ: …`, now a corpus row
// (`tests/marker_corpus.py`): oracle True, walk false, whole-line respelling false,
// attributed UNDECLARED, the Go sweep RED. No row of that shape existed before, which is
// why `210 / 0` held over a broken attribution.
//
// ⚠ SCOPED TO THE MARKER WORD FOR *BOTH* PROBES, NOT BRANCHED ON THE PATTERN'S FLAGS.
// The declared residual is the same one in both — `hasFoldedPrefix`'s ASCII-only fold of
// the marker word (`marker.go`'s paragraph; `nearMissMarker`'s own ledger) — and every
// other consequence of `re.I` is CLOSED in the walk, so the marker word is the only place
// there is a cause to remove. On `_MARKER_ANYWHERE` the wider respelling was merely
// harmless, never load-bearing: MEASURED, the two scopes attribute that probe's 480
// divergences identically, 480 declared and 0 undeclared either way. Narrower is also the
// safe direction — see `divergenceIsTheDeclaredFold`.
func asReadUnderReI(line string) string {
	rs := []rune(line)
	inWord := make([]bool, len(rs))
	for i := range rs {
		for _, word := range []string{"open", "resolved"} {
			if reIFoldedPrefix(rs[i:], word) {
				for k := i; k < i+len(word); k++ {
					inWord[k] = true
				}
			}
		}
	}
	out := make([]rune, len(rs))
	for i, r := range rs {
		if a, ok := reIFoldsOnto[r]; ok && inWord[i] {
			out[i] = a
			continue
		}
		out[i] = r
	}
	return string(out)
}

// divergenceIsTheDeclaredFold attributes one divergence to the declared residual by
// REMOVING its cause and requiring the divergence to VANISH: re-run the same predicate
// on the line as `re.I` reads it and demand it now agrees with the oracle. A
// divergence that survives the respelling is by construction not the ASCII-only fold,
// whatever runes it happens to be spelled with.
//
// ⚠ STRICTLY NARROWER THAN `hasFoldingRune`, NEVER WIDER: on a line carrying none of
// the four runes the respelling is the identity, so it can never rescue a divergence
// the spelled gate would have rejected. Swapping one for the other can therefore only
// move lines OUT of the declared bucket.
func divergenceIsTheDeclaredFold(fn func(string) bool, d markerSweepDivergence) bool {
	return fn(asReadUnderReI(d.line)) == d.oracle
}

// hasNonASCIIDigit is the population round 1 closed in `refAtomEnds` and round 2 found
// still open in `looksISODate` — kept NAMED so a revert of either is reported as itself
// rather than as an unexplained count.
func hasNonASCIIDigit(line string) bool {
	for _, r := range line {
		if unicode.IsDigit(r) && (r < '0' || r > '9') {
			return true
		}
	}
	return false
}

func hasLowerPR(line string) bool { return strings.Contains(line, "pr#") }

// classify names every declared axis a line carries, joined, so a failure says WHICH
// convergences could be involved instead of printing a number.
func classify(line string) string {
	var axes []string
	if hasFoldingRune(line) {
		axes = append(axes, "re.I-folding rune (U+0130/0131/017F/212A)")
	}
	if hasLowerPR(line) {
		axes = append(axes, "lower-case `pr#`")
	}
	if hasNonASCIIDigit(line) {
		axes = append(axes, "non-ASCII decimal digit")
	}
	if len(axes) == 0 {
		return "UNCLASSIFIED — a line carrying no declared axis at all"
	}
	return strings.Join(axes, " + ")
}

func summarise(divs []markerSweepDivergence) map[string]int {
	counts := map[string]int{}
	for _, d := range divs {
		counts[classify(d.line)]++
	}
	return counts
}

// 🔴 THE LEDGER, IN FOUR CLAUSES. (a) Every divergence between either Go
// transcription and its oracle must be ATTRIBUTABLE to the one residual `marker.go`
// declares — the ASCII-only fold — by `divergenceIsTheDeclaredFold`, which removes
// that cause and requires the divergence to vanish. (b) Every divergence must be
// ORACLE-WIDER: the reverse is the dangerous direction, because a
// walk wider than its oracle manufactures a declaration nobody typed, and two such
// populations were live and undeclared until this sweep existed. (c) The residual must
// be NON-EMPTY. (d) It must be EXACTLY the size the ledger records, per probe.
//
// ⚠ (c) IS NOT PEDANTRY. A sweep reporting zero everywhere is indistinguishable from a
// sweep wired to nothing — an axis silently dropped from the corpus, a bit-string read
// off the wrong key — so the pair this test reports is "N on the declared residual, 0
// outside it", never a bare zero. `TestTheMarkerSweepCanReportANonZero` is the other
// half of the same argument: it proves the instrument goes red on a narrowing that
// really shipped.
//
// ⚠ WHEN THE RESIDUAL CLOSES, (c) IS WHAT FAILS, AND THAT IS THE DESIGNED EXIT. The
// message says so: delete the clause here and the paragraph in `marker.go` together.
//
// ⚠ (d) IS A LITERAL, AND A LITERAL IS BRITTLE ON PURPOSE HERE. `> 0` let the
// residual GROW silently: the spelled gate above absorbed a whole extra divergence from
// a mutant — 210 to 211 on the corpus as it then stood, one row shorter than today's —
// and the run stayed green, because nothing in the tree
// pinned either number. A count that must be re-derived when the corpus moves is the
// cost of a residual that cannot grow behind a passing test. Do NOT do arithmetic on
// the two sides when this fails — re-run the sweep and copy the number it prints.
func TestTheGoMarkerTranscriptionsMatchTheOracleExceptTheDeclaredResidual(t *testing.T) {
	const residual = "re.I-folding rune (U+0130/0131/017F/212A)"
	fx, lines := loadMarkerSweep(t)
	for _, probe := range []struct {
		name string
		bits string
		fn   func(string) bool
		// want is clause (d): the residual's EXACT size on this corpus, measured by
		// this test and copied from what it prints.
		want int
	}{
		{"LineMentionsMarker vs _MARKER_ANYWHERE", fx.Anywhere, LineMentionsMarker, 480},
		// 210 -> 211: one corpus row added, `- Reſolved ſ: …`, the shape that made
		// the whole-line respelling misattribute. See `asReadUnderReI`.
		{"nearMissMarker vs _NEAR_MISS_MARKER", fx.NearMiss, nearMissMarker, 211},
	} {
		divs := runMarkerSweep(lines, probe.bits, probe.fn)
		declared, undeclared := 0, 0
		for _, d := range divs {
			if divergenceIsTheDeclaredFold(probe.fn, d) {
				declared++
			} else {
				undeclared++
			}
			if !d.oracle {
				t.Errorf("%s: this walk is WIDER than the oracle on %q [%s] — the "+
					"dangerous direction, which manufactures a declaration nobody "+
					"typed", probe.name, d.line, classify(d.line))
			}
		}
		if undeclared > 0 {
			t.Errorf("%s: %d divergence(s) outside the ONE declared residual %q.\n"+
				"counts by axis: %v", probe.name, undeclared, residual, summarise(divs))
		}
		if declared == 0 {
			t.Errorf("%s: the declared residual reported ZERO divergences over %d "+
				"lines. Either it CLOSED — in which case delete this clause and the "+
				"residual paragraph in marker.go in the same change — or this sweep "+
				"is measuring nothing, which reads identically.",
				probe.name, len(lines))
		} else if declared != probe.want {
			t.Errorf("%s: the declared residual is %d divergence(s), and this ledger "+
				"records %d. It MOVED, and a residual that moves silently is the whole "+
				"failure this clause exists for. If the corpus changed, put %d here and "+
				"say in the commit which lines were added or dropped; if it did not, "+
				"something in the walk did. counts by axis: %v",
				probe.name, declared, probe.want, declared, summarise(divs))
		}
		t.Logf("%s: %d divergence(s) over %d lines — %d on the declared residual, "+
			"%d outside it", probe.name, len(divs), len(lines), declared, undeclared)
	}
}

// narrowedMentionsMarker is `LineMentionsMarker` with `foldPR` reverted to the value it
// carried before round 1 — the REAL defect, not a synthetic one. It exists so the sweep
// above has a positive control built from a narrowing that actually shipped.
func narrowedMentionsMarker(line string) bool {
	rs := []rune(line)
	for i := range rs {
		for _, word := range []string{"open", "resolved"} {
			if !hasFoldedPrefix(rs[i:], word) {
				continue
			}
			end := i + len(word)
			if end < len(rs) && isWordish(rs[end]) {
				continue
			}
			if refRunThenColon(rs, end, map[int]bool{}, false) {
				return true
			}
		}
	}
	return false
}

// 🔴 THE POSITIVE CONTROL ON THE INSTRUMENT. A differential sweep that reports a
// comfortable count is worth nothing until it has been watched to report a LARGER one
// for a narrowing it must catch. This feeds it the exact regression round 1 closed —
// `foldPR` false on the `_MARKER_ANYWHERE` side — and requires the `pr#` population to
// appear. Without this the ledger above would pass unchanged against a corpus whose
// `pr#` rows had quietly stopped being generated.
func TestTheMarkerSweepCanReportANonZero(t *testing.T) {
	fx, lines := loadMarkerSweep(t)
	counts := summarise(runMarkerSweep(lines, fx.Anywhere, narrowedMentionsMarker))
	pr := counts["lower-case `pr#`"]
	if pr == 0 {
		t.Fatalf("the control narrowing produced NO `pr#` divergence, so a zero from "+
			"the real sweep is a claim about nothing. counts=%v", counts)
	}
	t.Logf("control: %d `pr#` divergence(s) — the sweep CAN report non-zero", pr)
}
