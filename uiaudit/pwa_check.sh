#!/usr/bin/env bash
# uiaudit/pwa_check.sh — the mobile plan's CLOSING-CONDITION instrument
# (`claudedocs/plan-cairn-mobile-pwa.md`, "closing-condition").
#
#   uiaudit/pwa_check.sh              # run every wired clause over this tree
#   uiaudit/pwa_check.sh --self-test  # prove each clause can go RED on its own sabotage
#
# 🔴 AN ORCHESTRATOR, NEVER A SECOND IMPLEMENTATION (audit D4). Each clause lives in ONE place and
# this script only boots it and reads ITS verdict:
#   (a) installability            -> `TestPWAClauses/a_installability` (uiaudit/pwa_test.go)
#   (b) per-instance name         -> `TestPWAClauses/b_name`           (uiaudit/pwa_test.go)
#   (b) per-instance icon         -> `TestPWAClauses/b_icon`           (uiaudit/pwa_test.go)
#   (c) touch reachability,       -> the uiaudit WALK's `refuseWalkRegressions` (uiaudit/main.go),
#       target size, input font      run through `uiaudit/run.sh` exactly as CI runs it
# (d) is S3's and (b: screenshots), (e) are S4's: NOT wired here until those slices land.
#
# EXIT: 0 every wired check PASSED · 1 a check FAILED (or, with --self-test, a sabotage was not
# caught by its own clause) · 2 COULD NOT VOUCH — chromium or a built cairn-ui is missing, a
# harness step produced no verdict, or one of the script's own controls misbehaved. 2 is never a
# pass and never a skip.
#
# 🔴 VERDICTS ARE READ FROM THE RUNNERS' OWN RESULT LINES, NEVER FROM AN EXIT CODE ALONE. A subtest
# is PASS only on its `--- PASS:` line and FAIL only on its `--- FAIL:` line; a run that printed
# neither is a harness problem (exit 2), because a go test that never reached the subtest exits
# non-zero exactly like one whose subtest failed. The walk's three (c) checks are PASS only on their
# own `… refusal PASSED` lines with rc 0, and FAIL only on their own refusal headline. A (c) check
# with neither, in a walk that REFUSED, is NOT_MEASURED (masked by a sibling's refusal; see
# `verdict`) — the run still exits 1, because the refusal that masked it is a FAIL. A walk that
# refused on a (c) class none of the three names (horizontal overflow) prints `c_walk FAIL`.
#
# ENV: PWA_CHECK_WORK — the PARENT of the work dir (default $TMPDIR); PWA_CHECK_KEEP=1 keeps the work
# dir (removed on every exit path otherwise); PWA_CHECK_PORT; PWA_CHECK_SABOTAGES — a subset for
# --self-test debugging (a subset always exits 1). The CAIRN_AUDIT_* push credentials are REMOVED
# from every walk this script runs.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/.." && pwd)"
port="${PWA_CHECK_PORT:-18791}"

# 🔴 THE WORK DIR IS ALWAYS A FRESH `mktemp` DIRECTORY THE SCRIPT OWNS, AND IT IS REMOVED ON EVERY EXIT
# PATH. One `--self-test` leaves seven tree copies, seven builds and four walks behind — measured at
# ~305 MB before this trap existed. `PWA_CHECK_WORK` names the PARENT it is created under (default
# `$TMPDIR`), never the directory itself, so a caller's directory is never what gets deleted.
# `PWA_CHECK_KEEP=1` keeps it, for reading the logs after a failure.
work="$(mktemp -d "${PWA_CHECK_WORK:-${TMPDIR:-/tmp}}/pwa-check-XXXXXX")" || {
  echo "pwa_check: COULD NOT VOUCH — cannot create a work dir under ${PWA_CHECK_WORK:-${TMPDIR:-/tmp}}" >&2
  exit 2
}
cleanup() {
  if [ "${PWA_CHECK_KEEP:-}" = 1 ]; then
    echo "pwa_check: work dir KEPT (PWA_CHECK_KEEP=1): $work" >&2
  else
    rm -rf -- "$work"
  fi
}
trap cleanup EXIT

could_not_vouch() {
  echo "pwa_check: COULD NOT VOUCH — $*" >&2
  echo "pwa_check: (the work dir is removed on exit; re-run with PWA_CHECK_KEEP=1 to read its logs)" >&2
  exit 2
}

# The six checks, in report order, and the message that is each one's OWN verdict when it fails.
checks=(a_installability b_name b_icon c_reachability c_target_size c_input_font)
declare -A own_message=(
  [a_installability]="pwa clause (a) installability"
  [b_name]="pwa clause (b) name"
  [b_icon]="pwa clause (b) icon"
  [c_reachability]="TOUCH REACHABILITY FAILED"
  [c_target_size]="TOUCH TARGET SIZE (WCAG 2.5.8"
  [c_input_font]="INPUT FONT UNDER 16px"
)
declare -A walk_passed=(
  [c_reachability]="TOUCH REACHABILITY refusal PASSED"
  [c_target_size]="TOUCH TARGET SIZE refusal PASSED"
  [c_input_font]="INPUT FONT refusal PASSED"
)

# ---- preconditions: each missing one is exit 2, named --------------------------------------------
chromium=""
for c in chromium chromium-browser google-chrome google-chrome-stable headless-shell; do
  if command -v "$c" > /dev/null 2>&1; then chromium="$c"; break; fi
done
[ -n "$chromium" ] || could_not_vouch "no chromium on PATH (looked for chromium, chromium-browser, google-chrome, google-chrome-stable, headless-shell)"
command -v go > /dev/null 2>&1 || could_not_vouch "no go toolchain on PATH, so no cairn-ui can be built"
command -v python3 > /dev/null 2>&1 || could_not_vouch "no python3 on PATH: the synthetic world is built by tests/reader_fixtures.py"
echo "pwa_check: chromium: $("$chromium" --version 2>/dev/null || echo "$chromium (version unreadable)")"
echo "pwa_check: work dir $work"

# run_ab <tree> <out>: clauses (a) and (b) — `TestPWAClauses` over a cairn-ui built from <tree>.
run_ab() {
  local tree="$1" out="$2"
  mkdir -p "$out/bin"
  if ! ( cd "$tree" && go build -o "$out/bin/cairn-ui" ./cmd/cairn-ui ) > "$out/build.log" 2>&1 \
      || [ ! -x "$out/bin/cairn-ui" ]; then
    echo "missing built cairn-ui (see $out/build.log)" > "$out/harness"
    return
  fi
  ( cd "$tree/uiaudit" && UIAUDIT_REPO_ROOT="$tree" UIAUDIT_CAIRN_UI="$out/bin/cairn-ui" \
      go test -count=1 -v -run '^TestPWAClauses$' . ) > "$out/ab.log" 2>&1
}

# run_c <tree> <out> <port>: clause (c) — the walk, through the tree's own run.sh, as CI runs it.
#
# 🔴 WITH THE AUDIT HUB'S CREDENTIALS REMOVED. A walk that passes PUSHES when all four are in the
# environment, and a sabotaged tree is not something to publish as a run. `env -u` drops every one
# for the walk's process alone; the walk then takes its documented no-credentials branch, and
# `walk_pushed` refuses to vouch if a push confirmation appears anyway.
audit_vars=(CAIRN_AUDIT_PUSH_URL CAIRN_AUDIT_PUSH_TOKEN CAIRN_AUDIT_API_URL CAIRN_AUDIT_API_TOKEN)
run_c() {
  local tree="$1" out="$2" p="$3" unset_args=() v
  for v in "${audit_vars[@]}"; do unset_args+=(-u "$v"); done
  ( env "${unset_args[@]}" UIAUDIT_WORK="$out/walk" UIAUDIT_PORT="$p" "$tree/uiaudit/run.sh" ) > "$out/c.log" 2>&1
  echo $? > "$out/c.rc"
}
# walk_refused: the walk ran to its verdict and REFUSED (one error carrying every refusal it found).
walk_refused() { [ -f "$1/c.log" ] && grep -qE "the walk measured [0-9]+ regression class\(es\)" "$1/c.log"; }
walk_pushed() { [ -f "$1/c.log" ] && grep -qF "uiaudit: PUSH CONFIRMED" "$1/c.log"; }

# verdict <out> <check>: PASS, FAIL, NOT_MEASURED, or NONE (no result line: a harness problem).
#
# 🔴 NOT_MEASURED EXISTS BECAUSE THE WALK PRINTS ITS `… refusal PASSED` LINES ONLY WHEN *EVERY*
# REFUSAL PASSED (`refuseWalkRegressions` returns all refusals as ONE error first). So when one (c)
# check fires, its siblings have no PASS line and no headline of their own — that is "masked by a
# sibling's refusal", not "the harness produced nothing". The first version read it as NONE and exited
# 2 naming the WRONG check on every real (c) failure. NOT_MEASURED is given only when the walk's own
# refusal line ("the walk measured N regression class(es)") is present; without it, NONE stands.
verdict() {
  local out="$1" check="$2"
  case "$check" in
    a_*|b_*)
      [ -f "$out/ab.log" ] || { echo NONE; return; }
      if grep -qF -- "--- FAIL: TestPWAClauses/$check " "$out/ab.log"; then echo FAIL
      elif grep -qF -- "--- PASS: TestPWAClauses/$check " "$out/ab.log"; then echo PASS
      else echo NONE; fi ;;
    c_*)
      [ -f "$out/c.log" ] || { echo NONE; return; }
      if grep -qF -- "${own_message[$check]}" "$out/c.log"; then echo FAIL
      elif [ "$(cat "$out/c.rc")" = 0 ] && grep -qF -- "${walk_passed[$check]}" "$out/c.log"; then echo PASS
      elif walk_refused "$out"; then echo NOT_MEASURED
      else echo NONE; fi ;;
  esac
}

# 🔴 THE CONTROL INSIDE CLAUSE (a): its unarmed boot must read exactly [no-manifest]. If it did not,
# `pwa_test.go` says `pwa clause (a) CONTROL`, and that is a misbehaving control — exit 2, not 1.
control_misbehaved() { [ -f "$1/ab.log" ] && grep -qF "pwa clause (a) CONTROL" "$1/ab.log"; }

# plain_run <tree> <out>: every wired check over <tree>; prints one line per check and RETURNS the exit
# code (0, 1 or 2) instead of exiting, so `--self-test` can run this very loop over a sabotaged tree.
plain_run() {
  local tree="$1" out="$2" c v pass=0 fail=0 nm=0
  mkdir -p "$out"
  run_ab "$tree" "$out"
  if [ -f "$out/harness" ]; then echo "pwa_check: COULD NOT VOUCH — $(cat "$out/harness")"; return 2; fi
  if control_misbehaved "$out"; then
    echo "pwa_check: COULD NOT VOUCH — clause (a)'s unarmed control did not read [no-manifest]"; return 2
  fi
  run_c "$tree" "$out" "$port"
  if walk_pushed "$out"; then
    echo "pwa_check: COULD NOT VOUCH — the walk PUSHED to the audit hub although its credentials were removed"; return 2
  fi
  if grep -qF "push skipped (no credentials)" "$out/c.log"; then
    echo "pwa_check: walk push           SKIPPED (CAIRN_AUDIT_* removed for the walk)"
  fi
  for c in "${checks[@]}"; do
    v=$(verdict "$out" "$c")
    printf 'pwa_check: %-18s %s\n' "$c" "$v"
    case "$v" in
      PASS) pass=$((pass + 1)) ;;
      FAIL) fail=$((fail + 1))
            grep -hF -- "${own_message[$c]}" "$out"/ab.log "$out"/c.log 2>/dev/null | head -3 | cut -c1-240 | sed 's/^/pwa_check:     /' ;;
      NOT_MEASURED) nm=$((nm + 1)) ;;
      *) echo "pwa_check: COULD NOT VOUCH — check $c produced NO verdict line"; return 2 ;;
    esac
  done
  # The walk refused on a class none of the three headlines names (horizontal overflow is part of (c)
  # too): that is a FAILURE of clause (c), printed as such, never a pass by absence.
  if walk_refused "$out" && ! grep -qF -e "${own_message[c_reachability]}" -e "${own_message[c_target_size]}" \
      -e "${own_message[c_input_font]}" "$out/c.log"; then
    fail=$((fail + 1))
    echo "pwa_check: c_walk             FAIL (the walk refused on another clause (c) class:)"
    grep -hE -A2 "the walk measured [0-9]+ regression class" "$out/c.log" | head -3 | cut -c1-240 | sed 's/^/pwa_check:     /'
  fi
  echo "pwa_check: ${#checks[@]} check(s): $pass PASS, $fail FAIL, $nm not measured (masked by a sibling refusal)"
  [ "$fail" = 0 ] && [ "$pass" = "${#checks[@]}" ] && return 0
  return 1
}

# ---- the plain run ----------------------------------------------------------------------------------
if [ "${1:-}" != "--self-test" ]; then
  [ $# -eq 0 ] || { echo "usage: $0 [--self-test]" >&2; exit 2; }
  plain_run "$root" "$work/check"
  exit $?
fi

# ---- --self-test ------------------------------------------------------------------------------------
# One sabotage per check, each on a SCRATCH COPY of the tree with no `.git` (the
# `tests/control_mutants.py` pattern): a copy cannot commit to the real branch, and an interrupt
# leaves the checkout untouched. Each must be caught by its OWN check's message.
#
# 🔴 THE COPY IS THE TRACKED + UNTRACKED-NOT-IGNORED FILE LIST, NEVER `cp -a` OF THE DIRECTORY: a base
# clone can hold other agents' worktrees under an ignored directory, and a recursive copy would copy
# them all. 🔴 AND THE POSITIVE CONTROL IS THE SAME COPY MECHANICS WITH NO EDIT, which must pass every
# check — otherwise "caught" cannot be told from "the copied tree never built".
make_copy() {
  local dst="$1" sabotage="$2"
  python3 - "$root" "$dst" "$sabotage" <<'PY'
import os, shutil, subprocess, sys
root, dst, sabotage = sys.argv[1], sys.argv[2], sys.argv[3]
names = subprocess.run(["git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
                       check=True, capture_output=True).stdout.split(b"\0")
copied = 0
for raw in names:
    rel = raw.decode()
    if not rel or rel.endswith("/"):
        continue  # a nested repository or worktree directory: never copied
    src = os.path.join(root, rel)
    if not os.path.lexists(src):
        continue  # tracked and deleted in the working tree
    out = os.path.join(dst, rel)
    os.makedirs(os.path.dirname(out), exist_ok=True)
    shutil.copy2(src, out, follow_symlinks=False)
    copied += 1
if os.path.exists(os.path.join(dst, ".git")):
    sys.exit(f"pwa_check: the copy at {dst} carries a .git, so a command inside it could reach the real repository")
if copied < 50:
    sys.exit(f"pwa_check: only {copied} file(s) copied — the file list is not this tree")

# 🔴 THE SABOTAGE LEDGER. Each edit asserts its occurrence COUNT, so a pattern that drifted out of the
# source is a loud harness error rather than a no-op scored "not caught" (or, worse, "caught" by an
# unrelated failure). `append` adds text at the end of the file instead of replacing.
SABOTAGES = {
    "a_installability": ("internal/ui/pwa.go",
        '\t\th.Link(h.Rel("manifest"), h.Href(ManifestPath)),\n', "", 1),
    "b_name": ("internal/ui/pwa.go",
        "Name: a.Name, ShortName: a.ShortName,", 'Name: "cairn", ShortName: a.ShortName,', 1),
    "b_icon": ("internal/ui/pwa.go",
        'if f.Variant != a.IconVariant || f.Kind.ManifestPurpose == "" {',
        'if f.Variant != IconVariants()[0] || f.Kind.ManifestPurpose == "" {', 1),
    "c_reachability": ("uiaudit/browser.go",
        "return emulation.SetTouchEmulationEnabled(true).WithMaxTouchPoints(touchPoints)",
        "return emulation.SetTouchEmulationEnabled(false)", 1),
    # The adjacent-12px shape S0's control measured RED; a LONE small target passes 2.5.8's spacing
    # exception, so shrinking one element would not be a sabotage of this check at all.
    "c_target_size": ("internal/ui/app.css", None,
        "\n@media (pointer: coarse) { .view-tab { min-height: 12px !important; min-width: 12px !important; "
        "width: 12px !important; height: 12px !important; padding: 0 !important; overflow: hidden !important; } "
        ".view-tabs { column-gap: 0 !important; row-gap: 0 !important; } }\n", 0),
    # Revert S1's 16px input rule to the base's `text-sm` (14px).
    "c_input_font": ("internal/ui/app.css", "font-size: max(16px, 1em);", "font-size: 0.875rem;", 1),
}
if sabotage == "none":
    sys.exit(0)
path, old, new, count = SABOTAGES[sabotage]
target = os.path.join(dst, path)
text = open(target, encoding="utf-8").read()
if old is None:
    text += new
else:
    n = text.count(old)
    if n != count:
        sys.exit(f"pwa_check: sabotage {sabotage}: {old!r} occurs {n} time(s) in {path}, want {count}")
    text = text.replace(old, new)
open(target, "w", encoding="utf-8").write(text)
PY
}

command -v git > /dev/null 2>&1 || could_not_vouch "no git on PATH, so the tree's file list cannot be read"

# Positive control: the unedited copy passes every check.
ctl="$work/self-test/control"
rm -rf "$ctl"; mkdir -p "$ctl/tree"
make_copy "$ctl/tree" none || could_not_vouch "the scratch copy could not be made"
echo "pwa_check: positive control (an UNEDITED copy, through the plain loop) ..."
plain_run "$ctl/tree" "$ctl" > "$ctl/plain.out" 2>&1
prc=$?
if [ "$prc" != 0 ]; then
  sed 's/^/pwa_check:     | /' "$ctl/plain.out" >&2
  could_not_vouch "positive control: the plain loop exited $prc on an UNEDITED copy, so every sabotage below would score caught for a reason that has nothing to do with it"
fi
echo "pwa_check: positive control PASSED all ${#checks[@]} check(s)"

# 🔴 THE (c) SABOTAGES RUN THE *PLAIN LOOP* (`plain_run`), NOT ONLY `verdict`. A verdict function that
# names the right check is not a script that EXITS right: the walk prints its three `… PASSED` lines only
# when EVERY refusal passes, so one failing (c) check leaves its siblings with no PASS line — and a loop
# that read that as "no verdict" exited 2 ("could not vouch") on every real (c) failure. Each (c)
# sabotage must make the plain loop exit 1 with its own check printed FAIL.
# `PWA_CHECK_SABOTAGES` (a space-separated subset) is a debugging aid: any subset reports fewer than
# six and so exits 1.
selected=(${PWA_CHECK_SABOTAGES:-${checks[*]}})
sabotaged=0; caught=0; plain_arms=0; plain_ok=0
for s in "${selected[@]}"; do
  d="$work/self-test/$s"
  rm -rf "$d"; mkdir -p "$d/tree"
  make_copy "$d/tree" "$s" || could_not_vouch "sabotage $s could not be applied"
  sabotaged=$((sabotaged + 1))
  case "$s" in
    a_*|b_*)
      run_ab "$d/tree" "$d"
      [ -f "$d/harness" ] && could_not_vouch "sabotage $s: $(cat "$d/harness")"
      log="$d/ab.log" ;;
    c_*)
      plain_run "$d/tree" "$d" > "$d/plain.out" 2>&1
      prc=$?
      log="$d/c.log"
      plain_arms=$((plain_arms + 1))
      if [ "$prc" = 1 ] && grep -qE "^pwa_check: $s +FAIL\$" "$d/plain.out"; then
        plain_ok=$((plain_ok + 1))
        printf 'pwa_check: plain loop on %-18s exit 1, names %s FAIL\n' "$s" "$s"
      else
        printf 'pwa_check: plain loop on %-18s WRONG: exit %s; it printed:\n' "$s" "$prc"
        sed 's/^/pwa_check:     | /' "$d/plain.out"
      fi ;;
  esac
  v=$(verdict "$d" "$s")
  also=()
  for c in "${checks[@]}"; do
    [ "$c" = "$s" ] && continue
    [ "$(verdict "$d" "$c")" = FAIL ] && also+=("$c")
  done
  if [ "$v" = FAIL ] && grep -qF -- "${own_message[$s]}" "$log"; then
    caught=$((caught + 1))
    printf 'pwa_check: sabotage %-18s CAUGHT by its own check%s\n' "$s" \
      "$([ ${#also[@]} -gt 0 ] && echo " (also red: ${also[*]})")"
  else
    printf 'pwa_check: sabotage %-18s NOT CAUGHT (own verdict %s; red elsewhere: %s) — see %s\n' "$s" "$v" \
      "${also[*]:-none}" "$d"
  fi
done
echo "sabotaged=$sabotaged caught=$caught plain-loop=$plain_ok/$plain_arms"
[ "$sabotaged" = "${#checks[@]}" ] && [ "$caught" = "$sabotaged" ] && [ "$plain_arms" = 3 ] \
  && [ "$plain_ok" = "$plain_arms" ] && exit 0
exit 1
