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
	r2seed(4)
	r, _ := New(bytes.Repeat([]byte{7}, 32), nil)
	// symbols a password manager draws from, minus the ones the dotenv value class already
	// stops at (# , ' ") so the measurement isolates the Accept filter.
	pmSyms := "!@$%^&*()-_=+[]{};:.<>?/|~"
	gens := map[string]func() string{
		"alnum-32":           func() string { return r2pick(r2alnum, 32) },
		"pm-20-with-symbols": func() string { return r2pick(r2alnum+pmSyms, 20) },
		"pm-16-3symbols":     func() string { return r2pick(r2alnum, 13) + r2pick(pmSyms, 3) },
		"base64-32B":         func() string { return base64.StdEncoding.EncodeToString([]byte(r2pick(r2alnum+pmSyms, 32))) },
		"hex-64":             func() string { return r2pick("0123456789abcdef", 64) },
		"diceware-dots": func() string {
			return r2pick("abcdefgh", 5) + "." + r2pick("abcdefgh", 6) + "." + r2pick("abcdefgh", 5)
		},
		"diceware-dash-digit": func() string {
			return r2pick("abcdefgh", 5) + "-" + r2pick("abcdefgh", 6) + "-" + r2pick("0123456789", 2)
		},
	}
	keys := []string{"DB_PASSWORD=%s", "export API_SECRET=%s", "password: %s", "host=db password=%s dbname=x"}
	for _, gname := range []string{"alnum-32", "pm-20-with-symbols", "pm-16-3symbols", "base64-32B", "hex-64", "diceware-dots", "diceware-dash-digit"} {
		for _, k := range keys {
			n, caught := 200, 0
			for i := 0; i < n; i++ {
				v := gens[gname]()
				in := strings.Replace(k, "%s", v, 1) + "\n"
				out, _ := r.String(in)
				if noWindowSurvives(v, out, 6) {
					caught++
				}
			}
			t.Logf("%-20s %-28s %d/%d", gname, k, caught, n)
			if floor := rateFloor(gname, k); caught < floor {
				t.Errorf("%s / %s: caught %d/%d, floor %d", gname, k, caught, n, floor)
			}
		}
	}
}

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
// and `&` no longer cut it.
func rateFloor(gen, shape string) int {
	measured := map[string][4]int{
		"pm-20-with-symbols": {198, 197, 199, 196},
		"pm-16-3symbols":     {198, 199, 199, 195},
	}
	shapes := map[string]int{"DB_PASSWORD=%s": 0, "export API_SECRET=%s": 1, "password: %s": 2, "host=db password=%s dbname=x": 3}
	if m, ok := measured[gen]; ok {
		return m[shapes[shape]]
	}
	return 200
}
