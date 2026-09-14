package netid

import (
	"math"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// 🔴 NO IP LITERAL IN THIS FILE NAMES REAL INFRASTRUCTURE. The documentation ranges
// (RFC 5737 for v4, RFC 3849 for v6) are what the fixtures use, and the one case that
// needs the upper half of the v4 space builds the address ARITHMETICALLY rather than
// spelling it — this repository is public and `tests/leakscan.py` refuses an IP literal
// on sight, which is how the first draft of the equivalent Python comment was caught.
const (
	docV4    = "203.0.113.7"
	docV4Two = "203.0.113.99"
	proxyV4  = "192.0.2.1"
)

func TestTrustedNetworkRefusesAnythingTooWide(t *testing.T) {
	t.Run("a default route is refused BY PREFIX LENGTH", func(t *testing.T) {
		// Refused by LENGTH rather than by matching the two spellings, so every future
		// equivalent is one rule instead of a list somebody has to extend.
		for _, entry := range []string{"0.0.0.0/0", "::/0"} {
			_, err := TrustedNetwork(entry)
			if err == nil || !strings.Contains(err.Error(), "trusts every peer") {
				t.Fatalf("%s: got %v", entry, err)
			}
		}
	})

	t.Run("the FLOOR is what closes the two-halves walk", func(t *testing.T) {
		// 🔴 REFUSING ONLY `/0` CHECKS ONE ENTRY IN ISOLATION AND IS WALKABLE. The two
		// halves of the v4 space, each written as a `/1`, parse clean and TOGETHER trust
		// every peer — no single entry is a default route, so a per-entry `/0` check sees
		// nothing wrong. The halves are built arithmetically because the upper one is
		// routable space and must not be spelled in a public repository.
		lower := netip.AddrFrom4([4]byte{0, 0, 0, 0})
		upper := netip.AddrFrom4([4]byte{1 << 7, 0, 0, 0})
		for _, half := range []netip.Addr{lower, upper} {
			entry := netip.PrefixFrom(half, 1).String()
			if _, err := TrustedNetwork(entry); err == nil {
				t.Fatalf("a /1 must be refused by the floor: %s", entry)
			}
		}
		// …and the realistic one: an ordinary pod CIDR hands the client identity to every
		// pod in the cluster, which is verbatim the attacker in this design's own threat
		// model.
		if _, err := TrustedNetwork("10.244.0.0/16"); err == nil {
			t.Fatal("a /16 is too broad to be a proxy tier")
		}
	})

	t.Run("a host and a small tier are accepted, in both spellings", func(t *testing.T) {
		for _, entry := range []string{proxyV4, proxyV4 + "/32", "192.0.2.0/24", "2001:db8::1", "2001:db8::/64"} {
			if _, err := TrustedNetwork(entry); err != nil {
				t.Fatalf("%s: %v", entry, err)
			}
		}
		// Host bits set is accepted as the network it NAMES — an operator writing a CIDR
		// from memory means the network, and refusing here would push them towards the
		// default route the floor exists to stop.
		prefix, err := TrustedNetwork("192.0.2.5/24")
		if err != nil {
			t.Fatal(err)
		}
		if prefix.String() != "192.0.2.0/24" {
			t.Fatalf("got %s", prefix)
		}
	})

	t.Run("there is NO DEFAULT, and an unset variable refuses to start", func(t *testing.T) {
		// A default would be a guess about somebody else's network, and the only guess
		// that keeps every deployment working is a permissive one — which is the defect
		// this setting exists to close, shipped as a constant.
		for _, env := range []map[string]string{nil, {}, {EnvTrustedProxies: ""}, {EnvTrustedProxies: "   "}} {
			if _, err := LoadTrustedProxies(env); err == nil {
				t.Fatalf("an unset or blank allowlist must refuse: %v", env)
			}
		}
		got, err := LoadTrustedProxies(map[string]string{EnvTrustedProxies: proxyV4 + ", 192.0.2.0/24"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("comma- and whitespace-separated entries: got %v", got)
		}
	})
}

func TestAForgedHeaderCanOnlyEverLockOutItself(t *testing.T) {
	// 🔴 THE SECURITY PROPERTY IS *A FORGED HEADER MUST NEVER NAME A THIRD PARTY*. The
	// pre-fix behaviour read the header from whoever connected, so ANY peer that could
	// address the pod could send a header naming a victim plus five bad tokens and that
	// victim was locked out for fifteen minutes — seeing a 401 indistinguishable from a
	// wrong credential.
	trusted := []netip.Prefix{netip.MustParsePrefix(proxyV4 + "/32")}
	headers := http.Header{}
	headers.Set(ClientIPHeader, docV4)

	t.Run("a TRUSTED peer's header IS the client", func(t *testing.T) {
		key, peerTrusted, ok := ResolveClient(headers, proxyV4+":4000", trusted)
		if !ok || !peerTrusted || key != docV4 {
			t.Fatalf("got key=%q trusted=%v ok=%v", key, peerTrusted, ok)
		}
	})

	t.Run("an UNTRUSTED peer's header is NOT READ AT ALL", func(t *testing.T) {
		// Distrust is expressed by IGNORING what the caller claims, not by hanging up on
		// them: refusing outright is stricter than the property needs and it broke the
		// documented acceptance procedure, where a port-forward presents a loopback peer
		// while the deployment allowlists the node's internal address.
		key, peerTrusted, ok := ResolveClient(headers, "198.51.100.4:5000", trusted)
		if !ok {
			t.Fatal("an untrusted peer is SERVED, bucketed under its own address")
		}
		if peerTrusted {
			t.Fatal("…and is reported as untrusted")
		}
		if key == docV4 {
			t.Fatal("the forged header named a THIRD PARTY and was believed")
		}
		if key != "198.51.100.4" {
			t.Fatalf("the bucket must be the PEER's own address, got %q", key)
		}
	})

	t.Run("a trusted peer with NO header FAILS CLOSED", func(t *testing.T) {
		// The alternative — bucketing every unidentified request under one shared key —
		// is the failure the whole design exists to avoid: one abuser would then lock out
		// everybody.
		_, peerTrusted, ok := ResolveClient(http.Header{}, proxyV4+":4000", trusted)
		if ok {
			t.Fatal("there is no bucket to meter, so the request must be refused")
		}
		if !peerTrusted {
			t.Fatal("the peer was still trusted; only the header was missing")
		}
	})

	t.Run("a DUPLICATED header is refused rather than guessed at", func(t *testing.T) {
		// Which one is the client is unanswerable, and picking one is how a caller
		// smuggles a second value past a proxy that appends rather than overwrites.
		two := http.Header{}
		two.Add(ClientIPHeader, docV4)
		two.Add(ClientIPHeader, docV4Two)
		if _, _, ok := ResolveClient(two, proxyV4+":4000", trusted); ok {
			t.Fatal("two client-IP headers must be refused")
		}
	})

	t.Run("an unparseable header value is refused", func(t *testing.T) {
		mangled := http.Header{}
		mangled.Set(ClientIPHeader, "not-an-address")
		if _, _, ok := ResolveClient(mangled, proxyV4+":4000", trusted); ok {
			t.Fatal("a forged or mangled value must be refused")
		}
	})

	t.Run("an unparseable PEER is refused, and reported as no-client-ip", func(t *testing.T) {
		if _, _, ok := ResolveClient(headers, "not-an-address", trusted); ok {
			t.Fatal("there is no bucket to charge")
		}
	})

	t.Run("X-Forwarded-For is never consulted", func(t *testing.T) {
		// It is caller-supplied, so keying on it gives one attacker a fresh bucket per
		// request AND lets them lock out a third party by forging theirs.
		xff := http.Header{}
		xff.Set("X-Forwarded-For", docV4)
		key, _, ok := ResolveClient(xff, proxyV4+":4000", trusted)
		if ok {
			t.Fatalf("a trusted peer with no client-IP header must fail closed, got %q", key)
		}
	})
}

func TestPeerAddressUnwrapsIPv4Mapped(t *testing.T) {
	// 🔴 A DUAL-STACK LISTENER REPORTS A v4 CALLER AS `::ffff:…`, and an allowlist
	// written as a /32 would then never match — a fail-CLOSED break rather than a hole,
	// but a break that reads as "the guard is wrong" and gets widened until it is.
	peer, ok := PeerAddress("[::ffff:" + proxyV4 + "]:4000")
	if !ok {
		t.Fatal("a mapped peer must parse")
	}
	if !PeerIsTrusted(peer, []netip.Prefix{netip.MustParsePrefix(proxyV4 + "/32")}) {
		t.Fatalf("the mapped form of %s did not match its own /32: %s", proxyV4, peer)
	}
	// A link-local peer carries a scope id that is a local interface name, not part of
	// the identity being allowlisted.
	if _, ok := PeerAddress("[fe80::1%eth0]:4000"); !ok {
		t.Fatal("a zoned link-local peer must parse")
	}
}

func TestPeerIsTrustedDoesNotMatchAcrossFamilies(t *testing.T) {
	v4, _ := PeerAddress(proxyV4 + ":1")
	if PeerIsTrusted(v4, []netip.Prefix{netip.MustParsePrefix("2001:db8::/64")}) {
		t.Fatal("a v4 peer must not match a v6 prefix")
	}
	var invalid netip.Addr
	if PeerIsTrusted(invalid, []netip.Prefix{netip.MustParsePrefix(proxyV4 + "/32")}) {
		t.Fatal("an invalid peer is not trusted")
	}
}

func TestRateLimitKeyAggregatesIPv6ButNotTheTrustCheck(t *testing.T) {
	// 🔴 A FULL IPv6 ADDRESS IS NOT A CLIENT, IT IS A CHOICE. An ordinary allocation is
	// a /64 the same person can pick freely, so keying on the full address gives one
	// attacker 2^64 buckets and the lockout becomes decorative — and it is the cheapest
	// way to grow the failure table without bound.
	one := RateLimitKey(netip.MustParseAddr("2001:db8::1"))
	two := RateLimitKey(netip.MustParseAddr("2001:db8::dead:beef"))
	if one != two {
		t.Fatalf("two addresses in one /64 are one client: %q vs %q", one, two)
	}
	other := RateLimitKey(netip.MustParseAddr("2001:db8:0:1::1"))
	if other == one {
		t.Fatal("a different /64 is a different client")
	}
	// 🔴 AN IPv4-MAPPED ADDRESS IS THE SAME CLIENT AS ITS IPv4 FORM. Left alone, one
	// caller gets a free second bucket simply by reaching the edge over v6.
	if RateLimitKey(netip.MustParseAddr("::ffff:"+docV4)) != RateLimitKey(netip.MustParseAddr(docV4)) {
		t.Fatal("the mapped and bare forms of one address must be one bucket")
	}
	// 🔴 AND THE TRUST CHECK MUST **NOT** AGGREGATE. A /64 of trusted proxies is 2^64
	// peers the operator did not name — the same reasoning that makes aggregation right
	// for a bucket makes it wrong here, which is why they are two functions.
	peer, _ := PeerAddress("[2001:db8::dead:beef]:1")
	if PeerIsTrusted(peer, []netip.Prefix{netip.MustParsePrefix("2001:db8::1/128")}) {
		t.Fatal("a /128 allowlist must not match a sibling address in the same /64")
	}
}

func TestLimiterSettingsRefuseRatherThanDefault(t *testing.T) {
	// 🔴 A TYPO THAT QUIETLY BECAME THE DEFAULT IS AN OPERATOR BELIEVING A SETTING TOOK
	// EFFECT. A misconfiguration at startup is visible in a crash loop; one that defaults
	// is invisible forever.
	for _, env := range []map[string]string{
		{EnvMaxFailures: "fve"},
		{EnvMaxFailures: "0"},
		{EnvMaxFailures: "-1"},
		{EnvFailureWindow: "nope"},
		{EnvFailureWindow: "0"},
		{EnvLockout: "-5"},
		// 🔴 `nan` AND `inf` PARSE, AND BOTH WALK STRAIGHT THROUGH `<= 0`. Measured
		// consequences: a nan WINDOW silently disables the limiter entirely because every
		// recorded failure compares as outside it, and a nan or inf LOCKOUT makes it
		// permanent — both arriving through the one comparison that does not order them.
		{EnvFailureWindow: "nan"},
		{EnvFailureWindow: "inf"},
		{EnvLockout: "nan"},
		{EnvLockout: "+Inf"},
	} {
		if _, _, _, err := LimiterSettings(env); err == nil {
			t.Fatalf("%v must be refused", env)
		}
	}
	// The defaults live in code, so a deployment that sets nothing still gets them.
	maxFailures, window, lockout, err := LimiterSettings(nil)
	if err != nil {
		t.Fatal(err)
	}
	if maxFailures != 5 || window != time.Minute || lockout != 15*time.Minute {
		t.Fatalf("got %d %v %v", maxFailures, window, lockout)
	}
	// …and each is overridable.
	maxFailures, window, lockout, err = LimiterSettings(map[string]string{
		EnvMaxFailures: "1000000", EnvFailureWindow: "1.5", EnvLockout: "2",
	})
	if err != nil || maxFailures != 1000000 || window != 1500*time.Millisecond || lockout != 2*time.Second {
		t.Fatalf("got %d %v %v err=%v", maxFailures, window, lockout, err)
	}
}

// 🔴 A LOCKOUT THE LOG CLAIMS AND THE LIMITER DOES NOT HOLD. `time.Duration(f *
// float64(time.Second))` is implementation-defined once the product leaves int64, and on
// amd64 it yields `math.MinInt64` — so `LOCKOUT_S` above ~9.223e9 set `lockedUntil` to a
// time 292 years in the PAST while `RecordFailure` returned true. The audit log wrote
// `status=lockout-triggered`, `LockedOut` answered false, and the same branch wiped the
// failure streak: unlimited brute force behind an alert that says it is being stopped.
//
// 🔴 MEASURED AT BOTH SIDES OF THE BOUNDARY, NOT AT ONE POINT, because the defect IS the
// boundary — a test that only tried `1e10` could not tell a saturating conversion from a
// blanket refusal, and a test that only tried `900` sees nothing at all.
//
// The oracle has no such ceiling (`now + lockout_s` is Python float arithmetic), so the
// contract is "accept it and make it work", never "refuse it" — which is why the FIRST
// assertion here is that the value still loads.
func TestALockoutLargerThanADurationStillLocksOut(t *testing.T) {
	// 9223372036.854775807 seconds is the exact int64-nanosecond ceiling. Spelled as a
	// literal, not derived from the constant the code uses, per this file's header.
	const ceilingSeconds = 9223372036.854775807

	for _, tc := range []struct {
		raw        string
		wantAtMost time.Duration // 0 means "no ceiling expected — it fits"
	}{
		{"900", 0},
		// Just BELOW the ceiling: must be carried through unchanged, so a fix that
		// saturated everything would be caught here rather than looking correct.
		{"9.2e9", 0},
		// 🔴 THE CEILING ITSELF, SPELLED EXACTLY, because the guard is a COMPARISON and a
		// table that straddles the boundary without landing ON it cannot tell `>=` from
		// `>`. This value times 1e9 rounds to 9223372036854775808.0, which is one MORE
		// than MaxInt64 — so `>` overflows here and `>=` does not.
		{"9223372036.854775808", math.MaxInt64},
		// Just ABOVE it, and then far above it. Both must saturate, and both must LOCK.
		{"9.3e9", math.MaxInt64},
		{"1e10", math.MaxInt64},
		{"1e300", math.MaxInt64},
		{"1.7976931348623157e308", math.MaxInt64}, // the largest finite float64
	} {
		t.Run(tc.raw, func(t *testing.T) {
			_, _, lockout, err := LimiterSettings(map[string]string{EnvLockout: tc.raw})
			if err != nil {
				t.Fatalf("the oracle accepts %s and serves, so this must load it: %v", tc.raw, err)
			}
			if lockout <= 0 {
				t.Fatalf("LOCKOUT_S=%s produced a NON-POSITIVE duration (%v, %d ns) — "+
					"a lockout that has already expired before it is stored",
					tc.raw, lockout, int64(lockout))
			}
			if tc.wantAtMost != 0 && lockout != tc.wantAtMost {
				t.Fatalf("LOCKOUT_S=%s must saturate at %d ns, got %d",
					tc.raw, int64(tc.wantAtMost), int64(lockout))
			}

			// 🔴 THE BEHAVIOURAL HALF, AND IT IS THE ONE THAT MATTERS. A positive
			// duration is necessary and not sufficient: the claim is that the client is
			// actually held, so the lockout is taken and then OBSERVED through the same
			// accessor the request path uses.
			now := time.Unix(946684800, 0)
			limiter := &RateLimiter{MaxFailures: 2, Window: time.Minute, Lockout: lockout,
				Now: func() time.Time { return now }}
			var reported bool
			for i := 0; i < 2; i++ {
				reported = limiter.RecordFailure("client")
			}
			if !reported {
				t.Fatalf("LOCKOUT_S=%s: RecordFailure did not report a lockout", tc.raw)
			}
			if !limiter.LockedOut("client") {
				t.Fatalf("LOCKOUT_S=%s: RecordFailure reported `lockout-triggered` to the "+
					"audit log and LockedOut says the client is free", tc.raw)
			}
			// …and the streak must not have been wiped into nothing by a branch that
			// thought it had created a lockout.
			if !limiter.LockedOut("client") {
				t.Fatalf("LOCKOUT_S=%s: the lockout did not survive a second read", tc.raw)
			}
		})
	}

	// The window overflows through the SAME conversion, in the opposite direction:
	// negating MinInt64 is MinInt64 again, so the cutoff landed 292 years in the past and
	// nothing was ever pruned — an effectively INFINITE window, which makes the limiter
	// stricter rather than disabled. Pinned because the direction is counter-intuitive and
	// the first write-up of it was wrong.
	_, window, _, err := LimiterSettings(map[string]string{EnvFailureWindow: "1e10"})
	if err != nil {
		t.Fatal(err)
	}
	if window <= 0 {
		t.Fatalf("FAILURE_WINDOW_S=1e10 produced %v (%d ns)", window, int64(window))
	}
	if ceilingSeconds*float64(time.Second) < float64(math.MaxInt64) {
		t.Fatal("the spelled ceiling is below MaxInt64 nanoseconds — the fixture is wrong")
	}
}

func TestTheLockout(t *testing.T) {
	now := time.Unix(946684800, 0)
	limiter := NewRateLimiter(3, time.Minute, 15*time.Minute)
	limiter.Now = func() time.Time { return now }

	if limiter.LockedOut("k") {
		t.Fatal("a fresh key is not locked out")
	}
	if limiter.RecordFailure("k") || limiter.RecordFailure("k") {
		t.Fatal("the first two failures must not trip the lockout")
	}
	if !limiter.RecordFailure("k") {
		t.Fatal("the third failure must trip it")
	}
	if !limiter.LockedOut("k") {
		t.Fatal("…and it must hold")
	}

	// 🔴 A SUCCESS DOES NOT FORGIVE A FAILURE STREAK. It used to, which created two
	// attacks that both turn on the key being an ADDRESS rather than an identity: an
	// attacker holding ANY accepted token interleaves one success per N-1 guesses and
	// brute-forces the rest of the set forever, and an attacker sharing a NAT with a
	// legitimate client is never locked out because the victim's own traffic keeps
	// resetting the counter on their behalf.
	fresh := NewRateLimiter(3, time.Minute, 15*time.Minute)
	fresh.Now = func() time.Time { return now }
	fresh.RecordFailure("k")
	fresh.RecordFailure("k")
	fresh.RecordSuccess("k")
	if !fresh.RecordFailure("k") {
		t.Fatal("a success must not forgive the streak")
	}

	// The window and the lockout EXPIRE, which is the half a test that only sleeps
	// cannot show.
	now = now.Add(16 * time.Minute)
	if limiter.LockedOut("k") {
		t.Fatal("an expired lockout must be dropped")
	}
	sliding := NewRateLimiter(3, time.Minute, 15*time.Minute)
	sliding.Now = func() time.Time { return now }
	sliding.RecordFailure("k")
	sliding.RecordFailure("k")
	now = now.Add(2 * time.Minute)
	if sliding.RecordFailure("k") {
		t.Fatal("two failures that aged out of the window must not count towards the third")
	}

	// A lockout is per KEY, so one client cannot lock out another.
	other := NewRateLimiter(1, time.Minute, time.Minute)
	other.RecordFailure("a")
	if other.LockedOut("b") {
		t.Fatal("a lockout must not spill across buckets")
	}
}
