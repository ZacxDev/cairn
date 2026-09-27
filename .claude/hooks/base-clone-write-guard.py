#!/usr/bin/env python3
"""PreToolUse guard: refuse tree-mutating git in this repo's SHARED base clone.

Concurrent sessions and dispatched agents work this repository at the same time,
through linked worktrees of ONE clone. The collision this guard exists to stop is
the quiet one: a `git commit` in the base clone while a peer session has that
clone checked out on its own branch. There is no conflict, no error, and
`git log` afterwards shows exactly what you expect — because you are reading the
branch you landed on. The recipe that avoids it, and the measurements behind each
rule, are in `claudedocs/working-in-parallel.md`; this file is the half that does
not depend on anybody reading prose.

🔴 IT FAILS **OPEN**, WHICH IS THE OPPOSITE OF THE OPERATOR'S HOST-WIDE
`bash-guard.py`, AND THE INVERSION IS DELIBERATE. That guard protects one
operator on two known hosts and fails CLOSED so a partial `home-manager switch`
cannot silently disarm it. This one is TRACKED IN A PUBLIC REPOSITORY, so it runs
on the machine of anyone who clones cairn and starts an agent session in it. A
guard that can wedge a stranger's checkout because its own `git` call behaved
unexpectedly is worse than no guard, so every unexpected condition here exits 0
and says nothing. Its whole job is to refuse a narrow, well-understood shape; it
is not a security boundary and must never behave like one.

Three conditions must ALL hold before it refuses anything, and the third is what
keeps it silent for people who are not us:

  1. the command mutates the working tree, the index or HEAD (see `_REFUSED`);
  2. the cwd is the clone's MAIN worktree, not a linked one;
  3. that clone has at least one LINKED worktree — i.e. somebody is actually
     working this repo in parallel right now.

A fresh clone has no linked worktrees, so condition 3 is false and an outside
contributor never sees this hook fire at all.

🔴 WHAT IS DELIBERATELY **NOT** REFUSED, because each is a documented recipe and a
guard that breaks one trains everybody to route around the guard:

  * `git merge --ff-only <ref>` — the base-clone RE-SYNC recipe. It cannot
    conflict or autostash: it either fast-forwards or refuses, and the refusal is
    the signal that the clone diverged. Refusing it here would break the one
    command that keeps a write-only base clone current.
  * `git checkout <ref> -- <paths>` and `git restore <paths>` — taking a ref's
    version of a file. The PATHSPEC form does not move HEAD, and it is how a
    session reads a doc at a ref. Bare `git checkout <branch>` IS refused,
    because that moves the shared HEAD under a peer.
  * every read: `log`, `status`, `diff`, `show`, `fetch`, `ls-files`,
    `rev-parse`, `worktree`, `branch`, `push`. Pushing from the base clone
    touches no file in it.

🔴 `git stash` IS IN THE REFUSED SET AND THAT IS NOT REDUNDANT WITH THE HOST
GUARD. `refs/stash` lives in the COMMON git dir, so the stack is shared by every
worktree of the clone — the hazard is not the base clone specifically. The
host-wide guard already denies it everywhere for that reason, and this entry
exists so the rule still holds on a machine that does not run the host guard,
which is every machine but the author's.

Contract (Claude Code `PreToolUse`): a JSON object on stdin carrying `tool_name`,
`tool_input.command` and `cwd`; refuse by printing
`hookSpecificOutput.permissionDecision = "deny"` and exiting 0. 🔴 Exit code 2 is
the ONLY status that blocks — every other non-zero status lets the command RUN —
so a crash in here is an ALLOW, which is exactly the fail-open behaviour this
file wants and the reason it never needs a non-zero exit.
"""
import json
import os
import re
import shlex
import subprocess
import sys
from typing import NoReturn

#: The doc every refusal points at. Kept as a constant because the message is the
#: ONLY routing this feature has: cairn's `AGENTS.md` sits three bytes under an
#: enforced ceiling, so there is no per-session prose pointing here.
DOC = "claudedocs/working-in-parallel.md"

#: Set this in the environment, or inline on the command, to proceed anyway.
OVERRIDE = "BASE_CLONE_WRITE_OK"

#: git subcommands that mutate the working tree, the index or HEAD.
#: 🔴 THIS SET IS A LEDGER AND `tests/test_base_clone_write_guard.py` PINS IT
#: AGAINST THE DOC, failing when it GROWS or SHRINKS. A subcommand added here
#: without the doc's table moving is a refusal nobody can look up; one removed
#: without the doc moving leaves the doc promising a guard that is gone.
_REFUSED = frozenset({
    "add",
    "am",
    "apply",
    "cherry-pick",
    "checkout",
    "commit",
    "merge",
    "rebase",
    "reset",
    "stash",
    "switch",
})


def _allow() -> NoReturn:
    """Say nothing and let the command run. Every non-refusal path ends here.

    🔴 THE `NoReturn` IS LOAD-BEARING DOCUMENTATION, NOT DECORATION, AND THE
    OPERATOR'S HOST GUARD RECORDS THE SAME LESSON BESIDE ITS OWN `_deny`. Without
    it a type checker reports every later read of `data`/`cwd` as "possibly
    unbound", because it cannot see that control does not come back from an
    early-exit — and in a guard whose whole contract is its exit behaviour, that
    diagnostic reads exactly like a fail-open bug. Measured here: annotating both
    exits cleared six Pyright errors and changed no behaviour.
    """
    sys.exit(0)


def _deny(reason: str) -> NoReturn:
    print(json.dumps({
        "hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "permissionDecision": "deny",
            "permissionDecisionReason": reason,
        }
    }))
    sys.exit(0)


def _git(cwd: str, *args: str) -> str | None:
    """One read-only git call. `None` on ANY failure — see the fail-open note."""
    try:
        out = subprocess.run(
            ("git", *args),
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=5,
            check=False,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if out.returncode != 0:
        return None
    return out.stdout.strip()


def _git_subcommands(command: str) -> list[str]:
    """Every git subcommand in a shell command line, in order.

    A single Bash call routinely chains several commands (`a && b; c | d`), so
    reading only the first word would miss `… && git commit …` — the exact shape
    a session uses. This splits on shell operators and reads the first word of
    each resulting simple command, skipping git's own global options and any
    leading `VAR=value` assignments.

    🔴 IT IS DELIBERATELY LEXICAL AND THEREFORE APPROXIMATE, WHICH THE FAIL-OPEN
    POSTURE MAKES ACCEPTABLE: `shlex` cannot see through a variable holding a
    subcommand, `eval`, or a function. Anything it cannot parse yields an empty
    list and the command is ALLOWED. This guard reduces a routine mistake; it
    does not contain an adversary, and the docstring says so rather than letting
    a reader infer a stronger property from a list of parsed operators.
    """
    try:
        parts = shlex.split(command, comments=True)
    except ValueError:
        return []

    found: list[str] = []
    # Operators that START a new simple command. `|&` and `&` are included so a
    # backgrounded `git commit` is still read.
    breaks = {"&&", "||", ";", "|", "|&", "&", "(", ")", "{", "}", "\n"}
    words: list[list[str]] = [[]]
    for part in parts:
        if part in breaks:
            words.append([])
        else:
            words[-1].append(part)

    for simple in words:
        # Drop leading `VAR=value` assignments and `env`, so
        # `BASE_CLONE_WRITE_OK=1 git commit` still parses as a git call.
        i = 0
        while i < len(simple) and (re.fullmatch(r"[A-Za-z_][A-Za-z_0-9]*=.*", simple[i])
                                   or simple[i] == "env"):
            i += 1
        if i >= len(simple) or os.path.basename(simple[i]) != "git":
            continue
        # Skip git's global options. The ones taking a VALUE must consume it too,
        # or `git -C <path> commit` reads `<path>` as the subcommand.
        j = i + 1
        takes_value = {"-C", "-c", "--git-dir", "--work-tree", "--namespace",
                       "--exec-path", "--config-env"}
        while j < len(simple):
            word = simple[j]
            if word in takes_value:
                j += 2
                continue
            if word.startswith("-"):
                j += 1
                continue
            break
        if j < len(simple):
            found.append(simple[j])
    return found


def _is_main_worktree(cwd: str) -> bool | None:
    """True when `cwd` is the clone's MAIN worktree, `None` when unknowable.

    The discriminator is git's own: in a linked worktree `--git-dir` resolves to
    `<common>/worktrees/<name>` while `--git-common-dir` resolves to `<common>`;
    in the main worktree the two are the SAME directory. Resolved with
    `realpath` because one side is routinely relative (`.git`) and the other
    absolute, so a string compare would report "different" for one directory.
    """
    git_dir = _git(cwd, "rev-parse", "--absolute-git-dir")
    common = _git(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
    if not git_dir or not common:
        return None
    try:
        return os.path.realpath(git_dir) == os.path.realpath(common)
    except OSError:
        return None


def _linked_worktrees(cwd: str) -> int | None:
    """How many LINKED worktrees this clone has (main worktree excluded)."""
    out = _git(cwd, "worktree", "list", "--porcelain")
    if out is None:
        return None
    return max(0, sum(1 for line in out.splitlines()
                      if line.startswith("worktree ")) - 1)


def _is_ff_only_merge(command: str) -> bool:
    """`git merge --ff-only …` — the documented base-clone re-sync, never refused."""
    return bool(re.search(r"\bgit\b[^&|;]*\bmerge\b[^&|;]*--ff-only", command))


def _is_stash_read(command: str) -> bool:
    """`git stash list` / `git stash show` — reads, and the standing rules say so.

    🔴 CAUGHT BY THIS FILE'S OWN TEST SUITE, NOT BY REVIEW. The first version read
    only the subcommand, so `git stash list` — which the fleet rules explicitly
    keep allowed, and which is the documented way to discover that the stack is
    shared before touching it — was refused. A guard that blocks the diagnostic
    for the hazard it is guarding is worse than one that blocks nothing.
    """
    for segment in re.split(r"&&|\|\||;|\|", command):
        if re.search(r"\bgit\b", segment) and re.search(r"\bstash\b", segment):
            if not re.search(r"\bstash\s+(list|show)\b", segment):
                return False
    return True


def _is_pathspec_checkout(command: str) -> bool:
    """`git checkout <ref> -- <paths>` / `git restore …` — takes a file, not a branch.

    The `--` is what makes it a pathspec form, and a pathspec checkout does not
    move HEAD. Matched per simple command so a chain cannot smuggle a bare
    `git checkout <branch>` past it on the strength of a sibling's `--`.
    """
    for segment in re.split(r"&&|\|\||;|\|", command):
        if re.search(r"\bgit\b", segment) and re.search(r"\bcheckout\b", segment):
            if " -- " not in segment:
                return False
    return True


def main() -> None:
    try:
        data = json.load(sys.stdin)
    except Exception:
        _allow()

    if not isinstance(data, dict) or data.get("tool_name") != "Bash":
        _allow()

    command = (data.get("tool_input") or {}).get("command", "")
    if not isinstance(command, str) or not command.strip():
        _allow()

    # The override is honoured from the hook's own environment AND from an inline
    # assignment on the command, because the inline form is the practical one for
    # a single deliberate write and the hook cannot see a future `export`.
    if os.environ.get(OVERRIDE) or re.search(rf"\b{OVERRIDE}=[^\s]", command):
        _allow()

    subcommands = _git_subcommands(command)
    hits = sorted(set(subcommands) & _REFUSED)
    if not hits:
        _allow()

    # Narrow the hits by the two documented exemptions before doing any git work,
    # so the common `fetch && merge --ff-only` re-sync costs nothing.
    if hits == ["merge"] and _is_ff_only_merge(command):
        _allow()
    if hits == ["checkout"] and _is_pathspec_checkout(command):
        _allow()
    if hits == ["stash"] and _is_stash_read(command):
        _allow()

    cwd = data.get("cwd")
    if not isinstance(cwd, str) or not os.path.isdir(cwd):
        _allow()

    if _is_main_worktree(cwd) is not True:
        _allow()

    linked = _linked_worktrees(cwd)
    if not linked:
        _allow()

    branch = _git(cwd, "branch", "--show-current") or "a detached HEAD"
    _deny(
        f"REFUSED: {cwd} is this repo's SHARED base clone, and {linked} linked "
        f"worktree(s) exist — so another session or agent is working here right "
        f"now. `git {', '.join(hits)}` mutates the tree, the index or HEAD that "
        f"peer is standing on. A commit landing on the wrong branch is the SILENT "
        f"failure: no conflict, no error, and `git log` afterwards shows what you "
        f"expect because you are reading the branch you landed on. "
        f"This clone is currently on `{branch}`.\n"
        f"\n"
        f"Do this instead — and note the worktree goes OUTSIDE the repo root, "
        f"because a nested checkout of cairn reds this repo's root-walking guards "
        f"and takes `tests/leakscan.py` to exit 2:\n"
        f"  git -C {cwd} fetch origin\n"
        f"  git -C {cwd} worktree add <scratchpad>/wt-<topic> -b <branch> origin/main\n"
        f"  # …edit, test and commit INSIDE that worktree…\n"
        f"  git -C <scratchpad>/wt-<topic> push -u origin HEAD:<branch>\n"
        f"  git -C {cwd} worktree remove <scratchpad>/wt-<topic>   # ONLY after the push SUCCEEDED\n"
        f"\n"
        f"Full rules, and the measurement behind each one: {DOC}\n"
        f"Deliberately doing this anyway: prefix the command with {OVERRIDE}=1."
    )


main()
