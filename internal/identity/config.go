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
// be set alone without arming the check — which is the same defect one level down; one in
// the WRONG list arms the wrong backend and leaves the right one unarmed by the only
// variable that should arm it. `TestTheEnvironmentLedgersNameEveryVariableEachBackendReads`
// pins BOTH of those, and from two derivations rather than one: the union of the ledgers
// against every `Env*` declaration in this package's own source, and each list separately
// against the `Env*` names its OWN constructor references — `supabaseEnv` against
// `supabaseFromEnv`, `proxyEnv` against `trustedHeaderFromEnv` — both read BY AST.
//
// ⚠ EACH HALF OF THAT WAS MEASURED FALSE ONCE, IN THE SAME SHAPE: A SENTENCE ABOUT A
// RELATIONSHIP OVER A TEST THAT INSPECTED ONE SIDE. First, the "every declaration" side
// was a hand-written list of the same fifteen names, so a sixteenth absent from the list
// and from both ledgers was in neither side of the comparison and the whole suite stayed
// green with it live. Then, with that side derived, the comparison was still a UNION: a
// name in the wrong ledger is in the union either way, and moving `EnvProxyPeers` from
// `proxyEnv` into `supabaseEnv` left the test green. Read it as a worked example of the
// class rather than as two closed bugs.
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

	// 🔴 ALSO BEFORE THE `anySet` BRANCHES, AND FOR THE MIRROR-IMAGE REASON. `anySet`
	// TRIMS, so a whitespace-only value arms nothing at all; a check inside a backend's
	// own branch could therefore never reach the deployment that has only that. It asks
	// `anySet` ITSELF, once per ledger — the question is "is THIS backend armed by
	// anything else", which is what bounds the refusal to the silently-off case.
	//
	// ⚠ IT IS HALF THE ANSWER, AND `envSetting` IS THE OTHER HALF — SAY SO HERE, BECAUSE
	// THE TWO ARE EASY TO READ AS ONE. This one covers the ledger NOTHING armed, which is
	// the only case a constructor never runs for. Where a ledger IS armed the constructors
	// do run, every value passes through a reader, and `envSetting` refuses the same
	// spelling there — so the whitespace-only case is closed on both sides of the branch
	// rather than only on the side the ledgers can see.
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
//
// 🔴 PRESENT AND NON-EMPTY, NOT `TrimSpace(…) != ""`, WHICH IS WHAT MAKES THIS THE MIRROR
// IMAGE `FromEnvironment` CALLS IT. Trimming before the test discards exactly the value
// `refuseBlankSettings` was written to stop being discarded: measured, `CAIRN_SUPABASE_JWT_SECRET`
// set to three spaces returned a nil error and a machine-token-only chain, so a line the
// operator wrote vanished with no log — the silent ignoring `retiredEnv`'s own comment
// refuses one paragraph up. The EMPTY string is left alone for the same reason it is there:
// it carries no value to discard, and a manifest emitting every variable with an empty
// default is a shape this package deliberately does not refuse.
//
// ⚠ AND THE PER-LEDGER NARROWING IN `refuseBlankSettings` HAS NO ANALOGUE HERE. That
// narrowing asks whether the backend a blank value belongs to is armed by something else;
// a retired name belongs to no backend and can never arm one, so there is no case in which
// a value here is anything but a line to delete.
func refuseRetiredSettings(env map[string]string) error {
	names := make([]string, 0, len(retiredEnv))
	for name := range retiredEnv {
		if value, present := env[name]; present && value != "" {
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

// ErrBlankSetting is the refusal a setting earns by being PRESENT and holding only
// whitespace. Two guards return it and they cover different halves of one hazard:
// `refuseBlankSettings` for a ledger nothing else armed, where no constructor runs and no
// reader is reached; `envSetting` for every value a reader does reach, armed ledger
// included. An ABSENT name and a name holding the EMPTY string are outside both — see
// `envSetting` for why the scope is drawn there.
var ErrBlankSetting = errors.New("identity: a setting is present and holds only whitespace")

// refuseBlankSettings refuses a whitespace-only value in a ledger NOTHING ELSE ARMED.
//
// 🔴 A WHITESPACE-ONLY VALUE IS INDISTINGUISHABLE FROM AN ABSENT ONE TO `anySet`, SO THE
// BACKEND THE OPERATOR CONFIGURED IS SILENTLY OFF — the same defect `supabaseEnv`'s comment
// describes one level up, in the one spelling the ledgers cannot see. Measured before this
// guard existed: `FromEnvironment({CAIRN_TRUSTED_HEADER_SECRET: "   "}, …)` returned a nil
// error and a ONE-backend chain, so the pod came up healthy, passed its health check, and
// logged nothing about the trusted-header backend it was not running.
//
// 🔴 AND THE SILENT-OFF CASE IS THE WHOLE OF IT, WHICH IS WHY THE LEDGERS ARE ASKED ONE AT
// A TIME RATHER THAN AS A UNION. Where a ledger holds something that DOES arm its backend,
// a blank sibling is not a backend silently off — it is an unset optional, and the
// refusal's own sentence would be false for that input. Measured on the first version of
// this guard, which walked the union unconditionally: a COMPLETE trusted-header
// configuration carrying `CAIRN_TRUSTED_HEADER_PEERS="  "` beside it was refused, and
// `main.go` turned that into `os.Exit(78)` — a crash loop for a deployment that had worked.
// `TestABlankSettingBesideAnArmedBackendIsAcceptedAsUnset` pins both halves, including the
// arm that keeps this per-LEDGER: an armed Supabase backend does not license a blank proxy
// secret, because the trusted-header backend is still the one silently off.
//
// ⚠ "AN UNSET OPTIONAL" IS NOW WHAT ONLY *SOME* READERS MAKE OF IT, AND THIS PARAGRAPH
// USED TO SAY OTHERWISE. It read "`envBool`, `envDuration` and every direct field read
// trim, so a blank value there is the zero" — true of the code then, and the measured
// defect: with a COMPLETE armed trusted-header configuration,
// `CAIRN_TRUSTED_HEADER_REQUIRE_CLIENT_CERT="  "` produced a nil error and a backend with
// `requireCert` FALSE, so mTLS was not enforced while `envBool` refused `treu` three lines
// away — the same hazard in two spellings, refused in one. The readers that reach
// `envSetting` now refuse the whitespace-only spelling themselves, and this guard is
// unchanged: it still skips an armed ledger, because its own sentence is about a backend
// being silently OFF and that sentence is false there. The SEVEN setting READERS still on
// the bare `strings.TrimSpace(env[…])` shape — `anySet`'s own use of it is the ledger scan
// rather than a reader, so it is not one of them — do turn a blank value into the zero, and
// `envSetting`'s
// comment enumerates them with what each zero is — a documented default, or a constructor
// refusal naming the missing field. Never a check switched off; that was the discriminator.
//
// ⚠ AND `secretFrom` IS THE THIRD SHAPE, WITH ITS TWO FORMS ON OPPOSITE SIDES. It does NOT
// trim its inline form, so a whitespace `CAIRN_TRUSTED_HEADER_SECRET` beside an armed
// backend reaches `NewTrustedHeader` as three bytes — measured, on this tree and at
// `7ac810e` alike, it is refused by the 32-byte length floor rather than read as unset.
// Refused either way, by a NARROWER guard naming the real problem. The FILE form had no such
// floor behind it and is read through `envSetting` now; the measurement is beside the call.
//
// ⚠ PRESENT-AND-WHITESPACE ONLY, AND TWO FURTHER CASES ARE DELIBERATELY LEFT ALONE ON TOP
// OF THE ARMED-LEDGER ONE ABOVE. An ABSENT name is how a deployment says "I am not using this
// backend" — refusing it would refuse every deployment, including the machine-token-only
// one this package's whole compatibility claim rests on. A name present with the EMPTY
// string is left alone too: it carries no value to mistake for one, and a manifest that
// emits every variable with an empty default is a common enough shape that refusing it
// would be a second, wider change than the defect measured above.
func refuseBlankSettings(env map[string]string) error {
	var names []string
	for _, ledger := range [][]string{supabaseEnv, proxyEnv} {
		if anySet(env, ledger) {
			// This backend is armed by something else in its own ledger, so nothing
			// here is silently off and a blank value is an unset optional.
			continue
		}
		for _, name := range ledger {
			value, present := env[name]
			if !present || value == "" {
				continue
			}
			if strings.TrimSpace(value) == "" {
				names = append(names, name)
			}
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

// envSetting reads one setting, trimmed, and REFUSES a value that is present and holds
// only whitespace.
//
// 🔴 ONE PREDICATE IN ONE PLACE, AND THE HAZARD IT CLOSES IS `envBool`'S OWN IN A DIFFERENT
// SPELLING. `envBool` refuses `treu` because "a typo that silently disables a security
// setting leaves the operator believing it is on" — and it used to accept `"  "`, which is
// that sentence exactly. Measured at `d5880f3` with a complete, armed trusted-header
// configuration (shared secret AND client-certificate requirement):
// `CAIRN_TRUSTED_HEADER_REQUIRE_CLIENT_CERT="yes"` gave `requireCert` true;
// `…="treu"` was refused; `…="  "` returned a NIL error and `requireCert` FALSE — mTLS not
// enforced, pod healthy, nothing logged. The same shape reached
// `CAIRN_SUPABASE_REQUIRE_ROLE` (no role required), `CAIRN_TRUSTED_HEADER_PEERS` (no peer
// allowlist) and `CAIRN_SUPABASE_MAX_AGE` (no `iat` bound): settings whose ZERO IS
// PERMISSIVE, which is what makes a value read as unset a security decision nobody made.
//
// 🔴 AND A FIFTH WAS FOUND BY READING THIS COMMENT AGAINST THE CODE RATHER THAN BY THE
// AUDIT THAT NAMED THE OTHER FOUR. `CAIRN_TRUSTED_HEADER_SECRET_FILE="  "` beside
// `CAIRN_TRUSTED_HEADER_REQUIRE_CLIENT_CERT=yes` built a backend with a ZERO-length secret
// and authenticated a request that carried the certificate and no shared-secret header —
// defence in depth reduced to one rung, silently. `secretFrom` reads it through here now.
//
// 🔴 IT IS A FUNCTION RATHER THAN FIVE COPIES BECAUSE A PREDICATE OPEN-CODED AT N SITES IS
// TYPICALLY WRONG AT N−1 OF THEM, IN THE SAME DIRECTION — and the fifth site above is that
// claim arriving on schedule. The callers are `envBool` (proxy-fronted,
// require-client-cert), `envDuration` (leeway, max-age), `secretFrom` (the secret PATH, not
// the inline form) and the direct reads for the required role and the peer list. Leeway and
// proxy-fronted come along because they share a reader, not because their zero is permissive
// — a blank line is a typo in every case, and the refusal says nothing that is false for
// them. What does NOT route through here is the other shape, and it was measured rather
// than assumed — all seven of it. Blank REFUSES by name at the constructor: the issuer, the
// JWKS URL, the subject header, the trusted-header provider. Blank becomes a documented
// DEFAULT: the audience (`authenticated`), the Supabase provider (`supabase`), the secret
// header (`X-Cairn-Proxy-Secret`). Neither group has a permissive zero, which is why they
// stay on a bare `strings.TrimSpace` and why `TestABlankSettingBesideAnArmedBackendIsAcceptedAsUnset`
// still has arms.
//
// ⚠ TWO CASES ARE DELIBERATELY LEFT ALONE, AND THEY ARE THE ONES `refuseBlankSettings`
// ALREADY DECIDED. An ABSENT name is how a deployment says "I am not using this" — refusing
// it would refuse the machine-token-only deployment this package's whole compatibility
// claim rests on. A name present with the EMPTY string is left alone too: a manifest that
// emits every variable with an empty default is a common shape, and refusing it is a wider
// change than the defect above. ONLY the whitespace-only spelling moves.
//
// ⚠ AND IT FIRES BESIDE AN ARMED BACKEND, WHERE `refuseBlankSettings` DELIBERATELY DOES
// NOT — THAT IS NOT THAT NARROWING REVERTED. The narrowing exists because the LEDGER-level
// refusal's own sentence ("the backend it belongs to would be silently OFF") is false when
// something else armed the ledger, and a false message crash-looped a deployment that
// worked. This refusal makes no claim about a backend being off; it says the line reads as
// unset, which is true in both worlds.
func envSetting(env map[string]string, name string) (string, error) {
	raw, present := env[name]
	if !present || raw == "" {
		return "", nil
	}
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		return trimmed, nil
	}
	return "", fmt.Errorf(
		"%w: %s=%q. Give it a value or delete the line — trimmed to nothing it is read as UNSET, "+
			"which is a different configuration from the one written here, and where the unset value "+
			"turns a check OFF that is a security setting silently disabled",
		ErrBlankSetting, name, raw)
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
	// 🔴 `envSetting`, NOT A BARE `TrimSpace`: THE ZERO HERE IS "NO ROLE IS REQUIRED".
	// A whitespace-only value would turn the role check off for a deployment that wrote
	// one down — see `envSetting`, which states the measurement.
	requireRole, err := envSetting(env, EnvSupabaseRequireRole)
	if err != nil {
		return nil, err
	}

	return NewSupabaseJWT(SupabaseConfig{
		Authority:   authority,
		Keys:        keys,
		Issuer:      strings.TrimSpace(env[EnvSupabaseIssuer]),
		Audience:    audience,
		Provider:    strings.TrimSpace(env[EnvSupabaseProvider]),
		RequireRole: requireRole,
		Leeway:      leeway,
		MaxAge:      maxAge,
	})
}

// envDuration reads a Go duration setting. ABSENT, or present with the empty string,
// means the zero value; present and whitespace-only is a refusal, via `envSetting`.
//
// ⚠ ONE READER FOR BOTH DURATIONS, BECAUSE TWO WOULD BE TWO CHANCES TO GET THE SAME
// PARSE WRONG — the one-rule-one-place ruling `secretFrom` already carries. An
// unparseable value is an error and never the zero: the operator who typed `10min`
// believes a bound is armed, and `time.ParseDuration` refuses that spelling. A
// whitespace-only value is the same claim in the spelling `ParseDuration` never sees,
// which is why the blank test is in `envSetting` above it rather than here.
func envDuration(env map[string]string, name string) (time.Duration, error) {
	raw, err := envSetting(env, name)
	if err != nil {
		return 0, err
	}
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
	// 🔴 `envSetting`, NOT A BARE `TrimSpace`: THE ZERO HERE IS "NO PEER ALLOWLIST". The
	// split below is over whitespace as well as commas, so a whitespace-only value yields
	// an EMPTY field set rather than a bad one — indistinguishable from unset, and the
	// operator who wrote a narrowing down gets every peer. See `envSetting`.
	rawPeers, err := envSetting(env, EnvProxyPeers)
	if err != nil {
		return nil, err
	}
	var peers []string
	if rawPeers != "" {
		peers = strings.FieldsFunc(rawPeers, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
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
// accepted spellings are enumerated below.
//
// ⚠ THE ACCEPTED SET IS `1 t true y yes on` AND `0 f false n no off`, CASE-INSENSITIVELY,
// PLUS THE ABSENT AND EMPTY-STRING SPELLINGS OF "NOT SET", WHICH READ AS `false`. A
// WHITESPACE-ONLY value is NOT in that set and does not reach the switch: `envSetting`
// refuses it one call up, because "reads as its own default" was exactly as true of `"  "`
// as of `treu` while only one of the two was refused. The `""` case below is therefore the
// absent and empty-string spellings only.
func envBool(env map[string]string, name string) (bool, error) {
	raw, err := envSetting(env, name)
	if err != nil {
		return false, err
	}
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
	// 🔴 `envSetting` ON THE PATH, AND THE INLINE FORM DELIBERATELY NOT — THE TWO ARE
	// ALREADY IN DIFFERENT PLACES AND ONLY ONE OF THEM WAS SAFE. Measured on this tree
	// while checking the sentence above: with `CAIRN_TRUSTED_HEADER_REQUIRE_CLIENT_CERT=yes`
	// arming the backend, `CAIRN_TRUSTED_HEADER_SECRET_FILE="  "` built quietly with a
	// ZERO-length secret — and a request carrying a verified client certificate and NO
	// shared-secret header AUTHENTICATED, where the same request against the real path is
	// refused with "the shared-secret header is absent or duplicated". Defence in depth
	// silently reduced to one rung, by whitespace, for a deployment that wrote both down.
	// The INLINE form needs nothing: it is not trimmed, so `"  "` arrives as two bytes and
	// the 32-byte floor refuses it by name — measured in the same run.
	path, err := envSetting(env, fromFile)
	if err != nil {
		return nil, err
	}
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
