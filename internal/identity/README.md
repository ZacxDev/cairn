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

RS256, ES256 and HS256. `crypto/rsa`, `crypto/ecdsa`, `crypto/hmac`, `crypto/ecdh` for
point validation, `encoding/json` + `math/big` for JWK parsing, `net/http` for the JWKS
fetch. No dependency was needed; `go.mod` still has no `require` block.

🔴 **THE TOKEN'S HEADER SELECTS FROM THE CONFIGURED SET AND CAN NEVER WIDEN IT.** The
classic forgery is `alg: none`; the second classic one is `alg: HS256` against a deployment
that verifies with an RSA public key, where the public key — which the attacker has,
because it is public — becomes the HMAC secret. Both die on one rule, enforced in three
places: `accepts` (the deployment's allowlist), the key resolver (`KeySet.key` and
`resolverPair`, which route HS256 to a configured secret and never to the key set), and
`verifySignature`'s own type assertions.

🔴 **AND THE TEST FOR IT HAS TWO ARMS BECAUSE ONE OF THEM DOES NOT REACH THE GUARD —
MEASURED, NOT ASSUMED.** The first draft of `TestTheAlgorithmConfusionForgeryIsREFUSED`
tested a JWKS-only deployment. It passed, and it passed for the wrong reason: `accepts`
refuses HS256 before any key is resolved, so the key/algorithm rule never executed. Proven
by opening the hole on purpose — `KeySet.key`'s type check replaced by `if false`, and
`verifySignature`'s HS256 arm taught to take an RSA modulus as its secret — after which
that arm **still passed**. The second arm is a **mixed** deployment (a JWKS *and* a legacy
symmetric secret), which is both the configuration that reaches the resolver and the
realistic one: a Supabase project mid-migration accepts both, which is exactly when the
attack is available. With the hole open, the mixed arm goes red and the JWKS-only arm stays
green.

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
  public document is a secret everybody has.
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

Other rules:

- **an unrecognised boolean is an ERROR, never `false`** —
  `CAIRN_TRUSTED_HEADER_PROXY_FRONTED=treu` must not silently mean "not proxy-fronted";
- **a secret has exactly one source** — inline or a file, never both, because a precedence
  rule nobody reads makes a rotation that updated the wrong one appear to work;
- **prefer the `_FILE` form**: an environment variable is readable from
  `/proc/<pid>/environ`, inherited by every child, and printed by any `env` that reaches a
  log. The pod already mounts its bearer token as a file.

## What this package structurally CANNOT see

- **A real identity provider.** Every key here is generated at test time and every JWKS is
  served by a loopback `httptest` server. Supabase's actual token shape, its actual claim
  set and its actual rotation cadence are unmeasured — the corpus of this package is what
  RFC 7515/7517/7519 say, not what one vendor emits.
- **A real auth proxy.** No oauth2-proxy, no forward-auth ingress, no mTLS handshake. The
  client-certificate rung reads `r.TLS.VerifiedChains`, which a test sets directly; that
  the field is populated only by a correctly-configured `tls.Config` is a property of
  `crypto/tls`, relied on rather than measured here.
- **A deployed instance of either backend.** `packages.server-image` does not carry P4
  configuration and nothing has run it. Everything below `api.New`'s default is exercised
  in-process.
- **Concurrency.** `KeySet` takes an `RWMutex` and is exercised under `-race`, but nothing
  runs a refresh and a verification at the same instant deliberately, so the lock's
  correctness is reasoned about rather than measured — the same honest limit
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
