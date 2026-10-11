//go:build pgtest

// This file is the POSTGRES TIER's second package, and it exists because everything the
// DSN branch does is invisible without a server.
//
// # 🔴 WHAT NO UNTAGGED TEST IN THIS PACKAGE CAN SEE
//
// `cmd/cairn-ui/database_test.go` measures the refusals — the blank policy, the exit
// before any listener, and the no-database branch's own announcement. Every one of those
// is about a configuration that FAILS or is ABSENT. The branch that succeeds is the
// product: a DSN that works, a schema applied, the session table moved off the disk and an
// `Inviting` that is not nil. Nothing without a database can observe any of it, so on a
// tree with only that file the whole DSN-present path is uncovered — and the most likely
// way to break it is silent. `Inviting: inviting` dropped from the `ui.Config` literal
// compiles, serves, announces `invitations in postgres` and then answers `NoInviteStore`
// at every click.
//
// # ⚠ THE REFUSAL IS A SECOND SPELLING, AND IT IS A SMALL ONE ON PURPOSE
//
// `internal/pgstore`'s harness owns the canonical refusal text and the per-test schema
// machinery. This package cannot import it — a `_test` package's helpers are not
// exported — so [requirePgtestDSN] restates the one thing that matters: `t.Fatal`, never
// `t.Skip`, in the words `tests/pgtest/run.sh`'s negative control greps for. It
// deliberately does NOT restate the schema machinery: this case wants the same public
// schema the real program would use, because what it is measuring is the program applying
// its own migrations to a database it was pointed at.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/invite"
	"github.com/ZacxDev/cairn/internal/pgstore"
)

// pgtestDSNEnv is the tier's one input, spelled as `internal/pgstore`'s harness spells it.
//
// ⚠ NOT A `CAIRN_*` NAME, for the reason that harness gives: every `CAIRN_*` name is part
// of the product's configuration surface and carries an alias ledger, a deprecation
// warning and a parity test. This is a harness input no shipped binary reads.
const pgtestDSNEnv = "CAIRN_PGTEST_DSN"

// requirePgtestDSN returns the connection string or REFUSES TO VOUCH.
//
// 🔴 `t.Fatal`, NEVER `t.Skip` — the tier's whole design. A skip is indistinguishable from
// a pass in any summary anybody reads, and `tests/pgtest/run.sh` refuses on one.
func requirePgtestDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(pgtestDSNEnv))
	if dsn == "" {
		t.Fatalf(`REFUSING TO VOUCH: the Postgres tier ran with no database.

This tier does not skip. Set %s to a libpq connection string for a THROWAWAY database, or
run the tier through its own runner, which starts one and tears it down:

    tests/pgtest/run.sh`, pgtestDSNEnv)
	}
	return dsn
}

// TestWithADatabaseTheSurfaceMovesItsStateThereAndHoldsInvitations is the DSN branch, end
// to end, against a real server — and it is the only case anywhere that can say the
// wiring 28(c) added does what it announces.
//
// 🔴 IT ASSERTS FOUR INDEPENDENT THINGS, AND EACH ONE FAILS SEPARATELY. A single
// "it came up" assertion would be satisfied by a binary that ignored the DSN entirely.
//
//  1. the schema is APPLIED — the two tables exist in the database the child was pointed
//     at, which is the only evidence `pgstore.Open` actually ran rather than a flag being
//     parsed;
//  2. the session table MOVED — the `-session-file` path was never created, and that is
//     discriminating because `identity.OpenFileSessionStore` creates it EAGERLY with
//     `O_CREATE` and `MkdirAll`s its directory. Its positive control is
//     `TestWithNoDatabaseTheSurfaceComesUpSayingItHoldsNoInvitation`, which asserts the
//     same path DOES exist on the other branch;
//  3. the announcement says both halves, and it is derived from the wired objects rather
//     than from the flag — see the state line in `main`;
//  4. the NOTE about an ignored `-session-file` is printed, because a manifest carrying
//     both is the shape that arrives and silence would leave somebody mounting a volume
//     for a file nothing opens.
//
// ⚠ WHAT IT STILL CANNOT SEE: an invitation being minted, redeemed or revoked over HTTP.
// That needs a signed-in principal and a provider exchange, and it is rank 9's and rank
// 13's — a human's, on the deployed surface. This case measures that the store is THERE
// and reachable, which is the precondition, not the feature.
func TestWithADatabaseTheSurfaceMovesItsStateThereAndHoldsInvitations(t *testing.T) {
	dsn := requirePgtestDSN(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, journal := seededJournal(t, credentialLive)

	// A session-file path under a directory that does NOT exist yet. Both halves matter:
	// the file store would create the directory as well as the file, so a missing
	// directory afterwards is the stronger reading of "this path was never opened".
	sessionDir := filepath.Join(t.TempDir(), "never-created")
	sessionFile := filepath.Join(sessionDir, "sessions")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	child := exec.CommandContext(ctx, self,
		"-control-journal", journal,
		"-store", t.TempDir(),
		"-token-file", filepath.Join(t.TempDir(), "absent-token"),
		// 🔴 THE SESSION FILE IS PASSED EXPLICITLY, which is what arms assertion (4): the
		// NOTE only fires when the operator wrote one of the two spellings, and a test that
		// let it default would measure the silent case while claiming the loud one.
		"-session-file", sessionFile,
		"-db-dsn", dsn,
		"-host", "127.0.0.1", "-port", "0")
	child.Env = []string{reexecEnv + "=1"}
	var body syncBuffer
	// Through `startChild`, so the wait fails the moment the child EXITS rather than polling out
	// its 60 s deadline — see `presenceChild.done`.
	c := startChild(t, child, cancel, &body, &body)

	c.waitWithin(t, 60*time.Second, "the surface to come up against a WORKING database. Every refusal "+
		"this tier's sibling file asserts is about a configuration that fails; if the one that succeeds "+
		"cannot start, those refusals are a binary that refuses everything",
		func() bool { return strings.Contains(body.String(), "serving") })
	line := body.String()

	// (1) The schema is applied, read out of the database rather than inferred from the log.
	assertTablesExist(t, dsn, "sessions", "invites")

	// (2) The session table moved: neither the file nor its directory was created.
	if _, statErr := os.Stat(sessionDir); statErr == nil {
		t.Errorf("%s EXISTS, so the file session store was opened despite a database being configured. "+
			"`identity.OpenFileSessionStore` creates this directory and the file inside it eagerly, so "+
			"its presence means the DSN branch built an invite store and left `sessions` on disk — "+
			"which the startup line would then be announcing wrongly", sessionDir)
	}

	// (3) Both halves of the announcement.
	for _, want := range []string{"sessions in postgres", "invitations in postgres", "team links in postgres"} {
		if !strings.Contains(line, want) {
			t.Errorf("the startup line does not say %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "NO invitation store") {
		t.Errorf("the surface came up against a working database still announcing that it holds no "+
			"invitation store, so `inviting` was left nil and every /invite page will say so:\n%s", line)
	}
	if strings.Contains(line, "sessions in "+sessionFile) {
		t.Errorf("the startup line names the FILE as the session table while a database is "+
			"configured:\n%s", line)
	}

	// (4) The NOTE that the configured session file is inert.
	if !strings.Contains(line, "is IGNORED") {
		t.Errorf("the surface did not say that the -session-file it was given is ignored. A manifest "+
			"carrying both is the shape that arrives, and silence leaves somebody mounting a volume "+
			"for a file nothing opens:\n%s", line)
	}
	if !strings.Contains(line, "signs every existing browser session out once") {
		t.Errorf("the NOTE does not say that the move signs existing sessions out. That is the one "+
			"user-visible consequence of this configuration and the log is where an operator "+
			"looks for it afterwards:\n%s", line)
	}
}

// assertTablesExist reads the schema out of the server.
//
// 🔴 IT OPENS THROUGH `pgstore.Open`, WHICH APPLIES THE MIGRATIONS ITSELF — so this helper
// cannot be used to prove the CHILD applied them, and it is not. What it proves is that the
// tables are present in the database the child was pointed at after the child came up; the
// child is what got there first, and a `pgstore.Open` against an already-migrated database
// is a no-op past its ledger check. ⚠ Stated because the alternative reading — "this
// created them" — would make the assertion vacuous, and it is exactly the reading a later
// editor would take.
func assertTablesExist(t *testing.T, dsn string, tables ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgstore.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("this test could not reach the database it just pointed a child at: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, table := range tables {
		var present bool
		if err := db.SQL().QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			  WHERE table_schema = current_schema() AND table_name = $1)`, table,
		).Scan(&present); err != nil {
			t.Fatalf("the table check for %q failed: %v", table, err)
		}
		if !present {
			t.Errorf("the %q table does not exist in the database the surface was pointed at, so "+
				"`pgstore.Open` — which pings AND migrates — did not run. The DSN was parsed and "+
				"nothing was done with it", table)
		}
	}
	fmt.Fprintf(os.Stderr, "cairn-ui pgtest: %d table(s) confirmed present\n", len(tables))
}

// TestTheWiredHalvesMintAndRedeemALinkAgainstPostgres is round 1 🟡4's Postgres half: the
// function `main` builds both halves with (`wireInvitations`), over the REAL stores, mints a
// team link through the Team page's half and redeems it through the invitation half — the
// callback's path — and the redemption row is CONFIRMED in the database (round 1 🟡1).
// `TestTheWiredInvitationHalfRedeemsATeamLink` is the same claim in the ordinary tier.
func TestTheWiredHalvesMintAndRedeemALinkAgainstPostgres(t *testing.T) {
	dsn := requirePgtestDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := pgstore.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("could not vouch: opening the tier's database failed: %v", err)
	}
	defer db.Close()
	authority, _ := seededJournal(t, credentialLive)
	links := pgstore.NewTeamLinkStore(db)
	inviting := wireInvitations(authority, pgstore.NewInviteStore(db), links)
	team := inviting.TeamLinks()
	if team == nil {
		t.Fatal("the wired invitation half carries no team-link half")
	}
	owner, ok := authority.Model().PrincipalFor(control.KindUser, control.DerivedID(control.PrefixUser, "startup-user"))
	if !ok {
		t.Fatal("precondition: the seeded owner is not in the model")
	}
	project := control.DerivedID(control.PrefixProject, "startup-project")
	token, link, err := team.Mint(ctx, owner, []invite.Target{{Kind: invite.TargetProject, ID: project}},
		invite.LinkReader, 0, true)
	if err != nil {
		t.Fatalf("minting through the wired half: %v", err)
	}
	red, err := inviting.Redeem(ctx, token, "fixture-provider", fmt.Sprintf("pg-wired-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("a link minted on the Team page was not redeemable through the invitation half: %v", err)
	}
	log, err := links.LinkRedemptions(link.Digest)
	if err != nil || len(log) != 1 || !log[0].Confirmed || log[0].By != red.Principal.ID {
		t.Fatalf("the redemption log is %+v (%v); want one CONFIRMED row naming %s", log, err, red.Principal.ID)
	}
}
