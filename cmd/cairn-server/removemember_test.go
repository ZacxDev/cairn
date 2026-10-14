package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// removeMemberFlagsFor builds the mode's flags the way an operator's command line would.
func removeMemberFlagsFor(project, user, role string) *removeMemberFlags {
	enabled := true
	return &removeMemberFlags{enabled: &enabled, project: &project, user: &user, role: &role}
}

// aJournalWithACoMember is `aJournalWithTwoSeparateUsers` after the one `-set-member` an
// operator would run: b is a member of a's project, and still owns their own.
func aJournalWithACoMember(t *testing.T) (journal string, env map[string]string, a, b control.Provisioned) {
	t.Helper()
	journal, env, a, b = aJournalWithTwoSeparateUsers(t)
	var out, errOut bytes.Buffer
	if rc := runSetMember(env, setMemberFlagsFor(string(a.Project), string(b.User), "member"), &out, &errOut); rc != 0 {
		t.Fatalf("the fixture's -set-member failed: rc=%d %s", rc, errOut.String())
	}
	return journal, env, a, b
}

func appendTo(t *testing.T, journal string, events ...control.Event) {
	t.Helper()
	store, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), events...); err != nil {
		t.Fatalf("appending the fixture: %v", err)
	}
}

// TestRemoveMemberWritesOneRecordAndReportsWhatItLeft is the command's success contract, in
// the world that has residue: a grant naming the user on the project's scope and a live
// credential. The stdout line is anchored whole; each stderr report line is matched whole too,
// because a script greps these for the ids to act on.
func TestRemoveMemberWritesOneRecordAndReportsWhatItLeft(t *testing.T) {
	journal, env, a, b := aJournalWithACoMember(t)
	scope := a.Scopes[0].ID
	at := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	appendTo(t, journal, control.Event{
		Kind: control.EventGranted, At: at, GrantID: "grt_quarryshare",
		SubjectKind: control.KindUser, SubjectID: b.User,
		ObjectKind: control.ObjectScope, ObjectID: scope, Verbs: control.NewVerbSet(control.VerbRead),
	})
	store, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := control.IssueCredential(context.Background(), store, control.NewCredential{
		SubjectKind: control.KindUser, SubjectID: b.User, Label: "wren laptop", At: at,
	})
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}
	before := modelOf(t, journal)

	var out, errOut bytes.Buffer
	if rc := runRemoveMember(env, removeMemberFlagsFor(string(a.Project), string(b.User), ""), &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d, stderr=%s", rc, errOut.String())
	}

	line := regexp.MustCompile(`^cairn-control: member-removed project=` + regexp.QuoteMeta(string(a.Project)) +
		` user=` + regexp.QuoteMeta(string(b.User)) + ` role=member epoch=([0-9]+)\n$`)
	found := line.FindStringSubmatch(out.String())
	if found == nil {
		t.Fatalf("stdout is not the one machine-readable line: %q", out.String())
	}
	after := modelOf(t, journal)
	if _, in := after.Memberships[a.Project][b.User]; in {
		t.Fatal("stdout reported a removal and the journal still holds the membership")
	}
	if after.Epoch != before.Epoch+1 || found[1] != itoa(after.Epoch) {
		t.Fatalf("epoch %d -> %d, stdout says %s: want one event and the line to quote it", before.Epoch, after.Epoch, found[1])
	}
	if !after.Grants["grt_quarryshare"].Live() || !after.Credentials[cred.Credential].Live() {
		t.Fatal("the removal revoked a grant or a credential — it must report them, not take them")
	}

	said := errOut.String()
	for _, want := range []string{
		"subsystem-store-api: " + string(b.User) + " STILL REACHES 1 of project " + string(a.Project) +
			"'s scopes through 1 live grant(s) this command did NOT revoke: quarry-notes (" + string(scope) + ") read.",
		"\nsubsystem-store-api: NOT REVOKED grant=grt_quarryshare subject=user:" + string(b.User) +
			" object=scope:" + string(scope) + " verbs=read (names them directly)\n",
		"\nsubsystem-store-api: STILL AUTHENTICATES credential=" + string(cred.Credential) + " label=\"wren laptop\"\n",
		"-revoke-credential -credential <id>",
		"revoked NO grant and NO credential",
		"picks this up within " + refreshInterval.String(),
	} {
		if !strings.Contains(said, want) {
			t.Errorf("stderr does not say %q:\n%s", want, said)
		}
	}
	if strings.Contains(said, "reaches NONE") || strings.Contains(said, "holds no live credential") {
		t.Errorf("stderr reports BOTH residue and its absence:\n%s", said)
	}
}

// TestRemoveMemberSaysSoWhenItLeftNothing — the absence is a sentence, not a silence: an
// empty report and an unprinted one are otherwise the same stderr.
func TestRemoveMemberSaysSoWhenItLeftNothing(t *testing.T) {
	_, env, a, b := aJournalWithACoMember(t)
	var out, errOut bytes.Buffer
	if rc := runRemoveMember(env, removeMemberFlagsFor(string(a.Project), string(b.User), ""), &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d, stderr=%s", rc, errOut.String())
	}
	for _, want := range []string{
		string(b.User) + " now reaches NONE of project " + string(a.Project) + "'s scopes",
		string(b.User) + " holds no live credential",
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr does not say %q:\n%s", want, errOut.String())
		}
	}
	if strings.Contains(errOut.String(), "NOT REVOKED") || strings.Contains(errOut.String(), "STILL AUTHENTICATES") {
		t.Errorf("a removal with no residue reported some:\n%s", errOut.String())
	}
}

// TestEveryRemoveMemberRefusalWritesNOTHING covers each refusal by a fragment unique to it,
// and asserts exit 78, an empty stdout and a byte-identical journal for every one.
func TestEveryRemoveMemberRefusalWritesNOTHING(t *testing.T) {
	journal, env, a, b := aJournalWithACoMember(t)
	cases := []struct {
		name                string
		project, user, role string
		want                string
	}{
		{"no flags", "", "", "", "-member-project and -member-user are required"},
		{"no user", string(a.Project), "", "", "-member-user is required"},
		{"no project", "", string(b.User), "", "-member-project is required"},
		{"a role, which reads as a demotion", string(a.Project), string(b.User), "member", "a removal confers no role"},
		{"the two ids swapped", string(b.User), string(a.Project), "", "the two flags are the wrong way round"},
		{"a project id in the user flag", string(a.Project), string(b.Project), "", "is not a user id"},
		{"a project the journal does not hold", "prj_nowhere", string(b.User), "", "holds no project with id prj_nowhere"},
		{"a user the journal does not hold", string(a.Project), "usr_nobody", "", "holds no user with id usr_nobody"},
		{"somebody who is not a member", string(b.Project), string(a.User), "", "there is nothing to remove"},
		{"the only owner", string(a.Project), string(a.User), "", "so removing them from it leaves a project"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, err := os.ReadFile(journal)
			if err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			rc := runRemoveMember(env, removeMemberFlagsFor(tc.project, tc.user, tc.role), &out, &errOut)
			if rc != exitConfig {
				t.Fatalf("rc=%d, want %d; stderr=%s", rc, exitConfig, errOut.String())
			}
			if out.Len() != 0 {
				t.Errorf("a refusal wrote to stdout, where a script reads success: %q", out.String())
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("the refusal does not say %q:\n%s", tc.want, errOut.String())
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

// TestRemoveMemberWithNoJournalConfiguredRefusesRatherThanDefaulting — unset and
// set-to-empty are both "not set", and neither may fall through to a path.
func TestRemoveMemberWithNoJournalConfiguredRefusesRatherThanDefaulting(t *testing.T) {
	for _, env := range []map[string]string{{}, {EnvControlJournal: ""}} {
		var out, errOut bytes.Buffer
		rc := runRemoveMember(env, removeMemberFlagsFor("prj_any", "usr_any", ""), &out, &errOut)
		if rc != exitConfig || out.Len() != 0 {
			t.Fatalf("rc=%d stdout=%q", rc, out.String())
		}
		if want := "-remove-member needs a control journal and $" + EnvControlJournal + " is not set"; !strings.Contains(errOut.String(), want) {
			t.Fatalf("the refusal does not say %q:\n%s", want, errOut.String())
		}
	}
}

// TestTheBinaryActuallyDispatchesRemoveMember runs the real `main`: everything above calls
// `runRemoveMember` directly and is blind to a mode never registered, ledgered or dispatched
// — and to the shared `-member-*` flags not actually reaching it.
func TestTheBinaryActuallyDispatchesRemoveMember(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	journal, _, a, b := aJournalWithACoMember(t)
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
	// Two modes at once is a refusal, and nobody was removed.
	code, body := run("-set-member", "-remove-member", "-member-project", string(a.Project), "-member-user", string(b.User))
	if code != exitConfig || !strings.Contains(body, "are both/all set") {
		t.Fatalf("`-set-member -remove-member` exited %d:\n%s", code, body)
	}
	if _, in := modelOf(t, journal).Memberships[a.Project][b.User]; !in {
		t.Fatal("a refused two-mode command removed the member")
	}

	code, body = run("-remove-member", "-member-project", string(a.Project), "-member-user", string(b.User))
	if code != 0 || !strings.Contains(body, "cairn-control: member-removed project="+string(a.Project)+" user="+string(b.User)) {
		t.Fatalf("`-remove-member` exited %d:\n%s", code, body)
	}
	if _, in := modelOf(t, journal).Memberships[a.Project][b.User]; in {
		t.Error("the child reported success and the journal still holds the membership")
	}
	_, help := run("-h")
	if !strings.Contains(help, "  -remove-member\n") {
		t.Errorf("`-h` does not render a bare `-remove-member`:\n%s", help)
	}
}

func itoa(n uint64) string {
	return strconv.FormatUint(n, 10)
}
