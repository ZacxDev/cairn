package redact

import (
	"bytes"
	"encoding/base64"
	"math/rand/v2"
	"strings"
	"testing"
)

// TestTheEntropyRuleThresholds pins the entropy rule's two error rates at the numbers measured
// when the thresholds were chosen (seed 15, 500 tokens per generator), so a threshold change that
// moves either is a deliberate one:
//
//   - RECALL on random tokens, by length and alphabet, embedded in prose with no key in front;
//   - the clean shapes it was tuned against — identifiers, paths, digests, IDs — NOT redacted.
//
// ⚠ The tokens are random in the alphabets real tokens use; a token format that is all one case,
// or hex, is outside this rule by construction (see entropy.go) and is not measured here.
func TestTheEntropyRuleThresholds(t *testing.T) {
	g := &gen{rng: rand.New(rand.NewPCG(15, 15))}
	r, _ := New(bytes.Repeat([]byte{7}, 32), nil)
	gens := []struct {
		name string
		make func() string
	}{
		{"alnum-20", func() string { return g.pick(alnum, 20) }},
		{"alnum-24", func() string { return g.pick(alnum, 24) }},
		{"alnum-32", func() string { return g.pick(alnum, 32) }},
		{"alnum-40", func() string { return g.pick(alnum, 40) }},
		{"base64-18B", func() string { return base64.StdEncoding.EncodeToString(g.bytes(18)) }},
		{"base64-32B", func() string { return base64.StdEncoding.EncodeToString(g.bytes(32)) }},
		{"base64url-32B", func() string { return base64.RawURLEncoding.EncodeToString(g.bytes(32)) }},
		{"base64-64B", func() string { return base64.StdEncoding.EncodeToString(g.bytes(64)) }},
	}
	for _, gn := range gens {
		const n = 500
		caught := 0
		for i := 0; i < n; i++ {
			v := gn.make()
			out, _ := r.String("the value is " + v + " and nothing names it\n")
			if noWindowSurvives(v, out, 6) {
				caught++
			}
		}
		t.Logf("%-14s caught=%d/%d", gn.name, caught, n)
		if caught < entropyFloor[gn.name] {
			t.Errorf("%s: caught %d/%d, floor %d", gn.name, caught, n, entropyFloor[gn.name])
		}
	}

	clean := []string{
		"9f2c4e1a7b3d5f6e8a0c2b4d6f8e1a3c5b7d9f0e",                                                     // git SHA
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",                             // sha256
		"6e2cfbaa-40f5-493c-a5f0-92c9d011a74a",                                                         // UUID
		"0c9sr0zj1q7v8x5k3m2n4p6r8t0w2y4a",                                                             // Nix store hash
		"TestRoundFourToolPrefixesAreNormalisedBeforeMatching",                                         // identifier
		"UserProfileComponentV2Factory",                                                                // identifier with a digit
		"test_a_404_carries_ETag_and_HTTP2",                                                            // snake_case test name
		"internal/redact/audit_rate_test",                                                              // path
		"en-US/firefox/128x/releasenotes",                                                              // URL path with a version
		"-work-alpha/6e2cfbaa-40f5-493c-a5f0-92c9d011a74a/tool-results",                                // a session path
		"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789",                               // an alphabet literal
		"toolu_01AbCdEfGhIjKlMnOpQrStUv",                                                               // a transcript ID
		"msg_01ZyXwVuTsRqPoNmLkJiHgFe",                                                                 // a transcript ID
		"sha512-Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MEFCQ0RFRkdISUpLTE1OT1BRUlNUVVZXWFla",                      // SRI digest
		"h1:Ab3dEfGh1jKlMn0pQrStUvWxYz0123456789AbCdEfG=",                                              // go.sum digest
		"v0.0.0-20240101123456-abcdef123456",                                                           // Go pseudo-version
		"-work-alpha/6e2cfbaa-40f5-493c-a5f0-92c9d011a74a/tool-results/toolu_EdzkdmQEGaDFmS2QffYrtNSa", // a blob path: split at `/`
		"GRPCHTTP2Transport4XXRetry",                                                                   // spared by the transition rate alone
		"TestTLS13HandshakeRejectsSSLv3",                                                               // spared by the transition rate alone
		"TestAnUnknownRouteIsA404AndNeverA405",                                                         // a Go test name
		"TestTheFingerprintIsAPrefixOfSha256AndNeverTheToken",                                          // a Go test name
	}
	for _, c := range clean {
		in := "see " + c + " here\n"
		if out, hits := r.String(in); out != in {
			t.Errorf("clean shape redacted (hits %v): %q -> %q", hits, in, out)
		}
	}
	// POSITIVE CONTROL for the clean half: the same harness DOES redact a random token in the
	// same position, so a clean pass is not a harness that never redacts.
	tok := g.pick(alnum, 32)
	if out, _ := r.String("see " + tok + " here\n"); strings.Contains(out, tok) {
		t.Fatal("control: a random 32-character token in the clean harness survived")
	}
}

// entropyFloor is what seed 15 measured (see the test doc).
var entropyFloor = map[string]int{
	"alnum-20": 478, "alnum-24": 479, "alnum-32": 496, "alnum-40": 499,
	"base64-18B": 482, "base64-32B": 497, "base64url-32B": 499, "base64-64B": 498,
}
