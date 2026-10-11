//go:build pgtest

// This file exports ONE test-only opener into the package, for the up-path test.
//
// ⚠ IT IS PACKAGE `pgstore`, NOT `pgstore_test`, BECAUSE THAT IS THE ONLY WAY TO REACH
// `migrateThrough` WITHOUT EXPORTING IT FROM THE SHIPPED PACKAGE. A `_test.go` file is
// compiled only into the test binary, and the build tag keeps it out of even that unless
// the Postgres tier is running — so production has no way to open a database at an old
// schema version, which is the property `DB`'s own comment requires.
package pgstore

import (
	"context"
	"database/sql"
	"time"
)

// OpenThroughForTest is [Open] that stops migrating after version `through`.
func OpenThroughForTest(through int) func(context.Context, string) (*DB, error) {
	return func(ctx context.Context, dsn string) (*DB, error) {
		pool, err := sql.Open(DriverName, dsn)
		if err != nil {
			return nil, err
		}
		if err := pool.PingContext(ctx); err != nil {
			pool.Close()
			return nil, err
		}
		db := &DB{sql: pool}
		if err := db.migrateThrough(ctx, through); err != nil {
			pool.Close()
			return nil, err
		}
		return db, nil
	}
}

// OlderBuildRefusalForTest is the startup decision an OLDER build — one that knows versions
// up to `knownThrough` and nothing newer — would take against this database, through the
// same predicate (`refuseFromTheFuture`) every build runs. nil means it would start.
func OlderBuildRefusalForTest(ctx context.Context, db *DB, knownThrough int) error {
	conn, err := db.sql.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	applied, err := db.appliedVersions(ctx, conn)
	if err != nil {
		return err
	}
	var known []int
	for _, m := range migrations {
		if m.Version <= knownThrough {
			known = append(known, m.Version)
		}
	}
	return refuseFromTheFuture(applied, known)
}

// HoldMemoSendBeforeCommitForTest installs `hook` as the memo store's before-COMMIT seam — the
// send's row is then written, stamped and invisible, and its transaction IDLE — and returns the
// function that removes it. It is how the tier measures what decision 6's cursor margin rests on.
func HoldMemoSendBeforeCommitForTest(s *MemoStore, hook func(*sql.Tx)) func() {
	s.beforeCommit = hook
	return func() { s.beforeCommit = nil }
}

// ShortenMemoTransactionTimeoutForTest lowers the send's `transaction_timeout` so the tier can
// watch it FIRE without a 30 s sleep, and returns the restore. Shortening only: the production
// value is `memo.TransactionTimeout`, which `TestTheSendArmsTheProductionTimeout` reads back.
func ShortenMemoTransactionTimeoutForTest(d time.Duration) func() {
	prev := memoTransactionTimeout
	memoTransactionTimeout = d
	return func() { memoTransactionTimeout = prev }
}
