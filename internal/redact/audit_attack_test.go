// Review round 2's attack, PEM, rate and clean measurements, ADOPTED as permanent regression
// tests so that none of their numbers can move silently again. They were written by the round-2
// auditor (not by the rules' author) against S1 at 2ba3e5c, where they measured the regressions
// recorded beside each assertion. Every value is drawn at RUN time — from a SEEDED generator, so a
// failure reproduces; the auditor's original drew from crypto/rand.
package redact

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"testing"
)

const r2alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
const r2sym = "!@#$%^&*()-_=+[]{};:.<>?/|~"

// r2rng is reseeded by each adopted test (r2seed) so the four are independent of test order.
var r2rng = rand.New(rand.NewPCG(2, 2))

func r2seed(s uint64) { r2rng = rand.New(rand.NewPCG(s, s^0x5bd1e995)) }

func r2pick(set string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(set[r2rng.IntN(len(set))])
	}
	return b.String()
}

// r2tok: alnum with a guaranteed digit + upper + lower.
func r2tok(n int) string { return "Q" + r2pick(r2alnum, n-3) + "7z" }

type r2case struct {
	class, name string
	mode        string // "s" String, "r" Record, "b" Blob
	in          string
	secret      string
}

func r2jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }

func r2cases() []r2case {
	var cs []r2case
	add := func(class, name, mode, in, secret string) {
		cs = append(cs, r2case{class, name, mode, in, secret})
	}
	// --- YAML block scalars / config YAML (not k8s Secret)
	v := r2tok(24)
	add("yaml", "block-scalar password |", "s", "database:\n  password: |\n    "+v+"\n  host: db\n", v)
	v = r2tok(24)
	add("yaml", "plain yaml password", "s", "database:\n  user: app\n  password: "+v+"\n", v)
	v = r2tok(24)
	add("yaml", "quoted yaml api_key", "s", "service:\n  api_key: \""+v+"\"\n", v)
	v = r2tok(24)
	add("yaml", "folded > secret", "s", "auth:\n  client_secret: >\n    "+v+"\n", v)
	// --- export lines
	v = r2tok(28)
	add("export", "export DB_PASSWORD=", "s", "export DB_PASSWORD="+v+"\n", v)
	v = r2tok(28)
	add("export", "export PGPASSWORD=", "s", "export PGPASSWORD="+v+"\n", v)
	v = r2tok(28)
	add("export", "export MYSQL_PWD=", "s", "export MYSQL_PWD="+v+"\n", v)
	v = r2tok(28)
	add("export", "export SMTP_PASS=", "s", "export SMTP_PASS="+v+"\n", v)
	v = r2tok(28)
	add("export", "export OPENAI_KEY='…'", "s", "export OPENAI_KEY='"+v+"'\n", v)
	// --- HCL / Terraform
	v = r2tok(24)
	add("hcl", "password = \"…\"", "s", "resource \"aws_db_instance\" \"main\" {\n  username = \"app\"\n  password = \""+v+"\"\n}\n", v)
	v = r2tok(24)
	add("hcl", "admin_password = \"…\"", "s", "  admin_password = \""+v+"\"\n", v)
	v = r2tok(24)
	add("hcl", "tfvars db_password", "s", "db_password = \""+v+"\"\nregion = \"x\"\n", v)
	// --- INI
	v = r2tok(24)
	add("ini", "[database] password = ", "s", "[database]\nhost = db\npassword = "+v+"\n", v)
	v = r2tok(40)
	add("ini", "aws credentials", "s", "[default]\naws_access_key_id = AKIA"+r2pick("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", 16)+"\naws_secret_access_key = "+v+"\n", v)
	v = r2tok(24)
	add("ini", ".pgpass line", "s", "db.internal:5432:app:app:"+v+"\n", v)
	v = r2tok(20)
	add("ini", ".netrc password", "s", "machine api.example.test login bot password "+v+"\n", v)
	// --- source string literals
	v = r2tok(32)
	add("source", "python AWS_SECRET_KEY = \"\"", "s", "AWS_SECRET_KEY = \""+v+"\"\n", v)
	v = r2tok(32)
	add("source", "js const apiSecret = ''", "s", "const apiSecret = '"+v+"';\n", v)
	v = r2tok(32)
	add("source", "py stripe_key = ''", "s", "    stripe_key = '"+v+"'\n", v)
	v = r2tok(32)
	add("source", "go token := \"\"", "s", "\ttoken := \""+v+"\"\n", v)
	v = r2tok(32)
	add("source", "py kwarg connect(password=)", "s", "conn = psycopg2.connect(host=\"db\", user=\"app\", password=\""+v+"\")\n", v)
	v = r2tok(32)
	add("source", "py kwarg first arg (password=", "s", "conn = connect(password=\""+v+"\")\n", v)
	v = r2tok(32)
	add("source", "js object literal apiKey:", "s", "const cfg = { apiKey: \""+v+"\", region: 'x' };\n", v)
	v = r2tok(32)
	add("source", "py dict 'secret_key':", "s", "CONFIG = {'secret_key': '"+v+"'}\n", v)
	v = r2tok(32)
	add("source", "java String password =", "s", "    private static final String DB_PASSWORD = \""+v+"\";\n", v)
	// --- URL query strings
	v = r2tok(32)
	add("url-query", "?token=", "s", "curl https://api.example.test/v1/items?token="+v+"&page=2\n", v)
	v = r2tok(32)
	add("url-query", "?api_key=", "s", "GET https://maps.example.test/geo?q=x&api_key="+v+"\n", v)
	v = r2tok(32)
	add("url-query", "&access_token=", "s", "https://graph.example.test/me?fields=id&access_token="+v+"\n", v)
	v = r2tok(32)
	add("url-query", "presigned X-Amz-Signature", "s", "https://b.s3.amazonaws.com/k?X-Amz-Credential=AKIA/x&X-Amz-Signature="+strings.ToLower(r2pick("0123456789abcdef", 64))+"\n", "")
	// --- JWK
	d := base64.RawURLEncoding.EncodeToString([]byte(r2pick(r2alnum, 48)))
	add("jwk", "\"d\": in JWK (record)", "r", `{"type":"x","jwk":{"kty":"EC","crv":"P-256","x":"abc","y":"def","d":"`+d+`"}}`, d)
	d = base64.RawURLEncoding.EncodeToString([]byte(r2pick(r2alnum, 48)))
	add("jwk", "\"d\": in JWK printed (string)", "s", `{"kty":"RSA","n":"xyz","e":"AQAB","d":"`+d+`","p":"q"}`, d)
	// --- SSH key file content
	body := base64.StdEncoding.EncodeToString([]byte(r2pick(r2alnum, 300)))
	var wrapped strings.Builder
	for i := 0; i < len(body); i += 70 {
		e := i + 70
		if e > len(body) {
			e = len(body)
		}
		wrapped.WriteString(body[i:e] + "\n")
	}
	add("ssh", "OPENSSH PRIVATE KEY", "s", "-----BEGIN OPENSSH "+"PRIVATE KEY-----\n"+wrapped.String()+"-----END OPENSSH "+"PRIVATE KEY-----\n", body[70:140])
	add("ssh", "OPENSSH key via Read numbered", "s", "     1\t-----BEGIN OPENSSH "+"PRIVATE KEY-----\n     2\t"+body[:70]+"\n     3\t"+body[70:140]+"\n     4\t-----END OPENSSH "+"PRIVATE KEY-----\n", body[70:140])
	add("ssh", "RSA key JSON-escaped in GCP sa", "r", `{"type":"service_account","private_key_id":"`+strings.ToLower(r2pick("0123456789abcdef", 40))+`","private_key":"-----BEGIN `+"PRIVATE"+` KEY-----\n`+body[:64]+`\n`+body[64:128]+`\n-----END `+"PRIVATE"+` KEY-----\n"}`, body[64:128])
	// --- Azure / GCP connection strings
	ak := base64.StdEncoding.EncodeToString([]byte(r2pick(r2alnum, 64)))
	add("conn", "Azure storage AccountKey", "s", "DefaultEndpointsProtocol=https;AccountName=acct;AccountKey="+ak+";EndpointSuffix=core.windows.net", ak)
	ak = base64.StdEncoding.EncodeToString([]byte(r2pick(r2alnum, 32)))
	add("conn", "Azure SB SharedAccessKey", "s", "Endpoint=sb://ns.servicebus.windows.net/;SharedAccessKeyName=Root;SharedAccessKey="+ak, ak)
	v = r2tok(24)
	add("conn", "ADO.NET Password=", "s", "Server=tcp:x.database.windows.net,1433;Database=d;User ID=u;Password="+v+";Encrypt=True;", v)
	v = r2tok(24)
	add("conn", "JDBC ?password=", "s", "jdbc:postgresql://db:5432/app?user=app&password="+v, v)
	v = r2tok(24)
	add("conn", "mongodb+srv userinfo", "s", "mongodb+srv://app:"+v+"@cluster0.example.test/db", v)
	// --- Docker config.json
	auth := base64.StdEncoding.EncodeToString([]byte("user:" + r2tok(20)))
	add("docker", "config.json auths.auth (record)", "r", `{"auths":{"ghcr.io":{"auth":"`+auth+`"}}}`, auth)
	add("docker", "config.json printed (string)", "s", "{\n  \"auths\": {\n    \"ghcr.io\": {\n      \"auth\": \""+auth+"\"\n    }\n  }\n}\n", auth)
	v = r2tok(24)
	add("docker", "docker login -p", "s", "docker login -u bot -p "+v+" ghcr.io\n", v)
	// --- symbol-bearing passwords
	for i := 0; i < 6; i++ {
		v = "Aa9" + r2pick(r2alnum+r2sym, 17)
		v = strings.NewReplacer("#", "x", ",", "y", "'", "z", "\"", "w").Replace(v)
		add("symbol-pw", fmt.Sprintf("dotenv DB_PASSWORD symbols #%d", i), "s", "DB_PASSWORD="+v+"\n", v)
	}
	v = "Xk9$mP2qL8vR4tW6"
	v = "Zq" + r2pick(r2alnum, 5) + "$" + r2pick(r2alnum, 8)
	add("symbol-pw", "dotenv with one $", "s", "API_SECRET="+v+"\n", v)
	v = "Zq" + r2pick(r2alnum, 5) + ";" + r2pick(r2alnum, 8)
	add("symbol-pw", "dotenv with one ;", "s", "API_SECRET="+v+"\n", v)
	v = "Zq" + r2pick(r2alnum, 5) + "(" + r2pick(r2alnum, 8)
	add("symbol-pw", "libpq password= with (", "s", "host=db password="+v+" dbname=x", v)
	// --- code-shaped disguise
	v = "Tr" + r2pick("abcdefghij", 4) + ".Horse" + r2pick("abcdef", 3) + ".Battery" + r2pick("xyz", 3)
	add("code-shaped", "dotted passphrase (attributePath)", "s", "DB_PASSWORD="+v+"\n", v)
	v = "correct" + r2pick("abcdefghijklmnop", 6) + "Horse"
	add("code-shaped", "letters-only passphrase (bareWord)", "s", "DB_PASSWORD="+v+"\n", v)
	v = "getenv_fallback_" + r2tok(16)
	add("code-shaped", "getenv_fallback_ prefix", "s", "PASSWORD="+v+"\n", v)
	v = "<" + r2tok(20) + ">"
	add("code-shaped", "angle-bracketed real", "s", "API_TOKEN="+r2tok(4)+"<"+v+"\n", v)
	// --- SecretKey END rule
	v = r2tok(24)
	add("key-name", "apiKeyValue", "s", "apiKeyValue="+v+"\n", v)
	v = r2tok(24)
	add("key-name", "SECRET_KEY_BASE (rails)", "s", "SECRET_KEY_BASE="+v+"\n", v)
	v = "base64:" + base64.StdEncoding.EncodeToString([]byte(r2pick(r2alnum, 32)))
	add("key-name", "APP_KEY (laravel)", "s", "APP_KEY="+v+"\n", strings.TrimPrefix(v, "base64:"))
	v = r2tok(32)
	add("key-name", "ENCRYPTION_KEY", "s", "ENCRYPTION_KEY="+v+"\n", v)
	v = r2tok(32)
	add("key-name", "SIGNING_KEY", "s", "JWT_SIGNING_KEY="+v+"\n", v)
	v = r2tok(32)
	add("key-name", "secretAccessKeyId-ish (json)", "r", `{"accessKeySecret":"`+v+`"}`, v)
	v = r2tok(32)
	add("key-name", "json clientSecretValue", "r", `{"clientSecretValue":"`+v+`"}`, v)
	v = r2tok(32)
	add("key-name", "json password_hash (ok to skip)", "r", `{"password_hash":"`+v+`"}`, v)
	v = r2tok(32)
	add("key-name", "json \"pass\"", "r", `{"user":"u","pass":"`+v+`"}`, v)
	v = r2tok(32)
	add("key-name", "json \"pwd\"", "r", `{"user":"u","pwd":"`+v+`"}`, v)
	// --- k8s env order reversed
	v = r2tok(24)
	add("k8s", "env value before name", "s", "env:\n  - value: "+v+"\n    name: DB_PASSWORD\n", v)
	v = r2tok(24)
	add("k8s", "env normal", "s", "env:\n  - name: DB_PASSWORD\n    value: "+v+"\n", v)
	// --- signature smuggling (blobs)
	v = r2tok(32)
	add("sig-smuggle", "blob text starting %PDF-", "b", "%PDF-1.7 extracted:\nDB_PASSWORD="+v+"\n", v)
	v = r2tok(32)
	add("sig-smuggle", "blob text starting BZh", "b", "BZh91 log line\nGITHUB_TOKEN=ghp_"+r2pick(r2alnum, 36)+"\nDB_PASSWORD="+v+"\n", v)
	v = r2tok(32)
	add("sig-smuggle", "blob text starting GIF89a", "b", "GIF89a header dump then\nDB_PASSWORD="+v+"\n", v)
	v = r2tok(32)
	add("sig-smuggle", "record string starting %PDF-", "r", `{"type":"user","message":{"content":[{"type":"tool_result","content":"%PDF-1.4\nDB_PASSWORD=`+v+`\n"}]}}`, v)
	pdfb := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\nDB_PASSWORD=" + r2tok(24) + "\n"))
	add("sig-smuggle", "base64 of text prefixed %PDF-", "r", `{"x":"`+pdfb+`"}`, pdfb)
	// --- encodings
	v = r2tok(32)
	u16 := []byte{}
	for _, c := range "DB_PASSWORD=" + v + "\n" {
		u16 = append(u16, byte(c), 0)
	}
	add("encoding", "UTF-16LE no BOM blob", "b", string(u16), v)
	u16b := append([]byte{0xff, 0xfe}, u16...)
	v2 := v
	add("encoding", "UTF-16LE BOM blob", "b", string(u16b), v2)
	b64u16 := base64.StdEncoding.EncodeToString(u16b)
	add("encoding", "base64 of BOM UTF-16 dotenv", "r", `{"x":"`+b64u16+`"}`, b64u16)
	return cs
}

// auditExpectedMisses are the attack cases that are NOT secrets by this package's predicate, and
// are asserted to stay uncaught so a change here is deliberate: a password HASH is not a password.
var auditExpectedMisses = map[string]bool{"json password_hash (ok to skip)": true}

// TestAuditAttackSetIsCaught: at 2ba3e5c the auditor measured 39 of 75 cases caught (url-query
// 0/4, jwk 0/2, key-name 1/10, symbol-pw 1/9, source 2/9, conn 1/5, sig-smuggle 1/5, …); every case
// but the declared non-secret is caught now, at two seeds.
func TestAuditAttackSetIsCaught(t *testing.T) {
	for _, seed := range []uint64{1, 20000101} {
		r2seed(seed)
		auditAttackOnce(t, seed)
	}
}

func auditAttackOnce(t *testing.T, seed uint64) {
	r, err := New(bytes.Repeat([]byte{7}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	type tally struct{ caught, total int }
	byClass := map[string]*tally{}
	for _, c := range r2cases() {
		var out string
		switch c.mode {
		case "s":
			o, _ := r.String(c.in)
			out = o
		case "r":
			o, _ := r.Record([]byte(c.in))
			out = string(o)
		case "b":
			o, _ := r.Blob("out.txt", []byte(c.in))
			out = string(o)
		}
		var ok bool
		if c.secret == "" {
			ok = out != c.in
		} else if c.class == "encoding" && strings.HasPrefix(c.name, "UTF-16") {
			ok = !strings.Contains(out, c.in[24:40]) // utf16 bytes of part of secret
		} else {
			ok = !strings.Contains(out, c.secret)
		}
		if byClass[c.class] == nil {
			byClass[c.class] = &tally{}
		}
		byClass[c.class].total++
		mark := "MISSED"
		if ok {
			byClass[c.class].caught++
			mark = "caught"
		}
		if ok == auditExpectedMisses[c.name] {
			t.Errorf("seed %d: %s / %s: %s (expected the opposite)", seed, c.class, c.name, mark)
		}
	}
	var ks []string
	for k := range byClass {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		t.Logf("seed %d: %-12s caught=%d/%d", seed, k, byClass[k].caught, byClass[k].total)
	}
}
