package ui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The gomponents API this ban is about, spelled once.
//
// 🔴 `Raw` AND `Rawf` ARE THE LIBRARY'S DOCUMENTED "RENDER THIS UNESCAPED"
// CONSTRUCTORS. `Text`/`Textf` run `template.HTMLEscapeString`; `Raw`/`Rawf` write
// the string through verbatim. An entry-content path that calls one has no escaping
// at all, and no amount of care elsewhere compensates.
//
// 🔴 `El` AND `Attr` ARE BANNED ONLY FOR A NON-CONSTANT NAME, WHICH IS A DIFFERENT
// HAZARD AND THE ONE THAT IS EASY TO MISS. gomponents escapes an attribute's VALUE
// (measured: `valueAttr` calls `template.HTMLEscapeString`) and writes its NAME
// verbatim, and `El` writes the element name verbatim too. So a name built from user
// text is a breakout the value escaping structurally cannot see. A constant name is
// exactly as safe as the literal markup it stands for, which is why the ban is on
// the argument's SHAPE rather than on the function.
const (
	gomponentsModule = "maragu.dev/gomponents"
	rawCtor          = "Raw"
	rawfCtor         = "Rawf"
	elCtor           = "El"
	attrCtor         = "Attr"
)

// scanForRawNodes reports every banned construction in one parsed file, and how many
// gomponents calls it inspected.
//
// 🔴 IT RESOLVES THE IMPORT ALIAS RATHER THAN GREPPING FOR A NAME. This package
// imports gomponents as `g`, so a text search for `gomponents.Raw` finds nothing
// that exists and a search for `.Raw(` finds every unrelated method with that name.
// Reading the file's import declarations and then matching selector expressions
// against the alias is what makes this a claim about the CODE rather than about its
// spelling.
func scanForRawNodes(fset *token.FileSet, file *ast.File) (findings []string, inspected int) {
	// Which local identifiers refer to the gomponents module (its root package or a
	// subpackage). A dot-import would bind the names into file scope with no
	// selector at all, so it is refused outright rather than handled.
	aliases := map[string]bool{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if path != gomponentsModule && !strings.HasPrefix(path, gomponentsModule+"/") {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			if imp.Name.Name == "." {
				findings = append(findings, fmt.Sprintf(
					"%s: DOT-IMPORT of %s. A dot-import binds Raw and Rawf into file scope with no selector, so "+
						"this ban cannot see a call to either. Import it under a name.",
					fset.Position(imp.Pos()), path))
				continue
			}
			name = imp.Name.Name
		}
		aliases[name] = true
	}
	if len(aliases) == 0 {
		return findings, 0
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel {
			return true
		}
		ident, isIdent := sel.X.(*ast.Ident)
		if !isIdent || !aliases[ident.Name] {
			return true
		}
		inspected++
		where := fset.Position(call.Pos()).String()
		switch sel.Sel.Name {
		case rawCtor, rawfCtor:
			findings = append(findings, fmt.Sprintf(
				"%s: calls %s.%s, which renders its argument UNESCAPED. This package renders store content — "+
					"text somebody else wrote — into HTML, and %s.%s is the one constructor that turns that text "+
					"into markup. Use %s.Text/%s.Textf.",
				where, ident.Name, sel.Sel.Name, ident.Name, sel.Sel.Name, ident.Name, ident.Name))
		case elCtor, attrCtor:
			if len(call.Args) == 0 {
				return true
			}
			if isStringConstant(call.Args[0]) {
				return true
			}
			findings = append(findings, fmt.Sprintf(
				"%s: calls %s.%s with a NON-CONSTANT name argument. gomponents escapes an attribute's VALUE and "+
					"writes its NAME verbatim, and writes an element name verbatim too — so a name built from "+
					"user text is a breakout the value escaping cannot see.",
				where, ident.Name, sel.Sel.Name))
		}
		return true
	})
	return findings, inspected
}

// isStringConstant answers whether an expression is a compile-time string constant
// this file can see: a literal, a concatenation of literals, or a bare identifier
// (which in practice is a package-level `const`).
//
// ⚠ IT IS DELIBERATELY CONSERVATIVE IN THE UNSAFE DIRECTION FOR IDENTIFIERS AND
// SAYING SO IS THE POINT. A bare identifier is accepted without proving it is a
// constant, because proving that needs the type checker and this ban runs without
// one. The consequence is stated rather than hidden: `g.Attr(someVariable, v)` is
// NOT caught. What is caught is every shape that could carry user text directly — a
// function call, an index, a selector, a conversion — which is where such a name
// actually comes from.
func isStringConstant(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING
	case *ast.Ident:
		return true
	case *ast.BinaryExpr:
		return v.Op == token.ADD && isStringConstant(v.X) && isStringConstant(v.Y)
	case *ast.ParenExpr:
		return isStringConstant(v.X)
	}
	return false
}

// uiPackageDir is this package's own source directory.
//
// 🔴 `os.Getwd`, NOT `runtime.Caller`, AND THAT IS A MEASURED CORRECTION. `go test`
// sets the working directory to the package's source directory in every environment
// this runs in; `runtime.Caller` does not survive `-trimpath`, which nixpkgs' Go
// builder sets — so the first version of this helper failed inside `nix build` with
// "the ui package directory could not be read" while passing on a developer host.
// The identity check below is what stops the scanner silently reading some OTHER
// directory: the files it must find are named, so a wrong cwd is a loud failure
// rather than a clean zero.
func uiPackageDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("the working directory is unavailable, so the package directory cannot be derived: %v", err)
	}
	for _, must := range []string{"render.go", "routes.go", "auth.go", "server.go"} {
		if _, err := os.Stat(filepath.Join(dir, must)); err != nil {
			t.Fatalf("%s does not hold %s, so it is not the ui package directory and a clean scan of it would "+
				"be a clean scan of the wrong thing: %v", dir, must, err)
		}
	}
	return dir
}

// TestNoRawNodeConstructorAppearsInTheUIPackage is the mechanical half of the XSS
// guard: a rendering test measures what the current code produces, and this measures
// that nobody can reach for the constructor that would make such a test irrelevant.
//
// 🔴 IT COVERS `_test.go` FILES TOO, WHICH IS NOT AN OVERSIGHT. A test that renders
// with `g.Raw` and asserts a comfortable output is exactly how a rendering guard
// gets walked; there is no case in which this package's tests need an unescaped
// node, and the negative control below is built from an in-memory source rather than
// from a real file precisely so no exemption is needed.
//
// 🔴 AND IT REPORTS A PAIR. The number of gomponents calls it INSPECTED must be
// non-zero — a scanner that resolved no alias inspects nothing and reports a clean
// zero indistinguishable from a pass.
func TestNoRawNodeConstructorAppearsInTheUIPackage(t *testing.T) {
	dir := uiPackageDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("the ui package directory could not be read: %v", err)
	}

	fset := token.NewFileSet()
	var findings []string
	inspected, scanned := 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s did not parse: %v", path, err)
		}
		scanned++
		got, n := scanForRawNodes(fset, file)
		findings = append(findings, got...)
		inspected += n
	}

	if scanned == 0 {
		t.Fatal("the scanner read NO .go file, so its clean result is about nothing")
	}
	if inspected == 0 {
		t.Fatal("the scanner inspected ZERO gomponents calls across the whole package, so it resolved no import " +
			"alias and its clean result is about nothing. Check scanForRawNodes before reading a verdict from it.")
	}

	if len(findings) > 0 {
		t.Errorf("THE RAW-NODE BAN FAILED, %d finding(s):\n  %s", len(findings), strings.Join(findings, "\n  "))
	}
	t.Logf("raw-node ban: %d file(s) scanned, %d gomponents call(s) inspected, %d finding(s)",
		scanned, inspected, len(findings))
}

// TestTheRawNodeBanCanGoRED is the negative control. A ban that cannot fail is a
// comment with a `func Test` in front of it.
//
// 🔴 EACH ARM ASSERTS ITS OWN MESSAGE, NOT "A FINDING APPEARED". A mutant killed by
// a different arm's message proves the scanner can produce output and proves nothing
// about the rule the arm exists for.
func TestTheRawNodeBanCanGoRED(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		wantFinding string
	}{{
		name: "Raw",
		src: `package ui
import g "maragu.dev/gomponents"
func bad(userText string) g.Node { return g.Raw(userText) }`,
		wantFinding: "calls g.Raw, which renders its argument UNESCAPED",
	}, {
		name: "Rawf",
		src: `package ui
import g "maragu.dev/gomponents"
func bad(userText string) g.Node { return g.Rawf("<b>%s</b>", userText) }`,
		wantFinding: "calls g.Rawf, which renders its argument UNESCAPED",
	}, {
		name: "Raw under a different alias",
		src: `package ui
import gom "maragu.dev/gomponents"
func bad(userText string) gom.Node { return gom.Raw(userText) }`,
		wantFinding: "calls gom.Raw, which renders its argument UNESCAPED",
	}, {
		name: "El with a computed name",
		src: `package ui
import g "maragu.dev/gomponents"
func bad(e Entry) g.Node { return g.El(e.Ref) }`,
		wantFinding: "calls g.El with a NON-CONSTANT name argument",
	}, {
		name: "Attr with a computed name",
		src: `package ui
import g "maragu.dev/gomponents"
func bad(e Entry) g.Node { return g.Attr(e.Ref, "x") }`,
		wantFinding: "calls g.Attr with a NON-CONSTANT name argument",
	}, {
		name: "dot-import",
		src: `package ui
import . "maragu.dev/gomponents"
func bad(userText string) Node { return Text(userText) }`,
		wantFinding: "DOT-IMPORT of maragu.dev/gomponents",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "mutant.go", tc.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("the mutant source did not parse, so it proves nothing about the ban: %v", err)
			}
			findings, _ := scanForRawNodes(fset, file)
			if len(findings) == 0 {
				t.Fatalf("the ban found NOTHING in a mutant that %s", tc.name)
			}
			joined := strings.Join(findings, "\n")
			if !strings.Contains(joined, tc.wantFinding) {
				t.Errorf("the ban fired, but with the WRONG message — a kill by another arm's rule is a "+
					"misattribution, not a kill.\nwanted: %s\ngot:\n%s", tc.wantFinding, joined)
			}
		})
	}

	// The mirror: constant names and escaped text must NOT be flagged, or the ban is
	// a refusal of all rendering rather than of unescaped rendering.
	clean := `package ui
import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)
const refAttr = "data-ref"
func fine(e Entry) g.Node {
	return h.Span(g.Attr(refAttr, e.Ref), g.El("span", g.Text(e.Title)), g.Textf("%s", e.Ref))
}`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "clean.go", clean, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("the clean source did not parse: %v", err)
	}
	findings, inspected := scanForRawNodes(fset, file)
	if len(findings) != 0 {
		t.Errorf("the ban flagged CORRECT code, which is how a ban gets deleted: %v", findings)
	}
	if inspected == 0 {
		t.Error("the ban inspected nothing in the clean source, so its zero says nothing")
	}
	t.Logf("negative control: %d mutant shape(s) each caught by their OWN message; clean source: %d call(s) inspected, 0 findings",
		len(cases), inspected)
}
