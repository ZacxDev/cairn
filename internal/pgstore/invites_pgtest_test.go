//go:build pgtest

package pgstore_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/invite"
	"github.com/ZacxDev/cairn/internal/pgstore"
)

// synthetic year-2000 instants. `AGENTS.md`: a fixture that genuinely needs a date uses
// an obviously-synthetic year-2000 one, which is what `leakscan.py` allows.
var (
	fixtureNow     = time.Date(2000, 3, 4, 5, 6, 7, 0, time.UTC)
	fixtureProject = control.ID("prj_fixture_one")
	fixtureInviter = control.ID("usr_fixture_inviter")
	fixtureJoiner  = control.ID("usr_fixture_joiner")
)

// subSecond is the sub-second part every expiry fixture in this file carries.
//
// 🔴 ITS DIGITS ARE CHOSEN SO THAT EVERY CANDIDATE OPERATION GIVES A DIFFERENT ANSWER,
// AND THAT REQUIREMENT IS MEASURED RATHER THAN FUSSY — IT CAUGHT A WRONG CLAIM IN THIS
// FILE'S OWN TEST NAME. The first version of these fixtures carried a 500ns remainder,
// which is EXACTLY half a microsecond, and two things went wrong at once:
//
//   - a mutant rounding the expiry to the nearest SECOND SURVIVED a fully green run,
//     because at 500ns `Round(time.Second)` and the column's own resolution agree;
//
//   - the test asserted the column TRUNCATES, passed, and was WRONG. At an exact tie
//     the server breaks half downward, which is indistinguishable from truncation. With
//     a non-tie remainder the column visibly ROUNDS: .123456789 is stored as .123457.
//
//     written                     .123456789
//     Round(time.Microsecond)     .123457     <- what the column actually holds, MEASURED
//     Truncate(time.Microsecond)  .123456     <- what this file used to assert
//     Round(time.Second)          .000000     <- the mutant that used to survive
//
// ⚠ GENERAL SHAPE, worth carrying to the next fixture: a fixture value that is a
// MULTIPLE of, or EQUIDISTANT from, the step an operation uses cannot distinguish that
// operation from its neighbours — so it hides both a mutant and a false expectation, and
// a green run reports neither. Pick digits that overshoot every boundary you care about.
const subSecond = 123456789 * time.Nanosecond

// mintInvite stores one open invitation and returns its token.
func mintInvite(t *testing.T, s *pgstore.InviteStore, expires time.Time) string {
	t.Helper()
	token, digest, err := invite.NewToken()
	if err != nil {
		t.Fatalf("minting a token: %v", err)
	}
	if err := s.Create(invite.Invite{
		Digest:    digest,
		ProjectID: fixtureProject,
		Role:      control.RoleMember,
		Inviter:   fixtureInviter,
		CreatedAt: fixtureNow,
		ExpiresAt: expires,
	}); err != nil {
		t.Fatalf("creating an invitation: %v", err)
	}
	return token
}

// TestTheSQLRedemptionGuardAgreesWithStateAt is the test `internal/pgstore/invites.go`
// names in its own comment, and it exists because the redemption `WHERE` clause is a
// DELIBERATE SECOND SPELLING of `invite.StateAt`.
//
// 🔴 THE TWO SPELLINGS AND WHY THEY CANNOT BE UNIFIED. `Redeem` has to evaluate the
// condition IN THE DATABASE — the atomicity is the whole point, and a read-then-write in
// Go loses the race that produces a double join. So `expires_at > $at` exists in SQL
// while `StateAt` exists in Go, and this repository's "one rule, one place" rule cannot
// be satisfied by deleting either. What is available instead is a test that makes them
// disagree loudly, which is this one.
//
// 🔴 IT DRIVES THE BOUNDARY INSTANT, NOT A COMFORTABLE MIDDLE. `StateAt`'s boundary is
// CLOSED — `!now.Before(ExpiresAt)` is expired, so the invite is dead AT `ExpiresAt` —
// and SQL's `expires_at > $at` is the same boundary written the other way round. Two
// spellings of a closed boundary are identical everywhere except on the boundary itself,
// so a test that probed only "an hour before" and "an hour after" would pass against a
// `>=` and against a `>`, which is exactly the mutation this guard has to kill.
//
// 🔴 AND IT PROBES A NON-MICROSECOND-ALIGNED EXPIRY, WHICH IS A SECOND DIMENSION THE
// OBVIOUS TEST MISSES. `TIMESTAMPTZ` is MICROSECOND-precision and Go's `time.Time` is
// nanosecond, so an `ExpiresAt` carrying a nanosecond remainder does not survive the
// round trip intact — it is ROUNDED to the column's resolution, which is measured by
// [TestAnExpiryIsRoundedToPostgresResolution] below rather than assumed here. A fixture
// built from a whole second is already aligned and cannot see that at all; both shapes
// are driven.
//
// ⚠ "ROUNDED" IS LOAD-BEARING AND THIS COMMENT FIRST SAID "TRUNCATED". The correction
// came from the mutation sweep, not from reading the docs, and it is noted because the
// wrong word here would send the next reader to the wrong side of the boundary: rounding
// can place the stored expiry LATER than the written one, where truncation could only
// place it earlier.
func TestTheSQLRedemptionGuardAgreesWithStateAt(t *testing.T) {
	db := openTestDB(t)
	store := pgstore.NewInviteStore(db)

	shapes := []struct {
		name    string
		expires time.Time
	}{
		// A whole second: what an ordinary fixture looks like, and already aligned to
		// Postgres resolution.
		{"microsecond-aligned expiry", fixtureNow.Add(time.Hour)},
		// 🔴 A NANOSECOND REMAINDER, so the written value and the stored value differ.
		// `time.Now()` in production carries one of these, which is what makes this the
		// realistic shape rather than the exotic one.
		//
		// ⚠ THE VALUE IS `subSecond`, NOT A TIDY 500ns, AND THE DIFFERENCE WAS MEASURED.
		// A 500ns remainder is the same distance from `Truncate(time.Microsecond)` as it
		// is from `Round(time.Second)`, so a mutant that rounded the expiry to the
		// nearest SECOND produced byte-identical fixtures and SURVIVED a green run. See
		// `subSecond`'s own comment.
		{"expiry with a nanosecond remainder", fixtureNow.Add(time.Hour + subSecond)},
	}
	// One microsecond is the smallest step the column can represent, so these three
	// probes are "the last instant it is open", "the boundary", and "the first instant
	// past it" — adjacent values, with nothing between them the column could hold.
	offsets := []time.Duration{-time.Microsecond, 0, time.Microsecond}

	for _, sh := range shapes {
		for _, off := range offsets {
			name := sh.name + " at " + offsetName(off)
			t.Run(name, func(t *testing.T) {
				// A FRESH invitation per probe: `Redeem` is a state change, so reusing
				// one would make every probe after the first measure the `redeemed_at`
				// clause instead of the expiry clause.
				token := mintInvite(t, store, sh.expires)

				stored, ok, err := store.ByToken(token)
				if err != nil || !ok {
					t.Fatalf("reading back the invitation: ok=%v err=%v", ok, err)
				}
				// 🔴 THE PROBE IS RELATIVE TO THE STORED EXPIRY, NOT THE WRITTEN ONE.
				// The database's copy is what its own `WHERE` compares, so an offset
				// measured from the pre-rounding value would put the two probes on
				// different sides of the boundary for a reason that has nothing to do
				// with the predicates under test.
				probe := stored.ExpiresAt.Add(off)

				// The Go spelling, read from the row the database actually holds —
				// which is the value the serving path has too, because it reaches
				// `StateAt` through `ByToken`.
				wantOpen := stored.StateAt(probe) == invite.StateOpen
				// `Redeemable` is the only question the redemption path may ask, and it
				// delegates to `StateAt`. Asserting both makes a wrapper that stopped
				// delegating a red test here rather than a silent divergence.
				if got := stored.Redeemable(probe); got != wantOpen {
					t.Fatalf("Redeemable(%v)=%v disagrees with StateAt=%q — the wrapper no longer delegates",
						probe, got, stored.StateAt(probe))
				}

				// The SQL spelling.
				redeemed, err := store.Redeem(token, fixtureJoiner, probe)
				gotOpen := err == nil
				switch {
				case gotOpen && !errors.Is(err, nil):
					t.Fatalf("internal: err non-nil on the success arm: %v", err)
				case !gotOpen && !errors.Is(err, invite.ErrNotRedeemable):
					t.Fatalf("Redeem refused with an unexpected error, so this probe measured a "+
						"database or driver failure rather than the guard: %v", err)
				}

				if gotOpen != wantOpen {
					t.Fatalf(`THE TWO SPELLINGS OF THE EXPIRY BOUNDARY DISAGREE.

  written ExpiresAt : %v
  stored  ExpiresAt : %v   (TIMESTAMPTZ is microsecond-precision)
  probe instant     : %v   (stored ExpiresAt %s)

  invite.StateAt    : %q  -> open=%v
  SQL expires_at>$at: open=%v

One of the two was edited without the other. invites.go's Redeem comment names this
test: fix BOTH spellings and watch this fail first.`,
						sh.expires, stored.ExpiresAt, probe, offsetName(off),
						stored.StateAt(probe), wantOpen, gotOpen)
				}

				// On the open arm, the row the UPDATE returned must be the redeemed one —
				// otherwise `Redeem` could report success having written nothing, which
				// the handler would turn into a membership with no invitation behind it.
				if gotOpen {
					if redeemed.RedeemedAt.IsZero() || redeemed.RedeemedBy != fixtureJoiner {
						t.Fatalf("Redeem succeeded but returned an unredeemed row: RedeemedAt=%v RedeemedBy=%q",
							redeemed.RedeemedAt, redeemed.RedeemedBy)
					}
					if st := redeemed.StateAt(probe); st != invite.StateRedeemed {
						t.Fatalf("the returned row is %q, want %q", st, invite.StateRedeemed)
					}
				}
			})
		}
	}
}

func offsetName(d time.Duration) string {
	switch {
	case d < 0:
		return "minus one microsecond (the last open instant)"
	case d == 0:
		return "exactly (the boundary)"
	default:
		return "plus one microsecond (the first expired instant)"
	}
}

// TestTheRedemptionGuardAgreesWithStateAtOnTheOtherTwoClauses covers the conjuncts the
// expiry test cannot reach.
//
// 🔴 THE `WHERE` HAS THREE CONJUNCTS AND THE BOUNDARY TEST EXERCISES ONE. A mutant
// deleting `redeemed_at IS NULL` or `revoked_at IS NULL` leaves every expiry probe
// green, so without this the guard would be a third as wide as its name suggests — the
// "a guard's description claims coverage; check the implementation is as wide as the
// sentence" shape this repository already tracks.
func TestTheRedemptionGuardAgreesWithStateAtOnTheOtherTwoClauses(t *testing.T) {
	db := openTestDB(t)
	store := pgstore.NewInviteStore(db)
	expires := fixtureNow.Add(time.Hour)
	// Well inside the open window, so a failure here cannot be an expiry effect.
	probe := fixtureNow.Add(time.Minute)

	t.Run("an already-redeemed invitation", func(t *testing.T) {
		token := mintInvite(t, store, expires)
		if _, err := store.Redeem(token, fixtureJoiner, probe); err != nil {
			t.Fatalf("the first redemption should have succeeded: %v", err)
		}
		stored, ok, err := store.ByToken(token)
		if err != nil || !ok {
			t.Fatalf("a redeemed invitation must stay readable: ok=%v err=%v", ok, err)
		}
		if st := stored.StateAt(probe); st != invite.StateRedeemed {
			t.Fatalf("StateAt=%q, want %q", st, invite.StateRedeemed)
		}
		if _, err := store.Redeem(token, fixtureJoiner, probe); !errors.Is(err, invite.ErrNotRedeemable) {
			t.Fatalf("a second redemption returned %v, want ErrNotRedeemable — the "+
				"`redeemed_at IS NULL` conjunct is not doing anything", err)
		}
	})

	t.Run("a revoked invitation", func(t *testing.T) {
		token := mintInvite(t, store, expires)
		if err := store.Revoke(token, probe); err != nil {
			t.Fatalf("revoking an open invitation: %v", err)
		}
		stored, ok, err := store.ByToken(token)
		if err != nil || !ok {
			t.Fatalf("a revoked invitation must stay readable: ok=%v err=%v", ok, err)
		}
		if st := stored.StateAt(probe); st != invite.StateRevoked {
			t.Fatalf("StateAt=%q, want %q", st, invite.StateRevoked)
		}
		if _, err := store.Redeem(token, fixtureJoiner, probe); !errors.Is(err, invite.ErrNotRedeemable) {
			t.Fatalf("redeeming a revoked invitation returned %v, want ErrNotRedeemable — the "+
				"`revoked_at IS NULL` conjunct is not doing anything", err)
		}
	})

	t.Run("revoking something that is not open is reported, not swallowed", func(t *testing.T) {
		token := mintInvite(t, store, expires)
		if _, err := store.Redeem(token, fixtureJoiner, probe); err != nil {
			t.Fatalf("the redemption should have succeeded: %v", err)
		}
		// `revoke`'s own comment: a revoke that silently succeeded against an
		// already-redeemed invitation would tell an administrator they had closed a
		// door that is open.
		if err := store.Revoke(token, probe); !errors.Is(err, invite.ErrNotRedeemable) {
			t.Fatalf("revoking a redeemed invitation returned %v, want ErrNotRedeemable", err)
		}
	})
}

// TestAnExpiryIsRoundedToPostgresResolution pins the precision claim the boundary test
// rests on.
//
// 🔴 IT IS A MEASUREMENT, AND THE FIRST VERSION OF IT ASSERTED THE WRONG OPERATION AND
// PASSED. `TIMESTAMPTZ` is microsecond resolution — a property of the SERVER, not of this
// code — and the obvious assumption is that the extra nanoseconds are TRUNCATED. They are
// not: the column ROUNDS. Measured here, .123456789 is stored as .123457. The earlier
// fixture carried an exact half-microsecond, where the server breaks the tie downward and
// rounding is indistinguishable from truncation, so the wrong claim sat in this test's
// NAME and in its assertion while the run was green. See `subSecond`.
//
// 🔴 AND THE CORRECTION REVERSES THE DIRECTION OF THE WINDOW THAT REMAINS, WHICH IS WHY
// GETTING IT RIGHT MATTERS RATHER THAN BEING PEDANTRY. Under truncation the stored expiry
// would be EARLIER than the written one, so a stale in-memory copy would be the permissive
// one and the database the strict one. Under rounding the stored expiry can be up to half
// a microsecond LATER, so the DATABASE is the permissive side: an invitation is redeemable
// for up to 500ns after the instant its minting caller asked it to die.
//
// ⚠ UNREACHABLE TODAY, AND SAID PLAINLY RATHER THAN LEFT AS REASSURANCE. Nothing in the
// serving path compares against the in-memory value — `Redeem` evaluates in SQL and every
// page reaches `StateAt` through `ByToken` — and 500ns is far below any clock a caller
// could aim at. It is recorded because the fix is a DESIGN decision (round at the call
// site that sets `ExpiresAt`, so the value a human is shown is the value stored) and not
// this test's to take.
func TestAnExpiryIsRoundedToPostgresResolution(t *testing.T) {
	db := openTestDB(t)
	store := pgstore.NewInviteStore(db)

	written := fixtureNow.Add(time.Hour + subSecond)
	token := mintInvite(t, store, written)

	stored, ok, err := store.ByToken(token)
	if err != nil || !ok {
		t.Fatalf("reading back: ok=%v err=%v", ok, err)
	}

	wantStored := written.Round(time.Microsecond)
	if !stored.ExpiresAt.Equal(wantStored) {
		// The rival operation is named in the message, because "truncate" is what a
		// reader will assume and this test exists because I assumed it too.
		t.Fatalf(`the stored expiry is not the written one ROUNDED to microseconds.

  written  : %v
  stored   : %v
  rounded  : %v   <- expected
  truncated: %v   <- the operation this test used to assert, and it was wrong

If `+"`stored`"+` now equals `+"`truncated`"+`, the server's behaviour changed: fix the
expectation AND the window paragraph in this test's doc comment, which is written for
rounding and points the other way under truncation.`,
			written, stored.ExpiresAt, wantStored, written.Truncate(time.Microsecond))
	}

	// The bound, which is the half-resolution claim the doc comment makes. It holds for
	// either rounding direction, so it survives a tie-breaking change that the equality
	// above would not.
	if d := stored.ExpiresAt.Sub(written); d > 500*time.Nanosecond || d < -500*time.Nanosecond {
		t.Fatalf("the stored expiry is %v from the written one, further than half the column's resolution", d)
	}

	// 🔴 THE WINDOW, ASSERTED RATHER THAN DESCRIBED, AND IN THE MEASURED DIRECTION. At
	// the written expiry the in-memory copy is already dead (`StateAt` is closed) while
	// the stored copy — rounded LATER — is still open. If a future change closes the
	// window this goes red, which is the signal to delete the paragraph rather than to
	// keep asserting a window that no longer exists.
	at := written
	inMemory := invite.Invite{ProjectID: fixtureProject, Role: control.RoleMember, Inviter: fixtureInviter, ExpiresAt: written}
	if got := inMemory.StateAt(at); got != invite.StateExpired {
		t.Fatalf("the in-memory copy is %q at its own expiry instant, want %q — StateAt's boundary is closed", got, invite.StateExpired)
	}
	if got := stored.StateAt(at); got != invite.StateOpen {
		t.Fatalf(`the rounding window is CLOSED: the stored copy is %q at the written expiry too.

That is a better state than the one this test was written for. Delete the window
paragraph in the doc comment above and this assertion with it, rather than relaxing it.`, got)
	}
}

// TestTwoSimultaneousRedemptionsProduceExactlyOneWinner is the race `Redeem`'s comment
// says the conditional UPDATE exists for.
//
// 🔴 THE FAILURE IT GUARDS IS A DOUBLE JOIN, NOT AN ERROR. Under any read-then-write
// shape both callers read `open` and both go on to write a `member-set` to the control
// journal — two membership records from one invitation, on an append-only file, with no
// undo. So "both succeeded" is the assertion to make, and it must be made with real
// concurrency rather than by reading the SQL.
//
// ⚠ AND THE INSTRUMENT NEEDS ITS OWN CONTROL: N goroutines that never actually overlap
// would report one winner for the wrong reason. They are released together by a closed
// channel, and the test asserts every one of them returned SOMETHING — so a run where
// most goroutines never executed is a failure rather than a pass.
func TestTwoSimultaneousRedemptionsProduceExactlyOneWinner(t *testing.T) {
	db := openTestDB(t)
	store := pgstore.NewInviteStore(db)
	probe := fixtureNow.Add(time.Minute)
	token := mintInvite(t, store, fixtureNow.Add(time.Hour))

	const racers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := store.Redeem(token, fixtureJoiner, probe)
			results[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	winners, refusals, other := 0, 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, invite.ErrNotRedeemable):
			refusals++
		default:
			other++
			t.Errorf("a racer failed with neither success nor ErrNotRedeemable: %v", err)
		}
	}
	// The positive control on the instrument: every goroutine must have run.
	if winners+refusals+other != racers {
		t.Fatalf("only %d of %d racers reported — this run did not measure the race",
			winners+refusals+other, racers)
	}
	if winners != 1 {
		t.Fatalf("%d of %d redemptions succeeded, want exactly 1. More than one means the "+
			"conditional UPDATE is not deciding the race, and the consequence is two "+
			"`member-set` records from one invitation.", winners, racers)
	}
}

// TestTheTableNeverHoldsTheToken is the property every comment in this package rests on.
//
// 🔴 ASSERTED AGAINST THE TABLE'S OWN BYTES, NOT AGAINST THE STRUCT. `Invite` has no
// token field, so a test reading the struct proves only that the type compiles as
// documented. The hazard is an INSERT that put the presented value in some column, which
// only a query over the table can see.
func TestTheTableNeverHoldsTheToken(t *testing.T) {
	db := openTestDB(t)
	store := pgstore.NewInviteStore(db)
	token := mintInvite(t, store, fixtureNow.Add(time.Hour))

	// A positive control first: the DIGEST is findable by this same query shape, so a
	// zero below is a fact about the token rather than about the query.
	var digestHits int
	if err := db.SQL().QueryRow(
		`SELECT count(*) FROM invites WHERE digest = $1`, invite.Digest(token),
	).Scan(&digestHits); err != nil {
		t.Fatalf("the positive control query failed: %v", err)
	}
	if digestHits != 1 {
		t.Fatalf("the positive control found %d rows by digest, want 1 — a zero for the "+
			"token below would prove nothing", digestHits)
	}

	var tokenHits int
	if err := db.SQL().QueryRow(
		`SELECT count(*) FROM invites
		  WHERE digest = $1 OR project_id = $1 OR role = $1 OR inviter = $1
		     OR redeemed_by = $1`, token,
	).Scan(&tokenHits); err != nil {
		t.Fatalf("scanning for the token: %v", err)
	}
	if tokenHits != 0 {
		t.Fatalf("the presented token appears in %d row(s) of `invites`. The token must never "+
			"be stored: a table of tokens is a set of capabilities that can CREATE principals.",
			tokenHits)
	}
}

// TestCreateRefusesAnInvitationNobodyDecidedToGive pins the constructor's refusals.
func TestCreateRefusesAnInvitationNobodyDecidedToGive(t *testing.T) {
	db := openTestDB(t)
	store := pgstore.NewInviteStore(db)
	good := invite.Invite{
		Digest:    "0123456789abcdef",
		ProjectID: fixtureProject,
		Role:      control.RoleMember,
		Inviter:   fixtureInviter,
		CreatedAt: fixtureNow,
		ExpiresAt: fixtureNow.Add(time.Hour),
	}

	for _, tc := range []struct {
		name   string
		mutate func(invite.Invite) invite.Invite
	}{
		{"no digest", func(i invite.Invite) invite.Invite { i.Digest = ""; return i }},
		{"no project", func(i invite.Invite) invite.Invite { i.ProjectID = ""; return i }},
		{"no role", func(i invite.Invite) invite.Invite { i.Role = ""; return i }},
		{"no inviter", func(i invite.Invite) invite.Invite { i.Inviter = ""; return i }},
		{"an unknown role", func(i invite.Invite) invite.Invite { i.Role = control.Role("superuser"); return i }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := store.Create(tc.mutate(good)); err == nil {
				t.Fatal("Create accepted it — an invitation missing any of project, role or " +
					"inviter is authority nobody decided to give")
			}
		})
	}

	// And the happy path, so the refusals above are not all satisfied by a Create that
	// refuses everything.
	if err := store.Create(good); err != nil {
		t.Fatalf("Create refused a complete invitation: %v", err)
	}
	// 🔴 A REPEATED DIGEST IS A LOUD FAILURE, NOT AN UPSERT — the opposite of
	// `SessionStore.Create`, deliberately. See `InviteStore.Create`'s comment.
	if err := store.Create(good); err == nil {
		t.Fatal("a second Create with the same digest succeeded. It must fail loudly: a " +
			"repeated invite digest is a 256-bit collision, and silently overwriting would " +
			"replace somebody else's live invitation with a different project and role.")
	}
}
