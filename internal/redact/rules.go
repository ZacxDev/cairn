// Package redact is the host-side redactor for session transcripts: ONE rule table, applied to
// DECODED strings, with a keyed tag in place of every match.
//
// 🔴 IT RUNS ON DECODED STRING VALUES, NEVER ON RAW BYTES (`claudedocs/plan-cairn-plugins.md`,
// decision 6). A secret inside a JSON string can be written with escapes — a hyphen as the six
// characters backslash, `u`, `002d` — and a regex over the raw line then sees different bytes and
// misses it. So a record is decoded, every string value is scanned, and the record is re-encoded
// only when something matched; an untouched record keeps its ORIGINAL bytes.
//
// 🔴 A MATCH BECOMES `[redacted:<rule>:<tag>]`, AND THE TAG IS KEYED. `<tag>` is the first 8 hex of
// HMAC-SHA256 under a per-host secret: two occurrences of one secret on one host stay recognisably
// the same, and a reader without the key cannot confirm a guessed low-entropy secret by hashing it
// (T15). An unkeyed digest prefix would allow exactly that confirmation.
//
// 🔴 BINARY CONTENT SHIPS AS IT IS (operator decision O12, decision 6a). Every text-decodable
// string is scanned — base64 or a `data:` URL whose payload decodes to TEXT included — and a value
// whose payload is NOT text (an image, a PDF, UTF-16 text) is left byte-identical. That is a
// stated residual (T1): a secret inside an image, a PDF or non-UTF-8 text ships unredacted.
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
// recognisable keyword (`DB_PASSWORD=`) and replace only the value. `Accept`, when set, vetoes a
// candidate: it exists for the shapes whose regex cannot express "not a number".
type Rule struct {
	Name   string
	Re     *regexp.Regexp
	Group  int
	Accept func(secret string) bool
}

// notTrivial refuses a dotenv/field "value" that is a number, a boolean or a null — the values a
// `max_tokens = 4096` line or a `token_count: 12` line carry, which are not secrets and which a
// greedy rule would otherwise eat.
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
	case "true", "false", "null", "none", "nil", "yes", "no", "changeme", "redacted":
		return false
	}
	return !strings.HasPrefix(v, "[redacted:")
}

// secretKeyName is the "this key names a secret" predicate, ONE spelling shared by the dotenv
// text rule and the structural field rule (`traverse.go`) so the two cannot drift apart.
const secretKeyName = `[A-Za-z0-9_.-]*?(?:SECRET|TOKEN|PASSWORD|PASSWD|PASSPHRASE|PRIVATE_?KEY|API_?KEY|ACCESS_?KEY|CREDENTIALS?|DSN|AUTH)[A-Za-z0-9_]*`

// secretFieldRe is the structural twin of the dotenv rule: a decoded OBJECT key that names a
// secret. Anchored at a word boundary so `max_tokens` (`tokens`, plural, a count) and
// `input_tokens` do not match while `token`, `access_token` and `client_secret` do.
var secretFieldRe = regexp.MustCompile(`(?i)(?:^|[_.-])(?:secret|password|passwd|passphrase|token|api[_-]?key|access[_-]?key|secret[_-]?key|private[_-]?key|client[_-]?secret|credentials?|auth[_-]?token)$`)

// DefaultRules is the table, in application order. Specific shapes run before the generic
// keyword rules, so a recognised token is tagged by its own name rather than as "dotenv".
func DefaultRules() []Rule {
	return []Rule{
		{Name: "pem-private-key", Re: regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----(?s:.*?)(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|\z)`)},
		{Name: "aws-access-key-id", Re: regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
		{Name: "github-token", Re: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}|\bgithub_pat_[A-Za-z0-9_]{22,}`)},
		{Name: "slack-token", Re: regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
		{Name: "age-secret-key", Re: regexp.MustCompile(`\bAGE-SECRET-KEY-[A-Z0-9]+`)},
		{Name: "anthropic-key", Re: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`)},
		{Name: "sk-key", Re: regexp.MustCompile(`\bsk-[A-Za-z0-9][A-Za-z0-9_-]{19,}`)},
		{Name: "google-api-key", Re: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`)},
		{Name: "stripe-key", Re: regexp.MustCompile(`\b[rs]k_(?:live|test)_[A-Za-z0-9]{16,}`)},
		{Name: "clickup-token", Re: regexp.MustCompile(`\bpk_[0-9]{3,}_[A-Z0-9]{20,}`)},
		{Name: "gitlab-token", Re: regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`)},
		{Name: "npm-token", Re: regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}`)},
		{Name: "huggingface-token", Re: regexp.MustCompile(`\bhf_[A-Za-z0-9]{30,}`)},
		{Name: "jwt", Re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
		// 🔴 THE SEPARATOR AND THE SCHEME ARE OPTIONAL, for leakscan's measured reason:
		// `Authorization: Bearer <token>` puts a SPACE and a short word between the keyword and
		// the value, and a rule demanding `:` right before the token matched neither half.
		{Name: "authorization", Group: 1, Accept: notTrivial, Re: regexp.MustCompile(
			`(?i)\b(?:authorization|bearer|x-api-key|api-key)\b\s*[:=]?\s*(?:bearer\s+|basic\s+|token\s+)?["']?([A-Za-z0-9+/_.=~-]{20,})`)},
		{Name: "url-userinfo-password", Group: 1, Re: regexp.MustCompile(
			`\b[A-Za-z][A-Za-z0-9+.-]{1,20}://[^\s:/@"'<>]+:([^\s@/"'<>]+)@`)},
		// A dotenv / YAML / INI line whose KEY names a secret. Line-anchored, so prose that merely
		// mentions a password is not a match.
		{Name: "dotenv", Group: 1, Accept: notTrivial, Re: regexp.MustCompile(
			`(?mi)^[ \t]*(?:export[ \t]+)?["']?` + secretKeyName + `["']?[ \t]*[=:][ \t]*["']?([^\s"'#,]{4,})`)},
	}
}
