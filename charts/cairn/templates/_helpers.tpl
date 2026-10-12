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
🔴 A /16 IS REFUSED BY THE BINARY AND IS REFUSED HERE, EARLIER. Enumerating every
node /24 inside a /16 is the same /16 respelled: it satisfies the floor while
defeating the point. This checks the PREFIX LENGTH, which is the property, and
not the count of entries, which is not.
*/}}
{{/*
🔴 AN EMPTY LIST IS REFUSED TOO, BECAUSE BOTH BINARIES REFUSE IT — MEASURED BY RUNNING
THEM. `cairn-server` exits 78 without `CAIRN_TRUSTED_PROXIES` whatever its bind
address ("there is deliberately no default"), and `cairn-ui` exits 78 without it on
any non-loopback bind, which `0.0.0.0` here always is. A chart that rendered an empty
list would install two pods that crash-loop, so the refusal moves to render time.
*/}}
{{- if not .Values.trustedProxies -}}
{{- fail "cairn: trustedProxies is required. Both binaries refuse to start without CAIRN_TRUSTED_PROXIES when reachable from outside the pod (cairn-server always; cairn-ui on any non-loopback bind, and this chart binds 0.0.0.0), so an empty list renders two pods that crash-loop. List the network(s) of the proxy that fronts them, /24 or narrower." -}}
{{- end -}}

{{/*
🔴 ONE UI REPLICA, BECAUSE THE SESSION TABLE IS A FILE ON A PER-POD VOLUME. A cookie
issued by one replica is unknown to another, so a second replica signs browsers out
at random, and a Deployment reporting 2/2 Ready gives no sign of it.
*/}}
{{- if and .Values.ui.enabled (gt (int .Values.ui.replicas) 1) -}}
{{- fail "cairn: ui.replicas must be 1. The UI keeps its browser session table in a file on a per-pod emptyDir, so a session issued by one replica is unknown to the next and sign-ins break at random while every pod reports Ready." -}}
{{- end -}}

{{- range .Values.trustedProxies -}}
  {{- $cidr := . -}}
  {{- $parts := splitList "/" $cidr -}}
  {{- if ne (len $parts) 2 -}}
    {{- fail (printf "cairn: trustedProxies entry %q is not CIDR notation. Write a network, never a bare address." $cidr) -}}
  {{- end -}}
  {{- $prefix := atoi (index $parts 1) -}}
  {{- if lt $prefix 24 -}}
    {{- fail (printf "cairn: trustedProxies entry %q is wider than /24. The binary itself refuses a /16 (\"/16 covers 65536 peers; the floor is /24\"), and a wide range here hands proxy trust to every host inside it. Narrow it, and note that listing every /24 of a /16 is the same /16 respelled." $cidr) -}}
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
{{- define "cairn.storeVolume" -}}
- name: store
  persistentVolumeClaim:
    claimName: {{ .Values.storage.existingClaim }}
{{- end -}}
