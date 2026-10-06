#!/usr/bin/env bash
# The arcs/sessions CLOSING CHECK (slice S5 of claudedocs/plan-cairn-arcs-sessions.md), end to end
# over the REAL binaries: it builds `cmd/cairn-server` and `cmd/cairn`, seeds a synthetic store and
# an arc journal OUTSIDE it, writes trailered bullets with `cairn append`, registers arcs with
# `cairn arc-register`, and asserts what `sessions`, `arcs`, `arc-show` and `arcs --check` answer —
# including that a principal WITHOUT the home scope gets the not-found answer and sees neither the
# slug nor the home.
#
#   usage: tests/arcs/e2e.sh              # the check: exits 0 iff every assertion PASSes
#          tests/arcs/e2e.sh --self-test  # the negative control: exits 0 iff every SABOTAGED
#                                         # build turns its named assertion RED
#
# Output: one `PASS <name>` / `FAIL <name>` line per assertion, then `SUMMARY e2e: passed=N
# failed=M expected=E`. Exit 0 all passed, 1 any failed, 2 COULD NOT VOUCH (the build or the pods
# did not come up, or the assertion count is not the declared one — a run that asserted less than
# it declares is a run about less than it claims).
#
# 🔴 WHY A SELF-TEST AND NOT ONLY A GREEN. Every assertion here is a `grep` or an exit-code compare,
# and a harness wired to nothing passes both. `--self-test` copies the source tree, breaks it in a
# named way (the visibility rule disabled; the check unable to report a finding), rebuilds, reruns
# this script against the broken build and requires it to go RED on the assertion that names the
# break. A sabotage that stays green is a blind spot of this script, and the self-test exits 1.
#
# Everything is synthetic: the scope names, slugs, session ids and identities below are made up;
# the tokens are generated per run and never written outside the run's temp directory.
set -uo pipefail

repo="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
# `E2E_SOURCE` is the tree the binaries are BUILT from; the self-test points it at a sabotaged copy.
src="${E2E_SOURCE:-$repo}"

# The number of assertions a full run makes. 🔴 PINNED, so an assertion that silently stopped
# running (a `return` in the wrong place, a block skipped) is exit 2 rather than a smaller green.
EXPECTED=32

if [[ "${1:-}" == "--self-test" ]]; then
  work="$(mktemp -d "${TMPDIR:-/tmp}/cairn-arcs-e2e-selftest-XXXXXX")"
  trap 'rm -rf "$work"' EXIT
  sabotaged=0
  caught=0
  # sabotage <name> <file> <exact line> <replacement> <assertion that must FAIL>
  sabotage() {
    local name="$1" file="$2" line="$3" replacement="$4" must_fail="$5"
    local tree="$work/$name"
    mkdir -p "$tree"
    # Tracked AND new-untracked files, so an uncommitted change under test is in the copy; no
    # `.git` is copied, so nothing in the copy can reach the real repository.
    (cd "$src" && git ls-files -z -co --exclude-standard | xargs -0 cp --parents -t "$tree" 2>/dev/null)
    local count
    count="$(grep -cxF -- "$line" "$tree/$file" || true)"
    if [[ "$count" != "1" ]]; then
      echo "HARNESS ERROR: sabotage $name: the anchor occurs $count times in $file (need exactly 1)" >&2
      exit 2
    fi
    python3 - "$tree/$file" "$line" "$replacement" <<'PY'
import sys
path, line, replacement = sys.argv[1], sys.argv[2], sys.argv[3]
text = open(path, encoding="utf-8").read().split("\n")
text = [replacement if l == line else l for l in text]
open(path, "w", encoding="utf-8").write("\n".join(text))
PY
    sabotaged=$((sabotaged + 1))
    local out rc
    out="$(E2E_SOURCE="$tree" "$repo/tests/arcs/e2e.sh" 2>&1)"
    rc=$?
    if [[ $rc -eq 2 ]]; then
      echo "$out" | tail -20
      echo "HARNESS ERROR: sabotage $name: the sabotaged run could not vouch (exit 2)" >&2
      exit 2
    fi
    if [[ $rc -eq 1 ]] && grep -qxF "FAIL $must_fail" <<<"$out"; then
      caught=$((caught + 1))
      echo "CAUGHT $name: exit $rc, and FAIL $must_fail ($(grep -c '^FAIL ' <<<"$out") assertion(s) red)"
    else
      echo "MISSED $name: exit $rc; 'FAIL $must_fail' $(grep -qxF "FAIL $must_fail" <<<"$out" && echo present || echo ABSENT)"
    fi
  }
  # 1. The visibility rule (operator decision Q1) disabled in BOTH arc answers: an arc homed in a
  #    scope the caller cannot read must then leak, and the closing condition's assertion go red.
  sabotage visibility-off internal/report/arcs.go \
    $'\tif err != nil || !found || !visible.Allows(key.Home) {' \
    $'\tif err != nil || !found {' \
    "outsider-arc-show-equals-never-registered"
  # 1b. The same rule disabled in the LISTING only: an alpha-homed arc whose member wrote in beta
  #     must then be listed (inferred) to a caller who cannot read alpha.
  sabotage listing-visibility-off internal/report/arcs.go \
    $'\t\tif !visible.Allows(reg.Home) {' \
    $'\t\tif false {' \
    "outsider-sees-beta-arc-not-alpha-homed"
  # 2. The check unable to report a finding: every state is "clean", so the 9 must go red.
  sabotage check-never-finds internal/report/arcscheck.go \
    $'\tif len(rep.Findings) > 0 {' \
    $'\tif false {' \
    "check-declared-scope-absent-exits-9"
  echo "SUMMARY e2e-self-test: sabotaged=$sabotaged caught=$caught"
  [[ $sabotaged -gt 0 && $caught -eq $sabotaged ]] && exit 0
  exit 1
fi

work="$(mktemp -d "${TMPDIR:-/tmp}/cairn-arcs-e2e-XXXXXX")"
pids=()
cleanup() {
  for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
  rm -rf "$work"
}
trap cleanup EXIT

couldnt() { echo "COULD NOT VOUCH: $*" >&2; exit 2; }

mkdir -p "$work/bin"
go build -C "$src" -o "$work/bin/cairn-server" ./cmd/cairn-server || couldnt "cairn-server did not build"
go build -C "$src" -o "$work/bin/cairn" ./cmd/cairn || couldnt "cairn did not build"

# --- the synthetic world -----------------------------------------------------------------------
store="$work/store"
entry() { # entry <scope> <service>
  mkdir -p "$store/$1"
  printf -- '---\nservice: %s\nscope: %s\n---\n\n## What it is\nsynthetic.\n\n## Nuance / work-history\n- 2000-01-01: seeded, unsigned\n' \
    "$2" "$1" >"$store/$1/$2.md"
}
entry alpha-notes gadget-one
entry beta-notes widget-two
entry gamma-notes doodad-three

token() { python3 -c 'import secrets; print(secrets.token_urlsafe(32))'; }
writer_token="$(token)"
outsider_token="$(token)"
# The writer may read and write alpha, beta and omega-notes — omega has NO directory, which is how a
# registration can name a scope that does not exist (a grant may name a scope before its first
# entry). The outsider reads beta only: it cannot read alpha, the home of the arc under test.
printf '%s e2e-writer alpha-notes,beta-notes,omega-notes\n%s e2e-outsider beta-notes\n' \
  "$writer_token" "$outsider_token" >"$work/tokens"
mkdir -p "$work/journal"
journal="$work/journal/arcs.jsonl"

free_port() { python3 -c 'import socket
s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'; }

# start_pod <log> [extra args...] sets $url and $pod. 🔴 NOT CALLED IN A `$(…)`: a command
# substitution is a subshell, so the pid it recorded would never reach `cleanup` and the pod would
# outlive the run — measured on this script's first draft.
pod=""
start_pod() {
  local log="$1"; shift
  local port; port="$(free_port)"
  # The pod refuses to start without a trusted-proxy set. It names a documentation range
  # (TEST-NET-1) that the loopback client is NOT in, so the pod reads the peer address directly
  # rather than waiting for a forwarding header no proxy will send.
  CAIRN_TRUSTED_PROXIES=192.0.2.0/24 CAIRN_MAX_FAILURES=1000000 "$work/bin/cairn-server" --store "$store" --host 127.0.0.1 \
    --port "$port" --token-file "$work/tokens" "$@" >"$log" 2>&1 &
  pod=$!
  pids+=("$pod")
  for _ in $(seq 1 200); do
    if curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then
      url="http://127.0.0.1:$port"
      return 0
    fi
    kill -0 "$pod" 2>/dev/null || break
    sleep 0.05
  done
  cat "$log" >&2
  return 1
}
stop_pod() { kill "$pod" 2>/dev/null; wait "$pod" 2>/dev/null; }

# --- the client --------------------------------------------------------------------------------
# One HOME per principal, so the two caches can never mix. `CAIRN_CONFIG` names a file that does not
# exist and `CAIRN_ROUTES` is empty, so the URL and token come from the environment alone and this
# host's own configuration cannot leak in.
url=""
cairn() { # cairn <writer|outsider> args... ; leaves $out, $err, $rc
  local who="$1"; shift
  local tok="$writer_token"
  [[ "$who" == "outsider" ]] && tok="$outsider_token"
  mkdir -p "$work/home-$who"
  out="$(env -u SUBSYSTEM_STORE_URL -u SUBSYSTEM_STORE_TOKEN -u CAIRN_MIRROR_ROOT \
    HOME="$work/home-$who" CAIRN_CONFIG="$work/home-$who/no-config" CAIRN_ROUTES="" \
    CAIRN_URL="$url" CAIRN_TOKEN="$tok" "$work/bin/cairn" "$@" 2>"$work/stderr")"
  rc=$?
  err="$(cat "$work/stderr")"
}

passed=0
failed=0
assert() { # assert <name> <command...>
  local name="$1"; shift
  if "$@"; then
    echo "PASS $name"; passed=$((passed + 1))
  else
    echo "FAIL $name"; failed=$((failed + 1))
    { echo "  rc=$rc"; echo "  --- stdout"; sed 's/^/  | /' <<<"$out"; echo "  --- stderr"; sed 's/^/  | /' <<<"$err"; } >&2
  fi
}
has() { grep -qF -- "$1" <<<"$out"; }
lacks() { ! grep -qF -- "$1" <<<"$out"; }
rc_is() { [[ "$rc" == "$1" ]]; }

# --- 1. the journal UNCONFIGURED: the designed off state, never "no arc" -----------------------
start_pod "$work/pod-off.log" || couldnt "the journal-less pod did not come up"
cairn writer arcs --scope alpha-notes
assert off-arcs-answers-unconfigured-at-0 eval 'rc_is 0 && has "status=registrations-unconfigured" && has "REGISTRATIONS ARE NOT CONFIGURED ON THIS POD"'
cairn writer arcs --check --scope alpha-notes
assert off-check-could-not-look-10 eval 'rc_is 10 && has "status=registrations-unconfigured" && has "Nothing was checked (exit 10: could not look)."'
stop_pod

# --- 2. the configured pod ---------------------------------------------------------------------
start_pod "$work/pod.log" --arc-journal "$journal" || couldnt "the pod did not come up"

cairn writer arcs --check --scope alpha-notes
assert empty-registry-check-clean-0 eval 'rc_is 0 && has "status=arcs-check-clean" && has "arcs checked: 0 (open 0 · closed 0 · unknown 0)"'

# Three sessions write: two in alpha, one in beta.
cairn writer append --scope alpha-notes --ref gadget-one --text "rotated the gadget lease" --session s-e2e-0001
assert append-alpha-s1 eval 'rc_is 0'
cairn writer append --scope alpha-notes --ref gadget-one --text "pinned the gadget clock" --session s-e2e-0002
assert append-alpha-s2 eval 'rc_is 0'
cairn writer append --scope beta-notes --ref widget-two --text "drained the widget queue" --session s-e2e-0003
assert append-beta-s3 eval 'rc_is 0'

cairn writer sessions --scope alpha-notes
assert sessions-lists-both-writers eval 'rc_is 0 && has "status=sessions-listed scope=alpha-notes" && has "s-e2e-0001" && has "s-e2e-0002" && lacks "s-e2e-0003"'
assert sessions-carries-coverage eval 'has "coverage: writes measured from entry trailers · reads NOT recorded (not collected in this phase)" && has "attributed: 2 of 3 bullets carry a write trailer (1 have none — their writers are NOT listed)"'

# Arc 1 homed in alpha, declaring alpha; its members are s-e2e-0001 (wrote alpha) and s-e2e-0003
# (wrote beta — so the arc is INFERRED in beta). Arc 2 homed in beta, declaring beta; its member
# s-e2e-0002 wrote alpha, so it is INFERRED in alpha.
cat >"$work/arc1.json" <<'JSON'
{"schema": 1, "status": "open", "closing_kind": "check", "declared_scopes": ["alpha-notes"],
 "writers_measured": true, "readers_measured": false, "commits_total": 4, "commits_unstamped": 1,
 "reported_at": "2000-01-02T03:04:05Z",
 "members": [{"session": "s-e2e-0001", "role": "originated", "first_seen": "2000-01-01T00:00:00Z"},
             {"session": "s-e2e-0003", "role": "wrote", "first_seen": ""}]}
JSON
cat >"$work/arc2.json" <<'JSON'
{"schema": 1, "declared_scopes": ["beta-notes"],
 "members": [{"session": "s-e2e-0002", "role": "wrote", "first_seen": ""}]}
JSON
cairn writer arc-register --scope alpha-notes --slug e2e-rollout --from "$work/arc1.json"
assert register-home-alpha eval 'rc_is 0 && has "arc-registered: alpha-notes/e2e-rollout · members=2 · unjoinable=0"'
cairn writer arc-register --scope beta-notes --slug e2e-beta-fix --from "$work/arc2.json"
assert register-home-beta eval 'rc_is 0 && has "arc-registered: beta-notes/e2e-beta-fix · members=1 · unjoinable=0"'

cairn writer arcs --scope alpha-notes
assert arcs-lists-declared-and-inferred eval 'rc_is 0 && has "status=arcs-listed scope=alpha-notes" && has "- alpha-notes/e2e-rollout · declared · status open · closing check · 2 members" && has "- beta-notes/e2e-beta-fix · inferred (s-e2e-0002 wrote here) · status unknown · closing none · 1 member"'
assert arcs-carries-coverage eval 'has "coverage: writes measured from entry trailers" && has "attributed: 2 of 3 bullets in \`alpha-notes/\` carry a write trailer" && has "sessions: 2 of 2 writing sessions here belong to an arc listed below"'

cairn writer arc-show --scope alpha-notes --slug e2e-rollout
assert arc-show-content eval 'rc_is 0 && has "status=arc-found home=alpha-notes slug=e2e-rollout" && has "status: open" && has "tooling coverage: 1 of 4 commits carry no session id · writers: measured · readers: NOT measured"'
assert arc-show-members-and-writes eval 'has "- s-e2e-0001 · originated · first seen 2000-01-01T00:00:00Z · wrote in: alpha-notes" && has "- s-e2e-0003 · wrote · first seen unknown · wrote in: beta-notes"'

# --- 3. THE CLOSING CONDITION: a principal without the home scope sees nothing of the arc --------
cairn outsider arc-show --scope alpha-notes --slug never-registered
never="$out"
cairn outsider arc-show --scope alpha-notes --slug e2e-rollout
assert outsider-arc-show-not-found eval 'rc_is 0 && has "status=arc-unregistered" && lacks "e2e-rollout" && lacks "alpha-notes"'
assert outsider-arc-show-equals-never-registered eval '[[ "$out" == "$never" ]]'
cairn outsider arcs --scope alpha-notes
assert outsider-arcs-scope-absent eval 'rc_is 0 && has "status=scope-absent scope=alpha-notes" && lacks "e2e-rollout"'
# The POSITIVE CONTROL for the outsider: a live token that DOES see an arc (beta's own), and still
# not the alpha-homed arc whose member wrote in beta — the writer sees that one inferred there.
cairn outsider arcs --scope beta-notes
assert outsider-sees-beta-arc-not-alpha-homed eval 'rc_is 0 && has "- beta-notes/e2e-beta-fix · declared" && lacks "e2e-rollout"'
cairn writer arcs --scope beta-notes
assert writer-sees-alpha-homed-arc-inferred-in-beta eval 'rc_is 0 && has "- alpha-notes/e2e-rollout · inferred (s-e2e-0003 wrote here)"'
cairn outsider sessions --scope alpha-notes
assert outsider-sessions-scope-absent eval 'rc_is 0 && has "status=scope-absent" && lacks "s-e2e-0001"'

# --- 4. arcs --check on constructed states -------------------------------------------------------
cairn writer arcs --check --scope alpha-notes
assert check-clean-exits-0 eval 'rc_is 0 && has "status=arcs-check-clean" && has "arcs checked: 1 (open 1 · closed 0 · unknown 0)"'
cairn writer arcs --check --all-scopes --scope alpha-notes
assert check-all-scopes-clean-exits-0 eval 'rc_is 0 && has "checked: every arc visible to you" && has "arcs checked: 2 (open 1 · closed 0 · unknown 1)"'
assert check-coverage-not-findings eval 'has "members: 3 of 3 member sessions wrote an attributed bullet in a scope readable to you (0 did not — coverage, not a finding)" && has "sessions: 3 of 3 writing sessions in the scopes readable to you belong to an arc visible to you (0 in no arc — coverage, not a finding)"'

# A declared scope the writer may read that does not exist (omega-notes has no directory).
cat >"$work/arc3.json" <<'JSON'
{"schema": 1, "status": "closed", "declared_scopes": ["omega-notes"], "members": []}
JSON
cairn writer arc-register --scope alpha-notes --slug e2e-orphan --from "$work/arc3.json"
assert register-orphan eval 'rc_is 0'
cairn writer arcs --check --scope alpha-notes
assert check-declared-scope-absent-exits-9 eval 'rc_is 9 && has "status=arcs-check-findings" && has "- declared-scope-absent · alpha-notes/e2e-orphan · it declares \`omega-notes/\`, which is readable to you and does not exist in the store"'
# The outsider's check over everything IT can see: the orphan is homed in alpha, so it is neither
# checked nor named, and the outsider's registry is clean.
cairn outsider arcs --check --all-scopes --scope beta-notes
assert outsider-check-does-not-see-alpha-orphan eval 'rc_is 0 && has "status=arcs-check-clean" && lacks "e2e-orphan" && lacks "omega-notes" && has "arcs checked: 1 "'

# A HOME scope that does not exist.
cat >"$work/arc4.json" <<'JSON'
{"schema": 1, "members": []}
JSON
cairn writer arc-register --scope omega-notes --slug e2e-lost-home --from "$work/arc4.json"
assert register-absent-home eval 'rc_is 0'
cairn writer arcs --check --scope omega-notes
assert check-home-scope-absent-exits-9 eval 'rc_is 9 && has "- home-scope-absent · omega-notes/e2e-lost-home · its home scope \`omega-notes/\` is readable to you and does not exist in the store"'
# Scoped: beta's arcs are fine, whatever alpha's and omega's are.
cairn writer arcs --check --scope beta-notes
assert check-scoped-beta-still-clean eval 'rc_is 0 && has "status=arcs-check-clean"'

# A damaged journal: one complete line that is not a record.
printf '{"not": "a record"}\n' >>"$journal"
cairn writer arcs --check --scope beta-notes
assert check-journal-damaged-exits-9 eval 'rc_is 9 && has "- journal-damaged · the registration journal holds record(s) that could not be read"'

# --- 5. no new exit code (Q5): the printed tables, whole ------------------------------------------
cairn writer -exit-codes
want_codes="client EXIT_CORRUPT 5
client EXIT_OK 0
client EXIT_REFRESH_FAILED 4
client EXIT_UNREACHABLE_NO_CACHE 3
client EXIT_UNROUTED 11
client EXIT_USAGE 2
client EXIT_WRITE_EXISTS 9
client EXIT_WRITE_PRECONDITION 8
client EXIT_WRITE_REFUSED 6
client EXIT_WRITE_UNREACHABLE 7
doctor EXIT_DOCTOR_OK 0
doctor EXIT_DOCTOR_PROBLEM 9
doctor EXIT_DOCTOR_UNMEASURED 10"
assert exit-code-tables-unchanged eval 'rc_is 0 && [[ "$out" == "$want_codes" ]]'

# --- 6. a pod that is not there: could not look ----------------------------------------------------
stop_pod
cairn writer arcs --check --scope alpha-notes
assert check-unreachable-pod-10 eval 'rc_is 10 && grep -qF "arcs --check could NOT look" <<<"$err"'

echo "SUMMARY e2e: passed=$passed failed=$failed expected=$EXPECTED"
if [[ $((passed + failed)) -ne $EXPECTED ]]; then
  couldnt "$((passed + failed)) assertions ran, $EXPECTED are declared"
fi
[[ $failed -eq 0 ]] && exit 0
exit 1
