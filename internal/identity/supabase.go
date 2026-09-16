package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
)

// SupabaseBackend is this backend's name in a `Refusal`. Never rendered to a caller.
const SupabaseBackend = "supabase-jwt"

// DefaultSupabaseProvider is the `control.User.Provider` a Supabase-issued session
// resolves against.
//
// ⚠ IT NAMES THE ISSUER, NOT THE UPSTREAM SOCIAL PROVIDER. A user who signed in with
// GitHub and one who signed in with Google both arrive with a Supabase JWT whose `sub`
// is the Supabase user id; the GitHub/Google distinction lives in Supabase's own
// `app_metadata` and is not what identifies the user HERE. Keying on the upstream
// provider instead would give one person two `control.User` rows the day they link a
// second login — and `control.User`'s comment already records why the key is
// provider+subject rather than email.
const DefaultSupabaseProvider = "supabase"

// SupabaseJWT verifies a Supabase session token locally and resolves it to a principal
// this control plane already holds.
//
// 🔴 VERIFICATION IS LOCAL AND THE HOT PATH MAKES NO NETWORK CALL — the plan's §E
// requirement. The signature is checked against `KeySet`, which is refreshed on a timer
// and read under an RLock; the principal is looked up in `control.Cache`, which is
// materialized and read the same way. An identity-provider outage therefore stops
// nothing for a session that has already been issued, which is measured rather than
// argued (see this package's tests, and the README).
//
// 🔴 A VERIFIED TOKEN FOR AN UNKNOWN USER IS A REFUSAL, NOT A SIGNUP. The IdP vouches
// for who somebody is; it says nothing about whether this cairn instance has an account
// for them. Minting one here would make every read route a user-creation endpoint for
// anybody with an account at the provider — which is self-serve signup, which is P6, and
// which brings quotas, rate limits, abuse handling and deletion/export with it.
type SupabaseJWT struct {
	authority ModelSource
	verify    VerifyOptions
	// keys is the same `*KeySet` `verify.Keys` holds, kept in its concrete type so the
	// refresh methods need no type assertion. `NewSupabaseJWT` guarantees it is
	// non-nil: a Supabase backend with nothing to verify against does not get built.
	keys     *KeySet
	provider string
	// requireRole, when non-empty, additionally refuses a token whose `role` claim is
	// not it. Supabase issues `anon` to a session that has not signed in.
	requireRole string
}

var _ Authenticator = (*SupabaseJWT)(nil)

// SupabaseConfig is what a deployment declares.
type SupabaseConfig struct {
	// Authority is the materialized control plane the subject is resolved against.
	// Required.
	Authority ModelSource
	// Keys is the cached JWKS. Required, and the ONLY source of a verification key:
	// this build verifies no shared-secret algorithm, from any source.
	Keys *KeySet
	// Issuer must equal the token's `iss` exactly. Required — for Supabase this is
	// `https://<project-ref>.supabase.co/auth/v1`.
	Issuer string
	// Audience must appear in the token's `aud`. Required — Supabase issues
	// `authenticated`.
	Audience string
	// Provider is the `control.User.Provider` to resolve `sub` against. Empty means
	// DefaultSupabaseProvider.
	Provider string
	// RequireRole refuses a token whose `role` claim is not this. Empty disables the
	// check.
	RequireRole string
	// Leeway absorbs clock skew. Bounded by MaxLeeway.
	Leeway time.Duration
	// MaxAge, when non-zero, refuses a token whose `iat` is older than it, regardless
	// of `exp`. A negative value is refused rather than read as zero.
	MaxAge time.Duration
	// Now is the clock. nil means `time.Now().UTC()`.
	Now func() time.Time
}

// The construction refusals. Each is its own sentinel so a test can watch THAT ONE
// fire rather than "a constructor returned an error", and each is reachable by a
// configuration no earlier guard rejects.
var (
	// ErrSupabaseNoIssuer refuses a verifier with no issuer to require.
	ErrSupabaseNoIssuer = errors.New("identity: no Supabase issuer configured — without one, a token signed by ANY issuer whose key happened to be in the key set would verify")
	// ErrSupabaseNoAudience refuses a verifier with no audience to require.
	ErrSupabaseNoAudience = errors.New("identity: no Supabase audience configured — without one, a token minted for a DIFFERENT application at the same issuer would verify here")
	// ErrSupabaseNoKeys refuses a verifier with nothing to verify against.
	ErrSupabaseNoKeys = errors.New("identity: no JWKS is configured, so no token could ever verify")
	// ErrSupabaseLeeway refuses a clock-skew allowance wide enough to matter.
	ErrSupabaseLeeway = errors.New("identity: the clock-skew leeway is outside its bound")
	// ErrSupabaseMaxAge refuses a NEGATIVE maximum token age.
	//
	// 🔴 REFUSED RATHER THAN READ AS ZERO, BECAUSE ZERO MEANS "OFF". `checkClaims` tests
	// `opts.MaxAge > 0`, so a negative value silently disables a bound the operator
	// configured — the exact shape `envBool` refuses one file over, and the shape this
	// repository refuses everywhere else.
	ErrSupabaseMaxAge = errors.New("identity: the maximum token age is negative")
)

// NewSupabaseJWT builds the Supabase backend, or refuses.
//
// 🔴 THE GUARD ORDER IS THE POINT AND EVERY RUNG IS REACHABLE BY A CONFIGURATION EVERY
// RUNG ABOVE IT ACCEPTS. Five rungs, re-derived when the symmetric path was deleted:
//
//	0  ErrNoAuthority        no control plane to resolve a subject against
//	1  ErrSupabaseNoIssuer   everything else complete, no `iss` to require
//	2  ErrSupabaseNoAudience issuer set, no `aud` to require
//	3  ErrSupabaseNoKeys     issuer and audience set, nothing to verify against
//	4  ErrSupabaseLeeway     a skew allowance outside [0, MaxLeeway]
//	5  ErrSupabaseMaxAge     a NEGATIVE maximum token age, which would read as "off"
//
// 🔴 AND A SIXTH RUNG WAS DELETED RATHER THAN LEFT STANDING. `ErrSupabaseWeakSecret`
// asked whether a configured symmetric secret cleared a length floor; rung 3 used to ask
// only whether there was "any key at all", which is what made the floor reachable —
// *two rungs, two questions*. With no `Secret` field there is no second question, and a
// rung no configuration can reach is an unreachable guard with a live-looking message,
// which this repository names as a defect rather than as spare safety.
//
// ⚠ `ErrSupabaseMaxAge` IS NOT THAT RUNG RENAMED. It is reachable — `MaxAge: -time.Second`
// clears every rung above it — and it exists because P4's round 0 found
// `SupabaseConfig.MaxAge` had no environment variable at all; wiring one made a negative
// value something an operator can now type.
func NewSupabaseJWT(cfg SupabaseConfig) (*SupabaseJWT, error) {
	if cfg.Authority == nil {
		return nil, ErrNoAuthority
	}
	if cfg.Issuer == "" {
		return nil, ErrSupabaseNoIssuer
	}
	if cfg.Audience == "" {
		return nil, ErrSupabaseNoAudience
	}
	if cfg.Keys == nil {
		return nil, ErrSupabaseNoKeys
	}
	if cfg.Leeway < 0 || cfg.Leeway > MaxLeeway {
		return nil, fmt.Errorf("%w: %s is outside [0, %s]", ErrSupabaseLeeway, cfg.Leeway, MaxLeeway)
	}
	if cfg.MaxAge < 0 {
		return nil, fmt.Errorf("%w: %s. Zero means the check is off; a negative value would be read as zero and silently disable a bound you configured", ErrSupabaseMaxAge, cfg.MaxAge)
	}

	// 🔴 THE ACCEPTED ALGORITHM SET IS THE ASYMMETRIC PAIR, UNCONDITIONALLY, AND THE
	// CONDITION THAT USED TO GUARD IT WAS DELETED WITH THE THING IT DISTINGUISHED. It
	// read `if cfg.Keys != nil` beside an `if len(cfg.Secret) > 0` that appended
	// `AlgHS256` — a set derived from what the deployment configured rather than from
	// what the build can do. Rung 3 above now guarantees `cfg.Keys != nil`, so the
	// surviving condition can no longer be false, and a branch that cannot be false is
	// the same unreachable shape as a rung that cannot fire.
	//
	// The rule it enforced has not gone anywhere; it moved and got STRONGER. A
	// shared-secret algorithm is refused by `symmetricAlg` in `Verify` for every
	// deployment, rather than by this list for the deployments that happened not to
	// configure a secret.
	algs := []Alg{AlgRS256, AlgES256}

	provider := cfg.Provider
	if provider == "" {
		provider = DefaultSupabaseProvider
	}
	return &SupabaseJWT{
		authority: cfg.Authority,
		verify: VerifyOptions{
			Algs:     algs,
			Issuer:   cfg.Issuer,
			Audience: cfg.Audience,
			Keys:     cfg.Keys,
			Leeway:   cfg.Leeway,
			MaxAge:   cfg.MaxAge,
			Now:      cfg.Now,
		},
		keys:        cfg.Keys,
		provider:    provider,
		requireRole: cfg.RequireRole,
	}, nil
}

// Authenticate verifies the bearer token as a Supabase session.
//
// ⚠ IT READS THE SAME `Authorization: Bearer …` HEADER THE MACHINE-TOKEN BACKEND DOES,
// AND THAT IS NOT A COLLISION. A Supabase client sends its access token there, and so
// does a CI job with a cairn credential; the two are told apart by what they ARE, not by
// where they arrive. In the chain the machine-token backend looks first — a hashed
// lookup that a JWT simply misses — and this one gets the bytes next. Splitting them
// onto two headers would mean a caller choosing which authenticator to face, which is a
// choice no caller should have.
func (s *SupabaseJWT) Authenticate(r *http.Request) (Identity, error) {
	presented := authz.PresentedToken(r.Header.Get("Authorization"))
	if presented == "" {
		return Identity{}, refuse(SupabaseBackend, "no bearer token")
	}
	claims, err := Verify(presented, s.verify)
	if err != nil {
		// 🔴 THE VERIFIER'S REASON IS KEPT HERE AND GOES NOWHERE NEAR THE WIRE. It is
		// what makes "expired" distinguishable from "signed by a key we do not hold"
		// in a test and, one day, on an operator stream; `internal/api` answers the
		// same uniform 401 either way, because a refusal that discriminates tells an
		// attacker which half of their forgery was right.
		return Identity{}, refuse(SupabaseBackend, err.Error())
	}
	if s.requireRole != "" && claims.Role != s.requireRole {
		return Identity{}, refuse(SupabaseBackend, "role claim is not "+s.requireRole)
	}

	// The model is read ONCE. Every fact below — the user row, the principal and the
	// authorization — comes out of this one value, so a refresh landing mid-request
	// cannot authenticate against one world and authorise against another.
	model := s.authority.Model()
	user, known := model.UserByProviderSubject(s.provider, claims.Subject)
	if !known {
		return Identity{}, refuse(SupabaseBackend, "the token verifies and names a subject this control plane holds no user for")
	}
	principal, held := model.PrincipalFor(control.KindUser, user.ID)
	if !held {
		// Unreachable while `UserByProviderSubject` returned a row from this same
		// model: `displayOf` reads `m.Users` too. Refusing rather than proceeding with
		// an unnamed principal keeps the direction safe if the two ever stop agreeing —
		// a principal with no display is a request whose writes could not record an
		// actor.
		return Identity{}, refuse(SupabaseBackend, "the user row resolves to no principal")
	}
	return Identity{
		Principal: principal,
		Auth:      control.Resolve(model, principal),
		// No fingerprint: this session is not a credential this pod minted, so there is
		// no row in the startup line's table for an operator to correlate it with. See
		// `Identity.Fingerprint`.
	}, nil
}

// RefreshKeys materializes the key set.
//
// Exposed so `cmd/cairn-server` can fetch once at startup and shout about a failure,
// rather than discovering at the first sign-in that the provider was unreachable when
// the pod came up.
//
// ⚠ THE NIL CHECK THESE THREE METHODS CARRIED IS GONE, AND ITS ABSENCE IS DELIBERATE.
// It answered "this deployment is symmetric-only, so there is nothing to refresh" — a
// state that no longer exists, because rung 3 of `NewSupabaseJWT` refuses a backend with
// no key set. A caller holding a nil `*SupabaseJWT` (which is what `FromEnvironment`
// returns when no Supabase variable was set) must still not call these; that was true
// before the change too, since the old body dereferenced `s` just as this one does.
func (s *SupabaseJWT) RefreshKeys(ctx context.Context) error { return s.keys.Refresh(ctx) }

// RunKeyRefresh keeps the key set current until ctx is done.
func (s *SupabaseJWT) RunKeyRefresh(ctx context.Context) error { return s.keys.Run(ctx) }

// KeyStatus reports the cached key set's age.
func (s *SupabaseJWT) KeyStatus() KeySetStatus { return s.keys.Status() }
