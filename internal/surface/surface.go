// Package surface reads a Go package's EXPORTED SURFACE out of its source: every exported name
// with its signature as printed, for the ledgers of decision 19 of
// `claudedocs/plan-cairn-scope-mail.md` (Guard A over `internal/memo`, Guard C over
// `internal/pgstore`). It is used only by tests; it lives in a package because two ledgers read
// it, and a second copy of the printer would be a second answer to "what is this name".
//
// 🔴 THE LEDGER KEY IS NAME **AND** PRINTED TYPE. A name-only ledger stays green when an
// EXISTING name's signature changes — `Statuses() []string` becoming `Statuses(...Stored)
// []string` returns stored text under a name the ledger already trusted (the S0 audit's
// finding). And an EMBEDDED field is recorded by its printed type WHATEVER its package or
// exportedness — an embedded `json.RawMessage` (another package's type) was invisible to the
// name-only ledger, which skipped any qualified name.
//
// ⚠ IT READS SOURCE, NOT TYPES: no type-checker runs (one would need the module's third-party
// sources, which the nix sandbox's test step need not have). So a signature is what the source
// SAYS — an alias or a named type that hides a string is a name the ledger shows, not its
// underlying type. That is the scope of the claim.
package surface

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Exported reads every non-test `.go` file in `dir` that belongs to package `pkg` — with
// `override` REPLACING a file's contents (by base name) and `extra` ADDING files — and returns
// every exported identifier mapped to its printed signature:
//
//   - a function: `func(<param types>) <results>`, parameter names stripped;
//   - a method on ANY receiver: `Recv.Name` → `(<recv>) func(…)`;
//   - a type: `struct`, `interface`, `= <aliased>` or its printed underlying type expression;
//   - a struct field or interface method of ANY type, exported or not (an unexported type's
//     exported field is reachable through any exported name returning it): `Type.Field` →
//     printed type; an EMBEDDED field, of any package and exportedness: `Type.<printed type>` →
//     `embedded`;
//   - a package-level variable or constant: `var <type>`, `var = <value>` (a func literal as its
//     signature), `const <type>`, or `const` for an untyped constant.
func Exported(dir, pkg string, override, extra map[string]string) (map[string]string, error) {
	fset := token.NewFileSet()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	used := map[string]bool{}
	for _, p := range paths {
		base := filepath.Base(p)
		if strings.HasSuffix(base, "_test.go") {
			continue
		}
		var src any
		if o, ok := override[base]; ok {
			used[base] = true
			src = o
		} else {
			raw, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			src = raw
		}
		f, err := parser.ParseFile(fset, base, src, 0)
		if err != nil {
			return nil, err
		}
		if f.Name.Name != pkg {
			continue // a `package main` generator or a build-ignored file
		}
		files = append(files, f)
	}
	for name := range override {
		if !used[name] {
			return nil, fmt.Errorf("override names %s, which is not a source file of %s", name, dir)
		}
	}
	names := make([]string, 0, len(extra))
	for name := range extra {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, extra[name], 0)
		if err != nil {
			return nil, fmt.Errorf("extra file %s does not parse: %w", name, err)
		}
		files = append(files, f)
	}
	r := reader{fset: fset, out: map[string]string{}}
	for _, f := range files {
		r.file(f)
	}
	if r.err != nil {
		return nil, r.err
	}
	return r.out, nil
}

type reader struct {
	fset *token.FileSet
	out  map[string]string
	err  error
}

func (r *reader) pr(n ast.Node) string {
	var b bytes.Buffer
	if err := printer.Fprint(&b, r.fset, n); err != nil && r.err == nil {
		r.err = err
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func (r *reader) sig(ft *ast.FuncType) string {
	strip := func(fl *ast.FieldList) *ast.FieldList {
		if fl == nil {
			return nil
		}
		out := &ast.FieldList{}
		for _, f := range fl.List {
			n := len(f.Names)
			if n == 0 {
				n = 1
			}
			for i := 0; i < n; i++ {
				out.List = append(out.List, &ast.Field{Type: f.Type})
			}
		}
		return out
	}
	return r.pr(&ast.FuncType{TypeParams: ft.TypeParams, Params: strip(ft.Params), Results: strip(ft.Results)})
}

func (r *reader) members(typeName string, e ast.Expr) {
	var list *ast.FieldList
	switch e := e.(type) {
	case *ast.StructType:
		list = e.Fields
	case *ast.InterfaceType:
		list = e.Methods
	default:
		return
	}
	for _, fl := range list.List {
		if len(fl.Names) == 0 {
			r.out[typeName+"."+r.pr(fl.Type)] = "embedded"
			continue
		}
		for _, n := range fl.Names {
			if !n.IsExported() {
				continue
			}
			if ft, ok := fl.Type.(*ast.FuncType); ok {
				r.out[typeName+"."+n.Name] = r.sig(ft)
				continue
			}
			r.out[typeName+"."+n.Name] = r.pr(fl.Type)
		}
	}
}

func (r *reader) file(f *ast.File) {
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			if d.Recv == nil {
				r.out[d.Name.Name] = r.sig(d.Type)
				continue
			}
			recv := d.Recv.List[0].Type
			r.out[RecvName(recv)+"."+d.Name.Name] = "(" + r.pr(recv) + ") " + r.sig(d.Type)
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						switch t := s.Type.(type) {
						case *ast.StructType:
							r.out[s.Name.Name] = "struct"
						case *ast.InterfaceType:
							r.out[s.Name.Name] = "interface"
						default:
							if s.Assign != 0 {
								r.out[s.Name.Name] = "= " + r.pr(t)
							} else {
								r.out[s.Name.Name] = r.pr(t)
							}
						}
					}
					r.members(s.Name.Name, s.Type)
				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for i, n := range s.Names {
						if !n.IsExported() {
							continue
						}
						switch {
						case s.Type != nil:
							r.out[n.Name] = kind + " " + r.pr(s.Type)
						case i < len(s.Values):
							if fl, ok := s.Values[i].(*ast.FuncLit); ok {
								r.out[n.Name] = kind + " = " + r.sig(fl.Type)
							} else if kind == "const" {
								r.out[n.Name] = "const"
							} else {
								r.out[n.Name] = kind + " = " + r.pr(s.Values[i])
							}
						default:
							r.out[n.Name] = kind + " (implicit)"
						}
					}
				}
			}
		}
	}
}

// RecvName is a method receiver's base type name, through pointers and type parameters.
func RecvName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return RecvName(e.X)
	case *ast.IndexExpr:
		return RecvName(e.X)
	case *ast.IndexListExpr:
		return RecvName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return fmt.Sprintf("%T", e)
}

// Diff compares a measured surface with a ledger: rows that GREW (rendered as a ledger line to
// paste after review), rows that SHRANK, and rows whose printed signature CHANGED. Each is sorted.
func Diff(got, want map[string]string) (grew, shrank, changed []string) {
	for name, s := range got {
		w, known := want[name]
		switch {
		case !known:
			grew = append(grew, fmt.Sprintf("%q: %q", name, s))
		case w != s:
			changed = append(changed, fmt.Sprintf("%s: ledger %q, source %q", name, w, s))
		}
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			shrank = append(shrank, name)
		}
	}
	sort.Strings(grew)
	sort.Strings(shrank)
	sort.Strings(changed)
	return
}

// Moved reports whether a Diff moved by `name`: grew by it, or changed it.
func Moved(grew, changed []string, name string) bool {
	for _, g := range grew {
		if strings.HasPrefix(g, fmt.Sprintf("%q:", name)) {
			return true
		}
	}
	for _, c := range changed {
		if strings.HasPrefix(c, name+":") {
			return true
		}
	}
	return false
}
