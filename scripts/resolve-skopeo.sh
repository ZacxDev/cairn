#!/usr/bin/env bash
# Select the one store path that actually carries a runnable `skopeo`.
#
# 🔴 WHY THIS IS A SCRIPT AND NOT THREE LINES IN THE WORKFLOW. `nixpkgs#skopeo`
# is a MULTI-OUTPUT derivation — `outputs = ["out", "man"]` — so
# `nix build … --print-out-paths` prints TWO store paths, and the `-man` one is
# printed FIRST. A step that captured that into one variable and appended
# `/bin/skopeo` therefore built a TWO-LINE command: the shell reported exit 127
# on a path that does not exist, and the same multi-line value written to
# `$GITHUB_OUTPUT` was rejected with `Invalid format`. That failure was not a
# corner case — it is what every run of the publish workflow did, at this step,
# from the moment the workflow landed.
#
# 🔴 AND THE FIX IS A SELECTION, NOT A SMARTER PARSE. Teaching a parser one more
# spelling — "take the last line", "drop anything ending in `-man`" — is how the
# next spelling arrives: a derivation that gains a `dev` output, an output order
# that changes, a `debug` output on some platform. This asks the only question
# that is actually about the thing wanted — which candidate HAS an executable
# `bin/skopeo` — and refuses rather than guess when the answer is not exactly
# one.
#
# 🔴 STDIN, SO THE SELECTION IS TESTABLE WITHOUT NIX. The candidates arrive one
# per line on stdin, which means `tests/test_publish_workflow.py` drives this
# exact file over a real temp-directory layout with no nix, no network and no
# registry. A resolver that could only be exercised inside a publish run would be
# verifiable only by publishing.
#
# Usage:
#   nix build --inputs-from . nixpkgs#skopeo --no-link --print-out-paths \
#     | scripts/resolve-skopeo.sh
#
# Prints ONE absolute path to the binary on stdout, exit 0. Otherwise prints a
# NAMED refusal on stderr and exits non-zero, with NOTHING on stdout — the caller
# captures stdout, so a refusal that also printed a path would hand it one to run.
set -euo pipefail

me="resolve-skopeo"

# Two distinct codes, because the two refusals are different facts about the
# world and an operator reading a log should not have to guess which happened.
readonly RC_NONE=3
readonly RC_AMBIGUOUS=4

candidates=()
qualified=()

while IFS= read -r line; do
  # A blank or whitespace-only line is not a candidate. `nix` does not emit one,
  # but a pipeline that produced NOTHING at all must reach the zero-candidate
  # refusal below rather than be reported as an empty-string candidate.
  [ -n "${line//[[:space:]]/}" ] || continue
  candidates+=("$line")
  bin="$line/bin/skopeo"
  # 🔴 `-f` AND `-x`, BOTH. `-x` alone is true for a searchable DIRECTORY, so a
  # candidate holding `bin/skopeo/` would qualify; `-f` alone is true for a file
  # that cannot be executed, which is the shape a wrong output or a partially
  # realised path has. Both follow symlinks, which is right: every path in a nix
  # store bin directory is one.
  if [ -f "$bin" ] && [ -x "$bin" ]; then
    qualified+=("$bin")
  fi
done

if [ "${#qualified[@]}" -eq 0 ]; then
  {
    printf '%s: NO CANDIDATE CARRIES AN EXECUTABLE bin/skopeo.\n' "$me"
    printf '  %d candidate path(s) were read on stdin:\n' "${#candidates[@]}"
    for c in ${candidates[@]+"${candidates[@]}"}; do
      printf '    %s\n' "$c"
    done
    printf '  Nothing is printed on stdout, so the caller cannot proceed on an\n'
    printf '  empty string. Check that the build actually produced an output; an\n'
    printf '  empty list here means the pipeline upstream produced no path at all.\n'
  } >&2
  exit "$RC_NONE"
fi

if [ "${#qualified[@]}" -gt 1 ]; then
  {
    printf '%s: AMBIGUOUS — %d candidates carry an executable bin/skopeo.\n' \
      "$me" "${#qualified[@]}"
    for q in "${qualified[@]}"; do
      printf '    %s\n' "$q"
    done
    printf '  Refusing rather than picking one. An ambiguous answer silently\n'
    printf '  chosen is the bug class this script exists to close: it keeps\n'
    printf '  working, on whichever path happened to come first, until the day\n'
    printf '  the two differ — and then it has no way to say that it guessed.\n'
  } >&2
  exit "$RC_AMBIGUOUS"
fi

printf '%s\n' "${qualified[0]}"
