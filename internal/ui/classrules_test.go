package ui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// classCtor is the gomponents attribute constructor this guard reads, spelled once.
const classCtor = "Class"

// collectRenderedClasses returns every class TOKEN this file renders, plus a finding for
// every `Class` call whose argument is not a string literal, and how many `Class` calls it
// inspected.
//
// 🔴 IT IS AN AST SCAN AND NOT A GREP, AND THIS PACKAGE PROVES WHY RATHER THAN ASSERTING
// IT. `render.go`'s own doc comment contains the text `h.Class("flex gap-2")` as the WORKED
// EXAMPLE of the mistake this guard exists to catch. A grep for `h\.Class\("` finds it,
// reports `flex` and `gap-2` as rendered classes, and the guard fails on a comment — so the
// first thing anyone would do is weaken the guard. Reading the syntax tree sees no call
// there at all, because there is none.
//
// 🔴 A NON-LITERAL ARGUMENT IS A FINDING RATHER THAN A SKIP, AND THAT IS THE DIFFERENCE
// BETWEEN THIS GUARD AND A GUARD-SHAPED HOLE. `h.Class(someVar)` or `h.Class("text-"+size)`
// is a class name this scan cannot resolve; passing over it silently would make the clean
// verdict mean "every class I happened to be able to read", which is not what the test's
// name says. The rule it enforces is therefore the stronger one: a class on this surface is
// a literal, and it has a rule.
func collectRenderedClasses(fset *token.FileSet, file *ast.File) (tokens []string, findings []string, inspected int) {
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
				// The raw-node ban already refuses a dot-import for its own reason; this
				// scan says so too rather than silently seeing nothing.
				findings = append(findings, fmt.Sprintf(
					"%s: DOT-IMPORT of %s, so no selector carries the alias and this scan cannot see a "+
						"Class call in this file.", fset.Position(imp.Pos()), path))
				continue
			}
			name = imp.Name.Name
		}
		aliases[name] = true
	}
	if len(aliases) == 0 {
		return nil, findings, 0
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || sel.Sel.Name != classCtor {
			return true
		}
		ident, isIdent := sel.X.(*ast.Ident)
		if !isIdent || !aliases[ident.Name] {
			return true
		}
		inspected++
		where := fset.Position(call.Pos()).String()
		if len(call.Args) != 1 {
			findings = append(findings, fmt.Sprintf("%s: %s.Class takes one argument; this call has %d",
				where, ident.Name, len(call.Args)))
			return true
		}
		lit, isLit := call.Args[0].(*ast.BasicLit)
		if !isLit || lit.Kind != token.STRING {
			findings = append(findings, fmt.Sprintf(
				"%s: %s.Class is called with a NON-LITERAL argument. Nothing scans this package to build the "+
					"stylesheet — `tailwind.css` is the generator's only input — so a class name this test "+
					"cannot read is a class name nobody can check has a rule. Spell it as a literal.",
				where, ident.Name))
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			findings = append(findings, fmt.Sprintf("%s: the class literal did not unquote: %v", where, err))
			return true
		}
		for _, tok := range strings.Fields(value) {
			tokens = append(tokens, tok)
		}
		return true
	})
	return tokens, findings, inspected
}

// hasSelectorFor answers whether `css` carries a rule whose selector names exactly this
// class.
//
// 🔴 IT IS BOUNDARY-CHECKED, BECAUSE A SUBSTRING TEST IS SATISFIED BY THE WRONG CLASS AND
// THIS PACKAGE ALREADY CONTAINS THE PAIR THAT PROVES IT. `strings.Contains(css, ".signin")`
// is TRUE for a stylesheet that defines only `.signin-main` — so a naive check would report
// the sign-in form styled while its rule had been renamed away. A CSS class name continues
// through `[A-Za-z0-9_-]`, so the character after the name must be outside that set (`{`,
// `,`, ` `, `:`, `.`, or end of file).
func hasSelectorFor(css, class string) bool {
	return regexp.MustCompile(`\.` + regexp.QuoteMeta(class) + `([^A-Za-z0-9_\-]|$)`).MatchString(css)
}

// TestEveryRenderedClassHasARuleInTheStylesheet is the mechanical form of a rule that was
// PROSE ONLY until this test existed, and prose is exactly the wrong shape for it.
//
// 🔴 THE HAZARD IS SILENT IN EVERY EXISTING GATE. Nothing scans this package to build the
// stylesheet — `tailwind.css` carries `source(none)` and declares no `@source` — so adding
// `h.Class("flex gap-2")` to an element leaves `app.css` BYTE-UNCHANGED. The currency check
// compares generated against committed and stays green; `go build` has nothing to say about
// a string; every render test still passes, because they assert markup and not appearance.
// The element simply ships with class names the served bytes have no rule for. This test is
// the only thing in the tree that can see that.
//
// ⚠ WHAT IT DOES NOT CLAIM, STATED SO NOBODY READS IT WIDER THAN IT IS: it proves a SELECTOR
// exists, not that the rule is correct, reachable, or visually right. A `.viewer {}` with an
// empty body satisfies it. The browser walk is what measures appearance; this pins the one
// property that fails silently.
func TestEveryRenderedClassHasARuleInTheStylesheet(t *testing.T) {
	dir := uiPackageDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("the ui package directory could not be read: %v", err)
	}

	fset := token.NewFileSet()
	var findings []string
	seen := map[string][]string{}
	scanned, inspected := 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s did not parse: %v", path, err)
		}
		scanned++
		toks, got, n := collectRenderedClasses(fset, file)
		findings = append(findings, got...)
		inspected += n
		for _, tok := range toks {
			seen[tok] = append(seen[tok], e.Name())
		}
	}

	// 🔴 THE THREE WAYS THIS TEST COULD PASS WHILE MEASURING NOTHING, EACH REFUSED BY NAME.
	// A reassuring zero is indistinguishable from a harness wired to nothing, and every one
	// of these produces a clean run.
	if scanned == 0 {
		t.Fatal("the scan read NO non-test .go file, so its clean result is about nothing")
	}
	if inspected == 0 {
		t.Fatal("the scan inspected ZERO Class calls across the whole package, so it resolved no import " +
			"alias — its clean result is about nothing")
	}
	if len(seen) == 0 {
		t.Fatal("the scan found no class TOKEN at all. Every Class call was unreadable, or the splitter is " +
			"broken; either way this test proves nothing about the stylesheet")
	}
	if len(stylesheet) == 0 {
		t.Fatal("the embedded stylesheet is EMPTY, so every lookup below would fail for a reason that has " +
			"nothing to do with the class names")
	}

	// 🔴 POSITIVE CONTROL ON THE LOOKUP ITSELF, BECAUSE "ALL 34 RESOLVED" AND "THE LOOKUP
	// ALWAYS SAYS YES" ARE THE SAME OUTPUT. A name no stylesheet defines must NOT resolve,
	// and a name this one certainly defines must.
	const absent = "this-class-is-defined-nowhere"
	if hasSelectorFor(stylesheet, absent) {
		t.Fatalf("the lookup found a rule for %q, which nothing defines. It answers yes to everything, so a "+
			"green verdict below would be a fact about the lookup and not about the stylesheet", absent)
	}
	if !hasSelectorFor(stylesheet, "viewer") {
		t.Fatal("the lookup found no rule for `.viewer`, which the stylesheet certainly defines. It answers " +
			"no to everything, so it cannot distinguish a missing rule from a present one")
	}
	// 🔴 AND THE BOUNDARY, WHICH IS THE ONE THIS PACKAGE CAN ACTUALLY WALK INTO. `.signin`
	// and `.signin-main` are both real here; a substring lookup would report `signin` present
	// even if only `signin-main` survived a rename.
	if hasSelectorFor(".signin-main { color: red }", "signin") {
		t.Error("the lookup is satisfied by a PREFIX: it reports `.signin` present in a stylesheet that " +
			"defines only `.signin-main`. A renamed rule would then read as a present one")
	}

	for _, f := range findings {
		t.Errorf("CLASS SCAN: %s", f)
	}
	missing := 0
	for class, files := range seen {
		if !hasSelectorFor(stylesheet, class) {
			missing++
			t.Errorf("the surface renders class %q (in %s) and the served stylesheet has NO rule for it. "+
				"Nothing scans this package to build the stylesheet, so a raw Tailwind utility written into a "+
				"Class call silently does not exist: `app.css` is byte-unchanged, the currency check stays "+
				"green, and the element ships unstyled. Define %q in `tailwind.css` — as a semantic class "+
				"composed with @apply, like the other rules there — and regenerate with "+
				"`nix run .#build-ui-stylesheet`.", class, strings.Join(files, ", "), class)
		}
	}
	t.Logf("class rules: %d file(s) scanned, %d Class call(s) inspected, %d distinct class(es) rendered, "+
		"%d without a rule", scanned, inspected, len(seen), missing)
}

// TestTheClassRuleGuardCanGoRED is the negative control, and it drives the guard's TWO arms
// separately because they fail for different reasons and a control that only exercised one
// would leave the other unproven.
//
// ⚠ IT TESTS THE PREDICATES RATHER THAN RE-RUNNING THE TEST ABOVE, because a Go test cannot
// invoke another and read its failure. What it pins is that each arm's decision function
// answers correctly on a case built to be caught — which is what the arm's verdict rests on.
func TestTheClassRuleGuardCanGoRED(t *testing.T) {
	// Arm 1: a rendered class with no rule must be reported missing.
	const sheet = ".viewer { opacity: 0.7 }\n.signin-main { color: red }\n"
	if hasSelectorFor(sheet, "flex") {
		t.Error("arm 1 is dead: a raw utility `flex` was reported present in a stylesheet that does not " +
			"define it, so the guard would pass for exactly the defect it exists to catch")
	}
	if !hasSelectorFor(sheet, "viewer") {
		t.Error("arm 1 is dead in the other direction: a class the stylesheet DOES define was reported " +
			"missing, so the guard would be red on a correct tree and get deleted")
	}

	// Arm 2: a non-literal argument must be a finding rather than a silent skip.
	src := `package ui

import h "maragu.dev/gomponents/html"

func f(name string) any { return h.Class(name) }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "synthetic.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("the synthetic file did not parse: %v", err)
	}
	toks, findings, inspected := collectRenderedClasses(fset, file)
	if inspected != 1 {
		t.Fatalf("the scan inspected %d Class call(s) in the synthetic file, want 1 — it did not resolve the "+
			"import alias, so the arm-2 result below is about nothing", inspected)
	}
	if len(toks) != 0 {
		t.Errorf("the scan reported class token(s) %v from a NON-LITERAL argument; it cannot know them", toks)
	}
	if len(findings) != 1 {
		t.Fatalf("arm 2 is dead: a non-literal Class argument produced %d finding(s), want 1. A class name "+
			"the scan cannot read would then pass silently, and the clean verdict would mean only 'every "+
			"class I happened to be able to read'", len(findings))
	}
	if !strings.Contains(findings[0], "NON-LITERAL") {
		t.Errorf("arm 2 fired, but for the wrong reason: %q", findings[0])
	}
}
