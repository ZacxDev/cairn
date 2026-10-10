package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

func renameFlagsFor(user, display string) *renameUserFlags {
	enabled := true
	return &renameUserFlags{enabled: &enabled, user: &user, display: &display}
}

func displayIn(t *testing.T, journal string, id control.ID) string {
	t.Helper()
	store, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatalf("opening: %v", err)
	}
	m, err := store.Model(context.Background())
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	p, ok := m.PrincipalFor(control.KindUser, id)
	if !ok {
		t.Fatalf("the journal holds no principal for %s", id)
	}
	return p.Display
}

// TestRenameUserChangesWhatTheJournalRendersThroughTheCommand: the operator path alone, over
// the state `-create-user` produces, changes the display every reader renders.
func TestRenameUserChangesWhatTheJournalRendersThroughTheCommand(t *testing.T) {
	journal, env, a, _ := aJournalWithTwoSeparateUsers(t)
	if got := displayIn(t, journal, a.User); got != "rowan@notes.example.test" {
		t.Fatalf("PRE-STATE: the fixture user displays as %q, want the email", got)
	}
	var out, errOut bytes.Buffer
	if rc := runRenameUser(env, renameFlagsFor(string(a.User), "octocat-example"), &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	if got := displayIn(t, journal, a.User); got != "octocat-example" {
		t.Errorf("after -rename-user the journal renders %q", got)
	}
	if want := "cairn-control: user-renamed user=" + string(a.User) + " display=octocat-example epoch="; !strings.HasPrefix(out.String(), want) {
		t.Errorf("stdout %q does not start %q", out.String(), want)
	}
	if !strings.Contains(errOut.String(), `now displays as "octocat-example" (was "rowan@notes.example.test")`) {
		t.Errorf("stderr does not say what changed: %s", errOut.String())
	}
}

// TestEveryRenameRefusalWritesNOTHING: each refusal carries its own fragment, exits 78, writes
// nothing to stdout, and leaves the journal's epoch where it was.
func TestEveryRenameRefusalWritesNOTHING(t *testing.T) {
	for _, tc := range []struct {
		name string
		args func(a, b control.Provisioned) *renameUserFlags
		want string
	}{
		{"no flags", func(a, b control.Provisioned) *renameUserFlags { return renameFlagsFor("", "") },
			"-rename-user-id and -rename-display-name are required"},
		{"a project id", func(a, b control.Provisioned) *renameUserFlags {
			return renameFlagsFor(string(a.Project), "octocat-example")
		}, "is not a user id"},
		{"a provider:subject spelling", func(a, b control.Provisioned) *renameUserFlags {
			return renameFlagsFor(string(b.User), "notes-idp:subject-0001")
		}, "no `:` or `@`"},
		{"a newline", func(a, b control.Provisioned) *renameUserFlags {
			return renameFlagsFor(string(b.User), "octocat\nexample")
		}, `"octocat\nexample"`},
		{"an unknown user", func(a, b control.Provisioned) *renameUserFlags {
			return renameFlagsFor("usr_nobody", "octocat-example")
		}, "holds no user with id usr_nobody"},
		{"another user's email, which the alphabet already refuses", func(a, b control.Provisioned) *renameUserFlags {
			return renameFlagsFor(string(b.User), "rowan@notes.example.test")
		}, "no `:` or `@`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			journal, env, a, b := aJournalWithTwoSeparateUsers(t)
			before, err := os.ReadFile(journal)
			if err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			if rc := runRenameUser(env, tc.args(a, b), &out, &errOut); rc != exitConfig {
				t.Fatalf("rc=%d, want %d. stderr=%s", rc, exitConfig, errOut.String())
			}
			if out.Len() != 0 {
				t.Errorf("a refusal wrote to stdout: %q", out.String())
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("the refusal does not carry %q: %s", tc.want, errOut.String())
			}
			if after, _ := os.ReadFile(journal); !bytes.Equal(before, after) {
				t.Errorf("a refused rename changed the journal")
			}
		})
	}
	// The uniqueness refusal needs one rename to have landed first.
	journal, env, a, b := aJournalWithTwoSeparateUsers(t)
	var out, errOut bytes.Buffer
	if rc := runRenameUser(env, renameFlagsFor(string(a.User), "octocat-example"), &out, &errOut); rc != 0 {
		t.Fatalf("POSITIVE CONTROL: the first rename failed: %s", errOut.String())
	}
	before, _ := os.ReadFile(journal)
	out.Reset()
	errOut.Reset()
	if rc := runRenameUser(env, renameFlagsFor(string(b.User), "OCTOCAT-example"), &out, &errOut); rc != exitConfig {
		t.Fatalf("a second user took a name differing only in case: rc=%d", rc)
	}
	if !strings.Contains(errOut.String(), "already displays as") {
		t.Errorf("the uniqueness refusal does not say so: %s", errOut.String())
	}
	if after, _ := os.ReadFile(journal); !bytes.Equal(before, after) {
		t.Errorf("a refused rename changed the journal")
	}
}

// TestCreateUserWritesTheDisplayName: `-create-user -display-name` reaches the row and the
// stdout line names it; without the flag the line is unchanged.
func TestCreateUserWritesTheDisplayName(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "control.journal")
	env := map[string]string{EnvControlJournal: journal}
	f := flagsFor("notes-idp", "subject-0001", "rowan@notes.example.test", "quarry", "")
	*f.display = "octocat-example"
	var out, errOut bytes.Buffer
	if rc := runCreateUser(env, t.TempDir(), f, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	if !strings.HasSuffix(strings.TrimSpace(out.String()), " display=octocat-example") {
		t.Errorf("the created line does not end with the display: %q", out.String())
	}
	id := control.ID(strings.Fields(strings.TrimPrefix(out.String(), "cairn-control: created user="))[0])
	if got := displayIn(t, journal, id); got != "octocat-example" {
		t.Errorf("the created user renders %q", got)
	}

	out.Reset()
	errOut.Reset()
	g := flagsFor("notes-idp", "subject-0002", "wren@notes.example.test", "harbour", "")
	if rc := runCreateUser(env, t.TempDir(), g, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	if strings.Contains(out.String(), "display=") {
		t.Errorf("a user created WITHOUT a display name printed one: %q", out.String())
	}

	out.Reset()
	errOut.Reset()
	h := flagsFor("notes-idp", "subject-0003", "", "harbour", "")
	*h.display = "two words"
	if rc := runCreateUser(env, t.TempDir(), h, &out, &errOut); rc != exitConfig {
		t.Fatalf("a refused display name was accepted: rc=%d", rc)
	}
}

// TestTheBinaryActuallyDispatchesRenameUser runs the real `main`: everything above calls
// `runRenameUser` directly and is blind to a mode never added to the dispatch or the ledger.
func TestTheBinaryActuallyDispatchesRenameUser(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	journal, _, a, _ := aJournalWithTwoSeparateUsers(t)
	run := func(args ...string) (int, string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		child := exec.CommandContext(ctx, self, args...)
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
	code, body := run("-rename-user", "-rename-user-id", string(a.User), "-rename-display-name", "octocat-example")
	if code != 0 || !strings.Contains(body, "cairn-control: user-renamed user="+string(a.User)) {
		t.Fatalf("`-rename-user` exited %d:\n%s", code, body)
	}
	if got := displayIn(t, journal, a.User); got != "octocat-example" {
		t.Errorf("the child reported success and the journal renders %q", got)
	}
	// Two modes at once is a refusal, and the rename did NOT happen.
	code, body = run("-routes", "-rename-user", "-rename-user-id", string(a.User), "-rename-display-name", "hubot-example")
	if code != exitConfig || strings.Contains(body, "user-renamed user=") {
		t.Fatalf("`-routes -rename-user` exited %d:\n%s", code, body)
	}
	if got := displayIn(t, journal, a.User); got != "octocat-example" {
		t.Errorf("a refused two-mode command renamed the user to %q", got)
	}
}
