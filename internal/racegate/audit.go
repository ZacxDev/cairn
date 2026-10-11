package racegate

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Analysis is the concurrency reachability of one package directory: which of its declarations
// can reach a goroutine (or a `t.Parallel`), and the chain that proves it.
//
// 🔴 EVERY APPROXIMATION IN IT ERRS TOWARDS "CONCURRENT", because an over-report costs a test
// its exemption (a slower CI run) and an under-report costs a race the detector would have seen:
//
//   - the unit is the package-level DECLARATION: a `go` statement anywhere in a function body —
//     including inside a function literal that is never called — marks the whole function;
//   - a method call `x.M` is an edge to EVERY method named `M` in the package, whatever `x` is;
//   - an identifier naming a package-level function or variable is an edge whether it is called,
//     passed as a value, or shadowed by a local of the same name;
//   - `init` and `TestMain` are reached by every test;
//   - a MODULE package imported by any file in the directory, test files included, is AMBIENT:
//     if its non-test code — or, transitively, any module package it imports — holds a `go`
//     statement, EVERY test here is concurrent. Values of its types can flow anywhere (a
//     `*redact.Redactor` field called as `a.red.Record(…)` names no `redact.` identifier at the
//     call), so following its functions by name would under-report; the whole package is the
//     honest unit.
//
// What it does NOT follow is stated in the package doc: the standard library, and `reflect`.
type Analysis struct {
	dir   string
	tests map[string]bool
	nodes map[string]*node
	// ambient is the reason every test is concurrent ("" if none).
	ambient string
}

type node struct {
	edges map[string]bool
	seed  string // why this declaration is concurrent on its own ("" if it is not)
}

// Analyse reads every `.go` file in dir — test and non-test, every package clause, every build
// tag — and the module packages they import.
func Analyse(dir string) (*Analysis, error) {
	root, modPath, err := moduleOf(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	files, err := parseDir(fset, dir, true)
	if err != nil {
		return nil, err
	}
	a := &Analysis{dir: dir, tests: map[string]bool{}, nodes: map[string]*node{}}
	get := func(k string) *node {
		n := a.nodes[k]
		if n == nil {
			n = &node{edges: map[string]bool{}}
			a.nodes[k] = n
		}
		return n
	}
	// Pass 1: the declarations, so pass 2 can tell a package-level name from anything else.
	type decl struct {
		key  string
		body ast.Node
	}
	var decls []decl
	for _, f := range files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				key := d.Name.Name
				if d.Recv != nil {
					key = "." + key
				} else if isTestFunc(d) {
					a.tests[key] = true
				}
				get(key)
				if d.Body != nil {
					decls = append(decls, decl{key, d.Body})
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					vs, ok := s.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range vs.Names {
						get(name.Name)
						for _, v := range vs.Values {
							decls = append(decls, decl{name.Name, v})
						}
					}
				}
			}
		}
	}
	// Pass 2: the edges and the seeds.
	for _, d := range decls {
		n := a.nodes[d.key]
		ast.Inspect(d.body, func(x ast.Node) bool {
			switch x := x.(type) {
			case *ast.GoStmt:
				if n.seed == "" {
					n.seed = "a `go` statement at " + rel(fset, x.Pos())
				}
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Parallel" && len(x.Args) == 0 && n.seed == "" {
					n.seed = "`Parallel()` at " + rel(fset, x.Pos())
				}
			case *ast.SelectorExpr:
				if _, ok := a.nodes["."+x.Sel.Name]; ok {
					n.edges["."+x.Sel.Name] = true
				}
			case *ast.Ident:
				if _, ok := a.nodes[x.Name]; ok && x.Name != d.key {
					n.edges[x.Name] = true
				}
			}
			return true
		})
	}
	// The ambient half: every module package any file here imports.
	seen := map[string]string{}
	for _, f := range files {
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p != modPath && !strings.HasPrefix(p, modPath+"/") {
				continue
			}
			why, err := packageGoes(fset, root, modPath, p, seen)
			if err != nil {
				return nil, err
			}
			if why != "" && a.ambient == "" {
				a.ambient = "imports " + p + ", " + why
			}
		}
	}
	return a, nil
}

// Tests is the set of top-level `func TestXxx(t *testing.T)` declarations in the directory.
func (a *Analysis) Tests() map[string]bool { return a.tests }

// Concurrent answers whether the declaration named key (a function, a variable, or "."+method)
// can reach a goroutine, and if so the chain that reaches it, ending in the reason.
func (a *Analysis) Concurrent(key string) ([]string, bool) {
	if a.ambient != "" && a.tests[key] {
		return []string{key, a.ambient}, true
	}
	starts := []string{key}
	if a.tests[key] {
		for _, k := range []string{"init", "TestMain"} {
			if _, ok := a.nodes[k]; ok {
				starts = append(starts, k)
			}
		}
	}
	parent := map[string]string{}
	var queue []string
	for _, s := range starts {
		if _, ok := a.nodes[s]; !ok {
			continue
		}
		if _, dup := parent[s]; !dup {
			parent[s] = ""
			queue = append(queue, s)
		}
	}
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		if s := a.nodes[k].seed; s != "" {
			var chain []string
			for c := k; c != ""; c = parent[c] {
				chain = append([]string{c}, chain...)
			}
			if chain[0] != key {
				chain = append([]string{key}, chain...)
			}
			return append(chain, s), true
		}
		next := make([]string, 0, len(a.nodes[k].edges))
		for e := range a.nodes[k].edges {
			next = append(next, e)
		}
		sort.Strings(next) // a deterministic witness
		for _, e := range next {
			if _, ok := parent[e]; !ok {
				parent[e] = k
				queue = append(queue, e)
			}
		}
	}
	return nil, false
}

// Check is the seam guard a package's own test calls: every exempt name must be a top-level test
// in dir that CANNOT reach a goroutine, and every name in mustBeConcurrent — the positive
// control — must be classified concurrent, so an analysis that sees nothing cannot pass.
func Check(t testing.TB, dir string, exempt, mustBeConcurrent []string) {
	t.Helper()
	for _, p := range Problems(dir, exempt, mustBeConcurrent) {
		t.Error(p)
	}
}

// Problems is Check's verdict as values: one line per refusal, none when the list is sound.
func Problems(dir string, exempt, mustBeConcurrent []string) []string {
	if err := validate(exempt); err != nil {
		return []string{"racegate: the exempt list is malformed: " + err.Error()}
	}
	if len(mustBeConcurrent) == 0 {
		return []string{"racegate: no positive control given — an analysis wired to nothing would pass"}
	}
	a, err := Analyse(dir)
	if err != nil {
		return []string{fmt.Sprintf("racegate: could not analyse %s: %v", dir, err)}
	}
	var out []string
	for _, k := range mustBeConcurrent {
		if _, ok := a.Concurrent(k); !ok {
			out = append(out, fmt.Sprintf("racegate: POSITIVE CONTROL: %s must be classified concurrent and is not — the analysis is blind, or the control is stale", k))
		}
	}
	for _, name := range exempt {
		if !a.tests[name] {
			out = append(out, fmt.Sprintf("racegate: %s is exempt from -race but is not a top-level test function in %s", name, dir))
			continue
		}
		if chain, ok := a.Concurrent(name); ok {
			out = append(out, fmt.Sprintf("racegate: %s is exempt from -race but CAN REACH A GOROUTINE: %s — it must run under the detector; remove it from the exempt list",
				name, strings.Join(chain, " -> ")))
		}
	}
	return out
}

// packageGoes reports why the module package p can start a goroutine ("" if it cannot): a `go`
// statement in its non-test files, or one in a module package it imports.
func packageGoes(fset *token.FileSet, root, modPath, p string, seen map[string]string) (string, error) {
	if why, ok := seen[p]; ok {
		return why, nil
	}
	seen[p] = "" // Go has no import cycles; this only short-circuits a diamond.
	files, err := parseDir(fset, filepath.Join(root, strings.TrimPrefix(strings.TrimPrefix(p, modPath), "/")), false)
	if err != nil {
		return "", err
	}
	why := ""
	for _, f := range files {
		ast.Inspect(f, func(x ast.Node) bool {
			if g, ok := x.(*ast.GoStmt); ok && why == "" {
				why = "whose code has a `go` statement at " + rel(fset, g.Pos())
			}
			return why == ""
		})
	}
	for _, f := range files {
		for _, imp := range f.Imports {
			q, _ := strconv.Unquote(imp.Path.Value)
			if why != "" || (q != modPath && !strings.HasPrefix(q, modPath+"/")) {
				continue
			}
			sub, err := packageGoes(fset, root, modPath, q, seen)
			if err != nil {
				return "", err
			}
			if sub != "" {
				why = "which imports " + q + ", " + sub
			}
		}
	}
	seen[p] = why
	return why, nil
}

func parseDir(fset *token.FileSet, dir string, withTests bool) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || (!withTests && strings.HasSuffix(n, "_test.go")) {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .go files in %s", dir)
	}
	return files, nil
}

func isTestFunc(d *ast.FuncDecl) bool {
	if !strings.HasPrefix(d.Name.Name, "Test") || d.Name.Name == "TestMain" || d.Type.Params == nil || len(d.Type.Params.List) != 1 {
		return false
	}
	star, ok := d.Type.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "T"
}

// moduleOf walks up from dir to the go.mod and reads its module path.
func moduleOf(dir string) (root, modPath string, err error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	for d := abs; ; d = filepath.Dir(d) {
		f, err := os.Open(filepath.Join(d, "go.mod"))
		if err == nil {
			defer f.Close()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
					return d, strings.Trim(strings.TrimSpace(rest), `"`), nil
				}
			}
			return "", "", fmt.Errorf("%s/go.mod names no module", d)
		}
		if filepath.Dir(d) == d {
			return "", "", fmt.Errorf("no go.mod above %s", abs)
		}
	}
}

func rel(fset *token.FileSet, p token.Pos) string {
	pos := fset.Position(p)
	return filepath.Base(pos.Filename) + ":" + strconv.Itoa(pos.Line)
}
