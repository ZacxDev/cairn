package main

// `cairn-server -remove-member`: the operator-driven removal of ONE user from ONE project.
//
// 🔴 IT CLOSES A STATED GAP: `member-removed` was in the closed event set and `apply` folded
// it, and nothing wrote one — `-set-member` declined to, naming two questions removal has
// that a role change does not. `control.RemoveMember` holds both answers and every rule; this
// file parses, calls it and says what happened, including what it LEFT in place.
//
// 🔴 A COMMAND, NOT A ROUTE, for `-set-member`'s reason: a route that changes who belongs to
// a project would be a public, internet-reachable endpoint over the authority itself.
// `api.DeclaredRoutes()` is unchanged by this file.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ZacxDev/cairn/internal/control"
)

// removeMemberFlags is the mode's flag set. Only `enabled` is registered here: the two ids
// are `-set-member`'s `-member-project` and `-member-user`, shared, and `role` is held so a
// stray `-member-role` can be REFUSED rather than silently ignored.
type removeMemberFlags struct {
	enabled *bool
	project *string
	user    *string
	role    *string
}

// registerRemoveMemberFlags declares the mode's ONE flag and borrows `-set-member`'s ids.
//
// 🔴 REUSED, NOT REDECLARED, AND THAT IS FORCED AS WELL AS CHOSEN. Every mode registers on the
// one default FlagSet, so a second `-member-project` is a redefinition panic at startup; and
// a `-remove-project` beside it would be a second spelling of "the project's id" that an
// operator must remember per mode. The modes are mutually exclusive (`main`'s ledger), so one
// flag never carries two meanings on one command line.
func registerRemoveMemberFlags(sm *setMemberFlags) *removeMemberFlags {
	return &removeMemberFlags{
		enabled: flag.Bool("remove-member", false,
			// ⚠ NO BACKQUOTES: `flag.PrintDefaults` takes the first backquoted word as the
			// placeholder, which is how `-create-scope`'s help once rendered a stray operand.
			"REMOVE ONE USER FROM ONE PROJECT: append ONE member-removed record to the control "+
				"journal named by $"+EnvControlJournal+", and exit. Refuses to remove the project's "+
				"ONLY owner. It revokes NO grant and NO credential: it lists, on stderr, every live "+
				"grant that still reaches the project's scopes for that user and every live "+
				"credential that still authenticates as them. Requires -member-project and "+
				"-member-user (the same flags -set-member takes); -member-role is refused"),
		project: sm.project,
		user:    sm.user,
		role:    sm.role,
	}
}

// runRemoveMember performs the removal and returns the process exit code — a code rather than
// `os.Exit`, so a test can run it in-process and read the journal back (`runCreateUser`'s
// ruling).
//
// Everything it prints is ids, a role, verbs, an epoch and credential LABELS — no secret — so
// the one machine-readable line goes to stdout and the report to stderr, `-set-member`'s split.
func runRemoveMember(env map[string]string, f *removeMemberFlags, out, errOut io.Writer) int {
	warn := func(line string) { fmt.Fprintln(errOut, reloadSafe(line)) }

	journal, err := controlJournalPath(env)
	if err != nil {
		warn("subsystem-store-api: " + err.Error())
		return exitConfig
	}
	if journal == "" {
		// REFUSED rather than defaulted, for the reason every sibling mode states.
		warn(fmt.Sprintf(
			"subsystem-store-api: -remove-member needs a control journal and $%s is not set. Set "+
				"it to the path the running surfaces read their journal from — a removal written "+
				"anywhere else is one no running server will ever resolve",
			EnvControlJournal))
		return exitConfig
	}

	project := control.ID(strings.TrimSpace(*f.project))
	user := control.ID(strings.TrimSpace(*f.user))
	if project == "" || user == "" {
		var missing []string
		if project == "" {
			missing = append(missing, "-member-project")
		}
		if user == "" {
			missing = append(missing, "-member-user")
		}
		warn(fmt.Sprintf(
			"subsystem-store-api: -remove-member refused: %s %s required. There is no default "+
				"for either: a journal with one project today has two tomorrow",
			strings.Join(missing, " and "),
			map[bool]string{true: "is", false: "are"}[len(missing) == 1]))
		return exitConfig
	}
	if strings.TrimSpace(*f.role) != "" {
		// 🔴 REFUSED, NOT IGNORED. `-member-role` beside `-remove-member` reads as "demote them
		// to this", which is `-set-member`'s job; ignoring it would remove somebody whose
		// operator meant to keep them.
		warn(fmt.Sprintf(
			"subsystem-store-api: -remove-member refused: -member-role %q was given, and a removal "+
				"confers no role. To CHANGE their role instead, use -set-member; to remove them, "+
				"drop -member-role", strings.TrimSpace(*f.role)))
		return exitConfig
	}
	// The prefix checks buy a better message for the swap two id flags invite; they are not a
	// second authority — `-set-member`'s standing.
	if !control.MustPrefix(project, control.PrefixProject) {
		warn(fmt.Sprintf(
			"subsystem-store-api: -remove-member refused: -member-project %q is not a project id "+
				"— those begin %q. If that value begins %q it is a USER id and the two flags are "+
				"the wrong way round", project, control.PrefixProject, control.PrefixUser))
		return exitConfig
	}
	if !control.MustPrefix(user, control.PrefixUser) {
		warn(fmt.Sprintf(
			"subsystem-store-api: -remove-member refused: -member-user %q is not a user id — "+
				"those begin %q", user, control.PrefixUser))
		return exitConfig
	}

	store, err := control.OpenFileStore(journal)
	if err != nil {
		warn("subsystem-store-api: -remove-member could not open the control journal: " + err.Error())
		return exitConfig
	}

	got, err := control.RemoveMember(context.Background(), store, control.MemberRemoval{
		ProjectID: project, UserID: user,
		// No `Actor`, for `-set-member`'s reason.
	})
	if err != nil {
		switch {
		case errors.Is(err, control.ErrNoSuchProject):
			warn(fmt.Sprintf(
				"subsystem-store-api: -remove-member refused, and nothing was written: this control "+
					"journal holds no project with id %s. -member-project takes an ID, not a name. "+
					"Journal %s", project, journal))
		case errors.Is(err, control.ErrNoSuchMember):
			warn(fmt.Sprintf(
				"subsystem-store-api: -remove-member refused, and nothing was written: this control "+
					"journal holds no user with id %s", user))
		default:
			// `ErrNotAMember`, `ErrWouldOrphanProject` and the journal's own refusals each name
			// the value and the remedy, so they are relayed, not re-worded.
			warn("subsystem-store-api: -remove-member refused, and nothing was written: " + err.Error())
		}
		return exitConfig
	}

	fmt.Fprintln(out, reloadSafe(fmt.Sprintf(
		"cairn-control: member-removed project=%s user=%s role=%s epoch=%d",
		got.ProjectID, got.UserID, got.Role, got.Epoch)))

	warn(fmt.Sprintf(
		"subsystem-store-api: %s is no longer IN project %s (they were %q), so membership no "+
			"longer gives them anything over the scopes that project owns",
		got.UserID, got.ProjectID, got.Role))

	// 🔴 THE RESIDUE, SAID EITHER WAY. An empty report and a report nobody printed look the
	// same, so "reaches none" is a sentence of its own rather than an absence.
	if len(got.StillReaches) == 0 {
		warn(fmt.Sprintf(
			"subsystem-store-api: %s now reaches NONE of project %s's scopes: no live grant on the "+
				"project or its scopes names them, or a project they are still in",
			got.UserID, got.ProjectID))
	} else {
		reach := make([]string, 0, len(got.StillReaches))
		for _, r := range got.StillReaches {
			reach = append(reach, fmt.Sprintf("%s (%s) %s", r.Name, r.ID, r.Verbs))
		}
		warn(fmt.Sprintf(
			"subsystem-store-api: %s STILL REACHES %d of project %s's scopes through %d live "+
				"grant(s) this command did NOT revoke: %s. A grant is a separate decision from a "+
				"membership; revoke one through the browser surface's share flow (unshare), by "+
				"a principal with authority over its object — no cairn-server command revokes a grant",
			got.UserID, len(got.StillReaches), got.ProjectID, len(got.Grants), strings.Join(reach, ", ")))
		for _, g := range got.Grants {
			via := "names them directly"
			if g.Via != "" {
				via = "via their membership of project " + string(g.Via)
			}
			warn(fmt.Sprintf(
				"subsystem-store-api: NOT REVOKED grant=%s subject=%s:%s object=%s:%s verbs=%s (%s)",
				g.Grant.ID, g.Grant.SubjectKind, g.Grant.SubjectID,
				g.Grant.ObjectKind, g.Grant.ObjectID, g.Grant.Verbs, via))
		}
	}

	if len(got.Credentials) == 0 {
		warn(fmt.Sprintf("subsystem-store-api: %s holds no live credential", got.UserID))
	} else {
		warn(fmt.Sprintf(
			"subsystem-store-api: %d live credential(s) still AUTHENTICATE as %s — a credential "+
				"binds to the person, not to a project, so this removal changed what each reaches, "+
				"not whether it works. Revoke any that should stop with -revoke-credential "+
				"-credential <id>", len(got.Credentials), got.UserID))
		for _, c := range got.Credentials {
			warn(fmt.Sprintf(
				"subsystem-store-api: STILL AUTHENTICATES credential=%s label=%q", c.ID, c.Label))
		}
	}

	warn("subsystem-store-api: this wrote ONE member-removed record and nothing else. It revoked " +
		"NO grant and NO credential, deleted nothing under the store root, and did not delete the " +
		"user — they still exist, with every other membership they hold")
	warn(fmt.Sprintf(
		"subsystem-store-api: the running server picks this up within %s; any other program reading "+
			"this journal does so on its own refresh", refreshInterval))
	return 0
}
