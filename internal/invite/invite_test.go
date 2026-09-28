package invite

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"
)

// The invite value type's own guards.
//
// # 🔴 WHY THIS FILE EXISTS
//
// `internal/invite` shipped with NO test file at all, and the thing that went untested is
// the thing its own doc marks load-bearing: the ORDER of [Invite.StateAt]'s arms. Measured
// on the tree before this file: moving the `expired` arm to the front left `go test ./...`
// fully green (1427 PASS / 0 FAIL) and the Postgres tier green too. A positive control —
// `StateAt` hardwired to [StateOpen] — reddened 8 tests, so the suite could REACH the
// function and was simply blind to the ordering. That is the gap this closes.
//
// # ⚠ WHAT THE ORDER IS WORTH TODAY, STATED RATHER THAN OVERSTATED
//
// It is a LABEL, not an authority decision. [Invite.Redeemable] is false for all three
// non-open states, `internal/ui`'s redemption paths collapse every one of them into
// `ErrNotRedeemable`, and the page offers the revoke button only for `open` — so a reordered
// switch changes which word a person reads on the invitations table and nothing about what
// anybody may do. The reason to pin it anyway is the one the doc gives: "already used" and
// "expired" send a person to two different places, and only one of them is a place they need
// to go. Read these as guards on a declared property that nothing measured, not as a fix for
// a live outage.
//
// # ⚠ WHAT THEY DO NOT COVER
//
// [Store] is an interface with no implementation here — `internal/pgstore` holds the real
// one and its own Postgres-tier tests hold the SQL that is a deliberate second spelling of
// [Invite.StateAt]. Nothing in this file touches a database, a clock it does not inject, or
// the handlers.

// The whole file's world is year 2000, which is this repository's synthetic-date convention.
var (
	// fixtureNow is the instant every case reads "now" as.
	fixtureNow = time.Date(2000, 3, 1, 12, 0, 0, 0, time.UTC)
	// hourAgo and hourHence straddle it by far more than any boundary question.
	hourAgo   = fixtureNow.Add(-time.Hour)
	hourHence = fixtureNow.Add(time.Hour)
)

// open is the baseline: nothing set but an expiry in the future.
func open() Invite {
	return Invite{
		Digest:    "0f0f0f",
		ProjectID: "prj_fixture",
		Role:      "member",
		Inviter:   "usr_fixture-inviter",
		CreatedAt: hourAgo,
		ExpiresAt: hourHence,
	}
}

// TestStateAtReadsEachConditionOnItsOwn walks the four states one condition at a time.
//
// ⚠ AN INVARIANT GUARD, LABELLED AS ONE. No defect ever made these four disagree; they are
// here so the ordering cases below cannot pass vacuously — a combination case asserting
// "redeemed beats expired" means nothing unless `expired` alone really is expired, and this
// is what establishes that.
func TestStateAtReadsEachConditionOnItsOwn(t *testing.T) {
	for _, tc := range []struct {
		name string
		inv  Invite
		want State
	}{
		{"nothing set and the expiry is ahead", open(), StateOpen},
		{"the expiry has passed", func() Invite { i := open(); i.ExpiresAt = hourAgo; return i }(), StateExpired},
		{"it was revoked", func() Invite { i := open(); i.RevokedAt = hourAgo; return i }(), StateRevoked},
		{"it was redeemed", func() Invite { i := open(); i.RedeemedAt = hourAgo; return i }(), StateRedeemed},
	} {
		if got := tc.inv.StateAt(fixtureNow); got != tc.want {
			t.Errorf("%s: StateAt = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestTheExpiryBoundaryIsCLOSEDAtExpiresAt measures the one instant the doc singles out.
//
// 🔴 THREE POINTS, NOT ONE, AND THE MIDDLE ONE IS THE BOUNDARY ITSELF. `StateAt` is
// `!now.Before(ExpiresAt)`, so the invitation is already expired AT its expiry instant —
// which is the spelling `identity.Session.Live` uses and which
// `internal/pgstore`'s SQL guard is pinned against at exactly this instant. A case that only
// probed an hour either side would be satisfied by `now.After(ExpiresAt)`, the off-by-one
// this boundary exists to state.
func TestTheExpiryBoundaryIsCLOSEDAtExpiresAt(t *testing.T) {
	inv := open()
	inv.ExpiresAt = fixtureNow
	for _, tc := range []struct {
		name string
		at   time.Time
		want State
	}{
		{"one nanosecond before the expiry", fixtureNow.Add(-time.Nanosecond), StateOpen},
		{"exactly at the expiry", fixtureNow, StateExpired},
		{"one nanosecond after the expiry", fixtureNow.Add(time.Nanosecond), StateExpired},
	} {
		if got := inv.StateAt(tc.at); got != tc.want {
			t.Errorf("%s: StateAt = %q, want %q — the boundary is CLOSED at ExpiresAt", tc.name, got, tc.want)
		}
	}
}

// TestTheOrderOfStateAtsArmsIsLoadBearing is the guard the package's own doc asked for and
// nothing provided.
//
// # 🔴 WHAT IT PINS AND HOW IT CAN GO RED
//
// Every pair and the triple of overlapping conditions, with the winner the doc names.
// Three reorderings were applied to a detached copy and WATCHED, and the rows each one
// reddened are what is written here rather than what was predicted — a first draft of this
// list named rows the isolated mutation leaves green:
//
//	`expired` to the FRONT of the switch     -> rows 1, 2 and 4 fail
//	`expired` above `revoked` only           -> row 2 fails (redeemed still wins 1, 3 and 4)
//	`revoked` above `redeemed`               -> rows 3 and 4 fail
//
// So every one of the three moves the doc forbids reddens at least one row, and no row is
// load-bearing for all three — which is why the table is a table.
//
// ⚠ EACH ROW ASSERTS ITS OWN PRECONDITIONS FIRST. "redeemed beats expired" is satisfied by a
// fixture that was never expired, which is the vacuous shape this table would otherwise
// have; so before comparing, each row is rebuilt with each condition set ALONE and checked to
// produce that condition's state. A fixture that stopped being both things fails loudly
// instead of passing quietly.
func TestTheOrderOfStateAtsArmsIsLoadBearing(t *testing.T) {
	// redeemed sets RedeemedAt; revoked sets RevokedAt; expired pulls ExpiresAt into the past.
	type condition struct {
		name  string
		apply func(*Invite)
		alone State
	}
	redeemed := condition{"redeemed", func(i *Invite) { i.RedeemedAt = hourAgo; i.RedeemedBy = "usr_fixture-redeemer" }, StateRedeemed}
	revoked := condition{"revoked", func(i *Invite) { i.RevokedAt = hourAgo }, StateRevoked}
	expired := condition{"expired", func(i *Invite) { i.ExpiresAt = hourAgo }, StateExpired}

	for _, tc := range []struct {
		conditions []condition
		want       State
		because    string
	}{
		{[]condition{redeemed, expired}, StateRedeemed,
			"a person clicking their own used link a week later must be told it is already used, not that " +
				"it expired — the second sends them to ask for a replacement they do not need"},
		{[]condition{revoked, expired}, StateRevoked,
			"a revocation was a DECISION somebody took; reporting it as an expiry hides that anybody took it"},
		{[]condition{redeemed, revoked}, StateRedeemed,
			"the invitation was already spent before it was withdrawn, and spent is what happened to it"},
		{[]condition{redeemed, revoked, expired}, StateRedeemed,
			"redemption outranks both, so no reordering of the other two can change this row"},
	} {
		names := ""
		for i, c := range tc.conditions {
			if i > 0 {
				names += " + "
			}
			names += c.name
		}

		// PRECONDITIONS: each condition, on its own, really does produce its own state.
		for _, c := range tc.conditions {
			solo := open()
			c.apply(&solo)
			if got := solo.StateAt(fixtureNow); got != c.alone {
				t.Fatalf("%s: the %q condition ALONE reports %q, want %q — this row's fixture is not the "+
					"overlap it is named for, so the comparison below would be vacuous", names, c.name, got, c.alone)
			}
		}

		combined := open()
		for _, c := range tc.conditions {
			c.apply(&combined)
		}
		if got := combined.StateAt(fixtureNow); got != tc.want {
			t.Errorf("%s: StateAt = %q, want %q. The ORDER OF THE ARMS in StateAt is load-bearing and is "+
				"not alphabetical: %s", names, got, tc.want, tc.because)
		}
	}
}

// TestRedeemableIsTrueForStateOpenAndNothingElse pins the wrapper to the predicate.
//
// 🔴 IT WALKS THE SAME OVERLAPS, NOT JUST THE FOUR SINGLE CONDITIONS. `Redeemable` is the only
// question the redemption path may ask, so a wrapper that stopped delegating — or that grew a
// second opinion about one of the overlapping shapes — would hand authority out on an
// invitation that is not open. It is also what makes the label-only caveat above MEASURED
// rather than asserted: every non-open shape here is equally unredeemable, so reordering the
// arms cannot change what anybody may do.
func TestRedeemableIsTrueForStateOpenAndNothingElse(t *testing.T) {
	cases := map[string]Invite{
		"open":                 open(),
		"expired":              func() Invite { i := open(); i.ExpiresAt = hourAgo; return i }(),
		"revoked":              func() Invite { i := open(); i.RevokedAt = hourAgo; return i }(),
		"redeemed":             func() Invite { i := open(); i.RedeemedAt = hourAgo; return i }(),
		"redeemed and expired": func() Invite { i := open(); i.RedeemedAt, i.ExpiresAt = hourAgo, hourAgo; return i }(),
		"revoked and expired":  func() Invite { i := open(); i.RevokedAt, i.ExpiresAt = hourAgo, hourAgo; return i }(),
		"redeemed, revoked, expired": func() Invite {
			i := open()
			i.RedeemedAt, i.RevokedAt, i.ExpiresAt = hourAgo, hourAgo, hourAgo
			return i
		}(),
	}
	for name, inv := range cases {
		want := inv.StateAt(fixtureNow) == StateOpen
		if got := inv.Redeemable(fixtureNow); got != want {
			t.Errorf("%s: Redeemable = %v but StateAt = %q — the wrapper no longer delegates",
				name, got, inv.StateAt(fixtureNow))
		}
		if name == "open" && !inv.Redeemable(fixtureNow) {
			t.Error("the OPEN fixture is not redeemable, so every false above is a fact about the fixture")
		}
		if name != "open" && inv.Redeemable(fixtureNow) {
			t.Errorf("%s is redeemable", name)
		}
	}
}

// TestTheFourStateSpellingsAreDistinctAndRendered pins the strings, because they are a
// rendered contract rather than internal names.
//
// ⚠ `internal/ui`'s `inviteRows` puts `string(inv.StateAt(now))` straight into the row a
// person reads, and `TestTheRevokeButtonIsOfferedOnlyForAnOpenInvitation` keys its fixture map
// on these exact words. Two states that collapsed to one spelling would make that walk cover
// three cases while claiming four.
func TestTheFourStateSpellingsAreDistinctAndRendered(t *testing.T) {
	want := map[State]string{
		StateOpen:     "open",
		StateRedeemed: "redeemed",
		StateRevoked:  "revoked",
		StateExpired:  "expired",
	}
	seen := map[State]bool{}
	for got, spelling := range want {
		if string(got) != spelling {
			t.Errorf("a state constant spells %q, want %q", string(got), spelling)
		}
		if seen[got] {
			t.Errorf("%q is the spelling of more than one state", got)
		}
		seen[got] = true
	}
	if len(seen) != 4 {
		t.Errorf("the closed set has %d distinct spellings, want 4", len(seen))
	}
}

// TestNewTokenReturnsATokenThatHashesToTheDigestItReturns pins the pair the whole no-stored-
// token design rests on.
//
// 🔴 THE POINT OF RETURNING BOTH IS THAT NO CALLER EVER HASHES, so nothing else in the tree
// compares these two values — a `NewToken` that returned a digest of something else would
// store rows no presented token could ever match, and every invitation would be silently
// unredeemable rather than loudly broken.
func TestNewTokenReturnsATokenThatHashesToTheDigestItReturns(t *testing.T) {
	token, digest, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if got := Digest(token); got != digest {
		t.Fatalf("Digest(token) = %q but NewToken returned digest %q — a stored row could never be "+
			"matched by the token that was handed out", got, digest)
	}
	// The digest is hex SHA-256: 64 characters, all decodable.
	if raw, err := hex.DecodeString(digest); err != nil || len(raw) != 32 {
		t.Errorf("the digest %q is not 32 hex-encoded bytes (err=%v)", digest, err)
	}
	// The token is RAW (unpadded) base64url of TokenBytes, so it needs no escaping in the
	// invitation link it travels in.
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("the token %q is not raw base64url: %v", token, err)
	}
	if len(raw) != TokenBytes {
		t.Errorf("the token decodes to %d bytes, want TokenBytes = %d", len(raw), TokenBytes)
	}
	// POSITIVE CONTROL on the comparison above: a DIFFERENT token must not produce that
	// digest, or `Digest` is a constant and the equality proved nothing.
	other, otherDigest, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if other == token || otherDigest == digest {
		t.Fatal("two calls to NewToken produced the same token or digest, so it is not minting from " +
			"crypto/rand and the equality above is about a constant")
	}
	if Digest(other) == digest {
		t.Fatal("Digest maps two different tokens onto one value")
	}
}

// TestDefaultTTLIsSevenDaysAndTokenBytesIsAFullSessionWidth pins the two numbers whose values
// the package doc argues for, so a silent narrowing is not free.
//
// ⚠ AN INVARIANT GUARD, LABELLED AS ONE. Nothing ever changed either; they are pinned because
// each is an argued security property (`TokenBytes` is deliberately equal to
// `identity.SessionIDBytes`, and it cannot be compared to it from here — this package must not
// import `internal/identity`).
func TestDefaultTTLIsSevenDaysAndTokenBytesIsAFullSessionWidth(t *testing.T) {
	if DefaultTTL != 7*24*time.Hour {
		t.Errorf("DefaultTTL = %v, want 7 days", DefaultTTL)
	}
	if TokenBytes != 32 {
		t.Errorf("TokenBytes = %d, want 32 — the same width as a session id, which is the stated argument",
			TokenBytes)
	}
}
