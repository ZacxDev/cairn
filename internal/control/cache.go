package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Cache is a materialized projection of a control Source: a Model, the epoch it was
// built from, and the instant it was built.
//
// 🔴 READS NEVER TOUCH THE AUTHORITY, AND THAT IS THE POINT RATHER THAN AN
// OPTIMISATION. cairn's promise is that an offline "orient me" still answers, so an
// outage of the durable authority — a file that has become unreadable today, a
// Postgres or an identity provider once P4 lands — must not be able to stop a read.
// Every authorization decision served from here is computed from `c.model`, which
// only a refresh may replace; `Source.Model` is called from `refresh` and from
// nowhere else. `TestTheHotPathNeverCallsTheAuthority` counts those calls from BOTH
// directions — zero while authorising, non-zero on a refresh — because a counter that
// never moves is indistinguishable from a counter wired to nothing.
//
// 🔴 AND THE HONEST LIMIT, STATED HERE BECAUSE EVERY OTHER PROPERTY IS BOUGHT WITH
// IT: A REVOCATION IS NOT EFFECTIVE UNTIL THE CACHE REFRESHES. No arrangement of this
// design makes that false — a cache that asked the authority whether it was stale
// would be making exactly the call the outage is supposed to survive. So the lag is
// BOUNDED by the refresh schedule, REPORTED by `Staleness` as a value rather than a
// log line, and BYPASSABLE by `ApplyNow` for the one case that cannot wait.
//
// ⚠ WHAT A `Cache` IS NOT: a second place that decides visibility. It holds a Model
// and hands it to `Authenticate`/`Resolve`, which remain the only functions that
// answer "what may this principal see". Adding a narrowing here would be the second
// implementation `internal/control/README.md` exists to forbid.
type Cache struct {
	src    Source
	maxAge time.Duration
	now    func() time.Time

	// mu guards everything below it. 🔴 IT IS NEVER HELD ACROSS A CALL TO `src`.
	// A refresh that held the write lock while the authority answered would let a
	// HUNG authority block every read — which is the precise outage this cache
	// exists to survive, reintroduced by the thing built to survive it. The Source
	// call happens outside the lock and only the swap happens inside.
	// `TestAHungAuthorityDoesNotBlockReads` is the measurement.
	mu             sync.RWMutex
	model          Model
	materialized   bool
	materializedAt time.Time
	lastAttempt    time.Time
	lastTrigger    RefreshReason
	lastErr        error
	refreshes      uint64
	failures       uint64
}

// CacheOptions configures a Cache.
type CacheOptions struct {
	// MaxAge is the staleness BOUND this cache declares: the age past which it
	// reports itself `stale` rather than merely old.
	//
	// 🔴 IT BOUNDS THE REPORT, NOT THE READS. Refusing to serve past the bound would
	// convert an authority outage into a total read outage, which is the failure this
	// whole type exists to prevent — and it would do so at the moment the operator is
	// least able to fix it. So an exceeded bound is LOUD and still serving. The one
	// thing it must never be is silent.
	//
	// ⚠ ZERO MEANS NO DECLARED BOUND, and `Staleness.Exceeded` is then structurally
	// always false. That is a reassuring zero of exactly the kind this repository's
	// rules refuse to read as evidence, so the render prints `bound=none` rather than
	// `bound=0s`: a reader must not be able to mistake "no bound was declared" for
	// "the bound was met".
	MaxAge time.Duration

	// Now is the clock. Injected so a test can pin an age; nil means
	// `time.Now().UTC()`.
	Now func() time.Time
}

// NewCache builds a cache over src. It does NOT contact src.
//
// 🔴 THE CACHE STARTS UNMATERIALIZED AND AUTHORISES NOBODY, WHICH IS THE FAIL-CLOSED
// DIRECTION AND IS NOT THE SAME STATE AS "STALE". A zero `Model` has no credentials,
// so `Authenticate` refuses every token — the same answer `Authorization`'s zero value
// gives, deliberately. An outage AFTER a successful materialization keeps serving; an
// outage BEFORE one has nothing to serve, and conflating the two would produce a cache
// that authorises nobody while reporting itself healthy. `Staleness.Status` says
// `unmaterialized` for exactly that reason.
//
// Materializing is the caller's first `Refresh`, so a startup failure is the caller's
// to shout about rather than something a constructor swallowed.
func NewCache(src Source, opts CacheOptions) *Cache {
	return &Cache{src: src, maxAge: opts.MaxAge, now: opts.Now}
}

func (c *Cache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now().UTC()
}

// RefreshReason names WHICH trigger caused the last refresh attempt.
//
// 🔴 IT EXISTS BECAUSE "THE CACHE REFRESHED" CANNOT DISTINGUISH THREE MECHANISMS. An
// operator who sends SIGHUP and sees the epoch move has learned nothing about whether
// the signal did it — the timer fires on its own schedule and would have produced the
// identical observable. A refresh that does not say what woke it is an empty result
// in the sense this repository's rules mean: the observable that the most causes
// share, and therefore the one that identifies none of them.
type RefreshReason string

const (
	// RefreshExplicit is a direct `Refresh` call — startup, and the "on change"
	// trigger when the caller already knows the world moved.
	RefreshExplicit RefreshReason = "explicit"
	// RefreshTimer is the scheduled tick that keeps the declared bound.
	RefreshTimer RefreshReason = "timer"
	// RefreshSignal is SIGHUP. Operators already have that muscle memory from the
	// token file, which is the whole reason it is a trigger here.
	RefreshSignal RefreshReason = "signal"
	// RefreshChange is a change notification from the authority side.
	RefreshChange RefreshReason = "change"
	// RefreshWrite is `ApplyNow`: this process wrote, so this process already holds
	// the new Model and materialized from it without a read.
	RefreshWrite RefreshReason = "write"
	// RefreshNever is the zero value: no attempt has been made.
	RefreshNever RefreshReason = "never"
)

// Refresh re-materializes from the authority. This is the explicit/on-change trigger.
//
// 🔴 ON FAILURE THE PREVIOUS MODEL KEEPS SERVING AND `materializedAt` DOES NOT MOVE.
// Both halves matter and the second is the one that is easy to get wrong: advancing
// the timestamp on a failed attempt would reset the reported age to zero on every
// failure, so a cache whose authority has been dead for a week would report itself
// seconds old — a staleness report that is *most* wrong exactly when it is most
// needed. The attempt is recorded separately (`LastAttempt`, `Failures`) so the two
// facts stay distinguishable.
func (c *Cache) Refresh(ctx context.Context) error { return c.refresh(ctx, RefreshExplicit) }

func (c *Cache) refresh(ctx context.Context, why RefreshReason) error {
	// Outside the lock. See the `mu` comment: a hung authority must not block reads.
	m, err := c.src.Model(ctx)
	now := c.clock()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastAttempt = now
	c.lastTrigger = why
	if err != nil {
		c.failures++
		c.lastErr = err
		return err
	}
	c.model = m
	c.materialized = true
	c.materializedAt = now
	c.refreshes++
	c.lastErr = nil
	return nil
}

// Model is the materialized world. The hot path's only read.
//
// It returns the `Model` VALUE, which is six map headers over shared buckets — that
// is safe here for the reason `Model`'s own comment gives: a Model is never mutated
// after construction, and a refresh REPLACES it rather than editing it.
func (c *Cache) Model() Model {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.model
}

// Authenticate resolves a bearer token against the materialized world.
//
// It is `control.Authenticate` over `c.Model()` and deliberately nothing else: the
// cache decides WHEN the world was read, never WHAT it permits.
func (c *Cache) Authenticate(token string) (Principal, Authorization, error) {
	return Authenticate(c.Model(), token)
}

// Resolve computes a principal's authority from the materialized world.
func (c *Cache) Resolve(p Principal) Authorization { return Resolve(c.Model(), p) }

// CacheStatus is the one word a status surface shows.
type CacheStatus string

const (
	// CacheUnmaterialized: no successful materialization has ever happened, so this
	// cache authorises nobody. Not an outage — a cold start that has not completed.
	CacheUnmaterialized CacheStatus = "unmaterialized"
	// CacheStale: the age is past the declared bound. Still serving, loudly.
	CacheStale CacheStatus = "stale"
	// CacheDegraded: the last refresh ATTEMPT failed, but the age is still inside the
	// bound. Serving last-known-good with the authority unreachable.
	CacheDegraded CacheStatus = "degraded"
	// CacheFresh: materialized, inside the bound, last attempt succeeded.
	CacheFresh CacheStatus = "fresh"
)

// Staleness is the epoch and its age, as a VALUE.
//
// 🔴 A VALUE RATHER THAN A LOG LINE, BECAUSE A CACHE THAT SILENTLY SERVES A REVOKED
// GRANT IS PRECISELY THE INSTRUMENT THAT CANNOT SEE THE THING IT IS TRUSTED FOR. A
// log line is read by whoever happens to be tailing; a value can be rendered into a
// status surface, compared in a test, and asserted on. Every field here is derived
// from the same locked read, so no two of them can describe different instants.
//
// ⚠ `LastError` IS A FIELD AND IS NOT IN `String()`, DELIBERATELY. The text comes from
// the authority — an OS error naming a path, a journal parse failure quoting a line —
// and this repository has already paid for letting un-authored text reach an operator
// stream: a newline in there is a second, syntactically perfect log line of somebody
// else's choosing (see `reloadSafe` in `cmd/cairn-server`). `String()` therefore
// carries the BOOLEAN (`failing=true`) and the caller that wants the text sanitises it
// with whatever its own stream requires. Duplicating a sanitiser here would be a
// second copy of a predicate that already exists, which is how the two drift.
type Staleness struct {
	// Status is the one word, derived from the booleans below. It cannot disagree
	// with them because it is computed from them.
	Status CacheStatus
	// Materialized is false until a refresh has succeeded at least once.
	Materialized bool
	// Epoch is the Model epoch being served. Zero when unmaterialized.
	Epoch uint64
	// ModelAt is the timestamp of the last event in the served Model — the model's
	// own clock, not the reader's. Distinct from MaterializedAt: a world nobody has
	// written to for a month is old in this sense and perfectly fresh in the other.
	ModelAt time.Time
	// MaterializedAt is when this copy was built. Moves only on success.
	MaterializedAt time.Time
	// Age is now minus MaterializedAt: the revocation lag this cache is carrying.
	// Zero when unmaterialized, where it would otherwise be "the age of the epoch
	// zero", which is not a thing.
	Age time.Duration
	// Bound is the declared MaxAge. Zero means none was declared.
	Bound time.Duration
	// Exceeded is Age > Bound. Structurally false when Bound is zero — see
	// CacheOptions.MaxAge for why the render refuses to let that read as "met".
	Exceeded bool
	// Failing is true when the LAST refresh attempt failed. Independent of Exceeded:
	// an authority that died one second ago is failing and not yet exceeded, and a
	// process descheduled past its bound is exceeded and not failing.
	Failing bool
	// LastError is the last refresh failure, or nil. See the type comment for why it
	// is not rendered.
	LastError error
	// LastAttempt is when a refresh was last TRIED, successful or not.
	LastAttempt time.Time
	// LastTrigger is what woke that attempt. `never` before the first one.
	LastTrigger RefreshReason
	// Refreshes and Failures count successful and failed attempts since start.
	Refreshes uint64
	Failures  uint64
}

// Staleness reports the epoch and its age at this instant.
func (c *Cache) Staleness() Staleness {
	now := c.clock()
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stalenessLocked(now)
}

func (c *Cache) stalenessLocked(now time.Time) Staleness {
	s := Staleness{
		Materialized: c.materialized,
		Bound:        c.maxAge,
		Failing:      c.lastErr != nil,
		LastError:    c.lastErr,
		LastAttempt:  c.lastAttempt,
		LastTrigger:  c.lastTrigger,
		Refreshes:    c.refreshes,
		Failures:     c.failures,
	}
	if s.LastTrigger == "" {
		s.LastTrigger = RefreshNever
	}
	if c.materialized {
		s.Epoch = c.model.Epoch
		s.ModelAt = c.model.At
		s.MaterializedAt = c.materializedAt
		s.Age = now.Sub(c.materializedAt)
		// A bound of zero declares nothing, so nothing can exceed it. Strict `>`:
		// an age exactly equal to the bound is the bound being MET, and a schedule
		// that refreshes every MaxAge is meant to be a legal configuration rather
		// than one that reports itself stale on every tick.
		s.Exceeded = c.maxAge > 0 && s.Age > c.maxAge
	}
	switch {
	case !s.Materialized:
		s.Status = CacheUnmaterialized
	case s.Exceeded:
		s.Status = CacheStale
	case s.Failing:
		s.Status = CacheDegraded
	default:
		s.Status = CacheFresh
	}
	return s
}

// String renders one line for a status surface.
//
// The order is fixed and every key is always present, so a reader can split on
// whitespace and on `=` without a schema — and, more to the point, so a missing field
// is a visible absence rather than a shorter line that still parses.
func (s Staleness) String() string {
	var b strings.Builder
	b.WriteString("authz-cache: status=")
	b.WriteString(string(s.Status))
	b.WriteString(" epoch=")
	if s.Materialized {
		b.WriteString(strconv.FormatUint(s.Epoch, 10))
	} else {
		b.WriteString("n/a")
	}
	b.WriteString(" age=")
	if s.Materialized {
		// Rounded to the millisecond: an unrounded `time.Duration` prints nine
		// significant places of noise on a real clock. NOT rounded to the second —
		// that renders 500ms as `0s`, which reads as "just refreshed" and is the one
		// direction a staleness report must never round.
		b.WriteString(s.Age.Round(time.Millisecond).String())
	} else {
		b.WriteString("n/a")
	}
	b.WriteString(" bound=")
	if s.Bound > 0 {
		b.WriteString(s.Bound.String())
	} else {
		b.WriteString("none")
	}
	b.WriteString(" materialized=")
	b.WriteString(renderTime(s.MaterializedAt))
	b.WriteString(" trigger=")
	b.WriteString(string(s.LastTrigger))
	b.WriteString(" refreshes=")
	b.WriteString(strconv.FormatUint(s.Refreshes, 10))
	b.WriteString(" failures=")
	b.WriteString(strconv.FormatUint(s.Failures, 10))
	b.WriteString(" failing=")
	b.WriteString(strconv.FormatBool(s.Failing))
	return b.String()
}

func renderTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.UTC().Format(time.RFC3339)
}

// RefreshTriggers is the set of things that wake a running cache.
//
// 🔴 THE CACHE DOES NOT INSTALL A SIGNAL HANDLER; IT RECEIVES A CHANNEL. `signal.Notify`
// is process-global state, and a library that calls it takes a decision away from the
// program that owns the process — including the decision to have a handler at all,
// which on an ordinary process is the difference between a SIGHUP that reloads and one
// that terminates. `cmd/cairn-server` already owns that call for the token file; this
// takes the channel so both can live in one program.
//
// ⚠ TWO `signal.Notify` REGISTRATIONS ON ONE SIGNAL BOTH RECEIVE IT — they are not a
// choice between handlers. Measured rather than assumed, by
// `TestTwoNotifyChannelsBothReceiveOneSIGHUP`, because the opposite belief ("the token
// reload will swallow it") is the plausible one and it would make this trigger silently
// dead in the only program that has both.
type RefreshTriggers struct {
	// Interval is the timer. Zero disables it.
	Interval time.Duration
	// Signals is SIGHUP (or whatever the caller registered). Nil disables it.
	Signals <-chan os.Signal
	// OnChange is a change notification from the authority side. Nil disables it.
	OnChange <-chan struct{}
}

// ErrRefreshCannotKeepBound refuses a schedule that cannot keep the bound it declares.
//
// 🔴 A BOUND NOTHING KEEPS IS WORSE THAN NO BOUND, because it is READ as a promise. A
// cache configured to refresh every five minutes while declaring a one-minute staleness
// bound spends four minutes of every five reporting `stale` — and the operator who
// tuned the interval without touching the bound will read that as a broken report
// rather than as the configuration they wrote.
//
// ⚠ IT IS A CHECK ON THE SCHEDULE, NOT ON THE WALL CLOCK, AND THE DIFFERENCE IS REAL.
// A refresh takes time, a process gets descheduled, a clock steps. Passing this says
// the schedule is CAPABLE of keeping the bound; `Staleness.Exceeded` is what says
// whether it did. Both exist because neither answers the other's question.
var ErrRefreshCannotKeepBound = errors.New("control: the refresh interval cannot keep the declared staleness bound")

// Run refreshes on every trigger until ctx is done, and returns ctx.Err() then.
//
// 🔴 A FAILED REFRESH DOES NOT STOP THE LOOP. Returning on the first error would turn
// a transient authority outage into a permanently stale cache that never tries again —
// the failure would be reported once, by a goroutine nobody is watching, and the age
// would then grow forever with the mechanism that could fix it already dead. The error
// is recorded in `Staleness` (`Failing`, `Failures`, `LastError`), which is the
// reporting surface this piece exists to provide.
//
// ⚠ A NIL CHANNEL IN A `select` IS NEVER READY, which is how `Signals` and `OnChange`
// are disabled. That is a Go property rather than a trick, and it is stated because a
// nil channel in a receive looks like a nil-dereference bug to a reader who has not met
// it.
func (c *Cache) Run(ctx context.Context, tr RefreshTriggers) error {
	if tr.Interval > 0 && c.maxAge > 0 && tr.Interval > c.maxAge {
		return fmt.Errorf("%w: interval %s, bound %s", ErrRefreshCannotKeepBound, tr.Interval, c.maxAge)
	}

	var tick <-chan time.Time
	if tr.Interval > 0 {
		ticker := time.NewTicker(tr.Interval)
		defer ticker.Stop()
		tick = ticker.C
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick:
			_ = c.refresh(ctx, RefreshTimer)
		case <-tr.Signals:
			_ = c.refresh(ctx, RefreshSignal)
		case <-tr.OnChange:
			_ = c.refresh(ctx, RefreshChange)
		}
	}
}

// Effect says whether a write is IN FORCE on this cache yet.
type Effect string

const (
	// EffectImmediate: the cache is already serving the written epoch. What a UI may
	// render as "revoked" with no qualifier.
	EffectImmediate Effect = "immediate"
	// EffectDeferred: the authority holds it, this cache does not yet. What a UI must
	// render as "revoked, effective by <time>" — the qualifier is not optional, and
	// omitting it is the sharing dialog implying a guarantee the system cannot make.
	EffectDeferred Effect = "deferred"
)

// ErrAuthorityReadOnly is what a write gets when the Cache was built over a plain
// Source. A Source is the read half by design; refusing here names the missing half
// instead of panicking on a type assertion.
var ErrAuthorityReadOnly = errors.New("control: the authority behind this cache is read-only")

// ErrNoEvents refuses a write of nothing.
//
// A zero-event append is a no-op that would still return a WriteResult, and that
// result would claim an Effect about a change nobody made — the one input that can
// make `Effect == EffectImmediate` true without anything having happened. Refusing it
// is what keeps the invariant below structural.
var ErrNoEvents = errors.New("control: a write with no events")

// WriteResult is what a caller tells the user.
//
// 🔴 THE INVARIANT IS STRUCTURAL: `Effect == EffectImmediate` EXACTLY WHEN
// `ServingEpoch >= WrittenEpoch`. The Effect is DERIVED from the two epochs, never
// from which method was called, and that is the design decision rather than an
// implementation detail. A deferred write that a concurrent refresh has already picked
// up IS in force, and labelling it by its code path would have the UI say "effective
// within 60s" about something that already happened. A caller that does not trust the
// label can check it against the numbers beside it.
type WriteResult struct {
	Effect Effect
	// ServingEpoch is the epoch this cache is serving as the call returns.
	ServingEpoch uint64
	// WrittenEpoch is the epoch the AUTHORITY holds as the call returns. It comes
	// from `Writer.Append`'s return rather than from a read-after-write, which is the
	// reason that return exists.
	WrittenEpoch uint64
	// EffectiveBy is the instant by which this cache is guaranteed to be serving
	// WrittenEpoch. The call's own clock reading when the effect is immediate;
	// `MaterializedAt + Bound` when deferred; the ZERO TIME when no bound is
	// declared, which a renderer must show as `unbounded` rather than as a date.
	EffectiveBy time.Time
}

// Immediate is the question a UI actually asks.
func (r WriteResult) Immediate() bool { return r.Effect == EffectImmediate }

// String renders one line for a status surface or an audit record.
func (r WriteResult) String() string {
	by := "unbounded"
	switch {
	case r.Effect == EffectImmediate:
		by = "now"
	case !r.EffectiveBy.IsZero():
		by = r.EffectiveBy.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("authz-write: effect=%s serving-epoch=%d written-epoch=%d effective-by=%s",
		r.Effect, r.ServingEpoch, r.WrittenEpoch, by)
}

// Apply writes to the authority and lets the ordinary refresh schedule pick it up.
//
// This is the cheap path and the right default: it does one durable write and no
// re-materialization, so a batch of grants does not rebuild the world once per grant.
// The caller learns from `WriteResult.Effect` that the change is not in force here yet
// and from `EffectiveBy` when it will be.
func (c *Cache) Apply(ctx context.Context, events ...Event) (WriteResult, error) {
	return c.write(ctx, false, events)
}

// ApplyNow writes to the authority and re-materializes BEFORE returning.
//
// 🔴 THIS IS THE "REVOKE NOW" PATH, AND ITS ONLY PROMISE IS ABOUT THIS PROCESS. When
// it returns with `EffectImmediate`, this cache is serving the written epoch, so no
// subsequent read through it can honour the revoked grant. It says nothing about
// another replica's cache, and nothing at all about the copy of the entries already on
// somebody's laptop — revoking access stops future syncs and does not recall a replica.
//
// 🔴 IT MATERIALIZES FROM `Append`'s RETURN RATHER THAN RE-READING. A read-after-write
// against any backend with replication can observe a state OLDER than the one just
// created, which would make the synchronous path silently deferred — the one failure
// this method exists to rule out. `Writer.Append` returns the Model it produced for
// exactly this reason.
func (c *Cache) ApplyNow(ctx context.Context, events ...Event) (WriteResult, error) {
	return c.write(ctx, true, events)
}

func (c *Cache) write(ctx context.Context, materialize bool, events []Event) (WriteResult, error) {
	w, writable := c.src.(Writer)
	if !writable {
		return WriteResult{}, ErrAuthorityReadOnly
	}
	if len(events) == 0 {
		return WriteResult{}, ErrNoEvents
	}

	// 🔴 A WRITE REFUSES CLEANLY WHEN THE AUTHORITY IS DOWN, AND "CLEANLY" MEANS THE
	// CACHE IS NOT TOUCHED. Reads go on being served from the last-known-good copy at
	// the same epoch and the same age; nothing here is rolled back because nothing
	// here was speculatively applied. The asymmetry is the design: reads survive an
	// outage, writes do not, and a write that appeared to succeed against a dead
	// authority is a revocation the operator believes landed.
	next, err := w.Append(ctx, events...)
	if err != nil {
		return WriteResult{}, fmt.Errorf("control cache: %w", err)
	}

	now := c.clock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if materialize {
		c.model = next
		c.materialized = true
		c.materializedAt = now
		c.refreshes++
		c.lastErr = nil
		c.lastAttempt = now
		c.lastTrigger = RefreshWrite
	}

	serving := c.model.Epoch
	if !c.materialized {
		serving = 0
	}
	if serving >= next.Epoch {
		return WriteResult{
			Effect:       EffectImmediate,
			ServingEpoch: serving,
			WrittenEpoch: next.Epoch,
			EffectiveBy:  now,
		}, nil
	}
	return WriteResult{
		Effect:       EffectDeferred,
		ServingEpoch: serving,
		WrittenEpoch: next.Epoch,
		EffectiveBy:  c.effectiveByLocked(),
	}, nil
}

// effectiveByLocked is the deadline the declared bound promises.
//
// Zero — rendered `unbounded` — when there is no bound, and also when nothing has
// been materialized: an unmaterialized cache has not started the clock the bound is
// measured against, so it has no deadline to offer. Inventing `now + bound` there
// would be a promise derived from a timestamp that does not exist.
func (c *Cache) effectiveByLocked() time.Time {
	if c.maxAge <= 0 || !c.materialized {
		return time.Time{}
	}
	return c.materializedAt.Add(c.maxAge)
}
