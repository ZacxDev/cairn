package redact

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"strings"
)

// DeclaredPlants is how many secrets [NewCorpus] plants. The self-test REFUSES (exit 2) when the
// generator plants a different number: P is asserted against this declaration, never read off the
// run, so a generator that silently lost a position cannot report a smaller perfect score.
const DeclaredPlants = 79

// Plant is one planted secret: where it sits, the rule expected to catch it, and every FORM whose
// presence in the redacted output means it leaked (the plaintext, and its encoding when it was
// planted encoded). It never prints a form.
type Plant struct {
	Label string
	Rule  string
	Forms []string
}

// Clean is one value that must survive redaction untouched — a UUID, a SHA, a digest, an image
// payload, a thinking signature. A redactor that eats every hash makes transcripts unreadable,
// which is a failure too.
type Clean struct {
	Label string
	Value string
}

// Item is one unit the capture agent redacts: a JSON record (a JSONL line, an opencode part) or a
// persisted tool-result blob.
type Item struct {
	Blob bool
	Name string
	Data []byte
}

// Corpus is a realistic synthetic transcript world in BOTH runtimes' shapes, with secrets planted
// in the positions decision 6 lists.
//
// 🔴 EVERY SECRET IS GENERATED AT RUN TIME FROM A SEEDED RNG, AND NONE IS A TEXTBOOK EXAMPLE. A
// committed credential-shaped value is itself a `leakscan` finding (and should be), and a scanner
// that only recognises its own canonical examples passes a real leak — so the values are random in
// the real formats' alphabets and lengths, in realistic positions, and change with the seed.
type Corpus struct {
	Items  []Item
	Plants []Plant
	Clean  []Clean
}

// escapedHyphen is the six characters a JSON encoder may write for "-": backslash, `u`, `002d`.
// It is BUILT from bytes rather than spelled, because a spelled escape is exactly the thing an
// editor, a tool or a string literal decodes on the way in — the first draft of this corpus
// spelled it, the escape was silently decoded to a plain hyphen, and the "escaped" plant was an
// ordinary one that every raw-byte scan caught. The control in `TestAJSONEscapedSecretIs…` is what
// went red on that.
var escapedHyphen = string([]byte{0x5c, 'u', '0', '0', '2', 'd'})

type gen struct {
	rng *rand.Rand
	c   *Corpus
}

const (
	alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	digit = "0123456789"
	b64u  = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
)

func (g *gen) pick(alphabet string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[g.rng.IntN(len(alphabet))]
	}
	return string(b)
}

func (g *gen) bytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(g.rng.IntN(256))
	}
	return b
}

func (g *gen) uuid() string {
	h := hex.EncodeToString(g.bytes(16))
	return h[:8] + "-" + h[8:12] + "-4" + h[13:16] + "-a" + h[17:20] + "-" + h[20:]
}

func (g *gen) words(n int) string {
	vocab := strings.Fields("lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor")
	out := make([]string, n)
	for i := range out {
		out[i] = vocab[g.rng.IntN(len(vocab))]
	}
	return strings.Join(out, " ")
}

func (g *gen) png() string {
	img := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	for i := range img.Pix {
		img.Pix[i] = byte(g.rng.IntN(256))
	}
	img.Set(0, 0, color.NRGBA{A: 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func (g *gen) jwt() string {
	enc := base64.RawURLEncoding
	head := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body := enc.EncodeToString([]byte(`{"sub":"` + g.pick(alnum, 12) + `","iat":946684800}`))
	return head + "." + body + "." + enc.EncodeToString(g.bytes(32))
}

func (g *gen) plant(label, rule, secret string, extraForms ...string) string {
	g.c.Plants = append(g.c.Plants, Plant{Label: label, Rule: rule, Forms: append([]string{secret}, extraForms...)})
	return secret
}

func (g *gen) clean(label, v string) string {
	g.c.Clean = append(g.c.Clean, Clean{Label: label, Value: v})
	return v
}

func (g *gen) record(v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	g.c.Items = append(g.c.Items, Item{Data: raw})
}

func (g *gen) blob(name, text string) {
	g.c.Items = append(g.c.Items, Item{Blob: true, Name: name, Data: []byte(text)})
}

type m = map[string]any

func (g *gen) envelope(kind, session string) m {
	return m{"type": kind, "uuid": g.clean("record-uuid", g.uuid()), "sessionId": session,
		"parentUuid": nil, "isSidechain": false, "cwd": "/work/alpha", "timestamp": "2000-01-01T00:00:00.000Z",
		"version": "2.1.289", "gitBranch": "feature/clk0000a1-sample", "entrypoint": "cli", "userType": "external"}
}

func (g *gen) toolResult(session, content string, dup m) m {
	r := g.envelope("user", session)
	r["message"] = m{"role": "user", "content": []any{m{"type": "tool_result", "tool_use_id": "toolu_" + g.pick(alnum, 24),
		"content": content}}}
	if dup != nil {
		r["toolUseResult"] = dup
	}
	return r
}

func (g *gen) toolUse(session, name string, input m) m {
	r := g.envelope("assistant", session)
	r["message"] = m{"role": "assistant", "model": "claude-synthetic-1", "content": []any{
		m{"type": "thinking", "thinking": g.words(8), "signature": g.clean("thinking-signature",
			base64.StdEncoding.EncodeToString(g.bytes(240)))},
		m{"type": "tool_use", "id": "toolu_" + g.pick(alnum, 24), "name": name, "input": input}}}
	return r
}

// NewCorpus builds the corpus for one seed.
func NewCorpus(seed uint64) Corpus {
	c := Corpus{}
	g := &gen{rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), c: &c}
	session := g.clean("session-uuid", g.uuid())
	sha := func() string { return g.clean("commit-sha", hex.EncodeToString(g.bytes(20))) }
	digest := func() string { return g.clean("sha256", hex.EncodeToString(g.bytes(32))) }

	// 1, 2 — a shell tool's stdout printing a dotenv file.
	dotenv := fmt.Sprintf("# generated %s\nDB_HOST=db.example\nDB_PASSWORD=%s\nGITHUB_TOKEN=%s\nLOG_LEVEL=debug\nmax_tokens = 4096\n",
		sha(), g.plant("shell-stdout-dotenv", "key-context", g.pick(alnum, 24)),
		g.plant("shell-stdout-github", "github-token", "ghp_"+g.pick(alnum, 36)))
	g.record(g.toolUse(session, "Bash", m{"command": "cat .env", "description": g.words(3)}))
	g.record(g.toolResult(session, dotenv, m{"stdout": dotenv, "stderr": "", "interrupted": false}))

	// 3 — `toolUseResult.originalFile`, and 4 — an edit's `oldString`.
	anth := g.plant("original-file-anthropic", "anthropic-key", "sk-ant-api03-"+g.pick(b64u, 80))
	aws := g.plant("edit-oldstring-aws", "aws-access-key-id", "AKIA"+g.pick(upper, 16))
	g.record(g.toolResult(session, "The file has been updated.", m{
		"filePath":  "/work/alpha/settings.py",
		"oldString": "AWS_KEY_ID = \"" + aws + "\"", "newString": "AWS_KEY_ID = os.environ[\"AWS_KEY_ID\"]",
		"originalFile": "import os\n\nCLIENT = make_client(api_key=\"" + anth + "\")\nREVISION = \"" + sha() + "\"\n",
	}))

	// 5 — a JSON-ESCAPED value: the hyphens of the token are written as - in the RAW line,
	// so a raw-byte regex sees different bytes from the decoded string.
	slack := g.plant("json-escaped-slack", "slack-token", "xoxb-"+g.pick(digit, 12)+"-"+g.pick(digit, 13)+"-"+g.pick(alnum, 24))
	escaped := strings.ReplaceAll(slack, "-", escapedHyphen)
	g.c.Plants[len(g.c.Plants)-1].Forms = append(g.c.Plants[len(g.c.Plants)-1].Forms, escaped)
	rec := g.toolResult(session, "SLACK_BOT="+slack+"\n", nil)
	raw, _ := json.Marshal(rec)
	raw = bytes.Replace(raw, []byte(slack), []byte(escaped), 1)
	if !bytes.Contains(raw, []byte(escaped)) {
		panic("corpus: the escaped plant did not land")
	}
	g.c.Items = append(g.c.Items, Item{Data: raw})

	// 6, 7, 8 — a Kubernetes Secret manifest (YAML) in a tool output; metadata.name must survive.
	d1 := g.plant("k8s-yaml-data-1", "k8s-secret", base64.StdEncoding.EncodeToString([]byte(g.pick(alnum, 16))))
	d2 := g.plant("k8s-yaml-data-2", "k8s-secret", base64.StdEncoding.EncodeToString([]byte(g.pick(alnum, 20))))
	sd := g.plant("k8s-yaml-stringdata", "k8s-secret", g.pick(alnum, 18))
	name := g.clean("k8s-metadata-name", "alpha-store-"+g.pick("abcdefghijklmnopqrstuvwxyz", 6))
	manifest := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: " + name + "\n  namespace: alpha\ntype: Opaque\ndata:\n  username: " +
		d1 + "\n  replica-key: " + d2 + "\nstringData:\n  config.ini: " + sd + "\n"
	g.record(g.toolResult(session, manifest, m{"stdout": manifest, "stderr": ""}))

	// 10 — a DSN with a password in its userinfo, in a COMMAND LINE.
	pw := g.plant("command-line-dsn", "url-userinfo-password", g.pick(alnum, 20))
	g.record(g.toolUse(session, "Bash", m{"command": "psql 'postgres://app:" + pw + "@db.example:5432/alpha' -c 'select 1'",
		"description": g.words(2)}))

	// 11 — a PROMPT string carrying a bearer header.
	bearer := g.plant("prompt-bearer", "authorization", g.pick(alnum, 39)+"7")
	prompt := g.envelope("user", session)
	prompt["message"] = m{"role": "user", "content": "the call fails with this header: Authorization: Bearer " + bearer + " — why?"}
	g.record(prompt)

	// 12 — a BOOKKEEPING record: `queue-operation` content.
	g.record(m{"type": "queue-operation", "operation": "enqueue", "timestamp": "2000-01-01T00:00:01.000Z",
		"sessionId": session, "content": "retry with token " + g.plant("queue-operation-jwt", "jwt", g.jwt())})

	// 13 — a SUBAGENT's tool output.
	sub := g.toolResult(session, "maps:\n  key: "+g.plant("subagent-output-google", "google-api-key", "AIza"+g.pick(b64u, 35))+"\n", nil)
	sub["isSidechain"] = true
	sub["agentId"] = g.pick("0123456789abcdef", 16)
	g.record(sub)

	// 14, 15, 16 — a persisted TEXT blob: a dotenv dump.
	g.blob("toolu_"+g.pick(alnum, 24)+".txt", fmt.Sprintf("STRIPE_SECRET_KEY=%s\nCLICKUP_TOKEN=%s\nSESSION_SECRET=%s\nBUILD=%s\nTOKEN_COUNT=12\n",
		g.plant("blob-dotenv-stripe", "stripe-key", "sk_live_"+g.pick(alnum, 24)),
		g.plant("blob-dotenv-clickup", "clickup-token", "pk_"+g.pick(digit, 8)+"_"+g.pick(upper, 32)),
		g.plant("blob-dotenv-plain", "key-context", g.pick(alnum, 28)), sha()))

	// 17, 18 — a persisted TEXT blob: a Secret manifest, two documents, the second a ConfigMap
	// whose `data` must survive.
	b1 := g.plant("blob-k8s-data-1", "k8s-secret", base64.StdEncoding.EncodeToString([]byte(g.pick(alnum, 24))))
	// `token:` names a secret, so the key-context rule names this value before the Secret rule does.
	b2 := g.plant("blob-k8s-data-2", "key-context", base64.StdEncoding.EncodeToString([]byte(g.pick(alnum, 24))))
	cm := g.clean("configmap-value", "replicas-"+g.pick(digit, 3))
	g.blob("toolu_"+g.pick(alnum, 24)+".txt", "apiVersion: v1\nkind: Secret\nmetadata:\n  name: alpha-db\ndata:\n  password: |\n    "+
		b1+"\n  token: "+b2+"\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: alpha-config\ndata:\n  mode: "+cm+"\n")

	// 19 — a WHOLE-string base64 value whose payload is a 40-character token: 56 characters, which
	// the revision-4 floor of 64 would have let through.
	tok := g.plant("base64-whole-40char", "base64/github-token", "ghp_"+g.pick(alnum, 36))
	encTok := base64.StdEncoding.EncodeToString([]byte(tok))
	g.c.Plants[len(g.c.Plants)-1].Forms = append(g.c.Plants[len(g.c.Plants)-1].Forms, encTok)
	g.record(g.toolResult(session, encTok, nil))

	// 20 — a JSON blob whose secret is unicode-escaped.
	gl := g.plant("json-blob-escaped-gitlab", "gitlab-token", "glpat-"+g.pick(alnum, 20))
	glEsc := strings.ReplaceAll(gl, "-", escapedHyphen)
	g.c.Plants[len(g.c.Plants)-1].Forms = append(g.c.Plants[len(g.c.Plants)-1].Forms, glEsc)
	g.blob("toolu_"+g.pick(alnum, 24)+".txt", "{\n  \"id\": 12345678901234567890,\n  \"ci\": {\"note\": \""+g.words(3)+
		"\", \"value\": \""+glEsc+"\"},\n  \"rev\": \""+sha()+"\"\n}\n")

	// 21 — a PEM private key in a tool output; 22 — an age identity.
	var pem strings.Builder
	// The header is SPLICED, as is the age prefix below: spelled whole, each is (rightly) a
	// leakscan `credential` finding on this source line, although every byte after it is drawn
	// at run time. The generated value has the realistic shape; the source never holds one.
	pem.WriteString("-----BEGIN RSA " + "PRIVATE KEY-----\n")
	first := g.pick(alnum, 64)
	pem.WriteString(first + "\n")
	for i := 0; i < 4; i++ {
		pem.WriteString(g.pick(alnum, 64) + "\n")
	}
	pem.WriteString("-----END RSA PRIVATE KEY-----\n")
	g.plant("pem-in-output", "pem-private-key", first)
	age := g.plant("age-key-in-output", "age-secret-key", "AGE-SECRET-"+"KEY-1"+g.pick(upper, 58))
	g.record(g.toolResult(session, pem.String()+"# public key: age1"+g.pick(digit, 10)+"\n"+age+"\n", nil))

	// 23 — a text secret BESIDE an image payload in one record: the image must ship byte-identical
	// (O12) while the text secret is caught.
	img := g.clean("image-payload", g.png())
	beside := g.envelope("user", session)
	beside["message"] = m{"role": "user", "content": []any{m{"type": "tool_result", "tool_use_id": "toolu_" + g.pick(alnum, 24),
		"content": []any{
			m{"type": "image", "source": m{"type": "base64", "media_type": "image/png", "data": img}},
			m{"type": "text", "text": "NPM_TOKEN=" + g.plant("beside-image-npm", "npm-token", "npm_"+g.pick(alnum, 36))},
		}}}}
	beside["toolUseResult"] = m{"type": "image", "file": m{"base64": img, "type": "image/png", "originalSize": 77}}
	g.record(beside)

	// 24 — an opencode attachment `data:text/plain;base64,…` whose payload is TEXT carrying a
	// secret; 25 — an opencode tool INPUT field named `password`; plus a binary image attachment.
	hf := g.plant("opencode-attachment-text-hf", "base64/huggingface-token", "hf_"+g.pick(alnum, 34))
	payload := base64.StdEncoding.EncodeToString([]byte("HF_TOKEN=" + hf + "\n"))
	g.c.Plants[len(g.c.Plants)-1].Forms = append(g.c.Plants[len(g.c.Plants)-1].Forms, payload)
	ocImg := g.clean("opencode-image", "data:image/png;base64,"+g.png())
	ses := g.clean("opencode-session", "ses_"+hex.EncodeToString(g.bytes(6))+g.pick(alnum, 14))
	g.record(m{"id": g.clean("opencode-part", "prt_"+hex.EncodeToString(g.bytes(6))+g.pick(alnum, 14)), "sessionID": ses,
		"messageID": "msg_" + g.pick(alnum, 26), "type": "tool", "callID": "call_" + g.pick(alnum, 24), "tool": "read",
		"state": m{"status": "completed", "input": m{"filePath": "/work/alpha/notes.txt"}, "output": "read 1 file",
			"attachments": []any{
				m{"type": "file", "mime": "text/plain", "url": "data:text/plain;base64," + payload},
				m{"type": "file", "mime": "image/png", "url": ocImg},
			}}})
	// 9 — an opencode tool `state.output`, and 25 the field rule.
	g.record(m{"id": "prt_" + g.pick(alnum, 26), "sessionID": ses, "messageID": "msg_" + g.pick(alnum, 26), "type": "tool",
		"callID": "call_" + g.pick(alnum, 24), "tool": "bash",
		"state": m{"status": "completed",
			"input":    m{"command": "./login.sh", "password": g.plant("opencode-input-secret-field", "secret-field", g.pick(alnum, 16))},
			"output":   "exporting OPENAI_KEY " + g.plant("opencode-output-openai", "sk-key", "sk-proj-"+g.pick(alnum, 48)) + " for " + digest() + "\n",
			"metadata": m{"exit": 0, "truncated": false}}})

	// 26 — a Secret manifest printed as JSON inside a tool output (a JSON document in a string).
	jv := g.plant("assistant-text-k8s-json", "k8s-secret", base64.StdEncoding.EncodeToString([]byte(g.pick(alnum, 22))))
	g.record(g.toolResult(session, `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"alpha-api"},"data":{"key":"`+jv+`"}}`, nil))

	g.reviewRoundPlants(session, ses)
	g.reviewRound3Plants(session)
	g.roundFourPlants(session)
	g.codeShapedClean(session)

	// Clean filler that must survive — hashes, digests, ids, URLs — around ONE plant: a random base64
	// value in prose, which was clean filler until O15 made a long random-looking token a secret.
	g.record(g.toolResult(session, "commit "+sha()+"\nAuthor: someone\n\n    "+g.words(6)+"\n\ndigest sha256:"+digest()+
		"\nsee https://github.com/example-org/alpha-notes/pull/7 and git@github.com:example-org/alpha-notes.git\n"+
		"the bearer of this note keeps the token count low\nrandom "+
		g.plant("random-base64-in-output", "entropy", base64.StdEncoding.EncodeToString(g.bytes(48)))+"\n", nil))
	g.record(g.toolResult(session, g.clean("build-uuid", g.uuid())+" "+digest()+" "+sha(), nil))
	g.blob("toolu_"+g.pick(alnum, 24)+".txt", "no secrets here: "+g.words(12)+"\n"+digest()+"\n")
	return c
}
