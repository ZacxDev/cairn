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
    of the real deployment (`<repo>/.claude/hooks/<file>`), which is also why
    `shutil.copy` and not a symlink: `os.path.realpath` on a symlink resolves back
    to the cairn checkout and the guard would police the wrong tree.
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
