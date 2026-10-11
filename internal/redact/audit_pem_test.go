package redact

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// TestAuditPEMBodyIsRedactedThroughEveryCopyPrefix: at 2ba3e5c the bounded PEM rule stopped at the
// first copy prefix, so a key read through Read (`  1\t`, `1→`) or `grep -n` shipped 3 of 3 body
// lines. Asserted 0 of 3 for every copy shape.
func TestAuditPEMBodyIsRedactedThroughEveryCopyPrefix(t *testing.T) {
	r2seed(3)
	r, _ := New(bytes.Repeat([]byte{7}, 32), nil)
	body := base64.StdEncoding.EncodeToString([]byte(r2pick(r2alnum, 200)))
	lines := []string{"-----BEGIN OPENSSH " + "PRIVATE KEY-----", body[:70], body[70:140], body[140:210], "-----END OPENSSH " + "PRIVATE KEY-----"}
	shapes := map[string]func(i int, l string) string{
		"raw":        func(i int, l string) string { return l },
		"read-tab":   func(i int, l string) string { return fmt.Sprintf("%6d\t%s", i+1, l) },
		"read-arrow": func(i int, l string) string { return fmt.Sprintf("%d→%s", i+1, l) },
		"grep-n":     func(i int, l string) string { return fmt.Sprintf("id_ed25519:%d:%s", i+1, l) },
		"diff-plus":  func(i int, l string) string { return "+" + l },
		"indented-2": func(i int, l string) string { return "  " + l },
		"crlf":       func(i int, l string) string { return l + "\r" },
	}
	names := []string{"raw", "read-tab", "read-arrow", "grep-n", "diff-plus", "indented-2", "crlf"}
	for _, n := range names {
		var b strings.Builder
		for i, l := range lines {
			b.WriteString(shapes[n](i, l) + "\n")
		}
		out, hits := r.String(b.String())
		surv := 0
		for _, seg := range []string{body[:70], body[70:140], body[140:210]} {
			if strings.Contains(out, seg) {
				surv++
			}
		}
		if surv != 0 || len(hits) == 0 {
			t.Errorf("%s: %d/3 body lines survived (hits %d)", n, surv, len(hits))
		}
	}
}
