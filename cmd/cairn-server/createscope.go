package main

// `cairn-server -create-scope`: the operator-driven creation of ONE scope in a project that
// already exists.
//
// 🔴 IT CLOSES A STATED GAP: `-create-user` was the only writer of a `scope-created` record,
// and it writes one only beside a new user and a new project — so a project's scope set was
// frozen the day its owner was provisioned. See `control.CreateScope`, which holds every rule;
// this file parses, calls it and says what happened.
//
// 🔴 A COMMAND, NOT A ROUTE, for `-create-user`'s reason: a route that widens what every
// member of a project can reach would be a public, internet-reachable endpoint over the
// authority itself. `api.DeclaredRoutes()` is unchanged by this file.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ZacxDev/cairn/internal/control"
)

type createScopeFlags struct {
	enabled *bool
	project *string
	name    *string
}

// registerCreateScopeFlags declares the mode's flags.
//
// 🔴 `-scope-project`, NOT `-project`, FOR `registerSetMemberFlags`' REASON: every mode
// registers on the default FlagSet, so a name is global to the program, and `-project` is
// already `-create-user`'s — where it is a NAME to create. This one is the ID of a project
// that exists, which is the opposite, and one flag with two meanings by mode is the
// ambiguity the mode ledger in `main` refuses.
func registerCreateScopeFlags() *createScopeFlags {
	return &createScopeFlags{
		enabled: flag.Bool("create-scope", false,
			"CREATE ONE SCOPE in an EXISTING project: append ONE `scope-created` record to the "+
				"control journal named by $"+EnvControlJournal+", print its id, and exit. Every member "+
				"of that project reaches the new scope at their role; a credential narrowed to other "+
				"scopes does not. It creates NO directory under -store. Requires -scope-project and "+
				"-scope-name"),
		project: flag.String("scope-project", "",
			"the owning project's id (`prj_…`), as -create-user printed it. NOT its name: "+
				"-create-user's -project takes a name because it creates one; project names are not "+
				"unique, so an existing project is addressed by id"),
		name: flag.String("scope-name", "",
			"the new scope's display name — ONE per run, stored exactly as typed. It is the "+
				"DIRECTORY name under the store root and the name a client passes as --scope: "+
				"[A-Za-z0-9_-] only, and unique across the WHOLE journal once folded (lowercase, `_` "+
				"to `-`). The journal is append-only and nothing renames a scope"),
	}
}

// runCreateScope performs the creation and returns the process exit code — a code rather
// than `os.Exit`, so a test can run it in-process and read the journal back
// (`runCreateUser`'s ruling).
//
// `storeRoot` is passed for `-create-user`'s reason and no other: this mode chooses a scope
// display name, and the store root is the only thing that can say whether that name already
// reaches somebody's directory.
func runCreateScope(env map[string]string, storeRoot string, f *createScopeFlags, out, errOut io.Writer) int {
	warn := func(line string) { fmt.Fprintln(errOut, reloadSafe(line)) }

	journal, err := controlJournalPath(env)
	if err != nil {
		warn("subsystem-store-api: " + err.Error())
		return exitConfig
	}
	if journal == "" {
		// REFUSED rather than defaulted to a path, for the reason every sibling mode states.
		warn(fmt.Sprintf(
			"subsystem-store-api: -create-scope needs a control journal and $%s is not set. Set it "+
				"to the path the running surfaces read their journal from — a scope written anywhere "+
				"else is one no running server will ever resolve",
			EnvControlJournal))
		return exitConfig
	}

	project := control.ID(strings.TrimSpace(*f.project))
	// ⚠ NOT TRIMMED: the name is stored exactly as typed, and a name with a space at either
	// end is refused by `control.CreateScope`'s class rule rather than silently repaired.
	name := *f.name
	if project == "" || name == "" {
		var missing []string
		if project == "" {
			missing = append(missing, "-scope-project")
		}
		if name == "" {
			missing = append(missing, "-scope-name")
		}
		warn(fmt.Sprintf(
			"subsystem-store-api: -create-scope refused: %s %s required. There is no default for "+
				"either: a journal with one project today has two tomorrow, and a scope nobody named "+
				"is a directory nobody chose",
			strings.Join(missing, " and "),
			map[bool]string{true: "is", false: "are"}[len(missing) == 1]))
		return exitConfig
	}

	ctx := context.Background()
	store, err := control.OpenFileStore(journal)
	if err != nil {
		warn("subsystem-store-api: -create-scope could not open the control journal: " + err.Error())
		return exitConfig
	}

	// BEFORE the write, for `runCreateUser`'s reason: afterwards there is no undo.
	warnScopesThatAlreadyExistOnDisk(storeRoot, []string{name}, errOut)

	made, err := control.CreateScope(ctx, store, control.NewScope{ProjectID: project, Name: name})
	if err != nil {
		switch {
		case errors.Is(err, control.ErrNoSuchProject):
			// 🔴 THE REMEDY IS THE LIST. The mistake this flag invites is a project NAME —
			// `-create-user`'s `-project` takes one — and "no such project: quarry" alone
			// sends an operator to read a JSONL file for an id this program can print.
			held := "it holds NO projects; create one with -create-user"
			if m, readErr := store.Model(ctx); readErr == nil && len(m.Projects) > 0 {
				held = "it holds " + renderProjects(m)
			}
			warn(fmt.Sprintf(
				"subsystem-store-api: -create-scope refused: this control journal holds no project "+
					"with id %q, so nothing was written. -scope-project takes an ID (`%s_…`), not a "+
					"name. Journal %s: %s",
				project, control.PrefixProject, journal, held))
		default:
			// `ErrScopeNameUnusable`, `ErrScopeNameTaken` and the journal's own refusals all
			// name the offending value and the rule, so they are relayed, not re-worded.
			warn("subsystem-store-api: -create-scope refused, and nothing was written: " + err.Error())
		}
		return exitConfig
	}

	// The record line on STDOUT, in the `cairn-control:` shape the sibling modes print —
	// nothing in it is secret. The name is inside `[A-Za-z0-9_-]`, so `name=` is one token.
	fmt.Fprintln(out, reloadSafe(fmt.Sprintf(
		"cairn-control: scope-created scope=%s name=%s project=%s epoch=%d",
		made.Scope.ID, made.Scope.Name, made.Project, made.Epoch)))

	// 🔴 WHAT IT DID NOT DO, SAID OUT LOUD, because each is a thing an operator would
	// otherwise assume from "created".
	warn(fmt.Sprintf(
		"subsystem-store-api: this wrote a RECORD and created NO directory. A scope's entries live "+
			"in a directory of that name under the store root (-store), and creating the record never "+
			"creates it: until one exists, scope %q holds no entries", made.Scope.Name))
	warn(fmt.Sprintf(
		"subsystem-store-api: every member of project %s now reaches %q at their role. A credential "+
			"NARROWED to other scopes does not, and no credential was issued: a narrowing lists scope "+
			"ids fixed at issue. Issue one with -issue-credential -narrow-scopes %s -narrow-verbs …",
		made.Project, made.Scope.Name, made.Scope.ID))
	warn(fmt.Sprintf(
		"subsystem-store-api: the running server picks this up within %s; any other program reading "+
			"this journal does so on its own refresh", refreshInterval))
	return 0
}

// renderProjects lists a journal's projects as `prj_… (name)`, sorted by id so the line is
// reproducible. Names are shown because an operator recognises a project by its name and
// needs its id; two projects may share one, which is why the id leads.
func renderProjects(m control.Model) string {
	ids := make([]string, 0, len(m.Projects))
	for id := range m.Projects {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%s (%q)", id, m.Projects[control.ID(id)].Name))
	}
	return strings.Join(parts, ", ")
}
