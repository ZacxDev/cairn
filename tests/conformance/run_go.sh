#!/usr/bin/env bash
# Replay the conformance corpus against the GO server.
#
# 🔴 THE SERVER UNDER TEST MUST BE CONFIGURED THE WAY THE GOLDENS WERE RECORDED, and
# that configuration is `oracle.ORACLE_ENV` — not a guess made here. This script asks
# `suite.py build-store` for the store, the token file AND the env, and exports what it
# is told rather than restating it, because a second copy of that table is a second
# thing that can drift from the goldens.
#
# 🔴 `MAX_FAILURES` IS THE ONE THAT WOULD SILENTLY DESTROY A RUN. The lockout answers
# the SAME uniform 401 a bad token does — that is the design — so once it trips a
# correctly authorized request also answers 401 and matches no golden except by
# accident. The corpus issues fifteen deliberate refusals from one client address, six
# of which the limiter counts, over a production default of five per minute.
#
#   usage: tests/conformance/run_go.sh [extra suite.py run arguments]
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
work="$(mktemp -d "${TMPDIR:-/tmp}/cairn-go-conformance-XXXXXX")"
trap 'rm -rf "$work"' EXIT

binary="$work/cairn-server"
go build -C "$repo" -o "$binary" ./cmd/cairn-server

# `build-store` prints `store=`, `token-file=` and `env:` — the same three facts the
# generator uses. Parsed rather than reinvented.
declaration="$(python3 "$repo/tests/conformance/suite.py" build-store "$work/world")"
store="$(sed -n 's/^store=//p' <<<"$declaration")"
tokens="$(sed -n 's/^token-file=//p' <<<"$declaration")"
env_line="$(sed -n 's/^env: //p' <<<"$declaration")"

port="$(python3 -c 'import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()')"

# shellcheck disable=SC2086  # the env line is a deliberate word-split of KEY=VALUE pairs
env $env_line "$binary" \
  --store "$store" --host 127.0.0.1 --port "$port" --token-file "$tokens" \
  >"$work/server.log" 2>&1 &
server=$!
trap 'kill "$server" 2>/dev/null || true; rm -rf "$work"' EXIT

for _ in $(seq 1 200); do
  if curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then break; fi
  if ! kill -0 "$server" 2>/dev/null; then
    echo "the go server exited before answering /healthz:" >&2
    cat "$work/server.log" >&2
    exit 1
  fi
  sleep 0.05
done

set +e
python3 "$repo/tests/conformance/suite.py" run \
  --base-url "http://127.0.0.1:$port" --token-file "$tokens" "$@"
rc=$?
set -e

echo "--- go server log ---"
cat "$work/server.log"
exit "$rc"
