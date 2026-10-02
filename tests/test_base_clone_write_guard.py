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

import ast
import itertools
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


def _hook_namespace() -> dict:
    """The hook's module-level definitions, loaded WITHOUT running `main()`.

    🔴 THIS EXISTS SO STRUCTURAL CLAIMS CAN BE **DERIVED** FROM THE CODE INSTEAD OF
    RESTATED BESIDE IT. Four audit rounds in a row found the same defect — a count or
    an enumeration written in prose next to a thing that changes — and three of those
    were in paragraphs added to correct the previous one. `claude/RULES.md` says to
    prefer a deterministic fix over a prose one; a count nobody can re-derive is the
    prose one.

    The hook is a SCRIPT: its last statement is `try: main()`, which consumes stdin and
    exits, so a plain import is impossible — which is why the other readers in this
    file parse the source. Cutting the source at that invocation and `exec`ing the rest
    gives the real functions and tables.
    """
    source = HOOK.read_text(encoding="utf-8")
    marker = "\ntry:\n    main()\n"
    assert marker in source, (
        "the hook no longer ends with the `try: main()` invocation this cut relies on"
    )
    namespace: dict = {"__name__": "base_clone_write_guard_under_test"}
    exec(compile(source[: source.index(marker)], str(HOOK), "exec"), namespace)
    # POSITIVE CONTROL: a namespace missing these would make every derived check below
    # vacuous, which is the failure mode of loading code by cutting a string.
    for symbol in ("_resolve_long", "_option_state", "_OPTION_GRAMMAR", "_REFUSED",
                   "_DRY_RUN_SUBCOMMANDS"):
        assert symbol in namespace, f"the hook's `{symbol}` did not load"
    return namespace


def _git_long_options(subcommand: str) -> dict[str, bool]:
    """`{canonical long option: does it take a REQUIRED separate value}`, from git itself.

    `--[no-]dry-run` yields `dry-run: False`; `--exclude <pattern>` yields `True`. An
    OPTIONAL argument (`--log[=<n>]`, `--gpg-sign[=<key-id>]`) yields False, because git
    only accepts those attached and treating them as value-taking would swallow the next
    word. Read from the INSTALLED git, so a claim about option arity is measured on the
    git that is running rather than asserted for the one that was.
    """
    done = subprocess.run(["git", subcommand, "-h"], capture_output=True, text=True,
                          env={**os.environ, **_GIT_ENV})
    text = done.stdout + done.stderr
    assert "usage:" in text, f"`git {subcommand} -h` printed no usage: {text[:200]}"
    options: dict[str, bool] = {}
    # `--[no-]name <arg>` / `--name <arg>` / `--name[=<arg>]` / `--name=<arg>` / `--name`
    #
    # ⚠ THE ALTERNATION ORDER WAS WRONG IN THE FIRST DRAFT AND THE POSITIVE CONTROL
    # CAUGHT IT: the attached branch could match EMPTY, so it always won and every option
    # read as taking no value — `git clean --exclude` came back False. A reader whose
    # every answer is "no value" makes the whole comparison below vacuous.
    for name, arg in re.findall(
            r"--(?:\[no-\])?([a-z][a-z0-9-]*)(\[?=<[^>]+>\]?| <[^>]+>)?", text):
        # A REQUIRED separate value is the space-then-`<…>` form. An attached or
        # optional one (`[=<n>]`, `=<n>`) consumes no following word, so it is False
        # here on purpose — treating it as value-taking would swallow the next word.
        takes = bool(arg) and arg.lstrip().startswith("<")
        options[name] = options.get(name, False) or takes
    return options


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


#: 🔴 THE DIFFERENTIAL'S OPTION POOL. Every word is a spelling git 2.55.0 ACCEPTS for
#: that subcommand; the harness builds commands from them and compares the guard's
#: verdict against what git really did. Kept small on purpose — the combinatorics are
#: cubic at the depth below — and the wider out-of-band sweep is in the commit message.
_DIFFERENTIAL_POOL: dict[str, list[str]] = {
    "clean": ["-n", "-f", "-d", "--no-dry-run", "--dry", "--exclude=",
              "--exclude=junk.txt", "-e", "-ejunk.txt", "--"],
    "rm": ["-n", "-f", "--no-dry-run", "--dry", "--cached", "--ignore-unmatch",
           "--no-pathspec-from-file", "--pathspec-from-file=", "--"],
    "mv": ["-n", "-f", "-k", "--no-dry-run", "--dry", "--verbose", "--"],
}

#: What the subcommand prints when GIT itself considered the run a DRY one. Read from
#: the real command's output rather than inferred from the flags, so the oracle is git
#: and not a second copy of the model under test.
_DRY_RUN_MARKER = {"clean": "Would remove", "rm": "rm '", "mv": "Checking rename"}

#: Operands appended after the option words, so every generated command has something
#: real to act on — otherwise "nothing changed" would not mean "it was a dry run".
_DIFFERENTIAL_OPERANDS = {"clean": [], "rm": ["seed.txt"],
                          "mv": ["seed.txt", "moved.txt"]}

#: 🔴 HOW MANY OPTION WORDS THE DIFFERENTIAL COMBINES, AND IT IS A FLOOR RATHER THAN A
#: SETTING. At depth 2 the sweep catches the FALSE-POSITIVE half of the defect it was
#: built for and **structurally cannot see the FAIL-OPEN half**: the swallowed word has
#: to sit BETWEEN a `-n` and a `--no-dry-run`, which needs three. Measured on the
#: reverted conditional — depth 2: 6 divergences, **zero fail-opens**; depth 3: 58, with
#: the fail-opens present. So an edit trimming the depth for runtime would leave the
#: whole class unobserved behind a green suite, and because this is deliberately ONE
#: collected test neither the CI floor nor `MAX_GAP` could see it.
#:
#: ⚠ THE DEPTH IS ONE OF **TWO** AXES AND THE FIRST VERSION OF THESE FLOORS GUARDED ONLY
#: THIS ONE. A derived case count cannot see a trimmed POOL, because the derivation reads
#: the same pools the generator does and both shrink together — measured, cutting
#: `clean`'s pool in half took 1664 commands to 929 and PASSED, and deleting that pool
#: outright, a whole subcommand, took it to 844 with the runtime halved and PASSED. The
#: pool axis is pinned separately, by `_DIFFERENTIAL_POOL_FLOOR` and by the
#: subcommand-set assertion beside it.
_DIFFERENTIAL_DEPTH = 3

#: Per-subcommand minimum pool size, pinned INDEPENDENTLY of the generator because the
#: derived case count structurally cannot see a trim. Set a couple of words below what
#: each pool carries, so ADDING words needs no edit here and REMOVING any is a decision.
_DIFFERENTIAL_POOL_FLOOR = {"clean": 8, "rm": 7, "mv": 5}

#: The hook's own dry-run policy set, read from its SOURCE rather than retyped, so the
#: differential cannot drift to sweep a different three subcommands than the exemption
#: actually applies to.
_DRY_RUN_SUBCOMMANDS_FOR_TESTS = frozenset(
    re.findall(r'"([a-z-]+)"',
               re.search(r"_DRY_RUN_SUBCOMMANDS = frozenset\(\{(.*?)\}\)",
                         HOOK.read_text(encoding="utf-8"), re.S).group(1))
)


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
    """The subcommands `_DRY_RUN_SUBCOMMANDS` exempts a dry run for.

    ⚠ IT READS THE POLICY SET, NOT THE GRAMMAR. `_OPTION_GRAMMAR` also has a
    `symbolic-ref` entry — it models that subcommand's options for the `--delete`
    check — and reading keys from there would silently claim `symbolic-ref` has an
    exempt dry run. Two sets, two questions.
    """
    text = HOOK.read_text(encoding="utf-8")
    block = re.search(r"_DRY_RUN_SUBCOMMANDS = frozenset\(\{(.*?)\}\)", text, re.S)
    assert block, "could not find the _DRY_RUN_SUBCOMMANDS literal"
    return frozenset(re.findall(r'"([a-z-]+)"', block.group(1)))


def test_the_dry_run_ledger_is_parseable():
    """POSITIVE CONTROL for the parse the test below rests on."""
    keys = _dry_run_keys_from_hook()
    assert "clean" in keys, f"the parse found no `clean` — it matches nothing: {keys}"


def test_the_DRY_RUN_exemption_covers_EXACTLY_THREE_SUBCOMMANDS_AND_NO_MORE():
    """🔴 AN EXEMPTION LEDGER THAT CAN GROW SILENTLY IS THE HAZARD HERE, not a
    missing row — so this fails on GROW as well as on shrink.

    🔴 TWO CATEGORIES SIT OUTSIDE THIS SET AND AN EARLIER VERSION OF THIS DOCSTRING
    LUMPED THEM UNDER ONE HEADING, WHICH IS A CATEGORY ERROR RATHER THAN A STALE
    MEASUREMENT. `add -n` and `apply --check` were measured to change nothing: they
    ARE reads, left out by an operator decision about scope creep, and that decision is
    revisitable. `commit --dry-run` is not in the same category at all — it WRITES a
    tree object — so it must never be exempted, and a sentence that filed it beside the
    other two is exactly what a future widening would have cited.
    And 🔴 TWO SPELLINGS THAT LOOK LIKE DRY RUNS ARE NOT — `git merge
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


def test_the_shared_state_WRITERS_LEFT_OUT_carry_a_RECORDED_DECISION():
    """🔴 THIS PINS THE DECISIONS TAKEN, **NOT** THAT NO OTHER WRITER EXISTS — AND
    THE EARLIER VERSION OF IT PINNED THE SECOND THING, WHICH WAS FALSE.

    It asserted the out-set was exactly `{worktree remove, branch -D}` while the hook
    comment said "an audit found five writers" and the doc's table listed two. A
    round-1 audit then measured `git revert --no-edit HEAD` ALLOWED — writing the
    tree, the index AND HEAD, through the same sequencer as `cherry-pick`, which was
    already refused. So the trio of prose + table + pin was a **pinned false
    completeness**: this repo's own "reads as coverage while providing none", with a
    test holding it in place. `revert`, `update-index`, `read-tree` and
    `symbolic-ref` are all in `_REFUSED` now.

    What the three rows mean, each a decision with a reason in the doc:

      * `worktree remove` — the parallel-work recipe PRESCRIBES it from the base
        clone, and refusing a documented recipe is the guard's stated failure mode;
      * `branch -D` — a branch ref lives in the COMMON git dir, so the same delete is
        available identically from any worktree, which conditions 2 and 3 cannot
        scope;
      * `restore` — it DOES overwrite the working-tree file (measured: an unsaved
        edit replaced by committed content), but the ordinary form carries no `--`,
        so refusing it would refuse every `git restore <path>`.

    Fails on GROW (a decision added without its reason) and on SHRINK (a decision
    deleted), and fails if one is added to `_REFUSED` without its doc row moving —
    the mirror of `test_the_refused_set_matches_the_documented_table`.
    ⚠ It can never assert completeness: the complement of `_REFUSED` is not
    enumerable, and the doc now names the known tail instead of implying there is
    none.
    """
    assert _declared_out_from_doc() == {"worktree remove", "branch -D", "restore"}
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


@pytest.mark.parametrize("command,expected,why", [
    ("git merge --ff-only origin/main", None, "the canonical spelling, the control"),
    ("git merge --ff-onl origin/main", None, "an unambiguous PREFIX git accepts"),
    ("git merge --ff-on origin/main", None, "…shorter"),
    ("git merge --ff-o origin/main", None,
     "🔴 the shortest one git still resolves, and it was REFUSED — a false positive on "
     "the one base-clone resync recipe this repo prescribes. Measured: git reaches the "
     "same `not something we can merge` as the full form, i.e. past option parsing"),
    ("git merge --ff origin/main", "deny",
     "⚠ A DIFFERENT OPTION, NOT AN ABBREVIATION: `--ff` ALLOWS a fast-forward, it does "
     "not require one, so this is an ordinary merge and must stay refused"),
    ("git merge -m --ff-only origin/main", "deny",
     "🔴 THE FAIL-OPEN THE STRING MATCH HAD: `-m` takes a value, so this is a real "
     "merge whose MESSAGE is `--ff-only`, and `\"--ff-only\" in rest` exempted it"),
    ("git merge --message --ff-only origin/main", "deny",
     "⚠ THE LONG TWIN, AND A SURVIVING MUTANT IS WHY IT IS HERE: emptying `merge`'s "
     "`long_value` left every test green, because the row above exercises the SHORT "
     "`-m` only. Same fail-open, reached through the long spelling"),
    ("git merge --clean --ff-only origin/main", "deny",
     "…and through a PREFIX of a long value option (`--cleanup <mode>`), which is the "
     "combination of the two rules this model exists for"),
    ("git merge -s ours --ff-only origin/main", None,
     "a value-taking option BEFORE the flag: `ours` must not be mistaken for it"),
    ("git merge -S --ff-only origin/main", None,
     "⚠ `-S`/`--gpg-sign` takes its key ONLY attached, so modelling it as value-taking "
     "would eat the `--ff-only` and refuse a documented recipe"),
    ("git merge --log --ff-only origin/main", None,
     "…and the long twin, `--log[=<n>]`"),
])
def test_the_FF_ONLY_exemption_goes_through_the_OPTION_GRAMMAR(
        parallel_clone, command, expected, why):
    """🔴 THE EXEMPTION MATCHED A SPELLING UNDER A CLAIM THAT NOTHING DOES.

    `_is_exempt` read `"--ff-only" in rest` while `_option_state`'s docstring asserted
    it was "THE ONE PLACE THAT READS GIT'S OPTION GRAMMAR. Everything that used to
    match an option spelling now asks this." The behaviour predated that sentence; the
    sentence was new in the commit that consolidated the other three — which is what
    makes it a defect rather than a gap, because a claim of coverage is what stops the
    next reader looking.

    Routing it through the model makes the claim true and closes both directions at
    once: the abbreviations git accepts are allowed, and a `--ff-only` sitting in a
    flag's VALUE no longer exempts a real merge.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook(command, clone)) == expected, f"{why}: {command}"


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
    ('echo "say \\"hi\\""\ngit commit -m sneaky',
     'an ESCAPED quote inside `"…"` — the `\\"` was read as the closer, so the real '
     'closer re-opened a quote that never closed and the WHOLE command came back as '
     'one logical line with `git` not at argv[0]'),
    ('printf "%s\\n" "a\\"b"\ngit commit -m sneaky',
     "the same through an ordinary `printf`"),
    ('sed -i "s/x/\\"y\\"/" f.txt\ngit commit -m sneaky',
     "…and an ordinary `sed`"),
    ("echo $'it\\'s'\ngit commit -m sneaky",
     "`$'…'` is the one single-quote form that DOES honour a backslash, which is "
     "why the flag is set from the character before the opening quote"),
    ('echo "a\\\\b"\ngit commit -m sneaky',
     "⚠ THE DISCRIMINATOR: a BALANCED escape was already handled, and must stay so — "
     "deny before the fix as well as after"),
    ("echo \\$'a\\'\ngit commit -m sneaky",
     "🔴 THE REGRESSION THE ESCAPE MODEL ITSELF SHIPPED, and the narrowest one in this "
     "file: an ESCAPED dollar. `\\$` is a literal dollar to bash and the `'` after it "
     "opens a PLAIN single quote, but the unquoted-escape branch sets `prev` to the "
     "character it consumed — so `prev == \"$\"` and the quote was routed into the "
     "escape-honouring model, eating its own closer. deny before the escape model, "
     "ALLOW after it, deny now: `prev_escaped` is what separates the two"),
    ("echo \\$'a\\'\ngit commit -m sneaky",
     "🔴 THE REGRESSION THE ESCAPE MODEL ITSELF SHIPPED, and the narrowest thing in "
     "this file: an ESCAPED dollar. `\\$` is a literal dollar to bash and the `'` "
     "after it opens a PLAIN single quote — but the unquoted-escape branch sets "
     "`prev` to the character it consumed, so `prev == \"$\"` held and the quote was "
     "routed into the escape-honouring model, eating its own closer. deny before the "
     "escape model, **ALLOW** after it, deny now; `prev_escaped` is the difference. "
     "It is the hole this file's own sweep found as `r14` one commit earlier, "
     "reachable in production by a one-character prefix: the guard was closed against "
     "the mutant and open against the shell"),
    ("echo 'ends with a backslash \\'\ngit commit -m sneaky",
     "🔴 THE OTHER DIRECTION, AND A MUTATION SWEEP IS WHAT FOUND IT UNGUARDED: inside "
     "`'…'` a backslash is LITERAL, so the quote above closes and this line ends. A "
     "mutant that let EVERY quote honour escapes ate the closing quote, glued the "
     "rest of the command on, and flipped this to ALLOW with all 292 tests still "
     "green — so the single-quote half of the model had no case at all"),
    ('echo "say \\"hi\\""\ngit add new.txt && git commit -m sneaky',
     "⚠ AND THE ROW THAT EXPLAINS WHY ONE PUBLISHED REPRO DID NOT REPRODUCE: with an "
     "OPERATOR before the write this was already deny at `0aa1ba2`. The glued line "
     "still reaches the lexer, which handles `\\\"` correctly, and `&&` is emitted as "
     "its own token — so the write lands in a SECOND segment whose first word is "
     "`git`. The fail-open needs the write to be the first word of its segment"),
])
def test_an_ESCAPED_QUOTE_does_not_disarm_the_guard_for_LATER_LINES(
        parallel_clone, command, why):
    """🔴 ONE `\\"` ON AN EARLIER LINE TURNED THE GUARD OFF FOR THE WHOLE REST OF THE
    COMMAND, AND THE DOCSTRING CLAIMED THIS FAMILY WAS CLOSED.

    `_shell_lines` tracked quotes but had no backslash model inside them, so the
    escaped quote closed the string, the real closer opened a new one, and nothing
    ever closed THAT — the walk returned every remaining line glued into one. A
    later-line `git add … && git commit …` then sits in the middle of a token list
    whose first word is `echo`, so no candidate is produced and the write is
    ALLOWED. Measured end to end with a plain-`echo` control denying in the same run.

    ⚠ SINGLE-LINE COMMANDS WERE NEVER AFFECTED — `;` still splits — so this needed a
    newline and a later-line write, which is precisely the ordinary shape of an
    agent's Bash call rather than an exotic one.

    🔴 AND THE REPRO IS NARROWER THAN THE FIRST REPORT OF IT, WHICH IS WHY THE LAST
    ROW IS HERE RATHER THAN THE SHAPE THAT WAS HANDED TO ME. A report of this gave
    the later line as `git add new.txt && git commit -m sneaky`; driven against
    `0aa1ba2` that is **deny**, not allow. The glued line still reaches `shlex`,
    which DOES model `\\"` inside double quotes, and `&&` comes back as its own
    operator token — so the write starts a second segment and is seen. The fail-open
    needs the write to be the FIRST word of its segment, i.e. a bare later line.
    Every row above was re-measured in that shape before being asserted; taking the
    reported one would have shipped five rows that were green at the base ref.

    🔴 THE FIX IS QUOTE-KIND-AWARE AND THAT IS NOT PEDANTRY: inside `'…'` a backslash
    is LITERAL, so honouring escapes there would mis-parse `<<'EOF'` and break the
    heredoc battery above. The full (a) battery was re-run against this change, not
    just these rows.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook(command, clone)) == "deny", f"{why}: {command!r}"


def test_an_UNESCAPED_dollar_quote_IS_honoured_and_its_reset_is_LIVE(parallel_clone):
    """🔴 THE OTHER SIDE OF `prev_escaped`, AND AN UNDECLARED LIVE MUTANT POINTED AT IT.

    Here the `$` is UNESCAPED — the escape consumed the `x` — so `$'…'` really does
    honour the backslash, the quote never closes, and **bash itself** reports
    `unexpected EOF while looking for matching '` (measured): the script dies on line
    one and the second line never runs. ALLOW is therefore correct, and refusing would
    be a false positive on a command that cannot execute.

    ⚠ IT IS HERE BECAUSE THE SURVIVOR LEDGER WAS SHORT BY ONE — TWICE. Six
    `prev_escaped` resets were added and exactly one survivor was declared; deleting the
    reset on the GENERIC TAIL branch kept the whole suite green while flipping this row
    to deny, behaviourally live and unguarded. The correcting round then labelled four
    of the five dead ones and left the fifth bare while claiming "the other two".

    🔴 SO THE COUNT IS GONE AND THE RULE REPLACES IT: `prev_escaped` is read at exactly
    one site, so every reset but the one marked LIVE is dead, and
    `test_prev_escaped_is_READ_AT_EXACTLY_ONE_SITE` asserts the property the labels
    depend on. Each dead reset was also individually mutated and SURVIVED, which is what
    makes the labels honest rather than hopeful.
    """
    clone = parallel_clone[0]
    command = "echo \\x$'a\\'\ngit commit -m sneaky"
    assert _decision(_run_hook(command, clone)) is None, repr(command)


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


@pytest.mark.parametrize("command,why", [
    ("git revert --no-edit HEAD",
     "🔴 ALLOWED at `0aa1ba2` and it moves HEAD, the index and the worktree — "
     "condition 1's own three, through the SAME sequencer as `cherry-pick`, which "
     "was already refused"),
    ("git revert -n HEAD", "the staged form: no new commit, but the index rewritten"),
    ("git revert HEAD", "the bare form"),
    ("git update-index --force-remove seed.txt",
     "plumbing that rewrites `.git/index` directly — measured, the path left the index"),
    ("git read-tree HEAD", "rewrites `.git/index` from a tree"),
    ("git read-tree -m HEAD side", "…and from two"),
    ("git symbolic-ref HEAD refs/heads/side",
     "rewrites `.git/HEAD` — the shared HEAD moved under a peer, measured"),
    ("git symbolic-ref --delete refs/heads/alias",
     "a one-operand WRITE, so it cannot be excluded by operand count"),
    # 🔴 THE FOUR SPELLINGS AN EXACT-STRING `--delete` CHECK MISSED, each measured to
    # DELETE A REAL REF. Branch refs live in the COMMON git dir — the stated reason
    # `branch -D` could not be scoped — so every one was reachable from any worktree.
    ("git symbolic-ref -qd refs/heads/alias", "SHORT BUNDLING, `-q` then `-d`"),
    ("git symbolic-ref -dq refs/heads/alias", "…and the other order"),
    ("git symbolic-ref --del refs/heads/alias", "an unambiguous PREFIX"),
    ("git symbolic-ref --d refs/heads/alias", "…the shortest one git still resolves"),
])
def test_the_FOUR_writers_a_ROUND_ONE_AUDIT_found_are_refused(
        parallel_clone, command, why):
    """🔴 THE PR THAT ADDED `clean`/`rm`/`mv` ALSO ASSERTED THE OUT-SET WAS COMPLETE,
    AND IT WAS NOT. Every row here was ALLOWED at `0aa1ba2` while the hook's comment
    said an audit had found five writers, the doc tabled two as deliberately out, and
    a test pinned that pair on grow AND shrink. A pinned false completeness is worse
    than an unstated limit: it is the thing that stops the next reader looking.

    `revert` is the sharpest row — it is implemented by the same sequencer as
    `cherry-pick`, which was in the ledger, so the asymmetry had no reason at all.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook(command, clone)) == "deny", f"{why}: {command}"


@pytest.mark.parametrize("command,why", [
    ("git symbolic-ref HEAD", "the READ form: prints the ref, changes nothing"),
    ("git symbolic-ref --short HEAD", "…and with a flag, which is not an operand"),
    ("git symbolic-ref -q HEAD", "…and the quiet one"),
    ("git symbolic-ref -m reason HEAD",
     "`-m <reason>` is this subcommand's one value-taking SHORT option, so its value "
     "must not be counted as a second operand"),
    ("git symbolic-ref --no-delete refs/heads/alias",
     "last-wins negation: measured, it PRINTED the ref and deleted nothing"),
])
def test_the_symbolic_ref_READ_form_is_not_refused(parallel_clone, command, why):
    """🔴 THE READ AND WRITE FORMS DIFFER BY ONE OPERAND, WHICH IS THE ONLY REASON
    THIS SUBCOMMAND NEEDS AN EXEMPTION.

    Measured on git 2.55.0: `git symbolic-ref HEAD` prints `refs/heads/<branch>` and
    leaves HEAD, the index and the worktree byte-identical, while
    `git symbolic-ref HEAD refs/heads/<other>` rewrote `.git/HEAD`. Refusing the
    one-operand form would refuse the ordinary way to ask which branch is checked
    out — a false positive, the direction this guard forbids itself — so the
    exemption COUNTS operands with flags dropped, and excludes `--delete` by name
    because that is a write with one operand.

    ⚠ NOT REGRESSION COVERAGE: green at `0aa1ba2` vacuously, because `symbolic-ref`
    was not refused there at all. These pin the exemption that lands with the
    refusal; the pair is what makes the refusal usable.
    """
    clone = parallel_clone[0]
    assert _decision(_run_hook(command, clone)) is None, f"{why}: {command}"


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
    # ---- 🔴 THE THREE THAT DELETED REAL FILES, or would have. Each was ALLOWED at
    # `0aa1ba2` by the first version of this exemption, and the first two were
    # measured destroying files in an armed clone.
    ("git clean -f -e -n", "deny"),      # `-n` is --exclude's VALUE; deleted junk
    ("git clean -f --exclude -n", "deny"),       # the long spelling of the same
    ("git rm -f -- -n", "deny"),         # `-n` is a PATHSPEC after `--`; deleted it
    ("git rm -f -- -n seed.txt", "deny"),
    ("git mv -f -- -n other.txt", "deny"),
    # …and the one a SURVIVING MUTANT pointed at: deleting the long-option guard
    # flipped this to ALLOW while all 256 tests stayed green.
    ("git rm --ignore-unmatch seed.txt", "deny"),
    ("git rm --cached --ignore-unmatch seed.txt", "deny"),
    # The attached spellings must keep working — `-fenfoo` was measured to really
    # delete, and `--exclude=n` is the long twin.
    ("git clean -f --exclude=n", "deny"),
    ("git clean -fenfoo", "deny"),
    # 🔴 THE BOUNDARY ROW A SURVIVING MUTANT ASKED FOR. `position == len(word) - 1`
    # → `<=` SURVIVED with the whole suite green: `-fenfoo` denies under that mutant
    # too, so it is not a control for the boundary at all. What distinguishes them is
    # an ATTACHED value followed by a REAL `-n`: today `-efoo` consumes nothing
    # further and the `-n` is the dry run, so this is ALLOW; under the mutant the
    # `-n` is eaten as `-e`'s value and a genuine dry run is REFUSED.
    ("git clean -f -efoo -n", None),
    ("git clean -f --exclude=foo -n", None),
    # 🔴 PREFIX MATCHING, BOTH DIRECTIONS — git resolves an unambiguous abbreviation
    # and the abbreviation eats its value identically. `--exc` ate the `-n` and the
    # clean really deleted; `--dry` and `--d` are real dry runs that were REFUSED.
    ("git clean -f --exc -n", "deny"),
    ("git clean -f --ex -n", "deny"),
    ("git clean -f --e -n", "deny"),
    ("git clean --dry", None),
    ("git clean --d", None),
    ("git rm --dry seed.txt", None),
    # 🔴 LAST-WINS NEGATION. The old walk returned on the first `n` and never read the
    # rest, so both of these deleted real files while being allowed.
    ("git clean -n --no-dry-run -f", "deny"),
    ("git clean -n --no-dry -f", "deny"),
    ("git rm -n --no-dry-run seed.txt", "deny"),
    ("git mv -n --no-dry-run seed.txt other.txt", "deny"),
    # …and the other order, which really is a dry run.
    ("git clean --no-dry-run -n", None),
    ("git rm --no-dry-run -n seed.txt", None),
    # ⚠ THE AMBIGUOUS-PREFIX WIDENING, AND THIS ROW PINS A DECISION RATHER THAN A
    # SAFETY PROPERTY — say so, because it looks like the latter. `--pathspec-f`
    # prefixes BOTH `--pathspec-from-file` (takes a value) and `--pathspec-file-nul`
    # (does not), so `_resolve_long` cannot resolve it; it reports value-taking
    # because ANY candidate does, which swallows the `-n` and REFUSES. 🔴 git itself
    # answers this command `rc 129 ambiguous option` and writes NOTHING (measured), so
    # neither verdict can prevent or permit a write — the row exists so the widening is
    # a choice somebody made on purpose.
    # ⚠ AND IT IS NOT THE ONLY SHAPE THAT REACHES THAT BRANCH, as an earlier comment
    # here claimed — nor is the correction's own count right, which is why there is no
    # count here now. The widening test — its name is one token, so it is written on a
    # line of its own rather than wrapped, because an identifier split across two
    # comment lines is unsearchable for a reader and unparseable for the existence sweep:
    # `test_the_AMBIGUOUS_PREFIX_WIDENING_HOLDS_FOR_EVERY_TOKEN_THAT_REACHES_IT`
    # enumerates them from the tables and PRINTS the tally; read it there. Every one of
    # them is ambiguous to git, which writes nothing, so the conclusion holds however
    # many there turn out to be — the point of asserting the invariant, not the list.
    ("git rm --pathspec-f -n seed.txt", "deny"),
    ("git rm --p -n seed.txt", "deny"),
    ("git rm --pathspec- -n seed.txt", "deny"),
    # ---- 🔴 THE FIVE ROWS A DIFFERENTIAL FUZZ AGAINST REAL GIT FOUND INSIDE THE
    # OPTION MODEL ITSELF, each measured with the verdict taken BEFORE the command.
    # Two errors in one conditional: `partition` discarded the `=` separator, so
    # `--exclude=` looked like "no value" and the NEXT word was eaten; and `negated`
    # was computed and never consulted, so the `--no-` form of a value-taking option
    # ate one too. The swallowed word is read as neither flag nor operand, so a
    # following `--no-dry-run` went invisible and the exemption stood while git deleted.
    ("git rm -n --no-pathspec-from-file --no-dry-run seed.txt", "deny"),
    ("git rm -n --pathspec-from-file= --no-dry-run seed.txt", "deny"),
    ("git clean -n --exclude= --no-dry-run -f -d", "deny"),
    # …and the same bug REFUSING two real dry runs — the direction this file forbids
    # itself. Measured: `Would remove` / `rm '…'`, and nothing changed.
    ("git clean --exclude= -n", None),
    ("git rm --no-pathspec-from-file -n seed.txt", None),
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
    and it is a dry run — as are `-nr`, `-qn`, `-nv`, `-vn` and `-kn`. Every spelling
    here is resolved by the ONE option model in `_OPTION_GRAMMAR`: bundles, unambiguous
    PREFIXES (`--dry`, `--d`), a value-taking flag's attached or spaced value, the `--`
    stop, and last-wins negation. ⚠ THE EARLIER REASONING THIS DOCSTRING CARRIED — that
    a cluster is safe because the scan "stops at an `e`" — IS RETRACTED: it was true for
    the attached spelling and false for the spaced one, and `git clean -f -e -n` deleted
    a file while being allowed.

    ⚠ `git add -n` IS A REAL DRY RUN (measured: rc 0, repository unchanged) AND IS
    ASSERTED **deny** HERE ON PURPOSE — an operator decision about scope creep, with
    `apply --check` the other one of its kind. 🔴 `commit --dry-run` IS NOT ONE OF
    THOSE AND MUST NOT BE FILED WITH THEM: it writes a tree object. 🔴 IF THE `add -n`
    DECISION CHANGES, THIS ROW IS WHERE IT CHANGES, together with `_DRY_RUN_SUBCOMMANDS`,
    the hook docstring's read-list bullet and the doc's exemption paragraph — four
    places, and a previous round moved two of them and called it done. It is NOT a
    claim that `git add -n` writes.
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
    member of the ledger answers `-h` with rc 129, `usage:` on the first line, and
    the
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
                              "for every refused subcommand, and the cheap "
                              "direction for an "
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


#: The PR's payload, ENUMERATED. Not a glob and not `grep -r`: the local `grep` is a
#: ugrep wrapper that honours `.gitignore`, so a `-r` zero is a claim about grep's view.
_PROSE_PAYLOAD = (
    ".claude/hooks/base-clone-write-guard.py",
    "tests/test_base_clone_write_guard.py",
    "claudedocs/working-in-parallel.md",
)

# PROSE-SWEEP SELF-EXCLUSION START — everything between this marker and the END marker
# is the sweep's own patterns, controls and reasoning, so it necessarily contains the
# shapes it looks for. The span is cut from the scanned text, the markers are asserted to
# exist, and a planted claim is matched AFTER the cut to prove the cut did not blind it.
#: 🔴 (pattern, what it catches, a CONTROL STRING IT MUST MATCH). The controls are
#: asserted on every run: a sweep whose pattern cannot match its own control is wired to
#: nothing, and its zero is a fact about the sweep. Three instruments in this PR reported
#: a reassuring zero while blind — a marker-grep over `git merge-tree`, a `" 1 passed"`
#: substring, and a case-SENSITIVE sweep for a word that was written in capitals.
_PROSE_SWEEPS = (
    (r"(?:\b(?:thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty)\b"
     r"|\b1[0-9]\b)(?:(?!_REFUSED|refused|ledger|option tables).){0,90}?"
     r"(?:_REFUSED|refused subcommand|refused set|the ledger|option tables)"
     r"|(?:_REFUSED|refused subcommand|refused set|the ledger|option tables)"
     r"(?:(?!_REFUSED).){0,90}?(?:\b(?:thirteen|fourteen|fifteen|sixteen|seventeen"
     r"|eighteen|nineteen|twenty)\b|\b1[0-9]\b)",
     "a COUNT of a ledger the tests already derive",
     "measured for all fourteen members of `_REFUSED`"),
    (r"_DRY_RUN_(?:SHORT_)?VALUE_FLAGS",
     "a pointer to a symbol that no longer exists",
     "see `_DRY_RUN_VALUE_FLAGS` for the measurement"),
    (r"own letters are never read as flags",
     "the retracted `-e` stop reasoning, true only for the attached spelling",
     "so the pattern's own letters are never read as flags"),
    (r"\b60 fail-opens\b",
     "a divergence count from a draft that is not in the tree, so nobody can re-derive it",
     "it reported 60 fail-opens including `git clean -f`"),
    # ⚠ NO OPERATOR IDENTIFIER IN EITHER THE PATTERN OR THE CONTROL, AND THAT IS A
    # DELIBERATE NARROWING RATHER THAN AN OVERSIGHT. The first draft matched the
    # operator's username and used their real checkout path as the control — committing
    # a host path into a PUBLIC repository to test for host paths. `home/` and `/Users/`
    # give the reach without naming anybody, and the identity class is `leakscan.py`'s
    # own `operator-identity` rule, which is exempted from its own scan by name for
    # exactly this reason: a scanner has to contain the strings it looks for.
    (r"(?:home/|/Users/|\b10\.\d+\.\d+\.\d+|\b192\.168\.|\b172\.(?:1[6-9]|2\d|3[01])\.)",
     "an absolute home path or a private IP — this repository is PUBLIC",
     "a path such as home/<someone>/checkout"),
)


def test_NO_LIVE_PROSE_COUNT_OR_DEAD_POINTER_SURVIVES_IN_THE_PAYLOAD():
    """🔴 THE MECHANICAL CLOSURE FOR THE DEFECT EVERY FIX ROUND OF THIS PR REPRODUCED:
    a count or a pointer stated in PROSE beside a thing that changes.

    Four rounds fixed the reported instance and left the shape alive elsewhere. The last
    one reported "the count is gone from all eight sites" on the strength of a
    case-SENSITIVE grep, and the survivor was `MEASURED FOR ALL FOURTEEN` — uppercase,
    seven lines above a comment saying no count was there. So the sweep is no longer
    something somebody runs: it runs here, case-INSENSITIVELY, over an ENUMERATED file
    list, with each pattern's control asserted to HIT.

    ⚠ IT IS SCOPED TO LIVE CLAIMS, NOT TO HISTORY. The ledger pattern only fires when a
    number sits within ninety characters of a reference to the refused ledger or the
    option tables; the mechanism narratives this file is full of — what was measured,
    what was retracted — keep their numbers, because a claim about the past cannot go
    stale. That is why the history was de-numbered where it sat next to a ledger
    instead: an absolute rule needs no exemption list, and an exemption list is how a
    guard becomes walkable.
    """
    # ⚠ BUILT FROM A TAG RATHER THAN WRITTEN OUT: a literal spelling the whole marker
    # would be a second occurrence of it, and the first draft's `index()` found that
    # literal instead of the real END marker — cutting the wrong span and leaving the
    # markers behind. The assertion below caught it.
    tag = "PROSE-SWEEP SELF-" + "EXCLUSION"
    start, end = f"# {tag} START", f"# {tag} END"
    scanned: dict[str, str] = {}
    for name in _PROSE_PAYLOAD:
        raw = (ROOT / name).read_text(encoding="utf-8")
        if name == Path(__file__).name or name.endswith(Path(__file__).name):
            assert start in raw and end in raw, (
                "the self-exclusion markers are gone; without them this sweep matches "
                "its own patterns and says nothing about the rest of the file"
            )
            raw = raw[: raw.index(start)] + raw[raw.index(end) + len(end):]
            assert start not in raw and end not in raw, "the cut left a marker behind"
        # Normalised: a claim wrapped across comment lines is invisible per-line, which
        # is how the hook's own top-level summary hid for two rounds.
        flat = re.sub(r"\n\s*(?:#:|#|\*|>)?\s*", " ", raw)
        scanned[name] = re.sub(r"[ \t]+", " ", flat)

    hits: list[str] = []
    for pattern, what, control in _PROSE_SWEEPS:
        compiled = re.compile(pattern, re.I | re.S)
        # CONTROL 1: the pattern must match its own control string.
        assert compiled.search(control), (
            f"the sweep for {what!r} cannot match its own control — it is wired to "
            f"nothing and a zero from it would mean nothing"
        )
        # CONTROL 2: and it must still reach the SCANNED text after the self-cut. A
        # cut that removed too much would leave every pattern matching nothing, which
        # is the same reassuring zero one layer along.
        for name, flat in scanned.items():
            assert compiled.search(flat + " " + control), (
                f"the sweep for {what!r} cannot see a planted claim in {name} — the "
                f"self-exclusion cut too much"
            )
            for match in compiled.finditer(flat):
                hits.append(f"{name}: {what}: …{match.group(0)[:110]}…")
    assert not hits, (
        f"{len(hits)} live prose claim(s) that a test should own:\n" + "\n".join(hits)
    )
# PROSE-SWEEP SELF-EXCLUSION END


def _test_functions_defined_under_tests() -> frozenset[str]:
    """Every `test_*` function name defined anywhere under `tests/`, from the AST.

    Parsed rather than grepped: a name in a docstring or an assertion message is not a
    definition, and only the grammar can tell those apart. A file that fails to parse
    raises here rather than being skipped — a skip would silently shrink this set, and a
    shrunken set turns a live pointer into a false positive.
    """
    names: set[str] = set()
    for path in sorted((ROOT / "tests").rglob("*.py")):
        tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
        for node in ast.walk(tree):
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) \
                    and node.name.startswith("test_"):
                names.add(node.name)
    return frozenset(names)


def _test_module_stems() -> frozenset[str]:
    """`test_*` FILE stems under `tests/` — names that are modules, not functions.

    Derived rather than listed: a comment citing `tests/test_base_clone_write_guard.py`
    mentions a `test_`-prefixed token that no function defines, and hardcoding that one
    exclusion would leave a permanent false positive the moment any other test module is
    cited. A gate with a standing false positive is one everybody learns to ignore.
    """
    return frozenset(path.stem for path in (ROOT / "tests").rglob("test_*.py"))


#: Tests this payload cites as HISTORY — a record that they were deleted or replaced —
#: so naming them is correct and their absence is the point. A DECLARED ledger rather
#: than a lexical "is this sentence about the past" guess, which would be a spelled
#: guard: the existence sweep asserts each of these is still ABSENT, so if one is ever
#: re-created the sweep says so instead of quietly hiding a live pointer again.
_TESTS_CITED_AS_HISTORY = frozenset({
    # Replaced by the parametrised crowding case when the probe cap was removed; both
    # went vacuous with the cap, which is what the citing docstring records.
    "test_junk_candidates",
    "test_junk_ADDITIVE_candidates",
})


def test_EVERY_TEST_A_PAYLOAD_COMMENT_NAMES_STILL_EXISTS():
    """🔴 THE HALF OF THE DEAD-POINTER SHAPE THAT **IS** MECHANISABLE, AND I WRONGLY
    DECLARED IT UNCLOSABLE ALONG WITH THE HALF THAT IS NOT.

    Two questions were collapsed into one "unclosable" limit, and only one of them
    deserved it:

      * "does the named test PRINT a tally?" — genuinely unclosable here. Measured: a
        detector for it fired on eight sites, seven legitimate (`git worktree list`
        "reports REGISTRATIONS"; a ledger whose members you "read there" in the
        ASSERTION; `git symbolic-ref HEAD` "prints the ref"), and narrowing it until it
        went quiet left it matching only the instance already fixed. Deleted.
      * "does the named test EXIST?" — a plain AST lookup with **no false-positive
        class at all**. This is that, and it is the half that then failed: the F3 fix
        replaced `…IS_A_REAL_GIT_OPTION` and left the old name in the hook's comment,
        pointing at nothing.

    A limit stated wider than reality is the same defect as a claim stated wider than
    reality. So the declared limit is now the print half only.
    """
    defined = _test_functions_defined_under_tests()
    modules = _test_module_stems()
    # CONTROLS, both asserted every run: a name known PRESENT resolves, a name known
    # ABSENT does not. Without the pair, an empty `defined` would flag every pointer and
    # a universe-sized one would flag none.
    # ⚠ THE ABSENT CONTROL IS BUILT FROM PARTS so the full token never appears as a
    # literal in this file — otherwise the sweep below finds its own control and reports
    # it, which is what the first run did.
    absent = "test_" + "a_name_no_test_will_ever_define"
    assert "test_a_commit_in_the_base_clone_is_refused" in defined, (
        "a test known to exist is missing from the parse — it is wired to nothing"
    )
    assert absent not in defined, (
        "a name known not to exist resolved — the parse is not discriminating"
    )
    assert len(defined) >= 500, f"implausibly few test functions parsed: {len(defined)}"
    assert "test_base_clone_write_guard" in modules, "the module-stem derivation is wrong"
    # …and the history ledger must stay history: a re-created test here would mean the
    # citing docstring is now describing something live.
    for historic in sorted(_TESTS_CITED_AS_HISTORY):
        assert historic not in defined, (
            f"`{historic}` is cited as a DELETED test but now exists — drop it from "
            f"`_TESTS_CITED_AS_HISTORY` and re-read the docstring that cites it"
        )

    dead: list[str] = []
    for name in _PROSE_PAYLOAD:
        raw = (ROOT / name).read_text(encoding="utf-8")
        # ⚠ NORMALISED FIRST: a long name wrapped across two comment lines was otherwise
        # captured TRUNCATED and reported as missing — a false positive manufactured by
        # the reader, which is how a gate earns being ignored.
        text = re.sub(r"[ \t]+", " ", re.sub(r"\n\s*(?:#:|#|\*|>)?\s*", " ", raw))
        for cited in sorted(set(re.findall(r"\btest_[A-Za-z0-9_]+", text))):
            if cited in modules or cited in defined or cited in _TESTS_CITED_AS_HISTORY:
                continue
            dead.append(f"{name} names `{cited}`, which no test under tests/ defines")
    assert not dead, (
        f"{len(dead)} comment(s) pointing at a test that does not exist:\n"
        + "\n".join(dead)
    )
    print(f"\nexistence sweep: {len(defined)} test functions defined, "
          f"{len(modules)} test modules, 0 dead pointers in {len(_PROSE_PAYLOAD)} files")


def test_the_CI_FLOOR_PROSE_CANNOT_DISAGREE_WITH_THE_LITERAL():
    """🔴 THE SAME COMMENT HAS SHIPPED A WRONG FLOOR NUMBER THREE ROUNDS RUNNING, EACH
    TIME IN THE PARAGRAPH WRITTEN TO CORRECT THE LAST ONE.

    Round 2's heading said 2565 against a literal of 2566; the paragraph added to name
    that said "2588 -> 2607" against a literal of 2609. The defect is structural, not
    careless: a number written in PROSE beside a number that is COMPUTED will drift, and
    only the computed one is checked by anything. `claude/RULES.md` prescribes a
    deterministic fix over a prose one, and `RULES.md`'s own precedent is a test that
    owns the constants so nobody restates them.

    So this parses the step: the arithmetic line's result, and every `A -> B` transition
    in the floor commentary, must agree with the `FLOOR` literal the job actually uses.
    ⚠ IT PINS AGREEMENT, NOT THE VALUE — raising the floor needs no edit here, which is
    what keeps it from becoming one more number to maintain.
    """
    text = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
    literal = re.search(r"^\s*FLOOR = (\d+)\s*$", text, re.M)
    assert literal, "no `FLOOR = <n>` literal found — this parse is wrong, not the file"
    floor = int(literal.group(1))
    block = text[: literal.start()]

    # The derivation the comment prescribes: `m - min(50, max(1, m/20)) = <floor>`.
    arithmetic = re.findall(r"min\(50, max\(1, \d+/20\)\) = (\d+)", block)
    assert arithmetic, "the floor's arithmetic line is gone — it is the only derivation"
    assert int(arithmetic[-1]) == floor, (
        f"the floor comment's arithmetic computes {arithmetic[-1]} but the job uses "
        f"{floor}"
    )
    # …and no transition arrow may claim a different destination than the live literal.
    transitions = re.findall(r"#\s*⚠?\s*(\d{4}) -> (\d{4})", block)
    assert transitions, "no floor transitions found — the parse is wrong, not the file"
    assert int(transitions[-1][1]) == floor, (
        f"the last floor transition says `-> {transitions[-1][1]}` while the job uses "
        f"{floor}; a heading restating the number is what drifted three rounds running"
    )
    # POSITIVE CONTROL: the patterns must be able to see a disagreement at all.
    assert re.search(r"min\(50, max\(1, \d+/20\)\) = (\d+)",
                     "`9999 - min(50, max(1, 9999/20)) = 1234`"), "pattern wired to nothing"


def test_the_hook_namespace_loads_and_is_not_the_live_one():
    """POSITIVE CONTROL for the loader the derived checks below rest on."""
    namespace = _hook_namespace()
    assert callable(namespace["_resolve_long"])
    assert "clean" in namespace["_OPTION_GRAMMAR"]
    assert namespace["__name__"] != "__main__", (
        "the cut must not leave the module looking like the script entry point"
    )


def test_NO_TOKENS_VALUE_TAKING_VERDICT_DIFFERS_FROM_THE_INSTALLED_GITS():
    """🔴 THE THING THAT ACTUALLY MATTERS, ASSERTED INSTEAD OF A VERSION-DEPENDENT PREMISE.

    This test used to assert the SUBSET PREMISE — every name in `_OPTION_GRAMMAR` is a
    real option of that subcommand. That premise is true on git 2.55 and **false on
    2.50**, where `git merge --compact-summary` does not exist: measured FAIL on 2.49.0
    and 2.50.1, PASS from 2.51.0 up. 🔴 AND IT WAS A FALSE FAILURE — the hook is
    unaffected, because a phantom name in `long_bool` can never make a token
    value-taking, so across every prefix token of `merge`'s names the verdict differed in
    ZERO cases even on 2.50. The `tests` job runs `ubuntu-latest` with python pinned and
    **git not pinned at all**, so that gate was a function of the runner image; and
    `claude/RULES.md` is explicit that a permanently-red gate is worse than none, because
    it trains everyone to click through.

    So the assertion is the version-robust one: **for every token, if the installed git
    resolves it to exactly one option, the hook's value-taking verdict must equal git's
    for that option.** That is what the swallow-the-next-word decision depends on. A
    token git finds AMBIGUOUS is unconstrained — git refuses it and writes nothing — and
    a token git does not know at all is unconstrained for the same reason.

    🔴 MEASURED ON TWO REAL BINARIES, NOT ARGUED: **git 2.44.2 PASS · 2.55.0 PASS**, run
    from two nixpkgs pins on one machine. 2.44.2 is the useful end — it has no
    `--compact-summary` at all (`git merge -h | grep -c` is 0 there, 2 on 2.55.0), which
    is exactly the condition that made the old premise red, and this invariant does not
    notice.

    ⚠ A SIMULATION OF AN OLDER OPTION SET WAS WRITTEN FIRST AND IS DELETED, which is
    worth recording because it was the same defect one layer along: it asserted that the
    installed git HAS `--compact-summary` so it could remove it, and therefore failed on
    git 2.44.2 — a new gate that is a function of the runner image, in the fix for a gate
    that was one. The real two-point matrix replaces it; CI keeps only the invariant,
    which holds on both.

    ⚠ WHAT THIS STILL CANNOT SEE, stated rather than implied: a value-taking option that
    a FUTURE git adds and the tables do not name resolves to one git option with
    `takes=True` while the hook says False — so this test WOULD catch it, which is the
    narrow fail-open `_OPTION_GRAMMAR`'s version limit names. What it cannot catch is a
    git that changes an option's arity without changing its name in `-h`.
    """
    namespace = _hook_namespace()
    grammar, resolve = namespace["_OPTION_GRAMMAR"], namespace["_resolve_long"]
    differences: list[str] = []
    compared = 0
    for subcommand, spec in sorted(grammar.items()):
        real = _git_long_options(subcommand)
        assert real, f"no options parsed for `git {subcommand} -h`"
        hook_names = set(spec["long_value"]) | set(spec["long_bool"])
        tokens = {name[: cut] for name in hook_names
                  for cut in range(3, len(name) + 1)}
        tokens |= {f"--{name}"[: cut] for name in real
                   for cut in range(3, len(name) + 3)}
        for token in sorted(tokens):
            bare = token[2:]
            git_matches = [n for n in real if n.startswith(bare)]
            if bare in real:
                git_matches = [bare]
            if len(git_matches) != 1:
                continue                      # ambiguous or unknown to git: it refuses
            compared += 1
            _, hook_takes = resolve(token, spec)
            git_takes = real[git_matches[0]]
            if hook_takes != git_takes:
                differences.append(
                    f"`git {subcommand} {token}` -> git resolves "
                    f"`--{git_matches[0]}` (takes a value: {git_takes}) but the hook "
                    f"says takes a value: {hook_takes}"
                )
    # POSITIVE CONTROL: the comparison must actually have compared something, and a
    # known value-taking option must come back as one from BOTH sides.
    assert compared >= 50, f"only {compared} token(s) compared — this proved little"
    assert _git_long_options("clean").get("exclude") is True, (
        "`git clean --exclude` did not parse as value-taking — the `-h` reader is wrong"
    )
    assert not differences, (
        f"{len(differences)} token(s) where the hook and the installed git disagree "
        f"about consuming a value ({compared} compared):\n" + "\n".join(differences[:15])
    )


def test_the_AMBIGUOUS_PREFIX_WIDENING_HOLDS_FOR_EVERY_TOKEN_THAT_REACHES_IT():
    """🔴 THE INVARIANT, DERIVED — REPLACING A PROSE ENUMERATION THAT WENT STALE TWICE.

    The docstring used to count the tokens reaching the widening branch: first "exactly
    one shape", corrected to "ten", and the correcting commit made it wrong by nine
    again by adding a second subcommand to the grammar. A count beside a table that
    grows is the defect this whole file keeps re-finding, so the count is gone and this
    is what replaces it:

      for every token this branch answers `(None, value-taking)` for, the hook's own
      table matches it with AT LEAST TWO names.

    Combined with the subset premise above — every table name is a real git option —
    that is the whole safety argument: two hook names sharing the prefix means two git
    names do, so git refuses the abbreviation as ambiguous and writes nothing, whichever
    verdict the guard gives. An invariant does not go stale when a table grows.

    The numbers are REPORTED, never asserted, for exactly the same reason.
    """
    namespace = _hook_namespace()
    grammar, resolve = namespace["_OPTION_GRAMMAR"], namespace["_resolve_long"]
    # ⚠ A SET PER SUBCOMMAND, NOT A LIST: a token that prefixes several names is
    # generated once per name, and appending it each time inflated the first reported
    # figure to 46 — the same restate-a-count defect, one layer in, in the test written
    # to retire it. The number is only reported, but a wrong report is still a claim.
    reached: dict[str, set[str]] = {}
    for subcommand, spec in sorted(grammar.items()):
        names = set(spec["long_value"]) | set(spec["long_bool"])
        for name in sorted(names):
            for cut in range(3, len(name) + 1):          # `--` plus one char, upward
                token = name[:cut]
                canonical, takes_value = resolve(token, spec)
                if canonical is None and takes_value:
                    matches = sorted(n for n in names if n.startswith(token))
                    assert len(matches) >= 2, (
                        f"`git {subcommand} {token}` reaches the widening branch but "
                        f"the table matches it with {matches} — fewer than two names "
                        f"means git may NOT find it ambiguous, and the safety argument "
                        f"for consuming a value does not apply"
                    )
                    reached.setdefault(subcommand, set()).add(token)
    # POSITIVE CONTROL for the walk itself: an exact name must resolve, and the branch
    # must actually be reachable — a zero here would mean the loop proved nothing.
    assert resolve("--exclude", grammar["clean"]) == ("--exclude", True)
    assert reached, "no token reached the widening branch — this test proved nothing"

    # 🔴 THE OTHER HALF OF THE RULE, AND A SURVIVING MUTANT IS WHY IT IS HERE. The walk
    # above only generates prefixes OF EXISTING NAMES, so every token it tries matches at
    # least one — which means it can never see a mutant that reports value-taking for a
    # token matching NOTHING. `return None, True` survived the whole suite. An UNKNOWN
    # long option must be read as a bare boolean and consume no value, which is the
    # documented behaviour (and the narrow, named fail-open in `_OPTION_GRAMMAR`'s
    # version limit); treating it as value-taking would swallow the following word, so a
    # `-n` after it would go unseen and a real dry run would be refused — and on `merge`
    # it would swallow a `--ff-only` and refuse the prescribed resync recipe.
    for subcommand, spec in sorted(grammar.items()):
        names = set(spec["long_value"]) | set(spec["long_bool"])
        unknown = "--zzz-not-a-real-option"
        assert not any(n.startswith(unknown) for n in names), "pick a different probe"
        assert resolve(unknown, spec) == (None, False), (
            f"`git {subcommand} {unknown}` matches no name in the table, so it must be "
            f"read as a bare boolean: reporting it value-taking swallows the next word"
        )
    print("\nambiguous-prefix widening reached by: "
          + ", ".join(f"{sub} {len(toks)}" for sub, toks in sorted(reached.items()))
          + f" (total {sum(len(t) for t in reached.values())}, before `--no-` probes)")


def test_prev_escaped_is_READ_AT_EXACTLY_ONE_SITE():
    """🔴 THE INVARIANT BEHIND THE DEAD/LIVE LABELS, so the ledger cannot drift again.

    Two audit rounds found the `prev_escaped` survivor ledger short by one — first
    missing the LIVE reset, then missing a DEAD label. The fix is not a third count: the
    flag is READ at exactly one site, the quote-open branch, so a reset can only matter
    if control can reach that read from it with no intervening write. Every reset but
    the one labelled LIVE is on a branch that cannot (each says which), and a mutation
    sweep confirms each of those individually SURVIVES.

    If this test fails, a second read was added and every DEAD label needs re-deriving.

    🔴 COUNTED FROM THE AST, NOT FROM A REGEX, AND THE REGEX WAS WALKABLE. It excluded
    any line matching `^\\s*prev_escaped = `, so a read on the RIGHT-HAND SIDE of an
    assignment to the flag itself counted as a pure write: the mutant
    `prev_escaped = bool(prev_escaped) and False` SURVIVED green, and three realistic
    shapes hide in the same place (`= prev_escaped and char != "\\n"`, `= not
    prev_escaped`, `: bool = prev_escaped or False`). A line-shaped guard over code is a
    spelled guard; `ast` distinguishes a `Load` from a `Store` because that is what the
    grammar means.
    """
    tree = ast.parse(HOOK.read_text(encoding="utf-8"), filename=str(HOOK))
    reads = [
        node for node in ast.walk(tree)
        if isinstance(node, ast.Name) and node.id == "prev_escaped"
        and isinstance(node.ctx, ast.Load)
    ]
    stores = [
        node for node in ast.walk(tree)
        if isinstance(node, ast.Name) and node.id == "prev_escaped"
        and isinstance(node.ctx, ast.Store)
    ]
    # POSITIVE CONTROL: the walk must see the writes too, or a zero on reads would be
    # indistinguishable from an AST that never found the name at all.
    assert len(stores) >= 5, (
        f"the AST walk found only {len(stores)} write(s) of `prev_escaped` — it is not "
        f"looking at the right tree"
    )
    assert len(reads) == 1, (
        f"`prev_escaped` is READ at {len(reads)} site(s), not one, so the DEAD labels on "
        f"its resets no longer follow: lines "
        f"{sorted(node.lineno for node in reads)}"
    )


def test_the_OPTION_STATE_CONSUMERS_ARE_AN_ASSERTED_LEDGER():
    """🔴 `_option_state`'s "ONE PLACE" SENTENCE LISTED ITS CONSUMERS BY HAND AND MISSED
    THE ONE ADDED IN THE SAME COMMIT. That sentence is what a reader uses to decide how
    many call sites an edit must satisfy, so it is pinned here instead of maintained.

    Fails on GROW (a consumer added without the prose moving) and on SHRINK (one
    removed), which is the shape `claude/RULES.md` prescribes for a caller ledger: a
    relationship, failing in both directions, rather than a count in a comment.
    """
    source = HOOK.read_text(encoding="utf-8")
    callers: list[tuple[str, str]] = []
    enclosing = "<module>"
    for line in source.splitlines():
        match = re.match(r"def (\w+)\(", line)
        if match:
            enclosing = match.group(1)
        if "_option_state(" in line and not line.lstrip().startswith("#") \
                and not line.startswith("def _option_state"):
            callers.append((enclosing, line.strip()))
    assert [c[0] for c in callers] == ["_is_dry_run", "_is_exempt", "_is_exempt"], (
        f"the `_option_state` call sites moved: {callers}"
    )


def _repo_state(repo: Path) -> str:
    """A hash of everything a `clean`/`rm`/`mv` could change: HEAD, the index, and the
    working tree's own file list and contents."""
    env = {**os.environ, **_GIT_ENV}
    head = subprocess.run(["git", "rev-parse", "HEAD"], cwd=repo, capture_output=True,
                          text=True, env=env).stdout
    index = subprocess.run(["git", "ls-files", "-s"], cwd=repo, capture_output=True,
                           text=True, env=env).stdout
    tree = []
    for path in sorted(p for p in repo.rglob("*") if ".git" not in p.parts):
        tree.append(f"{path.relative_to(repo)}:"
                    f"{path.read_bytes().hex() if path.is_file() else 'DIR'}")
    return head + index + "\n".join(tree)


def test_the_guard_AGREES_WITH_REAL_GIT_over_generated_option_combinations(tmp_path):
    """🔴 A DIFFERENTIAL AGAINST REAL GIT, ADOPTED BECAUSE A HANDFUL OF REGRESSION ROWS
    CLOSES SPELLINGS AND LEAVES THE METHOD THAT FOUND THEM UNAVAILABLE TO THE NEXT
    CHANGE. The fail-open it was added for was invisible to 315 passing tests: the
    option model swallowed a word it should not have, so a following `--no-dry-run`
    went unseen and `git rm -n --no-pathspec-from-file --no-dry-run seed.txt` was
    ALLOWED while deleting a TRACKED file. Nothing in the suite enumerated option
    combinations against git, so nothing could see it.

    The invariants, with GIT as the oracle rather than the model under test:

      * the command CHANGED the repository  ⇒  the guard must **deny**  (fail-open);
      * it changed nothing, exited 0, and git's OWN output says it was a dry run  ⇒
        the guard must **allow**  (false positive).

    Anything else — an error, or a no-op for an unrelated reason — asserts nothing,
    which is why the dry-run half reads git's marker instead of inferring from flags.

    🔴 IT REPORTS ITS OWN CONTROLS, because "0 divergences" is otherwise
    indistinguishable from a harness wired to nothing: the run must classify at least
    five commands as WROTE and five as DRY, or it observed neither class and the zero
    means nothing. ⚠ Measured negative control, out of band: reverting either half of
    the conditional this was written for turns it RED, and each half independently —
    see the commit message for the per-variant counts.
    """
    template = tmp_path / "template"
    _init_clone(template)
    _add_worktree(template, tmp_path / "template-wt")
    (template / "junk.txt").write_text("untracked\n", encoding="utf-8")
    env = {**os.environ, **_GIT_ENV}

    # 🔴 DEPTH THREE, NOT TWO, AND THE REASON IS A MEASUREMENT RATHER THAN THOROUGHNESS.
    # At depth 2 this harness catches the FALSE-POSITIVE half of the defect it was
    # written for and misses the FAIL-OPEN half entirely: the swallowed word has to sit
    # BETWEEN a `-n` and a `--no-dry-run`, so the shape needs three option words. A
    # depth-2 sweep reports zero and reads as coverage. Ask what a sweep's shape
    # structurally cannot reach, not only how many cases it runs.
    cases: list[list[str]] = []
    for subcommand, pool in _DIFFERENTIAL_POOL.items():
        operands = _DIFFERENTIAL_OPERANDS[subcommand]
        for words in itertools.chain.from_iterable(
                itertools.permutations(pool, depth)
                for depth in range(1, _DIFFERENTIAL_DEPTH + 1)):
            cases.append([subcommand, *words, *operands])

    wrote = dry = 0
    divergences: list[str] = []
    for argv in cases:
        repo = tmp_path / "case"
        shutil.rmtree(repo, ignore_errors=True)
        shutil.copytree(template, repo, symlinks=True)
        # 🔴 EACH CASE DRIVES THE HOOK COPY INSIDE ITS OWN COPY OF THE REPO. The first
        # version of this harness passed the TEMPLATE's hook while standing in the
        # copy, so `own_repo` and the cwd were different repositories and EVERY verdict
        # was the cross-repo ALLOW — so every command that wrote anything read as a
        # fail-open, `git clean -f` included, which the rest of this file proves is
        # denied. A differential whose instrument is wired to the wrong tree reports the
        # guard as ABSENT, which is indistinguishable from a guard that is absent.
        # ⚠ NO COUNT FOR THAT RUN: the draft is not in the tree, so nobody can
        # re-derive one, and an unreproducible number is the same defect as a stale one.
        case_hook = repo / ".claude" / "hooks" / HOOK.name
        # 🔴 THE VERDICT IS TAKEN BEFORE THE COMMAND RUNS, the only order that measures
        # what the guard would have done to a live call.
        verdict = _decision(_run_hook("git " + " ".join(argv), repo, hook=case_hook))
        before = _repo_state(repo)
        done = subprocess.run(["git", *argv], cwd=repo, capture_output=True,
                              text=True, env=env, timeout=60)
        after = _repo_state(repo)
        output = done.stdout + done.stderr
        marker = _DRY_RUN_MARKER[argv[0]]
        if before != after:
            wrote += 1
            if verdict != "deny":
                divergences.append(f"FAIL-OPEN: `git {' '.join(argv)}` CHANGED the "
                                   f"repository and the guard said {verdict!r}")
        elif done.returncode == 0 and marker in output:
            dry += 1
            if verdict is not None:
                divergences.append(f"FALSE POSITIVE: `git {' '.join(argv)}` is a dry "
                                   f"run (git printed {marker!r}, nothing changed) and "
                                   f"the guard said {verdict!r}")
    shutil.rmtree(tmp_path / "case", ignore_errors=True)

    # Reported, not merely asserted: a zero means nothing without the pair beside it.
    print(f"\ndifferential: {len(cases)} commands, {wrote} wrote, {dry} dry runs, "
          f"{len(divergences)} divergence(s)")

    # 🔴 THE SHAPE FLOORS, BECAUSE THE CLASS FLOORS ALONE LET THIS COLLAPSE TO A
    # FRACTION OF ITSELF AND STAY GREEN. Nothing used to assert the number of generated
    # commands or the depth, and `wrote >= 5` was two orders of magnitude below the
    # measured 383 — so trimming the pool or dropping the third loop for runtime would
    # pass, with the fail-open class unobserved and the collected count unchanged.
    #
    # ⚠ AND THE FIRST VERSION OF THESE FLOORS CLOSED ONE AXIS OF TWO, which is the
    # failure this whole ladder keeps repeating: fix the reported instance, leave the
    # shape. The derived case count catches a dropped LOOP and is structurally blind to
    # a trimmed POOL, because it reads the same pools the generator does. Both axes are
    # covered now, and each is measured below rather than asserted in a sentence:
    # the depth by `_DIFFERENTIAL_DEPTH` plus a PER-CASE depth check, the pool by the
    # subcommand set and `_DIFFERENTIAL_POOL_FLOOR`. Growing a pool still needs no edit.
    assert _DIFFERENTIAL_DEPTH >= 3, (
        "the differential must combine at least three option words: at depth 2 the "
        "fail-open class this test exists for is structurally unreachable (measured — "
        "see `_DIFFERENTIAL_DEPTH`)"
    )
    # 🔴 THE POOL AXIS, WHICH THE FIRST VERSION OF THESE FLOORS COULD NOT SEE AT ALL.
    # `expected` is derived from the same pools the generator reads, so the two shrink
    # together and `len(cases) == expected` is blind to a trim: measured, `clean`'s pool
    # cut from ten words to five took 1664 commands to 929 and PASSED, and DELETING the
    # `clean` pool entirely — a whole subcommand, 49% of the sweep — took it to 844 and
    # PASSED, runtime halved. Both cleared the class floors too. So the subcommand set
    # and the pool sizes are pinned here, independently of the generator.
    assert set(_DIFFERENTIAL_POOL) == {"clean", "rm", "mv"}, (
        f"the differential must sweep all three refused writers with a dry run; it "
        f"covers {sorted(_DIFFERENTIAL_POOL)}"
    )
    assert set(_DIFFERENTIAL_POOL) == set(_DIFFERENTIAL_OPERANDS) == _DRY_RUN_SUBCOMMANDS_FOR_TESTS, (
        "the differential's subcommands, its operand table and the hook's own dry-run "
        "policy set must be the same three"
    )
    for subcommand, floor in _DIFFERENTIAL_POOL_FLOOR.items():
        assert len(_DIFFERENTIAL_POOL[subcommand]) >= floor, (
            f"`{subcommand}`'s option pool has {len(_DIFFERENTIAL_POOL[subcommand])} "
            f"words, below the floor of {floor} — trimming a pool is invisible to the "
            f"derived case count, which is why the floor is written down separately"
        )
    expected = 0
    for pool in _DIFFERENTIAL_POOL.values():
        size, arrangements = len(pool), 0
        for depth in range(1, _DIFFERENTIAL_DEPTH + 1):
            term = 1
            for factor in range(depth):
                term *= size - factor
            arrangements += term
        expected += arrangements
    assert len(cases) == expected, (
        f"the generator produced {len(cases)} commands where the pools and "
        f"`_DIFFERENTIAL_DEPTH`={_DIFFERENTIAL_DEPTH} imply {expected} — a loop was "
        f"changed without the floors moving with it"
    )
    # 🔴 PER CASE, NOT ACROSS CASES. The first form mixed the MAXIMUM argv length over
    # all cases with the MINIMUM operand count over subcommands — `6 - 1 - 0 = 5 >= 3`
    # — so it held even with the generator collapsed to depth 1, while its message
    # claimed to check that some command carries the full depth. It could not fail.
    deepest = max(len(argv) - 1 - len(_DIFFERENTIAL_OPERANDS[argv[0]]) for argv in cases)
    assert deepest >= _DIFFERENTIAL_DEPTH, (
        f"the deepest generated command carries {deepest} option word(s), not "
        f"{_DIFFERENTIAL_DEPTH} — the generator was collapsed"
    )
    # The CLASS floors, now set near what was measured (383 wrote / 516 dry) with
    # headroom for a different git, rather than at a token 5.
    assert wrote >= 200 and dry >= 250, (
        f"the differential observed too little to vouch for anything: {len(cases)} "
        f"commands, {wrote} that wrote, {dry} dry runs — both classes must be "
        f"populated or a zero-divergence result is a fact about the harness"
    )
    assert not divergences, (
        f"{len(divergences)} divergence(s) over {len(cases)} commands "
        f"({wrote} wrote, {dry} dry):\n" + "\n".join(divergences[:20])
    )


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

    Red/green, corrected after a re-measurement — ⚠ AN EARLIER VERSION OF THIS
    DOCSTRING SAID FOUR AND THE TRUE NUMBER IS THREE. At `d6eafdb` the suite reports
    **4 failed / 7 passed** for this test, and the `xargs -n 1 git rm seed.txt` row
    was **already deny** there: `rm` had no dry-run exemption yet, so the wrapper's
    `-n` had nothing to borrow. It only became a bypass one commit later, which is
    why it is red against the INTERMEDIATE state and not against `d6eafdb`. So:
    `nice`, `sudo --` and `sudo --ff-only` were ALLOW at `d6eafdb`; the `env` one was
    ALLOW at `ffa0eca` as well. The four allow rows are ⚠ invariant
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
    `test_the_shared_state_WRITERS_LEFT_OUT_carry_a_RECORDED_DECISION` forces.
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
    # ⚠ `git restore` IS NOT IN THIS LIST — see the test below. It is allowed, but it
    # is not a read, and this test's NAME would have asserted that it is.
    "ls -la",
])
def test_reads_and_non_git_commands_are_allowed(parallel_clone, command):
    clone, _ = parallel_clone[:2]
    assert _decision(_run_hook(command, clone)) is None, command


@pytest.mark.parametrize("command", [
    "git restore seed.txt",
    "git restore --staged seed.txt",
    "git restore .",
])
def test_git_restore_is_ALLOWED_and_is_NOT_a_read(parallel_clone, command):
    """🔴 SPLIT OUT OF THE "reads" TEST, BECAUSE THE NAME WAS THE CLAIM AND IT WAS
    FALSE. `git restore seed.txt` sat in `test_reads_and_non_git_commands_are_allowed`
    — measured, it **overwrites the working-tree file**: an unsaved edit was replaced
    by the committed content, which is destroying work rather than reading.

    The verdict is unchanged and correct. `restore` is deliberately OUT of `_REFUSED`
    and has its own row in the doc's out-table: the ordinary form carries no `--`, so
    adding it to the ledger would refuse EVERY `git restore <path>` — including the
    single-file recovery the fleet rules prescribe in place of `git checkout --`. The
    same hazard the `checkout` pathspec exemption exists for, arriving from the other
    side.

    ⚠ INVARIANT GUARD — green at every ref. What moved is the NAME above it, which is
    the whole point: a test title is a claim, and this one asserted that a
    work-destroying command is a read.
    """
    clone = parallel_clone[0]
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
