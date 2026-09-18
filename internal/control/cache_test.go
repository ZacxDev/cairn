package control

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// The doubles. One is the AUTHORITY WITH A POWER SWITCH, which is what "test it
// by killing the dependency" needs and what no real FileStore can be made to be:
// once a FileStore has loaded, its own last-known-good keeps answering, so the
// failure the cache must survive cannot be produced from below it.
// ---------------------------------------------------------------------------

// errAuthorityDown stands in for "Postgres is stopped" — the outage the plan says
// to measure rather than reason about.
var errAuthorityDown = errors.New("test: the authority is down")

// switchedStore is a real Store with a switch on it, and a call counter.
//
// 🔴 THE COUNTER IS HALF THE INSTRUMENT AND THE SWITCH IS THE OTHER HALF. A test
// that asserts "the hot path made no call" reads a zero, and a zero is
// indistinguishable from a counter wired to nothing — so every test that asserts the
// zero also watches the same counter move on a refresh, and reports the pair.
type switchedStore struct {
	inner Store

	mu     sync.Mutex
	reads  int
	writes int
	down   bool
	// hold, when non-nil, blocks Model until it is closed — a HUNG authority rather
	// than a failing one. The two are different outages and only one of them can
	// deadlock a reader.
	hold    chan struct{}
	entered chan struct{}
}

func (s *switchedStore) Model(ctx context.Context) (Model, error) {
	s.mu.Lock()
	s.reads++
	down, hold, entered := s.down, s.hold, s.entered
	s.mu.Unlock()
	if entered != nil {
		close(entered)
	}
	if hold != nil {
		<-hold
	}
	if down {
		return Model{}, errAuthorityDown
	}
	return s.inner.Model(ctx)
}

func (s *switchedStore) Append(ctx context.Context, events ...Event) (Model, error) {
	s.mu.Lock()
	s.writes++
	down := s.down
	s.mu.Unlock()
	if down {
		return Model{}, errAuthorityDown
	}
	return s.inner.Append(ctx, events...)
}

func (s *switchedStore) unplug()        { s.mu.Lock(); s.down = true; s.mu.Unlock() }
func (s *switchedStore) readCount() int { s.mu.Lock(); defer s.mu.Unlock(); return s.reads }

// gatedSource answers a DIFFERENT Model per call and holds each call INSIDE `Model`
// until the test releases it by hand.
//
// 🔴 IT EXISTS BECAUSE THE DEFECT IT MEASURES IS A LOGICAL RACE, NOT A DATA RACE, SO
// `-race` IS STRUCTURALLY BLIND TO IT. Two refreshes that read the authority in one
// order and commit in the other are perfectly synchronised and perfectly wrong; nothing
// about the memory model is violated. `switchedStore.hold` blocks ONE call for the hung
// authority test, which cannot express "call one waits while call two runs to
// completion" — this can, and the interleaving it forces is deterministic rather than
// hopeful, because call N+1 cannot start until call N is already parked inside `Model`.
type gatedSource struct {
	mu      sync.Mutex
	next    int
	answers []Model
	entered []chan struct{}
	release []chan struct{}
}

func newGatedSource(answers ...Model) *gatedSource {
	g := &gatedSource{answers: answers}
	for range answers {
		g.entered = append(g.entered, make(chan struct{}))
		g.release = append(g.release, make(chan struct{}))
	}
	return g
}

func (g *gatedSource) Model(context.Context) (Model, error) {
	g.mu.Lock()
	i := g.next
	g.next++
	g.mu.Unlock()
	close(g.entered[i])
	<-g.release[i]
	return g.answers[i], nil
}

// gatedWriter is a `gatedSource` whose WRITE half can be parked too.
//
// 🔴 PARKING `Append` IS WHAT MAKES THE GENERATION'S POSITION OBSERVABLE. `ApplyNow`
// takes its stamp AFTER `Append` returns rather than before, and the only interleaving
// that can tell those two apart is a refresh that starts while the write is in flight:
// stamped before, the refresh gets the HIGHER generation and undoes the write; stamped
// after, the write does. A double that could only park `Model` would score that choice
// EQUIVALENT.
type gatedWriter struct {
	*gatedSource
	appended       Model
	inAppend       chan struct{}
	releaseAppend  chan struct{}
	appendAttempts int
}

func newGatedWriter(appended Model, answers ...Model) *gatedWriter {
	return &gatedWriter{
		gatedSource:   newGatedSource(answers...),
		appended:      appended,
		inAppend:      make(chan struct{}),
		releaseAppend: make(chan struct{}),
	}
}

func (g *gatedWriter) Append(context.Context, ...Event) (Model, error) {
	g.mu.Lock()
	g.appendAttempts++
	g.mu.Unlock()
	close(g.inAppend)
	<-g.releaseAppend
	return g.appended, nil
}

// readOnlySource has no Append at all — the compile-time shape a plain `Source`
// backend has, and the one `ErrAuthorityReadOnly` exists for.
type readOnlySource struct{ m Model }

func (r readOnlySource) Model(context.Context) (Model, error) { return r.m, nil }

// fixedSource answers one hand-built Model, so a render test's expected string can
// be read off the fixture rather than computed from the code under test.
type fixedSource struct{ m Model }

func (f fixedSource) Model(context.Context) (Model, error) { return f.m, nil }

// cacheWorld is the fixture world PLUS its credentials, as events, so it can be
// journalled into a real FileStore. Every name in it is synthetic — see world_test.go.
func cacheWorld() []Event { return append(worldEvents(), credentialEvents()...) }

// liveCache builds a cache over a real FileStore seeded with the fixture world, with
// a pinned clock the test moves by hand.
func liveCache(t *testing.T, maxAge time.Duration) (*Cache, *switchedStore, *time.Time) {
	t.Helper()
	fs, err := OpenFileStore(filepath.Join(t.TempDir(), "control", "journal.jsonl"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	fs.Now = func() time.Time { return at(100) }
	if _, err := fs.Append(context.Background(), cacheWorld()...); err != nil {
		t.Fatalf("seeding the journal: %v", err)
	}
	src := &switchedStore{inner: fs}
	clock := at(100)
	c := NewCache(src, CacheOptions{MaxAge: maxAge, Now: func() time.Time { return clock }})
	return c, src, &clock
}

// ---------------------------------------------------------------------------
// The hot path
// ---------------------------------------------------------------------------

// TestTheHotPathNeverCallsTheAuthority is the property every other one rests on.
//
// It reports the PAIR — zero under test, non-zero on the positive control — because
// a reassuring zero on its own is indistinguishable from a counter nothing increments.
func TestTheHotPathNeverCallsTheAuthority(t *testing.T) {
	ctx := context.Background()
	c, src, _ := liveCache(t, time.Minute)

	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	after := src.readCount()
	if after != 1 {
		t.Fatalf("one Refresh made %d authority reads, want exactly 1", after)
	}

	for i := 0; i < 500; i++ {
		if _, _, err := c.Authenticate(carolToken); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		c.Resolve(user(uCarol))
		c.Model()
		c.Staleness()
	}
	if got := src.readCount(); got != after {
		t.Errorf("500 authorizations made %d authority reads, want 0 beyond the materialization", got-after)
	}

	// The positive control on the same counter: it CAN move.
	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if got := src.readCount(); got != after+1 {
		t.Errorf("the counter did not move on a refresh (%d -> %d); the zero above measured nothing", after, got)
	}
}

// TestAHungAuthorityDoesNotBlockReads measures the lock discipline, not the code
// shape: a refresh that held the write lock across the Source call would make a HUNG
// authority stop every read, which is the outage this type exists to survive.
//
// ⚠ A WEDGE HERE IS A TEST THAT NEVER RETURNS, so the read is done in a goroutine
// against a deadline. The healthy answer takes microseconds; the failing one takes
// forever, and the gap is not a timing judgement.
func TestAHungAuthorityDoesNotBlockReads(t *testing.T) {
	ctx := context.Background()
	c, src, _ := liveCache(t, time.Minute)
	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("materializing: %v", err)
	}

	hold := make(chan struct{})
	entered := make(chan struct{})
	src.mu.Lock()
	src.hold, src.entered = hold, entered
	src.mu.Unlock()

	go func() { _ = c.Refresh(ctx) }()
	<-entered // the refresh is now inside Source.Model and will not return

	done := make(chan uint64, 1)
	go func() {
		if _, _, err := c.Authenticate(carolToken); err != nil {
			t.Errorf("a read during a hung refresh: %v", err)
		}
		done <- c.Staleness().Epoch
	}()

	select {
	case epoch := <-done:
		if epoch == 0 {
			t.Errorf("the read served epoch 0 while a refresh was in flight")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("a read blocked for 5s behind a hung refresh — the Source call is inside the lock")
	}
	close(hold)
}

// ---------------------------------------------------------------------------
// Killing the dependency
// ---------------------------------------------------------------------------

// TestAKilledAuthorityKeepsServingAndTheAgeGrows is the plan's instruction taken
// literally: stop the authority, then prove reads still serve and the staleness is
// reported.
//
// 🔴 THE AGE IS MEASURED AT TWO NAMED POINTS — 60s and 300s after the last successful
// materialization — because one measurement cannot distinguish "the age is reported"
// from "a constant is printed". The bound is 2m, so the two points also straddle it:
// the first is `degraded` and the second is `stale`, which is the transition the whole
// report exists to make visible.
func TestAKilledAuthorityKeepsServingAndTheAgeGrows(t *testing.T) {
	ctx := context.Background()
	c, src, clock := liveCache(t, 2*time.Minute)
	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	healthy := c.Staleness()
	if healthy.Status != CacheFresh {
		t.Fatalf("a just-materialized cache reports %q, want fresh", healthy.Status)
	}

	src.unplug()

	// Point 1: 60 seconds after the last good materialization.
	*clock = at(101)
	if err := c.Refresh(ctx); !errors.Is(err, errAuthorityDown) {
		t.Fatalf("a refresh against a dead authority returned %v, want the authority error", err)
	}
	p1 := c.Staleness()
	if p1.Age != time.Minute {
		t.Errorf("point 1 age = %s, want 1m0s", p1.Age)
	}
	if p1.Status != CacheDegraded || !p1.Failing || p1.Exceeded {
		t.Errorf("point 1 = %q failing=%v exceeded=%v, want degraded/true/false", p1.Status, p1.Failing, p1.Exceeded)
	}

	// …and reads are still served, at the epoch that was last known good.
	if p1.Epoch != healthy.Epoch {
		t.Errorf("point 1 serves epoch %d, want the last known good %d", p1.Epoch, healthy.Epoch)
	}
	_, a, err := c.Authenticate(carolToken)
	if err != nil {
		t.Fatalf("a read with the authority DOWN: %v — reads must survive the outage", err)
	}
	if !a.Allows(sAtlasNotes, VerbAdmin) {
		t.Errorf("carol lost admin on atlas-notes while the authority was down")
	}

	// Point 2: 300 seconds. Past the 2m bound.
	*clock = at(105)
	if err := c.Refresh(ctx); !errors.Is(err, errAuthorityDown) {
		t.Fatalf("second refresh returned %v", err)
	}
	p2 := c.Staleness()
	if p2.Age != 5*time.Minute {
		t.Errorf("point 2 age = %s, want 5m0s", p2.Age)
	}
	if p2.Age <= p1.Age {
		t.Errorf("the age did not grow between the two points: %s then %s", p1.Age, p2.Age)
	}
	if p2.Status != CacheStale || !p2.Exceeded {
		t.Errorf("point 2 = %q exceeded=%v, want stale/true", p2.Status, p2.Exceeded)
	}
	if p2.Failures != 2 || p2.Refreshes != 1 {
		t.Errorf("counters = %d failures / %d refreshes, want 2 / 1", p2.Failures, p2.Refreshes)
	}
	if _, _, err := c.Authenticate(carolToken); err != nil {
		t.Errorf("a read past the staleness bound: %v — an exceeded bound is LOUD, not fatal", err)
	}
}

// TestAFailedRefreshDoesNotResetTheReportedAge pins the half that is easy to get
// wrong: advancing `materializedAt` on a failed attempt would report a week-dead
// authority as seconds old, so the report would be most wrong exactly when it matters.
//
// It is a REGRESSION guard rather than an invariant guard: the mutant
// `failed-refresh-restamps-the-age` in `tests/control_mutants.py` is the defect, and
// this test is what goes red on it.
func TestAFailedRefreshDoesNotResetTheReportedAge(t *testing.T) {
	ctx := context.Background()
	c, src, clock := liveCache(t, time.Hour)
	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	stamped := c.Staleness().MaterializedAt

	src.unplug()
	for _, minutes := range []int{101, 110, 130} {
		*clock = at(minutes)
		_ = c.Refresh(ctx)
		s := c.Staleness()
		if !s.MaterializedAt.Equal(stamped) {
			t.Fatalf("a failed refresh moved MaterializedAt from %s to %s", stamped, s.MaterializedAt)
		}
		want := at(minutes).Sub(at(100))
		if s.Age != want {
			t.Errorf("age at %s = %s, want %s", at(minutes), s.Age, want)
		}
		// The ATTEMPT is recorded separately, so the two facts stay distinguishable.
		if !s.LastAttempt.Equal(at(minutes)) {
			t.Errorf("LastAttempt = %s, want %s", s.LastAttempt, at(minutes))
		}
	}
}

// TestAWriteRefusesCleanlyWhenTheAuthorityIsDown — the third of the plan's three
// dependency-kill claims. "Cleanly" is the load-bearing word: the refusal must leave
// the cache untouched, so the asymmetry (reads survive, writes do not) is visible.
func TestAWriteRefusesCleanlyWhenTheAuthorityIsDown(t *testing.T) {
	ctx := context.Background()
	c, src, _ := liveCache(t, time.Minute)
	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	before := c.Staleness()

	src.unplug()

	revoke := Event{Kind: EventCredentialRevoked, At: at(101), CredentialID: "crd_carol"}
	for _, tc := range []struct {
		name string
		call func() (WriteResult, error)
	}{
		{"Apply", func() (WriteResult, error) { return c.Apply(ctx, revoke) }},
		{"ApplyNow", func() (WriteResult, error) { return c.ApplyNow(ctx, revoke) }},
	} {
		got, err := tc.call()
		if !errors.Is(err, errAuthorityDown) {
			t.Errorf("%s against a dead authority returned err=%v, want the authority error", tc.name, err)
		}
		if got != (WriteResult{}) {
			t.Errorf("%s returned %+v alongside its error — a refused write must claim no effect", tc.name, got)
		}
	}

	after := c.Staleness()
	if after.Epoch != before.Epoch || !after.MaterializedAt.Equal(before.MaterializedAt) {
		t.Errorf("a refused write moved the cache: epoch %d->%d, materialized %s->%s",
			before.Epoch, after.Epoch, before.MaterializedAt, after.MaterializedAt)
	}
	if _, _, err := c.Authenticate(carolToken); err != nil {
		t.Errorf("a read after a refused write: %v", err)
	}
	if src.writes != 2 {
		t.Errorf("the write double saw %d attempts, want 2 — the refusal must reach the authority, not be guessed", src.writes)
	}
}

// ---------------------------------------------------------------------------
// Fail-closed
// ---------------------------------------------------------------------------

// TestAnUnmaterializedCacheAuthorisesNobodyAndSaysSo pins the state that is NOT an
// outage: a cold start that never completed. It authorises nobody, and it must not
// render as `fresh` — a cache that authorises nobody while reporting itself healthy is
// the instrument failing silently.
func TestAnUnmaterializedCacheAuthorisesNobodyAndSaysSo(t *testing.T) {
	c, _, _ := liveCache(t, time.Minute)

	if _, _, err := c.Authenticate(carolToken); !errors.As(err, &ErrNoCredential{}) {
		t.Errorf("an unmaterialized cache authenticated carol: err=%v", err)
	}
	s := c.Staleness()
	if s.Status != CacheUnmaterialized || s.Materialized {
		t.Errorf("status = %q materialized=%v, want unmaterialized/false", s.Status, s.Materialized)
	}
	if s.Age != 0 || s.Epoch != 0 || s.LastTrigger != RefreshNever {
		t.Errorf("age=%s epoch=%d trigger=%q, want 0/0/never", s.Age, s.Epoch, s.LastTrigger)
	}
}

// ---------------------------------------------------------------------------
// Two triggers, one cache
// ---------------------------------------------------------------------------

// TestTwoConcurrentRefreshesCommitInSTARTOrderNotCompletionOrder is the revocation that
// came back.
//
// 🔴 THE SCENARIO IS THE DEPLOYED ONE, AND THIS PR IS WHAT MAKES IT REACHABLE. There are
// now two independent callers of `refresh` in one process — the timer in `Run` and SIGHUP
// through `api.Server.SetTokens` — and `src.Model` is called OUTSIDE the lock on purpose,
// so their reads overlap. The operator deletes a compromised row and sends SIGHUP. The
// timer had already entered `src.Model` and is holding the PRE-revocation table; SIGHUP
// reads the new one, commits, and returns. Then the timer resumes and assigns its stale
// model on top. The revoked credential authenticates again and `Staleness` says `fresh`.
//
// 🔴 AND THE FIXTURE IS BUILT SO THAT AN "IGNORE A LOWER EPOCH" GUARD WOULD FAIL IT
// RATHER THAN PASS IT. `Model.Epoch` is the event count of a projection, so removing a
// credential makes it go DOWN — asserted as a precondition below. A cache that ordered
// commits by epoch would reject exactly the smaller, newer world it exists to publish.
// The ordering therefore comes from a generation taken BEFORE the `src` call.
//
// ⚠ `-race` IS GREEN ON THE DEFECT AND ALWAYS WAS. Nothing here is a data race: the
// assignment is under the lock, the read is under the lock, and the result is wrong
// anyway. This is what a logical race needs instead — a forced interleaving.
func TestTwoConcurrentRefreshesCommitInSTARTOrderNotCompletionOrder(t *testing.T) {
	ctx := context.Background()

	// The world before the revocation, and the world after it. `crd_carol` is the
	// compromised row the operator deletes; everything else is untouched.
	before := withCredentials(t)
	var kept []Event
	for _, e := range append(worldEvents(), credentialEvents()...) {
		if e.CredentialID == "crd_carol" {
			continue
		}
		kept = append(kept, e)
	}
	after, err := Replay(kept)
	if err != nil {
		t.Fatalf("replaying the post-revocation world: %v", err)
	}

	// Preconditions, so nothing below can pass vacuously.
	if after.Epoch >= before.Epoch {
		t.Fatalf("precondition: a revocation must make the epoch go DOWN for this fixture "+
			"to say anything about epoch-ordering; got %d then %d", before.Epoch, after.Epoch)
	}
	if _, _, err := Authenticate(before, carolToken); err != nil {
		t.Fatalf("precondition: carol's credential must be live in the OLD world: %v", err)
	}
	if _, _, err := Authenticate(after, carolToken); err == nil {
		t.Fatal("precondition: carol's credential must be gone from the NEW world")
	}

	src := newGatedSource(before, after)
	clock := at(100)
	c := NewCache(src, CacheOptions{MaxAge: time.Minute, Now: func() time.Time { return clock }})

	// The TIMER starts first and is parked inside `src.Model` holding the old world.
	slow := make(chan error, 1)
	go func() { slow <- c.refresh(ctx, RefreshTimer) }()
	<-src.entered[0]

	// SIGHUP starts second and runs to completion.
	fast := make(chan error, 1)
	go func() { fast <- c.refresh(ctx, RefreshSignal) }()
	<-src.entered[1]
	close(src.release[1])
	if err := <-fast; err != nil {
		t.Fatalf("the SIGHUP refresh must succeed: %v", err)
	}
	if _, _, err := c.Authenticate(carolToken); err == nil {
		t.Fatal("precondition: the revocation must be in force once SIGHUP has committed")
	}

	// And now the timer resumes. This is the whole finding.
	close(src.release[0])
	if err := <-slow; err != nil {
		t.Fatalf("a superseded refresh is not a failure: %v", err)
	}
	if _, _, err := c.Authenticate(carolToken); err == nil {
		t.Fatal("THE FINDING: a refresh that STARTED before the revocation and finished " +
			"after it republished the pre-revocation world. The revoked credential " +
			"authenticates again and the status still says `fresh`")
	}

	s := c.Staleness()
	if s.Epoch != after.Epoch {
		t.Errorf("the serving epoch is %d, want the post-revocation %d", s.Epoch, after.Epoch)
	}
	// 🔴 THE DISCARD IS COUNTED, WHICH IS WHAT KEEPS THIS GUARD FROM BEING SATISFIABLE BY
	// A CACHE THAT SIMPLY STOPPED REFRESHING. One attempt materialized, one was
	// superseded, none failed — three different outcomes and three different counters.
	if s.Superseded != 1 || s.Refreshes != 1 || s.Failures != 0 {
		t.Errorf("superseded=%d refreshes=%d failures=%d, want 1/1/0 (%s)",
			s.Superseded, s.Refreshes, s.Failures, s)
	}
	if s.Status != CacheFresh || s.Failing {
		t.Errorf("a superseded refresh is not a failure: status=%q failing=%v", s.Status, s.Failing)
	}
	// The trigger names the last ATTEMPT, not the last COMMIT — the timer did attempt,
	// and a report that hid it would be hiding the very thing that raced.
	if s.LastTrigger != RefreshTimer {
		t.Errorf("trigger=%q, want %q", s.LastTrigger, RefreshTimer)
	}
}

// TestAConcurrentRefreshCannotUNDOApplyNow is the write half of the same ordering rule.
//
// 🔴 `ApplyNow`'s ONLY PROMISE IS ABOUT THIS PROCESS, AND A CONCURRENT REFRESH CAN TAKE
// IT AWAY. `Append` runs outside the lock exactly as `src.Model` does, so a timer refresh
// that read the authority BEFORE the write landed can commit afterwards and republish the
// pre-write world — while `ApplyNow` has already returned `EffectImmediate`, which a UI
// renders as "revoked" with no qualifier. That is the one failure the synchronous path
// exists to rule out.
//
// 🔴 THE INTERLEAVING IS THE ONE THAT DISTINGUISHES *WHERE* THE STAMP IS TAKEN, not
// merely whether one exists. The refresh starts while `Append` is in flight, so its read
// is genuinely ambiguous — it may have seen either side of the write. Stamped BEFORE
// `Append`, the write's generation is lower than the refresh's and the refresh wins,
// which is the defect. Stamped AFTER, the write's generation is higher and the ambiguous
// read is the one that is dropped. Both a missing stamp and a mis-placed one fail here.
func TestAConcurrentRefreshCannotUNDOApplyNow(t *testing.T) {
	ctx := context.Background()

	before := withCredentials(t)
	var kept []Event
	for _, e := range append(worldEvents(), credentialEvents()...) {
		if e.CredentialID == "crd_carol" {
			continue
		}
		kept = append(kept, e)
	}
	after, err := Replay(kept)
	if err != nil {
		t.Fatalf("replaying the post-revocation world: %v", err)
	}

	// Two `before` answers: one for the materialization, one for the refresh that races
	// the write. The authority's own answer to the WRITE is `after`.
	src := newGatedWriter(after, before, before)
	clock := at(100)
	c := NewCache(src, CacheOptions{MaxAge: time.Minute, Now: func() time.Time { return clock }})

	close(src.release[0])
	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	if _, _, err := c.Authenticate(carolToken); err != nil {
		t.Fatalf("precondition: carol must authenticate before the revocation: %v", err)
	}

	// The revoke-now, parked inside `Append`.
	result := make(chan WriteResult, 1)
	go func() {
		r, err := c.ApplyNow(ctx, Event{
			Kind: EventCredentialRevoked, At: at(101), CredentialID: "crd_carol",
		})
		if err != nil {
			t.Errorf("ApplyNow: %v", err)
		}
		result <- r
	}()
	<-src.inAppend

	// The timer refresh, entering `src.Model` while the write is in flight and reading
	// the PRE-revocation world.
	slow := make(chan error, 1)
	go func() { slow <- c.refresh(ctx, RefreshTimer) }()
	<-src.entered[1]

	close(src.releaseAppend)
	r := <-result
	if r.Effect != EffectImmediate {
		t.Fatalf("precondition: ApplyNow must report the revocation in force, got %s", r)
	}
	if _, _, err := c.Authenticate(carolToken); err == nil {
		t.Fatal("precondition: the revocation must be in force as ApplyNow returns")
	}

	close(src.release[1])
	if err := <-slow; err != nil {
		t.Fatalf("a superseded refresh is not a failure: %v", err)
	}

	if _, _, err := c.Authenticate(carolToken); err == nil {
		t.Fatalf("THE FINDING: a refresh that raced the write UNDID it. ApplyNow returned "+
			"%s and the revoked credential authenticates again", r)
	}
	s := c.Staleness()
	if s.Epoch != after.Epoch {
		t.Errorf("the serving epoch is %d, want the written %d", s.Epoch, after.Epoch)
	}
	if s.Superseded != 1 {
		t.Errorf("superseded=%d, want 1 — the racing refresh must be counted, not silent (%s)",
			s.Superseded, s)
	}
	// The positive control on the double: the write really did reach the authority once.
	if src.appendAttempts != 1 {
		t.Errorf("the authority saw %d appends, want exactly 1", src.appendAttempts)
	}
}

// TestAWriteDoesNotCommitOverAnAttemptThatSTARTEDAfterIt is the third arm of the
// ordering rule: `write`'s own `mine >= c.committed` clause, which decides what happens
// when the write is the LOSER rather than the winner.
//
// 🔴 THE CLAUSE WAS LABELLED EQUIVALENT ON A REASON THAT WAS FALSE, AND THAT IS WHY
// THIS TEST EXISTS RATHER THAN A SHORTER ONE. `tests/control_mutants.py` carried
// `the-write-ignores-a-newer-commit` as a survivor whose label said the interleaving
// needed "a window between two adjacent statements that no gate can open from outside".
// The statements are not adjacent: `now := c.clock()` sits between `c.begin()` and
// `c.mu.Lock()`, and `clock` is a CALLER-INJECTED hook (`CacheOptions.Now`). A label that
// reads as coverage while providing none forecloses the very test that closes the gap —
// the exact shape the battery exists to refuse, sitting inside the battery.
//
// 🔴 THE INTERLEAVING IS FORCED FROM THE CLOCK AND IS THEREFORE FULLY SYNCHRONOUS. No
// goroutine, no channel, no timing: the hook fires exactly once, on the `c.clock()` call
// `write` makes after taking its generation, and runs a whole refresh — `begin`, the
// authority read, the commit — inside it. That third attempt's generation is HIGHER than
// the write's, so it is known to have read the authority after the append returned, and
// the write must not publish over it.
//
// 🔴 AND THE THIRD WORLD'S EPOCH IS LOWER THAN THE WRITTEN ONE, SO NO "HIGHER EPOCH
// WINS" RULE CAN PASS THIS. The epoch is an event count and a revocation makes it go
// down; the ordering has to come from the generation or from nothing.
func TestAWriteDoesNotCommitOverAnAttemptThatSTARTEDAfterIt(t *testing.T) {
	ctx := context.Background()

	// Three worlds off one journal, each one credential smaller than the last.
	without := func(ids ...string) Model {
		t.Helper()
		drop := map[string]bool{}
		for _, id := range ids {
			drop[id] = true
		}
		var kept []Event
		for _, e := range cacheWorld() {
			if drop[string(e.CredentialID)] {
				continue
			}
			kept = append(kept, e)
		}
		m, err := Replay(kept)
		if err != nil {
			t.Fatalf("replaying the world without %v: %v", ids, err)
		}
		return m
	}
	before := withCredentials(t)
	after := without("crd_carol")              // what the WRITE's append returns
	third := without("crd_carol", "crd_atlas") // what the third attempt reads

	// Preconditions, so nothing below can pass vacuously.
	if !(third.Epoch < after.Epoch && after.Epoch < before.Epoch) {
		t.Fatalf("precondition: each revocation must make the epoch go DOWN, so that "+
			"ordering by epoch cannot produce the right answer here; got %d, %d, %d",
			before.Epoch, after.Epoch, third.Epoch)
	}
	for _, p := range []struct {
		world Model
		name  string
		token string
		live  bool
	}{
		{before, "before", carolToken, true}, {before, "before", atlasToken, true},
		{after, "after", carolToken, false}, {after, "after", atlasToken, true},
		{third, "third", carolToken, false}, {third, "third", atlasToken, false},
	} {
		_, _, err := Authenticate(p.world, p.token)
		if (err == nil) != p.live {
			t.Fatalf("precondition: in the %q world that credential must be live=%v, got err=%v",
				p.name, p.live, err)
		}
	}

	// Every gate is pre-opened: the forcing happens in the clock, not in a goroutine.
	src := newGatedWriter(after, before, third)
	close(src.release[0])
	close(src.release[1])
	close(src.releaseAppend)

	clock := at(100)
	var c *Cache
	var hookMu sync.Mutex
	armed := false
	c = NewCache(src, CacheOptions{MaxAge: time.Minute, Now: func() time.Time {
		hookMu.Lock()
		fire := armed
		armed = false // the nested refresh reads the clock too; it must not re-enter
		hookMu.Unlock()
		if fire {
			// THE THIRD ATTEMPT. It calls `begin` after the write's stamp and commits
			// before the write reaches the lock — the window the EQUIVALENT label said
			// could not be opened from outside.
			if err := c.refresh(ctx, RefreshTimer); err != nil {
				t.Errorf("the third attempt must succeed: %v", err)
			}
		}
		return clock
	}})

	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	if _, _, err := c.Authenticate(atlasToken); err != nil {
		t.Fatalf("precondition: the atlas credential must authenticate before any of this: %v", err)
	}

	hookMu.Lock()
	armed = true
	hookMu.Unlock()
	r, err := c.ApplyNow(ctx, Event{
		Kind: EventCredentialRevoked, At: at(101), CredentialID: "crd_carol",
	})
	if err != nil {
		t.Fatalf("ApplyNow: %v", err)
	}
	hookMu.Lock()
	stillArmed := armed
	hookMu.Unlock()
	if stillArmed {
		t.Fatal("the hook never fired, so the interleaving was never built and every " +
			"assertion below would be about an ordinary uncontested write")
	}

	// THE FINDING THE CLAUSE PREVENTS. Without it the write republishes `after`, which
	// is a world the authority has already moved on from, and the atlas credential the
	// third attempt removed comes back.
	if _, _, err := c.Authenticate(atlasToken); err == nil {
		t.Errorf("THE FINDING: the write committed over an attempt that STARTED after it. "+
			"The third attempt's world had the atlas credential removed and it "+
			"authenticates again (%s)", c.Staleness())
	}
	if _, _, err := c.Authenticate(carolToken); err == nil {
		t.Errorf("the revocation this write performed must be in force whichever of the "+
			"two worlds is serving (%s)", c.Staleness())
	}

	s := c.Staleness()
	if s.Epoch != third.Epoch {
		t.Errorf("the serving epoch is %d, want the third attempt's %d (the written world "+
			"is %d)", s.Epoch, third.Epoch, after.Epoch)
	}
	// Three outcomes, three counters: the materialization and the third attempt each
	// published, the write was discarded, nothing failed.
	if s.Superseded != 1 || s.Refreshes != 2 || s.Failures != 0 {
		t.Errorf("superseded=%d refreshes=%d failures=%d, want 1/2/0 (%s)",
			s.Superseded, s.Refreshes, s.Failures, s)
	}
	// The discarded write must not stamp the report either: `lastTrigger` and
	// `lastAttempt` move with the model, and the model here is the third attempt's.
	if s.LastTrigger != RefreshTimer {
		t.Errorf("trigger=%q, want %q — a write that published nothing must not claim the "+
			"last attempt", s.LastTrigger, RefreshTimer)
	}
	if src.appendAttempts != 1 {
		t.Errorf("the authority saw %d appends, want exactly 1", src.appendAttempts)
	}
	// ⚠ THE EFFECT IS `deferred` AND THAT IS THE INVARIANT RATHER THAN A DEFECT, worth
	// pinning because it is the surprising direction. `Effect` is derived from the two
	// epochs — `EffectImmediate` exactly when `ServingEpoch >= WrittenEpoch` — and the
	// serving world here is SMALLER than the written one while genuinely containing the
	// write. So a revocation that IS in force reports itself deferred. That is the
	// conservative half of the trade: a UI says "effective by <time>" about something
	// that already happened, which is the direction that cannot mislead an operator into
	// believing a revocation landed when it has not.
	if r.Effect != EffectDeferred {
		t.Errorf("effect=%s, want %s: the invariant is structural — `immediate` exactly "+
			"when serving >= written, and here serving-epoch=%d written-epoch=%d",
			r.Effect, EffectDeferred, r.ServingEpoch, r.WrittenEpoch)
	}
	if r.ServingEpoch != third.Epoch || r.WrittenEpoch != after.Epoch {
		t.Errorf("serving-epoch=%d written-epoch=%d, want %d and %d",
			r.ServingEpoch, r.WrittenEpoch, third.Epoch, after.Epoch)
	}
}

// ---------------------------------------------------------------------------
// The report
// ---------------------------------------------------------------------------

// TestTheStalenessRendersExactly pins the WHOLE normalised line for SEVEN states.
//
// 🔴 THE EXPECTATIONS ARE LITERAL AND THE FIXTURE IS HAND-BUILT, so nothing here is
// derived from the code under test: the source answers a Model with a spelled-out
// epoch of 7 and the clock is moved by hand. A guard on a few words would be walkable
// by rewording; this is the whole string.
//
// ⚠ THE SEVENTH SUBTEST IS THE ONE THAT MEASURES `superseded=`. The other six read it as
// `0`, which pins the spelling and nothing else; the seventh forces the interleaving and
// reads the number. Count the subtests before editing this sentence — it said "six" for a
// round after the seventh was added.
func TestTheStalenessRendersExactly(t *testing.T) {
	ctx := context.Background()
	m := NewModel()
	m.Epoch = 7
	m.At = at(50)
	src := fixedSource{m: m}

	build := func(maxAge time.Duration) (*Cache, *time.Time) {
		clock := at(100)
		return NewCache(src, CacheOptions{MaxAge: maxAge, Now: func() time.Time { return clock }}), &clock
	}

	t.Run("unmaterialized", func(t *testing.T) {
		c, _ := build(time.Minute)
		want := "authz-cache: status=unmaterialized epoch=n/a age=n/a bound=1m0s materialized=never trigger=never refreshes=0 failures=0 superseded=0 failing=false"
		if got := c.Staleness().String(); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})

	t.Run("fresh at 30s under a 1m bound", func(t *testing.T) {
		c, clock := build(time.Minute)
		if err := c.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		*clock = at(100).Add(30 * time.Second)
		want := "authz-cache: status=fresh epoch=7 age=30s bound=1m0s materialized=2000-01-01T01:40:00Z trigger=explicit refreshes=1 failures=0 superseded=0 failing=false"
		if got := c.Staleness().String(); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})

	// The boundary itself. An age exactly equal to the bound is the bound being MET —
	// a schedule that refreshes every MaxAge must be a legal configuration rather than
	// one that reports itself stale on every tick.
	t.Run("exactly at the bound is still fresh", func(t *testing.T) {
		c, clock := build(time.Minute)
		if err := c.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		*clock = at(101)
		want := "authz-cache: status=fresh epoch=7 age=1m0s bound=1m0s materialized=2000-01-01T01:40:00Z trigger=explicit refreshes=1 failures=0 superseded=0 failing=false"
		if got := c.Staleness().String(); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})

	t.Run("one second past the bound is stale", func(t *testing.T) {
		c, clock := build(time.Minute)
		if err := c.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		*clock = at(101).Add(time.Second)
		want := "authz-cache: status=stale epoch=7 age=1m1s bound=1m0s materialized=2000-01-01T01:40:00Z trigger=explicit refreshes=1 failures=0 superseded=0 failing=false"
		if got := c.Staleness().String(); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})

	// 🔴 NO DECLARED BOUND RENDERS `bound=none`, NEVER `bound=0s`. `Exceeded` is
	// structurally false there, and a reader must not be able to mistake "no bound was
	// declared" for "the bound was met".
	t.Run("no bound", func(t *testing.T) {
		c, clock := build(0)
		if err := c.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		*clock = at(200)
		want := "authz-cache: status=fresh epoch=7 age=1h40m0s bound=none materialized=2000-01-01T01:40:00Z trigger=explicit refreshes=1 failures=0 superseded=0 failing=false"
		if got := c.Staleness().String(); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
		if c.Staleness().Exceeded {
			t.Errorf("an undeclared bound reported itself exceeded")
		}
	})

	t.Run("degraded", func(t *testing.T) {
		c, src, clock := liveCache(t, time.Minute)
		if err := c.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
		src.unplug()
		*clock = at(100).Add(10 * time.Second)
		_ = c.Refresh(ctx)
		s := c.Staleness()
		// The epoch here is the fixture journal's length, which is fixture data.
		want := "authz-cache: status=degraded epoch=" + itoa(uint64(len(cacheWorld()))) +
			" age=10s bound=1m0s materialized=2000-01-01T01:40:00Z trigger=explicit refreshes=1 failures=1 superseded=0 failing=true"
		if got := s.String(); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
		// 🔴 THE ERROR TEXT IS A FIELD AND IS NOT IN THE LINE. Un-authored text on an
		// operator stream can forge a line boundary; the boolean carries the fact.
		if s.LastError == nil {
			t.Errorf("LastError is nil on a degraded cache — the text must still be reachable")
		}
	})

	// 🔴 THE SEVENTH STATE, AND WITHOUT IT `superseded=` IS A KEY NO EXPECTATION IN THIS
	// TEST EVER SEES NON-ZERO. Six literals reading `superseded=0` pin the spelling and
	// measure nothing about the counter; this one forces the interleaving and reads the
	// number. The epochs are hand-built and go DOWN — 7 then 5 — which is the shape a
	// revocation has and the reason the ordering cannot come from the epoch.
	t.Run("a superseded refresh is counted, not failed", func(t *testing.T) {
		older, newer := NewModel(), NewModel()
		older.Epoch, older.At = 7, at(50)
		newer.Epoch, newer.At = 5, at(50)

		g := newGatedSource(older, newer)
		clock := at(100)
		c := NewCache(g, CacheOptions{MaxAge: time.Minute, Now: func() time.Time { return clock }})

		slow := make(chan error, 1)
		go func() { slow <- c.refresh(ctx, RefreshTimer) }()
		<-g.entered[0]
		fast := make(chan error, 1)
		go func() { fast <- c.refresh(ctx, RefreshSignal) }()
		<-g.entered[1]
		close(g.release[1])
		if err := <-fast; err != nil {
			t.Fatal(err)
		}
		close(g.release[0])
		if err := <-slow; err != nil {
			t.Fatal(err)
		}

		want := "authz-cache: status=fresh epoch=5 age=0s bound=1m0s materialized=2000-01-01T01:40:00Z trigger=timer refreshes=1 failures=0 superseded=1 failing=false"
		if got := c.Staleness().String(); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// ---------------------------------------------------------------------------
// The three triggers
// ---------------------------------------------------------------------------

// waitFor polls until cond or the deadline. Polling rather than sleeping a fixed
// interval: a fixed sleep either flakes under load or wastes the budget, and this
// reports WHICH condition was never reached.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestTheTimerTriggerRefreshes runs with the OTHER TWO TRIGGERS DISABLED, so
// `LastTrigger` identifies the mechanism rather than merely confirming that something
// refreshed. Three refreshes rather than one, because a single one cannot distinguish
// a repeating timer from a one-shot.
func TestTheTimerTriggerRefreshes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _, _ := liveCache(t, time.Minute)

	go func() { _ = c.Run(ctx, RefreshTriggers{Interval: 2 * time.Millisecond}) }()

	waitFor(t, "three timer refreshes", func() bool { return c.Staleness().Refreshes >= 3 })
	if got := c.Staleness().LastTrigger; got != RefreshTimer {
		t.Errorf("LastTrigger = %q, want timer — with every other trigger disabled", got)
	}
}

// TestSIGHUPRefreshesTheCache sends a REAL signal to this process, because the
// operator's muscle memory is `kill -HUP` and a test over a hand-fed channel would
// measure the channel rather than the signal.
func TestSIGHUPRefreshesTheCache(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _, _ := liveCache(t, time.Minute)

	hups := make(chan os.Signal, 1)
	signal.Notify(hups, syscall.SIGHUP)
	defer signal.Stop(hups)

	// Interval 0: the timer is off, so a refresh here can only have come from the
	// signal.
	go func() { _ = c.Run(ctx, RefreshTriggers{Signals: hups}) }()

	if c.Staleness().Refreshes != 0 {
		t.Fatalf("something refreshed before the signal")
	}
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("sending SIGHUP: %v", err)
	}
	waitFor(t, "a refresh triggered by SIGHUP", func() bool { return c.Staleness().Refreshes >= 1 })
	if got := c.Staleness().LastTrigger; got != RefreshSignal {
		t.Errorf("LastTrigger = %q, want signal", got)
	}
}

// TestTwoNotifyChannelsBothReceiveOneSIGHUP measures the claim `RefreshTriggers`
// makes in prose.
//
// 🔴 THE OPPOSITE BELIEF IS THE PLAUSIBLE ONE: that a second `signal.Notify` replaces
// the first, so the token-file reload already installed by `cmd/cairn-server` would
// SWALLOW the signal and this trigger would be silently dead in the only program that
// has both. Measured, not reasoned about.
func TestTwoNotifyChannelsBothReceiveOneSIGHUP(t *testing.T) {
	first := make(chan os.Signal, 1)
	second := make(chan os.Signal, 1)
	signal.Notify(first, syscall.SIGHUP)
	signal.Notify(second, syscall.SIGHUP)
	defer signal.Stop(first)
	defer signal.Stop(second)

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("sending SIGHUP: %v", err)
	}
	for name, ch := range map[string]chan os.Signal{"first": first, "second": second} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Errorf("the %s channel never received the signal", name)
		}
	}
}

// TestTheChangeTriggerRefreshes — the "on change" half of the explicit trigger, for a
// caller that learns the world moved without doing the write itself.
func TestTheChangeTriggerRefreshes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _, _ := liveCache(t, time.Minute)

	changes := make(chan struct{}, 1)
	go func() { _ = c.Run(ctx, RefreshTriggers{OnChange: changes}) }()

	changes <- struct{}{}
	waitFor(t, "a refresh triggered by a change notification", func() bool { return c.Staleness().Refreshes >= 1 })
	if got := c.Staleness().LastTrigger; got != RefreshChange {
		t.Errorf("LastTrigger = %q, want change", got)
	}
}

// TestAFailedRefreshDoesNotStopTheLoop: a transient outage must not leave a cache that
// never tries again. The authority is dead, then revived; the loop must be the thing
// that notices.
func TestAFailedRefreshDoesNotStopTheLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, src, _ := liveCache(t, time.Minute)
	src.unplug()

	go func() { _ = c.Run(ctx, RefreshTriggers{Interval: 2 * time.Millisecond}) }()

	waitFor(t, "two failed attempts", func() bool { return c.Staleness().Failures >= 2 })
	if c.Staleness().Refreshes != 0 {
		t.Fatalf("something succeeded against a dead authority")
	}

	src.mu.Lock()
	src.down = false
	src.mu.Unlock()

	waitFor(t, "recovery after the authority came back", func() bool { return c.Staleness().Refreshes >= 1 })
	s := c.Staleness()
	if s.Failing || s.Status != CacheFresh {
		t.Errorf("after recovery: failing=%v status=%q, want false/fresh", s.Failing, s.Status)
	}
}

// TestARunningCacheREPORTSEveryRefreshResultToItsCaller.
//
// 🔴 THE HAZARD IS THE DISCARDED RETURN, AND `Staleness` BEING CORRECT DOES NOT CLOSE IT.
// `Run` threw its refresh errors away and recorded them in `Staleness`, which nothing
// outside this file read — so a control journal that stopped parsing left a pod serving
// last-known-good, correctly and SILENTLY, until a restart turned it into a crash loop
// with no signal in between. `RefreshTriggers.OnRefresh` is what a caller can render.
//
// ⚠ BOTH EDGES, BECAUSE A FAILURE-ONLY HOOK CANNOT SAY "IT RECOVERED" AND EVERY CONSUMER
// WOULD THEN POLL `Staleness` FOR THE OTHER HALF — a second mechanism answering the
// question this one exists for. The recovery arm below is the positive control on that.
func TestARunningCacheREPORTSEveryRefreshResultToItsCaller(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, src, _ := liveCache(t, time.Minute)
	src.unplug()

	var mu sync.Mutex
	var seen []error
	count := func(want bool) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, e := range seen {
			if (e != nil) == want {
				n++
			}
		}
		return n
	}

	go func() {
		_ = c.Run(ctx, RefreshTriggers{
			Interval: 2 * time.Millisecond,
			OnRefresh: func(err error) {
				mu.Lock()
				seen = append(seen, err)
				mu.Unlock()
			},
		})
	}()

	waitFor(t, "two REPORTED failures", func() bool { return count(true) >= 2 })
	if count(false) != 0 {
		t.Fatalf("a dead authority reported %d successes", count(false))
	}
	// 🔴 THE REPORTED VALUE IS THE AUTHORITY'S OWN ERROR, NOT A SENTINEL THE LOOP
	// INVENTED. A hook handed `errors.New("refresh failed")` would satisfy every count
	// above and tell an operator nothing about WHAT failed, which is the whole point of
	// pushing it rather than leaving it in a counter.
	mu.Lock()
	first := seen[0]
	mu.Unlock()
	if !errors.Is(first, errAuthorityDown) {
		t.Fatalf("the reported error is not the one the authority returned: %v", first)
	}

	src.mu.Lock()
	src.down = false
	src.mu.Unlock()

	waitFor(t, "a REPORTED success once the authority came back", func() bool { return count(false) >= 1 })
}

// TestRunRefusesAScheduleThatCannotKeepTheBound, measured at TWO points around the
// boundary: interval == bound is keepable and must be accepted; one nanosecond more is
// not and must be refused. A bound nothing keeps is read as a promise.
func TestRunRefusesAScheduleThatCannotKeepTheBound(t *testing.T) {
	c, _, _ := liveCache(t, time.Minute)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	if err := c.Run(cancelled, RefreshTriggers{Interval: time.Minute}); !errors.Is(err, context.Canceled) {
		t.Errorf("interval == bound returned %v, want it ACCEPTED (and then stopped by the context)", err)
	}
	err := c.Run(cancelled, RefreshTriggers{Interval: time.Minute + time.Nanosecond})
	if !errors.Is(err, ErrRefreshCannotKeepBound) {
		t.Errorf("interval > bound returned %v, want ErrRefreshCannotKeepBound", err)
	}
	// An undeclared bound cannot be broken, so any interval is legal against it.
	unbounded := NewCache(fixedSource{m: NewModel()}, CacheOptions{})
	if err := unbounded.Run(cancelled, RefreshTriggers{Interval: time.Hour}); !errors.Is(err, context.Canceled) {
		t.Errorf("an hour against no bound returned %v, want it accepted", err)
	}
}

// ---------------------------------------------------------------------------
// Revoke now, versus revoke and wait
// ---------------------------------------------------------------------------

// TestASynchronousRevokeIsInForceBeforeItReturns is the "revoke now" path measured by
// its OBSERVABLE — the credential stops authenticating — rather than by its label.
func TestASynchronousRevokeIsInForceBeforeItReturns(t *testing.T) {
	ctx := context.Background()
	c, _, _ := liveCache(t, time.Minute)
	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	if _, _, err := c.Authenticate(carolToken); err != nil {
		t.Fatalf("carol must authenticate before the revoke: %v", err)
	}
	before := c.Staleness().Epoch

	got, err := c.ApplyNow(ctx, Event{Kind: EventCredentialRevoked, At: at(101), CredentialID: "crd_carol"})
	if err != nil {
		t.Fatalf("ApplyNow: %v", err)
	}
	if got.Effect != EffectImmediate {
		t.Fatalf("effect = %q, want immediate", got.Effect)
	}
	if got.ServingEpoch != before+1 || got.WrittenEpoch != before+1 {
		t.Errorf("epochs = serving %d / written %d, want both %d", got.ServingEpoch, got.WrittenEpoch, before+1)
	}
	if !got.Immediate() {
		t.Errorf("Immediate() disagrees with Effect")
	}
	// The observable. No refresh between the call and this line.
	if _, _, err := c.Authenticate(carolToken); !errors.As(err, &ErrNoCredential{}) {
		t.Errorf("carol still authenticates after a SYNCHRONOUS revoke: %v", err)
	}
	if got := c.Staleness().LastTrigger; got != RefreshWrite {
		t.Errorf("LastTrigger = %q, want write", got)
	}
}

// TestAnOrdinaryRevokeIsDeferredAndSaysSo is the other half, and the reason the return
// value carries an Effect at all: the UI must be able to say WHICH one happened.
func TestAnOrdinaryRevokeIsDeferredAndSaysSo(t *testing.T) {
	ctx := context.Background()
	c, _, clock := liveCache(t, time.Minute)
	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	before := c.Staleness().Epoch

	// 🔴 THE CLOCK IS MOVED BEFORE THE WRITE, DELIBERATELY. `EffectiveBy` is
	// `MaterializedAt + Bound`, and a fixture that writes at the same instant it
	// materialized makes that indistinguishable from `now + Bound` — the deadline
	// measured from the wrong end, which silently under-reports the stale window. The
	// two constants must differ or the assertion below cannot see the difference.
	*clock = at(100).Add(20 * time.Second)

	got, err := c.Apply(ctx, Event{Kind: EventCredentialRevoked, At: at(101), CredentialID: "crd_carol"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.Effect != EffectDeferred {
		t.Fatalf("effect = %q, want deferred", got.Effect)
	}
	if got.ServingEpoch != before || got.WrittenEpoch != before+1 {
		t.Errorf("epochs = serving %d / written %d, want %d / %d", got.ServingEpoch, got.WrittenEpoch, before, before+1)
	}
	// The deadline is the declared bound measured from the LAST MATERIALIZATION —
	// at(100) + 1m — which is what lets a UI say "effective by" rather than "soon".
	// Not at(100)+20s+1m: the clock has moved since, and the stale window did not
	// restart when somebody happened to write.
	if want := at(100).Add(time.Minute); !got.EffectiveBy.Equal(want) {
		t.Errorf("EffectiveBy = %s, want %s", got.EffectiveBy, want)
	}
	// 🔴 THE CACHE IS STILL SERVING THE OLD WORLD, which is the honest limit this
	// whole piece exists to report rather than hide.
	if _, _, err := c.Authenticate(carolToken); err != nil {
		t.Errorf("a deferred revoke took effect immediately: %v — then the two paths are indistinguishable", err)
	}
	// …and the ordinary refresh picks it up.
	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, _, err := c.Authenticate(carolToken); !errors.As(err, &ErrNoCredential{}) {
		t.Errorf("the deferred revoke never landed: %v", err)
	}
}

// TestTheEffectIsDerivedFromTheEpochsNotFromTheCallSite pins the design decision.
// A deferred write whose epoch a concurrent refresh has ALREADY picked up is in force,
// and reporting it as pending would make a UI say "within 60s" about something that
// already happened.
func TestTheEffectIsDerivedFromTheEpochsNotFromTheCallSite(t *testing.T) {
	ctx := context.Background()
	c, src, _ := liveCache(t, time.Minute)
	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("materializing: %v", err)
	}

	// The double refreshes the cache from inside Append, which is the deterministic
	// stand-in for a concurrent refresh landing between the write and the return.
	raced := &racingStore{inner: src, cache: c, ctx: ctx}
	c.src = raced

	got, err := c.Apply(ctx, Event{Kind: EventCredentialRevoked, At: at(101), CredentialID: "crd_carol"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.Effect != EffectImmediate {
		t.Errorf("effect = %q, want immediate: the cache is already serving epoch %d and the write produced %d",
			got.Effect, got.ServingEpoch, got.WrittenEpoch)
	}
	if got.ServingEpoch < got.WrittenEpoch {
		t.Errorf("an immediate effect over serving %d < written %d", got.ServingEpoch, got.WrittenEpoch)
	}
}

// racingStore refreshes the cache from inside Append — a concurrent refresh landing at
// the worst possible instant, made deterministic.
type racingStore struct {
	inner Store
	cache *Cache
	ctx   context.Context
}

func (r *racingStore) Model(ctx context.Context) (Model, error) { return r.inner.Model(ctx) }

func (r *racingStore) Append(ctx context.Context, events ...Event) (Model, error) {
	m, err := r.inner.Append(ctx, events...)
	if err != nil {
		return m, err
	}
	if err := r.cache.Refresh(r.ctx); err != nil {
		return m, err
	}
	return m, nil
}

// TestAWriteToAReadOnlyAuthorityIsRefused — a `Source` has no write half, and naming
// that is better than a panicking type assertion.
func TestAWriteToAReadOnlyAuthorityIsRefused(t *testing.T) {
	ctx := context.Background()
	c := NewCache(readOnlySource{m: NewModel()}, CacheOptions{MaxAge: time.Minute})
	if _, err := c.Apply(ctx, Event{Kind: EventGrantRevoked, GrantID: "grt_1"}); !errors.Is(err, ErrAuthorityReadOnly) {
		t.Errorf("Apply over a Source returned %v, want ErrAuthorityReadOnly", err)
	}
	if _, err := c.ApplyNow(ctx, Event{Kind: EventGrantRevoked, GrantID: "grt_1"}); !errors.Is(err, ErrAuthorityReadOnly) {
		t.Errorf("ApplyNow over a Source returned %v, want ErrAuthorityReadOnly", err)
	}
}

// TestAWriteOfNoEventsIsRefused. A zero-event write is a no-op that would still return
// an Effect — the one input that makes `ServingEpoch >= WrittenEpoch` true without
// anything having happened, which would put a hole in the structural invariant.
func TestAWriteOfNoEventsIsRefused(t *testing.T) {
	ctx := context.Background()
	c, _, _ := liveCache(t, time.Minute)
	if err := c.Refresh(ctx); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	if _, err := c.Apply(ctx); !errors.Is(err, ErrNoEvents) {
		t.Errorf("Apply() with no events returned %v, want ErrNoEvents", err)
	}
	if _, err := c.ApplyNow(ctx); !errors.Is(err, ErrNoEvents) {
		t.Errorf("ApplyNow() with no events returned %v, want ErrNoEvents", err)
	}
}

// TestTheWriteResultRendersExactly — the whole normalised string, over hand-built
// values, for the three shapes a UI has to distinguish.
func TestTheWriteResultRendersExactly(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   WriteResult
		want string
	}{
		{
			name: "immediate",
			in:   WriteResult{Effect: EffectImmediate, ServingEpoch: 26, WrittenEpoch: 26, EffectiveBy: at(101)},
			want: "authz-write: effect=immediate serving-epoch=26 written-epoch=26 effective-by=now",
		},
		{
			name: "deferred with a bound",
			in:   WriteResult{Effect: EffectDeferred, ServingEpoch: 25, WrittenEpoch: 26, EffectiveBy: at(101)},
			want: "authz-write: effect=deferred serving-epoch=25 written-epoch=26 effective-by=2000-01-01T01:41:00Z",
		},
		{
			// No declared bound: there is no deadline to promise, and a renderer must
			// say so rather than invent a date.
			name: "deferred with no bound",
			in:   WriteResult{Effect: EffectDeferred, ServingEpoch: 25, WrittenEpoch: 26},
			want: "authz-write: effect=deferred serving-epoch=25 written-epoch=26 effective-by=unbounded",
		},
	} {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("%s:\ngot  %q\nwant %q", tc.name, got, tc.want)
		}
	}
}
