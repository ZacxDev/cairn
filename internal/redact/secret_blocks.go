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

// redactYAMLEnvValueFirst is the env pair written VALUE FIRST — YAML maps are unordered, so
// `- value: …` followed by `name: DB_PASSWORD` is the same entry (review round 2).
func (r *Redactor) redactYAMLEnvValueFirst(lines []yamlLine) []Hit {
	var hits []Hit
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
				secret := strings.Trim(trimmed[v[2]:v[3]], `"'`)
				if notTrivial(secret) {
					mk, h := r.marker("k8s-env", secret)
					lines[i].body = lines[i].body[:indent] + trimmed[:v[2]] + mk
					hits = append(hits, h)
				}
				break
			}
		}
	}
	return hits
}

// redactYAMLSecretScalars redacts a BLOCK scalar (`|`, `>`, with chomping/indent indicators) under
// a key [SecretKey] accepts, in ANY YAML document — `password: |` followed by the value on deeper
// lines. The `KEY: value` rule sees only the indicator on the key's own line (review round 2).
// It also runs the value-first env pair.
func (r *Redactor) redactYAMLSecretScalars(lines []yamlLine) []Hit {
	hits := r.redactYAMLEnvValueFirst(lines)
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
			if !strings.HasPrefix(t2, "[redacted:") {
				mk, h := r.marker("yaml-block-secret", t2)
				lines[j].body = lines[j].body[:ind2] + mk
				hits = append(hits, h)
			}
			i = j
		}
	}
	return hits
}
