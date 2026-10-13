package main

import (
	"regexp"
	"strings"
)

// 🔴 DETERMINISTIC MEANS EXPLICIT (decision 14, R5). ClickUp's task-id charset and length were not
// found in a primary source, so NOTHING HERE MATCHES A TOKEN BECAUSE IT LOOKS LIKE AN ID. A ticket is
// named by exactly these, and by nothing else:
//
//   - a ClickUp task URL, in either form: `https://app.clickup.com/t/<id>` (the id IS the canonical
//     id) or `https://app.clickup.com/t/<workspace>/<custom-id>` (resolved through the fetched list);
//   - an explicit `clickup:<id>` ref on an entry the session wrote (sources.go);
//   - a CUSTOM id that is a MEMBER of the workspace's own fetched list, standing as a whole token;
//   - in a STRUCTURED field only — a branch name, a commit trailer's value — a whole token that is a
//     fetched CANONICAL id, or one this same session already named by URL.
//
// Free prose (a commit message, a PR body, the transcript) is searched for URLs and listed custom ids
// only; the transcript for URLs only, because `transcript-url` is the one signal it has.
//
// 🔴 THE EDGE ALWAYS CARRIES THE CANONICAL ID: a custom id is resolved through the list, the canonical
// id is what is stored and linked, and a custom id the list does not hold produces NO edge.

// Hit is one ticket a text names.
type Hit struct {
	// ID is the canonical id.
	ID string
	// Custom is the custom id the text spelled, "" when it named the canonical id.
	Custom string
	// ByURL reports whether a URL named it — what lets a bare token elsewhere in the session count.
	ByURL bool
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

// URLs is every ticket a text names by URL.
func (m *Matcher) URLs(text string) []Hit {
	if !strings.Contains(text, urlMarker) {
		return nil
	}
	var out []Hit
	for _, loc := range taskURL.FindAllStringSubmatchIndex(text, -1) {
		first := text[loc[2]:loc[3]]
		if loc[4] >= 0 && allDigits.MatchString(first) {
			// `/t/<workspace>/<custom-id>`. A trailing `-`/`_` is punctuation around the URL, not part
			// of an id the list could hold.
			if m.Team != "" && first != m.Team {
				continue
			}
			// A custom id a tool cut short (`…/ALPHA-1...`) is not the shorter listed id it now spells.
			if continues(text[loc[5]:]) {
				continue
			}
			custom := text[loc[4]:loc[5]]
			t, ok := m.Index.Resolve(custom)
			if !ok {
				t, ok = m.Index.Resolve(strings.TrimRight(custom, "-_"))
				custom = strings.TrimRight(custom, "-_")
			}
			if ok {
				out = append(out, Hit{ID: t.ID, Custom: custom, ByURL: true})
			}
			// 🔴 NO FALLBACK to reading the workspace id as a ticket: an unresolved custom id is no edge.
			continue
		}
		// `/t/<id>`. The id must END there: `…/t/abc123-draft` addresses no task `abc123`, and neither
		// does an id a tool cut short (`…/t/abc...`).
		if continues(text[loc[3]:]) {
			continue
		}
		// 🔴 ALL DIGITS AND THEN A `/`: that is the WORKSPACE form with a custom id this pattern could
		// not read (`/t/9000001/`, `/t/9000001/{custom_id}`). The workspace id is never a ticket.
		if allDigits.MatchString(first) && loc[3] < len(text) && text[loc[3]] == '/' {
			continue
		}
		if canonicalID.MatchString(first) {
			out = append(out, Hit{ID: first, ByURL: true})
		}
	}
	return out
}

// Customs is every listed custom id standing in text as a whole token: not preceded or followed by a
// letter or digit (`ALPHA-12` is not in `ALPHA-123`, and is in `feature/ALPHA-12-fix`).
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
		// text[i:j] is a maximal alphanumeric run, so it starts on a token boundary.
		for _, custom := range m.Index.byFirst[text[i:j]] {
			end := i + len(custom)
			if !strings.HasPrefix(text[i:], custom) || (end < len(text) && isAlnum(text[end])) {
				continue
			}
			t, _ := m.Index.Resolve(custom)
			out = append(out, Hit{ID: t.ID, Custom: custom})
			break // the longest listed id at this position wins
		}
		i = j
	}
	return out
}

// Tokens is every whole alphanumeric token of a STRUCTURED field that is a fetched canonical id, or
// one of `also` — the ids this session already named by URL.
func (m *Matcher) Tokens(text string, also map[string]bool) []Hit {
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
		if tok := text[i:j]; m.Index.Has(tok) || also[tok] {
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

// Field is what a structured field names: everything prose does, plus a bare fetched id.
func (m *Matcher) Field(text string, also map[string]bool) []Hit {
	return append(m.Prose(text), m.Tokens(withoutURLs(text), also)...)
}
