package redact

import (
	"encoding/base64"
	"strconv"
	"strings"
)

// reviewRound3Plants are the shapes review round 2 found missing or regressed: a private key read
// through a copy prefix, credentials in URL queries and `;`-separated connection strings, JWK
// private members, YAML block scalars, `.pgpass`/`.netrc`, source literals, `docker login -p`,
// value-first k8s env entries, glued and suffixed key names, symbol-bearing and dotted passwords,
// text that starts with an ASCII file magic, and BOM-less UTF-16. Every value is drawn at run time.
func (g *gen) reviewRound3Plants(session string) {
	// 50, 51 — a private key read through Read's numbered copy and through `grep -n`.
	for i, prefix := range []func(n int) string{
		func(n int) string { return "     " + strconv.Itoa(n) + "\t" },
		func(n int) string { return "id_ed25519:" + strconv.Itoa(n) + ":" },
	} {
		body1, body2 := g.pick(alnum, 64), g.pick(alnum, 64)
		label := []string{"pem-read-numbered", "pem-grep-numbered"}[i]
		g.plant(label, "pem-private-key", body1)
		g.c.Plants[len(g.c.Plants)-1].Forms = append(g.c.Plants[len(g.c.Plants)-1].Forms, body2)
		text := prefix(1) + "-----BEGIN OPENSSH " + "PRIVATE KEY-----\n" + prefix(2) + body1 + "\n" + prefix(3) + body2 + "\n" +
			prefix(4) + "-----END OPENSSH " + "PRIVATE KEY-----\n"
		g.record(g.toolResult(session, text, nil))
	}

	// 52–56 — URL queries and connection strings.
	g.record(g.toolUse(session, "Bash", m{"command": "curl 'https://api.example.invalid/v1/items?page=2&token=" +
		g.plant("url-query-token", "key-context", g.pick(alnum, 30)+"4") + "'"}))
	g.record(g.toolResult(session, "https://bucket.s3.example.invalid/k?X-Amz-Credential=x&X-Amz-Signature="+
		g.plant("url-query-signature", "query-param", strings.ToLower(g.pick("0123456789abcdef", 64)))+"\n", nil))
	g.record(g.toolResult(session, "DefaultEndpointsProtocol=https;AccountName=alpha;AccountKey="+
		g.plant("azure-account-key", "key-context", base64.StdEncoding.EncodeToString([]byte(g.pick(alnum, 48))))+
		";EndpointSuffix=core.example.invalid\n", nil))
	g.record(g.toolResult(session, "Server=tcp:db.example,1433;Database=alpha;User ID=app;Password="+
		g.plant("adonet-password", "key-context", g.pick(alnum, 22)+"3")+";Encrypt=True;\n", nil))
	g.record(g.toolResult(session, "jdbc:postgresql://db.example:5432/alpha?user=app&password="+
		g.plant("jdbc-password", "key-context", g.pick(alnum, 22)+"8")+"\n", nil))

	// 57, 58 — JWK private members: EC `d` and a symmetric key's `k`.
	g.record(m{"type": "user", "sessionId": session, "toolUseResult": m{"stdout": "", "jwk": m{"kty": "EC", "crv": "P-256",
		"x": "abc", "y": "def", "d": g.plant("jwk-ec-d", "jwk-private", base64.RawURLEncoding.EncodeToString(g.bytes(32)))}}})
	g.record(m{"type": "user", "sessionId": session, "toolUseResult": m{"keys": []any{m{"kty": "oct", "alg": "HS256",
		"k": g.plant("jwk-oct-k", "jwk-private", base64.RawURLEncoding.EncodeToString(g.bytes(32)))}}}})

	// 59 — a YAML block scalar under a secret key (not a k8s Secret).
	g.record(g.toolResult(session, "database:\n  host: db.example\n  password: |\n    "+
		g.plant("yaml-block-scalar", "yaml-block-secret", g.pick(alnum, 26)+"5")+"\n  pool: 5\n", nil))

	// 60, 61 — `.pgpass` and `.netrc`.
	g.blob("toolu_"+g.pick(alnum, 24)+".txt", "db.example:5432:alpha:app:"+g.plant("pgpass-line", "pgpass", g.pick(alnum, 20)+"6")+"\n")
	g.blob("toolu_"+g.pick(alnum, 24)+".txt", "machine api.example.invalid login bot password "+
		g.plant("netrc-password", "netrc-password", g.pick(alnum, 20)+"2")+"\n")

	// 62, 63 — source literals: a JS const and a Python dict entry.
	g.record(g.toolResult(session, "const apiSecret = '"+g.plant("source-js-const", "key-context", g.pick(alnum, 30)+"1")+"';\n", nil))
	g.record(g.toolResult(session, "CONFIG = {'secret_key': '"+g.plant("source-py-dict", "key-context", g.pick(alnum, 30)+"9")+"'}\n", nil))

	// 64 — `docker login -p`; 65 — a value-FIRST k8s env entry.
	g.record(g.toolUse(session, "Bash", m{"command": "docker login -u bot -p " +
		g.plant("docker-login", "cli-flag", g.pick(alnum, 24)+"7") + " registry.example.invalid"}))
	g.record(g.toolResult(session, "env:\n  - value: "+g.plant("k8s-env-value-first", "k8s-env", g.pick(alnum, 22)+"4")+
		"\n    name: SMTP_PASSWORD\n", nil))

	// 66–68 — key names the first END rule lost: glued, suffixed, `Value`-suffixed JSON.
	g.record(g.toolResult(session, "export PGPASSWORD="+g.plant("glued-pgpassword", "key-context", g.pick(alnum, 24)+"3")+"\n", nil))
	g.record(g.toolResult(session, "SECRET_KEY_BASE="+g.plant("suffixed-secret-key-base", "key-context", strings.ToLower(g.pick("0123456789abcdef", 64)))+"\n", nil))
	g.record(m{"type": "user", "sessionId": session, "toolUseResult": m{"apiKeyValue": g.plant("json-api-key-value", "secret-field", g.pick(alnum, 32))}})

	// 69, 70 — a symbol-bearing password and a dotted passphrase (the shapes `notCode` refused).
	g.record(g.toolResult(session, "DB_PASSWORD="+g.plant("symbol-password", "key-context", "Zq9"+g.pick(alnum+"!@%^&*-_=+.?~;$(", 14)+"7")+"\n", nil))
	g.record(g.toolResult(session, "DB_PASSWORD="+g.plant("dotted-passphrase", "key-context",
		g.pick("abcdefgh", 5)+"."+g.pick("abcdefgh", 6)+"."+g.pick("abcdefgh", 5))+"\n", nil))

	// 71 — text that STARTS with an ASCII file magic; 72 — UTF-16LE with no BOM.
	g.blob("toolu_"+g.pick(alnum, 24)+".txt", "%PDF-1.7 text extracted from a page:\nDB_PASSWORD="+
		g.plant("ascii-magic-text", "key-context", g.pick(alnum, 24)+"5")+"\n")
	g.c.Items = append(g.c.Items, Item{Blob: true, Name: "toolu_" + g.pick(alnum, 24) + ".txt",
		Data: utf16LE("LOG=1\nAPI_TOKEN=" + g.plant("utf16-no-bom", "key-context", g.pick(alnum, 24)+"6") + "\n")})
}
