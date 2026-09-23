package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ZacxDev/cairn/internal/control"
)

// setMemberFlags is the mode's flag set, held in a struct for the reason
// `createUserFlags` records: `main` reads them to decide which mode was asked for, and a
// bag of loose pointers is the second place the program decides what it was told to do.
type setMemberFlags struct {
	enabled *bool
	project *string
	user    *string
	role    *string
}

// registerSetMemberFlags declares the mode's flags.
//
// 🔴 THE NAMES ARE `-member-*` AND THEY CANNOT BE `-project`/`-principal`, WHICH IS A
// PROPERTY OF THE PACKAGE RATHER THAN A STYLE CHOICE. Every mode here registers on the
// `flag` package's DEFAULT FlagSet, so a name is global to the program: `-project` is
// already `-create-user`'s (a project NAME to create) and `-principal` is already
// `-issue-credential`'s. Reusing either would be a redefinition panic at startup, and
// reusing one that happened to be free would give one flag two meanings depending on
// which mode was set — which is exactly the ambiguity the mode ledger in `main` refuses.
//
// ⚠ AND THEY ARE IDS, NOT NAMES, WHICH IS THE OPPOSITE OF `-create-user`'s `-project`.
// That mode NAMES a project it is about to create; this one ADDRESSES two that already
// exist. A display name would be ambiguous — `Model.ScopeByName` already refuses a
// duplicate display name rather than picking, and project names are not unique either —
// so this mode takes the immutable ids both other modes print.
func registerSetMemberFlags() *setMemberFlags {
	return &setMemberFlags{
		enabled: flag.Bool("set-member", false,
			"PUT A USER IN A PROJECT at a role, or change the role they already hold, in "+
				"the control journal named by $"+EnvControlJournal+", and exit. This is the "+
				"only path in this repository that writes a `member-set` record: "+
				"-create-user creates a NEW project every time it runs, so without this two "+
				"users are never co-members and the browser surface's share flow has nobody "+
				"to offer. Requires -member-project, -member-user and -member-role"),
		project: flag.String("member-project", "",
			"the project's id (`prj_…`), as printed by -create-user. NOT its display name: "+
				"names are mutable and not unique, so addressing by name would be a lookup "+
				"that can silently mean a different project tomorrow"),
		user: flag.String("member-user", "",
			"the user's id (`usr_…`), as printed by -create-user. NOT a provider subject "+
				"and not an email. ⚠ A PROJECT CANNOT BE A MEMBER: `member-set` carries a "+
				"user id and nothing else, and a project is not a member of itself — a "+
				"service account reaches a scope only through an explicit grant"),
		role: flag.String("member-role", "",
			"`owner`, `admin` or `member`. owner and admin have identical authority over "+
				"the project's scopes; they differ only in that an owner may delete the "+
				"project. member may read and write its scopes and nothing else. There is "+
				"no default: a role that was not chosen is authority nobody decided to give"),
	}
}

// runSetMember performs the membership write and returns the process exit code.
//
// 🔴 IT RETURNS A CODE RATHER THAN CALLING `os.Exit`, so a test can run it in-process and
// read the journal back in the same function — the ruling `runCreateUser` records.
//
// 🔴 EVERYTHING IT PRINTS IS SAFE TO RE-STAGE, WHICH IS WHY IT USES STDOUT FOR THE RESULT
// UNLIKE `-issue-credential`. That mode routes prose to stderr because the machine-readable
// thing it emits IS a secret; nothing here is. Ids, a role and an epoch are all values an
// operator already has on their command line, so the `cairn-control:` line goes to stdout
// where `-create-user`'s does, and the advisory notes go to stderr.
func runSetMember(env map[string]string, f *setMemberFlags, out, errOut io.Writer) int {
	warn := func(line string) { fmt.Fprintln(errOut, reloadSafe(line)) }

	journal, err := controlJournalPath(env)
	if err != nil {
		warn("subsystem-store-api: " + err.Error())
		return exitConfig
	}
	if journal == "" {
		// REFUSED rather than defaulted to a path, for the reason both sibling modes
		// state: a membership written anywhere else is authority no running pod reads,
		// and the operator's evidence that it "worked" would be a change nobody can see.
		warn(fmt.Sprintf(
			"subsystem-store-api: -set-member needs a control journal and $%s is not set. "+
				"Set it to the path this pod reads its journal from — a membership written "+
				"anywhere else is a change no running server will ever resolve",
			EnvControlJournal))
		return exitConfig
	}

	project := control.ID(strings.TrimSpace(*f.project))
	user := control.ID(strings.TrimSpace(*f.user))
	role := control.Role(strings.TrimSpace(*f.role))

	if project == "" || user == "" || role == "" {
		// 🔴 ALL THREE IN ONE REFUSAL, NAMING THE MISSING ONES, rather than three
		// sequential refusals. An operator who forgot two flags gets one round trip
		// instead of two, and the message can say what the trio is FOR.
		var missing []string
		if project == "" {
			missing = append(missing, "-member-project")
		}
		if user == "" {
			missing = append(missing, "-member-user")
		}
		if role == "" {
			missing = append(missing, "-member-role")
		}
		warn(fmt.Sprintf(
			"subsystem-store-api: -set-member refused: %s %s required. A membership is a "+
				"triple — which project, which person, what authority — and there is no "+
				"default for any of them: a journal with one project today has two "+
				"tomorrow, and a role nobody chose is authority nobody decided to give",
			strings.Join(missing, " and "),
			map[bool]string{true: "is", false: "are"}[len(missing) == 1]))
		return exitConfig
	}

	// 🔴 PREFIX CHECKS, WHICH BUY A BETTER MESSAGE AND ARE NOT A SECOND AUTHORITY — the
	// same standing as `-issue-credential`'s. `control.SetMember` refuses an id that names
	// nothing, and `apply` refuses it again under the lock. What these catch is the
	// mistake the flag pair invites: two id-shaped values on one command line, swapped.
	// Without them the operator is told "no such project: usr_…", which is true and reads
	// as "that project was deleted" rather than "you transposed two flags".
	if !control.MustPrefix(project, control.PrefixProject) {
		warn(fmt.Sprintf(
			"subsystem-store-api: -set-member refused: -member-project %q is not a project "+
				"id — those begin %q. If that value begins %q it is a USER id and the two "+
				"flags are the wrong way round",
			project, control.PrefixProject, control.PrefixUser))
		return exitConfig
	}
	if !control.MustPrefix(user, control.PrefixUser) {
		warn(fmt.Sprintf(
			"subsystem-store-api: -set-member refused: -member-user %q is not a user id — "+
				"those begin %q. A project cannot be a member of a project, so a %q value "+
				"here is not a narrower request but an impossible one",
			user, control.PrefixUser, control.PrefixProject))
		return exitConfig
	}
	if !role.Valid() {
		warn(fmt.Sprintf(
			"subsystem-store-api: -set-member refused: -member-role %q is not a role. It is "+
				"one of %q, %q or %q — a role this build cannot map grants NOTHING, so "+
				"accepting it would write a record that silently strips authority",
			role, control.RoleOwner, control.RoleAdmin, control.RoleMember))
		return exitConfig
	}

	store, err := control.OpenFileStore(journal)
	if err != nil {
		warn("subsystem-store-api: -set-member could not open the control journal: " +
			err.Error())
		return exitConfig
	}

	got, err := control.SetMember(context.Background(), store, control.NewMembership{
		ProjectID: project,
		UserID:    user,
		Role:      role,
		// No `Actor`: an operator running this against the journal file has no principal
		// to name, and a fabricated one would be worse than an absent one in a log whose
		// whole purpose is attribution. `control.NewMembership.Actor` says the same.
	})
	if err != nil {
		// 🔴 BRANCHING ON THE SENTINELS IS WHAT MAKES THEM GUARDS RATHER THAN DECORATION —
		// the lesson `ErrNoSuchPrincipal`'s own comment records, where the sentinel had
		// exactly one consumer tree-wide and it was a test.
		switch {
		case errors.Is(err, control.ErrNoSuchProject):
			warn(fmt.Sprintf(
				"subsystem-store-api: -set-member refused: this control journal holds no "+
					"project with id %s. List what it does hold by reading %s, or create "+
					"one with -create-user", project, journal))
		case errors.Is(err, control.ErrNoSuchMember):
			warn(fmt.Sprintf(
				"subsystem-store-api: -set-member refused: this control journal holds no "+
					"user with id %s. Create them with -create-user first — this mode "+
					"changes authority, it does not provision people", user))
		case errors.Is(err, control.ErrWouldOrphanProject):
			warn("subsystem-store-api: -set-member refused: " + err.Error())
		default:
			warn("subsystem-store-api: -set-member failed: " + err.Error())
		}
		return exitConfig
	}

	// The machine-readable line, on stdout, in `-create-user`'s vocabulary.
	fmt.Fprintf(out,
		"cairn-control: member-set project=%s user=%s role=%s epoch=%d\n",
		got.ProjectID, got.UserID, got.Role, got.Epoch)

	// 🔴 SAY WHICH OF THE THREE OUTCOMES THIS WAS. "It worked" is the one thing an
	// operator can already see; whether they JOINED somebody, PROMOTED them, or changed
	// nothing is what they cannot, and a command that reads identically in all three
	// cases is how a no-op gets believed as a change.
	switch {
	case !got.WasMember:
		warn(fmt.Sprintf(
			"subsystem-store-api: %s is now IN project %s at %q, where they were not a "+
				"member before", got.UserID, got.ProjectID, got.Role))
	case got.Changed():
		warn(fmt.Sprintf(
			"subsystem-store-api: %s moved from %q to %q in project %s",
			got.UserID, got.Previous, got.Role, got.ProjectID))
	default:
		warn(fmt.Sprintf(
			"subsystem-store-api: NOTHING CHANGED — %s was already %q in project %s. The "+
				"record was still appended, because the journal is append-only and an "+
				"operator asserting a role at a time is itself information",
			got.UserID, got.Role, got.ProjectID))
	}

	// ⚠ WHAT MEMBERSHIP DOES AND DOES NOT CONFER, said here because the gap between them
	// is the first surprise for whoever runs this expecting a share to appear.
	warn(fmt.Sprintf(
		"subsystem-store-api: this confers %q's authority over the scopes project %s OWNS. "+
			"A scope shared INTO this project from elsewhere still needs its own grant, and "+
			"a scope this project owns is reachable by every member without one",
		got.Role, got.ProjectID))
	// INTERPOLATED, not spelled — `createuser.go` and `issuecredential.go` both do this
	// and a hardcoded "30s" here would be the ungated prose count this PR's own new test
	// exists to stop, reintroduced one file over.
	warn(fmt.Sprintf(
		"subsystem-store-api: the running server picks this up within %s", refreshInterval))
	return 0
}
