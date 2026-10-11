package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/memo"
	// Aliased: this package already has a `redact` — the DSN redactor in pgstore.go, which
	// must keep its name and its behaviour (a URL-form DSN leaks its password through
	// `*url.Error` without it).
	secretscan "github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/store"
)

// MemoStore is scope mail's storage (S1 of `claudedocs/plan-cairn-scope-mail.md`, decisions 2,
// 6, 9, 10, 11 and 12): migration 3's `memos` and `memo_events`, behind the authority
// predicates of `internal/memo`.
//
// 🔴 EVERY METHOD TAKES THE CALLER'S AUTHORITY AND ASKS THE PREDICATE ITSELF. `Send` asks
// [memo.MaySend], every read asks [memo.MayRead] NOW (decision 12), and `Retract` asks
// [memo.MayRetract]. A store that trusted its caller to have asked would make every surface
// that reaches it — the client listener (S2), the browser form (S5) — a second place to forget
// it; asking here makes the predicate unskippable by construction.
//
// 🔴 STORED TEXT LEAVES ONLY AS `memo.Stored` (decision 19, Guard C). No exported result or
// exported field in this package carries a memo column in any other type —
// `TestGuardCNoExportedResultOrFieldCarriesMemoText` is the ledger. Parameters are not
// constrained: `Send` takes the subject and body to store.
//
// ⚠ INERT UNLESS `cairn-ui -client-api-addr` IS SET: S2's client listener (`internal/clientapi`) is
// its one request-reachable caller; the verbs (S3) and the browser tab (S5) are later slices.
type MemoStore struct {
	db   *DB
	scan *secretscan.Redactor

	// beforeCommit is a TEST SEAM, called inside `Send` after every write and right before
	// `COMMIT` — the transaction is then open, IDLE, with its row stamped and invisible. Nil in
	// production; set only by `export_pgtest_test.go`, which is how the tier holds a send open
	// to measure what the cursor margin rests on (decision 6). `refreshInterval`'s discipline:
	// one assignment, in a test.
	beforeCommit func(*sql.Tx)
}

// NewMemoStore wraps an open database and the send scan's redactor ([memo.NewScanner]).
//
// 🔴 A NIL SCANNER IS REFUSED HERE, AT CONSTRUCTION, as well as by [memo.CheckSend] on every
// send: a store wired without its scan would refuse every send at request time, and the
// construction refusal is what makes that a startup failure instead.
func NewMemoStore(db *DB, scan *secretscan.Redactor) (*MemoStore, error) {
	if db == nil || scan == nil {
		return nil, errors.New("pgstore: a memo store needs a database and a secret scanner")
	}
	return &MemoStore{db: db, scan: scan}, nil
}

// memoTransactionTimeout is what `Send` arms as its transaction's first statement. It is
// [memo.TransactionTimeout]; a variable only so the pgtest tier can SHORTEN it (to measure the
// timeout firing without a 30 s sleep), never lengthen it. Production never assigns it.
var memoTransactionTimeout = memo.TransactionTimeout

// memoLockNamespace is the first key of the two-key `pg_advisory_xact_lock` a send takes per
// scope. The two-key form is a different lock space from the migration runner's one-key
// `advisoryLockKey`, so the two can never serialise against each other; the namespace keeps a
// future two-key user from colliding with this one.
const memoLockNamespace int32 = 0x6d656d6f // "memo"

// memoLive is THE liveness predicate for a memo row, in SQL, spelled ONCE: unretracted and not
// yet expired. `$1` is the store's clock.
//
// ⚠ THE PACKAGE DOC'S RULE ("THE EXPIRY PREDICATE IS NOT WRITTEN IN SQL") IS ABOUT SESSIONS,
// whose liveness has a Go spelling (`Session.Live`) a SQL one could disagree with. A memo has no
// Go liveness predicate to disagree with; this constant is the one spelling, used by the
// live-set bound and by delivery, and it agrees with the session rule on the boundary: CLOSED at
// `expires_at` (`expires_at > now` is false at the instant), which
// `TestAMemoIsLiveUntilItsExpiryInstantAndNotAt` pins.
const memoLive = `retracted_at IS NULL AND expires_at > $1`

// Send stores one memo for the caller, or refuses it.
//
// The order is decision 6's, and it is what makes the client's cursor margin a bound:
//
//  1. the content rules ([memo.CheckSend]) and the send predicate ([memo.MaySend]), before any
//     connection is taken;
//  2. `BEGIN`; `SET LOCAL transaction_timeout` as the FIRST statement — so the transaction
//     cannot outlive [memo.TransactionTimeout] from here on;
//  3. the per-scope advisory lock (decision 9: the counts are exact across replicas);
//  4. THEN the clock is read for `created_at` — after the timeout is armed and after the lock is
//     held. Time spent waiting for a pooled connection or for `BEGIN` is BEFORE the stamp and
//     cannot age the row; and because the stamp is taken under the scope's lock, rows of ONE
//     scope commit in `created_at` order (see the plan's S1 corrections);
//  5. the quota (≤ [memo.SendsPerScopePerWindow] in [memo.QuotaWindow], all senders) and the
//     live bound (≤ [memo.MaxLivePerScope]);
//  6. the row, its `sent` event, and the retention prune — all in this one transaction.
//
// ⚠ A FIRED `transaction_timeout` TERMINATES THE CONNECTION (measured by the plan's round-3
// auditor on 17.11, and by `TestASendHeldOpenPastTheTimeoutIsTerminated` here); the send then
// fails with a connection error and nothing is stored.
func (s *MemoStore) Send(ctx context.Context, caller control.Principal, auth control.Authorization,
	scope, subject, body string, ttl time.Duration) (int64, time.Time, error) {
	if !memo.MaySend(auth, scope) {
		return 0, time.Time{}, &memo.Refusal{Reason: memo.RefusedNoWrite}
	}
	out, err := memo.CheckSend(subject, body, ttl, s.scan)
	if err != nil {
		return 0, time.Time{}, err
	}
	key := store.NormalizeRef(scope)

	tx, err := s.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("pgstore: beginning a memo send: %w", err)
	}
	defer tx.Rollback()
	// `set_config(…, true)` IS `SET LOCAL`, parameterised: transaction-scoped, so it can never
	// carry over onto a session or team-link transaction that later borrows this pooled
	// connection (`TestTheTimeoutDoesNotLeakOntoTheNextTransaction`).
	if _, err := tx.ExecContext(ctx, `SELECT set_config('transaction_timeout', $1, true)`,
		fmt.Sprintf("%dms", memoTransactionTimeout.Milliseconds())); err != nil {
		return 0, time.Time{}, fmt.Errorf("pgstore: arming the memo send's transaction_timeout: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`, memoLockNamespace, key); err != nil {
		return 0, time.Time{}, fmt.Errorf("pgstore: taking the scope's send lock: %w", err)
	}
	now := s.db.now()

	var sent, live int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM memos WHERE scope_name = $1 AND created_at > $2`,
		key, now.Add(-memo.QuotaWindow)).Scan(&sent); err != nil {
		return 0, time.Time{}, fmt.Errorf("pgstore: counting the scope's sends: %w", err)
	}
	if sent >= memo.SendsPerScopePerWindow {
		return 0, time.Time{}, &memo.Refusal{Reason: memo.RefusedQuota}
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM memos WHERE `+memoLive+` AND scope_name = $2`, now, key).Scan(&live); err != nil {
		return 0, time.Time{}, fmt.Errorf("pgstore: counting the scope's live memos: %w", err)
	}
	if live >= memo.MaxLivePerScope {
		return 0, time.Time{}, &memo.Refusal{Reason: memo.RefusedLiveLimit}
	}

	var id int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO memos (scope_name, sender_kind, sender_id, sender_credential_id, sender_display_at_send,
		                    subject, body, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		key, string(caller.Kind), string(caller.ID), nullable(string(caller.CredentialID)), caller.Display,
		out.Subject, out.Body, now, now.Add(out.TTL)).Scan(&id); err != nil {
		return 0, time.Time{}, fmt.Errorf("pgstore: storing a memo: %w", err)
	}
	if err := appendMemoEvent(ctx, tx, id, "sent", caller, now); err != nil {
		return 0, time.Time{}, err
	}
	// Retention (decision 2): a row is deleted RetentionAfterExpiry after it expires — STRICTLY
	// past it, so a row exactly on the boundary survives — and its EVENTS are kept, which is the
	// append-only property `TestPruneDeletesTheRowAndKeepsItsEvents` measures by behaviour.
	if _, err := tx.ExecContext(ctx, `DELETE FROM memos WHERE expires_at < $1`,
		now.Add(-memo.RetentionAfterExpiry)); err != nil {
		return 0, time.Time{}, fmt.Errorf("pgstore: pruning expired memos: %w", err)
	}
	if s.beforeCommit != nil {
		s.beforeCommit(tx)
	}
	if err := tx.Commit(); err != nil {
		return 0, time.Time{}, fmt.Errorf("pgstore: committing a memo send: %w", err)
	}
	return id, now, nil
}

// MemoQuery is one listing of a scope.
type MemoQuery struct {
	Scope string
	// After returns only memos whose `created_at` is STRICTLY after it. The client passes its
	// high-water mark minus [memo.CursorMargin] (decision 6); zero means from the start.
	After time.Time
	// Limit caps the rows; ≤ 0 or above [memo.MaxLivePerScope] means [memo.MaxLivePerScope],
	// which is every LIVE memo a scope can hold.
	Limit int
	// IncludeRetracted also returns unexpired RETRACTED memos, as tombstones — what `memo-read`'s
	// listing shows (decision 10). The check route leaves it false: a retracted memo is never
	// delivered to a session that has not seen it.
	IncludeRetracted bool
}

// List returns a scope's memos, oldest first (`created_at`, then id), resolving every sender
// and retractor through `m` at READ time (decision 5).
//
// 🔴 READ IS AUTHORISED NOW, against the caller's authority as it is at this request (decision
// 12): a withdrawn grant stops delivery on the next call. A scope the caller cannot read and a
// scope that does not exist are the SAME answer, [memo.ErrNotFound] (T12).
func (s *MemoStore) List(ctx context.Context, m control.Model, auth control.Authorization, q MemoQuery) ([]memo.Stored, error) {
	if !memo.MayRead(auth, q.Scope) {
		return nil, memo.ErrNotFound
	}
	limit := q.Limit
	if limit <= 0 || limit > memo.MaxLivePerScope {
		limit = memo.MaxLivePerScope
	}
	where := memoLive
	if q.IncludeRetracted {
		where = `expires_at > $1`
	}
	ctx, cancel := withStatementTimeout(ctx)
	defer cancel()
	rows, err := s.db.sql.QueryContext(ctx, memoSelect+` WHERE `+where+
		` AND m.scope_name = $2 AND m.created_at > $3 ORDER BY m.created_at, m.id LIMIT $4`,
		s.db.now(), store.NormalizeRef(q.Scope), q.After.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("pgstore: listing memos: %w", err)
	}
	defer rows.Close()
	var out []memo.Stored
	for rows.Next() {
		st, err := scanMemo(rows, m)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgstore: listing memos: %w", err)
	}
	return out, nil
}

// Get returns ONE memo — a retracted one as its tombstone — if the caller may read `scope` NOW
// and the memo is unexpired and belongs to THAT scope.
//
// 🔴 THE SCOPE MATCH IS PART OF THE ANSWER (decision 8, round 1 🔴3): an id is unique only
// within one instance's database, so the caller names the scope it routed by, and a memo of
// another scope is the SAME [memo.ErrNotFound] as an absent id — the pair cannot be used to
// probe which scope an id belongs to.
func (s *MemoStore) Get(ctx context.Context, m control.Model, auth control.Authorization, scope string, id int64) (memo.Stored, error) {
	if !memo.MayRead(auth, scope) {
		return memo.Stored{}, memo.ErrNotFound
	}
	ctx, cancel := withStatementTimeout(ctx)
	defer cancel()
	rows, err := s.db.sql.QueryContext(ctx, memoSelect+` WHERE expires_at > $1 AND m.scope_name = $2 AND m.id = $3`,
		s.db.now(), store.NormalizeRef(scope), id)
	if err != nil {
		return memo.Stored{}, fmt.Errorf("pgstore: reading a memo: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return memo.Stored{}, fmt.Errorf("pgstore: reading a memo: %w", err)
		}
		return memo.Stored{}, memo.ErrNotFound
	}
	return scanMemo(rows, m)
}

// Retract withdraws one memo (decision 10, O8): it sets `retracted_at`, NULLs the subject and the
// body, and appends a `retracted` event with the retracting actor and credential — one
// transaction, and never an UPDATE of an event.
//
// A memo the caller cannot read, an absent id, an id of another scope, an expired memo and one
// already retracted are all [memo.ErrNotFound]; a memo the caller CAN read but may not retract
// (not the sender, not a scope admin) is a [*memo.Refusal] with [memo.RefusedNotPermitted].
//
// 🔴 WHO MAY RETRACT IS [memo.MayRetract] ALONE; THE READ CHECK BELOW IS A DIFFERENT RULE. It is
// decision 4's uniform miss (T12), asked BEFORE the row is looked up: without it a caller who
// cannot read the scope would get `not-permitted` for an id that EXISTS and `not-found` for one
// that does not — an existence oracle over another scope's ids. `MayRetract` also refuses that
// caller (it carries the read clause since S2), so the two agree; the order is what keeps the
// answer uniform. `TestARetractByACallerWhoCannotReadIsTheSameMissForAnyId` pins it.
func (s *MemoStore) Retract(ctx context.Context, caller control.Principal, auth control.Authorization, scope string, id int64) error {
	if !memo.MayRead(auth, scope) {
		return memo.ErrNotFound
	}
	tx, err := s.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pgstore: beginning a retraction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('transaction_timeout', $1, true)`,
		fmt.Sprintf("%dms", memoTransactionTimeout.Milliseconds())); err != nil {
		return fmt.Errorf("pgstore: arming the retraction's transaction_timeout: %w", err)
	}
	now := s.db.now()
	var senderKind, senderID string
	err = tx.QueryRowContext(ctx,
		`SELECT sender_kind, sender_id FROM memos
		  WHERE `+memoLive+` AND scope_name = $2 AND id = $3 FOR UPDATE`,
		now, store.NormalizeRef(scope), id).Scan(&senderKind, &senderID)
	if errors.Is(err, sql.ErrNoRows) {
		return memo.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("pgstore: reading a memo to retract: %w", err)
	}
	if !memo.MayRetract(caller, auth, scope, senderKind, senderID) {
		return &memo.Refusal{Reason: memo.RefusedNotPermitted}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE memos SET retracted_at = $1, subject = NULL, body = NULL WHERE id = $2`, now, id); err != nil {
		return fmt.Errorf("pgstore: retracting a memo: %w", err)
	}
	if err := appendMemoEvent(ctx, tx, id, "retracted", caller, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("pgstore: committing a retraction: %w", err)
	}
	return nil
}

// appendMemoEvent appends the next audit row for a memo. The sequence is per memo and is read
// inside the caller's transaction, which holds the memo row (the send's insert, the
// retraction's `FOR UPDATE`), so two writers cannot take one seq.
func appendMemoEvent(ctx context.Context, tx *sql.Tx, id int64, kind string, actor control.Principal, at time.Time) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memo_events (memo_id, seq, kind, actor_kind, actor_id, actor_credential_id, actor_display, at)
		 VALUES ($1, (SELECT coalesce(max(seq), 0) + 1 FROM memo_events WHERE memo_id = $1), $2, $3, $4, $5, $6, $7)`,
		id, kind, string(actor.Kind), string(actor.ID), nullable(string(actor.CredentialID)), actor.Display, at); err != nil {
		return fmt.Errorf("pgstore: appending a memo event: %w", err)
	}
	return nil
}

// memoSelect reads a memo row with its retraction event (if any). `$1` is the store's clock in
// every caller, which is what lets `memoLive` be spliced in.
const memoSelect = `SELECT m.id, m.scope_name, m.sender_kind, m.sender_id, coalesce(m.sender_credential_id, ''),
       m.subject, m.body, m.created_at, m.expires_at, m.retracted_at,
       coalesce(e.actor_kind, ''), coalesce(e.actor_id, ''), coalesce(e.actor_credential_id, '')
  FROM memos m
  LEFT JOIN memo_events e ON e.memo_id = m.id AND e.kind = 'retracted'`

// scanMemo builds the `memo.Stored` for one row, resolving the sender's and retractor's display
// and the credential labels through the authority model AT READ TIME (decision 5): nothing in
// the row's text can set them, and a principal that no longer resolves renders as such.
func scanMemo(rows *sql.Rows, m control.Model) (memo.Stored, error) {
	var (
		f                             memo.Fields
		senderCred, rKind, rID, rCred string
		subject, body                 sql.NullString
		retracted                     sql.NullTime
	)
	if err := rows.Scan(&f.ID, &f.Scope, &f.SenderKind, &f.SenderID, &senderCred, &subject, &body,
		&f.CreatedAt, &f.ExpiresAt, &retracted, &rKind, &rID, &rCred); err != nil {
		return memo.Stored{}, fmt.Errorf("pgstore: reading a memo row: %w", err)
	}
	f.Subject, f.Body = subject.String, body.String
	f.SenderDisplay, f.CredentialLabel = resolveActor(m, f.SenderKind, f.SenderID, senderCred)
	if retracted.Valid {
		f.RetractedAt = retracted.Time
		f.RetractorKind, f.RetractorID = rKind, rID
		f.RetractorDisplay, f.RetractorLabel = resolveActor(m, rKind, rID, rCred)
	}
	return memo.NewStored(f), nil
}

// resolveActor answers the display and the credential label for an actor, from the model. "" for
// a principal the model no longer holds, and "" for a credential it does not (a browser session
// sends with none).
func resolveActor(m control.Model, kind, id, credential string) (display, label string) {
	if p, ok := m.PrincipalFor(control.Kind(kind), control.ID(id)); ok {
		display = p.Display
	}
	if c, ok := m.Credentials[control.ID(credential)]; ok && credential != "" {
		label = c.Label
	}
	return display, label
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// withStatementTimeout bounds a read by [StatementTimeout] UNDER the caller's context, so a hung
// server cannot hold a request forever and a cancelled request still cancels the query.
func withStatementTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, StatementTimeout)
}
