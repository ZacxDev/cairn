# shellcheck shell=bash
# The scope-mail end-to-end WORLD (S3 of claudedocs/plan-cairn-scope-mail.md), SOURCED — not run —
# by `tests/memo/e2e.sh` and by `tests/memo/live-probe.sh --self-test`. One synthetic world, built
# at run time from the REAL binaries of the tree at `$src`:
#
#   - a store with `alpha-notes`, `beta-notes` and `probe-notes`, served by a `cairn-server` pod
#     whose token file holds ONE shared row over all three (the deployment's shape: STEP 1);
#   - a control journal, written by `cairn-server`'s own operator commands, with four principals:
#     `writer-a` (OWNER of the project holding the three scopes, so ADMIN on `alpha-notes` — verb
#     narrowing has an `admin` to strip), `reader-b` (a READ grant on `alpha-notes` only),
#     `outsider-c` (its own project, nothing on `alpha-notes`), `admin-d` (ADMIN member of
#     writer-a's project);
#   - credentials issued the way decision 16 issues them — `-issue-credential -narrow-scopes …
#     -narrow-verbs read,write` for every agent credential (decision 17): agents@host-a and
#     agents@host-c (alpha), agents@host-b (beta), agents@host-r (reader-b, alpha), one un-narrowed
#     credential for outsider-c, admin@host-d (alpha, read+write+admin, explicitly), and the live
#     probe's probe@host-p (writer-a, probe-notes, read+write);
#   - a scratch PostgreSQL SCHEMA (from `$CAIRN_PGTEST_DSN`, or a throwaway server this script
#     starts when `initdb` is on PATH), and `cairn-ui` booted with `-db-dsn`, `-control-journal`
#     and `-client-api-addr`, and NO token file — as on the deployment.
#
# Every token is generated per run from the OS's randomness and never leaves `$work`. Every name,
# host and date is synthetic. A prerequisite that is missing is EXIT 2 ("could not vouch") — never
# a skip and never 0.
#
# The caller sets `src` (the tree to BUILD from) and `work` (a fresh temp dir it owns) and defines
# nothing else; this file defines `couldnt`, `cleanup` (installed as the EXIT trap) and the world.

pids=()
pg_started=""
schema=""
base_dsn=""
couldnt() { echo "COULD NOT VOUCH: $*" >&2; exit 2; }
cleanup() {
  for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
  for p in "${pids[@]}"; do wait "$p" 2>/dev/null || true; done
  if [[ -n "$schema" && -n "$base_dsn" ]]; then
    MEMOTOOL_DSN="$base_dsn" "$work/bin/memotool" sql exec "DROP SCHEMA IF EXISTS \"$schema\" CASCADE" >/dev/null 2>&1 || true
  fi
  # 🔴 STOPPED BY DATA DIRECTORY, NEVER BY A PROCESS-NAME PATTERN — the `tests/pgtest/run.sh` rule.
  if [[ -n "$pg_started" ]]; then pg_ctl -D "$pg_started" -m immediate stop >/dev/null 2>&1 || true; fi
  [[ -n "${pg_sock:-}" ]] && rm -rf "$pg_sock"
  rm -rf "$work"
}
trap cleanup EXIT

for tool in go curl python3; do
  command -v "$tool" >/dev/null 2>&1 || couldnt "\`$tool\` is not on PATH — this check needs it and refuses to skip"
done

world_build() {
  mkdir -p "$work/bin"
  local b
  for b in cairn cairn-server cairn-ui; do
    go build -C "$src" -o "$work/bin/$b" "./cmd/$b" || couldnt "$b did not build"
  done
  go build -C "$src" -o "$work/bin/memotool" ./tests/memo/memotool || couldnt "memotool did not build"
}

# world_db sets $base_dsn and $dsn (the run's own schema). The DATABASE comes from
# `$CAIRN_PGTEST_DSN` (the `pgtest` CI job's pinned service), or from a throwaway server started
# here when `initdb` is available; with neither, exit 2.
world_db() {
  if [[ -n "${CAIRN_PGTEST_DSN:-}" ]]; then
    base_dsn="$CAIRN_PGTEST_DSN"
  else
    local bin
    for bin in initdb pg_ctl createdb postgres; do
      command -v "$bin" >/dev/null 2>&1 || couldnt "CAIRN_PGTEST_DSN is unset and \`$bin\` is not on PATH — run inside the dev shell (nix develop) or set CAIRN_PGTEST_DSN"
    done
    # 🔴 THE SOCKET DIRECTORY IS SHORT ON PURPOSE: a Unix socket path is capped at 107 bytes, and a
    # scratch `TMPDIR` can be longer than that on its own (measured: the server refused to start).
    pg_sock="$(mktemp -d /tmp/cme-XXXXXX)"
    initdb -D "$work/pgdata" -A trust -U cairn_e2e --no-sync >"$work/initdb.log" 2>&1 || couldnt "initdb failed: $(tail -5 "$work/initdb.log")"
    pg_ctl -D "$work/pgdata" -o "-k '$pg_sock' -c listen_addresses='' -c fsync=off" -l "$work/pg.log" -w start >/dev/null 2>&1 ||
      couldnt "the throwaway server did not start: $(tail -5 "$work/pg.log")"
    pg_started="$work/pgdata"
    createdb -h "$pg_sock" -U cairn_e2e cairn_e2e >>"$work/pg.log" 2>&1 || couldnt "createdb failed"
    base_dsn="host=$pg_sock user=cairn_e2e dbname=cairn_e2e sslmode=disable"
  fi
  schema="memoe2e_$$_$RANDOM"
  MEMOTOOL_DSN="$base_dsn" "$work/bin/memotool" sql exec "CREATE SCHEMA \"$schema\"" >/dev/null ||
    couldnt "could not create the run's schema (is the database reachable?)"
  if [[ "$base_dsn" == postgres://* || "$base_dsn" == postgresql://* ]]; then
    local sep="?"
    [[ "$base_dsn" == *\?* ]] && sep="&"
    dsn="${base_dsn}${sep}options=-csearch_path%3D${schema}"
  else
    dsn="$base_dsn options=-csearch_path=$schema"
  fi
}

sql() { MEMOTOOL_DSN="$dsn" "$work/bin/memotool" sql "$@"; }

free_port() { python3 -c 'import socket
s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'; }

token() { python3 -c 'import secrets; print(secrets.token_urlsafe(32))'; }

# A clean environment for every SERVER binary: no ambient `CAIRN_*` or `SUBSYSTEM_STORE_*` reaches
# them. An ARRAY so a backgrounded binary's `$!` is the binary itself (`env` execs it).
clean_env=(env -i PATH="$PATH" HOME="$work" CAIRN_MAX_FAILURES=1000000)

world_store_and_pod() {
  store="$work/store"
  local s
  for s in alpha-notes beta-notes probe-notes; do
    mkdir -p "$store/$s"
    printf -- '---\nservice: gadget\nscope: %s\n---\n\n## What it is\nsynthetic.\n\n## Nuance / work-history\n- 2000-01-01: seeded, unsigned\n' \
      "$s" >"$store/$s/gadget.md"
  done
  pod_token="$(token)"
  printf '%s e2e-operator alpha-notes,beta-notes,probe-notes\n' "$pod_token" >"$work/pod-tokens"
  local port; port="$(free_port)"
  # The pod refuses to start without a trusted-proxy set; TEST-NET-1, which loopback is not in.
  "${clean_env[@]}" CAIRN_TRUSTED_PROXIES=192.0.2.0/24 "$work/bin/cairn-server" --store "$store" --host 127.0.0.1 \
    --port "$port" --token-file "$work/pod-tokens" >"$work/pod.log" 2>&1 &
  pids+=("$!")
  local _
  for _ in $(seq 1 200); do
    if curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then
      pod_url="http://127.0.0.1:$port"
      return 0
    fi
    sleep 0.05
  done
  couldnt "the pod did not come up: $(tail -5 "$work/pod.log")"
}

journal_cmd() { "${clean_env[@]}" CAIRN_CONTROL_JOURNAL="$journal" "$work/bin/cairn-server" -store "$store" "$@"; }

world_journal() {
  journal="$work/control/journal.jsonl"
  mkdir -p "$work/control"
  local line
  # create_user <subject> <display> <project> [scopes] → "usr_… prj_… <scope-map>". `-create-user`
  # always creates a NEW project for the user (measured: no project name is refused), so reader-b
  # and admin-d each own an EMPTY one and reach alpha-notes only by the grant / membership below.
  create_user() {
    local extra=(-project "$3")
    [[ -n "${4:-}" ]] && extra+=(-scopes "$4")
    line="$(journal_cmd -create-user -provider e2e-provider -subject "$1" -email "$1@e2e.example.invalid" \
      -display-name "$2" "${extra[@]}" 2>>"$work/journal.log")" || couldnt "create-user $2: $(tail -3 "$work/journal.log")"
    python3 - "$line" <<'PY'
import re, sys
line = sys.argv[1]
user = re.search(r"\buser=(usr_\S+)", line)
project = re.search(r"\bproject=(\S*)", line)
scopes = ",".join(f"{n}={i}" for i, n in re.findall(r"(scp_[^:,\s]+):([a-z0-9-]+)", line))
if not user:
    sys.exit(1)
print(user.group(1), project.group(1) if project else "-", scopes or "-")
PY
  }
  read -r user_a project_a scope_map < <(create_user subject-e2e-a writer-a project-e2e-a alpha-notes,beta-notes,probe-notes) || couldnt "writer-a"
  read -r user_b _ _ < <(create_user subject-e2e-b reader-b project-e2e-b) || couldnt "reader-b"
  read -r user_c _ _ < <(create_user subject-e2e-c outsider-c project-e2e-c gamma-notes) || couldnt "outsider-c"
  read -r user_d _ _ < <(create_user subject-e2e-d admin-d project-e2e-d) || couldnt "admin-d"
  scope_alpha="$(tr ',' '\n' <<<"$scope_map" | sed -n 's/^alpha-notes=//p')"
  scope_beta="$(tr ',' '\n' <<<"$scope_map" | sed -n 's/^beta-notes=//p')"
  scope_probe="$(tr ',' '\n' <<<"$scope_map" | sed -n 's/^probe-notes=//p')"
  [[ "$user_a" == usr_* && "$user_b" == usr_* && "$user_c" == usr_* && "$user_d" == usr_* && "$scope_alpha" == scp_* &&
     "$scope_beta" == scp_* && "$scope_probe" == scp_* ]] || couldnt "the world's principals/scopes were not created: $scope_map"
  journal_cmd -set-member -member-project "$project_a" -member-user "$user_d" -member-role admin >>"$work/journal.log" 2>&1 ||
    couldnt "set-member admin-d: $(tail -3 "$work/journal.log")"
  grant_b="grt_e2ereaderalpha"
  "$work/bin/memotool" journal-append "$journal" \
    "{\"kind\":\"granted\",\"at\":\"2000-01-01T00:00:00Z\",\"grant_id\":\"$grant_b\",\"subject_kind\":\"user\",\"subject_id\":\"$user_b\",\"object_kind\":\"scope\",\"object_id\":\"$scope_alpha\",\"verbs\":[\"read\"]}" \
    >>"$work/journal.log" 2>&1 || couldnt "the read grant for reader-b: $(tail -3 "$work/journal.log")"

  # issue <var> <principal> <label> [scope verbs] — sets ${var}_tok and ${var}_crd
  issue() {
    local var="$1" principal="$2" label="$3" extra=()
    [[ -n "${4:-}" ]] && extra=(-narrow-scopes "$4" -narrow-verbs "$5")
    journal_cmd -issue-credential -principal "$principal" -label "$label" -token-out "$work/tok-$var" "${extra[@]}" \
      2>"$work/issue-$var.log" >/dev/null || couldnt "issue $label: $(tail -3 "$work/issue-$var.log")"
    printf -v "${var}_tok" '%s' "$(head -n1 "$work/tok-$var")"
    printf -v "${var}_crd" '%s' "$(grep -o 'credential=crd_[^ ]*' "$work/issue-$var.log" | head -n1 | cut -d= -f2)"
  }
  issue hosta "$user_a" agents@host-a "$scope_alpha" read,write
  issue hostc "$user_a" agents@host-c "$scope_alpha" read,write
  issue hostb "$user_a" agents@host-b "$scope_beta" read,write
  issue hostr "$user_b" agents@host-r "$scope_alpha" read,write
  issue outsider "$user_c" outsider@host-o
  issue hostd "$user_d" admin@host-d "$scope_alpha" read,write,admin
  issue probe "$user_a" probe@host-p "$scope_probe" read,write
  local v
  for v in hosta hostc hostb hostr outsider hostd probe; do
    local t="${v}_tok" c="${v}_crd"
    [[ -n "${!t}" && "${!c}" == crd_* ]] || couldnt "credential $v was not issued (token or id missing)"
  done
}

# world_ui_start sets $ui_url (browser) and $api_url (the client listener).
world_ui_start() {
  local port aport
  port="$(free_port)"
  aport="$(free_port)"
  "${clean_env[@]}" "$work/bin/cairn-ui" -store "$store" -control-journal "$journal" -db-dsn "$dsn" \
    -token-file "$work/absent-token-file" -host 127.0.0.1 -port "$port" -client-api-addr "127.0.0.1:$aport" \
    >"$work/ui.log" 2>&1 &
  ui_pid=$!
  pids+=("$ui_pid")
  local _
  for _ in $(seq 1 400); do
    if [[ "$(curl -fsS "http://127.0.0.1:$port/healthz" 2>/dev/null)" == "ok" ]] &&
       curl -sS -o /dev/null "http://127.0.0.1:$aport/client/v1/memos" 2>/dev/null; then
      [[ "$(readlink -f "/proc/$ui_pid/exe")" == "$(readlink -f "$work/bin/cairn-ui")" ]] ||
        couldnt "pid $ui_pid is not the cairn-ui binary; killing it would orphan the server"
      ui_url="http://127.0.0.1:$port"
      api_url="http://127.0.0.1:$aport"
      grep -qF "client API on 127.0.0.1:$aport (7 route(s), machine tokens only)" "$work/ui.log" ||
        couldnt "cairn-ui did not announce the client API listener: $(tail -5 "$work/ui.log")"
      return 0
    fi
    kill -0 "$ui_pid" 2>/dev/null || break
    sleep 0.05
  done
  couldnt "cairn-ui did not come up: $(tail -20 "$work/ui.log")"
}

# client_home <who> <ui-token> [ui-url] — one HOST: its own HOME, config file (the pod's shared row
# and this host's memo credential) and state dir; then a `sync`, so its cache holds the scopes the
# pod serves (which is what `memo-check` asks before any request, decision 7).
client_home() {
  local who="$1" tok="$2" url="${3:-$api_url}" h="$work/h-$1"
  mkdir -p "$h/.config/subsystem-store" "$h/state"
  printf 'CAIRN_URL=%s\nCAIRN_TOKEN=%s\nCAIRN_UI_URL=%s\nCAIRN_UI_TOKEN=%s\n' "$pod_url" "$pod_token" "$url" "$tok" \
    >"$h/.config/subsystem-store/env"
  chmod 600 "$h/.config/subsystem-store/env"
  cairn "$who" sync
  [[ "$rc" == 0 && -d "$h/.cache/subsystem-store/alpha-notes" ]] || couldnt "client $who could not sync its cache (rc=$rc): $err"
}

# cairn <who> args… — the BUILT client as host <who>, under `env -i`: only PATH, its own HOME and its
# own XDG_STATE_HOME. Leaves $rc, $out (stdout, also in $work/stdout), $err (stderr) and $status_line.
out=""
err=""
rc=""
status_line=""
cairn() {
  local who="$1"; shift
  env -i PATH="$PATH" HOME="$work/h-$who" XDG_STATE_HOME="$work/h-$who/state" \
    "$work/bin/cairn" "$@" >"$work/stdout" 2>"$work/stderr"
  rc=$?
  out="$(cat "$work/stdout")"
  err="$(cat "$work/stderr")"
  status_line="$(head -n1 "$work/stderr")"
}

world_up() {
  world_build
  world_db
  world_store_and_pod
  world_journal
  world_ui_start
  client_home host-a "$hosta_tok"
  client_home host-c "$hostc_tok"
  client_home host-b "$hostb_tok"
  client_home host-r "$hostr_tok"
  client_home outsider "$outsider_tok"
  client_home host-d "$hostd_tok"
}
