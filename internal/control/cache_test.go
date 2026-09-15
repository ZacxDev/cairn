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
// The report
// ---------------------------------------------------------------------------

// TestTheStalenessRendersExactly pins the WHOLE normalised line for six states.
//
// 🔴 THE EXPECTATIONS ARE LITERAL AND THE FIXTURE IS HAND-BUILT, so nothing here is
// derived from the code under test: the source answers a Model with a spelled-out
// epoch of 7 and the clock is moved by hand. A guard on a few words would be walkable
// by rewording; this is the whole string.
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
		want := "authz-cache: status=unmaterialized epoch=n/a age=n/a bound=1m0s materialized=never trigger=never refreshes=0 failures=0 failing=false"
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
		want := "authz-cache: status=fresh epoch=7 age=30s bound=1m0s materialized=2000-01-01T01:40:00Z trigger=explicit refreshes=1 failures=0 failing=false"
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
		want := "authz-cache: status=fresh epoch=7 age=1m0s bound=1m0s materialized=2000-01-01T01:40:00Z trigger=explicit refreshes=1 failures=0 failing=false"
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
		want := "authz-cache: status=stale epoch=7 age=1m1s bound=1m0s materialized=2000-01-01T01:40:00Z trigger=explicit refreshes=1 failures=0 failing=false"
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
		want := "authz-cache: status=fresh epoch=7 age=1h40m0s bound=none materialized=2000-01-01T01:40:00Z trigger=explicit refreshes=1 failures=0 failing=false"
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
			" age=10s bound=1m0s materialized=2000-01-01T01:40:00Z trigger=explicit refreshes=1 failures=1 failing=true"
		if got := s.String(); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
		// 🔴 THE ERROR TEXT IS A FIELD AND IS NOT IN THE LINE. Un-authored text on an
		// operator stream can forge a line boundary; the boolean carries the fact.
		if s.LastError == nil {
			t.Errorf("LastError is nil on a degraded cache — the text must still be reachable")
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
