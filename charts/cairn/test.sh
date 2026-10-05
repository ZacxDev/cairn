#!/usr/bin/env bash
# Validate the cairn chart. Every `fail` guard gets a NEGATIVE control (it must
# refuse) and the happy path gets a POSITIVE one (it must render), because a
# guard nobody watched refuse is decoration.
set -u
C="$(cd "$(dirname "${BASH_SOURCE[0]}")" && CDPATH= pwd -P)"
OK="--set storage.existingClaim=cairn-data --set tokenFile.secretName=cairn-token"
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
    echo "  ⚠ $1 — refused, but for the WRONG reason:"; printf '%s' "$out" | tail -2 | sed 's/^/        /'
    fail=$((fail+1))
  fi
}

echo "=== POSITIVE CONTROL — the happy path must render ==="
t_render "complete values render" ""
t_render "trustedProxies at /24 accepted" "--set trustedProxies[0]=192.0.2.0/24"
t_render "ui disabled still renders the store" "--set ui.enabled=false"

echo
echo "=== NEGATIVE CONTROLS — each guard must refuse, with ITS OWN message ==="
t_refuse "missing storage.existingClaim" "--set tokenFile.secretName=t" "storage.existingClaim is required"
t_refuse "missing tokenFile.secretName"  "--set storage.existingClaim=c" "tokenFile.secretName is required"
t_refuse "trustedProxies /16 refused"    "$OK --set trustedProxies[0]=192.0.2.0/16" "wider than /24"
t_refuse "bare address refused"          "$OK --set trustedProxies[0]=192.0.2.1" "not CIDR notation"
t_refuse "both workloads disabled"       "$OK --set store.enabled=false --set ui.enabled=false" "no workload at all"

echo
echo "=== the rendered output must CARRY the landmine answers ==="
R=$(helm template rel "$C" $OK --set trustedProxies[0]=192.0.2.0/24 2>/dev/null)
chk() { n=$(printf '%s' "$R" | grep -c "$2"); if [ "$n" -ge "$3" ]; then echo "  ✅ $1 ($n)"; pass=$((pass+1)); else echo "  🔴 $1 — found $n, want >=$3"; fail=$((fail+1)); fi; }
chk "enableServiceLinks: false on both pods" 'enableServiceLinks: false' 2
chk "explicit CAIRN_UI_PORT"                 'CAIRN_UI_PORT' 1
chk "explicit CAIRN_PORT"                    'name: CAIRN_PORT' 1
chk "/healthz is the only probe path"        'path: /healthz' 4
chk "UI mount is readOnly"                   'readOnly: true' 3
chk "netpol names both ports"                'port: 810' 2

echo
echo "=== 🔴 THE ONE THAT MATTERS: no readOnly on the CLAIM REFERENCE ==="
claim_ro=$(printf '%s' "$R" | grep -A3 'persistentVolumeClaim:' | grep -c 'readOnly')
if [ "$claim_ro" -eq 0 ]; then echo "  ✅ claim reference carries no readOnly (0 found)"; pass=$((pass+1));
else echo "  🔴 claim reference has readOnly — this is the LINSTOR mount refusal"; fail=$((fail+1)); fi
printf '%s' "$R" | grep -A3 'persistentVolumeClaim:' | sed 's/^/        /'

echo
echo "=== $pass passed, $fail failed ==="
[ "$fail" -eq 0 ]
