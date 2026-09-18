# `internal/identity` — P4: who a request is, behind one interface

This is **P4** of `claudedocs/plan-cairn-control-plane.md` §E: identity as an interface
with three backends, all resolving to one `control.Principal`, with nothing downstream
branching on which produced it.

🔴 **IT DECIDES *WHO*, NEVER *WHAT*.** Nothing in this package narrows anything. A backend
answers "who is this", and the `control.Authorization` it carries is `control.Resolve`'s
answer, computed from the same model read and carried rather than recomputed. Adding a
scope check here would be the second implementation of visibility that
`internal/control/README.md` exists to forbid.

## The interface, and the two places it deliberately differs from the plan's sketch

The plan sketches:

```go
type Authenticator interface { Authenticate(*http.Request) (Principal, error) }
```

What shipped:

```go
type Identity struct {
    Principal   control.Principal
    Auth        control.Authorization
    Fingerprint string
}
type Authenticator interface { Authenticate(r *http.Request) (Identity, error) }
```

**Difference 1 — it returns the authority as well as the principal, in ONE value.**
`control.Principal`'s own comment forbids the sketch's shape: *"authentication and
authorization must come out of the SAME match, so no route can be authenticated against
one credential and authorised against another. `Authenticate` is the only constructor that
a server path may use, and it returns the Principal and the Authorization together."* An
interface returning the principal alone makes the server call `Resolve` afterwards — a
second read of the materialized model, which a refresh may have replaced between the two
calls. Returning one value closes that structurally rather than by discipline.

⚠ The authority field is `Auth`, not `Authorization`, and the reason is recorded beside
it: `tests/leakscan.py`'s credential rule matches `authorization` followed by twenty-plus
identifier characters, which a declaration of the shape `<the word> control.<the word>`
is. (Spelled with a placeholder here for the reason `claudedocs/handoff-cairn-control-plane.md`
records for the `dated-incident` rule: a note that instantiates the pattern it warns about
refuses its own file.) It
is a false positive, and it is fixed on this side because a credential gate's false
positives are the safe direction — the name it lands on is the one `api.request.auth`
already uses.

`Fingerprint` is the third field for a reason the sketch could not anticipate: once the
server no longer sees the bearer token, it can no longer derive the audit line's `token=`
value, so the backend that DID see it has to carry it.

**Difference 2 — the interface cannot live in `internal/control`, which the sketch's
placement implies.** That package's doc says it "deliberately stops at the library
boundary — nothing here imports `net/http`, opens a socket or knows a server exists".
`Authenticate(*http.Request)` is an `http.Request`. The interface lives here, one level
out, and `control` stays a library the CLI and the UI can link without a server.

## The three backends

| backend | credential | resolves by | fingerprint |
|---|---|---|---|
| `MachineToken` | a bearer token this pod minted | hashed lookup in `control.Cache` | `authz.TokenID`, 12 hex |
| `SupabaseJWT` | a Supabase session JWT | local signature check, then provider+subject → `control.User` | none (`-` in the audit line) |
| `TrustedHeader` | an upstream auth proxy's assertion | source check, then provider+subject → `control.User` | none |

`MachineToken` is a **move, not a rewrite**: every line was `internal/api`'s
`request.authenticate` before P4. P4's claim about the existing deployment is that it
behaves byte-identically, and rewriting the one path every deployed credential takes would
have made that claim about new code.

🔴 **A VERIFIED TOKEN FOR AN UNKNOWN USER IS A REFUSAL, NOT A SIGNUP.** The IdP vouches for
who somebody is; it says nothing about whether this instance has an account for them.
Just-in-time provisioning would make every read route a user-creation endpoint for anybody
with an account at the provider — which is self-serve signup, which is P6, and which brings
quotas, rate limits, abuse handling and deletion/export with it.
`TestAVerifiedTokenForAnUnknownUserIsRefusedRatherThanProvisioned` counts the users before
and after.

## 🔴 The trusted-header backend is a foot-gun built to refuse to fire

State the failure plainly, because everything else is shaped by it: **if the pod is
reachable directly, anyone who can open a socket to it can set the identity header and
become any user in this control plane.** Not "read one scope" — BE that user, at that
user's full authority, on every route, with the writes attributed to them. There is no
narrowing downstream that limits the blast radius, because downstream cannot tell this
principal from one that presented a credential. That is the design, and here it is the
hazard.

Four things stand against it:

1. **It is never the default.** `api.New` installs `MachineToken` and nothing else.
2. **It refuses to exist without an explicit proxy-fronted declaration** — a boolean whose
   only purpose is to be a sentence an operator had to write, deliberately NOT inferred
   from "a header name was configured".
3. **It refuses to exist without a source check**: a shared secret, or a verified TLS
   client certificate. A peer allowlist alone does **not** satisfy it.
4. **Every check runs on every request**, source before identity, and every failure is the
   same `control.ErrNoCredential` a wrong token gets.

### The construction ladder, and why the order is load-bearing

`TestEveryTrustedHeaderConstructionRefusalIsReachable` walks all six with a configuration
every rung above accepts, and asserts the **specific sentinel** rather than "an error
happened" — because a guard that dies because an earlier check rejected the input first is
a guard that has never run.

| rung | refusal | reached by |
|---|---|---|
| 0 | `ErrNoAuthority` | no control plane to resolve against |
| 1 | `ErrTrustedHeaderNotDeclared` | everything else perfect, `ProxyFronted` false |
| 2 | `ErrTrustedHeaderNoSubjectHeader` | declared, no header named |
| 3 | `ErrTrustedHeaderNoProvider` | declared, header named, no provider namespace |
| 4 | `ErrTrustedHeaderNoSourceCheck` | no secret and no client certificate — **and a peer allowlist does not count** |
| 5 | `ErrTrustedHeaderWeakSecret` | a secret below `MinProxySecretBytes` (32) |
| 6 | `ErrTrustedHeaderPeer` | a peer entry `netid.TrustedNetwork` refuses |

Rung 4 and rung 5 are two questions, not one: rung 4 asks whether the operator configured
any source check, rung 5 asks whether the one they configured is strong enough. That is
what makes rung 5 reachable at all.

🔴 **A PEER ALLOWLIST IS A SECOND LAYER, NOT A SOURCE CHECK.** The plan names "shared
secret or mTLS" for a reason: an address proves only that something occupying it sent the
request, and what can occupy an address in a cluster is a NetworkPolicy question, not an
authentication one. Accepting it alone would be exactly the "harmless because of where it
happens to be deployed" reasoning `internal/api`'s path-component guard refuses. The
battery carries `a-peer-allowlist-counts-as-a-source-check` because that is the realistic
wrong belief, not a textbook mutation.

### What an attacker reaching the pod directly gets

`TestAnAttackerReachingThePodDirectlyGetsNothing` runs a correctly-configured backend and
probes it, with a positive control first (a genuine proxied request resolves to the real
user, with real authority) so that no refusal below can pass vacuously. Nine request-time
rungs, each watched firing with its own message:

- the identity header and nothing else → the source check refuses **before the claimed
  identity is read at all**;
- a guessed secret → constant-time compare refuses;
- an appended secret (two values) → refused as duplicated, not matched on whichever copy
  is right;
- the right secret from the wrong peer → the allowlist refuses;
- an unparseable peer → refused;
- an appended subject → refused as duplicated;
- an unknown subject → refused, and no user is created;
- an empty or over-long subject → refused.

🔴 **AND THE ORDER IS PINNED AS A RELATIONSHIP.**
`TestTheSourceCheckRunsBeforeAnythingTheCallerClaimsAboutWho` sends a KNOWN subject and an
UNKNOWN one, both without the secret, and requires the two refusals to be **identical
strings**. Reading the subject first and checking the secret afterwards accepts the same
set of requests and is wrong about everything else: it would be an enumeration API for this
control plane's user list.

### The honest limit

🔴 **A SHARED SECRET IN A HEADER PROVES THE SENDER KNOWS THE SECRET, NOT THAT THEY ARE THE
PROXY.** Anything that can read the proxy's configuration, or capture one plaintext
request, can replay it. `netid`'s package doc makes the same point one layer down about the
client-IP header. `RequireClientCert` is the stronger rung and is the one to prefer where
mTLS is available.

## The JWT verifier, stdlib-only

**RS256 and ES256 — asymmetric only.** `crypto/rsa`, `crypto/ecdsa`, `crypto/ecdh` for
point validation, `encoding/json` + `math/big` for JWK parsing, `net/http` for the JWKS
fetch. No dependency was needed; `go.mod` still has no `require` block.

🔴 **HS256 SUPPORT WAS DELETED AND THE HS256 *REFUSAL* WAS STRENGTHENED, WHICH ARE TWO
CHANGES AND NOT ONE.** The first draft of this package also verified Supabase's LEGACY
symmetric option, from a secret an operator configured. That path is gone: no `AlgHS256`,
no `keyOct`, no `staticSecret`, no `resolverPair`, no `SupabaseConfig.Secret`, no
`CAIRN_SUPABASE_JWT_SECRET`. **Deleting the check along with the support would have been
the wrong simplification** — it is the whole of the algorithm-confusion attack — so
`symmetricAlg` refuses any `HS*` algorithm in `Verify`, ahead of the deployment's
allowlist, with its own sentinel `ErrTokenAlgSymmetric`.

🔴 **THE TOKEN'S HEADER SELECTS FROM THE CONFIGURED SET AND CAN NEVER WIDEN IT.** The
classic forgery is `alg: none`; the second classic one is `alg: HS256` against a deployment
that verifies with an RSA public key, where the public key — which the attacker has,
because it is public — becomes the HMAC secret. Both die on one rule, and the HMAC half is
now **unconditional rather than deployment-dependent**: one rule, one arm.

🔴 **BE HONEST ABOUT WHAT THE NEW GUARD BUYS, BECAUSE THE OVER-CLAIM IS THE TEMPTING
ONE.** It is the FIRST refusal an `HS*` token meets, not the only one. Measured by
deleting its call site and reading what answers instead — two refusals, both `ErrTokenAlg`,
both fail-closed:

| the deployment | what refuses with the guard gone |
|---|---|
| ordinary (JWKS only) | `accepts` — nothing puts `HS256` in `Algs` |
| one that explicitly accepts `HS256` | `KeySet.key`'s `algKeyType` lookup, two-valued, no entry |

`hashFor`'s lookup in `verifySignature` is a third backstop of the same shape that neither
configuration reaches, because the resolver refuses first. So what the guard buys is a
refusal with its **own name** — which a test can assert, and which makes a one-line
re-addition of a symmetric algorithm to `algKeyType` insufficient to reopen the hole.
`tests/control_mutants.py` carries `the-symmetric-algorithm-refusal-is-removed` for
exactly that: without the sentinel it would be an EQUIVALENT mutant, and reading it as one
is the mistake the row's `why` exists to prevent.

🔴 **AND THE TEST NO LONGER NEEDS TWO DEPLOYMENTS, WHICH IS THE OTHER THING THE DELETION
BOUGHT.** The old `TestTheAlgorithmConfusionForgeryIsREFUSED` had a JWKS-only arm that
**passed for the wrong reason** — `accepts` refused HS256 before any key resolved, so the
key/algorithm rule never executed, proven at the time by opening the hole on purpose and
watching that arm stay green. It needed a second, **mixed** arm (a JWKS *and* a legacy
symmetric secret) to reach the guard at all, and that configuration can no longer be
built. Today every arm reaches the guard, because the guard runs before the allowlist.
Six arms, each forging with the deployment's **own published key** as the HMAC secret,
each asserting `ErrTokenAlgSymmetric`: the RSA modulus, the EC public point, a deployment
that explicitly accepts `HS256`, `HS384`, `HS512`, and a lower-case re-spelling. Watched
to fail: with the `if symmetricAlg(alg)` block deleted, all six go red naming the
sentinel. The last three are there because the rule is a **prefix** rule, not a spelling —
a guard written `alg == "HS256"` passes the first three and fails those.

`TestNoConfiguredAlgorithmIsSymmetric` is the structural half and a different claim: no
entry in `algKeyType`, none in `hashFor`, and nothing `NewSupabaseJWT` configures, is a
shared-secret algorithm. It is what makes re-adding one LOUD rather than merely
ineffective.

Other rules worth naming:

- **`exp` is REQUIRED**, not merely checked-if-present. A token with no `exp` never expires.
- **`crit` is refused**, per RFC 7515 §4.1.11 — a verifier implementing no extensions must
  reject a token that names one.
- **NumericDate is parsed as an integer**, so `1e400` cannot become `+Inf` and compare as
  never-expiring.
- **`aud` decodes as a string OR an array**, and the configured audience must MATCH rather
  than the check being skipped when the claim is absent.
- **Leeway is bounded by `MaxLeeway` (2m) and refused rather than clamped** — a generous
  skew allowance is how a revoked session outlives its revocation.
- **An `oct` key in a published JWKS is dropped**, never parsed: a symmetric key in a
  public document is a secret everybody has. Belt to `symmetricAlg`'s braces now, rather
  than the only thing standing there.
- **A maximum token age is bounded but optional**, and a NEGATIVE one is refused at
  construction rather than read as zero — `checkClaims` tests `MaxAge > 0`, so zero means
  OFF and a negative value would silently disable a bound the operator configured.
- **A duplicate `kid` refuses the whole document**, because last-wins would make "which key
  verifies this token" depend on Go's randomised map order.
- **A token with no `kid` is refused when more than one key could verify it.** Trying every
  key until one works turns a rotation into permanent acceptance of the retired key.
- **RSA moduli below 2048 bits are dropped**; EC points are validated on the curve through
  `crypto/ecdh` before becoming a key.

## The IdP outage, measured by killing the dependency

`TestAnIdentityProviderOutageDoesNotStopAnAlreadyIssuedSession` is four claims:

1. **the positive control** — a fetch happened at all, so every zero below is a fact about
   the hot path rather than about a counter wired to nothing;
2. **zero network on the hot path** — 25 verifications, 0 further requests, reported as the
   pair;
3. **the promise** — the listener is CLOSED (not slowed), and an already-issued session
   still verifies;
4. **the outage is visible** — a refresh now fails, the serving key set is unchanged,
   `FetchedAt` does not move, and `Status()` says `failing=true`.

`TestAnAuthorityOutageDoesNotStopAnAlreadyIssuedSession` is the same claim about the
**other** dependency — the control plane behind `control.Cache` — because they fail in
different places and measuring one says nothing about the other.

⚠ **AND THE HONEST COST, INHERITED FROM `control.Cache` AND STATED IN THE SAME WORDS: A
ROTATED-OUT KEY KEEPS VERIFYING UNTIL THE NEXT REFRESH.** No arrangement makes that false
— a cache that asked the provider whether it was stale would be making exactly the call the
outage is supposed to survive. The lag is **bounded** by `DefaultJWKSInterval` (10m) and
**reportable** by `Status`.

⚠ **"REPORTABLE" IS THE WORD IT EARNS, NOT "REPORTED".** No deployed program prints
`KeySetStatus`, for the same reason nothing prints `control.Staleness`: the one place to
put it is `cmd/cairn-server`'s startup banner, which `tests/dualrun/harness.py` compares
between the two servers, so a Go-only field there moves a gate. **CLOSING CONDITION:** the
same one `internal/control/README.md` states — a render (a `doctor` section, a status
route, or a banner field declared in `wire.NORMALIZATIONS`) that a `tests/dualrun/` run
exits 0 with.

## Configuration: a partial configuration REFUSES to start

🔴 **THE TRIGGER IS "DID ANYBODY TOUCH ANY OF THESE", NOT "ARE THE REQUIRED FIELDS
PRESENT".** The second reads sensibly and is the dangerous one: an operator who set the
subject header and forgot the shared secret gets a pod that comes up, passes its health
check, and quietly authenticates nobody through a backend they believe is live. So
`supabaseEnv` and `proxyEnv` are ledgers, `TestTheEnvironmentLedgersNameEveryVariableEachBackendReads`
pins them against the exported constants **and** proves every one of them, set alone,
reaches a refusal.

⚠ **ONE SETTING THIS PACKAGE'S BACKENDS DEPEND ON IS NOT IN EITHER LEDGER, AND IT IS NOT AN
OVERSIGHT.** `CAIRN_CONTROL_JOURNAL` is read by `cmd/cairn-server`, which supplies the
`ModelSource` the session backends resolve against. It configures no backend, so the
ledgers' `armed` question ("is this BACKEND half-configured") has no answer for it, and the
gate over that machinery requires a ledger to carry at least two settings — putting it
there would mean inventing a second setting to satisfy a test. It still declares a blank
policy, and it is the `refuseBlank` one, for the reason every `refuseBlank` setting here
has: an operator who wrote the line meant this pod to read a journal, and a blank read as
"not set" discards it. The predicate is `identity.ValueReducesToNothing` — this file's own,
exported, so there is no second spelling of "reduces to nothing" — and the gate is
`TestABlankControlJournalIsRefusedRatherThanReadAsUnset` in `cmd/cairn-server`.

⚠ **AND THE CONSEQUENCE OF A BLANK IS A CRASH LOOP, NOT A SILENT ONE, WHICH IS A CHANGE
FROM WHAT THIS PARAGRAPH USED TO SAY.** It read "read as unset it silently resolves
sessions against an authority that holds no provider-named user" — true when the blank
policy was written, and no longer: a session backend with no session authority is
`ErrSessionBackendWithoutAuthority` and the pod refuses to start. The blank policy is kept
anyway, and is now a better error rather than the only one: the operator is told their
journal line reduces to nothing, instead of being told to set a variable they can see they
already set.

### 🔴 EVERY SETTING DECLARES ITS BLANK POLICY, AS DATA

**Round after round found one more setting whose blank or degenerate value silently
disabled the check it configures — each time in a place the previous round's prose said was
covered.** Seven measured instances over six settings by the time this pass ran; the last
two, `CAIRN_TRUSTED_HEADER_PEERS=","` and a 32-space inline secret, were live at `70636bd`
beside a comment saying the class was closed. The diagnosis was structural rather than a run
of bad luck:
*"is this setting set?"* had **six different answers in one file**, and which one a setting
got was decided by which reader it happened to be plumbed through. The organising principle
the comments claimed — "does this setting's zero turn a check off" — existed **nowhere in
the data**: the ledgers were `[]string`, so nothing linked a name to a policy.

So each ledger entry is a `setting` that carries its own policy, and **one** resolver
(`setting.resolve`) consults it:

| policy | a value that reduces to nothing means |
|---|---|
| `refuseBlank` | a MISCONFIGURATION — refuse, naming the variable and what unset would have meant |
| `defaultsTo` | genuinely "not set", and unset is a documented default that leaves the same check armed |
| `refusedByConstructor` | "not set", and a named construction rung already refuses that — `setting.refusedBy` names which |
| `policyUndeclared` | the ZERO VALUE, and never a decision — a refusal at runtime and a RED test |

⚠ **WHICH SETTING HAS WHICH POLICY IS NOT WRITTEN DOWN HERE, AND THAT IS THE POINT RATHER
THAN AN OMISSION.** A membership list in prose is precisely the hand-maintained ~50-line
enumeration this pass deleted, and it went wrong in every round that touched it. Read it off
the ledgers in `config.go`, where each entry states its own policy beside its own name and a
test asserts the reader obeys it. Nothing here restates a count either.

`TestEverySettingDeclaresItsBlankPolicyAndTheReaderObeysIt` is the gate, and it pins a
relationship rather than one side: every entry carries a policy, an `unset` sentence and —
for `refusedByConstructor` — the rung it lands on; **and** a real `FromEnvironment` obeys
each of those, over spellings derived from the setting's own declaration. Adding a setting
without deciding its policy is red there, not a silent default.

🔴 **"REDUCES TO NOTHING" IS ONE PREDICATE AND IT IS WIDER THAN WHITESPACE.** That is the
whole bug class: a guard can be SPELLED rather than STRUCTURAL. `treu` was refused while
`"  "` was accepted; then `"  "` was refused while `","` was accepted; then `","` was
refused while a run of **zero-width** runes was accepted. So `setting.reducesToNothing`
runs a CONTENT test for every setting — is there a rune in here that is graphic and is not
a space — *and*, where the setting declares a split, asks whether the value yields any
fields at all. Measured at `70636bd` beside a complete armed trusted-header backend:
`CAIRN_TRUSTED_HEADER_PEERS` set to `","`, `",,"`, `", ,"`, `" , "` or `"\t,\n"` **built
with zero peers**, so `Authenticate`'s `if len(t.peers) > 0` never ran and any address
could present the identity header — while `"  "` was refused.

🔴 **AND THE CONTENT TEST IS NOT A COMPLETENESS CLAIM — ONE SPELLING IS OPEN AND NAMED
RATHER THAN IMPLIED CLOSED.** `strings.TrimSpace` reads `unicode.IsSpace`, whose set holds
U+00A0 and every `Zs` separator and none of the `Cf` zero-width runes, which is why the
whitespace spelling of this hazard was closed a round before the invisible one. The content
test closes both. What it does **not** close is a rune that is graphic by CATEGORY and
still renders blank — U+2800 BRAILLE PATTERN BLANK, U+3164 HANGUL FILLER, a lone combining
mark: measured on this tree, `CAIRN_TRUSTED_HEADER_SECRET` set to 32 × U+2800 is still
accepted as a live 96-byte secret. Closing that needs a rendered-width judgement this
package has no source for; `setting.reducesToNothing` carries the note beside the code.

🔴 **AND THE INLINE SECRET IS THE MIRROR CASE: NOT TRIMMED, BUT NOT ALLOWED TO BE
NOTHING.** Interior and edge whitespace may legitimately be part of a secret, so
`CAIRN_TRUSTED_HEADER_SECRET` declares `keepWhitespace` and its resolved value is the raw
string. What stood behind that was `NewTrustedHeader`'s `len(cfg.Secret) < MinProxySecretBytes`
— a **LENGTH** test, not a content one. Measured at `70636bd` with the backend armed by
`REQUIRE_CLIENT_CERT`: 2 spaces refused, 31 spaces refused, **32 spaces BUILT** and accepted
as a live shared secret, 40 spaces built. A caller sending the same run of spaces plus a
subject header authenticated as any user here. The fix is the `refuseBlank` policy beside
`keepWhitespace`, **not** a trim — a trim would silently corrupt a padded secret.

🔴 **AND THE SAME SECRET, ONE SPELLING OVER, WAS LIVE FOR ANOTHER ROUND.** Measured at
`02fad01` with the backend armed by the **secret alone** — no client-certificate
requirement, so the shared secret was the only source check — `CAIRN_TRUSTED_HEADER_SECRET`
set to 32 × U+200B, 32 × U+2060 or 32 × U+FEFF each **BUILT** and held a 96-byte live
shared secret, and a caller presenting the same run of zero-width runes plus a subject
header authenticated as the user that subject resolves to. 32 × U+00A0 and 32 × U+0020 were
refused, which is what made it a spelling rather than a missing guard.
`TestAZeroWidthSecretCannotBecomeASourceCheckAStrangerCanClear` is the behavioural evidence,
and it asserts that a **stranger** cannot clear the rung rather than that the rung is on —
a probe that presents the fixture's own secret reports "enforced" for a backend anybody can
walk into. A value that mixes content with invisible runes stays a legitimate secret and
arrives **byte for byte**, which is the regression the widening risked.

Other rules:

- **an unrecognised boolean is an ERROR, never `false`** —
  `CAIRN_TRUSTED_HEADER_PROXY_FRONTED=treu` must not silently mean "not proxy-fronted";
- ⚠ **an ABSENT name and a name holding the EMPTY string are outside all of it.** The first
  is how a deployment says "not using this"; the second is the manifest shape that emits
  every variable with an empty default. `touched` draws that line once, for the retired
  ledger and the live ones alike;
- 🔴 **a blank in a ledger NOTHING ELSE ARMED is refused whatever its policy**, because
  there the backend really would be silently off and that sentence is true for every
  setting. Where something else armed the ledger the sentence is false — a version that
  refused unconditionally crash-looped a complete trusted-header deployment with
  `os.Exit(78)` — and the setting's own policy answers instead;
- 🔴 **every offending line is named in ONE refusal.** Three blank settings used to cost
  three crash-loop/redeploy cycles, because the reader-level refusal returned on the first
  one while the ledger-level one beside it batched;
- **a secret has exactly one source** — inline or a file, never both, because a precedence
  rule nobody reads makes a rotation that updated the wrong one appear to work;
- **prefer the `_FILE` form**: an environment variable is readable from
  `/proc/<pid>/environ`, inherited by every child, and printed by any `env` that reaches a
  log. The pod already mounts its bearer token as a file;
- 🔴 **a RETIRED setting is a refusal, not an ignored line.** `CAIRN_SUPABASE_JWT_SECRET`
  and `CAIRN_SUPABASE_JWT_SECRET_FILE` named the legacy symmetric secret and this build no
  longer reads them. Dropping a name from a ledger inverts the rule above: the arming scan
  only counts names that are *in* a ledger, so a dropped one is invisible to it by
  construction, and an operator whose manifest still carries it would get a pod that comes
  up healthy having silently discarded the line they wrote. `retiredEnv` refuses instead,
  naming the variable and what to use — and it is deliberately **not** in `supabaseEnv`,
  because a name there would also *arm* the backend it was dropped from. A ledger test
  pins that the two sets do not overlap and that each retired name actually reaches the
  refusal.

## 🔴 BOTH NEW BACKENDS WERE INERT UNTIL P5's FIRST SLICE, AND THAT STATE IS NOW A REFUSAL TO START

**This section is kept rather than deleted**, because it is the measurement that says what
the fix is for. What has changed is its *scope*: the inert state is no longer something an
operator can configure. A session backend armed with no control journal does not come up
inert — it does not come up at all.

### What changed

`cmd/cairn-server` reads `$CAIRN_CONTROL_JOURNAL`, opens a `control.FileStore` at that
path, materializes it into a `control.Cache` over that store, and passes it to
`identity.FromEnvironment` as the **session authority** — the third parameter, which the
Supabase and trusted-header backends resolve against. `cairn-server -create-user` writes a
user, a project, the owner membership and the scopes into that journal, in one batch. The
machine-token backend is untouched and still resolves against the token-file projection.

**The two directions are BOTH refusals, and the second one is the whole point.**

- **Set, with no session backend configured, is a REFUSAL to start**
  (`ErrSessionAuthorityUnread`): a journal nobody reads is the "came up healthy and answers
  nothing" shape one level up from the partial-configuration ledgers.
- 🔴 **A session backend with no session authority is also a REFUSAL to start**
  (`ErrSessionBackendWithoutAuthority`), and this is the direction the defect was actually
  measured in. The first version of this parameter refused only the direction above and
  fell back to the token-file projection for this one — so the failure the whole slice
  exists to close stayed configurable in production while a test asserted it was gone. It
  is the strictly worse of the two: the sign-in **succeeds**, `Valid()` is true, an audit
  line names a principal, and the authorization is empty. Measured at `e11c3a7`: an armed
  backend with a nil session authority returned a nil error and a two-backend chain. The
  sentinel names `$CAIRN_CONTROL_JOURNAL` because that is its one remedy; the gate is
  `TestASessionBackendWithNoSessionAuthorityRefusesToStart`, red at that commit on both
  backends.
- **Nothing set is exactly today's wiring**, and that is what the refusal above asks the
  ARMED flags rather than the parameter alone: no session backend and no session authority
  is a machine-token-only chain, unchanged. Pinned by
  `TestNoControlJournalMeansNoSessionAuthorityAtAll` and by that same test's last arm.

🔴 **THE PRECEDENCE HAS A COST, IT IS WIDER THAN THE FIRST DRAFT OF `config.go` SAID, AND
IT SILENTLY EMPTIED A PRE-EXISTING GATE IN THIS PACKAGE.** `ErrSessionBackendWithoutAuthority`
runs **before** the constructors, so it shadows every backend-specific refusal for a
deployment with no journal. The draft called that cost narrow — only a ledger
half-configured "past the blank sweep" — on the grounds that the blank sweep catches the
half-configuration that actually occurs. It does not: the sweep refuses a value written
**blank**, not one left **absent**, and an absent companion (`CAIRN_SUPABASE_JWKS_URL` set,
`CAIRN_SUPABASE_ISSUER` never written) is the ordinary shape. **Measured on this tree:**
with a nil session authority, **15 of 15** ledger variables set alone are refused by this
sentinel and **0** reach their own backend; with an authority supplied, **0** and **15**.
`TestTheEnvironmentLedgersNameEveryVariableEachBackendReads`'s "the ledger is load-bearing
rather than decorative" loop passed `nil` and therefore observed only the sentinel —
proven by breaking both constructors out of the call path, which left the old loop
**green** on all 15 and turns the repaired one **red** on all 15. It now supplies a
session authority *and* asserts the refusal is neither blanket sentinel, because supplying
one makes `ErrSessionAuthorityUnread` the replacement shadow. **Any test in this package
that ARMS a ledger must pass `newTestSessionAuthority(t)`**, and one that asserts a
specific refusal must say the refusal is the backend's own.
- **The gate** is `TestAnOperatorProvisionedSupabaseSessionAuthenticatesWithRealAuthority`
  and its trusted-header twin: a user created through `control.ProvisionUser`, a real
  journal file, a session verified through `FromEnvironment`'s own chain, and an assertion
  on the **content** of the `Authorization` — every verb on the provisioned scope, the name
  present in the projection the reader narrows by, and a scope outside it unreachable.

### The state that produced this section, which no `cmd/cairn-server` configuration now reaches

Read from the code, three facts that compose:

1. **`tokenfile.Source` was the only authority any binary wired.** Nothing constructed a
   `control.FileStore` outside its own tests — measured at `229c142` by enumerating every
   non-test `.go` file under `cmd/` and `internal/` for `OpenFileStore`, which yielded only
   its own definition, with the same sweep hitting in `filestore_test.go` and `cache_test.go`
   as the positive control. `cmd/cairn-server` now constructs one when
   `$CAIRN_CONTROL_JOURNAL` is set, and only then.
2. **It synthesizes exactly one user**, with `Provider = "cairn-token-file"` and
   `Subject = "operator"`. `SupabaseJWT` resolves against provider `"supabase"` by default,
   so `Model.UserByProviderSubject` **can never match** and every verified session gets the
   uniform 401 that `TestAVerifiedTokenForAnUnknownUserIsRefusedRatherThanProvisioned`
   pins.
3. **Even naming that user does not help.** `tokenfile` emits no `EventMemberSet` at all
   and every one of its grant sites uses `SubjectKind: control.KindProject`, so the
   `operator` user is the subject of no grant and no membership. A `TrustedHeader` pointed
   at provider `cairn-token-file` asserting subject `operator` therefore authenticates
   somebody who resolves to an **empty `Authorization`** — a 200 that permits nothing.

The concrete trap: an operator follows this README, sets `CAIRN_SUPABASE_JWKS_URL` and
`CAIRN_SUPABASE_ISSUER`, gets a pod that fetches the JWKS, starts clean, satisfies the
partial-configuration ledger and passes its health check — and every sign-in **succeeds
and reads nothing**. That is precisely the failure `config.go`'s ledger exists to prevent,
arriving by a route the ledger structurally cannot see: it asks *"did you configure it"*,
never *"can it ever resolve anybody"*.

🔴 **THAT EXACT COMMAND LINE IS NOW A STARTUP REFUSAL.** Those two variables with no
`$CAIRN_CONTROL_JOURNAL` is `ErrSessionBackendWithoutAuthority`, above. Reaching the state
described here takes a caller that hands `FromEnvironment` the token-file projection AS the
session authority, deliberately — which nothing in `cmd/cairn-server` does, and which
`TestTheSameConfigurationOverTheTokenFileProjectionAuthenticatesNobody` now has to spell
out in order to measure it at all.

⚠ **MEASURED AT `229c142` RATHER THAN ARGUED, AND FACT 3 IS THE ONE WORTH READING TWICE.**
A trusted-header backend configured with `CAIRN_TRUSTED_HEADER_PROVIDER=cairn-token-file`
and a request asserting subject `operator` authenticated: `Identity.Valid()` returned
**true**, the principal rendered as `user:usr_… (cairn-token-file:operator)`, and
`Auth.ScopeIDs(read)` was **empty**. A guard that asked only "did authentication succeed"
was green for it.

🔴 **DO NOT CLOSE THIS BY HAVING A BACKEND CREATE USERS ON THE FLY.** That is self-serve
signup, it is P6, and doing it in an `Authenticator` would be an authorization decision
taken silently — the rule `SupabaseJWT`'s own doc comment states. The path that closed it
is the opposite one: `control.ProvisionUser`, reached only by an operator running
`cairn-server -create-user`, which is not an HTTP route and does not appear in
`api.DeclaredRoutes()`.

**WHAT REMAINS THE PRECONDITION:** a real `control.User` row with the provider and subject
an IdP actually asserts. Configuring `CAIRN_CONTROL_JOURNAL` and provisioning is how that
row comes to exist. A deployment that sets nothing still reaches nobody through these
backends — but it no longer reaches them *while looking configured*: arming one without the
journal is refused at startup, so "correct and unreachable" is now a property of a
deployment that never armed them rather than of one that did.

⚠ **The HS256 deletion did not change any of this**, checked rather than assumed: it moves
only what `Verify` will accept as a signature. `SupabaseJWT.Authenticate` reaches
`model.UserByProviderSubject` *after* verification, and neither that call nor
`DefaultSupabaseProvider` nor anything in `internal/control/tokenfile` was touched. A
deployment that could resolve nobody before can resolve nobody now, for the same reason.

## What this package structurally CANNOT see

- **A real identity provider.** Every key here is generated at test time and every JWKS is
  served by a loopback `httptest` server. Supabase's actual token shape, its actual claim
  set and its actual rotation cadence are unmeasured — the corpus of this package is what
  RFC 7515/7517/7519 say, not what one vendor emits.
- **A real auth proxy.** No oauth2-proxy, no forward-auth ingress, no mTLS handshake. The
  client-certificate rung reads `r.TLS.VerifiedChains`, which a test sets directly; that
  the field is populated only by a correctly-configured `tls.Config` is a property of
  `crypto/tls`, relied on rather than measured here.
- **A deployed instance of either backend.** `packages.server-image` does not carry P4 or
  P5 configuration and nothing has run it; everything below `api.New`'s default is
  exercised in-process, including the journal wiring — `TestTheBinaryActuallyDispatchesCreateUser`
  runs the real binary, and no test starts a server with a control journal AND a session
  backend AND a live request. ⚠ This line used to say something wider ("no deployment that
  can exist today resolves anybody through them") and that is now false for a deployment
  that sets `CAIRN_CONTROL_JOURNAL` — see the section above, which states both halves.
- **Concurrency.** `KeySet` takes an `RWMutex`, and CI's `go` job runs `go test -race ./...`,
  so an unsynchronised access on an interleaving one of these tests happens to produce would
  be reported. 🔴 **That is the whole of what `-race` establishes here, and it is narrower
  than it reads.** The detector sees a DATA race on an interleaving a test actually creates,
  and **no test in this package creates deliberate refresh-vs-verify contention** — so the
  interleaving that would matter is one it never observes. It is also structurally blind to
  a LOGICAL race: `internal/control/cache_test.go` records one that is green under `-race`
  and wrong anyway, two refreshes perfectly synchronised and committing in the wrong order.
  ⚠ This line used to claim the package "is exercised under `-race`" while **no gate in this
  repository ran it**: enumerated tree-wide at `7ac810e`, the literal `-race` appeared in four
  files, of which one — `tests/test_subsystem_store_api.py` — is the substring inside
  "port-race retry" rather than the flag at all, so **three** carried it and all three were
  prose; `.github/workflows/ci.yml` and both `flake.nix` check phases ran `go test ./...`
  plain. ⚠ Re-enumerated at `372601d`, the commit that added the gate, it is **five** files
  and one of them RUNS it — so both counts are pinned to a REF rather than to "the PR head",
  which is a moving target and is how this number went stale inside its own pull request.
  `flake.nix` still holds no `-race` at all. It had been run BY HAND at least once and it
  earned its keep doing so
  (`cmd/cairn-server/main_test.go` records a genuine data race it caught, twice in one
  `-count=2` run), which is the argument for gating it rather than deleting the sentence.
  The sentence is now true because the CI job changed, not because it was softened; the
  lock's correctness is still reasoned about rather than measured, the same honest limit
  `internal/control/README.md` records for `control.Cache`.
- **Whether the chain's ORDER matters under a real mixed deployment.** The order is pinned
  structurally (`TestBackendsOrdersTheChainMachineTokenFirstAndTrustedHeaderLast`) and no
  test presents a request carrying a machine token AND a valid session AND a proxy header
  at once against a live pod.
- **Session cookies, sign-in, sign-out, refresh-token rotation.** An `Authenticator` reads
  a request; establishing the session is the PWA's (P5) and signup's (P6).
- **Revocation latency for a session.** `control.Cache`'s bound governs the principal's
  AUTHORITY; a JWT remains valid until its own `exp` regardless, and this package has no
  denylist. That is a real gap and it is P5/P6's to close, with a session store.
