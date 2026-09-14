package netid

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"sync"
	"time"
)

// The three rate-limit defaults live HERE, in code, so a deployment that sets no
// environment still gets them; each is overridable.
const (
	DefaultMaxFailures   = 5
	DefaultFailureWindow = 60 * time.Second
	DefaultLockout       = 900 * time.Second
)

const (
	EnvMaxFailures   = "SUBSYSTEM_STORE_MAX_FAILURES"
	EnvFailureWindow = "SUBSYSTEM_STORE_FAILURE_WINDOW_S"
	EnvLockout       = "SUBSYSTEM_STORE_LOCKOUT_S"
)

// REAL bounds on both tables. Active lockouts are NEVER evicted for space; the
// lockout table is instead capped, and at the cap new lockouts are refused rather
// than old ones released.
const (
	MaxTrackedClients  = 4096
	MaxTrackedLockouts = 16384
)

// LimiterSettings reads the three knobs from the environment, or returns an error.
//
// 🔴 IT REFUSES RATHER THAN SILENTLY DEFAULTING. A typo'd `MAX_FAILURES=fve` that
// quietly became 5 is an operator believing a setting took effect. A
// misconfiguration at startup is visible in a CrashLoopBackOff; one that defaults is
// invisible forever.
//
// 🔴 `nan` AND `inf` PARSE, AND BOTH WALK STRAIGHT THROUGH `<= 0` (`nan <= 0` is
// false). Measured consequences on the Python side: a nan WINDOW silently disables
// the limiter entirely, because every recorded failure compares as outside it; a nan
// or inf LOCKOUT makes it permanent. Both are exactly the invisible
// misconfiguration this function exists to prevent, arriving through the one
// comparison that does not order them.
func LimiterSettings(env map[string]string) (maxFailures int, window, lockout time.Duration, err error) {
	maxFailures = DefaultMaxFailures
	window = DefaultFailureWindow
	lockout = DefaultLockout

	if raw, present := env[EnvMaxFailures]; present && raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil {
			return 0, 0, 0, fmt.Errorf("%s must be a number, got '%s'", EnvMaxFailures, raw)
		}
		if n <= 0 {
			return 0, 0, 0, fmt.Errorf("%s must be positive, got '%s'", EnvMaxFailures, raw)
		}
		maxFailures = n
	}
	for _, spec := range []struct {
		name   string
		target *time.Duration
	}{{EnvFailureWindow, &window}, {EnvLockout, &lockout}} {
		raw, present := env[spec.name]
		if !present || raw == "" {
			continue
		}
		f, convErr := strconv.ParseFloat(raw, 64)
		if convErr != nil {
			return 0, 0, 0, fmt.Errorf("%s must be a number, got '%s'", spec.name, raw)
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, 0, 0, fmt.Errorf("%s must be a finite number, got '%s'", spec.name, raw)
		}
		if f <= 0 {
			return 0, 0, 0, fmt.Errorf("%s must be positive, got '%s'", spec.name, raw)
		}
		*spec.target = secondsToDuration(f)
	}
	return maxFailures, window, lockout, nil
}

// maxDurationSeconds is the largest number of seconds `time.Duration` can hold, as a
// float64. `time.Duration` is int64 NANOSECONDS, so the ceiling is ~9.223e9 seconds —
// about 292 years.
const maxDurationSeconds = float64(math.MaxInt64) / float64(time.Second)

// secondsToDuration converts a positive, finite seconds value to a Duration, SATURATING
// instead of overflowing.
//
// 🔴 `time.Duration(f * float64(time.Second))` IS NOT A CONVERSION FOR LARGE `f` — IT IS
// AN UNDEFINED-BEHAVIOUR SIGN FLIP, AND IT DISABLED THE LOCKOUT WHILE LOGGING ONE. Go
// leaves a float-to-integer conversion implementation-defined when the value does not fit;
// on amd64 it yields `math.MinInt64`. MEASURED here, at both sides of the boundary rather
// than at one point, because the whole defect lives at the boundary:
//
//	LOCKOUT_S=9.2e9  -> +2555555h33m20s   RecordFailure=true   LockedOut=TRUE
//	LOCKOUT_S=9.3e9  -> -2562047h47m16s   RecordFailure=true   LockedOut=FALSE
//	LOCKOUT_S=1e10   -> -2562047h47m16s   RecordFailure=true   LockedOut=FALSE
//	LOCKOUT_S=1e300  -> -2562047h47m16s   RecordFailure=true   LockedOut=FALSE
//
// The third column is the finding. `RecordFailure` reports that it STARTED a lockout — so
// the audit log writes `status=lockout-triggered`, the one line an operator alerts on —
// and `lockedUntil[key]` is set to a time 292 years in the PAST, so `LockedOut` finds it
// expired, deletes it, and answers false. Worse, taking that branch also
// `delete(l.failures, key)`: the streak is wiped every `MaxFailures` failures, so no state
// ever accumulates and the brute force is UNLIMITED. A guard that reports itself active
// while being inert is worse than no guard, because it is what the alert is wired to.
//
// ⚠ THE WINDOW OVERFLOWS TOO, IN THE OPPOSITE DIRECTION, AND MEASURING IT IS WHAT STOPPED
// A WRONG COMMENT HERE. `FAILURE_WINDOW_S=9.3e9` also yields `MinInt64`, and negating
// `MinInt64` is `MinInt64` again, so `now.Add(-l.Window)` puts the cutoff 292 years in the
// PAST and nothing is ever pruned: the window becomes effectively infinite, which makes
// the limiter STRICTER rather than disabled. Measured, not assumed — the first draft of
// this comment said it disabled the limiter, by analogy with the lockout, and that was
// wrong. Both are fixed by this one function because both are the same conversion.
//
// 🔴 SATURATING RATHER THAN REFUSING, AND THAT IS NOT A RETREAT FROM
// `LimiterSettings`' "it refuses rather than silently defaulting". Defaulting throws the
// operator's value away and substitutes a DIFFERENT, smaller one; saturating gives them
// the largest value the type can express, which for a lockout is 292 years — behaviourally
// indistinguishable from the 317 years `LOCKOUT_S=1e10` asks for, and from the ~3e292
// years `1e300` asks for, in any run anyone will observe. Refusing would be a NEW
// divergence: the oracle's `now + lockout_s` is Python float arithmetic with no such
// ceiling, so it accepts these values and serves.
func secondsToDuration(seconds float64) time.Duration {
	if seconds >= maxDurationSeconds {
		return math.MaxInt64
	}
	return time.Duration(seconds * float64(time.Second))
}

// RateLimiter is N failed auths per client per window, then a lockout.
//
// This is the innermost of three layers and the only one that knows an auth
// actually FAILED rather than that a request arrived.
//
// The clock is injectable so a test can prove the window and the lockout EXPIRE,
// rather than sleeping fifteen minutes or asserting only the easy half.
type RateLimiter struct {
	MaxFailures int
	Window      time.Duration
	Lockout     time.Duration
	// Now is the clock. nil means `time.Now`, resolved on every read so a
	// zero-value RateLimiter is usable.
	Now func() time.Time

	mu          sync.Mutex
	failures    map[string][]time.Time
	lockedUntil map[string]time.Time
}

func NewRateLimiter(maxFailures int, window, lockout time.Duration) *RateLimiter {
	return &RateLimiter{MaxFailures: maxFailures, Window: window, Lockout: lockout}
}

func (l *RateLimiter) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *RateLimiter) ensure() {
	if l.failures == nil {
		l.failures = map[string][]time.Time{}
	}
	if l.lockedUntil == nil {
		l.lockedUntil = map[string]time.Time{}
	}
}

// LockedOut reports whether `key` is serving a lockout. Expired lockouts are
// dropped.
func (l *RateLimiter) LockedOut(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensure()
	until, present := l.lockedUntil[key]
	if !present {
		return false
	}
	if !until.After(now) {
		delete(l.lockedUntil, key)
		delete(l.failures, key)
		return false
	}
	return true
}

// RecordFailure counts one failed auth and reports whether THIS one started a
// lockout.
func (l *RateLimiter) RecordFailure(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ensure()

	cutoff := now.Add(-l.Window)
	var recent []time.Time
	for _, t := range l.failures[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	if len(recent) >= l.MaxFailures {
		// 🔴 PRUNE EXPIRED LOCKOUTS BEFORE ASKING WHETHER THERE IS ROOM. Found by
		// a test on the Python side, not by reading: eviction ran AFTER this check,
		// so a table full of ALREADY-EXPIRED entries reported "no room" and refused
		// a lockout that should have been granted. Once the cap was reached once,
		// that persisted for as long as new failures kept arriving — the cap
		// turning into a permanent disable of the whole lockout mechanism.
		for k, until := range l.lockedUntil {
			if !until.After(now) {
				delete(l.lockedUntil, k)
			}
		}
		_, already := l.lockedUntil[key]
		if already || len(l.lockedUntil) < MaxTrackedLockouts {
			l.lockedUntil[key] = now.Add(l.Lockout)
			delete(l.failures, key)
			l.evict(now)
			return true
		}
		// 🔴 THE TABLE IS FULL, SO NO LOCKOUT WAS CREATED — SAY SO. The first
		// Python version returned true here regardless, and popped the streak.
		// Measured consequences, both bad: the audit log wrote
		// `status=lockout-triggered` for a client that was NOT locked out (the log
		// lying about the one event the operator alerts on), and popping the streak
		// every `MaxFailures` failures meant no state accumulated at all —
		// unlimited brute force, for an attacker who had first filled the table.
		//
		// Keeping the streak is what makes this degrade safely: the client stays AT
		// the threshold, so every subsequent failure retries the lockout and takes
		// it the moment a slot frees.
		l.failures[key] = recent
		l.evict(now)
		return false
	}
	l.failures[key] = recent
	l.evict(now)
	return false
}

// RecordSuccess is 🔴 DELIBERATELY A NO-OP. A success does NOT forgive a failure
// streak.
//
// It used to, on the Python side. That was an invention rather than the
// specification — which says five failed auths per client per minute — and it
// created two attacks, both of which turn on the key being an ADDRESS rather than an
// identity: an attacker holding ANY accepted token (including the old one that
// overlap rotation deliberately keeps live) interleaves one success per four guesses
// and brute-forces the rest of the set forever; and an attacker sharing a NAT with a
// legitimate client is never locked out at all, because the victim's ordinary
// traffic keeps resetting the counter on their behalf.
//
// The sliding window already provides the forgiveness this was reaching for. Kept as
// a method rather than deleted so the call site still reads as a decision.
func (l *RateLimiter) RecordSuccess(string) {}

// evict bounds BOTH tables. Called with the lock held.
//
// 🔴 THIS USED TO BE A BOUND IN NAME ONLY on the Python side, and the comment
// saying otherwise was false. It dropped only entries whose whole streak had already
// aged out of the window — so INSIDE the window nothing was evictable and the table
// grew without limit (measured: 20,000 keys against a cap of 4,096), while the
// lockout table had no cap at all (measured: 5,000).
//
// Now: expired lockouts go first (free), then aged-out failure streaks, and only if
// the table is STILL over the cap are the oldest live streaks dropped —
// oldest-first, so the client closest to being locked out is the last to be
// forgotten.
//
// ⚠ ACTIVE LOCKOUTS ARE STILL NEVER DROPPED FOR SPACE. Evicting one is a bypass
// dressed as memory hygiene. The lockout table is instead bounded by construction:
// an entry costs an attacker `MaxFailures` requests to create, and each one expires
// on its own; at the cap, new lockouts are refused rather than old ones released — a
// bounded, stated failure mode.
func (l *RateLimiter) evict(now time.Time) {
	for key, until := range l.lockedUntil {
		if !until.After(now) {
			delete(l.lockedUntil, key)
		}
	}
	if len(l.failures) <= MaxTrackedClients {
		return
	}
	cutoff := now.Add(-l.Window)
	for key, times := range l.failures {
		if len(times) == 0 || !times[len(times)-1].After(cutoff) {
			delete(l.failures, key)
		}
	}
	if len(l.failures) <= MaxTrackedClients {
		return
	}
	// Still over: drop the OLDEST live streaks. The slice is append-ordered, so its
	// last element is that key's most recent failure — no scan of the list.
	keys := make([]string, 0, len(l.failures))
	for key := range l.failures {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int {
		ta, tb := l.failures[a], l.failures[b]
		return ta[len(ta)-1].Compare(tb[len(tb)-1])
	})
	for _, key := range keys[:len(l.failures)-MaxTrackedClients] {
		delete(l.failures, key)
	}
}
