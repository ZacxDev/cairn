//go:build pgtest

package pgstore_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/invite"
	"github.com/ZacxDev/cairn/internal/pgstore"
)

var (
	fixtureScopeA = control.ID("scp_fixture_a")
	fixtureScopeB = control.ID("scp_fixture_b")
)

// mintLink stores one link and returns its token.
func mintLink(t *testing.T, s *pgstore.TeamLinkStore, reusable bool, expires time.Time) (string, invite.TeamLink) {
	t.Helper()
	token, digest, err := invite.NewToken()
	if err != nil {
		t.Fatalf("minting a token: %v", err)
	}
	l := invite.TeamLink{
		Digest: digest,
		Targets: []invite.Target{
			{Kind: invite.TargetProject, ID: fixtureProject},
			{Kind: invite.TargetScope, ID: fixtureScopeA},
			{Kind: invite.TargetScope, ID: fixtureScopeB},
		},
		Role: invite.LinkReader, Inviter: fixtureInviter, Reusable: reusable,
		CreatedAt: fixtureNow, ExpiresAt: expires,
	}
	if err := s.CreateLink(l); err != nil {
		t.Fatalf("creating a team link: %v", err)
	}
	return token, l
}

// TestTheTeamLinkRedemptionGuardAgreesWithStateAt pins `RedeemLink`'s `WHERE` clause — a
// deliberate second spelling of `invite.TeamLink.StateAt` — against it, at the CLOSED expiry
// boundary and on each of the other clauses, `TestTheSQLRedemptionGuardAgreesWithStateAt`'s
// ruling for the invitation table. The expiry carries a non-microsecond remainder, so the
// stored value is what `StateAt` must be asked about (it is read back, not assumed).
func TestTheTeamLinkRedemptionGuardAgreesWithStateAt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		offset time.Duration // from the STORED expiry
	}{
		{"one microsecond before", -time.Microsecond},
		{"exactly at", 0},
		{"one microsecond after", time.Microsecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := pgstore.NewTeamLinkStore(openTestDB(t))
			token, minted := mintLink(t, s, true, fixtureNow.Add(time.Hour+subSecond))
			stored, ok, err := s.LinkByDigest(minted.Digest)
			if err != nil || !ok {
				t.Fatalf("reading the link back: ok=%v err=%v", ok, err)
			}
			at := stored.ExpiresAt.Add(tc.offset)
			wantOpen := stored.Redeemable(at)
			_, err = s.RedeemLink(token, fixtureJoiner, false, at)
			gotOpen := err == nil
			if err != nil && !errors.Is(err, invite.ErrNotRedeemable) {
				t.Fatalf("the redemption failed for a reason other than the guard: %v", err)
			}
			if gotOpen != wantOpen {
				t.Fatalf("at expiry%+v the SQL guard said open=%v and StateAt said open=%v — the two "+
					"spellings of one rule disagree", tc.offset, gotOpen, wantOpen)
			}
		})
	}

	// The other clauses: revoked, and a SPENT single-use link — each refused by both.
	s := pgstore.NewTeamLinkStore(openTestDB(t))
	at := fixtureNow.Add(time.Minute)
	revokedTok, revoked := mintLink(t, s, true, fixtureNow.Add(time.Hour))
	if err := s.RevokeLink(revoked.Digest, at); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	if got, _, _ := s.LinkByToken(revokedTok); got.Redeemable(at) {
		t.Fatal("StateAt calls a revoked link open")
	}
	if _, err := s.RedeemLink(revokedTok, fixtureJoiner, false, at); !errors.Is(err, invite.ErrNotRedeemable) {
		t.Fatalf("the SQL guard redeemed a REVOKED link (err=%v)", err)
	}
	singleTok, _ := mintLink(t, s, false, fixtureNow.Add(time.Hour))
	if _, err := s.RedeemLink(singleTok, fixtureJoiner, false, at); err != nil {
		t.Fatalf("the first redemption of a single-use link: %v", err)
	}
	if got, _, _ := s.LinkByToken(singleTok); got.Redeemable(at) {
		t.Fatal("StateAt calls a spent single-use link open")
	}
	if _, err := s.RedeemLink(singleTok, control.ID("usr_fixture_second"), false, at); !errors.Is(err, invite.ErrNotRedeemable) {
		t.Fatalf("the SQL guard redeemed a SPENT single-use link (err=%v)", err)
	}
	// And a spent single-use link cannot be "revoked" — the revoke's open is the redeem's open.
	if got, _, _ := s.LinkByToken(singleTok); s.RevokeLink(got.Digest, at) == nil {
		t.Fatal("a spent single-use link was reported revoked — a door the minter thinks they closed is one somebody already walked through")
	}
}

// TestSimultaneousRedemptionsRespectTheReuseFlag: eight concurrent redemptions of a
// SINGLE-USE link produce exactly one winner and one log row; of a REUSABLE link, eight
// winners with eight distinct sequence numbers.
func TestSimultaneousRedemptionsRespectTheReuseFlag(t *testing.T) {
	for _, reusable := range []bool{false, true} {
		s := pgstore.NewTeamLinkStore(openTestDB(t))
		token, link := mintLink(t, s, reusable, fixtureNow.Add(time.Hour))
		const n = 8
		var wg sync.WaitGroup
		wins := make(chan int, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				got, err := s.RedeemLink(token, control.ID("usr_fixture_racer_"+string(rune('a'+i))), true, fixtureNow.Add(time.Minute))
				if err == nil {
					wins <- got.Redemptions
				} else if !errors.Is(err, invite.ErrNotRedeemable) {
					t.Errorf("a racer failed for a reason other than the guard: %v", err)
				}
			}(i)
		}
		wg.Wait()
		close(wins)
		seen := map[int]bool{}
		for seq := range wins {
			seen[seq] = true
		}
		want := 1
		if reusable {
			want = n
		}
		if len(seen) != want {
			t.Fatalf("reusable=%v: %d distinct winner(s), want %d", reusable, len(seen), want)
		}
		log, err := s.LinkRedemptions(link.Digest)
		if err != nil || len(log) != want {
			t.Fatalf("reusable=%v: the log holds %d row(s) (%v), want %d", reusable, len(log), err, want)
		}
		for i, row := range log {
			if row.Seq != i+1 || !row.Provisioned {
				t.Errorf("reusable=%v: log row %d is %+v", reusable, i, row)
			}
		}
	}
}

// TestTheSingleUseConstraintIsTheDatabasesOwn: the `team_links_single_use` CHECK refuses a
// second redemption of a single-use row even from a statement that skipped the guard — the
// database stating the rule a second time, independently of `RedeemLink`.
func TestTheSingleUseConstraintIsTheDatabasesOwn(t *testing.T) {
	db := openTestDB(t)
	s := pgstore.NewTeamLinkStore(db)
	_, single := mintLink(t, s, false, fixtureNow.Add(time.Hour))
	_, reusable := mintLink(t, s, true, fixtureNow.Add(time.Hour))
	if _, err := db.SQL().Exec(`UPDATE team_links SET redemptions = 2 WHERE digest = $1`, single.Digest); err == nil {
		t.Fatal("the database accepted a SECOND redemption of a single-use link from an unguarded UPDATE")
	}
	// POSITIVE CONTROL: the same statement against a reusable row is accepted.
	if _, err := db.SQL().Exec(`UPDATE team_links SET redemptions = 2 WHERE digest = $1`, reusable.Digest); err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: the constraint refused a reusable row too: %v", err)
	}
}

// TestATeamLinkRoundTripsWithItsTargetsAndNeverHoldsTheToken.
func TestATeamLinkRoundTripsWithItsTargetsAndNeverHoldsTheToken(t *testing.T) {
	db := openTestDB(t)
	s := pgstore.NewTeamLinkStore(db)
	token, minted := mintLink(t, s, true, fixtureNow.Add(time.Hour))
	got, ok, err := s.LinkByToken(token)
	if err != nil || !ok {
		t.Fatalf("reading by token: ok=%v err=%v", ok, err)
	}
	if len(got.Targets) != 3 || got.Role != invite.LinkReader || !got.Reusable || got.Inviter != fixtureInviter {
		t.Fatalf("the link round-tripped as %+v", got)
	}
	listed, err := s.LinksBy(fixtureInviter)
	if err != nil || len(listed) != 1 || listed[0].Digest != minted.Digest || len(listed[0].Targets) != 3 {
		t.Fatalf("LinksBy answered %+v (%v)", listed, err)
	}
	if other, _ := s.LinksBy(fixtureJoiner); len(other) != 0 {
		t.Fatalf("another principal was listed the link: %+v", other)
	}
	// No column of any of the three tables holds the token.
	for _, q := range []string{
		`SELECT digest || role || inviter FROM team_links`,
		`SELECT digest || kind || target_id FROM team_link_targets`,
	} {
		rows, err := db.SQL().Query(q)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				t.Fatal(err)
			}
			n++
			if strings.Contains(v, token) {
				t.Fatalf("a team-link table holds the presented token: %q", q)
			}
		}
		rows.Close()
		if n == 0 {
			t.Fatalf("POSITIVE CONTROL: %q returned no rows, so the absence above proves nothing", q)
		}
	}
	// CreateLink refuses a link with no target.
	if err := s.CreateLink(invite.TeamLink{Digest: "abc", Role: invite.LinkReader, Inviter: fixtureInviter,
		CreatedAt: fixtureNow, ExpiresAt: fixtureNow.Add(time.Hour)}); !errors.Is(err, invite.ErrBadTarget) {
		t.Fatalf("a link with no target was stored (err=%v)", err)
	}
}

// TestMigrationTwoUpgradesAVersionOneDatabase is the UP-PATH: a database built by the
// previous build (version 1, an outstanding invitation in it) is migrated by this one. The
// invitation must survive untouched, the ledger must record both versions, and the new
// tables must work.
func TestMigrationTwoUpgradesAVersionOneDatabase(t *testing.T) {
	db := openTestDBWith(t, pgstore.OpenThroughForTest(1))
	var exists bool
	if err := db.SQL().QueryRow(`SELECT to_regclass('team_links') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("INSTRUMENT: the version-1 database already has `team_links`, so this is not an up-path")
	}
	invites := pgstore.NewInviteStore(db)
	invToken := mintInvite(t, invites, fixtureNow.Add(time.Hour))

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrating a version-1 database: %v", err)
	}
	rows, err := db.SQL().Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, v)
	}
	rows.Close()
	if len(versions) != 2 || versions[0] != 1 || versions[1] != 2 {
		t.Fatalf("the ledger records %v, want [1 2]", versions)
	}
	inv, ok, err := invites.ByToken(invToken)
	if err != nil || !ok || !inv.Redeemable(fixtureNow) {
		t.Fatalf("the version-1 invitation did not survive the upgrade: ok=%v err=%v %+v", ok, err, inv)
	}
	s := pgstore.NewTeamLinkStore(db)
	token, _ := mintLink(t, s, false, fixtureNow.Add(time.Hour))
	if _, err := s.RedeemLink(token, fixtureJoiner, false, fixtureNow); err != nil {
		t.Fatalf("the upgraded database cannot redeem a team link: %v", err)
	}
	// Re-running is a no-op, not an error.
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("a second migrate of an up-to-date database: %v", err)
	}
}

// TestTheRollbackRecipeLetsAnOlderBuildStartAndReUpgrades — round 1 🟡2. Migration 2 makes
// the older build's "database from the future" refusal reachable on a rollback (and on
// `cairn-ui` that refusal takes sign-in down). The recipe `migrate.go` documents —
// `DELETE FROM schema_migrations WHERE version = 2` — is measured here in three steps: the
// refusal EXISTS (positive control), the recipe lifts it, and re-upgrading is clean with the
// team-link rows intact.
func TestTheRollbackRecipeLetsAnOlderBuildStartAndReUpgrades(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := pgstore.NewTeamLinkStore(db)
	_, minted := mintLink(t, s, true, fixtureNow.Add(time.Hour))

	// (1) The hazard: a version-1 build refuses this database.
	if err := pgstore.OlderBuildRefusalForTest(ctx, db, 1); err == nil {
		t.Fatal("POSITIVE CONTROL FAILED: a version-1 build would START against a version-2 database, so the " +
			"rollback hazard this test exists for is not there and the recipe below proves nothing")
	}
	// (2) The recipe — the literal statement the README hands an operator.
	if _, err := db.SQL().ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = 2`); err != nil {
		t.Fatalf("the rollback statement failed: %v", err)
	}
	if err := pgstore.OlderBuildRefusalForTest(ctx, db, 1); err != nil {
		t.Fatalf("after the recipe a version-1 build still refuses: %v", err)
	}
	// …and an old build's own read path works: the invitations table is untouched.
	if _, _, err := pgstore.NewInviteStore(db).ByToken("no-such-token"); err != nil {
		t.Fatalf("the version-1 table is unreadable after the recipe: %v", err)
	}
	// (3) Re-upgrade: the newer build migrates again over the tables that are still there.
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("re-upgrading after the recipe failed: %v", err)
	}
	if err := pgstore.OlderBuildRefusalForTest(ctx, db, 2); err != nil {
		t.Fatalf("after re-upgrading the current build would refuse: %v", err)
	}
	if got, ok, err := s.LinkByDigest(minted.Digest); err != nil || !ok || len(got.Targets) != 3 {
		t.Fatalf("the team link minted before the rollback did not survive it: ok=%v err=%v %+v", ok, err, got)
	}
}

// TestARedemptionRowIsUnconfirmedUntilConfirmed — round 1 🟡1's store half: `RedeemLink`
// writes the row UNCONFIRMED, `ConfirmRedemption` confirms exactly that row, and confirming a
// row that does not exist is an error rather than a silent no-op.
func TestARedemptionRowIsUnconfirmedUntilConfirmed(t *testing.T) {
	s := pgstore.NewTeamLinkStore(openTestDB(t))
	token, link := mintLink(t, s, true, fixtureNow.Add(time.Hour))
	spent, err := s.RedeemLink(token, fixtureJoiner, true, fixtureNow)
	if err != nil {
		t.Fatalf("redeeming: %v", err)
	}
	log, _ := s.LinkRedemptions(link.Digest)
	if len(log) != 1 || log[0].Confirmed {
		t.Fatalf("a fresh redemption row reads %+v, want one UNCONFIRMED row", log)
	}
	if err := s.ConfirmRedemption(link.Digest, spent.Redemptions); err != nil {
		t.Fatalf("confirming: %v", err)
	}
	log, _ = s.LinkRedemptions(link.Digest)
	if len(log) != 1 || !log[0].Confirmed {
		t.Fatalf("after confirming the row reads %+v", log)
	}
	if err := s.ConfirmRedemption(link.Digest, spent.Redemptions+1); err == nil {
		t.Fatal("confirming a redemption that does not exist succeeded")
	}
}
