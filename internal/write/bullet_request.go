package write

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// SessionComponent validates the caller-supplied session id.
//
// 🔴 A SESSION ID IS CORRELATION DATA, NOT AN IDENTITY CLAIM, so unlike the actor it
// IS caller-supplied — and it is therefore validated as hostile input before it is
// written into a curated file. The class is narrower than a token and wider than an
// identity: agent session ids are uuids and short hex handles. A full match on this
// class is what stops a newline, a markdown control character or a `]` from breaking
// the attribution trailer it is written into.
var SessionComponent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// SessionComponentPattern is the class as a refusal message quotes it, derived from
// the compiled expression so the message cannot describe a class the code does not
// enforce.
var SessionComponentPattern = strings.TrimSuffix(strings.TrimPrefix(SessionComponent.String(), "^"), "$")

// forbiddenCategories are the Unicode general categories a bullet's text may not
// contain, and EACH WAS MEASURED LANDING IN THE CURATED FILE AT `200 appended` on the
// Python side:
//
//   - `Cc`  U+0000 makes text tools treat the entry as BINARY, so the file silently
//     drops out of every one of them; an ESC sequence rewrites the terminal of
//     whoever renders a recall digest.
//   - `Cf`  U+202E and the isolates reorder the rendered line, so what an operator
//     reads is not what is stored — visual spoofing of a curated record; U+200B is
//     invisible AND defeats idempotency, because two bullets that read identically
//     hash differently and a retry double-records.
//   - `Cs`  lone surrogates. See the note on the scan below.
//   - `Co`  private use.
//
// ONE CATEGORY TEST rather than a list of characters, because a list is walkable by
// the next character nobody thought of — the exact failure a two-character newline
// check already demonstrated on this code.
//
// `Cn` (unassigned) is NOT included: it would refuse whatever the running Unicode
// tables have not caught up with, which is a refusal that changes with the runtime.
//
// ⚠ `\t` IS `Cc` AND IS THEREFORE REFUSED. Decided, not incidental: a bullet is ONE
// line of prose, the content hash collapses runs of whitespace anyway, and a named
// 400 is better than a tab that renders differently everywhere.
var forbiddenCategories = map[string]*unicode.RangeTable{
	"Cc": unicode.Cc,
	"Cf": unicode.Cf,
	"Cs": unicode.Cs,
	"Co": unicode.Co,
}

// loneSurrogateEscape matches a `\uD800`-`\uDFFF` JSON escape.
var loneSurrogateEscape = regexp.MustCompile(`(?i)\\u(d[89ab][0-9a-f]{2}|d[c-f][0-9a-f]{2})`)

// DecodeBulletBody decodes an append request body.
//
// It returns the decoded value — which may be any JSON type, because "the body is
// not an object" is a DIFFERENT refusal from "the body is not JSON" and the two must
// not be collapsed.
//
// ⚠ A DEEPLY NESTED BODY IS A REFUSAL, NOT A CRASH, AND THAT IS WHY THIS FUNCTION
// EXISTS RATHER THAN AN INLINE `json.Unmarshal`. On the Python side a 400 KB body of
// `[[[[…]]]]` blew the interpreter's recursion limit INSIDE the parser and raised an
// error type the handler's `except` did not name, so the caller got a dropped
// connection with no response, no status header and — the part that matters — NO
// AUDIT LINE, on a request that had already been metered. Go's decoder returns an
// error for excessive nesting instead of unwinding the stack, so the shape is
// answered rather than survived; the guard is named here so the property is
// attributable rather than inherited.
func DecodeBulletBody(body []byte) (any, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// BulletRequestProblem validates an append request. It returns the sentence to
// refuse with, or "" when the request is acceptable.
//
// 🔴 ONE FUNCTION SO EVERY REFUSAL IS IN ONE PLACE AND NAMES ITSELF. Each clause is
// reachable by an input every earlier clause accepts, which is the same ladder
// discipline the token loader's guards are built to.
//
// ⚠ `actor` IS NOT VALIDATED AND NOT REJECTED — it is simply never read. An `actor`
// key is accepted because a client library may send one and a 400 there would be a
// compatibility trap; it is DISCARDED because a client-supplied actor lets any
// token-holder attribute a bullet to somebody else, and an attribution nobody can
// trust is worse than none.
//
// 🔴 THE CHARACTER CLAUSES ARE A PREDICATE, NOT A DENYLIST. The line-break clause
// asks the line splitter itself how many lines the text becomes, and the
// control/format clause asks for a CATEGORY — because the two-character
// `"\n" in text or "\r" in text` this replaced was walked by eight other characters
// the splitter splits on, one of which produced a stored bullet with NO attribution
// trailer and a second one whose trailer reads as somebody else's.
//
// `raw` is the request body as it arrived, and it is inspected for exactly one thing
// the decoded value cannot carry — see the surrogate clause.
func BulletRequestProblem(raw []byte, payload any) string {
	// 🔴 A GO-SIDE GUARD WITH NO PYTHON COUNTERPART, AND THE DIVERGENCE IS
	// DELIBERATE AND IN THE STRICT DIRECTION. CPython's JSON decoder yields a LONE
	// SURROGATE for a `\udc80`-style escape, so the `Cs` clause below refuses it
	// there. Go's decoder replaces an unpaired surrogate escape with U+FFFD, whose
	// category is `So` — so the `Cs` clause is UNREACHABLE here and the bullet would
	// be stored carrying a replacement character the Python server refuses. Scanning
	// the RAW body closes that, and it closes it slightly WIDER than Python: an
	// unpaired escape in a key this server ignores (`actor`) is a 400 here and a 200
	// there. No case in the conformance corpus sends one; the direction is the safe
	// one; and `Cs` stays in the category table above because the PREDICATE is the
	// rule, and a future decoder that preserves surrogates must not silently open
	// the hole this scan is covering.
	if match := loneSurrogateEscape.FindString(string(raw)); match != "" {
		return fmt.Sprintf(
			"the body carries the unpaired surrogate escape `%s`, which is not text that can be written into a curated entry",
			match)
	}
	object, isObject := payload.(map[string]any)
	if !isObject {
		return "the body must be a JSON object"
	}
	text, textIsString := object["text"].(string)
	if !textIsString || pytext.StripWhitespace(text) == "" {
		return "`text` is required and must be a non-empty string"
	}
	if runes := len([]rune(text)); runes > BulletTextMax {
		// 🔴 THE OVERAGE IS THE ONLY NUMBER THE CALLER CAN ACT ON, and it is the one
		// the message used to omit. Two absolute figures make a caller subtract on
		// every retry, and a caller who is over by 1,210 has to rewrite rather than
		// trim — which is a different decision, taken from a number they had to
		// compute themselves. Naming the excess turns the refusal into an instruction.
		return fmt.Sprintf("`text` is %d characters, max %d — %d over",
			runes, BulletTextMax, runes-BulletTextMax)
	}
	if len(pytext.SplitLines(text)) > 1 || pytext.TrimLineBreaks(text) != text {
		// 🔴 ASKING THE SPLITTER IS THE HONEST PREDICATE — it cannot fall behind the
		// ten characters that function splits on. The second clause covers a LEADING
		// or TRAILING break, which the splitter folds away and which would otherwise
		// open or close the bullet with an empty line.
		return "`text` must be ONE line — an embedded newline would be attached to " +
			"this bullet as a continuation, or would start a second, unattributed bullet"
	}
	for _, r := range text {
		if name, category := categoryOf(r); category != nil {
			// Named by CODE POINT, because the offending character is by definition
			// one the caller cannot see in their own error message.
			return fmt.Sprintf(
				"`text` contains U+%04X (%s), a control or formatting character that must not be written into a curated entry",
				r, name)
		}
	}
	lstripped := strings.TrimLeftFunc(text, func(r rune) bool {
		return pytext.StripWhitespace(string(r)) == ""
	})
	if strings.HasPrefix(lstripped, "- ") || strings.HasPrefix(lstripped, "* ") {
		return "`text` must not open a markdown bullet — the `- ` is added here, and " +
			"a second one would start a bullet with no attribution trailer"
	}
	session, sessionIsString := object["session"].(string)
	if !sessionIsString || !SessionComponent.MatchString(session) {
		return fmt.Sprintf(
			"`session` is required and must match %s — every appended bullet records the actor AND the session that wrote it",
			SessionComponentPattern)
	}
	return ""
}

// categoryOf reports the forbidden category a rune belongs to, or a nil table when
// it belongs to none. The NAME is returned because the refusal quotes it, and Go's
// unicode package has tables but no name lookup — so the pairing lives beside the
// tables rather than in a second map somewhere else.
func categoryOf(r rune) (string, *unicode.RangeTable) {
	// Iterated in a fixed order so the reported category is deterministic. The four
	// tables are disjoint, so the order cannot change the ANSWER — only the
	// determinism of a future non-disjoint addition.
	for _, name := range []string{"Cc", "Cf", "Cs", "Co"} {
		table := forbiddenCategories[name]
		if unicode.Is(table, r) {
			return name, table
		}
	}
	return "", nil
}

// BulletText and BulletSession read the two fields the handler passes on, after
// BulletRequestProblem has accepted the payload. They exist so the handler does not
// re-assert the types the validator already checked — a second type assertion is a
// second place to get the key name wrong.
func BulletText(payload any) string {
	text, _ := payload.(map[string]any)["text"].(string)
	return text
}

func BulletSession(payload any) string {
	session, _ := payload.(map[string]any)["session"].(string)
	return session
}
