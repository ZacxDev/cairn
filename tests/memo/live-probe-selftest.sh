#!/usr/bin/env bash
# `tests/memo/live-probe.sh --self-test`: the live probe's own controls, against a LOCAL world
# (`tests/memo/world.sh` — the e2e world: a real pod, a real `cairn-ui` client listener over a real
# PostgreSQL, a real journal with a probe credential narrowed to read+write on `probe-notes`). S3 of
# claudedocs/plan-cairn-scope-mail.md, "S3 also" and "S3 — the probe isolation test".
#
# One `PASS <check>` / `FAIL <check>` line per check, then `SUMMARY live-probe-self-test: …`.
# Exit 0 all passed, 1 any failed, 2 could not vouch (the world did not come up, or the number of
# checks is not the declared one).
#
#   positive        the probe exits 0 with its six steps PASS;
#   inputs          exit 2 when the input file lacks CAIRN_UI_URL (EVEN WITH one exported — the
#                   environment is never read), lacks the probe credential, or is not 0600;
#   isolation       with EVERY name the client reads (`memotool env-reads` — the client's own
#                   ledger, never a typed list) exported as a DECOY — a config naming a token the
#                   listener never saw, a dead pod URL, a decoy token, a routing table sending the
#                   probe scope to an instance that does not exist, a decoy HOME holding a decoy
#                   cache — the probe still exits 0 and every decoy file is BYTE-UNCHANGED;
#   its mutants     each RED under the same decoys: isolating HOME only (revision 6's mechanism); a
#                   list of unsets that omits SUBSYSTEM_STORE_URL, one that omits
#                   SUBSYSTEM_STORE_TOKEN, one that omits CAIRN_ROUTES; and the probe credential read
#                   from the environment instead of the input file;
#   red controls    with the probe's memo already retracted, it fails AT the positive control; with
#                   the listener stopped, it fails AT the 401 control;
#   cleanup         its temporary directory is gone after EVERY run above.
set -uo pipefail

repo="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
src="$repo"
EXPECTED=16
work="$(mktemp -d "${TMPDIR:-/tmp}/cairn-live-probe-selftest-XXXXXX")"
# shellcheck source=tests/memo/world.sh
source "$repo/tests/memo/world.sh"
world_build
world_db
world_store_and_pod
world_journal
world_ui_start

probe="$repo/tests/memo/live-probe.sh"
input="$work/probe-input"
write_input() { # write_input [omit-key]
  ( umask 077
    {
      echo "# synthetic probe input, written by the self-test"
      [[ "${1:-}" == CAIRN_URL ]] || echo "CAIRN_URL=$pod_url"
      [[ "${1:-}" == CAIRN_TOKEN ]] || echo "CAIRN_TOKEN=$pod_token"
      [[ "${1:-}" == CAIRN_UI_URL ]] || echo "CAIRN_UI_URL=$api_url"
      [[ "${1:-}" == CAIRN_UI_TOKEN ]] || echo "CAIRN_UI_TOKEN=$probe_tok"
      echo "PROBE_SCOPE=probe-notes"
      echo "CAIRN_BIN=$work/bin/cairn"
    } >"$input" )
}

passed=0
failed=0
check() { # check <name> <command…>
  local name="$1"; shift
  if "$@"; then
    echo "PASS $name"; passed=$((passed + 1))
  else
    echo "FAIL $name"; failed=$((failed + 1))
    sed 's/^/  | /' <<<"${pout:0:2000}" >&2
  fi
}

workdirs=()
pout=""
prc=""
# run_probe <script> [VAR=value …] — the probe under `env -i` plus exactly the given variables.
run_probe() {
  local script="$1"; shift
  pout="$(env -i PATH="$PATH" "$@" "$script" "$input" 2>&1)"
  prc=$?
  local w
  w="$(sed -n 's/^workdir=//p' <<<"$pout" | head -n1)"
  [[ -n "$w" ]] && workdirs+=("$w")
}
steps_passed() { [[ "$(grep -c '^PASS ' <<<"$pout")" == "$1" ]]; }

# --- positive -------------------------------------------------------------------------------------
write_input
run_probe "$probe"
check probe-passes-against-the-local-world eval '[[ $prc == 0 ]] && steps_passed 6 && grep -qx "SUMMARY live-probe: steps=6 passed=6 failed=0 exit=0" <<<"$pout"'
check probe-prints-no-credential eval '! grep -qF "$probe_tok" <<<"$pout" && ! grep -qF "$pod_token" <<<"$pout"'

# --- inputs -----------------------------------------------------------------------------------------
write_input CAIRN_UI_URL
run_probe "$probe" CAIRN_UI_URL="$api_url"
check exit-2-when-CAIRN_UI_URL-is-absent-from-the-file-even-if-exported eval '[[ $prc == 2 ]] && grep -qF "has no CAIRN_UI_URL" <<<"$pout"'
write_input CAIRN_UI_TOKEN
run_probe "$probe" CAIRN_UI_TOKEN="$probe_tok"
check exit-2-when-the-probe-credential-is-absent-from-the-file eval '[[ $prc == 2 ]] && grep -qF "has no CAIRN_UI_TOKEN" <<<"$pout"'
write_input
chmod 644 "$input"
run_probe "$probe"
check exit-2-when-the-input-file-is-not-0600 eval '[[ $prc == 2 ]] && grep -qF "not 0600" <<<"$pout"'
chmod 600 "$input"

# --- isolation ------------------------------------------------------------------------------------
decoy="$work/decoy"
mkdir -p "$decoy/home/.config/subsystem-store" "$decoy/home/.cache/subsystem-store/probe-notes" "$decoy/state" "$decoy/mirror"
dead_port="$(free_port)"
decoy_token="decoy-token-the-listener-never-issued-0000000"
printf 'CAIRN_URL=http://127.0.0.1:%s\nCAIRN_TOKEN=%s\nCAIRN_UI_URL=http://127.0.0.1:%s\nCAIRN_UI_TOKEN=%s\n' \
  "$dead_port" "$decoy_token" "$dead_port" "$decoy_token" >"$decoy/home/.config/subsystem-store/env"
cp "$decoy/home/.config/subsystem-store/env" "$decoy/config.env"
printf '{"probe-notes": "nowhere"}\n' >"$decoy/routes.json"
printf 'a decoy cache file\n' >"$decoy/home/.cache/subsystem-store/probe-notes/decoy.md"
printf 'schema=decoy\n' >"$decoy/home/.cache/subsystem-store/.sync-stamp"
decoy_hash() { (cd "$decoy" && find . -print0 | sort -z | xargs -0 -I{} sh -c 'if [ -f "{}" ]; then printf "%s " "{}"; sha256sum <"{}"; else echo "{}/"; fi') | sha256sum; }

# The decoy for each name, by what the name configures. A name this table does not recognise still
# gets a decoy — a value that is wrong for anything that reads it.
decoy_for() {
  case "$1" in
    *_CONFIG) echo "$decoy/config.env" ;;
    *_UI_URL | *_URL) echo "http://127.0.0.1:$dead_port" ;;
    *_TOKEN) echo "$decoy_token" ;;
    *_ROUTES) echo "$decoy/routes.json" ;;
    HOME) echo "$decoy/home" ;;
    XDG_STATE_HOME) echo "$decoy/state" ;;
    *_MIRROR_ROOT) echo "$decoy/mirror" ;;
    *) echo "decoy-value-for-$1" ;;
  esac
}
# 🔴 THE NAMES COME FROM THE CLIENT'S OWN LEDGER, THROUGH A BUILT HELPER — never a typed list (the
# plan names that mutant): a name the client starts reading next year is decoyed here the day it is
# added to `client.EnvReads`, and `TestEveryEnvironmentReadIsInTheLedger` refuses a read the ledger
# does not name.
decoy_names() { "$work/bin/memotool" env-reads; }
decoys=()
decoyed=()
while read -r name; do
  [[ -n "$name" ]] || continue
  decoys+=("$name=$(decoy_for "$name")")
  decoyed+=("$name")
done < <(decoy_names)
before="$(decoy_hash)"
write_input
run_probe "$probe" "${decoys[@]}"
check isolation-probe-passes-with-every-client-read-name-decoyed eval '[[ $prc == 0 ]] && steps_passed 6'
check isolation-every-decoy-file-is-byte-unchanged eval '[[ "$(decoy_hash)" == "$before" ]]'
# The relationship, not the list: what was decoyed IS the ledger, re-read now. A typed copy that
# had fallen behind the ledger is red here.
check isolation-decoyed-names-are-exactly-the-clients-ledger eval '[[ "$(printf "%s\n" "${decoyed[@]}" | sort)" == "$(decoy_names | sort)" && ${#decoyed[@]} -ge 12 ]]'

# Each mutant is a COPY of the probe with ONE line changed; under the same decoys each must go RED.
pc_line='  env -i PATH="$PATH" HOME="$tmp/home" XDG_STATE_HOME="$tmp/state" CAIRN_CONFIG="$tmp/env" "${pass[@]}" "$cairn_bin" "$@" >"$tmp/out" 2>"$tmp/err"'
unsets='env -u CAIRN_CONFIG -u SUBSYSTEM_STORE_CONFIG -u CAIRN_URL -u SUBSYSTEM_STORE_URL -u CAIRN_TOKEN -u SUBSYSTEM_STORE_TOKEN -u CAIRN_ROUTES -u CAIRN_UI_URL -u CAIRN_UI_TOKEN -u CAIRN_MIRROR_ROOT'
mutant() { # mutant <name> <anchor> <replacement>
  python3 - "$probe" "$work/mut-$1.sh" "$2" "$3" <<'PY' || couldnt "mutant $1: the anchor is not exactly once in the probe"
import sys
src, dst, anchor, repl = sys.argv[1:5]
text = open(src).read()
if text.count(anchor) != 1:
    sys.exit(1)
open(dst, "w").write(text.replace(anchor, repl))
PY
  chmod +x "$work/mut-$1.sh"
  run_probe "$work/mut-$1.sh" "${decoys[@]}"
}
mutant home-only "$pc_line" '  env HOME="$tmp/home" "$cairn_bin" "$@" >"$tmp/out" 2>"$tmp/err"'
check isolation-mutant-home-only-is-red eval '[[ $prc == 1 ]] && grep -q "^FAIL cache-sync" <<<"$pout"'
mutant unsets-omit-store-url "$pc_line" "  ${unsets/-u SUBSYSTEM_STORE_URL /} HOME=\"\$tmp/home\" XDG_STATE_HOME=\"\$tmp/state\" CAIRN_CONFIG=\"\$tmp/env\" \"\$cairn_bin\" \"\$@\" >\"\$tmp/out\" 2>\"\$tmp/err\""
check isolation-mutant-unsets-omitting-SUBSYSTEM_STORE_URL-is-red eval '[[ $prc == 1 ]] && grep -q "^FAIL cache-sync" <<<"$pout"'
mutant unsets-omit-store-token "$pc_line" "  ${unsets/-u SUBSYSTEM_STORE_TOKEN /} HOME=\"\$tmp/home\" XDG_STATE_HOME=\"\$tmp/state\" CAIRN_CONFIG=\"\$tmp/env\" \"\$cairn_bin\" \"\$@\" >\"\$tmp/out\" 2>\"\$tmp/err\""
check isolation-mutant-unsets-omitting-SUBSYSTEM_STORE_TOKEN-is-red eval '[[ $prc == 1 ]] && grep -q "^FAIL cache-sync" <<<"$pout"'
mutant unsets-omit-routes "$pc_line" "  ${unsets/-u CAIRN_ROUTES /} HOME=\"\$tmp/home\" XDG_STATE_HOME=\"\$tmp/state\" CAIRN_CONFIG=\"\$tmp/env\" \"\$cairn_bin\" \"\$@\" >\"\$tmp/out\" 2>\"\$tmp/err\""
check isolation-mutant-unsets-omitting-CAIRN_ROUTES-is-red eval '[[ $prc == 1 ]] && grep -q "^FAIL send" <<<"$pout"'
mutant credential-from-env 'probe_cred="${in[CAIRN_UI_TOKEN]}"' 'probe_cred="${CAIRN_UI_TOKEN:-${in[CAIRN_UI_TOKEN]}}"'
check isolation-mutant-credential-from-the-environment-is-red eval '[[ $prc == 1 ]] && grep -q "^FAIL send" <<<"$pout"'

# --- red controls ----------------------------------------------------------------------------------
mutant retracted-before-check '# @after-send' 'pc memo-retract --scope "$scope" --id "$id"'
check red-at-the-positive-control-when-the-memo-is-already-retracted eval '[[ $prc == 1 ]] && grep -q "^FAIL check-one-block" <<<"$pout"'
kill "$ui_pid" 2>/dev/null
wait "$ui_pid" 2>/dev/null
run_probe "$probe"
check red-at-the-401-control-when-the-listener-is-stopped eval '[[ $prc == 1 ]] && grep -q "^FAIL listener-401" <<<"$pout"'

# --- cleanup -----------------------------------------------------------------------------------------
left=()
for w in "${workdirs[@]}"; do [[ -e "$w" ]] && left+=("$w"); done
check the-probes-temporary-directory-is-gone-after-every-run eval '[[ ${#workdirs[@]} -ge 9 && ${#left[@]} -eq 0 ]]'

echo "SUMMARY live-probe-self-test: passed=$passed failed=$failed expected=$EXPECTED"
[[ $((passed + failed)) -eq $EXPECTED ]] || couldnt "$((passed + failed)) checks ran, $EXPECTED are declared"
[[ $failed -eq 0 ]] && exit 0
exit 1
