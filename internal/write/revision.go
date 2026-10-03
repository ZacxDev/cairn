// Package write holds the store's write primitives.
//
// 🔴 THE STORE IS NOT RE-DERIVABLE — it records gotchas, retracted theories and
// measurements that were true at a moment — so A LOST APPEND IS LOST FOREVER.
// Everything here is shaped by that one fact: the failure that matters is silent
// content destruction, not unavailability, and unavailability is the direction
// every ambiguity resolves towards.
package write

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// ContentHashChars is how much of a hash is carried. 16 hex characters is 64 bits
// — far past any collision an append stream could reach, and short enough to read
// in an audit line.
const ContentHashChars = 16

// BulletTextMax is the editorial cap on one bullet's text.
//
// 🔴 ONE CONSTANT FOR THE SERVER AND THE CLIENT, which is why the Python side
// imports it from a shared module rather than defining it in the server. It used
// to be server-local, which made the only way to learn the limit exceeding it: the
// client had no copy, so every over-long bullet cost a composed payload and a round
// trip. A second hardcoded 2000 would buy the local check at the price of two
// numbers that can drift silently.
const BulletTextMax = 2000

// Attribution is the trailer every appended bullet carries.
//
// 🔴 IT IS A SUFFIX, AND THE POSITION IS LOAD-BEARING RATHER THAN COSMETIC. The
// store's bullet grammar is a PREFIX grammar — the date reader and the openness
// reader are both anchored at position 0 with an EXACT terminator. Writing the
// actor between the date and the text (`- 2000-01-02 (zach): OPEN: …`) parses as NO
// MARKER, which is precisely the near-miss class: the badge silently stops
// rendering, and a vanished badge looks like success. A suffix leaves every prefix
// rule untouched, so an appended `OPEN:` bullet still declares itself.
const attributionFormat = " [cairn: %s/%s]"

// attributionRe parses the suffix above, and it is deliberately the SAME shape
// `RenderBullet` writes rather than a looser one: a trailer this cannot read is not
// an attribution, so its bullet's content hash is computed over the whole line and
// simply will not collide with a fresh append. Anchored at end-of-line.
var attributionRe = regexp.MustCompile(
	`[ \t]*\[cairn: [a-z0-9][a-z0-9-]{0,31}/[A-Za-z0-9][A-Za-z0-9_.-]{0,63}\]\z`)

// bulletOpenerRe is `- YYYY-MM-DD: ` — the dated bullet opener this writer emits
// and the one the corpus already uses. Stripped before hashing so a bullet
// re-POSTed on a later day is still recognised as the same CONTENT.
var bulletOpenerRe = regexp.MustCompile(`\A[-*][ \t]+(?:\d{4}-\d{2}-\d{2}:[ \t]+)?`)

// citationTokenRe is the opaque per-bullet token a READ surface prints —
// `store.JournalBullet.CitationID` in brackets, 8 lowercase hex. Anchored at
// end-of-line, the same shape `attributionRe` is.
//
// 🔴 IT IS STRIPPED BEFORE HASHING BECAUSE AN AGENT ECHOES WHAT IT READ, AND THAT
// IS MEASURED RATHER THAN FEARED: a downstream consumer's resume flow requires its
// report to echo the bullets it recalled, which fires on 451 of 454 runs (99.3%).
// So the text arriving at `POST /bullets` routinely ENDS in whatever the read
// surface appended. A token left in the hash breaks idempotency in the one
// direction that destroys nothing and is therefore silent: the re-POST stops
// matching the bullet already on disk, appends a near-duplicate, and that duplicate
// carries a `[cb:…]` in its PROSE whose value is not its own id — a citation that
// resolves to the bullet it was copied from, forever.
var citationTokenRe = regexp.MustCompile(`[ \t]*\[cb:[0-9a-f]{8}\]\z`)

// stripBulletTrailers removes every MACHINE-WRITTEN trailer from the end of one
// collapsed bullet, in ANY ORDER and ANY NUMBER, and nothing else.
//
// 🔴 ORDER-FREE IS THE POINT, AND IT IS WHY THIS IS A LOOP RATHER THAN TWO
// SEQUENTIAL `ReplaceAllString` CALLS, which are correct for exactly one ordering:
// with `attributionRe` applied first, `… [cairn: a/b] [cb:deadbeef]` leaves the
// attribution IN (its anchor no longer reaches the end of the line) and the whole
// trailer enters the hash. Both orders are reachable — the read surface appends the
// token after a stored line that already ends in an attribution, and
// `RenderBullet` appends an attribution after whatever text the caller sent.
//
// 🔴 ONE RULE, ONE PLACE: `BulletContent` is the only caller, and nothing else in
// this package or in `server/server.py` may re-spell "what a trailer is". The two
// languages carry one transcription each (`server.py`'s `_strip_bullet_trailers`)
// because `lib/` cannot import `internal/`, and `tests/parity/` is what compares
// them.
//
// ⚠ IT STRIPS REPEATEDLY, so a line that somehow ends in two attributions loses
// both. That is a widening over the single-pass version it replaces, it is in the
// idempotency-preserving direction, and a line carrying two is already a defect
// somewhere upstream. The loop terminates because every iteration shortens the
// string.
func stripBulletTrailers(s string) string {
	for {
		if next := attributionRe.ReplaceAllString(s, ""); next != s {
			s = next
			continue
		}
		if next := citationTokenRe.ReplaceAllString(s, ""); next != s {
			s = next
			continue
		}
		return s
	}
}

// EntryRevision is the revision an `If-Match` is compared against: the entry
// file's CONTENT.
//
// 🔴 THIS IS DELIBERATELY **NOT** THE SCOPE'S GIT HEAD, AND THE DEVIATION IS THE
// ONLY THING THAT MAKES THE PRECONDITION A GUARD AT ALL. No scope in the served
// copy is a git repository, so a scope revision answers "unknown" for every scope
// in the store — a precondition keyed on it would be satisfied by every caller
// sending the literal string `unknown`, forever, and could never refuse a stale
// write. A guard that cannot fail is not a guard.
//
// The entry's own content hash is the value a lost-update check actually needs: it
// changes exactly when the bytes a caller based its edit on change. It is also
// derivable by any client OFFLINE — `/snapshot` ships the entry files, so a cache
// can compute the revision of what it holds without a round trip and without a
// second endpoint existing to hand it out.
func EntryRevision(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:ContentHashChars]
}

// ContentHash is the idempotency key for ONE bullet: its CONTENT, and nothing else.
//
// Whitespace is collapsed before hashing so a re-POST that differs only in
// wrapping is the same bullet; the DATE, the ACTOR and the SESSION are not in it, so
// the same observation re-sent by the same agent after a timeout — or on the next
// day — is recognised rather than duplicated.
//
// ⚠ AND THE CONSEQUENCE, STATED RATHER THAN LEFT TO BE MET: a genuinely NEW bullet
// whose text is byte-identical to one already in the entry is treated as already
// recorded and is NOT appended. That is the idempotency criterion working, not a
// bug — but it means the same sentence written twice six months apart records once.
// A caller that means both must say something different in the second, which the
// store's own convention (a leading date in the prose, a sha, a run id) already
// produces.
func ContentHash(text string) string {
	sum := sha256.Sum256([]byte(pytext.CollapseWhitespace(text)))
	return hex.EncodeToString(sum[:])[:ContentHashChars]
}

// BulletContent reduces one STORED bullet to the content its hash is taken over.
//
// Strips what a MACHINE put there and the corpus already uses: the
// `- YYYY-MM-DD: ` opener, the ` [cairn: actor/session]` trailer, and the
// ` [cb:xxxxxxxx]` citation token a read surface prints — the last two in any order,
// see `stripBulletTrailers`. A bullet carrying none of them (most of the existing
// corpus) comes back as its own prose, which is what makes a fresh append idempotent
// against a hand-written bullet that says the same thing.
//
// ⚠ AND THE CITATION TOKEN COUNTS AGAINST `BulletTextMax` RATHER THAN BEING EXEMPT.
// Said here because this function is where someone would look for the exemption: the
// cap is measured on the text a caller SUBMITS (`bullet_request.go`, and the client's
// own pre-check), before anything is stripped, so an echoed token spends 14 of the
// 2000 characters. Exempting it would mean the cap measured a different string in the
// validator than here, and two clients would have to agree on that difference.
func BulletContent(lines []string) string {
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, pytext.CollapseWhitespace(line))
	}
	joined := pytext.StripWhitespace(strings.Join(parts, " "))
	joined = bulletOpenerRe.ReplaceAllString(joined, "")
	joined = stripBulletTrailers(joined)
	return pytext.CollapseWhitespace(joined)
}

// RenderBullet is the line that goes on disk. ONE line, always attributed.
//
// 🔴 `actor` IS THE AUTHENTICATED IDENTITY AND NOTHING ELSE. This function has no
// parameter a request body can reach; the caller passes the identity that came off
// the credential the match produced. That is the whole of the attribution
// guarantee, and it is structural: there is no field here for a client-supplied
// name to land in.
//
// ⚠ AND THE GUARANTEE IS EXACTLY AS WIDE AS THIS FUNCTION'S CALLERS. Only
// AppendBullet renders through here; ReplaceEntry and CreateEntry write the
// caller's bytes verbatim and enforce nothing. Stated because "the actor comes from
// the token" reads like a property of the SERVER, and it is a property of one ROUTE.
func RenderBullet(text, actor, session, today string) string {
	return "- " + today + ": " + pytext.StripWhitespace(text) +
		fmt.Sprintf(attributionFormat, actor, session)
}
