package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// recordedRun drives `KeySet.Run` with NO real waiting: every wait it asks for is recorded
// and fired at once, and after `waits` of them the context is cancelled, so `Run` returns
// on its own `ctx.Done()` arm.
//
// 🔴 THE SCHEDULE IS ASSERTED AS THE SEQUENCE OF DURATIONS `Run` ASKED FOR, NEVER BY
// SLEEPING. A test that slept through a backoff would need real seconds per step, and a
// test that shortened the interval to make that cheap would sit exactly on the boundary
// between "backoff" and "interval" where the two schedules cannot be told apart.
type recordedRun struct {
	asked []time.Duration
	lines []string
}

func runRecorded(t *testing.T, set *KeySet, waits int) *recordedRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &recordedRun{}
	set.after = func(d time.Duration) (<-chan time.Time, func()) {
		rec.asked = append(rec.asked, d)
		if len(rec.asked) > waits {
			cancel()
			return make(chan time.Time), func() {}
		}
		fired := make(chan time.Time, 1)
		fired <- time.Time{}
		return fired, func() {}
	}
	done := make(chan error, 1)
	go func() { done <- set.Run(ctx, func(line string) { rec.lines = append(rec.lines, line) }) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
	rec.asked = rec.asked[:waits]
	return rec
}

// newScriptedJWKSServer is `newJWKSServer` whose n-th request (1-based) answers 503 when
// fail(n) says so. The script is fixed at construction rather than swapped onto a running
// server, so the handler is never written while a request goroutine may read it.
func newScriptedJWKSServer(t *testing.T, body []byte, fail func(n int64) bool) (*jwksServer, *atomic.Int64) {
	t.Helper()
	var seen atomic.Int64
	j := &jwksServer{}
	j.body.Store(&body)
	j.code.Store(200)
	j.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail(seen.Add(1)) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(*j.body.Load())
	}))
	t.Cleanup(j.srv.Close)
	return j, &seen
}

// TestAKeySetThatHasNeverFetchedRetriesOnABackoffNotTheInterval.
//
// 🔴 THE DEFECT: `Run` ticked every `DefaultJWKSInterval` whatever the state, so a key set
// whose first fetch failed waited ten minutes for its second — and `cmd/cairn-ui` withholds
// the provider button until a fetch succeeds, so the door stayed shut for one or two whole
// intervals after the provider became reachable. The shape mirrors the deployment: the
// caller's startup `Refresh` fails, then `Run` starts.
//
// The fixture's failure count (3) and the expected sequence are chosen so every rung is a
// distinct value and none equals the interval: a mutant that keeps the ticker produces
// [10m 10m 10m …], one that never doubles produces [5s 5s 5s …], one that never returns to
// the interval after success produces a short wait in the fourth slot.
func TestAKeySetThatHasNeverFetchedRetriesOnABackoffNotTheInterval(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	provider, requests := newScriptedJWKSServer(t, jwksDocumentOf(t, s.publicJWK(t)), func(n int64) bool { return n <= 3 })
	set, _ := newLiveKeySet(t, provider)

	if err := set.Refresh(context.Background()); err == nil {
		t.Fatal("precondition: the startup fetch must fail")
	}
	rec := runRecorded(t, set, 5)

	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, DefaultJWKSInterval, DefaultJWKSInterval}
	if !reflect.DeepEqual(rec.asked, want) {
		t.Fatalf("Run waited %v, want %v — a never-fetched key set must retry on a doubling backoff and return to the interval once a fetch succeeds", rec.asked, want)
	}
	if st := set.Status(); !st.Fetched || st.Failing {
		t.Fatalf("after the provider recovered the set is fetched=%t failing=%t", st.Fetched, st.Failing)
	}
	// The positive control on the fixture: 1 startup + 2 failed retries + 1 success +
	// 2 interval refreshes. The provider really was asked, so the schedule above is a fact
	// about fetches and not about a loop that never refreshed.
	if got := requests.Load(); got != 6 {
		t.Fatalf("the provider saw %d requests, want 6", got)
	}
}

// TestTheBackoffIsCappedAtTheInterval: a provider that stays down is asked about as often
// as a healthy one once the doubling reaches the interval, never more rarely.
func TestTheBackoffIsCappedAtTheInterval(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	provider := newJWKSServer(t, jwksDocumentOf(t, s.publicJWK(t)))
	provider.code.Store(503)
	set, _ := newLiveKeySet(t, provider)
	_ = set.Refresh(context.Background())

	rec := runRecorded(t, set, 9)
	want := []time.Duration{
		5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second,
		80 * time.Second, 160 * time.Second, 320 * time.Second,
		DefaultJWKSInterval, DefaultJWKSInterval,
	}
	if !reflect.DeepEqual(rec.asked, want) {
		t.Fatalf("Run waited %v, want %v", rec.asked, want)
	}
}

// TestTheBackoffRestartsAtItsFloorAfterASuccess: a doubled backoff from one failing run must
// not carry into the next. Fails, fails, succeeds, fails: the second run starts at 5s again,
// not at the 20s the first run had reached.
func TestTheBackoffRestartsAtItsFloorAfterASuccess(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	provider, _ := newScriptedJWKSServer(t, jwksDocumentOf(t, s.publicJWK(t)), func(n int64) bool { return n != 3 })
	set, _ := newLiveKeySet(t, provider)
	_ = set.Refresh(context.Background())

	rec := runRecorded(t, set, 5)
	want := []time.Duration{5 * time.Second, 10 * time.Second, DefaultJWKSInterval, 5 * time.Second, 10 * time.Second}
	if !reflect.DeepEqual(rec.asked, want) {
		t.Fatalf("Run waited %v, want %v", rec.asked, want)
	}
}

// TestAnIntervalShorterThanTheBackoffFloorCapsIt: a deployment configured with an interval
// below `jwksRetryMin` must never wait LONGER while failing than while healthy. 3s is
// chosen below the 5s floor and not a power-of-two fraction of it, so the cap is what
// produces every value here. ⚠ AN INVARIANT GUARD: the pre-change loop always waited the
// interval, so it passed this too; it exists because the mutation sweep showed the cap
// otherwise unpinned.
func TestAnIntervalShorterThanTheBackoffFloorCapsIt(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	provider, _ := newScriptedJWKSServer(t, jwksDocumentOf(t, s.publicJWK(t)), func(int64) bool { return true })
	set, err := NewKeySet(JWKSOptions{URL: provider.url(), Interval: 3 * time.Second, Now: steppingClock()})
	if err != nil {
		t.Fatal(err)
	}
	_ = set.Refresh(context.Background())
	rec := runRecorded(t, set, 3)
	want := []time.Duration{3 * time.Second, 3 * time.Second, 3 * time.Second}
	if !reflect.DeepEqual(rec.asked, want) {
		t.Fatalf("Run waited %v, want %v", rec.asked, want)
	}
}

// TestAPreviouslyGoodSetThatStartsFailingRetriesOnABackoffAndKeepsItsKeys.
//
// Two claims. The SCHEDULE is new behaviour: a failure after a success restarts the
// backoff at its floor. The KEYS are an ⚠ INVARIANT GUARD — `Refresh` already kept
// last-known-good on failure before this change (`TestAnIdentityProviderOutage…` pins it
// for a direct call); this pins it through the loop, which is the path a deployed pod is
// actually on.
func TestAPreviouslyGoodSetThatStartsFailingRetriesOnABackoffAndKeepsItsKeys(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	// The startup fetch and one healthy tick succeed; the provider then dies for the rest
	// of the run.
	provider, _ := newScriptedJWKSServer(t, jwksDocumentOf(t, s.publicJWK(t)), func(n int64) bool { return n > 2 })
	set, _ := newLiveKeySet(t, provider)
	if err := set.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	good := set.Status()
	rec := runRecorded(t, set, 4)
	want := []time.Duration{DefaultJWKSInterval, DefaultJWKSInterval, 5 * time.Second, 10 * time.Second}
	if !reflect.DeepEqual(rec.asked, want) {
		t.Fatalf("Run waited %v, want %v", rec.asked, want)
	}

	got := set.Status()
	if !got.Failing || !got.Fetched || got.Keys != good.Keys {
		t.Fatalf("after failed refreshes through Run: failing=%t fetched=%t keys %d -> %d — last-known-good must keep serving",
			got.Failing, got.Fetched, good.Keys, got.Keys)
	}
	token := s.sign(t, defaultClaims(), nil)
	if _, err := Verify(token, VerifyOptions{
		Algs: []Alg{AlgRS256}, Issuer: testIssuer, Audience: testAudience, Keys: set, Now: fixedNow,
	}); err != nil {
		t.Fatalf("an already-issued token stopped verifying after failed refreshes through Run: %v", err)
	}
}

// TestEveryFailedRefreshIsLoggedAndTheRecoveryOnce.
//
// 🔴 THE DEFECT: `Run` discarded the error (`_ = k.Refresh(ctx)`), so after one startup
// warning a pod said nothing while its provider stayed unreachable. The lines are pinned
// WHOLE: what an operator greps for is the text, and a guard on one word would pass a line
// that dropped the error or the next-retry delay.
//
// And the silence on the healthy ticks is asserted too: the two interval refreshes after
// recovery must log NOTHING, so the count below is exactly failures + one recovery.
func TestEveryFailedRefreshIsLoggedAndTheRecoveryOnce(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	provider, _ := newScriptedJWKSServer(t, jwksDocumentOf(t, s.publicJWK(t)), func(n int64) bool { return n <= 3 })
	set, _ := newLiveKeySet(t, provider)
	_ = set.Refresh(context.Background())

	rec := runRecorded(t, set, 5)
	want := []string{
		"WARNING identity: JWKS refresh failed (identity: jwks fetch: HTTP 503); 2 consecutive failure(s); the key set has NEVER been fetched, so no token can verify; retrying in 10s",
		"WARNING identity: JWKS refresh failed (identity: jwks fetch: HTTP 503); 3 consecutive failure(s); the key set has NEVER been fetched, so no token can verify; retrying in 20s",
		"identity: JWKS refresh succeeded, ending a failing run; serving 1 key(s); 3 failure(s) since start; back to every 10m0s",
	}
	if !reflect.DeepEqual(rec.lines, want) {
		t.Fatalf("Run logged:\n  %s\nwant:\n  %s", strings.Join(rec.lines, "\n  "), strings.Join(want, "\n  "))
	}
}

// TestAFailureAfterASuccessNamesTheKeysStillServing: the other arm of the failure line,
// which tells an operator that sessions are NOT affected.
func TestAFailureAfterASuccessNamesTheKeysStillServing(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	provider := newJWKSServer(t, jwksDocumentOf(t, s.publicJWK(t)))
	set, _ := newLiveKeySet(t, provider)
	if err := set.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	provider.code.Store(503)
	rec := runRecorded(t, set, 1)
	want := []string{
		"WARNING identity: JWKS refresh failed (identity: jwks fetch: HTTP 503); 1 consecutive failure(s); still serving the last-known-good 1 key(s); retrying in 5s",
	}
	if !reflect.DeepEqual(rec.lines, want) {
		t.Fatalf("Run logged %q, want %q", rec.lines, want)
	}
}
