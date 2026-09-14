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
// Strips the two things this writer adds and the corpus already uses: the
// `- YYYY-MM-DD: ` opener and the ` [cairn: actor/session]` trailer. A bullet
// carrying neither (most of the existing corpus) comes back as its own prose, which
// is what makes a fresh append idempotent against a hand-written bullet that says
// the same thing.
func BulletContent(lines []string) string {
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, pytext.CollapseWhitespace(line))
	}
	joined := pytext.StripWhitespace(strings.Join(parts, " "))
	joined = bulletOpenerRe.ReplaceAllString(joined, "")
	joined = attributionRe.ReplaceAllString(joined, "")
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
