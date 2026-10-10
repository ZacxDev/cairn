package redact

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Seeds the corpus is measured at. More than one, because the secrets are random per seed and a
// rule that happens to fit one draw is not a rule that fits the format.
var seeds = []uint64{1, 2000, 987654321}

func testRedactor(t *testing.T) *Redactor {
	t.Helper()
	r, err := New(SelfTestKey(7), nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func without(t *testing.T, name string) *Redactor {
	t.Helper()
	var rules []Rule
	found := false
	for _, r := range DefaultRules() {
		if r.Name == name {
			found = true
			continue
		}
		rules = append(rules, r)
	}
	if !found {
		t.Fatalf("no rule named %s — the control would remove nothing", name)
	}
	r, err := newWithRules(rules, SelfTestKey(7), nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// rawScan applies the rule TABLE to a string exactly as given — no JSON decoding, no base64
// decoding. It is the "regex over raw bytes" the decoded pipeline exists to improve on, and the
// controls below use it to prove that decoding, not the table, is what catches an escaped value.
func rawScan(r *Redactor, s string) []Hit {
	var hits []Hit
	for _, rule := range r.rules {
		var hs []Hit
		s, hs = r.applyRule(rule, s)
		hits = append(hits, hs...)
	}
	return hits
}

// rnd draws a value in a format's alphabet AT RUN TIME. No credential-shaped literal is
// committed in this file: leakscan would (rightly) refuse it.
func rnd(seed uint64, alphabet string, n int) string {
	g := &gen{rng: rand.New(rand.NewPCG(seed, 99))}
	return g.pick(alphabet, n)
}

// TestTheCorpusIsFullyCaughtAndNothingCleanIsDamaged is closing-condition part 3, at three seeds.
func TestTheCorpusIsFullyCaughtAndNothingCleanIsDamaged(t *testing.T) {
	for _, seed := range seeds {
		var out bytes.Buffer
		code := SelfTest(&out, seed)
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		last := lines[len(lines)-1]
		want := "SUMMARY redaction: planted=26 caught=26 clean-damaged=0"
		if code != SelfTestOK || last != want {
			t.Errorf("seed %d: exit %d, last line %q, want exit 0 and %q\n%s", seed, code, last, want, out.String())
		}
	}
}

// TestTheCorpusPlantsExactlyTheDeclaredCount pins P to the declaration, at two seeds.
func TestTheCorpusPlantsExactlyTheDeclaredCount(t *testing.T) {
	for _, seed := range seeds[:2] {
		c := NewCorpus(seed)
		if len(c.Plants) != DeclaredPlants || DeclaredPlants != 26 {
			t.Fatalf("seed %d: %d plants, declared %d (literal 26)", seed, len(c.Plants), DeclaredPlants)
		}
		labels := map[string]bool{}
		for _, p := range c.Plants {
			if labels[p.Label] {
				t.Fatalf("duplicate plant label %s", p.Label)
			}
			labels[p.Label] = true
		}
	}
}

// TestWithoutTheDotenvRuleTheReportNamesIt is the plan's NEGATIVE control for the measurement.
func TestWithoutTheDotenvRuleTheReportNamesIt(t *testing.T) {
	c := NewCorpus(seeds[0])
	s := c.Score(without(t, "dotenv"))
	if s.Caught >= s.Planted {
		t.Fatalf("with the dotenv rule removed, caught=%d planted=%d: the corpus cannot see that rule", s.Caught, s.Planted)
	}
	var named []string
	for _, p := range s.Missed {
		named = append(named, p.Rule)
	}
	if !slices.Contains(named, "dotenv") {
		t.Fatalf("the report does not name the dotenv rule among the misses: %v", named)
	}
}

// TestTheSelfTestControlsCanEachGoRed proves the two instrument controls see what they claim.
func TestTheSelfTestControlsCanEachGoRed(t *testing.T) {
	c := NewCorpus(seeds[0])
	if s := c.Score(identity{}); s.Caught != 0 || s.Planted != DeclaredPlants {
		t.Fatalf("identity redactor: caught=%d planted=%d — the scorer cannot see a leak", s.Caught, s.Planted)
	}
	greedy, _ := newWithRules(append(DefaultRules(), greedyRule), SelfTestKey(7), nil)
	if s := c.Score(greedy); s.CleanDamaged == 0 {
		t.Fatal("a greedy rule damaged nothing: clean-damaged cannot move")
	}
}

// TestTheStructuralSecretRule: a YAML Secret's data values are replaced and metadata.name survives.
func TestTheStructuralSecretRule(t *testing.T) {
	r := testRedactor(t)
	v1 := base64.StdEncoding.EncodeToString([]byte(rnd(1, alnum, 16)))
	v2 := rnd(2, alnum, 18)
	src := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: alpha-db\ndata:\n  username: " + v1 +
		"\nstringData:\n  note.txt: " + v2 + "\n"
	out, hits := r.String(src)
	if strings.Contains(out, v1) || strings.Contains(out, v2) {
		t.Fatalf("a Secret data value survived:\n%s", out)
	}
	if !strings.Contains(out, "  name: alpha-db\n") {
		t.Fatalf("metadata.name did not survive:\n%s", out)
	}
	if len(hits) != 2 || hits[0].Rule != "k8s-secret" {
		t.Fatalf("hits = %v", hits)
	}
	// Control: the same text with `kind: ConfigMap` is untouched.
	cm := strings.Replace(src, "kind: Secret", "kind: ConfigMap", 1)
	if got, _ := r.String(cm); got != cm {
		t.Fatalf("a ConfigMap's data was redacted:\n%s", got)
	}
}

// TestAJSONEscapedSecretIsCaughtDecodedAndMissedRaw: decision 6's reason for decoding first.
func TestAJSONEscapedSecretIsCaughtDecodedAndMissedRaw(t *testing.T) {
	r := testRedactor(t)
	tok := "xoxb-" + rnd(3, digit, 12) + "-" + rnd(4, alnum, 24)
	esc := strings.ReplaceAll(tok, "-", escapedHyphen)
	raw := []byte(`{"type":"user","message":{"content":"bot ` + esc + `"}}`)
	out, hits := r.Record(raw)
	if len(hits) == 0 || bytes.Contains(out, []byte(esc)) || bytes.Contains(out, []byte(tok)) {
		t.Fatalf("the escaped token was not caught: %s", out)
	}
	// Control: the same table over the RAW line misses it.
	if rawHits := rawScan(r, string(raw)); len(rawHits) != 0 {
		t.Fatalf("the raw-byte scan caught the escaped token, so this test proves nothing: %v in %s", rawHits, raw)
	}
}

func TestTextBlobs(t *testing.T) {
	r := testRedactor(t)
	val := rnd(5, alnum, 24)
	src := "LOG_LEVEL=debug\nDB_PASSWORD=" + val + "\nREGION=alpha\n"
	out, hits := r.Blob("x.txt", []byte(src))
	want := strings.Replace(src, val, "[redacted:dotenv:"+r.Tag(val)+"]", 1)
	if string(out) != want || len(hits) != 1 {
		t.Fatalf("a dotenv blob is not the source with exactly that span replaced:\n%s", out)
	}
	clean := []byte("nothing secret here\nmax_tokens = 4096\n")
	if out, hits := r.Blob("y.txt", clean); !bytes.Equal(out, clean) || len(hits) != 0 {
		t.Fatalf("a clean text blob changed: %q", out)
	}
	// A JSON blob: escaped secret caught by decoded traversal; a 20-digit number byte-exact.
	gl := "glpat-" + rnd(6, alnum, 20)
	js := "{\n  \"id\": 12345678901234567890,\n  \"v\": \"" + strings.ReplaceAll(gl, "-", escapedHyphen) + "\"\n}\n"
	out, hits = r.Blob("z.txt", []byte(js))
	if len(hits) == 0 || strings.Contains(string(out), escapedHyphen) {
		t.Fatalf("the escaped secret in a JSON blob survived: %s", out)
	}
	if !strings.Contains(string(out), "12345678901234567890") {
		t.Fatalf("a 20-digit number was not kept byte-exact: %s", out)
	}
	// Control: the same blob redacted as ONE raw string misses the escaped secret.
	if rawHits := rawScan(r, js); len(rawHits) != 0 {
		t.Fatalf("redacting the JSON blob as raw text caught it, so the traversal is not what catches it")
	}
}

// TestBase64TextIsScannedDecoded: the floor and the decoder's four forms, each with a control.
func TestBase64TextIsScannedDecoded(t *testing.T) {
	r := testRedactor(t)
	tok := "ghp_" + rnd(7, alnum, 36) // 40 characters
	enc := base64.StdEncoding.EncodeToString([]byte(tok))
	if len(tok) != 40 || len(enc) != 56 {
		t.Fatalf("fixture arithmetic: token %d, encoding %d", len(tok), len(enc))
	}
	if out, hits := r.String(enc); len(hits) == 0 || out == enc {
		t.Fatal("a 56-character base64 encoding of a 40-character token was not caught")
	}
	// Control: with the floor back at 64 the same plant is missed.
	high := testRedactor(t)
	high.decode = func(s string) ([]byte, bool) { return decodeBase64Floor(s, 64) }
	if _, hits := high.String(enc); len(hits) != 0 {
		t.Fatal("a floor of 64 still caught a 56-character encoding — the floor control is not reaching the decoder")
	}

	secret := "DB_PASSWORD=" + rnd(8, alnum, 60) + "\n"
	padded := base64.StdEncoding.EncodeToString([]byte(secret))
	var wrapped strings.Builder
	for i := 0; i < len(padded); i += 76 {
		wrapped.WriteString(padded[i:min(i+76, len(padded))] + "\n")
	}
	forms := map[string]string{
		"padded":   padded,
		"unpadded": base64.RawStdEncoding.EncodeToString([]byte(secret)),
		"urlsafe":  base64.URLEncoding.EncodeToString([]byte(secret + "??>>")),
		"wrapped":  wrapped.String(),
	}
	stdOnly := testRedactor(t)
	stdOnly.decode = func(s string) ([]byte, bool) {
		if len(s) < MinBase64 {
			return nil, false
		}
		b, err := base64.StdEncoding.DecodeString(s)
		return b, err == nil
	}
	missedByStdOnly := 0
	for name, f := range forms {
		if _, hits := r.String(f); len(hits) == 0 {
			t.Errorf("the %s encoding of a text secret was not caught", name)
		}
		if _, hits := stdOnly.String(f); len(hits) == 0 {
			missedByStdOnly++
		}
	}
	if missedByStdOnly == 0 {
		t.Error("a StdEncoding-only decoder caught all four forms, so the four-encoding decoder is untested")
	}
	// Residual pinned AS a residual: the same encoding after a line of other text is NOT decoded.
	embedded := "the file holds:\n" + padded
	if _, hits := r.String(embedded); len(hits) != 0 {
		t.Fatalf("an EMBEDDED encoding was caught (%v): that residual closed — make it deliberate and update decision 6", hits)
	}
}

// TestBinaryContentShipsByteIdentical is O12: binary is never altered, while text around it is.
func TestBinaryContentShipsByteIdentical(t *testing.T) {
	r := testRedactor(t)
	g := &gen{rng: rand.New(rand.NewPCG(11, 12))}
	img := g.png()
	cases := map[string]string{
		"inline image item":     `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + img + `"}}]}]}}`,
		"file.base64 duplicate": `{"type":"user","toolUseResult":{"type":"image","file":{"base64":"` + img + `","type":"image/png"}}}`,
		"opencode attachment":   `{"type":"tool","state":{"attachments":[{"mime":"image/png","url":"data:image/png;base64,` + img + `"}]}}`,
	}
	for name, rec := range cases {
		if out, hits := r.Record([]byte(rec)); !bytes.Equal(out, []byte(rec)) || len(hits) != 0 {
			t.Errorf("%s: binary content changed (hits %v)", name, hits)
		}
	}
	// A text secret BESIDE the image is still caught, and the image survives re-encoding.
	npm := "npm_" + rnd(13, alnum, 36)
	beside := `{"content":[{"type":"image","source":{"data":"` + img + `"}},{"type":"text","text":"` + npm + `"}]}`
	out, hits := r.Record([]byte(beside))
	if len(hits) == 0 || bytes.Contains(out, []byte(npm)) || !bytes.Contains(out, []byte(img)) {
		t.Fatalf("beside an image: hits=%v, secret gone=%v, image intact=%v", hits, !bytes.Contains(out, []byte(npm)),
			bytes.Contains(out, []byte(img)))
	}
	// A `.txt` blob with a NUL in its first 8,000 bytes is binary by CONTENT and ships as is.
	val := rnd(14, alnum, 24)
	nul := append([]byte("header\x00\n"), []byte("DB_PASSWORD="+val+"\n")...)
	if out, _ := r.Blob("report.txt", nul); !bytes.Equal(out, nul) {
		t.Fatal("a NUL-carrying .txt blob was altered: binary must ship byte-identical (O12)")
	}
	// Control: a sniff keyed on the EXTENSION would have called it text — and the table WOULD
	// redact it then, so the byte-identity above is the content sniff's doing.
	if out, _ := r.String(string(nul)); out == string(nul) {
		t.Fatal("control: the table does not redact the NUL blob's text, so the sniff is untested")
	}
	// UTF-16 text is "binary" by the text rule: it ships unredacted (the stated residual).
	u16 := utf16le("\ufeffDB_PASSWORD=" + val + "\n")
	if out, _ := r.Blob("notes.txt", u16); !bytes.Equal(out, u16) {
		t.Fatal("a UTF-16 blob was altered; O12 ships non-UTF-8 text as it is")
	}
	// Thinking signatures and 64-hex digests are not text-bearing base64 and survive.
	sig := base64.StdEncoding.EncodeToString(g.bytes(240))
	dig := hex.EncodeToString(g.bytes(32))
	for _, v := range []string{sig, dig} {
		if out, hits := r.String(v); out != v || len(hits) != 0 {
			t.Errorf("a clean value was damaged: %d hits", len(hits))
		}
	}
}

func utf16le(s string) []byte {
	var b []byte
	for _, r := range s {
		b = append(b, byte(r), byte(r>>8))
	}
	return b
}

func TestTheTagIsKeyed(t *testing.T) {
	a, _ := New(bytes.Repeat([]byte{1}, 32), nil)
	b, _ := New(bytes.Repeat([]byte{2}, 32), nil)
	secret := rnd(15, alnum, 24)
	if a.Tag(secret) != a.Tag(secret) {
		t.Fatal("one secret under one key gave two tags")
	}
	if a.Tag(secret) == b.Tag(secret) {
		t.Fatal("two keys gave the same tag: the tag is not keyed")
	}
	sum := sha256.Sum256([]byte(secret))
	if a.Tag(secret) == hex.EncodeToString(sum[:])[:8] {
		t.Fatal("the tag equals the UNKEYED digest prefix: a guess could be confirmed by hashing (T15)")
	}
	if _, err := New([]byte("short"), nil); err == nil {
		t.Fatal("a 5-byte key was accepted")
	}
}

func TestAnUntouchedRecordKeepsItsBytes(t *testing.T) {
	r := testRedactor(t)
	raw := []byte(`{"b":1,"a":"x","n":12345678901234567890, "s":"ordinary text"}`)
	if out, hits := r.Record(raw); !bytes.Equal(out, raw) || len(hits) != 0 {
		t.Fatalf("an untouched record changed: %s", out)
	}
	// A redacted one keeps key ORDER and number TEXT.
	val := rnd(16, alnum, 20)
	raw = []byte(`{"z":1,"password":"` + val + `","a":12345678901234567890}`)
	out, _ := r.Record(raw)
	want := `{"z":1,"password":"[redacted:secret-field:` + r.Tag(val) + `"],"a":12345678901234567890}`
	want = strings.Replace(want, `"]`, `]"`, 1)
	if string(out) != want {
		t.Fatalf("re-encoded record:\n got  %s\n want %s", out, want)
	}
}

// leakscanCredential reads leakscan's `credential` rule and its credential NEGATIVE_CONTROLS out
// of tests/leakscan.py, so the containment relation is pinned to that file and not to a copy.
func leakscanCredential(t *testing.T) (*regexp.Regexp, []string) {
	t.Helper()
	path := filepath.Join("..", "..", "tests", "leakscan.py")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("tests/leakscan.py is missing (%v): it must be named in flake.nix's onlyGo filter, or this "+
			"containment test is green on a developer host and RED in the sandbox", err)
	}
	text := string(src)
	start := strings.Index(text, "\"credential\",\n")
	if start < 0 {
		t.Fatal("leakscan's credential rule could not be located")
	}
	var pattern strings.Builder
	frag := regexp.MustCompile(`^\s*r"(.*)",?\s*$`)
	for _, line := range strings.Split(text[start:], "\n")[1:] {
		if m := frag.FindStringSubmatch(line); m != nil {
			pattern.WriteString(m[1])
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		break
	}
	re, err := regexp.Compile(pattern.String())
	if err != nil {
		t.Fatalf("leakscan's credential pattern does not compile as RE2: %v", err)
	}
	ctl := regexp.MustCompile(`\("credential", (?:'([^']*)'|"([^"]*)")\)`)
	var controls []string
	for _, m := range ctl.FindAllStringSubmatch(text, -1) {
		controls = append(controls, m[1]+m[2])
	}
	return re, controls
}

var tokenRun = regexp.MustCompile(`[A-Za-z0-9+/_.-]+`)

// TestEveryLeakscanCredentialControlIsRedacted is the behavioural containment test (D5).
func TestEveryLeakscanCredentialControlIsRedacted(t *testing.T) {
	re, controls := leakscanCredential(t)
	if len(controls) < 3 {
		t.Fatalf("found %d credential controls in leakscan.py — the parse is broken", len(controls))
	}
	check := func(r *Redactor) []string {
		var failed []string
		for _, c := range controls {
			// POSITIVE CONTROL: leakscan's own rule must flag the control before redaction, or
			// the "after" check below is vacuous.
			if !re.MatchString(c) {
				t.Fatalf("leakscan's credential rule (as parsed, %q) does not match its own control %q — instrument broken", re, c)
			}
			// The credential is the longest token-alphabet run inside leakscan's own match —
			// the token, not the keyword. Asserting leakscan's regex no longer MATCHES would be
			// walkable: redacting the word `Bearer` alone breaks the match and leaves the token
			// (measured — that is how the dotenv rule "contains" the bearer control by itself).
			run := ""
			for _, m := range tokenRun.FindAllString(re.FindString(c), -1) {
				if len(m) > len(run) {
					run = m
				}
			}
			if out, _ := r.String(c); len(run) < 10 || strings.Contains(out, run) {
				failed = append(failed, c[:min(12, len(c))]+"…")
			}
		}
		return failed
	}
	if failed := check(testRedactor(t)); len(failed) > 0 {
		t.Fatalf("leakscan credential controls NOT redacted by internal/redact: %v", failed)
	}
	// Control: without the authorization rule the bearer control survives.
	if failed := check(without(t, "authorization")); len(failed) == 0 {
		t.Fatal("removing the authorization rule left every control redacted — the test cannot see a missing rule")
	}
	t.Logf("%d leakscan credential controls, all redacted", len(controls))
}

func TestTheDenylist(t *testing.T) {
	dir := t.TempDir()
	lit := "alpha-" + rnd(17, alnum, 10)
	p := filepath.Join(dir, "deny")
	if err := os.WriteFile(p, []byte("# local\nliteral:"+lit+"\nglob:*.env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := LoadDenylist(p)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := New(SelfTestKey(1), d)
	if out, hits := r.String("see " + lit + " there"); strings.Contains(out, lit) || len(hits) != 1 {
		t.Fatalf("a denylisted literal survived: %q", out)
	}
	if out, _ := r.Blob("prod.env", []byte("PLAIN=1\n")); strings.Contains(string(out), "PLAIN") {
		t.Fatal("a blob matching a denylist glob was not redacted whole")
	}
	rec := []byte(`{"file":{"filePath":"/work/x/.secrets/app.env","content":"PLAIN=1"}}`)
	if out, _ := r.Record(rec); bytes.Contains(out, []byte("PLAIN=1")) {
		t.Fatal("content of a record whose filePath matches a glob survived")
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDenylist(p); err == nil {
		t.Fatal("a world-readable denylist was accepted")
	}
	link := filepath.Join(dir, "link")
	_ = os.Chmod(p, 0o600)
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDenylist(link); err == nil {
		t.Fatal("a symlinked denylist was accepted")
	}
	bad := filepath.Join(dir, "bad")
	_ = os.WriteFile(bad, []byte("abc\n"), 0o600)
	if _, err := LoadDenylist(bad); err == nil {
		t.Fatal("a malformed denylist line was accepted")
	}
}

func TestTheHostKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "key")
	k1, err := LoadOrCreateKey(p)
	if err != nil || len(k1) != HostKeyBytes {
		t.Fatalf("create: %v (%d bytes)", err, len(k1))
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %o", st.Mode().Perm())
	}
	k2, err := LoadOrCreateKey(p)
	if err != nil || !bytes.Equal(k1, k2) {
		t.Fatal("the key was not reloaded unchanged")
	}
	_ = os.WriteFile(p, []byte("short"), 0o600)
	if _, err := LoadOrCreateKey(p); err == nil {
		t.Fatal("a short key was accepted (or regenerated)")
	}
	_ = os.WriteFile(p, k1, 0o644)
	_ = os.Chmod(p, 0o644)
	if _, err := LoadOrCreateKey(p); err == nil {
		t.Fatal("a world-readable key was accepted")
	}
}
