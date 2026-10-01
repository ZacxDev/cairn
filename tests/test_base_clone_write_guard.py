"""Guards for `.claude/hooks/base-clone-write-guard.py`.

🔴 WHAT MAKES THIS SUITE WORTH TRUSTING IS THE PAIR, NOT THE GREEN. A hook that
never refuses anything passes every "does it allow X?" test in here, so the
refusal cases below are the POSITIVE CONTROL — they must produce a `deny`, and
one of them asserts the refusal's own text so a hook wired to nothing cannot
satisfy it. The allow cases are the NEGATIVE CONTROL for over-firing, and three
of them pin recipes this repo and the wider fleet actually use, where a refusal
would train everybody to route around the guard.

🔴 AND THE GUARD FAILS **OPEN**, WHICH INVERTS THE USUAL READING OF A GREEN HERE.
`permissionDecision = "deny"` is the only observable that means "the guard ran and
decided"; an empty stdout is indistinguishable from "the guard crashed", "the
guard is not wired up" and "the guard allowed it". So every allow assertion in
this file is weak BY CONSTRUCTION, and the refusal cases are what carry the
suite. Said plainly rather than left for a reader to infer from a wall of
`assert_allows`.
"""
from __future__ import annotations

import json
import os
import shutil
import re
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
HOOK = ROOT / ".claude" / "hooks" / "base-clone-write-guard.py"
DOC = ROOT / "claudedocs" / "working-in-parallel.md"

#: A deterministic identity for the throwaway repos, so a host with no global git
#: config does not turn a commit into a fixture failure that reads as a defect.
_GIT_ENV = {
    "GIT_AUTHOR_NAME": "cairn tests",
    "GIT_AUTHOR_EMAIL": "tests@example.invalid",
    "GIT_COMMITTER_NAME": "cairn tests",
    "GIT_COMMITTER_EMAIL": "tests@example.invalid",
    "GIT_CONFIG_GLOBAL": os.devnull,
    "GIT_CONFIG_SYSTEM": os.devnull,
}


def _refused_from_hook() -> frozenset[str]:
    """The `_REFUSED` ledger, read out of the hook's SOURCE.

    Parsed rather than imported because the hook calls `main()` at import time (it
    is a script), so importing it would consume stdin and exit. A regex over the
    literal is the honest alternative; `test_the_hook_ledger_is_parseable` is the
    positive control that this parse can actually see a member, so a pattern that
    silently matched nothing cannot make the comparison below vacuous.
    """
    text = HOOK.read_text(encoding="utf-8")
    block = re.search(r"_REFUSED = frozenset\(\{(.*?)\}\)", text, re.S)
    assert block, "could not find the _REFUSED literal in the hook source"
    return frozenset(re.findall(r'"([a-z0-9-]+)"', block.group(1)))


def _refused_from_doc() -> frozenset[str]:
    """The subcommands the doc's refused-set TABLE names, in backticks."""
    text = DOC.read_text(encoding="utf-8")
    start = text.index("| refused | why it collides |")
    end = text.index("\n\n", start)
    rows = text[start:end].splitlines()[2:]  # skip header + separator
    names: set[str] = set()
    for row in rows:
        first_column = row.split("|")[1]
        names.update(re.findall(r"`([a-z0-9-]+)`", first_column))
    return frozenset(names)


def _init_clone(path: Path) -> Path:
    """A throwaway repo carrying its own COPY of the hook. Returns that copy.

    🔴 THE COPY IS LOAD-BEARING, NOT CONVENIENCE. The guard only polices the
    repository it ships in — it resolves its OWN repo from `__file__` and compares
    against the cwd's — so a fixture that invoked the cairn checkout's hook while
    standing in a synthetic repo would be measuring the cross-repo REFUSAL path
    and never the refusal itself. Copying makes each fixture a faithful miniature
    of the real deployment (`<repo>/.claude/hooks/<file>`).

    ⚠ THE REASON GIVEN HERE FOR `shutil.copy` OVER A SYMLINK WAS WRONG, AND AN
    AUDIT MEASURED IT. It claimed `os.path.realpath` on a symlink "resolves back
    to the cairn checkout and the guard would police the wrong tree". The guard
    uses `os.path.abspath(__file__)` and never `realpath` on it, so a symlinked
    hook resolves its own directory to the SYMLINK's location — the fixture repo —
    and behaves correctly; the auditor drove that case and saw it work. A copy is
    kept anyway, as a PREFERENCE rather than a necessity: it does not depend on
    which of `abspath`/`realpath` the hook happens to use, so changing that line
    in the hook cannot silently repoint every fixture in this file. Recorded
    because a false reason in a comment is what stops the next reader checking.
    """
    path.mkdir(parents=True, exist_ok=True)
    env = {**os.environ, **_GIT_ENV}
    subprocess.run(["git", "init", "-q", "-b", "main"], cwd=path, check=True, env=env)
    hooks = path / ".claude" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    local_hook = hooks / HOOK.name
    shutil.copy(HOOK, local_hook)
    (path / "seed.txt").write_text("seed\n", encoding="utf-8")
    subprocess.run(["git", "add", "seed.txt"], cwd=path, check=True, env=env)
    subprocess.run(["git", "commit", "-qm", "seed"], cwd=path, check=True, env=env)
    return local_hook


def _add_worktree(clone: Path, wt: Path, branch: str = "side") -> None:
    subprocess.run(
        ["git", "worktree", "add", "-q", "-b", branch, str(wt)],
        cwd=clone, check=True, env={**os.environ, **_GIT_ENV},
    )


#: The hook copy the CURRENT fixture installed. `_run_hook` prefers it, so each
#: test drives the copy inside the repo it is standing in rather than the cairn
#: checkout's — which, under the guard's own repo-identity condition, would be
#: measuring the cross-repo ALLOW and never the refusal. Set by the fixture and
#: cleared after it, so a test that forgets the fixture cannot silently inherit it.
_ACTIVE_HOOK: Path | None = None


def _run_hook(command: str, cwd: Path | str, env_extra: dict | None = None,
              hook: Path | None = None) -> dict:
    """Invoke the hook exactly the way Claude Code does, and parse its verdict."""
    payload = {"tool_name": "Bash", "tool_input": {"command": command}, "cwd": str(cwd)}
    env = {k: v for k, v in os.environ.items() if k != "BASE_CLONE_WRITE_OK"}
    env.update(_GIT_ENV)
    env.update(env_extra or {})
    proc = subprocess.run(
        [sys.executable, str(hook or _ACTIVE_HOOK or HOOK)],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        timeout=60,
        env=env,
    )
    # 🔴 The exit code is asserted on EVERY call, because a PreToolUse hook blocks
    # on exit 2 and on nothing else. A guard that decided "deny" while exiting
    # non-zero would be BOTH wrong about its contract and invisible to a test that
    # only read stdout.
    assert proc.returncode == 0, (
        f"the hook must always exit 0 (deny is carried in stdout, and any other "
        f"status silently ALLOWS); got {proc.returncode}\nstderr: {proc.stderr}"
    )
    if not proc.stdout.strip():
        return {}
    return json.loads(proc.stdout)


def _decision(verdict: dict) -> str | None:
    return (verdict.get("hookSpecificOutput") or {}).get("permissionDecision")


def _reason(verdict: dict) -> str:
    return (verdict.get("hookSpecificOutput") or {}).get("permissionDecisionReason", "")


@pytest.fixture
def parallel_clone(tmp_path: Path):
    """A base clone with ONE linked worktree — the state the guard fires in.

    Yields `(clone, worktree, hook)`, where `hook` is the clone's OWN copy. Tests
    must invoke that copy: see `_init_clone` for why anything else measures the
    cross-repo path instead of the refusal.
    """
    global _ACTIVE_HOOK
    clone = tmp_path / "clone"
    wt = tmp_path / "wt"
    local_hook = _init_clone(clone)
    _add_worktree(clone, wt)
    _ACTIVE_HOOK = local_hook
    yield clone, wt, local_hook
    _ACTIVE_HOOK = None


# ---------------------------------------------------------------- the instrument

def test_the_hook_ledger_is_parseable():
    """POSITIVE CONTROL for the parse the ledger comparison rests on.

    Without this, a regex that matched nothing would make
    `test_the_refused_set_matches_the_documented_table` compare two empty sets and
    pass — the disjoint-key-spaces failure this repo already tracks, where a green
    run cannot tell you the check went inert.
    """
    refused = _refused_from_hook()
    assert "commit" in refused, "the parse found no `commit` — it is matching nothing"
    assert len(refused) >= 5, f"implausibly small ledger, parse is probably broken: {refused}"


def test_the_doc_table_is_parseable():
    """POSITIVE CONTROL for the other half of the same comparison."""
    documented = _refused_from_doc()
    assert "commit" in documented
    assert len(documented) >= 5, f"parse is probably broken: {documented}"


def _declared_out_from_doc() -> frozenset[str]:
    """The subcommands the doc's DELIBERATELY-OUT table names, in backticks.

    A second table, with its own header, so the refused-set parse above cannot see
    it and the two cannot be confused for one another. Whole backticked cells
    rather than `[a-z0-9-]+`: the entries are `worktree remove` and `branch -D`,
    and the spelling — the flag, the second word — is the part that carries the
    decision.
    """
    text = DOC.read_text(encoding="utf-8")
    start = text.index("| NOT refused, and it is not a read |")
    end = text.index("\n\n", start)
    rows = text[start:end].splitlines()[2:]  # skip header + separator
    names: set[str] = set()
    for row in rows:
        names.update(re.findall(r"`([^`]+)`", row.split("|")[1]))
    return frozenset(names)


def test_the_doc_OUT_table_is_parseable():
    """POSITIVE CONTROL for the parse the decision test below rests on."""
    out = _declared_out_from_doc()
    assert out, "the out-table parse found nothing — it is matching nothing"


def _leading_words_from_hook() -> frozenset[str]:
    """The wrapper words `_LEADING_WORDS` skips, read out of the hook's SOURCE."""
    text = HOOK.read_text(encoding="utf-8")
    block = re.search(
        r"_LEADING_WORDS: dict\[str, tuple\[int, frozenset\[str\]\]\] = \{(.*?)\n\}",
        text, re.S)
    assert block, "could not find the _LEADING_WORDS literal in the hook source"
    # Keys only: a key is a quoted word at the start of a line, before a `:`.
    return frozenset(re.findall(r'^\s{4}"([^"]+)": \(', block.group(1), re.M))


def test_the_wrapper_ledger_is_parseable():
    """POSITIVE CONTROL for the parse the pin below rests on."""
    words = _leading_words_from_hook()
    assert "timeout" in words, f"the parse found no `timeout`: {words}"
    assert len(words) >= 10, f"implausibly small, parse is probably broken: {words}"


def test_the_WRAPPER_LEDGER_is_PINNED_so_GROWING_IT_IS_A_DECISION():
    """🔴 THIS GUARD EXISTS TO STOP A RATCHET, WHICH IS AN UNUSUAL THING FOR A TEST TO
    DO, SO IT SAYS SO.

    The closing condition this ledger was built against — "a parametrised case per
    wrapper word, watched red" — defined done as an ENUMERATION OVER AN OPEN SET. It
    is RETIRED (see the hook's own table): the class cannot be enumerated, `ssh`,
    `ionice -p`, `watch`, `coproc`, a shell function and `bash -c '…'` all remain
    unhandled, and the root fix is nested-shell recursion rather than another word.

    So the set is pinned and this test fails on GROW as well as shrink. A grow is not
    forbidden — it is made into a decision with a place to argue for it, instead of a
    chore the next reader feels obliged to complete. ⚠ The justification on record: a
    replay of one host's session history, 37,268 distinct real Bash commands, moved
    **zero** verdicts for any of the twelve words this ledger added, while the other
    two repairs in the same change moved 17 and 13. The counter-argument is on record
    too, beside it — a guard deters the unprecedented, and zero past is not zero
    future — which is why the CODE stays and only the closing condition retired.

    ⚠ INVARIANT GUARD, NOT REGRESSION COVERAGE: green at `336aebf` and at every ref
    where the ledger has these members. It pins a decision, not a fix.
    """
    assert _leading_words_from_hook() == {
        # shell reserved words and the brace group
        "{", "}", "!", "if", "while", "until", "then", "do", "else", "elif",
        # command wrappers
        "eval", "time", "command", "exec", "nohup", "nice", "stdbuf", "sudo",
        "xargs", "env", "timeout",
    }


def _dry_run_keys_from_hook() -> frozenset[str]:
    """The subcommands `_DRY_RUN_SHORT_VALUE_FLAGS` exempts a dry run for."""
    text = HOOK.read_text(encoding="utf-8")
    block = re.search(
        r"_DRY_RUN_SHORT_VALUE_FLAGS: dict\[str, frozenset\[str\]\] = \{(.*?)\n\}",
        text, re.S)
    assert block, "could not find the _DRY_RUN_SHORT_VALUE_FLAGS literal"
    return frozenset(re.findall(r'"([a-z-]+)":', block.group(1)))


def test_the_dry_run_ledger_is_parseable():
    """POSITIVE CONTROL for the parse the test below rests on."""
    keys = _dry_run_keys_from_hook()
    assert "clean" in keys, f"the parse found no `clean` — it matches nothing: {keys}"


def test_the_DRY_RUN_exemption_covers_EXACTLY_THREE_SUBCOMMANDS_AND_NO_MORE():
    """🔴 AN EXEMPTION LEDGER THAT CAN GROW SILENTLY IS THE HAZARD HERE, not a
    missing row — so this fails on GROW as well as on shrink.

    Three refused subcommands have a dry-run spelling that was measured to change
    nothing and are deliberately OUT (`add -n`, `commit --dry-run`,
    `apply --check`): an operator decision, not a mechanical consequence of the
    predicate. And 🔴 TWO SPELLINGS THAT LOOK LIKE DRY RUNS ARE NOT — `git merge
    --no-commit` staged a merge AND moved HEAD on a fast-forward, and `git
    cherry-pick -n` staged the picked file, both measured CHANGED. Adding `merge`
    or `cherry-pick` to this ledger would exempt a real write, so the set is pinned
    rather than described.

    Every key must also be IN `_REFUSED`: exempting a subcommand that is not
    refused is dead code that reads as coverage.
    """
    keys = _dry_run_keys_from_hook()
    assert keys == {"clean", "mv", "rm"}, keys
    assert keys <= _refused_from_hook(), keys - _refused_from_hook()


def test_the_two_shared_state_WRITERS_LEFT_OUT_carry_a_RECORDED_DECISION():
    """🔴 THE CLOSING CONDITION WAS A DECISION PER SUBCOMMAND, NOT A WIDER SET.

    An audit found five subcommands writing shared state outside `_REFUSED`:
    `clean`, `rm` and `mv` went IN, and `worktree remove` and `branch -D` stayed
    out — the first because this repo's own parallel-work recipe PRESCRIBES
    `git -C "$REPO" worktree remove "$WT"` from the base clone and refusing a
    documented recipe is the guard's own stated failure mode, and both because a
    worktree registration and a branch ref live in the COMMON git dir, so the same
    write is available identically from any worktree and conditions 2 and 3 cannot
    scope the hazard at all.

    This pins the DECISION rather than the prose: the out-table names exactly those
    two, and neither appears in the refused ledger. It fails if the set GROWS (a
    third subcommand quietly declared out) or SHRINKS (a decision deleted), and it
    fails if one is ever added to `_REFUSED` without its doc row moving — which is
    the mirror of `test_the_refused_set_matches_the_documented_table`.
    """
    assert _declared_out_from_doc() == {"worktree remove", "branch -D"}
    refused = _refused_from_hook()
    for entry in _declared_out_from_doc():
        assert entry.split()[0] not in refused, (
            f"`{entry}` is declared OUT in the doc and IN in the hook's ledger"
        )


def test_the_refused_set_matches_the_documented_table():
    """The ledger and the doc move together, and this fails on GROW *or* SHRINK.

    A subcommand added to the hook without the doc's table moving is a refusal
    nobody can look up; one removed without the doc moving leaves the doc
    promising a guard that is gone. Both directions are asserted because a
    one-directional check is satisfied by the wrong half being empty.
    """
    assert _refused_from_hook() == _refused_from_doc()


# ------------------------------------------------------- POSITIVE: it can refuse

def test_a_commit_in_the_base_clone_is_refused(parallel_clone):
    clone, _ = parallel_clone[:2]
    verdict = _run_hook("git commit -m 'work'", clone)
    assert _decision(verdict) == "deny"


def test_the_refusal_names_the_doc_and_the_override(parallel_clone):
    """The refusal message is the ONLY routing this feature has.

    `AGENTS.md` is at its byte ceiling, so no per-session prose points at the
    rules; if this message stops naming the doc, the doc becomes unreachable. The
    override is asserted for the same reason — a guard with no documented way past
    it is one somebody disables.
    """
    clone, _ = parallel_clone[:2]
    reason = _reason(_run_hook("git commit -m 'work'", clone))
    assert "claudedocs/working-in-parallel.md" in reason
    assert "BASE_CLONE_WRITE_OK=1" in reason
    assert "worktree add" in reason, "the refusal must carry the recipe, not just a refusal"


@pytest.mark.parametrize("command", [
    "git add somefile",
    "git checkout other-branch",
    "git switch other-branch",
    "git reset --soft HEAD~1",
    "git rebase origin/main",
    "git cherry-pick deadbeef",
    "git stash",
    "git merge origin/main",
])
def test_every_tree_mutating_subcommand_is_refused(parallel_clone, command):
    clone, _ = parallel_clone[:2]
    assert _decision(_run_hook(command, clone)) == "deny", command


def test_a_commit_later_in_a_chain_is_still_seen(parallel_clone):
    """Reading only the first word would miss `… && git commit …`.

    That is the shape a session actually types, so a guard that only inspected the
    leading command would be inert in practice while passing a test suite built
    from single commands.
    """
    clone, _ = parallel_clone[:2]
    verdict = _run_hook("git fetch origin && git commit -m 'work'", clone)
    assert _decision(verdict) == "deny"


def test_the_global_option_hop_does_not_hide_the_subcommand(parallel_clone):
    """`git -C <path> commit` must not read `<path>` as the subcommand.

    The operator's host-wide guard records this exact hop being added as a
    separate check after it was found to bypass an earlier one, so it is pinned
    here rather than assumed.
    """
    clone, _ = parallel_clone[:2]
    verdict = _run_hook(f"git -C {clone} commit -m 'work'", clone)
    assert _decision(verdict) == "deny"


# ------------------------------------- NEGATIVE: it must not fire where it would hurt

def test_the_same_commit_inside_the_linked_worktree_is_allowed(parallel_clone):
    """The discriminator, and the whole point of the feature.

    Same command, same clone, same repo — only the cwd differs. If this refuses,
    the guard has banned the very workflow it is telling people to use.
    """
    wt = parallel_clone[1]
    assert _decision(_run_hook("git commit -m 'work'", wt)) is None


def test_a_clone_with_no_linked_worktrees_is_left_alone(tmp_path):
    """An outside contributor's fresh clone must never see this hook fire.

    The file is tracked in a PUBLIC repository, so the common case for a stranger
    is exactly this state: one worktree, nobody working in parallel, nothing to
    collide with.
    """
    clone = tmp_path / "solo"
    local_hook = _init_clone(clone)
    # 🔴 ITS OWN copy. Driving the cairn checkout's hook here would ALLOW because
    # the repos differ, not because there are no worktrees — the test would pass
    # for the wrong reason and go green with the worktree condition deleted.
    assert _decision(_run_hook("git commit -m 'work'", clone, hook=local_hook)) is None


def test_the_ff_only_resync_recipe_is_allowed(parallel_clone):
    """🔴 The documented base-clone re-sync, and the trap this test exists for.

    `git -C <repo> fetch origin && git merge --ff-only origin/main` is how a
    write-only base clone is kept current. It cannot conflict or autostash: it
    fast-forwards or it REFUSES, and the refusal is the operator's signal that the
    clone diverged. A guard that blocked `merge` wholesale would break the one
    command the standing rules tell everybody to run — and it would look correct,
    because `merge` genuinely does mutate the tree.
    """
    clone, _ = parallel_clone[:2]
    verdict = _run_hook("git fetch origin && git merge --ff-only origin/main", clone)
    assert _decision(verdict) is None


def test_a_pathspec_checkout_is_allowed(parallel_clone):
    """`git checkout <ref> -- <paths>` takes a file; it does not move HEAD.

    It is also the recipe for reading a doc at a ref, which the staleness section
    of the doc tells people to do, so refusing it would contradict the same file.
    """
    clone, _ = parallel_clone[:2]
    verdict = _run_hook("git checkout origin/main -- AGENTS.md", clone)
    assert _decision(verdict) is None


def test_a_bare_checkout_is_still_refused_when_chained_with_a_pathspec_one(parallel_clone):
    """The pathspec exemption must be per-command, not per-command-LINE.

    Otherwise a chain containing one `--` smuggles a HEAD-moving
    `git checkout <branch>` past the guard on a sibling's authority — the
    "satisfied by a neighbour" shape this repo has already measured once, in a
    guard that searched a whole block for a field.
    """
    clone, _ = parallel_clone[:2]
    verdict = _run_hook(
        "git checkout origin/main -- AGENTS.md && git checkout other-branch", clone)
    assert _decision(verdict) == "deny"


def test_a_bare_merge_chained_after_an_ff_only_one_is_still_refused(parallel_clone):
    """🔴 THE DEFECT A ROUND-0 AUDIT FOUND BY READING THE THREE EXEMPTIONS SIDE BY SIDE.

    `_is_ff_only_merge` tested the whole command LINE while its two siblings
    tested per SEGMENT, so one `--ff-only` anywhere excused every other merge in
    the chain. MEASURED before the fix: this command passed straight through,
    while the identical shape one function over — a pathspec checkout chained
    with a bare one — was correctly refused. The asymmetry was the defect, and
    the only exemption with no chain test was the broken one.

    This is the same "satisfied by a neighbour" shape the sibling test below
    pins for `checkout`; the two must stay in step.
    """
    clone, _ = parallel_clone[:2]
    verdict = _run_hook(
        "git merge --ff-only origin/main && git merge other-branch", clone)
    assert _decision(verdict) == "deny"


@pytest.mark.parametrize("command", [
    # 🔴 EVERY ONE OF THESE WAS MEASURED PASSING STRAIGHT THROUGH, and the first
    # was proved end to end: it staged a file the single-line spelling was refused
    # for. The cause was tokenising with `shlex.split` and then looking for
    # operator TOKENS — shlex does not emit operators unless they are already
    # space-separated, and treats a newline as ordinary whitespace.
    "git status\ngit commit -m x",           # a newline is not a separator at all
    "git fetch; git commit -m x",            # `fetch;` is ONE token
    "git fetch;git commit -m x",
    "git fetch&&git commit -m x",            # `fetch&&git` likewise
    "false||git commit -m x",
    "true|git commit -m x",
    "(git commit -m x)",                     # `(git` is not the program name
    "cd /tmp; git commit -m x",
    "ls; git commit -m x",
    # `comments=True` truncated the line at a `#` bash treats as literal, making
    # the parse NARROWER than the shell's — in the fail-open direction.
    "curl https://example.invalid/x#frag && git commit -m x",
])
def test_an_operator_without_spaces_or_a_newline_still_separates(parallel_clone, command):
    """The five spellings a round-1 audit walked the guard with.

    `;` is the one operator a shell never requires whitespace around, and
    `git fetch; git commit` is idiomatic — so this was not an exotic gap but most
    of the ways a session actually types a git write.
    """
    clone, _ = parallel_clone[:2]
    assert _decision(_run_hook(command, clone)) == "deny", command


@pytest.mark.parametrize("command", [
    "{ git commit -m x; }",
    "{ git add seed.txt; }",
    "if true; then git commit -m x; fi",
    "for f in a; do git add $f; done",
])
def test_a_reserved_word_before_the_command_does_not_hide_it(parallel_clone, command):
    """🔴 `{ git commit -m x; }` WALKED THE GUARD, AND IT READ AS COVERED.

    The parser took the first word as the program name, so a brace group made it
    `{`. The docstring lists the `(…)` twin among the closed walks and `(` IS
    closed — because `(` is an OPERATOR character while `{` is a reserved WORD, so
    the fix for one could never cover the other. A round-2 audit measured the pair.

    The `if`/`for` one-liners were missed before the rewrite too, so they are not
    regressions; they close with the same reserved-word skip and are pinned here so
    the skip cannot be narrowed back to braces alone.
    """
    clone, _ = parallel_clone[:2]
    assert _decision(_run_hook(command, clone)) == "deny", command


@pytest.mark.parametrize("command", [
    # A heredoc BODY is data. Writing this repo's own recipe into a file is the
    # ordinary case, because every recipe here is one command per line starting
    # with `git`.
    "cat > /tmp/recipe <<'EOF'\ngit commit -m x\nEOF",
    "cat > /tmp/recipe <<EOF\ngit add .\nEOF",
    # A quoted multi-line argument, likewise.
    "echo 'line1\ngit add .\nline3' > /tmp/x",
])
def test_quoted_and_heredoc_CONTENT_is_data_not_a_command(parallel_clone, command):
    """🔴 A FAIL-CLOSED REGRESSION THE REWRITE INTRODUCED, MEASURED BY ROUND 2.

    Splitting the raw string on newlines before knowing the quote state cut through
    heredoc bodies and quoted arguments, so their inner lines were read as commands
    and REFUSED — with a message diagnosing a shared-tree mutation that was not
    happening. This file's own header forbids that direction, and it trips the
    guard's stated failure mode: break a documented recipe and people route around
    the guard.
    """
    clone, _ = parallel_clone[:2]
    assert _decision(_run_hook(command, clone)) is None, command


def test_a_command_after_a_heredoc_terminator_is_still_a_command(parallel_clone):
    """The control that stops the fix above from becoming a hole.

    "Skip heredoc bodies" must not mean "stop reading at the first `<<`". Once the
    terminator line has gone by, what follows is a command again — and a
    token-space skip gets exactly this case wrong, which is why the walk is
    line-aware.
    """
    clone, _ = parallel_clone[:2]
    command = "cat > /tmp/recipe <<'EOF'\nbody\nEOF\ngit commit -m x"
    assert _decision(_run_hook(command, clone)) == "deny"


# ------------------- (a) the heredoc OPENER, decided by the quote-aware walk
#
# 🔴 `_shell_lines` CALLED ITSELF "QUOTE- AND HEREDOC-AWARE" WHILE ITS OPENER
# SEARCH RAN A REGEX OVER THE ALREADY-JOINED RAW LINE, so a `<<WORD` in a quoted
# string or a `#` comment opened a heredoc bash never opened and every later line
# was swallowed as its body. The fail-open is reached by ORDINARY TEXT.
#
# Every case in this block was watched at `ffa0eca`, the ref this change is built
# on, and the verdict is named per row. `ffa0eca` is also where the existing
# heredoc cases above were green, which is why they are not repeated here: the one
# that matters most, `<<'EOF'` whose body is `git commit -m x`, is pinned by
# `test_quoted_and_heredoc_CONTENT_is_data_not_a_command` and is EXACTLY what the
# attractive implementation of this fix breaks — "blank out the quoted spans, then
# run the old regex" erases that delimiter, opens no heredoc, and REFUSES the body.


@pytest.mark.parametrize("command,why", [
    ('echo "a <<EOF b"\ngit commit -m x',
     "a `<<` inside DOUBLE quotes; proved end to end at `ffa0eca`, clone 1 -> 2"),
    ("echo 'a <<EOF b'\ngit commit -m x",
     "the same inside SINGLE quotes"),
    ("echo hi # write the recipe with <<EOF\ngit commit -m x",
     "a `<<` inside a trailing comment"),
    ("# a comment that mentions <<EOF\ngit commit -m x",
     "a comment at the START of the line, where there is no previous character"),
    ("echo hi;# see <<EOF\ngit commit -m x",
     "a `#` that begins a word because an OPERATOR ended the previous one — the "
     "only case that reaches `_WORD_BREAK_BEFORE_HASH` rather than the whitespace "
     "test beside it"),
    ("cat > /tmp/f <<EOF\nit's data\nEOF\ngit commit -m x",
     "an apostrophe in a heredoc BODY opened a quote that swallowed the terminator"),
    ("echo x # don't\ngit commit -m y",
     "an apostrophe in a COMMENT did the same, one mechanism over"),
    ("cat > /tmp/f <<EOF\nbody\nEOF\n# a comment with <<X\ngit commit -m x",
     "a comment on the FIRST line after a heredoc terminator — two mechanisms "
     "composed, and ⚠ NOT evidence for the one it was written for: a draft reset "
     "`prev` when a body line flushed, and a mutation sweep scored that reset dead "
     "because the command-mode newline had already cleared it"),
    ("cat <<<word\ngit commit -m x",
     "a herestring: `<<` read out of chars two and three of `<<<`"),
    ('cat <<<"EOF"\ngit commit -m x',
     "the same, with a delimiter-shaped quoted operand"),
])
def test_a_FAKE_heredoc_OPENER_cannot_swallow_the_commands_after_it(
        parallel_clone, command, why):
    """🔴 EVERY ROW WAS MEASURED **ALLOW** AT `ffa0eca` AND IS DENY HERE.

    Three mechanisms, all closed by moving the opener decision INTO the walk that
    already knew the quote state:

      * the opener regex ran on the raw line, so quotes and comments were invisible
        to it (rows 1-4);
      * quotes were tracked inside a heredoc BODY and inside a COMMENT, where bash
        tracks none, so one apostrophe swallowed the terminator or the rest of the
        command (rows 5-6);
      * a `<<<` HERESTRING carries no body, but its second and third `<` read as a
        `<<` and opened a heredoc named after the operand (rows 7-8). The regex
        declined the FIRST `<<` of the run by accident and then matched at the
        second; the walk declines a lookahead whose previous character is `<`.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook(command, clone)) == "deny", f"{why}: {command!r}"


@pytest.mark.parametrize("command,why", [
    ('cat > /tmp/recipe <<"EOF"\ngit commit -m x\nEOF',
     "a DOUBLE-quoted delimiter — quotes that belong to the delimiter, not a string"),
    ("cat > /tmp/recipe <<-EOF\n\tgit commit -m x\n\tEOF",
     "the tab-stripping `<<-` form still opens a heredoc"),
    ("cat > /tmp/recipe << EOF\ngit commit -m x\nEOF",
     "a space between `<<` and the delimiter"),
    ("cat <<A <<B\nx\nA\ngit commit -m x\nB",
     "the SECOND of two heredocs on one line — `re.search` could only find one"),
])
def test_a_REAL_heredoc_BODY_is_still_data_in_every_spelling(
        parallel_clone, command, why):
    """The fail-CLOSED direction, which is the one this file forbids itself.

    Rows 1-3 were green at `ffa0eca` — ⚠ INVARIANT GUARDS, pinning that moving the
    opener search into the walk did not lose a spelling the regex accepted. They
    are the cases the "blank out the quotes first" implementation breaks, so they
    are worth their place even though they evidence nothing about the fix.

    🔴 ROW 4 WAS MEASURED **DENY** AT `ffa0eca` AND IS ALLOW HERE — a FALSE
    POSITIVE, and regression coverage rather than an invariant guard. Only the
    first opener on a line was recorded, so B's body was parsed as commands and a
    line of DATA reading `git commit -m x` was refused with a message diagnosing a
    shared-tree mutation that was not happening.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook(command, clone)) is None, f"{why}: {command!r}"


# ------------------------- (b) wrapper words that used to hide the program name


@pytest.mark.parametrize("wrapper", [
    "if", "while", "until", "command", "nohup", "eval", "exec", "sudo", "xargs",
    "nice",
])
def test_a_WRAPPER_WORD_does_not_hide_the_program_name(parallel_clone, wrapper):
    """🔴 ONE ROW PER WORD, EVERY ONE MEASURED ALLOWING AT `ffa0eca`.

    `_leading_assignments` skipped assignments, `env`, and a `_LEADING_RESERVED`
    set of eight shell words — so the program name of `if git commit -m x` was read
    as `if` and the segment was not a git call at all. The repair is ONE ledger,
    `_LEADING_WORDS`, which absorbed both of those: `claude/RULES.md`'s "one rule,
    one place" is the reason it is not a second set beside the first.

    Parametrised per word rather than written as one case, for the reason the
    single-`&` cases above are: a suite that exercised one spelling of a class is
    exactly how this class stayed open while reading as covered.
    """
    clone = parallel_clone[0]
    command = f"{wrapper} git commit -m x"
    assert _decision(_run_hook(command, clone)) == "deny", command


@pytest.mark.parametrize("prefix,why", [
    ("timeout 5", "a bare DURATION operand, the only operand-consuming row"),
    ("timeout 5s", "the same with a unit suffix"),
    ("timeout --foreground 5", "a flag that takes no value, then the operand"),
    ("timeout -s TERM 5", "a flag that takes a SEPARATE value, then the operand"),
    ("timeout -k 1 5", "two values before the operand"),
    ("sudo -u somebody", "`-u <user>`"),
    ("sudo -u somebody --", "…and an end-of-options marker"),
    ("xargs -n 1", "`-n <count>`"),
    ("xargs -I {}", "`-I <string>`"),
    ("xargs --max-args=1", "an ATTACHED value takes no separate word"),
    ("stdbuf -o 0", "`-o <mode>`"),
    ("stdbuf -o0", "the attached spelling of the same flag"),
    ("nice -n 5", "`-n <adjustment>`"),
    ("nice -5", "the old attached spelling, which is not a known flag at all"),
    ("env -i", "pre-existing: `env`'s own flags, green at `ffa0eca`"),
    ("env -u FOO", "pre-existing: `env -u <name>` consumes a value, green there too"),
    ("/usr/bin/env -i", "the ledger is matched on the BASENAME too — green at "
                        "`ffa0eca`, which open-coded exactly this one"),
    ("/usr/bin/time", "…and the basename match is what generalises it: this was "
                      "ALLOW at `ffa0eca`, because `time` was matched as a bare "
                      "WORD only"),
])
def test_a_wrapper_that_consumes_a_VALUE_still_finds_the_program(
        parallel_clone, prefix, why):
    """The half of the ledger that is not just a word list.

    Every row but the last two was measured ALLOW at `ffa0eca`. The two `env` rows
    were already green: they are ⚠ INVARIANT GUARDS, and they are here because
    folding the open-coded `env` branch into the ledger is exactly the kind of
    consolidation that silently drops the behaviour it absorbed.
    """
    clone = parallel_clone[0]
    command = f"{prefix} git commit -m x"
    assert _decision(_run_hook(command, clone)) == "deny", f"{why}: {command}"


@pytest.mark.parametrize("command,why", [
    ("command -v git", "a LOOKUP, not a run — nothing follows `git` to be a write"),
    ("timeout 5 git status", "a read, reached through the operand-consuming row"),
    ("nice -n 5 git log --oneline -3", "a read behind a wrapper"),
    ("xargs -n 1 git status", "…and behind one with a value flag"),
    ("ssh host.invalid git commit -m x",
     "`ssh` is NOT in the ledger: the write lands on another machine, and skipping "
     "it would invent a false positive"),
])
def test_the_wrapper_ledger_does_not_OVER_fire(parallel_clone, command, why):
    """NEGATIVE CONTROL for the block above, with its POSITIVE CONTROL in the run.

    A ledger that refuses `command -v git` or `timeout 5 git status` has turned a
    read into a refusal, which is the direction this guard forbids itself. The
    paired `deny` in the same fixture is what stops this reading as a hook wired to
    nothing — every assertion here is an ALLOW, and an allow is indistinguishable
    from a crash.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook("git commit -m x", clone)) == "deny", "positive control"
    assert _decision(_run_hook(command, clone)) is None, f"{why}: {command}"


# --------------- (c) three more shared-state writers, and two decisions to leave out


@pytest.mark.parametrize("command", [
    "git clean -fd",
    "git clean -fdx",
    "git clean -f",
    # 🔴 `-e<pattern>` ATTACHED, AND THE PATTERN STARTS WITH AN `n`. The cluster
    # scan in `_is_clean_dry_run` stops at an `e` for exactly this: reading the
    # pattern's letters as flags would call a real delete a dry run. It is the only
    # case that reaches that `break`.
    "git clean -fenfoo",
    "git rm seed.txt",
    "git rm --cached seed.txt",
    "git mv seed.txt renamed.txt",
])
def test_the_three_newly_refused_WRITERS_are_refused(parallel_clone, command):
    """🔴 "EVERYTHING ELSE IS A READ" WAS FALSE, AND EVERY ROW ALLOWED AT `ffa0eca`.

    `git clean -fd` deletes untracked files out of a tree a peer is standing in;
    `git rm` and `git mv` delete or rename tracked files AND stage it. None was in
    the ledger, and the complement of the ledger was being described as reads.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook(command, clone)) == "deny", command


@pytest.mark.parametrize("command,expected", [
    # ---- DRY RUNS: reads, so they must be allowed. Every row measured on git
    # 2.55.0 (rc 0, repository bit-for-bit unchanged) before being asserted here.
    ("git clean -n", None),
    ("git clean --dry-run", None),
    ("git clean -nd", None),
    ("git clean -dn", None),
    ("git clean -n -d", None),
    ("git clean -xn", None),
    ("git rm -n seed.txt", None),
    ("git rm --dry-run seed.txt", None),
    ("git rm -rn seed.txt", None),
    ("git rm -nr seed.txt", None),
    ("git rm -qn seed.txt", None),
    ("git mv -n seed.txt other.txt", None),
    ("git mv --dry-run seed.txt other.txt", None),
    ("git mv -nv seed.txt other.txt", None),
    ("git mv -vn seed.txt other.txt", None),
    ("git mv -kn seed.txt other.txt", None),
    # ---- 🔴 THE POSITIVE CONTROLS, IN THE SAME PARAMETRISED RUN. An exemption
    # that widened to the real spelling is the failure mode here, and a test file
    # that only asserts allows cannot see it.
    ("git clean -fd", "deny"),
    ("git clean -f", "deny"),
    ("git clean -fenfoo", "deny"),
    ("git rm seed.txt", "deny"),
    ("git rm -r seed.txt", "deny"),
    ("git rm -f seed.txt", "deny"),
    ("git mv seed.txt other.txt", "deny"),
    ("git mv -f seed.txt other.txt", "deny"),
    ("git mv -kv seed.txt other.txt", "deny"),
    # ---- 🔴 AND THE CONTROLS THAT MAKE THE *LEDGER KEY* CHECK REACHABLE, which is
    # the half of `_is_dry_run` a dry-run-spelling test cannot exercise. `-n` does
    # NOT mean dry-run everywhere: on `commit` it is `--no-verify`, which commits,
    # and on `cherry-pick` it is `--no-commit`, which was MEASURED to stage the
    # picked file. `git merge --no-commit` staged a merge and moved HEAD on a
    # fast-forward. A predicate that answered on the FLAG rather than on the
    # subcommand would allow all four.
    ("git commit -n -m x", "deny"),
    ("git commit --no-verify -m x", "deny"),
    ("git cherry-pick -n deadbeef", "deny"),
    ("git merge --no-commit other-branch", "deny"),
    ("git add -n seed.txt", "deny"),
])
def test_the_DRY_RUN_exemption_is_ONE_PREDICATE_over_THREE_subcommands(
        parallel_clone, command, expected):
    """🔴 IT SHIPPED `clean`-ONLY AND THAT WAS A FALSE POSITIVE A REVIEW MEASURED.

    At `d6eafdb` — this branch's own head before the fix — `git clean -n` was
    allowed while `git rm -n`, `git rm --dry-run`, `git mv -n` and
    `git mv --dry-run` were **REFUSED**, with the real spellings denying in the same
    run. One predicate open-coded at one of three sites is wrong at the other two,
    and `git rm -n` is exactly what somebody types to see what a `git rm` would do
    BEFORE doing it: refusing the rehearsal alongside the dangerous spelling is how
    a guard teaches that it is noise, which this file's own header calls the worse
    failure. Now `_is_dry_run`, consulted for every subcommand.

    🔴 THE `rm`/`mv` ROWS ARE REAL REGRESSION COVERAGE, UNLIKE THE `clean` ONES.
    They were measured **deny** at `d6eafdb` and allow here. The `clean` rows were
    green at `ffa0eca` *vacuously* — `clean` was not refused there at all — and are
    green at `d6eafdb` for the right reason, so they are ⚠ invariant guards that the
    consolidation did not lose what it absorbed.

    🔴 AND THE CLUSTER ROWS ARE WHY THIS IS A MEASUREMENT RATHER THAN A SYMMETRY
    ARGUMENT. A brief asserted `git rm -rn` "is not a thing". It is, on git 2.55.0,
    and it is a dry run — as are `-nr`, `-qn`, `-nv`, `-vn` and `-kn`. Reading a
    cluster is safe here only because each subcommand's own short flags say so:
    `clean`'s `-e <pattern>` takes a value so the scan stops at an `e` (and
    `git clean -fenfoo` was measured to really delete, which is the control for
    that stop), while `rm`'s `-n -q -f -r` and `mv`'s `-v -n -f -k` take none.

    ⚠ `git add -n` IS A REAL DRY RUN (measured: rc 0, repository unchanged) AND IS
    ASSERTED **deny** HERE ON PURPOSE. It is one of three such spellings left out of
    the exemption by decision rather than by oversight — `commit --dry-run` and
    `apply --check` are the others — because widening a guard's exemptions is an
    operator call and scope creep in a guard is its own hazard. 🔴 IF THAT DECISION
    CHANGES, THIS ROW IS WHERE IT CHANGES, together with the ledger, the hook
    docstring and the doc. It is NOT a claim that `git add -n` writes.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook(command, clone)) == expected, command


#: Every member of `_REFUSED`, so the help test below cannot cover a subset of the
#: ledger and read as covering it. Derived from the hook's own literal rather than
#: retyped: a subcommand added there with no help case is the gap this closes.
_REFUSED_SUBCOMMANDS = sorted(_refused_from_hook())


@pytest.mark.parametrize("subcommand", _REFUSED_SUBCOMMANDS)
@pytest.mark.parametrize("flag", ["-h", "--help"])
def test_HELP_is_a_READ_for_EVERY_refused_subcommand(parallel_clone, subcommand, flag):
    """🔴 REFUSING `--help` IS THE PUREST FALSE POSITIVE THIS GUARD CAN EMIT, AND IT
    WAS LIVE FOR TWO ROUNDS WHILE THE DOCSTRING CLAIMED OTHERWISE.

    The hook's `WHAT IS DELIBERATELY NOT REFUSED` list named `--help` as a read; the
    code exempted it for `stash` ALONE. So `git rm -h`, `git mv -h`, `git clean -h`,
    `git commit --help` and `git rebase --help` were all **deny** — one predicate at
    one of two sites, wrong at the other. A replay of this project's real Bash
    history found TWO commands that are exactly this shape, so it had already fired
    falsely rather than merely being able to.

    🔴 MEASURED, NOT ASSUMED FROM "git uses parse-options": on git 2.55.0 every one
    of the fourteen answers `-h` with rc 129, `usage:` on the first line, and the
    repository bit-for-bit unchanged; `--help` execs the manual at rc 0, also
    unchanged. The question was asked because `-h` is NOT universally help in git —
    `git grep -h` means `--no-filename` — and the answer is that no refused
    subcommand is such a case, so none needs a `--help`-only exception.

    🔴 PARAMETRISED OVER THE LEDGER ITSELF — `_refused_from_hook()`, not a retyped
    list — so a subcommand added to `_REFUSED` with no help case is impossible.

    Red/green: **27 of these 28 rows were `deny` at `336aebf`**. The single green one
    is `[--help-stash]`, which is the whole point: that was the ONE site the exemption
    existed at. `git stash -h` was refused there too, which is why even `stash` is
    only half-green.
    """
    clone = parallel_clone[0]
    command = f"git {subcommand} {flag}"
    assert _decision(_run_hook(command, clone)) is None, command


@pytest.mark.parametrize("command,expected,why", [
    # 🔴 HELP IS READ FROM THE FIRST WORD AFTER THE SUBCOMMAND ONLY, and these two
    # rows are why. MEASURED with a positive control in the same run (`git commit
    # -m X` committed): `git commit -m -h` **creates a commit** with subject `-h`,
    # because the `-h` is the MESSAGE. A tail scan would allow a real base-clone
    # commit — the fail-open direction — so the exemption reads `rest[0]`, which no
    # flag's value can occupy.
    ("git commit -m -h", "deny", "the `-h` is the commit MESSAGE and it COMMITS"),
    ("git commit --message=-h", "deny", "the attached spelling of the same thing"),
    # ⚠ THE COST, STATED RATHER THAN HIDDEN: these two ARE help requests (rc 129,
    # `usage:`, repository unchanged — measured) and stay refused, because their `-h`
    # is not first. Residual FALSE POSITIVES, kept because the alternative is the
    # fail-open above and because no real spelling puts a flag before `-h`.
    ("git clean -i -h", "deny", "a help request whose `-h` is not first"),
    ("git clean -fh", "deny", "a CLUSTER containing `h` — also help, also refused: "
                              "`h` meaning help inside a cluster was not measured "
                              "for all fourteen, and the cheap direction for an "
                              "unmeasured widening is not to make it"),
])
def test_the_HELP_exemption_does_not_read_a_FLAGS_VALUE(
        parallel_clone, command, expected, why):
    """The fail-open the obvious implementation of the help fix would have opened.

    ⚠ The last two rows assert a REFUSAL that is a false positive, deliberately. They
    are not a claim that those commands write — they pin the boundary this exemption
    chose, so moving it is a decision somebody has to make here on purpose.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook(command, clone)) == expected, f"{why}: {command}"


@pytest.mark.parametrize("command,expected,why", [
    ("git clean -fd", "deny", "the bare control"),
    ("nice -n 5 git clean -fd", "deny",
     "`-n` is nice's ADJUSTMENT flag, not clean's dry-run"),
    ("xargs -n 1 git rm seed.txt", "deny",
     "`-n` is xargs' max-args"),
    ("stdbuf -o 0 git clean -fd", "deny",
     "a wrapper whose flags carry no `n` at all — the control for the control"),
    ("env -- git checkout other-branch", "deny",
     "`--` is env's end-of-options, not a pathspec separator. ⚠ THE OLDEST ROW: "
     "ALLOW at `ffa0eca` already, because `env` was skipped while the `checkout` "
     "exemption searched the whole segment"),
    ("sudo -- git checkout other-branch", "deny", "the same through `sudo`"),
    ("sudo --ff-only git merge other-branch", "deny",
     "not even a real sudo flag, and it still excused the merge"),
    # The exemptions themselves must still work — an over-narrow scan would refuse
    # the documented recipes, which is the direction this file forbids itself.
    ("git clean -n", None, "the exemption still fires when the flag is the "
                           "subcommand's own"),
    ("git checkout origin/main -- AGENTS.md", None, "…and the pathspec recipe"),
    ("git merge --ff-only origin/main", None, "…and the re-sync recipe"),
    ("git stash list", None, "…and the stash read"),
])
def test_an_EXEMPTING_FLAG_must_belong_to_the_SUBCOMMAND_not_to_a_WRAPPER(
        parallel_clone, command, expected, why):
    """🔴 FOUND BY MUTATION-TESTING THE DRY-RUN FIX, NOT BY THE SUITE, AND THE DEFECT
    WAS IN A NEIGHBOURING PREDICATE RATHER THAN IN THE CODE THAT CHANGED.

    Three of the four exemptions scanned the WHOLE segment for their excusing flag.
    Once `_LEADING_WORDS` started skipping `nice`, `xargs` and `sudo`, a flag
    belonging to the WRAPPER excused the git write behind it — measured ALLOW with
    the bare spelling DENYing in the same run. `env -- git checkout other-branch`
    needed no new ledger at all: it was ALLOW at `ffa0eca` too, because `env` was
    already skipped while `checkout`'s exemption searched the whole segment.

    So the lesson is the one worth pinning: **a widening in one predicate can turn a
    latent flaw in a different one into a live bypass.** `_is_exempt` now computes
    the words after the subcommand ONCE and every exemption reads only those.

    Red/green: the `nice`, `xargs`, `sudo` and `env` rows were ALLOW at `d6eafdb`;
    the `env` one was ALLOW at `ffa0eca` as well. The four allow rows are ⚠ invariant
    guards — they pin that narrowing the scan did not break the recipes, which is the
    failure mode a narrowing invites.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook(command, clone)) == expected, f"{why}: {command}"


@pytest.mark.parametrize("command", [
    "git worktree remove /tmp/some-worktree",
    "git worktree prune",
    "git branch -D some-branch",
    "git branch -d some-branch",
])
def test_the_two_DELIBERATE_omissions_stay_allowed(parallel_clone, command):
    """⚠ A DECLARED DECISION, PINNED SO THE DOC CANNOT GO STALE — not a requirement
    that these stay allowed forever.

    `worktree remove` and `branch -D` both write shared state, and both are out of
    `_REFUSED` on a decision recorded in the doc's second table:
    `claudedocs/working-in-parallel.md` PRESCRIBES `git -C "$REPO" worktree remove
    "$WT"` run from the base clone, and both write refs or registrations that live
    in the COMMON git dir — reachable identically from any worktree, so conditions 2
    and 3 cannot scope the hazard and refusing only the base-clone spelling would
    teach that the worktree spelling is safe.

    ⚠ INVARIANT GUARDS: green at `ffa0eca` too, because nothing refused them there
    either. 🔴 IF ONE GOES RED, THE DECISION CHANGED — move the doc row out of the
    out-table and into the refused table in the same commit, which
    `test_the_two_shared_state_WRITERS_LEFT_OUT_carry_a_RECORDED_DECISION` forces.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook(command, clone)) is None, command


@pytest.mark.parametrize("command", [
    "git merge --ff-only origin/main & git merge other-branch",
    "git checkout origin/main -- AGENTS.md & git checkout other-branch",
    "git stash list & git stash",
])
def test_a_single_ampersand_does_not_launder_an_exemption(parallel_clone, command):
    """🔴 ALL THREE EXEMPTIONS WERE WALKABLE WITH ONE CHARACTER.

    The subcommand scanner's break set contained `&`; the exemptions' own
    `re.split` did not. So a single-`&` chain yielded two refused hits while each
    exemption saw ONE segment carrying its excusing flag. Two grammars over one
    language — now a single `_segments`, which is the only structural fix.

    The suite could not see it because every chain case used `&&`, which is why
    these are parametrised on the operator rather than added as one case.
    """
    clone, _ = parallel_clone[:2]
    assert _decision(_run_hook(command, clone)) == "deny", command


@pytest.mark.parametrize("command", [
    "git stash list && git merge --ff-only origin/main",
    "git merge --ff-only origin/main && git checkout origin/main -- AGENTS.md",
    "git stash list && git checkout origin/main -- AGENTS.md",
    "git fetch origin && git merge --ff-only origin/main && git stash list",
])
def test_chaining_two_documented_recipes_is_allowed(parallel_clone, command):
    """Each half is a recipe the guard's own docs say must not be refused.

    They were refused: the exemptions were gated on the whole command producing a
    SINGLE hit, so two individually-exempt commands fell through to the denial.
    The guard's own stated reason for having exemptions is that breaking a
    documented recipe trains everybody to route around the guard — so refusing two
    of them at once is the same defect, doubled.
    """
    clone, _ = parallel_clone[:2]
    assert _decision(_run_hook(command, clone)) is None, command


@pytest.mark.parametrize("command", [
    "echo 'BASE_CLONE_WRITE_OK=1' && git commit -m x",
    "git commit -m 'BASE_CLONE_WRITE_OK=1'",
    "git commit -m 'docs: add the BASE_CLONE_WRITE_OK=1 escape hatch'",
])
def test_the_override_is_not_honoured_from_quotes_or_prose(parallel_clone, command):
    """🔴 THE GUARD DISARMED ITSELF, using a string from its own refusal message.

    The override was a bare `re.search` over the raw line, so the literal
    appearing ANYWHERE excused the command — including inside a quoted commit
    message, and including a session that echoed or grepped the refusal text and
    retried in the same Bash call. It is now read only as a LEADING assignment on
    a segment, which is the one position a shell would actually treat as one.
    """
    clone, _ = parallel_clone[:2]
    assert _decision(_run_hook(command, clone)) == "deny", command


def test_the_guard_polices_only_the_repository_it_ships_in(tmp_path):
    """🔴 IT WAS POLICING OTHER REPOSITORIES, AND ONE OF THEM DISAGREES BY DESIGN.

    Claude Code's Bash cwd persists across calls, so a session rooted in cairn
    that moves into a sibling repo carried this guard there. A round-1 audit
    measured it refusing `git commit` in a repo with 93 worktree registrations
    whose OWN instructions declare that committing to its main branch IS
    deploying — while citing a doc path that does not exist there.

    This is the false-POSITIVE mirror of the documented cwd narrowings, and worse
    than them: a false negative loses a guard, a false positive countermands
    another repo's documented workflow.
    """
    mine = tmp_path / "mine"
    other = tmp_path / "other"
    my_hook = _init_clone(mine)
    _add_worktree(mine, tmp_path / "mine-wt")
    _init_clone(other)
    _add_worktree(other, tmp_path / "other-wt")

    # POSITIVE CONTROL: the same hook, the same command, in its OWN repo.
    assert _decision(_run_hook("git commit -m x", mine, hook=my_hook)) == "deny"
    # …and it must say nothing about a repo it does not ship in.
    assert _decision(_run_hook("git commit -m x", other, hook=my_hook)) is None


def test_the_refusal_does_not_claim_a_peer_is_active(parallel_clone):
    """`git worktree list` reports REGISTRATIONS, not live sessions.

    An earlier refusal said "so another session or agent is working here right
    now", which the count cannot support: measured on the author's clone, 36
    registrations of which two belonged to a live session. A guard's message is
    a claim like any other, and this one was false in the direction that makes a
    reader trust it more.
    """
    clone, _ = parallel_clone[:2]
    reason = _reason(_run_hook("git commit -m 'work'", clone))
    assert "registration" in reason
    assert "working here right now" not in reason


def test_the_refusal_does_not_assert_the_closed_nested_worktree_hazard(parallel_clone):
    """The refusal used to tell the reader to keep worktrees outside the repo root.

    Both hazards it cited are closed in `main` — `leakscan` exits 0 and names the
    nested checkout as a skip (#127), and the root-walking Go guard asks git
    instead of walking (#135). Worse, the harness's own `isolation: "worktree"`
    places worktrees INSIDE the root, so the advice contradicted the default
    mechanism. Pinned because a refusal message is the copy a reader actually
    sees, and re-deriving a dead rule there is free.
    """
    clone, _ = parallel_clone[:2]
    reason = _reason(_run_hook("git commit -m 'work'", clone))
    assert "OUTSIDE the repo root" not in reason
    assert "exit 2" not in reason


@pytest.mark.parametrize("command", [
    "git status -sb",
    "git log --oneline -3",
    "git diff origin/main",
    "git show HEAD",
    "git fetch origin",
    "git worktree list",
    "git push origin HEAD:some-branch",
    "git stash list",
    "git restore somefile",
    "ls -la",
])
def test_reads_and_non_git_commands_are_allowed(parallel_clone, command):
    clone, _ = parallel_clone[:2]
    assert _decision(_run_hook(command, clone)) is None, command


def test_a_word_that_merely_mentions_a_subcommand_is_not_a_command(parallel_clone):
    """`echo "do not commit here"` is prose, not a commit."""
    clone, _ = parallel_clone[:2]
    verdict = _run_hook('echo "remember: never commit in the base clone"', clone)
    assert _decision(verdict) is None


# ------------------------------------------------------------------ the override

def test_the_inline_override_is_honoured(parallel_clone):
    clone, _ = parallel_clone[:2]
    verdict = _run_hook("BASE_CLONE_WRITE_OK=1 git commit -m 'deliberate'", clone)
    assert _decision(verdict) is None


def test_the_environment_override_is_honoured(parallel_clone):
    clone, _ = parallel_clone[:2]
    verdict = _run_hook("git commit -m 'deliberate'", clone,
                        env_extra={"BASE_CLONE_WRITE_OK": "1"})
    assert _decision(verdict) is None


# ------------------------------------------------------------------- fail OPEN

@pytest.mark.parametrize("payload", [
    "",
    "not json at all",
    "[]",
    '{"tool_name": "Read", "tool_input": {"file_path": "x"}}',
    '{"tool_name": "Bash", "tool_input": {}}',
    '{"tool_name": "Bash", "tool_input": {"command": "git commit -m x"}}',
])
def test_a_malformed_or_foreign_payload_exits_zero_and_allows(payload):
    """🔴 Every unexpected condition must ALLOW, because this file is public.

    A guard that can wedge a stranger's checkout because its own input looked odd
    is worse than no guard. The last case carries no `cwd` at all, which is the
    one an older or different harness would send.
    """
    proc = subprocess.run(
        [sys.executable, str(HOOK)],
        input=payload, capture_output=True, text=True, timeout=60,
        env={**os.environ, **_GIT_ENV},
    )
    assert proc.returncode == 0, proc.stderr
    assert _decision(json.loads(proc.stdout) if proc.stdout.strip() else {}) is None


def test_a_cwd_that_is_not_a_git_repo_allows(tmp_path):
    plain = tmp_path / "not-a-repo"
    plain.mkdir()
    assert _decision(_run_hook("git commit -m x", plain)) is None


def test_a_cwd_that_does_not_exist_allows(tmp_path):
    assert _decision(_run_hook("git commit -m x", tmp_path / "gone")) is None


# ------------------------------------------------------------------ the wiring

def test_the_hook_is_wired_in_the_projects_settings():
    """A hook nobody names is a hook nobody runs.

    This repo has already shipped that defect once, in a `checks.*` entry added to
    `flake.nix` while CI named five checks and not it — the guard for a whole
    approach would have been inert while reading as covered precisely because the
    file existed.
    """
    settings = json.loads((ROOT / ".claude" / "settings.json").read_text(encoding="utf-8"))
    carrying = [
        entry for entry in settings["hooks"]["PreToolUse"]
        if any("base-clone-write-guard.py" in h.get("command", "")
               for h in entry.get("hooks", []))
    ]
    assert carrying, settings["hooks"]["PreToolUse"]
    # 🔴 THE MATCHER IS THE HALF THAT WAS UNASSERTED, and it is the half that
    # decides whether the guard ever sees a Bash call. `matcher` appeared ZERO
    # times in this file, so changing it to any other tool left the suite green
    # while the hook went inert — the "reads as coverage while providing none"
    # failure this module's own docstring cites.
    for entry in carrying:
        assert entry.get("matcher") == "Bash", entry
        for h in entry.get("hooks", []):
            if "base-clone-write-guard.py" in h.get("command", ""):
                assert h.get("type") == "command", h


def test_the_wiring_names_no_absolute_interpreter_path():
    """An absolute `/nix/store` interpreter exists on one machine and nowhere else.

    The file is tracked in a public repo, so a store path here would make the hook
    fail to start for every other clone — and a PreToolUse hook that fails to
    start is silently an ALLOW, so nobody would notice.

    🔴 THIS ASSERTION READS THE PARSED `command` STRINGS, NEVER THE RAW FILE, AND
    THE FIRST VERSION GOT THAT WRONG IN A WAY WORTH KEEPING. It grepped the whole
    text and went red on the settings file's OWN `$comment`, which explains why a
    store path must not be used — the "an example that IS the thing it forbids is
    the thing it forbids" shape this repo has already recorded twice, here landing
    on a test rather than on prose. Asserting the STRUCTURE (the field that is
    executed) instead of a substring of the file is the fix, and it is also the
    stronger claim.
    """
    settings = json.loads((ROOT / ".claude" / "settings.json").read_text(encoding="utf-8"))
    commands = [
        hook.get("command", "")
        for entry in settings["hooks"]["PreToolUse"]
        for hook in entry.get("hooks", [])
    ]
    assert commands, "no PreToolUse commands found — the parse is wrong, not the file"
    for command in commands:
        assert "/nix/store" not in command, command


# ------------------------------------------------- the write TARGET, not the cwd
#
# 🔴 ONE DEFECT, BOTH DIRECTIONS, AND THE SECOND HALF IS THE ONE THAT MATTERS
# MORE. Every condition keyed on the **cwd**, which is neither necessary nor
# sufficient for "this command writes to the shared base clone":
#
#   * NOT SUFFICIENT — a session whose shell sits in the clone was refused for
#     `git -C <a linked worktree> commit`, the spelling the guard's own refusal
#     message PRESCRIBES, measured ten times across one effort; and a cross-repo
#     commit it blocked outright had to be handed back to the operator.
#   * NOT NECESSARY — a `-C`, `--git-dir`, `--work-tree`, `GIT_DIR` or `cd`
#     pointing INTO the clone from anywhere else passed straight through. That is
#     the fail-OPEN half, and the whole reason this hook exists is that a commit
#     landing on the wrong branch of a shared clone is SILENT.
#
# Both halves close by asking the conditions about the directory the command
# WRITES to. The cases below are grouped by direction, and each says which ref it
# was watched RED at: `fbe81e0` is the guard before any of this, `0e1060f` is
# after the `-C`-only half landed. A case green at `0e1060f` is labelled an
# INVARIANT GUARD, because a green there evidences nothing about this change.
#
# ⚠ ONE GAP IS LEFT OPEN DELIBERATELY and has its own declared-gap test at the
# bottom of this file: a git write inside a NESTED shell (`bash -c '…'`).


def test_a_write_redirected_into_a_linked_worktree_is_allowed(parallel_clone):
    """🔴 THE REGRESSION TEST. `-C <linked worktree>` from the base clone writes
    to the worktree, so the base clone is untouched and there is nothing to refuse.

    Watched RED at `03f912e` (the guard ignored `-C` entirely and denied) and
    green at HEAD.
    """
    clone, wt = parallel_clone[:2]
    verdict = _run_hook(f"git -C {wt} commit -m 'work'", clone)
    assert _decision(verdict) is None, (
        "a commit redirected into a linked worktree was refused while naming the "
        "base clone — the premise of the refusal is false"
    )


def test_a_write_redirected_INTO_the_base_clone_is_still_refused(parallel_clone):
    """The other half of the pair, and what stops the fix being a silencing.

    An explicit `-C <the base clone>` is a real base-clone write and must stay
    refused — otherwise `-C` becomes a bypass for the whole guard.

    ⚠ INVARIANT GUARD, NOT REGRESSION COVERAGE: it was already GREEN at
    `03f912e`, because the pre-fix guard denied everything and so could not get
    this case wrong. It pins that condition 5 did not widen into a bypass; it
    does not evidence the fix.
    """
    clone = parallel_clone[0]
    verdict = _run_hook(f"git -C {clone} commit -m 'work'", clone)
    assert _decision(verdict) == "deny"


def test_a_RELATIVE_redirect_into_a_linked_worktree_is_allowed(parallel_clone):
    """`-C` takes a relative path too, and resolving it against the wrong base is
    how a fix like this passes its absolute-path test and fails in practice."""
    clone, wt = parallel_clone[:2]
    rel = os.path.relpath(wt, clone)
    verdict = _run_hook(f"git -C {rel} commit -m 'work'", clone)
    assert _decision(verdict) is None


def test_CUMULATIVE_redirects_resolve_like_git_does(parallel_clone):
    """`git -C a -C b` is `cd a; cd b` — each is relative to the previous.

    Asserted because taking only the LAST `-C` is the obvious shortcut and it is
    wrong whenever the last one is relative.
    """
    clone, wt = parallel_clone[:2]
    verdict = _run_hook(
        f"git -C {wt.parent} -C {wt.name} commit -m 'work'", clone
    )
    assert _decision(verdict) is None


def test_a_redirect_to_an_UNRESOLVABLE_path_stays_refused(parallel_clone):
    """🔴 FAILS CLOSED. A `-C` the guard cannot resolve must NOT buy an exemption,
    or a typo'd path becomes a bypass. The guard refuses when it cannot prove the
    write lands elsewhere.

    ⚠ INVARIANT GUARD, NOT REGRESSION COVERAGE — green at `03f912e` too, for the
    same reason as the pair above."""
    clone = parallel_clone[0]
    verdict = _run_hook(f"git -C {clone}/no-such-dir commit -m 'work'", clone)
    assert _decision(verdict) == "deny"


# ------------------------------- the UNDER-blocking half: a redirect INTO the clone
#
# 🔴 THESE ARE THE FAIL-OPEN CASES, AND EVERY ONE OF THEM WAS MEASURED ALLOWING AT
# `0e1060f`. The guard's own docstring listed four of them as "cannot see" for two
# rounds while deferring the repair to the operator's host-wide guard — which is
# not importable from a PUBLIC repo, so the deferral could never complete. Each
# refusal here is a write that genuinely lands in the shared clone.


def test_a_redirect_INTO_the_clone_from_a_LINKED_WORKTREE_is_refused(parallel_clone):
    """🔴 THE SPELLING THIS REPO'S OWN RECIPE USES, pointed the wrong way.

    An agent standing in its worktree that types the clone's path by mistake
    commits onto whatever branch the clone is on. Watched RED at `fbe81e0` AND at
    `0e1060f` (both ALLOWED: the cwd is a linked worktree, so every cwd-keyed
    condition was false), green here.
    """
    clone, wt = parallel_clone[:2]
    assert _decision(_run_hook(f"git -C {clone} commit -m 'work'", wt)) == "deny"


def test_a_redirect_INTO_the_clone_from_ANOTHER_REPOSITORY_is_refused(tmp_path):
    """The cwd narrowing and the target widening are not in tension, and this is
    the case that shows it.

    Condition 2 stops the guard POLICING another repo — a `git commit` whose
    target is that repo stays allowed. It must not also stop the guard seeing a
    command that reaches back INTO its own clone from there, which is a real
    base-clone write however far away the shell is standing.

    Watched RED at `fbe81e0` and `0e1060f`.
    """
    global _ACTIVE_HOOK
    clone = tmp_path / "clone"
    other = tmp_path / "other"
    local_hook = _init_clone(clone)
    _add_worktree(clone, tmp_path / "wt")
    _init_clone(other)
    _ACTIVE_HOOK = local_hook
    try:
        # NEGATIVE CONTROL, in the same run: the other repo's own write is allowed.
        assert _decision(_run_hook("git commit -m 'work'", other)) is None
        assert _decision(
            _run_hook(f"git -C {clone} commit -m 'work'", other)) == "deny"
    finally:
        _ACTIVE_HOOK = None


@pytest.mark.parametrize("flag", [
    "--git-dir={gitdir}",
    "--git-dir {gitdir}",
    "--work-tree={clone}",
    "--work-tree {clone}",
])
def test_the_gitdir_and_worktree_flags_INTO_the_clone_are_refused(parallel_clone, flag):
    """🔴 BOTH FLAGS, BOTH SEPARATORS, AND NEITHER IS HYPOTHETICAL.

    MEASURED against git 2.55.0 on a miniature clone, and this is why they are
    judged IN ADDITION to the caller's directory rather than instead of it:

      * `git --git-dir=<the clone>/.git add <file>` run FROM a linked worktree
        exits 0 and stages into the CLONE's index;
      * `git --work-tree=<a linked worktree> rev-parse --absolute-git-dir` run
        from the main worktree answers the CLONE's `.git` — the flag moves which
        files are read, not which index and HEAD are written.

    So each names one half of the operation while the other half still comes from
    the caller. Watched RED at `fbe81e0` and `0e1060f` (both ALLOWED).
    """
    clone, wt = parallel_clone[:2]
    command = f"git {flag.format(gitdir=clone / '.git', clone=clone)} add seed.txt"
    assert _decision(_run_hook(command, wt)) == "deny", command


def test_a_GIT_DIR_ASSIGNMENT_INTO_the_clone_is_refused(parallel_clone):
    """`GIT_DIR=<the clone>/.git git add …` carries no flag at all.

    The variable overrides git's directory discovery, so a bare `git` after it
    writes to the clone wherever the shell is standing. Watched RED at `fbe81e0`
    and `0e1060f`.
    """
    clone, wt = parallel_clone[:2]
    command = f"GIT_DIR={clone / '.git'} git add seed.txt"
    assert _decision(_run_hook(command, wt)) == "deny"


def test_a_GIT_DIR_in_the_ENVIRONMENT_INTO_the_clone_is_refused(parallel_clone):
    """The same variable, already exported, so it never appears in the command.

    Read from the hook's own environment for that reason. Watched RED at
    `fbe81e0` and `0e1060f`.
    """
    clone, wt = parallel_clone[:2]
    verdict = _run_hook("git add seed.txt", wt,
                        env_extra={"GIT_DIR": str(clone / ".git")})
    assert _decision(verdict) == "deny"


def test_a_cd_INTO_the_clone_is_refused(parallel_clone):
    """`cd <the clone> && git commit` — the second row of the docstring's table.

    ⚠ CLOSED IN THE UNDER-BLOCKING DIRECTION ONLY, and the asymmetry is the
    point: a `cd` target is judged IN ADDITION to the caller's directory, never
    instead of it, because deciding that a `cd` REPLACES the caller needs bash's
    positional model — `( … )` does not persist, `{ … }` does — and a wrong model
    there fails OPEN. Watched RED at `fbe81e0` and `0e1060f`.
    """
    clone, wt = parallel_clone[:2]
    assert _decision(_run_hook(f"cd {clone} && git commit -m x", wt)) == "deny"


def test_a_redirect_into_a_SUBDIRECTORY_of_the_clone_is_refused(parallel_clone):
    """A `-C` does not have to name the clone's root to write to the clone.

    git discovers the repository by walking up, so any directory inside the main
    worktree is the main worktree for this purpose — measured: `git -C
    <clone>/<subdir> rev-parse --absolute-git-dir` answers `<clone>/.git`. A
    guard that string-compared the target against the clone's root would miss
    every one of these. Watched RED at `fbe81e0` and `0e1060f`.
    """
    clone, wt = parallel_clone[:2]
    sub = clone / ".claude"
    assert sub.is_dir(), "the fixture installs the hook under .claude/hooks"
    assert _decision(_run_hook(f"git -C {sub} commit -m x", wt)) == "deny"


def test_a_SAFE_redirect_does_not_vouch_for_a_DANGEROUS_SIBLING(parallel_clone):
    """🔴 THE FAIL-OPEN THE `-C`-ONLY HALF INTRODUCED, MEASURED RATHER THAN FEARED.

    `git -C <a linked worktree> --git-dir=<the clone>/.git commit` carries one
    redirect that is safe and one that is not. At `0e1060f` the safe one ended the
    enquiry and the command was ALLOWED — a strictly NEW fail-open, since
    `fbe81e0` refused it (on the cwd, for the wrong reason). Which repository such
    a command lands in is genuinely ambiguous, so a resolvable `-C` must not vouch
    for a sibling that names the clone.
    """
    clone, wt = parallel_clone[:2]
    command = f"git -C {wt} --git-dir={clone / '.git'} commit -m x"
    assert _decision(_run_hook(command, clone)) == "deny"


def test_a_GIT_DIR_in_the_environment_cannot_STEER_THE_GUARDS_OWN_READS(parallel_clone):
    """🔴 A SECOND FAIL-OPEN, IN THE INSTRUMENT RATHER THAN THE POLICY, AND IT WAS
    PRESENT AT BOTH BASE REFS.

    The guard answers "what is this directory" by shelling out to `git`, and that
    child inherited the hook's environment — so a `GIT_DIR` already exported in
    the session answered for every directory it asked about. Pointed at a LINKED
    worktree's git dir, `rev-parse --absolute-git-dir` stops equalling
    `--git-common-dir` ANYWHERE, so the clone itself stops looking like a main
    worktree and a plain `git commit` IN THE CLONE is allowed.

    MEASURED, with the no-variable control DENYing in the same run: ALLOW at
    `fbe81e0`, ALLOW at `0e1060f`, deny here.

    🔴 THE VARIABLE HERE POINTS SOMEWHERE HARMLESS ON PURPOSE. If it named the
    clone, the refusal could come from `_ambient_targets` judging it as a target
    and the test would pass with the scrub deleted — green for the wrong reason.
    Naming a linked worktree makes the scrub the only thing that can produce it.
    """
    clone, wt = parallel_clone[:2]
    wt_gitdir = clone / ".git" / "worktrees" / wt.name
    assert wt_gitdir.is_dir(), f"fixture: no worktree git dir at {wt_gitdir}"
    # POSITIVE CONTROL, same fixture, same command, no variable.
    assert _decision(_run_hook("git commit -m x", clone)) == "deny"
    verdict = _run_hook("git commit -m x", clone,
                        env_extra={"GIT_DIR": str(wt_gitdir)})
    assert _decision(verdict) == "deny", (
        "an exported GIT_DIR steered the guard's own `git` reads, so no directory "
        "looked like the main worktree and a real base-clone write was allowed"
    )


# ------------------------------- the OVER-blocking half: a redirect OUT of the clone


def test_a_redirect_to_ANOTHER_REPOSITORY_is_allowed(tmp_path):
    """The case that was handed back to the operator: a cross-repo worktree commit.

    A session rooted in cairn that runs `git -C <another repo's worktree> commit`
    touches this clone not at all. Watched RED at `fbe81e0`; green at `0e1060f`,
    so relative to the ref this change is built on it is an ⚠ INVARIANT GUARD —
    it pins that generalising the resolution to four spellings did not lose the
    one spelling that already worked.
    """
    global _ACTIVE_HOOK
    clone = tmp_path / "clone"
    other = tmp_path / "other"
    local_hook = _init_clone(clone)
    _add_worktree(clone, tmp_path / "wt")
    _init_clone(other)
    _add_worktree(other, tmp_path / "other-wt")
    _ACTIVE_HOOK = local_hook
    try:
        # POSITIVE CONTROL: the same fixture refuses a real base-clone write, so a
        # pair of allows below cannot be a hook that is simply wired to nothing.
        assert _decision(_run_hook("git commit -m x", clone)) == "deny"
        for target in (other, tmp_path / "other-wt"):
            assert _decision(
                _run_hook(f"git -C {target} commit -m x", clone)) is None, target
    finally:
        _ACTIVE_HOOK = None


def test_a_gitdir_redirect_to_a_LINKED_WORKTREE_is_allowed(parallel_clone):
    """🔴 THE FALSE POSITIVE THE `-C`-ONLY HALF LEFT STANDING, and the trap the
    whole resolution has to avoid.

    `--git-dir=<a linked worktree's git dir>` writes to that worktree's index and
    HEAD, not the clone's. The trap: a linked worktree and the main worktree of
    one clone share the git COMMON dir, so a check that resolved the target to the
    common dir would call this the base clone and re-create the very false
    positive being removed. `--absolute-git-dir` is what separates them.

    Watched RED at `0e1060f` (deny) and green here.

    🔴 AND THE VERDICT IS cwd-CONDITIONAL, WHICH AN EARLIER DOC ROW AND AN EARLIER
    LABEL IN THE ACCEPTANCE SCRIPT BOTH OMITTED — one read as unconditional, the
    other said "cwd = the base clone" while passing the worktree. `--git-dir` is
    ADDITIVE, so the caller's directory is judged as well: from the worktree this
    is allowed, from the clone it is REFUSED. Both are asserted here so the pair
    cannot drift apart again.
    """
    clone, wt = parallel_clone[:2]
    wt_gitdir = clone / ".git" / "worktrees" / wt.name
    command = f"git --git-dir={wt_gitdir} commit -m x"
    assert _decision(_run_hook(command, wt)) is None, command
    assert _decision(_run_hook(command, clone)) == "deny", command


def test_a_redirect_at_a_path_that_is_NO_REPOSITORY_stays_refused(parallel_clone):
    """🔴 FAILS CLOSED, and the host-wide guard records this exact shape as a
    measured fail-open: naming a repo handed it the whole verdict, so a `-C` at an
    ordinary directory left the branch check evaluating NOTHING and the command
    ran unchecked.

    ⚠ INVARIANT GUARD — green at `0e1060f` too.
    """
    clone = parallel_clone[0]
    outside = clone.parent / "plain"
    outside.mkdir()
    assert _decision(_run_hook(f"git -C {outside} commit -m x", clone)) == "deny"


def test_an_UNEXPANDED_VARIABLE_in_a_redirect_stays_refused(parallel_clone):
    """`git -C $WT commit` — the guard runs before the shell expands anything.

    A target it cannot resolve must leave the refusal standing; the alternative is
    that any unresolvable string disarms the guard. ⚠ INVARIANT GUARD — green at
    `0e1060f`.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook("git -C $WT commit -m x", clone)) == "deny"


# ------------------- a target named by a VARIABLE: REFUSED, and that is the ruling
#
# 🔴 THIS SECTION REPLACES ONE THAT RESOLVED `$VAR` FROM THE COMMAND TEXT, AND THE
# RETRACTION IS THE MOST USEFUL THING IN IT. The requirement came from a report
# that `git -C "$WT" add` was wrongly refused for a cross-repo worktree. Measured
# against the real paths with a control: the LITERAL-path spelling was already
# allowed, and the repro had assigned `WT` in a PREVIOUS Bash call, which no parser
# in this process could ever resolve. There was no defect.
#
# The resolver written for that non-defect opened FOUR fail-opens. In each of these
# bash leaves `WT` UNSET, so the command git runs is `git -C "" commit` — which git
# runs in the CURRENT directory, the clone:
#
#     ( WT=<wt> ) ; git -C "$WT" commit       a subshell assignment is discarded
#     false && WT=<wt> ; git -C "$WT" commit   the assignment never runs
#     if false; then WT=<wt>; fi; git -C …     nor does one in an untaken branch
#     WT=<wt> true; git -C "$WT" commit        a command PREFIX scopes to `true`
#
# All four were ALLOW with the resolver and DENY without it; one was proved end to
# end, the clone going 1 -> 2 commits while the worktree stayed at 1. Knowing a
# name is ASSIGNED SOMEWHERE IN THE TEXT is not knowing the shell will have
# ASSIGNED IT.
#
# 🔴 AND NOTE HOW THE FALLBACK DIED. The brief said not to relax it, and it was not
# relaxed — the resolver made it UNREACHABLE in those four shapes instead. A rule
# honoured formally while its purpose is defeated is the harder failure to see,
# which is why these cases are pinned by SHAPE rather than left to the one-line
# claim that unresolvable targets refuse.


@pytest.mark.parametrize("command,why", [
    ('( WT={wt} ) ; git -C "$WT" commit -m x',
     "a subshell assignment is discarded, so $WT is unset"),
    ('false && WT={wt} ; git -C "$WT" commit -m x',
     "the assignment never runs, so $WT is unset"),
    ('if false; then WT={wt}; fi; git -C "$WT" commit -m x',
     "an untaken branch assigns nothing"),
    ('WT={wt} true; git -C "$WT" commit -m x',
     "a command prefix scopes the assignment to `true`"),
    ('WT={wt}\ngit -C "$WT" commit -m x',
     "even the shape that WOULD have resolved: no expansion happens at all now"),
    ('export WT={wt}; git -C "$WT" commit -m x',
     "`export` is not modelled either, and now does not need to be"),
    ('git -C "$WT" commit -m x',
     "the value is nowhere in the text"),
    ('git -C "${{WT}}" commit -m x',
     "nor in the braced spelling"),
])
def test_a_target_named_by_a_VARIABLE_is_always_REFUSED(parallel_clone, command, why):
    """🔴 THE FOUR FAIL-OPEN SHAPES, PLUS THE ONES THAT MERELY LOOK RESOLVABLE.

    Every row is a REAL base-clone write: `git -C ""` runs in the current
    directory. The first four were measured **ALLOW** at the resolver head and
    **DENY** at `0e1060f`, and are green here — so they are regression coverage
    against a fail-open this branch itself introduced and removed.

    Rows five and six are the ⚠ ERGONOMIC COST, stated rather than hidden: the
    spelling a careful script uses is refused too. The remedy is an ABSOLUTE path,
    which the refusal message names, and it is a cheaper remedy than modelling
    shell scope in Python inside a security path.

    Rows seven and eight were green at both refs — ⚠ INVARIANT GUARDS.
    """
    clone, wt = parallel_clone[:2]
    text = command.format(wt=wt)
    assert _decision(_run_hook(text, clone)) == "deny", f"{why}: {text}"


def test_an_ASSIGNMENT_token_naming_the_clone_is_not_a_redirect_TARGET(parallel_clone):
    """🔴 A DRAFT OF THIS TEST ASSERTED `deny` AND WAS WRONG ABOUT THE SHELL, which
    is worth keeping because it is the same error the deleted resolver was built on.

    `WT=<the clone>` then `git -C "$WT" commit`, run from the LINKED WORKTREE. The
    draft reasoned "the clone is named in the text, so it must be judged". But
    `$WT` is unexpanded, so the command git runs is `git -C "" commit` — which runs
    in the CURRENT directory, the worktree. No base-clone write happens, and
    `allow` is the correct answer. Measured: allow at `fbe81e0`, at `0e1060f` and
    here.

    So what this pins is that a leading `VAR=` token is NOT mistaken for a
    redirect target. It would be a false positive to treat it as one, and the
    mirror case — the same command from the CLONE — is refused by the fallback and
    covered in the parametrised set above.

    ⚠ INVARIANT GUARD: green at both refs. It exists because the wrong answer here
    is the attractive one.
    """
    clone, wt = parallel_clone[:2]
    verdict = _run_hook(f'WT={clone}\ngit -C "$WT" commit -m x', wt)
    assert _decision(verdict) is None


def _existing_dirs(root, count: int) -> list[Path]:
    """`count` ORDINARY directories — not worktrees, not repositories.

    Plain directories are the point: the probe cap these pin the absence of
    filtered only paths that do NOT exist, so a handful of real directories was
    enough to spend it. That is what made an ordinary cross-repo sweep a bypass
    rather than an exotic payload.
    """
    out = []
    for n in range(count):
        path = root / f"pad{n}"
        path.mkdir()
        out.append(path)
    return out


@pytest.mark.parametrize("count", [8, 12, 30])
@pytest.mark.parametrize("shape", ["git-dir", "cd"])
def test_no_number_of_ADDITIVE_candidates_can_crowd_OUT_the_real_one(
        parallel_clone, shape, count):
    """🔴 THE HAZARD THAT HAD NO TEST, AND IT REPLACES TWO THAT LOOKED LIKE IT DID.

    The guard once capped how many directories it would ask `git` about. The cap
    was measured to convert a DENY into an ALLOW at exactly the bound, TWICE, one
    level apart: first globally (primaries crowding out primaries), then — after
    the fix — in the additive pass alone, because `cd` and `GIT_DIR` targets are
    whole-command scans appended to every segment's extras.

    Both shapes here were ALLOW at `c22e7a0` for N >= 8 and DENY for N <= 7, and
    the `cd` one was driven END TO END: the clone went 1 -> 2 commits with `HEAD`
    subject `PROOF` while the worktree stayed at 1. **That shape is an ordinary
    cross-repo sweep.** Green here at N = 8, 12 and 30, so a cap merely RAISED
    rather than removed cannot satisfy this.

    ⚠ IT REPLACES `test_junk_candidates…probe_budget` AND
    `test_junk_ADDITIVE_candidates…`, WHICH BOTH WENT VACUOUS WITH THE CAP. One
    padded with primaries, which were already uncapped; the other tested a cap
    that no longer exists. Deleting them without this would have left the file
    looking like it covered the area it had just stopped covering.
    """
    clone, wt = parallel_clone[:2]
    pads = _existing_dirs(clone.parent, count)
    if shape == "git-dir":
        pad = " ".join(f"--git-dir={d}" for d in pads)
        command = f"git {pad} --git-dir={clone / '.git'} add seed.txt"
    else:
        pad = " ".join(f"cd {d} && git fetch ;" for d in pads)
        command = f"{pad} cd {clone} && git commit -m PROOF"
    assert _decision(_run_hook(command, wt)) == "deny", f"{shape} N={count}"


@pytest.mark.parametrize("command", [
    "git -C a\0b commit -m x",
    "git --git-dir=a\0b commit -m x",
    "cd a\0b && git commit -m x",
    "GIT_DIR=a\0b git commit -m x",
    "GIT_INDEX_FILE=a\0b git add seed.txt",
])
def test_a_NUL_BYTE_in_a_redirect_does_not_CRASH_the_hook(parallel_clone, command):
    """🔴 A CRASH IS AN ALLOW, AND THIS ONE WAS REACHED THROUGH THE GUARD'S OWN
    PLUMBING RATHER THAN ITS POLICY.

    `subprocess.run(cwd=…)` raises `ValueError: embedded null byte` — not an
    `OSError` — so a NUL in a `-C` value escaped `_git`'s handler and killed the
    hook with a traceback and rc 1. Every status except 2 lets the command RUN, so
    the traceback was an ALLOW on a payload `0e1060f` DENIED.

    Measured: DENY at `0e1060f`, **rc 1** at `c22e7a0`, deny here. Reach is narrow
    — bash cannot carry a NUL in argv — but fail-open-on-crash is the property
    this file's own header forbids itself, and `_run_hook` asserts rc 0 on every
    call for exactly this reason.

    ⚠ IT ALSO FALSIFIED A CLAIM THE SAME BRANCH SHIPPED: `_abs_path` said a second
    existence check "could never change a verdict", and removing the duplicate
    turned the `-C` row's DENY into a crash-ALLOW. The mutation sweep scored that
    guard unreachable because it scored VERDICTS, and a crash is not a verdict — a
    blind spot of the instrument.

    🔴 THE NUL HALF OF THAT IS NOW RE-MEASURED AND CLOSED, AND THE CLAIM IS STILL
    FALSE FOR A DIFFERENT REASON — which is why the rows below are parametrised
    rather than left at one. All five spellings answer **deny at rc 0** both with
    and without the duplicate check, so NUL no longer separates them; what does is
    `GIT_INDEX_FILE=<the clone>/.git/index`, which `isdir` rejects because it names
    a FILE. The measurement is beside `_abs_path`. ⚠ The four rows added here were
    green at `ffa0eca` — INVARIANT GUARDS: they pin that the walk and ledger changes
    did not reintroduce a crash on a dimension `_run_hook`'s rc assertion is the
    only witness to.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook(command, clone)) == "deny", repr(command)


def test_GIT_INDEX_FILE_pointed_at_the_clones_INDEX_is_refused(parallel_clone):
    """🔴 CONDITION 1 SAYS "MUTATES THE INDEX", AND THIS MUTATED THE CLONE'S INDEX
    WHILE BEING ALLOWED.

    `GIT_INDEX_FILE=<the clone>/.git/index git add <file>` from a linked worktree
    was ALLOW at `0e1060f` and at `c22e7a0`, and was measured to REWRITE the
    clone's index with a file staged. Nothing judged the variable: the judged-env
    list was `GIT_DIR`/`GIT_WORK_TREE` while the SCRUB list beside it already named
    nine variables, and a comment calling the first "the two environment
    variables" read as though those were the same question.

    🔴 IT IS JUDGED BY ITS CONTAINING DIRECTORY, because the value is a FILE.
    `<clone>/.git/index` -> `<clone>/.git`, which `_protected` already recognises
    as the main worktree. That keeps `_protected` answering one question about one
    directory instead of learning about files.

    ⚠ AND THE SET STOPS AT WHAT LANDS: `GIT_COMMON_DIR`, `-c core.worktree=`,
    `GIT_CONFIG_KEY_*` and `--config-env` were each driven end to end and measured
    NOT to mutate the clone, so they stay unjudged. Widening past what was shown
    to land trades a false negative for a false positive with no measurement on
    either side.
    """
    clone, wt = parallel_clone[:2]
    command = f"GIT_INDEX_FILE={clone / '.git' / 'index'} git add seed.txt"
    assert _decision(_run_hook(command, wt)) == "deny"
    # The same variable in the ENVIRONMENT rather than the command text.
    verdict = _run_hook("git add seed.txt", wt,
                        env_extra={"GIT_INDEX_FILE": str(clone / ".git" / "index")})
    assert _decision(verdict) == "deny"


def test_a_cd_does_not_override_a_RESOLVED_redirect(parallel_clone):
    """🔴 THE FALSE POSITIVE THE `cd` SCAN WOULD HAVE INVENTED.

    `cd <the clone> && git -C <a linked worktree> commit` writes to the worktree:
    a `-C` moves git's whole repository discovery, so the shell's directory — and
    therefore the `cd` — is not consulted at all. Judging the `cd` target for a
    segment that resolved its own `-C` would refuse this, which is a new false
    positive created by the fix for a false negative.

    That is why `_ambient_targets` returns the `cd` and `GIT_DIR` families
    SEPARATELY: `GIT_DIR` overrides a `-C` (measured) and stays judged for every
    segment, while a `cd` loses to one.

    ⚠ INVARIANT GUARD relative to the refs — green at `0e1060f`, which read no
    `cd` at all. It pins the precedence this change had to get right.
    """
    clone, wt = parallel_clone[:2]
    verdict = _run_hook(f"cd {clone} && git -C {wt} commit -m x", wt)
    assert _decision(verdict) is None


# ------------------------------------------------------------ the DECLARED gap


def test_a_git_write_inside_a_NESTED_SHELL_is_still_unseen(parallel_clone):
    """⚠ A DECLARED GAP, PINNED SO THE DOCSTRING CANNOT GO STALE — not a
    requirement that it stay open.

    `bash -c 'cd <the clone> && git commit …'` is one quoted token to this
    parser, so nothing inside it is read as a command. The module docstring's
    table names this as the one row still open, and the operator's host-wide
    guard spends two separate recursion budgets to close the equivalent — so it is
    not cheap, and it is covered on the hosts that run that guard.

    🔴 IF THIS GOES RED, THE GUARD IMPROVED. Update the docstring's table to mark
    the row CLOSED and replace this test with a refusal case; do NOT re-open the
    gap to make it green again.
    """
    clone, wt = parallel_clone[:2]
    command = f"bash -c 'cd {clone} && git commit -m x'"
    assert _decision(_run_hook(command, wt)) is None, (
        "the nested-shell row is no longer a gap — see this test's docstring"
    )
