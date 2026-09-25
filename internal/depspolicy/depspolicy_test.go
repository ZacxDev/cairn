package depspolicy

import (
	"os"
	"os/exec"
	"path/filepath"
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

// TestTheNestedModuleSetIsExactlyTheAllowlist closes the deferral the package doc retracts.
//
// 🔴 IT FAILS ON GROW *OR* SHRINK, IN BOTH DIMENSIONS — the set of nested module DIRECTORIES, and
// each one's third-party module set. Grow is the obvious direction; shrink matters because an
// allowlist that quietly becomes a list of things that are no longer there reads as coverage while
// governing nothing, which is the failure `TestTheModuleSetIsExactlyTheAllowlist` exists for one
// level up.
//
// 🔴 AND IT REFUSES WHEN THE FILES ARE ABSENT RATHER THAN TOLERATING IT. These tests run inside the
// nix derivations, whose `src` is `onlyGo` — an ALLOWLIST that excluded `uiaudit/` entirely until
// `uiaudit/go.mod` and `uiaudit/go.sum` were added as named rows. Without those rows the walk finds
// zero nested modules and the natural failure message is "the set SHRANK", which is red for a
// reason that has nothing to do with the tree. A `t.Skip` would be worse: a skip nobody counts is a
// pass, and this is the only thing that observes the escape's boundary. So the refusal names the
// filter, which is where the fix is.
func TestTheNestedModuleSetIsExactlyTheAllowlist(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("the repository root could not be derived: %v", err)
	}

	found, err := NestedModuleDirs(root)
	if err != nil {
		t.Fatalf("walking for nested go.mod files: %v", err)
	}

	want := append([]string(nil), DeclaredNestedModules...)
	slices.Sort(want)
	if !slices.Equal(found, want) {
		// The absent-file case gets its own sentence, because its remedy is in `flake.nix` and
		// not in this file — and because "the set shrank" is a true but useless thing to be told
		// when the reason is that the source filter never shipped the file.
		for _, dir := range want {
			if _, statErr := os.Stat(filepath.Join(root, dir, "go.mod")); statErr != nil {
				t.Fatalf("%s/go.mod is not present in this tree (%v).\n"+
					"If this is a nix build, `flake.nix`'s `onlyGo` filter is an ALLOWLIST and must carry "+
					"`%s/go.mod` and `%s/go.sum` as named rows. This test REFUSES rather than skipping, "+
					"because a comparison against an absent operand reports SAME rather than MISSING.",
					dir, statErr, dir, dir)
			}
		}
		t.Fatalf("the nested-module DIRECTORY set does not match the allowlist.\n  walked:   %v\n  declared: %v\n"+
			"A directory appearing is a decision — a second nested module escapes the allowlist, the import "+
			"ban, the `ok` floor and every nix derivation, all of which the package doc spells out. A "+
			"directory DISAPPEARING is also a refusal, so this cannot become a list of things that are "+
			"no longer there.", found, want)
	}

	for _, dir := range want {
		t.Run(dir, func(t *testing.T) {
			modPath := filepath.Join(root, dir)
			fromMod, err := ModulesInGoMod(modPath)
			if err != nil {
				t.Fatalf("%s/go.mod could not be read: %v", dir, err)
			}
			fromSum, err := ModulesInGoSum(modPath)
			if err != nil {
				t.Fatalf("%s/go.sum could not be read: %v — `onlyGo` must carry it as a named row too", dir, err)
			}

			deps, ok := NestedModuleAllowlist[dir]
			if !ok || len(deps.GoMod) == 0 || len(deps.GoSum) == 0 {
				t.Fatalf("%s is in DeclaredNestedModules with no complete entry in NestedModuleAllowlist; "+
					"an empty allowlist is satisfied by any dependency set, which is coverage that governs nothing", dir)
			}

			// 🔴 BOTH LOCK FILES, AGAINST THEIR OWN DECLARED SETS, BECAUSE THEY DIFFER FOR A
			// STRUCTURAL REASON. `go.mod` is what the author required; `go.sum` records every
			// module in the GRAPH, including optional dependencies of a dependency. Comparing
			// both against one list is a gate that cannot be satisfied — see [NestedModuleDeps].
			// Reading only `go.mod` would miss a TRANSITIVE addition, which is the shape a
			// routine `go get -u` produces and what the CI toolchain assertion cannot see.
			for _, c := range []struct {
				file     string
				got      []string
				declared []string
			}{
				{"go.mod", thirdPartyOnly(fromMod), deps.GoMod},
				{"go.sum", thirdPartyOnly(fromSum), deps.GoSum},
			} {
				want := append([]string(nil), c.declared...)
				slices.Sort(want)
				if !slices.Equal(c.got, want) {
					t.Errorf("%s/%s's third-party module set does not match its declared list.\n"+
						"  in the file: %v\n  declared:    %v\n"+
						"  only in the file: %v\n  only declared:    %v\n"+
						"A module APPEARING is a decision nobody reviewed; a module DISAPPEARING means the "+
						"list has become a record of things that are no longer there.",
						dir, c.file, c.got, want, setDiff(c.got, want), setDiff(want, c.got))
				}
			}
		})
	}
}

// thirdPartyOnly is the comparison form: third-party modules only, sorted, duplicates collapsed.
//
// 🔴 [IsThirdParty] IS APPLIED, WHICH IS WHAT DROPS THE PARENT MODULE. `uiaudit/go.mod` requires
// `github.com/ZacxDev/cairn` through a `replace` onto this repository — that is not a third party,
// and listing it in an allowlist of third-party dependencies would make the ledger say something it
// does not mean. `go.sum` also carries two lines per module (the zip and the `/go.mod`), so a raw
// list from it double-counts.
func thirdPartyOnly(in []string) []string {
	out := make([]string, 0, len(in))
	for _, m := range in {
		if IsThirdParty(m) {
			out = append(out, m)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// TestNestedModuleDirsActuallyWALKS is the positive control on the walk, and it closes a mutant
// that SURVIVED the first battery.
//
// 🔴 THE DIRECTORY COMPARISON IN THE TEST ABOVE IS SELF-REFERENTIAL UNLESS SOMETHING PROVES THE
// WALK READS THE DISK. Measured: replacing [NestedModuleDirs]' body with `return
// DeclaredNestedModules, nil` left that test GREEN — the ledger was compared against itself, the
// absent-file branch became unreachable, and a second nested module would have been invisible. That
// is the "reads as coverage while providing none" shape this package exists to refuse, reached
// inside the package itself.
//
// So this drives the walk over a SYNTHETIC tree whose answer cannot come from the ledger: two
// nested modules with names nothing in this repository uses, a root `go.mod` that must be excluded,
// a `testdata` module that must be skipped, and a `go.mod`-less directory that must not appear.
func TestNestedModuleDirsActuallyWALKS(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{
		"go.mod",                  // the verified root — excluded, `DeclaredModules` governs it
		"alpha/go.mod",            // a nested module
		"beta/gamma/go.mod",       // a nested module two levels down
		"testdata/fixture/go.mod", // inside testdata — skipped, a fixture module may live there
		"delta/not-a-module.txt",  // no go.mod — must not appear
	} {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("module example.invalid/x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := NestedModuleDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "beta/gamma"}
	if !slices.Equal(got, want) {
		t.Fatalf("the walk returned %v, want %v.\n"+
			"If this returned %v it is not reading the disk at all — it is echoing the ledger, which makes "+
			"TestTheNestedModuleSetIsExactlyTheAllowlist a tautology and a second nested module invisible.",
			got, want, DeclaredNestedModules)
	}

	// 🔴 AND THE ANSWER MUST NOT BE THE LEDGER'S. If the two ever coincide this control stops
	// discriminating, so it is asserted rather than assumed — the synthetic names are chosen so
	// that they cannot.
	if slices.Equal(got, DeclaredNestedModules) {
		t.Fatal("the synthetic tree's answer equals DeclaredNestedModules, so this control cannot tell a " +
			"real walk from a stub; pick directory names this repository does not use")
	}
}

// TestAnUNTRACKEDNestedModuleIsIGNOREDButATRACKEDOneIsNOT is the regression guard for a
// dev-host-only permanently-red gate.
//
// 🔴 RED AT BASE, AND ONLY WHERE A HUMAN RUNS IT. The first [NestedModuleDirs] walked the disk
// behind a three-name denylist, honouring neither `.gitignore` nor untracked-ness. Agent worktrees
// live under `.claude/worktrees/<name>/`, each carrying `uiaudit/go.mod`, so in the base clone
// `go test ./...` — the command `AGENTS.md` documents — failed for any session with a live worktree,
// saying "a directory appearing is a decision" when nothing had been decided. CI and all three nix
// tiers were unaffected because `onlyGo` excludes `.claude/`, which is what made it the worst
// category: invisible to every gate, red in front of every developer.
//
// 🔴 AND THE HALF THAT MATTERS MOST IS THE SECOND SUBTEST. A fix that ignored untracked files could
// trivially go blind to the thing the ledger exists for. `TestNestedModuleDirsActuallyWALKS` cannot
// catch that — its synthetic tree is not a git repository at all, so it exercises the fallback and
// never the git path. This drives a real `git init`, so both answers come from git.
func TestAnUNTRACKEDNestedModuleIsIGNOREDButATRACKEDOneIsNOT(t *testing.T) {
	// 🔴 TWO TIERS, AND THE FIRST DRAFT OF THIS GUARD BROKE THE NIX BUILD. It refused outright when
	// git was absent, on the correct principle that a skip nobody counts is a pass. But the nix
	// derivations' check phase has NO git and cannot have one without adding a build input for a
	// single test — and in that environment git is not the authority anyway: the source is the
	// `onlyGo`-filtered tree, so `NestedModuleDirs`' fallback is the right code path there and
	// `TestNestedModuleDirsActuallyWALKS` is what exercises it. Refusing there made three derivations
	// red for a reason with nothing to do with the tree.
	//
	// So the tier that CAN run this must not skip it, and the tier that cannot must. That is the same
	// shape `tests/test_go_client_ledgers.py` already uses: it refuses on a skip in the `go` job
	// because the `tests` job has no Go toolchain. `CAIRN_GIT_TESTS_REQUIRED` is the explicit
	// statement of which tier is which, set in the `go` CI job — so a skip in the environment that
	// was supposed to measure this is a FAILURE, and a skip in the sandbox is a fact about the
	// sandbox.
	if _, err := exec.LookPath("git"); err != nil {
		if os.Getenv("CAIRN_GIT_TESTS_REQUIRED") != "" {
			t.Fatalf("CAIRN_GIT_TESTS_REQUIRED is set but git is absent: this is the only test of the "+
				"tracked-file path, and this tier is the one that is supposed to measure it (%v)", err)
		}
		t.Skipf("no git, so the tracked-file path cannot be measured here — expected inside a nix "+
			"derivation, where the `onlyGo`-filtered source makes the fallback path correct and "+
			"`TestNestedModuleDirsActuallyWALKS` covers it. 🔴 A skip here is only acceptable because "+
			"the `go` CI job sets CAIRN_GIT_TESTS_REQUIRED and therefore CANNOT skip it (%v)", err)
	}

	root := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	gitRun("init", "-q")
	write("go.mod", "module example.invalid/root\n")
	write("tracked/go.mod", "module example.invalid/root/tracked\n")
	gitRun("add", "go.mod", "tracked/go.mod")
	gitRun("commit", "-q", "-m", "root and one tracked nested module")

	// The shape that broke the dev host: an untracked nested module, in a directory whose name this
	// function must NOT need to know.
	write(".claude/worktrees/some-agent/uiaudit/go.mod", "module example.invalid/agent\n")
	// And one that is also a git root, which is what a real worktree looks like.
	write("vendored/checkout/.git", "gitdir: /nowhere\n")
	write("vendored/checkout/go.mod", "module example.invalid/vendored\n")

	got, err := NestedModuleDirs(root)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"tracked"}
	if !slices.Equal(got, want) {
		t.Fatalf("NestedModuleDirs returned %v, want %v.\n"+
			"  If %q is in there, an UNTRACKED module is being counted — that is the dev-host failure: a "+
			"stray checkout under a directory this function cannot be expected to name turns `go test "+
			"./...` red for every developer while every CI tier stays green.\n"+
			"  If %q is MISSING, the fix has gone blind to a TRACKED module, which is the thing the "+
			"whole ledger exists to notice.",
			got, want, ".claude/worktrees/some-agent/uiaudit", "tracked")
	}

	// 🔴 THE POSITIVE CONTROL ON THE CONTROL: track the previously-untracked module and it must
	// appear. Otherwise this test would pass against an implementation that simply ignored
	// everything outside a hardcoded set.
	gitRun("add", "-f", ".claude/worktrees/some-agent/uiaudit/go.mod")
	gitRun("commit", "-q", "-m", "track it deliberately")
	got, err = NestedModuleDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{".claude/worktrees/some-agent/uiaudit", "tracked"}
	if !slices.Equal(got, want) {
		t.Fatalf("after tracking it deliberately, NestedModuleDirs returned %v, want %v — the "+
			"distinction being drawn is not tracked-vs-untracked but something else", got, want)
	}
	t.Logf("untracked ignored, tracked counted, and a nested git root skipped: %v", got)
}
