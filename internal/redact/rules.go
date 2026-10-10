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
// 🔴 KEY CONTEXT PLUS ENTROPY (operator decision O15). The table is two general rules and a few
// shapes that carry no name: `key-context` redacts any value attached to a secret-sounding name in
// any notation (keyed.go), `entropy` redacts any long random-looking token wherever it stands
// (entropy.go), and the rest are vendor token formats, positional files (`.pgpass`, `.netrc`), URL
// userinfo, short CLI flags, private-key blocks and the YAML structure. Every rule sees a line's
// CONTENT: tool line prefixes are set aside before matching (normalise.go).
//
// ⚠ WHAT STILL GETS THROUGH: a secret that is neither NAMED nor RANDOM-LOOKING — a typed password
// in prose, an all-lower-case or hex token with no key before it, a short (< 20 character) unnamed
// token — and a flow-style YAML `Secret`. The corpus measures recall over the shapes it plants; the
// arming gate (heldback.go) over a held-back set; real recall is an operator-side, count-only
// measurement (Q4, adopted).
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
//
// `Find`, when set, replaces `Re`: a finder returning value spans (the key-context and entropy
// rules are parsers, not one regex). `Anchored` marks a rule that reads a line's START, and so is
// also run over every prefix-stripped VIEW (normalise.go); an unanchored rule finds the same spans
// in every view and runs once. `Late` marks the entropy rule: it runs after the whole-string base64
// decode, and not at all on a value the caller's structure calls opaque.
type Rule struct {
	Name     string
	Re       *regexp.Regexp
	Group    int
	KeyGroup int
	KeyOK    func(name string) bool
	Accept   func(secret string) bool
	Find     func(s string) [][2]int
	Anchored bool
	Late     bool
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
		"undefined", "string", "str", "number", "boolean", "bool", "int", "any", "bytes", "await", "async", "new",
		"function", "lambda", "typeof", "require", "self", "this", "super", "void", "return", "object", "float", "double":
		return false
	}
	return !strings.HasPrefix(v, "[redacted:")
}

var (
	// attributePath is `a.b.c`: a reference to a setting — OR a dotted passphrase. Which one is
	// decided by codeReference below, never by the shape alone (review round 2: refusing every
	// dotted value lost 200 of 200 dotted passphrases).
	attributePath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+$`)
	// callHead is an identifier path immediately followed by `(`, `[` or `{` — a call, an index, a
	// composite literal. Whether it IS code is decided by the head (see [codeHead]).
	callHead      = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.:]*)[(\[{]`)
	headWord      = regexp.MustCompile(`[A-Z][a-z]{2,}|[a-z]{3,}`)
	qualifiedHead = regexp.MustCompile(`[A-Za-z0-9_](?:\.|::)[A-Za-z_]`)
	// shellExpansion: `${…}`, `$(…)`, `$VAR` whole, or `$VAR/…` — not every value that starts with
	// `$` (round 4 measured 8 of 200 symbol-bearing passwords refused by a bare `$` prefix test).
	shellExpansion = regexp.MustCompile(`^\$(?:[{(]|[A-Za-z_][A-Za-z0-9_]*(?:$|/))`)
	// derefOrSlice is `&x`, `*x`, `[]byte(` — an address, a dereference, a conversion.
	derefOrSlice = regexp.MustCompile(`^[&*][A-Za-z_][A-Za-z0-9_.]*$|^\[\][A-Za-z_][A-Za-z0-9_.]*[({]`)
	// referencePrefix is a leading segment that names a scope or a module, not a passphrase word.
	referencePrefix = regexp.MustCompile(`^(?:var|local|data|module|each|self|this|args|cfg|conf|config|settings|opts|options|os|process|env|req|request|res|ctx|params|props|creds|credentials|secrets|vault|app|import)\.`)
	// goQualified is a package-qualified exported identifier: `tls.RequireAnyClientCert`.
	goQualified    = regexp.MustCompile(`^[a-z][a-z0-9]{0,9}\.[A-Z][A-Za-z0-9]*$`)
	bareWord       = regexp.MustCompile(`^[A-Za-z]*[a-z][A-Za-z]*$`)
	identifierTail = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*[;,]$`)
	nameLike       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
	shortLower     = regexp.MustCompile(`^[a-z]{3,4}$|^fn?$`)
	yamlTag        = regexp.MustCompile(`^![A-Za-z_][A-Za-z0-9_]*$`)
	bracedIdent    = regexp.MustCompile(`^\{[A-Za-z_0-9][A-Za-z0-9_.]*\}$|^\{\}$`)
	formatVerb     = regexp.MustCompile(`^%[-+# 0-9.]*[sdvqfxXcbegtpoT](?:$|[^A-Za-z0-9])|^\\[nrt]`)
	filePath       = regexp.MustCompile(`^(?:/|\./|\.\./|~/)[A-Za-z0-9_.@/-]*$`)
	// An identifier OR-ed, AND-ed or ??-ed with another: `e.apiKey||null`. Both sides must be
	// identifiers, so a password that merely contains `||` is not refused.
	logicalExpr = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$.]*(?:\|\||&&|\?\?)[A-Za-z_$][A-Za-z0-9_$.]*$`)
)

// codeHead: the identifier before a `(`/`[`/`{` reads as code when it is qualified (`a.b`,
// `std::env`), a short lower-case word (`env(`, `make(`, `str[`), a keyword (`func(`, `struct{`), or
// WORDY (`getToken(`, `loadSecret(`). A random password's head — `Xk9q` before a `(` — is none of those.
func codeHead(head string) bool {
	switch {
	case qualifiedHead.MatchString(head):
		return true
	case shortLower.MatchString(head):
		return true
	}
	n := 0
	for _, m := range headWord.FindAllStringIndex(head, -1) {
		n += m[1] - m[0]
	}
	return len(head) >= 5 && 4*n >= 3*len(head)
}

// notCode refuses a value whose SHAPE is code or a placeholder — and only those shapes.
//
// 🔴 REVIEW ROUND 2 MEASURED THE FIRST VERSION REFUSING REAL SECRETS: it refused any value
// containing `()[]{}<>;$` or a backtick ANYWHERE, every dotted value and every letters-only value,
// so ~90% of symbol-bearing passwords (18 of 200) and every dotted passphrase (0 of 200) shipped,
// while its comment claimed only digit-free dictionary words were lost — false. What is refused now
// is a SHAPE:
//
//   - a call, an index or a literal: an identifier head immediately followed by `(`, `[` or `{`,
//     when the head reads as code ([codeHead]: `os.Getenv(`, `getpass.getpass()`, `Optional[str]`,
//     `struct{}{}`, `func(`), or any value containing `()` or `{}`;
//   - an expansion, a template or a format: a value STARTING with `$`, `{{`, `%(`, a backtick, a
//     printf verb (`%s`, `%q`) or an escape (`\n`); `{token}`; an identifier `||`/`&&`/`??` another;
//   - a placeholder: a value that is ENTIRELY `<…>`, one character repeated (`********`),
//     `your…`/`…_here`, a YAML tag (`!vault`), a keyword/type name (`None`, `string`, `await`);
//   - a location: a path (`/run/secrets/db`, `./key.pem`, `~/.ssh/id_rsa`);
//   - a reference: an identifier path whose leading segment names a scope or module (`var.`,
//     `os.`, `process.env.`, `settings.`), a package-qualified exported name
//     (`tls.RequireAnyClientCert`), an identifier path whose LAST segment names a secret
//     (`cfg.Password`), a NAME that itself names a secret (`password`, `CAIRN_TOKEN` — an env
//     var's name, not its value), or an identifier followed by a trailing `;`/`,` (`string;`).
//
// ⚠ THE COST, MEASURED (TestRedactorRecallOnRealisticPasswords): a password that happens to take
// one of those shapes is not caught by a named rule. Over the rate test's generators that is the
// whole residual; the numbers are in that test's assertions.
func notCode(v string) bool {
	switch {
	case !notTrivial(v):
		return false
	case strings.Contains(v, "()"), strings.Contains(v, "{}"):
		return false
	case shellExpansion.MatchString(v), strings.HasPrefix(v, "{{"), strings.HasPrefix(v, "%("), strings.HasPrefix(v, "`"),
		formatVerb.MatchString(v):
		return false
	case strings.HasPrefix(v, "<") && strings.HasSuffix(v, ">"):
		return false
	case logicalExpr.MatchString(v):
		// An expression (`e.apiKey||null`), the shape minified code assigns.
		return false
	case bracedIdent.MatchString(v), filePath.MatchString(v):
		return false
	case repeatedRune(v), yamlTag.MatchString(v), identifierTail.MatchString(v):
		return false
	case strings.HasPrefix(strings.ToLower(v), "your"), strings.Contains(strings.ToLower(v), "_here"),
		strings.Contains(strings.ToLower(v), "-here"):
		return false
	case goQualified.MatchString(v), derefOrSlice.MatchString(v):
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
	case nameLike.MatchString(v) && SecretKey(v):
		return false
	}
	if m := callHead.FindStringSubmatch(v); m != nil && codeHead(m[1]) {
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
	`GEMINI|XAI|PINECONE|ELEVENLABS|CLIENT|TLS|SSL|SSH`

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
	if n == "PWD" || n == "OLDPWD" || n == "PASS" {
		// `PASS` in UPPER case alone is a test runner's verdict (`--- PASS: TestX`), not a key.
		return false
	}
	n = acronymEnd.ReplaceAllString(n, "${1}_${2}")
	n = camelBoundary.ReplaceAllString(n, "${1}_${2}")
	n = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(n)
	u := strings.ToUpper(n)
	return secretWord.MatchString(u) || gluedWord.MatchString(u)
}

// DefaultRules is the table, in application order: when two rules' spans overlap, the EARLIER
// rule names the merged span. Specific token shapes run before key context, so a recognised token
// is tagged by its own name; the entropy rule runs last of all.
//
// 🔴 NO RULE SPELLS A TOOL'S LINE PREFIX. Read's numbered copy, grep's `path:N:`/`N-`/`path:` and a
// diff's `+`/`-`/`<`/`>` are set aside ONCE, by the views in normalise.go, before any rule runs;
// a rule that reads a line's start says `Anchored` and is run over every view. Three rounds of
// teaching each rule each prefix left every rule blind to the prefixes it had not been taught.
func DefaultRules() []Rule {
	return []Rule{
		// BOUNDED: the header, optional `Name: value` header lines, the base64 body, and the END
		// line when present. An unbounded `.*?…|\z` ate the rest of any file that merely MENTIONED
		// a private-key header (round 1). A mention now loses only its header.
		{Name: "pem-private-key", Anchored: true, Re: regexp.MustCompile(
			`-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----` +
				`(?:\r?\n[ \t]*[A-Za-z-]+: [^\n]*)*` +
				`(?:\s*[A-Za-z0-9+/=]{16,}\r?)*` +
				`(?:\s*=[A-Za-z0-9+/]{4}\r?)?` +
				`(?:\s*-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----)?`)},
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
		// NARROWED by O15 to the query parameters that carry a credential WITHOUT a secret-sounding
		// name (`sig`, `X-Amz-Signature`, `code`, `key`); every `[SecretKey]` name in a query or a
		// connection string is the key-context rule's. A bare `key` is NOT one: `?key=` is also a
		// docs anchor (`?key=getting-started`, measured as damage in round 3); a random `key=` value
		// is the entropy rule's, and a Google key the google-api-key rule's.
		{Name: "query-param", KeyGroup: 1, Group: 2, KeyOK: unnamedQueryKey, Accept: notCode, Re: regexp.MustCompile(
			`[?&;]([A-Za-z][A-Za-z0-9_.-]*)=([^&\s#"';]{4,})`)},
		{Name: "curl-user", Anchored: true, Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`(?:^|\s)(?:-u|--user)[ =]["']?[^\s:"']+:([^\s"']{4,})`)},
		// SHORT flags carry no name, so they are a closed per-tool list: `docker login -p`,
		// `mysql -p<pw>` (glued — `-p <word>` is a database name), `redis-cli -a`, `sshpass -p`.
		{Name: "cli-flag", Anchored: true, Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`(?:^|\s)docker\s+login\b[^\n]*?\s(?:-p|--password)[ =]["']?([^\s"']{4,})`)},
		{Name: "cli-flag", Anchored: true, Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`(?:^|\s)(?:mysql|mysqldump|mysqladmin|mariadb)\b[^\n]*?\s-p([^\s"']{4,})`)},
		{Name: "cli-flag", Anchored: true, Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`(?:^|\s)redis-cli\b[^\n]*?\s-a[ \t]+["']?([^\s"']{4,})`)},
		{Name: "cli-flag", Anchored: true, Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`(?:^|\s)sshpass\b[^\n]*?\s-p[ \t]*["']?([^\s"']{4,})`)},
		// `.pgpass`: `host:port:database:user:password` — a POSITION, not a name. The password field
		// holds no UNESCAPED `:` (libpq's own escape is `\:`), so a grep `path:N:` in front of a
		// pgpass line cannot shift the fields and hand the rule `user:password` as the value.
		{Name: "pgpass", Anchored: true, Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`(?m)^[^:\s]+:(?:[0-9]+|\*):[^:\s]+:[^:\s]+:((?:[^:\s\\]|\\.){4,})[ \t]*\r?$`)},
		// `.netrc`: `machine … login … password <pw>`, on one line or its own — a whitespace join
		// the key-context rule takes only for a flag or a quoted value.
		{Name: "netrc-password", Anchored: true, Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`(?mi)(?:\b(?:machine|default)\b[^\n]*\bpassword|^[ \t]*password)[ \t]+(\S{4,})[ \t]*\r?$`)},
		// KEY CONTEXT: any value attached to a [SecretKey] name, in any notation (keyed.go).
		{Name: "key-context", Find: keyContextSpans},
		// ENTROPY: a long random-looking token anywhere (entropy.go). LAST, and late.
		{Name: "entropy", Find: entropySpans, Late: true},
	}
}

// unnamedQueryKey is the `query-param` rule's key predicate: the parameter names that carry a
// credential without [SecretKey] accepting them.
func unnamedQueryKey(name string) bool {
	switch strings.ToLower(name) {
	case "sig", "signature", "x-amz-signature", "x-goog-signature", "x-amz-security-token", "code":
		return true
	}
	return false
}
