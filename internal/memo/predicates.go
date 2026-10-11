package memo

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Unsafe reports whether the RENDERER must replace r: every code point in General Category C*
// — Cc, Cf, Co, Cs and Cn (unassigned) — plus Zl, Zp and the Variation_Selector property.
//
// It is computed as "in NONE of L, M, N, P, S, Z", which is exactly C* — Cn included — because
// every code point has exactly one General Category and C* is what the other six leave. It
// never consults Go's `unicode.C` table, whose Cn coverage is not documented. ⚠ An earlier draft computed Cn as "in no table,
// C included" and was measured WRONG: Go's `unicode.C` already holds unassigned code points
// (U+0378 and U+FFFE are in it), so that draft called every Cn assigned and let it through.
// Cn makes this a property of the TOOLCHAIN's tables (Unicode 15.0.0 for Go 1.25): a code
// point assigned in a later version is Cn here, and is shown as U+FFFD — fail-closed.
//
// The replacement character itself (U+FFFD, category So) is never Unsafe, which is what makes
// rendering already-rendered text a no-op (Guard C, round 5).
func Unsafe(r rune) bool {
	switch {
	case !unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Z):
		return true // C*: Cc, Cf, Co, Cs, Cn
	case unicode.In(r, unicode.Zl, unicode.Zp):
		return true
	case unicode.Is(unicode.Variation_Selector, r):
		return true
	}
	return false
}

// Normalize folds every "\r\n" to "\n". It is the form a SEND stores, and the form
// [BreaksFence] judges, so CRLF text is accepted and stored with "\n" (decision 5).
func Normalize(text string) string {
	return strings.ReplaceAll(text, "\r\n", "\n")
}

// BreaksFence is the SEND-side refusal for a memo BODY (decision 5): true when the text, after
// [Normalize], is not valid UTF-8 or holds a code point that can break the fence or reorder
// text — a Cc other than "\n" and "\t", a Bidi_Control code point, Zl or Zp, or the TAG block
// U+E0000–U+E007F.
//
// Cs (a surrogate) is covered by the validity check: Go's decoder treats the three-byte
// encoding of a surrogate as invalid UTF-8, so no valid string can carry one.
//
// ⚠ Stated costs, pinned by tests rather than hidden: RTL prose carrying LRM/RLM (U+200E/
// U+200F ARE Bidi_Control) and a subdivision flag (its tag characters are in the refused block)
// are refused. Whether LRM/RLM can be allowed without reopening a reordering attack is not
// established (plan, Q18).
func BreaksFence(text string) bool {
	text = Normalize(text)
	if !utf8.ValidString(text) {
		return true
	}
	for _, r := range text {
		if breaksFence(r) {
			return true
		}
	}
	return false
}

// SubjectBreaksFence is [BreaksFence] for a SUBJECT, which is one line: it ALSO refuses "\n"
// and "\t" (and therefore a "\r\n", which [Normalize] folds to "\n").
func SubjectBreaksFence(subject string) bool {
	if BreaksFence(subject) {
		return true
	}
	return strings.ContainsAny(Normalize(subject), "\n\t")
}

func breaksFence(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case unicode.Is(unicode.Cc, r):
		return true
	case unicode.Is(unicode.Bidi_Control, r):
		return true
	case unicode.In(r, unicode.Zl, unicode.Zp):
		return true
	case r >= 0xE0000 && r <= 0xE007F:
		return true
	}
	return false
}

// sanitize is the render-side replacement every field passes through before it leaves this
// package: each [Unsafe] code point becomes U+FFFD, except "\n" (" ⏎ ") and "\t" (" ").
// "\r\n" is folded first so a CRLF line break renders as ONE " ⏎ ". Invalid UTF-8 decodes to
// U+FFFD per invalid byte, which is already the visible replacement.
func sanitize(s string) string {
	s = Normalize(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(" ⏎ ")
		case r == '\t':
			b.WriteByte(' ')
		case Unsafe(r):
			b.WriteRune(utf8.RuneError)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
