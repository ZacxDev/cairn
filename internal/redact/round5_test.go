package redact

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// Round 5: the regression tests for review round 4's findings. Every test in THIS file compiles
// against the round-4 head (`ef4b452`) and was run there; the red/green matrix is in the round-5
// commit message. Tests that need this round's internals live in `round5_internal_test.go` and are
// labelled there as guards on this build, not as regression coverage.
//
// 🔴 As in round 4, the planted values are LOWER-CASE-AND-DIGIT wherever the entropy rule would
// otherwise catch them on its own, so each case can be caught only by the mechanism it names.

var r5rng = rand.New(rand.NewPCG(5, 5^0x5bd1e995))

// r5val is a lower-case-and-digit value with a digit inside: invisible to the entropy rule, and
// never a plain word.
func r5val() string {
	const set = "abcdefghijklmnopqrstuvwxyz0123456789"
	var b strings.Builder
	for i := 0; i < 11; i++ {
		b.WriteByte(set[r5rng.IntN(len(set))])
	}
	b.WriteString("7q")
	return b.String()
}

func r5redactor(t *testing.T) *Redactor {
	t.Helper()
	r, err := New(bytes.Repeat([]byte{7}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// minDuration is the fastest of `runs` timings of f — the least noisy reading of its cost.
func minDuration(runs int, f func()) time.Duration {
	best := time.Duration(1 << 62)
	for i := 0; i < runs; i++ {
		st := time.Now()
		f()
		if d := time.Since(st); d < best {
			best = d
		}
	}
	return best
}

// TestRoundFiveKeyContextIsLinearTime: the key-context finder's cost at 4N is under 8x its cost at
// N for each input round 4 measured quadratic (and one this round found). Linear is ~4x and
// quadratic ~16x, so the bound sits between them with room for noise on either side; each reading
// is the fastest of three. Measured at `ef4b452`, two runs: 13.1–15.6x across the three.
func TestRoundFiveKeyContextIsLinearTime(t *testing.T) {
	shapes := []struct {
		name string
		n    int
		gen  func(n int) string
	}{
		{"unclosed <password> elements", 64 << 10, func(n int) string { return strings.Repeat("<password>", n/10) }},
		{"one value then a run of `)`", 64 << 10, func(n int) string { return "password=Xk9" + strings.Repeat(")", n) }},
		{"`password=` repeated", 16 << 10, func(n int) string { return strings.Repeat("password=", n/9) }},
	}
	for _, sh := range shapes {
		small, large := sh.gen(sh.n), sh.gen(4*sh.n)
		ts := minDuration(3, func() { keyContextSpans(small) })
		tl := minDuration(3, func() { keyContextSpans(large) })
		if ts < 2*time.Millisecond {
			// A reading this short is scheduler and allocator noise at three runs (round 6 made
			// `password=` repeated a single scan — microseconds — and three runs then read 3.7x to
			// 11x on one machine). The fastest of many is the cost.
			ts = minDuration(41, func() { keyContextSpans(small) })
			tl = minDuration(41, func() { keyContextSpans(large) })
		}
		ratio := float64(tl) / float64(max(ts, time.Microsecond))
		t.Logf("%-32s N=%d: %v, 4N: %v, ratio %.1f", sh.name, sh.n, ts, tl, ratio)
		if ratio >= 8 {
			t.Errorf("%s: 4N took %.1fx as long as N — super-linear", sh.name, ratio)
		}
	}
}

// TestRoundFivePrefixViewsLeaveProseAlone: a stripped prefix-shaped word (`fix:`, `TODO:`, `Q:`),
// a grep line number or a diff/quote marker no longer hands prose to the line-start `.netrc` rule.
// All were left alone before round 4 and damaged by it.
func TestRoundFivePrefixViewsLeaveProseAlone(t *testing.T) {
	r := r5redactor(t)
	lines := []string{
		"fix: password reset",
		"feat: password reset flow",
		"docs: password policy",
		"TODO: password rotation",
		"Q: password managers",
		"Subject: password expiry",
		"12: password rotation",
		"> password managers",
		"3-password hygiene",
		"chore: Password Policy",
		// A stripped `Q:` turns this into a YAML block scalar under `password:` and the next line
		// into its value — no `.netrc` rule involved, so only the `path:` shape test spares it.
		"Q: password: |\n  how often should it rotate",
	}
	for _, l := range lines {
		in := l + "\n"
		if out, hits := r.String(in); out != in {
			t.Errorf("damaged (hits %v): %q -> %q", hits, l, out)
		}
	}
}

// TestRoundFiveNetrcStructureStillCaught: the positive side of the test above — a `.netrc` behind
// the same prefixes is still read. A plain-word password is caught WITH netrc structure (a
// `machine`/`login` line before it); a non-word password is caught on its own line.
func TestRoundFiveNetrcStructureStillCaught(t *testing.T) {
	var cases []r4case
	for _, p := range []string{"", "12:", "> ", "cfg/.netrc:"} {
		cases = append(cases, r4case{"plain word with structure behind " + p,
			p + "machine api.example.invalid\n" + p + "  login bot\n" + p + "  password tangerine\n", "tangerine"})
		v := r5val()
		cases = append(cases, r4case{"non-word own line behind " + p, p + "  password " + v + "\n", v})
	}
	r4mustCatch(t, cases)
}

// TestRoundFiveValuesStartingWithASymbol: a value whose FIRST character is `*`, `&` or a printf
// verb is a password in a quoted string, a dotenv/shell value or a YAML value. Round 4 refused them
// there as code: 34/200, 28/200 and 0/200 caught. The values here are mixed case, so the entropy
// rule could catch the 20-character ones on its own — they are 13 characters, below its floor.
func TestRoundFiveValuesStartingWithASymbol(t *testing.T) {
	r := r5redactor(t)
	named := []r4case{
		{"json *", `{"password": "*rpZ0U8K69qPc"}`, "*rpZ0U8K69qPc"},
		{"dotenv &", "DB_PASSWORD=&rpZ0U8K69qPc\n", "&rpZ0U8K69qPc"},
		{"export %verb", "export DB_PASSWORD=%T-Pap45SNZR\n", "%T-Pap45SNZR"},
	}
	r4mustCatch(t, named)

	r2seed(51)
	shapes := []string{`{"password": "%s"}`, "DB_PASSWORD=%s\n", "export DB_PASSWORD=%s\n", "password: %s\n", "api_token: \"%s\"\n"}
	for _, lead := range []string{"*", "&", "%T-", "%s", "%d."} {
		for _, sh := range shapes {
			caught := 0
			for i := 0; i < 200; i++ {
				v := lead + r2pick(r2alnum, 12-len(lead)) + "9"
				out, _ := r.String(strings.Replace(sh, "%s", v, 1))
				if noWindowSurvives(v, out, 6) {
					caught++
				}
			}
			if caught != 200 {
				t.Errorf("lead %q in %q: caught %d/200", lead, sh, caught)
			}
		}
	}
}

// TestRoundFiveCodeNotationStillRefusesCodeShapes: the code-only shapes are still refused where
// the JOIN is code — a Go `:=`, a spaced `=`, a value ending in code punctuation, a name inside a
// string literal — and a YAML alias or a format template is refused everywhere.
// ⚠ INVARIANT GUARD: round 4 refused all of these too (it refused the shapes everywhere); this
// pins that narrowing the refusal to code notation did not lose them.
func TestRoundFiveCodeNotationStillRefusesCodeShapes(t *testing.T) {
	r := r5redactor(t)
	lines := []string{
		"	token := *tokenPtr",
		"	apiKey = &cfg.APIKey",
		"		Password: &passwordValue,",
		"	secret = *secretRef;",
		`	q := fmt.Sprintf("token=%s&user=%s", tok, user)`,
		`	line := fmt.Sprintf("password=%-12s|", pw)`,
		"  password: *db_password",
		"  token: &default_token",
		`{"password": "%s:%s"}`,
		"export DB_PASSWORD=%s",
	}
	for _, l := range lines {
		if out, hits := r.String(l); out != l {
			t.Errorf("damaged (hits %v): %q -> %q", hits, l, out)
		}
	}
}

// TestRoundFiveToolPrefixShapes: the prefixes round 4 named and the normaliser had no grammar for —
// `path:N:C:` (`rg --vimgrep`, `grep --column`), `N:C:`, docker compose's `svc-1  | `, `kubectl
// logs --prefix`, a log timestamp (ISO 8601, syslog) and `git blame` — before a `.netrc` password
// line, a `.pgpass` line and a PEM block whose LAST body line is short. All three need the line's
// own start; the values carry no name and are invisible to the entropy rule.
func TestRoundFiveToolPrefixShapes(t *testing.T) {
	prefixes := map[string]func(n int) string{
		"rg --vimgrep (path:N:C:)": func(n int) string { return fmt.Sprintf("cfg/.netrc:%d:3:", n) },
		"grep --column (N:C:)":     func(n int) string { return fmt.Sprintf("%d:1:", n) },
		"compose (svc-1  | )":      func(int) string { return "svc-1  | " },
		"kubectl --prefix":         func(int) string { return "[pod/web-7d9f/app] " },
		"ISO timestamp":            func(n int) string { return fmt.Sprintf("2000-01-01T00:00:%02d.123456789Z ", n) },
		"compose + timestamp":      func(n int) string { return fmt.Sprintf("svc-1  | 2000-01-01T00:00:%02dZ ", n) },
		"syslog":                   func(n int) string { return fmt.Sprintf("Jan 01 00:00:%02d box unit[42]: ", n) },
		"git blame": func(n int) string {
			return fmt.Sprintf("1a2b3c4d (Example Author 2000-01-01 00:00:00 +0000 %2d) ", n)
		},
	}
	var cases []r4case
	for _, name := range sortedKeys(prefixes) {
		p := prefixes[name]
		v := r5val()
		cases = append(cases, r4case{"netrc password line behind " + name, p(1) + "  password " + v + "\n", v})
		v = r5val()
		cases = append(cases, r4case{"pgpass behind " + name, p(1) + "db.example:5432:alpha:app:" + v + "\n", v})
		short := "q9" + r5val()[:9] + "w=" // 13 characters: under every length floor but the END-bound one
		pem := []string{"-----BEGIN OPENSSH " + "PRIVATE KEY-----", "b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW",
			short, "-----END OPENSSH " + "PRIVATE KEY-----"}
		var b strings.Builder
		for i, l := range pem {
			b.WriteString(p(i+1) + l + "\n")
		}
		cases = append(cases, r4case{"PEM short final line behind " + name, b.String(), short})
	}
	cases = append(cases, r4case{"PEM short final line, no prefix",
		"-----BEGIN OPENSSH " + "PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW\nq9a1b2c3d4e5w=\n-----END OPENSSH " + "PRIVATE KEY-----\n",
		"q9a1b2c3d4e5w="})
	r4mustCatch(t, cases)
}

// TestRoundFiveSecretWordInsideTheName: a secret word that is a SEGMENT of the name rather than its
// end (`DB_PASSWORD_PROD`), and the abbreviations (`creds`, `privkey`) — 0/200 at round 4.
func TestRoundFiveSecretWordInsideTheName(t *testing.T) {
	r := r5redactor(t)
	names := []string{"DB_PASSWORD_PROD", "API_TOKEN_STAGING", "GITHUB_TOKEN_CI", "dbPasswordProd", "secret-token-v2",
		"creds", "cred", "privkey", "PRIVKEY_B64", "pwd", "passwd", "pass", "DB_CREDS"}
	for _, name := range names {
		for _, sh := range []string{"%s=%s\n", "{\"%s\": \"%s\"}", "%s: %s\n"} {
			caught := 0
			for i := 0; i < 200; i++ {
				v := r5val()
				out, _ := r.String(fmt.Sprintf(sh, name, v))
				if !strings.Contains(out, v) {
					caught++
				}
			}
			if caught != 200 {
				t.Errorf("%s in %q: caught %d/200", name, sh, caught)
			}
		}
	}
}

// TestRoundFiveSegmentNamesCostOnCleanProbes: what the segment rule COSTS, measured on clean lines
// written for it — an attribute after the secret word (`TOKEN_URL`, `DB_PASSWORD_FILE`), and a weak
// name whose value is a word, a number, a duration, a URL or a sentence. The lines in `costs` ARE
// damaged, and are pinned as damaged so the measured cost can only move visibly.
func TestRoundFiveSegmentNamesCostOnCleanProbes(t *testing.T) {
	r := r5redactor(t)
	clean := []string{
		"TOKEN_URL=https://auth.example.invalid/oauth/token",
		"DB_PASSWORD_FILE=/run/secrets/db",
		"password_hash: 9f2c4e1a7b3d5f6e8a0c",
		"SECRET_MANAGER_REGION=us-east-1",
		"TOKEN_TTL=3600s",
		"password_min_length: 12",
		"PASSWORD_POLICY=strict",
		"secret_scanning: enabled",
		"token_endpoint: https://auth.example.invalid/token",
		"API_TOKEN_SCOPES=read,write",
		"TOKEN_ISSUER=https://issuer.example.invalid",
		"SECRET_NAME=db-creds",
		"apiKeySource: header",
		"PASSWORD_RESET_URL=https://app.example.invalid/reset",
		"tokenType: bearer",
		"SECRET_KEY_REF=app-secret",
		"PASSWORD_EXPIRY_DAYS=90",
		"TOKEN_REFRESH_INTERVAL=15m",
		"SECRET_VERSION=v2.1.0",
		"CREDENTIALS_PROVIDER=env",
		"TOKEN_CACHE_DIR=/var/cache/tokens",
		"  passwordStrength: weak",
		"PASSWORD_MAX_AGE=30d",
		"secret_rotation_enabled: true",
		"TOKEN_PREFIX=Bearer",
		"PASSWORD_ALGORITHM=argon2id",
		"GITHUB_TOKEN_PERMISSIONS=write-all",
		// Round 5's pinned COST, clean since round 6: a slug of words and short numbers is an
		// identifier, not a credential ([credentialShaped]).
		"SECRET_BACKUP_BUCKET=s3-backups-01",
	}
	costs := []string{
		// A weak name whose following segment is not on the attribute list, with a value that is
		// none of the shapes [credentialShaped] refuses. MEASURED COST, pinned rather than hidden.
		"SECRET_BACKUP_BUCKET=bkt-2f9a01c3",
	}
	damaged := 0
	for _, l := range clean {
		if out, hits := r.String(l); out != l {
			damaged++
			t.Errorf("damaged (hits %v): %q -> %q", hits, l, out)
		}
	}
	for _, l := range costs {
		if out, _ := r.String(l); out == l {
			t.Errorf("a pinned cost line is no longer damaged — move it to `clean`: %q", l)
		}
	}
	t.Logf("clean probes: %d/%d damaged; pinned costs: %d", damaged, len(clean), len(costs))
}

// TestRoundFiveMysqlQuotedPassword: `mysql -p'pw'` and `-p"pw"` — 0/200 at round 4.
func TestRoundFiveMysqlQuotedPassword(t *testing.T) {
	var cases []r4case
	for i := 0; i < 200; i++ {
		v := r5val()
		cases = append(cases, r4case{"single", "mysql -uroot -p'" + v + "' alpha\n", v})
		v = r5val()
		cases = append(cases, r4case{"double", "mysqldump -u app -p\"" + v + "\" alpha > dump.sql\n", v})
	}
	r4mustCatch(t, cases)
}

// TestRoundFiveViewKeyIsInjective: two prefix views whose per-line offsets differ by 64 KiB were
// one view to round 4's dedupe key (two bytes per offset), so the view in which BOTH prefixes are
// stripped — the only one where this Secret's `kind:` line and its `data:` lines both read as YAML —
// was dropped, and the value shipped.
func TestRoundFiveViewKeyIsInjective(t *testing.T) {
	long := "d/" + strings.Repeat("x", 65536-3) + ":" // a path-like `path:` prefix, exactly 64 KiB
	v := r5val()
	in := long + "kind: Secret\n> data:\n>   replica: " + v + "\n"
	r4mustCatch(t, []r4case{{"64 KiB prefix collision", in, v}})
}

// TestRoundFiveComparisonIsNotAnAssignment: a SPACED `==` or `=~` after a secret name is a
// comparison, whatever follows it glued — round 4's audit found that branch unreachable by any
// test. Deleting it redacts both lines below.
// ⚠ INVARIANT GUARD: round 4 left both alone; this pins the branch, it is not regression coverage.
func TestRoundFiveComparisonIsNotAnAssignment(t *testing.T) {
	r := r5redactor(t)
	for _, l := range []string{
		`  if token =~/\A[a-f0-9]{32}\z/`,
		"	if password ==hunter2x9 {",
	} {
		if out, hits := r.String(l); out != l {
			t.Errorf("damaged (hits %v): %q -> %q", hits, l, out)
		}
	}
}

// TestRoundFiveSelfTestHoldsAcrossSeeds: the self-test scores every plant on every one of 40
// seeds. Round 4 scored 78/79 on about one seed in six: the scorer looked for a plant in its
// record's RAW bytes, where Go's JSON encoder had written `&` as a six-character escape, so the
// `symbol-password` plant's rule was never credited. Seeds 2 and 3 are two of them.
func TestRoundFiveSelfTestHoldsAcrossSeeds(t *testing.T) {
	for seed := uint64(1); seed <= 40; seed++ {
		var out bytes.Buffer
		if code := SelfTest(&out, seed); code != SelfTestOK {
			t.Errorf("seed %d: exit %d\n%s", seed, code, out.String())
		}
	}
}

// TestRoundFiveScorerCreditsAPlantTheEncoderEscaped: the scorer fix itself, without the corpus — a
// record whose plant's `&` the JSON encoder wrote as an escape is credited to the rule that
// redacted it.
func TestRoundFiveScorerCreditsAPlantTheEncoderEscaped(t *testing.T) {
	const bs = 0x5c // a backslash, built from a byte: a spelled escape is decoded on the way in (see escapedHyphen)
	v := "Zq9ab&cd7xy3"
	rec, _ := json.Marshal(map[string]string{"type": "tool_result", "content": "DB_PASSWORD=" + v + "\n"})
	if !bytes.Contains(rec, []byte{bs, 'u', '0', '0', '2', '6'}) {
		t.Fatalf("control: the encoder did not escape `&` — this test no longer exercises the case: %s", rec)
	}
	c := Corpus{Items: []Item{{Data: rec}}, Plants: []Plant{{Label: "amp", Rule: "key-context", Forms: []string{v}}}}
	if s := c.Score(r5redactor(t)); s.Caught != 1 {
		t.Errorf("caught %d/1 (missed %v, wrong-rule %v)", s.Caught, s.Missed, s.WrongRule)
	}
}

// TestRoundFiveEntropyIdentifierTestCountsCharacters: base64 with a `/` near each end is not a
// three-segment path (the self-test's one entropy miss in 400 seeds).
func TestRoundFiveEntropyIdentifierTestCountsCharacters(t *testing.T) {
	r2seed(52)
	var cases []r4case
	for i := 0; i < 50; i++ {
		mid := base64.StdEncoding.EncodeToString([]byte(r2pick(r2alnum, 44)))[:59]
		v := "qk/" + mid + "/VOC/"
		cases = append(cases, r4case{"slash near each end", "random " + v + "\n", mid})
	}
	r4mustCatch(t, cases)
}

// TestRoundFiveRandomDottedHeadIsNotCode: a password whose dotted head before `(` mixes case with a
// digit between letters (`Zq99x.FITR.Q7jVO(`) is not a qualified call — and real qualified calls
// still are.
func TestRoundFiveRandomDottedHeadIsNotCode(t *testing.T) {
	r4mustCatch(t, []r4case{{"dotted random head", "DB_PASSWORD=Zq99x.FITR.Q7jVO(7\n", "Zq99x.FITR.Q7jVO(7"}})
	r := r5redactor(t)
	for _, l := range []string{
		"SECRET=x509.ParseCertificate(der)",
		"TOKEN=oauth2.StaticTokenSource(tok)",
		"password: h2c.NewHandler(mux)",
		"API_KEY=s3.New(sess)",
	} {
		if out, hits := r.String(l); out != l {
			t.Errorf("damaged (hits %v): %q -> %q", hits, l, out)
		}
	}
}

// TestRoundFiveCleanShapesFoundWhileBuildingIt: clean lines an INTERMEDIATE round-5 build damaged,
// found by redacting every tracked text file of this repository at round 4's head and at this
// build and diffing the changed lines — never from a held-back set. ⚠ Round 4 left every one of
// them alone, so they are guards on this round's own regressions, not regression coverage.
func TestRoundFiveCleanShapesFoundWhileBuildingIt(t *testing.T) {
	r := r5redactor(t)
	lines := []string{
		// a format string's tail after a secret word: verbs and escapes only
		`	t.Fatalf("want a 43-character token:\n%q", out)`,
		`	t.Errorf("the advice no longer names a credential:\n%s", stderr)`,
		`	"deployment that worked before.\n  secret: %q\n  got: %v",`,
		`	dotenv := fmt.Sprintf("DB_HOST=db.example\nDB_PASSWORD=%s\nLOG_LEVEL=debug\n", pw)`,
		`	{"flag --token=", "client login --token=%s\n"},`,
		// a WEAK name (secret word inside it) whose value is a slug, a literal or an expression
		`	"registerIssueCredentialFlags": "-issue-credential",`,
		`	g.push("fixture-not-a-presence-token-at-all", "host-a", "s-0001")`,
		`	narrowedAllTokenA = "fixture-presence-narrowed-to-none"`,
		`	tokenFrontMatterKey = "quarry-grade"`,
		`	_entry("secret-thing", "hidden-scope", "not yours to see.")`,
		"const tokenChars = (TokenEntropyBytes*8 + 5) / 6",
		"	EventCredentialIssued: {ErrUnusableTokenDigest, ErrDuplicateTokenDigest},",
		// a negated flag takes no value
		"skopeo inspect --no-creds docker://registry.example.invalid/app:v1",
	}
	for _, l := range lines {
		if out, hits := r.String(l); out != l {
			t.Errorf("damaged (hits %v): %q -> %q", hits, l, out)
		}
	}
	// And a value that is ITSELF a weak name is still a password, tagged by the name rule.
	in := `	const fixtureSecret = "NOT-A-REAL-PASSWORD-e3b0c44298fc"`
	if _, hits := r.String(in); len(hits) != 1 || hits[0].Rule != "key-context" {
		t.Errorf("a weak-name-shaped VALUE: hits %v, want one key-context hit", hits)
	}
}
