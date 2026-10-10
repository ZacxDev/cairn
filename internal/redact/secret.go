package redact

import (
	"regexp"
	"strings"
)

var (
	yamlSecretKind = regexp.MustCompile(`(?m)^[ \t]*kind:[ \t]*["']?Secret["']?[ \t]*\r?$`)
	yamlDataKey    = regexp.MustCompile(`^(?:data|stringData):[ \t]*$`)
	yamlKeyValue   = regexp.MustCompile(`^([^:#\s][^:]*):[ \t]*(.*)$`)
	yamlDocSep     = regexp.MustCompile(`^---[ \t]*\r?$`)
)

// yamlSecret is the STRUCTURAL rule over YAML text: in a document carrying a `kind: Secret` line,
// every value nested under a `data:` or `stringData:` key is redacted.
//
// 🔴 IT IS LINE-BASED, BECAUSE THE STANDARD LIBRARY HAS NO YAML PARSER AND THE CAPTURE BINARY IS
// UNDER THE IMPORT BAN. "Nested under" means indented deeper than the key; a block scalar (`|` or
// `>`) redacts each of its deeper lines. `metadata.name` and every other key outside the two maps
// survive.
//
// ⚠ RESIDUAL: a FLOW-style mapping (`stringData: {password: …}` on one line) is not read by this
// rule; its values are caught only if another rule matches them (decision 6). The JSON form of a
// Secret is handled by the structural walk (`walk`), which has a real parser.
func (r *Redactor) yamlSecret(s string) (string, []Hit) {
	if !strings.Contains(s, "Secret") || !yamlSecretKind.MatchString(s) {
		return s, nil
	}
	lines := strings.SplitAfter(s, "\n")
	var hits []Hit
	// Split into `---` documents; only a document that itself says `kind: Secret` is touched.
	start := 0
	flush := func(end int) {
		doc := strings.Join(lines[start:end], "")
		if yamlSecretKind.MatchString(doc) {
			hits = append(hits, r.redactYAMLSecretDoc(lines[start:end])...)
		}
	}
	for i, l := range lines {
		if yamlDocSep.MatchString(strings.TrimRight(l, "\n")) {
			flush(i)
			start = i + 1
		}
	}
	flush(len(lines))
	return strings.Join(lines, ""), hits
}

func (r *Redactor) redactYAMLSecretDoc(lines []string) []Hit {
	var hits []Hit
	inData, dataIndent, scalarIndent := false, -1, -1
	for i, line := range lines {
		body := strings.TrimRight(line, "\r\n")
		eol := line[len(body):]
		trimmed := strings.TrimLeft(body, " \t")
		indent := len(body) - len(trimmed)
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
					lines[i] = body[:indent] + m + eol
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
		lines[i] = body[:indent] + trimmed[:kv[4]] + m + eol
		hits = append(hits, h)
	}
	return hits
}
