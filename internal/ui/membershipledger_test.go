package ui

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The membership-actor LEDGER: every use of an actor-taking method of `Inviting` or `Sharing`
// — or of their implementations `ControlInviting` / `ControlSharing` — anywhere in this
// package's non-test code, and the actor it is handed.
//
// 🔴 RECEIVERS ARE RESOLVED BY TYPE (`go/types`), NOT BY SPELLING, AND THAT IS A MEASURED
// CORRECTION. The first version matched the call shape `<x>.inviting.M(...)`; an auditor added
// `inv := s.inviting; inv.Invitable(id.Principal)` and it stayed PASS. A local alias, a method
// value, a helper taking the interface, a renamed field and a direct `ControlInviting` value
// all reach the same method through a different spelling. The type checker answers "which
// method is this" for every one of them, so the ledger is keyed on that answer.
//
// ⚠ THE CHECKER RUNS WITH NO IMPORTER, DELIBERATELY. Every import fails to resolve — which
// costs nothing here, because the four types this ledger is about are declared IN this
// package — and in exchange the test needs no export data, no `go list`, no module cache and
// no network, so it behaves the same inside `nix build`'s sandbox as on a developer host. The
// one thing an unresolved import does cost is the PARAMETER TYPE (`control.Principal` is
// unresolved), so which parameter is the actor is read from the interface's SOURCE instead.
// The positive controls in `TestEveryMembershipDecisionActsAsMembershipActor` are what make a
// checker that resolved nothing a failure rather than a clean zero.

// membershipTypes are the types whose actor-taking methods the ledger watches.
var membershipTypes = []string{"Inviting", "Sharing", "ControlInviting", "ControlSharing"}

// ledgerNotCalled marks a method used as a VALUE rather than called in place. It is never in
// the wanted ledger: a method value's actor is decided wherever it is eventually called, which
// no static reading of this site can see, so the shape itself is refused.
const ledgerNotCalled = "<method value: the actor cannot be read at this site>"

func parseUIPackage(t *testing.T, fset *token.FileSet, extra map[string]string) []*ast.File {
	t.Helper()
	dir := uiPackageDir(t)
	names, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, path := range names {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, filepath.Base(path), src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		files = append(files, f)
	}
	for name, src := range extra {
		f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing the synthetic %s: %v", name, err)
		}
		files = append(files, f)
	}
	return files
}

// membershipLedger returns one line per use of an actor-taking membership method:
// "<enclosing function> <Type>.<Method> <actor expression | ledgerNotCalled>".
func membershipLedger(t *testing.T, fset *token.FileSet, files []*ast.File) []string {
	t.Helper()

	// Which parameter is the actor, per method name, read from the interfaces' source.
	actorParam := map[string]int{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || (ts.Name.Name != "Inviting" && ts.Name.Name != "Sharing") {
				return true
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				return false
			}
			for _, m := range it.Methods.List {
				ft, ok := m.Type.(*ast.FuncType)
				if !ok {
					continue
				}
				idx := 0
				for _, p := range ft.Params.List {
					width := max(len(p.Names), 1)
					if astText(fset, p.Type) == "control.Principal" {
						for _, name := range m.Names {
							actorParam[name.Name] = idx
						}
					}
					idx += width
				}
			}
			return false
		})
	}

	info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
	conf := types.Config{
		Importer: importerFunc(func(string) (*types.Package, error) {
			return nil, errors.New("no importer: see the comment at the top of this file")
		}),
		Error: func(error) {}, // unresolved imports are expected; keep checking
	}
	pkg, _ := conf.Check("github.com/ZacxDev/cairn/internal/ui", fset, files, info)
	if pkg == nil {
		t.Fatal("the type checker produced no package")
	}
	watched := map[*types.TypeName]bool{}
	for _, name := range membershipTypes {
		tn, ok := pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok {
			t.Fatalf("the package declares no type %s, so the ledger would watch nothing named that", name)
		}
		watched[tn] = true
	}

	var out []string
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			where := fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) == 1 {
				where = strings.TrimPrefix(astText(fset, fn.Recv.List[0].Type), "*") + "." + where
			}
			calledAs := map[*ast.SelectorExpr]*ast.CallExpr{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
						calledAs[sel] = call
					}
				}
				return true
			})
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				selection := info.Selections[sel]
				if selection == nil || selection.Kind() == types.FieldVal {
					return true
				}
				recv := selection.Recv()
				if p, ok := recv.(*types.Pointer); ok {
					recv = p.Elem()
				}
				named, ok := recv.(*types.Named)
				if !ok || !watched[named.Obj()] {
					return true
				}
				idx, takesActor := actorParam[sel.Sel.Name]
				if !takesActor {
					return true
				}
				actor := ledgerNotCalled
				if call, ok := calledAs[sel]; ok && selection.Kind() == types.MethodVal && idx < len(call.Args) {
					actor = astText(fset, call.Args[idx])
				}
				out = append(out, where+" "+named.Obj().Name()+"."+sel.Sel.Name+" "+actor)
				return true
			})
		}
	}
	sort.Strings(out)
	return out
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// wantMembershipLedger is the whole set, by literal. Adding a line here is deciding that a new
// site may act as a principal; every line not passing `membershipActor(id)` says why.
var wantMembershipLedger = []string{
	// The implementation delegating to itself: `Redeem` hands the principal it just
	// provisioned or found to `RedeemFor`. No request identity is involved.
	"ControlInviting.Redeem ControlInviting.RedeemFor principal",
	"Server.handleInvite Inviting.Invitable membershipActor(id)",
	"Server.handleInvite Inviting.Mint membershipActor(id)",
	"Server.handleInvitePage Inviting.Invitable membershipActor(id)",
	"Server.handleInviteRevoke Inviting.Revoke membershipActor(id)",
	// EXEMPT: a provider identity with no credential behind it, so no narrowing to lose.
	"Server.handleOAuthCallback Inviting.RedeemFor principal",
	"Server.handleShare Sharing.Candidates membershipActor(id)",
	// EXEMPT: ATTRIBUTION only — the journal's `actor`. The authority is checked against the
	// narrowed `id.Auth` passed beside it.
	"Server.handleShare Sharing.Share id.Principal",
	"Server.handleSharePage Sharing.Candidates membershipActor(id)",
	// EXEMPT: ATTRIBUTION only, as for `Share`; authority is the narrowed `id.Auth`.
	"Server.handleUnshare Sharing.Unshare id.Principal",
}

// TestEveryMembershipDecisionActsAsMembershipActor pins the ledger against the real package.
//
// It fails when a site passes anything other than `membershipActor(id)` outside the named
// exemptions, when a method is used as a value, and when the set of sites grows or shrinks —
// however the receiver was reached, because the receiver is resolved by type.
func TestEveryMembershipDecisionActsAsMembershipActor(t *testing.T) {
	fset := token.NewFileSet()
	got := membershipLedger(t, fset, parseUIPackage(t, fset, nil))

	// POSITIVE CONTROL: the checker resolved the field-based calls at all. An empty or short
	// ledger would otherwise read as "nothing bypasses the helper".
	if len(got) < 9 {
		t.Fatalf("the type-based scan resolved only %d membership call(s) — the instrument is broken:\n%s",
			len(got), strings.Join(got, "\n"))
	}
	if strings.Join(got, "\n") != strings.Join(wantMembershipLedger, "\n") {
		t.Fatalf("the membership-actor ledger moved. Every actor-taking Inviting/Sharing call must pass "+
			"membershipActor(id) unless it is a named exemption; a NEW site must be added deliberately.\n"+
			"--- got\n%s\n--- want\n%s", strings.Join(got, "\n"), strings.Join(wantMembershipLedger, "\n"))
	}
}

// TestTheMembershipLedgerCanGoRED is the negative control, one arm per SHAPE that walked the
// first, spelling-based version. Each adds one synthetic file to the REAL package and asserts
// the ledger reports exactly the line that file should produce.
func TestTheMembershipLedgerCanGoRED(t *testing.T) {
	for _, tc := range []struct {
		name, src, wantLine string
	}{{
		name: "a local alias of the field",
		src: `package ui
import "github.com/ZacxDev/cairn/internal/identity"
func (s *Server) aliasBypass(id identity.Identity) {
	inv := s.inviting
	inv.Invitable(id.Principal)
}`,
		wantLine: "Server.aliasBypass Inviting.Invitable id.Principal",
	}, {
		name: "a method value",
		src: `package ui
import "github.com/ZacxDev/cairn/internal/identity"
func (s *Server) methodValueBypass(id identity.Identity) {
	f := s.inviting.Invitable
	f(id.Principal)
}`,
		wantLine: "Server.methodValueBypass Inviting.Invitable " + ledgerNotCalled,
	}, {
		name: "a helper taking the interface",
		src: `package ui
import "github.com/ZacxDev/cairn/internal/identity"
func helperBypass(sh Sharing, id identity.Identity) {
	sh.Candidates(id.Principal)
}`,
		wantLine: "helperBypass Sharing.Candidates id.Principal",
	}, {
		name: "a direct implementation value",
		src: `package ui
import "github.com/ZacxDev/cairn/internal/identity"
func directBypass(c ControlInviting, id identity.Identity) {
	c.Revoke(nil, id.Principal, "")
}`,
		wantLine: "directBypass ControlInviting.Revoke id.Principal",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			got := membershipLedger(t, fset, parseUIPackage(t, fset, map[string]string{"zz_bypass.go": tc.src}))
			found := false
			for _, line := range got {
				if line == tc.wantLine {
					found = true
				}
			}
			if !found {
				t.Fatalf("the ledger did not report %q for this shape, so a site written this way walks it:\n%s",
					tc.wantLine, strings.Join(got, "\n"))
			}
			if strings.Join(got, "\n") == strings.Join(wantMembershipLedger, "\n") {
				t.Fatal("the ledger with a bypass added still equals the wanted ledger")
			}
		})
	}
}

func astText(fset *token.FileSet, n ast.Node) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, n); err != nil {
		return "<unprintable>"
	}
	return b.String()
}
