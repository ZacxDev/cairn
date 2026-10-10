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
// through, BOM-marked UTF-16 decoded and re-encoded — and base64 or a `data:` URL whose payload is
// not signature-bearing is scanned decoded. Residual (T1): a secret inside an image or a PDF.
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
// names a group holding a KEY that must satisfy [SecretKey] — the one predicate for "this name
// names a secret". `Accept`, when set, vetoes a candidate value.
type Rule struct {
	Name     string
	Re       *regexp.Regexp
	Group    int
	KeyGroup int
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
	case "true", "false", "null", "none", "nil", "yes", "no", "changeme", "redacted", "required", "optional":
		return false
	}
	return !strings.HasPrefix(v, "[redacted:")
}

var attributePath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+$`)

// notCode refuses a value that is CODE, not a literal: a call, an index, a reference to another
// variable or setting. A line-oriented `KEY=value` rule runs over source files a session reads,
// where `password: cfg.Password,` and `API_KEY = os.environ["API_KEY"]` are everywhere; eating
// them makes code unreadable and protects nothing (review round 1 measured 216 dotenv hits over
// 311 files of this repository, all code). A literal secret almost never contains these.
func notCode(v string) bool {
	if strings.ContainsAny(v, "()[]{}<>;$`") || attributePath.MatchString(v) || bareWord.MatchString(v) {
		return false
	}
	return notTrivial(v)
}

// bareWord is a value made of LETTERS only with a lowercase among them — `password`,
// `secretValue`, `token`: in code that is the name of another variable (`Password: password,`).
// ⚠ The cost, stated: a dictionary-word password with no digit in a `KEY=value` line is not
// caught by the line rules (it is an unshaped secret, T1's first residual, either way).
var bareWord = regexp.MustCompile(`^[A-Za-z]*[a-z][A-Za-z]*$`)

// hasDigit is the bar a SHORT bearer-ish token must clear: a real token carries a digit; prose
// ("the bearer instrument") does not.
func hasDigit(v string) bool {
	return strings.ContainsAny(v, "0123456789") && notTrivial(v)
}

var (
	camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	acronymEnd    = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	secretWord    = regexp.MustCompile(`(?:^|_)(?:SECRET|TOKEN|PASSWORD|PASSWD|PASSPHRASE|PRIVATE_?KEY|API_?KEY|ACCESS_?KEY|SECRET_?KEY|CREDENTIALS?|DSN|AUTH|AUTHORIZATION|AUTH_?TOKEN|BEARER)_?[0-9]*$`)
)

// SecretKey is THE predicate for "this name names a secret" — for a `KEY=value` line, a YAML
// key, a `docker -e` flag, a k8s env `name:`, and a decoded JSON object key alike. One spelling,
// so the text rules and the structural rule cannot disagree (review round 0 found two).
//
// 🔴 CASE- AND STYLE-INSENSITIVE: the name is normalised to UPPER_SNAKE first, so `DB_PASSWORD`,
// `dbPassword`, `db-password`, `SecretAccessKey`, `SessionToken`, `accessToken`, `refreshToken`,
// `bearerToken` and Docker's `auths.<host>.auth` all reach the same table.
//
// 🔴 THE SECRET WORD MUST END THE NAME. `max_tokens`, `TOKEN_URL`, `DB_PASSWORD_FILE`,
// `apiKeySource` and `secret_name` name a count, a URL, a PATH, a source and a name — not
// secrets — and a substring rule eats every one of them.
func SecretKey(name string) bool {
	n := strings.Trim(name, `"' `)
	n = acronymEnd.ReplaceAllString(n, "${1}_${2}")
	n = camelBoundary.ReplaceAllString(n, "${1}_${2}")
	n = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(n)
	return secretWord.MatchString(strings.ToUpper(n))
}

// linePrefix is what may stand before a `KEY=value` line in the copies a transcript carries of
// it: Read's numbered copy (`     1\t`, `1→`), `grep -n` (`path:12:`) and a diff (`+`, `-`).
// Without it the line-anchored rules saw only the raw file, and the model-visible copy shipped.
const linePrefix = `(?:[ \t]*[0-9]+(?:\t|→)|[^\s:]+:[0-9]+:|[+-])?`

// DefaultRules is the table, in application order. Specific shapes run before the generic
// keyword rules, so a recognised token is tagged by its own name rather than as "dotenv".
func DefaultRules() []Rule {
	return []Rule{
		// BOUNDED: the header, optional `Name: value` header lines, the base64 body, and the END
		// line when present. An unbounded `.*?…|\z` ate the rest of any file that merely
		// MENTIONED a private-key header (review round 1); a mention now loses only its header.
		{Name: "pem-private-key", Re: regexp.MustCompile(
			`-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----(?:\r?\n[A-Za-z-]+: [^\n]*)*(?:\s*[A-Za-z0-9+/=]{16,})*(?:\s*=[A-Za-z0-9+/]{4})?(?:\s*-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----)?`)},
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
		{Name: "curl-user", Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`(?:^|\s)(?:-u|--user)[ =]["']?[^\s:"']+:([^\s"']{4,})`)},
		{Name: "docker-env", KeyGroup: 1, Group: 2, Accept: notCode, Re: regexp.MustCompile(
			`(?:^|\s)(?:-e|--env)[ =]["']?([A-Za-z_][A-Za-z0-9_]*)=([^\s"']{4,})`)},
		{Name: "npmrc-auth", Group: 1, Re: regexp.MustCompile(`(?:_authToken|_auth|_password)\s*=\s*["']?([^\s"']{4,})`)},
		// libpq keyword/value connection strings carry `password=` MID-line.
		{Name: "libpq-password", Group: 1, Accept: notCode, Re: regexp.MustCompile(
			`(?i)(?:^|\s)(?:password|passwd)\s*=\s*["']?([^\s'";&]{4,})`)},
		// A dotenv / YAML / INI line whose KEY names a secret ([SecretKey]), behind any copy prefix.
		{Name: "dotenv", KeyGroup: 1, Group: 2, Accept: notCode, Re: regexp.MustCompile(
			`(?m)^\x{FEFF}?` + linePrefix + `[ \t]*(?:export[ \t]+)?["']?([A-Za-z_][A-Za-z0-9_.-]*)["']?[ \t]*[=:][ \t]*["']?([^\s"'#,]{4,})`)},
	}
}
