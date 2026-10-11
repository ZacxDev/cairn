package redact

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// -redact.seeds widens the self-test sweep of [TestRoundSixSelfTestHoldsAcrossSeeds]: the committed
// run covers seeds 1, 2000 and 41–46; `go test ./internal/redact -run SelfTestHoldsAcrossSeeds
// -redact.seeds=400` runs 41–440.
var r6seedSweep = flag.Int("redact.seeds", 6, "how many seeds from 41 the round-6 self-test sweep runs")

// Round 6: the regression tests for review round 5's findings. Every test in THIS file compiles
// against the round-5 head (`dd01072`) and was run there; the red/green matrix is in the round-6
// commit message. A test or a case that was already green there is labelled an INVARIANT GUARD or
// a COST PIN where it stands. Guards that need this round's internals, and the clean-damage
// budgets over neutral text, are in `round6_internal_test.go` and `budget_test.go`.
//
// 🔴 The governing rule of this round: every heuristic is measured in BOTH directions — recall on
// generated secrets here, damage on text nobody wrote for the purpose in `budget_test.go`.

// r6windowsGone reports whether the value is gone from out: no sampled 12-character window of it
// survives. It samples (every 97th offset, plus both ends) because the values here run to 64 KiB.
func r6windowsGone(v, out string) bool {
	const w = 12
	if len(v) <= w {
		return !strings.Contains(out, v)
	}
	for i := 0; i+w <= len(v); i += 97 {
		if strings.Contains(out, v[i:i+w]) {
			return false
		}
	}
	return !strings.Contains(out, v[len(v)-w:])
}

// TestRoundSixLongNamedValuesAreRedacted: a bare value attached to a secret name is redacted at
// ANY length. Round 5 bounded the finder's work by refusing every bare value over 1 KiB "to the
// entropy rule", which reads neither hex nor a URL-encoded document: `MASTER_KEY=<2048 hex>`,
// `master_secret: <2048 hex>` and `AUTH_TOKEN=<1317 URL-encoded characters>` shipped 93–100%
// intact. The 1 KiB row is one byte OVER round 5's cap; 1024 itself is the boundary and was caught
// before (an invariant guard, kept so the boundary is measured on both sides).
func TestRoundSixLongNamedValuesAreRedacted(t *testing.T) {
	r := r5redactor(t)
	r2seed(61)
	gens := map[string]func(n int) string{
		"hex": func(n int) string { return r2pick("0123456789abcdef", n) },
		"url-encoded JSON": func(n int) string {
			var doc []string
			for len(strings.Join(doc, ",")) < n {
				doc = append(doc, fmt.Sprintf("%q:%q", r2pick("abcdefghij", 6), r2pick("abcdefghijklmnopqrstuvwxyz", 14)))
			}
			return url.QueryEscape("{" + strings.Join(doc, ",") + "}")[:n]
		},
		"lower-case and digits": func(n int) string { return r2pick("abcdefghijklmnopqrstuvwxyz0123456789", n) },
	}
	shapes := []string{"MASTER_KEY=%s\n", "master_secret: %s\n", "AUTH_TOKEN=%s\n", "export DB_PASSWORD=%s\n",
		"DB_PASSWORD_PROD=%s\n", "psql \"host=db.example password=%s dbname=alpha\"\n"}
	for _, n := range []int{1024, 1025, 1317, 2048, 64 << 10} {
		for _, gname := range sortedKeys(gens) {
			for _, sh := range shapes {
				v := gens[gname](n)
				in := fmt.Sprintf(sh, v)
				out, _ := r.String(in)
				if !r6windowsGone(v, out) {
					t.Errorf("%d bytes of %s in %q: the value survived (%d of %d bytes of output)", n, gname, sh, len(out), len(in))
				}
			}
		}
	}
}

// r6words are plain-word passwords — what a person types into a `.netrc`.
var r6words = []string{"letmein", "tangerine", "dragon", "sunshine", "monkey", "shadow", "master", "trustno",
	"hunter", "starlight", "welcome", "football", "princess", "jupiter", "marigold", "lantern", "harbour", "pebble",
	"orchid", "falcon"}

// TestRoundSixNetrcPlainWordPasswords: a `.netrc` password that is a plain WORD is read from netrc
// structure or file context anywhere in the block — not only on the three lines above. Round 5
// lost each shape below (all caught at round 4's head): the lone line `grep password ~/.netrc`
// prints, a record whose `machine`/`login` sit more than three lines up behind comments and
// blanks, and a `password` written before its `login` and `machine`.
func TestRoundSixNetrcPlainWordPasswords(t *testing.T) {
	shapes := map[string]string{
		"lone line":                                 "password %s\n",
		"lone line, indented":                       "  password %s\n",
		"lone line behind grep -n":                  "7:  password %s\n",
		"machine five lines above":                  "machine api.example.invalid\n# the deploy bot\n\n  login bot\n# rotated yearly\n\n  password %s\n",
		"password before login":                     "  password %s\n  login bot\nmachine api.example.invalid\n",
		"password first, same line":                 "password %s login bot machine api.example.invalid\n",
		"default record":                            "default\n  login anonymous\n\n\n\n  password %s\n",
		"file named by the command":                 "$ grep -A2 login ~/.netrc\n  login bot\n  password %s\n",
		"file named by a path prefix":               "cfg/.netrc:  password %s\n",
		"file named, grep -n":                       "cfg/.netrc:4:  password %s\n",
		"written by echo":                           "echo \"machine api.example.invalid login bot password %s\" >> ~/.netrc\n",
		"structure behind a quote":                  "> machine api.example.invalid\n>   login bot\n>\n>\n>\n>   password %s\n",
		"password first, behind a quote":            "> password %s\n> login bot\n> machine api.example.invalid\n",
		"file named by the command, behind a quote": "> $ grep -A2 login ~/.netrc\n>   login bot\n>   password %s\n",
		"structure behind compose":                  "svc-1  | machine api.example.invalid\nsvc-1  | login bot\nsvc-1  | # x\nsvc-1  | # y\nsvc-1  | # z\nsvc-1  | password %s\n",
		// ⚠ INVARIANT row (caught at round 5 by its looser inline pattern): other words between the pairs.
		"other words between pairs":    "machine api.example.invalid login bot port 22 password %s\n",
		"two records, second password": "machine a.example.invalid\n  login a\n  password %[1]sx9\nmachine b.example.invalid\n  login b\n  account ops\n  macdef init\n\n  password %[1]s\n",
	}
	r := r5redactor(t)
	caught, total := 0, 0
	for _, name := range sortedKeys(shapes) {
		n := 0
		for _, w := range r6words {
			in := fmt.Sprintf(shapes[name], w)
			out, _ := r.String(in)
			// The word must be gone from the password line itself.
			line := "password " + w
			if !strings.Contains(out, line+"\n") && !strings.Contains(out, line+" ") && !strings.Contains(out, line+"\"") {
				n++
			}
		}
		caught, total = caught+n, total+len(r6words)
		if n != len(r6words) {
			t.Errorf("%s: caught %d/%d plain-word passwords", name, n, len(r6words))
		}
	}
	t.Logf("plain-word netrc passwords: caught %d/%d over %d shapes", caught, total, len(shapes))
	// With STRUCTURE, even a password that is an attribute word is taken — the attribute test
	// belongs to the lone line only.
	r4mustCatch(t, []r4case{
		{"an attribute-word password under its machine", "machine api.example.invalid\n  login bot\n  password rotation\n", "password rotation"},
		{"an attribute-word password in a named file", "cfg/.netrc:  password reset\n", "password reset"},
	})
}

// TestRoundSixNetrcProseStaysClean: the other direction — the prose round 4 damaged, the new
// prefix shapes round 5's audit found producing contrived hits, and the type declarations the
// neutral-corpus sweep found (`Password *uint16`, a struct field in the Go standard library).
// ⚠ Most of these were clean at round 5 too (INVARIANT GUARDS for the widened netrc rule); the two
// marked RED were damaged there.
func TestRoundSixNetrcProseStaysClean(t *testing.T) {
	r := r5redactor(t)
	lines := []string{
		"fix: password reset",
		"12: password rotation",
		"> password managers",
		"3-password hygiene",
		"chore: Password Policy",
		"- password reset",
		"- password sharing",
		"+ password sharing",
		"* password sharing",
		"> password sharing",
		"Summary | password reset-flow", // RED at round 5
		"Summary | password sharing",
		"2000-01-01T00:00:00Z password sharing",
		"default password policy",
		"default password length",
		"password reset",
		"password policy",
		"password managers",
		"Password Sharing",
		"	Password    *uint16", // RED at round 5
		"	Password []byte",
		"	password string",
		"	Password sql.NullString",
		"  password varchar(255)",
		"- login page\n- password reset\n- account settings",
		"machine learning for password strength estimation",
		"the machine default login password policy",
		"on the machine room login screen the password field is masked",
		"see the machine learning password reset model",
		// a word that is NOT an attribute word, behind a prefix that is not a line number, or
		// with the keyword capitalised: only the lone-line rule's own conditions spare these
		"- password spreadsheet",
		"> password spreadsheet",
		"Summary | password spreadsheet",
		"notes.txt: password spreadsheet",
		"Password Spreadsheet",
		"PASSWORD spreadsheet",
	}
	damaged := 0
	for _, l := range lines {
		in := l + "\n"
		if out, hits := r.String(in); out != in {
			damaged++
			t.Errorf("damaged (hits %v): %q -> %q", hits, l, out)
		}
	}
	t.Logf("netrc-shaped prose and declarations: %d/%d damaged", damaged, len(lines))
}

// TestRoundSixNetrcStatedResiduals pins what the netrc rule still gets WRONG, in both directions,
// so the stated residual can only move visibly (positional.go, residuals a–c).
func TestRoundSixNetrcStatedResiduals(t *testing.T) {
	r := r5redactor(t)
	leaks := []string{ // real passwords that ship
		"password reset\n",                 // (a) the password IS an attribute word
		"> password letmein\n",             // (a) a plain word behind a quote marker, no netrc context
		"svc-1  | password letmein\n",      // (a) …behind a compose prefix
		"- password letmein\n",             // (a) …behind a diff marker
		"notes.txt:  password letmein\n",   // (a) …behind a path that does not name a netrc
		"Password letmein\n",               // (a) the keyword capitalised
		"login bot\n  password letmein2\n", // control: NOT a residual — see below
	}
	for i, l := range leaks {
		out, _ := r.String(l)
		if i == len(leaks)-1 {
			if strings.Contains(out, "letmein2") {
				t.Errorf("control: a non-word password on its own line shipped: %q", out)
			}
			continue
		}
		if out != l {
			t.Errorf("a pinned LEAK residual is now caught — move it to the recall test: %q -> %q", l, out)
		}
	}
	damage := []string{ // clean lines that are redacted
		"password spreadsheet\n",               // (b) exactly `password <non-attribute word>`
		"12: password spreadsheet\n",           // (b) …behind a line number
		"machine learning\npassword sharing\n", // (c) prose that reads as a record
	}
	for _, l := range damage {
		if out, _ := r.String(l); out == l {
			t.Errorf("a pinned DAMAGE residual is no longer damaged — move it to the clean test: %q", l)
		}
	}
}

// r6weakNames are names whose secret word is a segment rather than the end, and the `cred`/`creds`
// abbreviations — the names held to [credentialShaped].
var r6weakNames = []string{"DB_PASSWORD_PROD", "API_TOKEN_STAGING", "GITHUB_TOKEN_CI", "dbPasswordProd", "secret-token-v2",
	"creds", "cred", "DB_CREDS"}

// TestRoundSixWeakNamesNeedACredentialShapedValue — the clean direction. A name whose secret word
// is not its final segment, or that is the abbreviation `cred`/`creds`, takes only a value that
// itself looks like a credential. Round 5 refused words, slugs, numbers and URLs there and nothing
// else: 19 of the auditor's 53 clean probes were damaged, against 0 at round 4. The first thirteen
// lines are the audit's own examples; the rest were written for this test.
func TestRoundSixWeakNamesNeedACredentialShapedValue(t *testing.T) {
	r := r5redactor(t)
	clean := []string{
		`passwordChangedAt: "2000-01-01T00:00:00Z"`,
		`token_last_used: 2000-01-01T00:00:00Z`,
		`password_changed_by: alice@example.com`,
		`tokenStream: ReadableStream<Uint8Array>`,
		`tokenClient: TokenClient`,
		`passwordEncoder: BCryptPasswordEncoder`,
		`def __init__(self, password_mgr=None):`,
		`token.lexeme = "foo_bar9"`,
		`const PASSWORD_REGEXP = /^[a-z0-9]{12,}$/`,
		`TOKEN_EOF = 0x00`,
		`SCM_CREDS = 0x03`,
		`token_logprob=-0.1234`,
		`TOKEN_COOKIE_DOMAIN=.example.com`,
		// written for this test
		`tokenKind: lexer.Ident2`,
		`passwordField: *sql.NullString`,
		`secret_store: vault-kv-v2`,
		`TOKEN_BUCKET_RATE=12.5`,
		`token_first_seen: "2000-01-01 00:00:00"`,
		`password_updated_by="ops@example.com"`,
		`tokenParser: Parser<Token, ParseError>`,
		`SECRET_SCAN_EXCLUDE=^vendor/.*$`,
		`creds: aws.Credentials`,
		`creds = load_creds(profile)`,
		`	cred := ucred.Get(conn)`,
		`IP_IPSEC_LOCAL_CRED               = 0x19`,
		`const jsFetchCreds = "js.fetch:credentials"`,
		`passwordPolicyV2: minLength8`,
		`token_usage_p99=1_250`,
		`SECRET_BACKUP_BUCKET=s3-backups-01`,
		`  password_hint_text: first_pet_name`,
		`THREAD_SET_THREAD_TOKEN          = 0x0080`,
		`	LSH          ScanToken = -1000 - iota`,
		`TOKEN_ADJUST_PRIVILEGES = 0x0020`,
	}
	damaged := 0
	for _, l := range clean {
		if out, hits := r.String(l); out != l {
			damaged++
			t.Errorf("damaged (hits %v): %q -> %q", hits, l, out)
		}
	}
	// COST PINS: clean lines that ARE damaged — a weak name whose value is credential-shaped.
	costs := []string{
		`TOKEN_SIGNING_KID=kid-2f9a01c3`,
		`password_salt_hex: 9f2c4e1a7b3d5f6e`,
	}
	for _, l := range costs {
		if out, _ := r.String(l); out == l {
			t.Errorf("a pinned cost line is no longer damaged — move it to `clean`: %q", l)
		}
	}
	t.Logf("weak-name clean probes: %d/%d damaged; pinned costs: %d", damaged, len(clean), len(costs))
}

// TestRoundSixWeakNameRecall — the recall direction of the test above, on GENERATED secrets: what
// tightening the value test costs. Random values (any alphabet a generator or a password manager
// uses) are caught at or above the floor beside them; the two WORD-SHAPED families are the stated
// cost, pinned at exactly 0 so a later round that moves either sees it. The one random row under
// 200 is not this round's: a generated password that is `identifier&&identifier`, `…||…` or `…??…`
// is refused as an expression by [notCode] under every name, strong ones included.
// ⚠ COST PIN for the word-shaped rows (round 5 caught `word_NNNN` and lost the dashed slug); the
// random rows are an INVARIANT GUARD (the same at round 5).
func TestRoundSixWeakNameRecall(t *testing.T) {
	r := r5redactor(t)
	r2seed(62)
	pmSyms := "!@$%^&*-_=+.?/|~"
	gens := []struct {
		name string
		want int
		make func() string
	}{
		{"lower+digit-13", 200, r5val},
		{"alnum-12", 200, func() string { return r2pick(r2alnum, 11) + r2pick("0123456789", 1) }},
		{"alnum-32", 200, func() string { return r2pick(r2alnum, 32) }},
		{"hex-32", 200, func() string { return r2pick("0123456789abcdef", 32) }},
		{"pm-16-symbols", 196, func() string { return r2pick(r2alnum, 6) + r2pick(pmSyms, 2) + r2pick(r2alnum, 8) }},
		{"Word+year", 200, func() string { return r2pick("ABCDEFGH", 1) + r2pick("abcdefgh", 5) + "20" + r2pick("0123456789", 2) }},
		// The stated cost: an identifier- or slug-shaped password under a weak name ships.
		{"word_NNNN", 0, func() string { return r2pick("abcdefgh", 5) + "_" + r2pick("0123456789", 4) }},
		{"word-word-NN", 0, func() string {
			return r2pick("abcdefgh", 5) + "-" + r2pick("abcdefgh", 6) + "-" + r2pick("0123456789", 2)
		}},
	}
	for _, g := range gens {
		for _, sh := range []string{"%s=%s\n", "{\"%s\": \"%s\"}", "%s: %s\n"} {
			caught, total := 0, 0
			for _, name := range r6weakNames {
				for i := 0; i < 25; i++ {
					v := g.make()
					out, _ := r.String(fmt.Sprintf(sh, name, v))
					total++
					if noWindowSurvives(v, out, 6) {
						caught++
					}
				}
			}
			t.Logf("%-16s %-18q %d/%d", g.name, sh, caught, total)
			if caught < g.want || (g.want == 0 && caught != 0) {
				t.Errorf("%s in %q: caught %d/%d, pinned %d", g.name, sh, caught, total, g.want)
			}
		}
	}
}

// TestRoundSixIdentifierSegments: the identifiers round 5's character-counted segment test made
// the entropy rule redact are left alone again — and the base64 run with a `/` near each end that
// round 5 counted characters to catch (`TestRoundFiveEntropyIdentifierTestCountsCharacters`) still
// is. Each identifier stands in a line with no secret name near it.
func TestRoundSixIdentifierSegments(t *testing.T) {
	r := r5redactor(t)
	lines := []string{
		`		name:   "ClientCert-RSA-AES256-GCM-SHA384",`,
		`		"CurveTest-Client-MLKEM-TLS13": "PASS",`,
		`//go:cgo_import_static _cgo_be59f0f25121_Cfunc_puts`,
		`		return C.GO_NID_X9_62_prime256v1, nil`,
		`SPDX-License-Identifier: BSD-Systemics-W3Works`,
		`	ap_ImmSigned16_16_31   = &argField{Type: TypeImmSigned16, flags: 0x0, BitField: BitField{16, 16}}`,
		`	ETHTOOL_LINK_MODE_10000baseKX4_Full_BIT                                 = 0x12`,
		`	http2cipher_TLS_RSA_WITH_NULL_SHA                 uint16 = 0x0002`,
		`	if C._goboringcrypto_EVP_PKEY_CTX_set_rsa_mgf1_md(ctx, md) == 0 {`,
		`		"Client-TLS12-NoSign-RSA_PKCS1_MD5_SHA1": "PASS",`,
		`	case arg_Vd_arrangement_size_Q___8B_00__16B_01__4H_10__8H_11__2S_20__4S_21:`,
		`func BenchmarkSha3_512_MTU(b *testing.B) { benchmarkHash(b, New512(), 1350, 1) }`,
	}
	for _, l := range lines {
		if out, hits := r.String(l); out != l {
			t.Errorf("damaged (hits %v): %q -> %q", hits, l, out)
		}
	}
}

// TestRoundSixJSONIsRedactedInPlace: one hit in a JSON document rewrites ONE string. Round 5 (and
// every round before it) re-serialised the whole document, so a pretty-printed file came back with
// every line whose indentation, spacing, escapes or number text differed from the encoder's
// changed — 695 of 698 lines of the audit's example. The document below is laid out the way an
// encoder would NOT write it, on purpose.
func TestRoundSixJSONIsRedactedInPlace(t *testing.T) {
	r := r5redactor(t)
	v := r5val()
	const bs = 0x5c
	esc := string([]byte{bs, 'u', '0', '0', '2', 'd'}) // an escaped hyphen, built from bytes (see escapedHyphen)
	doc := "{\n" +
		"    \"name\"  :  \"synthetic" + esc + "world\",\n" +
		"\t\"count\": 1.50,\n" +
		"    \"big\": 12345678901234567890123,\n" +
		"    \"html\": \"a<b>&c\",\n" +
		"    \"nested\": { \"api_token\" : \"%s\", \"keep\": [ 1,2 ,3 ] },\n" +
		"    \"empty\": {  }\n" +
		"}\n"
	in := fmt.Sprintf(doc, v)
	want := fmt.Sprintf(doc, "[redacted:secret-field:"+r.Tag(v)+"]")
	if out, _ := r.Text([]byte(in)); string(out) != want {
		t.Errorf("Text re-serialised the document:\n--- got\n%s--- want\n%s", out, want)
	}
	if out, _ := r.Blob("world.json", []byte(in)); string(out) != want {
		t.Errorf("Blob re-serialised the document:\n--- got\n%s--- want\n%s", out, want)
	}
	if out, _ := r.String("  " + in); out != "  "+want {
		t.Errorf("String re-serialised the document:\n--- got\n%s--- want\n%s", out, "  "+want)
	}
	// A redacted KEY is spliced the same way, and a duplicate member is still scanned.
	key := "ghp_" + r2pick(r2alnum, 36)
	kin := "{ \"" + key + "\" : 1,\n  \"note\": \"x\" ,  \"note\": \"DB_PASSWORD=" + v + "\" }"
	kout, _ := r.Text([]byte(kin))
	kwant := "{ \"[redacted:github-token:" + r.Tag(key) + "]\" : 1,\n  \"note\": \"x\" ,  \"note\": \"DB_PASSWORD=[redacted:key-context:" + r.Tag(v) + "]\" }"
	if string(kout) != kwant {
		t.Errorf("a redacted key or a duplicate member:\n--- got\n%s\n--- want\n%s", kout, kwant)
	}
}

// TestRoundSixSymbolLeadingValuesInConfigLayouts: a password whose first character is `*`, `&` or
// a printf verb is a password after a SPACED `=` (the `.my.cnf`, INI and `.properties` layout) and
// in front of a trailing `;`, `,` or `)`. Round 5 decided "code notation" from the spacing and the
// trailing punctuation alone and refused them all.
func TestRoundSixSymbolLeadingValuesInConfigLayouts(t *testing.T) {
	r := r5redactor(t)
	r2seed(63)
	shapes := []string{"password = %s\n", "[client]\nuser = root\npassword = %s\n", "db.password = %s\n",
		"DB_PASSWORD=%s;\n", "DB_PASSWORD=%s,\n", "DB_PASSWORD=%s)\n", "export API_SECRET=%s; ./run\n", "  password: %s,\n"}
	for _, lead := range []string{"*", "&", "%T-", "%s", "%d."} {
		for _, sh := range shapes {
			caught := 0
			for i := 0; i < 200; i++ {
				// A digit BETWEEN letters, as a generated password has: `Zq9xK2mL7pQw`.
				v := lead + r2pick(r2alnum, 3) + r2pick("0123456789", 1) + r2pick(r2alnum, 8-len(lead)) + "x"
				out, _ := r.String(fmt.Sprintf(sh, v))
				if noWindowSurvives(v, out, 6) {
					caught++
				}
			}
			// One shape has a floor under 200: an indented `key: value,` is a struct or object
			// literal as often as YAML, so a dereference is still read there unless its operand
			// looks random ([randomOperand]) — and about 1 generated value in 70 does not.
			floor := 200
			if sh == "  password: %s,\n" && (lead == "*" || lead == "&") {
				floor = 194
			}
			if caught < floor {
				t.Errorf("lead %q in %q: caught %d/200, floor %d", lead, sh, caught, floor)
			}
		}
	}
}

// TestRoundSixIniLayoutNeedsTheLineStart: the INI reading above is taken only where the NAME starts
// its line and the value ends it — an indented statement or a declaration is still code, and a
// dereference of an identifier there is still refused.
// ⚠ INVARIANT GUARD for the clean lines (round 5 refused all of these): it pins the boundary of
// this round's widening. The three prefixed lines are regression cases (RED at round 5).
func TestRoundSixIniLayoutNeedsTheLineStart(t *testing.T) {
	r := r5redactor(t)
	for _, l := range []string{
		"	apiKey = &cfg.APIKey",
		"    token = *tokenPtr",
		"var password = &passwordValue",
		"char *secret = &secretBuffer;",
		"	cfg.Password = *passwordFlag // set by -password",
	} {
		if out, hits := r.String(l); out != l {
			t.Errorf("damaged (hits %v): %q -> %q", hits, l, out)
		}
	}
	// …and behind a tool's line prefix the name still starts its line.
	r4mustCatch(t, []r4case{
		{"ini behind grep -n", "12:password = *Zq9abcdefgh\n", "*Zq9abcdefgh"},
		{"ini behind a path", "etc/my.cnf:password = &Zq9abcdefgh\n", "&Zq9abcdefgh"},
		{"ini behind Read's numbers", "    12\tpassword = *Zq9abcdefgh\n", "*Zq9abcdefgh"},
	})
}

// r6corrupt redacts NOTHING, corrupts the encoding of what it returns and claims every rule fired.
type r6corrupt struct {
	rules  []Hit
	double bool
}

func (c r6corrupt) Record(raw []byte) ([]byte, []Hit) {
	if c.double {
		enc, _ := json.Marshal(string(raw))
		return enc, c.rules
	}
	return append(append([]byte(nil), raw...), " trailing-bytes"...), c.rules
}

func (c r6corrupt) Blob(_ string, data []byte) ([]byte, []Hit) {
	return append(append([]byte(nil), data...), " trailing-bytes"...), c.rules
}

// TestRoundSixACorruptingRedactorScoresNothing: a redactor that leaves every plant in place but
// returns output the scorer cannot decode is credited with NOTHING. Round 5's scorer looked for a
// plant in the raw output and in its DECODED strings; an output that did not decode kept a plant
// holding `&`, `<`, `>` or a quote in escaped form, where neither search looked, and
// [Corpus.ruleFiredOn] then credited it — 8 plants over 60 seeds, all `symbol-password`.
func TestRoundSixACorruptingRedactorScoresNothing(t *testing.T) {
	credited := 0
	for seed := uint64(1); seed <= 60; seed++ {
		c := NewCorpus(seed)
		var hits []Hit
		seen := map[string]bool{}
		for _, p := range c.Plants {
			if !seen[p.Rule] {
				seen[p.Rule] = true
				hits = append(hits, Hit{Rule: p.Rule})
			}
		}
		for _, double := range []bool{false, true} {
			s := c.Score(r6corrupt{rules: hits, double: double})
			credited += s.Caught
			if s.Caught != 0 {
				missed := map[string]bool{}
				for _, m := range s.Missed {
					missed[m.Label] = true
				}
				for _, p := range c.Plants {
					if !missed[p.Label] {
						t.Errorf("seed %d (double=%v): credited %s (rule %s) to a redactor that redacts nothing", seed, double, p.Label, p.Rule)
					}
				}
			}
		}
	}
	t.Logf("plants credited to the corrupting redactors over 60 seeds: %d", credited)
	// POSITIVE CONTROL: the same scorer still credits the real redactor with everything.
	c := NewCorpus(1)
	if s := c.Score(r5redactor(t)); s.Caught != s.Planted {
		t.Errorf("control: the real redactor is credited %d/%d", s.Caught, s.Planted)
	}
}

// TestRoundSixHeldBackOracleIgnoresMarkers: the arming gate's oracle does not count text INSIDE a
// redaction marker as surviving secret. A fully redacted fine-grained GitHub PAT was reported
// MISSED at round 5: its marker, `[redacted:github-token:…]`, spells `github`, which is also the
// first 6-character window of `github_pat_…`. A KNOWN-ANSWER test, with the two controls that keep
// the fix from becoming a blind spot.
func TestRoundSixHeldBackOracleIgnoresMarkers(t *testing.T) {
	r2seed(64)
	pat := "github_" + "pat_" + r2pick(r2alnum, 22) + "_" + r2pick(r2alnum, 59)
	c := HeldBackCase{Kind: "leak", Label: "fine-grained-pat", Text: "export GH_TOKEN=" + pat + "\n", Secrets: []string{pat}}
	r := r5redactor(t)
	out, _ := r.Text([]byte(c.Text))
	if !strings.Contains(string(out), "[redacted:github-token:") || strings.Contains(string(out), pat[:20]) {
		t.Fatalf("the fixture is not what this test assumes — the PAT was not fully redacted by its own rule: %q", out)
	}
	if leaked(c, string(out)) {
		t.Errorf("a FULLY redacted PAT is scored as leaked: %q", out)
	}
	if s := ScoreHeldBack([]HeldBackCase{c}, r); s.Caught != 1 {
		t.Errorf("ScoreHeldBack: caught %d/1 (missed %v)", s.Caught, s.Missed)
	}
	// Control 1: a marker does not excuse what survives OUTSIDE it.
	if !leaked(c, string(out)+" "+pat[30:40]) {
		t.Error("control: a window of the secret outside the marker was not counted")
	}
	// Control 2: only a well-formed marker is masked — the secret's own prefix in brackets is not one.
	if !leaked(c, "export GH_TOKEN=[redacted: "+pat[:12]+"]\n") {
		t.Error("control: bracketed text that is not a marker hid a surviving window")
	}
	// Control 3 (the marker's TAG): a hex secret whose window equals the tag is not a leak either.
	hexSecret := r2pick("0123456789abcdef", 40)
	hc := HeldBackCase{Kind: "leak", Text: "DB_PASSWORD=" + hexSecret + "\n", Secrets: []string{hexSecret}}
	if leaked(hc, "DB_PASSWORD=[redacted:key-context:"+hexSecret[4:12]+"]\n") {
		t.Error("a window of the secret that is only the marker's tag was counted as a leak")
	}
}

// TestRoundSixPgpassNeedsPgpassStructure: five colon-separated fields with a number second are
// also `path:N:C:` in front of a line holding one more colon. Round 5 read both lines marked RED
// as `.pgpass` rows in their unstripped form.
func TestRoundSixPgpassNeedsPgpassStructure(t *testing.T) {
	r := r5redactor(t)
	clean := []string{
		"src/pkg/file.go:100:7://go:noescape", // RED at round 5
		"a.go:12:3://nolint:errcheck",         // RED at round 5
		"main.css:12:3:color:darkred",         // RED at round 5: a column number is not a database
		"internal/api/server.go:88:12:http://example.invalid:8080/path",
		"notes.md:3:1:see:https://example.invalid/docs",
		"a.go:12:https://example.invalid:8443",
		"src/pkg/file.go:100:goto:retry:handleFault",
		"a.go:12:TODO://fixme:tomorrow",
		"12:30:45:build:finished",
	}
	for _, l := range clean {
		if out, hits := r.String(l + "\n"); out != l+"\n" {
			t.Errorf("damaged (hits %v): %q -> %q", hits, l, out)
		}
	}
	// Recall: real rows, bare and behind every numbered grep prefix. ⚠ INVARIANT GUARD (caught at
	// round 5 too) — it pins that asking for structure did not lose a row.
	var cases []r4case
	for _, host := range []string{"db.example", "localhost", "*", "pg-primary.internal.example", "/var/run/postgresql"} {
		for _, port := range []string{"5432", "*", "25060"} {
			for _, p := range []string{"", "7:", "cfg/pgpass:7:", "cfg/pgpass:7:3:", "cfg/pgpass-7-", "    7\t"} {
				v := r5val()
				cases = append(cases, r4case{"pgpass " + host + " " + port + " behind " + p, p + host + ":" + port + ":alpha_db:app-user:" + v + "\n", v})
			}
		}
	}
	r4mustCatch(t, cases)
	// COST PIN: `grep -n` output over a line that is itself `word:word:word` still reads as a row.
	if out, _ := r.String("a.go:12:foo:bar:bazqux7\n"); !strings.Contains(out, "[redacted:pgpass:") {
		t.Errorf("a pinned cost line is no longer damaged — move it to `clean`: %q", out)
	}
}

// TestRoundSixStrongNamesTakeNumbersAndQuotedWords: a STRONG name takes a digits-only value, and a
// quoted value under an ALL_CAPS environment-style weak name is a literal. `DB_PASSWORD=482915`
// shipped at round 4 AND round 5 (the number refusal was written for `max_tokens = 4096`, which
// is not a secret name at all).
func TestRoundSixStrongNamesTakeNumbersAndQuotedWords(t *testing.T) {
	r := r5redactor(t)
	r2seed(65)
	for _, sh := range []string{"DB_PASSWORD=%s\n", "export PGPASSWORD=%s\n", "password: %s\n", "{\"password\": \"%s\"}\n",
		"--password %s\n", "db.password=%s\n", "<password>%s</password>\n", "PGPASSWORD=%s psql -h db.example\n"} {
		caught := 0
		for i := 0; i < 200; i++ {
			v := r2pick("123456789", 1) + r2pick("0123456789", 5)
			if out, _ := r.String(fmt.Sprintf(sh, v)); !strings.Contains(out, v) {
				caught++
			}
		}
		if caught != 200 {
			t.Errorf("a 6-digit value in %q: caught %d/200", sh, caught)
		}
	}
	for _, sh := range []string{"DB_PASSWORD_PROD=\"%s\"\n", "export API_TOKEN_STAGING='%s'\n", "docker run -e GITHUB_TOKEN_CI=\"%s\" img\n"} {
		caught := 0
		for _, w := range r6words {
			if out, _ := r.String(fmt.Sprintf(sh, w)); !strings.Contains(out, w) {
				caught++
			}
		}
		if caught != len(r6words) {
			t.Errorf("a quoted word in %q: caught %d/%d", sh, caught, len(r6words))
		}
		// …and a quoted NUMBER there, for the same reason.
		if out, _ := r.String(fmt.Sprintf(sh, "482915")); strings.Contains(out, "482915") {
			t.Errorf("a quoted number in %q shipped: %q", sh, out)
		}
	}
	// A hex CONSTANT is a number; a hex KEY is not: `0x` and more than 8 digits is still taken.
	// ⚠ INVARIANT GUARD (caught at round 5): it pins the bound on [numericLiteral].
	hexKey := "0x" + r2pick("0123456789abcdef", 64)
	if out, _ := r.String("SIGNING_KEY=" + hexKey + "\n"); strings.Contains(out, hexKey[:20]) {
		t.Errorf("a 32-byte hex key written with 0x shipped: %q", out)
	}
	// The clean direction. ⚠ INVARIANT GUARD for all but the two hex constants (RED at round 5):
	// a number is still not a credential in code notation, under a weak name, or under a name
	// that is not a secret's.
	clean := []string{
		"max_tokens = 4096",
		"max_tokens: 4096",
		"	token = 12345",
		"	Token: 4096,",
		"	secret := 1000",
		"token_count: 1200",
		"TOKEN_TTL=3600",
		"API_TOKEN_STAGING=12345678",
		"SCM_CREDS = 0x03",                           // RED at round 5
		"	IMAGE_REL_AMD64_TOKEN            = 0x000D", // RED at round 5
		"	SIOCGLIFTOKEN                 = -0x3f879678",
		"password: 12",
		"	auth := \"fail\"",
		"	HasToken:       cfgErr == nil,",
		"	seen[record.token] = (lineno, record)",
		"    credentials = {fields[0] for _lineno, fields in rows}",
		"	var HelpGoAuth = &base.Command{",
		"	pass := &analysis.Pass{",
		"		Credential: &syscall.Credential{",
		"		Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},",
		"	warn(\"api: -issue-credential refused: \" + err.Error())",
		"cairn-ui -issue-presence-token push|claim -presence-owner alice",
		"	t.Errorf(\"enc.EncodeToken: StartElement %s\", err)",
		"	// Here the function identified by \"repeat\" is overwritten.",
	}
	for _, l := range clean {
		if out, hits := r.String(l); out != l {
			t.Errorf("damaged (hits %v): %q -> %q", hits, l, out)
		}
	}
}

// TestRoundSixSelfTestHoldsAcrossSeeds: the self-test at the two seeds the round names, and a
// sweep beyond the 1–40 that `TestRoundFiveSelfTestHoldsAcrossSeeds` pins (see [r6seedSweep]).
// ⚠ INVARIANT GUARD: round 5 scored these seeds too. It pins that this round's rule changes and the
// scorer's third control cost no plant and refused no run.
func TestRoundSixSelfTestHoldsAcrossSeeds(t *testing.T) {
	seedsToRun := []uint64{1, 2000}
	for s := uint64(41); s < 41+uint64(*r6seedSweep); s++ {
		seedsToRun = append(seedsToRun, s)
	}
	for _, seed := range seedsToRun {
		var out bytes.Buffer
		if code := SelfTest(&out, seed); code != SelfTestOK {
			t.Errorf("seed %d: exit %d\n%s", seed, code, out.String())
		}
	}
	t.Logf("%d seeds", len(seedsToRun))
}
