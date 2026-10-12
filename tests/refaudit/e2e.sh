#!/usr/bin/env bash
# The scope-refs CLOSING CHECK (closing-condition part 2 of claudedocs/plan-cairn-scope-refs.md), end
# to end over the REAL binaries: it builds `cmd/cairn-server`, `cmd/cairn` and a seed helper, stands up
# a synthetic world — bare repositories reached over `file://`, a loopback GitHub stub, a pod with a
# sources journal written through the ONE writer (`internal/codesrc`) — and asserts clauses (a)–(f).
# Nothing touches the network.
#
#   usage: tests/refaudit/e2e.sh              # the check: exits 0 iff every assertion PASSes
#          tests/refaudit/e2e.sh --self-test  # the negative control: exits 0 iff every SABOTAGED
#                                             # build turns its OWN clause's assertion RED
#
# Output: one `PASS <name>` / `FAIL <name>` line per assertion, then `SUMMARY e2e: passed=N failed=M
# expected=E`. Exit 0 all passed, 1 any failed, 2 COULD NOT VOUCH — `git`, `curl` or `python3` missing,
# a binary that did not build, a pod or stub that did not come up, or an assertion count that is not
# the declared one. That 2 is THIS SCRIPT's; `cairn audit-refs` exits 2 only for usage.
#
# The clauses (the plan's table):
#   (a) the declaration is served: the pod serves alpha's two sources; `cairn sources` prints both
#       canonical forms, in order; beta (no record) is `sources=undeclared`; an outsider gets for alpha
#       exactly what it gets for a scope that does not exist.
#   (b) clean world: `audit-refs` over the real `file://` fetch exits 0, `checked=N stale=0 unchecked=0`,
#       N the planted count.
#   (c) stale world: one missing path, one symbol absent from its file, one PR answering 404 → exit 9,
#       the stale finding set EQUALS the planted set; removing each defect drops exactly its finding.
#   (d) could not look, each exit 10 with its reason: a remote that does not exist; an undeclared
#       scope; a declared scope with checked=0; a clean mapped source beside an unmapped one. Control:
#       one claim, mapped sources only → 0.
#   (e) the pod and the UI never fetch: `go list -deps` finds no `internal/refaudit` in either server,
#       no `os/exec` in the pod, and `os/exec` in the UI's graph imported by `internal/client` ALONE
#       (it was there before S5 — the agent tab imports `internal/client`); control: `cmd/cairn` has both.
#   (f) reachability, not presence: a SHA reachable only from `refs/pull/1/head` is
#       `exists-off-branch` and the exit is unaffected; with that ref DELETED on the remote (a tag on
#       the same SHA kept there), the next run over the SAME mirror (the object still in it) is
#       `not-reachable` (exit 10); a fresh mirror agrees. ⚠ The kept tag never reaches the mirror
#       (`--no-tags`), so this clause does NOT test the tag restriction —
#       `TestATagInAnOlderMirrorNeverCountsAsReachable` plants a tag in the mirror and does.
#
# Everything is synthetic; tokens are generated per run and never leave its temp directory.
set -uo pipefail

repo="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
src="${E2E_SOURCE:-$repo}"

# 🔴 PINNED, so an assertion that silently stopped running is exit 2 rather than a smaller green.
EXPECTED=27

if [[ "${1:-}" == "--self-test" ]]; then
  work="$(mktemp -d "${TMPDIR:-/tmp}/cairn-refaudit-e2e-selftest-XXXXXX")"
  trap 'rm -rf "$work"' EXIT
  sabotaged=0
  caught=0
  # sabotage <name> <file> <exact line> <replacement> <assertion that must FAIL>
  sabotage() {
    local name="$1" file="$2" line="$3" replacement="$4" must_fail="$5"
    local tree="$work/$name"
    mkdir -p "$tree"
    # Tracked AND new-untracked files; no `.git` is copied, so the copy cannot reach the repository.
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
    out="$(E2E_SOURCE="$tree" "$repo/tests/refaudit/e2e.sh" 2>&1)"
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
  # (a) the pod serves the first source only.
  sabotage a-first-source-only internal/report/sources.go \
    $'\tfor _, s := range r.Record.Sources {' $'\tfor _, s := range r.Record.Sources[:1] {' \
    "a-sources-prints-both-in-order"
  # (b) the denominator prints 0.
  sabotage b-denominator-zero internal/refaudit/refaudit.go \
    $'\tfmt.Fprintf(&b, "checked=%d stale=%d unchecked=%d\\n", checked, stale, unchecked)' \
    $'\tfmt.Fprintf(&b, "checked=%d stale=%d unchecked=%d\\n", checked*0, stale, unchecked)' \
    "b-clean-world-exits-0-with-the-planted-denominator"
  # (c) the symbol check dropped: every symbol is "present".
  sabotage c-symbol-check-dropped internal/refaudit/audit.go \
    $'\t\tif found {' $'\t\tif found || true {' \
    "c-stale-finding-set-equals-the-planted-set"
  # (d) an unmapped host counts as clean.
  sabotage d-unmapped-counted-clean internal/refaudit/refaudit.go \
    $'\tif r.unlookedSource() {' $'\tif false {' \
    "d-clean-beside-unmapped-is-10"
  # (e) a blank os/exec import goes into the pod.
  sabotage e-exec-in-the-pod cmd/cairn-server/main.go \
    'package main' $'package main\n\nimport _ "os/exec"' \
    "e-the-pod-links-neither-exec-nor-the-auditor"
  # (f) reachability replaced by presence: any commit object in the mirror counts as reachable.
  sabotage f-presence-not-reachability internal/refaudit/audit.go \
    $'\t\tif len(refs) > 0 {' $'\t\tif true {' \
    "f-deleted-pull-ref-tag-kept-is-not-reachable"
  echo "SUMMARY e2e-self-test: sabotaged=$sabotaged caught=$caught"
  [[ $sabotaged -gt 0 && $caught -eq $sabotaged ]] && exit 0
  exit 1
fi

couldnt() { echo "COULD NOT VOUCH: $*" >&2; exit 2; }
for tool in git curl python3 go; do
  command -v "$tool" >/dev/null 2>&1 || couldnt "$tool is not on PATH"
done

work="$(mktemp -d "${TMPDIR:-/tmp}/cairn-refaudit-e2e-XXXXXX")"
pids=()
cleanup() {
  for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
  rm -rf "$work"
}
trap cleanup EXIT

mkdir -p "$work/bin"
go build -C "$src" -o "$work/bin/cairn-server" ./cmd/cairn-server || couldnt "cairn-server did not build"
go build -C "$src" -o "$work/bin/cairn" ./cmd/cairn || couldnt "cairn did not build"
go build -C "$src" -o "$work/bin/seed" ./tests/refaudit/seed || couldnt "the seed helper did not build"

# --- the remote world: bare repositories over file:// ---------------------------------------------
g() { git -c init.defaultBranch=main "$@"; }
export GIT_AUTHOR_NAME=e2e GIT_AUTHOR_EMAIL=e2e@example.invalid GIT_COMMITTER_NAME=e2e \
  GIT_COMMITTER_EMAIL=e2e@example.invalid GIT_AUTHOR_DATE=2000-01-01T00:00:00Z \
  GIT_COMMITTER_DATE=2000-01-01T00:00:00Z GIT_CONFIG_NOSYSTEM=1
remote="$work/remotes/example-org/example-repo"
mkdir -p "$remote"
g init --quiet --bare "$remote" || couldnt "git init --bare failed"
wt="$work/worktree-src"
g init --quiet "$wt" || couldnt "git init failed"
mkdir -p "$wt/cmd" "$wt/services/widget"
printf 'package main\n\nfunc Widget() {}\n' >"$wt/cmd/x.go"
printf 'package main\n\nfunc Serve() {}\n' >"$wt/services/widget/main.go"
g -C "$wt" add -A && g -C "$wt" commit --quiet -m one || couldnt "commit one failed"
on_main="$(g -C "$wt" rev-parse HEAD)"
g -C "$wt" push --quiet "$remote" HEAD:refs/heads/main || couldnt "push main failed"
printf 'package main\n' >"$wt/feature.go"
g -C "$wt" add -A && g -C "$wt" commit --quiet -m two || couldnt "commit two failed"
off_branch="$(g -C "$wt" rev-parse HEAD)"
g -C "$wt" push --quiet "$remote" HEAD:refs/pull/1/head || couldnt "push the pull ref failed"

src_root="git:github.com/example-org/example-repo@main"
src_sub="git:github.com/example-org/example-repo//services/widget@main"
src_none="git:github.com/example-org/no-such-repo@main"
src_unmapped="git:git.example.com/team/example-repo@main"

# --- the GitHub stub: the repository visible, #1 open, #2 merged, every other number 404 -------------
free_port() { python3 -c 'import socket
s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'; }
stub_port="$(free_port)"
cat >"$work/stub.py" <<'PY'
import http.server, json, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = {"/repos/example-org/example-repo": {"full_name": "example-org/example-repo"},
                "/repos/example-org/example-repo/pulls/1": {"state": "open", "merged_at": None},
                "/repos/example-org/example-repo/pulls/2": {"state": "closed", "merged_at": "2000-01-02T00:00:00Z"}}.get(self.path)
        self.send_response(200 if body else 404)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(body or {"message": "Not Found"}).encode())
    def log_message(self, *a):
        pass
http.server.HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
PY
python3 "$work/stub.py" "$stub_port" >"$work/stub.log" 2>&1 &
pids+=($!)
stub="http://127.0.0.1:$stub_port"

# --- the store and the journal --------------------------------------------------------------------
store="$work/store"
entry() { # entry <scope> <service> <refs> <pointers> <nuance>
  mkdir -p "$store/$1"
  printf -- '---\nservice: %s\nscope: %s\nrefs: [%s]\n---\n\n## What it is\nsynthetic.\n\n## Pointers\n%s\n\n## Nuance / work-history\n%s\n' \
    "$2" "$1" "$3" "$4" "$5" >"$store/$1/$2.md"
}
# alpha: the CLEAN world — 4 checked claims (a path, a subpath-relative path, a symbol, a RESOLVED sha
# on main) plus PR #1 open and PR #2 merged: N = 6.
entry alpha-notes gadget-one "github:example-org/example-repo#1, github:example-org/example-repo#2" \
  $'- `cmd/x.go` — the main\n- `main.go` — the widget server\n- `cmd/x.go#Widget` — the entry point' \
  "- 2000-01-02: RESOLVED ${on_main}: landed."
entry beta-notes widget-two "" '- `cmd/x.go` — x' '- 2000-01-02: a note.'
# gamma: the STALE world — three planted defects beside one holding claim.
gamma_pointers=$'- `cmd/x.go` — holds\n- `cmd/gone.go` — DEFECT: missing\n- `cmd/x.go#Vanished` — DEFECT: absent'
gamma_refs="github:example-org/example-repo#7"
entry gamma-notes doodad-three "$gamma_refs" "$gamma_pointers" '- 2000-01-02: a note.'
entry delta-notes thing-four "" '- `cmd/x.go` — x' '- 2000-01-02: a note.'
entry epsilon-notes thing-five "" '- the runbook — prose only' '- 2000-01-02: a note.'
entry zeta-notes thing-six "" '- `cmd/x.go` — x' '- 2000-01-02: a note.'
entry eta-notes thing-seven "" '- `cmd/x.go` — x' '- 2000-01-02: a note.'
entry theta-notes thing-eight "" '- `cmd/x.go` — x' "- 2000-01-02: RESOLVED ${off_branch}: a pull ref only."

mkdir -p "$work/journal"
journal="$work/journal/sources.jsonl"
seed() { "$work/bin/seed" "$journal" "$@" || couldnt "seeding $1 failed"; }
seed alpha-notes "$src_root" "$src_sub"
seed gamma-notes "$src_root"
seed delta-notes "$src_none"
seed epsilon-notes "$src_root"
seed zeta-notes "$src_root" "$src_unmapped"
seed eta-notes "$src_root"
seed theta-notes "$src_root"

token() { python3 -c 'import secrets; print(secrets.token_urlsafe(32))'; }
reader_token="$(token)"
outsider_token="$(token)"
all="alpha-notes,beta-notes,gamma-notes,delta-notes,epsilon-notes,zeta-notes,eta-notes,theta-notes"
printf '%s e2e-reader %s\n%s e2e-outsider beta-notes\n' "$reader_token" "$all" "$outsider_token" >"$work/tokens"

port="$(free_port)"
CAIRN_SOURCE_JOURNAL="$journal" CAIRN_TRUSTED_PROXIES=192.0.2.0/24 CAIRN_MAX_FAILURES=1000000 \
  "$work/bin/cairn-server" --store "$store" --host 127.0.0.1 --port "$port" --token-file "$work/tokens" \
  >"$work/pod.log" 2>&1 &
pids+=($!)
url="http://127.0.0.1:$port"
up=0
for _ in $(seq 1 200); do
  pod_ok="$(curl -s -o /dev/null -w '%{http_code}' "$url/healthz" 2>/dev/null || true)"
  stub_ok="$(curl -s -o /dev/null -w '%{http_code}' "$stub/healthz" 2>/dev/null || true)"
  if [[ "$pod_ok" == "200" && "$stub_ok" == "404" ]]; then up=1; break; fi
  sleep 0.05
done
[[ $up -eq 1 ]] || { cat "$work/pod.log" "$work/stub.log" >&2; couldnt "the pod or the GitHub stub did not come up"; }

# --- the client -----------------------------------------------------------------------------------
cairn() { # cairn <reader|outsider> <xdg-name> args... ; leaves $out, $err, $rc
  local who="$1" xdg="$2"; shift 2
  local tok="$reader_token"
  [[ "$who" == "outsider" ]] && tok="$outsider_token"
  mkdir -p "$work/home-$who"
  out="$(env -u SUBSYSTEM_STORE_URL -u SUBSYSTEM_STORE_TOKEN -u CAIRN_MIRROR_ROOT -u GH_TOKEN -u GITHUB_TOKEN \
    HOME="$work/home-$who" CAIRN_CONFIG="$work/home-$who/no-config" CAIRN_ROUTES="" \
    CAIRN_URL="$url" CAIRN_TOKEN="$tok" XDG_CACHE_HOME="$work/xdg-$xdg" \
    CAIRN_AUDIT_HOSTS="github.com=file://$work/remotes" CAIRN_AUDIT_GITHUB_API="$stub" \
    "$work/bin/cairn" "$@" 2>"$work/stderr")"
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
stale_set() { grep '^stale ' <<<"$out" | sed 's/ source=.*//' | sort; }

# --- (a) the declaration is served ----------------------------------------------------------------
cairn reader a sources --scope alpha-notes
assert a-sources-prints-both-in-order eval 'rc_is 0 && [[ "$(grep "^source=" <<<"$out")" == "source=$src_root"$'"'\n'"'"source=$src_sub" ]] && has "sources=declared count=2"'
cairn reader a sources --scope beta-notes
assert a-beta-is-undeclared eval 'rc_is 0 && has "sources=undeclared" && lacks "source=git:"'
cairn outsider a sources --scope alpha-notes
hidden="${out//alpha-notes/SCOPE}"
cairn outsider a sources --scope never-existed-notes
assert a-refused-equals-absent eval 'rc_is 0 && [[ "$hidden" == "${out//never-existed-notes/SCOPE}" ]] && lacks "source=git:"'

# --- (b) the clean world --------------------------------------------------------------------------
cairn reader b audit-refs --scope alpha-notes
assert b-clean-world-exits-0-with-the-planted-denominator eval 'rc_is 0 && has "checked=6 stale=0 unchecked=0"'
assert b-clean-world-states eval 'has "state=\"holds(root)\"" && has "state=\"holds(subpath)\"" && has "state=\"present\"" && has "state=\"on-branch\"" && has "state=\"open\"" && has "state=\"merged\""'
assert b-clean-world-quotes-the-fetched-commit eval 'has "source $src_root at=$on_main"'

# --- (c) the stale world --------------------------------------------------------------------------
want_stale=$'stale doodad-three path "cmd/gone.go" state="missing"\nstale doodad-three pr "example-org/example-repo#7" state="not-found"\nstale doodad-three symbol "cmd/x.go#Vanished" state="absent-from-file"'
cairn reader c audit-refs --scope gamma-notes
assert c-stale-world-exits-9 eval 'rc_is 9 && has "stale=3"'
assert c-stale-finding-set-equals-the-planted-set eval '[[ "$(stale_set)" == "$want_stale" ]]'
# Removing each planted defect drops EXACTLY its own finding.
no_missing=$'- `cmd/x.go` — holds\n- `cmd/x.go#Vanished` — DEFECT: absent'
no_absent=$'- `cmd/x.go` — holds\n- `cmd/gone.go` — DEFECT: missing'
f_missing='stale doodad-three path "cmd/gone.go" state="missing"'
f_absent='stale doodad-three symbol "cmd/x.go#Vanished" state="absent-from-file"'
f_404='stale doodad-three pr "example-org/example-repo#7" state="not-found"'
drop() { # drop <pointers> <refs> <finding-that-must-vanish>
  entry gamma-notes doodad-three "$2" "$1" '- 2000-01-02: a note.'
  cairn reader c audit-refs --scope gamma-notes
  rc_is 9 && [[ "$(stale_set)" == "$(grep -vxF -- "$3" <<<"$want_stale")" ]]
}
assert c-removing-the-missing-path-drops-only-it drop "$no_missing" "$gamma_refs" "$f_missing"
assert c-removing-the-absent-symbol-drops-only-it drop "$no_absent" "$gamma_refs" "$f_absent"
assert c-removing-the-404-pr-drops-only-it drop "$gamma_pointers" "" "$f_404"
entry gamma-notes doodad-three "$gamma_refs" "$gamma_pointers" '- 2000-01-02: a note.'

# --- (d) could not look ---------------------------------------------------------------------------
cairn reader d audit-refs --scope delta-notes
assert d-missing-remote-is-10 eval 'rc_is 10 && has "state=\"unchecked: remote unreachable\"" && has "could-not-look reason=checked=0"'
cairn reader d audit-refs --scope beta-notes
assert d-undeclared-is-10 eval 'rc_is 10 && has "could-not-look reason=sources=undeclared"'
cairn reader d audit-refs --scope epsilon-notes
assert d-checked-zero-is-10 eval 'rc_is 10 && has "checked=0 stale=0 unchecked=1" && has "could-not-look reason=checked=0"'
cairn reader d audit-refs --scope zeta-notes
assert d-clean-beside-unmapped-is-10 eval 'rc_is 10 && has "checked=1 stale=0" && has "source $src_unmapped state=\"unchecked: host not mapped\""'
cairn reader d audit-refs --scope eta-notes
assert d-control-one-claim-mapped-only-is-0 eval 'rc_is 0 && has "checked=1 stale=0 unchecked=0"'
# The PAIR: the mapped source's mirror exists (it was fetched), the unmapped one's does not (never
# contacted). The mirror directory is the digest of the canonical source (T4).
mirror_of() { printf '%s/cairn/mirrors/%s.git' "$work/xdg-$1" "$(printf '%s' "$2" | sha256sum | cut -d' ' -f1)"; }
assert d-unmapped-host-was-never-contacted eval '[[ -d "$(mirror_of d "$src_root")" && ! -e "$(mirror_of d "$src_unmapped")" ]]'

# --- (e) the pod and the UI never fetch -----------------------------------------------------------
deps() { go list -C "$src" -deps "$1" 2>/dev/null; }
importers_of_exec() {
  go list -C "$src" -deps -f '{{.ImportPath}} {{join .Imports " "}}' "$1" 2>/dev/null |
    awk '$1 ~ /^github.com\/ZacxDev\/cairn\// { for (i = 2; i <= NF; i++) if ($i == "os/exec") print $1 }' | sort
}
pod_deps="$(deps ./cmd/cairn-server)"
ui_deps="$(deps ./cmd/cairn-ui)"
cli_deps="$(deps ./cmd/cairn)"
out="$pod_deps"; rc=0
assert e-the-pod-links-neither-exec-nor-the-auditor eval '[[ -n "$pod_deps" ]] && ! grep -qx "os/exec" <<<"$pod_deps" && ! grep -qx "github.com/ZacxDev/cairn/internal/refaudit" <<<"$pod_deps"'
out="$(importers_of_exec ./cmd/cairn-ui)"
assert e-the-ui-links-no-auditor-and-exec-only-via-the-client eval '[[ -n "$ui_deps" ]] && ! grep -qx "github.com/ZacxDev/cairn/internal/refaudit" <<<"$ui_deps" && [[ "$out" == "github.com/ZacxDev/cairn/internal/client" ]]'
out="$cli_deps"
assert e-control-the-cli-links-both eval 'grep -qx "os/exec" <<<"$cli_deps" && grep -qx "github.com/ZacxDev/cairn/internal/refaudit" <<<"$cli_deps"'

# --- (f) reachability, not presence, not tags ----------------------------------------------------
cairn reader f audit-refs --scope theta-notes
assert f-pull-ref-only-is-off-branch-and-informational eval 'rc_is 0 && has "state=\"exists-off-branch\""'
g -C "$remote" tag v1 "$off_branch" || couldnt "tagging failed"
g -C "$remote" update-ref -d refs/pull/1/head || couldnt "deleting the pull ref failed"
cairn reader f audit-refs --scope theta-notes
assert f-deleted-pull-ref-tag-kept-is-not-reachable eval 'rc_is 10 && has "state=\"not-reachable\""'
mirror_has_object() { g -C "$(mirror_of f "$src_root")" cat-file -e "$off_branch" 2>/dev/null; }
assert f-the-object-is-still-in-the-mirror eval 'mirror_has_object'
cairn reader f2 audit-refs --scope theta-notes
assert f-a-fresh-mirror-agrees eval 'rc_is 10 && has "state=\"not-reachable\""'

# --- the shared controls ----------------------------------------------------------------------------
cairn reader x audit-refs --scope alpha-notes --json
assert json-carries-the-same-counts eval 'rc_is 0 && python3 -c "import json,sys; d=json.loads(sys.stdin.read()); assert (d[\"checked\"], d[\"stale\"], d[\"unchecked\"], d[\"exit\"]) == (6, 0, 0, 0)" <<<"$out"'
cairn reader x audit-refs --repo "$work"
assert no-scope-outside-a-repo-is-usage eval 'rc_is 2'
cairn reader x audit-refs --scope alpha-notes --bogus
assert unknown-flag-is-usage eval 'rc_is 2'

echo "SUMMARY e2e: passed=$passed failed=$failed expected=$EXPECTED"
if [[ $((passed + failed)) -ne $EXPECTED ]]; then
  couldnt "$((passed + failed)) assertions ran, $EXPECTED are declared"
fi
[[ $failed -eq 0 ]] && exit 0
exit 1
