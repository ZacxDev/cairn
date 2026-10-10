package redact

import (
	"regexp"
	"strings"
)

// The POSITIONAL files: `.pgpass` and `.netrc` put a password at a position, not after a name, so
// neither general rule can read them. A position is weak evidence — the same bytes are a grep line,
// a struct field or a sentence — so each is read only with the STRUCTURE of its file.

// 🔴 PGPASS NEEDS PGPASS STRUCTURE (review round 5). Five colon-separated fields with a number in
// the second is also `path:N:C:` in front of any line holding one more colon —
// `src/pkg/file.go:100:7://go:noescape` lost `noescape`, `a.go:12:3://nolint:errcheck` lost
// `errcheck`. A row is taken only when:
//
//   - the HOST is `*`, a host name or address (no `/`), or an absolute socket directory;
//   - the PORT is `*` or one to five digits;
//   - the DATABASE and the USER are `*` or start with a letter or `_` — so a `path:N:C:` line, whose
//     third field is a column NUMBER, and a URL or comment (`//go`, `//nolint`) in the fourth, are
//     not rows;
//   - the password holds no unescaped `:` (libpq's own escape is `\:`), so a `path:N:` in front of a
//     real row cannot shift the fields, and it passes [notCode].
//
// ⚠ RESIDUAL, measured by `TestRoundSixPgpassNeedsPgpassStructure`: `grep -n` output over a line
// that is itself `word:word:word` with no space (`a.go:12:foo:bar:bazqux`) still reads as a row.
var (
	pgpassRow = regexp.MustCompile(`(?m)^([^:\s]+):([0-9]{1,5}|\*):([^:\s]+):([^:\s]+):((?:[^:\s\\]|\\.){4,})[ \t]*\r?$`)
	pgHost    = regexp.MustCompile(`^(?:\*|[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?|/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_-]+)$`)
	pgIdent   = regexp.MustCompile(`^(?:\*|[A-Za-z_][A-Za-z0-9_.$@-]*)$`)
)

func pgpassSpans(s string) [][2]int {
	var out [][2]int
	for _, m := range pgpassRow.FindAllStringSubmatchIndex(s, -1) {
		if !pgHost.MatchString(s[m[2]:m[3]]) || !pgIdent.MatchString(s[m[6]:m[7]]) || !pgIdent.MatchString(s[m[8]:m[9]]) {
			continue
		}
		if notCode(s[m[10]:m[11]]) {
			out = append(out, [2]int{m[10], m[11]})
		}
	}
	return out
}

// 🔴 NETRC: `password <word>` IS A RECORD LINE AND A SENTENCE, AND TWO ROUNDS FAILED IN OPPOSITE
// DIRECTIONS. Round 4 took every `password <word>` line a stripped prefix exposed and redacted
// `fix: password reset`, `12: password rotation` and `> password managers`. Round 5 then required a
// `machine`/`login` token within three lines for a plain-word value, and a lone `password letmein`
// (what `grep password ~/.netrc` prints), a record whose `machine` line sat further up behind
// comments, and a `password` line written BEFORE its `login` all shipped.
//
// The discriminator is netrc STRUCTURE or FILE CONTEXT, read over the whole block rather than three
// lines, and a deliberate, stated choice for the line with neither. A password value is taken when:
//
//  1. SAME LINE: the line is a netrc record — every token `default` or a `machine`/`login`/
//     `account`/`macdef`/`password` pair — that carries `machine`, or `default` with `login`;
//  2. BLOCK: a `machine …` or `default` record line stands within [netrcWindow] lines of it, above
//     OR below (comments, blanks and other records between);
//  3. FILE: the prefix this view set aside from the line names a netrc file (`cfg/.netrc:12:`), or
//     the word `netrc` stands on one of the [netrcWindow] lines above (`$ grep password ~/.netrc`);
//  4. EMBEDDED: `machine H login U password P` inside a longer line (`echo "…" >> ~/.netrc`), other
//     words between the pairs allowed unless the value is an [attributeWord];
//  5. the value is not a WORD — it carries a digit or a symbol and is not a slug of lower-case
//     words — and is not a type (`*uint16`, `[]byte`): what round 5 already took, in any view;
//  6. ALONE (the choice): the line is exactly `password <word>` with the keyword in lower case, it
//     carried no prefix or only a line NUMBER (Read's copy, `grep -n`), and the word is not an
//     [attributeWord] (`reset`, `rotation`, `policy`, `managers`, `hygiene`).
//
// ⚠ RESIDUALS, each measured in `TestRoundSixNetrc…`: (a) a lone plain-word password that IS an
// attribute word (`password reset` in a real `.netrc`) or sits behind a quote, diff or compose
// prefix with no netrc context ships; (b) a clean line that is exactly `password <non-attribute
// word>` — bare, or behind a line number — IS redacted (`12: password sharing`); (c) a block of
// prose holding both a line `machine <word>` and a line `password <word>` reads as a record.
const netrcWindow = 12

var (
	netrcEmbedded = regexp.MustCompile(`(?i)\bmachine[ \t]+\S+((?:[ \t]+(?:login|account)[ \t]+\S+)*)[ \t]+password[ \t]+([^\s"'` + "`" + `]{4,})`)
	// netrcLoose is the same record with OTHER words between its pairs (`machine h login u port 22
	// password p`): `machine <x>`, later `login <y>`, later `password <z>`, on one line.
	netrcLoose = regexp.MustCompile(`(?i)\bmachine[ \t]+\S+[^\n]*?\blogin[ \t]+\S+[^\n]*?\bpassword[ \t]+([^\s"'` + "`" + `]{4,})`)
	netrcName  = regexp.MustCompile(`(?i)netrc`)
)

type netrcLine struct {
	start int
	// record: every token is netrc grammar and there is at least one pair or a `default`.
	record       bool
	machine, def bool
	login        bool
	// lone: the line is exactly `password <value>`, keyword in lower case.
	lone bool
	pw   [][2]int
}

func parseNetrcLine(body string, start int) netrcLine {
	l := netrcLine{start: start}
	type tok struct{ lo, hi int }
	var toks []tok
	for i := 0; i < len(body); {
		for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == '\r') {
			i++
		}
		j := i
		for j < len(body) && body[j] != ' ' && body[j] != '\t' && body[j] != '\r' {
			j++
		}
		if j > i {
			toks = append(toks, tok{i, j})
			if len(toks) > 16 {
				return l // longer than any record line
			}
		}
		i = j
	}
	if len(toks) == 0 || body[toks[0].lo] == '#' {
		return l
	}
	for i := 0; i < len(toks); i++ {
		kw := body[toks[i].lo:toks[i].hi]
		switch strings.ToLower(kw) {
		case "default":
			l.def = true
			continue
		case "machine", "login", "account", "macdef", "password":
		default:
			return netrcLine{start: start}
		}
		if i+1 >= len(toks) {
			return netrcLine{start: start}
		}
		i++
		switch strings.ToLower(kw) {
		case "machine":
			l.machine = true
		case "login":
			l.login = true
		case "password":
			l.pw = append(l.pw, [2]int{start + toks[i].lo, start + toks[i].hi})
			l.lone = len(toks) == 2 && kw == "password"
		}
	}
	l.record = true
	return l
}

// weakNetrcValue: what prose puts after `password` — a word, CamelCase or not, or a slug of
// lower-case words (`reset`, `Policy`, `reset-flow`).
func weakNetrcValue(v string) bool {
	return lettersOnly.MatchString(v) || wordSlug.MatchString(v)
}

func netrcSpans(v *view) [][2]int {
	s := v.text
	if !strings.Contains(s, "assword") && !strings.Contains(s, "ASSWORD") {
		return nil
	}
	var out [][2]int
	for _, m := range netrcEmbedded.FindAllStringSubmatchIndex(s, -1) {
		val := s[m[4]:m[5]]
		if notCode(val) && (m[3] > m[2] || !weakNetrcValue(val)) {
			out = append(out, [2]int{m[4], m[5]})
		}
	}
	for _, m := range netrcLoose.FindAllStringSubmatchIndex(s, -1) {
		// Looser structure, so a word that prose puts after `password` is left alone here.
		if val := s[m[2]:m[3]]; notCode(val) && !(weakNetrcValue(val) && attributeWord(val)) {
			out = append(out, [2]int{m[2], m[3]})
		}
	}
	// One pass to parse every line; `anchor[i]` counts the block-context lines before line i and
	// `named[i]` the lines naming a netrc file, so each window is two subtractions.
	raw := strings.SplitAfter(s, "\n")
	lines := make([]netrcLine, len(raw))
	anchor := make([]int, len(raw)+1)
	named := make([]int, len(raw)+1)
	pos := 0
	for i, l := range raw {
		body := strings.TrimRight(l, "\r\n")
		lines[i] = parseNetrcLine(body, pos)
		anchor[i+1], named[i+1] = anchor[i], named[i]
		if lines[i].record && (lines[i].machine || lines[i].def) {
			anchor[i+1]++
		}
		if netrcName.MatchString(body) || netrcName.MatchString(v.prefix(pos)) {
			named[i+1]++
		}
		pos += len(l)
	}
	for i, l := range lines {
		if !l.record || len(l.pw) == 0 {
			continue
		}
		lo, hi := max(0, i-netrcWindow), min(len(lines), i+netrcWindow+1)
		context := l.machine || (l.def && l.login) || // 1: the same line
			anchor[hi]-anchor[lo] > boolInt(l.def) || // 2: the block (not the line's own `default`)
			named[i+1]-named[lo] > 0 // 3: the file
		for _, sp := range l.pw {
			val := s[sp[0]:sp[1]]
			if len(val) < 4 || !notCode(val) {
				continue
			}
			switch {
			case context:
			case !weakNetrcValue(val):
				// 5: not a word. A type after a field named `Password` is not a credential.
				if val[0] == '[' || (pointerTy.MatchString(val) && !randomOperand(strings.TrimLeft(val, "&*"))) {
					continue
				}
			default:
				// 6: alone.
				if !l.lone || !lineNumberPrefix(v.prefix(l.start)) || attributeWord(val) {
					continue
				}
			}
			out = append(out, sp)
		}
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
