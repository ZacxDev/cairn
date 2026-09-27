#!/usr/bin/env bash
# The POSTGRES TIER's runner: start a throwaway server, run the tagged tests against it,
# tear it down.
#
# 🔴 WHY A RUNNER AT ALL, AND WHY IT CANNOT BE A NIX CHECK (YET). The tier's tests are
# behind `//go:build pgtest`, so `go test ./...` — which every nix derivation runs in
# `doCheck` — cannot see them. That keeps the sandbox honest (it has no database and
# must not pretend to) and it moves the hazard: a tier nobody RUNS is as green as a tier
# that skips. This script is the thing that runs it, and `.github/workflows/ci.yml`
# invokes it. A nix check COULD own this — a unix-socket Postgres needs no network and
# would work in the sandbox — and that is a deliberate follow-up rather than an
# oversight: it makes the tier a build failure, which is strictly stronger, and it is a
# separate change from introducing the tier.
#
# 🔴 IT REFUSES RATHER THAN SKIPPING, AT EVERY STEP. No `initdb` on PATH, a server that
# will not start, or a run in which ZERO tests executed all exit non-zero with a
# "REFUSING TO VOUCH" line. This repository has measured the alternative: a wrapper that
# reports "nothing to do" instead of erroring is how a green gets believed, and a
# battery that exited 1 at its own baseline read as merely red for weeks.
#
# 🔴 IT READS THE OUTPUT, NOT THE EXIT CODE. `go test`'s status is necessary and not
# sufficient — a `-run` filter matching nothing reports `ok`, measured in this very
# repository. So the run counts the runner's own `--- PASS` / `--- FAIL` lines and
# refuses on a zero, and the counts are what it prints.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
# Every `go test ./internal/...` below is a package path relative to the module root, so
# the script runs from there rather than from wherever it was invoked. `server/seed.sh`'s
# missing `cd` is a defect this repository already tracks.
cd "$repo"

refuse() {
	echo "" >&2
	echo "REFUSING TO VOUCH: $1" >&2
	echo "" >&2
	echo "This is not a test failure. The tier could not be measured, which is a" >&2
	echo "different outcome from passing and from failing, and reporting it as either" >&2
	echo "would be wrong." >&2
	exit 2
}

# ── where the server comes from is a PARAMETER; how the tier is measured is not ──
# 🔴 TWO WAYS IN, ONE MEASUREMENT. A `CAIRN_PGTEST_DSN` already in the environment is
# used as-is (CI pins a service container that way, to the same major the deployment
# runs); otherwise this script starts a throwaway server itself, which is what a
# developer gets. The controls, the counting and the refusals below are identical in
# both cases — the alternative was a second CI-only invocation, and a measurement
# spelled twice is the shape this repository consolidates on sight.
external_dsn=${CAIRN_PGTEST_DSN:-}

command -v go >/dev/null 2>&1 || refuse "\`go\` is not on PATH. Run this inside the dev shell:
    nix develop $repo -c $0"
go_version=$(go version 2>/dev/null | tr -d '\n')

echo "== POSTGRES TIER =="
echo "go     : ${go_version:-UNKNOWN}"
echo "repo   : $repo"

if [ -n "$external_dsn" ]; then
	# 🔴 THE SERVER'S OWN VERSION IS READ OUT OF THE SERVER, NOT ASSUMED FROM THE IMAGE
	# TAG. A tag is a claim about the manifest; `SELECT version()` is the artefact
	# answering for itself — the same distinction this repository makes between a
	# deployed tag and a running image. `psql` may not be present, so the read goes
	# through the tier itself rather than through a client this script would then have
	# to require.
	echo "server : supplied externally via CAIRN_PGTEST_DSN (version read below)"
else
	# 🔴 THE PIN IS REPORTED RATHER THAN ASSUMED. The devShell carries `postgresql_18`;
	# a bare shell on this host may carry a different major or none at all, and a tier
	# measured against an unknown server is a claim about an unknown server.
	for bin in initdb pg_ctl createdb postgres; do
		command -v "$bin" >/dev/null 2>&1 || refuse \
			"\`$bin\` is not on PATH, and no CAIRN_PGTEST_DSN was supplied. Either run this
inside the dev shell, which pins the server:

    nix develop $repo -c $0

or set CAIRN_PGTEST_DSN to a throwaway database."
	done
	echo "server : $(postgres --version 2>/dev/null | tr -d '\n')"
fi

# ── an ephemeral server ──────────────────────────────────────────────────────────
# 🔴 A UNIX SOCKET AND NO TCP LISTENER, WHICH IS A SAFETY PROPERTY RATHER THAN A STYLE
# CHOICE. `listen_addresses=''` means this server cannot be reached from off the box at
# all, so a throwaway database holding test fixtures is not briefly a network service —
# and it also means two concurrent runs on one host cannot collide on a port, which the
# TCP version would have needed `ss -ltn` to avoid.
workdir=$(mktemp -d "${TMPDIR:-/tmp}/cairn-pgtest.XXXXXXXX")
datadir="$workdir/data"
sockdir="$workdir/sock"
mkdir -p "$sockdir"

started=0

cleanup() {
	local rc=$?
	if [ "$started" = 1 ]; then
		# 🔴 STOPPED BY DATA DIRECTORY, NEVER BY A PROCESS-NAME PATTERN. `pg_ctl` reads
		# the PID file in `$datadir`, so it can only ever stop the server THIS script
		# started. A `pkill -f postgres` would reach a sibling agent's server, or the
		# operator's own, and the damage would read exactly like a defect in the code
		# under test.
		pg_ctl -D "$datadir" -m immediate stop >/dev/null 2>&1 || true
	fi
	rm -rf "$workdir"
	exit $rc
}
trap cleanup EXIT INT TERM

if [ -n "$external_dsn" ]; then
	echo "dsn    : from CAIRN_PGTEST_DSN (not echoed — a DSN can carry a password)"
else
	echo "datadir: $datadir (removed on exit)"
	if ! initdb -D "$datadir" -A trust -U cairn_pgtest --no-sync >"$workdir/initdb.log" 2>&1; then
		sed -n '1,40p' "$workdir/initdb.log" >&2 || true
		refuse "\`initdb\` failed — see the log above."
	fi

	# `--no-sync` and `fsync=off` are safe here and only here: the entire cluster is
	# deleted when this script exits, so durability buys nothing and costs the whole
	# runtime. They are NOT set on an externally supplied server, which is not ours.
	if ! pg_ctl -D "$datadir" -o "-k '$sockdir' -c listen_addresses='' -c fsync=off -c full_page_writes=off" \
		-l "$workdir/server.log" -w start >"$workdir/pgctl.log" 2>&1; then
		sed -n '1,40p' "$workdir/server.log" >&2 || true
		refuse "the server did not start — see the log above."
	fi
	started=1

	if ! createdb -h "$sockdir" -U cairn_pgtest cairn_pgtest >>"$workdir/server.log" 2>&1; then
		sed -n '1,40p' "$workdir/server.log" >&2 || true
		refuse "could not create the test database — see the log above."
	fi

	# A keyword/value DSN, which is the spelling `withSearchPath` appends to with a space.
	export CAIRN_PGTEST_DSN="host=$sockdir user=cairn_pgtest dbname=cairn_pgtest sslmode=disable"
	echo "dsn    : host=<socket in the temp dir> user=cairn_pgtest dbname=cairn_pgtest"
fi
echo ""

# ── the negative control, BEFORE the run under test ──────────────────────────────
# 🔴 PROVE THE TIER CAN REFUSE BEFORE BELIEVING THAT IT PASSED. With the variable unset
# the tier must FAIL, loudly, naming the refusal — that is what makes a pass below a
# statement about the SQL rather than about a harness wired to nothing. A tier whose
# tests quietly did nothing without a database would pass this script's happy path and
# every reader would take the green at face value.
echo "-- negative control: the tier with no DSN must REFUSE --"
control_out="$workdir/control.txt"
if env -u CAIRN_PGTEST_DSN go test -tags pgtest -count=1 -run TestTheSQLRedemptionGuardAgreesWithStateAt \
	./internal/pgstore/ >"$control_out" 2>&1; then
	sed -n '1,30p' "$control_out" >&2 || true
	refuse "the tier PASSED with no database configured. It must refuse instead — a tier
that is green without a server has measured nothing, and every green below would be
a fact about the harness."
fi
if ! grep -q 'REFUSING TO VOUCH' "$control_out"; then
	sed -n '1,30p' "$control_out" >&2 || true
	refuse "the tier failed with no database, but not with its own refusal message. That
is a different failure from the one the control is testing for, so the control did not
measure what it claims."
fi
echo "   control OK: refused, naming its own refusal"
echo ""

# ── the run under test ───────────────────────────────────────────────────────────
echo "-- the tier --"
out="$workdir/test.txt"
set +e
go test -tags pgtest -count=1 -v ./internal/pgstore/ 2>&1 | tee "$out"
# 🔴 THE STATUS OF THE PIPELINE'S FIRST STAGE, NOT `tee`'s. A pipe eats the exit status,
# and this repository has already filed a false defect against a correct gate by reading
# the wrong one.
test_rc=${PIPESTATUS[0]}
set -e
echo ""

# ── read the CONTENT ────────────────────────────────────────────────────────────
ran=$(grep -c '^=== RUN' "$out" || true)
passed=$(grep -c '^ *--- PASS' "$out" || true)
failed=$(grep -c '^ *--- FAIL' "$out" || true)
skipped=$(grep -c '^ *--- SKIP' "$out" || true)

echo "== RESULT =="
echo "  === RUN lines : $ran"
echo "  --- PASS      : $passed"
echo "  --- FAIL      : $failed"
echo "  --- SKIP      : $skipped"
echo "  go test rc    : $test_rc"

# 🔴 A ZERO IS THE HARNESS-BROKE SIGNAL, NOT A PASS. `go test` answers `ok` for a `-run`
# that matches nothing — measured in this repository — so a run that executed no tests
# is indistinguishable from a clean one by status alone.
if [ "$ran" -eq 0 ]; then
	refuse "ZERO tests ran. The build tag, the package path or a filter selected nothing,
so \`go test\`'s status is a fact about the selection and not about the SQL."
fi

# 🔴 A SKIP IS A FAILURE OF THE TIER. The tier's whole design is that it refuses instead
# of skipping; a `--- SKIP` means somebody added one, and a skip nobody counts is a pass.
if [ "$skipped" -gt 0 ]; then
	grep -n '^ *--- SKIP' "$out" >&2 || true
	refuse "$skipped test(s) SKIPPED. This tier must refuse rather than skip — see the
header of internal/pgstore/harness_pgtest_test.go."
fi

if [ "$failed" -gt 0 ] || [ "$test_rc" -ne 0 ]; then
	echo ""
	echo "POSTGRES TIER FAILED: $failed failing test(s)." >&2
	exit 1
fi

echo ""
# 🔴 THE SUMMARY NAMES THE SERVER FROM THE RUN'S OWN OUTPUT, NOT FROM A SHELL VARIABLE
# THIS SCRIPT GUESSED. An earlier version printed "against an unknown server" on the
# external-DSN path, because the variable it read is only set when this script starts the
# server itself — while the tier had just logged `server_version=18.6`, read out of the
# server by `SHOW server_version`. A summary line that says "unknown" about something a
# test measured one screen earlier is a false claim in the most-read line of the output.
reported=$(grep -oE 'server_version=[0-9][0-9.A-Za-z]*' "$out" | head -1 | cut -d= -f2)
echo "POSTGRES TIER PASSED: $passed test(s) against PostgreSQL ${reported:-<version not reported by the run>},"
echo "with the no-database control watched refusing first."
