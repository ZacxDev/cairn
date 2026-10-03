package write

import (
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// TestTheRenderedCitationTokenIsStrippedByTheWritePath pins a RELATIONSHIP between two
// packages that neither package's own tests can see, and it is the seam this feature
// created: `internal/store.CitationToken` is the FORMAT a read surface prints and
// `citationTokenPattern` here is the PATTERN that strips it before a bullet's content hash
// is taken.
//
// 🔴 A FORMAT STRING IS NOT A REGEXP, SO NOTHING CAN DERIVE ONE FROM THE OTHER. Both sides
// were hermetically tested before this guard existed — `internal/write`'s battery over
// hand-written `[cb:deadbeef]` strings, `internal/report`'s over whatever its own helper
// emits — and every one of those fixtures spelled the token by hand. The defect this
// refuses lives in the gap: a renderer that printed `[cb: deadbeef]`, or `[CB:…]`, or
// dropped the leading space, passes every test on both sides and silently leaves the token
// in `ContentHash`.
//
// 🔴 WHY THAT MATTERS, MEASURED RATHER THAN FEARED: a downstream consumer's resume flow
// requires its report to echo the bullets it recalled, which fires on 451 of 454 runs
// (99.3%). So the text arriving at `POST /bullets` routinely ends in whatever the read
// surface appended. A token left in the hash breaks idempotency in the direction that
// destroys nothing and is therefore silent — the re-POST stops matching the bullet already
// on disk, appends a near-duplicate, and that duplicate carries a `[cb:…]` in its PROSE
// whose value names the bullet it was COPIED FROM, forever.
//
// ⚠ IT IS AN INVARIANT GUARD AGAINST `d7e1fec8`, NOT REGRESSION COVERAGE, and is labelled
// one deliberately: at base the renderer printed no token, so no echo existed to strip and
// nothing could have violated this. It becomes reachable in this change. What it covers is
// the next edit to either side.
func TestTheRenderedCitationTokenIsStrippedByTheWritePath(t *testing.T) {
	// 🔴 THE ID COMES FROM THE REAL DERIVATION, NOT FROM A HAND-SPELLED 8 HEX, because the
	// hazard is precisely that a hand-spelled fixture agrees with both sides while the live
	// value does not. `CitationID()` over a parsed bullet is what a read surface prints.
	bullets := store.ParseJournalBullets("- 2000-01-02: the retry budget is still unbounded.")
	if len(bullets) != 1 {
		t.Fatalf("the fixture body parsed to %d bullets, want 1", len(bullets))
	}
	stored := bullets[0].FirstLine()
	id := bullets[0].CitationID()

	// The echo, assembled exactly as a reader sees it: the stored line plus the token the
	// renderer appends to it.
	echoed := stored + store.CitationToken(id)

	if got, want := BulletContent([]string{echoed}), BulletContent([]string{stored}); got != want {
		t.Fatalf("an echoed citation token survived the strip, so a re-POST of a recalled "+
			"bullet will not match the bullet on disk\n  echoed  -> %q\n  stored  -> %q\n"+
			"  token   -> %q", got, want, store.CitationToken(id))
	}
	if ContentHash(echoed) == ContentHash(stored) {
		// ⚠ NOT THE ASSERTION — THE CONTROL. `ContentHash` is taken over the RAW submitted
		// text, so these two must DIFFER; if they did not, the comparison above could pass
		// against a strip that does nothing at all because the inputs were already equal.
		t.Fatal("the raw hashes of the echoed and stored lines are equal, so this fixture " +
			"cannot tell a working strip from no strip")
	}

	// 🔴 AND THE ORDER-FREE HALF, WHICH IS REACHABLE AND NOT HYPOTHETICAL: the read surface
	// appends its token AFTER a stored line that already ends in a write attribution, so the
	// trailer run is `[cairn: …] [cb:…]` in that order on every appended bullet in a real
	// store.
	attributed := RenderBullet("the retry budget is still unbounded.", "fixture-actor",
		"sess-0000000000000001", "2000-01-02")
	attributedBullets := store.ParseJournalBullets(attributed)
	if len(attributedBullets) != 1 {
		t.Fatalf("the attributed line parsed to %d bullets, want 1", len(attributedBullets))
	}
	bothTrailers := attributed + store.CitationToken(attributedBullets[0].CitationID())
	if got, want := BulletContent([]string{bothTrailers}),
		BulletContent([]string{attributed}); got != want {
		t.Fatalf("an attribution followed by a citation token did not strip to the same "+
			"content\n  both   -> %q\n  attrib -> %q", got, want)
	}

	// 🔴 THE NEGATIVE CONTROL ON THE STRIP ITSELF: a trailer this pattern cannot READ is not
	// a trailer, and must stay in the hash. Without this, a pattern widened to `\[cb:[^]]*\]`
	// — which would swallow arbitrary prose a writer put in brackets — passes everything
	// above.
	for _, bad := range []string{
		" [cb:deadbeef9]", // nine hex
		" [cb:deadbee]",   // seven hex
		" [cb:DEADBEEF]",  // upper case
		" [cb: deadbeef]", // a space inside
		" [cb:deadbeeg]",  // not hex
		" (cb:deadbeef)",  // the wrong brackets
	} {
		if BulletContent([]string{stored + bad}) == BulletContent([]string{stored}) {
			t.Errorf("%q was stripped as a citation token, but it is not one — the pattern is "+
				"wider than the format", bad)
		}
	}

	// And the format itself, pinned as a WHOLE NORMALISED STRING rather than by a property,
	// because a property check ("it contains the id") is satisfied by every one of the six
	// malformed spellings above.
	if store.CitationToken("deadbeef") != " [cb:deadbeef]" {
		t.Errorf("the rendered token spelling moved: %q", store.CitationToken("deadbeef"))
	}
	if n := len(store.CitationToken("deadbeef")); n != 14 {
		t.Errorf("the token is %d bytes; every cost claim in this tree says 14", n)
	}
	if !strings.HasPrefix(store.CitationToken("deadbeef"), " ") {
		t.Error("the leading space is part of the token, or every annotated line runs its " +
			"prose into the bracket")
	}
}
