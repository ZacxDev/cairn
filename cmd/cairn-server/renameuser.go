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

// renameUserFlags is the mode's flag set, held in a struct for the reason `createUserFlags`
// records.
type renameUserFlags struct {
	enabled *bool
	user    *string
	display *string
}

// registerRenameUserFlags declares the mode's flags.
//
// ⚠ THE NAMES ARE `-rename-*`, NOT `-user`/`-display-name`, FOR `registerSetMemberFlags`'
// REASON: every mode registers on the DEFAULT FlagSet, so a name is global to the program.
// `-display-name` is already `-create-user`'s; sharing it would make one flag silently belong
// to whichever mode is set, and the mode ledger in `main` exists so that nothing here depends on
// which mode won.
func registerRenameUserFlags() *renameUserFlags {
	return &renameUserFlags{
		enabled: flag.Bool("rename-user", false,
			"SET AN EXISTING USER'S DISPLAY NAME in the control journal named by $"+EnvControlJournal+
				", and exit — the name the audit line, every written bullet's ACTOR and the browser's "+
				"\"signed in as\" render instead of the email. Appends one `user-renamed` record. "+
				"Requires -rename-user-id and -rename-display-name. ⚠ An OLDER cairn-server or cairn-ui "+
				"refuses a journal holding this record whole — roll every binary that reads the journal first"),
		user: flag.String("rename-user-id", "",
			"the user's id (`usr_…`), as printed by -create-user. NOT an email and not a provider "+
				"subject: those are not unique keys, an id is"),
		display: flag.String("rename-display-name", "",
			"the new display name: [A-Za-z0-9] then [A-Za-z0-9._-], at most 32, no `:` or `@`, and "+
				"unique among users case-insensitively, including any display another user once had. "+
				"OPERATOR-written; nothing an identity "+
				"provider sends ever reaches it"),
	}
}

// runRenameUser performs the write and returns the process exit code, for `runSetMember`'s
// reason: a test runs it in-process and reads the journal back.
//
// Stdout carries the one machine-readable line, stderr the advisories — `-set-member`'s split,
// and for its reason: nothing here is a secret.
func runRenameUser(env map[string]string, f *renameUserFlags, out, errOut io.Writer) int {
	warn := func(line string) { fmt.Fprintln(errOut, reloadSafe(line)) }

	journal, err := controlJournalPath(env)
	if err != nil {
		warn("subsystem-store-api: " + err.Error())
		return exitConfig
	}
	if journal == "" {
		warn(fmt.Sprintf(
			"subsystem-store-api: -rename-user needs a control journal and $%s is not set. Set it to "+
				"the path this pod reads its journal from — a name written anywhere else is a change no "+
				"running server will ever render", EnvControlJournal))
		return exitConfig
	}

	user := control.ID(strings.TrimSpace(*f.user))
	// ⚠ THE NAME IS *NOT* TRIMMED. A surrounding space is a refusal from the shape rule, which
	// names it, rather than a silent edit of what the operator typed.
	name := *f.display
	if user == "" || name == "" {
		var missing []string
		if user == "" {
			missing = append(missing, "-rename-user-id")
		}
		if name == "" {
			missing = append(missing, "-rename-display-name")
		}
		warn(fmt.Sprintf(
			"subsystem-store-api: -rename-user refused: %s %s required. There is no default user and no "+
				"default name", strings.Join(missing, " and "),
			map[bool]string{true: "is", false: "are"}[len(missing) == 1]))
		return exitConfig
	}
	if !control.MustPrefix(user, control.PrefixUser) {
		warn(fmt.Sprintf(
			"subsystem-store-api: -rename-user refused: -rename-user-id %q is not a user id — those begin "+
				"%q. A project's display is its name and is not changed by this mode", user, control.PrefixUser))
		return exitConfig
	}
	// Judged BEFORE the journal is opened, so a malformed name never reaches the lock.
	if err := control.ValidUserDisplayName(name); err != nil {
		warn("subsystem-store-api: -rename-user refused: -rename-display-name: " + err.Error())
		return exitConfig
	}

	store, err := control.OpenFileStore(journal)
	if err != nil {
		warn("subsystem-store-api: -rename-user could not open the control journal: " + err.Error())
		return exitConfig
	}
	got, err := control.RenameUser(context.Background(), store, control.NewUserDisplayName{
		UserID: user, DisplayName: name,
		// No `Actor`, for `-set-member`'s reason: an operator at the journal file has no
		// principal to name, and a fabricated one is worse than none.
	})
	if err != nil {
		switch {
		case errors.Is(err, control.ErrNoSuchMember):
			warn(fmt.Sprintf(
				"subsystem-store-api: -rename-user refused: this control journal holds no user with id %s. "+
					"Create them with -create-user first", user))
		case errors.Is(err, control.ErrUserDisplayNameTaken):
			warn("subsystem-store-api: -rename-user refused: " + err.Error())
		default:
			warn("subsystem-store-api: -rename-user failed: " + err.Error())
		}
		return exitConfig
	}

	fmt.Fprintf(out, "cairn-control: user-renamed user=%s display=%s epoch=%d\n", got.UserID, got.Display, got.Epoch)
	if got.Previous == got.Display {
		warn(fmt.Sprintf(
			"subsystem-store-api: NOTHING CHANGED — %s already displayed as %q. The record was still "+
				"appended: the journal is append-only, and an assertion at a time is itself information",
			got.UserID, got.Display))
	} else {
		warn(fmt.Sprintf("subsystem-store-api: %s now displays as %q (was %q)", got.UserID, got.Display, got.Previous))
	}
	warn(fmt.Sprintf("subsystem-store-api: the running server picks this up within %s", refreshInterval))
	return 0
}
