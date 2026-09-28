//go:build pgtest

// The SESSION table against a real server — the half of this package that 28(c) put on
// the authentication path and that no gate in this repository had ever executed.
//
// # 🔴 WHY THIS FILE EXISTS, AND WHY ITS ABSENCE WAS THE SHARPEST THING IN THE ROUND
//
// 28(c)'s operator decision is that ONE DSN moves BOTH tables. The invite half arrived
// with six real-SQL tests in `invites_pgtest_test.go`; the SESSION half arrived with
// none — measured, the whole tier mentioned `SessionStore` exactly once, in a comment.
// So on the day this became the backend for every authenticated request on a deployed
// surface, `Lookup`, `Create`, `Revoke` and `Prune` had been run against a real
// PostgreSQL by nothing.
//
// ⚠ AND THE UNTAGGED TESTS COULD NOT HAVE COVERED IT. `identity`'s own suite exercises
// `FileSessionStore`; the interface conformance line in `sessions.go` is a COMPILE-time
// assertion, which says the methods exist and nothing about what the SQL does. The
// failure mode this closes is not "the code is wrong" — it is "nobody has run it".
//
// # ⚠ WHAT THIS FILE IS STILL NOT
//
// It drives the STORE, not the chain. Nothing here signs in through `internal/ui`, so a
// defect in how `identity.CookieSession` USES this store is out of scope — that seam is
// `cmd/cairn-ui/database_pgtest_test.go`'s, and that case asserts the table exists
// rather than that a session round-trips. Named so the green is not read wider.
package pgstore_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/pgstore"
)

// 🔴 PIN THE CLOCK WHENEVER YOU PIN THE DATE — THE TWO ARE NOT ONE DECISION, AND TWO
// CASES HERE WERE WRITTEN WITHOUT THE SECOND AND FAILED. `AGENTS.md` requires an
// obviously-synthetic YEAR-2000 date in a fixture that needs one, and
// `SessionStore.Create` PRUNES on the write path — so a year-2000 expiry measured against
// the REAL clock is deleted by the very call that inserted it. Measured: `Count` read 0
// where the upsert had just written twice, and the round trip reported a one-second-old
// session as not live. Neither was a defect in the SQL; both read exactly like one.
// `db.Now` is the injection point that makes the pair consistent, and every case below
// sets it.
//
// aSession is one record, built from a pinned instant so every expiry assertion below is
// about an arithmetic boundary rather than about how long the test took to run.
func aSession(id string, issued time.Time, ttl time.Duration) identity.Session {
	return identity.Session{
		Digest:    identity.SessionDigest(id),
		Kind:      control.KindUser,
		Principal: control.DerivedID(control.PrefixUser, "pgtest-session-user"),
		IssuedAt:  issued,
		ExpiresAt: issued.Add(ttl),
	}
}

// TestASessionRoundTripsThroughPostgres is the base case, and it is the one that would
// have caught "nobody has ever run this".
//
// 🔴 IT PRESENTS THE ID AND NEVER THE DIGEST, which is the interface's own rule and the
// property most likely to be broken by a caller: `SessionStore.Lookup` hashes what it is
// given, so a store that accidentally compared the presented id against the stored digest
// would return "no such session" for every real session — a total sign-in outage that
// every compile-time assertion in this package is blind to.
func TestASessionRoundTripsThroughPostgres(t *testing.T) {
	db := openTestDB(t)
	issued := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	// One minute after issue: inside the hour, so the row is live AND survives the prune
	// `Create` runs on the write path. See the note on `aSession`.
	db.Now = func() time.Time { return issued.Add(time.Minute) }
	store := pgstore.NewSessionStore(db)

	const id = "pgtest-session-round-trip-id"
	rec := aSession(id, issued, time.Hour)
	if err := store.Create(rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, live, err := store.Lookup(id)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !live {
		t.Fatal("a session created one second ago is not live, so no browser could ever stay signed in")
	}
	if got.Digest != rec.Digest || got.Kind != rec.Kind || got.Principal != rec.Principal {
		t.Errorf("the row came back as a different principal: got kind=%q principal=%q digest=%q, want kind=%q principal=%q digest=%q",
			got.Kind, got.Principal, got.Digest, rec.Kind, rec.Principal, rec.Digest)
	}
	// 🔴 THE TIMESTAMPS MUST SURVIVE THE ROUND TRIP AS UTC INSTANTS. `TIMESTAMPTZ` is
	// stored as an instant and handed back in the session's time zone, so a store that
	// dropped the `.UTC()` would return the same moment wearing a different location —
	// equal under `Equal`, NOT equal under `==`, and `Live` uses `Before`, which is
	// instant-wise. Asserting `Equal` is therefore the honest assertion; asserting `==`
	// would pin the driver's zone handling, which is not this package's contract.
	if !got.ExpiresAt.Equal(rec.ExpiresAt) {
		t.Errorf("ExpiresAt round-tripped to %s, want the instant %s", got.ExpiresAt, rec.ExpiresAt)
	}
	if !got.IssuedAt.Equal(rec.IssuedAt) {
		t.Errorf("IssuedAt round-tripped to %s, want the instant %s", got.IssuedAt, rec.IssuedAt)
	}
}

// TestTheSessionTableNeverHoldsThePresentedID is `TestTheTableNeverHoldsTheToken`'s claim
// for the second secret this surface stores.
//
// 🔴 IT READS EVERY COLUMN OF EVERY ROW AS TEXT, rather than asserting that the `digest`
// column is a digest. A guard that checked one named column would pass a store that had
// grown a second column holding the id — which is exactly how a "keep the raw value for
// debugging" change ships.
func TestTheSessionTableNeverHoldsThePresentedID(t *testing.T) {
	db := openTestDB(t)
	issued := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	db.Now = func() time.Time { return issued.Add(time.Minute) }
	store := pgstore.NewSessionStore(db)

	const id = "pgtest-session-secret-id-do-not-store"
	if err := store.Create(aSession(id, issued, time.Hour)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rows, err := db.SQL().Query(`SELECT digest::text, kind::text, principal::text,
		issued_at::text, expires_at::text FROM sessions`)
	if err != nil {
		t.Fatalf("reading the table back: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var cols [5]string
		if err := rows.Scan(&cols[0], &cols[1], &cols[2], &cols[3], &cols[4]); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen++
		for i, c := range cols {
			if strings.Contains(c, id) {
				t.Errorf("column %d of a stored session contains the PRESENTED ID verbatim (%q). "+
					"A table holding ids IS a set of bearer credentials: a backup, a replica or "+
					"any SELECT is then every signed-in user.", i, c)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	// 🔴 THE POSITIVE CONTROL. Without it a query that returned NO rows — a wrong table, a
	// wrong search_path, a Create that silently did nothing — passes the loop above
	// vacuously, and "the id is not in the table" would be a fact about an empty result.
	if seen != 1 {
		t.Fatalf("read %d row(s), want exactly 1 — the absence checked above is otherwise a "+
			"statement about an empty query", seen)
	}
}

// TestTheLivenessBoundaryAgreesWithSessionLive is the expiry boundary, driven at the exact
// instant and one microsecond either side.
//
// 🔴 THE PACKAGE DOC PROMISES THIS AND NOTHING MEASURED IT. `pgstore.go` argues that the
// expiry predicate is deliberately NOT written in SQL, because `WHERE expires_at > now()`
// would be a SECOND spelling of `identity.Session.Live` and "the two differ on exactly one
// instant". That is an argument about a boundary; this is the case that drives it.
//
// ⚠ MICROSECONDS, NOT NANOSECONDS, AND THAT IS POSTGRES RATHER THAN A CHOICE.
// `TIMESTAMPTZ` has microsecond resolution, so a nanosecond offset is rounded on the way
// in and the ±1 arms would land on the boundary itself. `invites_pgtest_test.go` records
// the same constraint for the invite expiry.
func TestTheLivenessBoundaryAgreesWithSessionLive(t *testing.T) {
	db := openTestDB(t)

	expires := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	for _, arm := range []struct {
		name     string
		now      time.Time
		wantLive bool
	}{
		{"one microsecond before expiry (the last live instant)", expires.Add(-time.Microsecond), true},
		// 🔴 THE BOUNDARY ITSELF. `Live` is `now.Before(ExpiresAt)`, so AT the expiry a
		// session is DEAD. A SQL `>=` would answer the opposite here and nowhere else.
		{"exactly at expiry (the boundary)", expires, false},
		{"one microsecond after expiry", expires.Add(time.Microsecond), false},
	} {
		t.Run(arm.name, func(t *testing.T) {
			// The clock is the DB wrapper's, which is what `Lookup` consults — the same
			// injection point the package doc names as "the program's clock, not the
			// database's".
			db.Now = func() time.Time { return arm.now }
			t.Cleanup(func() { db.Now = nil })
			store := pgstore.NewSessionStore(db)

			id := "pgtest-boundary-" + strings.ReplaceAll(arm.name, " ", "-")
			rec := aSession(id, expires.Add(-time.Hour), time.Hour) // ExpiresAt == expires
			if !rec.ExpiresAt.Equal(expires) {
				t.Fatalf("fixture error: ExpiresAt is %s, want %s", rec.ExpiresAt, expires)
			}
			if err := store.Create(rec); err != nil {
				t.Fatalf("Create: %v", err)
			}

			_, live, err := store.Lookup(id)
			if err != nil {
				t.Fatalf("Lookup: %v", err)
			}
			if live != arm.wantLive {
				t.Errorf("Lookup reports live=%v at %s against an expiry of %s, want live=%v — "+
					"the SQL and `identity.Session.Live` disagree about this instant, which is the "+
					"one thing the package doc says must not happen",
					live, arm.now, expires, arm.wantLive)
			}
			// …and the Go predicate's own answer, so a divergence names which side moved.
			if got := rec.Live(arm.now); got != arm.wantLive {
				t.Errorf("identity.Session.Live itself answers %v at this instant, want %v — the "+
					"disagreement is in the PREDICATE, not in the SQL", got, arm.wantLive)
			}
		})
	}
}

// TestRevokeRemovesTheSessionAndAnUnknownIDIsNotAnError pins the logout, both halves.
//
// 🔴 THE SECOND HALF IS THE ONE WITH A REAL FAILURE MODE. The interface requires that an
// id naming no session is NOT an error, because a browser holding an already-expired
// cookie must still be able to complete a logout — otherwise the one action that clears a
// stale credential is the one action that fails while it is stale.
func TestRevokeRemovesTheSessionAndAnUnknownIDIsNotAnError(t *testing.T) {
	db := openTestDB(t)
	store := pgstore.NewSessionStore(db)

	const id = "pgtest-session-to-revoke"
	issued := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	db.Now = func() time.Time { return issued.Add(time.Minute) }
	if err := store.Create(aSession(id, issued, time.Hour)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, live, _ := store.Lookup(id); !live {
		t.Fatal("POSITIVE CONTROL FAILED: the session is not live before the revoke, so the " +
			"assertion after it would be about a session that never existed")
	}
	if err := store.Revoke(id); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, live, err := store.Lookup(id); err != nil {
		t.Fatalf("Lookup after revoke: %v", err)
	} else if live {
		t.Error("the session is STILL LIVE after a successful-looking Revoke — a logout that " +
			"reports success and revokes nothing")
	}
	if err := store.Revoke("pgtest-session-that-never-existed"); err != nil {
		t.Errorf("revoking an unknown id returned %v, want nil: a browser holding a dead cookie "+
			"must still be able to log out", err)
	}
}

// TestPruneDeletesStrictlyLessThanLiveRejects is the claim `Prune`'s own comment makes,
// and the direction matters more than the deletion.
//
// 🔴 A PRUNE THAT DELETED **MORE** THAN THE LIVENESS TEST REJECTS WOULD SIGN SOMEBODY OUT.
// `Prune` is `expires_at < now` while `Live` is false AT `expires_at`, so a row sitting
// exactly on the boundary is REFUSED by `Lookup` and SURVIVES the prune. That one-instant
// gap is deliberate and is the whole assertion here: the test keeps a boundary row and a
// live row, prunes, and requires both still present while the clearly-dead one is gone.
func TestPruneDeletesStrictlyLessThanLiveRejects(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	db.Now = func() time.Time { return now }
	store := pgstore.NewSessionStore(db)

	rows := []struct {
		id       string
		expires  time.Time
		survives bool
	}{
		{"pgtest-prune-live", now.Add(time.Hour), true},
		// ON the boundary: dead to `Lookup`, and `Prune` must NOT take it.
		{"pgtest-prune-boundary", now, true},
		{"pgtest-prune-expired", now.Add(-time.Hour), false},
	}
	for _, r := range rows {
		rec := aSession(r.id, r.expires.Add(-2*time.Hour), r.expires.Sub(r.expires.Add(-2*time.Hour)))
		if !rec.ExpiresAt.Equal(r.expires) {
			t.Fatalf("fixture error for %s: ExpiresAt %s, want %s", r.id, rec.ExpiresAt, r.expires)
		}
		if err := store.Create(rec); err != nil {
			t.Fatalf("Create %s: %v", r.id, err)
		}
	}
	// 🔴 `Create` PRUNES ON THE WRITE PATH, so the expired row may already be gone by now —
	// which is the behaviour, not a problem. What this measures is the STATE AFTER, so an
	// explicit Prune is called and the counts are read once at the end.
	if err := store.Prune(); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	for _, r := range rows {
		var present bool
		if err := db.SQL().QueryRow(
			`SELECT EXISTS (SELECT 1 FROM sessions WHERE digest = $1)`,
			identity.SessionDigest(r.id),
		).Scan(&present); err != nil {
			t.Fatalf("existence check for %s: %v", r.id, err)
		}
		if present != r.survives {
			if r.survives {
				t.Errorf("%s was DELETED by Prune and should not have been. A prune that deletes "+
					"more than `Live` rejects signs somebody out; one that deletes less only "+
					"leaves a dead row behind.", r.id)
			} else {
				t.Errorf("%s survived Prune, so expired rows accumulate without bound", r.id)
			}
		}
	}

	// The boundary row is kept AND still refused — the two halves of the same instant.
	if _, live, err := store.Lookup("pgtest-prune-boundary"); err != nil {
		t.Fatalf("Lookup of the boundary row: %v", err)
	} else if live {
		t.Error("the row sitting exactly on its expiry is reported LIVE, so `Lookup` and " +
			"`Session.Live` disagree at the boundary the package doc is about")
	}
}

// TestReCreatingASessionDigestReplacesRatherThanDuplicates pins the `ON CONFLICT DO UPDATE`.
//
// 🔴 THE FAILURE IT PREVENTS IS A LOGOUT THAT REVOKES ONE OF TWO ROWS. `Create`'s comment
// names it: two rows for one digest would make `Revoke` remove one and leave the other —
// "a logout that reports success and revokes nothing". A primary key makes the duplicate
// impossible, so this asserts the COUNT as well as the new value.
func TestReCreatingASessionDigestReplacesRatherThanDuplicates(t *testing.T) {
	db := openTestDB(t)
	issued := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	// Pinned for `aSession`'s reason: without it `Create`'s prune removes both writes and
	// `Count` reads 0, which reads as a missing upsert rather than as an expired fixture.
	db.Now = func() time.Time { return issued.Add(time.Minute) }
	store := pgstore.NewSessionStore(db)

	const id = "pgtest-session-reissued"
	if err := store.Create(aSession(id, issued, time.Hour)); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	second := aSession(id, issued, 4*time.Hour)
	if err := store.Create(second); err != nil {
		t.Fatalf("second Create: %v", err)
	}

	n, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 1 {
		t.Fatalf("the table holds %d row(s) for one digest, want 1 — `Revoke` would then remove "+
			"one and leave the other, which is a logout that reports success and revokes nothing", n)
	}
	got, live, err := store.Lookup(id)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !live {
		t.Fatal("the re-issued session is not live")
	}
	if !got.ExpiresAt.Equal(second.ExpiresAt) {
		t.Errorf("the row kept the FIRST expiry (%s), want the re-issued one (%s) — the upsert "+
			"inserted without updating, so a refreshed session keeps its old lifetime",
			got.ExpiresAt, second.ExpiresAt)
	}
}
