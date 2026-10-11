package racegate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pkg writes a throwaway package directory: a calm production file, a concurrent helper, and two
// exempt-able tests. Each case below edits ONE file and must draw its own refusal.
func pkg(t *testing.T, edits map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"p.go": "package p\n\n// A comment saying go func() { } is not concurrency.\nfunc Calm() int { return 1 }\n",
		"p_test.go": "package p\n\nimport \"testing\"\n\nfunc spread() { go Calm() }\n\n" +
			"func TestA(t *testing.T) { Calm() }\n\nfunc TestB(t *testing.T) { Calm() }\n\nfunc TestUsesSpread(t *testing.T) { spread() }\n",
	}
	for k, v := range edits {
		files[k] = v
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestProblemsNamesEachRefusal(t *testing.T) {
	ex, conc := []string{"TestA", "TestB"}, []string{"spread"}
	if p := Problems(pkg(t, nil), ex, conc); len(p) != 0 {
		t.Fatalf("a sound package refused: %v", p)
	}
	testFile := func(bodyA string) map[string]string {
		return map[string]string{"p_test.go": "package p\n\nimport \"testing\"\n\nfunc spread() { go Calm() }\n\n" +
			"func TestA(t *testing.T) { " + bodyA + " }\n\nfunc TestB(t *testing.T) {}\n\nfunc TestUsesSpread(t *testing.T) { spread() }\n"}
	}
	for _, c := range []struct {
		name  string
		edits map[string]string
		ex    []string
		conc  []string
		want  string
	}{
		{"goroutine in an exempt body", testFile("go Calm()"), ex, conc, "EXEMPT TEST TestA starts a goroutine"},
		{"Parallel in an exempt body", testFile("t.Parallel()"), ex, conc, "EXEMPT TEST TestA calls Parallel()"},
		{"concurrent helper caller exempted", nil, []string{"TestA", "TestUsesSpread"}, conc, "EXEMPT TEST TestUsesSpread calls the concurrent helper spread"},
		{"production go statement", map[string]string{"p.go": "package p\n\nfunc Calm() int { go func() {}(); return 1 }\n"}, ex, conc, "PREMISE BROKEN: production code has a `go` statement"},
		{"production channel", map[string]string{"p.go": "package p\n\nfunc Calm() int { c := make(chan int, 1); c <- 1; return <-c }\n"}, ex, conc, "PREMISE BROKEN: production code has a channel send"},
		{"production sync import", map[string]string{"p.go": "package p\n\nimport \"sync\"\n\nvar mu sync.Mutex\n\nfunc Calm() int { return 1 }\n"}, ex, conc, "PREMISE BROKEN: production file"},
		{"helper no longer concurrent", map[string]string{"p_test.go": "package p\n\nimport \"testing\"\n\nfunc spread() { Calm() }\n\nfunc TestA(t *testing.T) {}\n\nfunc TestB(t *testing.T) {}\n"}, ex, conc, "CONCURRENT HELPER spread is missing or no longer has a `go` statement"},
		{"helper missing", nil, ex, []string{"gone"}, "CONCURRENT HELPER gone is missing"},
		{"no helper declared", nil, ex, nil, "no concurrent function declared"},
		{"stale exempt name", nil, []string{"TestA", "TestGone"}, conc, "STALE: TestGone is exempt from -race but is not a top-level test function"},
		{"malformed list", nil, []string{"TestA.*"}, conc, "is not a top-level Test identifier"},
	} {
		got := strings.Join(Problems(pkg(t, c.edits), c.ex, c.conc), "\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want it to contain %q", c.name, got, c.want)
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
		race, only      bool
		preset          fakeFlags
		wantRun, wantSk string
		wantErr         string
	}{
		{name: "race skips the list", race: true, wantSk: "^(TestA|TestB)$"},
		{name: "exempt-only runs the list", only: true, wantRun: "^(TestA|TestB)$"},
		{name: "plain run touches nothing"},
		{name: "a caller's -skip is left alone", race: true, preset: fakeFlags{"test.skip": "^$"}, wantSk: "^$"},
		{name: "exempt-only under race refuses", race: true, only: true, wantErr: "under -race"},
		{name: "exempt-only with -run refuses", only: true, preset: fakeFlags{"test.run": "X"}, wantRun: "X", wantErr: "with -run"},
	} {
		fl := fakeFlags{}
		for k, v := range c.preset {
			fl[k] = v
		}
		_, err := plan(c.race, c.only, ex, fl)
		if (err != nil) != (c.wantErr != "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: err=%v, want %q", c.name, err, c.wantErr)
		}
		if fl["test.run"] != c.wantRun || fl["test.skip"] != c.wantSk {
			t.Errorf("%s: run=%q skip=%q, want run=%q skip=%q", c.name, fl["test.run"], fl["test.skip"], c.wantRun, c.wantSk)
		}
	}
}
