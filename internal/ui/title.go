package ui

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// 🔴 THE ONE PLACE A PAGE'S `<title>` IS COMPOSED, AND THE ONE PLACE THE HEADER'S WORDMARK IS.
//
// Every frame — `shell`, `SignInPage`, `JoinPage` — passes its page LABEL ("arcs", a scope name,
// "sign in", "" for the root) to [documentTitle] and never a finished title, so an instance label
// cannot be missed by a page that spelled its own. `TestEveryFrameTitleIsComposedByDocumentTitle`
// reads the AST and refuses a `c.HTML5Props` whose `Title` is anything but a call to it, and the
// labelled route walk refuses a page whose title does not carry the label — the structural half
// and the behavioural half, because a structural check type-checks past a caller handing
// `documentTitle` an already-composed "cairn — arcs".
//
// 🔴 THE FORMAT, AND WHY THE INSTANCE COMES FIRST. With no instance a title is exactly what it
// was before the label existed — `cairn` for the root, `cairn — <page>` otherwise — and a test
// pins that byte for byte. With one, it is `<instance> — <page> · cairn` (root: `<instance> ·
// cairn`). A browser tab shows the START of a title and cuts the end, and the label exists so two
// deployments' tabs can be told apart: in `cairn — <page>` the distinguishing word would be the
// first thing cut. The page comes second because it is what tells two tabs of ONE deployment
// apart; the product name goes last, where it costs only truncated space and still reaches a
// window title and history search.

// InstanceNameMax is the longest instance label accepted, in characters. A tab shows roughly the
// first twenty; past that a label stops distinguishing anything, and a longer one is refused
// rather than cut, because a cut label is a label nobody chose.
const InstanceNameMax = 32

// ErrInstanceName refuses an instance label of the wrong shape. One sentinel, so `cmd/cairn-ui`
// can name the flag.
var ErrInstanceName = errors.New("ui: the instance name is not an acceptable page-title label")

// validInstanceName judges a label. "" is "no label" and is valid; a BLANK line is refused by
// `cmd/cairn-ui` before it reaches this, which is the `-app-*` flags' split.
//
// The value is rendered as TEXT and as a title — gomponents escapes both (`g.Text` and
// `c.HTML5Props.Title` go through `html.EscapeString`, pinned by a test with `<`, `&` and `"`) —
// so escaping is not what this guards. What it guards is the label being a LABEL: no control,
// format (a bidi override would reorder the title around it) or line/paragraph-separator
// character, and no surrounding whitespace, which a title bar collapses invisibly.
func validInstanceName(name string) error {
	if name == "" {
		return nil
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("%w: it is not valid UTF-8", ErrInstanceName)
	}
	if n := utf8.RuneCountInString(name); n > InstanceNameMax {
		return fmt.Errorf("%w: %q is %d characters, at most %d are accepted", ErrInstanceName, name, n, InstanceNameMax)
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("%w: %q begins or ends with whitespace", ErrInstanceName, name)
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return fmt.Errorf("%w: %q carries the control or format character %U", ErrInstanceName, name, r)
		}
	}
	return nil
}

// documentTitle composes a page's `<title>` from this deployment's label and the page's own.
// See this file's header for the format and why.
func documentTitle(app App, page string) string {
	if app.Instance == "" {
		if page == "" {
			return "cairn"
		}
		return "cairn — " + page
	}
	if page == "" {
		return app.Instance + " · cairn"
	}
	return app.Instance + " — " + page + " · cairn"
}

// wordmark is the header's `<h1>`: `cairn`, a link to the root on an authenticated frame and
// plain text on a public one (`TestNoPublicPageOffersAuthenticatedNavigation`'s rule), followed
// by the instance label when there is one.
//
// ⚠ THE LABEL IS INSIDE THE `<h1>`, NOT A NEW HEADER ITEM. The compact header is a grid whose
// auto-placement IS the DOM order (`tailwind.css`, B2); a new child of `<header>` would take a
// cell and shift every nav link. Inside the heading it is one cell, and the heading's accessible
// name reads "cairn <instance>" — the separating space is rendered text, not CSS.
func wordmark(app App, linked bool) g.Node {
	mark := g.Text("cairn")
	if linked {
		mark = h.A(h.Href(RootPath), mark)
	}
	if app.Instance == "" {
		return h.H1(mark)
	}
	return h.H1(mark, g.Text(" "), h.Span(h.Class("instance-name"), g.Text(app.Instance)))
}
