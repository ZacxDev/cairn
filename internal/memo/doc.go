// Package memo is scope mail's TRUST BOUNDARY: the two code-point predicates, the one type
// stored memo text travels in, and the one renderer that turns it into the fenced block an
// agent reads. S0 of `claudedocs/plan-cairn-scope-mail.md` (decisions 5 and 19). Nothing calls
// it yet: storage (S1), the listener (S2) and the verbs (S3) are later slices.
//
// 🔴 MEMO TEXT IS THE FIRST STORE-ORIGINATED TEXT AUTO-INJECTED INTO AN AGENT'S CONTEXT, so
// this package states what it guarantees at the scope its tests check, and no wider.
//
// TWO PREDICATES, because the two sides protect different things (decision 5):
//
//   - [BreaksFence] is the SEND-side refusal, deliberately NARROW: invalid UTF-8, every Cc
//     but `\n` and `\t` (after `\r\n` is folded to `\n`), the Bidi_Control property, Zl/Zp,
//     and the TAG block U+E0000–U+E007F. [SubjectBreaksFence] also refuses `\n` and `\t`.
//     Ordinary text — gofmt'd code, CRLF, ZWJ emoji, Persian with ZWNJ, a soft hyphen — is
//     accepted; RTL prose carrying LRM/RLM and subdivision flags are REFUSED, a stated cost
//     pinned by a test so nobody relaxes it silently.
//   - [Unsafe] is the RENDER-side predicate, deliberately WIDE: every C* code point (Cc, Cf,
//     Co, Cs and unassigned Cn), Zl/Zp, and the Variation_Selector property, which is Mn and
//     would otherwise pass. The renderer replaces each with U+FFFD, except `\n` (shown as
//     " ⏎ ") and `\t` (one space). So the store DOES hold Cf, private-use, unassigned and
//     variation-selector code points; the claim is about OUTPUT.
//
// ONE TYPE, [Stored], carries stored text, and it has NO exported field and NO method. Its text
// sits behind a FUNC, which `fmt` never calls or dereferences under any verb, so a Stored printed
// directly or nested at any depth — including inside an UNEXPORTED field, which `fmt` does print
// and which bypasses any String/GoString/Format a type defines — shows an address and never text
// (Guard B). ⚠ A plain pointer, which the plan adopted, is NOT enough: it is measured to leak
// through every verb that is invalid for a pointer (`%s`, `%q`, …); see [Stored]. The text
// leaves this package only through the names in `exportLedger` (memo_test.go), each of which
// applies [Unsafe]'s replacement first (Guard A): [Render], [RenderPreview], [RenderFull].
//
// THE FENCE IS STRUCTURAL, NOT SPELLED. Every content line of a block begins with "| ", so no
// content can occupy column 0, where the opening and closing markers live; the nonce is fresh per
// render; and the marker word and the current nonce are replaced inside content as well.
//
// OUT OF SCOPE, named so no sentence above is read wider than its guards (decision 19): code
// INSIDE this package; a deliberate `reflect` or `unsafe` read; reads of the table that bypass
// Go; text a person copies out of a rendered page. And a fence makes memo content LEGIBLE as
// data — it does not make a model incapable of following it (threat T1).
package memo
