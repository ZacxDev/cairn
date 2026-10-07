package presence

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/ZacxDev/cairn/internal/identity"
)

// DefaultRingTTL is how long an unclaimed ring waits (O4, review ruling D3).
const DefaultRingTTL = 60 * time.Second

// Ring is one queued terminal-bell request. It carries NO bytes for the pane: the host
// re-resolves the pane locally at ring time (decision 9), so the UI never knows one.
type Ring struct {
	ID      string
	Session string
	Owner   Owner
	Host    string
	Created time.Time
}

type ringKey struct {
	owner   Owner
	session string
}

// Queue holds pending rings: at most ONE per `(owner, session)` (a repeat while one is pending
// is a no-op with the same answer), each expiring unclaimed after [DefaultRingTTL], each claimed
// at most once and only by the `(owner, host)` it was queued for.
type Queue struct {
	// TTL defaults to [DefaultRingTTL].
	TTL time.Duration
	// Now defaults to `time.Now`.
	Now func() time.Time
	// NewID defaults to [NewRingID].
	NewID func() (string, error)

	mu      sync.Mutex
	pending map[ringKey]Ring
}

func (q *Queue) now() time.Time {
	if q.Now != nil {
		return q.Now()
	}
	return time.Now()
}

func (q *Queue) ttl() time.Duration {
	if q.TTL > 0 {
		return q.TTL
	}
	return DefaultRingTTL
}

// NewRingID is a random, unguessable ring id. Random rather than a counter because a ring id
// is the one value a claim hands back to a host; a sequence would let one claim predict the
// next.
func NewRingID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "r-" + hex.EncodeToString(buf), nil
}

// dropExpired removes every ring past its TTL. Called under q.mu.
func (q *Queue) dropExpired(now time.Time) {
	for k, r := range q.pending {
		if !now.Before(r.Created.Add(q.ttl())) {
			delete(q.pending, k)
		}
	}
}

// Enqueue queues a ring for `(owner, session)` aimed at `host`, or returns the one already
// pending with `fresh == false`.
//
// ⚠ IT DECIDES NOTHING ABOUT WHO MAY RING. The only production caller is [Service.Ring], which
// reaches it through the owner predicate; a direct call is for this package's tests, which
// build the queue with two owners precisely because the single-owner wall makes that case
// unreachable end to end.
func (q *Queue) Enqueue(owner Owner, host, session string) (r Ring, fresh bool, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now()
	q.dropExpired(now)
	if q.pending == nil {
		q.pending = map[ringKey]Ring{}
	}
	key := ringKey{owner, session}
	if existing, ok := q.pending[key]; ok {
		return existing, false, nil
	}
	newID := q.NewID
	if newID == nil {
		newID = NewRingID
	}
	id, err := newID()
	if err != nil {
		return Ring{}, false, err
	}
	r = Ring{ID: id, Session: session, Owner: owner, Host: host, Created: now}
	q.pending[key] = r
	return r, true, nil
}

// Claim removes and returns every unexpired ring queued for `(owner, host)`, ordered by
// creation then id. Each ring is returned exactly once.
//
// 🔴 THE FILTER IS THE PAIR, AND THE OWNER HALF IS THE ONE THE WALL HIDES. A claim token is
// bound to `(owner, ONE host label)`; filtering on the host alone would hand owner A's rings to
// owner B's claim token on a host both name. `TestTheQueueIsKeyedByOwnerAtUnitLevel` builds that
// case directly, because decision 15's wall makes it unreachable through the listener.
func (q *Queue) Claim(owner Owner, host string) []Ring {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dropExpired(q.now())
	out := []Ring{}
	for k, r := range q.pending {
		if r.Owner != owner || r.Host != host {
			continue
		}
		out = append(out, r)
		delete(q.pending, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.Before(out[j].Created)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Service is the store and the queue together, which is what a browser surface holds.
type Service struct {
	Store *Store
	Queue *Queue
}

// Ring queues a ring for `session` on behalf of `viewer`, aimed at the TARGET row's host at
// enqueue time (decision 7) — and only when [Store.For] returns that row. `false` is the same
// answer for another owner's session, an expired one, a narrowed viewer and no presence.
func (s *Service) Ring(viewer identity.Identity, session string) (bool, error) {
	p, ok := s.Store.For(viewer, session)
	if !ok {
		return false, nil
	}
	if _, _, err := s.Queue.Enqueue(p.Target.Owner, p.Target.Host, session); err != nil {
		return false, err
	}
	return true, nil
}
