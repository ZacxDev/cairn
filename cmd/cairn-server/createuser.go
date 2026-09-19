package main

// `cairn-server -create-user`: the operator-driven user-creation path.
//
// 🔴 A COMMAND, NOT A ROUTE, AND THAT IS THE DESIGN DECISION RATHER THAN A CONVENIENCE.
// `AGENTS.md`: "Adding a row to a dispatch table is adding a public, internet-reachable
// endpoint." A creation endpoint is the highest-value target this pod could grow — it
// mints principals — and putting it on the HTTP surface would require the route ledger,
// the conformance corpus, the construction-time check and `checks.go-server-declares-its-routes`
// to move together, for a capability that has exactly one caller: a human with `exec`
// into the pod. So it is a flag mode that exits, reached the same way the documented
// token rotation is (`kubectl exec … -- sh -c 'kill -HUP 1'`), and it adds nothing to the
// internet-reachable surface. `api.DeclaredRoutes()` is unchanged by this file, which is
// the checkable half of that sentence.
//
// 🔴 AND IT IS NOT SIGNUP. Nothing on a read path creates a user; see
// `control.ProvisionUser`, whose comment states the rule and what signup would owe.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZacxDev/cairn/internal/api"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/store"
)

// createUserFlags is the mode's own flag set, registered by `main` before `flag.Parse`.
type createUserFlags struct {
	enabled  *bool
	provider *string
	subject  *string
	email    *string
	project  *string
	scopes   *string
}

// registerCreateUserFlags declares the mode's flags.
//
// ⚠ THEY ARE PLAIN FLAGS ON THE SERVER BINARY RATHER THAN A SUBCOMMAND, MATCHING
// `-routes`. A subcommand would mean a second `flag.FlagSet` and a second parse of
// `os.Args`, which is a second place the program decides what it was asked to do.
func registerCreateUserFlags() *createUserFlags {
	return &createUserFlags{
		enabled: flag.Bool("create-user", false,
			"CREATE A USER in the control journal named by $"+EnvControlJournal+" and exit. "+
				"An OPERATOR action: a verified session for a subject this control plane holds no "+
				"user for is refused, never provisioned, so somebody with write access to the "+
				"journal has to do this deliberately. Requires -provider, -subject and -project"),
		provider: flag.String("provider", "",
			"the identity provider's namespace, which must EQUAL what the session backend "+
				"resolves against: `supabase` (or $CAIRN_SUPABASE_PROVIDER) for the JWT backend, "+
				"$CAIRN_TRUSTED_HEADER_PROVIDER for the proxy backend. A user created under any "+
				"other string is one that backend will never find"),
		subject: flag.String("subject", "",
			"the provider's own stable id for this person — the JWT's `sub`, or what the "+
				"trusted subject header carries. NOT an email: an email is mutable at the "+
				"provider and reused across providers"),
		email: flag.String("email", "",
			"display only, and optional. It is what the audit line's identity= field and every "+
				"written bullet's ACTOR render when present; without it they carry "+
				"`<provider>:<subject>`"),
		project: flag.String("project", "",
			"the project created for this user, who becomes its OWNER — read, write and admin "+
				"over every scope the project holds"),
		scopes: flag.String("scopes", "",
			"comma-separated scope display names to create in that project. A display name is "+
				"the DIRECTORY name under the store root, and creating the record does NOT "+
				"create the directory. Empty is accepted and creates a user who can reach "+
				"nothing until a scope is created or shared"),
	}
}

// runCreateUser performs the creation and returns the process exit code.
//
// 🔴 IT RETURNS A CODE RATHER THAN CALLING `os.Exit`, SO A TEST CAN RUN IT IN-PROCESS.
// The alternative — re-executing the binary for every case — is what `main_test.go`
// already pays for the server path, and it is worth paying there because the thing under
// test IS the process. Here the thing under test is a journal write, and an in-process
// call lets a test read the journal back in the same function.
func runCreateUser(env map[string]string, storeRoot string, f *createUserFlags, out, errOut io.Writer) int {
	journal, err := controlJournalPath(env)
	if err != nil {
		fmt.Fprintln(errOut, reloadSafe("subsystem-store-api: "+err.Error()))
		return exitConfig
	}
	if journal == "" {
		// 🔴 REFUSED RATHER THAN DEFAULTED TO A PATH. A default would put a journal
		// somewhere the running pod is not reading — the two would then disagree
		// silently, and the operator's evidence that the command "worked" would be a file
		// nothing loads.
		fmt.Fprintln(errOut, reloadSafe(fmt.Sprintf(
			"subsystem-store-api: -create-user needs a control journal and $%s is not set. "+
				"Set it to the path this pod reads its journal from — a user written anywhere else "+
				"is a user no running server can resolve a session to",
			EnvControlJournal)))
		return exitConfig
	}

	store, err := control.OpenFileStore(journal)
	if err != nil {
		fmt.Fprintln(errOut, reloadSafe("subsystem-store-api: "+err.Error()))
		return exitConfig
	}

	req := control.NewUser{
		Provider:    *f.provider,
		Subject:     *f.subject,
		Email:       *f.email,
		ProjectName: *f.project,
		ScopeNames:  scopeNames(*f.scopes),
	}
	// 🔴 BEFORE THE WRITE, BECAUSE AFTERWARDS THERE IS NO UNDO. The journal is
	// append-only and nothing emits `scope-renamed`, so a scope record that aliases
	// somebody else's directory cannot be taken back — the operator's only remedy is to
	// edit the journal by hand.
	//
	// ⚠ THE COST IS THAT A REFUSED PROVISIONING STILL PRINTS IT, and that is the
	// direction to be wrong in: a warning about a scope that was not created is noise the
	// operator can read past on the very next line (the refusal names itself), while a
	// warning withheld until after a successful write is a warning about a thing that has
	// already happened.
	warnScopesThatAlreadyExistOnDisk(storeRoot, req.ScopeNames, errOut)

	made, err := control.ProvisionUser(context.Background(), store, req)
	if err != nil {
		// 🔴 78, THE SAME CODE AS EVERY OTHER REFUSAL IN THIS PROGRAM, AND THE SECOND CODE
		// THAT USED TO BE HERE IS GONE. It returned 65 ("the request was refused, the
		// deployment is fine") for every error this call can produce — including
		// `reading the control journal`, which is the opposite fault. See `exitConfig`'s
		// comment for why it was deleted rather than narrowed.
		//
		// The journal's own message names the event and the field, so it is passed
		// through rather than re-worded — a second wording here would be a second
		// description of a rule that lives in `internal/control` — and it is what
		// distinguishes a typo from a broken mount, on the stream an operator reads.
		fmt.Fprintln(errOut, reloadSafe("subsystem-store-api: -create-user refused: "+err.Error()))
		return exitConfig
	}

	// 🔴 THE IDS GO TO STDOUT AND THE CAVEATS TO STDERR, because the ids are what an
	// operator pipes somewhere and the caveats are what they read. Never a token: this
	// path mints no credential at all — a session backend authenticates against the IdP,
	// and `Identity.Fingerprint` is empty for it by design.
	fmt.Fprintln(out, reloadSafe(fmt.Sprintf(
		"cairn-control: created user=%s provider=%s subject=%s project=%s epoch=%d scopes=%s",
		made.User, req.Provider, req.Subject, made.Project, made.Epoch, renderScopes(made.Scopes))))

	if len(made.Scopes) == 0 {
		// 🔴 SAID OUT LOUD, BECAUSE FROM OUTSIDE THE POD THIS STATE IS INDISTINGUISHABLE
		// FROM THE DEFECT THIS WHOLE PATH CLOSES. The user authenticates and resolves to
		// an EMPTY `Authorization`, so every read answers as if the scope did not exist —
		// exactly what a session got before any journal was wired. It is a legitimate
		// intermediate state (create the person, decide access after), which is why it is
		// a line rather than a refusal.
		fmt.Fprintln(errOut, reloadSafe(fmt.Sprintf(
			"subsystem-store-api: WARNING user=%s can reach NOTHING: no scope was created, so this "+
				"person will authenticate and see an empty world, which looks identical to a broken "+
				"sign-in. Re-run with -scopes, or share a scope with them",
			made.User)))
	}
	// ⚠ THE RUNNING POD DOES NOT SEE THIS YET, AND THE LAG IS BOUNDED RATHER THAN
	// INSTANT. This process wrote the file; the server materializes its authority from a
	// `control.Cache` that re-reads the journal on its TIMER. Saying so is the difference
	// between an operator who waits and one who concludes the command did nothing.
	//
	// 🔴 IT PROMISES THE TIMER AND NOTHING ELSE, AND THE CLAUSE THAT USED TO OFFER
	// `kill -HUP 1` WAS DELETED WITH THE TRIGGER IT NAMED. A line advertising a mechanism
	// the program no longer has is worse than no line: the operator sends the signal, it
	// is received by the TOKEN reload (which re-reads a different file and says so), and
	// they read that verdict as confirmation the journal was re-read. `openSessionAuthority`
	// is where the trigger was and why it went.
	fmt.Fprintln(errOut, reloadSafe(fmt.Sprintf(
		"subsystem-store-api: the running server picks this up within %s", refreshInterval)))
	return 0
}

// controlJournalPath reads $CAIRN_CONTROL_JOURNAL and applies its declared blank policy.
//
// See `EnvControlJournal` for the policy and why it is `refuseBlank`. Absent, and
// present-with-the-EMPTY-string, are both "not set" — the same boundary
// `internal/identity/config.go`'s `touched` draws, and for the same reason: a manifest
// that emits every variable with an empty default is a common shape, so `X=""` is "not
// using this" and `X="  "` is a line the operator wrote and this pod would discard.
func controlJournalPath(env map[string]string) (string, error) {
	raw, present := env[EnvControlJournal]
	if !present || raw == "" {
		return "", nil
	}
	if identity.ValueReducesToNothing(raw) {
		return "", fmt.Errorf(
			"%s=%q reduces to nothing, so this pod has no control journal: a session backend "+
				"would refuse to start, and `-create-user` has nowhere to write a user to. Give it "+
				"a path or delete the line",
			EnvControlJournal, raw)
	}
	return strings.TrimSpace(raw), nil
}

// scopeNames splits the -scopes value.
//
// ⚠ COMMAS ONLY, AND NOT WHITESPACE, WHICH IS DELIBERATELY NARROWER THAN
// `identity.peerFields`. A scope display name is a DIRECTORY NAME and a directory name may
// contain a space; splitting on whitespace would silently turn one scope into two, each
// pointing at a directory that does not exist. Surrounding whitespace is trimmed because
// `-scopes "a, b"` is what a human types, and an empty element is dropped so a trailing
// comma is not a nameless scope the journal then refuses.
func scopeNames(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// warnScopesThatAlreadyExistOnDisk names every requested scope whose FOLDED display name
// already exists as a directory under the store root.
//
// 🔴 IT CLOSES THE HALF OF `checkScopeNamesAreFree` THAT IS LIVE IN EVERY DEPLOYMENT, AND
// IT IS THE ONLY PLACE THAT CAN. That guard iterates the JOURNAL's scopes; the
// machine-token world's scopes are not journal rows at all — `tokenfile.Source.storeDirs`
// enumerates the store root's subdirectories — and both worlds are narrowed against the
// one store root. On first use the journal is empty, so the guard passes unconditionally
// while the store root may already hold every existing tenant's directory. Measured: a
// store root holding `tenant-a-notes/` and `ProvisionUser(…, ScopeNames: ["tenant-a-notes"])`
// → accepted, read AND write allowed. `internal/control` cannot see this because it holds
// no path and must not grow one; this program holds `-store`.
//
// 🔴 A WARNING RATHER THAN A REFUSAL, AND THE REASON IS THAT NOTHING ON DISK ANSWERS THE
// QUESTION. An existing directory is the hazard (somebody else's tenant) AND the ordinary
// sequence (seed the store, then provision the person who owns it) — the `-scopes` flag's
// own help says creating the record does not create the directory, so an operator who
// seeded first is doing the documented thing. A directory carries no owner, so a refusal
// would reject both at the same rate and there is no override flag to escape it with.
// Same shape, and the same ruling, as `openSessionAuthority`'s empty-journal warning.
// ⚠ SO THIS IS NOT AN INVARIANT AND MUST NOT BE READ AS ONE. It closes the SIGNAL gap,
// not the hazard. **Closing condition for the hazard itself:** a scope's bytes addressed
// by its id rather than its display name — the same condition `checkScopeNamesAreFree`
// states — at which point an existing directory cannot be aliased by a name at all.
//
// ⚠ AN UNREADABLE STORE ROOT IS SILENT HERE, DELIBERATELY. `-create-user` writes to the
// journal and not to the store, so a store root this command cannot enumerate is not a
// reason to refuse a write that does not touch it — and the SERVER refuses to start over
// exactly that condition (`tokenfile.ErrStoreRootUnreadable`), which is where an operator
// meets it. What is lost is this warning, and a warning that cannot be produced is the
// same state as a store root with nothing in it: not vouched for either way.
//
// 🔴 THE ENTRY IS STATTED, NOT ASKED — AND THAT IS THE SAME PREDICATE AS
// `tokenfile.Source.storeDirs`, WHICH IS THE WHOLE POINT OF THE FUNCTION. This warning
// claims to show the operator the world the READER will narrow on, so a predicate
// narrower than the reader's makes it silent on exactly the entries that matter.
// `os.ReadDir` yields `DirEntry` values whose `IsDir` reports the entry's OWN type and
// does NOT follow a symlink; `storeDirs` calls `os.Stat`, which does. Measured at
// `8c06ea1` over a root holding `tenant-a-notes -> <a directory>` and a plain
// `tenant-c-notes`: `os.Stat`+`IsDir` saw both, `DirEntry.IsDir` saw only the plain one —
// so a tar-seeded or migrated store got no warning at all and the operator provisioned
// `RoleOwner` over another tenant's bytes with no undo. A stat ERROR (a dangling symlink,
// a directory that cannot be traversed) is skipped rather than reported, for the reason
// the paragraph above gives about the root itself: this command does not touch the store,
// and an entry it cannot resolve is not vouched for either way.
func warnScopesThatAlreadyExistOnDisk(storeRoot string, names []string, errOut io.Writer) {
	if storeRoot == "" || len(names) == 0 {
		return
	}
	dirents, err := os.ReadDir(storeRoot)
	if err != nil {
		return
	}
	// Folded directory name → the raw directory name, so the message can name what is
	// actually on disk rather than the fold of it.
	onDisk := make(map[string]string, len(dirents))
	for _, d := range dirents {
		info, statErr := os.Stat(filepath.Join(storeRoot, d.Name()))
		if statErr != nil || !info.IsDir() {
			continue
		}
		onDisk[store.NormalizeRef(d.Name())] = d.Name()
	}
	for _, name := range names {
		dir, exists := onDisk[store.NormalizeRef(name)]
		if !exists {
			continue
		}
		fmt.Fprintln(errOut, reloadSafe(fmt.Sprintf(
			"subsystem-store-api: WARNING scope %q already exists as %s/%s. The journal holds no "+
				"record of it — the store root is ALSO the machine-token world's scope list — so this "+
				"is either the directory you seeded for this person or another tenant's, and nothing "+
				"on disk says which. Creating it hands this user READ AND WRITE over whatever is in "+
				"there, and the journal is append-only: there is no undo",
			name, storeRoot, dir)))
	}
}

func renderScopes(scopes []control.ProvisionedScope) string {
	if len(scopes) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(scopes))
	for _, sc := range scopes {
		parts = append(parts, string(sc.ID)+":"+sc.Name)
	}
	return strings.Join(parts, ",")
}

// openSessionAuthority builds the journal-backed authority the SESSION backends resolve
// against, or reports that this deployment configured none.
//
// 🔴 IT RETURNS A nil `identity.ModelSource` FOR THE UNCONFIGURED CASE, AND THE nil MUST
// BE THE INTERFACE'S OWN. Returning a typed nil `*control.Cache` would make
// `sessions != nil` true inside `identity.FromEnvironment`, which is the check that
// distinguishes "no journal" from "a journal nobody reads" — a typed nil there would turn
// every existing deployment into `ErrSessionAuthorityUnread` and refuse to start. That is
// why the concrete type never leaves this function's `if`.
//
// 🔴 A CACHE, NOT THE `FileStore` ITSELF, FOR THE REASON `control.Cache` EXISTS: reads
// must not touch the authority. The `FileStore` goes in as the cache's `Source` directly —
// `FileStore.Model` re-reads the journal on every call, which is what makes the pod see a
// user `-create-user` wrote from another process. That used to take a second type
// (`control.ReloadingSource`) wrapping the same store; it was deleted, because a read path
// with two spellings is one where the wrong spelling is the default.
//
// 🔴 AND THAT CHANGES WHAT THE CACHE CAN DO, WHICH IS WORTH STATING RATHER THAN LEAVING TO
// BE FOUND. `Cache.write` type-asserts its source to `control.Writer`; `ReloadingSource`
// implemented only the read half, so the cache it fed refused `Apply`/`ApplyNow` with
// `ErrAuthorityReadOnly`. A `*FileStore` is both halves, so that refusal is gone. The
// narrowing survives by a different and stronger mechanism: this function's return type is
// `identity.ModelSource` — an interface with ONE method — so the `*control.Cache` never
// escapes as a concrete value and nothing in the serving path can reach a write at all.
// ⚠ Widening that return type to `*control.Cache` would hand the pod a write path to the
// control journal with no caller and no gate; if a reason to do it ever appears, the
// read-only refusal has to come back with it.
func openSessionAuthority(ctx context.Context, env map[string]string, warn func(string)) (identity.ModelSource, error) {
	journal, err := controlJournalPath(env)
	if err != nil {
		return nil, err
	}
	if journal == "" {
		return nil, nil
	}
	store, err := control.OpenFileStore(journal)
	if err != nil {
		return nil, err
	}
	cache := control.NewCache(store, control.CacheOptions{MaxAge: api.AuthorityMaxAge})
	// 🔴 MATERIALIZED BEFORE THE LISTENER ACCEPTS, AND A FAILURE IS FATAL — the same
	// asymmetry the Supabase key set draws between a cold start and an outage. A cache
	// that has never materialized authorises NOBODY (`control.NewCache`'s comment), so a
	// pod that came up over an unreadable journal would refuse every session while
	// looking healthy, and the operator's only signal would be users reporting 401s.
	// After one success there is last-known-good to serve and a later read failure must
	// not stop sessions already issued.
	if err := cache.Refresh(ctx); err != nil {
		return nil, err
	}
	if len(cache.Model().Users) == 0 {
		// ⚠ A WARNING RATHER THAN A REFUSAL, AND THE ASYMMETRY IS ABOUT THE ORDER AN
		// OPERATOR CAN ACTUALLY WORK IN. `control.OpenFileStore` CREATES the file if it is
		// absent, so a typo'd path yields an empty journal that authorises nobody — which
		// is worth shouting about. But an empty journal is also the legitimate state
		// between enabling this setting and running `-create-user`, and in a cluster the
		// pod starts before anybody can exec into it, so refusing would be a crash loop
		// with no way out.
		warn(fmt.Sprintf(
			"subsystem-store-api: WARNING the control journal %s holds NO users, so every browser and "+
				"proxy sign-in will be refused. If that path is a typo this file was just created empty. "+
				"Provision with `cairn-server -create-user`",
			journal))
	}
	// 🔴 THE TIMER, AND ONLY THE TIMER. A second `signal.Notify` channel was registered
	// here so an operator who had just run `-create-user` did not have to wait; it was
	// removed, and the reasoning is worth keeping because the feature reads as free.
	// What it bought was bounded by `api.AuthorityRefreshInterval` — 30 seconds — and
	// bounded from above as well, because `Cache.Run` refuses an interval larger than
	// `MaxAge` (2 minutes). What it cost was the rule `main` states sixty lines above the
	// authority timer: SIGHUP is deliberately not one of that loop's triggers, ONE
	// TRIGGER, ONE PLACE. A mechanism that saves at most half a minute on a command a
	// human types is not worth a second answer to "what makes this pod re-read".
	//
	// ⚠ SO `-create-user` PROMISES THE INTERVAL AND NOTHING ELSE. Its closing line says
	// so; if this ever grows a signal trigger again, that line moves with it.
	// 🔴 BUILT ON THE CALLER'S GOROUTINE, NOT INSIDE THE ONE BELOW, AND THAT IS A RACE FIX
	// RATHER THAN A STYLE PREFERENCE. `refreshInterval` is a package `var` that a test
	// assigns (`main_test.go`, and the reporter's own gate), so reading it inside the
	// spawned goroutine makes the read happen at whatever moment the scheduler starts it —
	// concurrently with a later test's write. Measured by `go test -race`: a WRITE from
	// one test against a READ from a refresh goroutine an EARLIER test had leaked by
	// passing `context.Background()`. Reading here makes the value sequential with every
	// other access on this goroutine.
	//
	// ⚠ THE LEAK THAT MADE IT OBSERVABLE IS CLOSED; THE REASON IS NOT. Every caller in
	// `createuser_test.go` now cancels, so no test currently leaks a loop — but that is a
	// property of the callers, not of this function, and `main`'s own context outlives
	// everything by design. A loop that reads package state at an arbitrary later instant
	// is unsafe whether or not a test happens to leak one today, which is why this stays
	// where it is rather than moving into the goroutine as a "simplification".
	triggers := control.RefreshTriggers{
		Interval:  refreshInterval,
		OnRefresh: journalRefreshReporter(journal, warn),
	}
	go func() {
		if err := cache.Run(ctx, triggers); err != nil && !errors.Is(err, context.Canceled) {
			warn("subsystem-store-api: control journal refresh loop stopped: " + err.Error())
		}
	}()
	return cache, nil
}

// journalRefreshReporter turns the refresh result this loop used to DISCARD into one
// stderr line per TRANSITION.
//
// 🔴 THE GAP IT CLOSES IS THE SILENT ONE, AND SILENT IS THE WORST OF THE THREE STATES A
// BAD JOURNAL HAS. `-create-user` exits 78 and names the line; a restarting pod refuses to
// start and stays down; and BETWEEN those two the running pod keeps serving
// last-known-good with nothing said anywhere. That is correct behaviour (an authority that
// drops the rows it cannot parse revokes people at random) and it is also how a torn
// journal — an append that hit ENOSPC, where `os.File.Write` reports a short write after
// the partial bytes are already on disk — reaches a restart as a surprise crash loop.
// `Cache.Staleness()` recorded all of it and nothing outside the tests read it.
//
// 🔴 PER TRANSITION, NOT PER REFRESH, AND THE ARITHMETIC IS WHY. The timer is 30 s, so a
// line per failure is 2,880 a day for one broken file: a volume of identical lines is how
// an operator learns to filter the stream this warning arrives on, which is the same
// outcome as not warning. Two edges are what an operator can act on — it broke, and it is
// fixed — and the RECOVERY edge is the half a failure-only hook cannot express.
//
// ⚠ IT DOES NOT RENDER `Cache.Staleness()`, AND THAT IS A DELIBERATE OMISSION RATHER
// THAN AN OVERSIGHT. The epoch being served and its age would belong in these lines, and
// `Staleness()` is exactly that value — but comments spread across this tree state, as a
// load-bearing claim, that it has **NO CALLER OUTSIDE THE TESTS**, and each of them
// reasons from that to "bounded and SILENT" about a DIFFERENT window (the token-file scope
// enumeration). WHICH comments is the PREDICATE below and never a list here: a list goes
// stale exactly the way the tally it replaced did, and the one that stood here had already
// lost `internal/identity`. Adding the first caller here would falsify all of them at once
// while closing none of the thing they defer, which is a status SURFACE — a `doctor`
// section, a route, or the startup banner `tests/dualrun/harness.py` compares. So this
// reports the EVENT and the remedy, and the value stays where those comments say it is.
//
// ⚠ NO COUNT IS QUOTED, AND THE ABSENCE IS DELIBERATE. This sentence used to say "six
// comments", which is a number in prose with no gate — the exact shape
// `tests/test_control_mutant_count_is_pinned.py` exists to refuse — and it went loose
// within one commit, which moved two of them into the past tense. What is load-bearing is
// the PREDICATE, not the population, and the predicate is mechanical:
// `find . -name '*.go' ! -name '*_test.go' -print0 | xargs -0 grep -n 'Staleness()'`
// returns only its own definition in `cache.go` and comments. Re-run that rather than
// trusting a tally.
// **Closing condition:** when that surface lands, this line renders `Staleness()` with it
// and every one of those sentences moves with it.
func journalRefreshReporter(journal string, warn func(string)) func(error) {
	// Closed over rather than package state: two authorities in one process would
	// otherwise share one edge detector and each silence the other's transitions.
	failing := false
	return func(err error) {
		switch {
		case err != nil && !failing:
			failing = true
			warn(fmt.Sprintf(
				"subsystem-store-api: WARNING the control journal %s no longer loads, and this pod is now "+
					"serving the LAST model it read successfully — sessions already resolvable stay "+
					"resolvable, anything provisioned since is invisible, and a RESTART will refuse to "+
					"come up until the file parses: %v",
				journal, err))
		case err == nil && failing:
			failing = false
			warn(fmt.Sprintf(
				"subsystem-store-api: the control journal %s loads again; this pod is serving it",
				journal))
		}
	}
}
