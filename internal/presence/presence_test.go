package presence

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// TestTheOwnerPredicateIsARelationship pins decision 5's ONE predicate as a relationship over
// one store: owner A sees A's row; every other viewer — B, a PROJECT principal carrying A's id
// string, A after the TTL, A through a narrowed BEARER credential — sees exactly what a viewer
// sees for a session with no presence at all.
//
// Each refusal is a different clause of `visible`, and each is shown RED by deleting that
// clause alone (`tests/control_mutants.py`, the `presence-*` rows). The positive control is
// what keeps the refusals from being satisfied by a predicate that refuses everybody.
func TestTheOwnerPredicateIsARelationship(t *testing.T) {
	authority := world(t)
	clk := &fakeClock{now: clock0}
	s := &Store{Now: clk.Now}
	s.Replace(ownerA, "host-a", []Row{row("s-0001", "2000-01-02T03:00:00Z")})

	a := cookieIdentity(t, authority, userA)
	got, ok := s.For(a, "s-0001")
	if !ok || got.Target.Host != "host-a" || got.Target.Target != "notes:3" || got.Target.Owner != ownerA {
		t.Fatalf("POSITIVE CONTROL FAILED: owner A does not see its own row (ok=%v, %+v)", ok, got)
	}
	if _, ok := s.For(a, "s-0002"); ok {
		t.Fatal("POSITIVE CONTROL: a session nobody pushed is visible")
	}

	sameIDOtherKind := identity.Identity{
		Principal: control.Principal{Kind: control.KindProject, ID: userA, Display: "a project sharing A's id"},
	}
	if !sameIDOtherKind.Valid() || sameIDOtherKind.Auth.Narrowed() {
		t.Fatal("precondition: the same-id project viewer must be a valid, un-narrowed identity, or its refusal below is about something else")
	}

	for _, tc := range []struct {
		name   string
		viewer identity.Identity
		at     time.Time
	}{
		{"owner B", cookieIdentity(t, authority, userB), clock0},
		{"a project principal with A's id string", sameIDOtherKind, clock0},
		{"A at the TTL instant", a, clock0.Add(DefaultTTL)},
		{"A after the TTL", a, clock0.Add(DefaultTTL + time.Second)},
		{"A through a narrowed bearer credential", bearerIdentity(t, authority, narrowedTokenA), clock0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clk.now = tc.at
			defer func() { clk.now = clock0 }()
			if p, ok := s.For(tc.viewer, "s-0001"); ok {
				t.Fatalf("%s SEES owner A's presence: %+v", tc.name, p)
			}
		})
	}

	// The TTL boundary from the other side: one nanosecond before expiry A still sees it, so
	// the expiry case above is the clause and not a clock that broke everything.
	clk.now = clock0.Add(DefaultTTL - time.Nanosecond)
	if _, ok := s.For(a, "s-0001"); !ok {
		t.Fatal("POSITIVE CONTROL: A does not see its row one nanosecond before the TTL")
	}
}

// TestTheBearerNarrowingBitReachesThePredicate is decision 11 as BUILT: the bit is
// `control.Authorization.Narrowed()` on the viewer's own identity, and it arrives there on the
// bearer path because `identity.MachineToken` hands through the `Authorization`
// `control.Authenticate` produced.
//
// Positive controls: A's UN-narrowed bearer credential and A's cookie session both see A's
// presence — so a refusal below is about narrowing, not about the bearer path or the backend.
// Every narrowed shape refuses, including a narrowing EQUAL to today's full set and a narrowing
// to NOTHING (non-nil empty): `Narrowed()` is a fact about how the authority was produced, not
// a comparison of its contents. RED by ignoring `Narrowed()` in `visible`.
func TestTheBearerNarrowingBitReachesThePredicate(t *testing.T) {
	authority := world(t)
	s := &Store{Now: func() time.Time { return clock0 }}
	s.Replace(ownerA, "host-a", []Row{row("s-0001", "")})

	plain := bearerIdentity(t, authority, plainTokenA)
	if plain.Auth.Narrowed() {
		t.Fatal("precondition: the plain credential's authority reads as narrowed")
	}
	if _, ok := s.For(plain, "s-0001"); !ok {
		t.Fatal("POSITIVE CONTROL FAILED: A's un-narrowed bearer credential does not see A's presence")
	}
	cookie := cookieIdentity(t, authority, userA)
	if cookie.Auth.Narrowed() {
		t.Fatal("a cookie session's authority reads as narrowed — the cookie path re-resolves with control.Resolve and must not")
	}
	if _, ok := s.For(cookie, "s-0001"); !ok {
		t.Fatal("POSITIVE CONTROL FAILED: A's cookie session does not see A's presence")
	}

	for _, tc := range []struct{ name, token string }{
		{"narrowed to one scope", narrowedTokenA},
		{"narrowed to every scope A has today", narrowedAllTokenA},
		{"narrowed to nothing (non-nil empty)", emptyNarrowTokenA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := bearerIdentity(t, authority, tc.token)
			if !id.Auth.Narrowed() {
				t.Fatalf("precondition: %s does not carry the narrowed bit through the bearer backend", tc.name)
			}
			if OwnerOf(id.Principal) != ownerA {
				t.Fatalf("precondition: %s is not owner A (%s)", tc.name, OwnerOf(id.Principal))
			}
			if p, ok := s.For(id, "s-0001"); ok {
				t.Fatalf("A's credential %s SEES presence: %+v", tc.name, p)
			}
		})
	}
}

// TestTheTargetIsTheNewestActivityThenTheSmallerHost pins decision 7. Each case pushes host-b
// FIRST so insertion order cannot be what decides; the store's map iteration is random besides.
func TestTheTargetIsTheNewestActivityThenTheSmallerHost(t *testing.T) {
	authority := world(t)
	a := cookieIdentity(t, authority, userA)
	for _, tc := range []struct {
		name         string
		hostA, hostB string // last_activity per host
		want, also   string
	}{
		{"host-b newer wins although its label is larger", "2000-01-02T01:00:00Z", "2000-01-02T02:00:00Z", "host-b", "host-a"},
		{"host-a newer wins", "2000-01-02T02:30:00Z", "2000-01-02T02:00:00Z", "host-a", "host-b"},
		{"equal instants go to the smaller label", "2000-01-02T02:00:00Z", "2000-01-02T02:00:00Z", "host-a", "host-b"},
		{"equal instants spelled differently still tie", "2000-01-02T02:00:00Z", "2000-01-02T03:00:00+01:00", "host-a", "host-b"},
		{"both empty go to the smaller label", "", "", "host-a", "host-b"},
		{"empty sorts oldest", "", "2000-01-01T00:00:00Z", "host-b", "host-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Store{Now: func() time.Time { return clock0 }}
			s.Replace(ownerA, "host-b", []Row{row("s-0001", tc.hostB)})
			s.Replace(ownerA, "host-a", []Row{row("s-0001", tc.hostA)})
			for i := 0; i < 20; i++ { // map order is random; ask repeatedly
				p, ok := s.For(a, "s-0001")
				if !ok {
					t.Fatal("no presence")
				}
				if p.Target.Host != tc.want || !slices.Equal(p.AlsoOn, []string{tc.also}) {
					t.Fatalf("target %s also %v, want %s also [%s]", p.Target.Host, p.AlsoOn, tc.want, tc.also)
				}
			}
		})
	}
}

// TestAPushReplacesItsHostsWholeSetAndNoOtherHosts pins decision 6 at unit level.
func TestAPushReplacesItsHostsWholeSetAndNoOtherHosts(t *testing.T) {
	authority := world(t)
	a := cookieIdentity(t, authority, userA)
	s := &Store{Now: func() time.Time { return clock0 }}
	s.Replace(ownerA, "host-a", []Row{row("s-0001", ""), row("s-0002", "")})
	s.Replace(ownerA, "host-b", []Row{row("s-0003", "")})
	s.Replace(ownerA, "host-a", []Row{row("s-0002", "")})
	if _, ok := s.For(a, "s-0001"); ok {
		t.Fatal("s-0001 survived a push of host-a's whole set that omitted it")
	}
	if _, ok := s.For(a, "s-0002"); !ok {
		t.Fatal("s-0002 vanished although the second push carried it")
	}
	if p, ok := s.For(a, "s-0003"); !ok || p.Target.Host != "host-b" {
		t.Fatal("host-b's row was touched by host-a's pushes")
	}
	s.Replace(ownerA, "host-a", nil)
	if _, ok := s.For(a, "s-0002"); ok {
		t.Fatal("an empty push did not clear host-a")
	}
}

// TestTheQueueIsKeyedByOwnerAtUnitLevel is the control for the queue's owner filter, built
// directly with TWO owners on ONE host label because decision 15's wall makes the case
// unreachable through the listener (no second owner can authenticate). RED by dropping the
// owner from `Claim`'s filter.
func TestTheQueueIsKeyedByOwnerAtUnitLevel(t *testing.T) {
	ids := []string{"r-0001", "r-0002", "r-0003"}
	q := &Queue{Now: func() time.Time { return clock0 }, NewID: func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}}
	mustEnqueue := func(o Owner, host, session string) {
		t.Helper()
		if _, fresh, err := q.Enqueue(o, host, session); err != nil || !fresh {
			t.Fatalf("enqueue %s %s %s: fresh=%v err=%v", o, host, session, fresh, err)
		}
	}
	mustEnqueue(ownerA, "host-a", "s-0001")
	mustEnqueue(ownerB, "host-a", "s-0002")
	mustEnqueue(ownerA, "host-b", "s-0003")

	got := q.Claim(ownerB, "host-a")
	if len(got) != 1 || got[0].ID != "r-0002" || got[0].Session != "s-0002" {
		t.Fatalf("(B, host-a) claimed %+v, want only B's ring r-0002", got)
	}
	got = q.Claim(ownerA, "host-a")
	if len(got) != 1 || got[0].ID != "r-0001" {
		t.Fatalf("(A, host-a) claimed %+v, want only A's host-a ring r-0001", got)
	}
	if got := q.Claim(ownerA, "host-a"); len(got) != 0 {
		t.Fatalf("a ring was returned twice: %+v", got)
	}
	got = q.Claim(ownerA, "host-b")
	if len(got) != 1 || got[0].ID != "r-0003" {
		t.Fatalf("(A, host-b) claimed %+v, want r-0003", got)
	}
}

// TestOnePendingRingPerSessionAndItExpires pins the queue's rate limit (ruling D3): a repeat
// while one is pending is a no-op with the same answer; an unclaimed ring is gone at 60 s.
func TestOnePendingRingPerSessionAndItExpires(t *testing.T) {
	clk := &fakeClock{now: clock0}
	n := 0
	q := &Queue{Now: clk.Now, NewID: func() (string, error) { n++; return "r-" + strconv.Itoa(n), nil }}
	first, fresh, _ := q.Enqueue(ownerA, "host-a", "s-0001")
	again, freshAgain, _ := q.Enqueue(ownerA, "host-a", "s-0001")
	if !fresh || freshAgain || again.ID != first.ID {
		t.Fatalf("a repeat while pending queued a second ring: %+v then %+v (fresh %v/%v)", first, again, fresh, freshAgain)
	}
	clk.now = clock0.Add(59 * time.Second)
	if got := q.Claim(ownerA, "host-a"); len(got) != 1 || got[0].ID != first.ID {
		t.Fatalf("a ring claimed at 59 s: %+v", got)
	}
	if got := q.Claim(ownerA, "host-a"); len(got) != 0 {
		t.Fatalf("claimed twice: %+v", got)
	}

	clk.now = clock0
	q2 := &Queue{Now: clk.Now}
	if _, _, err := q2.Enqueue(ownerA, "host-a", "s-0001"); err != nil {
		t.Fatal(err)
	}
	clk.now = clock0.Add(DefaultRingTTL)
	if got := q2.Claim(ownerA, "host-a"); len(got) != 0 {
		t.Fatalf("an unclaimed ring survived its TTL: %+v", got)
	}
	if _, fresh, _ := q2.Enqueue(ownerA, "host-a", "s-0001"); !fresh {
		t.Fatal("after expiry a new ring could not be queued")
	}
}

// TestARingGoesThroughTheOwnerPredicate: `Service.Ring` reaches the queue only through `For`.
// B ringing A's session, and A ringing a session with no presence, queue nothing; A's ring is
// queued for the TARGET row's host.
func TestARingGoesThroughTheOwnerPredicate(t *testing.T) {
	authority := world(t)
	svc := &Service{Store: &Store{Now: func() time.Time { return clock0 }}, Queue: &Queue{Now: func() time.Time { return clock0 }}}
	svc.Store.Replace(ownerA, "host-a", []Row{row("s-0001", "2000-01-02T01:00:00Z")})
	svc.Store.Replace(ownerA, "host-b", []Row{row("s-0001", "2000-01-02T02:00:00Z")})

	if ok, err := svc.Ring(cookieIdentity(t, authority, userB), "s-0001"); ok || err != nil {
		t.Fatalf("B rang A's session (ok=%v err=%v)", ok, err)
	}
	if ok, _ := svc.Ring(cookieIdentity(t, authority, userA), "s-0009"); ok {
		t.Fatal("a ring was queued for a session with no presence")
	}
	for _, host := range []string{"host-a", "host-b"} {
		if got := svc.Queue.Claim(ownerB, host); len(got) != 0 {
			t.Fatalf("B's refused ring is in the queue: %+v", got)
		}
	}
	if ok, err := svc.Ring(cookieIdentity(t, authority, userA), "s-0001"); !ok || err != nil {
		t.Fatalf("POSITIVE CONTROL: A could not ring its own session (ok=%v err=%v)", ok, err)
	}
	if got := svc.Queue.Claim(ownerA, "host-a"); len(got) != 0 {
		t.Fatalf("the ring went to host-a, which is not the target: %+v", got)
	}
	if got := svc.Queue.Claim(ownerA, "host-b"); len(got) != 1 || got[0].Session != "s-0001" {
		t.Fatalf("the ring did not reach the target host host-b: %+v", got)
	}
}
