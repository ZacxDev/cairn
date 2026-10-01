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

Three conditions must ALL hold before it refuses anything, and they are asked
about the directory THE COMMAND WRITES TO rather than the one the shell happens
to be standing in (`_judged_dirs`):

  1. the command mutates the working tree, the index or HEAD (see `_REFUSED`);
  2. a directory that command writes to belongs to THE REPOSITORY THIS FILE
     SHIPS IN, and is that clone's MAIN worktree rather than a linked one;
  3. that clone has at least one LINKED worktree REGISTRATION.

A fresh clone has no linked worktrees, so condition 3 is false and an outside
contributor never sees this hook fire at all.

🔴 CONDITION 2 IS ABOUT THE **TARGET**, AND EVERY EARLIER VERSION ASKED IT ABOUT
THE **cwd** — WHICH WAS WRONG IN BOTH DIRECTIONS AT ONCE. Claude Code's Bash cwd
persists across calls, so a session rooted in cairn that moves into a sibling
repo carried this hook there and refused `git commit` in it — measured against a
repo with 93 worktree registrations whose OWN instructions declare that
committing to its main branch IS deploying, and the refusal cited a doc path that
does not exist there. The mirror image was measured too: a session rooted in the
base clone was refused for `git -C <a linked worktree> commit`, which is the
spelling this guard's own refusal message PRESCRIBES, and a cross-repo commit it
blocked outright had to be handed back to the operator. Both are the same defect
— the cwd is not the write target — so both close together here, and neither is
the worse half. A false negative loses a guard; a false positive countermands
another repo's documented workflow and teaches everyone the override.

🔴 CONDITION 3 IS "HAS EVER HAD A WORKTREE NOBODY REMOVED", NOT "SOMEBODY IS
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
linked worktrees, where condition 3 is false and this entry can never fire. The
only host it covers is one that BOTH lacks the fleet guard AND runs parallel
worktrees of cairn — a future fleet host, which is plausible enough to keep a
cheap table row for. Kept for that reason and no other.

🔴 WHAT THIS GUARD NOW SEES, AND WHAT IT STILL CANNOT. Naming the second list is
not optional: a guard whose limits are unstated reads as coverage it does not
have. This table was once four open items, every one MEASURED passing straight
through; three are CLOSED and the entry that closed each is named so a reader can
check the claim rather than take it:

  * `git -C <the base clone> commit …` from a linked worktree — CLOSED, the `-C`
    chain is resolved and judged (`_redirect_targets`);
  * `git --git-dir=…/.git commit …` / `--work-tree=…` — CLOSED, both spellings
    and both separators, judged IN ADDITION to the caller's directory;
  * `GIT_DIR=…/.git git commit …` — CLOSED, from a leading assignment on any
    segment and from this hook's own environment;
  * `cd <the base clone> && git commit …` — CLOSED in the UNDER-blocking
    direction only, and the asymmetry is deliberate: a `cd` target is judged IN
    ADDITION, never instead of the caller's directory, because deciding that a
    `cd` REPLACES it needs bash's positional model (`( … )` does not persist,
    `{ … }` does) and a wrong model there fails OPEN. So `cd <clone> && git
    commit` is refused, while the mirror — `cd <a worktree> && git commit` from
    the clone — is still refused too, as it was before. Pass `-C` instead.
  * `bash -c 'cd <the base clone> && git commit …'` — STILL OPEN. The inner
    script is one quoted token, so nothing in it is parsed as a command. Closing
    it means recursing into nested shells, which is where the host-wide guard
    spends two separate recursion budgets.

⚠ AND A FIFTH ROW IS CLOSED IN THE ONLY DIRECTION THAT IS SAFE: `git -C "$WT" …`
IS REFUSED, NEVER RESOLVED. A `$VAR` target is one this guard cannot follow, so
the caller's directory is judged and the refusal stands. That costs ergonomics on
the spelling this repo's own recipe is written in, and the cost is paid
deliberately — the comment above `_abs_path` carries the four fail-opens that a
resolver for it opened, all four measured ALLOW where a bare commit in the clone
is DENY. **The remedy is an absolute path, and the refusal message says so.**

🔴 AND "REUSE THE HOST GUARD'S RESOLUTION" — WHICH THIS DOCSTRING TOLD TWO
EARLIER ROUNDS TO DO — IS NOT AVAILABLE, SO SAYING IT WAS THE ADVICE THAT KEPT
THE GAP OPEN. `guard_core.py` lives in the operator's `~/.claude/hooks/`, is not
tracked here, and ships to two known hosts; THIS file is tracked in a PUBLIC
repository and must run on a stranger's clone with nothing beside it. An import
of it would be a missing module on every other machine — and a `PreToolUse` hook
that fails to start is silently an ALLOW, so the guard would go inert exactly
where it is the only one present. The resolution below is therefore a second
implementation on purpose. It is deliberately much smaller than the host guard's:
it resolves the four REDIRECTION spellings and no variables, reads no sourced
files, and treats everything it cannot resolve as "judge the caller's directory
too" — the fail-CLOSED fallback, which is what lets it stay small.

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

#: git's global options that NAME a repository or a work tree. 🔴 THEY ARE JUDGED
#: IN ADDITION TO THE CALLER'S DIRECTORY, NEVER INSTEAD OF IT, AND THAT IS
#: MEASURED RATHER THAN CAUTIOUS. From the main worktree,
#: `git --work-tree=<a linked worktree> rev-parse --absolute-git-dir` answers the
#: MAIN clone's `.git` — the flag moves where the FILES are read, not which index
#: and HEAD get written — and `git --git-dir=<the clone>/.git add <file>` run from
#: a linked worktree really does stage into the clone (rc 0, measured). So each of
#: these names one half of the operation while the other half still comes from the
#: caller, and suppressing the caller's directory on their authority would be
#: fail-OPEN. Only `-C` replaces it; see `_judged_dirs`.
_GIT_REPO_OPTS = ("--git-dir", "--work-tree")

#: The two environment variables that override git's directory discovery, with
#: the same standing as the flags above: judged in addition, never instead.
_GIT_DIR_ENV_NAMES = ("GIT_DIR", "GIT_WORK_TREE")

#: Shell builtins that move the caller's directory. Read as ADDITIONAL judged
#: directories only — the docstring's table says why a replacement needs bash's
#: positional model and why guessing it fails open.
_CHDIR_BUILTINS = frozenset({"cd", "pushd"})

#: Upper bound on how many distinct ADDITIVE directories one Bash call may make
#: this hook ask `git` about: the `--git-dir` / `--work-tree` / `GIT_DIR` / `cd`
#: targets, and those only. A command text carrying a hundred `--git-dir=` flags
#: must not turn a per-Bash-call hook into a hundred `git` spawns.
#:
#: 🔴 IT IS SCOPED TO THE ADDITIVE PASS BECAUSE A GLOBAL BUDGET WAS A MEASURED
#: BYPASS, AND THE COMMENT THAT STOOD HERE CLAIMED OTHERWISE. It said a truncation
#: "can lose only the additive coverage added here — never the directory every
#: earlier version of this guard judged". False: the counter was shared by both
#: passes, so PRIMARIES crowded out primaries. Eight `git -C <a real linked
#: worktree> add` segments — each a legitimate, resolvable, trusted redirect —
#: followed by a bare `git commit` IN THE CLONE was **ALLOW**, where the base ref
#: denies it. Bisected on one fixture: DENY for N ≤ 7, ALLOW for N ≥ 8, exactly the
#: bound. Padding with work the guard is SUPPOSED to permit bought an exemption
#: for the write it is supposed to refuse.
#:
#: So the primary directory of every candidate segment is now ALWAYS probed, with
#: no cap. That is unbounded in principle, and it is the same exposure the base ref
#: carries (it, too, probed per candidate); `_ROOTS` and `_PROBED` memoise per
#: path, so the cost is distinct directories rather than segments. A cap belongs on
#: the half that cannot change a refusal into an allow, which is the additive half
#: alone.
_MAX_PROBED_DIRS = 8


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


#: Environment variables that steer git's repository DISCOVERY, scrubbed from
#: every read this guard makes. 🔴 NOT HYGIENE — A MEASURED FAIL-OPEN. The guard's
#: question is "what is THIS DIRECTORY", and `_git` inherited the hook's
#: environment, so a `GIT_DIR` already exported in the session answered for it
#: instead. Measured on a miniature clone, with the no-variable control DENYing in
#: the same run: `GIT_DIR=<a linked worktree's git dir>` in the hook's environment
#: made a bare `git commit` IN THE BASE CLONE **allow** — every directory looks
#: like a linked worktree, so condition 2 is false everywhere. Both base refs of
#: this change had it. The variables are not ignored, they are judged explicitly
#: instead (`_ambient_targets`), which is the only reading under which the guard's
#: own reads and the command's targets cannot disagree.
#: ⚠ NOT CLOSED, the same caveat `_GIT_GLOBALS_WITH_VALUE` carries: a discovery
#: variable missing from this set steers the reads again. The fail-open posture
#: makes that a gap rather than a crash, and `_ROOTS`/`_PROBED` cache per path, so
#: adding a name here costs nothing.
_GIT_DISCOVERY_ENV = (
    "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE",
    "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
    "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM",
)


def _git(cwd: str, *args: str) -> str | None:
    """One read-only git call. `None` on ANY failure — see the fail-open note."""
    env = {k: v for k, v in os.environ.items() if k not in _GIT_DISCOVERY_ENV}
    try:
        out = subprocess.run(
            ("git", *args),
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=5,
            check=False,
            env=env,
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


#: Memo for `_repo_root`. It is asked about every `-C` target as well as every
#: probed directory, and a command line may carry many of each; without this the
#: `_MAX_PROBED_DIRS` budget would bound only half the `git` spawns.
_ROOTS: dict[str, str | None] = {}


def _repo_root(path: str) -> str | None:
    """The COMMON git dir of the repository containing `path`, resolved.

    🔴 THIS ANSWER IDENTIFIES THE CLONE AND NOT THE WORKTREE, which is exactly
    why it cannot be the whole test: a linked worktree and the main worktree of
    one clone give the SAME common dir. `_protected` pairs it with
    `_is_main_worktree` for that reason.
    """
    if path in _ROOTS:
        return _ROOTS[path]
    common = _git(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
    result: str | None = None
    if common:
        try:
            result = os.path.realpath(common)
        except OSError:
            result = None
    _ROOTS[path] = result
    return result


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


#: 🔴 THERE IS NO VARIABLE EXPANSION HERE, AND A VERSION OF THIS FILE THAT HAD IT
#: OPENED FOUR FAIL-OPENS AN AUDIT MEASURED. The reasoning that produced it was:
#: an unresolvable `-C "$WT"` falls back to judging the caller's directory, which
#: is correct but annoying, so resolve what the command text itself assigns. The
#: defect is that KNOWING A NAME IS ASSIGNED SOMEWHERE IN THE TEXT IS NOT KNOWING
#: THE SHELL WILL HAVE ASSIGNED IT. In each of these bash leaves `WT` **unset**,
#: so the command git actually runs is `git -C "" commit` — and git runs that in
#: the CURRENT directory, the clone:
#:
#:     ( WT=<wt> ) ; git -C "$WT" commit       a subshell assignment is discarded
#:     false && WT=<wt> ; git -C "$WT" commit   the assignment never runs
#:     if false; then WT=<wt>; fi; git -C …     nor does one in an untaken branch
#:     WT=<wt> true; git -C "$WT" commit        a command PREFIX scopes to `true`
#:
#: All four were ALLOW with expansion and DENY without it, re-measured on a
#: miniature clone with a bare `git commit` in the clone DENYing as the control in
#: the same run; the first was proved end to end, the clone going 1 -> 2 commits
#: while the worktree stayed at 1. That is precisely the silent wrong-branch commit
#: this hook exists to prevent.
#:
#: 🔴 AND NOTE WHAT THE EXPANSION DID TO THE FALLBACK: it did not relax it, it made
#: it UNREACHABLE in those four shapes — honoured formally, not substantively. A
#: rule obeyed to the letter while its purpose is defeated is the harder failure to
#: see, which is why this comment is long and the code is gone.
#:
#: It was also NARROWER than intended: `export WT=…; git -C "$WT" add` stayed
#: refused, because the assignment walk does not model `export`. So it bought
#: ergonomics in the shapes where it was WRONG and not in the shape a careful
#: script uses.
#:
#: The replacement is not a better parser, it is PROSE: an unresolvable target
#: refuses, and the refusal names the remedy — pass `-C` an ABSOLUTE path. Modelling
#: shell scope in Python inside a security path is a cost with no measured symptom
#: behind it: the requirement traced to a report whose own repro assigned the
#: variable in a PREVIOUS Bash call, which no parser here could ever resolve.


def _abs_path(value: str, base: str) -> str | None:
    """A path a command NAMES, made absolute against `base`. `None` if unusable.

    🔴 IT DOES NOT ASK WHETHER THE DIRECTORY EXISTS, AND A DRAFT THAT DID WAS A
    SECOND GUARD SCORED **UNREACHABLE** BY THE SAME MUTATION SWEEP. `_protected`
    has to make that check anyway — it is the function that decides whether a
    directory is the base clone — so a second copy here could never change a
    verdict, and this file's own rule is that one predicate in two places
    regenerates the same bug at both. The existence check now lives ONCE, in
    `_protected`, ahead of the probe budget so a junk path costs nothing.

    `~` is expanded because the lexer hands the tilde through literally while bash
    would have expanded it, and a parser NARROWER than the shell's is one that
    misses real commands — the same reasoning that cleared `commenters`.
    """
    if not value or value.startswith("-"):
        return None
    try:
        return os.path.normpath(os.path.join(base, os.path.expanduser(value)))
    except (OSError, ValueError):
        return None


def _redirect_targets(segment: list[str], cwd: str,
                      ) -> tuple[str | None, list[str]]:
    """`(the directory this git call RUNS IN, the directories it NAMES)`.

    The first element is the cumulative `-C` result, or `None` when the segment
    carries no `-C` at all — which the caller must be able to tell apart from "a
    `-C` that happens to resolve to `cwd`". The second is every `--git-dir` /
    `--work-tree` value, in both the spaced and the attached `=` spelling.

    ⚠ A `$VAR` IS NOT RESOLVED, AND THAT IS A DECISION WITH A MEASUREMENT BEHIND
    IT RATHER THAN AN OMISSION — the long comment above `_abs_path` carries the
    four fail-opens a resolver opened. `git -C "$WT" commit` therefore names a
    target this guard cannot follow, so the caller's directory is judged and the
    refusal stands. The remedy is in the refusal message: an ABSOLUTE path.

    🔴 THE `-C` CHAIN IS CUMULATIVE, BECAUSE GIT IS. `git -C a -C b` is
    `cd a; cd b` — each `-C` is relative to the one before it, so taking only the
    LAST is wrong whenever the last is relative.

    🔴 AND THE NAMED DIRECTORIES RESOLVE AGAINST THE `-C` RESULT, NOT AGAINST THE
    CALLER — MEASURED, because the other order is just as plausible. From a cwd of
    `<M>`, `git -C clone --git-dir=.git rev-parse --absolute-git-dir` answers
    `<M>/clone/.git`: git applies every `-C` first and only then interprets
    `--git-dir`. `GIT_DIR` behaves the same way.

    ⚠ THE ATTACHED `-C<path>` BRANCH IS A HEDGE, NOT A LIVE PATH, AND MEASURING IT
    IS WHY THAT IS WRITTEN DOWN: git 2.55.0 answers `unknown option: -Cnope` and
    exits 129, so no attached redirect ever runs. It is read anyway because a
    reader comparing this walk against `_git_subcommand`'s would otherwise have to
    guess whether the omission was deliberate, and because the cost of reading it
    is one branch.
    """
    _, i = _leading_assignments(segment)
    j = i + 1
    here: str | None = None
    named: list[str] = []
    while j < len(segment):
        word = segment[j]
        hop: str | None = None
        if word == "-C" and j + 1 < len(segment):
            hop, j = segment[j + 1], j + 2
        elif word.startswith("-C") and len(word) > 2:
            hop, j = word[2:], j + 1
        if hop is not None:
            # 🔴 `normpath`, not `_abs_path`: an intermediate hop in the chain is a
            # directory to resolve the NEXT one against, and demanding that each
            # link exist would drop a chain whose final link does. A hop carrying
            # an unexpanded `$VAR` therefore produces a path that is not a
            # repository, which `_judged_dirs` turns into "judge the caller too".
            here = os.path.normpath(
                os.path.join(here or cwd, os.path.expanduser(hop)))
            continue
        for opt in _GIT_REPO_OPTS:
            if word == opt and j + 1 < len(segment):
                named.append(segment[j + 1])
            elif word.startswith(opt + "="):
                named.append(word.split("=", 1)[1])
        if word in _GIT_GLOBALS_WITH_VALUE:
            j += 2
            continue
        if word.startswith("-"):
            j += 1
            continue
        break
    base = here or cwd
    return here, [d for d in (_abs_path(v, base) for v in named) if d]


def _ambient_targets(segments: list[list[str]], cwd: str,
                     ) -> tuple[list[str], list[str]]:
    """`(directories a GIT_DIR-family variable names, directories a `cd` names)`.

    Both are judged IN ADDITION to whatever a git segment names for itself,
    because both govern a later BARE `git commit` carrying no flag of its own —
    but they are returned SEPARATELY because they lose to a `-C` differently:

      * a `GIT_DIR=` / `GIT_WORK_TREE=` value, from an assignment anywhere or from
        this hook's OWN environment, OVERRIDES a `-C`: measured, `git -C <clone>`
        with `GIT_DIR=.git` resolves the git dir from the variable, so the variable
        is judged for EVERY candidate segment;
      * a `cd` / `pushd` target is only the shell's directory, which a `-C`
        supersedes entirely. So it is judged only for segments that do NOT resolve
        a `-C` of their own. Without that split, `cd <the clone> && git -C <a
        worktree> commit` would be refused on the strength of a `cd` git never
        consults — a false positive invented by the fix for a false negative.

    🔴 THIS IS A WHOLE-COMMAND SCAN AND THE OVERRIDE'S HISTORY IS NOT A REASON TO
    NARROW IT. `OVERRIDE` was a whole-text `re.search` once and three audits walked
    it, so the shape reads as the known defect — but that scan decided an ALLOW, so
    a spurious match was fail-OPEN. This one decides a REFUSAL, so a spurious match
    is fail-CLOSED, and the two are opposite in exactly the dimension that mattered.
    It still reads TOKENS from the one `_segments` parse rather than the raw string:
    heredoc bodies are already dropped there, and this file's own rule is that two
    grammars over one language is how the exemptions got walked.
    """
    env_dirs: list[str] = []
    cd_dirs: list[str] = []
    for name in _GIT_DIR_ENV_NAMES:
        resolved = _abs_path(os.environ.get(name, ""), cwd)
        if resolved:
            env_dirs.append(resolved)
    for segment in segments:
        for word in segment:
            for name in _GIT_DIR_ENV_NAMES:
                if word.startswith(name + "="):
                    resolved = _abs_path(word.split("=", 1)[1], cwd)
                    if resolved:
                        env_dirs.append(resolved)
        _, i = _leading_assignments(segment)
        if i < len(segment) and os.path.basename(segment[i]) in _CHDIR_BUILTINS:
            for word in segment[i + 1:]:
                if word.startswith("-"):
                    continue
                resolved = _abs_path(word, cwd)
                if resolved:
                    cd_dirs.append(resolved)
                break
    return env_dirs, cd_dirs


def _judged_dirs(segment: list[str], cwd: str,
                 env_dirs: list[str], cd_dirs: list[str]) -> list[str]:
    """Every directory a write in this segment could land in, primary one FIRST.

    🔴 `-C` IS THE ONLY SPELLING THAT REPLACES THE CALLER'S DIRECTORY. It means
    "run git as if the cwd were this", so git's whole repository discovery starts
    there and nothing of the caller's is left — measured: from a linked worktree,
    `git -C <the main worktree> rev-parse --absolute-git-dir` answers the clone's
    `.git` and `git -C <a linked worktree> …` answers that worktree's. Every other
    redirection spelling leaves one half of the operation with the caller, so each
    is additive; `_GIT_REPO_OPTS` carries the measurement.

    🔴 AND IT REPLACES ONLY WHEN IT RESOLVES TO A GIT WORKTREE. A `-C` the guard
    cannot follow — a typo, an unexpanded `$VAR`, a path the command is about to
    create, a directory that is no repository at all — must never buy an
    exemption, so the caller's directory is judged instead and the refusal stands.
    The host-wide guard records the same rule from a measured fail-OPEN: naming a
    repo handed it the whole verdict, so a `-C` at an ordinary directory left the
    branch check evaluating NOTHING.

    🔴 AND A RESOLVED `-C` DROPS THE `cd` TARGETS WHILE KEEPING THE GIT_DIR ONES.
    `_ambient_targets` carries the measurement; the short form is that a `cd` only
    moves the shell, which a `-C` then overrides, while `GIT_DIR` overrides the
    `-C` in turn.
    """
    here, named = _redirect_targets(segment, cwd)
    trusted = bool(here) and _repo_root(here) is not None
    primary = here if trusted else cwd
    extra = list(named) + env_dirs + ([] if trusted else cd_dirs)
    out: list[str] = []
    for path in (primary, *extra):
        if path and path not in out:
            out.append(path)
    return out


def _linked_worktrees(cwd: str) -> int | None:
    """How many LINKED worktrees this clone has (main worktree excluded)."""
    out = _git(cwd, "worktree", "list", "--porcelain")
    if out is None:
        return None
    return max(0, sum(1 for line in out.splitlines()
                      if line.startswith("worktree ")) - 1)


def _main_worktree_path(cwd: str) -> str | None:
    """The clone's MAIN worktree path, read from git rather than derived.

    `git worktree list --porcelain` lists the main worktree FIRST (measured, from
    the main worktree, a linked one, the git dir and a linked worktree's git dir —
    same answer from all four). Read because the refusal message has to name a
    directory a reader can `cd` into, and a judged target may be a `.git` dir,
    where `rev-parse --show-toplevel` is a fatal rather than an answer.
    """
    out = _git(cwd, "worktree", "list", "--porcelain")
    for line in (out or "").splitlines():
        if line.startswith("worktree "):
            return line[len("worktree "):].strip()
    return None


#: Memo for `_protected`, keyed on the directory asked about. One Bash call can
#: name the same directory from several segments, and each probe is up to three
#: `git` spawns.
_PROBED: dict[str, tuple[str, int] | None] = {}

#: How many ADDITIVE probes this process has spent. Counted separately from
#: `_PROBED` because a primary probe must never consume the additive budget — see
#: `_MAX_PROBED_DIRS` for the bypass that taught the distinction.
_ADDITIVE_PROBES = 0


def _protected(path: str, own_repo: str,
               budgeted: bool = False) -> tuple[str, int] | None:
    """`(the clone's main worktree, its linked-worktree count)` when a write into
    `path` lands in the SHARED BASE CLONE of the repository this file ships in.
    `None` for every other directory, and for every question git cannot answer.

    This is conditions 2 and 3 in one place, asked about ONE directory, so the
    only thing that changed when the guard stopped keying on the cwd is which
    directories get passed in. The two git questions are different and the
    distinction is the whole trap: `--git-common-dir` identifies the CLONE and is
    therefore IDENTICAL for the main worktree and every linked worktree of it, so
    a check built on it alone re-creates the false positive this resolution exists
    to remove. `--absolute-git-dir` is what separates them — `<common>` in the
    main worktree, `<common>/worktrees/<name>` in a linked one.

    ⚠ `--show-toplevel` IS REJECTED FOR TWO DIFFERENT REASONS AND AN EARLIER DRAFT
    OF THIS COMMENT MERGED THEM INTO ONE WRONG ONE. It said the flag form is
    "fatal when the target is a git dir". Measured: `git --git-dir=<clone>/.git
    rev-parse --show-toplevel` from a linked worktree exits **0** and answers the
    WORKTREE — a confidently wrong answer, which is worse than a fatal. The fatal
    belongs to the probe form this guard would have to use, `git -C <a git dir>
    rev-parse --show-toplevel` → rc 128 `must be run in a work tree`. Two
    independent disqualifications; the conclusion was right and the attribution
    was not. And it answers neither question anyway: a worktree root does not say
    whether it is the MAIN one, so it would need the comparison above regardless.

    A `path` that IS a git dir is answered correctly by both questions (measured
    for `<clone>/.git` and `<common>/worktrees/<name>`), which is what lets one
    probe serve a worktree path and a `--git-dir` value alike.
    """
    global _ADDITIVE_PROBES
    # 🔴 THE ONE EXISTENCE CHECK, AND IT COMES BEFORE THE BUDGET. This hook runs
    # BEFORE the command does, so a directory the command is about to create does
    # not exist yet; a path that is not a directory now cannot be the base clone,
    # and spending probe budget on it would let a line full of junk
    # `--git-dir=` values crowd a REAL additive candidate out of the verdict.
    # Not memoised either: `os.path.isdir` is a stat, not a `git` spawn.
    if not os.path.isdir(path):
        return None
    if path in _PROBED:
        return _PROBED[path]
    if budgeted:
        # Exhaustion returns without writing `_PROBED`, so the budget can never
        # record a verdict — only decline to compute one. ⚠ THAT IS BELT AND
        # BRACES, NOT A LIVE GUARANTEE, and saying so is the point: with the
        # passes ordered unbudgeted-first, every primary is probed before any
        # budget can run out, so memoising the exhaustion here would change no
        # answer today. It is written this way so that reordering the passes
        # cannot quietly turn a cost control into a verdict.
        if _ADDITIVE_PROBES >= _MAX_PROBED_DIRS:
            return None
        _ADDITIVE_PROBES += 1
    result: tuple[str, int] | None = None
    if _repo_root(path) == own_repo and _is_main_worktree(path) is True:
        linked = _linked_worktrees(path)
        if linked:
            result = (_main_worktree_path(path) or path, linked)
    _PROBED[path] = result
    return result


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

    # 🔴 THE CHEAP, STRING-ONLY PASS COMES FIRST AND THAT ORDERING IS DELIBERATE.
    # This hook costs a `python3` spawn on EVERY Bash call, so the refused-set
    # match — pure parsing, no subprocess — decides early-out for the overwhelming
    # majority of commands. The `git` calls below run only for a command that is
    # ALREADY a candidate for refusal. The segment is carried alongside its
    # subcommand because the target resolution needs the argv, not just the verb.
    candidates: list[tuple[str, list[str], int]] = []
    for index, segment in enumerate(segments):
        subcommand = _git_subcommand(segment)
        if subcommand is None or subcommand not in _REFUSED:
            continue
        if _is_exempt(subcommand, segment):
            continue
        candidates.append((subcommand, segment, index))
    if not candidates:
        _allow()

    cwd = data.get("cwd")
    if not isinstance(cwd, str) or not os.path.isdir(cwd):
        _allow()

    # Conditions 2 and 3 are `_protected`, and this is the only place that knows
    # which repository "this repo" is. `__file__` is `<repo>/.claude/hooks/<this>`,
    # so the repo it belongs to is the one containing this file — read ONCE, and
    # never from the cwd, which is what let a wandering session carry the guard
    # into a sibling repository.
    own_repo = _repo_root(os.path.dirname(os.path.abspath(__file__)))
    if not own_repo:
        _allow()

    # 🔴 TWO PASSES, AND THE SPLIT IS WHAT MAKES THE PROBE BUDGET SAFE RATHER THAN
    # MERELY TIDY. Pass one asks about the directory each git call actually RUNS IN
    # — its `-C` target, or the caller's — which is the only directory any earlier
    # version of this guard judged, and it is UNBUDGETED. Pass two asks about the
    # ADDITIVE ones (`--git-dir`, `--work-tree`, `GIT_DIR`, a `cd`) and is the only
    # pass a cap applies to.
    #
    # An earlier draft shared one budget across both and ordered the passes to
    # compensate. Ordering is not a guarantee: the counter was global, so eight
    # legitimate `git -C <a real linked worktree> add` segments exhausted it and a
    # bare `git commit` IN THE CLONE on the same line was ALLOWED. Padding with
    # permitted work bought an exemption for a refused write. `_MAX_PROBED_DIRS`
    # carries the bisect.
    env_dirs, cd_dirs = _ambient_targets(segments, cwd)
    plans = [(sub, _judged_dirs(segment, cwd, env_dirs, cd_dirs))
             for sub, segment, _ in candidates]

    hits: list[str] = []
    found: tuple[str, int] | None = None
    for budgeted in (False, True):
        for subcommand, dirs in plans:
            for path in (dirs[1:] if budgeted else dirs[:1]):
                verdict = _protected(path, own_repo, budgeted)
                if verdict:
                    hits.append(subcommand)
                    found = found or verdict
                    break
        if hits:
            break
    if not hits or not found:
        _allow()

    clone, linked = found
    branch = _git(clone, "branch", "--show-current") or "a detached HEAD"
    ordered = sorted(set(hits))
    _deny(
        f"REFUSED: {clone} is this repo's SHARED base clone, and it carries "
        f"{linked} linked worktree registration(s) — so this tree may be shared "
        f"with another session or agent. `git {', '.join(ordered)}` mutates the "
        f"tree, the index or HEAD that a peer would be standing on. A commit "
        f"landing on the wrong branch is the SILENT failure: no conflict, no "
        f"error, and `git log` afterwards shows what you expect because you are "
        f"reading the branch you landed on. "
        f"This clone is currently on `{branch}`.\n"
        f"\n"
        f"Do this instead:\n"
        f"  git -C {clone} fetch origin\n"
        f"  git -C {clone} worktree add <a path> -b <branch> origin/main\n"
        f"  # …edit, test and commit INSIDE that worktree…\n"
        f"  git -C <that path> push -u origin HEAD:<branch>\n"
        f"  git -C {clone} worktree remove <that path>   # ONLY after the push SUCCEEDED\n"
        f"\n"
        f"The directory judged was the one this command WRITES to, not the shell's:\n"
        f"a `git -C <a linked worktree> …` from here is NOT refused, and a\n"
        f"`-C` / `--git-dir` / `GIT_DIR` pointing INTO this clone is refused from\n"
        f"anywhere. A target this guard cannot resolve leaves the refusal standing.\n"
        f"\n"
        f"Full rules, and the measurement behind each one: {DOC}\n"
        f"Deliberately doing this anyway: put {OVERRIDE}=1 in front of the command."
    )


main()
