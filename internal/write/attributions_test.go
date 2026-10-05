package write

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// attributionShapes is the per-shape contract `ParseAttributions` documents, one row
// per behaviour, with LITERAL expectations (never derived from the parser).
var attributionShapes = []struct {
	name      string
	lines     []string
	pieces    []Attribution
	malformed bool
}{
	{"absent", []string{"- 2000-01-02: plain prose"}, nil, false},
	{"one-trailer", []string{"- 2000-01-02: prose [cairn: alpha-bot/s-0001]"},
		[]Attribution{{"alpha-bot", "s-0001"}}, false},
	{"uuid-session", []string{"- prose [cairn: alpha-bot/0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0]"},
		[]Attribution{{"alpha-bot", "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0"}}, false},
	{"non-uuid-session", []string{"- prose [cairn: beta-bot/ses_example0000000000000000001]"},
		[]Attribution{{"beta-bot", "ses_example0000000000000000001"}}, false},
	{"after-citation-token", []string{"- prose [cairn: alpha-bot/s-0002] [cb:0a1b2c3d]"},
		[]Attribution{{"alpha-bot", "s-0002"}}, false},
	{"before-citation-token", []string{"- prose [cb:0a1b2c3d] [cairn: alpha-bot/s-0003]"},
		[]Attribution{{"alpha-bot", "s-0003"}}, false},
	{"two-in-a-run", []string{"- prose [cairn: alpha-bot/s-0004] [cairn: gamma-bot/s-0005]"},
		[]Attribution{{"alpha-bot", "s-0004"}, {"gamma-bot", "s-0005"}}, false},
	{"mid-prose-quote", []string{"- write `[cairn: alpha-bot/s-0006]` to attribute it"}, nil, false},
	{"empty-actor", []string{"- prose [cairn: /s-0007]"}, nil, true},
	{"empty-session", []string{"- prose [cairn: alpha-bot/]"}, nil, true},
	{"email-actor", []string{"- prose [cairn: someone@example.invalid/s-0008]"}, nil, true},
	{"uppercase-actor", []string{"- prose [cairn: Alpha/s-0009]"}, nil, true},
	{"placeholder", []string{"- prose [cairn: <actor>/<session>]"}, nil, true},
	{"malformed-before-valid", []string{"- prose [cairn: BAD/x] [cairn: alpha-bot/s-0010]"},
		[]Attribution{{"alpha-bot", "s-0010"}}, true},
	{"valid-before-malformed", []string{"- prose [cairn: alpha-bot/s-0011] [cairn: BAD/x]"}, nil, true},
	{"malformed-mid-prose", []string{"- prose [cairn: BAD/x] more prose"}, nil, false},
	{"no-space-after-colon", []string{"- prose [cairn:alpha-bot/s-0018]"}, nil, true},
	{"unclosed",[]string{"- prose [cairn: alpha-bot/s-0012"}, nil, false},
	{"non-final-line", []string{"- 2000-01-02: prose [cairn: alpha-bot/s-0013]", "  added later"}, nil, false},
	{"final-continuation-line", []string{"- 2000-01-02: wrapped", "  prose [cairn: alpha-bot/s-0014]"},
		[]Attribution{{"alpha-bot", "s-0014"}}, false},
	{"crlf-residue", []string{"- prose [cairn: alpha-bot/s-0015]\r"},
		[]Attribution{{"alpha-bot", "s-0015"}}, false},
	{"tab-separated", []string{"- prose\t[cairn: alpha-bot/s-0016]"},
		[]Attribution{{"alpha-bot", "s-0016"}}, false},
	{"trailer-only", []string{"- 2000-01-02: [cairn: alpha-bot/s-0017]"},
		[]Attribution{{"alpha-bot", "s-0017"}}, false},
	{"session-too-long", []string{"- prose [cairn: alpha-bot/" + strings.Repeat("a", 65) + "]"}, nil, true},
	{"session-at-cap", []string{"- prose [cairn: alpha-bot/" + strings.Repeat("b", 64) + "]"},
		[]Attribution{{"alpha-bot", strings.Repeat("b", 64)}}, false},
}

func TestParseAttributionsPerShape(t *testing.T) {
	for _, c := range attributionShapes {
		got := ParseAttributions(c.lines)
		if !reflect.DeepEqual(got.Pieces, c.pieces) || got.Malformed != c.malformed {
			t.Errorf("%s: got pieces=%v malformed=%v, want pieces=%v malformed=%v",
				c.name, got.Pieces, got.Malformed, c.pieces, c.malformed)
		}
	}
}

// independentCitationRe is the citation token spelled by the TEST, not borrowed from
// the package, so the tiling check below does not share the package's regexps.
var independentCitationRe = regexp.MustCompile(`^\[cb:[0-9a-f]{8}\]`)

// tileRun checks that `run` is EXACTLY the returned attributions — each re-rendered
// through the PRODUCER's own `attributionFormat` — interleaved with citation tokens
// and blanks, in order, with nothing left over. It is an independent reader of the
// run: it never consults `bulletTrailersRe` or `attributionPieceRe`.
func tileRun(run string, pieces []Attribution) error {
	i, pos := 0, 0
	for pos < len(run) {
		if run[pos] == ' ' || run[pos] == '\t' {
			pos++
			continue
		}
		if i < len(pieces) {
			want := strings.TrimPrefix(fmt.Sprintf(attributionFormat, pieces[i].Actor, pieces[i].Session), " ")
			if strings.HasPrefix(run[pos:], want) {
				pos += len(want)
				i++
				continue
			}
		}
		if m := independentCitationRe.FindString(run[pos:]); m != "" {
			pos += len(m)
			continue
		}
		return fmt.Errorf("byte %d of the stripped run %q is not a returned attribution or a citation token", pos, run)
	}
	if i != len(pieces) {
		return fmt.Errorf("returned %d attributions, only %d appear in the stripped run %q", len(pieces), i, run)
	}
	return nil
}

// agreementCorpus is every shape row, plus a generated cross product: prose prefixes
// × suffix sequences of length 0..3 over attribution, citation and malformed pieces.
func agreementCorpus() [][]string {
	var out [][]string
	for _, c := range attributionShapes {
		out = append(out, c.lines)
	}
	prefixes := []string{"- prose", "- 2000-01-02: prose", "- OPEN: prose", "- `[cairn: q-bot/s-9]` quoted", "-"}
	pieces := []string{
		"[cairn: alpha-bot/s-0001]", "[cairn: b/ses_x.y-z]", "[cairn: 9/0]",
		"[cb:0a1b2c3d]", "[cb:ffffffff]",
		"[cairn: /x]", "[cairn: A/x]", "[cairn: a/]", "[cb:XYZ]", "[cairn: a/b c]", "trailing-word",
	}
	seps := []string{" ", "\t", "  "}
	var walk func(prefix string, depth int)
	walk = func(prefix string, depth int) {
		out = append(out, []string{prefix})
		if depth == 3 {
			return
		}
		for pi, p := range pieces {
			walk(prefix+seps[(pi+depth)%len(seps)]+p, depth+1)
		}
	}
	for _, pre := range prefixes {
		walk(pre, 0)
	}
	// Wrapped forms: the trailer on a non-final and on the final line.
	out = append(out,
		[]string{"- wrapped", "  [cairn: alpha-bot/s-0001]"},
		[]string{"- wrapped [cairn: alpha-bot/s-0001]", "  tail"},
		[]string{"- wrapped [cairn: alpha-bot/s-0001]", "", "  [cb:0a1b2c3d]"},
	)
	return out
}

// TestTheParserAndTheStripAgree pins the seam: over one corpus, the attributions
// `ParseAttributions` returns are EXACTLY the attribution pieces `stripBulletTrailers`
// removes — none extra (a parser reading beyond the run), none missing (a parser
// narrower than the grammar), in order.
func TestTheParserAndTheStripAgree(t *testing.T) {
	corpus := agreementCorpus()
	withPieces := 0
	for _, lines := range corpus {
		s := collapseBullet(lines)
		run := s[len(stripBulletTrailers(s)):]
		got := ParseAttributions(lines)
		if len(got.Pieces) > 0 {
			withPieces++
		}
		if err := tileRun(run, got.Pieces); err != nil {
			t.Errorf("%q: %v", lines, err)
		}
	}
	// Positive control: the corpus must actually exercise attributions, or the
	// agreement above is vacuous.
	if withPieces < 100 {
		t.Fatalf("only %d of %d corpus rows carried a parsed attribution — the corpus is not exercising the parser", withPieces, len(corpus))
	}
	t.Logf("corpus: %d rows, %d with ≥1 parsed attribution", len(corpus), withPieces)
}

// TestRenderedBulletsParseBack is the producer ⇄ parser round trip: whatever
// `RenderBullet` writes with a grammatical actor and session reads back as exactly
// that pair.
func TestRenderedBulletsParseBack(t *testing.T) {
	for _, sess := range []string{"s-0001", "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0", "ses_example0000000000000000001", "x"} {
		line := RenderBullet("a note [cb:0a1b2c3d]", "delta-bot", sess, "2000-01-05")
		got := ParseAttributions([]string{line})
		want := []Attribution{{"delta-bot", sess}}
		if !reflect.DeepEqual(got.Pieces, want) || got.Malformed {
			t.Errorf("%q: got %+v, want %v", line, got, want)
		}
	}
}
