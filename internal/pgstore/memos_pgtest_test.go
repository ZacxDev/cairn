//go:build pgtest

// The MEMO store (S1 of `claudedocs/plan-cairn-scope-mail.md`) against a real PostgreSQL — the
// S1 test plan's storage rows: the quota and the live bound under the advisory lock, the cursor
// margin's premise (`transaction_timeout`, armed before the stamp, not leaking), retraction and
// its audit, retention that keeps events, expiry, read-time authority, the scope match, and the
// migration's rollback recipe.
//
// Every time is the store's INJECTED clock, set to year-2000 instants, except where a test
// measures the server's own wall-clock timer (`transaction_timeout`), which no clock can fake.
package pgstore_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/pgstore"
)

var m0 = time.Date(2000, 1, 3, 4, 5, 6, 0, time.UTC)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) set(t time.Time)     { c.mu.Lock(); c.t = t; c.mu.Unlock() }
func (c *fakeClock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// The synthetic world: writer-a OWNS a project holding alpha-notes and beta-notes (so it is an
// admin of both); reader-b reads alpha-notes; admin-d holds read+admin on alpha-notes. Agent
// credentials are writer-a's, scope- AND verb-narrowed (decision 17), one per host label.
const (
	mWriter = control.ID("usr_mwriter")
	mReader = control.ID("usr_mreader")
	mAdminD = control.ID("usr_madmind")
	mProj   = control.ID("prj_mnotes")
	mAlpha  = control.ID("scp_malpha")
	mBeta   = control.ID("scp_mbeta")

	tWriter = "pgtest-memo-writer-credential-00000000000000"
	tAgentA = "pgtest-memo-agent-host-a-credential-000000000"
	tAgentC = "pgtest-memo-agent-host-c-credential-000000000"
	tAgentB = "pgtest-memo-agent-host-b-credential-000000000"
	tReader = "pgtest-memo-reader-credential-0000000000000000"
	tAdminD = "pgtest-memo-admin-d-credential-000000000000000"
)

func memoEvents(extra ...control.Event) []control.Event {
	rw := control.NewVerbSet(control.VerbRead, control.VerbWrite)
	cred := func(id control.ID, user control.ID, tok, label string, scopes []control.ID) control.Event {
		return control.Event{Kind: control.EventCredentialIssued, At: m0, CredentialID: id,
			SubjectKind: control.KindUser, SubjectID: user, TokenHash: control.HashToken(tok),
			Label: label, NarrowedScopes: scopes}
	}
	return append([]control.Event{
		{Kind: control.EventUserCreated, At: m0, UserID: mWriter, Provider: "p", Subject: "m1", DisplayName: "writer-a"},
		{Kind: control.EventUserCreated, At: m0, UserID: mReader, Provider: "p", Subject: "m2", DisplayName: "reader-b"},
		{Kind: control.EventUserCreated, At: m0, UserID: mAdminD, Provider: "p", Subject: "m3", DisplayName: "admin-d"},
		{Kind: control.EventProjectCreated, At: m0, ProjectID: mProj, Name: "notes", UserID: mWriter},
		{Kind: control.EventMemberSet, At: m0, ProjectID: mProj, UserID: mWriter, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: m0, ScopeID: mAlpha, DisplayName: "alpha-notes", ProjectID: mProj},
		{Kind: control.EventScopeCreated, At: m0, ScopeID: mBeta, DisplayName: "beta-notes", ProjectID: mProj},
		{Kind: control.EventGranted, At: m0, GrantID: "grt_mreader", SubjectKind: control.KindUser, SubjectID: mReader,
			ObjectKind: control.ObjectScope, ObjectID: mAlpha, Verbs: control.NewVerbSet(control.VerbRead)},
		{Kind: control.EventGranted, At: m0, GrantID: "grt_madmind", SubjectKind: control.KindUser, SubjectID: mAdminD,
			ObjectKind: control.ObjectScope, ObjectID: mAlpha, Verbs: control.NewVerbSet(control.VerbRead, control.VerbAdmin)},
		cred("crd_mwriter", mWriter, tWriter, "operator laptop", nil),
		cred("crd_magent_a", mWriter, tAgentA, "agents@host-a", []control.ID{mAlpha}),
		{Kind: control.EventCredentialVerbsNarrowed, At: m0, CredentialID: "crd_magent_a", Verbs: rw},
		cred("crd_magent_c", mWriter, tAgentC, "agents@host-c", []control.ID{mAlpha}),
		{Kind: control.EventCredentialVerbsNarrowed, At: m0, CredentialID: "crd_magent_c", Verbs: rw},
		cred("crd_magent_b", mWriter, tAgentB, "agents@host-b", []control.ID{mBeta}),
		{Kind: control.EventCredentialVerbsNarrowed, At: m0, CredentialID: "crd_magent_b", Verbs: rw},
		cred("crd_mreader", mReader, tReader, "reader laptop", nil),
		cred("crd_madmind", mAdminD, tAdminD, "admin laptop", nil),
	}, extra...)
}

func memoWorld(t *testing.T, extra ...control.Event) control.Model {
	t.Helper()
	m, err := control.Replay(memoEvents(extra...))
	if err != nil {
		t.Fatalf("building the memo world: %v", err)
	}
	return m
}

type who struct {
	p control.Principal
	a control.Authorization
}

func as(t *testing.T, m control.Model, tok string) who {
	t.Helper()
	p, a, err := control.Authenticate(m, tok)
	if err != nil {
		t.Fatalf("authenticating: %v", err)
	}
	return who{p, a}
}

type memoRig struct {
	db    *pgstore.DB
	store *pgstore.MemoStore
	clock *fakeClock
	m     control.Model
}

func newMemoRig(t *testing.T) *memoRig {
	t.Helper()
	db := openTestDB(t)
	clock := &fakeClock{t: m0}
	db.Now = clock.now
	scan, err := memo.NewScanner()
	if err != nil {
		t.Fatal(err)
	}
	s, err := pgstore.NewMemoStore(db, scan)
	if err != nil {
		t.Fatal(err)
	}
	return &memoRig{db: db, store: s, clock: clock, m: memoWorld(t)}
}

func (r *memoRig) send(t *testing.T, w who, scope string, ttl time.Duration) (int64, error) {
	t.Helper()
	id, _, err := r.store.Send(context.Background(), w.p, w.a, scope, "subject", "body", ttl)
	return id, err
}

func (r *memoRig) mustSend(t *testing.T, w who, scope string, ttl time.Duration) int64 {
	t.Helper()
	id, err := r.send(t, w, scope, ttl)
	if err != nil {
		t.Fatalf("send to %s: %v", scope, err)
	}
	return id
}

func (r *memoRig) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := r.db.SQL().QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func refusedWith(err error, want memo.RefusalReason) bool {
	var r *memo.Refusal
	return errors.As(err, &r) && r.Reason == want
}

// TestASendIsStoredWithTheStoresClockAndItsSentEvent — the round trip, and the injectable
// clock: `created_at`, `expires_at`, the event's `at` and (below) `retracted_at` are EXACTLY the
// year-2000 instants the test set, never the server's `now()`.
func TestASendIsStoredWithTheStoresClockAndItsSentEvent(t *testing.T) {
	r := newMemoRig(t)
	ctx := context.Background()
	agent := as(t, r.m, tAgentA)
	id, created, err := r.store.Send(ctx, agent.p, agent.a, "alpha-notes", "schema change", "the rename ships\r\nnext", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !created.Equal(m0) {
		t.Fatalf("Send reported created_at %v, want the injected %v", created, m0)
	}
	var gotCreated, gotExpires, evAt time.Time
	var scope, body, cred, display string
	if err := r.db.SQL().QueryRow(`SELECT scope_name, body, created_at, expires_at, sender_credential_id, sender_display_at_send
	                                 FROM memos WHERE id = $1`, id).Scan(&scope, &body, &gotCreated, &gotExpires, &cred, &display); err != nil {
		t.Fatal(err)
	}
	if !gotCreated.Equal(m0) || !gotExpires.Equal(m0.Add(memo.DefaultTTL)) {
		t.Fatalf("stored created_at %v / expires_at %v, want %v / %v", gotCreated, gotExpires, m0, m0.Add(memo.DefaultTTL))
	}
	if scope != "alpha-notes" || body != "the rename ships\nnext" || cred != "crd_magent_a" || display != "writer-a" {
		t.Fatalf("stored scope=%q body=%q cred=%q display=%q", scope, body, cred, display)
	}
	if err := r.db.SQL().QueryRow(`SELECT at FROM memo_events WHERE memo_id = $1 AND seq = 1 AND kind = 'sent'`, id).Scan(&evAt); err != nil || !evAt.Equal(m0) {
		t.Fatalf("the sent event: at=%v err=%v, want %v", evAt, err, m0)
	}
	got, err := r.store.List(ctx, r.m, as(t, r.m, tReader).a, pgstore.MemoQuery{Scope: "alpha-notes"})
	if err != nil || len(got) != 1 {
		t.Fatalf("the reader's list: %d memos, err %v", len(got), err)
	}
	rendered := memo.Render(got[0])
	if rendered.Sender != "writer-a via agents@host-a (user)" || rendered.Subject != "schema change" {
		t.Fatalf("rendered sender %q subject %q", rendered.Sender, rendered.Subject)
	}
}

// TestTheQuotaIsOnePerScopeAcrossSendersAndRolls — O12: 50 sends to one scope in a rolling day,
// split between TWO credentials, pass; the 51st is refused WHICHEVER sends it; a send to another
// scope passes; and a day later the scope sends again.
func TestTheQuotaIsOnePerScopeAcrossSendersAndRolls(t *testing.T) {
	r := newMemoRig(t)
	a, c := as(t, r.m, tAgentA), as(t, r.m, tAgentC)
	for i := 0; i < memo.SendsPerScopePerWindow; i++ {
		w := a
		if i%2 == 1 {
			w = c
		}
		r.mustSend(t, w, "alpha-notes", 0)
		r.clock.add(time.Second)
	}
	for _, w := range []who{a, c} {
		if _, err := r.send(t, w, "alpha-notes", 0); !refusedWith(err, memo.RefusedQuota) {
			t.Fatalf("send %d to alpha-notes: %v, want the quota refusal", memo.SendsPerScopePerWindow+1, err)
		}
	}
	if n := r.count(t, `SELECT count(*) FROM memos WHERE scope_name = 'alpha-notes'`); n != memo.SendsPerScopePerWindow {
		t.Fatalf("%d rows after the refusals, want %d", n, memo.SendsPerScopePerWindow)
	}
	// POSITIVE CONTROL: another scope, same minute.
	r.mustSend(t, as(t, r.m, tAgentB), "beta-notes", 0)
	// …and the window ROLLS: one day after the FIRST send, that send leaves the window.
	r.clock.set(m0.Add(memo.QuotaWindow))
	r.mustSend(t, a, "alpha-notes", 0)
}

// TestTheLiveBoundRefusesThe201stAndPassesAgainAfterOneExpires — decision 2's live-set bound,
// reached across four days so the per-day quota never fires first.
func TestTheLiveBoundRefusesThe201stAndPassesAgainAfterOneExpires(t *testing.T) {
	r := newMemoRig(t)
	w := as(t, r.m, tWriter)
	for day := 0; day < 4; day++ {
		r.clock.set(m0.Add(time.Duration(day) * memo.QuotaWindow))
		ttl := memo.MaxTTL
		if day == 0 {
			ttl = 5 * 24 * time.Hour // the first batch expires on day 5
		}
		for i := 0; i < memo.MaxLivePerScope/4; i++ {
			r.mustSend(t, w, "alpha-notes", ttl)
		}
	}
	r.clock.set(m0.Add(4 * memo.QuotaWindow))
	if _, err := r.send(t, w, "alpha-notes", 0); !refusedWith(err, memo.RefusedLiveLimit) {
		t.Fatalf("the 201st live memo: %v, want the live-limit refusal", err)
	}
	r.clock.set(m0.Add(5*24*time.Hour + time.Second))
	if _, err := r.send(t, w, "alpha-notes", 0); err != nil {
		t.Fatalf("after the first batch expired the send was still refused: %v", err)
	}
}

// TestConcurrentSendsAtQuotaMinusOneCommitExactlyOne — 20 goroutines racing for the last quota
// slot: EXACTLY one commits. RED with the advisory lock removed (each transaction then counts 49
// under READ COMMITTED and all of them insert).
func TestConcurrentSendsAtQuotaMinusOneCommitExactlyOne(t *testing.T) {
	r := newMemoRig(t)
	w := as(t, r.m, tAgentA)
	for i := 0; i < memo.SendsPerScopePerWindow-1; i++ {
		r.mustSend(t, w, "alpha-notes", 0)
	}
	// 🔴 EVERY SEND THAT GETS AS FAR AS INSERTING THEN HOLDS ITS ROW UNCOMMITTED FOR 200 ms. Without
	// this the race window was measured TOO NARROW TO BE A RACE: with the advisory lock REMOVED
	// the test stayed green (1 committed, 19 refused), because the first send finished on the
	// pool's one warm connection while the others were still OPENING theirs. Holding the commit
	// makes the window wider than connection setup, so the lock is what serialises — not the pool.
	defer pgstore.HoldMemoSendBeforeCommitForTest(r.store, func(*sql.Tx) { time.Sleep(200 * time.Millisecond) })()
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, quota, other := 0, 0, []error{}
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := r.store.Send(context.Background(), w.p, w.a, "alpha-notes", "race", "race", 0)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case refusedWith(err, memo.RefusedQuota):
				quota++
			default:
				other = append(other, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if ok != 1 || quota != 19 || len(other) != 0 {
		t.Fatalf("20 racing sends at quota-1: %d committed, %d quota refusals, other errors %v — want exactly 1 and 19", ok, quota, other)
	}
	if n := r.count(t, `SELECT count(*) FROM memos`); n != memo.SendsPerScopePerWindow {
		t.Fatalf("%d rows, want exactly %d", n, memo.SendsPerScopePerWindow)
	}
}

// TestALateCommitterIsReturnedInsideTheCursorMargin — decision 6's premise, rounds 2–3 (2)/(3).
// A send to alpha-notes is stamped T and held open, IDLE, before COMMIT; 25 s later a send to
// beta-notes is stamped T+25 s and commits. A session that saw beta's memo has its high-water at
// T+25 s; alpha's memo — LOWER id and EARLIER created_at, committed SECOND — must still come
// back from a re-ask at high-water − margin. RED with the margin set to 0 (`memo.CursorMargin`
// mutated), and the premise that an id cursor would skip it is asserted (lower id, later commit).
//
// ⚠ TWO SCOPES, NOT ONE, AND THAT IS A MEASURED CONSEQUENCE OF THE SEND'S ORDER: the stamp is
// taken UNDER the scope's advisory lock, so two sends to ONE scope cannot commit out of
// created_at order — the second waits for the first. The margin's work is across scopes (one
// cursor per session) and backward clock steps; see the plan's "S1 — measurements".
func TestALateCommitterIsReturnedInsideTheCursorMargin(t *testing.T) {
	r := newMemoRig(t)
	ctx := context.Background()
	writer := as(t, r.m, tWriter)
	held, release := make(chan struct{}), make(chan struct{})
	restore := pgstore.HoldMemoSendBeforeCommitForTest(r.store, func(*sql.Tx) { close(held); <-release })
	type result struct {
		id  int64
		err error
	}
	done := make(chan result, 1)
	go func() {
		id, _, err := r.store.Send(ctx, writer.p, writer.a, "alpha-notes", "late", "late", 0)
		done <- result{id, err}
	}()
	<-held
	restore() // only the first send is held
	r.clock.add(25 * time.Second)
	betaID, betaAt, err := r.store.Send(ctx, writer.p, writer.a, "beta-notes", "early", "early", 0)
	if err != nil {
		t.Fatal(err)
	}
	highWater := betaAt // the session saw beta's memo
	close(release)
	late := <-done
	if late.err != nil {
		t.Fatal(late.err)
	}
	if late.id >= betaID {
		t.Fatalf("premise: the late committer's id %d is not below the early committer's %d — an id cursor would not be the trap here", late.id, betaID)
	}
	got, err := r.store.List(ctx, r.m, writer.a, pgstore.MemoQuery{Scope: "alpha-notes", After: highWater.Add(-memo.CursorMargin)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || memo.Render(got[0]).ID != late.id {
		t.Fatalf("the late committer (stamped %v, committed after a row stamped %v) was not returned at high-water − %v: %d memos",
			m0, highWater, memo.CursorMargin, len(got))
	}
}

// TestASendHeldOpenPastTheTimeoutIsTerminated — round 3 🟡1 (1): a send held open, IDLE, past
// its `transaction_timeout` is TERMINATED by the server and stores nothing. The timeout is
// shortened to 1 s for the measurement; RED under `statement_timeout`, which an idle open
// transaction never trips.
func TestASendHeldOpenPastTheTimeoutIsTerminated(t *testing.T) {
	defer pgstore.ShortenMemoTransactionTimeoutForTest(time.Second)()
	r := newMemoRig(t)
	defer pgstore.HoldMemoSendBeforeCommitForTest(r.store, func(*sql.Tx) { time.Sleep(2500 * time.Millisecond) })()
	_, err := r.send(t, as(t, r.m, tWriter), "alpha-notes", 0)
	if err == nil {
		t.Fatal("a send held idle past its transaction_timeout COMMITTED — the cursor margin's bound does not hold")
	}
	if n := r.count(t, `SELECT count(*) FROM memos`); n != 0 {
		t.Fatalf("the terminated send left %d row(s)", n)
	}
	t.Logf("the terminated send's error: %v", err)
}

// TestTheTimeoutDoesNotLeakOntoTheNextTransaction — round 3 🟡1 (5): after a memo send on a
// pooled connection, a transaction on the SAME connection runs longer than the send's timeout
// without being terminated. RED under a plain (session-level) SET. The pool is pinned to ONE
// connection so "the same connection" is a fact, not a hope.
func TestTheTimeoutDoesNotLeakOntoTheNextTransaction(t *testing.T) {
	defer pgstore.ShortenMemoTransactionTimeoutForTest(time.Second)()
	r := newMemoRig(t)
	r.db.SQL().SetMaxOpenConns(1)
	r.mustSend(t, as(t, r.m, tWriter), "alpha-notes", 0)
	tx, err := r.db.SQL().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_sleep(1.5)`); err != nil {
		t.Fatalf("a later transaction on the send's connection was terminated — the timeout leaked: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("a later transaction on the send's connection could not commit — the timeout leaked: %v", err)
	}
}

// TestTheSendArmsTheProductionTimeout — the value armed is decision 6's 30 s, read back from
// INSIDE a send's own transaction (the before-commit seam hands over the transaction), and it is
// LOCAL: after the send, the same connection's setting is back to the server default.
func TestTheSendArmsTheProductionTimeout(t *testing.T) {
	r := newMemoRig(t)
	r.db.SQL().SetMaxOpenConns(1)
	var inside string
	defer pgstore.HoldMemoSendBeforeCommitForTest(r.store, func(tx *sql.Tx) {
		if err := tx.QueryRow(`SELECT current_setting('transaction_timeout')`).Scan(&inside); err != nil {
			t.Errorf("reading the setting inside the send: %v", err)
		}
	})()
	r.mustSend(t, as(t, r.m, tWriter), "alpha-notes", 0)
	if inside != "30s" {
		t.Fatalf("inside the send, transaction_timeout = %q, want 30s (decision 6)", inside)
	}
	var after string
	if err := r.db.SQL().QueryRow(`SELECT current_setting('transaction_timeout')`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != "0" {
		t.Fatalf("after a send, the connection's transaction_timeout is %q — the SET was not LOCAL", after)
	}
}

// TestAPoolWaitBeforeBeginCannotAgeTheRow — round 3 🟡1 (4): with every pooled connection held,
// a send waits for one while the clock moves 60 s; its `created_at` is the time AFTER the wait
// (within the timeout of its commit), so it cannot surface "in the past". RED under a mutant that
// reads the clock before BEGIN.
func TestAPoolWaitBeforeBeginCannotAgeTheRow(t *testing.T) {
	r := newMemoRig(t)
	ctx := context.Background()
	r.db.SQL().SetMaxOpenConns(8)
	var held []interface{ Close() error }
	for i := 0; i < 8; i++ {
		c, err := r.db.SQL().Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	type result struct {
		at  time.Time
		err error
	}
	done := make(chan result, 1)
	w := as(t, r.m, tWriter)
	go func() {
		_, at, err := r.store.Send(ctx, w.p, w.a, "alpha-notes", "waited", "waited", 0)
		done <- result{at, err}
	}()
	time.Sleep(300 * time.Millisecond) // the send is now blocked on the pool
	r.clock.add(60 * time.Second)
	commitAt := r.clock.now()
	for _, c := range held {
		c.Close()
	}
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	if commitAt.Sub(got.at) > memo.TransactionTimeout {
		t.Fatalf("a send that waited 60 s for a connection was stamped %v, %v before its commit — the pool wait aged the row",
			got.at, commitAt.Sub(got.at))
	}
}

// TestRetractionIsTheSenderOrAnAdminAndLeavesATombstoneAndTwoEvents — decision 10 (O8): the
// sender may, a scope admin may, a reader may not; after it the subject and body are NULL, the
// events are exactly `sent` then `retracted` each with its actor, the check no longer delivers
// it, and a read shows the tombstone naming the retractor.
func TestRetractionIsTheSenderOrAnAdminAndLeavesATombstoneAndTwoEvents(t *testing.T) {
	r := newMemoRig(t)
	ctx := context.Background()
	agentA, agentC, reader, adminD := as(t, r.m, tAgentA), as(t, r.m, tAgentC), as(t, r.m, tReader), as(t, r.m, tAdminD)
	own := r.mustSend(t, agentA, "alpha-notes", 0)
	moderated := r.mustSend(t, agentA, "alpha-notes", 0)
	byReader := r.mustSend(t, as(t, r.m, tWriter), "alpha-notes", 0)

	// a reader may not retract another's memo, and nothing changes.
	if err := r.store.Retract(ctx, reader.p, reader.a, "alpha-notes", byReader); !refusedWith(err, memo.RefusedNotPermitted) {
		t.Fatalf("a reader's retract: %v, want not-permitted", err)
	}
	r.clock.add(2 * time.Minute)
	// the sender, through ANOTHER credential of the same principal that can read the scope.
	if err := r.store.Retract(ctx, agentC.p, agentC.a, "alpha-notes", own); err != nil {
		t.Fatalf("the sender's retract: %v", err)
	}
	// a scope admin, another principal.
	if err := r.store.Retract(ctx, adminD.p, adminD.a, "alpha-notes", moderated); err != nil {
		t.Fatalf("a scope admin's retract: %v", err)
	}

	var subject, body *string
	var retractedAt time.Time
	if err := r.db.SQL().QueryRow(`SELECT subject, body, retracted_at FROM memos WHERE id = $1`, own).Scan(&subject, &body, &retractedAt); err != nil {
		t.Fatal(err)
	}
	if subject != nil || body != nil || !retractedAt.Equal(m0.Add(2*time.Minute)) {
		t.Fatalf("after retraction: subject=%v body=%v retracted_at=%v (want NULL, NULL, %v)", subject, body, retractedAt, m0.Add(2*time.Minute))
	}
	rows, err := r.db.SQL().Query(`SELECT kind, actor_id, coalesce(actor_credential_id, '') FROM memo_events WHERE memo_id = $1 ORDER BY seq`, own)
	if err != nil {
		t.Fatal(err)
	}
	var evs []string
	for rows.Next() {
		var k, a, c string
		rows.Scan(&k, &a, &c)
		evs = append(evs, k+"/"+a+"/"+c)
	}
	rows.Close()
	if strings.Join(evs, " ") != "sent/usr_mwriter/crd_magent_a retracted/usr_mwriter/crd_magent_c" {
		t.Fatalf("events for the retracted memo: %v", evs)
	}
	live, err := r.store.List(ctx, r.m, reader.a, pgstore.MemoQuery{Scope: "alpha-notes"})
	if err != nil || len(live) != 1 {
		t.Fatalf("the check after two retractions returned %d memos (err %v), want the one live memo", len(live), err)
	}
	st, err := r.store.Get(ctx, r.m, reader.a, "alpha-notes", moderated)
	if err != nil {
		t.Fatal(err)
	}
	rd := memo.Render(st)
	if rd.Tombstone != "retracted by admin-d via admin laptop (user) at 2000-01-03T04:07:06Z" || rd.Subject != "" || rd.Body != "" {
		t.Fatalf("tombstone %q subject %q body %q", rd.Tombstone, rd.Subject, rd.Body)
	}
	all, err := r.store.List(ctx, r.m, reader.a, pgstore.MemoQuery{Scope: "alpha-notes", IncludeRetracted: true})
	if err != nil || len(all) != 3 {
		t.Fatalf("the listing with tombstones returned %d (err %v), want 3", len(all), err)
	}
	// A second retraction of the same memo is the uniform not-found and appends nothing.
	if err := r.store.Retract(ctx, adminD.p, adminD.a, "alpha-notes", own); !errors.Is(err, memo.ErrNotFound) {
		t.Fatalf("retracting twice: %v", err)
	}
	if n := r.count(t, `SELECT count(*) FROM memo_events WHERE memo_id = $1`, own); n != 2 {
		t.Fatalf("%d events after a refused second retraction, want 2", n)
	}
}

// TestAnAgentCredentialOfAnAdminCannotModerate — e2e (g)'s decision-17 control at the store:
// agent-c's principal (writer-a) OWNS the scope, so only the credential's verb narrowing refuses
// it retracting admin-d's memo. RED under `Narrow` ignoring its verb argument.
func TestAnAgentCredentialOfAnAdminCannotModerate(t *testing.T) {
	r := newMemoRig(t)
	ctx := context.Background()
	agentC := as(t, r.m, tAgentC)
	// admin-d holds read+admin on alpha-notes but NOT write, so the memo it "sent" is planted
	// by SQL — what is under test is the retraction, not the send.
	var id int64
	if err := r.db.SQL().QueryRow(`INSERT INTO memos (scope_name, sender_kind, sender_id, sender_display_at_send, subject, body, created_at, expires_at)
	       VALUES ('alpha-notes', 'user', $1, 'admin-d', 's', 'b', $2, $3) RETURNING id`, string(mAdminD), m0, m0.Add(time.Hour)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if !as(t, r.m, tWriter).a.VisibleScopes(control.VerbAdmin).Allows("alpha-notes") {
		t.Fatal("precondition: agent-c's principal is not an admin of alpha-notes")
	}
	if err := r.store.Retract(ctx, agentC.p, agentC.a, "alpha-notes", id); !refusedWith(err, memo.RefusedNotPermitted) {
		t.Fatalf("an agent credential of a scope admin retracted another principal's memo: %v", err)
	}
	// POSITIVE CONTROL: the owner's un-narrowed credential may.
	w := as(t, r.m, tWriter)
	if err := r.store.Retract(ctx, w.p, w.a, "alpha-notes", id); err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: the owner could not moderate: %v", err)
	}
}

// TestPruneDeletesTheRowAndKeepsItsEvents — decision 2's retention, and the append-only property
// tested by BEHAVIOUR: a row 30 days + 1 s past expiry is gone after the next send and its EVENT
// COUNT is unchanged; at 30 days − 1 s it stays. RED with an `ON DELETE CASCADE` foreign key (the
// count drops) and with a plain foreign key (the prune fails, and so does the send).
func TestPruneDeletesTheRowAndKeepsItsEvents(t *testing.T) {
	r := newMemoRig(t)
	w := as(t, r.m, tWriter)
	ttl := time.Hour
	old := r.mustSend(t, w, "alpha-notes", ttl)
	r.store.Retract(context.Background(), w.p, w.a, "alpha-notes", old) // two events, so a cascade is visible
	events := r.count(t, `SELECT count(*) FROM memo_events WHERE memo_id = $1`, old)
	if events != 2 {
		t.Fatalf("precondition: %d events", events)
	}
	expiry := m0.Add(ttl)
	r.clock.set(expiry.Add(memo.RetentionAfterExpiry - time.Second))
	r.mustSend(t, w, "beta-notes", 0)
	if n := r.count(t, `SELECT count(*) FROM memos WHERE id = $1`, old); n != 1 {
		t.Fatalf("at retention − 1 s the row was pruned")
	}
	r.clock.set(expiry.Add(memo.RetentionAfterExpiry + time.Second))
	r.mustSend(t, w, "beta-notes", 0)
	if n := r.count(t, `SELECT count(*) FROM memos WHERE id = $1`, old); n != 0 {
		t.Fatalf("at retention + 1 s the row was NOT pruned")
	}
	if n := r.count(t, `SELECT count(*) FROM memo_events WHERE memo_id = $1`, old); n != events {
		t.Fatalf("the prune changed the memo's event count %d -> %d — the audit must outlive the content", events, n)
	}
}

// TestAMemoIsLiveUntilItsExpiryInstantAndNotAt — the expiry filter, closed AT `expires_at` (the
// session rule's boundary): delivered one second before, not at the instant, not after; and a
// read of the expired id is the uniform not-found.
func TestAMemoIsLiveUntilItsExpiryInstantAndNotAt(t *testing.T) {
	r := newMemoRig(t)
	ctx := context.Background()
	w := as(t, r.m, tWriter)
	id := r.mustSend(t, w, "alpha-notes", time.Hour)
	for _, tc := range []struct {
		at   time.Time
		live bool
	}{{m0.Add(time.Hour - time.Second), true}, {m0.Add(time.Hour), false}, {m0.Add(time.Hour + time.Second), false}} {
		r.clock.set(tc.at)
		got, err := r.store.List(ctx, r.m, w.a, pgstore.MemoQuery{Scope: "alpha-notes"})
		if err != nil {
			t.Fatal(err)
		}
		if (len(got) == 1) != tc.live {
			t.Errorf("at expiry%+v: listed %d, want live=%v", tc.at.Sub(m0.Add(time.Hour)), len(got), tc.live)
		}
		_, err = r.store.Get(ctx, r.m, w.a, "alpha-notes", id)
		if tc.live != (err == nil) || (!tc.live && !errors.Is(err, memo.ErrNotFound)) {
			t.Errorf("at expiry%+v: Get err=%v, want live=%v", tc.at.Sub(m0.Add(time.Hour)), err, tc.live)
		}
	}
}

// TestReadIsAuthorisedAtReadTimeAndMissesAreUniform — decision 12 and T12: after reader-b's
// grant is revoked, the already-sent memo answers the uniform not-found to list and read; an
// unreadable scope, an absent scope, an absent id and an id asked under the WRONG scope (by a
// caller who reads both) are each the SAME error value. POSITIVE CONTROL: the sender still reads.
func TestReadIsAuthorisedAtReadTimeAndMissesAreUniform(t *testing.T) {
	r := newMemoRig(t)
	ctx := context.Background()
	w := as(t, r.m, tWriter)
	id := r.mustSend(t, w, "alpha-notes", 0)
	reader := as(t, r.m, tReader)
	if _, err := r.store.Get(ctx, r.m, reader.a, "alpha-notes", id); err != nil {
		t.Fatalf("precondition: the reader cannot read the memo: %v", err)
	}
	revoked := memoWorld(t, control.Event{Kind: control.EventGrantRevoked, At: m0, GrantID: "grt_mreader"})
	readerNow := as(t, revoked, tReader)
	misses := map[string]error{}
	_, misses["list after the grant was withdrawn"] = r.store.List(ctx, revoked, readerNow.a, pgstore.MemoQuery{Scope: "alpha-notes"})
	_, misses["read after the grant was withdrawn"] = r.store.Get(ctx, revoked, readerNow.a, "alpha-notes", id)
	_, misses["a scope that does not exist"] = r.store.Get(ctx, r.m, w.a, "gamma-notes", id)
	_, misses["an id that does not exist"] = r.store.Get(ctx, r.m, w.a, "alpha-notes", id+1000)
	_, misses["the id under the WRONG scope, by a caller who reads both"] = r.store.Get(ctx, r.m, w.a, "beta-notes", id)
	misses["a retract under the WRONG scope"] = r.store.Retract(ctx, w.p, w.a, "beta-notes", id)
	for what, err := range misses {
		if err != memo.ErrNotFound {
			t.Errorf("%s: %v, want exactly memo.ErrNotFound", what, err)
		}
	}
	if _, err := r.store.Get(ctx, revoked, as(t, revoked, tWriter).a, "alpha-notes", id); err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: the sender cannot read its memo in the revoked world: %v", err)
	}
	if st, err := r.store.Get(ctx, r.m, w.a, "ALPHA-NOTES", id); err != nil || memo.Render(st).ID != id {
		t.Fatalf("the scope name did not fold the way the authority's does: %v", err)
	}
}

// TestSendIsAuthorisedAndScannedBeforeAnythingIsStored — O2 and decision 11 at the store: a
// read-only credential, a credential narrowed AWAY from the scope, and a body carrying a
// vendor-shaped token are each refused with nothing stored; the same principal's credential for
// the right scope is accepted (POSITIVE CONTROL).
func TestSendIsAuthorisedAndScannedBeforeAnythingIsStored(t *testing.T) {
	r := newMemoRig(t)
	ctx := context.Background()
	if _, err := r.send(t, as(t, r.m, tReader), "alpha-notes", 0); !refusedWith(err, memo.RefusedNoWrite) {
		t.Errorf("a read-only send: %v", err)
	}
	if _, err := r.send(t, as(t, r.m, tAgentB), "alpha-notes", 0); !refusedWith(err, memo.RefusedNoWrite) {
		t.Errorf("a send narrowed away from the scope: %v", err)
	}
	a := as(t, r.m, tAgentA)
	token := "gh" + "p_" + strings.Repeat("A1b2", 9)
	_, _, err := r.store.Send(ctx, a.p, a.a, "alpha-notes", "creds", "use "+token+" for the deploy", 0)
	var ref *memo.Refusal
	if !errors.As(err, &ref) || ref.Reason != memo.RefusedSecret || ref.Rule != "github-token" {
		t.Errorf("a body carrying a github token: %v", err)
	}
	if n := r.count(t, `SELECT count(*) FROM memos`); n != 0 {
		t.Fatalf("refused sends stored %d row(s)", n)
	}
	r.mustSend(t, a, "alpha-notes", 0)
}

// TestTwoAgentsOfOnePrincipalAreToldApartByCredential — O9: the two memos store the SAME
// sender_kind/sender_id and DIFFERENT sender_credential_id, and render `via <label>`.
func TestTwoAgentsOfOnePrincipalAreToldApartByCredential(t *testing.T) {
	r := newMemoRig(t)
	a := r.mustSend(t, as(t, r.m, tAgentA), "alpha-notes", 0)
	c := r.mustSend(t, as(t, r.m, tAgentC), "alpha-notes", 0)
	if n := r.count(t, `SELECT count(DISTINCT (sender_kind, sender_id)) FROM memos`); n != 1 {
		t.Fatalf("%d distinct senders, want 1 principal", n)
	}
	if n := r.count(t, `SELECT count(DISTINCT sender_credential_id) FROM memos`); n != 2 {
		t.Fatalf("%d distinct credentials, want 2", n)
	}
	got, _ := r.store.List(context.Background(), r.m, as(t, r.m, tWriter).a, pgstore.MemoQuery{Scope: "alpha-notes"})
	senders := map[int64]string{}
	for _, st := range got {
		rd := memo.Render(st)
		senders[rd.ID] = rd.Sender
	}
	if senders[a] != "writer-a via agents@host-a (user)" || senders[c] != "writer-a via agents@host-c (user)" {
		t.Fatalf("sender lines: %v", senders)
	}
}

// TestMigrationThreeUpgradesAVersionTwoDatabase — the up-path from the deployed schema.
func TestMigrationThreeUpgradesAVersionTwoDatabase(t *testing.T) {
	db := openTestDBWith(t, pgstore.OpenThroughForTest(2))
	var n int
	if err := db.SQL().QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name IN ('memos', 'memo_events')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a version-2 database already has %d memo tables (err %v)", n, err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name IN ('memos', 'memo_events')`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("after migrating, %d memo tables (err %v), want 2", n, err)
	}
}

// TestTheMemoRollbackRecipeLetsAnOlderBuildStartAndReUpgrades — the recipe on
// `refuseFromTheFuture`, measured in three steps: an older build (knows 1 and 2) REFUSES a
// version-3 database; after `DELETE FROM schema_migrations WHERE version = 3` it STARTS; and
// this build re-applies version 3 over the tables left in place, which keep their rows.
func TestTheMemoRollbackRecipeLetsAnOlderBuildStartAndReUpgrades(t *testing.T) {
	r := newMemoRig(t)
	ctx := context.Background()
	r.mustSend(t, as(t, r.m, tWriter), "alpha-notes", 0)
	if err := pgstore.OlderBuildRefusalForTest(ctx, r.db, 2); err == nil {
		t.Fatal("an older build would START against a version-3 database — the refusal the recipe exists for is gone")
	}
	if _, err := r.db.SQL().ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = 3`); err != nil {
		t.Fatal(err)
	}
	if err := pgstore.OlderBuildRefusalForTest(ctx, r.db, 2); err != nil {
		t.Fatalf("after the recipe an older build still refuses: %v", err)
	}
	if err := r.db.Migrate(ctx); err != nil {
		t.Fatalf("re-upgrading over the tables the rollback left: %v", err)
	}
	if n := r.count(t, `SELECT count(*) FROM memos`); n != 1 {
		t.Fatalf("after rollback and re-upgrade, %d memo rows, want the 1 sent before", n)
	}
}

// ---------------------------------------------------------------- S1 round-1 findings, fixed in S2
//
// #235's round-1 audit found two CORRECT guards nobody had watched fail (the retraction's read
// check and the scope fold on send) and four untested details (the quota counting retracted rows,
// the retraction's transaction timeout, `List`'s cap). Each test below was shown RED under the
// mutant its comment names; the matrix is in the S2 PR.

// The admin-ONLY grantee the S1 world lacks: admin on alpha-notes, and no read. The share form
// allows such a grant, which is what made the retraction rule's two places disagree (S2 ruling,
// `memo.MayRetract`).
const (
	mAdminOnly = control.ID("usr_madminonly")
	tAdminOnly = "pgtest-memo-admin-only-credential-000000000000"
)

func adminOnlyEvents() []control.Event {
	return []control.Event{
		{Kind: control.EventUserCreated, At: m0, UserID: mAdminOnly, Provider: "p", Subject: "m4", DisplayName: "admin-only-e"},
		{Kind: control.EventGranted, At: m0, GrantID: "grt_madminonly", SubjectKind: control.KindUser, SubjectID: mAdminOnly,
			ObjectKind: control.ObjectScope, ObjectID: mAlpha, Verbs: control.NewVerbSet(control.VerbAdmin)},
		{Kind: control.EventCredentialIssued, At: m0, CredentialID: "crd_madminonly", SubjectKind: control.KindUser,
			SubjectID: mAdminOnly, TokenHash: control.HashToken(tAdminOnly), Label: "admin-only laptop"},
	}
}

// TestARetractByACallerWhoCannotReadIsTheSameMissForAnyId — T12 on the retraction route: a caller
// who cannot read the scope gets EXACTLY `memo.ErrNotFound` for an id that exists there and for one
// that does not, so retraction cannot be used to learn which ids a scope holds. Two such callers:
// reader-b on beta-notes (no grant at all there) and an ADMIN-ONLY grantee on alpha-notes (admin,
// no read — the S2 ruling). RED with `Retract`'s up-front `MayRead` check deleted: the existing id
// then answers `not-permitted`. POSITIVE CONTROL: the same calls by a caller who CAN read answer
// `not-permitted` for the existing id, so the mutant's observable is reachable.
func TestARetractByACallerWhoCannotReadIsTheSameMissForAnyId(t *testing.T) {
	r := newMemoRig(t)
	r.m = memoWorld(t, adminOnlyEvents()...)
	ctx := context.Background()
	w := as(t, r.m, tWriter)
	inBeta := r.mustSend(t, w, "beta-notes", 0)
	inAlpha := r.mustSend(t, w, "alpha-notes", 0)
	reader, adminOnly := as(t, r.m, tReader), as(t, r.m, tAdminOnly)
	if memo.MayRead(adminOnly.a, "alpha-notes") || !adminOnly.a.VisibleScopes(control.VerbAdmin).Allows("alpha-notes") {
		t.Fatal("precondition: the admin-only grantee is not admin-without-read on alpha-notes")
	}
	for _, tc := range []struct {
		who    string
		c      who
		scope  string
		exists int64
	}{
		{"reader-b on a scope it has no grant on", reader, "beta-notes", inBeta},
		{"an ADMIN-ONLY grantee (no read)", adminOnly, "alpha-notes", inAlpha},
	} {
		existing := r.store.Retract(ctx, tc.c.p, tc.c.a, tc.scope, tc.exists)
		absent := r.store.Retract(ctx, tc.c.p, tc.c.a, tc.scope, tc.exists+100000)
		if existing != memo.ErrNotFound || absent != memo.ErrNotFound {
			t.Errorf("%s: existing id -> %v, absent id -> %v; want exactly memo.ErrNotFound for both", tc.who, existing, absent)
		}
	}
	if n := r.count(t, `SELECT count(*) FROM memos WHERE retracted_at IS NOT NULL`); n != 0 {
		t.Fatalf("a caller who cannot read retracted %d memo(s)", n)
	}
	// POSITIVE CONTROL: reader-b CAN read alpha-notes, and its refusal for the existing id there is
	// the DISTINCT not-permitted — the answer the mutant would leak on the unreadable scope.
	if err := r.store.Retract(ctx, reader.p, reader.a, "alpha-notes", inAlpha); !refusedWith(err, memo.RefusedNotPermitted) {
		t.Fatalf("POSITIVE CONTROL FAILED: a reader of alpha-notes retracting another's memo: %v, want not-permitted", err)
	}
}

// TestAScopeNamedInAnotherCaseSharesItsQuota — T3: `ALPHA-NOTES` and `alpha-notes` are ONE scope at
// the predicate (`ScopeSet.Allows` folds), so they must be one scope at the store: one quota, one
// stored name. RED with `Send`'s `store.NormalizeRef(scope)` replaced by the raw name — each spelling
// then gets its own 50 and the 51st send passes.
func TestAScopeNamedInAnotherCaseSharesItsQuota(t *testing.T) {
	r := newMemoRig(t)
	w := as(t, r.m, tWriter)
	for i := 0; i < memo.SendsPerScopePerWindow; i++ {
		scope := "alpha-notes"
		if i%2 == 1 {
			scope = "ALPHA-NOTES"
		}
		r.mustSend(t, w, scope, 0)
	}
	for _, scope := range []string{"ALPHA-NOTES", "alpha-notes", "Alpha-Notes"} {
		if _, err := r.send(t, w, scope, 0); !refusedWith(err, memo.RefusedQuota) {
			t.Errorf("send %d to %q: %v, want the quota refusal — the spelling bought a second quota",
				memo.SendsPerScopePerWindow+1, scope, err)
		}
	}
	if n := r.count(t, `SELECT count(*) FROM memos WHERE scope_name <> 'alpha-notes'`); n != 0 {
		t.Fatalf("%d row(s) stored under an unfolded scope name", n)
	}
}

// TestTheQuotaCountsRetractedSends — O12 counts SENDS: a memo retracted after it was sent still used
// its slot, so retracting cannot be used to send a 51st memo in a day. RED with the quota count
// restricted to unretracted rows (`AND retracted_at IS NULL`). POSITIVE CONTROL: the live bound,
// which DOES exclude retracted rows, is not what refuses here (only 50 rows exist, 25 of them live).
func TestTheQuotaCountsRetractedSends(t *testing.T) {
	r := newMemoRig(t)
	ctx := context.Background()
	w := as(t, r.m, tWriter)
	for i := 0; i < memo.SendsPerScopePerWindow; i++ {
		id := r.mustSend(t, w, "alpha-notes", 0)
		if i%2 == 0 {
			if err := r.store.Retract(ctx, w.p, w.a, "alpha-notes", id); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := r.send(t, w, "alpha-notes", 0); !refusedWith(err, memo.RefusedQuota) {
		t.Fatalf("send 51 after 25 retractions: %v, want the quota refusal (retraction must not refund a send)", err)
	}
}

// TestTheRetractionIsBoundedByTheTransactionTimeout — the retraction arms decision 6's
// `transaction_timeout` the way the send does: a retraction BLOCKED on the memo's row lock (another
// transaction holds it `FOR UPDATE`) is terminated by the server once the (shortened) timeout passes,
// instead of waiting for the blocker. RED with the retraction's `set_config` removed: it then waits
// out the blocker and succeeds. POSITIVE CONTROL: after the blocker is gone the same retraction
// commits, so the timeout — not the lock — is what failed the first one.
func TestTheRetractionIsBoundedByTheTransactionTimeout(t *testing.T) {
	defer pgstore.ShortenMemoTransactionTimeoutForTest(time.Second)()
	r := newMemoRig(t)
	ctx := context.Background()
	w := as(t, r.m, tWriter)
	id := r.mustSend(t, w, "alpha-notes", 0)
	blocker, err := r.db.SQL().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(`SELECT id FROM memos WHERE id = $1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- r.store.Retract(ctx, w.p, w.a, "alpha-notes", id) }()
	var got error
	select {
	case got = <-done:
	case <-time.After(4 * time.Second):
		_ = blocker.Rollback()
		got = <-done
		t.Fatalf("a retraction blocked on a row lock was still waiting %v later (it then returned %v) — "+
			"no transaction_timeout bounds it", time.Since(start).Round(time.Millisecond), got)
	}
	_ = blocker.Rollback()
	if got == nil {
		t.Fatal("a retraction blocked past its transaction_timeout COMMITTED")
	}
	t.Logf("the terminated retraction's error after %v: %v", time.Since(start).Round(time.Millisecond), got)
	if err := r.store.Retract(ctx, w.p, w.a, "alpha-notes", id); err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: with the blocker gone the retraction still fails: %v", err)
	}
}

// TestAListingIsCappedAtTheLiveBound — `List`'s limit is capped at `memo.MaxLivePerScope` whatever the
// caller asks: a listing WITH tombstones can hold more rows than the live bound (≤ 50 a day for up to
// 30 days), so the cap is the only thing bounding one response. 250 rows are planted by SQL (the send
// path's own bounds would stop at 200 live). RED with the `limit > memo.MaxLivePerScope` clause
// removed (a 1000-row ask then returns 250). POSITIVE CONTROLS: a small limit is honoured exactly, and
// limit 0 means the cap rather than nothing.
func TestAListingIsCappedAtTheLiveBound(t *testing.T) {
	r := newMemoRig(t)
	ctx := context.Background()
	const planted = memo.MaxLivePerScope + 50
	if _, err := r.db.SQL().Exec(`INSERT INTO memos (scope_name, sender_kind, sender_id, sender_display_at_send, subject, body, created_at, expires_at)
	     SELECT 'alpha-notes', 'user', $1, 'writer-a', 's', 'b', $2::timestamptz + make_interval(secs => g), $3
	       FROM generate_series(1, $4) AS g`, string(mWriter), m0, m0.Add(memo.MaxTTL), planted); err != nil {
		t.Fatal(err)
	}
	reader := as(t, r.m, tReader)
	for _, tc := range []struct {
		limit, want int
	}{{1000, memo.MaxLivePerScope}, {0, memo.MaxLivePerScope}, {-1, memo.MaxLivePerScope}, {5, 5}} {
		got, err := r.store.List(ctx, r.m, reader.a, pgstore.MemoQuery{Scope: "alpha-notes", Limit: tc.limit, IncludeRetracted: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != tc.want {
			t.Errorf("Limit %d over %d rows returned %d, want %d", tc.limit, planted, len(got), tc.want)
		}
	}
}

// TestAllIsEveryUnexpiredMemoOfOneScopeInOneCall — `All`, the browser tab's listing (S5 follow-up, D2
// of #242's round 0: the tab used to page `List` with a second spelling of the client's boundary rule).
// 253 alpha-notes rows are planted by SQL — more than one `List` page (200), in PAIRS sharing one
// instant, the shape a strictly-after page boundary loses — plus one beta-notes row and one EXPIRED
// alpha-notes row; one alpha-notes memo is then retracted through the store.
//
// `All` returns exactly the 253: oldest first with the id breaking each shared instant, the tombstone
// INCLUDED as a tombstone, the expired row and the other scope's row absent. CONTROLS: `List` over the
// same rows stops at its 200-row page, so the 253 is not a page; a caller who cannot read the scope
// gets the uniform `ErrNotFound`.
//
// RED with `All` selecting only LIVE rows (252, no tombstone), and with its scope clause removed.
func TestAllIsEveryUnexpiredMemoOfOneScopeInOneCall(t *testing.T) {
	r := newMemoRig(t)
	ctx := context.Background()
	const planted = memo.MaxLivePerScope + 53
	plant := func(scope string, n int, created, expires time.Time) {
		t.Helper()
		if _, err := r.db.SQL().Exec(`INSERT INTO memos (scope_name, sender_kind, sender_id, sender_display_at_send, subject, body, created_at, expires_at)
		     SELECT $1, 'user', $2, 'writer-a', 's', 'b', $3::timestamptz + make_interval(secs => g / 2), $4
		       FROM generate_series(1, $5) AS g`, scope, string(mWriter), created, expires, n); err != nil {
			t.Fatal(err)
		}
	}
	plant("alpha-notes", planted, m0, m0.Add(memo.MaxTTL))
	plant("beta-notes", 1, m0, m0.Add(memo.MaxTTL))
	plant("alpha-notes", 1, m0.Add(-2*time.Hour), m0.Add(-time.Hour)) // expired before the rig's clock
	var retracted int64
	if err := r.db.SQL().QueryRow(`SELECT id FROM memos WHERE scope_name = 'alpha-notes' AND expires_at > $1 ORDER BY created_at, id OFFSET 7 LIMIT 1`, m0).Scan(&retracted); err != nil {
		t.Fatal(err)
	}
	w := as(t, r.m, tWriter)
	if err := r.store.Retract(ctx, w.p, w.a, "alpha-notes", retracted); err != nil {
		t.Fatalf("retracting memo %d: %v", retracted, err)
	}

	reader := as(t, r.m, tReader)
	got, err := r.store.All(ctx, r.m, reader.a, "alpha-notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != planted {
		t.Fatalf("All returned %d memo(s), want the %d unexpired alpha-notes rows (tombstone included, expired and beta-notes rows not)", len(got), planted)
	}
	tombstones, prevAt, prevID := 0, "", int64(0)
	for i, st := range got {
		rd := memo.Render(st)
		if rd.Scope != "alpha-notes" {
			t.Fatalf("row %d is scope %q", i, rd.Scope)
		}
		if rd.CreatedAt < prevAt || (rd.CreatedAt == prevAt && rd.ID <= prevID) {
			t.Fatalf("row %d (%s, id %d) is not after row %d (%s, id %d): not oldest first with the id breaking a tie",
				i, rd.CreatedAt, rd.ID, i-1, prevAt, prevID)
		}
		prevAt, prevID = rd.CreatedAt, rd.ID
		if rd.Tombstone != "" {
			tombstones++
			if rd.ID != retracted || rd.Subject != "" || rd.Body != "" {
				t.Errorf("tombstone on memo %d (subject %q body %q), want only on %d with no text", rd.ID, rd.Subject, rd.Body, retracted)
			}
		}
	}
	if tombstones != 1 {
		t.Errorf("All returned %d tombstone(s), want the 1 retracted memo", tombstones)
	}
	// CONTROL: the wire's listing over the same rows is ONE PAGE, so the count above is not one.
	page, err := r.store.List(ctx, r.m, reader.a, pgstore.MemoQuery{Scope: "alpha-notes", IncludeRetracted: true})
	if err != nil || len(page) != memo.MaxLivePerScope {
		t.Fatalf("CONTROL FAILED: List returned %d (err %v), want its %d-row page", len(page), err, memo.MaxLivePerScope)
	}
	// The read predicate is asked: reader-b holds nothing on beta-notes.
	if _, err := r.store.All(ctx, r.m, reader.a, "beta-notes"); !errors.Is(err, memo.ErrNotFound) {
		t.Errorf("All for a scope the caller cannot read: %v, want the uniform not-found", err)
	}
	if beta, err := r.store.All(ctx, r.m, w.a, "beta-notes"); err != nil || len(beta) != 1 {
		t.Errorf("All for beta-notes as its owner returned %d (err %v), want its 1 row", len(beta), err)
	}
}

// TestAllRefusesAScopeHoldingMoreThanItsSendRulesAllow — `All` has no page, so it has a BOUND:
// `MaxUnexpiredPerScope`, what the quota and the TTL ceiling let one scope hold. Measured at the bound
// (returned whole) and one row past it (an error, never a listing cut to the bound). The bound itself
// is pinned to the two send rules it is derived from, as a literal: 50 a day for 30 days.
func TestAllRefusesAScopeHoldingMoreThanItsSendRulesAllow(t *testing.T) {
	if pgstore.MaxUnexpiredPerScope != 1500 || memo.SendsPerScopePerWindow != 50 || memo.MaxTTL != 30*memo.QuotaWindow {
		t.Fatalf("MaxUnexpiredPerScope is %d with a quota of %d per %s and a %s TTL ceiling — re-derive the bound",
			pgstore.MaxUnexpiredPerScope, memo.SendsPerScopePerWindow, memo.QuotaWindow, memo.MaxTTL)
	}
	r := newMemoRig(t)
	ctx := context.Background()
	plant := func(n int) {
		t.Helper()
		if _, err := r.db.SQL().Exec(`INSERT INTO memos (scope_name, sender_kind, sender_id, sender_display_at_send, subject, body, created_at, expires_at)
		     SELECT 'alpha-notes', 'user', $1, 'writer-a', 's', 'b', $2::timestamptz + make_interval(secs => g), $3
		       FROM generate_series(1, $4) AS g`, string(mWriter), m0, m0.Add(memo.MaxTTL), n); err != nil {
			t.Fatal(err)
		}
	}
	reader := as(t, r.m, tReader)
	plant(pgstore.MaxUnexpiredPerScope)
	got, err := r.store.All(ctx, r.m, reader.a, "alpha-notes")
	if err != nil || len(got) != pgstore.MaxUnexpiredPerScope {
		t.Fatalf("at the bound All returned %d (err %v), want all %d", len(got), err, pgstore.MaxUnexpiredPerScope)
	}
	plant(1)
	got, err = r.store.All(ctx, r.m, reader.a, "alpha-notes")
	if err == nil || !strings.Contains(err.Error(), "refusing to return a truncated listing") {
		t.Fatalf("one row past the bound All returned %d row(s) and %v, want its own refusal", len(got), err)
	}
}
