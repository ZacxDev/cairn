package identity

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// Alg is a JWS signature algorithm this build can verify. The set is CLOSED, and every
// member of it verifies with a PUBLIC key.
//
// 🔴 TWO, AND NEITHER `none` NOR ANY SHARED-SECRET ALGORITHM IS AMONG THEM. The classic
// JWT forgery is a token whose header says `alg: none` and whose signature is empty; the
// second classic one is `alg: HS256` against a deployment that verifies with an RSA
// PUBLIC key, where the public key — which the attacker has, because it is public —
// becomes the HMAC secret. Both are defeated by the same rule, stated once: **the
// verifier's configuration decides which algorithms are acceptable and which KEY
// verifies them; the token's own header only SELECTS from that set and can never widen
// it.** `KeySet.key` is where that rule is enforced, `verifySignature` re-asserts it at
// the primitive, and `algKeyType` is the table both read.
//
// 🔴 AND SINCE THE SYMMETRIC PATH WAS DELETED THE HMAC REFUSAL IS UNCONDITIONAL RATHER
// THAN DEPLOYMENT-DEPENDENT — see `symmetricAlg` and `ErrTokenAlgSymmetric`. It used to
// depend on what the deployment had configured, which is why it took a "mixed
// deployment" fixture to reach it at all.
type Alg string

const (
	// AlgRS256 is RSASSA-PKCS1-v1_5 with SHA-256. Supabase's asymmetric RSA option.
	AlgRS256 Alg = "RS256"
	// AlgES256 is ECDSA on P-256 with SHA-256. Supabase's asymmetric EC option, and
	// the default for a new project.
	AlgES256 Alg = "ES256"
)

// keyType is what kind of key an algorithm needs. It is what stops a token's header
// from choosing its own verification primitive.
//
// ⚠ THERE IS NO `oct` MEMBER, AND ITS ABSENCE IS THE POINT. A `keyType` naming a shared
// secret would be a slot for a symmetric algorithm to be added back into `algKeyType` by
// a one-line edit — which is the edit `symmetricAlg` exists to survive.
type keyType string

const (
	keyRSA keyType = "RSA"
	keyEC  keyType = "EC"
)

// algKeyType is THE table. An algorithm absent from it is an algorithm this build
// cannot verify, and the lookup is two-valued everywhere so a miss fails CLOSED.
//
// 🔴 NO DEFAULT ARM ANYWHERE THAT READS IT. The precedent is `control.Verb.Valid` and
// `store.classify`: an unmapped value silently taking the permissive branch is how the
// same defect recurs. Here the permissive branch would be "verify it somehow", which is
// the whole attack.
var algKeyType = map[Alg]keyType{
	AlgRS256: keyRSA,
	AlgES256: keyEC,
}

// hashFor is the digest each algorithm signs over. Separate from `algKeyType` because
// they answer different questions, and one table answering both would make a new
// algorithm a one-line edit that gets one of them wrong.
var hashFor = map[Alg]crypto.Hash{
	AlgRS256: crypto.SHA256,
	AlgES256: crypto.SHA256,
}

// Valid answers whether this build can verify alg at all.
func (a Alg) Valid() bool { _, known := algKeyType[a]; return known }

// jwsHeader is the protected header. Only the fields this verifier acts on are
// declared: a header carrying anything else is not refused for carrying it, because
// JWS permits extensions and refusing them would break on the provider's next release.
type jwsHeader struct {
	Alg  string `json:"alg"`
	Kid  string `json:"kid"`
	Typ  string `json:"typ"`
	Crit []any  `json:"crit"`
}

// Claims is the registered claim set this verifier reads.
//
// ⚠ THERE IS DELIBERATELY NO `Email` FIELD. A Supabase token carries one, and decoding
// it here would put a second copy of a user's email in the tree — caller-supplied,
// competing with the `control.User` row that a UI and an audit line actually read.
// `control.User`'s own comment states why provider+subject is the key and email is not,
// and a decoded-but-unread field is the exported-API-with-no-consumer shape this
// repository refuses. The first draft carried one; it had no reader.
type Claims struct {
	Issuer    string      `json:"iss"`
	Subject   string      `json:"sub"`
	Audience  audience    `json:"aud"`
	Expiry    numericDate `json:"exp"`
	NotBefore numericDate `json:"nbf"`
	IssuedAt  numericDate `json:"iat"`
	// Role is Supabase's own `role` claim (`authenticated` / `anon` / `service_role`).
	// Read so a caller can refuse an anonymous session; NOT used for authorization,
	// which is `control.Resolve`'s answer and nothing else's.
	Role string `json:"role"`
}

// audience decodes RFC 7519's `aud`, which is EITHER a string OR an array of strings.
//
// 🔴 BOTH SPELLINGS, BECAUSE A DECODER THAT HANDLES ONE SILENTLY ACCEPTS THE OTHER AS
// ABSENT. `encoding/json` unmarshalling an array into a `string` field errors, which
// would at least be loud; unmarshalling a string into a `[]string` errors too. The
// dangerous variant is a decoder that swallows the error and leaves the field empty,
// because an empty audience then compares equal to nothing and the check is skipped —
// which is why `Verify` requires a non-empty configured audience and requires it to
// MATCH, rather than skipping the check when the claim is absent.
type audience []string

func (a *audience) UnmarshalJSON(raw []byte) error {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return fmt.Errorf("aud is neither a string nor an array of strings")
	}
	*a = many
	return nil
}

func (a audience) has(want string) bool {
	for _, got := range a {
		if got == want {
			return true
		}
	}
	return false
}

// numericDate decodes RFC 7519's NumericDate: seconds since the epoch, as a JSON
// number.
//
// 🔴 PARSED THROUGH `json.Number` RATHER THAN `float64`, AND THE ZERO IS
// DISTINGUISHABLE FROM ABSENT. A `float64` holds every plausible second value exactly,
// so precision is not the reason; the reason is that a float accepts `1.5e9` and
// `"exp": 1e400` (which becomes `+Inf` and compares as never-expiring). A number that
// is not an integral count of seconds is not a NumericDate, and this refuses it.
type numericDate struct {
	Seconds int64
	Present bool
}

func (n *numericDate) UnmarshalJSON(raw []byte) error {
	text := strings.TrimSpace(string(raw))
	if text == "null" {
		return nil
	}
	seconds, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("not an integral NumericDate: %s", text)
	}
	n.Seconds, n.Present = seconds, true
	return nil
}

func (n numericDate) Time() time.Time { return time.Unix(n.Seconds, 0).UTC() }

// VerifyOptions is everything the verifier needs that does NOT come from the token.
//
// 🔴 EVERY FIELD HERE IS A CONSTRAINT THE TOKEN CANNOT RELAX. That is the whole shape
// of the type: `Algs` is the closed set the deployment accepts, `Issuer` and `Audience`
// are exact-match requirements, and `Keys` is the only place a verification key can come
// from. Nothing in the token widens any of them.
type VerifyOptions struct {
	// Algs is the accepted algorithm set. Empty means NOTHING is accepted.
	Algs []Alg
	// Issuer must equal the token's `iss` exactly. Required.
	Issuer string
	// Audience must appear in the token's `aud`. Required.
	Audience string
	// Keys resolves a `kid` and an algorithm to a verification key. Required.
	Keys keyResolver
	// Leeway absorbs clock skew between this pod and the IdP, applied to `exp`, `nbf`
	// and `iat`.
	//
	// ⚠ IT IS A WINDOW IN WHICH AN EXPIRED TOKEN STILL VERIFIES, so it is bounded:
	// `MaxLeeway` is the ceiling and a larger value is a configuration refusal, not a
	// silently-clamped one. A generous skew allowance is how a revoked session outlives
	// its revocation.
	Leeway time.Duration
	// MaxAge, when non-zero, additionally refuses a token whose `iat` is older than it,
	// regardless of `exp`. Zero means `exp` alone bounds the session. A NEGATIVE value
	// is refused at construction rather than read as zero — see `ErrSupabaseMaxAge`.
	MaxAge time.Duration
	// Now is the clock. nil means `time.Now().UTC()`.
	Now func() time.Time
}

// MaxLeeway is the ceiling on clock-skew tolerance.
//
// Two minutes: enough for any host whose clock is disciplined by NTP at all, and short
// enough that "expired but still accepted" is a window an operator can reason about.
const MaxLeeway = 2 * time.Minute

// keyResolver is what `VerifyOptions.Keys` needs. `KeySet` is the only implementation:
// every algorithm this build verifies takes a public key out of a published document.
//
// 🔴 IT TAKES THE ALGORITHM AS WELL AS THE KID, WHICH IS WHAT MAKES THE CONFUSION
// ATTACK IMPOSSIBLE AT THE SEAM RATHER THAN AT THE CALLER. A resolver that answered
// "here is the key for kid X" would hand an RSA public key to whatever primitive the
// token asked for. Answering "here is the key for kid X *as an ES256 key*" lets the
// resolver say no.
type keyResolver interface {
	key(kid string, alg Alg) (any, error)
}

var (
	// ErrTokenMalformed is a token that is not a compact JWS at all.
	ErrTokenMalformed = errors.New("identity: not a compact JWS")
	// ErrTokenAlg is an algorithm this deployment does not accept.
	ErrTokenAlg = errors.New("identity: unacceptable JWS algorithm")
	// ErrTokenAlgSymmetric is the algorithm-confusion refusal, and it is a NARROWING of
	// ErrTokenAlg rather than a sibling: `errors.Is(err, ErrTokenAlg)` still holds, so
	// no existing classifier moved when it was added.
	//
	// 🔴 IT EXISTS SO THE REFUSAL HAS A NAME A TEST CAN ASSERT ON. Deleting the guard
	// leaves the token refused anyway — see `symmetricAlg` for the measured fallbacks —
	// but refused with a DIFFERENT error, so a mutant that removes it dies against this
	// sentinel instead of surviving behind anonymous refusals that happen to agree.
	// That is the whole reason it is a second sentinel and not a second message.
	ErrTokenAlgSymmetric = fmt.Errorf(
		"%w: a shared-secret (HMAC) algorithm, which this verifier never accepts from any deployment", ErrTokenAlg)
	// ErrTokenSignature is a signature that does not verify.
	ErrTokenSignature = errors.New("identity: JWS signature does not verify")
	// ErrTokenClaims is a signature that verifies over claims this deployment refuses.
	ErrTokenClaims = errors.New("identity: JWT claims refused")
	// ErrNoKey is a token naming a key the verifier does not hold.
	ErrNoKey = errors.New("identity: no verification key for this token")
)

// maxTokenBytes caps what will be parsed at all.
//
// 🔴 A CAP BEFORE ANY DECODING, BECAUSE EVERY STEP AFTER IT ALLOCATES. base64 decoding
// and JSON parsing are both linear in the input and both happen before any signature is
// checked — so without a cap an unauthenticated caller chooses how much work and memory
// one request costs. 8 KiB is several times any session token a provider issues.
const maxTokenBytes = 8 << 10

// Verify parses and verifies a compact JWS and returns its claims.
//
// 🔴 THE LADDER IS ORDERED SO EACH STEP IS REACHABLE BY AN INPUT NO EARLIER STEP
// REJECTS, and it is ordered CHEAPEST-REFUSAL-FIRST so that an unauthenticated caller
// cannot make this pod do expensive work:
//
//  1. size cap                 — before any decoding
//  2. three dot-separated parts — a five-part token is JWE, not JWS
//  3. the header decodes        — and `crit` is refused, see below
//  4. `alg` is NOT a shared-secret algorithm — unconditional, ahead of the allowlist
//  5. `alg` is in the CONFIGURED set — never merely "known"
//  6. a key exists for (kid, alg)  — the resolver enforces key/alg agreement
//  7. the SIGNATURE verifies    — nothing below this line trusts the payload
//  8. the claims are acceptable
//
// 🔴 STEP 4 IS AHEAD OF STEP 5 BECAUSE THAT IS THE ONLY PLACE IT IS REACHABLE. Behind
// the allowlist it could never run: no configuration this package can build puts an
// HMAC algorithm in `Algs`, so `accepts` would refuse first and the guard would be an
// unreachable branch with a live-looking message. Ahead of it, every HS* token in
// existence reaches it.
//
// 🔴 STEP 7 BEFORE STEP 8, ALWAYS. Reading `iss` or `exp` from an unverified payload and
// refusing on it would be a decision made on attacker-controlled bytes; worse, it makes
// the refusal reason depend on content that was never authenticated, which is an
// oracle. The claims check exists, and it runs on verified bytes only.
func Verify(token string, opts VerifyOptions) (Claims, error) {
	if len(token) == 0 || len(token) > maxTokenBytes {
		return Claims{}, fmt.Errorf("%w: %d bytes", ErrTokenMalformed, len(token))
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("%w: %d dot-separated parts, want 3", ErrTokenMalformed, len(parts))
	}

	headerBytes, err := decodeSegment(parts[0])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: header: %v", ErrTokenMalformed, err)
	}
	var header jwsHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return Claims{}, fmt.Errorf("%w: header is not JSON", ErrTokenMalformed)
	}
	// 🔴 `crit` IS REFUSED RATHER THAN IGNORED, WHICH IS WHAT RFC 7515 §4.1.11 REQUIRES.
	// The header field means "the recipient MUST understand these extensions or reject
	// the token". A verifier that ignores it accepts a token whose issuer believes an
	// extension was enforced — so the safe answer for a verifier that implements no
	// extensions is to reject every token that names one.
	if len(header.Crit) != 0 {
		return Claims{}, fmt.Errorf("%w: the header declares `crit` extensions this verifier does not implement", ErrTokenMalformed)
	}
	// `typ` is optional in JWS and Supabase sets `JWT`. Anything else is a different
	// media type being passed off as a session token.
	if header.Typ != "" && !strings.EqualFold(header.Typ, "JWT") {
		return Claims{}, fmt.Errorf("%w: typ %q is not JWT", ErrTokenMalformed, header.Typ)
	}

	alg := Alg(header.Alg)
	// 🔴 THE ALGORITHM-CONFUSION REFUSAL, AND IT IS UNCONDITIONAL. See `symmetricAlg`.
	if symmetricAlg(alg) {
		return Claims{}, fmt.Errorf("%w: %q", ErrTokenAlgSymmetric, header.Alg)
	}
	if !accepts(opts.Algs, alg) {
		// Covers `none`, every algorithm this build cannot verify, and every algorithm
		// it can verify but this deployment did not configure — one refusal, because
		// they are one question: is this algorithm acceptable HERE.
		return Claims{}, fmt.Errorf("%w: %q", ErrTokenAlg, header.Alg)
	}
	if opts.Keys == nil {
		return Claims{}, fmt.Errorf("%w: no key resolver configured", ErrNoKey)
	}
	verifyKey, err := opts.Keys.key(header.Kid, alg)
	if err != nil {
		return Claims{}, err
	}

	signature, err := decodeSegment(parts[2])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: signature segment: %v", ErrTokenMalformed, err)
	}
	// The signing input is the two segments AS SENT, joined by the dot — not a
	// re-encoding of the decoded values. A re-encode would normalise padding or field
	// order and verify a signature over bytes the issuer never signed.
	signingInput := []byte(parts[0] + "." + parts[1])
	if err := verifySignature(alg, verifyKey, signingInput, signature); err != nil {
		return Claims{}, err
	}

	payload, err := decodeSegment(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: payload segment: %v", ErrTokenMalformed, err)
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, fmt.Errorf("%w: payload is not a JSON claim set (%v)", ErrTokenClaims, err)
	}
	if err := checkClaims(claims, opts); err != nil {
		return Claims{}, err
	}
	return claims, nil
}

// symmetricAlg reports whether alg is a shared-secret (HMAC) JWS algorithm.
//
// 🔴 THIS IS THE ALGORITHM-CONFUSION RULE, AND IT IS NOW UNCONDITIONAL RATHER THAN
// DEPLOYMENT-DEPENDENT. The attack: every verification key this build holds is PUBLIC —
// it comes out of a JWKS document anybody can fetch. An attacker takes those bytes, uses
// them as an HMAC-SHA-256 secret, mints a token whose header says `alg: HS256` carrying
// the real key's `kid`, and presents it. A verifier that lets the token's header choose
// the primitive computes an HMAC under a "secret" the attacker also has, and it matches.
//
// 🔴 THE SUPPORT IS GONE AND THE CHECK IS NOT, AND THAT IS THE WHOLE RULING. Deleting
// both would have been the tempting simplification: with no `AlgHS256`, no `keyOct` and
// no symmetric resolver, nothing can put an HMAC algorithm in `VerifyOptions.Algs`.
// The check stays because the closed tables are a property of TODAY's tables, and
// re-adding a symmetric algorithm to `algKeyType` is a one-line edit that this file's
// own comment warns is easy to get wrong. This guard is what makes that edit insufficient
// to reopen the hole: it refuses ahead of the allowlist, so the re-added algorithm would
// still never reach a key.
//
// ⚠ AND THE HONEST SCOPE, BECAUSE THE OPPOSITE CLAIM IS THE ONE A READER WILL ASSUME:
// this guard is the FIRST refusal an HS* token meets, NOT the only one, and it is not
// what stands between this deployment and the forgery today. MEASURED by deleting the
// call site below and reading what answers instead — two refusals, each in a different
// configuration, both `ErrTokenAlg` and both fail-closed:
//
//	an ordinary deployment           → `accepts`: nothing puts HS256 in `Algs`
//	  identity: unacceptable JWS algorithm: "HS256"
//	one that explicitly accepts HS256 → `KeySet.key`'s `algKeyType` lookup, which is
//	  two-valued and has no entry for it — the same text, from the resolver
//
// `hashFor`'s lookup in `verifySignature` is a THIRD backstop of the same shape and
// NEITHER configuration reaches it, because the resolver refuses first — so it is named
// here as a belt nobody has watched work, not as a measured refusal.
//
// What the guard buys is a refusal with its OWN NAME, `ErrTokenAlgSymmetric`, which a
// test can assert and a mutant cannot survive behind two anonymous refusals that happen
// to agree — watched: every arm of `TestTheAlgorithmConfusionForgeryIsREFUSED` goes red
// when this call site is deleted. The same trade, in the same words, as the ECDSA
// signature-length check further down.
//
// 🔴 A PREFIX RULE, NOT A SPELLING. RFC 7518 §3.1 registers exactly HS256/HS384/HS512 as
// the MAC family and `HS` is the prefix all of them carry, so this covers a symmetric
// algorithm nobody has written down here yet — which a set literal naming `"HS256"`
// would not. Folded to upper case because a guard walkable by re-spelling is not a guard;
// `alg` is case-sensitive in RFC 7515, so `hs256` would be refused by the allowlist
// anyway, and refusing it HERE costs nothing and closes the question.
//
// ⚠ IT WILL OVER-REFUSE ANY FUTURE ASYMMETRIC ALGORITHM NAMED `HS…`. There is none in
// the JWA registry, and over-refusing is the safe direction.
func symmetricAlg(alg Alg) bool {
	return strings.HasPrefix(strings.ToUpper(string(alg)), "HS")
}

// accepts answers whether alg is in the configured set. An EMPTY set accepts nothing,
// which is the fail-closed reading of "nobody configured this".
func accepts(algs []Alg, alg Alg) bool {
	for _, ok := range algs {
		if ok == alg {
			return true
		}
	}
	return false
}

// decodeSegment decodes one base64url segment.
//
// 🔴 `RawURLEncoding`: JWS segments carry NO padding, and the padded decoder would
// accept a segment with `=` in it — a second spelling of the same bytes, which is a
// signature over one string verifying for another.
func decodeSegment(segment string) ([]byte, error) {
	out, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return nil, fmt.Errorf("not unpadded base64url")
	}
	return out, nil
}

// verifySignature is the one place a signature is checked, and the key's TYPE is what
// decides the primitive.
//
// 🔴 THE SWITCH IS ON THE ALGORITHM AND EACH ARM RE-ASSERTS THE KEY TYPE. The resolver
// already refuses a key whose type does not match the algorithm, so the type assertions
// here are a SECOND check of the same rule — kept because the two live in different
// files and a future resolver is exactly the kind of thing that gets replaced. A failed
// assertion here is a refusal, never a panic.
//
// ⚠ THERE IS NO HMAC ARM, AND `crypto/hmac` IS NOT IMPORTED. A shared-secret algorithm
// cannot reach here: `symmetricAlg` refuses it in `Verify`, `accepts` refuses it again,
// and the resolver has no key to hand back — so the `hashFor` miss at the top of this
// function is a backstop nothing has been observed to reach. The arm's absence is
// load-bearing rather than incidental: the arm that existed took `verifyKey.([]byte)`,
// which is the exact shape an RSA modulus would arrive in.
func verifySignature(alg Alg, verifyKey any, signingInput, signature []byte) error {
	hash, known := hashFor[alg]
	if !known {
		return fmt.Errorf("%w: no digest for %q", ErrTokenAlg, alg)
	}
	digest := hashBytes(hash, signingInput)

	switch alg {
	case AlgRS256:
		pub, ok := verifyKey.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: %s needs an RSA public key", ErrTokenAlg, alg)
		}
		if err := rsa.VerifyPKCS1v15(pub, hash, digest, signature); err != nil {
			return fmt.Errorf("%w: %s", ErrTokenSignature, alg)
		}
		return nil

	case AlgES256:
		pub, ok := verifyKey.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: %s needs an ECDSA public key", ErrTokenAlg, alg)
		}
		// 🔴 A JWS ECDSA SIGNATURE IS FIXED-WIDTH `r || s`, NOT ASN.1 — RFC 7518 §3.4.
		// `ecdsa.VerifyASN1` would refuse every correct token, which is the loud
		// direction and is what the positive control catches.
		//
		// ⚠ THE LENGTH CHECK IS DEFENCE IN DEPTH, NOT THE THING THAT STOPS A FORGERY,
		// AND AN EARLIER DRAFT OF THIS COMMENT CLAIMED OTHERWISE. It said a short
		// signature would be "left-padded into a valid one for a different message".
		// Measured: with the check removed, a 63-byte signature splits into a correct
		// `r` and a 31-byte `s` that `SetBytes` reads as a smaller number, so
		// `ecdsa.Verify` returns false anyway. What the check buys is that a
		// wrong-length signature is refused EXPLICITLY, with a message naming the
		// length, rather than incidentally by a verification that happens to fail — so
		// a future change to how the halves are split cannot silently start accepting
		// one. `tests/control_mutants.py` carries no row for it for exactly that
		// reason: a mutant removing it SURVIVES, and correctly.
		size := (pub.Curve.Params().BitSize + 7) / 8
		if len(signature) != 2*size {
			return fmt.Errorf("%w: %s signature is %d bytes, want %d", ErrTokenSignature, alg, len(signature), 2*size)
		}
		r := new(big.Int).SetBytes(signature[:size])
		s := new(big.Int).SetBytes(signature[size:])
		if !ecdsa.Verify(pub, digest, r, s) {
			return fmt.Errorf("%w: %s", ErrTokenSignature, alg)
		}
		return nil

	default:
		// Unreachable: `accepts` has already refused anything outside the configured
		// set, and the configured set is validated against `algKeyType`. Refusing
		// rather than falling through keeps the direction safe if that stops being
		// true — an algorithm nobody understands verifies nothing.
		return fmt.Errorf("%w: %q has no verifier", ErrTokenAlg, alg)
	}
}

// hashBytes computes the digest an algorithm signs over.
//
// ⚠ IT HANDLES EXACTLY WHAT `hashFor` DECLARES, WHICH TODAY IS SHA-256 ALONE. The
// first draft carried SHA-384 and SHA-512 arms as well, and they were unreachable:
// `hashFor` maps both supported algorithms to SHA-256, so nothing could ever
// select them. Unreachable arms in a crypto path are worse than absent ones — they read
// as coverage of algorithms this build does not actually verify. A default that returns
// nil rather than hashing with something else keeps the direction safe: every
// comparison against a nil digest fails.
func hashBytes(h crypto.Hash, data []byte) []byte {
	if h != crypto.SHA256 {
		return nil
	}
	sum := sha256.Sum256(data)
	return sum[:]
}

// checkClaims refuses a verified token whose claims this deployment does not accept.
//
// 🔴 `exp` IS REQUIRED, NOT MERELY CHECKED-IF-PRESENT, AND THAT IS THE ONE CLAIM RULE
// WORTH STATING TWICE. A token with no `exp` never expires: a session leaked once is a
// credential forever, and it cannot be revoked by anything short of rotating the signing
// key for every user. Every other claim here is a narrowing; this one is the difference
// between a session and a permanent grant.
func checkClaims(c Claims, opts VerifyOptions) error {
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now()
	}
	if opts.Issuer == "" || opts.Audience == "" {
		// Unreachable through `NewSupabaseJWT`, which refuses both at construction. A
		// programmatic caller can still reach it, and an empty requirement compared
		// against a claim would PASS for a token from any issuer — so it is a refusal
		// here rather than a comparison that silently succeeds.
		return fmt.Errorf("%w: the verifier was given no issuer or audience to require", ErrTokenClaims)
	}
	if c.Issuer != opts.Issuer {
		return fmt.Errorf("%w: iss", ErrTokenClaims)
	}
	if !c.Audience.has(opts.Audience) {
		return fmt.Errorf("%w: aud", ErrTokenClaims)
	}
	if c.Subject == "" {
		return fmt.Errorf("%w: sub is empty, so the token names nobody", ErrTokenClaims)
	}
	if !c.Expiry.Present {
		return fmt.Errorf("%w: exp is absent, so this token would never expire", ErrTokenClaims)
	}
	leeway := opts.Leeway
	if leeway < 0 || leeway > MaxLeeway {
		// Unreachable through the constructors, which refuse it. Clamping silently
		// would make a misconfiguration invisible in exactly the direction that keeps
		// expired sessions alive.
		return fmt.Errorf("%w: leeway %s is outside [0, %s]", ErrTokenClaims, opts.Leeway, MaxLeeway)
	}
	if !now.Add(-leeway).Before(c.Expiry.Time()) {
		return fmt.Errorf("%w: exp", ErrTokenClaims)
	}
	if c.NotBefore.Present && now.Add(leeway).Before(c.NotBefore.Time()) {
		return fmt.Errorf("%w: nbf", ErrTokenClaims)
	}
	if c.IssuedAt.Present {
		if now.Add(leeway).Before(c.IssuedAt.Time()) {
			// Issued in the future by more than the skew allowance: either a clock is
			// wrong or the token was minted by somebody predicting this verifier's
			// window.
			return fmt.Errorf("%w: iat is in the future", ErrTokenClaims)
		}
		if opts.MaxAge > 0 && now.Sub(c.IssuedAt.Time()) > opts.MaxAge+leeway {
			return fmt.Errorf("%w: iat is older than the configured max age", ErrTokenClaims)
		}
	} else if opts.MaxAge > 0 {
		// A max age configured against a token that carries no `iat` cannot be
		// enforced, and silently not enforcing a configured bound is the shape this
		// repository refuses everywhere else.
		return fmt.Errorf("%w: a max age is configured and the token carries no iat", ErrTokenClaims)
	}
	return nil
}
