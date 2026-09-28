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

Four conditions must ALL hold before it refuses anything:

  1. the command mutates the working tree, the index or HEAD (see `_REFUSED`);
  2. the cwd's repository is THE ONE THIS FILE SHIPS IN;
  3. the cwd is that clone's MAIN worktree, not a linked one;
  4. that clone has at least one LINKED worktree REGISTRATION.

A fresh clone has no linked worktrees, so condition 4 is false and an outside
contributor never sees this hook fire at all.

🔴 CONDITION 2 EXISTS BECAUSE A ROUND-1 AUDIT CAUGHT THIS GUARD POLICING ANOTHER
REPOSITORY. Claude Code's Bash cwd persists across calls, so a session rooted in
cairn that moves into a sibling repo carried this hook there and refused
`git commit` in it — measured against a repo with 93 worktree registrations whose
OWN instructions declare that committing to its main branch IS deploying. The
refusal cited a doc path that does not exist there. That is the false-POSITIVE
mirror of the cwd narrowings listed at the bottom of this docstring, and it is
worse than them: a false negative loses a guard, a false positive countermands
another repo's documented workflow.

🔴 CONDITION 4 IS "HAS EVER HAD A WORKTREE NOBODY REMOVED", NOT "SOMEBODY IS
WORKING HERE NOW", AND AN EARLIER DRAFT OF THIS DOCSTRING CLAIMED THE LATTER.
`git worktree list` reports REGISTRATIONS, and an abandoned one is indistinguishable
here from a live peer: measured on the author's clone, 36 registrations of which
**two** belonged to a live session — the rest were long-dead scratchpads, some from
other repositories' session directories. Consequences, all accepted: the guard is
effectively always-on in a clone like that, and a stranger who once forgot a
`worktree remove` sees it fire forever. Liveness is NOT cheaply knowable — a pid
under the worktree is wrong (a read-only agent needs no worktree and a dead one
leaves the registration), so the honest fix was to describe the condition
correctly rather than to invent a liveness probe. The refusal message says
"registration(s)" for the same reason.

🔴 THE PARSER SEGMENTS THE **RAW STRING** BEFORE TOKENISING, AND THE FIRST VERSION
DID THE OPPOSITE — WHICH A ROUND-1 AUDIT WALKED FIVE DIFFERENT WAYS. It called
`shlex.split()` and then looked for operator TOKENS in the result. `shlex` does
not emit operators as tokens unless they are already space-separated, and it
treats a newline as ordinary whitespace, so every one of these hid the git write
completely and was MEASURED to pass straight through — the newline case proved
end to end, staging a file the single-line form was refused for:

    git status⏎git commit -m x        the newline is not a separator at all
    git fetch; git commit -m x        `fetch;` is one token, matching no operator
    git fetch&&git commit -m x        `fetch&&git` likewise
    (git commit -m x)                 `(git` is not the program name `git`
    false||git commit -m x            `false||git` likewise
    curl https://x/y#frag && git …    `comments=True` truncated at the `#`

`;` is the one operator a shell never requires whitespace around, and
`git fetch; git commit` is the idiomatic spelling — so the guard was walked by
most of the ways a session actually types. The repair is `shlex.shlex` with
`punctuation_chars=True`, which emits operators as their own tokens WHILE still
respecting quoting (`git commit -m 'a;b'` stays one argument), plus splitting on
newlines first and clearing `commenters`.

🔴 AND ONE SEGMENTATION SERVES BOTH READERS, BECAUSE TWO GRAMMARS OVER ONE
LANGUAGE IS HOW THE EXEMPTIONS GOT WALKED. There used to be a `breaks` set here
and a separate `re.split(r"&&|\\|\\||;|\\|", …)` inside each exemption. The set
contained `&` and the regex did not, so a single-`&` chain produced two refused
hits while each exemption saw ONE segment carrying its excusing flag — all three
exemptions walkable with one character. The suite could not see it because every
chain case used `&&`. `claude/RULES.md` is explicit: one predicate in two places
regenerates the same bug at both.

🔴 WHAT IS DELIBERATELY **NOT** REFUSED, and it is decided PER SEGMENT so that
chaining two exempt recipes is not refused either (it was, until round 1 —
`git stash list && git merge --ff-only origin/main` denied, because the exemptions
were gated on the hit set being a singleton):

  * `git merge --ff-only <ref>` — the base-clone RE-SYNC recipe. It cannot
    conflict or autostash: it either fast-forwards or refuses, and the refusal is
    the signal that the clone diverged. Refusing it here would break the one
    command that keeps a write-only base clone current.
  * `git checkout <ref> -- <paths>` — taking a ref's version of a file. The
    PATHSPEC form does not move HEAD, and it is how a session reads a doc at a
    ref. Bare `git checkout <branch>` IS refused, because that moves the shared
    HEAD under a peer. ⚠ `git restore` is NOT in `_REFUSED` and never reaches
    here; do not add it on the strength of this paragraph, because the ordinary
    `git restore <path>` form carries no `--` and would be refused wholesale.
  * `git stash list` / `show` / `--help` — reads.
  * every other read: `log`, `status`, `diff`, `show`, `fetch`, `ls-files`,
    `rev-parse`, `worktree`, `branch`, `push`. Pushing from the base clone
    touches no file in it.

⚠ `git stash` IS IN THE REFUSED SET AND ON THIS HOST IT IS A PURE DUPLICATE —
measured: the host-wide guard denies it too. `refs/stash` lives in the COMMON git
dir, so the stack is shared by every worktree of the clone and the hazard is not
the base clone specifically. 🔴 AN EARLIER DRAFT JUSTIFIED THE ENTRY AS HOLDING
"on a machine that does not run the host guard, which is every machine but the
author's", AND THAT IS STRUCTURALLY FALSE: such a machine is a fresh clone with no
linked worktrees, where condition 4 is false and this entry can never fire. The
only host it covers is one that BOTH lacks the fleet guard AND runs parallel
worktrees of cairn — a future fleet host, which is plausible enough to keep a
cheap table row for. Kept for that reason and no other.

🔴 WHAT THIS GUARD STILL CANNOT SEE, BECAUSE IT KEYS ON THE **cwd**. Naming these
is not optional: a guard whose limits are unstated reads as coverage it does not
have. All four were MEASURED to pass straight through:

  * `git -C <the base clone> commit …` issued from a linked worktree;
  * `cd <the base clone> && git commit …` likewise;
  * `git --git-dir=…/.git --work-tree=… commit …`;
  * `bash -c 'cd <the base clone> && git commit …'`.

⚠ THE FIRST TWO ARE THE SPELLING THIS REPO'S OWN RECIPE USES, so the gap is not
exotic. The operator's host-wide `guard_core.py` already resolves all four —
`_commands_with_cwd`, `_argv_named_repo_dirs`, `_git_dir_env_targets` — and on
`cd <clone> && git commit` that guard is therefore STRICTLY STRONGER than this
one. The right repair is to reuse that resolution rather than grow a second copy
of it here. Recorded as a known narrowing, not as done.

⚠ AND IT IS INERT IN THE OTHER RUNTIME. Only Claude Code reads
`.claude/settings.json`; the opencode plugin spawns `guard_core.py` and never
consults this file, so a rule the fleet states for BOTH runtimes is enforced here
in one. ⚠ It also costs a `python3` spawn on EVERY Bash call — measured ~23 ms
(independently re-measured at a 24.4 ms median), which roughly doubles per-call
guard latency in a clone where it is armed.

Contract (Claude Code `PreToolUse`): a JSON object on stdin carrying `tool_name`,
`tool_input.command` and `cwd`; refuse by printing
`hookSpecificOutput.permissionDecision = "deny"` and exiting 0. 🔴 Exit code 2 is
the ONLY status that blocks — every other non-zero status lets the command RUN —
so a crash in here is an ALLOW, which is exactly the fail-open behaviour this
file wants.
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

#: Set this in the environment, or as a LEADING assignment on a segment, to proceed.
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

#: git's own global options that consume a SEPARATE value, so
#: `git <opt> <value> commit` does not read `<value>` as the subcommand.
#: ⚠ NOT CLOSED, and a round-1 audit found two it was missing (`--attr-source`,
#: `--super-prefix`), each of which hid a `commit`. An unknown option that takes a
#: value still hides one; the fail-open posture makes that a gap rather than a
#: crash, and this comment says so instead of claiming completeness.
_GIT_GLOBALS_WITH_VALUE = frozenset({
    "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path",
    "--config-env", "--attr-source", "--super-prefix",
})

#: Shell operator tokens that END one simple command. `shlex` with
#: `punctuation_chars=True` emits these as their own tokens.
_OPERATOR_CHARS = set("();<>|&")


def _allow() -> NoReturn:
    """Say nothing and let the command run. Every non-refusal path ends here.

    🔴 THE `NoReturn` IS LOAD-BEARING DOCUMENTATION, NOT DECORATION, AND THE
    OPERATOR'S HOST GUARD RECORDS THE SAME LESSON BESIDE ITS OWN `_deny`. Without
    it a type checker reports every later read of `data`/`cwd` as "possibly
    unbound", because it cannot see that control does not come back from an
    early-exit — and in a guard whose whole contract is its exit behaviour, that
    diagnostic reads exactly like a fail-open bug.
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


def _shell_lines(command: str) -> list[str]:
    """The command's logical lines: newline-separated, but QUOTE- and HEREDOC-aware.

    🔴 A PLAIN `command.split("\\n")` WAS A FAIL-CLOSED REGRESSION, AND A ROUND-2
    AUDIT MEASURED IT. Splitting the raw string before knowing the quote state cuts
    through a quoted multi-line argument and a heredoc BODY, so their inner lines
    were read as commands:

        cat > /tmp/f <<'EOF'      →  the body line `git commit -m x` was REFUSED
        git commit -m x
        EOF

        echo 'line1              →  the middle line `git add .` was REFUSED
        git add .
        line3' > /tmp/x

    Every recipe in this repo's own docs is written one-command-per-line starting
    with `git`, so WRITING OR PRINTING one of those recipes inside the base clone
    was refused, with a message diagnosing it as a shared-tree mutation. That is
    the direction this file forbids itself, and it is the guard's own stated
    failure mode: break a documented recipe and people route around the guard.

    So the newline split happens here, over a walk that tracks quoting and
    heredocs, and only the resulting lines reach the lexer. A heredoc body is DATA
    and is dropped; the lines AFTER its terminator are commands again, which is the
    case a token-space skip gets wrong.
    """
    lines: list[str] = []
    current: list[str] = []
    pending: list[str] = []          # heredoc delimiters still awaited
    quote: str | None = None
    index, size = 0, len(command)

    def flush(line: str) -> None:
        if pending:
            # Inside a heredoc body: data, never a command. Only its terminator
            # is interesting, and only because it ends the body.
            if line.strip() == pending[0]:
                pending.pop(0)
            return
        opener = re.search(r"<<-?\s*(['\"]?)([A-Za-z_][A-Za-z_0-9]*)\1", line)
        if opener:
            pending.append(opener.group(2))
        lines.append(line)

    while index < size:
        char = command[index]
        if quote:
            current.append(char)
            if char == quote:
                quote = None
            index += 1
            continue
        if char in ("'", '"'):
            quote = char
            current.append(char)
            index += 1
            continue
        if char == "\\" and index + 1 < size:
            current.append(command[index:index + 2])
            index += 2
            continue
        if char == "\n":
            flush("".join(current))
            current = []
            index += 1
            continue
        current.append(char)
        index += 1
    flush("".join(current))
    return [line for line in lines if line.strip()]


def _segments(command: str) -> list[list[str]]:
    """Split a shell command into simple commands, as token lists.

    🔴 THE ONE SEGMENTATION IN THIS FILE. Both the refused-subcommand scan and
    every exemption read these same segments; the docstring above records what
    happened when there were two grammars over one language.

    Lines come from `_shell_lines`, then `punctuation_chars=True` emits shell
    operators as their own tokens while still honouring quotes, and `commenters`
    is cleared so a `#` inside a URL or a quoted string cannot truncate the line —
    bash would not treat it as a comment there, and a parser NARROWER than bash's
    is a parser that misses real commands.

    Returns `[]` for anything it cannot tokenise, which ALLOWS. That is the
    fail-open posture, and it is why this guard is described as reducing a routine
    mistake rather than containing an adversary.
    """
    out: list[list[str]] = []
    for line in _shell_lines(command):
        lexer = shlex.shlex(line, posix=True, punctuation_chars=True)
        lexer.whitespace_split = True
        lexer.commenters = ""
        current: list[str] = []
        try:
            for token in lexer:
                if token and all(ch in _OPERATOR_CHARS for ch in token):
                    out.append(current)
                    current = []
                else:
                    current.append(token)
        except ValueError:
            # An unbalanced quote. Keep whatever was read rather than discarding
            # the line: a partial parse can still carry a refused subcommand.
            pass
        out.append(current)
    return [segment for segment in out if segment]


#: Shell reserved words that can PRECEDE a command without being one.
#: 🔴 `{` COST A GUARD: `{ git commit -m x; }` passed straight through because the
#: program name was read as `{`. A round-2 audit measured it, and noted why it
#: reads as covered — the docstring lists the `(…)` twin among the closed walks,
#: and `(` IS closed, because it is an OPERATOR character while `{` is a reserved
#: WORD. The paren fix could never have covered it. `then`/`do`/`else` are here for
#: the same reason and close `if …; then git commit; fi` on one line, which was
#: missed before and after the rewrite.
_LEADING_RESERVED = frozenset({"{", "}", "!", "then", "do", "else", "elif", "time"})


def _leading_assignments(segment: list[str]) -> tuple[dict[str, str], int]:
    """`VAR=value` prefixes (and `env` with its own flags), and where argv starts."""
    assignments: dict[str, str] = {}
    i = 0
    while i < len(segment):
        word = segment[i]
        if word in _LEADING_RESERVED:
            i += 1
            continue
        match = re.fullmatch(r"([A-Za-z_][A-Za-z_0-9]*)=(.*)", word)
        if match:
            assignments[match.group(1)] = match.group(2)
            i += 1
            continue
        if os.path.basename(word) == "env":
            i += 1
            # `env -i`, `env -u NAME`, `env --`: consume env's own options so the
            # program name is found. A round-1 audit measured `env -i git commit`
            # and `env -u FOO git commit` both passing through.
            while i < len(segment) and segment[i].startswith("-"):
                if segment[i] in ("-u", "--unset"):
                    i += 2
                else:
                    i += 1
            continue
        break
    return assignments, i


def _git_subcommand(segment: list[str]) -> str | None:
    """The git subcommand this segment invokes, or `None` if it is not a git call."""
    _, i = _leading_assignments(segment)
    if i >= len(segment) or os.path.basename(segment[i]) != "git":
        return None
    j = i + 1
    while j < len(segment):
        word = segment[j]
        if word in _GIT_GLOBALS_WITH_VALUE:
            j += 2
            continue
        if word.startswith("-"):
            j += 1
            continue
        break
    return segment[j] if j < len(segment) else None


def _is_exempt(subcommand: str, segment: list[str]) -> bool:
    """Is this SEGMENT one of the documented recipes that must not be refused?

    Per segment, so chaining two exempt recipes is not refused — it was, while
    the exemptions were gated on the whole command yielding a single hit.
    """
    if subcommand == "merge":
        return "--ff-only" in segment
    if subcommand == "checkout":
        # The pathspec form. `--` is what makes it one, and it does not move HEAD.
        return "--" in segment
    if subcommand == "stash":
        rest = segment[segment.index("stash") + 1:]
        return bool(rest) and rest[0] in ("list", "show", "--help")
    return False


def _repo_root(path: str) -> str | None:
    """The COMMON git dir of the repository containing `path`, resolved."""
    common = _git(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
    if not common:
        return None
    try:
        return os.path.realpath(common)
    except OSError:
        return None


def _is_main_worktree(cwd: str) -> bool | None:
    """True when `cwd` is the clone's MAIN worktree, `None` when unknowable.

    The discriminator is git's own: in a linked worktree `--git-dir` resolves to
    `<common>/worktrees/<name>` while `--git-common-dir` resolves to `<common>`;
    in the main worktree the two are the SAME directory. Resolved with
    `realpath` because one side is routinely relative (`.git`) and the other
    absolute, so a string compare would report "different" for one directory.
    """
    git_dir = _git(cwd, "rev-parse", "--absolute-git-dir")
    common = _repo_root(cwd)
    if not git_dir or not common:
        return None
    try:
        return os.path.realpath(git_dir) == common
    except OSError:
        return None


def _linked_worktrees(cwd: str) -> int | None:
    """How many LINKED worktrees this clone has (main worktree excluded)."""
    out = _git(cwd, "worktree", "list", "--porcelain")
    if out is None:
        return None
    return max(0, sum(1 for line in out.splitlines()
                      if line.startswith("worktree ")) - 1)


def main() -> None:
    try:
        data = json.load(sys.stdin)
    except Exception:
        _allow()

    if not isinstance(data, dict) or data.get("tool_name") != "Bash":
        _allow()

    tool_input = data.get("tool_input")
    if not isinstance(tool_input, dict):
        _allow()
    command = tool_input.get("command", "")
    if not isinstance(command, str) or not command.strip():
        _allow()

    segments = _segments(command)

    # The override is honoured from the hook's own environment, and from a LEADING
    # assignment on any segment. 🔴 It used to be a bare `re.search` over the whole
    # command, which a round-1 audit walked three ways: an `echo` of the refusal
    # text, a `grep` for it, and `git commit -m 'BASE_CLONE_WRITE_OK=1'` — the
    # literal appears in this guard's OWN refusal message, so a session that
    # printed the refusal and retried in the same call disarmed the guard.
    if os.environ.get(OVERRIDE):
        _allow()
    for segment in segments:
        assignments, _ = _leading_assignments(segment)
        if assignments.get(OVERRIDE):
            _allow()

    hits: list[str] = []
    for segment in segments:
        subcommand = _git_subcommand(segment)
        if subcommand is None or subcommand not in _REFUSED:
            continue
        if _is_exempt(subcommand, segment):
            continue
        hits.append(subcommand)
    if not hits:
        _allow()

    cwd = data.get("cwd")
    if not isinstance(cwd, str) or not os.path.isdir(cwd):
        _allow()

    # Condition 2: this guard polices the repository it ships in, and no other.
    # `__file__` is `<repo>/.claude/hooks/<this>`, so the repo it belongs to is
    # the one containing this file.
    own_repo = _repo_root(os.path.dirname(os.path.abspath(__file__)))
    here = _repo_root(cwd)
    if not own_repo or not here or own_repo != here:
        _allow()

    if _is_main_worktree(cwd) is not True:
        _allow()

    linked = _linked_worktrees(cwd)
    if not linked:
        _allow()

    branch = _git(cwd, "branch", "--show-current") or "a detached HEAD"
    ordered = sorted(set(hits))
    _deny(
        f"REFUSED: {cwd} is this repo's SHARED base clone, and it carries "
        f"{linked} linked worktree registration(s) — so this tree may be shared "
        f"with another session or agent. `git {', '.join(ordered)}` mutates the "
        f"tree, the index or HEAD that a peer would be standing on. A commit "
        f"landing on the wrong branch is the SILENT failure: no conflict, no "
        f"error, and `git log` afterwards shows what you expect because you are "
        f"reading the branch you landed on. "
        f"This clone is currently on `{branch}`.\n"
        f"\n"
        f"Do this instead:\n"
        f"  git -C {cwd} fetch origin\n"
        f"  git -C {cwd} worktree add <a path> -b <branch> origin/main\n"
        f"  # …edit, test and commit INSIDE that worktree…\n"
        f"  git -C <that path> push -u origin HEAD:<branch>\n"
        f"  git -C {cwd} worktree remove <that path>   # ONLY after the push SUCCEEDED\n"
        f"\n"
        f"Full rules, and the measurement behind each one: {DOC}\n"
        f"Deliberately doing this anyway: put {OVERRIDE}=1 in front of the command."
    )


main()
