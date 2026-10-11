package redact

import (
	"regexp"
	"strings"
)

var (
	yamlEnvValueItem = regexp.MustCompile(`^(?:-[ \t]*)?value:[ \t]*(.+?)[ \t]*\r?$`)
	yamlEnvNameOnly  = regexp.MustCompile(`^name:[ \t]*["']?([A-Za-z_][A-Za-z0-9_.-]*)["']?[ \t]*\r?$`)
	yamlBlockKey     = regexp.MustCompile(`^(?:-[ \t]*)?["']?([A-Za-z_][A-Za-z0-9_.-]*)["']?:[ \t]*[|>][-+0-9]*[ \t]*\r?$`)
)

// yamlEnvValueFirst is the env pair written VALUE FIRST — YAML maps are unordered, so
// `- value: …` followed by `name: DB_PASSWORD` is the same entry (review round 2).
func yamlEnvValueFirst(lines []yamlLine) []ySpan {
	var out []ySpan
	for i := range lines {
		trimmed, indent := indentOf(lines[i].body)
		if !strings.HasPrefix(trimmed, "-") {
			continue
		}
		v := yamlEnvValueItem.FindStringSubmatchIndex(trimmed)
		if v == nil {
			continue
		}
		for j := i + 1; j < len(lines) && j <= i+3; j++ {
			t2, ind2 := indentOf(lines[j].body)
			if t2 == "" {
				continue
			}
			if ind2 <= indent || strings.HasPrefix(t2, "-") {
				break
			}
			if m := yamlEnvNameOnly.FindStringSubmatch(t2); m != nil && SecretKey(m[1]) {
				val := trimmed[v[2]:v[3]]
				if notTrivial(strings.Trim(val, `"'`)) {
					out = append(out, valueSpan(lines[i], indent+v[2], val, "k8s-env"))
				}
				break
			}
		}
	}
	return out
}

// yamlSecretScalars redacts a BLOCK scalar (`|`, `>`, with chomping/indent indicators) under a key
// [SecretKey] accepts, in ANY YAML document — `password: |` followed by the value on deeper lines.
// The key-context rule sees only the indicator on the key's own line (review round 2). It also runs
// the value-first env pair.
func yamlSecretScalars(lines []yamlLine) []ySpan {
	out := yamlEnvValueFirst(lines)
	for i := 0; i < len(lines); i++ {
		trimmed, indent := indentOf(lines[i].body)
		m := yamlBlockKey.FindStringSubmatch(trimmed)
		if m == nil || !SecretKey(m[1]) {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			t2, ind2 := indentOf(lines[j].body)
			if t2 == "" {
				continue
			}
			if ind2 <= indent {
				break
			}
			out = append(out, valueSpan(lines[j], ind2, strings.TrimRight(t2, " \t\r"), "yaml-block-secret"))
			i = j
		}
	}
	return out
}
