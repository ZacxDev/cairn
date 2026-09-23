package ui

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/netid"
)

// 🔴 WHAT THIS FILE IS FOR. `POST /sign-in` had no client identity and no lockout:
// unlimited attempts, and a refusal line naming nobody. Credentials are 256 bits from
// `crypto/rand`, so this was never a guess-the-token risk — what it exposed is unbounded
// volume against a path that computes a digest and reads the authority on every attempt,
// and an attack invisible in the log. `internal/api`'s `identifyAndMeter` decided both
// mattered for a surface other people can reach; this is that decision on the surface
// that is about to become reachable.
//
// 🔴 THE TRUST BOUNDARY IS THE ONLY HARD PART, SO IT IS DRIVEN FROM BOTH SIDES. A header
// from an allowlisted peer is authority; the identical header from anyone else must be
// ignored. A file that tested only the trusted side would pass against a server that
// trusts everybody, which is the whole defect.
//
// ⚠ IT EXTENDS `live` RATHER THAN BUILDING A SECOND FIXTURE, because that type's own
// comment says every session test drives it so a test varying ONE thing varies exactly
// one. `newLiveMetered` is `newLive` plus the two fields under test.

// doFrom is `live.do` with a PEER and HEADERS, which is what these cases vary.
//
// ⚠ IT DOES NOT WRAP `live.do`: that helper builds the request and serves it in one call,
// so there is no seam to set `RemoteAddr` on. The duplicated lines are the four `do`
// already sets for the origin gate — kept identical on purpose, because a request that
// failed the origin gate would never reach the handler under test and would read as a
// client-identity failure.
func (l *live) doFrom(peer string, hdr map[string]string, form url.Values) *httptest.ResponseRecorder {
	l.t.Helper()
	r := httptest.NewRequest(http.MethodPost, SignInPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Host = testHost
	r.Header.Set("Origin", "https://"+testHost)
	r.RemoteAddr = peer
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	l.srv.ServeHTTP(rec, r)
	return rec
}

func wrongToken(t *testing.T) url.Values {
	t.Helper()
	return url.Values{FieldToken: {"a-wrong-credential-value"}}
}
func rightToken(t *testing.T) url.Values { t.Helper(); return url.Values{FieldToken: {testCredential}} }

func prefixes(t *testing.T, items ...string) []netip.Prefix {
	t.Helper()
	var out []netip.Prefix
	for _, item := range items {
		p, err := netid.TrustedNetwork(item)
		if err != nil {
			t.Fatalf("building the allowlist entry %q: %v", item, err)
		}
		out = append(out, p)
	}
	return out
}

// oneFailureLimiter trips on the FIRST failure. Use it only where the test needs a
// lockout to exist quickly and does not care which key carried it.
func oneFailureLimiter() *netid.RateLimiter {
	return netid.NewRateLimiter(1, netid.DefaultFailureWindow, netid.DefaultLockout)
}

// twoFailureLimiter is what any BUCKET-SEPARATION test must use, and the reason is a
// measured mistake in this file's first draft.
//
// 🔴 WITH A ONE-FAILURE LIMITER "SEPARATE BUCKETS" IS UNOBSERVABLE. `RecordFailure`
// reports the lockout when the count REACHES the threshold, so at a threshold of 1 every
// client trips on its own first failure — and an assertion that the second client "must not
// trip" then fails against CORRECT code, which is exactly what happened. At 2, client A
// needs two failures to trip and client B's first must not, so the two keyings give
// opposite answers.
func twoFailureLimiter() *netid.RateLimiter {
	return netid.NewRateLimiter(2, netid.DefaultFailureWindow, netid.DefaultLockout)
}

// TestAnUntrustedPeerCannotSUPPLYItsOwnClientIdentity is the security claim.
//
// 🔴 IF THIS GOES RED THE SURFACE TRUSTS A FORGEABLE HEADER — on a public deploy that
// means one attacker holds as many lockout buckets as they can invent header values, so
// the lockout is decorative and the log names an address of their choosing.
func TestAnUntrustedPeerCannotSUPPLYItsOwnClientIdentity(t *testing.T) {
	l := newLiveMetered(t, prefixes(t, "192.0.2.0/24"), twoFailureLimiter())

	// 🔴 TWO DIFFERENT FORGED VALUES FROM ONE UNTRUSTED PEER. At a threshold of 2 this
	// trips ONLY if both attempts counted under the same key — the peer's. A server that
	// honoured the header would give this attacker two buckets and trip neither, which is
	// the defect, and it is why the values differ rather than repeat.
	l.doFrom("203.0.113.7:5555",
		map[string]string{netid.ClientIPHeader: "198.51.100.1"}, wrongToken(t))
	l.doFrom("203.0.113.7:5556",
		map[string]string{netid.ClientIPHeader: "198.51.100.2"}, wrongToken(t))

	got := l.log.String()
	if strings.Contains(got, "198.51.100.1") {
		t.Errorf("the handler used an UNTRUSTED peer's header as the client identity — "+
			"that header is forgeable by anybody who can reach this surface:\n%s", got)
	}
	if !strings.Contains(got, "203.0.113.7") {
		t.Errorf("the handler did not key on the untrusted peer's own address:\n%s", got)
	}
	if !strings.Contains(got, "peer-address") {
		t.Errorf("the log does not say the identity came from the PEER rather than a "+
			"trusted header, which is what tells an operator their allowlist is wrong:\n%s", got)
	}
	if !strings.Contains(got, "LOCKOUT TRIGGERED") {
		t.Errorf("two failures from ONE untrusted peer, carrying DIFFERENT forged header "+
			"values, did not trip a 2-failure limiter — so they were counted under two "+
			"keys, which means the header was honoured:\n%s", got)
	}
}

// TestATrustedPeersHeaderISTheClientIdentity is the other side. Without it the case above
// passes against a server that ignores the header unconditionally — which would make every
// caller behind a proxy share the proxy's bucket, so one of them locks out all of them.
func TestATrustedPeersHeaderISTheClientIdentity(t *testing.T) {
	l := newLiveMetered(t, prefixes(t, "192.0.2.0/24"), twoFailureLimiter())

	// Client 1 fails TWICE, which at a threshold of 2 trips its own lockout.
	l.doFrom("192.0.2.5:4444",
		map[string]string{netid.ClientIPHeader: "198.51.100.1"}, wrongToken(t))
	l.doFrom("192.0.2.5:4444",
		map[string]string{netid.ClientIPHeader: "198.51.100.1"}, wrongToken(t))
	first := l.log.String()
	if !strings.Contains(first, "LOCKOUT TRIGGERED") {
		t.Fatalf("client 1's two failures did not trip a 2-failure limiter, so the rest of "+
			"this test cannot distinguish anything:\n%s", first)
	}
	if !strings.Contains(first, "198.51.100.1") {
		t.Errorf("a TRUSTED peer's header was not used as the client identity:\n%s", first)
	}
	if !strings.Contains(first, "trusted-header") {
		t.Errorf("the log does not record that the identity came from a trusted header:\n%s", first)
	}

	// 🔴 TWO DIFFERENT CLIENTS BEHIND ONE TRUSTED PROXY ARE TWO BUCKETS. Client 1 is now
	// locked out; client 2's FIRST failure must not be, because it has its own allowance.
	// If it trips, the server pooled both under the proxy's address.
	l.doFrom("192.0.2.5:4445",
		map[string]string{netid.ClientIPHeader: "198.51.100.2"}, wrongToken(t))
	second := strings.TrimPrefix(l.log.String(), first)
	if strings.Contains(second, "LOCKOUT TRIGGERED") {
		t.Errorf("a DIFFERENT client behind the same trusted proxy inherited the first "+
			"one's failures, so the limiter is keyed on the proxy:\n%s", second)
	}
}

// TestALockedOutClientCausesNOCredentialWork.
//
// 🔴 THIS ASSERTS A CALL COUNT, AND THE FIRST VERSION ASSERTED THE REFUSAL — WHICH A
// MUTANT WALKED STRAIGHT THROUGH. Moving the lockout check below `Authenticate` still
// refuses (the check still precedes the session mint), so "it was refused" is the same
// observable under both orders and the mutant SURVIVED a fully green suite. The only thing
// that differs is whether the credential was resolved at all — which is also the reason the
// order exists: throttling a path whose entire cost is a digest plus an authority read is
// pointless if the throttle runs after that work.
//
// ⚠ AND THE SECOND REASON THAT USED TO BE CLAIMED HERE IS RETRACTED IN `session.go`:
// `netid.RateLimiter.RecordSuccess` is a deliberate no-op, so there is no failure record a
// mid-run guess could wipe.
func TestALockedOutClientCausesNOCredentialWork(t *testing.T) {
	l := newLiveMetered(t, nil, oneFailureLimiter())

	for range 2 {
		l.doFrom("203.0.113.9:1111", nil, wrongToken(t))
	}
	before := l.log.String()

	callsBefore := l.authCalls.calls
	rec := l.doFrom("203.0.113.9:1111", nil, rightToken(t))
	after := strings.TrimPrefix(l.log.String(), before)

	if strings.Contains(after, "a session was opened") {
		t.Fatalf("a locked-out client signed in with a VALID credential:\n%s", after)
	}
	// 🔴 THE DISCRIMINATING ASSERTION. Zero resolutions means the metering ran first.
	if got := l.authCalls.calls - callsBefore; got != 0 {
		t.Errorf("a locked-out client caused %d credential resolution(s); want 0. The "+
			"lockout is running AFTER the token is read, so the work it exists to prevent "+
			"is done anyway", got)
	}
	if !strings.Contains(after, "is locked out") {
		t.Errorf("the refusal does not record the lockout:\n%s", after)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status %d, want %d — a lockout must not be distinguishable by status "+
			"either: `signInRefused`'s ruling is that a refusal which discriminates is an "+
			"enumeration API", rec.Code, http.StatusUnauthorized)
	}
}

// TestEveryRefusalRendersTheSAMEBytes pins the property the lockout had to be built
// around, rather than the one it would have been convenient to relax.
func TestEveryRefusalRendersTheSAMEBytes(t *testing.T) {
	l := newLiveMetered(t, nil, oneFailureLimiter())

	cases := []struct {
		name string
		form url.Values
	}{
		{"a wrong credential", wrongToken(t)},
		{"the same peer, now locked out", wrongToken(t)},
		{"a VALID credential while locked out", rightToken(t)},
	}
	var first, firstName string
	for _, tc := range cases {
		rec := l.doFrom("203.0.113.20:1", nil, tc.form)
		body := rec.Body.String()
		if !strings.Contains(body, signInRefused) {
			t.Fatalf("%s: the body is not the uniform refusal:\n%s", tc.name, body)
		}
		if first == "" {
			first, firstName = body, tc.name
			continue
		}
		if body != first {
			t.Errorf("%q renders a DIFFERENT body from %q — that difference is the "+
				"enumeration signal `signInRefused` exists to remove", tc.name, firstName)
		}
	}
}

// TestWithNoLimiterSignInIsUNBOUNDED_AndSaysSoAnyway.
//
// ⚠ AN INVARIANT GUARD, LABELLED AS ONE. A nil limiter is a real absence — a loopback
// bring-up gets one — and this pins that the handler still resolves and still ATTRIBUTES,
// so the only thing missing is the throttle. It is not regression coverage for the lockout.
func TestWithNoLimiterSignInIsUNBOUNDED_AndSaysSoAnyway(t *testing.T) {
	l := newLiveMetered(t, nil, nil)
	for range 5 {
		l.doFrom("203.0.113.30:1", nil, wrongToken(t))
	}
	got := l.log.String()
	if strings.Contains(got, "locked out") || strings.Contains(got, "LOCKOUT") {
		t.Errorf("a nil limiter produced a lockout:\n%s", got)
	}
	if n := strings.Count(got, "203.0.113.30"); n != 5 {
		t.Errorf("want 5 attributed refusals with no limiter, got %d:\n%s", n, got)
	}
}

// TestAPeerWithNoParseableAddressIsRefusedAndCOUNTSNOTHING — fail closed.
//
// 🔴 COUNTING AN UNIDENTIFIABLE REQUEST UNDER A SHARED KEY IS THE FAILURE THE WHOLE
// CLIENT-IP DESIGN EXISTS TO AVOID: one abuser then locks out everybody. So the refusal
// records nothing, and a later identifiable client must still have a clean allowance.
func TestAPeerWithNoParseableAddressIsRefusedAndCOUNTSNOTHING(t *testing.T) {
	l := newLiveMetered(t, nil, twoFailureLimiter())

	for range 3 {
		rec := l.doFrom("not-an-address", nil, wrongToken(t))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	}
	if !strings.Contains(l.log.String(), "no client identity could be resolved") {
		t.Errorf("the refusal does not say the identity could not be resolved:\n%s", l.log.String())
	}

	// Three unidentifiable attempts counted NOTHING, so this client's FIRST failure must
	// not trip a 2-failure limiter. If it does, those three went into a shared bucket that
	// this client now shares — the "one abuser locks out everybody" failure.
	before := l.log.String()
	l.doFrom("203.0.113.40:9", nil, wrongToken(t))
	after := strings.TrimPrefix(l.log.String(), before)
	if strings.Contains(after, "LOCKOUT TRIGGERED") {
		t.Errorf("an identifiable client inherited the unidentifiable ones' failures, so "+
			"they were pooled under a shared key:\n%s", after)
	}
}

// TestASuccessfulSignInIsATTRIBUTED — the operator-facing half, and the one a public
// deploy needs most: who signed in, from where, and on what evidence.
func TestASuccessfulSignInIsATTRIBUTED(t *testing.T) {
	l := newLiveMetered(t, prefixes(t, "192.0.2.0/24"), oneFailureLimiter())
	rec := l.doFrom("192.0.2.5:1", map[string]string{netid.ClientIPHeader: "198.51.100.9"},
		rightToken(t))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a valid credential did not redirect: status %d\n%s", rec.Code, l.log.String())
	}
	got := l.log.String()
	for _, want := range []string{"a session was opened", "198.51.100.9", "trusted-header"} {
		if !strings.Contains(got, want) {
			t.Errorf("the success line is missing %q:\n%s", want, got)
		}
	}
}
