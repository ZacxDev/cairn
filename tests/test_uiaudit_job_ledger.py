"""The `uiaudit` job's step ledger: every step, in order, with the leg it runs in.

`uiaudit` runs as a three-leg matrix (`walk`, `pwa`, `memos`) so its serial ~9.5 minutes became two
parallel legs, and the memos tab's browser check — the only leg with a database — a third beside them. Splitting a job is how a step gets DROPPED without anything going red — a step whose `if:`
names a leg the matrix does not have never runs and never fails — and how an ORDERING breaks
silently: the walk must stay behind the guards (they hold the positive control) and `verify-push`
must stay behind the walk, in the same leg, because steps in different legs share no state.

So the ledger below is the whole job, pinned as WHOLE `if:` strings in order: a step added,
removed, renamed, reordered or moved to another leg fails here, and so does a leg the matrix no
longer declares. The step set is the one the job had as a single job (PR "E" of the CI speed-up
carries the before/after list); only the `if:` column is new.

⚠ WHAT THIS FILE CANNOT DO: it reads `ci.yml` as text. That GitHub runs each step in the leg
pinned here is measured only by a live run — the PR that introduced this file carries one.
"""
from __future__ import annotations

import re

from test_ci_paths import JOBS

WALK = "matrix.part == 'walk'"
PWA = "matrix.part == 'pwa'"
MEMOS = "matrix.part == 'memos'"

#: (step label, its `if:` exactly, or None for a step that runs in EVERY leg).
#: The label is the step's `name:`, or its `uses:` when it has no name.
LEDGER = [
    ("actions/checkout@v4", None),
    ("actions/setup-go@v5", None),
    ("the toolchain that will actually run, asserted rather than assumed", None),
    ("actions/setup-python@v5", None),
    ("cachix/install-nix-action@v31", None),
    ("chromium from the flake's pinned nixpkgs, its version asserted", None),
    ("the pinned chromium is the one on PATH", None),
    ("the harness's own guards, including the positive control", WALK),
    ("the walk — boot cairn-ui, drive chromium, push, read the diff back", WALK),
    ("pwa_check.sh — the mobile plan's closing-condition clauses (a)–(e)", PWA),
    ("postgres from the flake's pinned nixpkgs, its major asserted", MEMOS),
    ("memos_check.py — the populated memos tab in a real browser, after proving each clause can go RED", MEMOS),
    ("verify-push — the push actually landed", f"{WALK} && steps.walk.outputs.credentialed == 'yes'"),
    ("the deterministic diff, in the log and in the step summary", f"always() && {WALK}"),
    ("upload the captures", f"always() && {WALK}"),
]


def _steps(job: str) -> list[str]:
    return re.split(r"^      - ", job, flags=re.M)[1:]


def _label(step: str) -> str:
    m = re.match(r"(?:name|uses): (.+)", step)
    assert m, step[:80]
    return m.group(1)


def _if(step: str) -> str | None:
    m = re.search(r"^        if: (.+)$", step, re.M)
    return m.group(1) if m else None


def test_every_step_is_in_the_ledger_in_order_with_its_leg_as_a_WHOLE_string():
    got = [(_label(s), _if(s)) for s in _steps(JOBS["uiaudit"])]
    assert got == LEDGER


def test_the_matrix_declares_exactly_the_three_legs_and_never_fails_fast():
    """With the ledger's `if:` strings pinned whole, this is what makes every leg a step names exist."""
    assert re.search(r"^    strategy:\n      fail-fast: false\n      matrix:\n        part: \[walk, pwa, memos\]$",
                     JOBS["uiaudit"], re.M)


#: Every function `go test` would run from a file: a top-level `Test`/`Example`/`Fuzz`/`Benchmark`.
GO_TEST_FUNC = re.compile(r"^func ((?:Test|Example|Fuzz|Benchmark)\w*)\(", re.M)


def _go_test_funcs(src: str) -> list[str]:
    return GO_TEST_FUNC.findall(src)


def test_the_go_test_func_finder_sees_each_kind_and_skips_methods_and_helpers():
    """The instrument's controls: it must find a second test of each kind (positive) and must not
    count a helper or a method whose name merely starts with `Test` (negative)."""
    src = ("func TestA(t *testing.T) {\n}\nfunc ExampleB() {\n}\nfunc FuzzC(f *testing.F) {\n}\n"
           "func BenchmarkD(b *testing.B) {\n}\nfunc helperTest() {\n}\nfunc (w world) TestE() {\n}\n")
    assert _go_test_funcs(src) == ["TestA", "ExampleB", "FuzzC", "BenchmarkD"]


def test_the_memos_leg_runs_the_tagged_browser_test_through_its_one_runner():
    """`TestTheMemosTabInARealBrowser` is behind `//go:build memobrowser`, so the module's own
    `go test ./...` (the walk leg) cannot see it, and the walk leg's declared-test count excludes
    its file by name. Both are only safe while SOMETHING runs it: this pins the three places that
    must agree — the tag on the file, the runner naming that tag and that test, and the leg
    invoking the runner (self-test first) and counting the five clause lines.

    ⚠ TEXT ONLY, like the rest of this file: that the leg passes is a live run's claim.
    """
    from pathlib import Path
    repo = Path(__file__).resolve().parent.parent
    test = (repo / "uiaudit/memos_browser_test.go").read_text(encoding="utf-8")
    assert test.startswith("//go:build memobrowser\n")
    assert "\nfunc TestTheMemosTabInARealBrowser(t *testing.T) {\n" in test
    # 🔴 AND IT IS THE ONLY ONE. The walk leg excludes this FILE by name and the runner names this
    # one TEST, so a second test function here would run in neither leg — and nothing would fail.
    assert _go_test_funcs(test) == ["TestTheMemosTabInARealBrowser"], _go_test_funcs(test)
    assert "t.Skip" not in test, "the browser test must refuse, never skip"
    clauses = re.findall(r'^\tt\.Run\("([a-e]_[a-z]+)", ', test, re.M)
    assert clauses == ["a_seeded", "b_send", "c_draft", "d_retract", "e_reader"]

    runner = (repo / "uiaudit/memos_check.py").read_text(encoding="utf-8")
    assert 'TEST = "TestTheMemosTabInARealBrowser"' in runner
    assert 'TAG = "memobrowser"' in runner
    assert re.findall(r'^    "([a-e]_[a-z]+)": "memos clause \(', runner, re.M) == clauses
    sabotages = re.findall(r'^    \("[a-z-]+", "([a-e]_[a-z]+)", ', runner, re.M)
    assert len(sabotages) == 7 and set(sabotages) == set(clauses), sabotages

    job = JOBS["uiaudit"]
    (step,) = [s for s in _steps(job) if _label(s).startswith("memos_check.py")]
    assert "./uiaudit/memos_check.py --self-test" in step
    assert "'SUMMARY memos-check-self-test: sabotaged=7 caught=7'" in step
    assert '[ "$passes" != "5" ]' in step
    assert "':(exclude)memos_browser_test.go'" in job, "the walk leg's static count must exclude the tagged file by name"


HUB_SECRETS = ["CAIRN_AUDIT_PUSH_URL", "CAIRN_AUDIT_PUSH_TOKEN", "CAIRN_AUDIT_API_URL", "CAIRN_AUDIT_API_TOKEN"]


def test_the_hub_secrets_reach_ONE_step_in_ONE_leg():
    """One leg pushes, so one run pushes at most once — the property the split must not break.

    🔴 COUNTED OVER THE WHOLE JOB TEXT, NOT OVER `_steps()`: the step split drops the job header,
    so a secret in the JOB-level `env:` (beside `GOTOOLCHAIN`) — which reaches BOTH legs — was
    invisible to a steps-only scan and left this test green.
    """
    job = JOBS["uiaudit"]
    (walk,) = [s for s in _steps(job)
               if _label(s) == "the walk — boot cairn-ui, drive chromium, push, read the diff back"]
    assert _if(walk) == WALK
    in_job = re.findall(r"secrets\.([A-Z_]+)", job)
    in_walk = re.findall(r"secrets\.([A-Z_]+)", walk)
    assert in_walk == HUB_SECRETS
    assert in_job == in_walk, f"a secret is referenced outside the walk step: {in_job}"


# --- `uiaudit-touch`: the BLOCKING job (S6b of the mobile plan) ------------------------------------

#: The steps `uiaudit-touch` SHARES with `uiaudit`, by label. They must be byte-identical in both jobs:
#: the browser that blocks a merge is S6a's pinned chromium only while its install is the SAME text.
SHARED_WITH_UIAUDIT = [label for label, leg in LEDGER[:7]]
GUARDS = "the touch gate's own guards and browser controls, under the pinned chromium"
GATE = "the walk's touch refusals — BLOCKING"
TOUCH_LEDGER = SHARED_WITH_UIAUDIT + [GUARDS, GATE]


def _step(job: str, label: str) -> str:
    """One step's text WITHOUT the comment block that follows it: `_steps` splits on the dash, so a
    step's tail is the NEXT step's leading comment, which legitimately differs between the jobs."""
    (s,) = [s for s in _steps(job) if _label(s) == label]
    lines = s.splitlines()
    while lines and (not lines[-1].strip() or lines[-1].lstrip().startswith("#")):
        lines.pop()
    return "\n".join(lines)


def test_the_touch_job_is_exactly_these_steps_and_shares_the_pinned_install_BYTE_FOR_BYTE():
    touch = JOBS["uiaudit-touch"]
    assert [_label(s) for s in _steps(touch)] == TOUCH_LEDGER
    assert SHARED_WITH_UIAUDIT[-2:] == ["chromium from the flake's pinned nixpkgs, its version asserted",
                                        "the pinned chromium is the one on PATH"]
    for label in SHARED_WITH_UIAUDIT:
        assert _step(touch, label) == _step(JOBS["uiaudit"], label), f"{label!r} drifted from the advisory job's"
    # And the version is ASSERTED, not only printed (the plan's closing condition reads it off the log).
    pin = _step(touch, "chromium from the flake's pinned nixpkgs, its version asserted")
    assert 'echo "the binary says: $got"' in pin and "::error::the pinned chromium reports" in pin


def test_the_touch_job_BLOCKS_holds_no_secret_and_runs_the_walk_under_the_touch_gate():
    """The plan's closing condition, part 3, read off the workflow file: NOT `continue-on-error`."""
    touch = JOBS["uiaudit-touch"]
    # As a KEY, at the job or at any step — the job's comments may name the word.
    assert not re.search(r"^\s+continue-on-error:", touch, re.M)
    assert "secrets." not in touch
    # The advisory job is unchanged in this respect: it stays non-blocking.
    assert re.search(r"^    continue-on-error: true$", JOBS["uiaudit"], re.M)
    assert re.search(r"^    timeout-minutes: 30$", touch, re.M), "a hung required check holds every merge"
    for label in (GUARDS, GATE):
        assert _if(_step(touch, label)) is None, f"{label!r} must run whenever the job does"
    assert re.search(r"^          UIAUDIT_GATE: touch$", _step(touch, GATE), re.M)


def _normalised(step: str) -> str:
    return "\n".join(ln.rstrip() for ln in step.splitlines() if ln.strip())


#: 🔴 THE TWO STEPS THIS JOB OWNS, PINNED AS WHOLE NORMALISED STRINGS. A substring check was walkable:
#: an `exit 0` as the step's first line, a dropped rc check, an inverted count, `|| true` for
#: `|| rc=$?`, or `UIAUDIT_GATE=all` inline each left every assertion above green (measured in
#: round 1 of the PR's audit). Any edit to these steps must edit this literal too, in the same diff.
GUARDS_STEP = r"""name: the touch gate's own guards and browser controls, under the pinned chromium
        run: |
          set -uo pipefail
          tests='TestTheTouchGateBindsExactlyTheTouchRefusals TestTheWalkGateFlagAcceptsExactlyTwoValues TestTheRefusalLineIsTheOnePwaCheckGreps TestTheWalkRefusalsCanEachGoRED TestTheTouchRefusalsBindOnlyTheTouchRungs TestTheReachabilityRefusalRefusesAnEMPTYSide TestTheInputFontThresholdIsSixteenPixelsExactly TestTouchEmulationReachesThePageAtEveryRung TestTheReachabilityRefusalGoesREDWhenEmulationIsOff TestTheTargetSizeRuleRunsOnlyBecauseItIsEnabled TestTheInputFontMeasurementSeesA14pxInput TestAnOverflowingPageIsRecordedAsOverflowAtATouchRung'
          pattern="^($(echo $tests | tr ' ' '|'))\$"
          rc=0
          ( cd uiaudit && go test -count=1 -v -run "$pattern" . ) > /tmp/touch-guards.log 2>&1 || rc=$?
          grep -E '^(=== RUN|--- |[[:space:]]+--- (FAIL|SKIP)|ok|FAIL|panic)' /tmp/touch-guards.log || true
          if [ "$rc" != "0" ] || grep -qE '^[[:space:]]*--- (FAIL|SKIP)' /tmp/touch-guards.log; then
            echo "::error::the touch gate's guards failed or skipped (go test rc $rc)"
            exit 1
          fi
          for t in $tests; do
            if ! grep -qE "^--- PASS: $t \(" /tmp/touch-guards.log; then
              echo "::error::$t printed no top-level PASS line — it did not run"
              exit 1
            fi
          done
          echo "touch gate guards: $(echo $tests | wc -w) named, each PASSED"
"""

GATE_STEP = r"""name: the walk's touch refusals — BLOCKING
        env:
          UIAUDIT_WORK: ${{ runner.temp }}/uiaudit-touch
          UIAUDIT_GATE: touch
        run: |
          set -uo pipefail
          needle='uiaudit: TOUCH GATE PASSED'
          if ! grep -qF "const touchGatePassed = \"$needle" uiaudit/main.go; then
            echo "::error::this step greps for '$needle', which uiaudit/main.go's touchGatePassed no longer starts with; the gate would match nothing"
            exit 1
          fi
          mkdir -p "$UIAUDIT_WORK"
          rc=0
          ./uiaudit/run.sh 2>&1 | tee "$UIAUDIT_WORK/walk.log" || rc=$?
          if [ "$rc" != "0" ]; then
            echo "::error::the touch-gated walk exited $rc — a touch refusal (its message is above, under 'regression class'), or a harness that could not measure"
            exit 1
          fi
          if ! grep -qF "$needle" "$UIAUDIT_WORK/walk.log"; then
            echo "::error::the walk exited 0 without printing '$needle', so it never reached the touch verdict"
            exit 1
          fi
          for line in 'uiaudit:   GATE touch — REFUSALS: 0 horizontal overflow over ' \
                      'uiaudit:   TOUCH REACHABILITY refusal PASSED: ' \
                      'uiaudit:   TOUCH TARGET SIZE refusal PASSED: ' \
                      'uiaudit:   INPUT FONT refusal PASSED: '; do
            n=$(grep -cF "$line" "$UIAUDIT_WORK/walk.log" || true)
            if [ "$n" != "1" ]; then
              echo "::error::'$line' printed $n time(s), want exactly 1"
              exit 1
            fi
            grep -F "$line" "$UIAUDIT_WORK/walk.log"
          done
"""


def test_the_touch_jobs_own_steps_are_pinned_WHOLE():
    touch = JOBS["uiaudit-touch"]
    assert _normalised(_step(touch, GUARDS)) == _normalised(GUARDS_STEP)
    assert _normalised(_step(touch, GATE)) == _normalised(GATE_STEP)


def test_every_test_the_guards_step_names_is_declared_in_the_uiaudit_module():
    """A name that matches no `func Test…` would be refused at run time (no PASS line); this says so
    before CI does, and pins the list against a rename on the Go side."""
    from pathlib import Path
    mod = Path(__file__).resolve().parent.parent / "uiaudit"
    declared = set()
    for f in mod.glob("*_test.go"):
        declared |= set(_go_test_funcs(f.read_text(encoding="utf-8")))
    (names,) = re.findall(r"^          tests='([^']+)'$", _step(JOBS["uiaudit-touch"], GUARDS), re.M)
    missing = [n for n in names.split() if n not in declared]
    assert len(names.split()) == 12 and not missing, missing


def test_the_touch_gates_needle_and_verdict_lines_are_ones_uiaudit_actually_prints():
    """The step greps four verdict lines and a needle. Each must be a string `uiaudit/main.go` can
    print — a needle nothing produces is a gate that can only ever fail (or, with an inverted test,
    only ever pass). Text only: that the walk prints them is the live run's claim."""
    from pathlib import Path
    main = (Path(__file__).resolve().parent.parent / "uiaudit/main.go").read_text(encoding="utf-8")
    gate = _step(JOBS["uiaudit-touch"], GATE)
    (needle,) = re.findall(r"^          needle='([^']+)'$", gate, re.M)
    assert f'const touchGatePassed = "{needle}' in main
    lines = re.findall(r"'(uiaudit:   [^']+)'", gate)
    assert len(lines) == 4, lines
    for line in lines:
        # The Go source spells each as a Printf format: compare up to the first verb.
        head = line.split("%")[0]
        assert f'"{head}' in main, f"{line!r} is not a prefix uiaudit/main.go prints"
