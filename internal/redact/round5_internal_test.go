package redact

import (
	"strings"
	"testing"
)

// Round 5's guards on its OWN internals. ⚠ They do not compile against round 4 (the functions they
// read are new), so they are guards on this build — not regression coverage, which is
// `round5_test.go`'s.

// TestKeyContextIsLinearTime: the finder's own operation count (bytes scanned and judged) at 4N is
// at most 4.5x its count at N — a clock-free statement of what `TestRoundFiveKeyContextIsLinearTime`
// measures by wall time, so it cannot flake under load. Each shape is one that was quadratic.
func TestKeyContextIsLinearTime(t *testing.T) {
	shapes := map[string]func(n int) string{
		"password= repeated":          func(n int) string { return strings.Repeat("password=", n/9) },
		"password=$( repeated":        func(n int) string { return strings.Repeat("password=$(", n/11) },
		"value then )":                func(n int) string { return "password=Xk9" + strings.Repeat(")", n) },
		"unclosed <password>":         func(n int) string { return strings.Repeat("<password>", n/10) },
		"token=Bearer repeated":       func(n int) string { return "token=" + strings.Repeat("Bearer ", n/7) },
		"password=a,password=a mix":   func(n int) string { return strings.Repeat("password=a,password=a ", n/22) },
		"api_key: x9 lines (control)": func(n int) string { return strings.Repeat("api_key: x9Qp2LmZ\n", n/18) },
	}
	for _, name := range sortedKeys(shapes) {
		_, small := keyContextScan(shapes[name](64 << 10))
		_, large := keyContextScan(shapes[name](256 << 10))
		t.Logf("%-30s ops N=%d 4N=%d", name, small, large)
		if float64(large) > 4.5*float64(max(small, 1)) {
			t.Errorf("%s: %d ops at 4N against %d at N — super-linear", name, large, small)
		}
	}
	// Positive control: the counter MOVES with the input — a counter wired to nothing passes the
	// ratio above with 0 and 0.
	if _, n := keyContextScan(strings.Repeat("password=", 1000)); n == 0 {
		t.Error("control: the operation counter did not move on a 9000-byte input")
	}
}

// TestTheNamePrefilterAdmitsEverySecretName: [mayNameSecret] is a prefilter in front of
// [SecretKey], so it must admit every name SecretKey accepts — otherwise it is a second, narrower
// predicate. ⚠ INVARIANT GUARD over a generated name set (every secret word × prefixes × spelling
// styles), not a proof over all names; the word list is read from the regexps' own source.
func TestTheNamePrefilterAdmitsEverySecretName(t *testing.T) {
	words := []string{"SECRET", "TOKEN", "PASSWORD", "PASSWD", "PASSPHRASE", "PASS", "PWD", "PRIVATE_KEY", "PRIVKEY",
		"API_KEY", "ACCESS_KEY", "SECRET_KEY", "CREDENTIALS", "CREDENTIAL", "CREDS", "CRED", "DSN", "AUTH",
		"AUTHORIZATION", "AUTH_TOKEN", "BEARER", "STRIPE_KEY", "CLIENT_KEY", "SSH_KEY"}
	for _, w := range words {
		if !SecretKey(strings.ToLower(w)) {
			t.Errorf("the word list names %q, which SecretKey does not accept — update this test", w)
		}
	}
	checked := 0
	for _, w := range words {
		for _, pre := range []string{"", "DB_", "MY_", "X"} {
			for _, suf := range []string{"", "_PROD", "_VALUE", "_2"} {
				up := pre + w + suf
				lowerCamel := camel(up)
				for _, n := range []string{up, strings.ToLower(up), strings.ReplaceAll(strings.ToLower(up), "_", "-"), lowerCamel} {
					if SecretKey(n) {
						checked++
						if !mayNameSecret(n) {
							t.Errorf("SecretKey accepts %q but the prefilter refuses it", n)
						}
					}
				}
			}
		}
	}
	if checked < 300 {
		t.Errorf("control: only %d generated names were secret names — the generator narrowed", checked)
	}
	t.Logf("%d secret names admitted", checked)
}

// camel spells UPPER_SNAKE as lowerCamel: `DB_PASSWORD_PROD` -> `dbPasswordProd`.
func camel(up string) string {
	var b strings.Builder
	for i, seg := range strings.Split(strings.ToLower(up), "_") {
		if i > 0 && seg != "" {
			seg = strings.ToUpper(seg[:1]) + seg[1:]
		}
		b.WriteString(seg)
	}
	return b.String()
}
