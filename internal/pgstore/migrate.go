package pgstore

import (
	"context"
	"database/sql"
	"fmt"
)

// Migration is one ordered, irreversible schema step.
//
// 🔴 A LIST RATHER THAN `CREATE TABLE IF NOT EXISTS` EVERYWHERE, AND THE DIFFERENCE IS
// THE SECOND CHANGE RATHER THAN THE FIRST. Idempotent `IF NOT EXISTS` statements are
// indistinguishable from a version ledger while there is exactly one of them; they stop
// being equivalent the moment a column has to be ADDED, because "if not exists" has no
// reading for a table that exists in the wrong shape. Starting with the ledger costs a
// table now and is the only way the second migration is a routine edit.
//
// ⚠ NO `Down`. A rollback that drops a table holding live sessions signs everybody out
// and a table holding invites voids every outstanding one; a rollback that does nothing
// is the honest answer, and a `Down` field nobody could safely call would read as a
// capability this package has.
type Migration struct {
	// Version is the ledger key, strictly increasing and never reused.
	Version int
	// Name is for the operator reading `schema_migrations`, never for control flow.
	Name string
	// Statements run in ONE transaction with the ledger insert, so a partially applied
	// migration cannot be recorded as applied.
	Statements []string
}

// migrations is the whole ordered schema history.
//
// 🔴 APPEND ONLY, AND NEVER EDIT AN APPLIED ROW. Editing version 1 changes what a fresh
// database gets while every existing one keeps what version 1 used to say, which is two
// schemas answering to one version number — the divergence is silent and shows up as a
// query failing on one deployment and not another.
var migrations = []Migration{
	{
		Version: 1,
		Name:    "sessions-and-invites",
		Statements: []string{
			// 🔴 `digest` IS THE PRIMARY KEY AND THE ID IS NOWHERE IN THIS TABLE. See the
			// package doc: a session table holding ids is a set of bearer credentials.
			//
			// ⚠ `kind` AND `principal` ARE A REFERENCE, NOT A SNAPSHOT OF AUTHORITY —
			// `identity.Session`'s own comment records why, and it is the reason there is
			// no verbs column here: authority is re-resolved through `control.Resolve` on
			// every request, so a grant revoked while a session is open takes effect on
			// the next request. A column caching it would be a second, stale answer.
			`CREATE TABLE IF NOT EXISTS sessions (
				digest     TEXT        PRIMARY KEY,
				kind       TEXT        NOT NULL,
				principal  TEXT        NOT NULL,
				issued_at  TIMESTAMPTZ NOT NULL,
				expires_at TIMESTAMPTZ NOT NULL
			)`,
			// The prune path deletes by expiry; without this it is a sequential scan of
			// every live session on every write.
			`CREATE INDEX IF NOT EXISTS sessions_expires_at_idx ON sessions (expires_at)`,

			// 🔴 AN INVITE IS A BEARER CAPABILITY, SO THE SAME RULE: the digest is the
			// key and the token is not stored. Anyone who can read this table can see
			// that an invite EXISTS and what it confers, and cannot redeem it.
			//
			// 🔴 `redeemed_at` IS NULLABLE AND THE ROW IS NEVER DELETED ON REDEMPTION.
			// A redeemed invite must stay readable: it is the only record on this side
			// of the system saying which invite produced which membership, and the
			// `member-set` record the redemption writes to the control journal carries
			// the inviter as its actor but not the invite's id. Deleting the row would
			// also make a replayed link indistinguishable from an unknown one, and
			// those are different answers to a person who clicked twice.
			`CREATE TABLE IF NOT EXISTS invites (
				digest      TEXT        PRIMARY KEY,
				project_id  TEXT        NOT NULL,
				role        TEXT        NOT NULL,
				inviter     TEXT        NOT NULL,
				created_at  TIMESTAMPTZ NOT NULL,
				expires_at  TIMESTAMPTZ NOT NULL,
				revoked_at  TIMESTAMPTZ,
				redeemed_at TIMESTAMPTZ,
				redeemed_by TEXT
			)`,
			// The mint page lists a project's outstanding invites.
			`CREATE INDEX IF NOT EXISTS invites_project_id_idx ON invites (project_id)`,
		},
	},
	{
		// 🔴 THE TEAM LINK: A SIBLING OF `invites`, NOT AN `ALTER` OF IT. See
		// `internal/invite/teamlink.go` for why widening the one-project, single-use table
		// would change what every guard over it means. Version 1's statements are not
		// touched — the APPEND ONLY rule above.
		Version: 2,
		Name:    "team-links",
		Statements: []string{
			// 🔴 DIGEST-KEYED, TOKEN-FREE: `invites`' rule. `redemptions` IS THE COUNT THE
			// ATOMIC SINGLE-USE GUARD READS, and the CHECK constraint is that guard stated a
			// second time BY THE DATABASE: a single-use row can never record a second
			// redemption even if the conditional `UPDATE` in `RedeemLink` were edited wrong.
			`CREATE TABLE IF NOT EXISTS team_links (
				digest      TEXT        PRIMARY KEY,
				role        TEXT        NOT NULL,
				inviter     TEXT        NOT NULL,
				reusable    BOOLEAN     NOT NULL,
				created_at  TIMESTAMPTZ NOT NULL,
				expires_at  TIMESTAMPTZ NOT NULL,
				revoked_at  TIMESTAMPTZ,
				redemptions INTEGER     NOT NULL DEFAULT 0,
				CONSTRAINT team_links_redemptions_nonnegative CHECK (redemptions >= 0),
				CONSTRAINT team_links_single_use CHECK (reusable OR redemptions <= 1)
			)`,
			// The Team page lists the caller's own links.
			`CREATE INDEX IF NOT EXISTS team_links_inviter_idx ON team_links (inviter)`,
			// One row per target. The kind is CHECKed so a target the consumer cannot
			// interpret can never be stored — it would be a target a redemption skips.
			`CREATE TABLE IF NOT EXISTS team_link_targets (
				digest    TEXT NOT NULL REFERENCES team_links (digest),
				kind      TEXT NOT NULL CHECK (kind IN ('project', 'scope')),
				target_id TEXT NOT NULL,
				PRIMARY KEY (digest, kind, target_id)
			)`,
			// 🔴 THE PER-REDEMPTION AUDIT RECORD. One row per redemption, keyed by the
			// count it produced, so a reusable link's every join — and every principal it
			// CREATED — is a row an operator can read.
			`CREATE TABLE IF NOT EXISTS team_link_redemptions (
				digest      TEXT        NOT NULL REFERENCES team_links (digest),
				seq         INTEGER     NOT NULL,
				redeemed_by TEXT        NOT NULL,
				redeemed_at TIMESTAMPTZ NOT NULL,
				provisioned BOOLEAN     NOT NULL,
				-- 🔴 FALSE until the authority write the row stands for succeeded;
				-- see invite.LinkRedemption.Confirmed. (Version 2 had never been applied
				-- outside a test when this column was added, so it is edited in place
				-- rather than appended as version 3.)
				confirmed   BOOLEAN     NOT NULL DEFAULT FALSE,
				PRIMARY KEY (digest, seq)
			)`,
		},
	},
}

// advisoryLockKey is the `pg_advisory_lock` key the migration runner serialises on.
//
// 🔴 TWO REPLICAS STARTING TOGETHER IS THE ORDINARY CASE, NOT AN EXOTIC ONE — it is
// what a rolling deploy DOES — and two concurrent `CREATE TABLE IF NOT EXISTS` runs
// race on the ledger insert, not on the DDL. Without the lock the loser's transaction
// aborts on the primary key and the pod exits at startup, which reads as "the database
// is broken" on exactly the deploy that introduced a second replica.
//
// ⚠ THE VALUE IS ARBITRARY AND ONLY HAS TO BE STABLE. It is namespaced by nothing —
// advisory locks are per-database — so a second application sharing this database and
// picking the same number would serialise against us. That is a reason to give cairn
// its own database, which the deployment does.
const advisoryLockKey int64 = 0x6361_69726e_3031 // "cairn01" as hex-ish digits

// Migrate applies every migration this build knows that the database has not recorded.
//
// 🔴 IT APPLIES RATHER THAN VERIFIES, AND THE ALTERNATIVE WAS WEIGHED. A build that
// only CHECKED the schema would need something else to apply it — a job, a manual step —
// and the failure mode of that shape is a pod that will not start until a human
// remembers. Applying at startup means the schema is a property of the BINARY, which is
// the same reasoning that makes the version the git revision everywhere else here.
//
// 🔴 IT REFUSES A DATABASE FROM THE FUTURE. A `schema_migrations` row this build does
// not know was written by a newer build; carrying on would run a program against a
// schema it has never seen, and the failure would be a query error somewhere later
// rather than at startup. This is the same ruling `control.Event.validate`'s `default:`
// arm takes for a journal record from a newer build, and for the same reason: the
// damage from proceeding is unbounded and unlocated.
func (d *DB) Migrate(ctx context.Context) error {
	return d.migrateThrough(ctx, migrations[len(migrations)-1].Version)
}

// migrateThrough is [DB.Migrate] stopped after `through`. It exists for the UP-PATH test,
// which has to build a database at an OLD version and then migrate it, because a schema
// that only ever meets fresh databases has never been measured against the deployments
// that already exist. Production calls [DB.Migrate], which is this at the newest version.
func (d *DB) migrateThrough(ctx context.Context, through int) error {
	// 🔴 THE LOCK IS TAKEN **BEFORE** THE LEDGER IS CREATED, AND THE OTHER ORDER WAS A
	// MEASURED RACE. `CREATE TABLE IF NOT EXISTS` is NOT atomic against a concurrent
	// creator in PostgreSQL: both sessions find the table absent, both insert into the
	// catalog, and the loser gets `23505 duplicate key value violates unique constraint
	// "pg_type_typname_nsp_index"`. Measured on 18.6 before this change — 8 concurrent
	// starts against each of 5 fresh databases, **11 of 40 refused**, varying 0/8 to 6/8 by
	// round — with `pgstore: creating the migration ledger:` as the refusal. That is exactly
	// the outcome `advisoryLockKey`'s own comment says the lock exists to prevent ("the pod
	// exits at startup, which reads as 'the database is broken'"), happening one statement
	// ABOVE the lock.
	//
	// ⚠ THE LOCK NEEDS NO TABLE, which is what makes this order available at all:
	// `pg_advisory_lock` is session state in the server, not a row, so it can be taken
	// against a database with no schema whatsoever.
	//
	// 🔴 AND IT IS `ExecContext`, NOT `Exec`. The bare `Exec` took no context, so
	// `cmd/cairn-ui`'s `dbConnectTimeout` did not bound it — a server that accepted the
	// ping and then stopped answering hung startup forever, on the one statement the
	// timeout's own comment claims to cover ("⚠ IT COVERS THE MIGRATION TOO"). Every
	// statement in this function takes `ctx` now, so that sentence is true.
	conn, err := d.sql.Conn(ctx)
	if err != nil {
		return fmt.Errorf("pgstore: taking a connection for the migration lock: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		return fmt.Errorf("pgstore: taking the migration lock: %w", err)
	}
	defer conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, advisoryLockKey)

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version     INTEGER     PRIMARY KEY,
		name        TEXT        NOT NULL,
		applied_at  TIMESTAMPTZ NOT NULL
	)`); err != nil {
		return fmt.Errorf("pgstore: creating the migration ledger: %w", err)
	}

	applied, err := d.appliedVersions(ctx, conn)
	if err != nil {
		return err
	}

	if err := refuseFromTheFuture(applied, knownVersions()); err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.Version] || m.Version > through {
			continue
		}
		if err := d.applyOne(ctx, conn, m); err != nil {
			return err
		}
	}
	return nil
}

// refuseFromTheFuture is the startup refusal of a database migrated by a NEWER build, as ONE
// predicate over (applied, known) — so the rollback recipe's test can ask what an OLDER build
// (one that knows fewer versions) would decide, through the same code.
//
// # 🔴 THE ROLLBACK RECIPE, BECAUSE MIGRATION 2 MAKES THIS REFUSAL REACHABLE ON A ROLLBACK
//
// A build that knows only version 1, started against a database this build migrated to 2,
// REFUSES TO START — and on `cairn-ui` that takes sign-in down. Rolling back across version 2
// therefore needs one statement, run against the database BEFORE the old image starts:
//
//	DELETE FROM schema_migrations WHERE version = 2;
//
// The old build then finds only versions it knows and starts; it never reads the three
// `team_*` tables, so they can stay (their rows — links and their redemption log — survive).
// Re-upgrading is safe: every version-2 statement is `IF NOT EXISTS`, so the newer build
// re-applies version 2 over the tables that are still there and records it again.
// `TestTheRollbackRecipeLetsAnOlderBuildStartAndReUpgrades` measures all three steps.
//
// ⚠ A team link minted before the rollback is NOT redeemable while the old build runs: it
// does not know the tables exist, so the join path answers "not redeemable" for its token.
func refuseFromTheFuture(applied map[int]bool, known []int) error {
	isKnown := map[int]bool{}
	for _, v := range known {
		isKnown[v] = true
	}
	for v := range applied {
		if !isKnown[v] {
			return fmt.Errorf(
				"pgstore: this database has schema version %d applied and this build knows only %v — "+
					"it was migrated by a NEWER build. Refusing to start rather than run against a schema "+
					"this binary has never seen: the failures that produces are query errors at request time, "+
					"in places nobody is looking, instead of one refusal here",
				v, known)
		}
	}
	return nil
}

func knownVersions() []int {
	out := make([]int, 0, len(migrations))
	for _, m := range migrations {
		out = append(out, m.Version)
	}
	return out
}

func (d *DB) appliedVersions(ctx context.Context, conn *sql.Conn) (map[int]bool, error) {
	rows, err := conn.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("pgstore: reading the migration ledger: %w", err)
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("pgstore: reading the migration ledger: %w", err)
		}
		out[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgstore: reading the migration ledger: %w", err)
	}
	return out, nil
}

// applyOne runs one migration's statements AND its ledger insert in a single
// transaction, so "applied" and "recorded as applied" cannot come apart.
func (d *DB) applyOne(ctx context.Context, conn *sql.Conn, m Migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pgstore: migration %d (%s): %w", m.Version, m.Name, err)
	}
	defer tx.Rollback()

	for i, stmt := range m.Statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("pgstore: migration %d (%s) statement %d: %w", m.Version, m.Name, i+1, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES ($1, $2, $3)`,
		m.Version, m.Name, d.now()); err != nil {
		return fmt.Errorf("pgstore: migration %d (%s) ledger insert: %w", m.Version, m.Name, err)
	}
	return tx.Commit()
}
