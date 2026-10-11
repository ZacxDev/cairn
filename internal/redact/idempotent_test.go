package redact

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// 🔴 THE POD RE-CHECKS WITH THE SAME TABLE (plan decision 6, clause (c)), SO THE TABLE MUST BE
// QUIET ON ITS OWN OUTPUT — and it was not. Measured while building S3 (the transcript store): over
// corpus seeds 1–40, re-scanning the agent's redacted output under a second key matched 1,502 times
// (key-context 1,128, authorization 117, k8s-secret 96, jwk-private 80, url-userinfo-password 80,
// yaml-block-secret 1). Every one was a rule reading a MARKER as a value: `password=[redacted:…]`
// is a named value, `github-token:67af7ba4` inside a marker is a `name:value` pair, and a DSN's
// userinfo regex read the marker's colons as `user:password`. A pod refusing on any of those would
// refuse every upload the agent redacted anything in — the re-check would stall exactly the
// sessions that most needed redaction, and never the clean ones.
//
// So a span that touches an existing marker is dropped, and a whole-value rule leaves a value that
// IS a marker alone. On marker-free text neither changes anything, which is why the corpus, the
// frozen budget and the held-back gate cannot move (their inputs carry no marker).

func TestTheRedactorIsQuietOnItsOwnOutput(t *testing.T) {
	reHits, rawHits := 0, 0
	byRule := map[string]int{}
	for seed := uint64(1); seed <= 40; seed++ {
		c := NewCorpus(seed)
		agent, err := New(SelfTestKey(seed), nil)
		if err != nil {
			t.Fatal(err)
		}
		// A DIFFERENT key, as on a real pod: the pod never holds the host's key.
		pod, err := New(SelfTestKey(seed+1000), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range c.Items {
			var out []byte
			var raw, again []Hit
			if it.Blob {
				_, raw = pod.Blob(it.Name, it.Data)
				out, _ = agent.Blob(it.Name, it.Data)
				_, again = pod.Blob(it.Name, out)
			} else {
				_, raw = pod.Record(it.Data)
				out, _ = agent.Record(it.Data)
				_, again = pod.Record(out)
			}
			rawHits += len(raw)
			reHits += len(again)
			for _, h := range again {
				byRule[h.Rule]++
			}
		}
	}
	// POSITIVE CONTROL: the same re-scan over the UNREDACTED items must see the plants, or a zero
	// below is a scanner wired to nothing. 40 seeds × DeclaredPlants is the floor.
	if rawHits < 40*DeclaredPlants {
		t.Fatalf("the re-scan saw %d hits on UNREDACTED items, under the %d plants it must see — the "+
			"instrument is broken, so the zero below would prove nothing", rawHits, 40*DeclaredPlants)
	}
	if reHits != 0 {
		t.Errorf("re-scanning the agent's own output matched %d times (by rule: %v; %d on the raw items). "+
			"The pod's refusing re-check would refuse every redacted upload", reHits, byRule, rawHits)
	}
}

// A marker does not SHIELD a secret beside it: the drop is for spans that touch a marker, and a
// vendor-shaped token after one is its own span.
func TestAMarkerDoesNotShieldASecretBesideIt(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 36)
	for i := range b {
		b[i] = alnum[rng.IntN(len(alnum))]
	}
	token := "ghp_" + string(b)
	r, err := New(SelfTestKey(1), nil)
	if err != nil {
		t.Fatal(err)
	}
	in := "TOKENS=[redacted:key-context:0123abcd] " + token + "\n"
	out, hits := r.String(in)
	if strings.Contains(out, token) || len(hits) == 0 {
		t.Fatalf("a token beside a marker shipped: hits=%v", hits)
	}
	if !strings.Contains(out, "[redacted:key-context:0123abcd]") {
		t.Fatalf("the existing marker was rewritten: %q", out)
	}
}

// The whole-value paths (a JWK private member, a k8s env value, a secret-named JSON field, a
// Secret's data) leave a value that is already a marker alone.
func TestAWholeValueThatIsAMarkerIsLeftAlone(t *testing.T) {
	r, err := New(SelfTestKey(1), nil)
	if err != nil {
		t.Fatal(err)
	}
	const m = "[redacted:secret-field:0123abcd]"
	for _, rec := range []string{
		`{"kty":"RSA","d":"` + m + `"}`,
		`{"name":"DB_PASSWORD","value":"` + m + `"}`,
		`{"password":"` + m + `"}`,
		`{"kind":"Secret","data":{"token":"` + m + `"}}`,
	} {
		out, hits := r.Record([]byte(rec))
		if len(hits) != 0 || string(out) != rec {
			t.Errorf("%s: re-redacted to %s (hits %v)", rec, out, hits)
		}
	}
}
