package depspolicy

import (
	"slices"
	"strings"
	"testing"
)

// setDiff is `a \ b`, and it is a helper rather than an inline loop because the
// defect this whole file exists to avoid is a comparison that can only fail in one
// direction. Both directions are computed from the same function, so a change that
// breaks one breaks both and is visible.
func setDiff(a, b []string) []string {
	var out []string
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

// TestTheModuleSetIsExactlyTheAllowlist is gate part (i).
//
// 🔴 IT FAILS WHEN THE SET GROWS *AND* WHEN IT SHRINKS, AND THE TWO ARE SEPARATE
// COMPARISONS WITH SEPARATE MESSAGES. This repository has a measured case of a
// guard whose docstring claimed it "fails when the set GROWS" and structurally
// could not: it compared two sides for a constant that was absent from BOTH, so the
// constant was in neither side of the comparison and the guard read as coverage
// while providing none. A single `reflect.DeepEqual` would fail in both directions
// but say only "not equal", which is the same defect one level up — the reader
// cannot tell which way it moved, and the natural fix is to edit the allowlist to
// match reality, which is exactly what the guard exists to prevent somebody doing
// without deciding.
//
// The instrument is validated before its verdict is read: the allowlist and the
// measured set are both required to be non-empty, because two empty sets compare
// equal and a parser wired to nothing produces one of them.
func TestTheModuleSetIsExactlyTheAllowlist(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("the repository root could not be derived: %v", err)
	}

	fromMod, err := ModulesInGoMod(root)
	if err != nil {
		t.Fatalf("go.mod could not be read: %v", err)
	}
	fromSum, err := ModulesInGoSum(root)
	if err != nil {
		t.Fatalf("go.sum could not be read: %v", err)
	}

	// INSTRUMENT CONTROL, before any verdict. A reassuring zero here is
	// indistinguishable from a parser that read nothing.
	if len(DeclaredModules) == 0 {
		t.Fatal("DeclaredModules is EMPTY, so every comparison below is between two empty sets and passes vacuously. " +
			"If this repository genuinely has no dependencies again, delete this test and restore `vendorHash = null` " +
			"in flake.nix — that is the stronger guarantee and this one exists only because it was given up.")
	}
	if len(fromMod) == 0 {
		t.Fatal("the go.mod parser found NO require directives while DeclaredModules is non-empty. " +
			"Either the requirement was removed (see the message above) or ModulesInGoMod is reading nothing — " +
			"check it before reading any verdict from it.")
	}

	// CROSS-CHECK, between two readers over two different files that fail
	// differently. This is not the allowlist comparison; it is the check that the
	// lock file and the requirement list have not come apart.
	if grew := setDiff(fromSum, fromMod); len(grew) > 0 {
		t.Errorf("go.sum carries %d module(s) go.mod does not require: %v\n"+
			"A hash with no requirement behind it means the two files were edited apart. Run `go mod tidy`.",
			len(grew), grew)
	}
	if shrank := setDiff(fromMod, fromSum); len(shrank) > 0 {
		t.Errorf("go.mod requires %d module(s) go.sum has no hash for: %v\n"+
			"An unhashed requirement is a module whose bytes nothing verifies. Run `go mod download`.",
			len(shrank), shrank)
	}

	// 🔴 THE ALLOWLIST COMPARISON, IN BOTH DIRECTIONS, WITH A MESSAGE PER DIRECTION.
	if grew := setDiff(fromMod, DeclaredModules); len(grew) > 0 {
		t.Errorf("THE MODULE SET GREW: %d module(s) are required that the allowlist does not name: %v\n"+
			"`vendorHash = null` used to make this a build failure and it no longer does. Adding a module is now "+
			"a decision taken HERE, in internal/depspolicy.DeclaredModules, and adding the line is the point at "+
			"which somebody has to think about whether the pod and the CLI are still clean — which is what "+
			"TestNoPackageTheCLIOrThePodLINKSReachesAThirdPartyModule measures separately.",
			len(grew), grew)
	}
	if shrank := setDiff(DeclaredModules, fromMod); len(shrank) > 0 {
		t.Errorf("THE MODULE SET SHRANK: the allowlist names %d module(s) go.mod does not require: %v\n"+
			"An allowlist entry with nothing behind it is dead weight that makes the list read as wider than the "+
			"tree is. If the dependency was genuinely dropped, drop the line here in the same change — and if it "+
			"was the LAST one, restore `vendorHash = null` in flake.nix rather than keeping a weaker gate.",
			len(shrank), shrank)
	}

	t.Logf("module set: go.mod=%v go.sum=%v allowlist=%v", fromMod, fromSum, DeclaredModules)
}

// TestNoPackageTheCLIOrThePodLINKSReachesAThirdPartyModule is gate part (ii), and
// it is the one that keeps the pod clean.
//
// 🔴 IT REPORTS A PAIR, NEVER A BARE ZERO. The positive control is `cmd/cairn-ui`,
// which MUST reach a third-party module; if that count is zero the walk is wired to
// nothing and the zeroes for the pod and the CLI mean nothing either. The negative
// side is the two binaries that ship, which must reach none.
//
// 🔴 AND IT PROVES THE WALK GOES DEEP RATHER THAN ONE HOP, BY A COMPUTED PROPERTY
// RATHER THAN A NAMED PACKAGE. Each root's closure must contain a module-internal
// package that is not one of that root's OWN imports — which a closure stopping at
// depth 1 cannot satisfy, and which no refactor can quietly empty. An earlier draft
// named `internal/store` here and was wrong on its face: `cmd/cairn-server` imports
// it directly, so the control would have been satisfied by exactly the shallow walk
// it existed to refuse.
func TestNoPackageTheCLIOrThePodLINKSReachesAThirdPartyModule(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("the repository root could not be derived: %v", err)
	}
	graph, err := ImportGraph(root, true)
	if err != nil {
		t.Fatalf("the import graph could not be built: %v", err)
	}
	if len(graph) == 0 {
		t.Fatal("the import graph is EMPTY, so every ban below passes vacuously. The walk found no package at all " +
			"under cmd/ or internal/ — that is an instrument failure, not a clean tree.")
	}

	// POSITIVE CONTROL FIRST, so the zeroes below are readable.
	uiClosure := Closure(graph, UIBinaryRoot)
	uiEdges := ThirdPartyEdges(graph, uiClosure)
	if len(uiEdges) == 0 {
		t.Fatalf("POSITIVE CONTROL FAILED: %s reaches NO third-party import, and it is the binary that links the "+
			"HTML renderer. A walk that cannot see the one dependency this repository has cannot vouch for the "+
			"absence of any other, so the results below are withheld rather than reported as clean.", UIBinaryRoot)
	}

	// DEPTH CONTROL: the transitive walk must actually be transitive.
	//
	// 🔴 IT IS COMPUTED RATHER THAN A NAMED PACKAGE, BECAUSE A NAMED ONE GOES STALE
	// IN THE DIRECTION THAT EMPTIES IT. The first draft named `internal/store` as a
	// package both binaries reach only indirectly, and `cmd/cairn-server` imports it
	// DIRECTLY — so the control would have been satisfied by a one-hop closure while
	// reading as proof of a deep one. What is asserted instead is the property: the
	// closure contains a module-internal package that is neither the root nor one of
	// the root's own imports, which cannot be true of a walk that stopped at depth 1.
	for _, rootPkg := range LinkedBinaryRoots {
		closure := Closure(graph, rootPkg)
		var indirect []string
		for pkg := range closure {
			if pkg == rootPkg || slices.Contains(graph[rootPkg], pkg) {
				continue
			}
			if strings.HasPrefix(pkg, ModulePath+"/") {
				indirect = append(indirect, pkg)
			}
		}
		if len(indirect) == 0 {
			t.Fatalf("DEPTH CONTROL FAILED: every module-internal package in %s's closure is one of its OWN "+
				"imports, so the walk proves nothing beyond depth 1. A closure that shallow would pass the ban "+
				"below while inspecting almost nothing — check Closure before reading any verdict from it.", rootPkg)
		}
		slices.Sort(indirect)
		t.Logf("DEPTH CONTROL %s: %d package(s) reached INDIRECTLY, e.g. %s", rootPkg, len(indirect), indirect[0])
	}

	// THE BAN.
	for _, rootPkg := range LinkedBinaryRoots {
		closure := Closure(graph, rootPkg)
		edges := ThirdPartyEdges(graph, closure)
		if len(edges) > 0 {
			t.Errorf("THE IMPORT BAN FAILED for %s: %d third-party import edge(s) inside its closure:\n  %s\n"+
				"This binary is deployed (the pod) or installed by consumers (the CLI), and neither may link code "+
				"from outside the standard library. The HTML renderer belongs to %s and to nothing that %s reaches. "+
				"If the shared package genuinely needs it, the fix is to move the rendering into internal/ui, not to "+
				"widen this ban.",
				rootPkg, len(edges), strings.Join(edges, "\n  "), UIBinaryRoot, rootPkg)
		}
		t.Logf("%s: %d package(s) in closure, %d third-party edge(s)", rootPkg, len(closure), len(edges))
	}
	t.Logf("POSITIVE CONTROL %s: %d package(s) in closure, %d third-party edge(s): %v",
		UIBinaryRoot, len(uiClosure), len(uiEdges), uiEdges)
}

// TestTheImportGraphDoesNotDependOnTheBuILDPLATFORM measures the one dimension
// `ImportGraph`'s default context pins, at a second point, instead of assuming it
// away.
//
// 🔴 A SUITE WHOSE CONFIG PINS A DIMENSION IS STRUCTURALLY BLIND TO THAT DIMENSION'S
// BUGS. `go/build`'s default context evaluates `//go:build` lines for the current
// GOOS/GOARCH, so an import reached only on another platform is invisible to the ban
// above — and a `//go:build windows` file importing the HTML renderer into
// `internal/report` would pass every run of this suite on linux. Re-walking with
// constraint evaluation DISABLED reads every file regardless of its build line; the
// two graphs agreeing is what makes the pin a measured non-issue.
//
// ⚠ IF THIS EVER FAILS, THE ANSWER IS NOT TO DELETE IT. A genuine platform-guarded
// import means the ban above has a blind spot and the ban is what needs widening.
func TestTheImportGraphDoesNotDependOnTheBuILDPLATFORM(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("the repository root could not be derived: %v", err)
	}
	evaluated, err := ImportGraph(root, true)
	if err != nil {
		t.Fatalf("the constraint-evaluated graph could not be built: %v", err)
	}
	all, err := ImportGraph(root, false)
	if err != nil {
		t.Fatalf("the constraint-ignoring graph could not be built: %v", err)
	}

	var differing []string
	for pkg, imports := range all {
		if !slices.Equal(imports, evaluated[pkg]) {
			differing = append(differing, pkg+": with constraints "+strings.Join(evaluated[pkg], ",")+
				" / ignoring them "+strings.Join(imports, ","))
		}
	}
	for pkg := range evaluated {
		if _, present := all[pkg]; !present {
			differing = append(differing, pkg+": present only in the constraint-evaluated walk")
		}
	}
	if len(differing) > 0 {
		t.Errorf("the import set DEPENDS ON THE BUILD PLATFORM in %d package(s):\n  %s\n"+
			"TestNoPackageTheCLIOrThePodLINKSReachesAThirdPartyModule runs on the current platform only, so a "+
			"platform-guarded import is a hole in it. Widen that ban to walk every GOOS this repository supports "+
			"rather than removing this measurement.",
			len(differing), strings.Join(differing, "\n  "))
	}
	t.Logf("%d package(s) walked; import sets identical with and without build-constraint evaluation", len(all))
}
