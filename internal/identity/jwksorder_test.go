package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
