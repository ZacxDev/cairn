package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// SessionStore is `identity.SessionStore` over Postgres.
//
// 🔴 IT TAKES THE PRESENTED ID AND HASHES IT ITSELF, WHICH IS THE INTERFACE'S OWN RULE
// AND NOT A CHOICE THIS TYPE MAKES. `identity.SessionStore`'s doc: *"Every method takes
// the presented id, NOT a digest, and hashes it itself. A caller that hashed would be
// the second place the hash function is chosen."* So the digest is computed here, by
// `identity.SessionDigest`, and never passed in.
type SessionStore struct {
	db *DB
}

// NewSessionStore wraps an open database.
func NewSessionStore(db *DB) *SessionStore { return &SessionStore{db: db} }

var _ identity.SessionStore = (*SessionStore)(nil)

// StatementTimeout bounds every statement this package issues on the serving path.
//
// 🔴 WITHOUT IT A POSTGRES THAT HANGS PRODUCES EXACTLY THE "LOOKS HEALTHY, SERVES NOBODY"
// STATE EVERY STARTUP REFUSAL IN `cmd/cairn-ui` EXISTS AGAINST — and it produces it AFTER
// startup, where no refusal can see it. The ERROR direction was always safe:
// `identity.CookieSession` refuses every session when the store returns an error, which is
// fail-closed and deliberate. The HANG direction was not covered at all. A server that
// accepts TCP and then stops answering — a failover, a lock, a saturated backend — blocks
// every authenticated request indefinitely, the pool exhausts at 8, and `/healthz` keeps
// answering 200 because it is served before the chain and touches nothing. The orchestrator
// therefore never restarts the pod.
//
// ⚠ THE MITIGATION THAT USED TO BE WRITTEN HERE DID NOT ADDRESS IT, AND IS RETRACTED. It
// read: "the pool caps concurrency at 8, so an abandoned query occupies a bounded resource".
// That bounds the DATABASE's resource, not this surface's availability — 8 blocked
// connections is precisely the outage.
//
// ⚠ 10s IS A CEILING ON A HUMAN-FACING LOOKUP, NOT A TUNING KNOB. Every statement here is a
// single primary-key probe or one small insert; one that has not answered in ten seconds has
// already failed the request that is waiting for it. It is deliberately far above any
// healthy latency so that a slow-but-working database is never cut off.
const StatementTimeout = 10 * time.Second

// opCtx is the bounded context every method here runs under.
//
// 🔴 THE CALLER MUST `defer cancel()`. The interface has no context parameter —
// `identity.SessionStore` was shaped around a file store, where every operation is a local
// read or an atomic rename and neither is worth a deadline — so the bound is applied here
// rather than passed in. Widening the interface to carry a real request context is still
// the right fix and is still a change to `internal/identity`, `internal/ui`'s call sites and
// the file store together; what this does is stop the UNBOUNDED case while that waits.
//
// ⚠ THE HONEST RESIDUAL: a request the browser abandoned still runs its query to completion,
// because the deadline is this package's and not the request's. Ten seconds, not forever.
func (d *DB) opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), StatementTimeout)
}

// Lookup resolves a presented id to a LIVE session.
//
// 🔴 THE LIVENESS TEST IS `rec.Live(now)` IN GO, NOT A `WHERE` CLAUSE. See the package
// doc: a SQL predicate would be a second spelling of a boundary that is closed at
// `ExpiresAt`, and the two would disagree on exactly one instant.
//
// ⚠ A ROW THAT EXISTS AND HAS EXPIRED ANSWERS `false`, INDISTINGUISHABLY FROM ONE THAT
// DOES NOT EXIST. That is the interface's stated contract ("the two are the same
// observable to a caller, deliberately") and not an accident of this implementation.
func (s *SessionStore) Lookup(presentedID string) (identity.Session, bool, error) {
	if presentedID == "" {
		return identity.Session{}, false, nil
	}
	digest := identity.SessionDigest(presentedID)

	var rec identity.Session
	var kind, principal string
	ctx, cancel := s.db.opCtx()
	defer cancel()
	err := s.db.sql.QueryRowContext(ctx,
		`SELECT digest, kind, principal, issued_at, expires_at FROM sessions WHERE digest = $1`,
		digest,
	).Scan(&rec.Digest, &kind, &principal, &rec.IssuedAt, &rec.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.Session{}, false, nil
	}
	if err != nil {
		// The digest is not interpolated: it names a live session.
		return identity.Session{}, false, fmt.Errorf("pgstore: reading a session: %w", err)
	}
	rec.Kind = control.Kind(kind)
	rec.Principal = control.ID(principal)

	if !rec.Live(s.db.now()) {
		return identity.Session{}, false, nil
	}
	return rec, true, nil
}

// Create stores one new session, replacing any row with the same digest.
//
// 🔴 `ON CONFLICT DO UPDATE` RATHER THAN AN INSERT THAT CAN FAIL, FOR THE REASON THE
// FILE STORE GIVES: two rows for one digest would make `Revoke` remove one and leave
// the other — "a logout that reports success and revokes nothing". A primary key makes
// the duplicate impossible, and the upsert makes the re-issue succeed rather than
// erroring on a collision the caller cannot do anything about.
func (s *SessionStore) Create(rec identity.Session) error {
	if rec.Digest == "" {
		return errors.New("pgstore: a session with no digest would be unreachable and would never expire")
	}
	ctx, cancel := s.db.opCtx()
	defer cancel()
	if _, err := s.db.sql.ExecContext(ctx,
		`INSERT INTO sessions (digest, kind, principal, issued_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (digest) DO UPDATE SET
		   kind = EXCLUDED.kind,
		   principal = EXCLUDED.principal,
		   issued_at = EXCLUDED.issued_at,
		   expires_at = EXCLUDED.expires_at`,
		rec.Digest, string(rec.Kind), string(rec.Principal), rec.IssuedAt.UTC(), rec.ExpiresAt.UTC(),
	); err != nil {
		return fmt.Errorf("pgstore: creating a session: %w", err)
	}
	// Pruning on the write path, for the reason `FileSessionStore.Create` does it: a
	// background goroutine whose failure would be silent is worse than a bounded cost
	// on a path somebody is watching. A failure to prune is NOT a failure to sign in,
	// so its error is dropped deliberately — the session is already durable.
	_ = s.Prune()
	return nil
}

// Revoke removes the session a presented id names. This is the logout.
//
// 🔴 AN ID WITH NO SESSION IS NOT AN ERROR, which the interface requires and which
// `DELETE` gives for free: it affects zero rows and reports success. A browser holding
// a cookie whose session has already expired must still be able to complete a logout,
// or the one action that clears a stale credential is the one action that fails while
// the credential is stale.
func (s *SessionStore) Revoke(presentedID string) error {
	if presentedID == "" {
		return nil
	}
	ctx, cancel := s.db.opCtx()
	defer cancel()
	if _, err := s.db.sql.ExecContext(ctx,
		`DELETE FROM sessions WHERE digest = $1`, identity.SessionDigest(presentedID),
	); err != nil {
		return fmt.Errorf("pgstore: revoking a session: %w", err)
	}
	return nil
}

// Prune deletes expired session rows.
//
// ⚠ THIS ONE COMPARISON IS IN SQL, AND THE PACKAGE DOC'S RULE STILL HOLDS. The rule is
// that the predicate deciding whether a caller IS AUTHENTICATED has one spelling; this
// statement decides nothing about a caller, it reclaims storage. It is deliberately
// written to delete STRICTLY LESS than `Live` rejects — `expires_at < $1` where `Live`
// is false at `expires_at = $1` — so a row on the exact boundary survives the prune and
// is refused by `Lookup`. A prune that deleted MORE than the liveness test rejects
// could sign somebody out; one that deletes less can only leave a dead row behind.
func (s *SessionStore) Prune() error {
	ctx, cancel := s.db.opCtx()
	defer cancel()
	if _, err := s.db.sql.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at < $1`, s.db.now(),
	); err != nil {
		return fmt.Errorf("pgstore: pruning sessions: %w", err)
	}
	return nil
}

// Count is the number of rows in the table, live or not. For tests and for an operator
// probe; nothing in the serving path reads it.
func (s *SessionStore) Count() (int, error) {
	var n int
	ctx, cancel := s.db.opCtx()
	defer cancel()
	if err := s.db.sql.QueryRowContext(ctx, `SELECT count(*) FROM sessions`).Scan(&n); err != nil {
		return 0, fmt.Errorf("pgstore: counting sessions: %w", err)
	}
	return n, nil
}
