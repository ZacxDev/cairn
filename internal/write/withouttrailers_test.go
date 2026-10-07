package write

import "testing"

// TestWithoutTrailersRemovesOnlyTheEndAnchoredRun pins the display strip as literal values: the
// trailer run at the END goes (attribution and citation token, in either order), a `[cairn:` token
// mid-prose stays, and a string with no trailer comes back unchanged.
func TestWithoutTrailersRemovesOnlyTheEndAnchoredRun(t *testing.T) {
	for in, want := range map[string]string{
		"trimmed the wick [cairn: tallow-bot/s-1]":                  "trimmed the wick",
		"trimmed [cb:0a1b2c3d] [cairn: tallow-bot/s-1]":             "trimmed",
		"trimmed [cairn: tallow-bot/s-1] [cb:0a1b2c3d]":             "trimmed",
		"quoted [cairn: other-bot/s-9] in prose":                    "quoted [cairn: other-bot/s-9] in prose",
		"quoted [cairn: other-bot/s-9] in prose [cairn: a-bot/s-1]": "quoted [cairn: other-bot/s-9] in prose",
		"a refused one [cairn: Bad/s-1]":                            "a refused one [cairn: Bad/s-1]",
		"no trailer at all":                                         "no trailer at all",
	} {
		if got := WithoutTrailers(in); got != want {
			t.Errorf("WithoutTrailers(%q) = %q, want %q", in, got, want)
		}
	}
}
