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

// The three ways this package can put a class on an element, spelled once.
//
// 🔴 THERE ARE THREE BECAUSE A DRAFT OF THIS GUARD HANDLED ONLY THE FIRST, AND ITS NAME
// CLAIMED ALL OF THEM. Measured on that draft: replacing one `h.Class("viewer")` with
// `g.Attr("class", "zzbypass gap-2")` — two names with no rule in `app.css` — left the guard
// PASSING, and so did `c.Classes{...}`. Neither was a finding; both were INVISIBLE. `g` and
// `c` are already imported by `render.go`, and `components.Classes` is gomponents' idiomatic
// conditional-class helper — exactly what somebody would reach for to collapse `taskItem`'s
// `Class("task refused")` / `Class("task")` branch. A guard whose description is wider than
// its implementation reads as coverage while providing none, which is worse than no guard
// because it stops anyone looking.
const (
	classCtor = "Class"
	// attrCtorName is `Attr`, which emits an arbitrary attribute — a class source when its
	// first argument is the literal "class".
	attrCtorName = "Attr"
	// classesType is `components.Classes`, a `map[string]bool` whose KEYS are class names and
	// which renders as a `class` attribute. It appears as a composite literal, not a call, so
	// it is matched separately from the two constructors above.
	classesType = "Classes"
	// classAttrName is the attribute name that makes an `Attr` call a class source.
	classAttrName = "class"
)

// collectRenderedClasses returns every class TOKEN this file renders, plus a finding for
// every class source whose names it cannot read, and how many class sources it inspected.
//
// 🔴 THE THREE ARMS ARE `Class`, `Attr("class", …)` AND `Classes{…}`, AND THE LIST IS
// CLOSED — WHICH IS THE LIMIT OF THIS GUARD AND IS STATED HERE RATHER THAN LEFT TO BE
// DISCOVERED. Those are every way gomponents emits a class attribute today. A FOURTH way —
// a helper in this package that wraps one of them behind a non-literal, a future gomponents
// API, a hand-rolled node type with its own `Render` — would be INVISIBLE to this scan: not
// a finding, absent. The scan cannot close that structurally without a type checker, so the
// mitigation is the non-literal rule instead: every shape it CAN see whose names it cannot
// READ is a finding, which is what stops the hole being reopened one level up by indirection.
// If a fourth source is added, add an arm here in the same commit.
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

	// literalString unquotes an expression if it is a string literal.
	literalString := func(e ast.Expr) (string, bool) {
		lit, isLit := e.(*ast.BasicLit)
		if !isLit || lit.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return "", false
		}
		return v, true
	}
	addTokens := func(value string) {
		tokens = append(tokens, strings.Fields(value)...)
	}

	ast.Inspect(file, func(n ast.Node) bool {
		// ARM 3 — `<alias>.Classes{…}`, a composite literal rather than a call.
		if lit, isComposite := n.(*ast.CompositeLit); isComposite {
			sel, isSel := lit.Type.(*ast.SelectorExpr)
			if !isSel || sel.Sel.Name != classesType {
				return true
			}
			ident, isIdent := sel.X.(*ast.Ident)
			if !isIdent || !aliases[ident.Name] {
				return true
			}
			inspected++
			where := fset.Position(lit.Pos()).String()
			for _, elt := range lit.Elts {
				kv, isKV := elt.(*ast.KeyValueExpr)
				if !isKV {
					findings = append(findings, fmt.Sprintf(
						"%s: a %s.Classes element is not a key/value pair, so this scan cannot read the class "+
							"name it carries.", where, ident.Name))
					continue
				}
				key, ok := literalString(kv.Key)
				if !ok {
					findings = append(findings, fmt.Sprintf(
						"%s: a %s.Classes KEY is not a string literal. The key IS the class name, and a name "+
							"this scan cannot read is a name nobody can check has a rule in the stylesheet.",
						where, ident.Name))
					continue
				}
				addTokens(key)
			}
			return true
		}

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
		where := fset.Position(call.Pos()).String()

		switch sel.Sel.Name {
		// ARM 1 — `<alias>.Class("…")`.
		case classCtor:
			inspected++
			if len(call.Args) != 1 {
				findings = append(findings, fmt.Sprintf("%s: %s.Class takes one argument; this call has %d",
					where, ident.Name, len(call.Args)))
				return true
			}
			value, ok := literalString(call.Args[0])
			if !ok {
				findings = append(findings, fmt.Sprintf(
					"%s: %s.Class is called with a NON-LITERAL argument. Nothing scans this package to build "+
						"the stylesheet — `tailwind.css` is the generator's only input — so a class name this "+
						"test cannot read is a class name nobody can check has a rule. Spell it as a literal.",
					where, ident.Name))
				return true
			}
			addTokens(value)

		// ARM 2 — `<alias>.Attr("class", "…")`.
		//
		// 🔴 A NON-LITERAL ATTRIBUTE NAME IS A FINDING TOO, BECAUSE IT *COULD* BE "class" AND
		// THIS SCAN CANNOT TELL. Skipping it would reopen the exact hole this arm closes, one
		// level up: `g.Attr(name, "zzbypass")` would be invisible again. The raw-node ban next
		// door refuses a non-constant `Attr` name for its own (escaping) reason; this refuses
		// it for a different one, and both want the same thing from the code.
		case attrCtorName:
			if len(call.Args) == 0 {
				return true
			}
			name, ok := literalString(call.Args[0])
			if !ok {
				inspected++
				findings = append(findings, fmt.Sprintf(
					"%s: %s.Attr is called with a NON-LITERAL attribute NAME, so this scan cannot tell whether "+
						"it emits a class attribute. Spell the name as a literal.", where, ident.Name))
				return true
			}
			if !strings.EqualFold(name, classAttrName) {
				return true
			}
			inspected++
			// A one-argument `Attr("class")` is a boolean attribute with no value, so it names
			// no class. It is pointless rather than dangerous; nothing to check.
			if len(call.Args) < 2 {
				return true
			}
			value, ok := literalString(call.Args[1])
			if !ok {
				findings = append(findings, fmt.Sprintf(
					"%s: %s.Attr(%q, …) is called with a NON-LITERAL value, so the class names it emits cannot "+
						"be checked against the stylesheet. Spell them as a literal, or use %s.Class.",
					where, ident.Name, name, ident.Name))
				return true
			}
			addTokens(value)
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

	// 🔴 POSITIVE CONTROL ON THE LOOKUP ITSELF, BECAUSE "THEY ALL RESOLVED" AND "THE LOOKUP
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

	// The remaining arms are driven over SYNTHETIC files, and that is a deliberate split
	// rather than a shortcut. `Attr("class", …)` and `Classes{…}` do not appear in this
	// package today, so there is nothing real for them to read — which is exactly the state
	// in which a dead arm is invisible. A synthetic source is the only way to prove the
	// machinery works BEFORE somebody writes the first one.
	scan := func(t *testing.T, src string) ([]string, []string, int) {
		t.Helper()
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "synthetic.go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("the synthetic file did not parse: %v", err)
		}
		return collectRenderedClasses(fset, file)
	}

	const imports = `package ui

import (
	g "maragu.dev/gomponents"
	c "maragu.dev/gomponents/components"
	h "maragu.dev/gomponents/html"
)

var _, _, _ = g.Text, c.Classes{}, h.Class
`

	for _, tc := range []struct {
		name       string
		src        string
		wantTokens []string
		wantFinds  int
		wantReason string
		why        string
	}{
		{
			name:       "arm 1: a non-literal Class argument is a finding, not a skip",
			src:        imports + "\nfunc f(name string) any { return h.Class(name) }\n",
			wantFinds:  1,
			wantReason: "NON-LITERAL",
			why: "a class name the scan cannot read would pass silently, and the clean verdict would " +
				"mean only 'every class I happened to be able to read'",
		},
		{
			name:       "arm 2: Attr(\"class\", …) is READ, not ignored",
			src:        imports + "\nfunc f() any { return g.Attr(\"class\", \"zzbypass gap-2\") }\n",
			wantTokens: []string{"zzbypass", "gap-2"},
			why: "measured on the first draft of this guard: swapping one h.Class for this exact call " +
				"left the guard PASSING with two classes that have no rule",
		},
		{
			name:       "arm 2: a non-literal Attr VALUE on class is a finding",
			src:        imports + "\nfunc f(v string) any { return g.Attr(\"class\", v) }\n",
			wantFinds:  1,
			wantReason: "NON-LITERAL",
			why:        "otherwise the arm is walkable by moving the string one variable away",
		},
		{
			name:       "arm 2: a non-literal Attr NAME is a finding, because it COULD be class",
			src:        imports + "\nfunc f(n string) any { return g.Attr(n, \"zzbypass\") }\n",
			wantFinds:  1,
			wantReason: "NON-LITERAL",
			why:        "skipping it reopens the hole one level up",
		},
		{
			name: "arm 2: an Attr on a DIFFERENT attribute is not a class source",
			src:  imports + "\nfunc f() any { return g.Attr(\"title\", \"zzbypass\") }\n",
			why:  "a guard that treated every attribute as classes would be red on correct code",
		},
		{
			name:       "arm 3: Classes{…} KEYS are read",
			src:        imports + "\nfunc f(ok bool) any { return c.Classes{\"task\": true, \"zzbypass\": ok} }\n",
			wantTokens: []string{"task", "zzbypass"},
			why: "components.Classes renders as a class attribute, and it is the idiomatic helper for " +
				"the conditional branch taskItem already has",
		},
		{
			name:       "arm 3: a non-literal Classes KEY is a finding",
			src:        imports + "\nfunc f(k string) any { return c.Classes{k: true} }\n",
			wantFinds:  1,
			wantReason: "not a string literal",
			why:        "the key IS the class name",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			toks, findings, inspected := scan(t, tc.src)
			if inspected == 0 {
				t.Fatalf("the scan inspected ZERO class sources in this file, so every result below is "+
					"about nothing — it resolved no import alias or the arm never matched. (%s)", tc.why)
			}
			if len(findings) != tc.wantFinds {
				t.Fatalf("got %d finding(s), want %d: %v. THE ARM IS DEAD — %s",
					len(findings), tc.wantFinds, findings, tc.why)
			}
			if tc.wantReason != "" && !strings.Contains(findings[0], tc.wantReason) {
				t.Errorf("the arm fired, but for the wrong reason: %q does not mention %q",
					findings[0], tc.wantReason)
			}
			if tc.wantFinds > 0 && len(toks) != 0 {
				t.Errorf("the scan reported token(s) %v it cannot actually know", toks)
			}
			if got := strings.Join(toks, " "); got != strings.Join(tc.wantTokens, " ") {
				t.Errorf("class tokens = %q, want %q. %s", got, strings.Join(tc.wantTokens, " "), tc.why)
			}
			// 🔴 AND THE ARM MUST REACH THE VERDICT, NOT MERELY PARSE. A token this scan
			// extracts is only useful if the missing-rule lookup would fire on it, so the
			// synthetic bypass names are checked against the REAL stylesheet here.
			for _, tok := range toks {
				if strings.HasPrefix(tok, "zzbypass") && hasSelectorFor(stylesheet, tok) {
					t.Errorf("the stylesheet unexpectedly defines %q, so this control cannot show the "+
						"arm producing a FAILING token", tok)
				}
			}
		})
	}
}
