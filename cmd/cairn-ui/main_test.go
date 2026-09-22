package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/envalias"
)

// The startup refusals are the only thing standing between a misconfigured deployment and
// a surface that passes its health check and can serve nobody. Until this file existed
// they were verified by hand and reported in a commit message — the same "a number in
// prose with no harness" defect an audit round had just filed against this PR's own cost
// comment, repeated one commit later on the guard that round added. So: a test.
//
// 🔴 ALMOST EVERYTHING HERE DRIVES THE PREDICATE AND `openAuthority`, NOT THE PROCESS.
// What can be wrong is WHICH STATES the guard admits, and that is what these cases vary;
// a test that spawned the binary would measure the same predicate through a slower door
// and would still not cover a state nobody thought of.
//
// ⚠ THIS PARAGRAPH USED TO SAY "THE EXIT CODE IS ONE `os.Exit(exitConfig)` IN `main`" AND
// READ AS A REASON NEVER TO SPAWN ANYTHING, AND ONE CASE NOW DOES. `main` carries TEN of
// them, and whether it ACTS on a refusal is wiring rather than a predicate: a
// mutation sweep replaced `if journalErr != nil { … }` with `_ = journalErr` and this
// package stayed GREEN while the built binary served. `TestMain` and
// `TestTheProcessExitsOnAWhitespaceControlJournalLine`, at the bottom of this file, are
// the exception and say why they are one.

func TestAnAbsentOrUnusableControlJournalIsRefusedBeforeTheListener(t *testing.T) {
	dir := t.TempDir()

	empty := filepath.Join(dir, "empty.journal")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// 🔴 ONE BYTE, BECAUSE ONE BYTE IS WHAT DEFEATED THE FIRST SPELLING OF THIS GUARD.
	// A draft refused `info.Size() == 0`; a journal holding a single newline replays clean
	// and the surface came up serving nobody.
	oneByte := filepath.Join(dir, "newline.journal")
	if err := os.WriteFile(oneByte, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	aDir := filepath.Join(dir, "a-directory")
	if err := os.MkdirAll(aDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, arm := range []struct {
		name    string
		journal string
		want    string
	}{
		{"a path that does not exist", filepath.Join(dir, "no", "such.journal"), "cannot be read"},
		{"a directory", aDir, "is a DIRECTORY"},
	} {
		if _, err := openAuthority(arm.journal, dir, ""); err == nil {
			t.Errorf("%s: openAuthority returned no error; a surface that starts here passes its "+
				"health check and refuses every sign-in", arm.name)
		} else if !strings.Contains(err.Error(), arm.want) {
			t.Errorf("%s: refusal is %q, want it to contain %q", arm.name, err.Error(), arm.want)
		}
	}

	// 🔴 AND THE FILE THE REFUSAL NAMES MUST NOT BE CREATED BY THE PROGRAM COMPLAINING
	// ABOUT IT. `control.OpenFileStore` does `MkdirAll` then `O_CREATE`, so the `Stat`
	// ordering is the whole reason a typo'd path stays a typo rather than becoming an
	// empty journal this program then has to refuse for a second reason.
	missing := filepath.Join(dir, "no", "such.journal")
	if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Errorf("openAuthority created %s while refusing it", filepath.Dir(missing))
	}

	// An empty and a one-byte journal both OPEN fine — they are refused by the state
	// check, not by `openAuthority`, and that split is the point of having two.
	for _, arm := range []struct {
		name    string
		journal string
	}{
		{"an empty journal", empty},
		{"a one-byte journal", oneByte},
	} {
		cache, err := openAuthority(arm.journal, dir, "")
		if err != nil {
			t.Fatalf("%s: openAuthority refused it (%v); this arm exists to reach the STATE check", arm.name, err)
		}
		if err := cache.Refresh(context.Background()); err != nil {
			t.Fatalf("%s: refresh: %v", arm.name, err)
		}
		if err := refuseAnAuthorityNobodyCanSignInTo(cache, arm.journal); err == nil {
			t.Errorf("%s: the state check admitted an authority with no usable credential", arm.name)
		}
	}
}

// TestAJournalWithUsersAndNoCredentialIsRefused is the case the SECOND spelling of this
// guard admitted, and it is the one the guard's own remedy used to produce.
//
// 🔴 `cairn-server -create-user` MINTS A USER AND NO CREDENTIAL — `createuser.go` says so
// in as many words — so a journal seeded exactly as the old refusal instructed had users,
// zero credentials, and could authenticate nobody. A `len(Users) == 0` check passes it.
// Measured end to end on both built binaries before this test was written.
func TestAJournalWithUsersAndNoCredentialIsRefused(t *testing.T) {
	cache, journal := seededJournal(t, noCredential)
	err := refuseAnAuthorityNobodyCanSignInTo(cache, journal)
	if err == nil {
		t.Fatal("a journal with a user and NO credential was admitted. `control.Authenticate` matches " +
			"a presented token against LIVE credentials, so this deployment answers 401 to every " +
			"sign-in while announcing itself writable — the exact state the refusal names.")
	}
	// The message has to name the real remedy, because the obvious one does not work.
	for _, want := range []string{"NO USABLE CREDENTIAL", "create-user", "does not fix", "1 user(s)"} {
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
			t.Errorf("the refusal does not mention %q, so an operator follows the wrong remedy:\n%s", want, err)
		}
	}
}

// TestAJournalWhoseCredentialsAreALLREVOKEDIsRefused covers the one clause of the
// predicate a mutation sweep found UNCOVERED.
//
// 🔴 DELETING `if !c.Live() { continue }` LEFT THIS PACKAGE GREEN — measured, in a sweep
// whose two controls both moved (an always-refuse mutant and an always-admit mutant were
// each killed). A journal whose every credential carries `credential-revoked` is a real
// operational state — a rotation with the new credential not yet issued — and without
// that clause the surface starts and can authenticate nobody.
//
// ⚠ THE OTHER CLAUSE, `PrincipalFor`, IS EQUIVALENT AND DELIBERATELY HAS NO TEST — AND
// THE REASON RESTS ON THREE CONSTRAINTS, NOT ONE, BECAUSE A CREDENTIAL'S PRINCIPAL MAY BE
// A PROJECT AS WELL AS A USER (`control.Kind.Valid()` accepts both). (a) `control.FileStore`
// refuses a `credential-issued` naming a subject the model does not hold
// (`event N (credential-issued): subject … does not exist`); (b) `AllEventKinds` has no
// user-deletion, project-removal or project-RENAME kind — only `scope-renamed` — so a held
// principal cannot stop being held or lose its name; and (c) `Event.validate` requires a
// non-empty `name` on `project-created`, which is the only thing stopping `displayOf`
// returning "" for a project the model DOES hold, which is how `PrincipalFor` reports
// false. No journal can reach the state this clause guards, so a test for it would mean
// writing a journal the store refuses to replay.
//
// 🔴 ALL THREE ARE LOAD-BEARING, AND (b) AND (c) ARE THE ONES A LATER CHANGE BREAKS. Add a
// `project-renamed` kind without a non-empty-name check, or any principal-removal kind, and
// this clause becomes REACHABLE while this comment still reads as a valid reason not to
// test it. An audit round found the earlier version of this paragraph naming only (a) and
// the user half of it.
func TestAJournalWhoseCredentialsAreALLREVOKEDIsRefused(t *testing.T) {
	cache, journal := seededJournal(t, credentialRevoked)
	err := refuseAnAuthorityNobodyCanSignInTo(cache, journal)
	if err == nil {
		t.Fatal("a journal whose only credential is REVOKED was admitted. `control.Authenticate` skips a " +
			"revoked credential, so this deployment answers 401 to every sign-in while announcing " +
			"itself writable.")
	}
	// 🔴 THE MESSAGE IS PINNED HERE FOR THE REASON ITS SIBLING PINS ONE, AND THIS IS THE
	// STATE WHERE IT MATTERS MOST. An operator hitting the revoked case needs to read that a
	// credential EXISTS and is not live — "1 credential record(s), 0 of them live" — not
	// that they have none. A refusal reworked to report `0 credential record(s)` here would
	// leave the no-credential test green and send them looking for a record that is sitting
	// in the journal, revoked.
	for _, want := range []string{"NO USABLE CREDENTIAL", "1 credential record(s)", "0 of them live"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q, so an operator cannot tell a REVOKED credential "+
				"from an absent one:\n%s", want, err)
		}
	}
}

// TestAJournalWithALiveCredentialIsAdmitted is the POSITIVE CONTROL. Without it every
// assertion above is satisfied by a guard that refuses everything.
func TestAJournalWithALiveCredentialIsAdmitted(t *testing.T) {
	cache, journal := seededJournal(t, credentialLive)
	if err := refuseAnAuthorityNobodyCanSignInTo(cache, journal); err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: a journal with a live, attributable credential was refused "+
			"(%v). Every refusal in this file would then be about a guard that admits nothing.", err)
	}
	// And the token-file branch must be exempt, or the guard fires where it cannot apply.
	if err := refuseAnAuthorityNobodyCanSignInTo(cache, ""); err != nil {
		t.Errorf("the state check fired with no -control-journal, where the token-file projection "+
			"synthesizes its own credentials and this check cannot apply: %v", err)
	}
}

// credentialState is which of the three shapes a seeded journal carries. Named rather
// than a bool, because the third state — a credential that exists and is REVOKED — is the
// one a bool could not express and the one a mutation sweep found uncovered.
type credentialState int

const (
	noCredential credentialState = iota
	credentialLive
	credentialRevoked
)

// seededJournal builds a real `control.FileStore` journal in the requested state.
func seededJournal(t *testing.T, state credentialState) (*control.Cache, string) {
	t.Helper()
	at := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "seeded.journal")
	store, err := control.OpenFileStore(path)
	if err != nil {
		t.Fatalf("opening the journal: %v", err)
	}
	user := control.DerivedID(control.PrefixUser, "startup-user")
	project := control.DerivedID(control.PrefixProject, "startup-project")
	events := []control.Event{
		{Kind: control.EventUserCreated, At: at, UserID: user,
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-000000000031",
			Email: "startup@notes.example.invalid"},
		{Kind: control.EventProjectCreated, At: at, ProjectID: project, Name: "startup", UserID: user},
		{Kind: control.EventMemberSet, At: at, ProjectID: project, UserID: user, Role: control.RoleOwner},
	}
	if state != noCredential {
		events = append(events, control.Event{
			Kind: control.EventCredentialIssued, At: at, CredentialID: "crd_startup",
			SubjectKind: control.KindUser, SubjectID: user,
			TokenHash: control.HashToken("fixture-startup-credential-not-a-real-token"),
			Label:     "fixture",
		})
	}
	if state == credentialRevoked {
		events = append(events, control.Event{
			Kind: control.EventCredentialRevoked, At: at, CredentialID: "crd_startup",
		})
	}
	if _, err := store.Append(context.Background(), events...); err != nil {
		t.Fatalf("seeding the journal: %v", err)
	}
	cache := control.NewCache(store, control.CacheOptions{})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	return cache, path
}

// TestAWhitespaceControlJournalLineIsRefusedRatherThanReadAsUnset drives the SEAM, and it
// is the only test in this file that does.
//
// 🔴 EVERY OTHER CASE HERE CALLS `openAuthority` WITH A LITERAL PATH, WHICH IS EXACTLY WHY
// NONE OF THEM COULD SEE THE DEFECT THIS ONE PINS. The two surfaces — `internal/envalias`'s
// "blank means unset" rule and this program's "a journal switches the authority" rule —
// were each tested hermetically and were each right. Together, a whitespace-only
// `CAIRN_UI_CONTROL_JOURNAL` resolved to `""`, the flag default stayed `""`, and
// `openAuthority` took the TOKEN-FILE branch: an authority `internal/control/tokenfile`
// confers `admin` on nobody through, so every scope page answers 404 and no share can be
// recorded — while `/healthz` answers 200 and the index renders. So this case starts at the
// ENVIRONMENT and ends at the authority `openAuthority` actually returns.
//
// Measured on two built binaries over one world before the fix: `1659663` exited **78**
// naming the journal; `68cf955` served, announcing `sharing read-only (no -control-journal:
// no share can be recorded)`.
//
// ⚠ THE ARMS THAT ARE **NOT** REFUSED ARE HALF THE POINT. A guard that refused every
// non-empty value would satisfy every whitespace arm below and break every deployment, so
// the empty, absent and real-path arms are this case's positive controls.
func TestAWhitespaceControlJournalLineIsRefusedRatherThanReadAsUnset(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	storeRoot := t.TempDir()
	tokenFile := filepath.Join(t.TempDir(), "token")
	// A bare row of at least `authz.MinTokenChars` runes, which is what makes the
	// token-file branch reach a non-empty table rather than `openAuthority`'s own refusal.
	if err := os.WriteFile(tokenFile, []byte(strings.Repeat("s", 43)), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, arm := range []struct {
		name string
		// set=false is "the variable is not in the environment at all", which is a
		// different input from the empty string and must resolve the same way.
		set      bool
		value    string
		wantErr  bool
		wantPath string
		// wantWritable is which BRANCH of `openAuthority` the resolved default reaches,
		// and it is the observation that makes this a seam test rather than a string test.
		wantWritable bool
	}{
		{name: "absent", set: false, wantPath: "", wantWritable: false},
		{name: "the empty string", set: true, value: "", wantPath: "", wantWritable: false},
		{name: "one space", set: true, value: " ", wantErr: true},
		{name: "three spaces", set: true, value: "   ", wantErr: true},
		{name: "tabs and newlines", set: true, value: "\t\n ", wantErr: true},
		// ⚠ THIS ROW IS AN INVARIANT GUARD, NOT REGRESSION COVERAGE, AND LABELLING IT IS
		// THE POINT. `strings.TrimSpace` does not strip U+200B, so `envalias` already
		// called this value PRESENT and `68cf955` already exited 78 on it — measured in
		// the red run, where this row went red while the BEHAVIOUR it describes was never
		// broken. What it pins is that the refusal stays HERE, naming the variable,
		// instead of migrating back to `openAuthority`'s `stat …: no such file or
		// directory`, which sends an operator to check a mount for a path made of
		// invisible runes. It is also the reason the predicate is
		// `identity.ValueReducesToNothing` rather than a fresh `TrimSpace`, which would
		// admit it.
		{name: "zero-width runes", set: true, value: strings.Repeat("\u200b", 8), wantErr: true},
		{name: "a real journal path", set: true, value: journal, wantPath: journal, wantWritable: true},
	} {
		t.Run(arm.name, func(t *testing.T) {
			t.Setenv(EnvUIControlJournal, arm.value)
			if !arm.set {
				if err := os.Unsetenv(EnvUIControlJournal); err != nil {
					t.Fatal(err)
				}
			}

			// This is `main`'s own resolution, reached the way `main` reaches it.
			resolved, err := controlJournalDefault(os.Getenv)
			if arm.wantErr {
				if err == nil {
					// Say what the green would have MEANT, because the failure is silent.
					cache, openErr := openAuthority(resolved, storeRoot, tokenFile)
					branch := "an error from openAuthority"
					if openErr == nil {
						branch = fmt.Sprintf("openAuthority writable=%v", cache.Writable())
					}
					t.Fatalf("%q resolved to %q with no refusal, so the flag default stays empty and "+
						"this surface comes up on whatever %s gives it. A whitespace journal line is "+
						"a line the operator wrote and this program would discard",
						arm.value, resolved, branch)
				}
				for _, want := range []string{EnvUIControlJournal, "reduces to nothing", "404"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("the refusal does not mention %q, so an operator cannot tell which "+
							"line to fix or what it cost them:\n%s", want, err)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("POSITIVE CONTROL FAILED: %q was refused (%v). A guard that refuses this "+
					"admits nothing, and every refusal above would be about nothing", arm.value, err)
			}
			if resolved != arm.wantPath {
				t.Fatalf("resolved to %q, want %q", resolved, arm.wantPath)
			}
			cache, err := openAuthority(resolved, storeRoot, tokenFile)
			if err != nil {
				t.Fatalf("openAuthority(%q): %v", resolved, err)
			}
			if got := cache.Writable(); got != arm.wantWritable {
				t.Errorf("openAuthority reached a writable=%v authority, want writable=%v — which is "+
					"the branch this whole case is about: the token-file projection confers `admin` "+
					"on nobody", got, arm.wantWritable)
			}
		})
	}
}

// TestTheTwoArrivalPathsOfAControlJournalAgree pins the RELATIONSHIP the fix exists for,
// not either side of it.
//
// 🔴 THE DEFECT WAS NEVER "A BAD VALUE IS ACCEPTED" — IT WAS "ONE VALUE BEHAVES OPPOSITELY
// DEPENDING ON HOW IT ARRIVED". `-control-journal '   '` never went through `envalias` and
// was refused at 78 by `openAuthority`'s `Stat` the whole time; only the environment path
// silently dropped it. `README.md` tells the reader "every default is env-resolved", which
// is the sentence that makes the two paths look interchangeable. So this asserts they
// refuse the same set OVER THE VALUES BELOW — and it goes red if a later change makes the
// ENV path lenient again OR makes the FLAG path lenient, which no single-sided test would.
//
// 🔴 "THE SAME SET" IS THE LOOP'S SET AND NOT EVERY STRING, AND THE DIFFERENCE IS A
// MEASURED RESIDUAL RATHER THAN A CAVEAT. The two sides refuse for DIFFERENT REASONS: the
// environment side by the blank policy, the flag side by `openAuthority`'s `stat`, which
// asks the filesystem rather than the spelling. So a value that reduces to nothing AND
// NAMES AN EXISTING FILE parts them — measured on the built binary with a live-credential
// journal whose filename is three spaces: `-control-journal '   '` came up
// `sharing writable (control journal    )` while `CAIRN_UI_CONTROL_JOURNAL='   '` exited
// 78. No value in the loop below names a file, which is why it is green and honest at once.
// The residual is left open on purpose — the flag is the LENIENT side, so the direction is
// safe, and `README.md` carries the ruling.
//
// ⚠ IT IS AN AGREEMENT TEST AND AGREEMENT IS SATISFIED BY TWO LENIENT SIDES, SO IT IS HALF
// A GUARD ON ITS OWN. What pins the ABSOLUTE answer is the case above, which requires the
// environment path to refuse; this one exists to catch the asymmetry that case cannot see.
// Read them as a pair.
func TestTheTwoArrivalPathsOfAControlJournalAgree(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	storeRoot := t.TempDir()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(strings.Repeat("s", 43)), 0o600); err != nil {
		t.Fatal(err)
	}
	// \ud83d\udd34 THE LAST VALUE IS A REAL PATH WEARING SPACES, AND IT IS THE ONLY THING PINNING
	// "RETURNED RAW, NOT TRIMMED". A `strings.TrimSpace` in `controlJournalDefault` \u2014
	// which is what the POD's reader does, so it is the obvious "consistency" edit \u2014
	// survives every other case in this file: the env path would open the journal while
	// `-control-journal "  <path>  "` still fails its `stat`, which is the SAME
	// arrival-path divergence this change exists to close, pointing the other way.
	for _, value := range []string{" ", "   ", "\t\n ", strings.Repeat("\u200b", 8), "  " + journal + "  "} {
		t.Setenv(EnvUIControlJournal, value)
		// 🔴 THE WHOLE CHAIN ON EACH SIDE, NOT ONE FUNCTION AGAINST ANOTHER. The
		// environment path is `controlJournalDefault` AND THEN `openAuthority`; comparing
		// only the first against the second is a category error that reports a divergence
		// for every value the resolver passes through for `openAuthority` to judge.
		envErr := func() error {
			resolved, err := controlJournalDefault(os.Getenv)
			if err != nil {
				return err
			}
			_, err = openAuthority(resolved, storeRoot, tokenFile)
			return err
		}()
		// The flag path: the value reaches `openAuthority` verbatim, because a flag is
		// parsed by `flag` and never by `envalias`.
		_, flagErr := openAuthority(value, storeRoot, tokenFile)
		if (envErr == nil) != (flagErr == nil) {
			t.Errorf("%q is refused by one arrival path and not the other — env=%v flag=%v. The same "+
				"value must not mean two things depending on whether a manifest or a command line "+
				"supplied it", value, envErr, flagErr)
		}
	}
}

// TestTheControlJournalVariableIsNotInTheAliasLedger is what makes `controlJournalDefault`'s
// raw `os.Getenv` safe, and it is an INVARIANT GUARD rather than regression coverage: no bug
// ever violated it.
//
// 🔴 READING RAW IS DELIBERATE — `envalias` treats a blank value as absent, which is the
// defect — but it also means a deprecated spelling of this name would go unread. This name
// has none. The day somebody adds one, this goes red at the ledger rather than leaving
// `controlJournalDefault` quietly half-blind.
func TestTheControlJournalVariableIsNotInTheAliasLedger(t *testing.T) {
	if len(envalias.Ledger) == 0 {
		t.Fatal("the ledger is empty, so the loop below asserts nothing at all")
	}
	for _, pair := range envalias.Ledger {
		if pair.New == EnvUIControlJournal || pair.Old == EnvUIControlJournal {
			t.Fatalf("%s is in the alias ledger as %+v, so `controlJournalDefault`'s raw os.Getenv "+
				"cannot see its other spelling. Resolve it through envalias — but note that "+
				"`envalias.blank` reads whitespace as absent, which is the very thing that "+
				"function exists to refuse", EnvUIControlJournal, pair)
		}
	}
}

// reexecEnv is the switch `TestMain` reads to become `cairn-ui` instead of a test binary.
const reexecEnv = "CAIRN_UI_TEST_REEXEC_AS_MAIN"

// TestMain exists for ONE case, and it is here rather than in that case because Go allows
// exactly one per package.
//
// 🔴 THE FILE ABOVE SAYS IT DRIVES THE PREDICATE AND NOT THE PROCESS, AND THAT REMAINS
// RIGHT FOR EVERY OTHER CASE. It is wrong for one: `main` deciding whether to ACT on
// `controlJournalDefault`'s refusal is wiring, not a predicate, and a mutation sweep
// measured it — replacing `if journalErr != nil { … os.Exit(exitConfig) }` with
// `_ = journalErr` left this package GREEN while the built binary served exactly the
// surface the refusal exists to prevent. No in-process test can see that, because the
// observable is a process exit. So one case re-execs this binary as `cairn-ui`.
func TestMain(m *testing.M) {
	if os.Getenv(reexecEnv) == "1" {
		// ⚠ `flag.CommandLine` IS **NOT** CLEAN HERE, AND THE COMMENT THAT SAID IT WAS —
		// "`testing.Init` has not run, so it holds only `main`'s flags" — WAS MEASURED
		// FALSE. `testing.MainStart` calls `Init()` before it invokes this function, so the
		// `-test.*` flags are already registered when `main()` adds its own: the child's
		// `-h` printed **41** flags, `main`'s 7 plus 34 `-test.*` (go1.26.7; that 34 is a
		// property of the toolchain, not of this file, so do not pin it).
		//
		// It is harmless TODAY for one reason only: no name `main` registers is also a
		// registered `-test.*` name, so nothing collides and `flag.Parse` sees the arguments
		// the case below passes. What it costs is that the child's usage output is not the
		// real binary's, and that a `main` flag whose name EXACTLY matched one of them would
		// panic with `flag redefined` rather than fail a comparison. Said here because the
		// old sentence told a future editor the flag set was clean, which would make either
		// surprise read as a defect somewhere else.
		main()
		return
	}
	os.Exit(m.Run())
}

// TestTheProcessExitsOnAWhitespaceControlJournalLine is the END of the chain the case above
// starts: environment -> flag default -> refusal -> `os.Exit(exitConfig)`.
//
// 🔴 IT IS THE ONLY THING THAT KILLS THE `_ = journalErr` MUTANT, AND ITS NEGATIVE CONTROL
// IS THE OTHER HALF. A child that exits 78 for ANY reason would satisfy a bare exit-code
// assertion — `main` carries ten `os.Exit(exitConfig)` sites — so the arms below pin WHICH
// refusal spoke, and the control arm reaches a DIFFERENT one with the same code.
func TestTheProcessExitsOnAWhitespaceControlJournalLine(t *testing.T) {
	storeRoot := t.TempDir()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(strings.Repeat("s", 43)), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, arm := range []struct {
		name    string
		journal string
		want    string
	}{
		{"whitespace", "   ", "reduces to nothing"},
		// 🔴 THE NEGATIVE CONTROL: a value that is NOT blank still exits 78, from
		// `openAuthority`'s `Stat` and with ITS message. Without this arm the assertion
		// above is satisfied by a program that refuses every journal, and without the
		// message comparison it is satisfied by either refusal firing for either input.
		{"a path that does not exist", filepath.Join(storeRoot, "no", "such.journal"), "cannot be read"},
	} {
		t.Run(arm.name, func(t *testing.T) {
			// 🔴 A DEADLINE, BECAUSE THE FAILURE MODE OF THIS CASE IS A PROCESS THAT DOES
			// NOT EXIT. The mutant it exists to kill makes the child SERVE, and a bare
			// `cmd.Run()` then blocks until the whole suite is killed — a hang reads as
			// infrastructure, not as a finding. Measured: the first draft of this case hung
			// exactly that way on the `_ = journalErr` mutant.
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-store", storeRoot, "-token-file", tokenFile,
				"-session-file", filepath.Join(t.TempDir(), "sessions"), "-port", "0")
			cmd.Env = append(os.Environ(), reexecEnv+"=1", EnvUIControlJournal+"="+arm.journal)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			err := cmd.Run()

			if ctx.Err() != nil {
				t.Fatalf("the process was STILL RUNNING after 30s, so it did not refuse: it bound a "+
					"listener and is serving the surface this guard exists against.\n%s", stderr.String())
			}
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("the process did not exit with a status (%v). It was expected to REFUSE; if it "+
					"exited 0 it came up.\n%s", err, stderr.String())
			}
			if exit.ExitCode() != exitConfig {
				t.Errorf("exit %d, want %d (EX_CONFIG)\n%s", exit.ExitCode(), exitConfig, stderr.String())
			}
			if !strings.Contains(stderr.String(), arm.want) {
				t.Errorf("stderr does not contain %q, so this arm cannot tell which refusal fired:\n%s",
					arm.want, stderr.String())
			}
		})
	}
}
