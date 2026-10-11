package redact

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// Realistic generator families; the value is "caught" only if no 6-char window of it survives.
// TestRedactorRecallOnRealisticPasswords is the rate test: 200 values per generator per line
// shape. At 2ba3e5c `notCode` caught 13–24/200 of symbol-bearing passwords and 0/200 dotted
// passphrases. The floors below are what this build measured at seed 4, minus nothing — see
// [rateFloor] for what the residual is.
func TestRedactorRecallOnRealisticPasswords(t *testing.T) {
	for _, c := range rateCells(4) {
		t.Logf("%-20s %-28s %d/200", c.gen, c.shape, c.caught)
		if floor := rateFloor(c.gen, c.shape); c.caught < floor {
			t.Errorf("%s / %s: caught %d/200, floor %d", c.gen, c.shape, c.caught, floor)
		}
	}
}

// rateGens and rateShapes are the rate test's generators and line shapes, in measurement order.
var (
	rateGens = []string{"alnum-32", "pm-20-with-symbols", "pm-16-3symbols", "pm-16-lead-symbol", "base64-32B",
		"hex-64", "diceware-dots", "diceware-dash-digit"}
	rateShapes = []string{"DB_PASSWORD=%s", "export API_SECRET=%s", "password: %s", "host=db password=%s dbname=x"}
)

type rateCell struct {
	gen, shape string
	caught     int
}

// rateCells measures every generator in every shape, 200 values each, at one seed. The SEED is a
// parameter because the README states the measured range over NAMED seeds, and
// `TestTheRateRangeHoldsOverSeedsFourToEight` re-measures it there.
func rateCells(seed uint64) []rateCell {
	r2seed(seed)
	r, _ := New(bytes.Repeat([]byte{7}, 32), nil)
	// symbols a password manager draws from, minus the ones the dotenv value class already
	// stops at (# , ' ") so the measurement isolates the Accept filter.
	pmSyms := "!@$%^&*()-_=+[]{};:.<>?/|~"
	gens := map[string]func() string{
		"alnum-32":           func() string { return r2pick(r2alnum, 32) },
		"pm-20-with-symbols": func() string { return r2pick(r2alnum+pmSyms, 20) },
		"pm-16-3symbols":     func() string { return r2pick(r2alnum, 13) + r2pick(pmSyms, 3) },
		// A LEADING symbol (round 4 refused `*…`, `&…` and `%verb…` as code in every notation).
		"pm-16-lead-symbol": func() string { return r2pick(pmSyms, 1) + r2pick(r2alnum+pmSyms, 15) },
		"base64-32B":        func() string { return base64.StdEncoding.EncodeToString([]byte(r2pick(r2alnum+pmSyms, 32))) },
		"hex-64":            func() string { return r2pick("0123456789abcdef", 64) },
		"diceware-dots": func() string {
			return r2pick("abcdefgh", 5) + "." + r2pick("abcdefgh", 6) + "." + r2pick("abcdefgh", 5)
		},
		"diceware-dash-digit": func() string {
			return r2pick("abcdefgh", 5) + "-" + r2pick("abcdefgh", 6) + "-" + r2pick("0123456789", 2)
		},
	}
	var out []rateCell
	for _, gname := range rateGens {
		for _, k := range rateShapes {
			caught := 0
			for i := 0; i < 200; i++ {
				v := gens[gname]()
				o, _ := r.String(strings.Replace(k, "%s", v, 1) + "\n")
				if noWindowSurvives(v, o, 6) {
					caught++
				}
			}
			out = append(out, rateCell{gname, k, caught})
		}
	}
	return out
}

// TestTheRateRangeHoldsOverSeedsFourToEight: the README states the symbol-bearing generators'
// recall as a RANGE over named seeds, because one seed is one measurement (round 4's audit
// measured "195–199/200" true only at seed 4, and 192–200 over seeds 5–8). This re-measures the
// stated range at every seed it names, so a README figure cannot outlive the code.
func TestTheRateRangeHoldsOverSeedsFourToEight(t *testing.T) {
	const lo = rateRangeLow // the README's stated floor over seeds 4–8
	worst, best := 200, 0
	for seed := uint64(4); seed <= 8; seed++ {
		for _, c := range rateCells(seed) {
			if !symbolBearing[c.gen] && c.caught != 200 {
				t.Errorf("seed %d: %s / %s: %d/200 — a symbol-free generator is not 200/200", seed, c.gen, c.shape, c.caught)
			}
			if !symbolBearing[c.gen] {
				continue
			}
			worst, best = min(worst, c.caught), max(best, c.caught)
		}
	}
	t.Logf("symbol-bearing generators over seeds 4–8: %d–%d/200", worst, best)
	if worst < lo {
		t.Errorf("measured %d/200 at the worst cell; the README states %d", worst, lo)
	}
}

// rateRangeLow is the worst symbol-bearing cell measured over seeds 4–8 (README: "The cost,
// measured").
const rateRangeLow = 193

// symbolBearing are the generators whose values carry symbols — the ones with a residual.
var symbolBearing = map[string]bool{"pm-20-with-symbols": true, "pm-16-3symbols": true, "pm-16-lead-symbol": true}

// noWindowSurvives is the oracle the test's doc states: NO w-character window of v appears in
// out. (Through round 3 the body checked only the whole value and its first half, which is a
// weaker oracle than its doc claimed: a value whose redaction stopped early — leaving a tail —
// counted as caught. Round 4 made the body match the doc and re-measured the floors under it.)
func noWindowSurvives(v, out string, w int) bool {
	if len(v) < w {
		return !strings.Contains(out, v)
	}
	for i := 0; i+w <= len(v); i++ {
		if strings.Contains(out, v[i:i+w]) {
			return false
		}
	}
	return true
}

// rateFloor is the measured floor per generator and line shape (see the test doc).
//
// Every generator without symbols is 200/200. The symbol-bearing ones are pinned at exactly what
// seed 4 measured under the window oracle ([noWindowSurvives], w=6), so ANY loss is red: their
// residual is the code-shape filter's stated cost ([notCode]: a shell expansion, a code-shaped head
// before `(`/`[`/`{`, an adjacent `()`/`{}`, a letters-only head before trailing brackets). Since
// round 4 the libpq shape is the key-context rule's, whose bare value runs to whitespace, so `;`
// and `&` no longer cut it. Round 5 re-pinned them (`pm-20` gained one in each of two shapes: a
// random dotted head before `(` is no longer read as a qualified call) and added
// `pm-16-lead-symbol` (round 4's head measured it 198/199/198/196 at seed 4; the leading `*`, `&`
// and `%verb` families are measured 200 at a time in `TestRoundFiveValuesStartingWithASymbol`).
// Over seeds 4–8 the symbol-bearing cells measure 193–200 here and 192–200 at round 4's head.
func rateFloor(gen, shape string) int {
	measured := map[string][4]int{
		"pm-20-with-symbols": {198, 198, 199, 197},
		"pm-16-3symbols":     {198, 199, 199, 195},
		"pm-16-lead-symbol":  {199, 199, 199, 197},
	}
	shapes := map[string]int{"DB_PASSWORD=%s": 0, "export API_SECRET=%s": 1, "password: %s": 2, "host=db password=%s dbname=x": 3}
	if m, ok := measured[gen]; ok {
		return m[shapes[shape]]
	}
	return 200
}
