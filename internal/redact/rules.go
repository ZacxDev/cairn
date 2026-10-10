// Package redact is the host-side redactor for session transcripts: ONE rule table, applied to
// DECODED strings, with a keyed tag in place of every match.
//
// 🔴 IT RUNS ON DECODED STRING VALUES, NEVER ON RAW BYTES (`claudedocs/plan-cairn-plugins.md`,
// decision 6). A secret inside a JSON string can be written with escapes — a hyphen as the six
// characters backslash, `u`, `002d` — and a regex over the raw line then sees different bytes and
// misses it. So a record is decoded, every string value AND every object key is scanned, and the
// record is re-encoded only when something matched; an untouched record keeps its ORIGINAL bytes.
//
// 🔴 A MATCH BECOMES `[redacted:<rule>:<tag>]`, AND THE TAG IS KEYED. `<tag>` is the first 8 hex of
// HMAC-SHA256 under a per-host secret: two occurrences of one secret on one host stay recognisably
// the same, and a reader without the key cannot confirm a guessed low-entropy secret by hashing it
// (T15). An unkeyed digest prefix would allow exactly that confirmation.
//
// 🔴 BINARY CONTENT SHIPS AS IT IS (operator decision O12, read by the coordinator as "bytes that
// begin with a known binary file signature", decision 6a). Everything else is text and is scanned
// — NUL-separated text segment by segment, text with stray invalid bytes with those bytes carried
// through, UTF-16 decoded and re-encoded — and base64 or a `data:` URL whose payload is not
// signature-bearing is scanned decoded. Residual (T1): a secret inside an image or a PDF.
//
// ⚠ NO SCANNER RECOGNISES AN UNSHAPED SECRET. A typed password, a novel token format, an encoded
// secret embedded inside a longer string, and a flow-style YAML `Secret` are not caught by any
// rule here; the corpus measures recall over the shapes it plants, and real recall is an
// operator-side, count-only measurement (Q4, adopted).
//
// ⚠ NOT SHARED WITH `tests/leakscan.py` (D5). Different predicates (a public source tree vs a
// session) and two regex engines; one behavioural containment test pins the relation — every
// realistic `credential` control in leakscan's self-test is redacted here.
package redact

import (
	"regexp"
	"strings"
)

// Rule is one row of the table.
//
// `Group` names the capture group holding the secret (0 = the whole match), so a rule can keep a
// recognisable keyword (`DB_PASSWORD=`) and replace only the value. `KeyGroup`, when non-zero,
// names a group holding a KEY that must satisfy `KeyOK` (default [SecretKey], the one predicate
// for "this name names a secret"). `Accept`, when set, vetoes a candidate value.
type Rule struct {
	Name     string
	Re       *regexp.Regexp
	Group    int
	KeyGroup int
	KeyOK    func(name string) bool
	Accept   func(secret string) bool
}

// notTrivial refuses a "value" that is a number, a boolean, a null or a placeholder — the values
// a `max_tokens = 4096` line or a `token_count: 12` line carry, which are not secrets.
func notTrivial(v string) bool {
	if v == "" {
		return false
	}
	allDigits := true
	for _, c := range v {
		if c < '0' || c > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return false
	}
	switch strings.ToLower(strings.Trim(v, `"'`)) {
	case "true", "false", "null", "none", "nil", "yes", "no", "changeme", "redacted", "required", "optional",
		"undefined", "string", "str", "number", "boolean", "bool", "int", "any", "bytes":
		return false
	}
	return !strings.HasPrefix(v, "[redacted:")
}

var (
	// attributePath is `a.b.c`: a reference to a setting — OR a dotted passphrase. Which one is
	// decided by codeReference below, never by the shape alone (review round 2: refusing every
	// dotted value lost 200 of 200 dotted passphrases).
	attributePath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+$`)
	// callOrIndex is an identifier path immediately followed by `(` or `[` and ENDING in a
	// bracket — `getpass.getpass()`, `os.environ[`, `config(`, `Optional[str]`. A password with a
	// `(` in its middle does not end in a bracket and is not refused.
	callOrIndex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*[(\[].*[()\[\]]$|^[A-Za-z_][A-Za-z0-9_.]*[(\[]$`)
	// referencePrefix is a leading segment that names a scope or a module, not a passphrase word.
	referencePrefix = regexp.MustCompile(`^(?:var|local|data|module|each|self|this|args|cfg|conf|config|settings|opts|options|os|process|env|req|request|res|ctx|params|props|creds|credentials|secrets|vault|app|import)\.`)
	bareWord        = regexp.MustCompile(`^[A-Za-z]*[a-z][A-Za-z]*$`)
	identifierTail  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*[;,]$`)
	yamlTag         = regexp.MustCompile(`^![A-Za-z_][A-Za-z0-9_]*$`)
)

// notCode refuses a value whose SHAPE is code or a placeholder — and only those shapes.
//
// 🔴 REVIEW ROUND 2 MEASURED THE FIRST VERSION REFUSING REAL SECRETS: it refused any value
// containing `()[]{}<>;$` or a backtick ANYWHERE, every dotted value and every letters-only value,
// so ~90% of symbol-bearing passwords (18 of 200) and every dotted passphrase (0 of 200) shipped,
// while its comment claimed only digit-free dictionary words were lost — false. What is refused now
// is a SHAPE:
//
//   - a call or an index: an identifier path immediately followed by `(` or `[`, ending in a
//     bracket (`os.Getenv(`, `getpass.getpass()`, `Optional[str]`);
//   - an expansion or a template: a value STARTING with `$`, `{{`, `%(` or a backtick;
//   - a placeholder: a value that is ENTIRELY `<…>`, one character repeated (`********`),
//     `your…`/`…_here`, a YAML tag (`!vault`), or a keyword/type name (`None`, `string`);
//   - a reference: an identifier path whose leading segment names a scope or module (`var.`,
//     `os.`, `process.env.`, `settings.`), or whose LAST segment itself names a secret
//     (`cfg.Password`, `config.jwtSecret`), or a letters-only word that names a secret
//     (`password`, `token`), or an identifier followed by a trailing `;`/`,` (`string;`).
//
// ⚠ THE COST, MEASURED (TestRedactorRecallOnRealisticPasswords): a password that happens to start
// with `$` or `{{`, end in a bracket after an identifier-shaped head, or be letters-only AND spell a
// secret word, is not caught by the line rules. Over the rate test's generators that is the whole
// residual; the numbers are in that test's assertions.
func notCode(v string) bool {
	switch {
	case !notTrivial(v):
		return false
	case callOrIndex.MatchString(v):
		return false
	case strings.HasPrefix(v, "$"), strings.HasPrefix(v, "{{"), strings.HasPrefix(v, "%("), strings.HasPrefix(v, "`"):
		return false
	case strings.HasPrefix(v, "<") && strings.HasSuffix(v, ">"):
		return false
	case repeatedRune(v), yamlTag.MatchString(v), identifierTail.MatchString(v):
		return false
	case strings.HasPrefix(strings.ToLower(v), "your"), strings.Contains(strings.ToLower(v), "_here"),
		strings.Contains(strings.ToLower(v), "-here"):
		return false
	case attributePath.MatchString(v):
		segs := strings.Split(v, ".")
		// A leading `snake_case` segment is a Terraform-style resource reference
		// (`random_password.db.result`) only when nothing in the value is UPPER case; a mixed-case
		// dotted value is a password (a corpus seed measured one shipping without this).
		hclRef := strings.Contains(segs[0], "_") && strings.ToLower(v) == v
		if referencePrefix.MatchString(v) || hclRef || SecretKey(segs[len(segs)-1]) {
			return false
		}
	case bareWord.MatchString(v) && SecretKey(v):
		return false
	}
	return true
}

func repeatedRune(v string) bool {
	rs := []rune(v)
	for _, r := range rs[1:] {
		if r != rs[0] {
			return false
		}
	}
	return len(rs) >= 3
}

// hasDigit is the bar a SHORT bearer-ish token must clear: a real token carries a digit; prose
// ("the bearer instrument") does not.
func hasDigit(v string) bool {
	return strings.ContainsAny(v, "0123456789") && notTrivial(v)
}

// keyVendors are the prefixes that make a `<PREFIX>_KEY` name a secret: a CLOSED set, because
// `_KEY` alone also ends `PRIMARY_KEY`, `SORT_KEY`, `CACHE_KEY` and `S3_OBJECT_KEY`.
const keyVendors = `APP|ENCRYPTION|SIGNING|MASTER|HMAC|JWT|CIPHER|CRYPTO|SESSION|COOKIE|LICENSE|WEBHOOK|ACCOUNT|` +
	`STORAGE|SERVICE|DEPLOY|OPENAI|ANTHROPIC|STRIPE|SENDGRID|MAILGUN|TWILIO|SLACK|GITHUB|GITLAB|CLOUDFLARE|` +
	`DATADOG|SENTRY|ALGOLIA|PUSHER|MAPBOX|GOOGLE|AZURE|AWS|CLICKUP|LINEAR|NOTION|HF|HUGGINGFACE|OPENROUTER|` +
	`GROQ|MISTRAL|COHERE|DEEPSEEK|REPLICATE|RESEND|POSTMARK|BREVO|PADDLE|SQUARE|SHOPIFY|FIREBASE|SUPABASE|` +
	`GEMINI|XAI|PINECONE|ELEVENLABS`

var (
	camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	acronymEnd    = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	// The secret word at a `_` boundary (or the start), optionally followed by a closed set of
	// suffixes that still name the secret's content (`SECRET_KEY_BASE`, `apiKeyValue`).
	secretWord = regexp.MustCompile(`(?:^|_)(?:SECRET|TOKEN|PASSWORD|PASSWD|PASSPHRASE|PASS|PWD|PRIVATE_?KEY|API_?KEY|ACCESS_?KEY|SECRET_?KEY|CREDENTIALS?|DSN|AUTH|AUTHORIZATION|AUTH_?TOKEN|BEARER|(?:` +
		keyVendors + `)_KEY)(?:_(?:BASE|VALUE|DATA|B64|BASE64))?_?[0-9]*$`)
	// A word GLUED to a prefix: `PGPASSWORD`, `MYSECRET`, `GHTOKEN`. Only the long words — a glued
	// `PASS` ends `BYPASS` and `COMPASS`, a glued `AUTH` ends `OAUTH`.
	gluedWord = regexp.MustCompile(`[A-Z0-9](?:PASSWORD|PASSWD|PASSPHRASE|SECRET|TOKEN)(?:_(?:BASE|VALUE|DATA))?_?[0-9]*$`)
)

// SecretKey is THE predicate for "this name names a secret" — for a `KEY=value` line, a YAML
// key, a `docker -e` flag, a k8s env `name:`, a query parameter and a decoded JSON object key
// alike. One spelling, so the text rules and the structural rule cannot disagree.
//
// 🔴 CASE- AND STYLE-INSENSITIVE: the name is normalised to UPPER_SNAKE first, so `DB_PASSWORD`,
// `dbPassword`, `db-password`, `SecretAccessKey`, `SessionToken`, `accessToken` and Docker's
// `auths.<host>.auth` all reach the same table.
//
// 🔴 THE SECRET WORD ENDS THE NAME, up to a closed suffix set (`_BASE`, `_VALUE`/`Value`, `_DATA`):
// `max_tokens`, `TOKEN_URL`, `DB_PASSWORD_FILE`, `passwordHash`, `secretKeyRef`, `apiKeySource`
// and `secret_name` name a count, a URL, a path, a hash, a reference, a source and a name. A word
// may also be GLUED to a prefix (`PGPASSWORD`), and `<VENDOR>_KEY` is a closed list (review round
// 2: the first END rule lost `PGPASSWORD`, `SECRET_KEY_BASE` and `apiKeyValue`).
//
// ⚠ `PWD` and `OLDPWD` in UPPER case are the shell's working directories, not passwords; `pwd` in
// any other case (a JSON field) is a password.
func SecretKey(name string) bool {
	n := strings.Trim(name, `"' `)
	if n == "PWD" || n == "OLDPWD" {
		return false
	}
	n = acronymEnd.ReplaceAllString(n, "${1}_${2}")
	n = camelBoundary.ReplaceAllString(n, "${1}_${2}")
	n = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(n)
	u := strings.ToUpper(n)
	return secretWord.MatchString(u) || gluedWord.MatchString(u)
}

// queryKey is the key predicate for `?name=value` / `&name=value` / `;name=value`: every
// [SecretKey] name, plus the parameter names that carry a credential without saying so.
func queryKey(name string) bool {
	switch strings.ToLower(name) {
	case "sig", "signature", "x-amz-signature", "x-goog-signature", "x-amz-security-token", "code", "key",
		"access_token", "id_token", "refresh_token", "client_secret", "apikey", "api_key":
		return true
	}
	return SecretKey(name)
}

// linePrefix is what may stand before a line in the copies a transcript carries of it: Read's
// numbered copy (`     1\t`, `1→`), `grep -n` (`path:12:`) and a diff (`+`, `-`). Without it the
// line-anchored rules saw only the raw file, and the model-visible copy shipped.
const linePrefix = `(?:[ \t]*[0-9]+(?:\t|→)|[^\s:]+:[0-9]+:|[+-])?`

// pemLinePrefix is the same copy grammar as it can stand before a PEM BODY line (after `\s*`).
const pemLinePrefix = `(?:[0-9]+(?:\t|→)|[^\s:]+:[0-9]+:|[+-])?`

// DefaultRules is the table, in application order. Specific shapes run before the generic
// keyword rules, so a recognised token is tagged by its own name rather than as "dotenv".
func DefaultRules() []Rule {
	pemLine := `(?:\s*` + pemLinePrefix + `[ \t]*`
	return []Rule{
		// BOUNDED: the header, optional `Name: value` header lines, the base64 body, and the END
		// line when present — EACH line possibly behind a copy prefix. An unbounded `.*?…|\z` ate
		// the rest of any file that merely MENTIONED a private-key header (round 1); a body that
		// stopped at the first `     2\t` prefix shipped the whole key read through Read or
		// `grep -n` (round 2). A mention now loses only its header.
		{Name: "pem-private-key", Re: regexp.MustCompile(
			`-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----` +
				`(?:\r?\n` + linePrefix + `[ \t]*[A-Za-z-]+: [^\n]*)*` +
				pemLine + `[A-Za-z0-9+/=]{16,}\r?)*` +
				pemLine + `=[A-Za-z0-9+/]{4}\r?)?` +
				pemLine + `-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----)?`)},
		{Name: "aws-access-key-id", Re: regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
		{Name: "github-token", Re: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}|\bgithub_pat_[A-Za-z0-9_]{22,}`)},
		{Name: "slack-token", Re: regexp.MustCompile(`\bxox[abprse]-[A-Za-z0-9-]{10,}|\bxapp-[0-9]+-[A-Za-z0-9-]{10,}`)},
		{Name: "slack-webhook", Re: regexp.MustCompile(`https://hooks\.slack\.com/(?:services|workflows|triggers)/[A-Za-z0-9/_-]{20,}`)},
		{Name: "age-secret-key", Re: regexp.MustCompile(`\bAGE-SECRET-KEY-[A-Z0-9]+`)},
		{Name: "anthropic-key", Re: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`)},
		{Name: "sk-key", Re: regexp.MustCompile(`\bsk-[A-Za-z0-9][A-Za-z0-9_-]{19,}`)},
		{Name: "google-api-key", Re: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`)},
		{Name: "stripe-key", Re: regexp.MustCompile(`\b[rs]k_(?:live|test)_[A-Za-z0-9]{16,}`)},
		{Name: "stripe-webhook-secret", Re: regexp.MustCompile(`\bwhsec_[A-Za-z0-9+/=]{20,}`)},
		{Name: "clickup-token", Re: regexp.MustCompile(`\bpk_[0-9]{3,}_[A-Z0-9]{20,}`)},
		{Name: "gitlab-token", Re: regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`)},
		{Name: "npm-token", Re: regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}`)},
		{Name: "huggingface-token", Re: regexp.MustCompile(`\bhf_[A-Za-z0-9]{30,}`)},
		{Name: "jwt", Re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
		// A header-named credential. The separator and the scheme are optional, for leakscan's
		// measured reason: `Authorization: Bearer <token>` puts a short word between keyword and
		// value.
		{Name: "authorization", Group: 1, Accept: hasDigit, Re: regexp.MustCompile(
			`(?i)\b(?:authorization|x-api-key|api-key|x-auth-token|private-token|x-access-token)\b["']?\s*[:=]\s*["']?(?:(?:bearer|basic|token|digest)\s+)?([A-Za-z0-9+/_.=~-]{8,})`)},
		{Name: "bearer", Group: 1, Accept: hasDigit, Re: regexp.MustCompile(`(?i)\bbearer\s+([A-Za-z0-9+/_.=~-]{8,})`)},
		// `scheme://user:pw@`, the user possibly EMPTY (`redis://:pw@host`).
		{Name: "url-userinfo-password", Group: 1, Re: regexp.MustCompile(
			`\b[A-Za-z][A-Za-z0-9+.-]{1,20}://[^\s:/@"'<>]*:([^\s@/"'<>]+)@`)},
		// A credential in a URL query or a `;`-separated connection string: `?token=`, `&api_key=`,
		// `X-Amz-Signature=`, JDBC `?password=`, Azure `;AccountKey=`, ADO.NET `;Password=`.
		{Name: "query-param", KeyGroup: 1, Group: 2, KeyOK: queryKey, Accept: notCode, Re: regexp.MustCompile(
			`[?&;]([A-Za-z][A-Za-z0-9_.-]*)=([^&\s#"';]{4,})`)},
		{Name: "curl-user", Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`(?:^|\s)(?:-u|--user)[ =]["']?[^\s:"']+:([^\s"']{4,})`)},
		{Name: "docker-login", Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`\bdocker\s+login\b[^\n]*?\s(?:-p|--password)[ =]["']?([^\s"']{4,})`)},
		{Name: "docker-env", KeyGroup: 1, Group: 2, Accept: notCode, Re: regexp.MustCompile(
			`(?:^|\s)(?:-e|--env)[ =]["']?([A-Za-z_][A-Za-z0-9_]*)=([^\s"']{4,})`)},
		{Name: "npmrc-auth", Group: 1, Re: regexp.MustCompile(`(?:_authToken|_auth|_password)\s*=\s*["']?([^\s"']{4,})`)},
		// `.pgpass`: `host:port:database:user:password`.
		{Name: "pgpass", Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`(?m)^` + linePrefix + `[^:\s]+:(?:[0-9]+|\*):[^:\s]+:[^:\s]+:(\S{4,})[ \t]*\r?$`)},
		// `.netrc`: `machine … login … password <pw>`, on one line or its own.
		{Name: "netrc-password", Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`(?mi)(?:\b(?:machine|login)\b[^\n]*\bpassword|^` + linePrefix + `[ \t]*password)[ \t]+(\S{4,})[ \t]*\r?$`)},
		// libpq keyword/value connection strings carry `password=` MID-line.
		{Name: "libpq-password", Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`(?i)(?:^|\s)(?:password|passwd)\s*=\s*["']?([^\s'";&]{4,})`)},
		// A SOURCE literal: a quoted value assigned to, or keyed by, a name [SecretKey] accepts —
		// `const apiSecret = '…'`, `token := "…"`, `password="…"` as a keyword argument,
		// `{ apiKey: "…" }`, `{'secret_key': '…'}`, `String DB_PASSWORD = "…";`.
		{Name: "source-literal", KeyGroup: 1, Group: 2, Accept: notCode, Re: regexp.MustCompile(
			`(?:^|[\s,({\[])["']?([A-Za-z_][A-Za-z0-9_.-]*)["']?[ \t]*(?::=|==?|:)[ \t]*["'` + "`" + `]([^"'` + "`" + `\n]{4,})["'` + "`" + `]`)},
		// A dotenv / YAML / INI line whose KEY names a secret ([SecretKey]), behind any copy prefix.
		{Name: "dotenv", KeyGroup: 1, Group: 2, Accept: notCode, Re: regexp.MustCompile(
			`(?m)^\x{FEFF}?` + linePrefix + `[ \t]*(?:export[ \t]+)?["']?([A-Za-z_][A-Za-z0-9_.-]*)["']?[ \t]*[=:][ \t]*["']?([^\s"'#,]{4,})`)},
	}
}
