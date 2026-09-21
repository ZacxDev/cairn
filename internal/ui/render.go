package ui

import (
	"strings"

	g "maragu.dev/gomponents"
	c "maragu.dev/gomponents/components"
	h "maragu.dev/gomponents/html"
)

// Page is the one page Phase A renders.
//
// 🔴 EVERY USER STRING GOES THROUGH `g.Text` OR THROUGH A QUOTED ATTRIBUTE VALUE,
// AND NOTHING GOES THROUGH `g.Raw`. See this package's doc comment for the measured
// scope of gomponents' escaper and for the one place it is NOT enough.
func Page(viewer string, scopes []Scope) g.Node {
	return c.HTML5(c.HTML5Props{
		Title:    "cairn",
		Language: "en",
		Head: []g.Node{
			h.StyleEl(g.Text(stylesheet)),
		},
		Body: []g.Node{
			h.Header(
				h.H1(g.Text("cairn")),
				// The viewer's display name is USER TEXT: it comes from a
				// `control.Principal`, which comes from a provisioned user record.
				h.P(h.Class("viewer"), g.Text("signed in as "+viewer)),
			),
			h.Main(
				g.If(len(scopes) == 0, h.P(h.Class("empty"), g.Text(
					"No scope is visible to this credential. That is an authority "+
						"answer, not an empty store."))),
				g.Map(scopes, scopeSection),
			),
		},
	})
}

func scopeSection(s Scope) g.Node {
	return h.Section(
		h.Class("scope"),
		h.H2(g.Text(s.Name)),
		h.Ul(g.Map(s.Entries, entryItem)),
	)
}

// entryItem renders ONE entry. This is the entry-content path the raw-node ban
// names, and every string it touches came out of a file somebody else wrote.
func entryItem(e Entry) g.Node {
	return h.Li(
		h.Class("entry"),
		// 🔴 THE REF IS IN AN ATTRIBUTE VALUE *AND* IN TEXT CONTENT, DELIBERATELY.
		// The two contexts escape through different code in gomponents —
		// `valueAttr` for the first, `Text` for the second — so a page that put user
		// text in only one of them would leave the other's escaping untested here.
		// A `title` attribute is the honest use as well: the ref is often wider than
		// the column.
		h.Span(h.Class("ref"), h.TitleAttr(e.Ref), g.Text(e.Ref)),
		h.Span(h.Class("title"), g.Text(e.Title)),
		g.If(len(e.Aliases) > 0, h.Ul(
			h.Class("aliases"),
			g.Map(e.Aliases, func(a string) g.Node {
				return h.Li(g.Text(a))
			}),
		)),
		g.If(len(e.Tasks) > 0, h.Ul(
			h.Class("tasks"),
			g.Map(e.Tasks, taskItem),
		)),
	)
}

// taskItem is the ONE href position on this page, and the only place [safeHref]
// is reached from.
//
// 🔴 A REF THAT DOES NOT PASS RENDERS AS TEXT RATHER THAN AS A DEAD LINK OR AS
// NOTHING. Dropping it would hide a fact the file carries; rendering `<a href="">`
// would put a clickable element on the page whose target the reader cannot see.
// Plain text is visible, inert, and says what the file says.
func taskItem(ref string) g.Node {
	href, ok := safeHref(ref)
	if !ok {
		return h.Li(h.Class("task refused"), g.Text(ref))
	}
	return h.Li(h.Class("task"), h.A(h.Href(href), g.Text(ref)))
}

// safeHref is the ONE place a URL reaches an href attribute, and it ALLOWLISTS.
//
// 🔴 A DENYLIST IS THE WRONG SHAPE HERE AND THAT IS NOT A STYLE PREFERENCE. The
// set of URL schemes a browser will execute is open — `javascript:`, `data:`,
// `vbscript:`, and whatever a handler registers — so a list of the ones to refuse
// is a list that is wrong the moment it is written. Two schemes are permitted, and
// a third requires an edit here.
//
// 🔴 THE SCHEME TEST IS CASE-INSENSITIVE AND WHITESPACE-STRIPPED BECAUSE BROWSERS
// ARE — AND WITH AN ALLOWLIST THAT PROTECTS THE *PERMIT* SIDE, WHICH IS THE OPPOSITE
// OF WHAT IT WOULD DO IN A DENYLIST. Measured, not reasoned: removing the
// `ToLower` makes `HTTPS://tracker.invalid/…` be REFUSED and changes nothing about
// `JaVaScRiPt:` — which an allowlist rejects at any casing, because it matches no
// permitted prefix however it is spelled. The same for the strip: removing it
// refuses `  https://…`. So neither is what stops the script-scheme attack; the
// allowlist is. What they buy is two real things: a correctly-spelled URL is not
// silently demoted to plain text, and the value written into the href is the
// STRIPPED one, so what the page shows and what the browser resolves are the same
// string. RFC 3986 §3.1 makes a scheme case-insensitive, and the HTML specification
// strips leading and trailing ASCII whitespace — including C0 controls — from a URL
// attribute before resolving it.
//
// ⚠ WHAT THIS DOES NOT CLAIM: it is not a URL validator. A permitted `https://`
// target may still be hostile — it may be a tracking pixel or a phishing page —
// and nothing here says otherwise. What it claims is narrower and is the property
// the escaper cannot supply: no value leaving this function can cause the BROWSER
// to execute the page author's string as code.
func safeHref(raw string) (string, bool) {
	// The HTML URL-attribute strip: ASCII whitespace and C0 controls at both ends.
	trimmed := strings.Trim(raw, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\t\n\v\f\r\x0e\x0f"+
		"\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f \x7f")
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	for _, scheme := range allowedSchemes {
		if strings.HasPrefix(lower, scheme) {
			return trimmed, true
		}
	}
	return "", false
}

// allowedSchemes is the whole allowlist. Lowercase, with the separator included so
// that a host literally named `https` cannot pass as a scheme.
//
// 🔴 A RELATIVE URL IS NOT ON THIS LIST, DELIBERATELY. A ref beginning `/` would be
// a same-origin path, which sounds safe and is the shape that lets a store entry
// address any route this binary serves — including ones a later phase adds. The UI
// builds its own internal links from the ledger, never from user text.
var allowedSchemes = []string{"http://", "https://"}

// stylesheet is a constant. It is NOT user text and could never be: it is written
// here, in this file, and no input reaches it — which is why `style-src
// 'unsafe-inline'` in the response's policy buys an attacker nothing.
const stylesheet = `
:root { color-scheme: light dark; }
body { font: 16px/1.5 system-ui, sans-serif; margin: 2rem auto; max-width: 48rem; }
.viewer { opacity: 0.7; }
.scope h2 { font-size: 1.1rem; border-bottom: 1px solid currentColor; }
.entry { margin-bottom: 0.75rem; }
.ref { font-family: ui-monospace, monospace; margin-right: 0.5rem; }
.task.refused { opacity: 0.6; text-decoration: line-through; }
`
