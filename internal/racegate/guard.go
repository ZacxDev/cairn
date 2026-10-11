package racegate

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

// Check is the guard a declaring package's own test calls: Problems over its directory.
func Check(t testing.TB, exempt, concurrent []string) {
	t.Helper()
	for _, p := range Problems(".", exempt, concurrent) {
		t.Error(p)
	}
}

// Problems reads dir with go/ast — never a regex, which would read a COMMENT saying "go func" as
// concurrency — and returns one line per refusal. `concurrent` names functions in dir known to
// start a goroutine (redact's `measureCorpus`); it must be non-empty and each must still contain
// a `go` statement, which is its positive control and what stops it going stale silently.
func Problems(dir string, exempt, concurrent []string) []string {
	if err := validate(exempt); err != nil {
		return []string{"racegate: the exempt list is malformed: " + err.Error()}
	}
	if len(concurrent) == 0 {
		return []string{"racegate: no concurrent function declared — the guard has no positive control"}
	}
	fset, funcs := token.NewFileSet(), map[string]*ast.FuncDecl{}
	var out []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{"racegate: " + err.Error()}
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			return []string{"racegate: " + err.Error()}
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Body != nil {
				funcs[fd.Name.Name] = fd
			}
		}
		if strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "sync" || p == "sync/atomic" {
				out = append(out, fmt.Sprintf("racegate: PREMISE BROKEN: production file %s imports %q", fset.Position(imp.Pos()), p))
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if what := concurrency(n); what != "" {
				out = append(out, fmt.Sprintf("racegate: PREMISE BROKEN: production code has %s at %s — an exempt package must have none", what, fset.Position(n.Pos())))
			}
			return true
		})
	}
	isConc := map[string]bool{}
	for _, h := range concurrent {
		isConc[h] = true
		if fd := funcs[h]; fd == nil || !hasGo(fd.Body) {
			out = append(out, fmt.Sprintf("racegate: CONCURRENT HELPER %s is missing or no longer has a `go` statement — the declared list is stale", h))
		}
	}
	for _, name := range exempt {
		fd := funcs[name]
		if fd == nil || !isTestFunc(fd) {
			out = append(out, fmt.Sprintf("racegate: STALE: %s is exempt from -race but is not a top-level test function in %s", name, dir))
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			why := ""
			switch n := n.(type) {
			case *ast.GoStmt:
				why = "starts a goroutine"
			case *ast.CallExpr:
				if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Parallel" && len(n.Args) == 0 {
					why = "calls Parallel()"
				}
			case *ast.Ident:
				if isConc[n.Name] {
					why = "calls the concurrent helper " + n.Name
				}
			}
			if why != "" {
				out = append(out, fmt.Sprintf("racegate: EXEMPT TEST %s %s at %s — it must run under -race", name, why, fset.Position(n.Pos())))
			}
			return true
		})
	}
	return out
}

// concurrency names the concurrency construct n is, or "" — a `go` statement, a channel type, a
// send, or a receive.
func concurrency(n ast.Node) string {
	switch n := n.(type) {
	case *ast.GoStmt:
		return "a `go` statement"
	case *ast.ChanType:
		return "a channel type"
	case *ast.SendStmt:
		return "a channel send"
	case *ast.UnaryExpr:
		if n.Op == token.ARROW {
			return "a channel receive"
		}
	}
	return ""
}

func hasGo(body ast.Node) (found bool) {
	ast.Inspect(body, func(n ast.Node) bool { _, ok := n.(*ast.GoStmt); found = found || ok; return !found })
	return found
}

func isTestFunc(d *ast.FuncDecl) bool {
	if !strings.HasPrefix(d.Name.Name, "Test") || d.Name.Name == "TestMain" || len(d.Type.Params.List) != 1 {
		return false
	}
	star, ok := d.Type.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "T"
}
