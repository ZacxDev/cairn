package ui

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// The label every test here uses is SYNTHETIC and distinct from every word a page renders on its
// own ("cairn", a scope name, a nav label), so a hardcoded constant cannot coincide with it.
const fixtureInstance = "acme-staging"

// TestDocumentTitleComposesEveryShape pins the format as literals — never read back off
// `documentTitle` — for both the unlabelled and the labelled deployment, root and non-root.
func TestDocumentTitleComposesEveryShape(t *testing.T) {
	for _, tc := range []struct {
		instance, page, want string
	}{
		{"", "", "cairn"},
		{"", "arcs", "cairn — arcs"},
		{"", "sharing quarry-notes", "cairn — sharing quarry-notes"},
		{fixtureInstance, "", "acme-staging · cairn"},
		{fixtureInstance, "arcs", "acme-staging — arcs · cairn"},
		{"Acme Prod", "sign in", "Acme Prod — sign in · cairn"},
	} {
		if got := documentTitle(App{Instance: tc.instance}, tc.page); got != tc.want {
			t.Errorf("documentTitle(%q, %q) = %q, want %q", tc.instance, tc.page, got, tc.want)
		}
	}
}

// TestEveryFrameTitleIsComposedByDocumentTitle is the STRUCTURAL half of "no page can miss the
// label": every `c.HTML5Props` literal in this package sets `Title` to a CALL of `documentTitle`,
// and nothing in the package builds a `<title>` element of its own (`h.TitleEl`). It reads the AST,
// so a comment or a string spelling `documentTitle` does not satisfy it.
//
// ⚠ IT CANNOT SEE A CALLER HANDING `documentTitle` AN ALREADY-COMPOSED TITLE — a structural check
// type-checks past a wrong argument. `TestEveryPageCarriesTheInstanceLabel` is the behavioural half
// that refuses that.
func TestEveryFrameTitleIsComposedByDocumentTitle(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	frames := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "TitleEl" {
				t.Errorf("%s: `TitleEl` builds a <title> element outside documentTitle", fset.Position(sel.Pos()))
			}
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if sel, ok := lit.Type.(*ast.SelectorExpr); !ok || sel.Sel.Name != "HTML5Props" {
				return true
			}
			frames++
			composed := false
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok || kv.Key.(*ast.Ident).Name != "Title" {
					continue
				}
				if call, ok := kv.Value.(*ast.CallExpr); ok {
					if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "documentTitle" {
						composed = true
					}
				}
			}
			if !composed {
				t.Errorf("%s: a `c.HTML5Props` frame whose Title is not a call to documentTitle — its pages "+
					"would carry no instance label", fset.Position(lit.Pos()))
			}
			return true
		})
	}
	// The positive control: the three frames this package has (`shell`, `SignInPage`, `JoinPage`).
	if frames < 3 {
		t.Fatalf("found %d `c.HTML5Props` frame(s), want at least 3 — the walk is reaching nothing", frames)
	}
}

// TestNoPageLabelSpellsTheProductName closes the gap the two guards above leave, MEASURED by
// mutation: a `shell("cairn — arcs", …)` on a branch the crawl does not reach (the configured arcs
// index — the fixture has no arc journal) passed both, because the AST check sees a well-formed
// `documentTitle` call and the crawl never renders that branch. Every `shell` and `documentTitle`
// call's LABEL argument is read from the AST, and a string literal in it carrying "cairn" is the old
// pre-composed title coming back — `documentTitle` is the only place the product name is spelled.
//
// ⚠ IT IS A GUARD ON A WORD, AND THAT IS STATED RATHER THAN HIDDEN: a label built from a variable
// holding "cairn — " walks past it. What it pins is the one regression this change makes likely —
// re-adding the prefix this commit removed from thirteen call sites.
func TestNoPageLabelSpellsTheProductName(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	calls := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "title.go" {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			var label ast.Expr
			switch {
			case id.Name == "shell" && len(call.Args) > 0:
				label = call.Args[0]
			case id.Name == "documentTitle" && len(call.Args) > 1:
				label = call.Args[1]
			default:
				return true
			}
			calls++
			ast.Inspect(label, func(m ast.Node) bool {
				if lit, ok := m.(*ast.BasicLit); ok && lit.Kind == token.STRING && strings.Contains(lit.Value, "cairn") {
					t.Errorf("%s: the page label %s spells the product name — `documentTitle` adds it", fset.Position(lit.Pos()), lit.Value)
				}
				return true
			})
			return true
		})
	}
	// Eleven `shell` call sites and three `documentTitle` ones (one inside `shell`) today; fewer than ten means the
	// walk is not reaching them.
	if calls < 10 {
		t.Fatalf("found %d label call site(s) — the walk is reaching nothing", calls)
	}
	t.Logf("%d label call site(s) read", calls)
}

// TestEveryPageCarriesTheInstanceLabel is the BEHAVIOURAL half: on a labelled deployment every
// page the crawl reaches — authenticated and public, every frame — has ONE title that starts with
// the label and ends with the product name, carries no pre-composed "cairn — " (a caller that
// handed `documentTitle` a finished title), and shows the label once in the heading.
func TestEveryPageCarriesTheInstanceLabel(t *testing.T) {
	cfg := testConfig(t, staticAuth{testIdentity()})
	cfg.App = App{Instance: fixtureInstance}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("the labelled server did not build: %v", err)
	}
	pages := crawlPages(t, srv)
	if len(pages) < 15 {
		t.Fatalf("the crawl reached only %d page(s)", len(pages))
	}
	t.Logf("%d labelled page(s) crawled", len(pages))
	public := 0
	for row, body := range pages {
		titles := titleElement.FindAllStringSubmatch(body, -1)
		if len(titles) != 1 {
			t.Errorf("%s: %d <title> elements, want 1", row, len(titles))
			continue
		}
		title := titles[0][1]
		if title != fixtureInstance+" · cairn" &&
			!(strings.HasPrefix(title, fixtureInstance+" — ") && strings.HasSuffix(title, " · cairn")) {
			t.Errorf("%s: title %q is not %q nor %q", row, title, fixtureInstance+" · cairn",
				fixtureInstance+" — <page> · cairn")
		}
		if strings.Contains(title, "cairn — ") {
			t.Errorf("%s: title %q carries a pre-composed \"cairn — \"", row, title)
		}
		if n := strings.Count(body, `<span class="instance-name">`+fixtureInstance+`</span>`); n != 1 {
			t.Errorf("%s: the heading shows the label %d time(s), want 1", row, n)
		}
		if strings.Contains(body, `<h1>cairn `) {
			public++
		}
	}
	// Both frame kinds are in the set: a crawl of authenticated pages alone would leave the two
	// public frames — the ones built outside `shell` — unmeasured.
	if public < 2 {
		t.Errorf("only %d PUBLIC frame(s) (plain-text wordmark) in the crawl, want the sign-in and join pages", public)
	}
	// The label arms nothing: no manifest link, and the manifest row still answers 404.
	for row, body := range pages {
		if strings.Contains(body, `rel="manifest"`) {
			t.Errorf("%s: a label-only deployment rendered the PWA head", row)
		}
	}
	if rec := fetch(srv, ManifestPath, nil); rec.Code != 404 {
		t.Errorf("a label-only deployment's manifest answered %d, want 404", rec.Code)
	}
}

// TestTheInstanceLabelIsEscapedInTheTitleAndTheHeading: a label carrying markup reaches the page as
// TEXT in both places — gomponents escapes `Title` and `g.Text`; this pins that rather than trusting
// it. The value passes validation (no control characters), which is what makes it reachable.
func TestTheInstanceLabelIsEscapedInTheTitleAndTheHeading(t *testing.T) {
	hostile := `<b>&"x</b>`
	if err := (App{Instance: hostile}).Validate(); err != nil {
		t.Fatalf("PRECONDITION: the hostile label must pass validation to reach a page: %v", err)
	}
	body := renderNode(t, SignInPage("", false, "", App{Instance: hostile}))
	if strings.Contains(body, "<b>") {
		t.Fatalf("the label's markup reached the page unescaped:\n%s", body)
	}
	for _, want := range []string{
		`<title>&lt;b&gt;&amp;&#34;x&lt;/b&gt; — sign in · cairn</title>`,
		`<span class="instance-name">&lt;b&gt;&amp;&#34;x&lt;/b&gt;</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not carry %s:\n%s", want, body)
		}
	}
}

// TestTheInstanceNameIsValidatedWhetherOrNotTheAppIsArmed: every refusal is `ErrInstanceName`, on an
// UNARMED app as well as an armed one — the label renders on every page either way — and `New`
// refuses to build around it.
func TestTheInstanceNameIsValidatedWhetherOrNotTheAppIsArmed(t *testing.T) {
	accepted := []string{"acme", "Acme Staging", "café-prod", strings.Repeat("x", InstanceNameMax),
		strings.Repeat("é", InstanceNameMax)}
	refused := map[string]string{
		strings.Repeat("x", InstanceNameMax+1): "one past the cap",
		" acme":                                "leading whitespace",
		"acme ":                                "trailing whitespace",
		"acme\nprod":                           "a newline",
		"acme\tprod":                           "a tab",
		"acme\u202eprod":                       "a bidi override",
		"acme\u2028prod":                       "a line separator",
		"acme\u200bprod":                       "a zero-width space (format)",
		"acme\xffprod":                         "invalid UTF-8",
	}
	for _, armed := range []App{{}, {Name: "n", IconVariant: "amber"}} {
		for _, name := range accepted {
			a := armed
			a.Instance = name
			if err := a.Validate(); err != nil {
				t.Errorf("Validate(%+v) refused an acceptable label: %v", a, err)
			}
		}
		for name, why := range refused {
			a := armed
			a.Instance = name
			if err := a.Validate(); !errors.Is(err, ErrInstanceName) {
				t.Errorf("Validate(armed=%v, %q) (%s) = %v, want ErrInstanceName", a.Armed(), name, why, err)
			}
			cfg := testConfig(t, refusingAuth{})
			cfg.App = a
			if _, err := New(cfg); !errors.Is(err, ErrInstanceName) {
				t.Errorf("New with armed=%v, %q (%s) = %v, want ErrInstanceName", a.Armed(), name, why, err)
			}
		}
	}
}
