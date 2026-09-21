package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// 🔴 THE STORAGE DECISION, AND THE ALTERNATIVES IT BEAT. P6 needs browser sessions, and
// the operator stated TWO requirements for them rather than one. (i) A LOGOUT THAT
// ACTUALLY REVOKES — which is why a client-held JWT was refused at the start: a signed
// assertion is valid until its `exp` and no server can take it back. (ii) DURABILITY
// ACROSS A PROCESS RESTART AND A ROLLING DEPLOY, claimed on its own authority and NOT
// derived from (i): "a rolling deploy signs NOBODY out" is the requirement, and a brief
// 503 while a replica restarts is acceptable where a sign-out is not. Stating (ii)
// separately matters because deriving it from (i) is circular — a logout that does not
// survive a restart is still a logout — and the circular version was written here first.
//
// Four shapes were priced against this tree, and the rejected three are written
// down here because the reasoning is what stops somebody re-deriving them.
//
//   - (a) A FILE-BACKED STORE IN THE SHAPE OF `control.FileStore` — CHOSEN. Durable
//     across restart, revocable in one write, and it reuses mechanics this repository has
//     already measured: an exclusive `flock`, a re-read UNDER that lock rather than from a
//     cache, and a `Sync` before success is reported. See `FileSessionStore`, which
//     differs from `control.FileStore` in exactly one respect (it REWRITES rather than
//     appends) and says why there.
//
//   - (b) SESSIONS AS EVENTS IN THE CONTROL JOURNAL — REJECTED. 🔴 THE REASON THIS
//     BULLET GAVE FIRST WAS FALSE, AND THE CORRECTION IS RECORDED RATHER THAN QUIETLY
//     SWAPPED, because it was the load-bearing half of a published justification. It read
//     "`control.FileStore.Model` re-reads and REPLAYS the whole journal on every call, and
//     every authenticated request calls it". The first clause is true; the second is not.
//     Every serving path wraps the store in a `control.Cache` — `internal/api/server.go`
//     and `cmd/cairn-server/createuser.go`, whose comment states the reason outright
//     ("reads must not touch the authority") — and `control.Cache.Model` returns the
//     materialized value under an RLock without contacting its source. The replay happens
//     once per `api.AuthorityRefreshInterval` (30s), not once per request. `cairn-ui`
//     wires no `FileStore` at all: its authority is a `control.Cache` over
//     `tokenfile.Source`.
//
//     What sessions-in-the-journal would ACTUALLY cost, which is the reason that holds:
//     the authority is read through a cache with a declared staleness bound, so a logout
//     would become EVENTUAL — up to one refresh interval — and bypassing the cache to make
//     it immediate is exactly what would reinstate the replay-per-request the false clause
//     imagined. So (i) is met only in the weakened form "revokes within 30 seconds", and
//     the strong form costs what the false clause claimed the weak one already did.
//
//     🔴 AND THERE IS A STRUCTURAL BLOCKER NOBODY HAD WRITTEN DOWN, which is harder than
//     any read cost: the journal's volume is ReadWriteOnce on a node-local storage class
//     (`claudedocs/handoff-cairn-control-plane.md`), so sessions living in it would pin
//     every UI replica to that one pod's node. Multi-replica dies by construction, and no
//     amount of caching changes it.
//
//     The category objection stands beside both and would survive a fix to either: the
//     journal answers "who could see this, and when", and an operator reading it for a
//     grant history would be reading it through session noise.
//
//   - (c) A SIGNED STATELESS COOKIE PLUS A REVOCATION LIST — REJECTED because it is (a)
//     with extra parts. The revocation list is a durable store that must survive restart
//     or logout is a lie, so nothing is saved; it adds a signing key, which is a new
//     secret with a new rotation story and a new way to be misconfigured; and its entries
//     must be kept until each revoked token's `exp`, so the store it was meant to avoid
//     is one that can only GROW. A session id is already an unguessable 256-bit value —
//     signing it proves nothing the lookup does not.
//
//   - (d) IN-MEMORY ONLY — REJECTED, and this one is worth being precise about because it
//     is the cheapest and it does satisfy "logout revokes". Two costs. Every restart signs
//     everybody out, which on a rolling deploy is not a rare event. And it is a NEW KIND
//     of multi-replica failure, not a worse version of the one
//     `internal/control/README.md` already declares: that blind spot is "two pods over one
//     journal refresh independently and can serve different epochs at the same instant" —
//     divergence about one durable truth, which converges. An in-memory session table has
//     no shared truth to converge on: a session opened on pod A does not exist on pod B
//     and never will, so a load balancer without sticky sessions produces a surface that
//     signs the user out on a random fraction of requests. It makes the declared blind
//     spot WORSE in that sense.
//
// 🔴 WHAT (a) DOES NOT BUY, SAID OPERATIONALLY SO NOBODY READS "DURABLE" AS "DISTRIBUTED"
// — AND THE HONEST VERSION IS BLUNTER THAN "(a) DOES NOT FIX REPLICATION EITHER".
// `cairn-ui` IS A SINGLE-REPLICA SURFACE TODAY. (a) inherits exactly the journal's
// shared-filesystem assumption, no more and no less; a second replica WITHOUT shared
// storage is not a degraded version of this design, it is a surface that signs users out
// on a random fraction of requests — the SAME failure (d) is rejected for. And "just share
// the filesystem" is not a configuration away: the journal's volume is ReadWriteOnce on a
// node-local storage class, so a shared session file pins every replica to one node.
//
// ⚠ THE DIRECTION, RECORDED AS A DIRECTION AND NOT AS A COMMITMENT. Requirement (ii)
// above — a rolling deploy signs NOBODY out — is met on one replica by this store, given a
// volume that outlives the process; the store's half of that is `Sync`-before-success, and
// the volume's half is a deployment property nothing in this package can assert. The intended answer for more than one replica
// is STICKY ROUTING: a StatefulSet with a per-ordinal volume, so each replica owns its own
// session table and a restart returns to it. That needs no change here and keeps the
// stdlib-only property. It is a separate arc, and it is where two other things get
// decided: whether `control.Store`'s eventual Postgres backend takes sessions with it, and
// `FileSessionStore.Revoke`'s cost — `Revoke` runs the full `mutate` (`flock`, whole-file
// re-read, rewrite, two `Sync`s) even when the digest is ABSENT, which no authenticated
// caller gains anything by but which is a lock-contention amplifier the moment N replicas
// share one file.

// SessionCookieName is the cookie the browser holds, and the `__Host-` prefix is a guard
// rather than a naming convention.
//
// 🔴 `__Host-` IS ENFORCED BY THE BROWSER AND IT CLOSES A SUBDOMAIN ATTACK THIS SERVER
// CANNOT SEE. A cookie without the prefix can be written by any sibling host under the
// registrable domain — `anything.example` can set a cookie that `ui.example` will then
// send — which is session FIXATION performed entirely outside this process, invisible to
// every guard here. The prefix makes a conforming browser refuse any such cookie unless
// it is `Secure`, has `Path=/` and carries NO `Domain` attribute, which a sibling host
// cannot satisfy for us.
//
// ⚠ IT IS A CLAIM ABOUT BROWSERS AND NOTHING IN THIS REPOSITORY HAS MEASURED IT. No test
// here drives a browser; see `internal/ui/README.md`'s list of what this surface's tests
// structurally cannot see. The failure direction if the claim is wrong is the safe one —
// a browser that does not implement the prefix treats it as an ordinary name — so the
// cost of being wrong is that we do not have a guard we thought we had, not that we have
// opened one.
const SessionCookieName = "__Host-cairn-session"

// SessionIDBytes is the entropy behind one session id, before encoding.
//
// 🔴 32 BYTES FROM `crypto/rand`, AND THE NUMBER IS NOT A ROUND-NUMBER CHOICE. A session
// id is a bearer credential with exactly the authority of the principal behind it, and
// the only thing standing between an attacker and it is that they cannot guess it. 256
// bits is the width at which guessing is not a strategy at any request rate a network
// permits; it is also `sha256.Size`, so the stored digest is no wider than the secret it
// stands for.
const SessionIDBytes = 32

// DefaultSessionTTL is how long a session lives if a caller does not say.
//
// ⚠ IT IS AN ABSOLUTE LIFETIME AND IT IS NOT REFRESHED ON USE, which is a decision rather
// than an omission. A sliding expiry means a stolen cookie stays live for as long as the
// thief keeps using it — the one case where the bound matters most is the one a sliding
// window removes it for. The cost is that an active operator is signed out mid-session
// once a day, which is visible and recoverable; the cost of the other choice is invisible.
const DefaultSessionTTL = 12 * time.Hour

// csrfLabel is the HMAC's message. A label rather than an empty message so that a second
// value derived from a session id later cannot collide with this one by construction.
const csrfLabel = "cairn-csrf-v1"

// Session is one live browser session, as the store holds it.
//
// 🔴 IT HOLDS A DIGEST, NEVER THE ID. The file this is written to is a set of bearer
// credentials if it holds ids — read it and you are every signed-in user. Storing
// `sha256(id)` makes a leaked store file useless for authenticating: the lookup hashes
// what the browser presented and compares digests, exactly as the token table already
// does for machine tokens. The same reasoning as `control.FileStore`'s "it holds
// credential DIGESTS, which are not secrets in the sense a token is".
//
// 🔴 AND IT HOLDS A PRINCIPAL REFERENCE, NOT A SNAPSHOT OF AUTHORITY. `Kind`+`Principal`
// are re-resolved through `control.Resolve` on EVERY request, so a grant revoked while a
// session is open takes effect on the next request rather than at the next sign-in. A
// `control.Authorization` frozen into this record would be a second, stale answer to
// "what may this caller see", which is the one thing `internal/control` exists to forbid.
type Session struct {
	// Digest is the hex `sha256` of the session id. The id itself is never stored.
	Digest string `json:"digest"`
	// Kind and Principal name the principal this session acts as.
	Kind      control.Kind `json:"kind"`
	Principal control.ID   `json:"principal"`
	// IssuedAt is for an operator reading the file, never for an authorization
	// decision.
	IssuedAt time.Time `json:"issued_at"`
	// ExpiresAt is the absolute lifetime. A session at or past it is refused and is
	// dropped by the next write.
	ExpiresAt time.Time `json:"expires_at"`
}

// Live answers whether this record is usable at `now`.
//
// ⚠ THE BOUNDARY IS CLOSED AT `ExpiresAt`: a session is dead AT its expiry instant, not
// after it. `!now.Before(ExpiresAt)` rather than `now.After(ExpiresAt)` — the two differ
// on exactly one instant, and an injected clock lands on exactly that instant whenever a
// test pins it there, which is how an off-by-one in the safe-looking direction survives.
func (s Session) Live(now time.Time) bool { return now.Before(s.ExpiresAt) }

// SessionStore is the durable half. An interface so the backend can be tested without a
// filesystem and so the Postgres-backed one that arrives with `control.Store`'s second
// implementation has a seam to land in.
//
// 🔴 EVERY METHOD TAKES THE PRESENTED ID, NOT A DIGEST, AND HASHES IT ITSELF. A caller
// that hashed would be the second place the hash function is chosen, and the two would
// disagree the day one of them changes. It also means no caller ever needs to hold a
// digest, so no caller can log one.
type SessionStore interface {
	// Create stores one new session.
	Create(rec Session) error
	// Lookup resolves a presented id to a LIVE session. A record that exists and has
	// expired answers `false`, the same as one that does not exist: the two are the
	// same observable to a caller, deliberately.
	Lookup(presentedID string) (Session, bool, error)
	// Revoke removes the session a presented id names. Revoking an id with no session
	// is not an error — logout must succeed for a browser holding a cookie the store
	// has already forgotten, or a user with a stale cookie can never clear it.
	Revoke(presentedID string) error
}

// ErrNoSessionStore refuses a cookie backend with nothing to look a session up in, at
// CONSTRUCTION time — the same fail-closed direction `ErrNoAuthority` takes.
var ErrNoSessionStore = errors.New("identity: no session store was configured, so no cookie could ever be resolved")

// NewSessionID mints one session id.
//
// 🔴 THE ERROR IS RETURNED RATHER THAN SWALLOWED. `crypto/rand.Read` failing is not a
// condition this program can carry on through: the alternative to a random id is a
// guessable one, so a caller that ignored this would mint a credential whose whole
// security property had silently gone.
func NewSessionID() (string, error) {
	buf := make([]byte, SessionIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	// Raw URL encoding: no padding, no `+` or `/`, so the value needs no escaping in a
	// cookie and cannot be corrupted by a proxy that re-encodes one.
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// SessionDigest is the ONE hash of a session id, so there is one place the function is
// chosen and no second spelling to disagree with it.
func SessionDigest(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// digestsEqual compares two stored digests WITHOUT leaking their difference in time.
//
// ⚠ ITS HONEST SCOPE, BECAUSE OVERSTATING IT IS HOW A GUARD GETS TRUSTED FOR THE WRONG
// REASON: the values compared here are SHA-256 digests, so a timing oracle on this
// comparison leaks a prefix of a digest, which cannot be inverted to the id a browser
// would have to present. Constant time here is defence in depth. Where it is genuinely
// load-bearing is [CSRFTokenValid], which compares an attacker-supplied string against a
// secret-derived one directly — that one uses `hmac.Equal`, and for the same reason.
//
// 🔴 THE PROPERTY WORTH PINNING IN THE LOOKUP IS NOT THIS FUNCTION BUT THE SCAN AROUND
// IT: a scan that stops at the first match leaks the matched record's POSITION in the
// file through time, which is a fact about the store's contents rather than about a
// digest. `FileSessionStore.Lookup` does not short-circuit, and
// `TestTheLookupScanHasNoEarlyExit` is what pins that — structurally, over the AST of the
// loop, for the reason that method's comment records.
func digestsEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// CSRFTokenFor derives a session's CSRF token from the session id itself.
//
// 🔴 NO SECOND SECRET, AND NO SECOND STORED FIELD, BECAUSE THE SESSION ID IS ALREADY THE
// STRONGEST SECRET IN THIS FLOW. The token is `HMAC-SHA256(key = the session id, msg =
// a fixed label)`. Three properties fall out, and each is the reason a stored random
// token was not used instead:
//
//   - It is computable from the COOKIE and from nothing else. A cross-site attacker can
//     make the browser SEND the cookie but cannot READ it — `HttpOnly` stops script,
//     the same-origin policy stops a response being read — so they cannot derive the
//     token. That is the whole CSRF property.
//   - It is NOT computable from the STORE. The store holds `sha256(id)`, and a digest
//     is not a key; somebody who reads the session file cannot mint a CSRF token for a
//     session. A stored random token would have made the file a forgery kit.
//   - It rotates and dies with the session for free: a new id on sign-in is a new token,
//     and a revoked session has no id to derive one from.
//
// ⚠ IT IS NOT A SECRET IN THE SENSE THE ID IS, AND IT IS RENDERED INTO THE PAGE ON
// PURPOSE — that is what a hidden form field is. Rendering it discloses `HMAC(id)`, which
// is what a MAC is for: it does not disclose `id`.
func CSRFTokenFor(sessionID string) string {
	mac := hmac.New(sha256.New, []byte(sessionID))
	mac.Write([]byte(csrfLabel))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// CSRFTokenValid answers whether a presented token belongs to this session id.
//
// 🔴 `hmac.Equal`, WHICH IS CONSTANT TIME, AND HERE THAT IS LOAD-BEARING RATHER THAN
// DEFENSIVE. The presented value is chosen by whoever made the request, so a comparison
// that returned early on the first differing byte is an oracle an attacker can drive
// directly: submit, measure, extend by one byte, repeat. See [digestsEqual] for the
// contrast — that one compares two digests, this one compares a secret against an
// attacker's guess at it.
//
// An empty presented token is refused before the comparison, because an empty session id
// would otherwise produce a well-formed MAC and a caller that lost its cookie would be
// comparing two derivations of the empty string.
func CSRFTokenValid(sessionID, presented string) bool {
	if sessionID == "" || presented == "" {
		return false
	}
	return hmac.Equal([]byte(CSRFTokenFor(sessionID)), []byte(presented))
}

// SessionCookie is the ONE place a session cookie's attributes are chosen.
//
// 🔴 THREE FLAGS, AND EACH IS REFUSING A DIFFERENT ATTACK RATHER THAN BEING GOOD PRACTICE:
//
//   - `HttpOnly` keeps the id out of `document.cookie`, so an XSS that survives
//     `internal/ui`'s escaping steals data rather than the session itself. It is also
//     what makes [CSRFTokenFor]'s derivation sound: a token derived from a value script
//     CAN read would be derivable by the attacker.
//   - `Secure` is UNCONDITIONAL. A session cookie sent over plaintext is readable by
//     every network hop, and the usual escape hatch — an env var to turn it off for local
//     development — is a variable that ends up set in production. Browsers treat
//     `http://localhost` as a secure context and will store a `Secure` cookie from it, so
//     local development is not blocked; ⚠ that last sentence is a claim about browsers
//     and no test here has measured it.
//   - `SameSite=Lax` refuses to attach this cookie to a cross-site POST, which is the
//     CSRF vector, while still attaching it to a top-level GET navigation so an ordinary
//     link into the surface works. `Strict` would break that link; it was not chosen for
//     that reason, and `Lax` is NOT treated as the CSRF guard — it is a browser-side
//     property this server cannot verify, and "same site" still includes a sibling
//     subdomain. The guard is the token.
//
// 🔴 NO `Domain` ATTRIBUTE, AND ITS ABSENCE IS REQUIRED RATHER THAN INCIDENTAL: a
// `__Host-` cookie with one is refused outright by a conforming browser. `Path` must be
// `/` for the same reason.
func SessionCookie(id string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    id,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

// ClearedSessionCookie is what a sign-out sends.
//
// ⚠ IT IS THE SECOND HALF OF A LOGOUT AND NOT THE LOGOUT. The revocation is the store
// write; this only asks the browser to forget a value that has already stopped working.
// A logout that did THIS alone would be exactly the client-held-JWT failure this whole
// design was chosen to avoid — the credential still valid, merely discarded by the one
// party who was willing to discard it.
//
// Every attribute except the value and the lifetime matches [SessionCookie], because a
// browser matches a deletion to an existing cookie on name, path and domain; a mismatch
// leaves the original in place.
func ClearedSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}
