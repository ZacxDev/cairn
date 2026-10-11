package redact

import (
	"regexp"
	"strings"
)

var (
	yamlSecretKind = regexp.MustCompile(`^[ \t]*kind:[ \t]*["']?Secret["']?[ \t]*\r?$`)
	yamlDataKey    = regexp.MustCompile(`^(?:data|stringData):[ \t]*$`)
	yamlKeyValue   = regexp.MustCompile(`^([^:#\s][^:]*):[ \t]*(.*)$`)
	yamlDocSep     = regexp.MustCompile(`^---[ \t]*\r?$`)
	// The list dash is optional: an env entry's `name:` may be the item's first key or a later one.
	yamlEnvName  = regexp.MustCompile(`^(?:-[ \t]*)?name:[ \t]*["']?([A-Za-z_][A-Za-z0-9_.-]*)["']?[ \t]*\r?$`)
	yamlEnvValue = regexp.MustCompile(`^value:[ \t]*(.+?)[ \t]*\r?$`)
)

// yamlLine is one line of a view: its body (no line break) and where the body starts in the view.
type yamlLine struct {
	body  string
	start int
}

// ySpan is a structural YAML match, in the coordinates of the text it was found in.
type ySpan struct {
	lo, hi int
	rule   string
}

func splitYAML(s string) []yamlLine {
	raw := strings.SplitAfter(s, "\n")
	out := make([]yamlLine, 0, len(raw))
	pos := 0
	for _, l := range raw {
		out = append(out, yamlLine{body: strings.TrimRight(l, "\r\n"), start: pos})
		pos += len(l)
	}
	return out
}

// yamlSpans is the STRUCTURAL rule over YAML text: in a document carrying a `kind: Secret` line,
// every value nested under a `data` or `stringData` key is redacted; and, in ANY document, a k8s
// env pair whose `name:` names a secret ([SecretKey]) has its `value:` redacted, and a block
// scalar under a secret key has its lines redacted.
//
// 🔴 IT DOES NOT READ COPY PREFIXES ITSELF. It runs over every VIEW of the text (normalise.go), so
// Read's numbered copy, a grep's `N:`/`path:`/`N-` and a diff's `<`/`>`/`+`/`-` are set aside
// before indentation is measured — the same normalisation every other rule gets.
//
// 🔴 IT IS LINE-BASED, BECAUSE THE STANDARD LIBRARY HAS NO YAML PARSER AND THE CAPTURE BINARY IS
// UNDER THE IMPORT BAN. "Nested under" means indented deeper than the key; a block scalar (`|` or
// `>`) redacts each of its deeper lines. `metadata.name` and every other key outside the two maps
// survive.
//
// ⚠ RESIDUAL: a FLOW-style mapping (`stringData: {password: …}` on one line) is not read by this
// rule; its values are caught only if another rule matches them (decision 6). The JSON form of a
// Secret is handled by the structural walk, which has a real parser.
func yamlSpans(s string) []ySpan {
	if !strings.Contains(s, ":") || !strings.Contains(s, "\n") {
		return nil
	}
	lines := splitYAML(s)
	var out []ySpan
	start := 0
	flush := func(end int) {
		doc := lines[start:end]
		for _, l := range doc {
			if yamlSecretKind.MatchString(l.body) {
				out = append(out, yamlSecretDoc(doc)...)
				break
			}
		}
		out = append(out, yamlEnvPairs(doc)...)
		out = append(out, yamlSecretScalars(doc)...)
	}
	for i, l := range lines {
		if yamlDocSep.MatchString(l.body) {
			flush(i)
			start = i + 1
		}
	}
	flush(len(lines))
	return out
}

func indentOf(body string) (string, int) {
	trimmed := strings.TrimLeft(body, " \t")
	return trimmed, len(body) - len(trimmed)
}

// valueSpan is the span of `val` (a substring of the line at byte offset off), quotes excluded.
func valueSpan(l yamlLine, off int, val, rule string) ySpan {
	lo, hi := off, off+len(val)
	if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
		lo, hi = lo+1, hi-1
	}
	return ySpan{lo: l.start + lo, hi: l.start + hi, rule: rule}
}

func yamlSecretDoc(lines []yamlLine) []ySpan {
	var out []ySpan
	inData, dataIndent, scalarIndent := false, -1, -1
	for i := range lines {
		trimmed, indent := indentOf(lines[i].body)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if inData && indent <= dataIndent {
			inData, scalarIndent = false, -1
		}
		if !inData {
			if yamlDataKey.MatchString(trimmed) {
				inData, dataIndent = true, indent
			}
			continue
		}
		if scalarIndent >= 0 {
			if indent > scalarIndent {
				out = append(out, valueSpan(lines[i], indent, strings.TrimRight(trimmed, " \t\r"), "k8s-secret"))
				continue
			}
			scalarIndent = -1
		}
		kv := yamlKeyValue.FindStringSubmatchIndex(trimmed)
		if kv == nil {
			continue
		}
		raw := trimmed[kv[4]:kv[5]]
		val := strings.TrimSpace(raw)
		switch {
		case val == "":
			continue
		case strings.HasPrefix(val, "|") || strings.HasPrefix(val, ">"):
			scalarIndent = indent
			continue
		}
		off := indent + kv[4] + (len(raw) - len(strings.TrimLeft(raw, " \t")))
		out = append(out, valueSpan(lines[i], off, val, "k8s-secret"))
	}
	return out
}

// yamlEnvPairs redacts the `value:` of a container env entry whose `name:` names a secret:
//
//   - name: DB_PASSWORD
//     value: <redacted>
func yamlEnvPairs(lines []yamlLine) []ySpan {
	var out []ySpan
	for i := range lines {
		trimmed, indent := indentOf(lines[i].body)
		m := yamlEnvName.FindStringSubmatch(trimmed)
		if m == nil || !SecretKey(m[1]) {
			continue
		}
		for j := i + 1; j < len(lines) && j <= i+3; j++ {
			t2, ind2 := indentOf(lines[j].body)
			if t2 == "" {
				continue
			}
			if ind2 <= indent && !strings.HasPrefix(t2, "value:") || strings.HasPrefix(t2, "-") {
				break
			}
			v := yamlEnvValue.FindStringSubmatchIndex(t2)
			if v == nil {
				continue
			}
			val := t2[v[2]:v[3]]
			if !notTrivial(strings.Trim(val, `"'`)) {
				break
			}
			out = append(out, valueSpan(lines[j], ind2+v[2], val, "k8s-env"))
			break
		}
	}
	return out
}
