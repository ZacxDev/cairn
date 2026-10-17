package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAnOlderRefreshFinishingLastDoesNotOverwriteANewerOne pins `Refresh`'s ordering guard.
//
// 🔴 DETERMINISTIC, NOT A RACE THE SCHEDULER MIGHT WIN. The first read is HELD at the source
// until the second has committed, so the out-of-order finish happens on every run rather than
// whenever two goroutines happen to interleave. The source is a loopback endpoint because it
// is the one source a test can block mid-read; the guard sits in `Refresh`, which both sources
// share, so what it proves holds for a file read the same way.
//
// The shape it rules out is a revocation that un-revokes: read #1 starts while the old key is
// still published, the operator removes it, read #2 sees the removal and commits, then read #1
// finishes and puts the old key back.
func TestAnOlderRefreshFinishingLastDoesNotOverwriteANewerOne(t *testing.T) {
	oldKey := newRSASigner(t, "rsa-old", 2048)
	newKey := newECSigner(t, "ec-new")
	oldDoc := jwksDocumentOf(t, oldKey.publicJWK(t))
	newDoc := jwksDocumentOf(t, newKey.publicJWK(t))

	arrived, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if requests.Add(1) == 1 {
			// Read #1 sees the document as it was BEFORE the rotation, then is held.
			close(arrived)
			<-release
			_, _ = w.Write(oldDoc)
			return
		}
		_, _ = w.Write(newDoc)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	set, err := NewKeySet(JWKSOptions{URL: srv.URL + "/jwks", Now: steppingClock()})
	if err != nil {
		t.Fatal(err)
	}

	first := make(chan error, 1)
	go func() { first <- set.Refresh(context.Background()) }()
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("read #1 never reached the source")
	}

	// Read #2 starts AFTER read #1 and commits first.
	if err := set.Refresh(context.Background()); err != nil {
		t.Fatalf("read #2: %v", err)
	}
	opts := verifierOverSet(set)
	if _, err := Verify(newKey.sign(t, defaultClaims(), nil), opts); err != nil {
		t.Fatalf("precondition: after read #2 the new key must verify: %v", err)
	}
	committed := set.Status()

	// Read #1 finishes LAST.
	close(release)
	select {
	case err := <-first:
		if err != nil {
			t.Fatalf("read #1: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("read #1 never returned")
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("positive control: the source must have served exactly two reads, served %d", got)
	}

	if _, err := Verify(oldKey.sign(t, defaultClaims(), nil), opts); !errors.Is(err, ErrNoKey) {
		t.Fatalf("the older read finished last and RE-INSTALLED the removed key: want ErrNoKey, got %v", err)
	}
	if _, err := Verify(newKey.sign(t, defaultClaims(), nil), opts); err != nil {
		t.Fatalf("the newer set must survive the older read finishing last: %v", err)
	}
	if after := set.Status(); after.FetchedAt != committed.FetchedAt || after.Fetches != committed.Fetches {
		t.Fatalf("a discarded read must change nothing:\n  committed %s\n  after     %s", committed, after)
	}
}

// sourceStep is how a scripted source answers ONE request (in arrival order): optionally
// signal `arrived` and wait on `hold` first, then answer `code` with `body`.
type sourceStep struct {
	arrived, hold chan struct{}
	code          int
	body          []byte
}

// scriptedSource serves steps[n] to the n-th request and counts them. A request past the
// script is a test bug and answers 500 loudly rather than repeating a step.
func scriptedSource(t *testing.T, steps ...sourceStep) (string, *atomic.Int64) {
	t.Helper()
	var seen atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(seen.Add(1))
		if n > len(steps) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		step := steps[n-1]
		if step.arrived != nil {
			close(step.arrived)
		}
		if step.hold != nil {
			<-step.hold
		}
		w.WriteHeader(step.code)
		if step.code == http.StatusOK {
			_, _ = w.Write(step.body)
		}
	}))
	t.Cleanup(srv.Close)
	for _, step := range steps {
		if step.hold != nil {
			hold := step.hold
			t.Cleanup(func() {
				select {
				case <-hold:
				default:
					close(hold)
				}
			})
		}
	}
	return srv.URL + "/jwks", &seen
}

func waitClosed(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never happened", what)
	}
}

func waitErr(t *testing.T, ch chan error, what string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never returned", what)
		return nil
	}
}

// TestANewerFAILEDReadDoesNotDiscardAnOlderSuccessfulOne — the round-2 defect. The committed
// set is {rsa-old}; read A sees the edit that removes it; a newer read B FAILS; A finishes
// last. A FAILED read saw no keys, so it cannot make A's view stale: A's edit must apply, the
// set must report healthy, and every return value must agree with that state.
func TestANewerFAILEDReadDoesNotDiscardAnOlderSuccessfulOne(t *testing.T) {
	oldKey, newKey := newRSASigner(t, "rsa-old", 2048), newECSigner(t, "ec-new")
	arrivedA, holdA := make(chan struct{}), make(chan struct{})
	url, seen := scriptedSource(t,
		sourceStep{code: http.StatusOK, body: jwksDocumentOf(t, oldKey.publicJWK(t))},                                 // the committed set
		sourceStep{arrived: arrivedA, hold: holdA, code: http.StatusOK, body: jwksDocumentOf(t, newKey.publicJWK(t))}, // A: the edit
		sourceStep{code: http.StatusInternalServerError},                                                              // B: fails
	)
	set, err := NewKeySet(JWKSOptions{URL: url, Now: steppingClock()})
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Refresh(context.Background()); err != nil {
		t.Fatalf("the committed set: %v", err)
	}
	opts := verifierOverSet(set)

	a := make(chan error, 1)
	go func() { a <- set.Refresh(context.Background()) }()
	waitClosed(t, arrivedA, "read A reaching the source")
	if err := set.Refresh(context.Background()); err == nil {
		t.Fatal("read B must fail")
	}
	if st := set.Status(); !st.Failing {
		t.Fatalf("after B failed, before A returns, the set must report failing: %s", st)
	}
	close(holdA)
	if err := waitErr(t, a, "read A"); err != nil {
		t.Fatalf("read A's edit was applied, so A must return nil; got %v", err)
	}
	if got := seen.Load(); got != 3 {
		t.Fatalf("positive control: the source must have served 3 reads, served %d", got)
	}

	if _, err := Verify(newKey.sign(t, defaultClaims(), nil), opts); err != nil {
		t.Fatalf("A's edit was DISCARDED behind a newer read that FAILED: the new key does not verify: %v", err)
	}
	if _, err := Verify(oldKey.sign(t, defaultClaims(), nil), opts); !errors.Is(err, ErrNoKey) {
		t.Fatalf("the removed kid still verifies after A's edit: want ErrNoKey, got %v", err)
	}
	if st := set.Status(); st.Failing || st.Keys != 1 {
		t.Fatalf("A returned nil, so the set must be healthy and serving A's one key: %s", st)
	}
}

// TestAnOlderFAILUREFinishingAfterANewerSuccessIsDiscarded — the mirror arm. The healthy state
// a newer success committed stands: no `failing=true`, and the late failure returns nil.
//
// ⚠ AN INVARIANT GUARD AT THE PREVIOUS HEAD: the generation guard already discarded this
// case there. It is here so the round-2 rule change — a failure no longer advances the
// number — cannot regress it into "the latest-finishing failure wins".
func TestAnOlderFAILUREFinishingAfterANewerSuccessIsDiscarded(t *testing.T) {
	newKey := newECSigner(t, "ec-new")
	arrivedA, holdA := make(chan struct{}), make(chan struct{})
	url, _ := scriptedSource(t,
		sourceStep{arrived: arrivedA, hold: holdA, code: http.StatusInternalServerError}, // A: fails late
		sourceStep{code: http.StatusOK, body: jwksDocumentOf(t, newKey.publicJWK(t))},    // B: succeeds
	)
	set, err := NewKeySet(JWKSOptions{URL: url, Now: steppingClock()})
	if err != nil {
		t.Fatal(err)
	}
	a := make(chan error, 1)
	go func() { a <- set.Refresh(context.Background()) }()
	waitClosed(t, arrivedA, "read A reaching the source")
	if err := set.Refresh(context.Background()); err != nil {
		t.Fatalf("read B: %v", err)
	}
	committed := set.Status()
	close(holdA)
	if err := waitErr(t, a, "read A"); err != nil {
		t.Fatalf("an older failure behind a newer success must return the committed outcome (nil), got %v", err)
	}
	if st := set.Status(); st.Failing || st.Failures != committed.Failures || st.LastAttempt != committed.LastAttempt {
		t.Fatalf("the discarded failure changed the healthy state:\n  committed %s\n  after     %s", committed, st)
	}
	if _, err := Verify(newKey.sign(t, defaultClaims(), nil), verifierOverSet(set)); err != nil {
		t.Fatalf("the newer success's key must keep verifying: %v", err)
	}
}

// TestRunDoesNotReportSuccessForARefreshItsSetDiscarded is the CALLER-level claim: `Run` logs
// "refresh succeeded" only when the outcome it was handed is a success.
//
// The shape: a never-fetched set (so `Run` starts in its failing state); `Run`'s first read
// is held; meanwhile a newer read SUCCEEDS and a newer-still read FAILS, so the committed
// state is failing. `Run`'s read then finishes, is discarded, and must be handed that
// failure — a discarded call that answered nil made `Run` print "succeeded, ending a failing
// run" and reset its backoff while `failing=true`.
func TestRunDoesNotReportSuccessForARefreshItsSetDiscarded(t *testing.T) {
	key := newECSigner(t, "ec-1")
	doc := jwksDocumentOf(t, key.publicJWK(t))
	arrivedRun, holdRun := make(chan struct{}), make(chan struct{})
	url, _ := scriptedSource(t,
		sourceStep{arrived: arrivedRun, hold: holdRun, code: http.StatusOK, body: doc}, // Run's read, held
		sourceStep{code: http.StatusOK, body: doc},                                     // newer: succeeds
		sourceStep{code: http.StatusInternalServerError},                               // newest: fails
	)
	set, err := NewKeySet(JWKSOptions{URL: url, Now: steppingClock()})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var lines []string
	secondWait := make(chan struct{})
	waits := 0
	set.after = func(time.Duration) (<-chan time.Time, func()) {
		mu.Lock()
		defer mu.Unlock()
		waits++
		if waits == 1 {
			fired := make(chan time.Time, 1)
			fired <- time.Time{}
			return fired, func() {}
		}
		if waits == 2 {
			close(secondWait)
		}
		return make(chan time.Time), func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- set.Run(ctx, func(line string) { mu.Lock(); lines = append(lines, line); mu.Unlock() })
	}()
	t.Cleanup(func() { cancel(); <-done })

	waitClosed(t, arrivedRun, "Run's read reaching the source")
	if err := set.Refresh(context.Background()); err != nil {
		t.Fatalf("the newer read must succeed: %v", err)
	}
	if err := set.Refresh(context.Background()); err == nil {
		t.Fatal("the newest read must fail")
	}
	close(holdRun)
	waitClosed(t, secondWait, "Run finishing its first refresh")

	mu.Lock()
	got := strings.Join(lines, "\n")
	mu.Unlock()
	if strings.Contains(got, "refresh succeeded") {
		t.Fatalf("Run reported success while the committed state is failing (%s):\n%s", set.Status(), got)
	}
	if !strings.Contains(got, "JWKS refresh failed") {
		t.Fatalf("Run must report the committed failure it was handed; logged:\n%s", got)
	}
}
