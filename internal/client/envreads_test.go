package client

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The environment-read ledger's guard (`envreads.go`; scope-mail plan, S3 — round 5 🟡4a-iii).
//
// PROPERTY: every environment read in `internal/client` and `cmd/cairn` names a constant in
// [EnvReads]; the ledger fails on GROW (a read it does not name) or SHRINK (a name nothing reads).
//
// MECHANISM: the two packages' non-test sources are PARSED — read, not trusted — and
//
//  1. every reference to a process-environment primitive (`os.Getenv`, `os.LookupEnv`,
//     `os.Environ`, `os.ExpandEnv`, `os.Expand`, `os.UserHomeDir`, `os.UserCacheDir`,
//     `os.UserConfigDir`, `syscall.Getenv`, `syscall.Environ`, and `envalias`'s process-environment
//     readers) must sit at one of the DECLARED sites in `envPrimitiveSites`, an exact set — so a new
//     `os.Getenv("…")` anywhere is red by its site, and a declared site that disappears is red too;
//     `os/user` may not be imported at all;
//  2. every CALL of a forwarder — `getenv(x)`, `pickConfig(_, _, x)`, `lookup(_)(x)` — must pass a
//     package constant whose VALUE is a ledger name (a literal, or a constant outside the ledger, is
//     red), except inside a declared forwarder passing its own parameter on; and `getenv` may be
//     used as a VALUE (not called) only where `envForwarderValues` declares;
//  3. the set of ledger names those calls read, plus `HOME` when `homeDir` is called, must EQUAL
//     the ledger — so a name nothing reads any more is red;
//  4. the ledger [EnvReads] returns at run time must equal the composite literal in its source,
//     which is what rule 3 compares against — so the list cannot be edited in one place.
//
// ⚠ SCOPE: these two packages. Reads in packages they import are not parsed here; the one that
// exists (`internal/hostid`'s display label) is named in `envreads.go` and folded into
// [EnvReadsWithAliases] by reference. And `runGit` passing the whole environment to `git` is a
// declared site, not a ledger name — see `envreads.go` for why.

// envPrimitiveSites is every (file:function, primitive) where a process-environment primitive may
// be referenced, exactly.
var envPrimitiveSites = map[string]bool{
	"internal/client/envreads.go:getenv envalias.OSValue": true,
	"internal/client/envreads.go:homeDir os.UserHomeDir":  true,
	"internal/client/reposcope.go:runGit os.Environ":      true,
	"internal/client/cli.go:Run envalias.OSDeprecations":  true,
}

// envForwarderParams is where a forwarder may pass its OWN parameter on (rule 2's exception).
var envForwarderParams = map[string]string{
	"internal/client/transport.go:pickConfig": "name",
}

// envForwarderValues is where `getenv` may be used as a value rather than called: `lookup` returns
// it as the process-environment getter, and its CALLERS are checked as `lookup(_)(x)`.
var envForwarderValues = map[string]bool{
	"internal/client/instances.go:lookup": true,
}

var envPrimitives = map[string]map[string]bool{
	"os": {"Getenv": true, "LookupEnv": true, "Environ": true, "ExpandEnv": true, "Expand": true,
		"UserHomeDir": true, "UserCacheDir": true, "UserConfigDir": true},
	"syscall": {"Getenv": true, "Environ": true},
	"github.com/ZacxDev/cairn/internal/envalias": {"OSValue": true, "OSValueOr": true, "Environ": true,
		"OSDeprecations": true},
}

// clientSources is the two packages' non-test Go files, keyed by repo-relative path.
func clientSources(t *testing.T) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, dir := range []string{".", "../../cmd/cairn"} {
		matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range matches {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			rel := "internal/client/" + filepath.Base(path)
			if dir != "." {
				rel = "cmd/cairn/" + filepath.Base(path)
			}
			out[rel] = data
		}
	}
	if len(out) < 20 || out["internal/client/envreads.go"] == nil || out["cmd/cairn/main.go"] == nil {
		t.Fatalf("read %d source files — the guard would be compared against too little", len(out))
	}
	return out
}

// envReadProblems is rules 1–4 over `files`, and the ledger it read out of the source.
func envReadProblems(files map[string][]byte) (problems []string, ledger []string) {
	fset := token.NewFileSet()
	parsed := map[string]*ast.File{}
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	consts := map[string]string{} // package-level string constants of internal/client
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, files[name], parser.ParseComments)
		if err != nil {
			return []string{"parse " + name + ": " + err.Error()}, nil
		}
		parsed[name] = f
		if !strings.HasPrefix(name, "internal/client/") {
			continue
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, id := range vs.Names {
					if i < len(vs.Values) {
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if v, err := strconv.Unquote(lit.Value); err == nil {
								consts[id.Name] = v
							}
						}
					}
				}
			}
		}
	}

	// Rule 4's source half: the composite literal EnvReads() is built from.
	if f := parsed["internal/client/envreads.go"]; f != nil {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != "EnvReads" {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				cl, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				for _, elt := range cl.Elts {
					id, ok := elt.(*ast.Ident)
					if !ok {
						problems = append(problems, "EnvReads' list holds a non-constant element; it must name ledger constants")
						continue
					}
					v, ok := consts[id.Name]
					if !ok {
						problems = append(problems, "EnvReads names "+id.Name+", which is not a package string constant")
						continue
					}
					ledger = append(ledger, v)
				}
				return false
			})
		}
	}
	sort.Strings(ledger)
	inLedger := map[string]bool{}
	for _, v := range ledger {
		inLedger[v] = true
	}

	foundSites := map[string]bool{}
	read := map[string]bool{}
	for _, name := range names {
		f := parsed[name]
		// local name → path. Seeded with the default names, so a primitive is recognised in a file
		// that has not (yet) imported its package — a mutant that adds the call and forgets the
		// import must still be read as the read it would be once it compiles.
		imports := map[string]string{"os": "os", "syscall": "syscall",
			"envalias": "github.com/ZacxDev/cairn/internal/envalias"}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			local := filepath.Base(path)
			if imp.Name != nil {
				local = imp.Name.Name
			}
			imports[local] = path
			if path == "os/user" {
				problems = append(problems, name+" imports os/user, which reads the environment behind the ledger's back")
			}
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			site := name + ":" + fd.Name.Name
			params := map[string]bool{}
			for _, field := range fd.Type.Params.List {
				for _, id := range field.Names {
					params[id.Name] = true
				}
			}
			called := map[*ast.Ident]bool{}
			constArg := func(what string, arg ast.Expr) {
				id, ok := arg.(*ast.Ident)
				if !ok {
					problems = append(problems, site+": "+what+" is handed a non-identifier argument; it must be a ledger constant")
					return
				}
				if fwd, ok := envForwarderParams[site]; ok && id.Name == fwd && params[id.Name] {
					return
				}
				v, ok := consts[id.Name]
				switch {
				case !ok:
					problems = append(problems, site+": "+what+"("+id.Name+") — "+id.Name+" is not a package string constant")
				case !inLedger[v]:
					problems = append(problems, site+": "+what+"("+id.Name+") reads "+v+", which the ledger (EnvReads) does not name")
				default:
					read[v] = true
				}
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.SelectorExpr:
					if pkg, ok := x.X.(*ast.Ident); ok {
						if path, ok := imports[pkg.Name]; ok && envPrimitives[path][x.Sel.Name] {
							key := site + " " + filepath.Base(path) + "." + x.Sel.Name
							foundSites[key] = true
							if !envPrimitiveSites[key] {
								problems = append(problems, key+" is not a declared environment read site — name the variable in the ledger and read it through getenv")
							}
						}
					}
				case *ast.CallExpr:
					switch fun := x.Fun.(type) {
					case *ast.Ident:
						switch fun.Name {
						case "getenv":
							called[fun] = true
							if len(x.Args) == 1 {
								constArg("getenv", x.Args[0])
							}
						case "pickConfig":
							if len(x.Args) == 3 {
								constArg("pickConfig", x.Args[2])
							}
						case "homeDir":
							read[HomeEnv] = true
						}
					case *ast.CallExpr:
						if id, ok := fun.Fun.(*ast.Ident); ok && id.Name == "lookup" && len(x.Args) == 1 {
							constArg("lookup(…)", x.Args[0])
						}
					}
				}
				return true
			})
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "getenv" && !called[id] && !envForwarderValues[site] {
					problems = append(problems, site+" uses getenv as a VALUE; its eventual argument cannot be checked there")
				}
				return true
			})
		}
	}
	for key := range envPrimitiveSites {
		if !foundSites[key] {
			problems = append(problems, "declared site "+key+" is gone — shrink envPrimitiveSites with it")
		}
	}
	for _, v := range ledger {
		if !read[v] {
			problems = append(problems, "ledger name "+v+" has NO read site — remove it from EnvReads, or restore its read")
		}
	}
	sort.Strings(problems)
	return problems, ledger
}

// TestEveryEnvironmentReadIsInTheLedger — rules 1–4 over the real sources.
func TestEveryEnvironmentReadIsInTheLedger(t *testing.T) {
	problems, ledger := envReadProblems(clientSources(t))
	for _, p := range problems {
		t.Error(p)
	}
	if !reflect.DeepEqual(ledger, EnvReads()) {
		t.Errorf("EnvReads() = %v at run time, but its source literal reads %v", EnvReads(), ledger)
	}
	// POSITIVE CONTROL: the discovery read the names it is meant to; a parser that found nothing
	// would agree with an empty ledger.
	want := []string{"CAIRN_CONFIG", "CAIRN_MIRROR_ROOT", "CAIRN_ROUTES", "CAIRN_TOKEN", "CAIRN_UI_TOKEN",
		"CAIRN_UI_URL", "CAIRN_URL", "HOME", "XDG_STATE_HOME"}
	if !reflect.DeepEqual(EnvReads(), want) {
		t.Fatalf("EnvReads() = %v, want the literal %v — move this list with the ledger, deliberately", EnvReads(), want)
	}
}

// TestTheEnvReadLedgerCatchesItsMutants — the plan's required mutants, applied to a COPY of the
// sources in memory, each required to make the guard report its OWN problem.
func TestTheEnvReadLedgerCatchesItsMutants(t *testing.T) {
	real := clientSources(t)
	for _, m := range []struct {
		name, file, old, new, want string
	}{
		{"a new os.Getenv with a literal", "internal/client/cli.go",
			"func MirrorRoot() string {", "func peek() string { return os.Getenv(\"CAIRN_SOMETHING\") }\n\nfunc MirrorRoot() string {",
			"internal/client/cli.go:peek os.Getenv is not a declared environment read site"},
		{"getenv handed a literal", "internal/client/cli.go",
			"getenv(MirrorRootEnv)", "getenv(\"CAIRN_MIRROR_ROOT\")",
			"getenv is handed a non-identifier argument"},
		{"getenv handed a constant outside the ledger", "internal/client/cli.go",
			"getenv(MirrorRootEnv)", "getenv(Remedy)",
			"which the ledger (EnvReads) does not name"},
		{"a ledger entry with no read site", "internal/client/cli.go",
			"getenv(MirrorRootEnv)", "\"\"",
			"ledger name CAIRN_MIRROR_ROOT has NO read site"},
		{"a ledger name added with no read", "internal/client/envreads.go",
			"StateHomeEnv, HomeEnv}", "StateHomeEnv, HomeEnv, SyncStamp}",
			"ledger name .sync-stamp has NO read site"},
		{"getenv smuggled out as a value", "internal/client/cli.go",
			"func MirrorRoot() string {", "func leak() func(string) string { return getenv }\n\nfunc MirrorRoot() string {",
			"internal/client/cli.go:leak uses getenv as a VALUE"},
		{"os.UserHomeDir outside homeDir", "internal/client/readstore.go",
			"home, err := homeDir()\n\tif err != nil {\n\t\t// The oracle's", "home, err := os.UserHomeDir()\n\tif err != nil {\n\t\t// The oracle's",
			"internal/client/readstore.go:DefaultCacheRoot os.UserHomeDir is not a declared environment read site"},
		{"os/user imported", "internal/client/envreads.go",
			"import (\n", "import (\n\t_ \"os/user\"\n",
			"imports os/user"},
		{"a read in cmd/cairn", "cmd/cairn/main.go",
			"func main() {", "func main() {\n\t_ = os.Getenv(\"CAIRN_URL\")",
			"cmd/cairn/main.go:main os.Getenv is not a declared environment read site"},
	} {
		files := map[string][]byte{}
		for k, v := range real {
			files[k] = v
		}
		src := string(files[m.file])
		if strings.Count(src, m.old) != 1 {
			t.Fatalf("%s: the anchor occurs %d times in %s", m.name, strings.Count(src, m.old), m.file)
		}
		files[m.file] = []byte(strings.Replace(src, m.old, m.new, 1))
		problems, _ := envReadProblems(files)
		if !strings.Contains(strings.Join(problems, "\n"), m.want) {
			t.Errorf("mutant %q SURVIVED: want a problem containing %q, got %q", m.name, m.want, problems)
		}
	}
}

// TestEnvReadsWithAliasesCoversTheDeprecatedSpellings — the decoy set is the ledger plus every
// alias it resolves, plus hostid's display-label names: the three aliased ledger names bring their
// old spellings, and nothing else does.
func TestEnvReadsWithAliasesCoversTheDeprecatedSpellings(t *testing.T) {
	got := map[string]bool{}
	for _, n := range EnvReadsWithAliases() {
		got[n] = true
	}
	for _, n := range append(EnvReads(), "SUBSYSTEM_STORE_CONFIG", "SUBSYSTEM_STORE_URL", "SUBSYSTEM_STORE_TOKEN", "CAIRN_HOST") {
		if !got[n] {
			t.Errorf("EnvReadsWithAliases lacks %s", n)
		}
	}
	if len(got) != len(EnvReads())+3+3 {
		t.Errorf("EnvReadsWithAliases has %d names: %v", len(got), EnvReadsWithAliases())
	}
}
