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
    return frozenset(re.findall(r'"([a-z-]+)"', block.group(1)))


def _refused_from_doc() -> frozenset[str]:
    """The subcommands the doc's refused-set TABLE names, in backticks."""
    text = DOC.read_text(encoding="utf-8")
    start = text.index("| refused | why it collides |")
    end = text.index("\n\n", start)
    rows = text[start:end].splitlines()[2:]  # skip header + separator
    names: set[str] = set()
    for row in rows:
        first_column = row.split("|")[1]
        names.update(re.findall(r"`([a-z-]+)`", first_column))
    return frozenset(names)


def _init_clone(path: Path) -> None:
    path.mkdir(parents=True, exist_ok=True)
    env = {**os.environ, **_GIT_ENV}
    subprocess.run(["git", "init", "-q", "-b", "main"], cwd=path, check=True, env=env)
    (path / "seed.txt").write_text("seed\n", encoding="utf-8")
    subprocess.run(["git", "add", "seed.txt"], cwd=path, check=True, env=env)
    subprocess.run(["git", "commit", "-qm", "seed"], cwd=path, check=True, env=env)


def _add_worktree(clone: Path, wt: Path, branch: str = "side") -> None:
    subprocess.run(
        ["git", "worktree", "add", "-q", "-b", branch, str(wt)],
        cwd=clone, check=True, env={**os.environ, **_GIT_ENV},
    )


def _run_hook(command: str, cwd: Path | str, env_extra: dict | None = None) -> dict:
    """Invoke the hook exactly the way Claude Code does, and parse its verdict."""
    payload = {"tool_name": "Bash", "tool_input": {"command": command}, "cwd": str(cwd)}
    env = {k: v for k, v in os.environ.items() if k != "BASE_CLONE_WRITE_OK"}
    env.update(_GIT_ENV)
    env.update(env_extra or {})
    proc = subprocess.run(
        [sys.executable, str(HOOK)],
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
def parallel_clone(tmp_path: Path) -> tuple[Path, Path]:
    """A base clone with ONE linked worktree — the state the guard fires in."""
    clone = tmp_path / "clone"
    wt = tmp_path / "wt"
    _init_clone(clone)
    _add_worktree(clone, wt)
    return clone, wt


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
    clone, _ = parallel_clone
    verdict = _run_hook("git commit -m 'work'", clone)
    assert _decision(verdict) == "deny"


def test_the_refusal_names_the_doc_and_the_override(parallel_clone):
    """The refusal message is the ONLY routing this feature has.

    `AGENTS.md` is at its byte ceiling, so no per-session prose points at the
    rules; if this message stops naming the doc, the doc becomes unreachable. The
    override is asserted for the same reason — a guard with no documented way past
    it is one somebody disables.
    """
    clone, _ = parallel_clone
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
    clone, _ = parallel_clone
    assert _decision(_run_hook(command, clone)) == "deny", command


def test_a_commit_later_in_a_chain_is_still_seen(parallel_clone):
    """Reading only the first word would miss `… && git commit …`.

    That is the shape a session actually types, so a guard that only inspected the
    leading command would be inert in practice while passing a test suite built
    from single commands.
    """
    clone, _ = parallel_clone
    verdict = _run_hook("git fetch origin && git commit -m 'work'", clone)
    assert _decision(verdict) == "deny"


def test_the_global_option_hop_does_not_hide_the_subcommand(parallel_clone):
    """`git -C <path> commit` must not read `<path>` as the subcommand.

    The operator's host-wide guard records this exact hop being added as a
    separate check after it was found to bypass an earlier one, so it is pinned
    here rather than assumed.
    """
    clone, _ = parallel_clone
    verdict = _run_hook(f"git -C {clone} commit -m 'work'", clone)
    assert _decision(verdict) == "deny"


# ------------------------------------- NEGATIVE: it must not fire where it would hurt

def test_the_same_commit_inside_the_linked_worktree_is_allowed(parallel_clone):
    """The discriminator, and the whole point of the feature.

    Same command, same clone, same repo — only the cwd differs. If this refuses,
    the guard has banned the very workflow it is telling people to use.
    """
    _, wt = parallel_clone
    assert _decision(_run_hook("git commit -m 'work'", wt)) is None


def test_a_clone_with_no_linked_worktrees_is_left_alone(tmp_path):
    """An outside contributor's fresh clone must never see this hook fire.

    The file is tracked in a PUBLIC repository, so the common case for a stranger
    is exactly this state: one worktree, nobody working in parallel, nothing to
    collide with.
    """
    clone = tmp_path / "solo"
    _init_clone(clone)
    assert _decision(_run_hook("git commit -m 'work'", clone)) is None


def test_the_ff_only_resync_recipe_is_allowed(parallel_clone):
    """🔴 The documented base-clone re-sync, and the trap this test exists for.

    `git -C <repo> fetch origin && git merge --ff-only origin/main` is how a
    write-only base clone is kept current. It cannot conflict or autostash: it
    fast-forwards or it REFUSES, and the refusal is the operator's signal that the
    clone diverged. A guard that blocked `merge` wholesale would break the one
    command the standing rules tell everybody to run — and it would look correct,
    because `merge` genuinely does mutate the tree.
    """
    clone, _ = parallel_clone
    verdict = _run_hook("git fetch origin && git merge --ff-only origin/main", clone)
    assert _decision(verdict) is None


def test_a_pathspec_checkout_is_allowed(parallel_clone):
    """`git checkout <ref> -- <paths>` takes a file; it does not move HEAD.

    It is also the recipe for reading a doc at a ref, which the staleness section
    of the doc tells people to do, so refusing it would contradict the same file.
    """
    clone, _ = parallel_clone
    verdict = _run_hook("git checkout origin/main -- AGENTS.md", clone)
    assert _decision(verdict) is None


def test_a_bare_checkout_is_still_refused_when_chained_with_a_pathspec_one(parallel_clone):
    """The pathspec exemption must be per-command, not per-command-LINE.

    Otherwise a chain containing one `--` smuggles a HEAD-moving
    `git checkout <branch>` past the guard on a sibling's authority — the
    "satisfied by a neighbour" shape this repo has already measured once, in a
    guard that searched a whole block for a field.
    """
    clone, _ = parallel_clone
    verdict = _run_hook(
        "git checkout origin/main -- AGENTS.md && git checkout other-branch", clone)
    assert _decision(verdict) == "deny"


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
    clone, _ = parallel_clone
    assert _decision(_run_hook(command, clone)) is None, command


def test_a_word_that_merely_mentions_a_subcommand_is_not_a_command(parallel_clone):
    """`echo "do not commit here"` is prose, not a commit."""
    clone, _ = parallel_clone
    verdict = _run_hook('echo "remember: never commit in the base clone"', clone)
    assert _decision(verdict) is None


# ------------------------------------------------------------------ the override

def test_the_inline_override_is_honoured(parallel_clone):
    clone, _ = parallel_clone
    verdict = _run_hook("BASE_CLONE_WRITE_OK=1 git commit -m 'deliberate'", clone)
    assert _decision(verdict) is None


def test_the_environment_override_is_honoured(parallel_clone):
    clone, _ = parallel_clone
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
    commands = [
        hook.get("command", "")
        for entry in settings["hooks"]["PreToolUse"]
        for hook in entry.get("hooks", [])
    ]
    assert any("base-clone-write-guard.py" in c for c in commands), commands


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
