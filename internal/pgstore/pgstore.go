// Package pgstore is the PostgreSQL backend for the browser surface's MUTABLE state:
// the session table and the invite table.
//
// # 🔴 WHAT IS IN HERE AND WHAT IS DELIBERATELY NOT
//
// 🔴 THE CONTROL JOURNAL IS NOT IN HERE, AND THAT IS A DECISION RATHER THAN A STAGE
// NOBODY GOT TO. `control.FileStore`'s own doc names "the Postgres backend behind
// `Store`" as where its limits end, so the obvious reading of this package is that it
// is the first half of that move. It is not, and the reason is mechanical: a
// `control.Store` over Postgres would put this package's driver in `internal/control`,
// which `cmd/cairn-server` imports — and that binary's import closure is
// [depspolicy.LinkedBinaryRoots], which must stay free of every third-party module.
// Moving the journal therefore also decides where `cairn-server -create-user` and
// `-set-member` live, because those two write it. That is a separate change with a
// separate argument, and it is NOT unblocked by this one.
//
// So the split here is not "durable vs ephemeral". It is:
//
//   - the JOURNAL is append-only authority, read by people, written by an operator
//     binary that may not link a driver — it stays a file;
//   - SESSIONS and INVITES are mutable, high-churn, authoritative about nothing
//     historical, and read only by `cmd/cairn-ui` — they come here.
//
// ⚠ THE CONSEQUENCE, SAID PLAINLY SO IT IS NOT DISCOVERED LATER: `cairn-ui` still
// needs its volume, because the journal is still on it. This package does NOT make the
// UI deployment stateless and does NOT by itself lift it past one replica.
//
// # 🔴 EVERY TABLE HOLDS A DIGEST, NEVER A TOKEN
//
// Both tables are keyed on `sha256(the value the browser presents)`, for the reason
// `identity.Session` records about the file it replaces: a table holding ids IS a set
// of bearer credentials, and a database backup, a replica, or a `SELECT` by anyone
// with read access is then every signed-in user and every outstanding invite. Hashing
// makes all three useless for authenticating.
//
// # ⚠ ONE PROPERTY OF THE FILE STORE IS NOT REPRODUCED HERE, AND IT IS NOT AN OVERSIGHT
//
// `identity.FileSessionStore.Lookup` scans every record with NO early exit, pinned
// structurally by `TestTheLookupScanHasNoEarlyExit`, because a scan that stops at the
// first match leaks the matched record's POSITION IN THE FILE through response time.
// There is no scan here — the lookup is an index probe on a primary key — so that
// guard has nothing to attach to, and re-spelling it would be a guard over code that
// does not exist.
//
// What replaces it is the argument that made constant-time comparison "defence in
// depth" rather than load-bearing in the first place, quoted from `digestsEqual` and
// still true: the values compared are SHA-256 digests, so a timing oracle on the probe
// leaks a prefix of a DIGEST, and a digest cannot be inverted to the id a browser would
// have to present. ⚠ It is weaker than the file store's property and is recorded as
// weaker; what it is not is unconsidered.
//
// # 🔴 THE EXPIRY PREDICATE IS NOT WRITTEN IN SQL
//
// A `WHERE expires_at > now()` would be a SECOND spelling of `Session.Live`, and the
// two differ on exactly one instant: `Live` is `now.Before(ExpiresAt)`, closed AT the
// expiry, and its own comment records that an injected clock lands on precisely that
// instant whenever a test pins it there. Two spellings of a boundary condition
// disagree the first time either is edited, and this repository's rule is one rule in
// one place. So every query here selects the ROW and the liveness decision is taken in
// Go by the same predicate the file store uses.
//
// ⚠ THE COST IS REAL AND BOUNDED FOR SESSIONS, AND UNBOUNDED FOR INVITES — the two are
// NOT the same and one sentence used to cover both. An expired row is fetched before it
// is rejected, and dead rows accumulate until something deletes them.
// [SessionStore.Prune] is that something FOR THE SESSION TABLE, called on the write paths
// for the reason `FileSessionStore.Create` prunes on write: a background goroutine whose
// failure would be silent is worse than a bounded cost on a path somebody is watching.
//
// 🔴 NOTHING PRUNES `invites`, EVER, AND THAT IS STATED RATHER THAN IMPLIED BY A LINK TO
// A FUNCTION THAT DOES NOT COVER IT. There is no `DELETE FROM invites` anywhere in this
// package. Revoked, expired and redeemed invitations are kept forever — which is CORRECT
// for a redeemed one (the schema comment argues it: deleting the row would make a replayed
// link indistinguishable from an unknown one) and is merely UNRECLAIMED for the other two.
// The growth is bounded by how many invitations humans mint, so it is an accepted cost
// rather than a leak; it is written down because the previous wording linked `[DB.Prune]`
// — a method that does not exist, on a type that has no prune — and read as though both
// tables were reclaimed.
package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	// The driver registers itself with `database/sql` as a side effect. It is the ONLY
	// third-party import in this package and the reason the package exists as its own
	// unit rather than as files inside `internal/identity`: that package is reached by
	// `cmd/cairn-server`, whose import closure may not contain a third-party module.
	_ "github.com/lib/pq"
)

// DriverName is the `database/sql` driver this package opens with, spelled once.
//
// ⚠ IT IS `postgres`, WHICH IS `lib/pq`'s REGISTERED NAME AND NOT A FREE CHOICE. A
// second driver registering the same name panics at init, so this constant is also the
// thing to read before adding one.
const DriverName = "postgres"

// DB is an open connection pool with the schema applied.
//
// 🔴 IT IS A TYPE RATHER THAN A BARE `*sql.DB` SO THE SCHEMA CANNOT BE SKIPPED. The
// stores below take a `*DB`, and the only constructor that produces one is [Open],
// which applies the schema before returning. A `*sql.DB` parameter would let a caller
// hand over a pool pointed at an empty database and get a store whose every method
// fails at runtime with a relation-does-not-exist the caller cannot act on.
type DB struct {
	sql *sql.DB

	// Now is the clock. Injected so expiry is testable without sleeping; nil means
	// `time.Now().UTC()`.
	//
	// 🔴 THE CLOCK IS THE PROGRAM'S, NOT THE DATABASE'S. `now()` in SQL would be the
	// SERVER's clock, which is a second clock the tests cannot pin and the pod cannot
	// see — and which would disagree with `identity.Session.Live`, evaluated in this
	// process, about which sessions are alive.
	Now func() time.Time
}

func (d *DB) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

// SQL is the underlying pool, for tests and for a caller that must run its own
// statement. Not for routine use: every query this package's stores need is a method.
func (d *DB) SQL() *sql.DB { return d.sql }

// Close releases the pool.
func (d *DB) Close() error { return d.sql.Close() }

// ErrNoDSN refuses an empty connection string at CONSTRUCTION time, in the same
// fail-closed direction as `identity.ErrNoSessionStore`.
//
// ⚠ IT MATTERS BECAUSE `sql.Open` DOES NOT CONNECT. Handed an empty DSN it returns a
// usable-looking `*sql.DB` and no error; the failure surfaces on the first query, which
// for this program is the first request a signed-in user makes rather than startup. A
// pod that starts and then cannot authenticate anybody is strictly worse than one that
// refuses to start, which is the ruling `cmd/cairn-ui` already applies to its other
// configuration.
var ErrNoDSN = errors.New("pgstore: no connection string was configured, so no session or invite could ever be resolved")

// redactedDSN is what replaces a connection string anywhere one would otherwise be
// printed. It is a fixed marker rather than a truncation, because a prefix of a DSN is
// still a hostname and a username.
const redactedDSN = "<connection string redacted>"

// redact removes the connection string from an error on its way out of this package.
//
// 🔴 THE SENTENCE "THE CONNECTION STRING IS DELIBERATELY NOT ECHOED" WAS FALSE, AND THIS
// IS WHAT MAKES IT TRUE. Not interpolating the DSN into the FORMAT covers this package's
// own text and nothing about the error it WRAPS. `lib/pq` hands a `postgres://…` DSN to
// `net/url.Parse`, whose failure is a `*url.Error` carrying `URL` — the raw string,
// verbatim, password and all. So a refusal that says twice that it does not echo the
// value printed:
//
//	parse "postgres://cairn:SuperSecret123@db.example:not-a-port/cairn": invalid port
//
// MEASURED on the built binary, against the KEYWORD/VALUE form as the control: that form
// reports only the offending key (`missing "=" after "oops"`) and leaks nothing, which is
// why the repo's own tests — all keyword/value — were green while this was broken. ⚠ The
// URL form is the one a Kubernetes secret carries and the one `README.md` documents, so
// the leaking shape was the deployed shape.
//
// 🔴 IT IS TWO PASSES AND BOTH ARE LOAD-BEARING. The typed pass rewrites `*url.Error.URL`,
// which is the known carrier. The textual pass then removes any remaining occurrence of
// the DSN — because a driver is free to put it in any message it likes, and a redactor
// that only knew today's one error type would go quietly stale the next time `lib/pq`
// changes. Neither pass alone is sufficient: the typed one misses an untyped mention, and
// the textual one misses a `*url.Error` whose `URL` differs from the DSN by so much as a
// trimmed space.
//
// ⚠ IT CANNOT CATCH A DRIVER THAT PRINTS A DECOMPOSED PIECE — a bare password with no
// surrounding DSN would survive both passes. That is stated rather than claimed away; the
// textual pass is a floor, not a proof.
func redact(err error, dsn string) error {
	if err == nil {
		return nil
	}
	// Pass 1: the typed carrier. `url.Error` is a struct with an exported `URL`, so this
	// rewrites the field rather than the rendered string, which keeps `errors.Is`/`As`
	// working on whatever it wraps.
	var uerr *url.Error
	if errors.As(err, &uerr) {
		uerr.URL = redactedDSN
	}
	// Pass 2: anything left. A trimmed DSN is also removed, because `cmd/cairn-ui` passes
	// the operator's value RAW and a leading space is a spelling the flag path allows.
	msg := err.Error()
	for _, secret := range []string{dsn, strings.TrimSpace(dsn)} {
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, redactedDSN)
		}
	}
	if msg == err.Error() {
		return err
	}
	// 🔴 THE REDACTED FORM IS A NEW ERROR VALUE AND THE ORIGINAL IS NOT WRAPPED, WHICH IS
	// DELIBERATE AND IS THE ONE COST. Wrapping with `%w` would keep the original reachable
	// through `errors.Unwrap`, and its `Error()` is the leaking string — so a caller that
	// unwrapped and printed would undo this. Nothing in this repository matches on a
	// driver error's type, so the loss is a capability nobody uses.
	return errors.New(msg)
}

// Open connects, verifies the connection, and applies the schema.
//
// 🔴 IT PINGS, BECAUSE `sql.Open` VALIDATES NOTHING. `sql.Open` parses the DSN and
// returns a lazy pool: a wrong host, a wrong password and a database that does not
// exist are all indistinguishable from success until the first query. The ping is what
// turns "the pod started" into evidence the datastore is reachable — the distinction
// this repository states as "a deploy reporting success is a claim about the deploy".
//
// 🔴 AND IT APPLIES THE SCHEMA RATHER THAN CHECKING FOR IT. See [Migrate].
func Open(ctx context.Context, dsn string) (*DB, error) {
	if dsn == "" {
		return nil, ErrNoDSN
	}
	pool, err := sql.Open(DriverName, dsn)
	if err != nil {
		// The DSN is NOT interpolated into this message, and `redact` is what makes that
		// true of the WRAPPED error as well — see its own comment.
		return nil, fmt.Errorf("pgstore: opening the connection pool failed (the connection string is deliberately not echoed — it carries a password): %w", redact(err, dsn))
	}
	// 🔴 A BOUND ON CONNECTIONS, BECAUSE THE DEFAULT IS UNBOUNDED. `database/sql`'s
	// zero value for `MaxOpenConns` is "no limit", so a burst of requests against a
	// slow database opens connections until Postgres refuses them — and Postgres
	// refusing a connection is an error this program reports as an auth failure. The
	// value is small on purpose: this surface serves human-rate traffic from one
	// replica, and a pool larger than the server's own `max_connections` share is a
	// way to take the database down rather than a way to go faster.
	pool.SetMaxOpenConns(8)
	pool.SetMaxIdleConns(4)
	pool.SetConnMaxLifetime(30 * time.Minute)

	if err := pool.PingContext(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pgstore: the database did not answer a ping (the connection string is deliberately not echoed — it carries a password): %w", redact(err, dsn))
	}
	db := &DB{sql: pool}
	if err := db.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return db, nil
}
