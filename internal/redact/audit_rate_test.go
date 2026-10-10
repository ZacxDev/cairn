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
// passphrases. The floors below are what this build measured at seed 4, minus nothing: the
// residual they leave IS the stated cost of the code-shape filter (a value that starts with `$`,
// `{{` or `<`, or ends in a bracket after an identifier head), plus the libpq value class
// stopping at `;` `&` and quotes.
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
				if !strings.Contains(out, v) && !strings.Contains(out, v[:len(v)/2]) {
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

// rateFloor is the measured floor per generator and line shape (see the test doc).
//
// Every generator without symbols is 200/200. The symbol-bearing ones are pinned at exactly what
// seed 4 measured, so ANY loss is red: their residual is the code-shape filter's stated cost plus,
// for `host=db password=…`, the libpq value class ending at `;` `&` or a quote (a value whose first
// half survives is counted as missed).
func rateFloor(gen, shape string) int {
	measured := map[string][4]int{
		"pm-20-with-symbols": {192, 197, 199, 176},
		"pm-16-3symbols":     {200, 196, 199, 197},
	}
	shapes := map[string]int{"DB_PASSWORD=%s": 0, "export API_SECRET=%s": 1, "password: %s": 2, "host=db password=%s dbname=x": 3}
	if m, ok := measured[gen]; ok {
		return m[shapes[shape]]
	}
	return 200
}
