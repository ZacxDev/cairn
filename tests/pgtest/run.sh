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

# 🔴 `CDPATH= cd --`, WHICH IS WHAT THREE OTHER SCRIPTS IN THIS REPO ALREADY DO
# (`server/build-push.sh`, `server/verify-byte-identity.sh`,
# `tests/conformance/run_go.sh`) AND WHICH THIS ONE DID NOT. A plain `cd <relative>`
# PRINTS the resolved directory whenever it reached it through `CDPATH`, so with
# `CDPATH` exported — `.` as its first entry is a common spelling and it survives into
# `nix develop` — this command substitution captured the path TWICE and the script died
# on `cd: <path>\n<path>: No such file or directory` before running anything.
#
# ⚠ MEASURED HERE, NOT INHERITED FROM THE OTHER SCRIPTS' COMMENTS: this runner failed
# exactly that way on a host with `CDPATH=.:…` set, while CI — which has none — has
# been green throughout. That is the shape the repository's own rule names: a green
# covers the ENVIRONMENT it ran in, and the one dimension this script's CI environment
# pins is the one it was blind to. `run_go.sh`'s comment records being "the last of the
# three to be hardened"; it was the last of FOUR, and `uiaudit/run.sh` is the fifth and
# is still unhardened — filed rather than fixed here, because it is a different script
# with its own tests.
here="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo="$(CDPATH= cd -- "$here/../.." && pwd)"
# Every `go test ./internal/...` below is a package path relative to the module root, so
# the script runs from there rather than from wherever it was invoked. `server/seed.sh`'s
# missing `cd` is a defect this repository already tracks.
cd "$repo"

# ── the tier's PACKAGES, spelled ONCE ────────────────────────────────────────────
# 🔴 A LIST RATHER THAN THE ONE PATH THIS SCRIPT USED TO CARRY, BECAUSE THE TIER STOPPED
# BEING ONE PACKAGE. `internal/pgstore` is the SQL; `cmd/cairn-ui` is the program that
# points itself at a database, and everything its DSN branch does — the schema applied,
# the session table moved off disk, an `Inviting` that is not nil — is invisible without
# a server. A tagged file in a package this list does not name is a file `go test`
# never compiles, which is the tier's own "a skip nobody counts is a pass" one level up:
# the ledger in `tests/test_pgtest_tier_is_declared.py` would still see the FILE.
#
# ⚠ ADDING A PACKAGE HERE IS HALF THE MOVE. The other half is a row in that ledger, and
# the two are checked against each other there.
PGTEST_PKGS=(./internal/pgstore/ ./cmd/cairn-ui/)

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
#
# 🔴 ONE CONTROL PER PACKAGE, BECAUSE THE REFUSAL IS PER PACKAGE. `cmd/cairn-ui` cannot
# import `internal/pgstore`'s `_test` helpers, so it restates the refusal — a SECOND
# spelling, and an unwatched second spelling is exactly the thing this control exists
# against. A control that only ever ran the first package would vouch for a second one
# that skips, or that never compiled its tagged file at all.
echo "-- negative control: each package of the tier, with no DSN, must REFUSE --"
control_i=0
for control in \
	"./internal/pgstore/:TestTheSQLRedemptionGuardAgreesWithStateAt" \
	"./cmd/cairn-ui/:TestWithADatabaseTheSurfaceMovesItsStateThereAndHoldsInvitations"; do
	# ⚠ BRACED AND SPLIT ON THE LAST COLON-FREE FIELD RATHER THAN BY WORD-SPLITTING: this
	# script runs under `bash`, but the repository's shell rules record that an unbraced
	# `$var:` followed by certain letters is eaten as a history modifier in zsh, and a
	# value that expands to a well-formed WRONG string is the failure mode with no error.
	control_pkg=${control%%:*}
	control_test=${control##*:}
	control_i=$((control_i + 1))
	control_out="$workdir/control-$control_i.txt"
	if env -u CAIRN_PGTEST_DSN go test -tags pgtest -count=1 -run "$control_test" \
		"$control_pkg" >"$control_out" 2>&1; then
		sed -n '1,30p' "$control_out" >&2 || true
		refuse "the tier PASSED in $control_pkg with no database configured. It must refuse
instead — a tier that is green without a server has measured nothing, and every green
below would be a fact about the harness."
	fi
	if ! grep -q 'REFUSING TO VOUCH' "$control_out"; then
		sed -n '1,30p' "$control_out" >&2 || true
		refuse "the tier failed in $control_pkg with no database, but not with its own refusal
message. That is a different failure from the one the control is testing for, so the
control did not measure what it claims."
	fi
	# 🔴 AND THE NAMED TEST MUST HAVE RUN. `go test -run` matching NOTHING reports `ok`,
	# which this script already refuses on for the main run — the same hole is open here,
	# where a RENAMED test would make the control exit 0 and be read as "no refusal
	# needed". `go test` prints the refusal through the test's own FAIL line, so the
	# package path in a failing run is what proves the selection hit something.
	if ! grep -q -- "--- FAIL: $control_test" "$control_out"; then
		sed -n '1,30p' "$control_out" >&2 || true
		refuse "the control for $control_pkg did not report '--- FAIL: $control_test'. The
-run filter selected no such test, so what was measured is the filter and not the tier."
	fi
	echo "   control OK: $control_pkg refused, naming its own refusal"
done
echo ""

# ── the run under test ───────────────────────────────────────────────────────────
echo "-- the tier --"
out="$workdir/test.txt"
set +e
go test -tags pgtest -count=1 -v "${PGTEST_PKGS[@]}" 2>&1 | tee "$out"
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
