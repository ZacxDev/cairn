package racegate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// world writes a throwaway module: go.mod plus files keyed by path relative to its root.
func world(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files["go.mod"] = "module example.test/m\n\ngo 1.25\n"
	for p, body := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// pkgA is one package whose tests cover every edge kind the analysis claims to follow, each
// paired with a test that must NOT be classified concurrent, so a classifier that says "yes" to
// everything fails as surely as one that says "no".
const pkgA = `package a

import "testing"

type worker struct{}

func (worker) run()  { go func() {}() }
func (worker) calm() {}

func spawn()        { go func() {}() }
func middle()       { spawn() }
func quiet()        {}
func use(f func())  { f() }
func litOnly()      { _ = func() { go quiet() } }

var spawnVar = func() { go quiet() }

func TestDirect(t *testing.T)       { go quiet() }
func TestTwoHops(t *testing.T)      { middle() }
func TestMethod(t *testing.T)       { var w worker; w.run() }
func TestMethodCalm(t *testing.T)   { var w worker; w.calm() }
func TestFuncValue(t *testing.T)    { use(spawn) }
func TestFuncValueCalm(t *testing.T){ use(quiet) }
func TestParallel(t *testing.T)     { t.Parallel() }
func TestVar(t *testing.T)          { spawnVar() }
func TestUncalledLit(t *testing.T)  { litOnly() }
func TestPlain(t *testing.T)        { quiet() }
func helperNotATest(t *testing.T)   {}
`

func TestTheAnalysisFollowsEveryEdgeKindAndFlagsOnlyThose(t *testing.T) {
	root := world(t, map[string]string{"a/a_test.go": pkgA})
	a, err := Analyse(filepath.Join(root, "a"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"TestDirect": true, "TestTwoHops": true, "TestMethod": true, "TestFuncValue": true,
		"TestParallel": true, "TestVar": true, "TestUncalledLit": true,
		"TestMethodCalm": false, "TestFuncValueCalm": false, "TestPlain": false,
	}
	if len(a.Tests()) != len(want) {
		t.Fatalf("tests found: %v, want exactly %d (helperNotATest is not one)", a.Tests(), len(want))
	}
	for name, conc := range want {
		chain, got := a.Concurrent(name)
		if got != conc {
			t.Errorf("%s: concurrent=%v, want %v (chain %v)", name, got, conc, chain)
		}
	}
	chain, _ := a.Concurrent("TestTwoHops")
	if got := strings.Join(chain, " -> "); !strings.HasPrefix(got, "TestTwoHops -> middle -> spawn -> a `go` statement at a_test.go:") {
		t.Errorf("the witness chain is %q", got)
	}
}

func TestInitAndTestMainAreReachedByEveryTest(t *testing.T) {
	for _, hook := range []string{"func init() { go func() {}() }", "func TestMain(m *testing.M) { go func() {}(); m.Run() }"} {
		root := world(t, map[string]string{"a/a_test.go": "package a\n\nimport \"testing\"\n\n" + hook + "\n\nfunc TestPlain(t *testing.T) {}\n"})
		a, err := Analyse(filepath.Join(root, "a"))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := a.Concurrent("TestPlain"); !ok {
			t.Errorf("with %q, TestPlain was not classified concurrent", hook)
		}
	}
}

// The AMBIENT rule, at both depths and in both directions: an imported module package with a
// goroutine — in itself or in its own module import — makes every test concurrent; one without
// does not.
func TestAnImportedModulePackageIsAmbient(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		conc  bool
	}{
		"direct": {map[string]string{"dep/dep.go": "package dep\n\nfunc Go() { go func() {}() }\n"}, true},
		"transitive": {map[string]string{
			"dep/dep.go":   "package dep\n\nimport \"example.test/m/deep\"\n\nfunc Go() { deep.F() }\n",
			"deep/deep.go": "package deep\n\nfunc F() { go func() {}() }\n",
		}, true},
		"calm": {map[string]string{"dep/dep.go": "package dep\n\nfunc Go() {}\n"}, false},
		"only its TEST file goes": {map[string]string{
			"dep/dep.go":      "package dep\n\nfunc Go() {}\n",
			"dep/dep_test.go": "package dep\n\nfunc g() { go func() {}() }\n",
		}, false},
	}
	for name, c := range cases {
		c.files["a/a_test.go"] = "package a\n\nimport (\n\t\"testing\"\n\t\"example.test/m/dep\"\n)\n\nfunc TestPlain(t *testing.T) {}\n\nvar _ = dep.Go\n"
		a, err := Analyse(filepath.Join(world(t, c.files), "a"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, got := a.Concurrent("TestPlain"); got != c.conc {
			t.Errorf("%s: concurrent=%v, want %v", name, got, c.conc)
		}
	}
}

func TestProblemsNamesEachRefusal(t *testing.T) {
	root := world(t, map[string]string{"a/a_test.go": pkgA})
	dir := filepath.Join(root, "a")
	ctl := []string{"TestDirect"}
	if p := Problems(dir, []string{"TestPlain", "TestMethodCalm"}, ctl); len(p) != 0 {
		t.Fatalf("a sound list refused: %v", p)
	}
	for _, c := range []struct {
		exempt, ctl []string
		want        string
	}{
		{[]string{"TestPlain", "TestTwoHops"}, ctl, "TestTwoHops is exempt from -race but CAN REACH A GOROUTINE: TestTwoHops -> middle -> spawn"},
		{[]string{"TestGone"}, ctl, "TestGone is exempt from -race but is not a top-level test function"},
		{[]string{"TestPlain"}, nil, "no positive control given"},
		{[]string{"TestPlain"}, []string{"TestPlain"}, "POSITIVE CONTROL: TestPlain must be classified concurrent"},
		{[]string{"TestPlain", "TestPlain"}, ctl, "is listed twice"},
		{[]string{"TestA.*"}, ctl, "is not a top-level Test identifier"},
		{nil, ctl, "the exempt list is empty"},
	} {
		got := strings.Join(Problems(dir, c.exempt, c.ctl), "\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("exempt=%v ctl=%v: got %q, want it to contain %q", c.exempt, c.ctl, got, c.want)
		}
	}
}

type fakeFlags map[string]string

func (f fakeFlags) get(n string) string   { return f[n] }
func (f fakeFlags) set(n, v string) error { f[n] = v; return nil }

func TestPlanSetsExactlyOneFlagPerMode(t *testing.T) {
	ex := []string{"TestA", "TestB"}
	for _, c := range []struct {
		name            string
		race, only, all bool
		preset          fakeFlags
		wantRun, wantSk string
		wantErr         string
	}{
		{name: "race skips the list", race: true, wantSk: "^(TestA|TestB)$"},
		{name: "exempt-only runs the list", only: true, wantRun: "^(TestA|TestB)$"},
		{name: "plain run touches nothing"},
		{name: "race with all touches nothing", race: true, all: true},
		{name: "a caller's -skip is left alone", race: true, preset: fakeFlags{"test.skip": "X"}, wantSk: "X"},
		{name: "exempt-only under race refuses", race: true, only: true, wantErr: "under -race"},
		{name: "exempt-only with -run refuses", only: true, preset: fakeFlags{"test.run": "X"}, wantRun: "X", wantErr: "with -run"},
	} {
		fl := fakeFlags{}
		for k, v := range c.preset {
			fl[k] = v
		}
		_, err := plan(c.race, c.only, c.all, ex, fl)
		if (err != nil) != (c.wantErr != "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: err=%v, want %q", c.name, err, c.wantErr)
		}
		if fl["test.run"] != c.wantRun || fl["test.skip"] != c.wantSk {
			t.Errorf("%s: run=%q skip=%q, want run=%q skip=%q", c.name, fl["test.run"], fl["test.skip"], c.wantRun, c.wantSk)
		}
	}
}
