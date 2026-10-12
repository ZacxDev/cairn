{{/*
Name helpers — ordinary chart boilerplate.
*/}}
{{- define "cairn.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "cairn.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "cairn.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "cairn.labels" -}}
app.kubernetes.io/name: {{ include "cairn.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
🔴 THE PRECONDITION BLOCK — THE REASON THIS CHART IS WORTH SHIPPING.

Every `fail` below is a landmine that has already been stepped on, converted from
a comment somebody has to read into a render that cannot proceed. A chart whose
value is documentation is worth less than the README it replaces; these refuse.

Call it from every template so a partial render cannot sneak past.
*/}}
{{- define "cairn.preconditions" -}}

{{- if not .Values.storage.existingClaim -}}
{{- fail "cairn: storage.existingClaim is required. This chart does NOT create the store PVC: the two known deployments express the volume's placement by different mechanisms (a linstor PV nodeAffinity vs local-path node-locality), so a claim created here would be wrong on one of them. Create the claim, then name it." -}}
{{- end -}}

{{- if not .Values.tokenFile.secretName -}}
{{- fail "cairn: tokenFile.secretName is required. The binary refuses to start without a token file and falls back to /run/secrets/subsystem-store/token, so a deployment that sets nothing is really mounting a secret at that default path — an absent variable in a working manifest is not an absent requirement." -}}
{{- end -}}

{{/*
🔴 AN EMPTY LIST IS REFUSED, BECAUSE BOTH BINARIES REFUSE IT — MEASURED BY RUNNING
THEM. `cairn-server` exits 78 without `CAIRN_TRUSTED_PROXIES` whatever its bind
address ("there is deliberately no default"), and `cairn-ui` exits 78 without it on
any non-loopback bind, which `0.0.0.0` here always is. A chart that rendered an empty
list would install two pods that crash-loop, so the refusal moves to render time.
*/}}
{{- if not .Values.trustedProxies -}}
{{- fail "cairn: trustedProxies is required. Both binaries refuse to start without CAIRN_TRUSTED_PROXIES when reachable from outside the pod (cairn-server always; cairn-ui on any non-loopback bind, and this chart binds 0.0.0.0), so an empty list renders two pods that crash-loop. List the proxy's address(es) or network(s): IPv4 /24 or narrower, IPv6 /64 or narrower." -}}
{{- end -}}
{{- if not (kindIs "slice" .Values.trustedProxies) -}}
{{- fail (printf "cairn: trustedProxies must be a LIST of addresses or CIDRs, got %s. Use trustedProxies[0]=… rather than trustedProxies=…" (kindOf .Values.trustedProxies)) -}}
{{- end -}}

{{/*
🔴 EACH ENTRY IS CHECKED BY THE BINARY'S OWN RULE, NOT A STRICTER OR LOOSER ONE —
`internal/netid`'s `TrustedNetwork`. A rule that differs in EITHER direction is a
defect: a looser one renders a release whose pods exit 78, and a stricter one turns
away a value the binary takes on purpose. Mirrored, in its order:

- a BARE ADDRESS is accepted as that one host (/32 or /128). The binary accepts it
  deliberately: refusing it pushes an operator towards the `/0` the next rule stops.
- `/0` is refused with its own message ("trusts every peer").
- the floor is PER ADDRESS FAMILY, `MinTrustedPrefix`: IPv4 /24, IPv6 /64. A /48
  is a fine v6 allocation and a fatal v6 proxy entry. Listing every /24 of a /16 is
  that same /16 respelled — the floor is on the prefix length, the property, and
  not on the count of entries.

The family is the address's: anything containing `:` is v6, as `netip` reads it
(an IPv4-mapped `::ffff:a.b.c.d` is v6 there too, so it gets the /64 floor).

⚠ HELM HAS NO IP PARSER, SO THE SHAPE CHECK IS A PATTERN, AND IT IS EXACT FOR ONLY
PART OF THE INPUT. An IPv4 address (octets 0-255, no leading zeros, as `netip`
requires) and the prefix length are checked exactly; an IPv6 literal is checked by
its CHARACTER SET only, so a malformed one (`::::`) renders and the binary then exits
78 naming it. Measured by a differential of chart against binary, not assumed. The
floor and `/0` rules are exact for every entry that parses.
*/}}
{{- range .Values.trustedProxies -}}
  {{- $entry := toString . -}}
  {{- $parts := splitList "/" $entry -}}
  {{- $addr := index $parts 0 -}}
  {{- $v6 := contains ":" $addr -}}
  {{- $bitlen := ternary 128 32 $v6 -}}
  {{- $octet := "(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])" -}}
  {{- $shape := ternary "^[0-9A-Fa-f:.]+$" (printf "^%s([.]%s){3}$" $octet $octet) $v6 -}}
  {{- if or (gt (len $parts) 2) (not (regexMatch $shape $addr)) -}}
    {{- fail (printf "cairn: trustedProxies entry %q is not an IP address or CIDR. One address or network per list entry." $entry) -}}
  {{- end -}}
  {{- $bits := $bitlen -}}
  {{- if eq (len $parts) 2 -}}
    {{- if not (regexMatch "^(0|[1-9][0-9]{0,2})$" (index $parts 1)) -}}
      {{- fail (printf "cairn: trustedProxies entry %q is not an IP address or CIDR: the prefix length is not a number." $entry) -}}
    {{- end -}}
    {{- $bits = atoi (index $parts 1) -}}
    {{- if gt $bits $bitlen -}}
      {{- fail (printf "cairn: trustedProxies entry %q is not an IP address or CIDR: /%d is longer than the address." $entry $bits) -}}
    {{- end -}}
  {{- end -}}
  {{- if eq $bits 0 -}}
    {{- fail (printf "cairn: trustedProxies entry %q trusts every peer, which is the defect this setting exists to close. Name the proxy's address." $entry) -}}
  {{- end -}}
  {{- $floor := ternary 64 24 $v6 -}}
  {{- if lt $bits $floor -}}
    {{- fail (printf "cairn: trustedProxies entry %q is too broad: the binary's floor is /%d for %s, and it exits 78 on anything wider. A wide range hands the client identity to everything inside it; list the proxy's addresses or narrower networks (listing every /%d of a wider range is that range respelled)." $entry $floor (ternary "IPv6" "IPv4" $v6) $floor) -}}
  {{- end -}}
{{- end -}}

{{/*
🔴 EXACTLY ONE REPLICA OF EACH WORKLOAD, AND "EXACTLY" INCLUDES THE TYPE. Compared as
the STRING "1" so a null, a non-integer (`1.5`) or a word is refused rather than
coerced: `int` maps all three to 0 or 1 and would wave them through.

- UI: the session table is a file on a per-pod emptyDir. A cookie issued by one
  replica is unknown to another, so a second replica signs browsers out at random
  while the Deployment reports 2/2 Ready.
- store: it holds a ReadWriteOnce claim and is the WRITER, which is why its rollout
  strategy is `Recreate`. A second replica either cannot attach (Pending) or, on a
  single node, is a second writer on one store.
*/}}
{{- if and .Values.ui.enabled (ne (toString .Values.ui.replicas) "1") -}}
{{- fail (printf "cairn: ui.replicas must be 1, got %v. The UI keeps its browser session table in a file on a per-pod emptyDir, so a session issued by one replica is unknown to the next and sign-ins break at random while every pod reports Ready." .Values.ui.replicas) -}}
{{- end -}}
{{- if and .Values.store.enabled (ne (toString .Values.store.replicas) "1") -}}
{{- fail (printf "cairn: store.replicas must be 1, got %v. The store is the single writer on a ReadWriteOnce claim (hence its Recreate strategy): a second replica either cannot attach or becomes a second writer on one store." .Values.store.replicas) -}}
{{- end -}}

{{/*
🔴 THE IMAGE IS REQUIRED AND MUST BE IMMUTABLE — THERE IS NO `latest` TO DEFAULT TO.
`publish-image.yml` publishes `sha-<40-hex commit>` (always) and a bare version (on a
git tag push), and says in so many words "THE TAG IS IMMUTABLE AND THERE IS NO
`latest`": a default of `:latest` is an ImagePullBackOff on every install.

Accepted: a ref whose tag is `sha-<40 hex>`, or ANY ref carrying `@sha256:<64 hex>`.
Refused: everything else, INCLUDING the version tags. They are published, but from a
`tags: ["*"]` push trigger plus `workflow_dispatch`, so re-pushing a git tag moves
them; a commit sha cannot be re-pointed, and a digest is the content itself.
Only an ENABLED workload's image is required.
*/}}
{{- range $which := list "store" "ui" -}}
  {{- if (index $.Values $which).enabled -}}
    {{- $ref := toString (index $.Values.image $which) -}}
    {{- if or (eq $ref "") (eq $ref "<nil>") -}}
      {{- fail (printf "cairn: image.%s is required. There is no default because there is no mutable tag to default to: the publish workflow pushes only immutable tags and no `latest`. Name ghcr.io/<owner>/<package>:sha-<40-hex commit>, or pin a digest (…@sha256:<64 hex>)." $which) -}}
    {{- end -}}
    {{- if not (or (regexMatch "@sha256:[0-9a-f]{64}$" $ref) (regexMatch "^[^@]+:sha-[0-9a-f]{40}$" $ref)) -}}
      {{- fail (printf "cairn: image.%s %q is not an immutable reference. Use the tag sha-<40-hex commit> or pin a digest (@sha256:<64 hex>). `latest` does not exist in this registry, and a version tag can be re-pointed by re-pushing its git tag." $which $ref) -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- if and (not .Values.store.enabled) (not .Values.ui.enabled) -}}
{{- fail "cairn: both store.enabled and ui.enabled are false — this release would render no workload at all." -}}
{{- end -}}

{{- end -}}

{{/*
🔴 THE VOLUME BLOCK IS A HELPER SO THE CLAIM REFERENCE CANNOT GROW A `readOnly`.

A read-only flag exists at TWO layers here and they are not the same flag twice:
`readOnly` on the CLAIM REFERENCE makes the CSI driver mount the device `-o ro`,
and on a block-backed volume (LINSTOR) that is REFUSED outright when another pod
holds the same device rw on the same node — "Can't mount, would change RO state".
`readOnly` on the container's `volumeMounts` entry is about the CONTAINER'S VIEW
and is free. On hostPath the wrong one silently works, which is exactly why it
survives a port between the two clusters.

So the claim reference is emitted HERE, once, with no knob.
*/}}
{{/*
The UI's session directory: ONE string, read by both the CAIRN_UI_SESSION_FILE value
and the `sessions` volumeMount, so the file cannot end up outside the writable mount.
It is the binary's own default directory, not a knob.
*/}}
{{- define "cairn.uiSessionDir" -}}/var/lib/cairn-ui{{- end -}}

{{- define "cairn.storeVolume" -}}
- name: store
  persistentVolumeClaim:
    claimName: {{ .Values.storage.existingClaim }}
{{- end -}}
