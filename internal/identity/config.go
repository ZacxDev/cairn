package identity

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// The environment this package reads. New names use the `CAIRN_` prefix, per
// `AGENTS.md`'s naming rule; nothing here has a `SUBSYSTEM_STORE_` alias because
// nothing here has ever been deployed under one, and minting an alias for a setting
// with no existing holder is inventing a migration nobody needs.
const (
	// EnvSupabaseJWKSURL is the JWKS endpoint — `https://<ref>.supabase.co/auth/v1/.well-known/jwks.json`.
	EnvSupabaseJWKSURL = "CAIRN_SUPABASE_JWKS_URL"
	// EnvSupabaseSecret is the LEGACY symmetric JWT secret, inline.
	EnvSupabaseSecret = "CAIRN_SUPABASE_JWT_SECRET"
	// EnvSupabaseSecretFile is the same secret, read from a file.
	EnvSupabaseSecretFile = "CAIRN_SUPABASE_JWT_SECRET_FILE"
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
// constants above, failing when the set GROWS as well as when it shrinks.
var supabaseEnv = []string{
	EnvSupabaseJWKSURL,
	EnvSupabaseSecret,
	EnvSupabaseSecretFile,
	EnvSupabaseIssuer,
	EnvSupabaseAudience,
	EnvSupabaseProvider,
	EnvSupabaseRequireRole,
	EnvSupabaseLeeway,
}

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
// `machine` is always first in the returned chain. `keys` is the key set to refresh, or
// nil when no asymmetric verification is configured.
func FromEnvironment(env map[string]string, authority interface {
	TokenAuthority
	ModelSource
}) (Chain, *SupabaseJWT, error) {
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

func anySet(env map[string]string, names []string) bool {
	for _, name := range names {
		if strings.TrimSpace(env[name]) != "" {
			return true
		}
	}
	return false
}

func supabaseFromEnv(env map[string]string, authority ModelSource) (*SupabaseJWT, error) {
	secret, err := secretFrom(env, EnvSupabaseSecret, EnvSupabaseSecretFile)
	if err != nil {
		return nil, err
	}

	var keys *KeySet
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

	var leeway time.Duration
	if raw := strings.TrimSpace(env[EnvSupabaseLeeway]); raw != "" {
		leeway, err = time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a duration (%v)", EnvSupabaseLeeway, raw, err)
		}
	}

	return NewSupabaseJWT(SupabaseConfig{
		Authority:   authority,
		Keys:        keys,
		Secret:      secret,
		Issuer:      strings.TrimSpace(env[EnvSupabaseIssuer]),
		Audience:    audience,
		Provider:    strings.TrimSpace(env[EnvSupabaseProvider]),
		RequireRole: strings.TrimSpace(env[EnvSupabaseRequireRole]),
		Leeway:      leeway,
	})
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
	return []byte(strings.TrimRight(string(body), "\r\n")), nil
}
