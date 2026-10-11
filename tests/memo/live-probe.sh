#!/usr/bin/env bash
# The scope-mail LIVE PROBE — closing-condition part 5 of claudedocs/plan-cairn-scope-mail.md. Run
# by the OPERATOR against the DEPLOYED memo listener after S7 reconciles; built and self-tested here
# (S3) against a LOCAL world (`tests/memo/world.sh`).
#
#   usage: tests/memo/live-probe.sh <probe-input-file>   # the probe: 0 iff every step passed
#          tests/memo/live-probe.sh --self-test          # its own negative controls, locally
#
# 🔴 ITS INPUTS ARRIVE THROUGH ONE CHANNEL: the probe-input file, mode 0600, prepared by the
# operator, holding `KEY=value` lines (round 5 🟡4a-ii):
#
#   CAIRN_URL=…        the POD's URL           CAIRN_TOKEN=…      a pod token that reads the probe scope
#   CAIRN_UI_URL=…     the memo listener       CAIRN_UI_TOKEN=…   the PROBE credential, narrowed to
#   PROBE_SCOPE=…      the designated probe scope                 read+write on PROBE_SCOPE only
#   CAIRN_BIN=…        optional: the client binary (default: `cairn` on PATH)
#
# Nothing the probe uses is read from its own environment, no token is ever on argv, and none is
# ever printed — the last line is a summary of counts and the exit code, which is what the operator
# records (on S7's change and in the arc's handoff).
#
# 🔴 ISOLATION — THE PROPERTY: no client call made by the probe reads any of the operator's client
# configuration, routing, cache or credentials, and none writes to them; the answer depends only on
# the input file. THE MEANS: every client call runs under `env -i` with an ALLOWLIST — `PATH`;
# `HOME` and `XDG_STATE_HOME` inside a 0700 temporary directory; `CAIRN_CONFIG` naming the one
# default-instance env file written there (0600) from the input file; and, passed through
# unchanged, `SSL_CERT_FILE`, `SSL_CERT_DIR` and the proxy variables in both cases (they change how
# the network is reached, never which configuration is read). An allowlist rather than a list of
# unsets, because a denylist has to name every variable the client reads, including ones added
# later. The directory is deleted on every exit.
#
# 🔴 THE CACHE-SYNC STEP IS LOAD-BEARING, NOT CEREMONY. `memo-check` asks the LOCAL cache whether
# any instance holds the scope before it sends anything (decision 7), so the probe must `sync` its
# own cache first — and that sync is what reads the POD's URL and token. It is what makes a leaked
# `SUBSYSTEM_STORE_URL`/`_TOKEN` (which beat the config file for the default instance) FAIL the
# probe instead of passing it: the decoy pod is synced, the cache does not hold the probe scope,
# and the positive control prints nothing.
#
# The steps, in order (each a `PASS <step>` / `FAIL <step>` line):
#   listener-401        an unauthenticated request to the listener answers 401 with the
#                       listener's OWN body — the listener answered, not an ingress
#   cache-sync          `sync` of the probe's own cache from the pod exits 0
#   send                `memo-send` to the probe scope exits 0
#   check-one-block     a probe session's `memo-check` prints exactly ONE block, of `count=1`, holding
#                       the probe's subject (the POSITIVE control)
#   retract             `memo-retract` exits 0
#   new-session-silent  a NEW probe session's `memo-check` prints nothing (`memo-status=none`)
# Exit 0 all passed; 1 a step failed (the run stops at the first failure — later steps would only
# measure the wreckage — and its EXIT trap retracts a memo it sent and had not yet retracted, printing
# `CLEANUP …` either way); 2 could not vouch — the input file is missing, not 0600, or lacks a key
# (CAIRN_UI_URL absent FROM THE FILE is exit 2), or a tool is missing.
set -uo pipefail

repo="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"

if [[ "${1:-}" == "--self-test" ]]; then
  exec "$repo/tests/memo/live-probe-selftest.sh"
fi

couldnt() { echo "COULD NOT VOUCH: $*" >&2; echo "SUMMARY live-probe: steps=0 passed=0 failed=0 exit=2"; exit 2; }
[[ $# -eq 1 ]] || couldnt "usage: live-probe.sh <probe-input-file> (exactly one argument; never a token)"
input="$1"
[[ -f "$input" ]] || couldnt "the probe-input file does not exist"
mode="$(stat -c '%a' "$input" 2>/dev/null)"
[[ "$mode" == 600 ]] || couldnt "the probe-input file is mode $mode, not 0600 — it holds two credentials"
for tool in curl mktemp; do command -v "$tool" >/dev/null 2>&1 || couldnt "\`$tool\` is not on PATH"; done

# Read the input file: KEY=value lines, `#` comments; every value from HERE, never the environment.
declare -A in=()
while IFS= read -r line || [[ -n "$line" ]]; do
  line="${line%%$'\r'}"
  [[ -z "${line// }" || "$line" == \#* || "$line" != *=* ]] && continue
  key="${line%%=*}"; key="${key// /}"
  in["$key"]="${line#*=}"
done <"$input"
for key in CAIRN_URL CAIRN_TOKEN CAIRN_UI_URL CAIRN_UI_TOKEN PROBE_SCOPE; do
  [[ -n "${in[$key]:-}" ]] || couldnt "the probe-input file has no $key"
done
probe_cred="${in[CAIRN_UI_TOKEN]}"
listener="${in[CAIRN_UI_URL]%/}"
scope="${in[PROBE_SCOPE]}"
cairn_bin="${in[CAIRN_BIN]:-$(command -v cairn || true)}"
[[ -n "$cairn_bin" && -x "$cairn_bin" ]] || couldnt "no client binary: set CAIRN_BIN in the probe-input file, or put \`cairn\` on PATH"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/cairn-live-probe-XXXXXX")"
chmod 700 "$tmp"
# 🔴 A FAILED RUN MUST NOT POISON THE NEXT ONE. `step` exits at the first failure, so a failure after
# `send` and before `retract` used to leave the probe's memo LIVE (TTL 1 h) — and every rerun inside
# that hour then saw it, and failed at `new-session-silent` for a reason that had nothing to do with
# what it measured. So on EVERY exit, a memo this run sent and has not retracted is retracted here,
# and the outcome is printed either way (the id only, never a token). The trap keeps the run's own
# exit code. `check-one-block` asserting `count=1` is the other half: a memo left by an EARLIER run
# (or anyone else's) on the probe scope is then named at the positive control, not three steps later.
id=""
retracted=no
on_exit() {
  local code=$?
  if [[ -n "$id" && $retracted != yes ]]; then
    pc memo-retract --scope "$scope" --id "$id"
    if [[ $rc == 0 ]]; then
      echo "CLEANUP retracted the probe memo id=$id, which this run sent and did not retract"
    else
      echo "CLEANUP FAILED: retracting the probe memo id=$id exited $rc — already retracted, or still live until its 1 h TTL; check it by hand" >&2
    fi
  fi
  rm -rf "$tmp"
  exit "$code"
}
trap on_exit EXIT
echo "workdir=$tmp"
mkdir -p "$tmp/home" "$tmp/state"
( umask 077
  printf 'CAIRN_URL=%s\nCAIRN_TOKEN=%s\nCAIRN_UI_URL=%s\nCAIRN_UI_TOKEN=%s\n' \
    "${in[CAIRN_URL]}" "${in[CAIRN_TOKEN]}" "$listener" "$probe_cred" >"$tmp/env" )

# The pass-through: network-reaching variables only, each only when set.
pass=()
for name in SSL_CERT_FILE SSL_CERT_DIR HTTP_PROXY HTTPS_PROXY NO_PROXY http_proxy https_proxy no_proxy; do
  [[ -n "${!name:-}" ]] && pass+=("$name=${!name}")
done

rc=0
out=""
err=""
pc() { # one client call, ISOLATED (the allowlist above)
  env -i PATH="$PATH" HOME="$tmp/home" XDG_STATE_HOME="$tmp/state" CAIRN_CONFIG="$tmp/env" "${pass[@]}" "$cairn_bin" "$@" >"$tmp/out" 2>"$tmp/err"
  rc=$?
  out="$(cat "$tmp/out")"
  err="$(cat "$tmp/err")"
}

passed=0
failed=0
step() { # step <name> <command…>
  local name="$1"; shift
  if "$@"; then
    echo "PASS $name"; passed=$((passed + 1))
  else
    echo "FAIL $name (client exit $rc)"; failed=$((failed + 1))
    echo "SUMMARY live-probe: steps=6 passed=$passed failed=$failed exit=1"
    exit 1
  fi
}

listener_401() {
  local code body
  code="$(env -i PATH="$PATH" "${pass[@]}" curl -sS -o "$tmp/401" -w '%{http_code}' "$listener/client/v1/memos?scope=$scope" 2>/dev/null)" || return 1
  body="$(cat "$tmp/401")"
  [[ "$code" == 401 && "$body" == '{"error":"unauthorized"}' ]]
}
step listener-401 listener_401

pc sync
step cache-sync eval '[[ $rc == 0 ]]'

nonce="$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')"
subject="live probe $nonce"
pc memo-send --scope "$scope" --subject "$subject" --body "a synthetic probe memo; it is retracted at once" --ttl 1h
id="$(sed -n 's/^cairn: memo sent — id=\([0-9]*\) .*/\1/p' <<<"$out")"
step send eval '[[ $rc == 0 && -n "$id" ]]'
# @after-send

pc memo-check --scope "$scope" --session "probe-$nonce"
blocks="$(grep -c '^<<<cairn-memo untrusted ' "$tmp/out" || true)"
# The block's own `count=` — exactly ONE memo, the probe's: a leftover from an earlier run is a FAIL
# here, named, rather than a FAIL at `new-session-silent` that reads as a retraction defect.
step check-one-block eval '[[ $rc == 0 && $blocks == 1 ]] && head -n1 "$tmp/out" | grep -q " count=1>>>\$" && grep -qF "|   subject: $subject" "$tmp/out" && grep -qx "memo-status=new" <<<"$(head -n1 "$tmp/err")"'

pc memo-retract --scope "$scope" --id "$id"
[[ $rc == 0 ]] && retracted=yes
step retract eval '[[ $rc == 0 ]]'

pc memo-check --scope "$scope" --session "probe-$nonce-new"
step new-session-silent eval '[[ $rc == 0 && ! -s "$tmp/out" ]] && [[ "$(head -n1 "$tmp/err")" == memo-status=none ]]'

echo "SUMMARY live-probe: steps=6 passed=$passed failed=$failed exit=0"
exit 0
