package main

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 🔴 DETERMINISTIC MEANS EXPLICIT *AND LISTED* (decision 14, R5). ClickUp's task-id charset and length
// were not found in a primary source, so NOTHING HERE MATCHES A TOKEN BECAUSE IT LOOKS LIKE AN ID —
// and nothing is an edge unless the id is a MEMBER of the ticket list this run fetched. ONE rule for
// every source:
//
//   - a ClickUp task URL, in either form: `https://app.clickup.com/t/<id>` whose id the list holds, or
//     `https://app.clickup.com/t/<workspace>/<custom-id>` whose custom id the list holds;
//   - an explicit `clickup:<id>` ref on an entry the session wrote (sources.go), the id or custom id
//     being one the list holds;
//   - a CUSTOM id the list holds, standing as a whole token;
//   - in a STRUCTURED field only — a branch name, a commit trailer's value — a whole token that is a
//     canonical id the list holds.
//
// Free prose (a commit message, a PR body, the transcript) is searched for URLs and listed custom ids
// only; the transcript for URLs only, because `transcript-url` is the one signal it has.
//
// 🔴 WHY EVEN A URL NEEDS THE LIST (#274's audit; an earlier revision let a `/t/<id>` URL vouch for
// itself). A URL is text like any other: a tool truncates it (`…/t/clk00`), a human mistypes it, a
// page prints `/t/<workspace>` with nothing after. Each of those is a well-formed URL naming a
// ticket that does not exist — and an edge to it is a link on the session page to nowhere, or to
// somebody else's task. Membership is the only check that cannot be fooled by a shape.
//
// 🔴 THE EDGE ALWAYS CARRIES THE CANONICAL ID: a custom id is resolved through the list, the canonical
// id is what is stored and linked, and an id the list does not hold produces NO edge.

// Hit is one ticket a text names.
type Hit struct {
	// ID is the canonical id.
	ID string
	// Custom is the custom id the text spelled, "" when it named the canonical id.
	Custom string
}

// taskURL is a ClickUp task permalink. The first segment is the canonical id's charset; the optional
// second is a custom id (when the first is a workspace id).
var taskURL = regexp.MustCompile(`https?://app\.clickup\.com/t/([A-Za-z0-9]+)(?:/([A-Za-z0-9][A-Za-z0-9_-]*))?`)

// urlMarker is the cheap test a string must pass before the pattern runs.
const urlMarker = "app.clickup.com/t/"

// Matcher is the fetched list plus the instance's workspace id.
type Matcher struct {
	Index *Index
	// Team is the workspace id a `/t/<workspace>/<custom>` URL must name; "" accepts any workspace
	// (the fetched list is still the closed set a custom id must be in).
	Team string
}

// URLs is every LISTED ticket a text names by URL.
func (m *Matcher) URLs(text string) []Hit {
	if !strings.Contains(text, urlMarker) {
		return nil
	}
	var out []Hit
	for _, loc := range taskURL.FindAllStringSubmatchIndex(text, -1) {
		first := text[loc[2]:loc[3]]
		if loc[4] >= 0 && allDigits.MatchString(first) {
			// `/t/<workspace>/<custom-id>`.
			if m.Team != "" && first != m.Team {
				continue
			}
			// A custom id a tool cut short (`…/ALPHA-1...`) is not the shorter listed id it now spells.
			if continues(text[loc[5]:]) {
				continue
			}
			// A trailing `-`/`_` is punctuation around the URL, not part of an id the list could hold.
			custom := text[loc[4]:loc[5]]
			t, ok := m.Index.Resolve(custom)
			if !ok {
				custom = strings.TrimRight(custom, "-_")
				t, ok = m.Index.Resolve(custom)
			}
			if ok {
				out = append(out, Hit{ID: t.ID, Custom: custom})
			}
			continue
		}
		// `/t/<id>`. The id must END there (`…/t/abc123-draft` addresses no task `abc123`) — and it
		// must be a LISTED ticket: the workspace's own id, an id a tool cut short and any other id the
		// list does not hold are no ticket of this workspace's lists, whatever the URL looks like.
		if continues(text[loc[3]:]) || first == m.Team || !m.Index.Has(first) {
			continue
		}
		out = append(out, Hit{ID: first})
	}
	return out
}

// wordRune reports whether r is a letter or a digit in ANY script: the boundary test is not ASCII's.
func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// boundedLeft reports whether a token starting at text[i] starts a token of its own: not preceded by a
// letter or digit, and not by a `-`/`_` that itself follows one (`PRE-ALPHA-12` names no `ALPHA-12`).
func boundedLeft(text string, i int) bool {
	prev, size := utf8.DecodeLastRuneInString(text[:i])
	if size == 0 {
		return true
	}
	if wordRune(prev) {
		return false
	}
	if prev == '-' || prev == '_' {
		before, n := utf8.DecodeLastRuneInString(text[:i-size])
		return n == 0 || !wordRune(before)
	}
	return true
}

// boundedRight reports whether a token ending before text[end] ends there: not followed by a letter
// or digit, and not by a `-`, `_` or `.` that a DIGIT follows (`ALPHA-12-13` and `ALPHA-12.5` are other
// ids; `ALPHA-12-fix` and `ALPHA-12.` are this one).
func boundedRight(text string, end int) bool {
	next, size := utf8.DecodeRuneInString(text[end:])
	if size == 0 {
		return true
	}
	if wordRune(next) {
		return false
	}
	if next == '-' || next == '_' || next == '.' {
		after, n := utf8.DecodeRuneInString(text[end+size:])
		return n == 0 || !unicode.IsDigit(after)
	}
	return true
}

// Customs is every listed custom id standing in text as a whole token ([boundedLeft],
// [boundedRight]): `ALPHA-12` is in `feature/ALPHA-12-fix`, and in none of `ALPHA-123`,
// `PRE-ALPHA-12`, `ALPHA-12-13` and `ALPHA-12.5`.
func (m *Matcher) Customs(text string) []Hit {
	if len(m.Index.byFirst) == 0 {
		return nil
	}
	var out []Hit
	for i := 0; i < len(text); {
		if !isAlnum(text[i]) {
			i++
			continue
		}
		j := i
		for j < len(text) && isAlnum(text[j]) {
			j++
		}
		if boundedLeft(text, i) {
			for _, custom := range m.Index.byFirst[text[i:j]] {
				end := i + len(custom)
				if !strings.HasPrefix(text[i:], custom) || !boundedRight(text, end) {
					continue
				}
				t, _ := m.Index.Resolve(custom)
				out = append(out, Hit{ID: t.ID, Custom: custom})
				break // the longest listed id at this position wins
			}
		}
		i = j
	}
	return out
}

// Tokens is every whole alphanumeric token of a STRUCTURED field that is a listed canonical id.
func (m *Matcher) Tokens(text string) []Hit {
	var out []Hit
	for i := 0; i < len(text); {
		if !isAlnum(text[i]) {
			i++
			continue
		}
		j := i
		for j < len(text) && isAlnum(text[j]) {
			j++
		}
		// The run is ASCII; its neighbours may not be (`éclk0000a1` is one word, not a ticket).
		prev, _ := utf8.DecodeLastRuneInString(text[:i])
		next, _ := utf8.DecodeRuneInString(text[j:])
		if tok := text[i:j]; m.Index.Has(tok) && !(i > 0 && wordRune(prev)) && !(j < len(text) && wordRune(next)) {
			out = append(out, Hit{ID: tok})
		}
		i = j
	}
	return out
}

// continues reports whether the text right after a URL's id shows the id did not end there.
func continues(rest string) bool {
	switch {
	case rest == "":
		return false
	case rest[0] == '-', rest[0] == '%':
		return true
	case rest[0] == '_':
		// `_` then more id-like text continues it; a lone `_` is markup around the URL.
		return len(rest) > 1 && isAlnum(rest[1])
	}
	return strings.HasPrefix(rest, "..") || strings.HasPrefix(rest, "…")
}

// withoutURLs is text with every task-URL-shaped span blanked.
//
// 🔴 A URL IS JUDGED ONCE, AS A URL. Without this the bare-token matchers re-read the inside of a URL
// the URL matcher had just REFUSED — another workspace's `/t/<workspace>/ALPHA-12` still held the
// listed token `ALPHA-12` — and wrote the edge the refusal existed to prevent.
func withoutURLs(text string) string {
	if !strings.Contains(text, urlMarker) {
		return text
	}
	out := []byte(text)
	for _, loc := range taskURL.FindAllStringIndex(text, -1) {
		// Through the END of the URL as written, not just the part the pattern read: the rest of
		// `…/t/9000002/x/ALPHA-12` is still that URL.
		end := loc[1]
		for end < len(text) && !strings.ContainsRune(" \t\r\n\"'<>()[]{}`", rune(text[end])) {
			end++
		}
		for i := loc[0]; i < end; i++ {
			out[i] = ' '
		}
	}
	return string(out)
}

// Prose is what free text names: URLs and listed custom ids.
func (m *Matcher) Prose(text string) []Hit {
	return append(m.URLs(text), m.Customs(withoutURLs(text))...)
}

// Field is what a structured field names: everything prose does, plus a bare listed id.
func (m *Matcher) Field(text string) []Hit {
	return append(m.Prose(text), m.Tokens(withoutURLs(text))...)
}
