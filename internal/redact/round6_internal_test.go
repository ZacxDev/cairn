package redact

import (
	"strings"
	"testing"
	"time"
)

// Round 6's guards on its OWN internals. ⚠ They do not compile against round 5 (the functions they
// read are new), so they are guards on this build — not regression coverage, which is
// `round6_test.go`'s.

// TestRoundSixLongValuesKeepTheFinderLinear: taking a bare value of any length did not bring back
// the quadratic rescans round 5 removed by refusing them. The finder's own operation count at 4N
// is at most 4.5x its count at N for every shape that puts NAMES inside, or punctuation after, a
// value over [maxBareValue] — a clock-free statement, so it cannot flake under load.
func TestRoundSixLongValuesKeepTheFinderLinear(t *testing.T) {
	hex := strings.Repeat("0123456789abcdef", 1<<16)
	shapes := map[string]func(n int) string{
		"one long hex value":                   func(n int) string { return "MASTER_KEY=" + hex[:n] },
		"2 KiB hex values, one per line":       func(n int) string { return strings.Repeat("MASTER_KEY="+hex[:2048]+"\n", n/2060) },
		"a weak name inside its own value":     func(n int) string { return strings.Repeat("token_x=", n/8) },
		"a refused head, repeated":             func(n int) string { return strings.Repeat("password=${", n/11) },
		"a refused weak value, repeated":       func(n int) string { return strings.Repeat("token_x:", n/8) },
		"names inside a value, then `;`":       func(n int) string { return strings.Repeat("password=", n/18) + strings.Repeat(";", n/2) },
		"names inside a value, then `)`":       func(n int) string { return strings.Repeat("password=$(", n/22) + strings.Repeat(")", n/2) },
		"a long value inside a string literal": func(n int) string { return strings.Repeat(`"token=%s`+hex[:1500]+`" `, n/1512) },
	}
	// taken: shapes whose FIRST name takes the whole input as its value. Every later name is inside
	// that value and must not be read at all, so the count is about one scan of the input — the
	// ratio above cannot see that (reading each of them costs a bounded [longValueHead], which is
	// still linear, 30 times over).
	taken := map[string]bool{"a weak name inside its own value": true, "names inside a value, then `;`": true}
	for _, name := range sortedKeys(shapes) {
		in := shapes[name](64 << 10)
		_, small := keyContextScan(in)
		_, large := keyContextScan(shapes[name](256 << 10))
		t.Logf("%-38s ops N=%d 4N=%d", name, small, large)
		if float64(large) > 4.5*float64(max(small, 1)) {
			t.Errorf("%s: %d ops at 4N against %d at N — super-linear", name, large, small)
		}
		if taken[name] && small > 2*len(in) {
			t.Errorf("%s: %d ops over %d bytes — names inside a taken value are being read", name, small, len(in))
		}
	}
	// POSITIVE CONTROL for the counter on the long path: it moves.
	if _, n := keyContextScan("MASTER_KEY=" + hex[:4096]); n < longValueHead {
		t.Errorf("control: the operation counter read %d on a 4 KiB value", n)
	}
}

// TestRoundSixLongValuesAreStillJudgedByTheirHead: a bare value over [maxBareValue] is taken by its
// first [longValueHead] bytes — so what a head can show as code or a placeholder is still refused.
// ⚠ INVARIANT GUARD (round 5 refused every long value, these among them): it pins that taking long
// values did not turn the rule into "anything long after a secret name".
func TestRoundSixLongValuesAreStillJudgedByTheirHead(t *testing.T) {
	r := r5redactor(t)
	pad := strings.Repeat("A_B/", 600) // 2.4 KiB with no white space, quote or comma
	lines := []string{
		"export DB_PASSWORD=${" + pad + "}",
		"TOKEN=$(" + pad + ")",
		"password=" + strings.Repeat("*", 3000),
		"API_KEY=<" + pad + ">",
		`	q := "token=%s&next=` + pad + `"`,
		"token_x=" + strings.Repeat("abc_def_", 400), // a WEAK name: an identifier-shaped head
	}
	for _, l := range lines {
		if out, hits := r.String(l); out != l {
			t.Errorf("damaged (hits %v): %.60q… -> %.90q…", hits, l, out)
		}
	}
	// CONTROL: the same lengths with a head that is none of those ARE taken.
	for _, l := range []string{"export DB_PASSWORD=" + pad, "token_x=k3j9x0qpl2m7q" + pad} {
		if out, _ := r.String(l); out == l {
			t.Errorf("control: a long value with an ordinary head was not taken: %.60q…", l)
		}
	}
}

// TestRoundSixTheWholePipelineIsLinear: the same question by WALL TIME over the whole redactor —
// views, the `.netrc` and `.pgpass` parsers, key context, entropy — because the operation counter
// above reads the key-context finder only. 4N under 8x N (linear is ~4x, quadratic ~16x), read by
// [minPair]. ⚠ A wall-time test: the counter above is the guard that cannot flake.
func TestRoundSixTheWholePipelineIsLinear(t *testing.T) {
	r := r5redactor(t)
	hex := strings.Repeat("0123456789abcdef", 1<<15)
	shapes := []struct {
		name string
		n    int
		gen  func(n int) string
	}{
		{"one long named value", 32 << 10, func(n int) string { return "MASTER_KEY=" + hex[:n] + "\n" }},
		{"netrc lines behind grep -n", 32 << 10, func(n int) string { return strings.Repeat("12:  password tangerine\n", n/24) }},
		{"pgpass-shaped lines", 32 << 10, func(n int) string { return strings.Repeat("a.go:12:3://nolint:errcheck\n", n/28) }},
		{"many machines, no password", 32 << 10, func(n int) string { return strings.Repeat("machine x login y ", n/18) + "\n" }},
	}
	// ⚠ N is 32 KiB on purpose: Go's regexp runs a faster matcher on SHORT inputs, so between 8 KiB
	// and 32 KiB the per-byte cost itself steps up and a linear path reads 5–8x (measured).
	for _, sh := range shapes {
		small, large := sh.gen(sh.n), sh.gen(4*sh.n)
		ts, tl := minPair(5, func() { r.String(small) }, func() { r.String(large) })
		ratio := float64(tl) / float64(max(ts, time.Microsecond))
		if ratio >= 8 {
			// One more reading before calling it: a load burst longer than five rounds is rarer
			// than one longer than three, not impossible. A quadratic path reads ~16x every time.
			ts, tl = minPair(21, func() { r.String(small) }, func() { r.String(large) })
			ratio = float64(tl) / float64(max(ts, time.Microsecond))
		}
		t.Logf("%-30s N=%d: %v, 4N: %v, ratio %.1f", sh.name, sh.n, ts, tl, ratio)
		if ratio >= 8 {
			t.Errorf("%s: 4N took %.1fx as long as N — super-linear", sh.name, ratio)
		}
	}
}

// minPair times f and g in ALTERNATION and returns the fastest reading of each. Alternating is
// what makes the RATIO robust under load: a burst inflates both in the same rounds, and each
// minimum comes from a quiet one. (Timing all of f and then all of g, three runs each, read one
// linear shape at 27x on a busy machine.)
func minPair(rounds int, f, g func()) (time.Duration, time.Duration) {
	bf, bg := time.Duration(1<<62), time.Duration(1<<62)
	for i := 0; i < rounds; i++ {
		st := time.Now()
		f()
		bf = min(bf, time.Since(st))
		st = time.Now()
		g()
		bg = min(bg, time.Since(st))
	}
	return bf, bg
}

// TestCredentialShaped is the value test's own table: what a weak name takes and what it refuses.
// ⚠ INVARIANT GUARD on [credentialShaped]; the measured cost and recall are in
// `TestRoundSixWeakNamesNeedACredentialShapedValue` and `TestRoundSixWeakNameRecall`.
func TestCredentialShaped(t *testing.T) {
	yes := []string{"k3j9x0qpl2m7q", "Zq9xK2mL7pQw", "hunter2", "Summer2024", "9f2c4e1a7b3d5f6e8a0c", "kid-2f9a01c3",
		"aB3$dE5&gH7", "Qj.-MKiLPCDB", "xk__OQIILXl9", "/Zq9xK2mL7pQw", "MyDog2020"}
	no := []string{"strict", "Enabled", "TokenClient", "BCryptPasswordEncoder", "foo_bar9", "host-a", "s3-backups-01",
		"-issue-credential", "js.fetch:credentials", "3600s", "1.2.3", "-0.1234", "0x00", "2000-01-01T00:00:00Z",
		"2000-01-01", "alice@example.com", ".example.com", "auth.example.invalid", "/^[a-z0-9]{12", "/^x$/", "^vendor/.*$",
		"ReadableStream<Uint8Array>", "Parser<Token", "*uint16", "&cfg", "(n*8", "{ErrX", "two words", "https://example.invalid/x",
		"tiger_2024", "correct-horse-battery", "_private", "a/b/c", ""}
	for _, v := range yes {
		if !credentialShaped(v) {
			t.Errorf("%q is refused as not credential-shaped", v)
		}
	}
	for _, v := range no {
		if credentialShaped(v) {
			t.Errorf("%q is accepted as credential-shaped", v)
		}
	}
}

// TestSpliceJSONRefusesAMismatch: [spliceJSON] reports false — and its caller re-encodes — rather
// than apply a tree that does not line up with the bytes. A tree this package decoded from the same
// bytes always lines up, so the branch is driven here with one that does not.
func TestSpliceJSONRefusesAMismatch(t *testing.T) {
	raw := []byte(`{"a": "x", "b": ["y", 1]}`)
	tree, err := decodeJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out, ok := spliceJSON(raw, tree); !ok || string(out) != string(raw) {
		t.Errorf("an unchanged tree: ok=%v out=%s", ok, out)
	}
	tree.(*object).pairs[0].v = "[redacted]"
	if out, ok := spliceJSON(raw, tree); !ok || string(out) != `{"a": "[redacted]", "b": ["y", 1]}` {
		t.Errorf("one changed value: ok=%v out=%s", ok, out)
	}
	short := &object{pairs: tree.(*object).pairs[:1]}
	if _, ok := spliceJSON(raw, short); ok {
		t.Error("a tree with FEWER strings than the document was spliced")
	}
	long := &object{pairs: append(append([]pair(nil), tree.(*object).pairs...), pair{"c", "z"})}
	if _, ok := spliceJSON(raw, long); ok {
		t.Error("a tree with MORE strings than the document was spliced")
	}
}

// TestOnlyPrefix: [onlyPrefix] — what lets the unanchored key-context rule ask whether a name
// starts its line — agrees with the views about what a prefix is.
func TestOnlyPrefix(t *testing.T) {
	for _, p := range []string{"", "12:", "    12\t", "cfg/my.cnf:12:", "cfg/my.cnf-12-", "> ", "+", "svc-1  | ", "2000-01-01T00:00:00Z ",
		"cfg/my.cnf:12:+"} {
		if !onlyPrefix(p) {
			t.Errorf("%q is not read as a prefix", p)
		}
	}
	for _, p := range []string{"var ", "\t", "  ", "char *", "fix: ", "x.", "const token = "} {
		if onlyPrefix(p) {
			t.Errorf("%q is read as a prefix", p)
		}
	}
}

// TestRoundSixPgpassOverPrefixedNeutralLines: the pgpass rule's damage on NEUTRAL text behind the
// two grep prefixes that produced round 5's finding. Every line of the FROZEN neutral corpus
// (`budget_test.go`) is given a `path:N:C:` and a `path:N:` prefix, and the lines the rule then
// reads as a row are counted against a ceiling. (Round 5's audit measured 312 of 201,899 behind
// `path:N:C:`.) It read the LIVE tree until round 7, which made it red on any doc edit that
// happened to add such a line — the brittleness the budget's file doc describes.
func TestRoundSixPgpassOverPrefixedNeutralLines(t *testing.T) {
	var files []corpusFile
	for _, f := range frozenCorpusFiles {
		files = append(files, readFrozenCorpus(t, f)...)
	}
	counts := map[string]int{}
	lines := 0
	var examples []string
	for _, f := range files {
		for _, l := range strings.Split(string(f.data), "\n") {
			if strings.Count(l, ":") == 0 || len(l) > 400 {
				continue
			}
			lines++
			for _, prefix := range []string{"src/pkg/file.go:100:7:", "a.go:12:3:", "src/pkg/file.go:100:", "a.go:12:"} {
				text := prefix + l + "\n"
				hit := false
				for _, v := range views(text) {
					hit = hit || len(pgpassSpans(v.text)) > 0
				}
				if hit {
					counts[prefix]++
					if len(examples) < 8 {
						examples = append(examples, prefix+l)
					}
				}
			}
		}
	}
	t.Logf("%d frozen-corpus lines holding a colon; read as a pgpass row behind each prefix: %v", lines, counts)
	for _, e := range examples {
		t.Logf("  e.g. %s", e)
	}
	// POSITIVE CONTROL: the counter sees a row, behind the same prefixes.
	for _, prefix := range []string{"", "a.go:12:3:", "a.go:12:"} {
		hit := false
		for _, v := range views(prefix + "db.example:5432:alpha:app:" + r5val() + "\n") {
			hit = hit || len(pgpassSpans(v.text)) > 0
		}
		if !hit {
			t.Fatalf("control: a real row behind %q is not read", prefix)
		}
	}
	// 8,407 as committed. ⚠ NARROWER than the live tree it replaced (20,000+ such lines, most of
	// them in docs the frozen corpus does not sample): the price of a deterministic gate.
	if lines < 8000 {
		t.Fatalf("control: only %d lines measured — the walk narrowed", lines)
	}
	for prefix, ceiling := range pgpassPrefixedCeilings {
		if counts[prefix] > ceiling {
			t.Errorf("behind %q: %d neutral lines read as a pgpass row, ceiling %d", prefix, counts[prefix], ceiling)
		}
	}
}

// pgpassPrefixedCeilings: measured over the frozen corpus (see the test's log).
var pgpassPrefixedCeilings = map[string]int{
	"src/pkg/file.go:100:7:": 0,
	"a.go:12:3:":             0,
	"src/pkg/file.go:100:":   0,
	"a.go:12:":               0,
}

// TestIdentifierSegmentsBothCounts is [identifierSegments]'s own table. ⚠ INVARIANT GUARD; the
// regression cases are `TestRoundSixIdentifierSegments` and round 5's
// `TestRoundFiveEntropyIdentifierTestCountsCharacters`.
func TestIdentifierSegmentsBothCounts(t *testing.T) {
	ident := []string{"ClientCert-RSA-AES256-GCM-SHA384", "CurveTest-Client-MLKEM-TLS13", "_cgo_be59f0f25121_Cfunc_puts",
		"GO_NID_X9_62_prime256v1", "BSD-Systemics-W3Works", "test_a_404_carries_ETag", "en-US/firefox/12x/notes",
		"ap_ImmSigned16_16_31", "arg_Vd_arrangement_size_Q___8B_00__16B_01__4H_10__8H_11__2S_20__4S_21"}
	for _, s := range ident {
		if !identifierSegments(s) {
			t.Errorf("%q is not read as an identifier", s)
		}
	}
	random := []string{
		"qk/" + strings.Repeat("Zq9xK2mL7pQw", 5)[:59] + "/VOC", // a short word at each end of a long random run
		"ab/Xk9qLm2Zp7QwRt5v/Qr",                                // …of a short one
		"Zq9xK2mL7pQw_Xk9qLm2Zp7QwRt5v-Lm2Zp7Qw",                // no word segment at all
		"abcdefgh12345678",                                      // fewer than three segments
	}
	for _, s := range random {
		if identifierSegments(s) {
			t.Errorf("%q is read as an identifier", s)
		}
	}
}
