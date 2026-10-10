package redact

import (
	"regexp"
	"strings"
)

var (
	// copyPrefix is [linePrefix] as a line-start match: what a Read copy, `grep -n` or a diff
	// puts before a line. YAML is read AFTER it, so indentation is measured on the real line.
	copyPrefix     = regexp.MustCompile(`^` + linePrefix)
	yamlSecretKind = regexp.MustCompile(`^[ \t]*kind:[ \t]*["']?Secret["']?[ \t]*\r?$`)
	yamlDataKey    = regexp.MustCompile(`^(?:data|stringData):[ \t]*$`)
	yamlKeyValue   = regexp.MustCompile(`^([^:#\s][^:]*):[ \t]*(.*)$`)
	yamlDocSep     = regexp.MustCompile(`^---[ \t]*\r?$`)
	// The list dash is optional: at column 0 the copy prefix's `-` (a diff's) and a list item's
	// dash are the same byte, and either reading must find the pair.
	yamlEnvName  = regexp.MustCompile(`^(?:-[ \t]*)?name:[ \t]*["']?([A-Za-z_][A-Za-z0-9_.-]*)["']?[ \t]*\r?$`)
	yamlEnvValue = regexp.MustCompile(`^value:[ \t]*(.+?)[ \t]*\r?$`)
)

// yamlLine is one line split into the copy prefix (kept verbatim) and the YAML body.
type yamlLine struct {
	prefix, body, eol string
}

func splitYAML(s string) []yamlLine {
	raw := strings.SplitAfter(s, "\n")
	out := make([]yamlLine, 0, len(raw))
	for _, l := range raw {
		body := strings.TrimRight(l, "\r\n")
		eol := l[len(body):]
		p := copyPrefix.FindString(body)
		if yamlDocSep.MatchString(body) {
			// `---` is a document separator, not a diff's `-` before `--`.
			p = ""
		}
		out = append(out, yamlLine{prefix: p, body: body[len(p):], eol: eol})
	}
	return out
}

func joinYAML(lines []yamlLine) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.prefix + l.body + l.eol)
	}
	return b.String()
}

// yamlSecret is the STRUCTURAL rule over YAML text: in a document carrying a `kind: Secret` line,
// every value nested under a `data` or `stringData` key is redacted; and, in ANY document, a k8s
// env pair whose `name:` names a secret ([SecretKey]) has its `value:` redacted.
//
// 🔴 IT READS THROUGH A COPY PREFIX. Read's numbered copy (`     1\tkind: Secret`), `grep -n` and a
// diff put bytes before every line; the prefix is set aside and indentation is measured on the
// real YAML, so the model-visible copy of a manifest is redacted like the file.
//
// 🔴 IT IS LINE-BASED, BECAUSE THE STANDARD LIBRARY HAS NO YAML PARSER AND THE CAPTURE BINARY IS
// UNDER THE IMPORT BAN. "Nested under" means indented deeper than the key; a block scalar (`|` or
// `>`) redacts each of its deeper lines. `metadata.name` and every other key outside the two maps
// survive.
//
// ⚠ RESIDUAL: a FLOW-style mapping (`stringData: {password: …}` on one line) is not read by this
// rule; its values are caught only if another rule matches them (decision 6). The JSON form of a
// Secret is handled by the structural walk, which has a real parser.
func (r *Redactor) yamlSecret(s string) (string, []Hit) {
	if !strings.Contains(s, ":") || !strings.Contains(s, "\n") {
		return s, nil
	}
	lines := splitYAML(s)
	var hits []Hit
	start := 0
	flush := func(end int) {
		doc := lines[start:end]
		for _, l := range doc {
			if yamlSecretKind.MatchString(l.body) {
				hits = append(hits, r.redactYAMLSecretDoc(doc)...)
				break
			}
		}
		hits = append(hits, r.redactYAMLEnvPairs(doc)...)
		hits = append(hits, r.redactYAMLSecretScalars(doc)...)
	}
	for i, l := range lines {
		if yamlDocSep.MatchString(l.body) {
			flush(i)
			start = i + 1
		}
	}
	flush(len(lines))
	if len(hits) == 0 {
		return s, nil
	}
	return joinYAML(lines), hits
}

func indentOf(body string) (string, int) {
	trimmed := strings.TrimLeft(body, " \t")
	return trimmed, len(body) - len(trimmed)
}

func (r *Redactor) redactYAMLSecretDoc(lines []yamlLine) []Hit {
	var hits []Hit
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
				if !strings.HasPrefix(trimmed, "[redacted:") {
					m, h := r.marker("k8s-secret", trimmed)
					lines[i].body = lines[i].body[:indent] + m
					hits = append(hits, h)
				}
				continue
			}
			scalarIndent = -1
		}
		kv := yamlKeyValue.FindStringSubmatchIndex(trimmed)
		if kv == nil {
			continue
		}
		val := strings.TrimSpace(trimmed[kv[4]:kv[5]])
		switch {
		case val == "":
			continue
		case strings.HasPrefix(val, "|") || strings.HasPrefix(val, ">"):
			scalarIndent = indent
			continue
		case strings.HasPrefix(val, "[redacted:"), strings.HasPrefix(val, "\"[redacted:"):
			continue
		}
		secret := strings.Trim(val, `"'`)
		m, h := r.marker("k8s-secret", secret)
		lines[i].body = lines[i].body[:indent] + trimmed[:kv[4]] + m
		hits = append(hits, h)
	}
	return hits
}

// redactYAMLEnvPairs redacts the `value:` of a container env entry whose `name:` names a secret:
//
//   - name: DB_PASSWORD
//     value: <redacted>
func (r *Redactor) redactYAMLEnvPairs(lines []yamlLine) []Hit {
	var hits []Hit
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
			secret := strings.Trim(val, `"'`)
			if !notTrivial(secret) || strings.HasPrefix(secret, "[redacted:") {
				break
			}
			mk, h := r.marker("k8s-env", secret)
			lines[j].body = lines[j].body[:ind2] + t2[:v[2]] + mk
			hits = append(hits, h)
			break
		}
	}
	return hits
}
