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
// cannot. It is never compiled on its own — `bulletTrailersRe` is the run it is a
// piece of, and `attributionPieceRe` is the SAME two classes with capture groups, built
// from the same two constants so the parser cannot accept a class the strip does not.
const attributionPattern = `\[cairn: ` + attributionActorClass + `/` + sessionClass + `\]`

// attributionActorClass is the `<actor>` half of a trailer as the grammar reads it.
//
// ⚠ NARROWER THAN WHAT `RenderBullet` WILL WRITE: the renderer formats any string, and
// a principal whose `Display` carries an `@`, an uppercase letter or a dot (a
// control-plane user's email) produces a trailer this class REJECTS. Such a trailer is
// left in the content hash and is reported as MALFORMED by `ParseAttributions`, never
// as an edge. Recorded, not widened: widening it changes what the strip removes, and
// the strip is pinned against the Python oracle by `trailerdigest_test.go`.
const attributionActorClass = `[a-z0-9][a-z0-9-]{0,31}`

// sessionClass is the `<session>` half — ONE spelling for the trailer grammar AND the
// request validator `SessionComponent`, which are the two sides of one promise: a
// session id the validator admits is one the grammar can read back.
const sessionClass = `[A-Za-z0-9][A-Za-z0-9_.-]{0,63}`

// attributionPieceRe is ONE attribution, with the actor and session captured. It is
// only ever run over the trailer RUN `bulletTrailersRe` already matched, never over a
// whole bullet — which is what keeps a trailer quoted mid-prose from becoming an edge.
var attributionPieceRe = regexp.MustCompile(
	`\[cairn: (` + attributionActorClass + `)/(` + sessionClass + `)\]`)

// malformedTrailerRe is a bracketed token that OPENS like an attribution and sits at
// the very end of what precedes the parsed run — the position a trailer occupies —
// but that the grammar did not accept (an empty half, an uppercase or `@` actor, a
// space inside). Deliberately loose: it exists to COUNT what the grammar refused, so
// a coverage line can say so, and it never produces an edge.
var malformedTrailerRe = regexp.MustCompile(`\[cairn:[^\]]*\][ \t]*\z`)

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
// per doubling out to n=64,000 / 896,048 bytes, AT-END 27.4ms and NOT-END 33.5ms, the
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
// because `lib/` cannot import `internal/`.
//
// 🔴 AND WHAT COMPARES THEM IS `trailerdigest_test.go`, NOT `tests/parity/`. This
// sentence read "`tests/parity/` is what compares them" until a round measured the
// claim: that harness diffs the two CLIENTS' rendered bytes, this is POD-side, and
// `tests/parity/README.md` contains ZERO occurrences of "trailer". It named a gate
// that does not see this function — a description claiming coverage the
// implementation does not provide, which is worse than none because it stops the
// next reader looking. The real comparison is the digest below.
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
//
// 🔴 AND IT IS NOW A SLICE AT `trailerRunStart`, WHICH `ParseAttributions` ALSO READS:
// the strip and the parser share ONE locator, so "what the strip removed" and "where
// the parser looked" cannot disagree. `TestTheParserAndTheStripAgree` checks the half
// that sharing does not give for free — that the attributions the parser RETURNS tile
// the run the strip REMOVED, piece for piece.
func stripBulletTrailers(s string) string {
	return s[:trailerRunStart(s)]
}

// trailerRunStart is the byte offset where the end-anchored trailer run begins, or
// `len(s)` when there is none. Leftmost match of an expression anchored at `\z`, so
// there is at most one, and it is non-empty when present.
func trailerRunStart(s string) int {
	loc := bulletTrailersRe.FindStringIndex(s)
	if loc == nil {
		return len(s)
	}
	return loc[0]
}

// WithoutTrailers is `s` with its END-ANCHORED machine-written trailer run removed — the run
// `trailerRunStart` locates, the same one `ParseAttributions` reads and the content hash strips — and
// nothing else: a `[cairn:` token mid-prose is not in trailer position and stays. For a DISPLAY
// surface that already shows the attribution elsewhere (the browser's session page); `s` should be
// the whole bullet, collapsed, so "end" means the bullet's end and not one line's.
func WithoutTrailers(s string) string {
	return strings.TrimRight(s[:trailerRunStart(s)], " \t")
}

// Attribution is one ` [cairn: <actor>/<session>]` piece, as written. Neither half is
// normalised: a session id is opaque and compared byte-exact.
//
// 🔴 A CLAIM THE ENTRY MAKES, NOT AN AUTHENTICATED FACT. `<actor>` was set from the
// credential only when `AppendBullet` wrote the line; `ReplaceEntry`/`CreateEntry`
// write a caller's bytes verbatim, so a trailer arriving through them says whatever
// was sent. `<session>` is self-declared on every path.
type Attribution struct {
	Actor   string
	Session string
}

// Attributions is what one bullet's trailer position holds.
type Attributions struct {
	// Pieces are the grammatical attributions in the END-ANCHORED run, left to right.
	// Usually zero or one; a run holding two is a quotation or an upstream defect, and
	// both are returned because the strip removes both.
	Pieces []Attribution

	// Malformed is true when a `[cairn:…]` token the grammar REFUSED sits where a
	// trailer goes — at the end of the bullet, or immediately before the parsed run.
	// It can be true alongside non-empty Pieces. A refused token mid-prose is prose,
	// and leaves this false.
	Malformed bool
}

// ParseAttributions reads the write trailer(s) of ONE stored bullet, given its lines
// exactly as `store.ParseJournalBullets` grouped them.
//
// 🔴 ONE DEFINITION OF "WHAT A TRAILER IS": the bullet is collapsed by the same
// function `BulletContent` uses and located by the same `trailerRunStart` the strip
// slices at, so an attribution is returned iff the content hash ignores it.
//
// Behaviour, per shape (rows of `attributions_test.go`, except the duplicate append,
// which `internal/touch`'s tests drive through `AppendBullet`):
//   - no trailer: no Pieces, not Malformed.
//   - a trailer the grammar refuses at the end (empty actor or session, an `@` or
//     uppercase actor, a space inside): no Pieces, Malformed.
//   - a non-uuid session (`ses_…`, a short word): ACCEPTED — the grammar never
//     shape-checks a session beyond `sessionClass`.
//   - a trailer on a NON-FINAL line of a wrapped bullet (prose follows it): not at the
//     end of the collapsed text, so NO Pieces and NOT Malformed. An attributed bullet
//     hand-extended after its append loses its edge — a coverage loss, and the same
//     ruling the content hash makes.
//   - a trailer at the end of the LAST continuation line: Pieces, like a one-line bullet.
//   - CRLF: `pytext.SplitLines` has already removed the terminator from each line,
//     and the collapse removes any stray `\r` as whitespace.
//   - a duplicate append: `AppendBullet` writes NOTHING on `duplicate`, so the second
//     session never reaches the bytes and there is nothing here to parse.
func ParseAttributions(lines []string) Attributions {
	// FAST PATH, NOT A SECOND DEFINITION: every attribution piece and every malformed
	// token begins with the literal `[cairn:`, which holds no whitespace, so the
	// collapse cannot create one that no line contains. Most bullets carry no trailer
	// (≈3 in 4, measured), and the regexps below were ~60% of a derivation's CPU.
	if !anyLineContains(lines, "[cairn:") {
		return Attributions{}
	}
	s := collapseBullet(lines)
	start := trailerRunStart(s)
	var out Attributions
	for _, m := range attributionPieceRe.FindAllStringSubmatch(s[start:], -1) {
		out.Pieces = append(out.Pieces, Attribution{Actor: m[1], Session: m[2]})
	}
	out.Malformed = malformedTrailerRe.MatchString(s[:start])
	return out
}

func anyLineContains(lines []string, sub string) bool {
	for _, line := range lines {
		if strings.Contains(line, sub) {
			return true
		}
	}
	return false
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
	return pytext.CollapseWhitespace(stripBulletTrailers(collapseBullet(lines)))
}

// collapseBullet is one stored bullet as ONE whitespace-collapsed string with its
// dated opener removed — the string the trailer run is located in, shared by
// `BulletContent` and `ParseAttributions` so the two cannot read different strings.
func collapseBullet(lines []string) string {
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, pytext.CollapseWhitespace(line))
	}
	joined := pytext.StripWhitespace(strings.Join(parts, " "))
	return bulletOpenerRe.ReplaceAllString(joined, "")
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
