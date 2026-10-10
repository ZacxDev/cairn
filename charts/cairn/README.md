# `charts/cairn` — the manifest knowledge, as refusals

🔴 **THIS CHART EXISTS TO ENCODE THE MANIFEST LANDMINES, NOT TO MAKE AUTH
TURNKEY.** Every guard in `_helpers.tpl` is a mistake that has already been made
on a real cluster, converted from a comment somebody has to read into a render
that cannot proceed. A chart whose value is documentation is worth less than the
README it replaces.

```bash
./charts/cairn/test.sh            # needs helm; nix-shell -p kubernetes-helm
helm template rel charts/cairn \
  --set storage.existingClaim=cairn-data \
  --set tokenFile.secretName=cairn-token
```

## What it refuses, and why each one is here

| refusal | the incident behind it |
|---|---|
| `storage.existingClaim` required | the two known deployments place the store volume by **different mechanisms** — a linstor PV `nodeAffinity` vs `local-path` node-locality — so a claim created here is wrong on one of them |
| `tokenFile.secretName` required | the binary refuses to start without a token file and **falls back to `/run/secrets/subsystem-store/token`**, so a manifest that "sets nothing" is really mounting a secret at that default path |
| `trustedProxies` wider than /24 | the binary itself refuses a /16 (*"/16 covers 65536 peers; the floor is /24"*). ⚠ And listing every /24 inside a /16 is **that same /16 respelled** — it satisfies the floor while defeating its point |
| a bare address in `trustedProxies` | a host where a network belongs |
| both workloads disabled | a release that renders nothing is a typo, not a configuration |

## What it hardcodes, and why those are NOT values

🔴 **A knob is an invitation to get it wrong.** These are fixed because every
value they could take other than this one is a known outage:

- **`enableServiceLinks: false` plus an explicit port variable, on both pods.**
  The kubelet injects `<SVCNAME>_PORT=tcp://<clusterIP>:<port>` for every Service
  in the namespace, so a Service named `cairn-ui` injects `CAIRN_UI_PORT=tcp://…`
  — the variable the binary reads as its **listen port**. A kubelet injection and
  a manifest `env:` are ONE process environment, so the injection shadows the
  manifest and startup is fatal. **Keep both halves: they fail differently.**
  ⚠ Renaming the Service does not remove the class — any name whose env form
  collides with a `CAIRN_*` the binary reads recreates it.
- **`/healthz` as the only probe path.** Measured by running the binary:
  `/healthz` answers **200 unauthenticated**; `/`, `/health` and `/readyz` all
  answer **401**. Probing anything else leaves the pod permanently NotReady while
  the process is perfectly healthy.
- **No `readOnly` on the claim reference, `readOnly: true` on the container
  mount.** 🔴 **A read-only flag at two layers is not the same flag twice.** On
  the claim reference it makes the CSI driver mount the device `-o ro`, which on
  a block-backed volume (LINSTOR) is **refused** when another pod holds the same
  device rw on the same node — *"Can't mount, would change RO state"*. On the
  container's `volumeMounts` it is about the container's view and is free.
  ⚠ **On hostPath the wrong one silently works**, which is exactly why it
  survives a port between the two clusters. The claim reference is emitted from
  one helper with no knob so it cannot grow the flag.
- **The NetworkPolicy's port list, derived from the same `enabled` flags the
  Deployments use.** A namespace-wide `podSelector: {}` covers a new pod the
  instant it exists — by denying everything — so a UI added on a second port
  behind a policy written for the store alone is **silently unreachable while
  reporting healthy**: 1/1 Ready, probes green, ingress unable to open a socket.
  That reads as a UI bug, not a policy one. Deriving the list means the two
  cannot disagree.

## What it deliberately does NOT do

🔴 **It does not bundle an identity provider**, and that was decided on evidence
rather than taste. Three reasons, the first being the one that matters:

1. **It would not solve the problem.** The burden is **authorization**, not
   authentication: scope visibility is `control.Resolve`'s answer and SSO
   membership is deliberately not a scope grant, so a federated user signs in
   beautifully and then sees *"No scope is visible to this credential"* until
   someone grants scopes.
2. **It converts a graceful degradation into a hard dependency.** With no
   provider the sign-in button is simply absent and the credential form still
   works; a failed JWKS fetch at startup is a warning rather than a refusal.
   Bundling makes an auth outage a cairn outage.
3. **The footprint inverts.** cairn's serving path is a stdlib-only Go binary
   reading markdown off a disk, guarded by an import ban. An IdP bundle would
   make this chart own a database lifecycle.

⚠ **It also does not create the PVC**, for the reason in the refusal table: the
one genuinely hard part is per-cluster, so it stays a value.

## Status

**Unreleased, and not yet wired into `flake.nix`'s checks** — `test.sh` is
runnable and complete, but nothing runs it automatically, so it is a test you
have to remember. That is a real gap and is named rather than implied.
