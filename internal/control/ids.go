package control

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"strings"
)

// ID prefixes. The prefix is part of the id, not a display convention: an id
// printed in an audit line, an error or a journal record says what it identifies,
// so a project id passed where a scope id belongs is visible rather than merely
// wrong.
const (
	PrefixUser       = "usr"
	PrefixProject    = "prj"
	PrefixScope      = "scp"
	PrefixGrant      = "grt"
	PrefixCredential = "crd"
)

// idAlphabet is Crockford-style base32 without padding: unambiguous when read
// aloud or transcribed, and safe in a path, a URL and a shell word without
// quoting.
var idAlphabet = base32.NewEncoding("0123456789abcdefghjkmnpqrstvwxyz").WithPadding(base32.NoPadding)

// idEntropyBytes is 16 — 128 bits, which is the standard "never collides in
// practice" bar and renders as 26 base32 characters.
const idEntropyBytes = 16

// NewID mints a random id with the given prefix.
//
// 🔴 `crypto/rand`, NOT `math/rand`, EVEN THOUGH AN ID IS NOT A SECRET. A scope id
// is an unguessable handle in a multi-tenant system: predictable ids turn any
// endpoint that takes one into an enumeration API, which is the exact property the
// uniform-401 design elsewhere in this server exists to deny. The cost of the
// stronger source at id-minting rates is nil.
func NewID(prefix string) (ID, error) {
	buf := make([]byte, idEntropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("minting %s id: %w", prefix, err)
	}
	return ID(prefix + "_" + idAlphabet.EncodeToString(buf)), nil
}

// MustPrefix answers whether an id carries the expected prefix.
//
// Callers that accept an id off the wire should use it: it is a cheap, total check
// that a caller has not passed the id of one entity where another is expected, and
// it is not a permission check of any kind.
func MustPrefix(id ID, prefix string) bool {
	return strings.HasPrefix(string(id), prefix+"_")
}

// HashHexLen is the length of a token digest in hex — sha256, so 64.
const HashHexLen = sha256.Size * 2

// HashToken is the ONE conversion from a bearer token to what the control plane
// stores.
//
// 🔴 A PLAIN SHA-256, NOT A PASSWORD KDF, AND THE REASON IS THE INPUT. bcrypt,
// scrypt and argon2 exist to make GUESSING feasible-to-infeasible for inputs drawn
// from a small space — human-chosen passwords. A cairn credential is machine-minted
// with at least 43 characters of high entropy (`authz.MinTokenChars`), so there is
// no dictionary to slow down, and a deliberately slow hash on the authentication
// path would be a per-request cost that buys nothing against this input. If a
// human-chosen secret ever becomes a credential here, this decision has to be
// revisited — which is why the premise is written down rather than the conclusion.
//
// 🔴 THE TOKEN IS NEVER STORED, LOGGED OR RETURNED. This function is the only place
// it is read, and the digest is the only thing that leaves.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// EqualHash compares two digests in constant time.
//
// 🔴 `subtle.ConstantTimeCompare`, NOT `==`, FOR THE SAME REASON `authz.Authorize`
// USES IT: a public endpoint makes a byte-at-a-time timing oracle practically
// exploitable, and the difference is invisible in every functional test. The digest
// of a presented token is derived from a secret, so a comparison against it that
// short-circuits leaks a prefix of that derivation.
//
// ⚠ `ConstantTimeCompare` RETURNS 0 FOR UNEQUAL LENGTHS WITHOUT COMPARING. Both
// operands here are fixed-width hex digests, so the length is a constant of the
// format rather than anything derived from the secret.
func EqualHash(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// copyIDs copies a slice of ids PRESERVING NIL-NESS.
//
// 🔴 `append([]ID(nil), src...)` DOES NOT DO THIS, AND THE DIFFERENCE IS THE WHOLE
// `NarrowedScopes` contract. Appending zero elements to a nil slice yields nil, so
// a non-nil EMPTY narrowing — "this credential sees nothing" — becomes nil, which
// means "NO narrowing" and is its exact opposite. A copy helper that flattens the
// two is a silent widening hiding inside a line that reads like defensive copying.
func copyIDs(src []ID) []ID {
	if src == nil {
		return nil
	}
	out := make([]ID, len(src))
	copy(out, src)
	return out
}
