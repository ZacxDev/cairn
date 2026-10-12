#!/usr/bin/env bash
# Validate the cairn chart. Every `fail` guard gets a NEGATIVE control (it must
# refuse) and the happy path gets a POSITIVE one (it must render), because a
# guard nobody watched refuse is decoration.
set -u
C="$(cd "$(dirname "${BASH_SOURCE[0]}")" && CDPATH= pwd -P)"
# Synthetic, obviously-not-a-real-commit sha and digest (fixtures must be synthetic).
SHA=0123456789abcdef0123456789abcdef01234567
DIGEST=sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
IMG="--set image.store=ghcr.io/example/cairn-store-go:sha-$SHA --set image.ui=ghcr.io/example/cairn-ui:sha-$SHA"
# BASE is every required value EXCEPT the images, so an image guard can be reached
# with nothing earlier refusing first; OK is the full required set.
BASE="--set storage.existingClaim=cairn-data --set tokenFile.secretName=cairn-token --set trustedProxies[0]=192.0.2.0/24"
OK="$BASE $IMG"
pass=0; fail=0

t_render() {  # label, extra-args  -> MUST render
  if helm template rel "$C" $OK $2 >/dev/null 2>&1; then
    echo "  ✅ $1"; pass=$((pass+1))
  else
    echo "  🔴 $1 — expected RENDER, got refusal:"; helm template rel "$C" $OK $2 2>&1 | tail -2 | sed 's/^/        /'
    fail=$((fail+1))
  fi
}
t_refuse() {  # label, args, expected-substring -> MUST refuse, with THIS message
  out=$(helm template rel "$C" $2 2>&1)
  if [ $? -eq 0 ]; then
    echo "  🔴 $1 — expected REFUSAL, it rendered"; fail=$((fail+1)); return
  fi
  if printf '%s' "$out" | grep -q "$3"; then
    echo "  ✅ $1 (refused with its own message)"; pass=$((pass+1))
  else
    echo "  ⚠ $1 — refused, but for the WRONG reason:"; printf '%s\n' "$out" | tail -2 | sed 's/^/        /'
    fail=$((fail+1))
  fi
}

echo "=== POSITIVE CONTROL — the happy path must render ==="
t_render "complete values render" ""
t_render "trustedProxies at /24 accepted" "--set trustedProxies[0]=192.0.2.0/24"
t_render "ui disabled still renders the store" "--set ui.enabled=false"
t_render "ui.replicas is not checked when ui is off" "--set ui.enabled=false --set ui.replicas=2"
t_render "store.replicas is not checked when store is off" "--set store.enabled=false --set store.replicas=2"
t_render "IPv6 /64 accepted (the binary's v6 floor)" "--set trustedProxies[0]=2001:db8::/64"
t_render "bare IPv4 address accepted, as the binary does" "--set trustedProxies[0]=192.0.2.1"
t_render "bare IPv6 address accepted, as the binary does" "--set trustedProxies[0]=2001:db8::1"
t_render "digest-pinned image accepted" "--set image.store=ghcr.io/example/cairn-store-go@$DIGEST"
t_render "a disabled workload needs no image" "--set ui.enabled=false --set image.ui="

echo
echo "=== NEGATIVE CONTROLS — each guard must refuse, with ITS OWN message ==="
t_refuse "missing storage.existingClaim" "--set tokenFile.secretName=t" "storage.existingClaim is required"
t_refuse "missing tokenFile.secretName"  "--set storage.existingClaim=c" "tokenFile.secretName is required"
t_refuse "trustedProxies /16 refused"    "$OK --set trustedProxies[0]=192.0.2.0/16" "floor is /24 for IPv4"
t_refuse "IPv6 /48 refused (v6 floor /64)" "$OK --set trustedProxies[0]=2001:db8::/48" "floor is /64 for IPv6"
t_refuse "a /0 refused with its own reason" "$OK --set trustedProxies[0]=0.0.0.0/0" "trusts every peer"
t_refuse "a hostname refused"            "$OK --set trustedProxies[0]=proxy.example" "is not an IP address or CIDR"
t_refuse "an octet above 255 refused"    "$OK --set trustedProxies[0]=999.0.2.0/24" "is not an IP address or CIDR"
t_refuse "a leading-zero octet refused"  "$OK --set trustedProxies[0]=192.0.2.01" "is not an IP address or CIDR"
t_refuse "a non-numeric prefix refused"  "$OK --set trustedProxies[0]=192.0.2.0/x" "prefix length is not a number"
t_refuse "a prefix past the address refused" "$OK --set trustedProxies[0]=192.0.2.0/33" "longer than the address"
t_refuse "trustedProxies as a string refused" "$BASE $IMG --set trustedProxies=192.0.2.0/24" "must be a LIST"
t_refuse "both workloads disabled"       "$OK --set store.enabled=false --set ui.enabled=false" "no workload at all"
t_refuse "empty trustedProxies refused"  "--set storage.existingClaim=c --set tokenFile.secretName=t" "trustedProxies is required"
t_refuse "ui.replicas=2 refused"         "$OK --set ui.replicas=2" "ui.replicas must be 1"
t_refuse "ui.replicas=1.5 refused"       "$OK --set ui.replicas=1.5" "ui.replicas must be 1"
t_refuse "ui.replicas=null refused"      "$OK --set ui.replicas=null" "ui.replicas must be 1"
t_refuse "store.replicas=2 refused"      "$OK --set store.replicas=2" "store.replicas must be 1"
t_refuse "store.replicas=null refused"   "$OK --set store.replicas=null" "store.replicas must be 1"
t_refuse "image.store missing refused"   "$BASE --set image.ui=ghcr.io/example/cairn-ui:sha-$SHA" "image.store is required"
t_refuse "image.ui missing refused"      "$BASE --set image.store=ghcr.io/example/cairn-store-go:sha-$SHA" "image.ui is required"
t_refuse "the default values refused (no image)" "$BASE" "is required"
t_refuse ":latest refused"               "$OK --set image.store=ghcr.io/example/cairn-store-go:latest" "not an immutable reference"
t_refuse "a version tag refused"         "$OK --set image.ui=ghcr.io/example/cairn-ui:1.2.3" "not an immutable reference"
t_refuse "a short sha tag refused"       "$OK --set image.ui=ghcr.io/example/cairn-ui:sha-0123456" "not an immutable reference"

echo
echo "=== the rendered output must CARRY the landmine answers ==="
R=$(helm template rel "$C" $OK 2>/dev/null)
chk() { n=$(printf '%s' "$R" | grep -c "$2"); if [ "$n" -ge "$3" ]; then echo "  ✅ $1 ($n)"; pass=$((pass+1)); else echo "  🔴 $1 — found $n, want >=$3"; fail=$((fail+1)); fi; }
chk "enableServiceLinks: false on both pods" 'enableServiceLinks: false' 2
chk "explicit CAIRN_UI_PORT"                 'CAIRN_UI_PORT' 1
chk "explicit CAIRN_PORT"                    'name: CAIRN_PORT' 1
chk "/healthz is the only probe path"        'path: /healthz' 4
chk "UI mount is readOnly"                   'readOnly: true' 3
chk "netpol names both ports"                'port: 810' 2
chk "UI session file set explicitly"        'name: CAIRN_UI_SESSION_FILE' 1
chk "UI session dir is a writable emptyDir" 'mountPath: /var/lib/cairn-ui$' 1
chk "sessions volume is an emptyDir"        'emptyDir: {}' 1

# 🔴 THE RELATIONSHIP, NOT THE TWO STRINGS: the session FILE must sit inside the
# writable `sessions` MOUNT. Each half alone can be right while the pair is wrong.
sess_file=$(printf '%s\n' "$R" | grep -A1 'name: CAIRN_UI_SESSION_FILE' | sed -n 's/.*value: *"\{0,1\}\([^"]*\)"\{0,1\}$/\1/p')
sess_mount=$(printf '%s\n' "$R" | grep -A1 -- '- name: sessions$' | sed -n 's/.*mountPath: *//p')
if [ -n "$sess_file" ] && [ -n "$sess_mount" ] && [ "$(dirname "$sess_file")" = "$sess_mount" ]; then
  echo "  ✅ session file $sess_file is inside the sessions mount $sess_mount"; pass=$((pass+1))
else
  echo "  🔴 session file '$sess_file' is NOT inside the sessions mount '$sess_mount'"; fail=$((fail+1))
fi

echo
echo "=== 🔴 THE ONE THAT MATTERS: no readOnly on the CLAIM REFERENCE ==="
claim_ro=$(printf '%s' "$R" | grep -A3 'persistentVolumeClaim:' | grep -c 'readOnly')
if [ "$claim_ro" -eq 0 ]; then echo "  ✅ claim reference carries no readOnly (0 found)"; pass=$((pass+1));
else echo "  🔴 claim reference has readOnly — this is the LINSTOR mount refusal"; fail=$((fail+1)); fi
printf '%s' "$R" | grep -A3 'persistentVolumeClaim:' | sed 's/^/        /'

echo
echo "=== $pass passed, $fail failed ==="
[ "$fail" -eq 0 ]
