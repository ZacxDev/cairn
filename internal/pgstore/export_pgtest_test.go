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
