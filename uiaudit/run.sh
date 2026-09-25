#!/usr/bin/env bash
# The CI entrypoint, and the way to run the walk by hand.
#
# 🔴 IT BUILDS `cairn-ui` FROM THE ROOT MODULE AND `uiaudit` FROM THE NESTED ONE, SEPARATELY,
# BECAUSE THAT SEPARATION IS THE WHOLE POINT OF THE LAYOUT. `go build` at the repository root
# does not descend into `uiaudit/`, so chromedp never enters the root module's graph and
# `internal/depspolicy`'s allowlist and import ban stay untouched. Building both here in one
# script is what makes the two-module arrangement operable rather than merely correct.
#
# It exits 0 when the walk ran, including when the push was skipped for want of credentials
# (a fork PR gets none, and a job that failed there would train everyone to ignore it). It
# exits non-zero only when the HARNESS failed: the pod would not boot, chromium would not
# start, sign-in did not take, a capture errored, or the audit hub is half-configured.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/.." && pwd)"
work="${UIAUDIT_WORK:-$(mktemp -d -t uiaudit-XXXXXX)}"
port="${UIAUDIT_PORT:-18771}"
label="${UIAUDIT_LABEL:-cairn-ui}"

mkdir -p "$work/bin"
echo "uiaudit: building cairn-ui from the ROOT module"
( cd "$root" && go build -o "$work/bin/cairn-ui" ./cmd/cairn-ui ) || exit 1
echo "uiaudit: building uiaudit from the NESTED module"
( cd "$here" && go build -o "$work/bin/uiaudit" . ) || exit 1

"$work/bin/uiaudit" \
  -repo-root "$root" \
  -cairn-ui "$work/bin/cairn-ui" \
  -work "$work" \
  -port "$port" \
  -label "$label"
rc=$?

# Exit 3 is "no audit-hub credentials": the capture ran and the push was skipped. The
# workflow maps it to success HERE rather than having the program lie about having pushed.
if [ "$rc" = 3 ]; then
  echo "uiaudit: walk complete, push skipped (no credentials)"
  rc=0
fi
echo "uiaudit: work dir $work"
exit "$rc"
