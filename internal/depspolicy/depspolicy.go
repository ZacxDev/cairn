// Package depspolicy is the guarantee that replaced `vendorHash = null`.
//
// 🔴 WHAT WAS LOST, STATED FIRST, BECAUSE A GUARANTEE REMOVED AND NOT REPLACED IS A
// LOSS RATHER THAN A TRADE. Until `internal/ui` existed, `go.mod` had no `require`
// block and `flake.nix` passed `vendorHash = null` to all three Go derivations.
// Together those made a new dependency a BUILD FAILURE: `buildGoModule` with a null
// vendor hash refuses a module that needs anything outside the standard library, so
// "no third-party code in the serving path" was checkable in one glance and enforced
// by nix rather than by review.
//
// Both are gone. The flake now carries a real vendor hash on every Go derivation —
// including `cairn-go` and `cairn-server-go`, which import nothing third-party and
// pay the hash anyway, because there is ONE module. The operator accepted that
// explicitly. What this package supplies in exchange is mechanical rather than
// glanceable, and it is deliberately TWO claims rather than one, because they fail
// for different reasons and neither implies the other:
//
//   - [DeclaredModules] versus the module set the repository actually carries —
//     `TestTheModuleSetIsExactlyTheAllowlist`. A module appearing is a refusal; a
//     module DISAPPEARING is also a refusal, so the allowlist cannot quietly become
//     a list of things that are no longer there.
//   - The import graph out of the two binaries that are DEPLOYED or INSTALLED —
//     `TestNoPackageTheCLIOrThePodLINKSReachesAThirdPartyModule`. The allowlist says
//     which modules exist; this says none of them reaches the pod or the CLI.
//
// 🔴 THE SECOND IS THE ONE THAT KEEPS THE POD CLEAN, AND THE FIRST CANNOT SUBSTITUTE
// FOR IT. An allowlist of one entry is satisfied by a tree in which `internal/api`
// imports that entry on every route. Only the import walk answers "is the HTML
// library in the pod's binary", and it answers it by reading what the compiler would
// read rather than by grepping for a string — a grep sees a name in a comment and
// misses an import behind an alias.
//
// # HOW THE GRAPH IS BUILT, AND WHAT IT DELIBERATELY EXCLUDES
//
// [ImportGraph] walks every directory under `cmd/` and `internal/` with
// `go/build`, which honours build constraints for the current platform and
// separates a package's imports from its TEST imports. Only the non-test imports
// are in the graph: a `_test.go` file is compiled into a test binary and never into
// the program, so counting it would refuse a test that imports a third-party
// assertion library — a refusal about nothing, which is how a gate gets deleted.
//
// ⚠ THE DIMENSION THIS PINS IS THE PLATFORM, AND IT IS PINNED RATHER THAN COVERED.
// `go/build`'s default context reads `GOOS`/`GOARCH` from the environment, so a
// file guarded by `//go:build windows` is invisible to a run on linux. Nothing in
// this module carries a platform-guarded import today, and the test asserts that
// separately — it re-walks with the constraint evaluation DISABLED and requires the
// same import set, which is what makes the platform pin a measured non-issue rather
// than an assumed one.
package depspolicy

import (
	"bufio"
	"fmt"
	"go/build"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ModulePath is this module, spelled once.
const ModulePath = "github.com/ZacxDev/cairn"

// DeclaredModules is the whole third-party module set this repository permits.
//
// 🔴 ADDING A LINE HERE IS THE DECISION, AND IT IS SUPPOSED TO BE THE HARD PART.
// A module added to `go.mod` without a line here fails
// `TestTheModuleSetIsExactlyTheAllowlist` with the "grew" message; a line here with
// no module behind it fails the same test with the "shrank" message. Neither is a
// warning and neither can be satisfied by editing only one side.
//
// ⚠ THE VERSION IS NOT PINNED HERE, AND THAT IS NOT AN OVERSIGHT. `go.sum` pins the
// version and its checksum, and `go mod verify` in CI is what proves the bytes on
// disk are the bytes that were hashed. Restating a version in this list would be a
// second spelling of a fact the lock file already owns, and the copy that goes stale
// is always the one a human maintains.
var DeclaredModules = []string{
	// The HTML renderer `internal/ui` is built on. Zero transitive dependencies of
	// its own — measured from its published `go.mod`, which carries no `require`
	// block at v1.3.0 — which is why this list has one entry rather than a tree.
	"maragu.dev/gomponents",
}

// LinkedBinaryRoots are the packages whose import closure must stay free of every
// third-party module.
//
// 🔴 `cmd/cairn-ui` IS DELIBERATELY ABSENT, AND ITS ABSENCE IS WHAT MAKES THE TEST
// ABLE TO SEE ANYTHING AT ALL. The UI binary is the one that links the dependency;
// a ban covering it would be a ban nothing could satisfy. It is used the other way
// round instead — as the POSITIVE CONTROL. A walk that reports zero third-party
// imports everywhere, including out of the binary that certainly has one, is a walk
// wired to nothing, and a zero from such a walk is indistinguishable from a pass.
var LinkedBinaryRoots = []string{
	ModulePath + "/cmd/cairn",
	ModulePath + "/cmd/cairn-server",
}

// UIBinaryRoot is the positive control's root: the one binary that MUST reach a
// third-party module.
const UIBinaryRoot = ModulePath + "/cmd/cairn-ui"

// RepoRoot is the repository root: the nearest ancestor of the working directory
// holding a `go.mod` whose module line is [ModulePath].
//
// 🔴 IT WALKS UP FROM THE WORKING DIRECTORY RATHER THAN FROM `runtime.Caller`, AND
// THAT IS A MEASURED CORRECTION RATHER THAN A PREFERENCE. The first version derived
// the root from this file's own compiled-in path, which is right on a developer
// host and WRONG inside the nix build: nixpkgs' Go builder compiles with
// `-trimpath`, so `runtime.Caller` hands back the relative
// `github.com/ZacxDev/cairn/internal/depspolicy/depspolicy.go` and every test in
// this package failed with "holds no go.mod". `go test` sets the working directory
// to the package's own source directory in BOTH environments, which is the property
// that is actually portable.
//
// 🔴 AND IT VERIFIES THE MODULE LINE RATHER THAN STOPPING AT THE FIRST `go.mod`. A
// walk that stopped at any `go.mod` would happily anchor on a vendored or nested
// module and then measure a tree nobody asked about — and report a confident clean
// result for it.
func RepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("depspolicy: the working directory is unavailable, so the repository root cannot be derived: %w", err)
	}
	start := dir
	for {
		candidate := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(candidate)
		if err == nil {
			if moduleLineOf(string(data)) == ModulePath {
				return dir, nil
			}
			return "", fmt.Errorf(
				"depspolicy: %s declares module %q, not %q — the walk anchored on the wrong module and every "+
					"result below would be about the wrong tree",
				candidate, moduleLineOf(string(data)), ModulePath)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf(
				"depspolicy: no go.mod declaring module %s was found walking up from %s, so the repository root "+
					"cannot be derived", ModulePath, start)
		}
		dir = parent
	}
}

// moduleLineOf reads the `module` directive, or "" if there is none.
func moduleLineOf(goMod string) string {
	for _, line := range strings.Split(goMod, "\n") {
		line = strings.TrimSpace(line)
		if rest, found := strings.CutPrefix(line, "module "); found {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// ModulesInGoMod is every module path `go.mod` requires, deduped and sorted.
//
// It parses rather than shells out, so the test that reads it does not depend on a
// toolchain being on PATH or on a module cache being populated — both of which a
// nix sandbox may lack and neither of which is what the claim is about.
//
// ⚠ IT COUNTS INDIRECT REQUIREMENTS TOO. An `// indirect` module is still code that
// gets fetched, hashed and potentially linked; excluding it would make the allowlist
// a statement about direct imports while the reader takes it for a statement about
// the module set.
func ModulesInGoMod(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, err
	}
	var out []string
	inBlock := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// A `//` comment can hold the word `require` — this file's own package doc
		// does — so comments are dropped before the directive is read.
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}
		if inBlock {
			if line == ")" {
				inBlock = false
				continue
			}
			if path, ok := firstField(line); ok {
				out = append(out, path)
			}
			continue
		}
		if line == "require (" {
			inBlock = true
			continue
		}
		if rest, found := strings.CutPrefix(line, "require "); found {
			if path, ok := firstField(strings.TrimSpace(rest)); ok {
				out = append(out, path)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return dedupeSorted(out), nil
}

// ModulesInGoSum is every module path `go.sum` carries, deduped and sorted.
//
// 🔴 A SECOND READER OVER A DIFFERENT FILE, ON PURPOSE. `go.mod` and `go.sum` are
// maintained by the same command and can still come apart — a hand-edited `require`
// with no matching hash, or a hash left behind by a removed requirement. Comparing
// the two against each other is a cross-check that fails differently from either
// one's comparison against the allowlist, which is the property a single parser
// cannot have. An ABSENT `go.sum` is an empty set rather than an error: that is the
// state this repository was in before the dependency, and it must compare equal to
// an empty `go.mod` requirement set rather than blowing up.
func ModulesInGoSum(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		path, ok := firstField(line)
		if !ok {
			continue
		}
		out = append(out, path)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return dedupeSorted(out), nil
}

func firstField(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", false
	}
	return fields[0], true
}

func dedupeSorted(in []string) []string {
	slices.Sort(in)
	return slices.Compact(in)
}

// IsThirdParty answers whether an import path leaves this module and the standard
// library.
//
// 🔴 THE TEST IS "DOES THE FIRST PATH ELEMENT CONTAIN A DOT", WHICH IS THE GO
// TOOLCHAIN'S OWN RULE AND NOT A HEURISTIC INVENTED HERE. No standard-library import
// path's first element contains a dot, and a module path's first element is a domain
// and always does. That is what makes this classification exact rather than a
// prefix list somebody has to maintain.
func IsThirdParty(importPath string) bool {
	if importPath == ModulePath || strings.HasPrefix(importPath, ModulePath+"/") {
		return false
	}
	first, _, _ := strings.Cut(importPath, "/")
	return strings.Contains(first, ".")
}

// ImportGraph maps every package under `cmd/` and `internal/` to its non-test
// imports.
//
// `evaluateConstraints` false re-reads every file with build constraints IGNORED,
// which is how the platform dimension this package's doc names gets measured at a
// second point instead of assumed away.
func ImportGraph(root string, evaluateConstraints bool) (map[string][]string, error) {
	ctx := build.Default
	if !evaluateConstraints {
		// `UseAllFiles` makes the context read every `.go` file in a directory
		// regardless of its `//go:build` line, which is exactly the second
		// measurement point.
		ctx.UseAllFiles = true
	}
	graph := make(map[string][]string)
	for _, top := range []string{"cmd", "internal"} {
		base := filepath.Join(root, top)
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				return nil
			}
			if name := d.Name(); path != base && (name == "testdata" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			pkg, err := ctx.ImportDir(path, 0)
			if err != nil {
				var noGo *build.NoGoError
				if ok := asNoGo(err, &noGo); ok {
					// A directory that only holds subdirectories is not a package.
					return nil
				}
				// ⚠ A MULTI-PACKAGE DIRECTORY IS ALSO REPORTED HERE WHEN
				// `UseAllFiles` IS SET, because constraint-guarded files for two
				// platforms then land in one read. It is returned rather than
				// skipped: silently dropping a directory is how a walk stops seeing
				// the package that has the offending import.
				return fmt.Errorf("depspolicy: %s: %w", path, err)
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			importPath := ModulePath + "/" + filepath.ToSlash(rel)
			graph[importPath] = dedupeSorted(append([]string(nil), pkg.Imports...))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return graph, nil
}

func asNoGo(err error, target **build.NoGoError) bool {
	if e, ok := err.(*build.NoGoError); ok {
		*target = e
		return true
	}
	return false
}

// Closure is every package reachable from root through the graph, root included.
//
// A package outside the graph — the standard library, or a third-party module — is
// recorded as reached and not descended into. The standard library is not this
// package's business, and a third-party module's own imports are irrelevant: the
// refusal fires at the EDGE that reaches it.
func Closure(graph map[string][]string, root string) map[string]bool {
	seen := map[string]bool{}
	var walk func(string)
	walk = func(pkg string) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true
		for _, imp := range graph[pkg] {
			walk(imp)
		}
	}
	walk(root)
	return seen
}

// ThirdPartyEdges is every `<importer> -> <third-party import>` inside a closure,
// sorted, so a refusal can name the edge rather than the set.
//
// 🔴 IT REPORTS THE EDGE, NOT THE PACKAGE. "The pod reaches gomponents" is not
// actionable; "internal/report imports maragu.dev/gomponents/html" is the one line
// somebody has to delete.
func ThirdPartyEdges(graph map[string][]string, closure map[string]bool) []string {
	var out []string
	for pkg := range closure {
		for _, imp := range graph[pkg] {
			if IsThirdParty(imp) {
				out = append(out, pkg+" -> "+imp)
			}
		}
	}
	return dedupeSorted(out)
}
