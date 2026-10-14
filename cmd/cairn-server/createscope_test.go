package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// createScopeFlagsFor builds the mode's flags the way an operator's command line would.
func createScopeFlagsFor(project, name string) *createScopeFlags {
	enabled := true
	return &createScopeFlags{enabled: &enabled, project: &project, name: &name}
}

func scopeNamed(m control.Model, name string) (control.Scope, bool) {
	for _, sc := range m.Scopes {
		if sc.DisplayName == name {
			return sc, true
		}
	}
	return control.Scope{}, false
}

// TestCreateScopeWritesOneRecordAndSaysWhatItDidNOTDo is the command's whole success
// contract: the stable stdout line, the record in the journal, the three things it says it
// did not do — and that the first of them is TRUE, measured against a real store root.
func TestCreateScopeWritesOneRecordAndSaysWhatItDidNOTDo(t *testing.T) {
	journal, env, a, _ := aJournalWithTwoSeparateUsers(t)
	root := t.TempDir()
	before := modelOf(t, journal)
	var out, errOut bytes.Buffer

	if rc := runCreateScope(env, root, createScopeFlagsFor(string(a.Project), "quarry-inbox"), &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d, stderr=%s", rc, errOut.String())
	}

	// 🔴 THE WHOLE LINE, ANCHORED: this is what a script greps, so its shape is the contract.
	line := regexp.MustCompile(`^cairn-control: scope-created scope=(scp_[A-Za-z0-9]+) name=quarry-inbox project=` +
		regexp.QuoteMeta(string(a.Project)) + ` epoch=([0-9]+)\n$`)
	found := line.FindStringSubmatch(out.String())
	if found == nil {
		t.Fatalf("stdout is not the one machine-readable line: %q", out.String())
	}

	// 🔴 RE-READ THE JOURNAL: a command that printed a correct line and appended nothing
	// satisfies everything above.
	after := modelOf(t, journal)
	sc, held := after.Scopes[control.ID(found[1])]
	if !held || sc.DisplayName != "quarry-inbox" || sc.ProjectID != a.Project {
		t.Fatalf("the journal does not hold the scope stdout names: held=%v %+v", held, sc)
	}
	if after.Epoch != before.Epoch+1 {
		t.Fatalf("the journal grew by %d event(s), not one", after.Epoch-before.Epoch)
	}

	said := errOut.String()
	for _, want := range []string{
		"created NO directory",
		// The WHOLE audience, not only members: project grants expand over new scopes.
		"is now reached by every member of project " + string(a.Project) + " at their role AND by " +
			"every holder of a live grant on that PROJECT",
		"-issue-credential -narrow-scopes " + found[1],
		"picks this up within " + refreshInterval.String(),
	} {
		if !strings.Contains(said, want) {
			t.Errorf("stderr does not say %q:\n%s", want, said)
		}
	}
	// "created NO directory" is a claim about this program; measure it.
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("the command says it creates no directory and the store root now holds %v (err=%v)", entries, err)
	}
	if strings.Contains(said, "already exists as") {
		t.Errorf("the on-disk warning fired over an EMPTY store root:\n%s", said)
	}
}

// TestEveryCreateScopeRefusalWritesNOTHING covers each refusal by a fragment unique to it,
// and asserts exit 78, an empty stdout and a byte-identical journal for every one.
func TestEveryCreateScopeRefusalWritesNOTHING(t *testing.T) {
	journal, env, a, b := aJournalWithTwoSeparateUsers(t)
	cases := []struct {
		name, project, scope string
		want                 []string
	}{
		{"no project", "", "quarry-inbox", []string{"-scope-project is required"}},
		{"no name", string(a.Project), "", []string{"-scope-name is required"}},
		{"neither", "", "", []string{"-scope-project and -scope-name are required"}},
		{"an id this journal does not hold", "prj_notinthisjournal", "quarry-inbox",
			[]string{`holds no project with id "prj_notinthisjournal"`, "takes an ID",
				// The remedy: both projects it DOES hold, id then name.
				string(a.Project) + ` ("quarry")`, string(b.Project) + ` ("quarry")`}},
		{"a project NAME, which is what -create-user's -project takes", "quarry", "quarry-inbox",
			[]string{`holds no project with id "quarry"`, "not a name"}},
		{"a name another scope already folds to", string(b.Project), "Quarry_Notes",
			[]string{"already taken", `folds to "quarry-notes"`}},
		{"a name outside the class", string(a.Project), "quarry.inbox",
			[]string{"carries a character outside"}},
		{"two names in one flag", string(a.Project), "quarry-inbox,quarry-outbox",
			[]string{"carries a character outside"}},
		{"a name that folds to nothing", string(a.Project), "___", []string{"folds to nothing"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, err := os.ReadFile(journal)
			if err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			rc := runCreateScope(env, t.TempDir(), createScopeFlagsFor(tc.project, tc.scope), &out, &errOut)
			if rc != exitConfig {
				t.Fatalf("rc=%d, want %d; stderr=%s", rc, exitConfig, errOut.String())
			}
			if out.Len() != 0 {
				t.Errorf("a refusal wrote to stdout, where a script reads success: %q", out.String())
			}
			for _, want := range tc.want {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("the refusal does not say %q:\n%s", want, errOut.String())
				}
			}
			after, err := os.ReadFile(journal)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("a refused command changed the journal:\n%s", after[len(before):])
			}
		})
	}
}

// TestCreateScopeWithNoJournalConfiguredRefusesRatherThanDefaulting — unset and
// set-to-empty are both "not set", and neither may fall through to a path.
func TestCreateScopeWithNoJournalConfiguredRefusesRatherThanDefaulting(t *testing.T) {
	for _, env := range []map[string]string{{}, {EnvControlJournal: ""}} {
		var out, errOut bytes.Buffer
		rc := runCreateScope(env, t.TempDir(), createScopeFlagsFor("prj_any", "quarry-inbox"), &out, &errOut)
		if rc != exitConfig || out.Len() != 0 {
			t.Fatalf("rc=%d stdout=%q", rc, out.String())
		}
		if want := "-create-scope needs a control journal and $" + EnvControlJournal + " is not set"; !strings.Contains(errOut.String(), want) {
			t.Fatalf("the refusal does not say %q:\n%s", want, errOut.String())
		}
	}
}

// TestCreateScopeCallsOutADirectoryThatAlreadyAnswersToTheName: the on-disk warning
// `-create-user` gives, reached from this mode — matched by FOLD, exit 0, the record written.
func TestCreateScopeCallsOutADirectoryThatAlreadyAnswersToTheName(t *testing.T) {
	journal, env, a, _ := aJournalWithTwoSeparateUsers(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Quarry_Inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if rc := runCreateScope(env, root, createScopeFlagsFor(string(a.Project), "quarry-inbox"), &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d — this is a warning, not a refusal:\n%s", rc, errOut.String())
	}
	for _, want := range []string{`WARNING scope "quarry-inbox" already exists as`, "Quarry_Inbox", "no undo"} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("the warning does not carry %q:\n%s", want, errOut.String())
		}
	}
	if _, held := scopeNamed(modelOf(t, journal), "quarry-inbox"); !held {
		t.Fatal("the warning replaced the write")
	}

	// 🔴 A REFUSED RE-RUN SAYS NOTHING ABOUT HANDING THE DIRECTORY OVER — the warning used to
	// run before `CreateScope` and printed for every refusal. Both refusals are reached with
	// the directory still on disk, so only the ORDER can keep them quiet.
	for _, flags := range []*createScopeFlags{
		createScopeFlagsFor(string(a.Project), "Quarry_Inbox"), // now taken, by fold
		createScopeFlagsFor("prj_notinthisjournal", "quarry-inbox"),
	} {
		var rout, rerr bytes.Buffer
		if rc := runCreateScope(env, root, flags, &rout, &rerr); rc != exitConfig {
			t.Fatalf("the re-run was not refused: rc=%d\n%s", rc, rerr.String())
		}
		if strings.Contains(rerr.String(), "already exists as") {
			t.Errorf("a REFUSED run still warned that it hands the directory over:\n%s", rerr.String())
		}
	}
}

// TestTheBinaryActuallyDispatchesCreateScope runs the real `main`: everything above calls
// `runCreateScope` directly and is blind to a mode never added to the dispatch or the ledger.
func TestTheBinaryActuallyDispatchesCreateScope(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	journal, _, a, _ := aJournalWithTwoSeparateUsers(t)
	root := t.TempDir()
	run := func(args ...string) (int, string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		child := exec.CommandContext(ctx, self, append([]string{"-store", root}, args...)...)
		child.Env = []string{
			runServerEnv + "=1",
			testRefreshEnv + "=" + testRefreshPeriod.String(),
			EnvControlJournal + "=" + journal,
		}
		body, _ := child.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("`%v` did not exit within 20s — it fell through to serving:\n%s", args, body)
		}
		return child.ProcessState.ExitCode(), string(body)
	}
	code, body := run("-create-scope", "-scope-project", string(a.Project), "-scope-name", "quarry-inbox")
	if code != 0 || !strings.Contains(body, "cairn-control: scope-created scope=scp_") {
		t.Fatalf("`-create-scope` exited %d:\n%s", code, body)
	}
	if _, held := scopeNamed(modelOf(t, journal), "quarry-inbox"); !held {
		t.Error("the child reported success and the journal holds no such scope")
	}
	// `-h` renders the FIRST backquoted word of a flag's help as its placeholder; an earlier
	// draft printed `-scope-name _` and `-create-scope scope-created`.
	_, help := run("-h")
	for _, want := range []string{"  -scope-name name\n", "  -scope-project prj_…\n", "  -create-scope\n"} {
		if !strings.Contains(help, want) {
			t.Errorf("`-h` does not render %q:\n%s", want, help)
		}
	}
	// Two modes at once is a refusal, and the scope was NOT created.
	code, body = run("-routes", "-create-scope", "-scope-project", string(a.Project), "-scope-name", "quarry-outbox")
	if code != exitConfig || strings.Contains(body, "scope-created scope=") {
		t.Fatalf("`-routes -create-scope` exited %d:\n%s", code, body)
	}
	if _, held := scopeNamed(modelOf(t, journal), "quarry-outbox"); held {
		t.Error("a refused two-mode command created the scope")
	}
}
