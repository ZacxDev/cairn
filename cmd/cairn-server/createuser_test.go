package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// flagsFor builds the mode's flag values WITHOUT touching the process's global flag set.
//
// ⚠ `registerCreateUserFlags` CALLS `flag.Bool`/`flag.String` ON THE DEFAULT SET, which
// `TestMain` has already had `flag.Parse` run over — registering again panics with "flag
// redefined". So the values are constructed directly here. That means this file does NOT
// prove the flags are registered; `TestTheBinaryActuallyDispatchesCreateUser` below runs
// the real `main` for exactly that half.
func flagsFor(provider, subject, email, project, scopes string) *createUserFlags {
	enabled := true
	return &createUserFlags{
		enabled:  &enabled,
		provider: &provider,
		subject:  &subject,
		email:    &email,
		project:  &project,
		scopes:   &scopes,
	}
}

// TestABlankControlJournalIsRefusedRatherThanReadAsUnset is the gate on the declared blank
// policy of the one setting this slice adds.
//
// 🔴 THE HAZARD IS THAT UNSET USED TO BE *PERMISSIVE IN THE SENSE THAT MATTERS*: it meant
// "resolve sessions against the token-file projection", which holds no user any identity
// provider can name, so a blank read as unset gave the operator a pod that started clean
// and authenticated people into an empty world. `identity.ErrSessionBackendWithoutAuthority`
// now refuses that at startup — so what this policy buys is the better MESSAGE, and the
// refusal on the `-create-user` path, which arms no backend and therefore never reaches
// that sentinel. A blank there without this guard is a user written to a journal at the
// path `""`, which `OpenFileStore` rejects with a message about an empty path rather than
// about the line the operator actually typed.
//
// 🔴 AND IT IS MEASURED AT TWO SPELLINGS OF "NOTHING", BECAUSE THIS REPOSITORY HAS ALREADY
// PAID FOR ONE OF THEM PASSING. Whitespace is what `strings.TrimSpace` sees; zero-width
// runes are `Cf` and it does not. A second `TrimSpace` here would accept 32 × U+200B as a
// path, which is why the predicate is `identity.ValueReducesToNothing` and not a local
// copy.
func TestABlankControlJournalIsRefusedRatherThanReadAsUnset(t *testing.T) {
	for _, arm := range []struct {
		name  string
		env   map[string]string
		want  string
		fault bool
	}{
		{name: "absent", env: map[string]string{}, want: ""},
		{
			// The EMPTY string is deliberately outside the policy, exactly as
			// `identity.touched` draws the line: a manifest that emits every variable
			// with an empty default is a common shape.
			name: "present and empty",
			env:  map[string]string{EnvControlJournal: ""},
			want: "",
		},
		{name: "one space", env: map[string]string{EnvControlJournal: " "}, fault: true},
		{name: "tabs and newlines", env: map[string]string{EnvControlJournal: "\t\n "}, fault: true},
		{
			// `TrimSpace` calls this content. `unicode.IsGraphic && !IsSpace` does not.
			name:  "zero-width runes only",
			env:   map[string]string{EnvControlJournal: strings.Repeat("\u200b", 8)},
			fault: true,
		},
		{
			name: "a real path, with surrounding whitespace trimmed",
			env:  map[string]string{EnvControlJournal: "  /data/control.journal\t"},
			want: "/data/control.journal",
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			got, err := controlJournalPath(arm.env)
			if arm.fault {
				if err == nil {
					t.Fatalf("%q was read as UNSET, which silently resolves sessions against an "+
						"authority that holds no provider-named user", arm.env[EnvControlJournal])
				}
				// The refusal must name the variable and quote what was written —
				// whitespace is invisible otherwise.
				if !strings.Contains(err.Error(), EnvControlJournal) {
					t.Fatalf("the refusal does not name the variable: %v", err)
				}
				if got != "" {
					t.Fatalf("a refused value still produced a path %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if got != arm.want {
				t.Fatalf("path = %q, want %q", got, arm.want)
			}
		})
	}
}

// TestCreateUserRefusesWithoutAJournalAndWritesOneWithIt walks the mode's exit codes.
//
// 🔴 THERE ARE TWO, AND THE THIRD WAS DELETED RATHER THAN NARROWED. 78 = this command did
// not act, 0 = written. A separate 65 ("the request was refused, the deployment is fine")
// used to sit beside them and was returned for EVERY `ProvisionUser` error, including an
// unreadable journal — a deployment fault wearing the request fault's code. See
// `exitConfig`'s comment in `main.go`.
//
// ⚠ SO THE DISTINCTION THE CODES USED TO CLAIM IS ASSERTED ON STDERR INSTEAD, AND IT IS
// ASSERTED — the two refusals below carry different text, and each arm checks its own. A
// test that only compared exit codes would now pass with both messages identical, which is
// exactly the collapse this rewrite must not smuggle in.
func TestCreateUserRefusesWithoutAJournalAndWritesOneWithIt(t *testing.T) {
	var out, errOut bytes.Buffer
	good := flagsFor("supabase", "subject-0001", "rowan@notes.example.test", "quarry", "quarry-notes")

	// 78: no journal configured. Refused rather than defaulted to a path, because a
	// default would write somewhere the running pod is not reading.
	if code := runCreateUser(map[string]string{}, "", good, &out, &errOut); code != exitConfig {
		t.Fatalf("exit = %d with no journal configured, want %d", code, exitConfig)
	}
	if !strings.Contains(errOut.String(), EnvControlJournal) {
		t.Fatalf("the refusal does not name the variable to set: %q", errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("a refused creation wrote to stdout: %q", out.String())
	}

	journal := filepath.Join(t.TempDir(), "control.journal")
	env := map[string]string{EnvControlJournal: journal}

	// 0: written.
	out.Reset()
	errOut.Reset()
	if code := runCreateUser(env, "", good, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, want 0. stderr: %s", code, errOut.String())
	}
	line := out.String()
	for _, want := range []string{"usr_", "prj_", "scp_", "provider=supabase", "subject=subject-0001", "epoch=4"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the created line does not carry %q: %q", want, line)
		}
	}
	// 🔴 AND IT MUST NOT CARRY A CREDENTIAL, BECAUSE THIS PATH MINTS NONE. A session
	// backend authenticates against the IdP; there is no token here to print, and a
	// printed secret re-stages itself in every transcript that captures the run.
	if strings.Contains(line, "token") || strings.Contains(line, "crd_") {
		t.Fatalf("the created line mentions a credential, and this path issues none: %q", line)
	}

	// 78 again, by the OTHER route: the journal refuses a second row for one
	// (provider, subject) pair. Same code as "no journal configured" above, and the
	// assertions below are what tell the two apart — which is the whole of what the
	// deleted 65 was supposed to buy.
	out.Reset()
	errOut.Reset()
	again := flagsFor("supabase", "subject-0001", "", "quarry-two", "quarry-other")
	if code := runCreateUser(env, "", again, &out, &errOut); code != exitConfig {
		t.Fatalf("exit = %d re-creating one person, want %d", code, exitConfig)
	}
	refusal := errOut.String()
	if !strings.Contains(refusal, "-create-user refused") {
		t.Fatalf("the refusal does not say it was refused: %q", refusal)
	}
	// 🔴 THE TWO 78s MUST NOT READ THE SAME, AND THIS IS THE ASSERTION THAT PINS IT. The
	// journal's own message names the offending pair; the no-journal refusal names the
	// variable to set. If those ever collapse into one wording, the exit code is all an
	// operator has left and it no longer distinguishes anything.
	if !strings.Contains(refusal, "provider, subject") {
		t.Fatalf("the refusal does not name WHY the journal declined the batch, so it is "+
			"indistinguishable from the no-journal refusal, which exits the same code: %q", refusal)
	}
	if strings.Contains(refusal, EnvControlJournal) {
		t.Fatalf("a request refusal names the journal VARIABLE, which is the other 78's "+
			"remedy and sends the operator to the manifest for a typo: %q", refusal)
	}

	// And the journal really holds exactly one user afterwards — the exit code alone is a
	// claim about the process, not about the file.
	store, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Model(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Users) != 1 || len(m.Scopes) != 1 {
		t.Fatalf("the journal holds %d users and %d scopes, want 1 and 1", len(m.Users), len(m.Scopes))
	}
}

// TestCreateUserSaysSoWhenTheUserItCreatedCanReachNothing.
//
// 🔴 THE STATE IS LEGITIMATE AND INDISTINGUISHABLE FROM THE BUG, WHICH IS WHY IT IS A LINE
// RATHER THAN EITHER SILENCE OR A REFUSAL. A user with no scope authenticates and resolves
// to an EMPTY authorization, so every read answers as if nothing exists — exactly what a
// session got before any journal could be wired. Creating a person before deciding their
// access is a real sequence, so it is allowed and announced.
func TestCreateUserSaysSoWhenTheUserItCreatedCanReachNothing(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "control.journal")
	env := map[string]string{EnvControlJournal: journal}
	var out, errOut bytes.Buffer

	if code := runCreateUser(env, "", flagsFor("supabase", "s1", "", "quarry", ""), &out, &errOut); code != 0 {
		t.Fatalf("a user with no scopes must be created: %d / %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "scopes=none") {
		t.Fatalf("the created line does not say the scope set is empty: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "can reach NOTHING") {
		t.Fatalf("no warning for a user who can reach nothing: %q", errOut.String())
	}

	// The negative control: WITH a scope, that warning must be absent. Without this the
	// assertion above would be satisfied by a line printed unconditionally.
	out.Reset()
	errOut.Reset()
	if code := runCreateUser(env, "", flagsFor("supabase", "s2", "", "quarry-two", "quarry-two-notes"), &out, &errOut); code != 0 {
		t.Fatalf("a user WITH a scope must be created: %d / %s", code, errOut.String())
	}
	if strings.Contains(errOut.String(), "can reach NOTHING") {
		t.Fatalf("the warning fired for a user who has a scope: %q", errOut.String())
	}
}

// TestNoControlJournalMeansNoSessionAuthorityAtAll is the compatibility claim.
//
// 🔴 THE nil MUST BE THE INTERFACE'S OWN nil, NOT A TYPED ONE. `identity.FromEnvironment`
// distinguishes "no journal" from "a journal nobody reads" by `sessions != nil`, and a
// typed nil `*control.Cache` in an interface is NOT nil — so returning one would make every
// existing deployment, which configures no session backend, refuse to start with
// `ErrSessionAuthorityUnread`. That is the worst available outcome: a live pod's auth path
// broken by a feature it does not use.
//
// 🔴 THE CONTEXT IS CANCELLED AND `warnings` IS UNDER A MUTEX, AND NOT BECAUSE A RACE WAS
// REPRODUCIBLE IN THE SHIPPED PRE-FIX SHAPE — IT WAS NOT. What the two do:
// `openSessionAuthority` starts `Cache.Run` on a goroutine, so a `context.Background()`
// here leaves a refresh loop running after this function returns, and that loop's
// `OnRefresh` calls `warn` on the first FAILING refresh — which `t.TempDir()`'s cleanup
// manufactures by deleting the journal as the test returns. The cancel bounds the loop's
// LIFETIME; the mutex makes the reads safe while it is still alive.
//
// ⚠ WHY THE SHIPPED SHAPE DID NOT RACE, WHICH IS THE MEASURED HALF. `Cache.Model()` takes
// `c.mu.RLock()` and `Cache.refresh()` takes `c.mu.Lock()`, and this test's own trailing
// `sessions.Model()` call sits AFTER both `warnings` reads — so it establishes
// happens-before from those reads to every later refresh, and therefore to every later
// `warn` write. Measured on the pre-fix shape at `8c06ea1`, journal removed by
// `t.TempDir()`'s cleanup, at TWO points on the interval: `ok`, no `DATA RACE`, both with
// the interval compressed to 2 ms and with it left at the production
// `api.AuthorityRefreshInterval` (30 s, given 45 s of post-cleanup life).
//
// Removing ONLY the `sessions.Model()` call from that same tree turns the 2 ms point RED,
// and the reported pair is the `warn` write from `journalRefreshReporter` against this
// function's own `len(warnings)` read. ⚠ **AT 30 s IT DOES NOT — so that row cannot
// attribute its own green, and must not be read as the edge being demonstrated.** Two
// independent runs DISAGREE on exactly that cell: one reported a race there, a later
// six-point sweep (100 ms, 1 s, 5 s, 15 s, 30 s) found none at any point but 2 ms, with
// the detector validated in both directions. The disagreement is recorded rather than
// resolved because nothing here depends on it: the claim this comment needs is the
// SHIPPED shape's green, which both runs agree on. What follows for a later editor is
// that `-race` at the production interval was not observed to catch this pair — so it is
// the 2 ms point, not a plain `go test -race`, that would catch a re-deletion of the
// `sessions.Model()` assertion.
//
// 🔴 SO IT IS KEPT FOR WHAT IT REMOVES, NOT FOR A RACE IT FIXED. WITHOUT it, this test's
// safety rested on an incidental mutex edge inside an unrelated accessor: deleting the
// `sessions.Model()` assertion as redundant would have reinstated the race — measured
// above — with no diff to anything a reader would recognise as synchronisation. WITH the
// cancel and the mutex that edge is no longer load-bearing, and that is the whole of the
// improvement. The goroutine-leak half stands on its own: a test that leaks a refresh loop
// into the rest of the package is a defect whether or not it races.
//
// ⚠ RETRACTED, RECORDED SO NOBODY RE-DERIVES IT. An earlier draft of this comment claimed
// the same race recurs "at ~35 s" with the production interval, and that a loaded runner,
// `-count>1` or one more slow test would turn the `go` CI job red for a reason no diff
// explains. The 30 s measurement above refutes it on its own terms — the shipped shape is
// green there, so there is no race for a loaded runner to expose. ⚠ It refutes the
// PREDICTION; it does not on its own establish WHY, because at 30 s neither shape reports
// a race (see above). NOTHING replaces that prediction — there is no timing claim in this
// comment.
func TestNoControlJournalMeansNoSessionAuthorityAtAll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var warnings []string
	warn := func(line string) { mu.Lock(); defer mu.Unlock(); warnings = append(warnings, line) }
	said := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), warnings...)
	}

	sessions, err := openSessionAuthority(ctx, map[string]string{}, warn)
	if err != nil {
		t.Fatalf("an unconfigured deployment must not fail: %v", err)
	}
	if sessions != nil {
		t.Fatalf("an unconfigured deployment produced a session authority of type %T — every "+
			"deployment that sets nothing new would then refuse to start", sessions)
	}
	if lines := said(); len(lines) != 0 {
		t.Fatalf("an unconfigured deployment emitted %d line(s) on the operator stream: %v. "+
			"`tests/dualrun/harness.py` compares the two servers' streams line for line",
			len(lines), lines)
	}

	// The positive control on the same call: a CONFIGURED journal does produce one, and
	// warns because it is empty. Without it the nil above is indistinguishable from a
	// function that always returns nil.
	journal := filepath.Join(t.TempDir(), "control.journal")
	sessions, err = openSessionAuthority(ctx, map[string]string{EnvControlJournal: journal}, warn)
	if err != nil {
		t.Fatalf("a configured journal must open: %v", err)
	}
	if sessions == nil {
		t.Fatal("a configured journal produced no session authority")
	}
	if lines := said(); len(lines) != 1 || !strings.Contains(lines[0], "NO users") {
		t.Fatalf("an EMPTY journal did not announce itself: %v. `OpenFileStore` creates the file, "+
			"so a typo'd path yields an authority that refuses every sign-in", lines)
	}
	if got := len(sessions.Model().Users); got != 0 {
		t.Fatalf("the fresh journal projected %d users", got)
	}
}

// TestTheSentinelNamesTheVariableThisProgramReads pins the one spelling of
// `CAIRN_CONTROL_JOURNAL` that lives outside this package.
//
// 🔴 `identity.ErrSessionBackendWithoutAuthority` WRITES THE VARIABLE'S NAME INTO ITS OWN
// MESSAGE, WHICH IS A SECOND COPY OF A FACT THIS PROGRAM OWNS. `internal/identity` cannot
// import `cmd/cairn-server`, so the constant cannot be shared; what can be shared is a
// failure when the two disagree. Rename `EnvControlJournal` without touching the sentinel
// and an operator is told to set a variable no binary reads — advice that is worse than
// none, because following it produces no change and no error.
//
// ⚠ IT IS A GUARD ON A SPELLING, WHICH IS EXACTLY THE SHAPE THAT IS USUALLY WALKABLE, AND
// HERE THAT IS THE POINT RATHER THAN THE WEAKNESS: the hazard IS a spelling. The
// relationship pinned is "the sentence an operator reads names the variable this program
// looks up", and the substring check is that relationship stated at its own width.
func TestTheSentinelNamesTheVariableThisProgramReads(t *testing.T) {
	message := identity.ErrSessionBackendWithoutAuthority.Error()
	if !strings.Contains(message, EnvControlJournal) {
		t.Fatalf("the sentinel does not name $%s, so it sends an operator to a variable this "+
			"program does not read:\n  %s", EnvControlJournal, message)
	}
	// The positive control: this assertion CAN fail. A name this program does not read
	// must not be found in it — without this, the check above passes for a sentinel that
	// mentions every plausible variable, and for a `strings.Contains` reading a constant
	// that had become the empty string.
	if EnvControlJournal == "" {
		t.Fatal("EnvControlJournal is empty, so the assertion above is satisfied by any string at all")
	}
	if strings.Contains(message, "CAIRN_CONTROL_LEDGER") {
		t.Fatalf("the sentinel names a variable nothing reads:\n  %s", message)
	}
}

// TestASessionAuthorityOverAProvisionedJournalSeesTheUser closes the loop between the two
// halves of this slice, in the program that wires them.
//
// ⚠ IT IS THE SEAM, AND IT IS THE ONE THING NEITHER PACKAGE'S OWN TESTS CAN SEE.
// `internal/control` proves the provisioning path writes a world that replays;
// `internal/identity` proves a session resolves against an authority that holds it.
// Whether the authority THIS PROGRAM builds from `$CAIRN_CONTROL_JOURNAL` is the one the
// operator command wrote to is a property of neither.
func TestASessionAuthorityOverAProvisionedJournalSeesTheUser(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "control.journal")
	env := map[string]string{EnvControlJournal: journal}
	var out, errOut bytes.Buffer
	if code := runCreateUser(env, "", flagsFor("supabase", "subject-0001", "", "quarry", "quarry-notes"), &out, &errOut); code != 0 {
		t.Fatalf("provisioning: %d / %s", code, errOut.String())
	}

	// ⚠ CANCELLED FOR THE REASON `TestNoControlJournalMeansNoSessionAuthorityAtAll`
	// STATES: `openSessionAuthority` starts `Cache.Run` on a goroutine, and a
	// `context.Background()` here leaves that loop running for the rest of the process —
	// reading a journal `t.TempDir()` has already deleted, against package state a later
	// test writes. No shared slice here, so this arm is a leak rather than a race; it is
	// closed anyway, because "harmless today" is decided by what the loop touches and
	// that set grows.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sessions, err := openSessionAuthority(ctx, env, func(string) {})
	if err != nil {
		t.Fatalf("opening the session authority over the journal just written: %v", err)
	}
	model := sessions.Model()
	user, held := model.UserByProviderSubject("supabase", "subject-0001")
	if !held {
		t.Fatal("the authority this program builds does not hold the user this program created")
	}
	principal, known := model.PrincipalFor(control.KindUser, user.ID)
	if !known {
		t.Fatal("the created user resolves to no principal")
	}
	auth := control.Resolve(model, principal)
	if !auth.VisibleScopes(control.VerbRead).Allows("quarry-notes") {
		t.Fatal("the provisioned user reaches no scope through the authority this program serves — " +
			"an empty authorization is what every deployment produced before this slice")
	}
}

// TestAScopeThatAlreadyExistsUnderTheStoreRootIsCalledOut.
//
// 🔴 THE HOLE IT COVERS IS THE ONE `checkScopeNamesAreFree` IS STRUCTURALLY BLIND TO, AND
// IT IS LIVE IN EVERY DEPLOYMENT. That guard iterates the JOURNAL's scopes; the
// machine-token world's scopes are the store root's SUBDIRECTORIES, and on first use the
// journal is empty — so the guard passes unconditionally while the store root may already
// hold every existing tenant's directory. Measured: a store root holding `tenant-a-notes/`
// and a provisioning naming `tenant-a-notes` → accepted, read AND write allowed.
//
// ⚠ IT ASSERTS A WARNING AND AN EXIT CODE OF ZERO, NOT A REFUSAL, AND THAT IS THE
// CONTRACT RATHER THAN A WEAK TEST. An existing directory is both the hazard and the
// ordinary sequence, and nothing on disk tells them apart — see
// `warnScopesThatAlreadyExistOnDisk`. A test asserting a refusal here would be asserting
// an invariant this program does not have.
func TestAScopeThatAlreadyExistsUnderTheStoreRootIsCalledOut(t *testing.T) {
	root := t.TempDir()
	// The pre-existing tenant, spelled the way a directory on disk is spelled, and a
	// FILE beside it that must not be mistaken for a scope.
	if err := os.MkdirAll(filepath.Join(root, "Tenant_A_Notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tenant-b-notes"), []byte("not a scope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(t.TempDir(), "control.journal")
	env := map[string]string{EnvControlJournal: journal}

	var out, errOut bytes.Buffer
	// 🔴 THE REQUESTED NAME FOLDS ONTO THE DIRECTORY RATHER THAN EQUALLING IT. A warning
	// built on a raw `==` would stay silent here, which is the same defect one layer up.
	code := runCreateUser(env, root,
		flagsFor("supabase", "subject-0001", "", "quarry", "tenant-a-notes,quarry-plans"), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — this is a warning, not a refusal:\n%s", code, errOut.String())
	}
	warning := errOut.String()
	for _, want := range []string{"tenant-a-notes", "Tenant_A_Notes", root, "no undo"} {
		if !strings.Contains(warning, want) {
			t.Fatalf("the warning does not carry %q — an operator cannot act on it:\n%s", want, warning)
		}
	}
	// 🔴 THE NEGATIVE CONTROL, AND IT IS WHAT STOPS THIS BEING A WARNING ABOUT EVERY
	// SCOPE. `quarry-plans` exists nowhere under the root and must not be named; neither
	// must `tenant-b-notes`, which exists as a FILE and is not a scope directory.
	for _, unwanted := range []string{"quarry-plans", "tenant-b-notes"} {
		if strings.Contains(warning, "WARNING scope \""+unwanted+"\"") {
			t.Fatalf("the warning names %q, which is not a directory under the store root:\n%s", unwanted, warning)
		}
	}

	// 🔴 AND THE OTHER NEGATIVE CONTROL: with NO store root the command is silent about
	// disk entirely, rather than warning on every name. Every other test in this file
	// passes "" for that reason, so a warning that fired unconditionally would have been
	// caught there too — this states it rather than relying on it.
	out.Reset()
	errOut.Reset()
	if code := runCreateUser(map[string]string{EnvControlJournal: filepath.Join(t.TempDir(), "j")}, "",
		flagsFor("supabase", "subject-0002", "", "quarry-two", "tenant-a-notes"), &out, &errOut); code != 0 {
		t.Fatalf("exit = %d with no store root:\n%s", code, errOut.String())
	}
	if strings.Contains(errOut.String(), "already exists as") {
		t.Fatalf("a command with no store root warned about disk anyway:\n%s", errOut.String())
	}
}

// TestTheStoreRootWarningSeesADirectoryTHROUGHASymlinkLikeTheReaderDoes.
//
// 🔴 THE LEDGER ABOVE IS NARROWER THAN THE PREDICATE IT EXISTS TO MIRROR, WHICH IS WHY
// THIS ROW IS SEPARATE. `TestAScopeThatAlreadyExistsUnderTheStoreRootIsCalledOut` plants a
// directory and a regular FILE, so it reads as "directories yes, non-directories no" — but
// the world it closes the gap on is `tokenfile.Source.storeDirs`, whose predicate is
// `os.Stat` + `info.IsDir()`, i.e. "anything that STATS as a directory". `os.ReadDir`
// hands back `DirEntry` values whose `IsDir` reports the entry's OWN type and does NOT
// follow a symlink, so the two disagree on exactly the shape a tar-seeded or migrated
// store produces.
//
// Measured at `8c06ea1`, over a store root holding `tenant-a-notes -> <dir>` and a plain
// `tenant-c-notes`: `os.Stat`+`IsDir` saw `[tenant-a-notes tenant-c-notes]` and
// `DirEntry.IsDir` saw `[tenant-c-notes]`. The reader therefore serves the symlinked
// directory as a scope while this warning stays silent, and the operator provisions
// `RoleOwner` — read AND write — over another tenant's bytes with no undo.
//
// ⚠ THE TWO NEGATIVE ARMS ARE WHAT STOP THE FIX BEING "WARN ABOUT EVERY ENTRY". A symlink
// to a FILE stats as a file and is not a scope; a DANGLING symlink stats to an error and
// must be skipped rather than propagated — `os.ReadDir` lists it, `os.Stat` fails on it,
// and a warning path that treated a stat error as a hit would fire on every broken link
// left behind by a half-finished migration.
func TestTheStoreRootWarningSeesADirectoryTHROUGHASymlinkLikeTheReaderDoes(t *testing.T) {
	root := t.TempDir()
	// The real bytes live outside the store root, which is what a PV mount or a
	// tar-seeded migration produces.
	elsewhere := t.TempDir()
	tenantA := filepath.Join(elsewhere, "tenant-a")
	if err := os.MkdirAll(tenantA, 0o755); err != nil {
		t.Fatal(err)
	}
	plainFile := filepath.Join(elsewhere, "a-file")
	if err := os.WriteFile(plainFile, []byte("not a scope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// symlink → directory: the reader sees a scope here, so the warning must too.
	//
	// ⚠ FATAL RATHER THAN A SKIP ON A FILESYSTEM WITHOUT SYMLINKS, DELIBERATELY. A skip
	// nobody counts is a pass, and this is the only row that measures the predicate the
	// reader actually uses — a silent skip would leave the whole finding unguarded on
	// whatever host could not make the link.
	if err := os.Symlink(tenantA, filepath.Join(root, "tenant-a-notes")); err != nil {
		t.Fatalf("this filesystem cannot make a symlink (%v), so the one predicate this "+
			"test exists to measure cannot be exercised here", err)
	}
	// symlink → regular file: not a scope on either side.
	if err := os.Symlink(plainFile, filepath.Join(root, "tenant-b-notes")); err != nil {
		t.Fatal(err)
	}
	// dangling symlink: `os.ReadDir` lists it, `os.Stat` fails on it.
	if err := os.Symlink(filepath.Join(elsewhere, "gone"), filepath.Join(root, "tenant-d-notes")); err != nil {
		t.Fatal(err)
	}
	// A plain directory, so a fix that only ever followed links would be caught too.
	if err := os.MkdirAll(filepath.Join(root, "tenant-c-notes"), 0o755); err != nil {
		t.Fatal(err)
	}

	journal := filepath.Join(t.TempDir(), "control.journal")
	env := map[string]string{EnvControlJournal: journal}
	var out, errOut bytes.Buffer
	code := runCreateUser(env, root, flagsFor("supabase", "subject-0001", "", "quarry",
		"tenant-a-notes,tenant-b-notes,tenant-c-notes,tenant-d-notes"), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — this is a warning, not a refusal, and a dangling "+
			"symlink under the store root must not turn it into one:\n%s", code, errOut.String())
	}
	warning := errOut.String()

	// The two that must be named: the symlinked directory and the plain one.
	for _, want := range []string{"tenant-a-notes", "tenant-c-notes"} {
		if !strings.Contains(warning, "WARNING scope \""+want+"\"") {
			t.Fatalf("the warning does not name %q, which the READER resolves to a scope "+
				"directory (`tokenfile.Source.storeDirs` stats the entry). An operator who "+
				"is not told provisions read AND write over it, and the journal is "+
				"append-only:\n%s", want, warning)
		}
	}
	// The two that must not be: a symlink to a file, and a dangling one.
	for _, unwanted := range []string{"tenant-b-notes", "tenant-d-notes"} {
		if strings.Contains(warning, "WARNING scope \""+unwanted+"\"") {
			t.Fatalf("the warning names %q, which does not stat as a directory — so this "+
				"warns about entries that are not scopes, which is the same outcome as "+
				"silence:\n%s", unwanted, warning)
		}
	}
}

// TestABrokenControlJournalIsSaidONCEAndItsRecoverySaidONCE.
//
// 🔴 THE STATE IT COVERS IS THE SILENT ONE, WHICH IS THE WORST OF THE THREE A BAD JOURNAL
// HAS. `-create-user` exits 78 and names the line; a restarting pod refuses to start and
// stays down; and between them a RUNNING pod keeps serving last-known-good with nothing
// said anywhere, so a torn journal reaches the next restart as a surprise crash loop.
// `control.Cache.Run` discarded every refresh error and `Staleness()` was read by nothing
// outside the tests.
//
// 🔴 AND IT ASSERTS THE COUNT, NOT MERELY THAT SOMETHING WAS SAID. A line per failed
// refresh is 2,880 a day at the 30 s interval for one broken file — a volume of identical
// lines is how an operator learns to filter the stream, which is the same outcome as
// silence. Two edges are what can be acted on. The second failure below is the arm that
// fails if the edge detector is deleted.
func TestABrokenControlJournalIsSaidONCEAndItsRecoverySaidONCE(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "control.journal")

	var said []string
	report := journalRefreshReporter(journal, func(line string) { said = append(said, line) })

	broken := errors.New("control journal " + journal + ": line 3: unexpected end of JSON input")
	// 🔴 THE SEQUENCE IS THE TEST. Two failures in a row must produce ONE line; two
	// successes in a row must produce ONE line; and the journal breaking AGAIN after a
	// recovery must be said again, because a latch that only ever fires once is
	// indistinguishable from an edge detector over a single outage.
	for _, e := range []error{broken, broken, broken, nil, nil, broken} {
		report(e)
	}
	if len(said) != 3 {
		t.Fatalf("expected 3 transition lines from 6 refreshes, got %d:\n%s", len(said), strings.Join(said, "\n"))
	}
	for _, want := range []string{journal, "no longer loads", "RESTART", "line 3"} {
		if !strings.Contains(said[0], want) {
			t.Fatalf("the failure line does not carry %q:\n%s", want, said[0])
		}
	}
	if !strings.Contains(said[1], "loads again") {
		t.Fatalf("the recovery line does not say so:\n%s", said[1])
	}
	if !strings.Contains(said[2], "no longer loads") {
		t.Fatalf("the second failure was not reported as a failure:\n%s", said[2])
	}

	// 🔴 THE NEGATIVE CONTROL, AND WITHOUT IT A REPORTER THAT PRINTED ON EVERY CALL WOULD
	// STILL FAIL THE COUNT ABOVE FOR THE WRONG REASON. A reporter that has never seen a
	// failure must say NOTHING about a run of successes — that is the ordinary state of
	// every pod in the fleet, and a line there would be noise on every refresh forever.
	said = nil
	quiet := journalRefreshReporter(journal, func(line string) { said = append(said, line) })
	for range 5 {
		quiet(nil)
	}
	if len(said) != 0 {
		t.Fatalf("a healthy journal produced %d lines:\n%s", len(said), strings.Join(said, "\n"))
	}
}

// TestTheRunningPodSAYSSoWhenItsControlJournalGoesBad is the WIRING, which the test above
// is structurally blind to.
//
// 🔴 `TestABrokenControlJournalIsSaidONCEAndItsRecoverySaidONCE` CALLS
// `journalRefreshReporter` DIRECTLY, SO IT WOULD PASS WITH THE `OnRefresh` FIELD DELETED
// FROM `openSessionAuthority`. That is the same "a capability that exists in a function
// nobody routes to" shape `main-never-dispatches-create-user` exists for, and it is the
// criterion this battery uses to justify covering `cmd/cairn-server` at all: a mitigation
// with no gate is a mitigation nobody can be told has stopped working. This drives the
// real loop — `openSessionAuthority` builds the cache, starts `Cache.Run`, and the
// journal is torn UNDER it.
//
// ⚠ IT SHORTENS `refreshInterval`, WHICH IS THE ONLY REASON IT IS FAST. The production
// value is 30 s; the variable is a package `var` for exactly this, and it is restored.
func TestTheRunningPodSAYSSoWhenItsControlJournalGoesBad(t *testing.T) {
	saved := refreshInterval
	refreshInterval = 5 * time.Millisecond
	t.Cleanup(func() { refreshInterval = saved })

	journal := filepath.Join(t.TempDir(), "control.journal")
	env := map[string]string{EnvControlJournal: journal}
	var out, errOut bytes.Buffer
	if code := runCreateUser(env, "", flagsFor("supabase", "subject-0001", "", "quarry", "quarry-notes"), &out, &errOut); code != 0 {
		t.Fatalf("provisioning a starting world: %d / %s", code, errOut.String())
	}
	healthy, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var lines []string
	warn := func(line string) { mu.Lock(); lines = append(lines, line); mu.Unlock() }
	saw := func(sub string) bool {
		mu.Lock()
		defer mu.Unlock()
		for _, l := range lines {
			if strings.Contains(l, sub) {
				return true
			}
		}
		return false
	}
	dump := func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(lines, "\n")
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s. Operator stream so far:\n%s", what, dump())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := openSessionAuthority(ctx, env, warn); err != nil {
		t.Fatalf("opening the session authority: %v", err)
	}

	// 🔴 THE NEGATIVE CONTROL FIRST, AND IT IS WHAT MAKES THE LINE BELOW MEAN ANYTHING. A
	// HEALTHY journal must stay silent across many refreshes; a reporter wired to print on
	// every tick would satisfy the assertion after it while flooding a working pod.
	time.Sleep(40 * refreshInterval)
	if saw("no longer loads") || saw("loads again") {
		t.Fatalf("a healthy journal produced transition lines:\n%s", dump())
	}

	// A TORN last line — the ENOSPC shape: `os.File.Write` reports a short write after the
	// partial bytes are already appended, so the tail is half an event.
	if err := os.WriteFile(journal, append(append([]byte{}, healthy...), []byte(`{"kind":"user-crea`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor("the failure to be reported", func() bool { return saw("no longer loads") })
	if !saw("journal line") {
		t.Fatalf("the reported line does not carry the journal's own parse error:\n%s", dump())
	}

	// …and the recovery, which is the half a failure-only hook could never express.
	if err := os.WriteFile(journal, healthy, 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor("the recovery to be reported", func() bool { return saw("loads again") })
}

// TestTheBinaryActuallyDispatchesCreateUser is the half the in-process tests cannot reach:
// that `main` REGISTERS the flag and ROUTES to the mode.
//
// 🔴 WITHOUT IT EVERY TEST ABOVE WOULD PASS WITH THE DISPATCH DELETED. `runCreateUser` is
// called directly there, so a `main` that never looked at `-create-user` — or that fell
// through into the server path — would leave them all green and the capability
// unreachable. This runs the real program.
func TestTheBinaryActuallyDispatchesCreateUser(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(t.TempDir(), "control.journal")

	run := func(args ...string) (int, string) {
		t.Helper()
		// 🔴 UNDER A DEADLINE, BECAUSE THE FAILURE SHAPE IS "IT FELL THROUGH AND SERVED".
		// A `-create-user` that reached the server path would not return, and a hanging
		// test is read as infrastructure rather than as a finding.
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
			t.Fatalf("the child did not exit within 20s — `%v` did not take the create-user path:\n%s", args, body)
		}
		if child.ProcessState == nil {
			t.Fatalf("no process state for `%v`:\n%s", args, body)
		}
		return child.ProcessState.ExitCode(), string(body)
	}

	code, body := run("-create-user", "-provider", "supabase", "-subject", "subject-0001",
		"-project", "quarry", "-scopes", "quarry-notes")
	if code != 0 {
		t.Fatalf("`-create-user` exited %d:\n%s", code, body)
	}
	if !strings.Contains(body, "cairn-control: created user=usr_") {
		t.Fatalf("the child did not print a created line:\n%s", body)
	}
	if _, err := os.Stat(journal); err != nil {
		t.Fatalf("the child reported success and wrote no journal: %v", err)
	}

	// 🔴 TWO MODES AT ONCE IS A REFUSAL. Checking `-routes` first and returning would
	// print the ledger and silently NOT create the user — exit 0, plausible output, and an
	// operator who believes a person now has access.
	code, body = run("-routes", "-create-user", "-provider", "supabase", "-subject", "subject-0002",
		"-project", "quarry-two")
	if code != exitConfig {
		t.Fatalf("`-routes -create-user` exited %d, want %d:\n%s", code, exitConfig, body)
	}
	if strings.Contains(body, "created user=") {
		t.Fatalf("`-routes -create-user` created a user anyway:\n%s", body)
	}

	// The positive control on this harness: `-routes` ALONE still prints the ledger and
	// exits 0. Without it the refusal above is indistinguishable from a binary that
	// refuses every flag combination.
	code, body = run("-routes")
	if code != 0 || !strings.Contains(body, "GET recall") {
		t.Fatalf("`-routes` alone must still print the ledger: exit %d\n%s", code, body)
	}
}
