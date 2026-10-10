package redact

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"io"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Tests for the findings of review rounds 0 and 1 on S1. Every secret is drawn at run time.

// TestTheLineRulesReadThroughEveryCopyPrefix: the model-visible copies of a file — Read's numbered
// copy, `grep -n`, a diff — carry a prefix before each line; the line rules must see through it.
func TestTheLineRulesReadThroughEveryCopyPrefix(t *testing.T) {
	r := testRedactor(t)
	for i, shape := range []string{
		"     1\tLOG=1\n     2\tDB_PASSWORD=%s\n",
		"1→LOG=1\n2→DB_PASSWORD=%s\n",
		".env:2:DB_PASSWORD=%s\n",
		"src/app/.env:14:export DB_PASSWORD=%s\n",
		"@@ -1 +1 @@\n-DB_PASSWORD=x\n+DB_PASSWORD=%s\n",
	} {
		v := rnd(uint64(100+i), alnum, 20) + "7"
		in := strings.Replace(shape, "%s", v, 1)
		if out, hits := r.String(in); strings.Contains(out, v) || len(hits) == 0 || hits[0].Rule != "key-context" {
			t.Errorf("shape %d: the value survived (hits %v):\n%s", i, hits, out)
		}
	}
	// The Secret rule through Read's numbered copy, indentation measured on the real YAML.
	v := base64.StdEncoding.EncodeToString([]byte(rnd(110, alnum, 18)))
	in := "     1\tkind: Secret\n     2\tmetadata:\n     3\t  name: alpha-db\n     4\tdata:\n     5\t  replica: " + v + "\n"
	out, _ := r.String(in)
	if strings.Contains(out, v) || !strings.Contains(out, "     3\t  name: alpha-db\n") {
		t.Fatalf("the numbered Secret copy:\n%s", out)
	}
}

// TestTheDenylistGlobCoversExactlyTheStatedKeys pins the glob's coverage claim BOTH ways: the
// structured copies it names are redacted, and the numbered tool_result copy it does NOT cover
// is not — so a later join that closes that gap is a deliberate change that turns this red.
func TestTheDenylistGlobCoversExactlyTheStatedKeys(t *testing.T) {
	d, err := parseDenylist(strings.NewReader("glob:*.secrets.yml\n"), "test")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := New(SelfTestKey(1), d)
	body := "plainvalue" + rnd(111, alnum, 6)
	covered := map[string]string{
		"Edit toolUseResult": `{"toolUseResult":{"filePath":"/w/app.secrets.yml","oldString":"` + body + `","newString":"x` + body +
			`","originalFile":"` + body + `","structuredPatch":[{"lines":["-` + body + `"]}]}}`,
		"Edit tool_use input": `{"type":"tool_use","input":{"file_path":"/w/app.secrets.yml","old_string":"` + body + `","new_string":"y` + body + `"}}`,
		"Read file copy":      `{"toolUseResult":{"type":"text","file":{"filePath":"/w/app.secrets.yml","content":"` + body + `"}}}`,
		"Write tool input":    `{"type":"tool_use","input":{"file_path":"/w/app.secrets.yml","content":"` + body + `"}}`,
	}
	for name, rec := range covered {
		if out, _ := r.Record([]byte(rec)); bytes.Contains(out, []byte(body)) {
			t.Errorf("%s: a denylisted file's content survived: %s", name, out)
		}
	}
	numbered := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t","content":"     1\t` + body + `"}]}}`
	if out, _ := r.Record([]byte(numbered)); !bytes.Contains(out, []byte(body)) {
		t.Fatal("the numbered tool_result copy was redacted by the glob — the coverage claim changed; update hostfiles.go")
	}
}

// TestSecretKeyIsOnePredicate pins THE predicate with literal names, both ways.
func TestSecretKeyIsOnePredicate(t *testing.T) {
	yes := []string{"DB_PASSWORD", "dbPassword", "db-password", "SecretAccessKey", "SessionToken", "accessToken",
		"refreshToken", "bearerToken", "auth", "client_secret", "GITHUB_TOKEN", "apiKey", "API_KEY", "Authorization",
		"x.api_key", "PRIVATE_KEY", "passphrase", "AWS_SECRET_ACCESS_KEY", "TOKEN_2"}
	no := []string{"max_tokens", "input_tokens", "TOKEN_URL", "DB_PASSWORD_FILE", "apiKeySource", "secret_name",
		"author", "oauth_provider", "token_count_total", "keyboard", "name", "value"}
	for _, k := range yes {
		if !SecretKey(k) {
			t.Errorf("SecretKey(%q) = false, want true", k)
		}
	}
	for _, k := range no {
		if SecretKey(k) {
			t.Errorf("SecretKey(%q) = true, want false", k)
		}
	}
	// The structural rule uses it — camelCase, spaces in the value, the key-context length floor.
	r := testRedactor(t)
	for _, rec := range []string{
		`{"SecretAccessKey":"%s"}`, `{"auths":{"registry.example":{"auth":"%s"}}}`, `{"refreshToken":"%s"}`,
	} {
		v := "a b " + rnd(112, alnum, 6)
		if out, _ := r.Record([]byte(strings.Replace(rec, "%s", v, 1))); bytes.Contains(out, []byte(v)) {
			t.Errorf("%s: the value survived: %s", rec, out)
		}
	}
	if out, _ := r.Record([]byte(`{"token":"Zq9x"}`)); bytes.Contains(out, []byte("Zq9x")) {
		t.Error("a 4-character secret field survived; the floor must match the key-context rule's")
	}
}

// TestNULSeparatedAndInvalidByteTextIsScanned is the coordinator's reading of O12: only a known
// file SIGNATURE makes bytes binary.
func TestNULSeparatedAndInvalidByteTextIsScanned(t *testing.T) {
	r := testRedactor(t)
	v := rnd(113, alnum, 30)
	environ := []byte("PATH=/bin\x00AWS_SECRET_ACCESS_KEY=" + v + "\x00TERM=xterm\x00")
	out, _ := r.Blob("environ.txt", environ)
	if bytes.Contains(out, []byte(v)) || !bytes.HasPrefix(out, []byte("PATH=/bin\x00")) || !bytes.HasSuffix(out, []byte("\x00TERM=xterm\x00")) {
		t.Fatalf("environ dump: %q", out)
	}
	w := rnd(114, alnum, 24) + "1"
	bad := []byte("x \xff\xfe y\nX_API_KEY=" + w + "\n\x80 z\n")
	out, _ = r.Blob("log.txt", bad)
	want := bytes.Replace(bad, []byte(w), []byte("[redacted:key-context:"+r.Tag(w)+"]"), 1)
	if !bytes.Equal(out, want) {
		t.Fatalf("invalid-byte text: only the matched span may change:\n got %q\nwant %q", out, want)
	}
	u := utf16LE("\ufeffSESSION_SECRET=" + w + "\n")
	out, _ = r.Blob("notes.txt", u)
	dec, _, ok := utf16BOM(out)
	if !ok || strings.Contains(dec, w) || !strings.Contains(dec, "SESSION_SECRET=[redacted:key-context:") {
		t.Fatalf("UTF-16: decoded=%v %q", ok, dec)
	}
	if bytes.Equal(out, u) {
		t.Fatal("UTF-16 blob unchanged")
	}
}

// pngWithText returns a real PNG carrying `text` in a tEXt chunk.
func pngWithText(g *gen, text string) []byte {
	png, _ := base64.StdEncoding.DecodeString(g.png())
	data := append([]byte("Comment\x00"), text...)
	var chunk bytes.Buffer
	_ = binary.Write(&chunk, binary.BigEndian, uint32(len(data)))
	chunk.WriteString("tEXt")
	chunk.Write(data)
	_ = binary.Write(&chunk, binary.BigEndian, crc32.ChecksumIEEE(append([]byte("tEXt"), data...)))
	at := 8 + 25 // the signature, then IHDR
	return append(append(append([]byte(nil), png[:at]...), chunk.Bytes()...), png[at:]...)
}

// TestASignatureBearingPayloadShipsEvenWithATokenInside is the O12 residual pinned AS a residual,
// and the guard REACHABLE: a PNG whose text chunk carries a token ships byte-identical — in an
// image item, and as the toolUseResult.file.base64 copy ALONE. Dropping the signature check makes
// the decoded bytes scanned and the value altered.
func TestASignatureBearingPayloadShipsEvenWithATokenInside(t *testing.T) {
	r := testRedactor(t)
	g := &gen{rng: rand.New(rand.NewPCG(21, 22))}
	tok := "ghp_" + g.pick(alnum, 36)
	img := base64.StdEncoding.EncodeToString(pngWithText(g, tok))
	if !IsBinary(pngWithText(g, tok)) {
		t.Fatal("fixture: the PNG is not binary by signature")
	}
	// CONTROL: the token IS visible to the table once the bytes are scanned as text.
	if _, hits := r.String(string(pngWithText(g, tok))); len(hits) == 0 {
		t.Fatal("control: the token in the PNG is not seen by the table at all — the case proves nothing")
	}
	for name, rec := range map[string]string{
		"image item":        `{"message":{"content":[{"type":"tool_result","content":[{"type":"image","source":{"data":"` + img + `"}}]}]}}`,
		"file.base64 alone": `{"toolUseResult":{"type":"image","file":{"base64":"` + img + `"}}}`,
		"data: URL":         `{"url":"data:image/png;base64,` + img + `"}`,
	} {
		if out, hits := r.Record([]byte(rec)); !bytes.Equal(out, []byte(rec)) || len(hits) != 0 {
			t.Errorf("%s: a signature-bearing payload was altered (hits %v)", name, hits)
		}
	}
}

// TestADuplicateKeyCannotHideAValue: every member is scanned and every member re-encoded.
func TestADuplicateKeyCannotHideAValue(t *testing.T) {
	r := testRedactor(t)
	tok := "ghp_" + rnd(115, alnum, 36)
	raw := []byte(`{"note":"` + tok + `","note":"ok","n":1}`)
	out, hits := r.Record(raw)
	if len(hits) == 0 || bytes.Contains(out, []byte(tok)) || !bytes.Contains(out, []byte(`"note":"ok"`)) {
		t.Fatalf("duplicate key: %s", out)
	}
	if bytes.Count(out, []byte(`"note"`)) != 2 {
		t.Fatalf("a duplicate member was dropped on re-encode: %s", out)
	}
}

// TestObjectKeysAreScanned: a secret in a KEY is redacted.
func TestObjectKeysAreScanned(t *testing.T) {
	r := testRedactor(t)
	tok := "ghp_" + rnd(116, alnum, 36)
	if out, hits := r.Record([]byte(`{"m":{"` + tok + `":1}}`)); len(hits) == 0 || bytes.Contains(out, []byte(tok)) {
		t.Fatalf("a key carrying a token survived: %s", out)
	}
}

// TestAPEMMentionLosesOnlyItsHeader: a file that MENTIONS a private-key header keeps every byte
// after it; a real block is redacted whole.
func TestAPEMMentionLosesOnlyItsHeader(t *testing.T) {
	r := testRedactor(t)
	header := "-----BEGIN RSA " + "PRIVATE KEY-----"
	src := "const header = \"" + header + "\"\n\nfunc parse(b []byte) error {\n\treturn decode(b, header)\n}\n"
	out, _ := r.String(src)
	tail := "\"\n\nfunc parse(b []byte) error {\n\treturn decode(b, header)\n}\n"
	if !strings.HasSuffix(out, tail) {
		t.Fatalf("a header MENTION ate the code after it:\n%s", out)
	}
}

// TestSelfTestRefusesToVouchOnEachBrokenControl drives every exit-2 branch.
func TestSelfTestRefusesToVouchOnEachBrokenControl(t *testing.T) {
	c := NewCorpus(5)
	real, _ := New(SelfTestKey(5), nil)
	greedy, _ := newWithRules(append(DefaultRules(), greedyRule), SelfTestKey(5), nil)
	ok := selfTestParts{corpus: c, declared: DeclaredPlants, identity: identity{}, greedy: greedy, real: real}
	if code := selfTest(io.Discard, ok); code != SelfTestOK {
		t.Fatalf("the unbroken parts exit %d", code)
	}
	cases := map[string]func(p *selfTestParts){
		"declared count wrong":        func(p *selfTestParts) { p.declared++ },
		"identity control catches":    func(p *selfTestParts) { p.identity = real },
		"greedy control damages none": func(p *selfTestParts) { p.greedy = identity{} },
	}
	for name, sabotage := range cases {
		p := ok
		sabotage(&p)
		var out bytes.Buffer
		if code := selfTest(&out, p); code != SelfTestNoVouch || !strings.Contains(out.String(), "COULD NOT VOUCH") {
			t.Errorf("%s: exit %d, want %d:\n%s", name, code, SelfTestNoVouch, out.String())
		}
	}
	// And exit 1 when a real plant is missed.
	p := ok
	p.real = without(t, "github-token")
	if code := selfTest(io.Discard, p); code != SelfTestFailed {
		t.Errorf("a missing rule: exit %d, want %d", code, SelfTestFailed)
	}
}

// TestScoreAssertsEachPlantsOwnRule: with the key-context rule gone, a value the Secret rule then
// removes is gone from the output — and still NOT caught, because the rule it was planted for
// never fired.
func TestScoreAssertsEachPlantsOwnRule(t *testing.T) {
	s := NewCorpus(seeds[0]).Score(without(t, "key-context"))
	var wrong []string
	for _, p := range s.WrongRule {
		wrong = append(wrong, p.Label)
	}
	if !slices.Contains(wrong, "blob-k8s-data-2") {
		t.Fatalf("a plant removed by ANOTHER rule was not reported as wrong-rule: %v", wrong)
	}
}

// attack is one fresh, run-time attack shape.
type attack struct {
	class, text string
	secret      string
	blob        bool
}

// freshAttacks are shapes written AFTER the review and NOT in the corpus, drawn from a seed the
// corpus never uses. ⚠ They were written by the same author as the rules, so this is a regression
// set across classes, not a blind recall measurement — real recall is Q4's host-local count.
func freshAttacks(seed uint64) []attack {
	g := &gen{rng: rand.New(rand.NewPCG(seed, 7))}
	v := func() string { return g.pick(alnum, 18) + g.pick(digit, 2) }
	// "§" marks the secret: "@" is part of a URL's grammar and must stay literal.
	var a []attack
	add := func(class, tmpl string, blob bool) {
		s := v()
		a = append(a, attack{class: class, text: strings.ReplaceAll(tmpl, "§", s), secret: s, blob: blob})
	}
	add("copy-prefix", "    12\tPOSTGRES_PASSWORD=§\n", false)
	add("copy-prefix", "3→CLIENT_SECRET=§\n", false)
	add("copy-prefix", "config/prod.env:7:STRIPE_API_KEY=§\n", false)
	add("copy-prefix", "+ANTHROPIC_AUTH_TOKEN=§\n", false)
	add("camel-field", `{"dbPassword":"§"}`, false)
	add("camel-field", `{"clientSecret":"§"}`, false)
	add("camel-field", `{"x":{"sessionToken":"§"}}`, false)
	add("shell-flag", "docker run --env API_TOKEN=§ img", false)
	add("shell-flag", "curl --user ci:§ https://ci.example.invalid", false)
	add("connection", "postgresql://app:§@db.example/alpha", false)
	add("connection", "rediss://:§@cache.example:6380", false)
	add("connection", "dbname=alpha user=app password=§ host=db", false)
	add("header", "X-Auth-Token: §", false)
	add("header", `{"headers":{"authorization":"Bearer §"}}`, false)
	add("header", "Authorization: token §", false)
	add("k8s", "env:\n- name: SMTP_PASSWORD\n  value: §\n", false)
	add("k8s", `{"env":[{"name":"API_KEY","value":"§"}]}`, false)
	add("nul-text", "A=1\x00GITLAB_TOKEN=§\x00B=2", true)
	add("invalid-bytes", "\xc3\x28 junk\nMAILGUN_API_KEY=§\n", true)
	add("utf16", "\ufeffSENTRY_AUTH_TOKEN=§\n", true)
	add("npmrc", "//npm.example.invalid/:_authToken=§\n", true)
	add("duplicate-key", `{"k":"ghp_§§","k":"ok"}`, false)
	return a
}

// TestAFreshAttackSet reports caught/missed PER CLASS over shapes the corpus does not carry.
func TestAFreshAttackSet(t *testing.T) {
	r := testRedactor(t)
	type tally struct{ caught, total int }
	byClass := map[string]*tally{}
	var missed []string
	for _, at := range freshAttacks(424242) {
		if byClass[at.class] == nil {
			byClass[at.class] = &tally{}
		}
		byClass[at.class].total++
		data := []byte(at.text)
		if at.class == "utf16" {
			data = utf16LE(at.text)
		}
		var out []byte
		if at.blob {
			out, _ = r.Blob("x.txt", data)
		} else {
			out, _ = r.Record(data)
		}
		visible := string(out)
		if s, _, ok := utf16BOM(out); ok {
			visible = s
		}
		if strings.Contains(visible, at.secret) {
			missed = append(missed, at.class+": "+strings.ReplaceAll(at.text, at.secret, "<v>"))
			continue
		}
		byClass[at.class].caught++
	}
	classes := make([]string, 0, len(byClass))
	for c := range byClass {
		classes = append(classes, c)
	}
	sort.Strings(classes)
	for _, c := range classes {
		t.Logf("%-14s caught=%d/%d", c, byClass[c].caught, byClass[c].total)
	}
	if len(missed) > 0 {
		t.Fatalf("missed:\n  %s", strings.Join(missed, "\n  "))
	}
}
