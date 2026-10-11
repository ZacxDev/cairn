//go:build pgtest

// This file and its siblings are the POSTGRES TIER. They are behind a build tag, and
// the tag is the whole design rather than a convenience.
//
// # 🔴 WHY A BUILD TAG AND NOT A `t.Skip`
//
// A test that skips itself when it finds no database is worse than no test: the skip
// is printed once, nobody counts it, and `go test ./...` stays green forever while the
// only coverage the SQL has ever had is absent. This repository has already measured
// that shape twice — `tests/test_go_client_ledgers.py`'s header records that the `go`
// job REFUSES on a skip "because a skip nobody counts is a pass", and
// `internal/depspolicy`'s nested-module ledger refuses rather than skipping when it
// reads a tree with zero nested modules, because a comparison against an absent
// operand reports SAME rather than MISSING.
//
// So there is no skip anywhere in this tier. Instead:
//
//   - WITHOUT the tag these files do not exist, so `go test ./...` — which is what the
//     nix derivations run in `doCheck`, in a sandbox that structurally cannot have a
//     database — compiles and passes without ever pretending to have measured SQL;
//   - WITH the tag and no DSN, [requireDSN] calls `t.Fatal`. Not a skip. The tier
//     refuses to vouch, in the same words and for the same reason `tests/parity/`'s
//     pre-flight does: "could not vouch" is a distinct outcome from "passed", and a
//     tier that cannot tell them apart is reporting the wrong one.
//
// ⚠ A BUILD TAG MOVES THE HAZARD RATHER THAN CLOSING IT, AND THAT IS SAID HERE RATHER
// THAN DISCOVERED LATER. A tier nobody RUNS is as green as a tier that skips. What
// closes that half is outside this file: `tests/pgtest/run.sh` is the runner, and
// `tests/test_pgtest_tier_is_declared.py` is the ledger asserting these files exist,
// carry this tag, and contain the tests they are named for — so deleting the tier is a
// RED test in the ordinary suite rather than a silent hole.
//
// # 🔴 EVERY TEST GETS ITS OWN SCHEMA, BECAUSE THE MIGRATION LEDGER IS SHARED STATE
//
// `DB.Migrate` writes `schema_migrations` and takes a per-DATABASE advisory lock. Two
// tests sharing one database therefore share a ledger and a lock, and the migration
// tests in particular assert what the ledger holds. [openTestDB] gives each test a
// fresh Postgres SCHEMA and points its pool's `search_path` at it, so the tables, the
// ledger and the indexes are all per-test and the teardown is one `DROP SCHEMA`.
package pgstore_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/pgstore"
)

// DSNEnv is the one environment variable this tier reads.
//
// ⚠ IT IS NOT `CAIRN_*`. Every `CAIRN_*` name is part of the product's configuration
// surface and carries an alias ledger (`internal/envalias`), a deprecation warning and
// a parity test diffing two clients' stderr. This is a test-harness input that no
// shipped binary reads, so giving it the product prefix would put a name in that
// namespace which the ledger does not know and which nothing resolves.
const DSNEnv = "CAIRN_PGTEST_DSN"

// refusal is what this tier says when it cannot measure anything.
//
// 🔴 THE WORDING IS DELIBERATE AND MATCHES `tests/parity/harness.py`. "Could not vouch"
// is a third outcome beside pass and fail, and a reader who sees "FAILED" here would
// reasonably go looking for a defect in the SQL. There is none; there is no database.
const refusal = `REFUSING TO VOUCH: the Postgres tier ran with no database.

This tier does not skip. A skipped test is indistinguishable from a passing one in any
summary anybody actually reads, and the SQL in this package would then have no coverage
while ` + "`go test ./...`" + ` stayed green.

Set ` + DSNEnv + ` to a libpq connection string for a THROWAWAY database, or run the
tier through its own runner, which starts one and tears it down:

    tests/pgtest/run.sh

The runner is what CI invokes. Do not point this at a database you care about: every
test creates a schema and drops it, and a failing run may leave one behind.`

// requireDSN returns the connection string or REFUSES.
//
// 🔴 `t.Fatal`, NEVER `t.Skip`. See this file's header.
func requireDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(DSNEnv))
	if dsn == "" {
		t.Fatal(refusal)
	}
	return dsn
}

// schemaCounter makes each test's schema name unique within a run.
//
// ⚠ A COUNTER RATHER THAN A RANDOM VALUE, so a failing run leaves a name a human can
// read and correlate with the test that made it. Uniqueness across CONCURRENT runs
// comes from the runner giving each run its own database, not from this name.
var schemaCounter = make(chan int, 1)

func init() { schemaCounter <- 0 }

func nextSchema(t *testing.T) string {
	n := <-schemaCounter
	n++
	schemaCounter <- n
	// The test name is sanitised into the schema name so a leftover schema names its
	// own cause. Postgres identifiers fold to lower case unless quoted; lower-casing
	// here means the name in an error message is the name in `\dn`.
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, t.Name())
	if len(safe) > 40 {
		safe = safe[:40]
	}
	return fmt.Sprintf("t%d_%s", n, safe)
}

// openTestDB gives one test a migrated database in a schema of its own.
//
// 🔴 IT GOES THROUGH `pgstore.Open`, NOT `OpenWith`, BECAUSE `Open` IS WHAT THE POD
// CALLS. `Open` pings, bounds the pool and applies the schema; a harness that used
// `OpenWith` would leave the ping and the pool bounds — two of the three things `Open`
// exists to do — unexercised by every test in this tier.
func openTestDB(t *testing.T) *pgstore.DB {
	t.Helper()
	return openTestDBWith(t, pgstore.Open)
}

// openTestDBWith is [openTestDB] with the opener chosen by the caller. The one other
// opener is `pgstore.OpenThroughForTest`, which the UP-PATH test uses to build a database
// at an OLD schema version before migrating it.
func openTestDBWith(t *testing.T, open func(context.Context, string) (*pgstore.DB, error)) *pgstore.DB {
	t.Helper()
	dsn := requireDSN(t)
	schema := nextSchema(t)

	// The admin connection creates the schema. It is separate from the pool under test
	// because that pool's `search_path` already names a schema that does not exist yet.
	admin, err := sql.Open(pgstore.DriverName, dsn)
	if err != nil {
		t.Fatalf("could not vouch: opening an admin pool against %s failed: %v", DSNEnv, err)
	}
	defer admin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		t.Fatalf("could not vouch: the database named by %s did not answer a ping: %v", DSNEnv, err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatalf("could not vouch: creating the test schema %q failed: %v", schema, err)
	}
	t.Cleanup(func() {
		drop, err := sql.Open(pgstore.DriverName, dsn)
		if err != nil {
			t.Logf("leaked schema %q: reopening to drop it failed: %v", schema, err)
			return
		}
		defer drop.Close()
		if _, err := drop.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); err != nil {
			t.Logf("leaked schema %q: %v", schema, err)
		}
	})

	db, err := open(ctx, withSearchPath(dsn, schema))
	if err != nil {
		t.Fatalf("opening against schema %q failed: %v", schema, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// withSearchPath points a libpq DSN at one schema.
//
// 🔴 IT IS SET IN THE DSN RATHER THAN BY A `SET search_path` STATEMENT, AND THE
// DIFFERENCE IS THE POOL. `database/sql` hands out any of up to `MaxOpenConns`
// connections per statement, so a `SET` executed once lands on ONE connection and the
// next query may run on another — the tables would be created in the test's schema and
// then not found by a query a moment later. A `options=-c search_path=…` parameter is
// applied by libpq to every connection the pool opens, including ones opened later.
//
// ⚠ `public` IS DELIBERATELY NOT IN THE PATH. If it were, a missing table in the test
// schema would silently resolve to a leftover one in `public` and the test would pass
// against the wrong rows.
//
// 🔴 THE OPTION IS SPELLED `-csearch_path=…` WITH NO SPACE, AND THE SPACE IS NOT A STYLE
// QUESTION — IT IS A BUG THIS FUNCTION ALREADY HAD. libpq splits the `options` value on
// whitespace and passes each piece as a separate argv entry to the backend, so
// `-c search_path=x` arrives as a bare `-c` and the server refuses the connection with
// `invalid command-line argument for server process: -c (42601)`. Watched happening: the
// first run of this tier failed all six tests on exactly that, at the ping in
// `pgstore.Open`. Attaching the value keeps it one token.
func withSearchPath(dsn, schema string) string {
	// A URL-shaped DSN takes query parameters; a keyword/value one takes space-separated
	// pairs. Both spellings are valid libpq and the runner uses the second, so both are
	// handled rather than assumed.
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		return dsn + sep + "options=-csearch_path%3D" + schema
	}
	return dsn + " options=-csearch_path=" + schema
}

// wantServerMajor is the Postgres major this tier is a claim about.
//
// 🔴 PINNED, NOT INHERITED — THE SAME DISCIPLINE AS THE INTERPRETER AND THE GO
// TOOLCHAIN, AND FOR THE SAME REASON. The tier's whole value is that it measures real SQL
// against a real server, so WHICH server is part of every result it reports. Three things
// choose it today — `flake.nix`'s `postgresql_18`, the service container in
// `.github/workflows/ci.yml`, and the StatefulSet in the deployment repo — and only the
// first two are readable from here. A test is the only place the third can be caught
// disagreeing, by a human reading a failure that names the number.
//
// ⚠ A nixpkgs bump that moved the major would redden this. That is the HONEST failure —
// the tier really would then be measuring a server nothing else in this repo runs — and
// it is the identical trade `pkgs.python312` and `buildGo125Module` already make.
const wantServerMajor = 18

// TestTheServerIsTheMajorThisTierPinsIsAboutTheSERVERNotTheImageTag reads the version out
// of the running server.
//
// 🔴 OUT OF THE SERVER, NEVER OFF THE IMAGE TAG OR THE PACKAGE NAME. A tag is a claim
// about a manifest; `SHOW server_version` is the artefact answering for itself. This
// repository has already shipped a healthy pod serving an image 13 commits stale, and the
// lesson it drew is exactly this one: name which artefact your reading is about.
func TestTheServerIsTheMajorThisTierPinsIsAboutTheSERVERNotTheImageTag(t *testing.T) {
	db := openTestDB(t)
	var version, full string
	if err := db.SQL().QueryRow(`SHOW server_version`).Scan(&version); err != nil {
		t.Fatalf("could not vouch: reading `server_version` failed: %v", err)
	}
	if err := db.SQL().QueryRow(`SELECT version()`).Scan(&full); err != nil {
		t.Fatalf("could not vouch: reading `version()` failed: %v", err)
	}
	t.Logf("server_version=%s", version)
	t.Logf("version()=%s", full)

	major := version
	if i := strings.IndexAny(version, ".-"); i >= 0 {
		major = version[:i]
	}
	if major != fmt.Sprint(wantServerMajor) {
		t.Fatalf(`this tier ran against PostgreSQL major %s, and it pins %d.

  server_version : %s
  version()      : %s

Every result in this tier is a claim about the server it ran against. Move the pin in
internal/pgstore/harness_pgtest_test.go, flake.nix and the CI service container
TOGETHER, or not at all — and check the deployment's StatefulSet, which no test here
can read.`, major, wantServerMajor, version, full)
	}
}
