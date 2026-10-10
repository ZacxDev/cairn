package redact

import (
	"regexp"
	"strings"
)

// 🔴 KEY CONTEXT (operator decision O15): a value attached to a secret-sounding NAME is redacted,
// whatever notation attaches it — unless its SHAPE is code, a placeholder or prose (below), it is a
// bare value over [maxBareValue], or the name is only WEAK (its secret word a segment, not the end:
// `DB_PASSWORD_PROD`) and the value fails [weakNameValueOK]. ONE detector reads every notation, and the name is judged by ONE
// predicate ([SecretKey]); what is notation-specific is only how a name and its value are joined.
//
// It replaced five per-format rules (`dotenv`, `source-literal`, `libpq-password`, `docker-env`,
// `npmrc-auth`) after three review rounds measured the per-format approach fixing each named case
// while a held-back set stayed flat: every new notation (`Environment=`, `os.environ[...] =`,
// `--password`, `IDENTIFIED BY`, `<password>`) was a new miss for all of them at once.
//
// A NAME is a run of `[A-Za-z0-9_.-]` starting with a letter or `_`, not preceded by a word
// character, `$` or `${` (a `$NAME` is a use, not an assignment). The joins it reads:
//
//	NAME=v  NAME = v  NAME: v  NAME := v  NAME => v      assignment, YAML, JSON-ish, Ruby/PHP
//	"NAME": "v"  ['NAME'] = 'v'                          a quoted name; `]` after it is skipped
//	f("NAME", "v")                                       a `,` join: quoted name AND value, first call argument
//	--NAME=v  --NAME v  -NAME v                          a flag; a spaced value must not start with `-`
//	PASSWORD 'v'  IDENTIFIED BY 'v'                      a whitespace join to a QUOTED value — SQL's two only
//	<NAME>v</NAME>  key="NAME" value="v"                 XML, .NET appSettings
//
// `Environment=DB_PASSWORD=v` and `-e DB_PASSWORD=v` need nothing special: `DB_PASSWORD` is a name
// in its own right. A `Bearer`/`Basic`/`Token`/`Digest` scheme word before the value is skipped.
// A bare value runs to whitespace, a quote or a `,` (in a URL query also `&`/`#`, after a `;` also
// `;`); a quoted value runs to its closing quote ON THE SAME LINE, and an unterminated one is none.
//
// ⚠ WHAT IT CANNOT TELL APART, and the choice made: an unquoted value after `:` or ` = ` is a LITERAL
// in YAML, INI and shell and a VARIABLE in source code. It is taken as a literal unless its SHAPE is
// code — [notCode], and the join-aware refusals in [bareValue]: an identifier before trailing
// `,`/`;`/brackets, a bare identifier after `:=`/`=>`, a letters-only identifier after a SPACED `=`,
// a letters-only word followed by more words (prose), a lower-case word glued to `:`, a `<…>` with
// spaces in it, and a lower-case mode after a name that is only `auth`. The cost of each is measured
// by the clean-probe and rate tests, not argued here.

var nameRun = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.\-]*`)

// 🔴 LINEAR TIME, AND THE WAYS IT WAS NOT (review round 4 measured two; this round found a third).
// The finder visits every name in the text, so any per-name cost that grows with the INPUT rather
// than with the name makes the whole rule quadratic:
//
//   - the XML join searched for `</` to the end of the input for every `<NAME>` (1 MB of unclosed
//     `<password>` took 18 s); it now stops at the first `<`, `>` or line break;
//   - the trailing-bracket strip recounted the value's brackets per stripped character (1 MB of
//     `)` after one value took 8 s); it now counts once — ⚠ but since the cap below, the recount
//     would be BOUNDED anyway (at most ~1 KiB² of byte counting per value), so counting once is a
//     constant factor, not the linear-time guard: a mutant restoring the recount SURVIVES every
//     test, measured, and none is claimed to pin it;
//   - a bare value was rescanned, and re-judged, from every name inside it (`password=password=…`:
//     256 KiB took over two minutes); its end is now memoised per stop-set, and a bare value longer
//     than [maxBareValue] is not one — so a name costs at most that much, whatever the input.
//
// `TestRoundFiveKeyContextIsLinearTime` pins the XML search and the rescan by wall time at N and
// 4N (red at round 4's head), and `TestKeyContextIsLinearTime` by an operation count, which
// cannot flake.
const maxBareValue = 1024

// bareEnds memoises where a bare value starting at a given offset stops, per stop-set (plain, URL
// query, `;`-separated): the stop is the first stop byte at or after the start, so every start
// inside one scanned stretch shares its end. `ops` counts the bytes scanned and judged, for the
// linear-time test.
type bareEnds struct {
	from, to [3]int
	ops      int
}

// keyContextSpans is the `key-context` rule's finder.
func keyContextSpans(s string) [][2]int {
	spans, _ := keyContextScan(s)
	return spans
}

func keyContextScan(s string) ([][2]int, int) {
	var out [][2]int
	be := &bareEnds{from: [3]int{-1, -1, -1}, to: [3]int{-1, -1, -1}}
	for _, loc := range nameRun.FindAllStringIndex(s, -1) {
		lo, hi := loc[0], loc[1]
		for hi > lo && (s[hi-1] == '.' || s[hi-1] == '-') {
			hi--
		}
		name := s[lo:hi]
		if !mayNameSecret(name) {
			continue
		}
		before := ""
		if lo > 0 {
			before = s[max(0, lo-64):lo]
		}
		if v, ok := keyedValue(s, lo, hi, name, before, be); ok {
			out = append(out, v)
		}
	}
	return out, be.ops
}

// mayNameSecret is a cheap PREFILTER, not a predicate: every name [SecretKey] or
// [sqlPasswordName] accepts contains one of these, case-insensitively, because SecretKey's
// normalisation only inserts or swaps separators and never splits a word
// (`TestTheNamePrefilterAdmitsEverySecretName` pins it against SecretKey). It exists because the
// rule visits every identifier in the text and SecretKey is several regexps.
func mayNameSecret(name string) bool {
	if len(name) == 2 && (name[0]|0x20) == 'b' && (name[1]|0x20) == 'y' {
		return true // SQL's IDENTIFIED BY
	}
	l := strings.ToLower(name)
	for _, w := range secretSubstrings {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

var secretSubstrings = []string{"pass", "pwd", "secret", "token", "key", "cred", "auth", "dsn", "bearer"}

var (
	identifiedBy = regexp.MustCompile(`(?i)\bIDENTIFIED(?:\s+WITH\s+\S+)?\s+$`)
	dotnetValue  = regexp.MustCompile(`^[ \t]+value[ \t]*=[ \t]*`)
	schemeWord   = regexp.MustCompile(`(?i)^(?:bearer|basic|token|digest)[ \t]+`)
	// fileName: a name ending in a file extension (`pg.pass`, `secret.yaml`, `token.txt`).
	fileName = regexp.MustCompile(`\.(?:[a-z]{1,4}|yaml|json|toml|conf|pass|properties)$`)
)

// sqlPasswordName is a name a WHITESPACE join may attach a quoted value to: SQL's `PASSWORD 'v'`
// and `IDENTIFIED BY 'v'`. Any wider, and a closing quote reads as an opening one (`"Bearer "+tok`
// took `+tok` as the value of `Bearer` in the first build).
func sqlPasswordName(name, before string) bool {
	switch strings.ToUpper(name) {
	case "PASSWORD", "PASSWD":
		return true
	case "BY":
		return identifiedBy.MatchString(before)
	}
	return false
}

// authMode: a name that is only `auth`/`authorization` carries a MODE as often as a credential
// (`auth=cookie`, `auth: proxy`); a lower-case word there is the mode.
func authMode(name, v string) bool {
	u := strings.ToUpper(strings.TrimLeft(name, "_-"))
	return (u == "AUTH" || u == "AUTHORIZATION") && lowerWord.MatchString(v)
}

// keyedValue parses what follows the name at s[lo:hi] and returns the value's span.
func keyedValue(s string, lo, hi int, name, before string, be *bareEnds) ([2]int, bool) {
	var prev byte
	if lo > 0 {
		prev = s[lo-1]
		if isWordByte(prev) || prev == '$' || (prev == '{' && lo > 1 && s[lo-2] == '$') {
			return [2]int{}, false
		}
	}
	if prev == '/' || fileName.MatchString(name) {
		// A path component or a file name — the `path:` of a grep line (`deploy/pg.pass:…`) is a
		// prefix, not a key, and its "value" would be the whole matched line.
		return [2]int{}, false
	}
	sqlName := prev != '"' && prev != '\'' && sqlPasswordName(name, before)
	strong, weak := secretKeyGrade(name)
	if !strong && !weak && !sqlName {
		return [2]int{}, false
	}
	sp, ok := keyedJoin(s, lo, hi, name, before, prev, sqlName, be)
	if ok && !strong && !sqlName && !weakNameValueOK(s[sp[0]:sp[1]]) {
		// A secret word INSIDE the name rather than ending it (`DB_PASSWORD_PROD`): the value
		// must also look like one ([weakNameValueOK]).
		return [2]int{}, false
	}
	return sp, ok
}

// keyedJoin reads the join after a secret-sounding name and returns the value's span.
func keyedJoin(s string, lo, hi int, name, before string, prev byte, sqlName bool, be *bareEnds) ([2]int, bool) {
	flag := strings.HasSuffix(before, "--") || (strings.HasSuffix(before, "-") && (len(before) < 2 ||
		before[len(before)-2] == ' ' || before[len(before)-2] == '\t'))
	queryish := prev == '?' || prev == '&'
	connish := prev == ';'
	quotedName := false
	// openQuote is a quote that opened BEFORE the name and did not close after it: the name is
	// inside a string (`"token="+tok`, `` `AccountKey=` ``), and that quote CLOSING is not a value.
	var openQuote byte
	i := hi
	if prev == '"' || prev == '\'' || prev == '`' {
		if i < len(s) && s[i] == prev {
			quotedName = true
			i++
		} else {
			openQuote = prev
		}
	}
	// XML element: <NAME>value</NAME>.
	if prev == '<' && i < len(s) && s[i] == '>' {
		// The value is what precedes the first `<`, `>` or line break, and that `<` must open
		// `</`. Searching for `</` itself ran to the END of the input for every unclosed element.
		end := strings.IndexAny(s[i+1:], "<>\n")
		if end < 0 {
			be.ops += len(s) - i
		} else {
			be.ops += end + 1
		}
		if end <= 0 || s[i+1+end] != '<' || i+2+end >= len(s) || s[i+2+end] != '/' {
			return [2]int{}, false
		}
		return acceptSpan(s, i+1, i+1+end, false)
	}
	// .NET appSettings: key="NAME" value="v".
	if quotedName {
		if m := dotnetValue.FindStringIndex(s[i:]); m != nil {
			return quotedValue(s, i+m[1])
		}
	}
	if i < len(s) && s[i] == ']' {
		i++
	}
	j := skipBlank(s, i)
	sep := ""
	switch {
	case strings.HasPrefix(s[j:], ":="):
		sep = ":="
	case strings.HasPrefix(s[j:], "=>"):
		sep = "=>"
	case (strings.HasPrefix(s[j:], "==") || strings.HasPrefix(s[j:], "=~")) &&
		(j > i || j+2 >= len(s) || s[j+2] == ' ' || s[j+2] == '\t'):
		// A comparison (`token == ""`, `$x =~ /re/`) — written with a space. Glued, it is an
		// assignment of a value that starts with `=` or `~` (`DB_PASSWORD==0Tm…`).
		return [2]int{}, false
	case strings.HasPrefix(s[j:], "="):
		sep = "="
	case strings.HasPrefix(s[j:], "::"):
		return [2]int{}, false
	case strings.HasPrefix(s[j:], ":"):
		sep = ":"
	case strings.HasPrefix(s[j:], ",") && quotedName && prev != '`' && callBefore(s, lo-1):
		// `os.Setenv("API_TOKEN", "v")`, `setdefault('token', 'v')`: a quoted name as a call's first
		// argument. Not a list: `["password", "token"]` and `` `passwd`, `su` `` are names.
		sep = ","
	}
	if sep == "" {
		// A whitespace join: a long flag's value, or a quoted value after SQL's PASSWORD / BY.
		if j == i || j >= len(s) {
			return [2]int{}, false
		}
		if sqlName && (s[j] == '"' || s[j] == '\'') {
			return quotedValue(s, j)
		}
		if !flag || s[j] == '-' || s[j] == '\n' || s[j] == '\r' || negatedFlag(name) {
			return [2]int{}, false
		}
		return unquotedValue(s, j, "flag", false, false, false, false, be)
	}
	spaced := j > i
	k := skipBlank(s, j+len(sep))
	spaced = spaced && k > j+len(sep)
	if k >= len(s) || s[k] == '\n' || s[k] == '\r' || (openQuote != 0 && s[k] == openQuote) {
		return [2]int{}, false
	}
	// A string-prefix letter before a quote: f"…", r'…', b"…", u"…".
	if k+1 < len(s) && strings.IndexByte("frbuFRBU", s[k]) >= 0 && (s[k+1] == '"' || s[k+1] == '\'') {
		k++
	}
	if s[k] == '"' || s[k] == '\'' || s[k] == '`' {
		return quotedValue(s, k)
	}
	if sep == "," {
		return [2]int{}, false
	}
	sp, ok := unquotedValue(s, k, sep, spaced, queryish, connish, openQuote != 0, be)
	if ok && authMode(name, s[sp[0]:sp[1]]) {
		return [2]int{}, false
	}
	return sp, ok
}

// negatedFlag: `--no-creds`, `--no_password` — a boolean negation, which never takes a value; the
// word after it is the next argument (`skopeo inspect --no-creds docker://…`).
func negatedFlag(name string) bool {
	l := strings.ToLower(name)
	return strings.HasPrefix(l, "no-") || strings.HasPrefix(l, "no_")
}

// callBefore: the byte at q (an opening quote) follows a `(`, blanks allowed.
func callBefore(s string, q int) bool {
	for q--; q >= 0 && (s[q] == ' ' || s[q] == '\t'); q-- {
	}
	return q >= 0 && s[q] == '('
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func skipBlank(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

// quotedValue reads a quoted value starting at the quote s[k]. An unterminated quote is NO value:
// it is a string closing, not opening (see the check below).
func quotedValue(s string, k int) ([2]int, bool) {
	q := s[k]
	lo := k + 1
	hi := lo
	for hi < len(s) && s[hi] != q && s[hi] != '\n' {
		if s[hi] == '\\' && hi+1 < len(s) {
			hi++
		}
		hi++
	}
	if hi >= len(s) || s[hi] != q {
		// UNTERMINATED on its line: the "opening" quote was a closing one (`" auth="+mode`), so
		// there is no quoted value here.
		return [2]int{}, false
	}
	if m := schemeWord.FindStringIndex(s[lo:hi]); m != nil {
		lo += m[1]
	}
	return acceptSpan(s, lo, hi, false)
}

// unquotedValue reads a bare value: to whitespace or a quote; in a URL query also to `&`/`#`, in
// a `;`-separated connection string also to `;`. `inString` is a name inside a string literal
// (`"token="+tok`), which is source code around it.
func unquotedValue(s string, k int, sep string, spaced, queryish, connish, inString bool, be *bareEnds) ([2]int, bool) {
	for {
		hi := be.end(s, k, queryish, connish)
		if hi-k > maxBareValue {
			// Longer than any credential a name carries; a long random run is the entropy rule's.
			return [2]int{}, false
		}
		be.ops += hi - k
		if hi < len(s) && (s[hi] == ' ' || s[hi] == '\t') && schemeWord.MatchString(s[k:hi]+" ") {
			k = skipBlank(s, hi)
			continue
		}
		return bareValue(s, k, hi, sep, spaced, inString)
	}
}

// end is where a bare value starting at k stops, memoised (see [bareEnds]).
func (be *bareEnds) end(s string, k int, queryish, connish bool) int {
	set := 0
	if queryish {
		set = 1
	} else if connish {
		set = 2
	}
	if be.from[set] >= 0 && k >= be.from[set] && k <= be.to[set] {
		return be.to[set]
	}
	hi := k
	for hi < len(s) {
		c := s[hi]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '"' || c == '\'' || c == '`' || c == ',' ||
			(queryish && (c == '&' || c == '#')) || (connish && c == ';') {
			break
		}
		hi++
		be.ops++
	}
	be.from[set], be.to[set] = k, hi
	return hi
}

func bareValue(s string, k, hi int, sep string, spaced, inString bool) ([2]int, bool) {
	// Trailing code punctuation: `cfg.Password,` `string;` `tok})`. A value that STOPPED at a `,`
	// has one too. A closing bracket is stripped while the value closes more of it than it opens;
	// the brackets are counted ONCE and the count decremented as each is stripped.
	tail := hi < len(s) && s[hi] == ','
	var opens, closes [3]int
	const brackets = "()[]{}"
	for i := k; i < hi; i++ {
		if p := strings.IndexByte(brackets, s[i]); p >= 0 {
			if p%2 == 0 {
				opens[p/2]++
			} else {
				closes[p/2]++
			}
		}
	}
	for hi > k {
		c := s[hi-1]
		strip := c == ',' || c == ';'
		if p := strings.IndexByte(brackets, c); p >= 0 && p%2 == 1 && closes[p/2] > opens[p/2] {
			closes[p/2]--
			strip = true
		}
		if !strip {
			break
		}
		hi--
		tail = true
	}
	v := s[k:hi]
	switch {
	case tail && codeIdent.MatchString(v) && (!strings.ContainsAny(v, "0123456789") || strings.Contains(v, ".")):
		// `Password: hashed,` `token: tok})` `Token: cfg.Token,` — a reference in a literal. A
		// random value that merely ENDS in `)`/`]`/`;` carries a digit and no dot, and is kept.
		return [2]int{}, false
	case (sep == ":=" || sep == "=>") && codeIdent.MatchString(v):
		// A Go `:=` or a Ruby/PHP `=>` with a bare right-hand side assigns a VARIABLE.
		return [2]int{}, false
	case strings.HasPrefix(v, "<") && placeholderClose(s, k):
		// `<the agent's token>`, `<redacted: shown once>` — a placeholder with spaces in it.
		return [2]int{}, false
	case sep == ":" && k > 0 && s[k-1] == ':' && lowerWord.MatchString(v):
		// `credentials:edit`, `secret:list` — a command or a scalar, not a mapping: YAML needs a space.
		return [2]int{}, false
	case sep == "=" && spaced && lettersIdent.MatchString(v):
		// `password = hashed`: shell forbids the spaces, so this is code assigning a variable.
		return [2]int{}, false
	case (sep == ":" || sep == "flag") && proseAfter(s, k, hi, v):
		// `The secret: keep it out of the logs.`
		return [2]int{}, false
	}
	// 🔴 CODE NOTATION is decided by the JOIN, never by the value: a Go `:=`, a Ruby/PHP `=>`, an
	// assignment written with spaces, a value that ended in code punctuation, or a name inside a
	// string literal. Only there are `*p`, `&v`, `%s…` and `\n` read as code ([codeOnlyShape]); in a
	// dotenv/shell/YAML value or a quoted string they are a password's first character (round 4
	// measured `DB_PASSWORD=&…` at 28/200 and `%T-…` at 0/200 when they were refused everywhere).
	code := sep == ":=" || sep == "=>" || (sep == "=" && spaced) || tail || inString
	return acceptSpan(s, k, hi, code)
}

var (
	codeIdent    = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$.]*$`)
	lettersIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z_.]*$`)
	letterWord   = regexp.MustCompile(`^[ \t]+[A-Za-z]`)
	lowerWord    = regexp.MustCompile(`^[a-z]+$`)
)

// placeholderClose: a `>` closes the `<` at k later on the same line, within 80 bytes, with a
// space between — `<the agent's token>`. (A space-free `<TOKEN>` is [notCode]'s.)
func placeholderClose(s string, k int) bool {
	end := min(len(s), k+80)
	if nl := strings.IndexByte(s[k:end], '\n'); nl >= 0 {
		end = k + nl
	}
	gt := strings.IndexByte(s[k+1:end], '>')
	return gt >= 0 && strings.ContainsAny(s[k+1:k+1+gt], " \t")
}

// proseAfter: a letters-only value followed on the same line by another word is a sentence.
func proseAfter(s string, k, hi int, v string) bool {
	return bareWord.MatchString(v) && letterWord.MatchString(s[hi:])
}

func acceptSpan(s string, lo, hi int, code bool) ([2]int, bool) {
	if hi <= lo || !keyedValueOK(s[lo:hi], code) {
		return [2]int{}, false
	}
	return [2]int{lo, hi}, true
}

// keyedValueOK is the value filter shared by the key-context rule and the JSON walk's
// `secret-field` rule: at least 4 characters, not code-shaped ([notCode]; in CODE notation also
// not [codeOnlyShape]), and — for a value with spaces — not a sentence.
func keyedValueOK(v string, code bool) bool {
	if len(v) < 4 || !notCode(v) || (code && codeOnlyShape(v)) {
		return false
	}
	if strings.ContainsAny(v, " \t") && prose(v) {
		return false
	}
	return true
}

var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true, "be": true, "been": true, "must": true,
	"should": true, "to": true, "of": true, "at": true, "least": true, "your": true, "not": true, "and": true,
	"or": true, "for": true, "with": true, "enter": true, "please": true, "invalid": true, "has": true,
	"have": true, "was": true, "were": true, "this": true, "that": true, "it": true, "in": true, "on": true,
	"by": true, "no": true, "do": true, "does": true, "can": true, "cannot": true, "will": true, "you": true,
	"if": true, "from": true, "as": true, "required": true, "expired": true, "wrong": true, "incorrect": true,
}

var proseWord = regexp.MustCompile(`^[A-Za-z][a-z']*[.!?:,]?$`)

// prose is a value made only of words, at least one of which is a stop word or names a secret,
// or that ends with sentence punctuation: a UI string, a validation message, a sentence. A
// diceware passphrase (`correct horse battery staple`) has none of the three and is kept.
func prose(v string) bool {
	words := strings.Fields(v)
	if len(words) < 2 {
		return false
	}
	signal := false
	for _, w := range words {
		if !proseWord.MatchString(w) {
			return false
		}
		bare := strings.ToLower(strings.TrimRight(w, ".!?:,"))
		if stopWords[bare] || SecretKey(bare) {
			signal = true
		}
	}
	last := words[len(words)-1]
	return signal || strings.ContainsAny(last[len(last)-1:], ".!?")
}
