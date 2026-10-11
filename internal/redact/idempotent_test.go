package redact

import (
	"math/rand/v2"
	"strconv"
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
// So a span loses the existing marker regions it overlaps (a span that BEGINS inside one is
// dropped whole), and a whole-value rule leaves a value that IS a marker alone. On marker-free text neither changes anything, which is why the corpus, the
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

// 🔴 A MARKER DOES NOT SHIELD A SECRET BESIDE IT — in ANY glued shape. The first cut of the
// quiet-on-its-own-output fix DROPPED every span that touched a marker, and measured against the
// base it LEAKED a key-context value glued directly before a marker (`password=<v>[redacted:…]`),
// under `export`, YAML, a CLI flag, a JSON text field and a DSN's userinfo: the rule's span was
// `<v>[redacted:…]`, it touched the marker, and the whole span — the secret with it — was let
// through. The fix subtracts the marker REGIONS from a span and redacts what is left, so every row
// below must lose its secret AND keep the existing marker byte-identical. Values are generated at
// run time, so no row can pass on a value the table happens to special-case. Measured matrix: the
// six before-marker rows are RED at the dropping cut (secret shipped) and RED at the base too, for
// a different reason (caught, but the existing marker was rewritten into the new one); the
// after-marker rows are DECLARED residuals ([withoutMarkers] says why) and ship at all three.
func TestAMarkerDoesNotShieldASecretBesideIt(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	gen := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = alnum[rng.IntN(len(alnum))]
		}
		return string(b)
	}
	// pw is a LOW-entropy, credential-shaped value — four syllables, each followed by a digit — that
	// ONLY a key-context rule can see. A random alnum value is no test here: the entropy rule catches
	// it as its own span, which ENDS where the marker begins and so never touches it, and the row
	// passes whether or not a marker shields anything (measured: 24 random alnum chars glued before a
	// marker were caught at the shielding head). The control below pins that each pw is invisible on
	// its own, so a row can only pass through the key-context or DSN rule.
	syl := []string{"ka", "lo", "mi", "nu", "ra", "te", "vo", "si", "be", "du"}
	pw := func() string {
		var b strings.Builder
		for range 4 {
			b.WriteString(syl[rng.IntN(len(syl))])
			b.WriteString(strconv.Itoa(rng.IntN(10)))
		}
		return b.String()
	}
	const m = "[redacted:key-context:0123abcd]"
	r, err := New(SelfTestKey(1), nil)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		name   string
		json   bool // run through Record rather than String
		secret string
		in     string
		// residual: a DECLARED leak — the row must still ship its secret, and the day it does not,
		// the residual statement on [withoutMarkers] and in the plan's T1 is wrong and must change.
		residual bool
	}
	var rows []row
	for _, sh := range []struct {
		name     string
		json     bool
		residual bool
		wrap     func(v string) string
	}{
		{"before-marker", false, false, func(v string) string { return "password=" + v + m + "\n" }},
		{"export-before-marker", false, false, func(v string) string { return "export DB_PASSWORD=" + v + m + "\n" }},
		{"yaml-before-marker", false, false, func(v string) string { return "db_password: " + v + m + "\n" }},
		{"cli-flag-before-marker", false, false, func(v string) string { return "app --password=" + v + m + " --verbose\n" }},
		{"json-text-before-marker", true, false, func(v string) string { return `{"type":"text","text":"API_TOKEN=` + v + m + `"}` }},
		{"dsn-before-marker", false, false, func(v string) string { return "postgres://app:" + v + m + "@db.example.test:5432/app\n" }},
		{"key-after-marker", false, false, func(v string) string { return m + "password=" + v + "\n" }},
		{"value-after-marker-under-key", false, true, func(v string) string { return "password=" + m + v + "\n" }},
		{"yaml-value-after-marker", false, true, func(v string) string { return "db_password: " + m + v + "\n" }},
		{"space-separated-before-marker", false, false, func(v string) string { return "password=" + v + " " + m + "\n" }},
		{"space-separated-after-marker", false, false, func(v string) string { return m + " password=" + v + "\n" }},
	} {
		v := pw()
		// CONTROL: the bare value is invisible, so the row can only pass through a keyed rule.
		if out, hits := r.String(v + "\n"); len(hits) != 0 || !strings.Contains(out, v) {
			t.Fatalf("%s: the fixture value %q is caught on its own (hits %v) — the row would pass "+
				"without reaching the marker path", sh.name, v, hits)
		}
		rows = append(rows, row{sh.name, sh.json, v, sh.wrap(v), sh.residual})
	}
	// A vendor-shaped token is its own span beside a marker.
	tok := "ghp_" + gen(36)
	rows = append(rows, row{"vendor-token-space-separated", false, tok, "TOKENS=" + m + " " + tok + "\n", false})

	for _, c := range rows {
		var out string
		var hits []Hit
		if c.json {
			b, h := r.Record([]byte(c.in))
			out, hits = string(b), h
		} else {
			out, hits = r.String(c.in)
		}
		if c.residual {
			if !strings.Contains(out, c.secret) {
				t.Errorf("%s: the DECLARED residual is now caught (%q) — update the residual statement "+
					"on withoutMarkers and in the plan's T1, then make this an ordinary row", c.name, out)
			}
			continue
		}
		if strings.Contains(out, c.secret) || len(hits) == 0 {
			t.Errorf("%s: the secret beside a marker shipped: %q (hits %v)", c.name, out, hits)
			continue
		}
		if !strings.Contains(out, m) {
			t.Errorf("%s: the existing marker was rewritten: %q", c.name, out)
		}
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
