package api

import (
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
)

// TestEveryAcceptedDisplayNameReachesTheAuditLineUnchanged pins the RELATIONSHIP between two rules
// in two packages: `control`'s display-name alphabet and this package's `auditField`. The alphabet
// exists so a display name is never rewritten in `identity=` — a rewrite (`?` for non-ASCII, `_` for
// a space, truncation past the cap) is how two distinct names would render as one. So every name the
// control plane ACCEPTS must pass through `auditField` byte-for-byte.
//
// It walks every single byte and a maximal-length name, so widening the alphabet to a character the
// audit field rewrites — or lifting the cap past the field's — goes red here, in the package that
// rewrites, rather than as a silent attribution collision.
func TestEveryAcceptedDisplayNameReachesTheAuditLineUnchanged(t *testing.T) {
	accepted := 0
	for b := 0; b < 256; b++ {
		for _, name := range []string{string([]byte{byte(b)}), "a" + string([]byte{byte(b)})} {
			if control.ValidUserDisplayName(name) != nil {
				continue
			}
			accepted++
			if got := auditField(name, authz.MaxIdentityChars); got != name {
				t.Errorf("display name %q is ACCEPTED by control and rendered %q in identity=", name, got)
			}
		}
	}
	longest := "a" + strings.Repeat("-", control.UserDisplayNameMax-1)
	if control.ValidUserDisplayName(longest) != nil {
		t.Fatalf("PRECONDITION: a %d-character name is refused", len(longest))
	}
	if got := auditField(longest, authz.MaxIdentityChars); got != longest {
		t.Errorf("the longest accepted display name renders %q in identity=, want it unchanged", got)
	}
	// The positive control: the walk admitted the alphabet's 62 alnum first characters plus the
	// three punctuation marks as second characters. Zero would mean it measured nothing.
	if accepted < 62 {
		t.Fatalf("only %d single-byte names were accepted — the walk is not reaching the alphabet", accepted)
	}
	t.Logf("%d accepted short names, each unchanged by auditField", accepted)
}
