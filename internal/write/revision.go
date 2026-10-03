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

// attributionPattern is the suffix above as a pattern, and it is deliberately the
// SAME shape `RenderBullet` writes rather than a looser one: a trailer this cannot
// read is not an attribution, so its bullet's content hash is computed over the whole
// line and simply will not collide with a fresh append.
//
// ⚠ A PATTERN FRAGMENT, NOT AN ANCHORED EXPRESSION, and that is what makes the one
// alternation below possible: a fragment can be repeated, an end-anchored expression
// cannot. It is never compiled on its own — `bulletTrailersRe` is the only consumer.
const attributionPattern = `\[cairn: [a-z0-9][a-z0-9-]{0,31}/[A-Za-z0-9][A-Za-z0-9_.-]{0,63}\]`

// citationTokenPattern is the opaque per-bullet token a READ surface prints —
// `store.JournalBullet.CitationID` in brackets, 8 lowercase hex. The same kind of
// fragment `attributionPattern` is.
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
const citationTokenPattern = `\[cb:[0-9a-f]{8}\]`

// bulletOpenerRe is `- YYYY-MM-DD: ` — the dated bullet opener this writer emits
// and the one the corpus already uses. Stripped before hashing so a bullet
// re-POSTed on a later day is still recognised as the same CONTENT.
//
// 🔴 IT IS ALSO THE REQUEST-SIDE PREDICATE, NOT ONLY THE STORED-SIDE ONE.
// `BulletRequestProblem` asks THIS expression whether a submitted `text` opens a
// bullet, over the same collapsed string `BulletContent` will reduce — see the
// clause there for what asking a different question cost.
var bulletOpenerRe = regexp.MustCompile(`\A[-*][ \t]+(?:\d{4}-\d{2}-\d{2}:[ \t]+)?`)

// bulletTrailersRe matches the WHOLE run of machine-written trailers at the end of
// one collapsed bullet — any order, any number — in ONE anchored alternation.
//
// 🔴 ORDER-FREE IS THE POINT, AND TWO SEQUENTIAL `ReplaceAllString` CALLS CANNOT
// DELIVER IT: with the attribution stripped first, `… [cairn: a/b] [cb:deadbeef]`
// leaves the attribution IN (its anchor no longer reaches the end of the line) and
// the whole trailer enters the hash. Both orders are reachable — the read surface
// appends the token after a stored line that already ends in an attribution, and
// `RenderBullet` appends an attribution after whatever text the caller sent. ONE
// alternation under a `+` is order-free without sequencing anything.
//
// 🔴 AND IT REPLACES A LOOP, WHICH WAS ORDER-FREE AND QUADRATIC. The loop re-ran an
// end-anchored expression until it stopped shrinking the string, so each iteration
// re-scanned from position 0: O(trailers × length) on a path that runs once per
// stored bullet on every `POST /bullets`, inside the per-entry write lock, with no
// per-line length cap on the bytes a write can put on disk. MEASURED on this host
// over one stored line of n trailing ` [cb:deadbeef]` tokens, `BulletContent` end to
// end, Go 1.25:
//
//	n        line bytes   loop        this
//	1                48   2.9µs       0.6µs
//	1,000        14,034   234ms       0.30ms
//	4,000        56,034   4.33s       1.15ms
//	16,000      224,034   72.8s       4.71ms
//
// EQUIVALENCE, re-derived rather than inherited: enumerated over every suffix
// sequence of length 0..3 drawn from 20 trailer-ish pieces (5 attribution spellings,
// 5 token spellings, 10 malformed ones) crossed with 10 prose prefixes — 84,210
// inputs in Go and the same 84,210 in Python — the loop and this expression agree on
// ALL of them, zero disagreements, including every row
// `citationtoken_test.go`/`test_bullet_trailer_strip.py` assert.
//
// 🔴 AND THIS EXPRESSION IS NO LONGER WHAT PYTHON CARRIES, WHICH IS A FACT ABOUT THE
// ENGINE AND NOT ABOUT THE GRAMMAR. The same `+`-and-anchor shape is QUADRATIC under
// CPython's backtracking engine whenever the trailer run does NOT reach end-of-line:
// `re` retries the alternation from every start position, and each of the ~n positions
// INSIDE the run matches its way to the final non-trailer word before failing — O(n)
// apiece (8.8s over a 224 KB line; `server.py`'s
// `_BULLET_TRAILER_PIECE_RE` carries the table). RE2 has no backtracking, so this
// side is linear on BOTH suffix shapes — measured in one process in one run, ×1.7-2.4
// per doubling out to n=64,000 / 896,059 bytes, AT-END 27.4ms and NOT-END 33.5ms, the
// per-n rows in `TestTheTrailerStripIsLinearRatherThanQuadratic`. `server.py` peels
// the run one piece at a time instead. Two mechanisms, one answer, and the answer is
// what `trailerdigest_test.go` compares — never the expression.
//
// ⚠ THE WIDENING THE LOOP BOUGHT IS KEPT, AND IT IS WHY THE `+` IS NOT A `?`: a line
// that somehow ends in two attributions loses both. In the idempotency-preserving
// direction, and a line carrying two is already a defect somewhere upstream.
var bulletTrailersRe = regexp.MustCompile(
	`(?:[ \t]*(?:` + attributionPattern + `|` + citationTokenPattern + `))+\z`)

// stripBulletTrailers removes every MACHINE-WRITTEN trailer from the end of one
// collapsed bullet, in ANY ORDER and ANY NUMBER, and nothing else.
//
// 🔴 ONE RULE, ONE PLACE: `BulletContent` is the only caller, and nothing else in
// this package or in `server/server.py` may re-spell "what a trailer is". The two
// languages carry one implementation each (`server.py`'s `_strip_bullet_trailers`)
// because `lib/` cannot import `internal/`, and `tests/parity/` is what compares
// them.
//
// ⚠ AND IT IS NO LONGER A TRANSCRIPTION — THE TWO ARE TWO MECHANISMS OVER ONE
// GRAMMAR, which is a stronger claim to hold up than a shared expression. This keeps
// the whole-run alternation because RE2 cannot backtrack; `server.py` peels the run
// one piece at a time because CPython's engine can and does. See `bulletTrailersRe`
// for the measurement, and `trailerdigest_test.go` for the one constant both
// implementations are pinned to — which is what makes the equivalence measured rather
// than assumed now that there is no shared expression to read it off.
//
// One pass. `bulletTrailersRe` is anchored at `\z` and every alternative consumes at
// least one bracketed token, so there is exactly one possible match and no empty one
// — `ReplaceAllString` and a slice at the match start are the same operation here.
func stripBulletTrailers(s string) string {
	return bulletTrailersRe.ReplaceAllString(s, "")
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
