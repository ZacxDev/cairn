package identity

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// The environment this package reads. New names use the `CAIRN_` prefix, per
// `AGENTS.md`'s naming rule; nothing here has a `SUBSYSTEM_STORE_` alias because
// nothing here has ever been deployed under one, and minting an alias for a setting
// with no existing holder is inventing a migration nobody needs.
const (
	// EnvSupabaseJWKSURL is the JWKS endpoint — `https://<ref>.supabase.co/auth/v1/.well-known/jwks.json`.
	//
	// ⚠ IT IS THE ONLY WAY TO GIVE THIS BACKEND A KEY. The two variables that named the
	// LEGACY symmetric secret are deleted, not deprecated: this build verifies no
	// shared-secret algorithm. Setting one of them now REFUSES — see `retiredEnv`.
	EnvSupabaseJWKSURL = "CAIRN_SUPABASE_JWKS_URL"
	// EnvSupabaseIssuer is the required `iss`.
	EnvSupabaseIssuer = "CAIRN_SUPABASE_ISSUER"
	// EnvSupabaseAudience is the required `aud`. Defaults to DefaultSupabaseAudience.
	EnvSupabaseAudience = "CAIRN_SUPABASE_AUDIENCE"
	// EnvSupabaseProvider is the `control.User.Provider` a subject resolves against.
	EnvSupabaseProvider = "CAIRN_SUPABASE_PROVIDER"
	// EnvSupabaseRequireRole refuses a token whose `role` claim is not this.
	EnvSupabaseRequireRole = "CAIRN_SUPABASE_REQUIRE_ROLE"
	// EnvSupabaseLeeway is the clock-skew allowance, as a Go duration.
	EnvSupabaseLeeway = "CAIRN_SUPABASE_LEEWAY"
	// EnvSupabaseMaxAge additionally refuses a token whose `iat` is older than this,
	// regardless of `exp`. A Go duration; empty or `0` leaves `exp` alone bounding the
	// session, and a NEGATIVE value is refused rather than read as zero.
	//
	// ⚠ IT EXISTS BECAUSE THE FIELD DID AND NOTHING COULD REACH IT. `SupabaseConfig.MaxAge`
	// and `checkClaims`'s two `MaxAge` branches shipped with no variable at all, so the
	// only caller that could set them was a test — the same shape this package deleted
	// `EmailHeader` for. The ruling differed because the shapes differ: `EmailHeader`
	// was a decoded claim with NO READER, dead on both ends, while these are live,
	// tested refusals whose only defect was that no deployment could arm them. Wiring
	// one variable is cheaper than deleting a working bound, and `EnvSupabaseLeeway` is
	// the sibling it now reads exactly like.
	EnvSupabaseMaxAge = "CAIRN_SUPABASE_MAX_AGE"

	// EnvProxyFronted is the explicit declaration. See `TrustedHeaderConfig.ProxyFronted`.
	EnvProxyFronted = "CAIRN_TRUSTED_HEADER_PROXY_FRONTED"
	// EnvProxySubjectHeader is the header carrying the provider's user id.
	EnvProxySubjectHeader = "CAIRN_TRUSTED_HEADER_SUBJECT"
	// EnvProxySecret is the shared secret, inline.
	EnvProxySecret = "CAIRN_TRUSTED_HEADER_SECRET"
	// EnvProxySecretFile is the same secret, read from a file.
	EnvProxySecretFile = "CAIRN_TRUSTED_HEADER_SECRET_FILE"
	// EnvProxySecretHeader is the header the shared secret arrives in.
	EnvProxySecretHeader = "CAIRN_TRUSTED_HEADER_SECRET_HEADER"
	// EnvProxyRequireClientCert demands a verified TLS client certificate.
	EnvProxyRequireClientCert = "CAIRN_TRUSTED_HEADER_REQUIRE_CLIENT_CERT"
	// EnvProxyPeers narrows which peer addresses may present the header.
	EnvProxyPeers = "CAIRN_TRUSTED_HEADER_PEERS"
	// EnvProxyProvider is the `control.User.Provider` a subject resolves against.
	EnvProxyProvider = "CAIRN_TRUSTED_HEADER_PROVIDER"
)

// DefaultSupabaseAudience is the `aud` Supabase issues to a signed-in session.
//
// ⚠ IT IS DEFAULTED RATHER THAN REQUIRED, AND THE TRADE IS STATED. The audience check
// is what stops a token minted for a DIFFERENT application at the same issuer from
// verifying here; defaulting it means an operator who sets nothing still gets the check,
// with the value Supabase actually issues. The alternative — refusing to start without
// it — buys nothing, because the only correct value for a Supabase deployment is this
// one, and a required setting whose value is fixed is a setting people copy without
// reading. `ErrSupabaseNoAudience` still stands behind the programmatic constructor.
const DefaultSupabaseAudience = "authenticated"

// supabaseEnv and proxyEnv are the LEDGERS of which variables belong to which backend.
//
// 🔴 A PARTIALLY-CONFIGURED BACKEND MUST REFUSE TO START, NOT SILENTLY BE OFF, AND THESE
// LISTS ARE HOW THAT QUESTION IS ASKED. "Build the backend if the required fields are
// present" reads sensibly and is the dangerous version: an operator who sets the subject
// header and forgets the shared secret gets a pod that comes up, passes its health check
// and quietly authenticates nobody through a backend they believe is live. So the trigger
// is "did anybody touch ANY of these", and the answer to a broken configuration is
// `os.Exit(78)` in the crash loop where an operator will see it.
//
// ⚠ EACH LIST MUST NAME EVERY VARIABLE ITS BACKEND READS. One left out is one that can
// be set alone without arming the check — which is the same defect one level down.
// `TestTheEnvironmentLedgersNameEveryVariableEachBackendReads` pins both against the
// `Env*` constants it reads out of this package's own source BY AST, so a constant added
// without a ledger entry is a RED test rather than a hope. ⚠ That last clause used to be
// written as a property and was
// measured FALSE: the test compared the ledgers against a hand-written list of the same
// fifteen names, so a sixteenth constant absent from the list and from both ledgers was
// in neither side of the comparison and the whole suite stayed green with it live.
var supabaseEnv = []string{
	EnvSupabaseJWKSURL,
	EnvSupabaseIssuer,
	EnvSupabaseAudience,
	EnvSupabaseProvider,
	EnvSupabaseRequireRole,
	EnvSupabaseLeeway,
	EnvSupabaseMaxAge,
}

// retiredEnv names variables this build once read and no longer does, with what to do
// instead. Setting one is a REFUSAL.
//
// 🔴 A DELETED SETTING MUST BE LOUD, NOT IGNORED, AND THAT IS THE SAME RULE `supabaseEnv`
// SERVES ONE DIRECTION OVER. The ledgers above ask "did anybody touch any of these" so a
// half-configured backend cannot come up quietly; a name DROPPED from a ledger inverts
// that — an operator's manifest still carries it, `anySet` no longer counts it, and the
// pod comes up healthy having silently discarded a line the operator wrote. These two
// named the LEGACY symmetric JWT secret, which this build no longer verifies with at
// all; an operator migrating a project is exactly who still has one set.
//
// ⚠ THEY ARE NOT IN `supabaseEnv` AND MUST NEVER BE. A name here can only REFUSE — it
// cannot arm a backend, which is the distinction that keeps it from being a second
// spelling of a live setting.
const retiredSymmetricSecret = "the legacy symmetric (HS256) JWT secret, which this build no longer verifies with at all. Use " +
	EnvSupabaseJWKSURL + " — a Supabase project's asymmetric signing keys"

var retiredEnv = map[string]string{
	"CAIRN_SUPABASE_JWT_SECRET":      retiredSymmetricSecret,
	"CAIRN_SUPABASE_JWT_SECRET_FILE": retiredSymmetricSecret,
}

// ErrRetiredSetting is the refusal a retired variable earns.
var ErrRetiredSetting = errors.New("identity: a setting this build no longer reads is set")

var proxyEnv = []string{
	EnvProxyFronted,
	EnvProxySubjectHeader,
	EnvProxySecret,
	EnvProxySecretFile,
	EnvProxySecretHeader,
	EnvProxyRequireClientCert,
	EnvProxyPeers,
	EnvProxyProvider,
}

// FromEnvironment builds the backends a deployment's environment asks for.
//
// 🔴 NOTHING SET MEANS MACHINE-TOKEN ONLY, AND THAT IS THE WHOLE COMPATIBILITY CLAIM.
// A deployment with no Supabase variables and no proxy variables gets exactly the chain
// `api.New` builds by itself, so its behaviour is not merely similar to today's — it is
// the same code path. Both new backends are opt-in, and the trusted-header one is opt-in
// twice.
//
// `machine` is always first in the returned chain. The second return is the Supabase
// backend whose key set the caller must refresh, or nil when no Supabase variable was
// set at all — it is never a backend with nothing to refresh, because rung 3 of
// `NewSupabaseJWT` refuses one.
func FromEnvironment(env map[string]string, authority interface {
	TokenAuthority
	ModelSource
}) (Chain, *SupabaseJWT, error) {
	// 🔴 BEFORE `anySet`, AND UNCONDITIONALLY. A retired name arms nothing, so checking
	// it inside a backend's own branch would only fire for deployments that had ALSO
	// set a live variable — which is precisely the deployment that gets a loud refusal
	// anyway. The silent case is the one where a retired name is all that is set.
	if err := refuseRetiredSettings(env); err != nil {
		return nil, nil, err
	}

	// 🔴 ALSO BEFORE `anySet`, AND FOR THE MIRROR-IMAGE REASON. `anySet` TRIMS, so a
	// whitespace-only value arms nothing at all; a check inside a backend's own branch
	// could therefore never reach the deployment that has only that.
	if err := refuseBlankSettings(env); err != nil {
		return nil, nil, err
	}

	machine, err := NewMachineToken(authority)
	if err != nil {
		return nil, nil, err
	}

	var supabase *SupabaseJWT
	if anySet(env, supabaseEnv) {
		supabase, err = supabaseFromEnv(env, authority)
		if err != nil {
			return nil, nil, err
		}
	}

	var trusted *TrustedHeader
	if anySet(env, proxyEnv) {
		trusted, err = trustedHeaderFromEnv(env, authority)
		if err != nil {
			return nil, nil, err
		}
	}

	chain, err := Backends(machine, supabase, trusted)
	if err != nil {
		return nil, nil, err
	}
	return chain, supabase, nil
}

// refuseRetiredSettings refuses a deployment that still sets a variable this build
// dropped. Sorted so the message is stable when more than one is set.
func refuseRetiredSettings(env map[string]string) error {
	names := make([]string, 0, len(retiredEnv))
	for name := range retiredEnv {
		if strings.TrimSpace(env[name]) != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	reasons := make([]string, 0, len(names))
	for _, name := range names {
		reasons = append(reasons, name+" was "+retiredEnv[name])
	}
	return fmt.Errorf("%w: %s. Remove it rather than leaving it set — a value nothing reads looks like configuration",
		ErrRetiredSetting, strings.Join(reasons, "; "))
}

// ErrBlankSetting is the refusal a ledger variable earns by holding only whitespace.
var ErrBlankSetting = errors.New("identity: a setting is present and holds only whitespace")

// refuseBlankSettings refuses a ledger variable whose value is whitespace and nothing else.
//
// 🔴 A WHITESPACE-ONLY VALUE IS INDISTINGUISHABLE FROM AN ABSENT ONE TO `anySet`, SO THE
// BACKEND THE OPERATOR CONFIGURED IS SILENTLY OFF — the same defect `supabaseEnv`'s comment
// describes one level up, in the one spelling the ledgers cannot see. Measured before this
// guard existed: `FromEnvironment({CAIRN_TRUSTED_HEADER_SECRET: "   "}, …)` returned a nil
// error and a ONE-backend chain, so the pod came up healthy, passed its health check, and
// logged nothing about the trusted-header backend it was not running.
//
// ⚠ PRESENT-AND-WHITESPACE ONLY, AND THE TWO CASES IT DELIBERATELY LEAVES ALONE ARE THE
// POINT OF THE SCOPE. An ABSENT name is how a deployment says "I am not using this
// backend" — refusing it would refuse every deployment, including the machine-token-only
// one this package's whole compatibility claim rests on. A name present with the EMPTY
// string is left alone too: it carries no value to mistake for one, and a manifest that
// emits every variable with an empty default is a common enough shape that refusing it
// would be a second, wider change than the defect measured above.
func refuseBlankSettings(env map[string]string) error {
	ledgers := append(append([]string{}, supabaseEnv...), proxyEnv...)
	names := make([]string, 0, len(ledgers))
	for _, name := range ledgers {
		value, present := env[name]
		if !present || value == "" {
			continue
		}
		if strings.TrimSpace(value) == "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return fmt.Errorf("%w: %s. Give it a value or delete the line — the value is trimmed to nothing, "+
		"so the backend it belongs to would be silently OFF while the manifest says it is on",
		ErrBlankSetting, strings.Join(names, ", "))
}

func anySet(env map[string]string, names []string) bool {
	for _, name := range names {
		if strings.TrimSpace(env[name]) != "" {
			return true
		}
	}
	return false
}

func supabaseFromEnv(env map[string]string, authority ModelSource) (*SupabaseJWT, error) {
	var keys *KeySet
	var err error
	if url := strings.TrimSpace(env[EnvSupabaseJWKSURL]); url != "" {
		keys, err = NewKeySet(JWKSOptions{URL: url})
		if err != nil {
			return nil, err
		}
	}

	audience := strings.TrimSpace(env[EnvSupabaseAudience])
	if audience == "" {
		audience = DefaultSupabaseAudience
	}

	leeway, err := envDuration(env, EnvSupabaseLeeway)
	if err != nil {
		return nil, err
	}
	maxAge, err := envDuration(env, EnvSupabaseMaxAge)
	if err != nil {
		return nil, err
	}

	return NewSupabaseJWT(SupabaseConfig{
		Authority:   authority,
		Keys:        keys,
		Issuer:      strings.TrimSpace(env[EnvSupabaseIssuer]),
		Audience:    audience,
		Provider:    strings.TrimSpace(env[EnvSupabaseProvider]),
		RequireRole: strings.TrimSpace(env[EnvSupabaseRequireRole]),
		Leeway:      leeway,
		MaxAge:      maxAge,
	})
}

// envDuration reads a Go duration setting. Empty means the zero value.
//
// ⚠ ONE READER FOR BOTH DURATIONS, BECAUSE TWO WOULD BE TWO CHANCES TO GET THE SAME
// PARSE WRONG — the one-rule-one-place ruling `secretFrom` already carries. An
// unparseable value is an error and never the zero: the operator who typed `10min`
// believes a bound is armed, and `time.ParseDuration` refuses that spelling.
func envDuration(env map[string]string, name string) (time.Duration, error) {
	raw := strings.TrimSpace(env[name])
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a duration (%v)", name, raw, err)
	}
	return d, nil
}

func trustedHeaderFromEnv(env map[string]string, authority ModelSource) (*TrustedHeader, error) {
	fronted, err := envBool(env, EnvProxyFronted)
	if err != nil {
		return nil, err
	}
	requireCert, err := envBool(env, EnvProxyRequireClientCert)
	if err != nil {
		return nil, err
	}
	secret, err := secretFrom(env, EnvProxySecret, EnvProxySecretFile)
	if err != nil {
		return nil, err
	}
	var peers []string
	if raw := strings.TrimSpace(env[EnvProxyPeers]); raw != "" {
		peers = strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
	}
	return NewTrustedHeader(TrustedHeaderConfig{
		ProxyFronted:      fronted,
		SubjectHeader:     strings.TrimSpace(env[EnvProxySubjectHeader]),
		SecretHeader:      strings.TrimSpace(env[EnvProxySecretHeader]),
		Secret:            secret,
		RequireClientCert: requireCert,
		ProxyPeers:        peers,
		Provider:          strings.TrimSpace(env[EnvProxyProvider]),
		Authority:         authority,
	})
}

// envBool reads a boolean setting.
//
// 🔴 AN UNRECOGNISED VALUE IS AN ERROR, NEVER `false`. `CAIRN_TRUSTED_HEADER_PROXY_FRONTED=treu`
// must not silently mean "not proxy-fronted": an operator who typed it believes the
// backend is armed, and a setting that reads a typo as its own default is how a
// deployment ends up in a state nobody chose. `strconv.ParseBool` would accept `t`/`T`
// and reject `yes`, which is the opposite of what an operator writing YAML types, so the
// accepted spellings are enumerated here.
func envBool(env map[string]string, name string) (bool, error) {
	raw := strings.TrimSpace(env[name])
	switch strings.ToLower(raw) {
	case "":
		return false, nil
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf(
			"%s: %q is not a boolean. Write yes or no — an unrecognised value is refused rather than read as `no`, because a typo that silently disables a security setting leaves the operator believing it is on",
			name, raw)
	}
}

// secretFrom reads a secret from an inline variable or from a file.
//
// 🔴 BOTH SET IS A REFUSAL, NOT A PRECEDENCE RULE. Two sources for one secret means the
// answer to "which one is live" depends on a precedence nobody reads, and a rotation that
// updates the wrong one appears to work. The same one-rule-one-place ruling the token
// file already carries.
//
// ⚠ THE FILE FORM IS THE ONE TO PREFER, AND THE REASON IS THE SAME ONE `AGENTS.md`
// RECORDS FOR `gh secret set`: a secret in an environment variable is readable from
// `/proc/<pid>/environ`, is inherited by every child process, and is printed by any crash
// dump or `env` that reaches a log. A mounted file is readable by exactly what opens it.
// The pod already mounts its bearer token that way.
//
// ⚠ A TRAILING NEWLINE IS STRIPPED FROM THE FILE FORM AND FROM NOTHING ELSE. Every editor
// and every `echo` adds one, and a secret that differs from the proxy's by an invisible
// byte fails with a refusal that says nothing about why. Interior whitespace is left
// alone: it may be part of the secret.
//
// 🔴 AND A FILE THAT YIELDS ZERO BYTES IS A REFUSAL HERE, BECAUSE THE CONSTRUCTOR CANNOT
// TELL IT FROM "NO SECRET CONFIGURED" AND BLAMES THE WRONG SETTING. A file holding only
// the newline an editor added strips to nothing, returns a zero-length slice, and reaches
// `NewTrustedHeader`'s `len(cfg.Secret) == 0` rung — so the operator is told there is no
// source check at all and to configure the very secret they did configure, in a crash
// loop. The MISSING-file case was already closed for exactly this reason; this is the same
// mis-blame one step further in, where the file exists and is empty.
func secretFrom(env map[string]string, inline, fromFile string) ([]byte, error) {
	direct := env[inline]
	path := strings.TrimSpace(env[fromFile])
	if direct != "" && path != "" {
		return nil, fmt.Errorf(
			"%s and %s are both set: one secret, one source. Which is live would depend on a precedence rule nobody reads, and a rotation that updated the other would appear to work",
			inline, fromFile)
	}
	if direct != "" {
		return []byte(direct), nil
	}
	if path == "" {
		return nil, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fromFile, err)
	}
	secret := strings.TrimRight(string(body), "\r\n")
	if secret == "" {
		return nil, fmt.Errorf(
			"%s: %s is empty, or holds only the newline an editor added. That is not the same as no secret "+
				"configured: read as one, the refusal you would get names the source check rather than this file, "+
				"and tells you to configure the secret you already did",
			fromFile, path)
	}
	return []byte(secret), nil
}
