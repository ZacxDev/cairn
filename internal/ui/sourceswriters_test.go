package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// T9 OF THE SCOPE-REFS PLAN — THE INTERNET-FACING UI'S WRITE REACH, `internal/ui`'s HALF. Two pins,
// each failing on GROW or SHRINK, each shown red inside its own test by a SYNTHETIC file injected
// beside the real ones (the negative control rides with the guard, so a walk wired to nothing cannot
// pass):
//
//  1. `internal/ui` calls ZERO package-level `os` functions that mutate the filesystem. The set is
//     read off `$(go env GOROOT)/src/os` — never `runtime.GOROOT()`, which `-trimpath` can empty — and
//     every exported package-level function there is classified below; an UNCLASSIFIED one fails, so
//     a toolchain bump that adds a mutator goes red until somebody decides. ⚠ AN INVARIANT GUARD,
//     LABELLED AS ONE: measured 0 before S4, and S4 did not change it.
//  2. `internal/ui` writes the sources journal from EXACTLY ONE site (`handleSourcesSave`'s
//     `s.sourceJournal.Set`) and calls none of `write.AppendBullet`/`ReplaceEntry`/`CreateEntry`.
//
// ⚠ WHAT THESE DO NOT SEE, stated rather than assumed away (the plan's T9 list): METHODS on `*os.File`
// and `*os.Root` (`os.OpenRoot` followed by `r.WriteFile` passes pin 1 — `OpenRoot` is refused below
// for that reason, which closes the doorway and not every other way to hold a handle); a write reached
// through ANOTHER package's function (the session store and the control file store are such, and are
// legitimately called); `cmd/cairn-ui`, which neither pin walks; and the deployment's actual mounts.
// Pin 2 is STRUCTURAL ON THE FIELD rather than type-resolved — this package's checker runs with no
// importer (`membershipledger_test.go` says why), so a call on the imported `codesrc.Journal` type
// cannot be resolved; instead the journal is reachable only through `Server.sourceJournal`, and every
// use of that field, every `codesrc.Journal` type reference and every `Config.SourceJournal` read is
// counted, so a COPY of the journal into a local (the shape that would walk around a call-site count)
// is itself a red.

// osMutators are the package-level `os` functions pin 1 refuses: every one that changes the
// filesystem, plus the two doorways to an unpinnable handle or process.
var osMutators = map[string]bool{
	"WriteFile": true, "OpenFile": true, "Create": true, "CreateTemp": true, "Rename": true,
	"Mkdir": true, "MkdirAll": true, "MkdirTemp": true, "Link": true, "Symlink": true,
	"Remove": true, "RemoveAll": true, "Truncate": true, "Chmod": true, "Chown": true,
	"Lchown": true, "Chtimes": true, "CopyFS": true,
	// A writable `*os.Root` is the shape pin 1 cannot follow (its methods), so the doorway is refused.
	"OpenRoot": true,
	// A child process can write anything; the browser surface never spawns one (clause e's concern).
	"StartProcess": true,
}

// osNonMutators are the rest, classified by hand: reads, process and environment state, and helpers.
var osNonMutators = map[string]bool{
	"Chdir": true, "Clearenv": true, "DirFS": true, "Environ": true, "Executable": true, "Exit": true,
	"Expand": true, "ExpandEnv": true, "FindProcess": true, "Getegid": true, "Getenv": true,
	"Geteuid": true, "Getgid": true, "Getgroups": true, "Getpagesize": true, "Getpid": true,
	"Getppid": true, "Getuid": true, "Getwd": true, "Hostname": true, "IsExist": true,
	"IsNotExist": true, "IsPathSeparator": true, "IsPermission": true, "IsTimeout": true,
	"LookupEnv": true, "Lstat": true, "NewFile": true, "NewSyscallError": true, "Open": true,
	"OpenInRoot": true, "Pipe": true, "ReadDir": true, "ReadFile": true, "Readlink": true,
	"SameFile": true, "Setenv": true, "Stat": true, "TempDir": true, "Unsetenv": true,
	"UserCacheDir": true, "UserConfigDir": true, "UserHomeDir": true,
}

// osExportedFuncs is every exported package-level function declared in the toolchain's `os` source.
func osExportedFuncs(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("`go env GOROOT` failed, so the os function set cannot be read: %v", err)
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), "src", "os")
	names, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range names {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.IsExported() {
				seen[fd.Name.Name] = true
			}
		}
	}
	var list []string
	for n := range seen {
		list = append(list, n)
	}
	sort.Strings(list)
	return list
}

// osMutatorCalls is every selector `<os import>.<mutator>` in the files, by position — whatever local
// name the file imports `os` under. A dot-import of `os` is itself reported, because it would hide
// every call from this walk.
func osMutatorCalls(fset *token.FileSet, files []*ast.File) []string {
	var hits []string
	for _, f := range files {
		local := map[string]bool{}
		for _, imp := range f.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); path == "os" {
				switch {
				case imp.Name == nil:
					local["os"] = true
				case imp.Name.Name == ".":
					hits = append(hits, fset.Position(imp.Pos()).String()+" dot-import of os")
				default:
					local[imp.Name.Name] = true
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && local[x.Name] && osMutators[sel.Sel.Name] {
				hits = append(hits, fset.Position(sel.Pos()).String()+" os."+sel.Sel.Name)
			}
			return true
		})
	}
	return hits
}

func TestTheUIPackageCallsNoFilesystemMutatingOSFunction(t *testing.T) {
	funcs := osExportedFuncs(t)
	if len(funcs) < 40 {
		t.Fatalf("read %d exported os functions; the GOROOT walk is not reading the os package", len(funcs))
	}
	for _, name := range funcs {
		if !osMutators[name] && !osNonMutators[name] {
			t.Errorf("os.%s is not classified. A toolchain bump added it: decide whether it mutates the "+
				"filesystem and put it in `osMutators` or `osNonMutators`", name)
		}
	}

	fset := token.NewFileSet()
	real := parseUIPackage(t, fset, nil)
	if len(real) < 20 {
		t.Fatalf("parsed %d file(s); the walk is not reading internal/ui", len(real))
	}
	if hits := osMutatorCalls(fset, real); len(hits) != 0 {
		t.Fatalf("internal/ui calls filesystem-mutating os functions — the internet-facing surface's write "+
			"reach is the four files T9 names, never a raw call in a handler: %v", hits)
	}

	// NEGATIVE CONTROLS: a raw `os.WriteFile` in a handler, an aliased import, and a dot-import are
	// each seen — so the zero above is a measurement, not a walk wired to nothing.
	for name, src := range map[string]string{
		"zz_raw.go":   "package ui\nimport \"os\"\nfunc zzRaw() { _ = os.WriteFile(\"x\", nil, 0o600) }\n",
		"zz_alias.go": "package ui\nimport xos \"os\"\nfunc zzAlias() { _ = xos.Remove(\"x\") }\n",
		"zz_dot.go":   "package ui\nimport . \"os\"\nfunc zzDot() { _ = Getenv(\"x\") }\n",
	} {
		fset := token.NewFileSet()
		files := parseUIPackage(t, fset, map[string]string{name: src})
		if hits := osMutatorCalls(fset, files); len(hits) != 1 {
			t.Errorf("negative control %s: %d hit(s), want exactly 1: %v", name, len(hits), hits)
		}
	}
}

// journalReach is pin 2's census over the files.
type journalReach struct {
	setSites     []string // `<x>.sourceJournal.Set`
	escapes      []string // `<x>.sourceJournal` not followed by .Read / .Set / .Path
	typeRefs     []string // `codesrc.Journal`
	configReads  []string // `<x>.SourceJournal`
	storeWriters []string // write.AppendBullet / ReplaceEntry / CreateEntry
}

func censusJournalReach(fset *token.FileSet, files []*ast.File) journalReach {
	var r journalReach
	for _, f := range files {
		// Parent links for selectors, so `x.sourceJournal` can be asked what it is the operand of.
		parent := map[ast.Node]ast.Node{}
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			if len(stack) > 0 {
				parent[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pos := fset.Position(sel.Pos()).String()
			switch sel.Sel.Name {
			case "sourceJournal":
				outer, ok := parent[sel].(*ast.SelectorExpr)
				switch {
				case ok && outer.X == sel && outer.Sel.Name == "Set":
					r.setSites = append(r.setSites, pos)
				case ok && outer.X == sel && (outer.Sel.Name == "Read" || outer.Sel.Name == "Path"):
				default:
					r.escapes = append(r.escapes, pos)
				}
			case "SourceJournal":
				r.configReads = append(r.configReads, pos)
			case "Journal":
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "codesrc" {
					r.typeRefs = append(r.typeRefs, pos)
				}
			case "AppendBullet", "ReplaceEntry", "CreateEntry":
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "write" {
					r.storeWriters = append(r.storeWriters, pos)
				}
			}
			return true
		})
	}
	return r
}

// The census of today's tree, as counts. 🔴 EXACT, GROW OR SHRINK: one `Set` site; the field and the
// `Config` field are the only two `codesrc.Journal` types; `New` is the only `Config.SourceJournal`
// read; nothing copies the field out; nothing writes the store.
func (r journalReach) problems() []string {
	var out []string
	if len(r.setSites) != 1 {
		out = append(out, "sourceJournal.Set sites "+strconv.Itoa(len(r.setSites))+" (want 1): "+strings.Join(r.setSites, ", "))
	}
	if len(r.escapes) != 0 {
		out = append(out, "sourceJournal used other than .Read/.Set/.Path: "+strings.Join(r.escapes, ", "))
	}
	if len(r.typeRefs) != 2 {
		out = append(out, "codesrc.Journal references "+strconv.Itoa(len(r.typeRefs))+" (want 2: the field and the Config field): "+strings.Join(r.typeRefs, ", "))
	}
	if len(r.configReads) != 1 {
		out = append(out, "Config.SourceJournal reads "+strconv.Itoa(len(r.configReads))+" (want 1, in New): "+strings.Join(r.configReads, ", "))
	}
	if len(r.storeWriters) != 0 {
		out = append(out, "store writers called: "+strings.Join(r.storeWriters, ", "))
	}
	return out
}

func TestTheUIWritesTheSourcesJournalFromOneSite(t *testing.T) {
	fset := token.NewFileSet()
	real := parseUIPackage(t, fset, nil)
	if p := censusJournalReach(fset, real).problems(); len(p) != 0 {
		t.Fatalf("the UI's write reach into the sources journal moved:\n  %s", strings.Join(p, "\n  "))
	}

	// NEGATIVE CONTROLS, each its own shape: a second Set site; a copy of the field into a local (the
	// walk-around a bare call-site count misses); a fresh journal value; a second Config read; a store
	// write. Each must be reported.
	for name, src := range map[string]string{
		"zz_second_set.go": "package ui\nfunc (s *Server) zz() { _, _ = s.sourceJournal.Set(\"a\", nil, \"\", \"b\", s.now(), nil) }\n",
		"zz_copy.go":       "package ui\nfunc (s *Server) zz() { j := s.sourceJournal; _ = j }\n",
		"zz_fresh.go":      "package ui\nimport \"github.com/ZacxDev/cairn/internal/codesrc\"\nvar zz = codesrc.Journal{Path: \"x\"}\n",
		"zz_config.go":     "package ui\nfunc zz(c Config) { _ = c.SourceJournal }\n",
		"zz_write.go":      "package ui\nimport \"github.com/ZacxDev/cairn/internal/write\"\nvar zz = write.CreateEntry\n",
	} {
		fset := token.NewFileSet()
		files := parseUIPackage(t, fset, map[string]string{name: src})
		if p := censusJournalReach(fset, files).problems(); len(p) == 0 {
			t.Errorf("negative control %s was NOT reported, so the census cannot see that shape", name)
		}
	}
}
