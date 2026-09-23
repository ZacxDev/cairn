package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// setMemberFlagsFor builds the mode's flags the way an operator's command line would.
func setMemberFlagsFor(project, user, role string) *setMemberFlags {
	enabled := true
	return &setMemberFlags{
		enabled: &enabled,
		project: &project,
		user:    &user,
		role:    &role,
	}
}

// aJournalWithTwoSeparateUsers is the state the OTHER operator command actually produces,
// which is the whole reason this mode exists.
//
// 🔴 IT RUNS `-create-user`'s LIBRARY PATH TWICE WITH THE SAME PROJECT NAME, because that
// is the sequence an operator follows and it is what makes two people NOT co-members —
// `ProvisionUser` creates a fresh project every call. A fixture that hand-built one project
// with two members would be testing a world no command here can reach.
func aJournalWithTwoSeparateUsers(t *testing.T) (
	journal string, env map[string]string, a, b control.Provisioned,
) {
	t.Helper()
	journal = filepath.Join(t.TempDir(), "control.journal")
	env = map[string]string{EnvControlJournal: journal}
	store, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatalf("opening the journal: %v", err)
	}
	at := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	a, err = control.ProvisionUser(context.Background(), store, control.NewUser{
		Provider: "notes-idp", Subject: "subject-0001", Email: "rowan@notes.example.test",
		ProjectName: "quarry", ScopeNames: []string{"quarry-notes"}, At: at,
	})
	if err != nil {
		t.Fatalf("provisioning the first user: %v", err)
	}
	b, err = control.ProvisionUser(context.Background(), store, control.NewUser{
		Provider: "notes-idp", Subject: "subject-0002", Email: "wren@notes.example.test",
		ProjectName: "quarry", ScopeNames: nil, At: at,
	})
	if err != nil {
		t.Fatalf("provisioning the second user: %v", err)
	}
	if a.Project == b.Project {
		t.Fatalf("the fixture's premise is gone: two -create-user runs with the same "+
			"project name produced ONE project (%s), so this mode's reason for existing "+
			"has changed and these tests are about the wrong thing", a.Project)
	}
	return journal, env, a, b
}

// TestSetMemberMakesTheTwoUsersCO_MEMBERS_ThroughTheCommand is the end-to-end claim: the
// operator sequence alone, with no hand-edited journal, reaches the state the share flow
// needs.
func TestSetMemberMakesTheTwoUsersCO_MEMBERS_ThroughTheCommand(t *testing.T) {
	journal, env, a, b := aJournalWithTwoSeparateUsers(t)
	var out, errOut bytes.Buffer

	rc := runSetMember(env, setMemberFlagsFor(string(a.Project), string(b.User), "member"),
		&out, &errOut)
	if rc != 0 {
		t.Fatalf("rc=%d, stderr=%s", rc, errOut.String())
	}

	// The machine-readable line is on STDOUT, in `-create-user`'s vocabulary.
	if !strings.Contains(out.String(), "cairn-control: member-set ") {
		t.Errorf("stdout carries no machine-readable line: %q", out.String())
	}
	// …and it names all three operands plus the epoch, so a script can read it back.
	for _, want := range []string{string(a.Project), string(b.User), "role=member", "epoch="} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stdout is missing %q: %q", want, out.String())
		}
	}

	// 🔴 RE-READ THE JOURNAL. A command that printed a correct line and appended nothing
	// satisfies every assertion above.
	store, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatalf("re-opening: %v", err)
	}
	m, err := store.Model(context.Background())
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	if ms, in := m.Memberships[a.Project][b.User]; !in || ms.Role != control.RoleMember {
		t.Fatalf("the journal does not hold the membership: in=%v %+v", in, ms)
	}
	// The question the share flow asks.
	if _, both := m.MembershipsOf(b.User)[a.Project]; !both {
		t.Error("the two users still share no project")
	}
}

// TestEveryRefusalWritesNOTHING covers each branch AND asserts the journal did not grow.
//
// 🔴 THE EPOCH CHECK IS THE HALF THAT MATTERS. A refusal that appended first and then
// reported an error would leave the change applied — and for the orphan case that means an
// unadministrable project created by the very guard that exists to prevent one.
func TestEveryRefusalWritesNOTHING(t *testing.T) {
	cases := []struct {
		name  string
		flags func(a, b control.Provisioned) *setMemberFlags
		// want is a fragment unique to THIS refusal, so a test cannot pass on a
		// neighbour's message.
		want string
	}{
		{
			name: "the two ids swapped, which is what two id flags invite",
			flags: func(a, b control.Provisioned) *setMemberFlags {
				return setMemberFlagsFor(string(b.User), string(a.Project), "member")
			},
			want: "the two flags are the wrong way round",
		},
		{
			name: "a project-shaped value in the user flag",
			flags: func(a, b control.Provisioned) *setMemberFlags {
				return setMemberFlagsFor(string(a.Project), string(b.Project), "member")
			},
			want: "not a narrower request but an impossible one",
		},
		{
			name: "an unknown role",
			flags: func(a, b control.Provisioned) *setMemberFlags {
				return setMemberFlagsFor(string(a.Project), string(b.User), "auditor")
			},
			want: `"auditor" is not a role`,
		},
		{
			name:  "no flags at all",
			flags: func(a, b control.Provisioned) *setMemberFlags { return setMemberFlagsFor("", "", "") },
			want:  "-member-project and -member-user and -member-role are required",
		},
		{
			name: "a well-formed project id that names nothing",
			flags: func(a, b control.Provisioned) *setMemberFlags {
				return setMemberFlagsFor("prj_nowhere", string(b.User), "member")
			},
			want: "holds no project with id prj_nowhere",
		},
		{
			name: "a well-formed user id that names nobody",
			flags: func(a, b control.Provisioned) *setMemberFlags {
				return setMemberFlagsFor(string(a.Project), "usr_nobody", "member")
			},
			want: "holds no user with id usr_nobody",
		},
		{
			name: "demoting the only owner",
			flags: func(a, b control.Provisioned) *setMemberFlags {
				return setMemberFlagsFor(string(a.Project), string(a.User), "member")
			},
			want: "Promote somebody else to owner first",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			journal, env, a, b := aJournalWithTwoSeparateUsers(t)
			store, err := control.OpenFileStore(journal)
			if err != nil {
				t.Fatalf("opening: %v", err)
			}
			before, err := store.Model(context.Background())
			if err != nil {
				t.Fatalf("reading: %v", err)
			}

			var out, errOut bytes.Buffer
			rc := runSetMember(env, tc.flags(a, b), &out, &errOut)
			if rc != exitConfig {
				t.Fatalf("rc=%d, want %d. stdout=%q stderr=%q",
					rc, exitConfig, out.String(), errOut.String())
			}
			if out.Len() != 0 {
				t.Errorf("a refusal wrote to STDOUT, which is where the machine-readable "+
					"success line goes: %q", out.String())
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("the refusal does not carry %q: %s", tc.want, errOut.String())
			}

			reread, err := control.OpenFileStore(journal)
			if err != nil {
				t.Fatalf("re-opening: %v", err)
			}
			after, err := reread.Model(context.Background())
			if err != nil {
				t.Fatalf("replaying: %v", err)
			}
			if after.Epoch != before.Epoch {
				t.Errorf("the journal grew %d -> %d on a REFUSED request",
					before.Epoch, after.Epoch)
			}
		})
	}
}

// TestWithNoJournalConfiguredItRefusesRatherThanDefaulting — the sibling modes' rule, and
// a default here would write authority a running pod is not reading.
func TestWithNoJournalConfiguredItRefusesRatherThanDefaulting(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runSetMember(map[string]string{}, setMemberFlagsFor("prj_x", "usr_y", "member"),
		&out, &errOut)
	if rc != exitConfig {
		t.Fatalf("rc=%d, want %d", rc, exitConfig)
	}
	if !strings.Contains(errOut.String(), EnvControlJournal) {
		t.Errorf("the refusal does not name the variable to set: %s", errOut.String())
	}
}

// TestTheThreeOUTCOMES_ReadDifferENTLY. "It worked" is the one thing an operator can
// already see; which of the three happened is what they cannot.
func TestTheThreeOUTCOMES_ReadDifferENTLY(t *testing.T) {
	_, env, a, b := aJournalWithTwoSeparateUsers(t)

	say := func(role string) string {
		var out, errOut bytes.Buffer
		if rc := runSetMember(env, setMemberFlagsFor(string(a.Project), string(b.User), role),
			&out, &errOut); rc != 0 {
			t.Fatalf("rc=%d: %s", rc, errOut.String())
		}
		return errOut.String()
	}

	if joined := say("member"); !strings.Contains(joined, "is now IN project") {
		t.Errorf("a join does not say so: %s", joined)
	}
	if promoted := say("admin"); !strings.Contains(promoted, `moved from "member" to "admin"`) {
		t.Errorf("a promotion does not name both roles: %s", promoted)
	}
	noop := say("admin")
	if !strings.Contains(noop, "NOTHING CHANGED") {
		t.Errorf("a restated role reads as a change: %s", noop)
	}
	// 🔴 AND IT SAYS THE RECORD WAS STILL WRITTEN, because "nothing changed" otherwise
	// reads as "nothing was appended", which is false and would make an operator re-run.
	if !strings.Contains(noop, "still appended") {
		t.Errorf("the no-op message does not say the record was appended: %s", noop)
	}
}

// TestTheBinaryActuallyDispatchesSetMember — the wiring claim. Everything above calls
// `runSetMember` directly, which is blind to a mode that was never added to `main`'s
// dispatch.
func TestTheBinaryActuallyDispatchesSetMember(t *testing.T) {
	main := filepath.Join("main.go")
	src, err := os.ReadFile(main)
	if err != nil {
		t.Fatalf("reading %s: %v", main, err)
	}
	text := string(src)
	for _, want := range []string{
		"registerSetMemberFlags()",
		`{"-set-member", *setMember.enabled}`,
		"runSetMember(envalias.Environ(), setMember,",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("main.go does not carry %q — the mode's flags, its entry in the "+
				"exclusivity ledger and its dispatch all have to move together, and a "+
				"missing ledger entry means `-set-member -routes` prints the ledger and "+
				"silently changes nobody's access", want)
		}
	}
}

// TestTheRegisteredFlagCountInProseMatchesTheCode closes a defect this file's subject has
// hit TWICE.
//
// 🔴 THE PROSE IT CHECKS SAYS SO ITSELF: "IT IS A COUNT IN PROSE WITH NO GATE — WHICH IS
// WHY IT HAS NOW BEEN WRONG TWICE." It read "`-create-user`'s six" and `-issue-credential`
// landing made it false with every test green; the correction said FIVE and `-token-out`
// made that false in the next round. This is the gate, and it is the deterministic fix for
// a number three commits have now had to update by hand.
//
// ⚠ IT COUNTS `flag.` CALLS PER `register*Flags` FUNCTION, which is exactly what the prose
// claims to describe — not a guess at the mode's shape. A flag added to one of those
// functions moves the number here and fails until the sentence moves with it.
func TestTheRegisteredFlagCountInProseMatchesTheCode(t *testing.T) {
	spelled := map[string]string{
		"registerCreateUserFlags":      "-create-user",
		"registerIssueCredentialFlags": "-issue-credential",
		"registerSetMemberFlags":       "-set-member",
	}
	words := map[int]string{
		1: "one", 2: "two", 3: "three", 4: "four", 5: "five",
		6: "six", 7: "seven", 8: "eight", 9: "nine", 10: "ten",
	}

	mainSrc, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	prose := string(mainSrc)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("listing the package: %v", err)
	}
	flagCall := regexp.MustCompile(`\bflag\.(Bool|String|Int|Duration|Float64|Int64|Uint|Uint64|Var)\(`)

	counted := 0
	for fn, mode := range spelled {
		var body string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") ||
				strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			src, err := os.ReadFile(e.Name())
			if err != nil {
				t.Fatalf("reading %s: %v", e.Name(), err)
			}
			text := string(src)
			start := strings.Index(text, "func "+fn+"(")
			if start < 0 {
				continue
			}
			rest := text[start:]
			end := strings.Index(rest, "\n}\n")
			if end < 0 {
				t.Fatalf("%s in %s does not close — the extractor is broken, and the "+
					"count below would be about a truncated body", fn, e.Name())
			}
			body = rest[:end]
			break
		}
		if body == "" {
			t.Fatalf("%s was not found in any non-test file in this package. The "+
				"extractor found nothing, so a zero count here would be a fact about "+
				"the extractor", fn)
		}
		n := len(flagCall.FindAllString(body, -1))
		if n == 0 {
			t.Fatalf("%s registers ZERO flags by this reader's count — the instrument is "+
				"wired to nothing", fn)
		}
		counted++
		want := "`" + mode + "`'s " + words[n]
		if !strings.Contains(prose, want) {
			t.Errorf("main.go's prose does not say %q, but %s registers %d flag(s). "+
				"That sentence has been wrong twice; move it in the same commit as the "+
				"flag.", want, fn, n)
		}
	}
	if counted != len(spelled) {
		t.Fatalf("checked %d of %d modes", counted, len(spelled))
	}

	// 🔴 AND `main`'s OWN COUNT, WHICH THIS TEST DID NOT COVER AND AN AUDIT MEASURED
	// MISSING. The sentence claims FOUR numbers — "five flags in `main`" plus one per
	// `register*Flags` — and the loop above checked only the latter three. Measured: a
	// sixth `flag.Bool` added inside `main()` left this whole package green while the
	// prose still said five. A guard covering three of the four things its own sentence
	// asserts reads as coverage while providing three quarters of it, which is the
	// narrower-body-than-docstring defect `AGENTS.md` names.
	mainStart := strings.Index(prose, "\nfunc main() {")
	if mainStart < 0 {
		t.Fatal("func main() not found — the reader below would count zero and pass")
	}
	mainBody := prose[mainStart:]
	// `flag.Parse()` ends the registration block; flags cannot be declared after it.
	if end := strings.Index(mainBody, "flag.Parse()"); end >= 0 {
		mainBody = mainBody[:end]
	} else {
		t.Fatal("flag.Parse() not found in main — the body below is unbounded")
	}
	inMain := len(flagCall.FindAllString(mainBody, -1))
	if inMain == 0 {
		t.Fatal("zero flags registered in main by this reader — it is wired to nothing")
	}
	if want := words[inMain] + " flags in `main`"; !strings.Contains(prose, want) {
		t.Errorf("main.go's prose does not say %q, but main() registers %d flag(s) before "+
			"flag.Parse(). That sentence has been wrong twice; move it in the same commit "+
			"as the flag.", want, inMain)
	}
}
