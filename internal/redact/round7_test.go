package redact

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// Round 7: the regression test for review round 6's entropy finding. Round 6 restored the SEGMENT
// count in [identifierSegments] so that real identifiers (`ClientCert-RSA-AES256-GCM-SHA384`) stop
// being redacted, and that count let a run of dictionary words shield a random segment standing at
// one END of the token: `prod-billing-service-api-token-<24 random>` is five word segments of six,
// and the words hold more than the 35% character floor, so the whole token read as an identifier.
// Caught at the round-5 head (`dd01072`), missed at round 6's (`13219d8`).
//
// This file compiles against `13219d8` and was run there; the matrix is in the round-7 commit.

// r7tail is a random alphanumeric segment of n characters carrying an upper-case letter, a
// lower-case letter and a digit. The three classes are FORCED, because a token missing one is the
// entropy rule's documented cost (about 2% of 20-character alphanumerics) at every head, and
// counting those here would measure that cost rather than this regression.
func r7tail(rng *rand.Rand, n int) string {
	const up, low, dig = "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "abcdefghijklmnopqrstuvwxyz", "0123456789"
	const all = up + low + dig
	b := make([]byte, n)
	for i := range b {
		b[i] = all[rng.IntN(len(all))]
	}
	b[rng.IntN(n)] = up[rng.IntN(len(up))]
	for {
		i, j := rng.IntN(n), rng.IntN(n)
		if i == j {
			continue
		}
		b[i], b[j] = low[rng.IntN(len(low))], dig[rng.IntN(len(dig))]
		break
	}
	if !strings.ContainsAny(string(b), up) {
		b[0] = 'Q' // the two writes above can land on the upper-case one
	}
	return string(b)
}

// TestRoundSevenWordsDoNotShieldARandomEdgeSegment: words before (or after) a random segment of at
// least 16 characters do not make the token an identifier. 240 trailing and 240 leading tails, of
// 16 to 32 characters, over the three shapes the audit reported.
//
// MEASURED over this exact set: 434 of 480 survive at `13219d8`, 110 at `dd01072` (so round 5 did
// not catch every such tail either — the audit's three were among the caught), 10 after this
// round. The 10 are tails that fail the rule's token test ON THEIR OWN — a class-change rate under
// 0.35, e.g. `L49fcHMLarkmpks7` — which is the documented per-token cost, not this shield. The
// ceiling is that literal count; it is not derived from the implementation.
const r7edgeMissCeiling = 10

func TestRoundSevenWordsDoNotShieldARandomEdgeSegment(t *testing.T) {
	r := r5redactor(t)
	// The three reported shapes, with tails chosen here (the audit's own values are held back).
	for _, line := range []string{
		"token: see prod-billing-service-api-token-Vq7mK2xR9tLp4WzN8bJc3Hd6 for the rotation",
		"passphrase Correct-Horse-Battery-Staple-Xk9qLm2Zp7QwRt5vB8nY was printed",
		"export from svc_xxx_deploy_key_Fz3Kp8LmQ2wX7vN4bR9tJ6hYd1 now",
	} {
		if out, _ := r.String(line); out == line {
			t.Errorf("not redacted: %q", line)
		}
	}
	rng := rand.New(rand.NewPCG(7, 216))
	words := []struct{ words, sep string }{
		{"prod-billing-service-api-token", "-"},
		{"Correct-Horse-Battery-Staple", "-"},
		{"svc_xxx_deploy_key", "_"},
	}
	missed, n := 0, 0
	for i := 0; i < 240; i++ {
		w := words[i%len(words)]
		tail := r7tail(rng, 16+i%17)
		for _, tok := range []string{w.words + w.sep + tail, tail + w.sep + w.words} {
			n++
			line := "the deploy step reads " + tok + " from the vault"
			if out, _ := r.String(line); strings.Contains(out, tail) {
				missed++
			}
		}
	}
	t.Logf("%d of %d tokens kept their random segment (ceiling %d)", missed, n, r7edgeMissCeiling)
	if missed > r7edgeMissCeiling {
		t.Errorf("%d of %d tokens kept their random segment, over the measured %d", missed, n, r7edgeMissCeiling)
	}
	if n < 400 {
		t.Fatalf("control: only %d tokens generated", n)
	}
}
