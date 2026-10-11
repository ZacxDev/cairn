package redact

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

// Round 4 (operator decision O15): the regression tests for the shapes review round 3 named — the
// tool line prefixes that hid a line from every line-anchored rule (🔴), and the secret-named keys
// no rule read (🟡B). Each was measured RED on the pre-O15 code (the merge of `main` into
// `zach/transcripts-s1`, before the key-context rewrite); the matrix is in the round-4 commit.
//
// 🔴 THE VALUES ARE LOWERCASE-AND-DIGIT ON PURPOSE. O15 adds an entropy rule that redacts a long
// token with all three character classes wherever it stands, so a mixed-case value would be caught
// by THAT rule and these tests would pass with the prefix normalisation or the key-context rule
// deleted. A lowercase value is invisible to the entropy rule, so each case below can be caught
// only by the mechanism it names (the PEM case is the exception, and says so).

var r4rng = rand.New(rand.NewPCG(4, 4^0x5bd1e995))

// r4val is a lowercase-and-digit value with a guaranteed digit: no entropy-rule catch possible.
func r4val() string {
	const set = "abcdefghijklmnopqrstuvwxyz0123456789"
	var b strings.Builder
	for i := 0; i < 13; i++ {
		b.WriteByte(set[r4rng.IntN(len(set))])
	}
	b.WriteByte('7')
	return b.String()
}

type r4case struct{ name, in, secret string }

func r4mustCatch(t *testing.T, cases []r4case) {
	t.Helper()
	r, _ := New(bytes.Repeat([]byte{7}, 32), nil)
	for _, c := range cases {
		out, _ := r.String(c.in)
		if strings.Contains(out, c.secret) {
			t.Errorf("%s: the secret survived:\n  in:  %q\n  out: %q", c.name, c.in, out)
		}
	}
}

// TestRoundFourToolPrefixesAreNormalisedBeforeMatching: every LINE-ANCHORED rule sees the content
// behind a tool's line prefix. The pgpass line and the k8s Secret `data` value carry no
// secret-sounding name, so only a rule anchored on the line's own start can catch them — which is
// exactly what a prefix used to hide.
func TestRoundFourToolPrefixesAreNormalisedBeforeMatching(t *testing.T) {
	var cases []r4case
	prefixes := map[string]func(n int) string{
		"grep-n single file (N:)":    func(n int) string { return fmt.Sprintf("%d:", n) },
		"grep -n context (N-)":       func(n int) string { return fmt.Sprintf("%d-", n) },
		"grep no -n (path:)":         func(n int) string { return "deploy/secret.yaml:" },
		"grep -rn (path:N:)":         func(n int) string { return fmt.Sprintf("deploy/secret.yaml:%d:", n) },
		"grep -rn context (path-N-)": func(n int) string { return fmt.Sprintf("deploy/secret.yaml-%d-", n) },
		"diff old (< )":              func(n int) string { return "< " },
		"diff new (> )":              func(n int) string { return "> " },
		"read copy then grep (N\\tpath:N:)": func(n int) string {
			return fmt.Sprintf("%6d\tdeploy/secret.yaml:%d:", n, n)
		},
	}
	for _, name := range sortedKeys(prefixes) {
		p := prefixes[name]
		v := r4val()
		cases = append(cases, r4case{"pgpass behind " + name, p(1) + "db.example:5432:alpha:app:" + v + "\n", v})
		v = r4val()
		manifest := []string{"apiVersion: v1", "kind: Secret", "metadata:", "  name: alpha-db", "data:", "  replica: " + v}
		var b strings.Builder
		for i, l := range manifest {
			b.WriteString(p(i+1) + l + "\n")
		}
		cases = append(cases, r4case{"k8s Secret data behind " + name, b.String(), v})
		v = r4val()
		cases = append(cases, r4case{"netrc password line behind " + name, p(1) + "machine api.example.invalid\n" +
			p(2) + "  login bot\n" + p(3) + "  password " + v + "\n", v})
	}
	r4mustCatch(t, cases)
}

// TestRoundFourRedactionLandsOnTheOriginalOffsets: a match found in a prefix-stripped VIEW
// replaces exactly the secret's bytes in the ORIGINAL text — the prefix, the line break and the
// next line's prefix are untouched. The output is pinned whole, so an off-by-one in the mapping
// back (into the newline, or into the next line's prefix) is red.
func TestRoundFourRedactionLandsOnTheOriginalOffsets(t *testing.T) {
	r, _ := New(bytes.Repeat([]byte{7}, 32), nil)
	for _, p := range []string{"3:", "3-", "deploy/pg.pass:", "deploy/pg.pass:3:", "< ", "> ", "     3\t"} {
		v := r4val()
		in := p + "db.example:5432:alpha:app:" + v + "\n" + p + "next line\n"
		want := p + "db.example:5432:alpha:app:[redacted:pgpass:" + r.Tag(v) + "]\n" + p + "next line\n"
		if out, _ := r.String(in); out != want {
			t.Errorf("prefix %q:\n got  %q\n want %q", p, out, want)
		}
	}
}

// TestRoundFourPEMBodyBehindEveryToolPrefix: no line of a private-key body survives behind the
// prefixes round 3 named. ⚠ A PEM body is base64, so the entropy rule would also catch most body
// lines on its own; the assertion is about the OUTCOME (no body line ships), not about which rule
// produced it — the prefix-only coverage is pinned by the test above.
func TestRoundFourPEMBodyBehindEveryToolPrefix(t *testing.T) {
	r2seed(41)
	r, _ := New(bytes.Repeat([]byte{7}, 32), nil)
	body := base64.StdEncoding.EncodeToString([]byte(r2pick(r2alnum, 200)))
	lines := []string{"-----BEGIN OPENSSH " + "PRIVATE KEY-----", body[:70], body[70:140], body[140:210], body[210:],
		"-----END OPENSSH " + "PRIVATE KEY-----"}
	prefixes := map[string]func(n int) string{
		"N:":      func(n int) string { return fmt.Sprintf("%d:", n) },
		"N-":      func(n int) string { return fmt.Sprintf("%d-", n) },
		"path:":   func(n int) string { return "keys/id_ed25519:" },
		"path-N-": func(n int) string { return fmt.Sprintf("keys/id_ed25519-%d-", n) },
		"< ":      func(n int) string { return "< " },
		"> ":      func(n int) string { return "> " },
	}
	for _, name := range sortedKeys(prefixes) {
		var b strings.Builder
		for i, l := range lines {
			b.WriteString(prefixes[name](i+1) + l + "\n")
		}
		out, _ := r.String(b.String())
		for i, seg := range []string{body[:70], body[70:140], body[140:210], body[210:]} {
			if strings.Contains(out, seg) {
				t.Errorf("%s: PEM body line %d survived", name, i+1)
			}
		}
	}
}

// TestRoundFourNamedKeysInEveryNotation: a value attached to a secret-sounding NAME is redacted in
// the notations round 3 named — systemd, process-environment assignment, CLI flags, SQL,
// kubeconfig, XML/.NET config.
func TestRoundFourNamedKeysInEveryNotation(t *testing.T) {
	type shape struct{ name, format string }
	shapes := []shape{
		{"systemd Environment=", "Environment=DB_PASSWORD=%s\n"},
		{"systemd Environment= quoted", "Environment=\"API_TOKEN=%s\" \"LOG_LEVEL=debug\"\n"},
		{"python os.environ[] =", "os.environ[\"DB_PASSWORD\"] = \"%s\"\n"},
		{"python os.environ[] = single", "os.environ['API_KEY']='%s'\n"},
		{"ruby ENV[] =", "ENV['SECRET_TOKEN'] = '%s'\n"},
		{"node process.env. =", "process.env.DB_PASSWORD = '%s';\n"},
		{"go os.Setenv", "os.Setenv(\"API_TOKEN\", \"%s\")\n"},
		{"flag --password=", "pg_dump --host db --password=%s alpha\n"},
		{"flag --password <v>", "deploy --password %s --verbose\n"},
		{"flag --db-password <v>", "migrate --db-password %s up\n"},
		{"flag --token=", "client login --token=%s\n"},
		{"mysql -p<pw>", "mysql -uroot -p%s alpha\n"},
		{"redis-cli -a", "redis-cli -h cache -a %s ping\n"},
		{"sshpass -p", "sshpass -p %s ssh bot@host\n"},
		{"SQL IDENTIFIED BY", "CREATE USER 'app'@'%%' IDENTIFIED BY '%s';\n"},
		{"SQL WITH PASSWORD", "ALTER USER app WITH PASSWORD '%s';\n"},
		{"SQL ROLE PASSWORD", "CREATE ROLE app LOGIN PASSWORD '%s';\n"},
		{"kubeconfig token", "users:\n- name: admin\n  user:\n    token: %s\n"},
		{"php/ruby =>", "'password' => '%s',\n"},
		{"ruby symbol =>", ":api_key => \"%s\"\n"},
		{"xml element", "<password>%s</password>\n"},
		{".NET appSettings", "<add key=\"ApiKey\" value=\"%s\" />\n"},
		{"java properties dotted", "spring.datasource.password=%s\n"},
		{"http header in curl", "curl -H 'X-Api-Key: %s' https://api.example.invalid\n"},
	}
	var cases []r4case
	for _, s := range shapes {
		v := r4val()
		cases = append(cases, r4case{s.name, fmt.Sprintf(s.format, v), v})
	}
	r4mustCatch(t, cases)

	// kubeconfig `client-key-data` is a base64 PEM; base64 is mixed-case, so this case is the
	// entropy rule's AND the name's — asserted as an outcome.
	r2seed(42)
	kd := base64.StdEncoding.EncodeToString([]byte("-----BEGIN EC " + "PRIVATE KEY-----\n" + r2pick(r2alnum, 120)))
	r4mustCatch(t, []r4case{{"kubeconfig client-key-data", "users:\n- name: admin\n  user:\n    client-key-data: " + kd + "\n", kd}})
}

// TestRoundFourCleanProbesNamedByRoundThree: the clean shapes round 3 measured DAMAGED — prose
// after a secret word, i18n strings, a `?key=` slug, minified JS — survive byte-identical.
func TestRoundFourCleanProbesNamedByRoundThree(t *testing.T) {
	r, _ := New(bytes.Repeat([]byte{7}, 32), nil)
	lines := []string{
		`		Password: hashed,`,
		`  passwordTooShort: "Password must be at least 8 characters",`,
		`  "invalidToken": "The token is invalid or has expired.",`,
		`  password: "Password",`,
		`  wrongPassword: 'Wrong password',`,
		`see https://docs.example.invalid/start?key=getting-started for setup`,
		`function(e){var t=e.password,n=e.token;return{password:t,token:n}}`,
		`!function(e){e.apiKey=e.apiKey||null,e.secret=void 0}(window);`,
		`The secret: keep it out of the logs.`,
		`Token: the bearer token issued by the IdP.`,
		`const token = await getToken();`,
		`    let token = std::env::var("TOKEN")?;`,
		`  session.headers["Authorization"] = f"Bearer {token}"`,
		`git log --oneline: 9f2c4e1a7b3d5f6e8a0c2b4d6f8e1a3c5b7d9f0e fix the token refresh`,
		`toolu_01AbCdEfGhIjKlMnOpQrStUv msg_01ZyXwVuTsRqPoNmLkJiHgFe`,
		`"integrity": "sha512-Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MEFCQ0RFRkdISUpLTE1OT1BRUlNUVVZXWFla",`,
		`github.com/example/mod v1.2.3 h1:Ab3dEfGh1jKlMn0pQrStUvWxYz0123456789AbCdEfG=`,
		`import { UserProfileComponentV2 } from "./components/UserProfileComponentV2";`,
	}
	damaged := 0
	for _, l := range lines {
		in := l + "\n"
		if out, _ := r.String(in); out != in {
			damaged++
			t.Errorf("damaged: %q -> %q", l, out)
		}
	}
	t.Logf("damaged=%d/%d", damaged, len(lines))
}

// TestRoundFourCleanShapesFoundWhileBuildingIt: clean lines an INTERMEDIATE round-4 build damaged —
// found by running it over this repository's own tracked text and over a fixer-written probe list,
// never over a held-back set. ⚠ They were not measured against the pre-O15 code (most were never
// damaged there, because the rules that damaged them did not exist), so they are guards on this
// build's own regressions, not round-3 regression coverage.
func TestRoundFourCleanShapesFoundWhileBuildingIt(t *testing.T) {
	r, _ := New(bytes.Repeat([]byte{7}, 32), nil)
	lines := []string{
		"	dsn := strings.TrimSpace(os.Getenv(DSNEnv))",
		"			firstSeen[record.Token] = seenRow{r.lineno, record}",
		"		token:      &token,",
		"	rec := l.do(http.MethodPost, SignInPath, url.Values{FieldToken: {token}}, cookies...)",
		`	Secret: []byte("fixture-shared-value"),`,
		`	req.Header.Set("Authorization", "Bearer "+tok)`,
		"Azure `AccountKey=`, ADO.NET `Password=` and JDBC `?password=` are each read",
		`	line := "token="+tok+" auth="+mode`,
		`		t.Fatalf("want a 43-character token:%q", out)`,
		"	return fmt.Sprintf(\"credential=%s token=<redacted: shown once, on issue>\", id)",
		"--- PASS: TestTheTagIsKeyed (0.00s)",
		"passes=$(grep -c '^PASS ' out.txt)",
		"	ClientAuth: tls.RequireAnyClientCert,",
		"method=GET path=/api/v1/snapshot status=200 auth=cookie",
		"	seen[token] = struct{}{}",
		"	deliverToken = func(_ *os.File, _ string) error {",
		"setuid binaries (`su`, `passwd`, `newgrp`, `gpasswd`)",
		"secret_key_base is generated by rails credentials:edit",
		"Usage: login [--token TOKEN] [--password PASSWORD]",
		`ENV_TOKEN = "CAIRN_TOKEN"`,
		"func TestAnUnknownRouteIsA404AndNeverA405(t *testing.T) {",
		"Rotate the API key every 90 days.",
		"pg_dump --password-file /run/secrets/pg alpha",
		`{"token_type": "Bearer", "expires_in": 3600}`,
		`  "forgotPassword": "Forgot your password?",`,
		"  secret: !Ref DbSecret",
		"kubectl create secret generic ssh --from-file=ssh-privatekey=~/.ssh/id_rsa",
		"user.password = await hash(password, 10);",
		`<input type="password" name="password" autocomplete="current-password">`,
		"  -p, --password string   database password",
		`FATAL:  password authentication failed for user "postgres"`,
		"Enter passphrase for key '/home/dev/.ssh/id_ed25519':",
		"export GITHUB_TOKEN=***",
	}
	for _, l := range lines {
		if out, hits := r.String(l); out != l {
			t.Errorf("damaged (hits %v): %q -> %q", hits, l, out)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	for i := 1; i < len(ks); i++ {
		for j := i; j > 0 && ks[j] < ks[j-1]; j-- {
			ks[j], ks[j-1] = ks[j-1], ks[j]
		}
	}
	return ks
}
