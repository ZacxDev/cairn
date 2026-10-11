package redact

import (
	"regexp"
	"strings"
)

// 🔴 KEY CONTEXT (operator decision O15): a value attached to a secret-sounding NAME is redacted,
// whatever notation attaches it — unless its SHAPE is code, a placeholder or prose (below), or the
// name is only WEAK (its secret word a segment, not the end: `DB_PASSWORD_PROD`; or the abbreviation
// `cred`/`creds`) and the value is not CREDENTIAL-SHAPED ([credentialShaped]). A bare value of ANY
// length is taken: one over [maxBareValue] is judged by its first [longValueHead] bytes (below).
// ONE detector reads every notation, and the name is judged by ONE predicate ([SecretKey]); what is
// notation-specific is only how a name and its value are joined.
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
// an identifier followed by a binary operator, an expression opening (`(`/`[`/`{`) the value does not
// close after a spaced `=` or `:=`, a message or a usage alternation after a flag,
// a letters-only word followed by more words (prose), a lower-case word glued to `:`, a `<…>` with
// spaces in it, and a lower-case mode after a name that is only `auth`. The cost of each is measured
// by the clean-probe, rate and tree-budget tests, not argued here.

// nameStart and nameByte are the name grammar above, read by hand in [keyContextScan]: collecting
// every name with a regexp first allocated a slice per identifier in the text.
func nameStart(c byte) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func nameByte(c byte) bool {
	return nameStart(c) || c >= '0' && c <= '9' || c == '.' || c == '-'
}

// 🔴 LINEAR TIME, AND THE WAYS IT WAS NOT (review round 4 measured two; round 5 found a third).
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
//     256 KiB took over two minutes); its end is now memoised per stop-set, and the JUDGING is
//     bounded per name: a value up to [maxBareValue] is judged whole, a longer one by its first
//     [longValueHead] bytes with no trailing-punctuation strip — so a name costs at most
//     [maxBareValue], whatever the input. And a name INSIDE a value already taken is skipped.
//
// 🔴 THE BOUND IS ON THE WORK, NOT ON THE VALUE (review round 5). Round 5 bounded it by REFUSING
// every bare value over 1 KiB "to the entropy rule" — and the entropy rule does not read hex or a
// URL-encoded document, so `MASTER_KEY=<2048 hex>` and `AUTH_TOKEN=<1317 URL-encoded characters>`
// shipped 93–100% intact where round 4 had caught them. `TestRoundSixLongNamedValuesAreRedacted`
// pins 1 KiB, 2 KiB and 64 KiB.
//
// `TestRoundFiveKeyContextIsLinearTime` pins the XML search and the rescan by wall time at N and
// 4N (red at round 4's head), and `TestKeyContextIsLinearTime` by an operation count, which
// cannot flake.
const (
	maxBareValue  = 1024
	longValueHead = 64
)

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
	for i := 0; i < len(s); {
		if !nameStart(s[i]) {
			i++
			continue
		}
		lo := i
		for i++; i < len(s) && nameByte(s[i]); i++ {
		}
		hi := i
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
			// A name INSIDE a value already taken is not read: whatever it would attach is redacted
			// with the value, and re-reading it is what made `password=password=…` quadratic. The
			// scan resumes after the value — past the rest of a name the value ended inside.
			if v[1] > i {
				for i = v[1]; i < len(s) && nameByte(s[i]) && nameByte(s[i-1]); i++ {
				}
			}
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
	// A name that only WEAKLY names a secret: its word is a segment rather than the end
	// (`DB_PASSWORD_PROD`), or it is the abbreviation `cred`/`creds`, which names a struct or a
	// handle in code far more often than a credential string.
	needShape := !sqlName && (!strong || credAbbreviation(name))
	j := joinCtx{name: name, before: before, prev: prev, sqlName: sqlName}
	sp, ok := keyedJoin(s, lo, hi, &j, be)
	if !ok {
		return [2]int{}, false
	}
	if needShape && !(j.quoted && j.envAssign()) {
		// …so the value must itself look like a credential ([credentialShaped]) — except a QUOTED
		// value assigned to an ALL_CAPS environment-style name (`DB_PASSWORD_PROD="dragon"`), which
		// is a shell or dotenv assignment of a literal. A long value is judged by its head, so the
		// work per name stays bounded (and is counted: `token_x:token_x:…` is refused from every
		// name in it, and judging the whole remainder each time is quadratic).
		v := s[sp[0]:sp[1]]
		if len(v) > maxBareValue {
			v = v[:longValueHead]
		}
		be.ops += len(v)
		if !credentialShaped(v) {
			return [2]int{}, false
		}
	}
	return sp, true
}

// joinCtx is what the parse of one name knows: the name and its surroundings going in, and how its
// value was attached coming out.
type joinCtx struct {
	name, before string
	prev         byte
	sqlName      bool
	// out: the separator read, whether it was written with spaces, and whether the value was quoted.
	sep    string
	spaced bool
	quoted bool
}

var envStyleName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// lineStart: the name is the first thing on its line — behind nothing, or behind tool prefixes only
// ([onlyPrefix]). `before` holds at most 64 bytes, so a name further in than that is not at a start.
func (j *joinCtx) lineStart() bool {
	if nl := strings.LastIndexByte(j.before, '\n'); nl >= 0 {
		return onlyPrefix(j.before[nl+1:])
	}
	return len(j.before) < 64 && onlyPrefix(j.before)
}

// iniAssign: a name at the very start of its line, a SPACED `=`, and a value that ends the line —
// the INI, `.my.cnf` and `.properties` layout. Source code does not put `name = *x` at column 0:
// a declaration starts with a keyword or a type, a statement is indented.
func (j *joinCtx) iniAssign(s string, hi int) bool {
	if j.sep != "=" || !j.spaced || !j.lineStart() {
		return false
	}
	rest := s[hi:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	return strings.TrimRight(rest, " \t\r") == ""
}

// envAssign: an ALL_CAPS name glued to `=` — a shell, dotenv, `docker -e` or systemd assignment.
// It is CONFIG evidence that does not depend on what follows the value: `DB_PASSWORD=*Zq9…;` ends
// in code punctuation and is still not a dereference.
func (j *joinCtx) envAssign() bool {
	return j.sep == "=" && !j.spaced && envStyleName.MatchString(j.name)
}

// keyedJoin reads the join after a secret-sounding name and returns the value's span.
func keyedJoin(s string, lo, hi int, jc *joinCtx, be *bareEnds) ([2]int, bool) {
	name, before, prev, sqlName := jc.name, jc.before, jc.prev, jc.sqlName
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
		return acceptSpan(s, i+1, i+1+end, valueShape{digits: true})
	}
	// .NET appSettings: key="NAME" value="v".
	if quotedName {
		if m := dotnetValue.FindStringIndex(s[i:]); m != nil {
			jc.quoted = true
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
			if s[j] == '"' && name == "by" {
				// `the function identified by "repeat" is overwritten` — prose. Lower-case SQL
				// quotes a password with `'`.
				return [2]int{}, false
			}
			jc.quoted = true
			return quotedValue(s, j)
		}
		if !flag || s[j] == '-' || s[j] == '\n' || s[j] == '\r' || negatedFlag(name) {
			return [2]int{}, false
		}
		jc.sep = "flag"
		return unquotedValue(s, j, jc, false, false, false, be)
	}
	spaced := j > i
	k := skipBlank(s, j+len(sep))
	spaced = spaced && k > j+len(sep)
	jc.sep, jc.spaced = sep, spaced
	if k >= len(s) || s[k] == '\n' || s[k] == '\r' || (openQuote != 0 && s[k] == openQuote) {
		return [2]int{}, false
	}
	// A string-prefix letter before a quote: f"…", r'…', b"…", u"…".
	if k+1 < len(s) && strings.IndexByte("frbuFRBU", s[k]) >= 0 && (s[k+1] == '"' || s[k+1] == '\'') {
		k++
	}
	if s[k] == '"' || s[k] == '\'' || s[k] == '`' {
		jc.quoted = true
		sp, ok := quotedValue(s, k)
		if ok && sp[0] == k+1 && authMode(name, s[sp[0]:sp[1]]) {
			// `auth := "fail"`, `"auth": "basic"` — the mode, quoted. Not after a scheme word:
			// what follows `Bearer ` is the credential.
			return [2]int{}, false
		}
		return sp, ok
	}
	if sep == "," {
		return [2]int{}, false
	}
	sp, ok := unquotedValue(s, k, jc, queryish, connish, openQuote != 0, be)
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
	// A quoted string is a literal in every notation: never a dereference, never a template head.
	return acceptSpan(s, lo, hi, valueShape{digits: true})
}

// unquotedValue reads a bare value: to whitespace or a quote; in a URL query also to `&`/`#`, in
// a `;`-separated connection string also to `;`. `inString` is a name inside a string literal
// (`"token="+tok`), which is source code around it.
func unquotedValue(s string, k int, jc *joinCtx, queryish, connish, inString bool, be *bareEnds) ([2]int, bool) {
	for {
		hi := be.end(s, k, queryish, connish)
		if hi-k > maxBareValue {
			return longBareValue(s, k, hi, inString, be)
		}
		be.ops += hi - k
		if hi < len(s) && (s[hi] == ' ' || s[hi] == '\t') && schemeWord.MatchString(s[k:hi]+" ") {
			k = skipBlank(s, hi)
			continue
		}
		return bareValue(s, k, hi, jc, inString)
	}
}

// longBareValue takes a bare value over [maxBareValue] — a long key, a URL-encoded document, a
// certificate on one line. It is judged by its first [longValueHead] bytes and taken WHOLE, with no
// trailing-punctuation strip: both are what keep the work per name bounded (see [maxBareValue]). A
// run that long with no whitespace, quote or comma in it is not an identifier or a sentence, so the
// join-aware refusals of [bareValue] have nothing to read; what the head can still show is an
// expansion, a call, a placeholder or — inside a string literal — a template.
func longBareValue(s string, k, hi int, inString bool, be *bareEnds) ([2]int, bool) {
	be.ops += longValueHead
	head := s[k : k+longValueHead]
	if !notCode(head) || (inString && formatVerb.MatchString(head)) || (s[k] == '<' && s[hi-1] == '>') {
		// The last clause is [notCode]'s own `<…>` placeholder test, which needs the value's END.
		return [2]int{}, false
	}
	return [2]int{k, hi}, true
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

func bareValue(s string, k, hi int, jc *joinCtx, inString bool) ([2]int, bool) {
	sep, spaced := jc.sep, jc.spaced
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
	if hi-k >= 2 && s[hi-1] == ':' && s[hi-2] == ')' && closes[0] > opens[0] {
		// `def f(self, password_mgr=None):` — the `):` that closes a signature.
		hi -= 2
		tail = true
	}
	if hi <= k {
		return [2]int{}, false
	}
	v := s[k:hi]
	// CODE NOTATION, by the join: a Go `:=`, a Ruby/PHP `=>`, an assignment written with spaces, a
	// value that ended in code punctuation, or a name inside a string literal.
	code := sep == ":=" || sep == "=>" || (sep == "=" && spaced) || tail || inString
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
	case inString && sep == ":" && bareWord.MatchString(v) && verbAfter.MatchString(s[hi:]):
		// `t.Errorf("enc.EncodeToken: StartElement %s", err)` — a message, its argument a verb.
		return [2]int{}, false
	case sep == "flag" && (flagMessage.MatchString(v) || usageChoice.MatchString(v)):
		// `-issue-credential refused: …` (a message about the flag) and `-presence-token push|claim`
		// (a usage line's alternatives).
		return [2]int{}, false
	case (sep == ":" || (sep == "=" && spaced)) && codeIdent.MatchString(v) && operatorAfter.MatchString(s[hi:]):
		// `HasToken: cfgErr == nil,` `token = base + suffix` — an identifier in an expression. Not
		// after a GLUED `=`: `TOKEN=abc && ./run` is a shell assignment.
		return [2]int{}, false
	case ((sep == "=" && spaced) || sep == ":=") && unclosedOpening(v, opens, closes):
		// `seen[r.token] = (lineno, r)`, `credentials = {f[0] for …` — an expression opening that
		// the value does not close, after a SPACED assignment. Glued to `=` or after a YAML `:` the
		// same bytes are a password's first character (the rate test measures that: refusing them
		// after every code-notation join lost 34 of 4,000 symbol-bearing passwords).
		return [2]int{}, false
	}
	// 🔴 WHAT READS AS CODE IS DECIDED BY EVIDENCE ABOUT THE LINE, NOT BY SPACING OR TRAILING
	// PUNCTUATION ALONE (review round 5). Round 5 read `*p`, `&v` and a leading printf verb as code
	// wherever the join was code notation, so `password = *Zq9xK2mL7pQw` (the INI, `.my.cnf` and
	// `.properties` layout) and `DB_PASSWORD=*Zq9xK2mL7pQw;` shipped. Now:
	//
	//   - a DEREFERENCE needs code notation, NOT an env-style assignment ([joinCtx.envAssign]) and
	//     NOT the INI layout ([joinCtx.iniAssign]: the name starts its line, the value ends it), AND
	//     an operand that reads as an identifier rather than a random string ([valueShape.deref]);
	//   - a leading printf VERB or escape is a template only INSIDE A STRING LITERAL — unquoted,
	//     `x = %T-…` is not code in any language. (A value made only of verbs is a template in
	//     every notation: [formatTemplate].)
	//   - a digits-only value is a credential unless the notation is code (`DB_PASSWORD=482915`,
	//     `password: 123456`; not `token = 12345`, `Token: 4096,`). Under a WEAK name it never gets
	//     here: [credentialShaped] refuses a number — unless the value is quoted under an env-style
	//     name, which is read as a strong one (`DB_PASSWORD_PROD="482915"`).
	deref := code && !jc.envAssign() && !(!tail && jc.iniAssign(s, hi))
	return acceptSpan(s, k, hi, valueShape{deref: deref, verb: inString, digits: !code})
}

// unclosedOpening: v starts with `(`, `[` or `{` and opens more of that bracket than it closes.
func unclosedOpening(v string, opens, closes [3]int) bool {
	p := strings.IndexByte("([{", v[0])
	return p >= 0 && opens[p] > closes[p]
}

var (
	flagMessage   = regexp.MustCompile(`^[A-Za-z]+:$`)
	usageChoice   = regexp.MustCompile(`^[a-z-]+(?:\|[a-z-]+)+$`)
	verbAfter     = regexp.MustCompile(`^[ \t]+%[-+# 0-9.]*[a-zA-Z]`)
	operatorAfter = regexp.MustCompile(`^[ \t]+(?:==|!=|<=|>=|&&|\|\||[-+*/<>?])(?:[ \t]|$)`)
)

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

// valueShape is what the JOIN lets a value be read as (see the note in [bareValue]).
type valueShape struct {
	// deref: `*p` / `&v` over an identifier-shaped operand is a dereference or an address.
	deref bool
	// verb: a leading printf verb or string escape is a template (the name is inside a string).
	verb bool
	// digits: a digits-only value is a credential (not code notation).
	digits bool
}

func acceptSpan(s string, lo, hi int, sh valueShape) ([2]int, bool) {
	if hi <= lo || !keyedValueOK(s[lo:hi], sh) {
		return [2]int{}, false
	}
	return [2]int{lo, hi}, true
}

// keyedValueOK is the value filter shared by the key-context rule and the JSON walk's
// `secret-field` rule: at least 4 characters, not code-shaped ([notCode], and what the join lets
// read as code — [valueShape]), and — for a value with spaces — not a sentence.
func keyedValueOK(v string, sh valueShape) bool {
	if len(v) < 4 {
		return false
	}
	if allDigits(v) {
		// [notTrivial] refuses a number; a secret name outside code notation overrides it.
		return sh.digits
	}
	if !notCode(v) || (sh.verb && formatVerb.MatchString(v)) {
		return false
	}
	if sh.deref && derefOrSlice.MatchString(v) && !randomOperand(v[1:]) {
		return false
	}
	if strings.ContainsAny(v, " \t") && prose(v) {
		return false
	}
	return true
}

// randomOperand: what follows a `*`/`&` reads as a random string, not an identifier — at least 8
// characters, a digit between two letters, and not WORDY ([wordy], the entropy rule's own test).
// `tokenPtr`, `cfg.APIKey` and `secretRef` carry no such digit; `Zq9xK2mL7pQw` does. ⚠ An
// identifier with a digit inside and few vowels (`x509Cert`) reads as random here: the measured
// cost is in `budget_test.go`, the measured recall in
// `TestRoundSixSymbolLeadingValuesInConfigLayouts`.
func randomOperand(x string) bool {
	return len(x) >= 8 && digitInWord.MatchString(x) && !wordy(x)
}

func allDigits(v string) bool {
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return v != ""
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
