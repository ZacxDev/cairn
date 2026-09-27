package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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

// bg is the context every method here runs under.
//
// 🔴 IT IS `context.Background()` BECAUSE THE INTERFACE HAS NO CONTEXT PARAMETER, AND
// THAT IS RECORDED RATHER THAN QUIETLY DONE. `identity.SessionStore` was shaped around
// a file store where every operation is a local read or an atomic rename — neither
// cancellable and neither worth a deadline. A network call is different, and the honest
// consequence is that a request the browser abandoned still runs its query to
// completion here.
//
// ⚠ WHAT MAKES THAT ACCEPTABLE RATHER THAN IGNORED: the pool caps concurrency at 8, so
// an abandoned query occupies a bounded resource, and each of these is a single
// primary-key statement. Widening the interface to carry a context is the right fix and
// it is a change to `internal/identity`, `internal/ui`'s call sites and the file store
// together — deliberately not bundled into the change that introduces the backend.
func bg() context.Context { return context.Background() }

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
	err := s.db.sql.QueryRowContext(bg(),
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
	if _, err := s.db.sql.ExecContext(bg(),
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
	if _, err := s.db.sql.ExecContext(bg(),
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
	if _, err := s.db.sql.ExecContext(bg(),
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
	if err := s.db.sql.QueryRowContext(bg(), `SELECT count(*) FROM sessions`).Scan(&n); err != nil {
		return 0, fmt.Errorf("pgstore: counting sessions: %w", err)
	}
	return n, nil
}
