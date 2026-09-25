package identity

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
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
	// EnvProxyPeers narrows which peer addresses may present the identity header.
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

// ============================================================================
// The declared blank policy: what a value that REDUCES TO NOTHING means, as DATA.
// ============================================================================

// blankPolicy is what this package does with a setting that is PRESENT and whose value
// reduces to nothing.
//
// 🔴 IT IS DATA BECAUSE THE PROSE VERSION KEPT BEING WRONG, AND THAT IS MEASURED RATHER
// THAN STYLISTIC. Round after round of this file found one more setting whose blank or
// degenerate value silently disabled the check it configures, each time in a place the
// previous round's comment said was covered — seven measured instances over six settings by
// the time this pass ran, the last two of them (`PEERS=","` and a 32-space inline secret)
// live at `70636bd` beside a comment that said the class was closed. ⚠ The count of ROUNDS
// is deliberately not stated: it is not derivable from this tree, and the instances are.
// The reason is structural: "is this setting set?" had SIX different answers in one file — `refuseRetiredSettings`'s
// `present && value != ""`, the same predicate open-coded again in the ledger sweep,
// `envSetting` as a function, a bare `strings.TrimSpace(env[…])` at seven reader sites,
// `secretFrom`'s untrimmed inline read, and `anySet` — and which one a setting got was
// decided by WHICH READER IT HAPPENED TO BE PLUMBED THROUGH. The organising principle the
// comments claimed, "does this setting's zero turn a check off", existed nowhere in the
// data: the ledgers were `[]string`, so nothing linked a name to a policy, and the policy
// lived in a hand-maintained comment with no gate.
//
// 🔴 THE ZERO VALUE IS `policyUndeclared` ON PURPOSE. A `setting` literal that forgets to
// say what its blank means does NOT quietly get the permissive one — it gets a policy the
// resolver refuses at runtime and
// `TestEverySettingDeclaresItsBlankPolicyAndTheReaderObeysIt` fails on. Adding a setting
// without deciding is a RED test, which is the whole point of the pass: without it this is
// the same problem rewritten.
type blankPolicy int

const (
	// policyUndeclared is the zero value and is never a decision. See above.
	policyUndeclared blankPolicy = iota

	// refuseBlank: a value that reduces to nothing is a MISCONFIGURATION. Refuse,
	// naming the variable and what unset would have meant.
	//
	// This is the policy for every setting whose unset value is PERMISSIVE — where
	// reading the line as "not set" turns a check off that the operator wrote down —
	// and for the two whose unset value is not permissive but for which a blank line
	// is a typo all the same (`EnvProxyFronted`, `EnvSupabaseLeeway`). The refusal
	// says nothing that is false for either group.
	refuseBlank

	// defaultsTo: a value that reduces to nothing is genuinely "not set", and unset is
	// a DOCUMENTED DEFAULT that leaves the same check armed. Accepted, and the reader
	// produces the empty string so the constructor applies its default.
	defaultsTo

	// refusedByConstructor: a value that reduces to nothing is "not set", and the
	// constructor refuses THAT by name — so a second refusal here would be the second
	// copy of one decision, and it would disagree with the EMPTY-string spelling of the
	// identical configuration, which reaches the same rung. `setting.refusedBy` names
	// the rung, and the gate asserts a blank value actually reaches it.
	refusedByConstructor
)

func (p blankPolicy) String() string {
	switch p {
	case refuseBlank:
		return "refuseBlank"
	case defaultsTo:
		return "defaultsTo"
	case refusedByConstructor:
		return "refusedByConstructor"
	default:
		return "policyUndeclared"
	}
}

// setting is one environment variable and everything this package decides about it.
//
// ⚠ EVERY FIELD IS READ BY THE ONE RESOLVER OR BY THE GATE — none of it is documentation
// that nothing checks, which is the shape the ~50-line enumeration comment this type
// replaces had become.
type setting struct {
	// name is the environment variable.
	name string

	// policy is what a value that reduces to nothing means. See `blankPolicy`.
	policy blankPolicy

	// unset says, in the operator's words, what this package does when the setting is
	// NOT set. For `refuseBlank` it is the hazard the refusal quotes back; for the
	// other two it is the benign outcome that makes accepting a blank correct.
	unset string

	// refusedBy is the construction rung a `refusedByConstructor` setting's unset value
	// reaches, and it is nil for every other policy. The gate asserts both halves, and
	// asserts that a blank value beside an ARMED ledger really does land on this error
	// rather than on some other refusal that would satisfy a bare `err != nil`.
	refusedBy error

	// keepWhitespace makes the resolved value the RAW string rather than the trimmed
	// one. Only the shared secret sets it: interior and edge whitespace may legitimately
	// be part of a secret, so trimming would silently change it.
	//
	// ⚠ IT DOES NOT WIDEN WHAT COUNTS AS BLANK. A secret with no CONTENT in it still
	// reduces to nothing — entirely whitespace, and also entirely zero-width, which is
	// the spelling that outlived the first version of this sentence. See
	// `reducesToNothing` for what "content" is and what it still lets through, and
	// `MinProxySecretBytes` for why the length floor is not that check.
	keepWhitespace bool

	// fields, when non-nil, says this setting's value is a LIST and gives the split the
	// reader uses. It is what lets a value that DOES carry content still reduce to
	// nothing — `CAIRN_TRUSTED_HEADER_PEERS=","`.
	fields func(string) []string
}

// reducesToNothing is THE predicate. One place, for every caller, for every setting.
//
// 🔴 A PREDICATE OPEN-CODED AT N SITES IS TYPICALLY WRONG AT N−1 OF THEM, IN THE SAME
// DIRECTION, AND THIS FILE PRODUCED SIX INSTANCES OF THE RESULTING HAZARD IN AS MANY
// ROUNDS. Three spellings of "nothing" the sites disagreed about are all here: whitespace-only
// (which `strings.TrimSpace` sees), ZERO-WIDTH-only (which it does not) and separator-only
// (which it does not either).
//
// 🔴 THE FIRST LIMB IS A CONTENT TEST, NOT A WHITESPACE TEST, AND IT REPLACED
// `strings.TrimSpace(raw) == ""` BECAUSE THAT COVERED ONE SPELLING OF INVISIBILITY AND
// NOT THE OTHER. `strings.TrimSpace` uses `unicode.IsSpace`, whose set is `White_Space`:
// it holds NBSP (U+00A0) and every `Zs` separator, and it does NOT hold the zero-width
// runes, which are `Cf`. Measured at `02fad01` with a complete armed trusted-header
// configuration (`PROXY_FRONTED=yes`, a subject header, a provider, no client-certificate
// requirement, so the shared secret was the only source check):
// `CAIRN_TRUSTED_HEADER_SECRET` set to 32 × U+200B, 32 × U+2060 or 32 × U+FEFF each BUILT
// and was held as a 96-byte LIVE shared secret — a caller sending the same run of
// zero-width runes plus a subject header authenticated as any user in this control plane —
// while 32 × U+00A0 and 32 × U+0020 were refused: one rung, two spellings of nothing,
// and only one of them refused.
//
// ⚠ WHAT THE LIMB TESTS, STATED AS THE PREDICATE RATHER THAN AS A GUARANTEE: the value
// holds at least one rune that is GRAPHIC and is not a SPACE. `unicode.IsGraphic` is
// `L|M|N|P|S|Zs`, so removing `Zs` by `!unicode.IsSpace` leaves "a rune that could carry
// content". `unicode.IsPrint` would do here too — measured over all 0x110000 code points,
// `IsGraphic(r) && !IsSpace(r)` and `IsPrint(r) && !IsSpace(r)` agree on every one of them,
// because every `Zs` rune is in `White_Space` — so the choice is a naming one and
// `IsGraphic` is the one that says what is meant. It is a strict WIDENING: also measured
// over all 0x110000, every rune `TrimSpace` called blank is still blank here, and 963,042
// more are too (controls, `Cf`, surrogates, private use, and the unassigned).
//
// 🔴 AND IT IS NOT A COMPLETENESS CLAIM — A FURTHER SPELLING IS OPEN, NAMED HERE RATHER
// THAN IMPLIED CLOSED. A rune can be graphic by category and still render as blank width:
// U+2800 BRAILLE PATTERN BLANK (`So`), U+3164 HANGUL FILLER, U+115F, U+FFA0 (`Lo`), and a
// lone combining mark such as U+0301 (`Mn`). A value made entirely of those passes this
// limb, so `SECRET` = 32 × U+2800 is still accepted as a live 96-byte secret. Closing it
// needs a rendered-width judgement this package has no source for, and the fix would be a
// new limb rather than a wider version of this one; it is recorded, not fixed. Two smaller
// scope notes, both measured: invalid UTF-8 counts as CONTENT (the bytes decode to
// U+FFFD, which is `So`), so a binary secret is not refused; and a value that mixes
// content with zero-width runes is content — `"s3cret\u200bmore"` is accepted byte for
// byte, which `TestTheInlineSecretKeepsItsWhitespaceAndAnInvisibleOneIsRefused` pins.
//
// 🔴 THE LIST HALF IS AN EXTENSION, NEVER A REPLACEMENT — the content test above runs first
// for every setting, so no spelling gets WEAKER by declaring a split. Measured at
// `70636bd` with a complete armed trusted-header configuration: `PEERS=","`, `",,"`,
// `", ,"`, `" , "` and `"\t,\n"` all BUILT with zero peers, so `Authenticate`'s
// `if len(t.peers) > 0` never ran and any address could present the identity header —
// while `PEERS="  "` was refused. The guard covered the whitespace spelling of the hazard
// and not the separator spelling of the same one.
func (s setting) reducesToNothing(raw string) bool {
	if !strings.ContainsFunc(raw, carriesContent) {
		return true
	}
	return s.fields != nil && len(s.fields(raw)) == 0
}

// carriesContent is the rune test `reducesToNothing`'s first limb is built from: graphic,
// and not a space. Its scope, and the spelling it does NOT close, are stated there.
func carriesContent(r rune) bool {
	return unicode.IsGraphic(r) && !unicode.IsSpace(r)
}

// ValueReducesToNothing is the content limb of `reducesToNothing`, exported for a setting
// that is read OUTSIDE this package.
//
// 🔴 EXPORTED RATHER THAN RE-SPELLED, BECAUSE THIS FILE'S WHOLE HISTORY IS ONE PREDICATE
// OPEN-CODED AT N SITES AND WRONG AT N−1 OF THEM. `cmd/cairn-server` reads
// `CAIRN_CONTROL_JOURNAL`, which does not belong to a backend ledger (a ledger's `armed`
// question is "is this BACKEND half-configured", and a journal path is not a backend —
// and the gate requires a ledger to hold at least two settings, which would mean
// inventing a second one). It still has to answer "what does a value that reduces to
// nothing mean", and answering it with a fresh `strings.TrimSpace` there would reproduce
// the measured bypass this predicate exists for: 32 zero-width runes are not whitespace,
// and `TrimSpace` calls them content.
//
// ⚠ IT IS THE CONTENT LIMB ONLY. The LIST half of `reducesToNothing` is per-setting — it
// needs that setting's own split function — and a caller with no declared split gets
// exactly what `setting{fields: nil}` gets. A path is not a list, so there is nothing
// here for the second limb to say.
func ValueReducesToNothing(raw string) bool { return setting{}.reducesToNothing(raw) }

// value is what the reader gets for a raw string this setting does NOT reduce to nothing.
func (s setting) value(raw string) string {
	if s.keepWhitespace {
		return raw
	}
	return strings.TrimSpace(raw)
}

// ledger is one backend's settings, and the name to use when telling an operator which
// backend a line belongs to.
//
// 🔴 A PARTIALLY-CONFIGURED BACKEND MUST REFUSE TO START, NOT SILENTLY BE OFF, AND THESE
// ARE HOW THAT QUESTION IS ASKED. "Build the backend if the required fields are present"
// reads sensibly and is the dangerous version: an operator who sets the subject header and
// forgets the shared secret gets a pod that comes up, passes its health check and quietly
// authenticates nobody through a backend they believe is live. So the trigger is "did
// anybody touch ANY of these", and the answer to a broken configuration is `os.Exit(78)`
// in the crash loop where an operator will see it.
//
// ⚠ EACH LEDGER MUST NAME EVERY VARIABLE ITS BACKEND READS. One left out is one that can
// be set alone without arming the check — the same defect one level down; one in the WRONG
// ledger arms the wrong backend and leaves the right one unarmed by the only variable that
// should arm it. `TestTheEnvironmentLedgersNameEveryVariableEachBackendReads` pins BOTH,
// and from two derivations rather than one: the union of the ledgers against every `Env*`
// declaration in this package's own source, and each ledger separately against the `Env*`
// names its OWN constructor references — both read BY AST.
//
// ⚠ EACH HALF OF THAT WAS MEASURED FALSE ONCE, IN THE SAME SHAPE: A SENTENCE ABOUT A
// RELATIONSHIP OVER A TEST THAT INSPECTED ONE SIDE. First, the "every declaration" side
// was a hand-written list of the same fifteen names, so a sixteenth absent from the list
// and from both ledgers was in neither side of the comparison and the whole suite stayed
// green with it live. Then, with that side derived, the comparison was still a UNION: a
// name in the wrong ledger is in the union either way, and moving `EnvProxyPeers` from
// `proxyEnv` into `supabaseEnv` left the test green. Read it as a worked example of the
// class rather than as two closed bugs.
type ledger struct {
	// backend is what an operator calls this thing.
	backend string
	// settings is every variable the backend reads, each with its declared policy.
	settings []setting
}

// peerFields is the split `EnvProxyPeers` is read with — commas OR whitespace, so an
// operator may write either and `netid.TrustedNetwork` sees one entry per element.
//
// 🔴 IT IS ONE FUNCTION BECAUSE THE SETTING'S BLANK TEST AND ITS READER MUST NOT BE ABLE
// TO DISAGREE. When they were two expressions, `reducesToNothing`'s ancestor tested
// whitespace and the reader split on separators, so a value that yielded no fields passed
// the first and produced an empty allowlist at the second.
func peerFields(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
}

var supabaseEnv = ledger{
	backend: "Supabase",
	settings: []setting{
		{
			name:      EnvSupabaseJWKSURL,
			policy:    refusedByConstructor,
			unset:     "there is no key set, so no token could ever verify",
			refusedBy: ErrSupabaseNoKeys,
		},
		{
			name:      EnvSupabaseIssuer,
			policy:    refusedByConstructor,
			unset:     "there is no `iss` to require",
			refusedBy: ErrSupabaseNoIssuer,
		},
		{
			name:   EnvSupabaseAudience,
			policy: defaultsTo,
			unset:  "the audience is `" + DefaultSupabaseAudience + "`, which is what Supabase issues — the check stays armed",
		},
		{
			name:   EnvSupabaseProvider,
			policy: defaultsTo,
			unset:  "the provider namespace is `" + DefaultSupabaseProvider + "` — a subject still resolves against exactly one provider",
		},
		{
			// A PERMISSIVE zero: measured, a blank here left the role check off for a
			// deployment that wrote one down, and an `anon` session authenticated.
			name:   EnvSupabaseRequireRole,
			policy: refuseBlank,
			unset:  "no role is required — an `anon` session authenticates",
		},
		{
			// NOT a permissive zero — the zero is the STRICTEST leeway there is. It
			// refuses because a blank line is a typo all the same, and the refusal's
			// sentence ("read as unset") is true for it. It shares a policy with its
			// sibling rather than a reader.
			name:   EnvSupabaseLeeway,
			policy: refuseBlank,
			unset:  "there is no clock-skew allowance at all",
		},
		{
			name:   EnvSupabaseMaxAge,
			policy: refuseBlank,
			unset:  "`iat` is unbounded — a session minted hours ago still authenticates",
		},
	},
}

var proxyEnv = ledger{
	backend: "trusted-header",
	settings: []setting{
		{
			// ⚠ `refusedByConstructor` WOULD ALSO HAVE BEEN DEFENSIBLE HERE, AND SAYING WHY
			// IT WAS NOT TAKEN IS THE POINT RATHER THAN LEAVING THE CHOICE TO BE RE-ARGUED.
			// Unset does not turn a check off: it makes `NewTrustedHeader` refuse by name,
			// with `ErrTrustedHeaderNotDeclared` — which is the `refusedByConstructor`
			// shape, and the cost of not using it is that `FRONTED=""` and `FRONTED="  "`
			// land on different refusals. `refuseBlank` is taken anyway because
			// `ErrTrustedHeaderNotDeclared` does not NAME this variable, and this is the one
			// setting whose whole purpose is to be a sentence an operator had to write; an
			// operator who wrote it as whitespace should be told THAT, not told the
			// deployment was never declared. The refusal for `treu` makes the same trade for
			// the same reason, one function over.
			name:   EnvProxyFronted,
			policy: refuseBlank,
			unset:  "this deployment is not declared proxy-fronted, so the backend refuses to exist at all",
		},
		{
			name:      EnvProxySubjectHeader,
			policy:    refusedByConstructor,
			unset:     "there is no header to read an identity from",
			refusedBy: ErrTrustedHeaderNoSubjectHeader,
		},
		{
			// 🔴 `keepWhitespace`, AND `refuseBlank` ON TOP OF IT — THE TWO ARE NOT IN
			// TENSION AND THE PAIR IS WHAT CLOSES A MEASURED BYPASS. Interior and edge
			// whitespace may be part of a secret, so the value is not trimmed. A secret
			// with no CONTENT in it is not a secret, and `NewTrustedHeader`'s floor
			// cannot say so because it is a LENGTH test: measured at `70636bd` with the
			// backend armed by `RequireClientCert`, 2 spaces and 31 spaces were refused by
			// the floor while 32 spaces and 40 spaces BUILT and were accepted as a live
			// shared secret — so a caller sending the same run of spaces plus a subject
			// header authenticated as any user in the control plane. ⚠ "No content" is
			// wider than "whitespace" and this comment said the narrower word for a
			// round: 32 ZERO-WIDTH runes BUILT at `02fad01` with the same consequence.
			// `reducesToNothing` holds the predicate and the spelling still open.
			name:           EnvProxySecret,
			policy:         refuseBlank,
			unset:          "the shared-secret rung is gone — whatever else is armed authenticates alone",
			keepWhitespace: true,
		},
		{
			// A PERMISSIVE zero, and the one found by reading a fix's own comment against
			// the code rather than by the audit that named its siblings: measured, a blank
			// path beside `REQUIRE_CLIENT_CERT=yes` built a backend with a ZERO-length
			// secret, and a request carrying the certificate and NO shared-secret header
			// authenticated — defence in depth silently reduced to one rung.
			name:   EnvProxySecretFile,
			policy: refuseBlank,
			unset:  "the shared-secret rung is gone — whatever else is armed authenticates alone",
		},
		{
			name:   EnvProxySecretHeader,
			policy: defaultsTo,
			unset:  "the secret is read from `" + DefaultProxySecretHeader + "` — the same check, at the documented header",
		},
		{
			name:   EnvProxyRequireClientCert,
			policy: refuseBlank,
			unset:  "mTLS is not enforced — a plaintext request carrying the shared secret authenticates",
		},
		{
			// 🔴 THE LIST. Its zero is permissive AND its degenerate spellings are not
			// whitespace — see `reducesToNothing` for the measurement.
			name:   EnvProxyPeers,
			policy: refuseBlank,
			unset:  "there is no peer allowlist — any address may present the identity header",
			fields: peerFields,
		},
		{
			name:      EnvProxyProvider,
			policy:    refusedByConstructor,
			unset:     "there is no provider namespace, so a subject could not be resolved to a user",
			refusedBy: ErrTrustedHeaderNoProvider,
		},
	},
}

// ledgers is every ledger, in the order refusals are collected and therefore reported.
func ledgers() []ledger { return []ledger{supabaseEnv, proxyEnv} }

// retiredEnv names variables this build once read and no longer does, with what to do
// instead. Setting one is a REFUSAL.
//
// 🔴 A DELETED SETTING MUST BE LOUD, NOT IGNORED, AND THAT IS THE SAME RULE THE LEDGERS
// SERVE ONE DIRECTION OVER. They ask "did anybody touch any of these" so a half-configured
// backend cannot come up quietly; a name DROPPED from a ledger inverts that — an operator's
// manifest still carries it, nothing counts it, and the pod comes up healthy having
// silently discarded a line the operator wrote. These two named the LEGACY symmetric JWT
// secret, which this build no longer verifies with at all; an operator migrating a project
// is exactly who still has one set.
//
// ⚠ THEY ARE NOT IN A LIVE LEDGER AND MUST NEVER BE. A name here can only REFUSE — it
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

// ErrBlankSetting is the refusal a setting earns by being PRESENT and holding a value
// that reduces to nothing. `setting.resolve` is the only place that returns it.
var ErrBlankSetting = errors.New("identity: a setting is present and holds a value that reduces to nothing")

// ErrUndeclaredSetting is the FAIL-CLOSED default for a reader that asks for a variable no
// ledger declares a policy for.
//
// ⚠ IT IS NOT A GUARD AND IS NOT COUNTED AS COVERAGE. While
// `TestTheEnvironmentLedgersNameEveryVariableEachBackendReads` is green no constructor can
// reach it through `FromEnvironment`, because that test pins each constructor's `Env*`
// references against its own ledger by AST. What it is instead is the answer to "what
// happens if it ever is reached", and the answer is a refusal rather than the empty string
// — which would be this whole file's defect resolved silently in the permissive direction.
// `TestAReaderCannotSilentlyGetAnUndeclaredSetting` reaches it directly.
//
// ⚠ AND THE ONE SHAPE THAT AST PIN CANNOT SEE, STATED RATHER THAN LEFT TO BE FOUND: a
// reader that spelled the variable as a STRING LITERAL (`r.get("CAIRN_…")`) rather than
// through its `Env*` constant. `envNamesReadBy` walks IDENTIFIERS, so a literal is invisible
// to it — and this refusal is then the only thing between that and a silent empty string.
var ErrUndeclaredSetting = errors.New("identity: a reader asked for a setting no ledger declares a policy for")

// touched reports whether an operator WROTE this variable down, and hands back what they
// wrote. Absent, and present-with-the-EMPTY-string, are both "not written".
//
// 🔴 ONE PREDICATE, THREE CALLERS, AND THAT IS THE POINT RATHER THAN TIDINESS.
// `refuseRetiredSettings` and the ledger sweep open-coded `present && value != ""`
// separately; they agreed, and nothing gated them agreeing.
//
// ⚠ THE EMPTY STRING IS DELIBERATELY OUTSIDE. A manifest that emits every variable with an
// empty default is a common shape, and refusing it would be a wider change than any defect
// measured here — so `X=""` is "not using this" everywhere in this file, and `X="  "` is
// not.
func touched(env map[string]string, name string) (string, bool) {
	value, present := env[name]
	return value, present && value != ""
}

// refuseRetiredSettings refuses a deployment that still sets a variable this build
// dropped. Sorted so the message is stable when more than one is set.
//
// 🔴 `touched`, NOT `TrimSpace(…) != ""`. Trimming before the test discards exactly the
// value the blank policy exists to stop being discarded: measured, `CAIRN_SUPABASE_JWT_SECRET`
// set to three spaces returned a nil error and a machine-token-only chain, so a line the
// operator wrote vanished with no log — the silent ignoring `retiredEnv`'s own comment
// refuses one paragraph up.
//
// ⚠ AND THE ARMED/UNARMED DISTINCTION IN `setting.resolve` HAS NO ANALOGUE HERE. That
// distinction asks whether the backend a value belongs to is armed by something else; a
// retired name belongs to no backend and can never arm one, so there is no case in which a
// value here is anything but a line to delete.
func refuseRetiredSettings(env map[string]string) error {
	names := make([]string, 0, len(retiredEnv))
	for name := range retiredEnv {
		if _, ok := touched(env, name); ok {
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

// blankFault is one refused line, carried rather than returned so that EVERY offending
// line reaches the operator in one message.
//
// 🔴 BATCHED, BECAUSE THREE BLANK SETTINGS USED TO COST THREE CRASH-LOOP CYCLES. The
// reader-level refusal this replaces returned on the first one it met, so an operator
// fixed one line, redeployed, and met the next. The ledger-level one already batched; the
// two disagreeing about that was itself a symptom of there being two.
type blankFault struct {
	// name is the variable, and it is always in the rendered message: an operator
	// reading an `os.Exit(78)` crash loop needs the line to edit.
	name string
	// raw is what they wrote, quoted, because whitespace is invisible otherwise.
	raw string
	// because is the consequence, in the operator's words.
	because string
}

// resolve applies ONE setting's declared policy.
//
// 🔴 THIS IS THE ONE READER. Every value a backend is built from comes through here, and
// `armed` is the only thing outside the setting's own declaration that changes the answer.
// (`refuseRetiredSettings` reaches the environment too, but it asks only whether a name was
// WRITTEN — through the same `touched` — and never takes a value from one.)
//
// 🔴 `armed` IS WHAT MAKES "MISCONFIGURATION OR UNSET OPTIONAL?" ANSWERABLE, AND IT IS
// ANSWERED PER SETTING RATHER THAN GLOBALLY. When NOTHING else in the ledger holds a value
// the backend is silently OFF, and a blank line is a line into a backend that is not
// running — true for every policy, so every policy refuses there. When something else DID
// arm the ledger that sentence is false, and the answer is the setting's own: measured, a
// version that refused unconditionally crash-looped a COMPLETE trusted-header deployment
// that carried one blank optional beside it, with a message that was false for that input.
func (s setting) resolve(env map[string]string, armed bool) (string, *blankFault) {
	raw, ok := touched(env, s.name)
	if !ok {
		return "", nil
	}
	if !s.reducesToNothing(raw) {
		return s.value(raw), nil
	}
	if !armed {
		return "", &blankFault{name: s.name, raw: raw,
			because: "nothing else in the " + ledgerOf(s.name) + " ledger holds a value, so that backend would be silently OFF while the manifest says it is on"}
	}
	switch s.policy {
	case defaultsTo, refusedByConstructor:
		// Accepted as UNSET. `s.unset` says what that means, and for
		// `refusedByConstructor` the rung in `s.refusedBy` is where it lands.
		return "", nil
	case refuseBlank:
		return "", &blankFault{name: s.name, raw: raw,
			because: "it is read as UNSET, and unset means: " + s.unset}
	default:
		// 🔴 FAIL CLOSED ON `policyUndeclared`. The zero value of `blankPolicy` is not a
		// decision, and a setting that forgot to make one must not inherit the permissive
		// answer. Reachable: `TestAnUndeclaredPolicyRefusesRatherThanDefaulting`.
		return "", &blankFault{name: s.name, raw: raw,
			because: "this setting declares no blank policy (" + s.policy.String() + "), so nothing here can say whether that is a misconfiguration"}
	}
}

// ledgerOf names the backend a variable belongs to, for the refusal above.
//
// ⚠ THE FALLBACK IS FOR A SETTING NO LEDGER HOLDS, WHICH IS A TEST FIXTURE RATHER THAN A
// DEPLOYMENT. `FromEnvironment` only ever resolves settings it took OUT of a ledger, so the
// loop always finds one there; a synthetic `setting` constructed in a test does not, and a
// generic word is a better answer than an empty one.
func ledgerOf(name string) string {
	for _, l := range ledgers() {
		for _, s := range l.settings {
			if s.name == name {
				return l.backend
			}
		}
	}
	return "identity"
}

// resolveLedger resolves every setting in one ledger, and reports whether anything armed
// the backend.
//
// ⚠ TWO PASSES, AND THE ORDER IS LOAD-BEARING. `armed` must be known before any policy is
// applied, because it is what decides whether "the backend would be silently off" is a
// true sentence — and a single pass would answer it differently depending on the order the
// ledger happens to list its settings in.
func resolveLedger(env map[string]string, l ledger) (map[string]string, bool, []blankFault) {
	armed := false
	for _, s := range l.settings {
		if raw, ok := touched(env, s.name); ok && !s.reducesToNothing(raw) {
			armed = true
		}
	}
	values := make(map[string]string, len(l.settings))
	var faults []blankFault
	for _, s := range l.settings {
		value, fault := s.resolve(env, armed)
		values[s.name] = value
		if fault != nil {
			faults = append(faults, *fault)
		}
	}
	return values, armed, faults
}

// refuseBlanks renders every refused line as ONE error.
//
// ⚠ IT DOES NOT SORT, AND THAT IS A DELETION RATHER THAN AN OVERSIGHT. `refuseRetiredSettings`
// sorts because it walks a MAP and would otherwise print a different message each restart;
// this walks the ledgers' own SLICES, in declaration order, so the message is already
// deterministic — and declaration order groups the lines by backend, which is more use to an
// operator than alphabetical. Measured: with the `sort.Slice` call deleted, the WHOLE
// package suite stayed green — an unpinnable line, which is what a sort over an
// already-ordered slice is. `TestEveryBlankSettingIsNamedInOneRefusal`'s repeat loop is what
// pins the determinism the sort was standing in for.
func refuseBlanks(faults []blankFault) error {
	if len(faults) == 0 {
		return nil
	}
	parts := make([]string, 0, len(faults))
	for _, f := range faults {
		parts = append(parts, fmt.Sprintf("%s=%q — %s", f.name, f.raw, f.because))
	}
	return fmt.Errorf("%w: %s. Give each a value or delete the line: the configuration written here is not the one this pod would run",
		ErrBlankSetting, strings.Join(parts, "; "))
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
// backend whose key set the caller must refresh, or nil when nothing ARMED the Supabase
// ledger — it is never a backend with nothing to refresh, because rung 3 of
// `NewSupabaseJWT` refuses one, and never nil while a Supabase variable holds a real value,
// because such a value is exactly what arms the ledger.
//
// 🔴 `sessions` IS THE AUTHORITY THE *SESSION* BACKENDS RESOLVE AGAINST, AND nil MEANS
// "THE SAME ONE THE MACHINE TOKEN USES", WHICH IS EXACTLY TODAY'S WIRING. It exists
// because the two kinds of principal come from two different places: a machine token is a
// row in the file `authority` projects, while a Supabase or proxy session is a USER that
// only a journal-backed `control.Store` can hold. Until this parameter existed the
// session backends had no choice but to resolve against the token-file projection, which
// mints one synthetic user at provider `cairn-token-file` and grants only project
// subjects — so a Supabase deployment's `UserByProviderSubject` could never match, and a
// trusted-header deployment aimed at that one pair resolved to an EMPTY `Authorization`.
// Both backends were inert in every deployment that could exist.
//
// 🔴 AND A `sessions` NOBODY READS IS A REFUSAL, NOT A NO-OP. See
// `ErrSessionAuthorityUnread`: a journal configured with no session backend to resolve
// against it authorises nobody through it, and that is the "came up healthy and answers
// nothing" shape every ledger in this file exists to refuse.
//
// 🔴 AND SO IS THE MIRROR — A SESSION BACKEND WITH NO `sessions` — WHICH IS THE
// DIRECTION THE ORIGINAL DEFECT WAS ACTUALLY MEASURED IN. See
// `ErrSessionBackendWithoutAuthority`. The first draft of this parameter refused only
// the first direction and silently fell back to `authority` for the second, which meant
// the failure this whole slice exists to close — a pod that starts, authenticates a
// session and hands it ZERO readable scopes — stayed configurable in production while a
// test asserted it was gone. A nil `sessions` is now legal in exactly one configuration:
// the one that arms no session backend at all, which is today's wiring.
func FromEnvironment(env map[string]string, authority interface {
	TokenAuthority
	ModelSource
}, sessions ModelSource) (Chain, *SupabaseJWT, error) {
	// 🔴 BEFORE EVERYTHING, AND UNCONDITIONALLY. A retired name arms nothing, so checking
	// it inside a backend's own branch would only fire for deployments that had ALSO
	// set a live variable — which is precisely the deployment that gets a loud refusal
	// anyway. The silent case is the one where a retired name is all that is set.
	if err := refuseRetiredSettings(env); err != nil {
		return nil, nil, err
	}

	// 🔴 EVERY SETTING IS RESOLVED BEFORE ANY BACKEND IS BUILT, AND THE REFUSALS ARE ONE
	// MESSAGE. Resolving inside the constructors would put the policy behind the "is this
	// ledger armed" branch — so a blank in a ledger nothing armed would never reach a
	// reader at all, which is the hole the ledger sweep existed to patch and the reason
	// there were two blank predicates to disagree.
	supabaseValues, supabaseArmed, faults := resolveLedger(env, supabaseEnv)
	proxyValues, proxyArmed, proxyFaults := resolveLedger(env, proxyEnv)
	if err := refuseBlanks(append(faults, proxyFaults...)); err != nil {
		return nil, nil, err
	}

	// 🔴 BEFORE THE CONSTRUCTORS, BECAUSE THE FALLBACK THIS REPLACES RAN INSIDE THEM. An
	// armed session backend with no session authority used to be built against
	// `authority` — the token-file projection — and that is not a degraded mode, it is the
	// defect: the backend authenticates, `Valid()` is true, and the authorization is
	// EMPTY. Asking the ARMED flags rather than "did a backend get built" is what lets the
	// refusal happen before a backend is constructed against the wrong authority at all,
	// so there is no branch left that can reach `authority` from here.
	//
	// ⚠ IT TAKES PRECEDENCE OVER EVERY CONSTRUCTOR ERROR, AND THAT COST IS WIDER THAN AN
	// EARLIER DRAFT OF THIS COMMENT CLAIMED. That draft said the cost was only "a
	// deployment that both half-configures a ledger past the blank sweep — an unparseable
	// duration, a too-short secret — and sets no journal", because "the blank sweep (the
	// half-configuration case that actually occurs) still runs first". That is FALSE, and
	// the error is in what the blank sweep sees: it refuses a setting written BLANK, not
	// one left ABSENT. The ordinary half-configuration is an absent companion —
	// `CAIRN_SUPABASE_JWKS_URL` set, `CAIRN_SUPABASE_ISSUER` never written — which arms
	// the ledger, produces no blank fault, and lands HERE. Measured on this tree: with a
	// nil `sessions`, all 15 ledger variables set alone reach this refusal and 0 reach
	// their own backend's; with a session authority, 0 reach this one and 15 reach their
	// own. So a deployment with no journal gets this message for ANY Supabase or
	// trusted-header misconfiguration, specific or not.
	//
	// It stays first anyway, because the precedence is what deletes the fallback rather
	// than documenting it: this refusal is asked of the ARMED FLAGS, so no branch is left
	// that can construct a backend against `authority` at all. The generic message names
	// the one variable that unblocks every case behind it, and the specific one is one
	// restart away. ⚠ `internal/identity`'s own tests must therefore supply a session
	// authority whenever they arm a ledger, or they observe this sentinel and nothing
	// else — see `TestTheEnvironmentLedgersNameEveryVariableEachBackendReads`, which was
	// silently emptied by exactly that and now asserts the refusal's PROVENANCE.
	// `ErrSessionAuthorityUnread`, the mirror, stays AFTER the constructors for the reason
	// stated there: it has no such conflict, because the configuration it refuses arms no
	// ledger.
	if (supabaseArmed || proxyArmed) && sessions == nil {
		return nil, nil, ErrSessionBackendWithoutAuthority
	}

	machine, err := NewMachineToken(authority)
	if err != nil {
		return nil, nil, err
	}

	// Both session backends resolve against the SAME authority, and it is the one the
	// caller supplied — one of them reading the journal while the other read the
	// token-file projection would mean a user who can sign in through the proxy and not
	// through the IdP, with no error anywhere to say why. The guard above is what makes
	// `sessions` non-nil on every path that reaches these two lines.
	var supabase *SupabaseJWT
	if supabaseArmed {
		supabase, err = supabaseFromEnv(supabaseValues, sessions)
		if err != nil {
			return nil, nil, err
		}
	}

	var trusted *TrustedHeader
	if proxyArmed {
		trusted, err = trustedHeaderFromEnv(proxyValues, sessions)
		if err != nil {
			return nil, nil, err
		}
	}

	// 🔴 AFTER THE CONSTRUCTORS, SO THE QUESTION IS "DID A SESSION BACKEND GET BUILT"
	// RATHER THAN "DID A LEDGER LOOK ARMED". The two differ on the configuration that
	// arms a ledger and then fails its own construction: that path already refuses above,
	// and asking the armed flags here instead would make this refusal shadow the specific
	// one an operator needs.
	if sessions != nil && supabase == nil && trusted == nil {
		return nil, nil, ErrSessionAuthorityUnread
	}

	// 🔴 `nil` FOR THE COOKIE BACKEND, AND IT IS A REFUSAL RATHER THAN A GAP. This
	// builder serves `cmd/cairn-server`, which is an API: it mints and resolves bearer
	// tokens and has no sign-in flow, no form, no page and no way to SET a cookie. A
	// pod that RESOLVED a session cookie it could never issue would be honouring a
	// credential minted by a different binary against a session table it does not own —
	// and it would do so on an endpoint whose whole contract is replayed byte-for-byte
	// by `tests/conformance/`. The browser session lives in `cmd/cairn-ui`, which wires
	// its own chain through `ui.AuthBackends` for exactly this reason. ⚠ There is
	// therefore no `CAIRN_*` variable for it here and none in either ledger; if that
	// ever changes, the ledgers are where it has to be declared.
	chain, err := Backends(machine, supabase, nil, trusted)
	if err != nil {
		return nil, nil, err
	}
	return chain, supabase, nil
}

// ErrSessionAuthorityUnread refuses a session authority that no backend resolves against.
//
// 🔴 A CONFIGURATION THAT DOES NOTHING IS THE SAME HAZARD AS A HALF-CONFIGURED BACKEND,
// WHICH IS WHAT EVERY LEDGER IN THIS FILE REFUSES. An operator who points a deployment at
// a control journal has provisioned users into it and expects them to be able to sign in;
// with no Supabase and no trusted-header backend configured, nothing reads that journal,
// every session is refused by the machine-token backend alone, and the pod comes up
// healthy. The failure is indistinguishable from the journal being empty or the users
// being wrong, so the answer is a startup refusal rather than a log line.
//
// ⚠ IT NAMES NO ENVIRONMENT VARIABLE, DELIBERATELY. The variable that supplies the
// authority is read by the caller — this package is handed a `ModelSource`, not a path —
// so the text an operator needs ("unset X, or configure a session backend") can only be
// written by the caller. `cmd/cairn-server` matches this sentinel with `errors.Is` and
// says it, the same way it already does for `tokenfile.ErrStoreRootUnreadable`.
var ErrSessionAuthorityUnread = errors.New(
	"identity: a session authority was configured and no session backend resolves against it — " +
		"with neither the Supabase nor the trusted-header backend configured, nothing reads it, " +
		"every browser sign-in is refused, and the pod comes up looking healthy")

// ErrSessionBackendWithoutAuthority refuses a session backend with no authority to
// resolve against — the MIRROR of `ErrSessionAuthorityUnread`, and the direction the
// defect was actually measured in.
//
// 🔴 IT IS THE STRICTLY WORSE OF THE TWO, WHICH IS WHY IT IS A REFUSAL RATHER THAN A
// WARNING. `ErrSessionAuthorityUnread` describes a pod that refuses every sign-in, which
// is loud: nobody gets in and somebody says so. This one describes a pod that ACCEPTS the
// sign-in — the JWT verifies, the proxy header is trusted, `Identity.Valid()` is true,
// the audit line names a principal — and hands it an authorization over nothing, because
// the only authority available was the token-file projection, whose one synthetic user
// sits at provider `cairn-token-file` with no membership and no grant. Every read then
// answers exactly as if the scopes did not exist. Measured at `e11c3a7`: an armed backend
// with a nil session authority built a two-backend chain and returned a nil error.
//
// 🔴 IT NAMES THE VARIABLE WHERE `ErrSessionAuthorityUnread` DELIBERATELY DOES NOT, AND
// THE ASYMMETRY IS THE ADVICE, NOT AN INCONSISTENCY. That one has two possible remedies
// and this package cannot tell which the operator wants (configure a backend, or unset a
// path it has never been told the name of). This one has exactly one: supply the journal.
// So the name is written here, where the sentence that needs it is — and
// `cmd/cairn-server`'s `TestTheSentinelNamesTheVariableThisProgramReads` pins this string
// against that program's `EnvControlJournal` constant, so the two spellings cannot drift.
var ErrSessionBackendWithoutAuthority = errors.New(
	"identity: a session backend is configured and no session authority was supplied — " +
		"set $CAIRN_CONTROL_JOURNAL to the control journal this pod resolves sessions against. " +
		"Without it the Supabase and trusted-header backends resolve against the token-file " +
		"projection, which holds no user any identity provider can name, so a verified sign-in " +
		"AUTHENTICATES and then reads nothing: the pod comes up healthy and every scope is empty")

// SupabaseBackendFromEnvironment builds the SUPABASE BACKEND ALONE from the environment,
// reporting whether anything armed it.
//
// 🔴 IT EXISTS BECAUSE `FromEnvironment` BUILDS A CHAIN THE BROWSER SURFACE MAY NOT HAVE,
// AND THE ALTERNATIVE WAS A SECOND READER OF THIS LEDGER. `cmd/cairn-ui` must not call
// `FromEnvironment`: that builder arms the trusted-header backend when an operator declares
// the deployment proxy-fronted, and `internal/ui/auth.go` records why a publicly-reachable
// browser endpoint cannot carry that trade at any setting. Without this function the UI
// would have to read `CAIRN_SUPABASE_*` itself — a second spelling of the blank policy this
// file's whole history is about, wrong in the same direction at the same seven sites.
//
// 🔴 SO THE ORDER HERE IS `FromEnvironment`'s ORDER, NOT A CHEAPER ONE: retired names first
// and unconditionally, then the ledger resolved in full, then the blank refusal, then the
// constructor. A caller that skipped the retired sweep would silently ignore a manifest
// still carrying `CAIRN_SUPABASE_JWT_SECRET`, which is exactly the shape `retiredEnv`'s own
// comment exists against.
//
// ⚠ IT DOES NOT ASK `ErrSessionBackendWithoutAuthority`'s QUESTION, AND THE REASON IS THAT
// THE ANSWER IS ALREADY DECIDED FOR THIS CALLER. That refusal guards a pod whose session
// backends would otherwise resolve against the token-file projection; this function takes
// the authority as a parameter and the one caller hands it the same `control.Cache` the
// whole surface authorises from. A nil `authority` reaches `NewSupabaseJWT`'s rung 0
// (`ErrNoAuthority`), which is the fail-closed direction.
//
// ⚠ AND "ARMED" IS THE LEDGER'S OWN QUESTION, NOT "DID A BACKEND GET BUILT". `false` with a
// nil error means no `CAIRN_SUPABASE_*` variable holds a real value — the deployment has not
// asked for this backend. An armed ledger that fails its own construction returns the
// error, never `false`: a half-configured backend must not read as an absent one.
func SupabaseBackendFromEnvironment(env map[string]string, authority ModelSource) (*SupabaseJWT, bool, error) {
	if err := refuseRetiredSettings(env); err != nil {
		return nil, false, err
	}
	values, armed, faults := resolveLedger(env, supabaseEnv)
	if err := refuseBlanks(faults); err != nil {
		return nil, false, err
	}
	if !armed {
		return nil, false, nil
	}
	backend, err := supabaseFromEnv(values, authority)
	if err != nil {
		return nil, false, err
	}
	return backend, true, nil
}

// Issuer is the `iss` this backend requires.
//
// 🔴 IT IS EXPOSED SO THE SIGN-IN FLOW CAN DERIVE ITS ENDPOINTS FROM THE VERIFIER RATHER
// THAN FROM A SECOND VARIABLE. For Supabase the issuer IS the GoTrue base URL —
// `https://<project-ref>.supabase.co/auth/v1` — so `/authorize` and `/token` hang off the
// same string the signature check is already pinned to. A separate
// `CAIRN_SUPABASE_AUTH_URL` would be a second place the project can be named, and the
// failure of two places is a deployment that verifies tokens from one project and starts
// sign-ins at another: every sign-in would complete at the provider and be refused here,
// with nothing naming the disagreement.
func (s *SupabaseJWT) Issuer() string { return s.verify.Issuer }

// reader hands a constructor the values `resolveLedger` already produced, and remembers
// the first fault.
//
// ⚠ THE CONSTRUCTORS NO LONGER RECEIVE THE ENVIRONMENT AT ALL, AND THAT IS THE POINT OF
// THE SIGNATURE CHANGE. A bare `strings.TrimSpace(env[…])` inside a constructor was the
// fourth of the six answers to "is this setting set?", at seven sites; with `env` out of
// scope there it is not a thing the code can express. That is SCOPE, not a type —
// `resolved`-as-a-named-map would not have been a compile error, because Go assigns freely
// between a named map type and its unnamed underlying one.
//
// ⚠ AND IT REMEMBERS THE FIRST FAULT RATHER THAN BATCHING, WHICH IS NARROWER THAN THE
// BLANK REFUSAL ABOVE AND DELIBERATELY SO. Every blank line in the whole environment is
// named in ONE message, because `resolveLedger` runs over both ledgers before any
// constructor does. A PARSE failure (`10min` is not a duration, `treu` is not a boolean)
// stops at the first: a constructor that carried on past one would be building a backend
// out of values it has already been told are wrong.
type reader struct {
	values map[string]string
	err    error
}

// get returns one resolved value. A name no ledger declares is a FAULT rather than the
// empty string — see `ErrUndeclaredSetting`.
func (r *reader) get(name string) string {
	value, ok := r.values[name]
	if !ok {
		if r.err == nil {
			r.err = fmt.Errorf("%w: %s", ErrUndeclaredSetting, name)
		}
		return ""
	}
	return value
}

// list splits a resolved value with the SAME function the setting's blank test used.
//
// ⚠ THE TRAILING FAULT IS AN INVARIANT GUARD, LABELLED AS ONE. A setting read as a list
// that declares no split cannot exist while the gate is green — the only caller is
// `EnvProxyPeers`, which declares `peerFields` — and the point of the arm is that the
// answer would be a refusal rather than an EMPTY allowlist, which is the exact hazard
// `reducesToNothing`'s list half closes one level up.
func (r *reader) list(name string) []string {
	raw := r.get(name)
	if raw == "" {
		return nil
	}
	for _, l := range ledgers() {
		for _, s := range l.settings {
			if s.name == name && s.fields != nil {
				return s.fields(raw)
			}
		}
	}
	if r.err == nil {
		r.err = fmt.Errorf("%w: %s is read as a list and declares no split", ErrUndeclaredSetting, name)
	}
	return nil
}

func (r *reader) boolean(name string) bool {
	value, err := parseBool(name, r.get(name))
	if err != nil && r.err == nil {
		r.err = err
	}
	return value
}

func (r *reader) duration(name string) time.Duration {
	value, err := parseDuration(name, r.get(name))
	if err != nil && r.err == nil {
		r.err = err
	}
	return value
}

func (r *reader) secret(inlineName, fileName string) []byte {
	value, err := secretFrom(inlineName, r.get(inlineName), fileName, r.get(fileName))
	if err != nil && r.err == nil {
		r.err = err
	}
	return value
}

// parseBool reads a boolean setting from a value the policy above already resolved.
//
// 🔴 AN UNRECOGNISED VALUE IS AN ERROR, NEVER `false`. `CAIRN_TRUSTED_HEADER_PROXY_FRONTED=treu`
// must not silently mean "not proxy-fronted": an operator who typed it believes the
// backend is armed, and a setting that reads a typo as its own default is how a
// deployment ends up in a state nobody chose. `strconv.ParseBool` would accept `t`/`T`
// and reject `yes`, which is the opposite of what an operator writing YAML types, so the
// accepted spellings are enumerated below.
//
// ⚠ THE ACCEPTED SET IS `1 t true y yes on` AND `0 f false n no off`, CASE-INSENSITIVELY,
// PLUS THE EMPTY STRING, WHICH IS WHAT "NOT SET" RESOLVES TO. A WHITESPACE-ONLY value is
// NOT in that set and never arrives here: `EnvProxyFronted` and `EnvProxyRequireClientCert`
// both declare `refuseBlank`, because "reads as its own default" was exactly as true of
// `"  "` as of `treu` while only one of the two was refused.
func parseBool(name, raw string) (bool, error) {
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

// parseDuration reads a Go duration setting from a resolved value. The empty string —
// "not set" — is the zero.
//
// ⚠ ONE PARSER FOR BOTH DURATIONS, BECAUSE TWO WOULD BE TWO CHANCES TO GET THE SAME PARSE
// WRONG — the one-rule-one-place ruling this file now applies to the blank test as well.
// An unparseable value is an error and never the zero: the operator who typed `10min`
// believes a bound is armed, and `time.ParseDuration` refuses that spelling. A
// whitespace-only value is the same claim in the spelling `ParseDuration` never sees, and
// it is refused by the declared policy before it gets here.
func parseDuration(name, raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a duration (%v)", name, raw, err)
	}
	return d, nil
}

// secretFrom reads a secret from an inline variable or from a file. Both values are
// already resolved: `EnvProxySecret` keeps its whitespace and `EnvProxySecretFile` does
// not, and neither can be a value that reduces to nothing.
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
// alone: it may be part of the secret. That is also why `EnvProxySecret` declares
// `keepWhitespace` — and why it declares `refuseBlank` beside it, because a secret with no
// CONTENT in it is not a secret and the length floor is a LENGTH test, not a content one.
// `reducesToNothing` is what "content" means here; it is wider than whitespace.
//
// 🔴 AND A FILE THAT YIELDS ZERO BYTES IS A REFUSAL HERE, BECAUSE THE CONSTRUCTOR CANNOT
// TELL IT FROM "NO SECRET CONFIGURED" AND BLAMES THE WRONG SETTING. A file holding only
// the newline an editor added strips to nothing, returns a zero-length slice, and reaches
// `NewTrustedHeader`'s `len(cfg.Secret) == 0` rung — so the operator is told there is no
// source check at all and to configure the very secret they did configure, in a crash
// loop. The MISSING-file case was already closed for exactly this reason; this is the same
// mis-blame one step further in, where the file exists and is empty.
func secretFrom(inlineName, direct, fileName, path string) ([]byte, error) {
	if direct != "" && path != "" {
		return nil, fmt.Errorf(
			"%s and %s are both set: one secret, one source. Which is live would depend on a precedence rule nobody reads, and a rotation that updated the other would appear to work",
			inlineName, fileName)
	}
	if direct != "" {
		return []byte(direct), nil
	}
	if path == "" {
		return nil, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fileName, err)
	}
	secret := strings.TrimRight(string(body), "\r\n")
	if secret == "" {
		return nil, fmt.Errorf(
			"%s: %s is empty, or holds only the newline an editor added. That is not the same as no secret "+
				"configured: read as one, the refusal you would get names the source check rather than this file, "+
				"and tells you to configure the secret you already did",
			fileName, path)
	}
	return []byte(secret), nil
}

// supabaseFromEnv builds the Supabase backend from values the policy has already resolved.
//
// ⚠ EVERY `Env*` NAME IT REFERENCES MUST BE IN `supabaseEnv`, AND THE REVERSE, PINNED BY
// AST — see `TestTheEnvironmentLedgersNameEveryVariableEachBackendReads`. That is also
// what keeps `reader.get` from ever reaching `ErrUndeclaredSetting` here.
func supabaseFromEnv(values map[string]string, authority ModelSource) (*SupabaseJWT, error) {
	r := &reader{values: values}

	var keys *KeySet
	if url := r.get(EnvSupabaseJWKSURL); url != "" {
		var err error
		keys, err = NewKeySet(JWKSOptions{URL: url})
		if err != nil {
			return nil, err
		}
	}

	// The documented default. `EnvSupabaseAudience` declares `defaultsTo`, so a blank
	// value arrives here as the empty string and lands on exactly this line.
	audience := r.get(EnvSupabaseAudience)
	if audience == "" {
		audience = DefaultSupabaseAudience
	}

	cfg := SupabaseConfig{
		Authority:   authority,
		Keys:        keys,
		Issuer:      r.get(EnvSupabaseIssuer),
		Audience:    audience,
		Provider:    r.get(EnvSupabaseProvider),
		RequireRole: r.get(EnvSupabaseRequireRole),
		Leeway:      r.duration(EnvSupabaseLeeway),
		MaxAge:      r.duration(EnvSupabaseMaxAge),
	}
	if r.err != nil {
		return nil, r.err
	}
	return NewSupabaseJWT(cfg)
}

// trustedHeaderFromEnv builds the trusted-header backend from resolved values. Same AST
// ledger claim as `supabaseFromEnv`.
func trustedHeaderFromEnv(values map[string]string, authority ModelSource) (*TrustedHeader, error) {
	r := &reader{values: values}
	cfg := TrustedHeaderConfig{
		ProxyFronted:      r.boolean(EnvProxyFronted),
		SubjectHeader:     r.get(EnvProxySubjectHeader),
		SecretHeader:      r.get(EnvProxySecretHeader),
		Secret:            r.secret(EnvProxySecret, EnvProxySecretFile),
		RequireClientCert: r.boolean(EnvProxyRequireClientCert),
		ProxyPeers:        r.list(EnvProxyPeers),
		Provider:          r.get(EnvProxyProvider),
		Authority:         authority,
	}
	if r.err != nil {
		return nil, r.err
	}
	return NewTrustedHeader(cfg)
}
