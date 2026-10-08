#!/usr/bin/env bash
# The arcs/presence CLOSING CHECK for cairn (closing condition (1) of
# claudedocs/plan-cairn-arcs-presence.md), end to end over the REAL binaries: it builds `cmd/cairn-ui`
# and `cmd/cairn-server`, seeds a synthetic store, a control journal (two users, three credentials, one
# of them NARROWED) and an arc journal, mints presence tokens with the binary's own
# `-issue-presence-token`, boots `cairn-ui` with presence armed, and proves the QUEUE, OWNER and HOST
# semantics up to "the ring was claimed by the right agent token". There is no executor here — ringing
# a pane is the host side's (S3), in the tooling repo's own suite.
#
#   usage: tests/presence/e2e.sh              # the check: exits 0 iff every assertion PASSes
#          tests/presence/e2e.sh --self-test  # the negative control: exits 0 iff every SABOTAGED
#                                             # build turns its clause's named assertion RED
#
# The clauses, as the plan states them, and the assertion that carries each (a PASS line per name):
#   (a) `GET /arcs` lists a recent and an open arc newest first and omits a closed arc older than 14
#       days until `?all=1`;
#   (b) owner A's push token pushes presence for `s-0001` on `host-a`; A's session page shows
#       `host-a · notes:3`, while owner B's page for the same session is BYTE-IDENTICAL to the page with
#       no presence at all;
#   (c) A's `POST /ring` enqueues; B's `POST /ring` for the same session gets the same answer as with no
#       presence and leaves the queue unchanged;
#   (d) A's `host-a` claim token claims the ring exactly once; A's `host-b` claim token and a
#       just-revoked claim token each claim nothing; a token row for owner B refuses the listener at
#       startup, while the same row added AFTER startup is refused as that ROW only (logged; A's rows
#       keep working) and B's token gets 401;
#   (e) with `s-0001` presented by both hosts, the ring goes to the newest `last_activity`, and on a tie
#       to the byte-wise smaller host label;
#   (f) a narrowed BEARER credential's session page shows no presence; a narrowed credential presented
#       to `POST /sign-in` is refused, sets no session cookie, and a follow-up carrying any cookie from
#       that response is unauthenticated.
#
# Output: one `PASS <name>` / `FAIL <name>` line per assertion, then `SUMMARY e2e: passed=N failed=M
# expected=E`. Exit 0 all passed, 1 any failed, 2 COULD NOT VOUCH — a prerequisite is missing (no Go
# toolchain, no curl, no python3, a binary that did not build, a pod that did not come up), or the
# assertion count is not the declared one. 🔴 NEVER 0 AND NEVER A SKIP WHEN A PREREQUISITE IS MISSING:
# a tier without the toolchain must go red, not vacuously green.
#
# 🔴 WHY A SELF-TEST AND NOT ONLY A GREEN. Every assertion is a byte compare, a grep or a status code,
# and a harness wired to nothing passes all three. `--self-test` copies the source tree, breaks the
# code path ONE clause pins (the `tests/control_mutants.py` pattern, on a scratch copy — never the
# working tree), rebuilds, reruns this script against the broken build and requires the run to go RED
# on THAT clause's own assertion. A sabotage that leaves its clause green is a clause that cannot fail,
# and the self-test exits 2 naming it.
#
# Everything is synthetic: scope names, hosts (`host-a`, `host-b`), the tmux target (`notes:3`), the
# session id (`s-0001`), the identities, and the year-2000 dates. Tokens and credentials are generated
# per run and never leave the run's temp directory.
set -uo pipefail

repo="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
# `E2E_SOURCE` is the tree the binaries are BUILT from; the self-test points it at a sabotaged copy.
src="${E2E_SOURCE:-$repo}"

# The number of assertions a full run makes. 🔴 PINNED, so an assertion that silently stopped running
# is exit 2 rather than a smaller green.
EXPECTED=19

couldnt() { echo "COULD NOT VOUCH: $*" >&2; exit 2; }

for tool in go curl python3; do
  command -v "$tool" >/dev/null 2>&1 || couldnt "\`$tool\` is not on PATH — this check needs it and refuses to skip"
done

if [[ "${1:-}" == "--self-test" ]]; then
  command -v git >/dev/null 2>&1 || couldnt "\`git\` is not on PATH — the self-test copies the tracked tree with it"
  work="$(mktemp -d "${TMPDIR:-/tmp}/cairn-presence-e2e-selftest-XXXXXX")"
  trap 'rm -rf "$work"' EXIT
  sabotaged=0
  caught=0
  missed=()
  # sabotage <name> <file> <exact line> <replacement> <assertion that must FAIL>
  sabotage() {
    local name="$1" file="$2" line="$3" replacement="$4" must_fail="$5"
    local tree="$work/$name"
    mkdir -p "$tree"
    # Tracked AND new-untracked files, so an uncommitted change under test is in the copy; no `.git`
    # is copied, so nothing in the copy can reach the real repository.
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
    if cmp -s "$src/$file" "$tree/$file"; then
      echo "HARNESS ERROR: sabotage $name: the copy of $file is unchanged after the edit" >&2
      exit 2
    fi
    sabotaged=$((sabotaged + 1))
    local out rc
    out="$(E2E_SOURCE="$tree" "$repo/tests/presence/e2e.sh" 2>&1)"
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
      missed+=("$name")
      echo "MISSED $name: exit $rc; 'FAIL $must_fail' $(grep -qxF "FAIL $must_fail" <<<"$out" && echo present || echo ABSENT)"
    fi
  }
  # (a) The 14-day window dropped: every arc is live, so the closed arc from 2000 is listed unasked.
  sabotage a-live-window-dropped internal/ui/arcsindex.go \
    $'\treturn int(now.Sub(act.At)/(24*time.Hour)) <= arcLiveDays' \
    $'\treturn true' \
    "a-arcs-live-newest-first-closed-old-hidden"
  # (b) The owner clause of the ONE predicate dropped: B is shown A's presence.
  sabotage b-owner-predicate-dropped internal/presence/presence.go \
    $'\tif owner != OwnerOf(viewer.Principal) {' \
    $'\tif false {' \
    "b-nonowner-page-byte-identical-to-no-presence"
  # (c) The ring reads the store WITHOUT the predicate — the target row for anybody naming the session,
  #     filed under its real owner and host, which is exactly where A's claim service would find it.
  sabotage c-ring-skips-the-predicate internal/presence/queue.go \
    $'\tp, ok := s.Store.For(viewer, session)' \
    $'\tvar p Presence\n\tok := false\n\ts.Store.mu.Lock()\n\tfor key, set := range s.Store.hosts {\n\t\tfor _, r := range set.rows {\n\t\t\tif r.Session == session {\n\t\t\t\tp, ok = Presence{Target: Located{Row: r, Owner: key.owner, Host: key.host}}, true\n\t\t\t}\n\t\t}\n\t}\n\ts.Store.mu.Unlock()' \
    "c-nonowner-ring-same-answer-queue-unchanged"
  # (d) The per-row owner check skipped in the token-file reader: B's row added after startup then
  #     authenticates (and the startup refusal goes too — the same function answers both).
  sabotage d-wall-skipped internal/presence/tokens.go \
    $'\tif row.Owner != owner {' \
    $'\tif false {' \
    "d-foreign-row-after-startup-refused-as-a-row"
  # (e) The target pick inverted: the OLDEST last_activity wins.
  sabotage e-target-picks-oldest internal/presence/presence.go \
    $'\t\treturn a.activity.After(b.activity)' \
    $'\t\treturn a.activity.Before(b.activity)' \
    "e-ring-goes-to-newest-activity"
  # (f) first half: the bearer Narrowed bit ignored by the predicate.
  sabotage f1-narrowing-ignored internal/presence/presence.go \
    $'\tif !viewer.Valid() || viewer.Auth.Narrowed() {' \
    $'\tif !viewer.Valid() {' \
    "f-narrowed-bearer-sees-no-presence"
  # (f) second half: the sign-in refusal of a narrowed credential reverted.
  sabotage f2-sign-in-admits-narrowed internal/ui/session.go \
    $'\tif auth.Narrowed() {' \
    $'\tif false && auth.Narrowed() {' \
    "f-narrowed-sign-in-refused-no-cookie"
  echo "SUMMARY e2e-self-test: sabotaged=$sabotaged caught=$caught"
  if [[ ${#missed[@]} -gt 0 ]]; then
    echo "a sabotage that leaves its clause green is a clause that cannot fail: ${missed[*]}" >&2
    exit 2
  fi
  [[ $sabotaged -gt 0 && $caught -eq $sabotaged ]] && exit 0
  exit 2
fi

work="$(mktemp -d "${TMPDIR:-/tmp}/cairn-presence-e2e-XXXXXX")"
pids=()
cleanup() {
  for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
  rm -rf "$work"
}
trap cleanup EXIT

mkdir -p "$work/bin"
go build -C "$src" -o "$work/bin/cairn-ui" ./cmd/cairn-ui || couldnt "cairn-ui did not build"
go build -C "$src" -o "$work/bin/cairn-server" ./cmd/cairn-server || couldnt "cairn-server did not build"

# --- the synthetic world -----------------------------------------------------------------------
# Two scopes, one per user; `s-0001` wrote in BOTH, so both users can open its session page.
store="$work/store"
entry() { # entry <scope> <service> <date> <text>
  mkdir -p "$store/$1"
  printf -- '---\nservice: %s\nscope: %s\n---\n\n## What it is\nsynthetic.\n\n## Nuance / work-history\n- %s: %s [cairn: e2e-writer/s-0001]\n' \
    "$2" "$1" "$3" "$4" >"$store/$1/$2.md"
}
entry alpha-notes gadget-one 2000-01-05 "tuned the gadget"
entry beta-notes widget-two 2000-01-06 "drained the widget"

# A clean environment for every binary: no ambient `CAIRN_*` or `SUBSYSTEM_STORE_*` can reach them.
run_env() { env -i PATH="$PATH" HOME="$work" CAIRN_MAX_FAILURES=1000000 "$@"; }

journal="$work/control/journal.jsonl"
mkdir -p "$work/control"
create_user() { # create_user <subject> <email> <project> <scope> → "usr_… scp_…"
  local line
  line="$(run_env CAIRN_CONTROL_JOURNAL="$journal" "$work/bin/cairn-server" -store "$store" -create-user \
    -provider e2e-provider -subject "$1" -email "$2" -project "$3" -scopes "$4" 2>>"$work/create.log")" ||
    couldnt "create-user $2 failed: $(cat "$work/create.log")"
  python3 - "$line" <<'PY'
import re, sys
line = sys.argv[1]
user = re.search(r"\buser=(usr_\S+)", line)
scopes = re.search(r"\bscopes=(scp_[^:\s]+):", line)
if not (user and scopes):
    sys.exit(1)
print(user.group(1), scopes.group(1))
PY
}
read -r user_a scope_a < <(create_user subject-e2e-a a@e2e.example.invalid project-e2e-a alpha-notes) || couldnt "user A"
read -r user_b _scope_b < <(create_user subject-e2e-b b@e2e.example.invalid project-e2e-b beta-notes) || couldnt "user B"
[[ "$user_a" == usr_* && "$user_b" == usr_* && "$user_a" != "$user_b" ]] || couldnt "the two users were not created distinctly"

issue() { # issue <file> <principal> <label> [narrow-scopes]
  local extra=()
  [[ -n "${4:-}" ]] && extra=(-narrow-scopes "$4")
  run_env CAIRN_CONTROL_JOURNAL="$journal" "$work/bin/cairn-server" -store "$store" -issue-credential \
    -principal "$2" -label "$3" -token-out "$1" "${extra[@]}" 2>>"$work/issue.log" ||
    couldnt "issue-credential $3 failed: $(cat "$work/issue.log")"
}
issue "$work/cred-a" "$user_a" e2e-a
issue "$work/cred-a-narrowed" "$user_a" e2e-a-narrowed "$scope_a"
issue "$work/cred-b" "$user_b" e2e-b
cred_a="$(head -n1 "$work/cred-a")"
cred_a_narrowed="$(head -n1 "$work/cred-a-narrowed")"
cred_b="$(head -n1 "$work/cred-b")"

# The arc journal: three arcs homed in alpha — one registered an hour ago (live by recency), one OPEN
# from 2000 (live by status), one CLOSED from 2000 (hidden until `?all=1`). The recent one is stamped
# from the run's clock because the live window is measured against the pod's.
arcs="$work/arcs/journal.jsonl"
mkdir -p "$work/arcs"
python3 - "$arcs" <<'PY'
import datetime, json, sys
now = datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(hours=1)
recent = now.strftime("%Y-%m-%dT%H:%M:%SZ")
with open(sys.argv[1], "w") as f:
    for slug, status, at in (("e2e-recent-arc", "closed", recent),
                             ("e2e-open-old-arc", "open", "2000-01-01T00:00:00Z"),
                             ("e2e-closed-old-arc", "closed", "2000-01-01T00:00:00Z")):
        f.write(json.dumps({"schema": 1, "home": "alpha-notes", "slug": slug, "status": status,
            "closing_kind": "check", "declared_scopes": ["alpha-notes"], "writers_measured": False,
            "readers_measured": False, "commits_total": 0, "commits_unstamped": 0,
            "reported_at": "2000-01-01T00:00:00Z", "members": [], "registered_by": "e2e-writer",
            "registered_at": at}) + "\n")
PY

# Presence tokens, minted by the binary under test. Owner A's rows go in the file the listener reads;
# owner B's push row is minted into a SEPARATE file (a mint refuses a file holding another owner's
# rows — the wall) and appended later.
ptokens="$work/presence-tokens"
mint() { # mint <file> <owner> <kind> <host> → the token on stdout
  run_env "$work/bin/cairn-ui" -store "$store" -control-journal "$journal" -session-file "$work/sessions.json" \
    -issue-presence-token "$3" -presence-owner "$2" -presence-host "$4" -presence-tokens "$1" 2>>"$work/mint.log"
}
owner_a="user:$user_a"
owner_b="user:$user_b"
push_a="$(mint "$ptokens" "$owner_a" push host-a)" || couldnt "mint push host-a: $(cat "$work/mint.log")"
push_b_host="$(mint "$ptokens" "$owner_a" push host-b)" || couldnt "mint push host-b"
claim_a="$(mint "$ptokens" "$owner_a" claim host-a)" || couldnt "mint claim host-a"
claim_b_host="$(mint "$ptokens" "$owner_a" claim host-b)" || couldnt "mint claim host-b"
claim_revoked="$(mint "$ptokens" "$owner_a" claim host-a)" || couldnt "mint the claim token to revoke"
push_owner_b="$(mint "$work/presence-tokens-b" "$owner_b" push host-a)" || couldnt "mint B's push token"
[[ $(grep -c . "$ptokens") -eq 5 && $(grep -c . "$work/presence-tokens-b") -eq 1 ]] || couldnt "the token files do not hold the minted rows"

free_port() { python3 -c 'import socket
s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'; }

# start_ui <log> <token file> sets $url, $agent, $ui. 🔴 NOT CALLED IN A `$(…)`: a command substitution
# is a subshell, so the pid it recorded would never reach `cleanup` (the arcs e2e measured that).
ui=""
url=""
agent=""
start_ui() {
  local log="$1" tokens="$2"
  local port aport
  port="$(free_port)"
  aport="$(free_port)"
  run_env "$work/bin/cairn-ui" -store "$store" -control-journal "$journal" -session-file "$work/sessions.json" \
    -arc-journal "$arcs" -host 127.0.0.1 -port "$port" \
    -presence-agent-addr "127.0.0.1:$aport" -presence-tokens "$tokens" -presence-owner "$owner_a" >"$log" 2>&1 &
  ui=$!
  pids+=("$ui")
  for _ in $(seq 1 200); do
    if [[ "$(curl -fsS "http://127.0.0.1:$port/healthz" 2>/dev/null)" == "ok" ]]; then
      url="http://127.0.0.1:$port"
      agent="http://127.0.0.1:$aport"
      return 0
    fi
    kill -0 "$ui" 2>/dev/null || break
    sleep 0.05
  done
  cat "$log" >&2
  return 1
}
stop_ui() { kill "$ui" 2>/dev/null; wait "$ui" 2>/dev/null; }

# --- HTTP: every call leaves $code, $hdrs (raw header block) and $body --------------------------
code=""
hdrs=""
body=""
http() { # http <curl args…>
  code="$(curl -sS -o "$work/body" -D "$work/hdrs" -w '%{http_code}' "$@" 2>"$work/curl.err")" || code="000"
  body="$(cat "$work/body" 2>/dev/null)"
  hdrs="$(tr -d '\r' <"$work/hdrs" 2>/dev/null)"
}
# The headers that make up an ANSWER: everything but `Date`, which moves with the clock.
answer() { printf '%s\n' "$code"; grep -iv '^date:' <<<"$hdrs"; printf '%s' "$body"; }
location() { grep -i '^location:' <<<"$hdrs" | sed 's/^[^:]*: *//'; }

sign_in() { # sign_in <credential> → sets $cookie to the session cookie's value ("" if none)
  http -X POST -H "Origin: $url" --data-urlencode "token=$1" "$url/sign-in"
  cookie="$(grep -i '^set-cookie: __Host-cairn-session=' <<<"$hdrs" | head -n1 | sed 's/^[^=]*=//; s/;.*//')"
}
page() { # page <cookie> <path>
  http -H "Cookie: __Host-cairn-session=$1" "$url$2"
}
csrf_of() { grep -o 'name="csrf" value="[^"]*"' <<<"$1" | head -n1 | sed 's/.*value="//; s/"$//'; }
ring() { # ring <cookie> <csrf> <session>
  http -X POST -H "Origin: $url" -H "Cookie: __Host-cairn-session=$1" \
    --data-urlencode "csrf=$2" --data-urlencode "session=$3" "$url/ring"
}
agent_post() { # agent_post <token> <path> <json>
  http -X POST -H "Authorization: Bearer $1" -H 'Content-Type: application/json' --data "$3" "$agent$2"
}
push() { # push <token> <host> <rows-json>
  agent_post "$1" /agent/v1/presence "{\"schema\":1,\"host\":\"$2\",\"rows\":$3}"
}
row() { # row <target> <last_activity>
  printf '[{"session":"s-0001","runtime":"claude","target":"%s","label":"notes","hotkey":"Alt+n","last_activity":"%s"}]' "$1" "$2"
}
claim() { agent_post "$1" /agent/v1/rings/claim '{}'; }
# claimed <token> → prints the claimed sessions, one per line, or CODE:<status> when not a 200.
claimed() {
  claim "$1"
  if [[ "$code" != "200" ]]; then echo "CODE:$code"; return; fi
  python3 -c 'import json, sys
d = json.loads(sys.argv[1])
assert d["schema"] == 1
for r in d["rings"]:
    print(r["session"])' "$body"
}

passed=0
failed=0
assert() { # assert <name> <command…>
  local name="$1"; shift
  if "$@"; then
    echo "PASS $name"; passed=$((passed + 1))
  else
    echo "FAIL $name"; failed=$((failed + 1))
    { echo "  last response: $code"; sed 's/^/  | /' <<<"$hdrs"; sed 's/^/  | /' <<<"${body:0:600}"; } >&2
  fi
}
has() { grep -qF -- "$1" <<<"$body"; }
lacks() { ! grep -qF -- "$1" <<<"$body"; }

# --- (d, startup half): a foreign owner's row in the file AT STARTUP refuses the listener -----------
cat "$ptokens" "$work/presence-tokens-b" >"$work/presence-tokens-mixed"
start_ui "$work/ui-mixed.log" "$work/presence-tokens-mixed" || couldnt "cairn-ui with a mixed token file did not come up"
mixed_agent="$agent"
sleep 0.2
agent_up=no
curl -sS -o /dev/null -X POST "$mixed_agent/agent/v1/rings/claim" 2>/dev/null && agent_up=yes
assert d-foreign-row-at-startup-refuses-the-listener eval '[[ $agent_up == no ]] &&
  grep -qF "WARNING the presence agent listener is NOT started" "$work/ui-mixed.log" &&
  [[ "$(curl -fsS "$url/healthz")" == ok ]]'
stop_ui

# --- the instance under test --------------------------------------------------------------------
start_ui "$work/ui.log" "$ptokens" || couldnt "cairn-ui did not come up"
grep -qF "presence agent on 127.0.0.1:" "$work/ui.log" || couldnt "the presence agent listener did not start: $(cat "$work/ui.log")"

# --- (a) the arcs-first page ----------------------------------------------------------------------
http -H "Authorization: Bearer $cred_a" "$url/arcs"
recent_at="$(grep -bo 'e2e-recent-arc' <<<"$body" | head -n1 | cut -d: -f1)"
open_at="$(grep -bo 'e2e-open-old-arc' <<<"$body" | head -n1 | cut -d: -f1)"
assert a-arcs-live-newest-first-closed-old-hidden eval '[[ $code == 200 && -n "$recent_at" && -n "$open_at" ]] &&
  (( recent_at < open_at )) && lacks "e2e-closed-old-arc"'
http -H "Authorization: Bearer $cred_a" "$url/arcs?all=1"
assert a-arcs-all-shows-the-closed-old-arc eval '[[ $code == 200 ]] && has "e2e-closed-old-arc" && has "e2e-recent-arc" && has "e2e-open-old-arc"'

# --- sign both owners in, and record every "no presence" baseline BEFORE any push -----------------
sign_in "$cred_a"
cookie_a="$cookie"
sign_in "$cred_b"
cookie_b="$cookie"
[[ -n "$cookie_a" && -n "$cookie_b" && "$cookie_a" != "$cookie_b" ]] || couldnt "the two owners could not both sign in"
page "$cookie_a" "/session?session=s-0001"
[[ "$code" == 200 ]] || couldnt "A's session page answered $code"
a_off="$body"
csrf_a="$(csrf_of "$body")"
page "$cookie_b" "/session?session=s-0001"
[[ "$code" == 200 ]] || couldnt "B's session page answered $code"
b_off="$body"
csrf_b="$(csrf_of "$body")"
[[ -n "$csrf_a" && -n "$csrf_b" ]] || couldnt "a session page rendered no CSRF token"
ring "$cookie_b" "$csrf_b" s-0001
b_ring_off="$(answer)"
[[ "$code" == 303 ]] || couldnt "B's ring with no presence answered $code, not the 303"
claimed "$claim_a" >"$work/claimed"
[[ ! -s "$work/claimed" ]] || couldnt "a ring was queued before any presence existed: $(cat "$work/claimed")"

# --- (b) A pushes; A's page shows it, B's page is byte-identical to before -------------------------
push "$push_a" host-a "$(row notes:3 2000-01-02T03:04:05Z)"
assert b-owner-push-accepted eval '[[ $code == 200 && "$body" == "rows=1" ]] && grep -qi "^x-presence-status: presence-replaced" <<<"$hdrs"'
page "$cookie_a" "/session?session=s-0001"
assert b-owner-page-shows-host-a-notes-3 eval '[[ $code == 200 && "$body" != "$a_off" ]] && has "host-a · notes:3 · Alt+n · claude" &&
  has "data-presence=\"bell\""'
page "$cookie_b" "/session?session=s-0001"
assert b-nonowner-page-byte-identical-to-no-presence eval '[[ $code == 200 && "$body" == "$b_off" ]]'

# --- (c) B's ring is the no-presence answer and queues nothing; A's ring queues one ---------------
ring "$cookie_b" "$csrf_b" s-0001
b_ring="$(answer)"
claimed "$claim_a" >"$work/claimed"
assert c-nonowner-ring-same-answer-queue-unchanged eval '[[ "$b_ring" == "$b_ring_off" && ! -s "$work/claimed" ]]'
ring "$cookie_a" "$csrf_a" s-0001
assert c-owner-ring-303-to-the-session-page eval '[[ $code == 303 && "$(location)" == "/session?session=s-0001" && -z "$body" ]]'
a_ring="$(answer)"
ring "$cookie_a" "$csrf_a" s-0001
assert c-owner-repeat-while-pending-same-answer eval '[[ "$(answer)" == "$a_ring" ]]'

# --- (d) claims: the right host's token, exactly once; the other host's and a revoked one, nothing --
claimed "$claim_b_host" >"$work/claimed-b"
assert d-host-b-claim-token-claims-nothing eval '[[ ! -s "$work/claimed-b" ]]'
claimed "$claim_a" >"$work/claimed-1"
claimed "$claim_a" >"$work/claimed-2"
assert d-host-a-claim-token-claims-exactly-once eval '[[ "$(cat "$work/claimed-1")" == "s-0001" && ! -s "$work/claimed-2" ]]'
ring "$cookie_a" "$csrf_a" s-0001
revoked_digest="$(printf '%s' "$claim_revoked" | sha256sum | cut -d" " -f1)"
grep -q "$revoked_digest" "$ptokens" || couldnt "the token to revoke has no row in the token file"
grep -v "$revoked_digest" "$ptokens" >"$work/ptokens.next" && cat "$work/ptokens.next" >"$ptokens"
claimed "$claim_revoked" >"$work/claimed-revoked"
claimed "$claim_a" >"$work/claimed-after"
assert d-revoked-claim-token-claims-nothing eval '[[ "$(cat "$work/claimed-revoked")" == "CODE:401" && "$(cat "$work/claimed-after")" == "s-0001" ]]'
# The wall, after startup: B's row appended to the LIVE file.
cat "$work/presence-tokens-b" >>"$ptokens"
push "$push_owner_b" host-a "$(row notes:9 "")"
b_push_code="$code"
assert d-foreign-row-after-startup-refused-as-a-row eval '[[ $b_push_code == 401 ]] &&
  grep -qF "refused: the row names an owner other than this instance" "$work/ui.log"'
push "$push_a" host-a "$(row notes:3 2000-01-02T03:04:05Z)"
a_push_code="$code"
ring "$cookie_a" "$csrf_a" s-0001
claimed "$claim_a" >"$work/claimed-wall"
assert d-owner-rows-keep-working-after-a-foreign-row eval '[[ $a_push_code == 200 && "$(cat "$work/claimed-wall")" == "s-0001" ]]'

# --- (e) two hosts present s-0001: newest last_activity, then the smaller label -------------------
push "$push_a" host-a "$(row notes:3 2000-01-02T03:04:05Z)"
push "$push_b_host" host-b "$(row notes:7 2000-01-02T04:00:00Z)"
ring "$cookie_a" "$csrf_a" s-0001
claimed "$claim_a" >"$work/e-a"
claimed "$claim_b_host" >"$work/e-b"
assert e-ring-goes-to-newest-activity eval '[[ ! -s "$work/e-a" && "$(cat "$work/e-b")" == "s-0001" ]]'
push "$push_a" host-a "$(row notes:3 "")"
push "$push_b_host" host-b "$(row notes:7 "")"
ring "$cookie_a" "$csrf_a" s-0001
claimed "$claim_a" >"$work/e-a"
claimed "$claim_b_host" >"$work/e-b"
assert e-tie-goes-to-the-smaller-host-label eval '[[ "$(cat "$work/e-a")" == "s-0001" && ! -s "$work/e-b" ]]'

# --- (f) narrowing: a narrowed bearer sees nothing; a narrowed credential cannot sign in ------------
http -H "Authorization: Bearer $cred_a" "$url/session?session=s-0001"
assert f-unnarrowed-bearer-sees-presence eval '[[ $code == 200 ]] && has "data-presence=\"pane\""'
http -H "Authorization: Bearer $cred_a_narrowed" "$url/session?session=s-0001"
assert f-narrowed-bearer-sees-no-presence eval '[[ $code == 200 ]] && has "s-0001" && lacks "data-presence=" && lacks "host-a"'
sign_in "$cred_a_narrowed"
narrowed_code="$code"
narrowed_cookies="$(grep -i '^set-cookie:' <<<"$hdrs" | sed 's/^[^:]*: *//; s/;.*//' | paste -sd ';' -)"
http -H "Cookie: ${narrowed_cookies:-__Host-cairn-session=none}" "$url/session?session=s-0001"
follow_code="$code"
assert f-narrowed-sign-in-refused-no-cookie eval '[[ $narrowed_code == 401 && -z "$(grep -o "__Host-cairn-session=[^;]\+" <<<"$narrowed_cookies")" && $follow_code == 401 ]]'

echo "SUMMARY e2e: passed=$passed failed=$failed expected=$EXPECTED"
if [[ $((passed + failed)) -ne $EXPECTED ]]; then
  couldnt "$((passed + failed)) assertions ran, $EXPECTED are declared"
fi
[[ $failed -eq 0 ]] && exit 0
exit 1
