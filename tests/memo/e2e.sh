#!/usr/bin/env bash
# The SCOPE-MAIL end-to-end check — closing-condition part 2 of claudedocs/plan-cairn-scope-mail.md
# (`### closing-condition`, "What `e2e.sh` asserts"), owned by S3 (decision 18) because only the
# BUILT memo verbs produce the exit codes, status tokens and blocks it asserts. Over the REAL
# binaries — `cairn-ui` with its client listener and a real PostgreSQL, `cairn-server` (the pod,
# and the operator commands that issue and revoke credentials), and the Go client — in the
# synthetic world `tests/memo/world.sh` builds.
#
#   usage: tests/memo/e2e.sh              # the check: exits 0 iff every assertion PASSes
#          tests/memo/e2e.sh --self-test  # the negative control: thirteen sabotaged builds, each
#                                         # of which must turn ITS OWN clause's assertion RED
#
# The eleven clauses, and the assertion(s) that carry each (one `PASS <name>` / `FAIL <name>` line
# per assertion, then `SUMMARY e2e: passed=N failed=M expected=E`):
#   (a) once per session — and the outsider's answer for alpha-notes is byte-identical to a scope
#       that does not exist (T12);
#   (b) who may send — a read-only credential's send exits 6 and stores nothing; the writer's lands;
#   (c) narrowing holds both ways — agents@host-b (beta) can neither send to nor read alpha-notes
#       (`scope-unreadable`), although its principal can write there;
#   (d) the fence holds on BOTH output paths — S0's 16-member hostile corpus (8 sent through the
#       client, 8 fence-breakers PLANTED by SQL; the 8 sent through the client are each refused at
#       6) prints as exactly one block, `count=16`, five previews and `and 11 more`; `memo-read`
#       prints one block of 16 bodies; every content line prefixed, no `memo.Unsafe` code point —
#       against the REAL listener AND against a RAW-serving stub (Guard D);
#   (e) a confident secret is refused at 6 naming its rule; the same body without it, and a body
#       only `entropy`/`key-context` match, are accepted;
#   (f) the per-scope quota (O12): the 51st send in a day exits 6 whichever sender makes it;
#   (g) retraction (O8 as S2 narrowed it): tombstone, NULLed text, events `sent` then `retracted`,
#       moderation by a scope admin, a reader refused, and decision 17's control — an AGENT
#       credential of an admin principal cannot moderate;
#   (h) an expired row (planted by SQL) is not delivered; an unexpired one is;
#   (i) a withdrawn read grant makes `memo-read` of an already-delivered memo `not-found`;
#   (j) two credentials of ONE principal are told apart by their labels, and stored apart;
#   (k) `cairn-server -revoke-credential` cuts a credential off within one refresh (+5 s), its
#       answer byte-identical to a random token's, while its sibling keeps working.
#
# Exit 0 all passed, 1 any failed, 2 COULD NOT VOUCH — a prerequisite missing (no Go toolchain, no
# curl or python3, no `CAIRN_PGTEST_DSN` and no `initdb`, a binary that did not build, a server that
# did not come up) or the assertion count is not the declared one. 🔴 NEVER 0 AND NEVER A SKIP WHEN A
# PREREQUISITE IS MISSING.
#
# 🔴 WHY A SELF-TEST AND NOT ONLY A GREEN: every assertion here is a grep, an exit code or a byte
# compare, which a harness wired to nothing passes. `--self-test` copies the tracked tree, breaks the
# ONE code path each clause pins (the `tests/control_mutants.py` pattern, on a scratch copy with no
# `.git` — never the working tree), rebuilds, reruns this script against the broken build and
# requires THAT clause's named assertion to go RED. Prints `sabotaged=13 caught=13`; a sabotage that
# leaves its clause green is exit 2, named.
set -uo pipefail

repo="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
# `E2E_SOURCE` is the tree the binaries are BUILT from; the self-test points it at a sabotaged copy.
src="${E2E_SOURCE:-$repo}"

# The number of assertions a full run makes. 🔴 PINNED, so an assertion that silently stopped
# running is exit 2 rather than a smaller green.
EXPECTED=35

if [[ "${1:-}" == "--self-test" ]]; then
  for tool in go git python3; do
    command -v "$tool" >/dev/null 2>&1 || { echo "COULD NOT VOUCH: \`$tool\` is not on PATH" >&2; exit 2; }
  done
  stwork="$(mktemp -d "${TMPDIR:-/tmp}/cairn-memo-e2e-selftest-XXXXXX")"
  trap 'rm -rf "$stwork"' EXIT
  sabotaged=0
  caught=0
  missed=()
  # sabotage <name> <file> <anchor> <replacement> <must FAIL (space-separated)> [must PASS …]
  # The anchor is EXACT text, possibly several lines, and must occur exactly ONCE in the file.
  sabotage() {
    local name="$1" file="$2" anchor="$3" replacement="$4" must_fail="$5" must_pass="${6:-}"
    local tree="$stwork/$name"
    mkdir -p "$tree"
    (cd "$src" && git ls-files -z -co --exclude-standard | xargs -0 cp --parents -t "$tree" 2>/dev/null)
    python3 - "$tree/$file" "$anchor" "$replacement" <<'PY' || exit 2
import sys
path, anchor, replacement = sys.argv[1], sys.argv[2], sys.argv[3]
text = open(path, encoding="utf-8").read()
n = text.count(anchor)
if n != 1:
    print(f"HARNESS ERROR: the anchor occurs {n} times in {path} (need exactly 1)", file=sys.stderr)
    sys.exit(2)
open(path, "w", encoding="utf-8").write(text.replace(anchor, replacement))
PY
    if cmp -s "$src/$file" "$tree/$file"; then
      echo "HARNESS ERROR: sabotage $name: the copy of $file is unchanged after the edit" >&2
      exit 2
    fi
    sabotaged=$((sabotaged + 1))
    local out rc ok=yes a
    out="$(E2E_SOURCE="$tree" "$repo/tests/memo/e2e.sh" 2>&1)"
    rc=$?
    rm -rf "$tree"
    if [[ $rc -eq 2 ]]; then
      echo "$out" | tail -20
      echo "HARNESS ERROR: sabotage $name: the sabotaged run could not vouch (exit 2)" >&2
      exit 2
    fi
    for a in $must_fail; do grep -qxF "FAIL $a" <<<"$out" || ok=no; done
    for a in $must_pass; do grep -qxF "PASS $a" <<<"$out" || ok=no; done
    if [[ $rc -eq 1 && $ok == yes ]]; then
      caught=$((caught + 1))
      echo "CAUGHT $name: exit $rc; FAIL $must_fail${must_pass:+; PASS $must_pass} ($(grep -c '^FAIL ' <<<"$out") assertion(s) red)"
    else
      missed+=("$name")
      echo "MISSED $name: exit $rc; wanted FAIL [$must_fail]${must_pass:+ and PASS [$must_pass]}; red were: $(grep '^FAIL ' <<<"$out" | tr '\n' ' ')"
    fi
  }
  # (a) the cursor never advances: nothing is recorded, so a session's second check re-delivers.
  sabotage a-cursor-never-advances internal/client/memo.go \
    $'\t\tif err := saveCursor(cursorPath, cursor); err != nil {' \
    $'\t\tif err := error(nil); err != nil {' \
    "a-second-check-of-the-session-is-silent"
  # (b) the send predicate asks READ instead of WRITE.
  sabotage b-send-checks-read internal/memo/policy.go \
    $'\treturn a.VisibleScopes(control.VerbWrite).Allows(scope)' \
    $'\treturn a.VisibleScopes(control.VerbRead).Allows(scope)' \
    "b-read-only-send-refused-nothing-stored"
  # (c) narrowing ignored. ⚠ NOT "send ignores Narrowed()" as the plan words it: no send-only seam
  #     exists — a credential's scope narrowing is applied ONCE, at authentication, by
  #     `control.Narrow`, and every surface inherits it. This breaks that one seam.
  sabotage c-scope-narrowing-ignored internal/control/resolve.go \
    $'\t\tif keep != nil {\n\t\t\tif _, wanted := keep[id]; !wanted {' \
    $'\t\tif false {\n\t\t\tif _, wanted := keep[id]; !wanted {' \
    "c-narrowed-credential-cannot-send-elsewhere"
  # (d1) the content-line prefix dropped: red on BOTH halves.
  sabotage d1-content-prefix-dropped internal/memo/block.go \
    $'\tcontentPrefix = "| "' \
    $'\tcontentPrefix = ""' \
    "d-real-check-one-block-count-16 d-stub-check-one-block-count-16"
  # (d2) `memo-read` prints the stored body without the render-side replacement: red on the STUB
  #      half only — the real listener's text is already rendered, which is why the stub exists.
  sabotage d2-full-body-unsanitised internal/memo/block.go \
    $'\t\t\tcontent = append(content, "  body: "+r.Body)' \
    $'\t\t\tcontent = append(content, "  body: "+memos[i].t().body)' \
    "d-stub-read-one-block-sixteen-bodies" "d-real-read-one-block-sixteen-bodies"
  # (e) the secret scan skipped.
  sabotage e-scan-skipped internal/memo/policy.go \
    $'\t\t_, hits := scan.String(text)' \
    $'\t\t_, hits := scan.String(text[:0])' \
    "e-confident-secret-refused-naming-its-rule"
  # (f) the quota counted per SENDER (credential) instead of per scope.
  sabotage f-quota-per-sender internal/pgstore/memos.go \
    $'\t\t`SELECT count(*) FROM memos WHERE scope_name = $1 AND created_at > $2`,' \
    $'\t\t`SELECT count(*) FROM memos WHERE scope_name = $1 AND created_at > $2 AND sender_credential_id IS NOT DISTINCT FROM \'`+string(caller.CredentialID)+`\'`,' \
    "f-fifty-first-send-refused-whoever-sends-it"
  # (g1) retraction keeps the body.
  sabotage g1-retract-keeps-body internal/pgstore/memos.go \
    $'\t\t`UPDATE memos SET retracted_at = $1, subject = NULL, body = NULL WHERE id = $2`, now, id); err != nil {' \
    $'\t\t`UPDATE memos SET retracted_at = $1 WHERE id = $2`, now, id); err != nil {' \
    "g-stored-text-nulled-events-sent-then-retracted"
  # (g2) `control.Narrow` ignores its VERB argument — which only (g)'s decision-17 control can see.
  sabotage g2-verb-narrowing-ignored internal/control/resolve.go \
    $'\t\tif verbs != nil {\n\t\t\tvs = vs.Intersect(*verbs)\n\t\t}' \
    $'\t\tif verbs != nil {\n\t\t\tvs = vs.Intersect(AllSet)\n\t\t}' \
    "g-agent-of-an-admin-principal-cannot-moderate"
  # (h) the expiry filter dropped from the live set.
  sabotage h-expiry-filter-dropped internal/pgstore/memos.go \
    $'const memoLive = `retracted_at IS NULL AND expires_at > $1`' \
    $'const memoLive = `retracted_at IS NULL AND $1::timestamptz IS NOT NULL`' \
    "h-expired-row-not-delivered"
  # (i) read authorised at SEND time instead of read time: `Get` no longer asks `MayRead` now, so a
  #     memo delivered while the grant stood stays readable after it is withdrawn.
  sabotage i-read-not-reauthorised internal/pgstore/memos.go \
    $'\tif !memo.MayRead(auth, scope) {\n\t\treturn memo.Stored{}, memo.ErrNotFound\n\t}' \
    $'\tif false {\n\t\treturn memo.Stored{}, memo.ErrNotFound\n\t}' \
    "i-withdrawn-grant-read-is-not-found"
  # (j) the sender line renders the principal without the credential label.
  sabotage j-sender-line-drops-the-label internal/memo/stored.go \
    $'\tb.WriteString(sanitize(display))\n\tif label != "" {\n\t\tb.WriteString(" via " + sanitize(label))\n\t}' \
    $'\tb.WriteString(sanitize(display))' \
    "j-sender-lines-name-each-credential"
  # (k) the revoke mode reports success without appending the event.
  sabotage k-revoke-appends-nothing cmd/cairn-server/revokecredential.go \
    $'\tr, err := control.RevokeCredential(ctx, store, id, "", time.Now().UTC())' \
    $'\tr, err := control.Revoked{}, error(nil)\n\t_ = time.Now\n\tif m, merr := store.Model(ctx); merr == nil {\n\t\tif c, ok := m.Credentials[id]; ok {\n\t\t\tr.Credential = c\n\t\t}\n\t}' \
    "k-revoked-credential-refused-within-deadline"
  echo "SUMMARY e2e-self-test: sabotaged=$sabotaged caught=$caught"
  if [[ ${#missed[@]} -gt 0 ]]; then
    echo "a sabotage that leaves its clause green is a clause that cannot fail: ${missed[*]}" >&2
    exit 2
  fi
  [[ $sabotaged -eq 13 && $caught -eq $sabotaged ]] && exit 0
  exit 2
fi

work="$(mktemp -d "${TMPDIR:-/tmp}/cairn-memo-e2e-XXXXXX")"
# shellcheck source=tests/memo/world.sh
source "$repo/tests/memo/world.sh"
world_up

passed=0
failed=0
assert() { # assert <name> <command…>
  local name="$1"; shift
  if "$@"; then
    echo "PASS $name"; passed=$((passed + 1))
  else
    echo "FAIL $name"; failed=$((failed + 1))
    { echo "  rc=$rc status=$status_line"; echo "  --- stdout"; sed 's/^/  | /' <<<"${out:0:1500}"; echo "  --- stderr"; sed 's/^/  | /' <<<"${err:0:800}"; } >&2
  fi
}
has() { grep -qF -- "$1" <<<"$out"; }
lacks() { ! grep -qF -- "$1" <<<"$out"; }
truncate_memos() { sql exec "TRUNCATE memos, memo_events RESTART IDENTITY" >/dev/null || couldnt "TRUNCATE failed"; }
count_memos() { sql query "SELECT count(*) FROM memos"; }
send() { # send <who> <scope> <subject> [body]
  cairn "$1" memo-send --scope "$2" --subject "$3" --body "${4:-body of $3}"
}
sent_id() { sed -n 's/^cairn: memo sent — id=\([0-9]*\) .*/\1/p' <<<"$out"; }
block_ok() { "$work/bin/memotool" block-check "$1" "$2" <"$work/stdout" >"$work/blockcheck" 2>&1 || { cat "$work/blockcheck" >&2; return 1; }; }
http_answer() { # http_answer <token> <path> → "<status>\n<body>"
  local code
  code="$(curl -sS -o "$work/http-body" -w '%{http_code}' -H "Authorization: Bearer $1" "$api_url$2" 2>/dev/null)" || code=000
  printf '%s\n%s' "$code" "$(cat "$work/http-body")"
}

# --- (a) once per session ------------------------------------------------------------------------
truncate_memos
send host-a alpha-notes "a-first"
[[ $rc == 0 ]] || couldnt "the first send failed (rc=$rc): $err"
cairn host-r memo-check --scope alpha-notes --session s-0001
assert a-first-check-of-a-session-prints-one-block eval '[[ $rc == 0 && $status_line == memo-status=new ]] && block_ok preview 1 && has "subject: a-first"'
cairn host-r memo-check --scope alpha-notes --session s-0001
assert a-second-check-of-the-session-is-silent eval '[[ $rc == 0 && -z "$out" && ! -s "$work/stdout" && $status_line == memo-status=none ]]'
cairn host-r memo-check --scope alpha-notes --session s-0002
assert a-another-session-sees-it-once eval '[[ $rc == 0 && $status_line == memo-status=new ]] && block_ok preview 1'
outsider_alpha="$(http_answer "$outsider_tok" "/client/v1/memos?scope=alpha-notes")"
outsider_absent="$(http_answer "$outsider_tok" "/client/v1/memos?scope=no-such-notes")"
assert a-outsider-answer-byte-identical-to-an-absent-scope eval '[[ "$outsider_alpha" == "$outsider_absent" && "$outsider_alpha" == 404* ]]'

# --- (b) who may send ----------------------------------------------------------------------------
before="$(count_memos)"
send host-r alpha-notes "b-from-a-reader"
reader_rc=$rc
after="$(count_memos)"
assert b-read-only-send-refused-nothing-stored eval '[[ $reader_rc == 6 && "$before" == "$after" ]]'
send host-a alpha-notes "b-from-a-writer"
assert b-writer-send-accepted eval '[[ $rc == 0 && "$(count_memos)" == $((after + 1)) ]]'

# --- (c) narrowing holds both ways -----------------------------------------------------------------
before="$(count_memos)"
send host-b alpha-notes "c-from-beta"
assert c-narrowed-credential-cannot-send-elsewhere eval '[[ $rc == 6 && "$(count_memos)" == "$before" ]]'
cairn host-b memo-check --scope alpha-notes --session s-c001
assert c-narrowed-credential-check-is-scope-unreadable eval '[[ $rc == 3 && -z "$out" && $status_line == memo-status=scope-unreadable ]]'
cairn host-a memo-check --scope alpha-notes --session s-c002
a_sees="$rc/$status_line"
send host-a alpha-notes "c-from-alpha"
assert c-unnarrowed-credential-sees-and-sends eval '[[ $a_sees == 0/memo-status=new && $rc == 0 ]]'

# --- (d) the fence holds, on BOTH output paths -----------------------------------------------------
truncate_memos
mkdir -p "$work/hostile"
python3 - "$src/internal/memo/testdata/hostile.json" "$work/hostile" "$user_a" "$hosta_crd" <<'PY' || couldnt "the hostile corpus could not be laid out"
import json, os, sys
corpus, out, user, crd = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
c = json.load(open(corpus))
assert len(c["fence_breaking"]) == 8 and len(c["send_accepted"]) == 8, "the corpus is not 8 + 8"
rows = []
for half in ("send_accepted", "fence_breaking"):
    with open(os.path.join(out, half + ".list"), "w") as idx:
        for m in c[half]:
            open(os.path.join(out, m["name"] + ".txt"), "wb").write(m["text"].encode("utf-8"))
            # 🔴 PostgreSQL TEXT cannot hold U+0000 (measured: "null character not permitted",
            # SQLSTATE 54000), so the NUL member is PLANTED with U+0001 in its place — still a Cc
            # control, still refused at send. The RAW stub serves the member verbatim.
            plant = m["text"].replace("\x00", "\x01")
            open(os.path.join(out, m["name"] + ".plant"), "wb").write(plant.encode("utf-8"))
            idx.write(m["field"] + " " + m["name"] + "\n")
            i = len(rows) + 1
            r = {"ID": i, "Scope": "alpha-notes", "Sender": "writer-a via agents@host-a (user)",
                 "CreatedAt": "2000-01-02T03:%02d:05Z" % i, "ExpiresAt": "2000-01-05",
                 "Subject": "hostile " + m["name"], "Body": "", "Tombstone": ""}
            r["Subject" if m["field"] == "subject" else "Body"] = m["text"]
            if m["field"] == "subject":
                r["Body"] = "carried in the subject"
            rows.append(r)
json.dump(rows, open(os.path.join(out, "raw-rows.json"), "w"))
PY
sent_ok=0
while read -r field name; do
  if [[ $field == subject ]]; then
    cairn host-a memo-send --scope alpha-notes --subject "$(cat "$work/hostile/$name.txt")" --body "carried in the subject"
  else
    cairn host-a memo-send --scope alpha-notes --subject "hostile $name" --body-file "$work/hostile/$name.txt"
  fi
  [[ $rc == 0 ]] && sent_ok=$((sent_ok + 1))
done <"$work/hostile/send_accepted.list"
assert d-the-eight-send-accepted-members-are-accepted eval '[[ $sent_ok == 8 ]]'
refused_ok=0
while read -r field name; do
  cairn host-a memo-send --scope alpha-notes --subject "hostile $name" --body-file "$work/hostile/$name.txt"
  [[ $rc == 6 ]] && grep -qF "[characters]" <<<"$err" && refused_ok=$((refused_ok + 1))
done <"$work/hostile/fence_breaking.list"
[[ "$(count_memos)" == 8 ]] || couldnt "after the eight accepted sends the table holds $(count_memos) rows, not 8"
assert d-the-eight-fence-breakers-are-refused-at-send eval '[[ $refused_ok == 8 && "$(count_memos)" == 8 ]]'
while read -r field name; do
  sql exec "INSERT INTO memos (scope_name, sender_kind, sender_id, sender_credential_id, sender_display_at_send, subject, body, created_at, expires_at)
            VALUES ('alpha-notes', 'user', \$1, \$2, 'writer-a', \$3, \$4, now(), now() + interval '1 hour')" \
    "$user_a" "$hosta_crd" "hostile $name" "@$work/hostile/$name.plant" >/dev/null || couldnt "planting $name by SQL failed"
done <"$work/hostile/fence_breaking.list"
[[ "$(count_memos)" == 16 ]] || couldnt "the planted world holds $(count_memos) memos, not 16"
cairn host-r memo-check --scope alpha-notes --session s-d001
assert d-real-check-one-block-count-16 eval '[[ $rc == 0 && $status_line == memo-status=new ]] && block_ok preview 16 &&
  has "| and 11 more on alpha-notes: cairn memo-read --scope alpha-notes"'
cairn host-r memo-read --scope alpha-notes
assert d-real-read-one-block-sixteen-bodies eval '[[ $rc == 0 ]] && block_ok full 16'
# The RAW-serving stub half (Guard D): the same 16 rows, UNRENDERED, behind the listener's routes.
stub_tok="$(token)"
"$work/bin/memotool" raw-stub "$work/stub-addr" "$work/hostile/raw-rows.json" "$stub_tok" 2>"$work/stub.log" &
pids+=("$!")
for _ in $(seq 1 200); do [[ -s "$work/stub-addr" ]] && break; sleep 0.05; done
[[ -s "$work/stub-addr" ]] || couldnt "the raw stub did not come up: $(cat "$work/stub.log")"
client_home stub "$stub_tok" "http://$(cat "$work/stub-addr")"
cairn stub memo-check --scope alpha-notes --session s-d002
assert d-stub-check-one-block-count-16 eval '[[ $rc == 0 && $status_line == memo-status=new ]] && block_ok preview 16'
cairn stub memo-read --scope alpha-notes
assert d-stub-read-one-block-sixteen-bodies eval '[[ $rc == 0 ]] && block_ok full 16'

# --- (e) secret refusal -----------------------------------------------------------------------------
truncate_memos
# Vendor-shaped tokens are GENERATED here from a seeded RNG, never committed (the #216 corpus rule).
read -r gh_token entropy_text key_context_text < <(python3 -c '
import random
r = random.Random(20000102)
alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
pick = lambda n: "".join(r.choice(alnum) for _ in range(n))
print("ghp_" + pick(36), "build-id-" + pick(48), "DATABASE_PASSWORD=" + pick(6) + "x9")')
cairn host-a memo-send --scope alpha-notes --subject "e-with-secret" --body "the token is $gh_token for the deploy"
assert e-confident-secret-refused-naming-its-rule eval '[[ $rc == 6 && "$(count_memos)" == 0 ]] && grep -qF "rule github-token" <<<"$err" && ! grep -qF "$gh_token" <<<"$err"'
cairn host-a memo-send --scope alpha-notes --subject "e-without-secret" --body "the token is  for the deploy"
assert e-same-body-without-the-token-accepted eval '[[ $rc == 0 && "$(count_memos)" == 1 ]]'
cairn host-a memo-send --scope alpha-notes --subject "e-entropy" --body "the build id is $entropy_text if you need it"
e1=$rc
cairn host-a memo-send --scope alpha-notes --subject "e-key-context" --body "set $key_context_text before the run"
assert e-entropy-and-key-context-only-bodies-accepted eval '[[ $e1 == 0 && $rc == 0 && "$(count_memos)" == 3 ]]'

# --- (f) the per-scope quota (O12) --------------------------------------------------------------------
truncate_memos
quota_ok=0
for i in $(seq 1 25); do
  send host-a alpha-notes "f-a-$i"; [[ $rc == 0 ]] && quota_ok=$((quota_ok + 1))
  send host-c alpha-notes "f-c-$i"; [[ $rc == 0 ]] && quota_ok=$((quota_ok + 1))
done
assert f-fifty-sends-split-across-two-senders-accepted eval '[[ $quota_ok == 50 ]]'
send host-a alpha-notes "f-a-51"
fa=$rc
fa_err="$err"
send host-c alpha-notes "f-c-51"
assert f-fifty-first-send-refused-whoever-sends-it eval '[[ $fa == 6 && $rc == 6 && "$(count_memos)" == 50 ]] && grep -qF "[quota]" <<<"$fa_err"'
send host-b beta-notes "f-beta-same-minute"
assert f-another-scope-is-unaffected eval '[[ $rc == 0 ]]'

# --- (g) retraction (O8) --------------------------------------------------------------------------------
truncate_memos
send host-a alpha-notes "g-one";   g1="$(sent_id)"
send host-a alpha-notes "g-two";   g2="$(sent_id)"
send host-a alpha-notes "g-three"; g3="$(sent_id)"
send host-d alpha-notes "g-four";  g4="$(sent_id)"
[[ -n "$g1" && -n "$g2" && -n "$g3" && -n "$g4" ]] || couldnt "the four (g) memos were not all sent"
cairn host-a memo-retract --scope alpha-notes --id "$g1"
r1=$rc
cairn host-r memo-check --scope alpha-notes --session s-g001
assert g-retracted-memo-not-delivered-to-a-new-session eval '[[ $r1 == 0 && $rc == 0 ]] && block_ok preview 3 && lacks "g-one" && has "subject: g-four"'
cairn host-a memo-read --scope alpha-notes --id "$g1"
assert g-tombstone-names-the-retractor-and-shows-no-text eval '[[ $rc == 0 ]] && block_ok full 1 &&
  has "|   retracted by writer-a via agents@host-a (user) at " && lacks "g-one"'
stored="$(sql query "SELECT subject IS NULL, body IS NULL FROM memos WHERE id = \$1" "$g1")"
events="$(sql query "SELECT kind, actor_kind, actor_id, actor_credential_id FROM memo_events WHERE memo_id = \$1 ORDER BY seq" "$g1")"
want_events="$(printf 'sent\tuser\t%s\t%s\nretracted\tuser\t%s\t%s' "$user_a" "$hosta_crd" "$user_a" "$hosta_crd")"
assert g-stored-text-nulled-events-sent-then-retracted eval '[[ "$stored" == "$(printf "true\ttrue")" && "$events" == "$want_events" ]]'
cairn host-d memo-retract --scope alpha-notes --id "$g2"
r2=$rc
cairn host-d memo-read --scope alpha-notes --id "$g2"
assert g-a-scope-admin-retracts-and-is-named eval '[[ $r2 == 0 ]] && has "|   retracted by admin-d via admin@host-d (user) at "'
cairn host-r memo-retract --scope alpha-notes --id "$g3"
assert g-a-reader-cannot-retract eval '[[ $rc == 6 && "$(sql query "SELECT retracted_at IS NULL FROM memos WHERE id = \$1" "$g3")" == true ]]'
# Decision 17's control: agents@host-c is NOT the sender of g-four (admin@host-d is), and its
# principal writer-a IS an admin of alpha-notes — so only the credential's VERB narrowing refuses.
cairn host-c memo-retract --scope alpha-notes --id "$g4"
assert g-agent-of-an-admin-principal-cannot-moderate eval '[[ $rc == 6 && "$(sql query "SELECT retracted_at IS NULL FROM memos WHERE id = \$1" "$g4")" == true ]]'

# --- (h) expiry ---------------------------------------------------------------------------------------------
truncate_memos
for row in "h-expired:now() - interval '1 second'" "h-live:now() + interval '1 minute'"; do
  sql exec "INSERT INTO memos (scope_name, sender_kind, sender_id, sender_credential_id, sender_display_at_send, subject, body, created_at, expires_at)
            VALUES ('alpha-notes', 'user', \$1, \$2, 'writer-a', \$3, 'planted', now() - interval '10 seconds', ${row#*:})" \
    "$user_a" "$hosta_crd" "${row%%:*}" >/dev/null || couldnt "planting ${row%%:*} failed"
done
cairn host-r memo-check --scope alpha-notes --session s-h001
assert h-expired-row-not-delivered eval '[[ $rc == 0 ]] && lacks "h-expired"'
assert h-unexpired-row-delivered eval '[[ $rc == 0 && $status_line == memo-status=new ]] && block_ok preview 1 && has "subject: h-live"'

# --- (i) a withdrawn grant --------------------------------------------------------------------------------
truncate_memos
send host-a alpha-notes "i-one"; i1="$(sent_id)"
cairn host-r memo-read --scope alpha-notes --id "$i1"
[[ $rc == 0 ]] || couldnt "(i): host-r could not read the memo before the grant was withdrawn (rc=$rc)"
"$work/bin/memotool" journal-append "$journal" "{\"kind\":\"grant-revoked\",\"at\":\"2000-01-01T00:00:01Z\",\"grant_id\":\"$grant_b\"}" >/dev/null ||
  couldnt "(i): the grant could not be revoked in the journal"
i_start=$SECONDS
i_seen=no
while (( SECONDS - i_start <= 35 )); do
  cairn host-r memo-read --scope alpha-notes --id "$i1"
  if [[ $rc == 3 ]] && grep -qF "not-found" <<<"$err"; then i_seen=yes; break; fi
  sleep 0.5
done
echo "  (i) the withdrawn grant took effect after $((SECONDS - i_start)) s (deadline 35 s)"
assert i-withdrawn-grant-read-is-not-found eval '[[ $i_seen == yes && -z "$out" ]]'
cairn host-a memo-read --scope alpha-notes --id "$i1"
assert i-the-writers-read-of-the-same-id-still-works eval '[[ $rc == 0 ]] && has "subject: i-one"'

# --- (j) which agent sent it (O9) ---------------------------------------------------------------------------
truncate_memos
send host-a alpha-notes "j-from-a"
send host-c alpha-notes "j-from-c"
cairn host-a memo-check --scope alpha-notes --session s-j001
assert j-sender-lines-name-each-credential eval '[[ $rc == 0 ]] && block_ok preview 2 &&
  has "from writer-a via agents@host-a (user) · alpha-notes" && has "from writer-a via agents@host-c (user) · alpha-notes"'
j_creds="$(sql query "SELECT count(DISTINCT sender_credential_id), count(DISTINCT (sender_kind, sender_id)) FROM memos")"
assert j-one-principal-two-credentials-stored eval '[[ "$j_creds" == "$(printf "2\t1")" ]]'

# --- (k) agent revocation (O9) ------------------------------------------------------------------------------
truncate_memos
random_tok="$(token)"
journal_cmd -revoke-credential -credential "$hosta_crd" >"$work/revoke.log" 2>&1 || couldnt "(k): -revoke-credential failed: $(cat "$work/revoke.log")"
k_start=$SECONDS
k_done=no
sibling_ok=yes
while (( SECONDS - k_start <= 35 )); do
  check_ans="$(http_answer "$hosta_tok" "/client/v1/memos?scope=alpha-notes")"
  random_ans="$(http_answer "$random_tok" "/client/v1/memos?scope=alpha-notes")"
  # The SEND route, probed with a body the listener refuses as MALFORMED (400) once a caller is
  # authenticated: authentication is decided first, so this answers the 401 when revoked and can
  # never store a memo while the revocation is still pending (a real send here, polled every half
  # second for up to 35 s, ate the scope's quota — measured on this script's first run).
  send_code="$(curl -sS -o "$work/k-send" -w '%{http_code}' -H "Authorization: Bearer $hosta_tok" -H 'Content-Type: application/json' \
    --data '{}' "$api_url/client/v1/memos" 2>/dev/null)"
  send_ans="$(printf '%s\n%s' "$send_code" "$(cat "$work/k-send")")"
  cairn host-c memo-check --scope alpha-notes --session "s-k$SECONDS"
  [[ $rc == 0 ]] || sibling_ok=no
  if [[ "$check_ans" == 401* && "$check_ans" == "$random_ans" && "$send_ans" == "$random_ans" ]]; then
    k_done=yes
    break
  fi
  sleep 0.5
done
echo "  (k) revocation latency measured: $((SECONDS - k_start)) s (deadline refreshInterval 30 s + 5 s)"
cairn host-a memo-check --scope alpha-notes --session s-k-client
k_client="$rc/$status_line"
assert k-revoked-credential-refused-within-deadline eval '[[ $k_done == yes && $k_client == 3/memo-status=unconfigured ]]'
send host-c alpha-notes "k-sibling-sends"
assert k-sibling-credential-keeps-working-throughout eval '[[ $sibling_ok == yes && $rc == 0 ]]'

echo "SUMMARY e2e: passed=$passed failed=$failed expected=$EXPECTED"
if [[ $((passed + failed)) -ne $EXPECTED ]]; then
  couldnt "$((passed + failed)) assertions ran, $EXPECTED are declared"
fi
[[ $failed -eq 0 ]] && exit 0
exit 1
