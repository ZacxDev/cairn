package redact

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// reviewRoundPlants are the positions review rounds 0 and 1 found the first corpus did not
// carry: the COPIES a transcript holds of a file (Read's numbered copy, `grep -n`, a diff), shapes
// that are not `KEY=value` at a line start, and the encodings revision 17 shipped unscanned.
// Every value is drawn at run time, like every other plant.
func (g *gen) reviewRoundPlants(session, ses string) {
	// 27 — Read's model-visible NUMBERED copy of a dotenv file.
	v := g.plant("read-numbered-dotenv", "dotenv", g.pick(alnum, 22)+"9")
	numbered := "     1\tLOG_LEVEL=debug\n     2\tDB_PASSWORD=" + v + "\n     3\tREGION=alpha\n"
	g.record(g.toolResult(session, numbered, m{"type": "text", "file": m{"filePath": "/work/alpha/.env",
		"content": "LOG_LEVEL=debug\nDB_PASSWORD=" + v + "\nREGION=alpha\n", "numLines": 3, "startLine": 1, "totalLines": 3}}))

	// 28 — `grep -n` output; 29 — a diff.
	g.record(g.toolResult(session, ".env:2:SERVICE_TOKEN="+g.plant("grep-numbered-dotenv", "dotenv", g.pick(alnum, 20)+"3")+"\n", nil))
	g.record(g.toolResult(session, "@@ -1 +1 @@\n-API_SECRET=x\n+API_SECRET="+g.plant("diff-dotenv", "dotenv", g.pick(alnum, 24)+"1")+"\n", nil))

	// 30 — Read's numbered copy of a Secret manifest.
	sv := g.plant("read-numbered-k8s", "k8s-secret", base64.StdEncoding.EncodeToString([]byte(g.pick(alnum, 18))))
	g.record(g.toolResult(session, "     1\tapiVersion: v1\n     2\tkind: Secret\n     3\tmetadata:\n     4\t  name: alpha-db\n"+
		"     5\tdata:\n     6\t  replica: "+sv+"\n", nil))

	// 31 — `docker run -e`; 32 — `curl -u`; 46 — a libpq keyword/value string; 47 — X-Auth-Token.
	g.record(g.toolUse(session, "Bash", m{"command": "docker run -e DB_PASSWORD=" +
		g.plant("docker-env", "docker-env", g.pick(alnum, 18)+"5") + " -e LOG=1 alpine true"}))
	g.record(g.toolUse(session, "Bash", m{"command": "curl -u admin:" +
		g.plant("curl-user", "curl-user", g.pick(alnum, 16)+"2") + " https://api.example.invalid/v1/items"}))
	g.record(g.toolUse(session, "Bash", m{"command": "psql 'host=db.example port=5432 password=" +
		g.plant("libpq-password", "libpq-password", g.pick(alnum, 18)+"6") + " dbname=alpha'"}))
	g.record(g.toolUse(session, "Bash", m{"command": "curl -H 'X-Auth-Token: " +
		g.plant("x-auth-token", "authorization", g.pick(alnum, 30)+"8") + "' https://api.example.invalid/v1"}))

	// 33 — an `.npmrc` registry token.
	g.blob("toolu_"+g.pick(alnum, 24)+".txt", "//registry.npmjs.org/:_authToken="+
		g.plant("npmrc-auth", "npmrc-auth", g.pick(alnum, 30)+"4")+"\nalways-auth=true\n")

	// 34 — a k8s container env pair.
	g.record(g.toolResult(session, "spec:\n  containers:\n    - name: app\n      env:\n        - name: DB_PASSWORD\n          value: "+
		g.plant("k8s-env-pair", "k8s-env", g.pick(alnum, 20)+"7")+"\n        - name: LOG_LEVEL\n          value: debug\n", nil))

	// 35 — a camelCase secret field inside a JSON document printed by a tool.
	cfg, _ := json.Marshal(m{"credentials": m{"secretAccessKey": g.plant("camelcase-field", "secret-field", g.pick(alnum, 40)),
		"region": "alpha"}})
	g.record(g.toolResult(session, string(cfg), nil))

	// 36 — a DUPLICATE key: a decoder that keeps only the last member never scans the first.
	dup := g.plant("duplicate-key", "github-token", "ghp_"+g.pick(alnum, 36))
	g.c.Items = append(g.c.Items, Item{Data: []byte(`{"type":"user","sessionId":"` + session +
		`","message":{"role":"user","content":"ok"},"note":"` + dup + `","note":"ok"}`)})

	// 37 — a secret as an object KEY.
	g.c.Items = append(g.c.Items, Item{Data: []byte(`{"type":"attachment","sessionId":"` + session +
		`","attachment":{"type":"hook_success","map":{"` + g.plant("secret-as-key", "github-token", "ghp_"+g.pick(alnum, 36)) + `":1}}}`)})

	// 38 — a NUL-separated environ dump; 39 — text with stray invalid bytes; 40 — UTF-16 with a BOM.
	g.blob("toolu_"+g.pick(alnum, 24)+".txt", "PATH=/usr/bin\x00HOME=/home/dev\x00AWS_SECRET_ACCESS_KEY="+
		g.plant("environ-nul", "dotenv", g.pick(alnum, 40))+"\x00TERM=xterm\x00")
	g.blob("toolu_"+g.pick(alnum, 24)+".txt", "build \xff\xfe log\nX_API_KEY="+
		g.plant("invalid-utf8", "dotenv", g.pick(alnum, 26)+"0")+"\n\x80 end\n")
	g.c.Items = append(g.c.Items, Item{Blob: true, Name: "toolu_" + g.pick(alnum, 24) + ".txt",
		Data: utf16LE("\ufeffLOG=1\nSESSION_SECRET=" + g.plant("utf16-bom", "dotenv", g.pick(alnum, 24)+"2") + "\n")})

	// 41 — a PGP private key block (armor: a blank line, the body, a checksum line).
	var pgp strings.Builder
	pgp.WriteString("-----BEGIN PGP " + "PRIVATE KEY BLOCK-----\n\n")
	first := g.pick(alnum, 64)
	g.plant("pgp-private-key", "pem-private-key", first)
	pgp.WriteString(first + "\n" + g.pick(alnum, 64) + "\n=" + g.pick(alnum, 4) + "\n-----END PGP " + "PRIVATE KEY BLOCK-----\n")
	g.record(g.toolResult(session, pgp.String()+"gpg: done\n", nil))

	// 42 — `redis://:pw@` (an EMPTY user); 43 — Slack app token; 44 — a Slack webhook; 45 — whsec.
	g.record(g.toolResult(session, "REDIS_URL=redis://:"+g.plant("redis-empty-user", "url-userinfo-password", g.pick(alnum, 24))+
		"@cache.example:6379/0\n", nil))
	g.record(g.toolResult(session, "app token xapp-1-"+g.plant("slack-app-token", "slack-token", "A"+g.pick(upper, 10)+"-"+g.pick(digit, 13)+"-"+g.pick(alnum, 40))+"\n", nil))
	// The xapp prefix is planted as part of the form: the slack rule's match includes it.
	g.c.Plants[len(g.c.Plants)-1].Forms[0] = "xapp-1-" + g.c.Plants[len(g.c.Plants)-1].Forms[0]
	g.record(g.toolResult(session, "notify via "+g.plant("slack-webhook", "slack-webhook", "https://hooks.slack.com/services/T"+
		g.pick(upper, 8)+"/B"+g.pick(upper, 8)+"/"+g.pick(alnum, 24))+"\n", nil))
	g.record(g.toolResult(session, "signing secret "+g.plant("stripe-whsec", "stripe-webhook-secret", "whsec_"+g.pick(alnum, 32))+"\n", nil))

	// 48 — `Authorization: Basic …` as a JSON headers map in a tool INPUT.
	basic := g.plant("json-headers-basic", "secret-field", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:"+g.pick(alnum, 14))))
	g.record(m{"id": "prt_" + g.pick(alnum, 26), "sessionID": ses, "messageID": "msg_" + g.pick(alnum, 26), "type": "tool",
		"callID": "call_" + g.pick(alnum, 24), "tool": "webfetch",
		"state": m{"status": "completed", "input": m{"url": "https://api.example.invalid/v1", "headers": m{"Authorization": basic}},
			"output": "200 OK"}})

	// 49 — a SHORT bearer token (≥ 8).
	p := g.envelope("user", session)
	p["message"] = m{"role": "user", "content": "the old one was bearer " + g.plant("short-bearer", "bearer", g.pick(alnum, 9)+"4") + " — rotate it"}
	g.record(p)
}

// codeShapedClean is SOURCE CODE a session reads — Go, Python, Markdown, YAML — written to look
// like a repository's, with no real name in it. Every snippet must survive byte-identical: a
// line-oriented rule that eats `password: cfg.Password` makes code unreadable and protects
// nothing (review round 1 measured 216 dotenv hits over 311 files of this repository).
func (g *gen) codeShapedClean(session string) {
	goSrc := g.clean("code-go", "package alpha\n\nfunc login(ctx context.Context, cfg Config) error {\n"+
		"\tpassword := os.Getenv(\"DB_PASSWORD\")\n\tclient := NewClient(Options{\n\t\tToken:    cfg.Token,\n"+
		"\t\tPassword: password,\n\t\tAPIKey:   settings.APIKey,\n\t})\n\tif token == \"\" {\n\t\treturn errNoToken\n\t}\n"+
		"\treturn client.Auth(ctx, secret)\n}\n")
	pySrc := g.clean("code-python", "import os\n\nAPI_KEY = os.environ[\"API_KEY\"]\npassword = getpass.getpass()\n"+
		"TOKEN_URL = \"https://example.invalid/oauth/token\"\nsecret_name: str = \"alpha-db\"\n\n"+
		"def connect(token: str, password: str) -> None:\n    session.headers[\"Authorization\"] = f\"Bearer {token}\"\n")
	md := g.clean("code-markdown", "## Configuration\n\nSet `DB_PASSWORD` in the env file; never commit it.\n\n"+
		"| key | meaning |\n|---|---|\n| token | the bearer token the client sends |\n| password | the database password |\n\n"+
		"    export API_TOKEN=${API_TOKEN}\n")
	yml := g.clean("code-yaml", "database:\n  host: db.example\n  password: ${DB_PASSWORD}\n  token_ttl: 3600\nauth:\n  enabled: true\n")
	numberedGo := g.clean("code-go-read-copy", "     1\tfunc load() {\n     2\t\tsecret := vault.Read(path)\n"+
		"     3\t\tcfg.Password = secret\n     4\t}\n")
	for _, src := range []string{goSrc, pySrc, md, yml, numberedGo} {
		g.record(g.toolResult(session, src, m{"stdout": src, "stderr": ""}))
	}
	g.blob("toolu_"+g.pick(alnum, 24)+".txt", goSrc+pySrc)
}

func utf16LE(s string) []byte {
	var b []byte
	for _, r := range s {
		b = append(b, byte(r), byte(r>>8))
	}
	return b
}
