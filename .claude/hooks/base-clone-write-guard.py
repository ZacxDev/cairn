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
  * `git stash list` / `show` — reads.
  * 🔴 `-h` / `--help` ON **EVERY** REFUSED SUBCOMMAND — measured as help, rc 129
    `usage:`, repository unchanged, for all fourteen. This list named `--help` as a
    read for two rounds while the code exempted it for `stash` ALONE, so
    `git rm -h`, `git commit --help` and `git rebase --help` were refused; a corpus
    replay over this project's real Bash history found two commands of exactly that
    shape. ⚠ It is read from the FIRST word after the subcommand only, because
    `git commit -m -h` COMMITS (the `-h` is the message) — `_is_read_only_spelling`
    carries that measurement and the two residual false positives it costs.
  * a DRY RUN of `clean`, `mv` or `rm` — `--dry-run`, `-n`, or an `n` in a combined
    short cluster (`-nd`, `-rn`, `-nv`), a PREFIX (`--dry`, `--d`), and the last-wins
    pair — `--no-dry-run -n` is a dry run, `-n --no-dry-run` is not. ONE predicate over
    all three subcommands, because a `clean`-only version of it refused `git rm -n`:
    the rehearsal somebody runs BEFORE the dangerous spelling. Measured on git 2.55.0,
    repository bit-for-bit unchanged; `_OPTION_GRAMMAR` carries every measurement and
    the two scope limits.
    🔴 AND THREE THINGS THIS BULLET MUST NOT LUMP TOGETHER, BECAUSE AN EARLIER VERSION
    OF IT DID AND THAT IS WHAT A FUTURE WIDENING WOULD HAVE LEANED ON:
      - `add -n` and `apply --check` ARE genuine reads (measured unchanged) that are
        deliberately not exempt — an operator decision about scope creep, revisitable;
      - 🔴 `commit --dry-run` IS NOT A READ AT ALL: it writes a tree object, so it must
        never be exempted. Not the same category, and not revisitable;
      - `merge --no-commit` and `cherry-pick -n` are not dry runs either — both
        measured to WRITE. A flag named for what it does not do is not a flag that
        does nothing.
  * every other read: `log`, `status`, `diff`, `show`, `fetch`, `ls-files`,
    `rev-parse`, `push`. Pushing from the base clone touches no file in it.
  * 🔴 AND `worktree` AND `branch`, WHICH ARE NOT READS — `worktree remove` and
    `branch -D` destroy shared state, and they are out of `_REFUSED` on a DECISION
    rather than by oversight. The reasons are in `_REFUSED`'s comment and in the
    doc's second table; do not read their absence here as "nobody looked".

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

⚠ AND `git clean` IS VERY NEARLY THE SAME STORY, WHICH IS WORTH SAYING BECAUSE THE
PARAGRAPH ABOVE EXISTS FOR `stash` AND NOTHING SAID IT FOR `clean` — so the row read
as new coverage that it mostly is not. Measured against the operator's host-wide
guard, both its policies, cwd = the base clone: that guard already DENIES
`git clean -f` / `--force`, which is the only spelling git permits to destroy
non-interactively; it fails CLOSED where this one fails open, and it applies in every
worktree rather than only the main one, so on that host it is strictly WIDER. What
this entry adds there is the residue: `git clean -i`, the interactive spelling, which
that guard allows. 🔴 THE SAME IS **NOT** TRUE OF `rm` AND `mv` — no equivalent check
exists for either, so those two are genuinely new coverage on every host. The `clean`
row is kept for the same forward-looking reason as `stash` and stated at that scope:
the overlap is a property of ONE operator's machine, not of this repository, which
cannot see that guard at all (see the note further down on why it is not importable).

🔴 WHAT THIS GUARD NOW SEES, AND WHAT IT STILL CANNOT. Naming the second list is
not optional: a guard whose limits are unstated reads as coverage it does not
have. This table was once four open items and then seven, every one MEASURED
passing straight through; six are CLOSED and the entry that closed each is named
so a reader can check the claim rather than take it:

  * `git -C <the base clone> commit …` from a linked worktree — CLOSED, the `-C`
    chain is resolved and judged (`_redirect_targets`);
  * `git --git-dir=…/.git commit …` / `--work-tree=…` — CLOSED, both spellings
    and both separators, judged IN ADDITION to the caller's directory;
  * `GIT_DIR=…/.git git commit …` — CLOSED, from a leading assignment on any
    segment and from this hook's own environment. `GIT_INDEX_FILE=…/.git/index`
    is CLOSED too, and it was found by an audit driving variables END TO END
    rather than by reading: it was ALLOWED and REWROTE the clone's index, which is
    condition 1's own words. ⚠ `GIT_COMMON_DIR`, `-c core.worktree=`,
    `GIT_CONFIG_KEY_*` and `--config-env` were driven the same way and measured
    NOT to mutate the clone, so they are deliberately NOT judged — the set stops
    at what lands;
  * `cd <the base clone> && git commit …` — CLOSED in the UNDER-blocking
    direction only, and the asymmetry is deliberate: a `cd` target is judged IN
    ADDITION, never instead of the caller's directory, because deciding that a
    `cd` REPLACES it needs bash's positional model (`( … )` does not persist,
    `{ … }` does) and a wrong model there fails OPEN. So `cd <clone> && git
    commit` is refused, while the mirror — `cd <a worktree> && git commit` from
    the clone — is still refused too, as it was before. Pass `-C` instead.
  * A `<<WORD` INSIDE QUOTES OR INSIDE A `#` COMMENT OPENING A HEREDOC THAT BASH
    NEVER OPENED, so every later line was swallowed as its body — CLOSED, the
    opener is now found BY the quote-aware walk (`_heredoc_delimiter`, called from
    `_shell_lines`) instead of by a regex over the line that walk had already
    joined. `echo "a <<EOF b"` then `git commit` was proven to pass straight
    through, clone 1 -> 2 commits. ⚠ IT WAS A FAIL-OPEN REACHED BY ORDINARY TEXT,
    not a crafted payload — which is also why the obvious repair is the wrong one:
    blanking the quoted spans first erases the delimiter of `<<'EOF'`, and that
    REFUSES the body, which is the fail-CLOSED regression `_shell_lines` exists to
    prevent. Two more of the same family closed with it, both named at
    `_shell_lines`: quote tracking inside a heredoc BODY, and recording only the
    FIRST of several openers on one line.
  * COMMAND WRAPPERS HIDING THE PROGRAM NAME — `if git commit -m x`, and `while`,
    `until`, `command`, `nohup`, `timeout`, `eval`, `stdbuf`, `exec`, `sudo`,
    `xargs`, `nice` — handled by ONE ledger, `_LEADING_WORDS`, which absorbed the
    `_LEADING_RESERVED` set and the open-coded `env` branch it used to be split
    across. The words that consume a value are marked there.
    🔴 THIS ROW IS NOT "CLOSED", AND ITS CLOSING CONDITION IS **RETIRED** RATHER
    THAN MET. That condition was "one ledger of wrapper words with the
    value-consuming ones marked, and a parametrised case per word watched red",
    which defines done as an ENUMERATION OVER AN OPEN SET — it licenses, and
    arguably obliges, the next reader to add word 21 forever. The class cannot be
    enumerated: `ssh`, `ionice -p`, `watch`, `strace`, `coproc`, a shell FUNCTION
    and an unknown tail are all still unhandled, and so is `bash -c '…'`.
    🔴 THE ROOT FIX IS THE `bash -c` ROW BELOW, NOT WORD 21. Recursing into nested
    shells is where real coverage would come from, and it is where the host-wide
    guard spends two separate recursion budgets. **No entry should be added to the
    ledger without re-opening this question** — `_LEADING_WORDS` is pinned by a
    test for exactly that reason, so growing it is a decision somebody makes rather
    than a chore somebody completes.
    ⚠ AND THE MEASUREMENT THAT JUSTIFIED THE RETIREMENT, WITH ITS COUNTER-ARGUMENT,
    BECAUSE ONLY ONE OF THEM IS A REASON TO ACT. A replay of ONE host's session
    history — 37,268 distinct real Bash commands — through the parser before and
    after each of these three repairs counted the verdict flips: the heredoc row 17,
    the refused-ledger row 13, and this row **0**. Only two of its twelve new words
    ever preceded a refused git subcommand at all, and in every such command a bare
    refused git call was already present, so no verdict moved. Nothing flipped in
    the loosening direction for any row. 🔴 THE COUNTER-ARGUMENT IS LIVE: a guard
    also deters what has not happened yet, and zero past occurrences is not zero
    future ones. That measurement is what a deletion pass asks for; it is NOT
    evidence the ledger is worthless, which is why the code stays. Scope, stated
    because the number is otherwise read wider than it was taken: one host, one
    project's history, lexical verdicts with the cwd unresolved — so some fraction
    of the 17 and the 13 would have been allowed by condition 2 anyway. The
    DIRECTION and the RECURRENCE are the load-bearing parts; the integers are not.
  * `clean`, `mv` AND `rm` WRITING SHARED STATE WHILE "EVERYTHING ELSE IS A READ" —
    CLOSED in `_REFUSED`, with a DRY RUN of any of the three exempt as the read it
    is (`_is_dry_run`). ⚠ AND THE FIRST VERSION OF THAT EXEMPTION WAS `clean`-ONLY,
    WHICH WAS A FALSE POSITIVE A REVIEW MEASURED: `git rm -n` and `git mv -n` denied
    while `git clean -n` allowed — one predicate at one of three sites. Widened to
    ONE predicate, red-first per spelling.
    ⚠ THE OTHER TWO THE SAME AUDIT NAMED, `worktree remove` AND `branch -D`,
    ARE DELIBERATELY OUT, and the decision is recorded in the DOC's second table
    rather than only here: `claudedocs/working-in-parallel.md` PRESCRIBES
    `git -C "$REPO" worktree remove "$WT"` run from the base clone, and refusing a
    documented recipe is this file's own stated failure mode; and both write refs
    or worktree registrations, which live in the COMMON git dir and are writable
    identically from any worktree, so conditions 2 and 3 cannot scope the hazard —
    refusing only in the base clone would teach that the worktree spelling is safe.
  * `bash -c 'cd <the base clone> && git commit …'` — STILL OPEN. The inner script
    is one quoted token, so nothing in it is parsed as a command. Closing it means
    recursing into nested shells, which is where the host-wide guard spends two
    separate recursion budgets.
  * BACKTICK COMMAND SUBSTITUTION — ``echo `git commit -m x` `` — STILL OPEN, and it
    is in this list because the list is the only place a limit gets stated. Measured:
    the backtick form is ALLOWED while `$(git commit -m x)` is REFUSED, because `(`
    and `)` are operator characters that `_segments` already splits on and a backtick
    is not. Neither form was in either list before. Pre-existing, not closed here,
    and the same shape as the nested-shell row above: it is a second grammar for
    "run this string as a command".

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
#:
#: 🔴 AND "EVERYTHING ELSE IS A READ" WAS WRONG, WHICH IS WHY `clean`, `mv`, `rm`,
#: `revert`, `update-index`, `read-tree` AND `symbolic-ref` ARE HERE. Two audits
#: found them: a round-0 pass found five writers outside the set, and a round-1
#: audit of the PR that fixed those found four more. 🔴 THE ACCOUNTING, WHICH AN
#: EARLIER VERSION OF THIS SENTENCE GOT WRONG BY ONE ON THE VERY ROUND WHOSE SUBJECT
#: WAS A FALSE COMPLETENESS CLAIM — it said nine considered, seven in, three out,
#: which is ten: **of the NINE considered, SEVEN are in — these — and TWO are out
#: (`worktree remove`, `branch -D`). `restore` is a THIRD out-row from a separate
#: discovery, not one of the nine**, so the doc's out-table has three rows while the
#: nine split 7/2. Each out-row carries its reason there. The decision is
#: recorded in both places because widening a set without the doc moving is a
#: refusal nobody can look up, and declining to widen it without the doc moving is a
#: hazard nobody can find.
#:
#: 🔴 "CONSIDERED" IS NOT "ALL", AND AN EARLIER VERSION OF THIS COMMENT IMPLIED IT
#: WAS — WHICH IS THE FAILURE THIS FILE NAMES ELSEWHERE AS "READS AS COVERAGE WHILE
#: PROVIDING NONE". It said an audit had found five writers and the other two were
#: named in the doc, and the doc's table plus its test then pinned the out-set as
#: exactly two. That is a pinned FALSE COMPLETENESS: `revert` was outside both lists
#: and ALLOWED, writing the tree, the index and HEAD — condition 1's own words —
#: through the same sequencer as `cherry-pick`, which was already refused. The
#: complement of this set is NOT enumerable, and the test now pins the DECISIONS
#: TAKEN rather than the absence of others. Known writers still outside it, named so
#: the next reader starts from the limit rather than from a rediscovery:
#: `update-ref`, `tag -d`/`-f`, `notes`, `replace`, `reflog delete`, `bisect start`,
#: `sparse-checkout set`, `submodule update`, `checkout-index`, `gc`/`prune`/`repack`
#: and `filter-branch`. ⚠ MOST OF THOSE WRITE REFS OR OBJECT STORAGE IN THE COMMON
#: GIT DIR, which is the `worktree remove` / `branch -D` reasoning — reachable
#: identically from any worktree, so conditions 2 and 3 cannot scope them. Measured
#: with a probe whose snapshot covered HEAD, the index and one worktree file: it is
#: BLIND to other refs, so its "SAME" for `update-ref` and `tag -f` is an artefact
#: of the instrument and is NOT evidence those are reads. Said rather than quoted.
_REFUSED = frozenset({
    "add",
    "am",
    "apply",
    "cherry-pick",
    "checkout",
    "clean",
    "commit",
    "merge",
    "mv",
    "read-tree",
    "rebase",
    "reset",
    "revert",
    "rm",
    "stash",
    "switch",
    "symbolic-ref",
    "update-index",
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

#: Environment variables naming a DIRECTORY that redirects a git write, with the
#: same standing as the flags above: judged in addition, never instead.
#: ⚠ THIS SET IS NOT `_GIT_DISCOVERY_ENV`, AND AN EARLIER COMMENT SAYING "the two
#: environment variables" READ AS IF IT WERE. Two different questions: that set is
#: everything whose presence would steer the guard's OWN reads and is therefore
#: scrubbed, which is cheap to over-include; THIS set is everything whose value
#: must be JUDGED as a write target, which has to be earned by measurement. They
#: are deliberately different sizes — and this one is the smaller, deliberately:
#: `GIT_COMMON_DIR` is scrubbed but NOT judged, because it was driven end to end
#: and measured not to mutate the clone. Judging a variable that cannot land buys
#: a false positive and no coverage.
_GIT_DIR_ENV_NAMES = ("GIT_DIR", "GIT_WORK_TREE")

#: Environment variables naming a FILE rather than a directory. Judged by the
#: directory CONTAINING that file, because that is what identifies the repository.
#: 🔴 `GIT_INDEX_FILE` IS HERE BECAUSE IT WAS MEASURED TO LAND, not because it
#: looked plausible: `GIT_INDEX_FILE=<the clone>/.git/index git add <file>` run
#: from a linked worktree was ALLOWED and REWROTE the clone's index. That is
#: condition 1's "mutates the index", in the clone, allowed — the exact shape this
#: guard exists for, reached by a variable nothing judged.
#: ⚠ AND THE SET STOPS AT WHAT LANDS. `-c core.worktree=`, `GIT_CONFIG_KEY_*` and
#: `--config-env` were each driven END TO END against a miniature clone and
#: measured NOT to mutate it, so they are deliberately unjudged: widening past
#: what was shown to land would trade false negatives for false positives with no
#: measurement on either side.
_GIT_FILE_ENV_NAMES = ("GIT_INDEX_FILE",)

#: Shell builtins that move the caller's directory. Read as ADDITIONAL judged
#: directories only — the docstring's table says why a replacement needs bash's
#: positional model and why guessing it fails open.
_CHDIR_BUILTINS = frozenset({"cd", "pushd"})

#: 🔴 THERE IS NO CAP ON HOW MANY DIRECTORIES THIS HOOK WILL ASK `git` ABOUT, AND
#: TWO EARLIER DRAFTS HAD ONE. Both were measured to convert a DENY into an ALLOW
#: at exactly the bound, which is the one direction this file may not fail in. The
#: history is kept because the cap is the obvious optimisation and the reasoning
#: that justified it was wrong twice, in the same way, one level apart:
#:
#:   * GLOBAL cap. The counter was shared by the primary and additive passes, so
#:     PRIMARIES crowded out primaries. Eight `git -C <a real linked worktree> add`
#:     segments — each a legitimate, resolvable, trusted redirect — followed by a
#:     bare `git commit` IN THE CLONE: DENY for N ≤ 7, ALLOW for N ≥ 8.
#:   * ADDITIVE-ONLY cap. The fix for the above, and still a bypass, because
#:     `cd` and `GIT_DIR` targets are WHOLE-COMMAND scans appended to every
#:     segment's extras. Bisected on two independent spellings, cwd = a linked
#:     worktree, N ≤ 7 deny / N ≥ 8 allow: `git --git-dir=<an existing dir>` × N
#:     then `--git-dir=<the clone>/.git add` REWROTE the clone's index with a file
#:     staged; and N × `cd <an existing dir> && git fetch` then `cd <the clone> &&
#:     git commit` took the clone from 1 to 2 commits. The second is an ORDINARY
#:     CROSS-REPO SWEEP, not an exotic payload.
#:
#: 🔴 AND THE SENTENCE THAT STOOD HERE WAS THE PROBLEM, NOT THE CODE: "a cap
#: belongs on the half that cannot change a refusal into an allow, which is the
#: additive half alone." Measured FALSE, and it is exactly the kind of claim that
#: stops the next reader from looking. Any N EXISTING directories named additively
#: crowded out the real one; the existence pre-check only filters paths that do not
#: exist, which a cross-repo sweep's `cd` targets certainly do.
#:
#: What makes "no cap" affordable: `_ROOTS` and `_PROBED` memoise per PATH, so the
#: cost is distinct directories rather than segments, and the cheap string-only
#: pass still early-outs every command that is not already a refusal candidate.
#: A hostile command text can still make this slow. It cannot make it wrong, and
#: that is the trade this file takes every time.


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
    """One read-only git call. `None` on ANY failure — see the fail-open note.

    🔴 `ValueError` IS IN THE EXCEPT CLAUSE BECAUSE IT WAS MEASURED, NOT BECAUSE IT
    LOOKED POSSIBLE. `subprocess.run(cwd=…)` raises `ValueError: embedded null
    byte` — not an `OSError` — for a path containing NUL, so `git -C $'a\\0b'
    commit` crashed this hook with a traceback and rc 1. Every non-zero status
    other than 2 lets the command RUN, so the crash was an ALLOW on a payload the
    base ref DENIED: a fail-open reached by a defect in the guard's own plumbing
    rather than in its policy. Reach is narrow — bash cannot carry NUL in argv —
    but fail-open-on-crash is the property this file's own header forbids itself.
    """
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
    except (OSError, ValueError, subprocess.SubprocessError):
        return None
    if out.returncode != 0:
        return None
    return out.stdout.strip()


#: Characters after which an unquoted `#` BEGINS A WORD, so bash reads it as the
#: start of a comment running to the end of the line. Whitespace is tested
#: separately; these are the shell operator characters, which end a word too.
#: ⚠ USED FOR OPENER DETECTION ONLY — see `_segments` on why `commenters` stays
#: cleared there, and why a parser narrower than bash's misses real commands.
_WORD_BREAK_BEFORE_HASH = frozenset(";|&()<>")


def _heredoc_delimiter(command: str, index: int) -> str | None:
    """The delimiter a `<<` at `command[index:]` opens, or `None` if it opens none.

    🔴 IT IS A LOOKAHEAD AND IT CONSUMES NOTHING, which is what lets `_shell_lines`
    call it from inside its own quote-aware walk: the walk then reads the
    delimiter's characters as ordinary text, so the LINE it returns stays
    byte-identical to its input and a quoted delimiter's two quotes open and close
    in the walk's own quote state exactly as they did before.

    🔴 AND A QUOTE IMMEDIATELY AFTER `<<` IS DELIMITER QUOTING, NOT STRING
    QUOTING — WHICH IS WHY THE OBVIOUS IMPLEMENTATION OF THIS FIX IS A FAIL-CLOSED
    REGRESSION. "Blank out the quoted spans, then run the old regex over the
    result" erases the delimiter of `<<'EOF'` and `<<"EOF"` along with the quotes,
    so NO heredoc opens, so the body — `git commit -m x` in this file's own worked
    example two functions down — is read as a command and REFUSED. That is
    precisely the regression `_shell_lines` exists to prevent, so the opener is
    read inline in the walk instead of over a rewritten string.

    What it declines, each on purpose:

      * `<<<WORD` — a bash HERESTRING, which carries its data on the SAME line and
        has no body and no terminator. ⚠ THAT EXPLICIT `return None` IS A HEDGE AND
        NOT A LIVE BRANCH, which is measured rather than assumed: a mutation sweep
        DELETED it and every test stayed green (SURVIVED, 0 failures), because the
        `re.match` below declines a delimiter starting with `<` anyway. It is kept
        for the same reason the attached `-C<path>` branch in `_redirect_targets` is
        — a reader comparing this walk against bash's grammar should not have to
        work out whether the omission was deliberate. 🔴 THE THING THAT ACTUALLY
        CLOSES HERESTRINGS IS IN THE WALK, NOT HERE: `_shell_lines` refuses to start
        a lookahead at the SECOND `<` of a `<<<` run, and deleting THAT fails two
        tests. Without it, `cat <<<"EOF"` reads chars two and three as a `<<` and
        opens a heredoc named `EOF`, swallowing every later line.
      * `<<-` is NOT declined: it is the tab-stripping heredoc and opens one, and a
        sweep that removed the `-` skip fails its test.
      * anything whose delimiter is not a bare identifier, optionally
        single- or double-quoted — the same shape the regex accepted. ⚠ The
        closing-quote test is a second HEDGE, SURVIVED by the same sweep: it can
        only change the answer for `<<'EOF` with no closing quote, which bash
        rejects outright, so no reachable command distinguishes the two.

    ⚠ ONE SHAPE IT STILL GETS WRONG, AND IT IS INHERITED RATHER THAN NEW: an
    arithmetic left shift whose right operand is a NAME, `$((a << b))`, reads as a
    heredoc opening on `b`. The predecessor regex did the same. It swallows later
    lines, so it is fail-OPEN; closing it needs the `$(( … ))` nesting this parser
    deliberately does not model.
    """
    at = index + 2                                  # just past the `<<`
    if command[at:at + 1] == "<":
        return None                                 # a herestring
    if command[at:at + 1] == "-":
        at += 1
    while command[at:at + 1] in (" ", "\t"):
        at += 1
    quote = ""
    if command[at:at + 1] in ("'", '"'):
        quote = command[at]
        at += 1
    match = re.match(r"[A-Za-z_][A-Za-z_0-9]*", command[at:])
    if not match:
        return None
    if quote and command[at + match.end():at + match.end() + 1] != quote:
        return None
    return match.group(0)


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

    🔴 AND THE OPENER IS FOUND BY THE WALK ITSELF, NOT BY A REGEX OVER THE JOINED
    LINE — WHICH IS THE OTHER HALF OF THE SAME FUNCTION AND WAS A FAIL-OPEN FOR
    TWO ROUNDS. The first version of this function searched the already-joined RAW
    line for `<<-?\\s*(['\"]?)([A-Za-z_]\\w*)\\1`, so a `<<WORD` inside a quoted
    string, or inside a `#` comment, opened a heredoc that bash never opened and
    EVERY LATER LINE was swallowed as its body. Measured end to end:
    `echo "a <<EOF b"` followed by `git commit` passed straight through and took a
    miniature clone from 1 to 2 commits. ⚠ IT WAS REACHED BY ORDINARY TEXT, not by
    a crafted payload — a function that calls itself quote-aware while its most
    consequential decision reads the string it had already stopped trusting.

    🔴 THE BODY IS NOT QUOTE-TRACKED EITHER, AND THAT WAS A THIRD FAIL-OPEN OF THE
    SAME FAMILY. A heredoc body has no quoting at all, so tracking quotes through
    it let one apostrophe in the DATA open a quote that swallowed the terminator
    line, after which the body never ended and every real command behind it was
    dropped: `cat > f <<EOF` / `it's data` / `EOF` / `git commit -m x` was ALLOW
    before and is DENY now. Bodies are still dropped, so recognising them better
    cannot invent a false positive.

    ⚠ EVERY OPENER ON A LINE IS RECORDED, NOT JUST THE FIRST, because `re.search`
    could only ever find one. `cat <<A <<B` has two bodies in order, and reading
    only `A` left B's body being parsed as commands — the fail-CLOSED direction: a
    body line reading `git commit -m x` was REFUSED while nothing was being
    committed.
    """
    lines: list[str] = []
    current: list[str] = []
    pending: list[str] = []          # heredoc delimiters still awaited
    openers: list[str] = []          # delimiters THIS line opens, in order
    quote: str | None = None
    quote_escapes = False            # does THIS quote honour `\`? see the open branch
    comment = False
    prev: str | None = None          # the previous character on this logical line
    prev_escaped = False             # …and did it arrive via a `\` escape pair?
    index, size = 0, len(command)

    def flush(line: str) -> None:
        nonlocal openers
        if pending:
            # Inside a heredoc body: data, never a command. Only its terminator
            # is interesting, and only because it ends the body. ⚠ `.strip()` is
            # WIDER than bash, which strips leading TABS for `<<-` only; a line
            # that is the delimiter plus spaces ends the body here and would not
            # in bash, which drops MORE text and is therefore the fail-open
            # direction. Kept as it was: narrowing it is a separate decision with
            # its own measurement to make.
            if line.strip() == pending[0]:
                pending.pop(0)
            openers = []
            return
        pending.extend(openers)
        openers = []
        lines.append(line)

    while index < size:
        char = command[index]
        if pending:
            # A heredoc BODY. No quoting, no comments, no openers of its own —
            # see the docstring: tracking any of them here was a fail-open.
            #
            # ⚠ IT DELIBERATELY DOES NOT RESET `prev`/`comment`, AND A DRAFT THAT DID
            # WAS **MEASURED DEAD**. The reasoning was: the terminator's flush is
            # what ends the body, so the first character of the next command line
            # would inherit the last character of the OPENER line, and a leading `#`
            # there would not read as a comment. Wrong — the command-mode newline
            # branch below sets `prev = None` AFTER calling `flush`, which is where
            # the body began, and body mode never writes `prev` again. So it is
            # already `None` for every line after a terminator. A mutation sweep
            # deleting the reset scored it SURVIVED with 0 failures, and the test
            # that was supposed to kill it (a comment on the line right after a
            # terminator) is green either way — it stays, because that case is a
            # REFUSAL that was ALLOW at `ffa0eca`, just not for this reason.
            if char == "\n":
                flush("".join(current))
                current = []
            else:
                current.append(char)
            index += 1
            continue
        if quote:
            # 🔴 A BACKSLASH ESCAPE INSIDE A QUOTE, AND THE VERSION WITHOUT IT
            # DISARMED THE GUARD FOR EVERY LATER LINE. Measured end to end: with
            # `echo "say \"hi\""` on the first line, a `git add` + `git commit` on a
            # LATER line passed straight through. The `\"` was read as the closing
            # quote, the real closer re-opened one that never closes, so the walk
            # returned the whole multi-line command as ONE logical line — `git` is
            # then not at argv[0] and no candidate is produced at all. Ordinary
            # text: `printf "%s\n" "a\"b"` and `sed -i "s/x/\"y\"/" f` do it too.
            # ⚠ Single-line commands were never affected (`;` still splits); it
            # needs a newline and a later-line write, which is the ordinary shape of
            # an agent's Bash call.
            #
            # ⚠ ESCAPES ARE HONOURED PER QUOTE KIND, NOT EVERYWHERE, because bash
            # does not: inside `'…'` a backslash is LITERAL, so honouring one there
            # would mis-parse the common `<<'EOF'` delimiter and every `'…\…'`
            # string. `$'…'` is the exception bash itself carves out, so the flag is
            # set from the character before the opening quote.
            if quote_escapes and char == "\\" and index + 1 < size:
                current.append(command[index:index + 2])
                prev = command[index + 1]
                # Set for symmetry with the unquoted escape branch rather than
                # because a `$'` can be reached from here — leaving a quote always
                # clears it — so that a reader does not have to prove that.
                prev_escaped = True
                index += 2
                continue
            current.append(char)
            if char == quote:
                quote = None
            prev = char
            # DEAD: leaving a quote always clears it, so a `$'` opener can
            # never be reached from here. Kept for symmetry; sweep-confirmed.
            prev_escaped = False
            index += 1
            continue
        if char == "\n":
            flush("".join(current))
            current = []
            comment = False
            prev = None
            # DEAD: a newline clears `prev` too, so the next line starts with
            # `prev is None` and the `$` test cannot fire. Sweep-confirmed.
            prev_escaped = False
            index += 1
            continue
        if comment:
            # To the end of the line, literally: a quote in a comment opens
            # nothing and a backslash continues nothing.
            current.append(char)
            prev = char
            # DEAD: a comment runs to end of line and opens no quote, so the
            # `$'` test is unreachable from inside one. Sweep-confirmed.
            prev_escaped = False
            index += 1
            continue
        if char == "\\" and index + 1 < size:
            current.append(command[index:index + 2])
            # bash DELETES a backslash-newline pair, so the characters either side
            # of it end up adjacent — `prev` must not become whitespace, or a `#`
            # after a line continuation would read as a comment where bash reads it
            # as part of the word (`echo foo\<nl>#bar` prints `foo#bar`). Any OTHER
            # escaped character IS the previous character and never breaks a word:
            # `\ ` is a literal space INSIDE one, so a `#` after it opens no
            # comment either.
            if command[index + 1] != "\n":
                prev = command[index + 1]
                # 🔴 THE FLAG THE `$'…'` TEST NEEDS: this character came from an
                # ESCAPE, so `\$` must not look like the `$` of a `$'` opener.
                prev_escaped = True
            index += 2
            continue
        if char in ("'", '"'):
            quote = char
            # `"…"` honours backslash escapes; `'…'` does not; `$'…'` does — see the
            # quote branch above for the measurement and for why the distinction is
            # load-bearing rather than pedantic.
            #
            # 🔴 THE `$` MUST BE UNESCAPED, AND THE VERSION THAT ONLY CHECKED `prev`
            # WAS A REGRESSION THIS FILE SHIPPED. The escape branch below sets
            # `prev` to the character it consumed, so `\$` left `prev == "$"` and an
            # ORDINARY `'…'` opening after an escaped dollar was routed into the
            # escape-honouring model. bash reads `\$` as a literal dollar and the
            # quote after it as a plain single quote. Measured: `echo \$'a\'` then a
            # bare `git commit` on the next line was deny before the escape model
            # landed and ALLOW after — the exact hole this file's own mutation sweep
            # had found as `r14`, reachable in production by a one-character prefix.
            # `prev_escaped` is why the check is on two variables and not one.
            quote_escapes = char == '"' or (prev == "$" and not prev_escaped)
            current.append(char)
            prev = char
            # DEAD: `#` is not `$`, so this cannot change the `$'` test.
            # Sweep-confirmed.
            prev_escaped = False
            index += 1
            continue
        if char == "#" and (prev is None or prev.isspace()
                            or prev in _WORD_BREAK_BEFORE_HASH):
            comment = True
            current.append(char)
            prev = char
            prev_escaped = False
            index += 1
            continue
        if char == "<" and prev != "<" and command[index + 1:index + 2] == "<":
            # `prev != "<"` so the SECOND `<` of a `<<<` herestring cannot start a
            # lookahead of its own; `_heredoc_delimiter` carries both halves.
            delimiter = _heredoc_delimiter(command, index)
            if delimiter is not None:
                openers.append(delimiter)
        current.append(char)
        prev = char
        # 🔴 LIVE, AND THE ONLY ONE OF THE SIX THAT IS. This is the generic
        # tail, where an ordinary `$` lands. Deleting it kept 315 tests green
        # while turning `echo \x$'a\'` plus a later write from allow into
        # deny — a false positive on a command bash itself rejects with
        # `unexpected EOF`. It was an UNDECLARED survivor until an audit
        # mutated the line; the ledger in the commit was short by one.
        prev_escaped = False
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

    ⚠ `_shell_lines` DOES MODEL COMMENTS AND THIS DOES NOT, WHICH IS A DELIBERATE
    ASYMMETRY RATHER THAN A DISAGREEMENT. There, a comment decides only whether a
    `<<` opens a heredoc — a question about the lines that FOLLOW. Here it would
    decide whether words are commands, and getting that narrower than bash drops
    real ones. So comment text still arrives in these segments as ordinary tokens,
    and the `curl …/x#frag && git commit` case stays refused.

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


#: 🔴 ONE LEDGER OF EVERY WORD THAT CAN PRECEDE THE PROGRAM NAME WITHOUT BEING IT,
#: AND IT IS ONE LEDGER DELIBERATELY. It was two — a `_LEADING_RESERVED` set of
#: shell reserved words plus an open-coded `env` branch — and the words that hide a
#: `git` do not divide along that line at all: `claude/RULES.md`'s "one rule, one
#: place" says a predicate split across two places is wrong at one of them, and it
#: was. `if git commit -m x`, `while`, `until`, `command`, `nohup`, `timeout`,
#: `eval`, `stdbuf`, `exec`, `sudo`, `xargs` and `nice` were each MEASURED passing
#: straight through, because the program name was read as the wrapper.
#:
#: 🔴 `{` COST A GUARD and is the row that explains the shape of this problem:
#: `{ git commit -m x; }` passed straight through because the program name was read
#: as `{`, while the docstring listed the `(…)` twin among the CLOSED walks — and
#: `(` is closed, because it is an OPERATOR character where `{` is a reserved WORD.
#: The paren fix could never have covered it.
#:
#: Each value is `(operands consumed before the program name, that word's own flags
#: that take a SEPARATE value)`. An ATTACHED value takes no separate word, so
#: `stdbuf -o0` and `xargs --max-args=3` fall out of the `startswith("-")` skip for
#: free; `timeout` is the only word here that eats a bare OPERAND (its duration).
#:
#: 🔴 NOT CLOSED, AND DELIBERATELY NOT CLOSABLE — DO NOT ADD A WORD WITHOUT
#: RE-OPENING THE QUESTION. ⚠ An earlier wording said "do not add word 21" against a
#: dict that already HAD 21 entries, so a reader counting to check landed on
#: "already fired" and could read the rule as spent. The invariant is the rule, never
#: the ordinal. The docstring's table records why this row's closing
#: condition was RETIRED rather than met: "a case per word" defines done as an
#: enumeration over an OPEN set, and the root fix is nested-shell recursion
#: (`bash -c '…'`), not another word. `tests/test_base_clone_write_guard.py` PINS
#: this dict so a growth is a decision rather than a chore — if you are here to add
#: an entry, read that table first and say what makes this word worth a row when a
#: replay of 37,268 real commands moved no verdict for any of the twelve already
#: here — a replay whose own scope is stated there. The direction of the gap is still
#: fail-OPEN, and that is accepted.
#: Known absences, each left out on purpose rather than forgotten, because skipping a
#: word whose operand is NOT a local program would invent a false positive — the
#: direction this file forbids itself:
#:   * `ssh <host> git commit` and `bash -c '…'` / `sh -c '…'` — the write lands on
#:     another machine, or inside a quoted token this parser cannot see at all (the
#:     nested-shell row in the docstring's table);
#:   * `ionice -p <pid> …` — `-p` re-prioritises an EXISTING process, so the word
#:     after the flags is not necessarily the program being run;
#:   * `watch`, `strace`, `coproc`, `find -exec`, `make`, a shell FUNCTION name — no
#:     measurement either way, and a guess here buys a refusal nobody can look up.
#: A `git` reached through any of those is unseen, exactly as a `git` inside
#: `bash -c` is.
_NO_VALUE_FLAGS: frozenset[str] = frozenset()
_LEADING_WORDS: dict[str, tuple[int, frozenset[str]]] = {
    # Shell reserved words and the brace group. None takes a flag or an operand.
    # `then`/`do`/`else`/`elif` close `if …; then git commit; fi` on one line;
    # `if`/`while`/`until` close the same shape when the git call is the CONDITION.
    "{": (0, _NO_VALUE_FLAGS),
    "}": (0, _NO_VALUE_FLAGS),
    "!": (0, _NO_VALUE_FLAGS),
    "if": (0, _NO_VALUE_FLAGS),
    "while": (0, _NO_VALUE_FLAGS),
    "until": (0, _NO_VALUE_FLAGS),
    "then": (0, _NO_VALUE_FLAGS),
    "do": (0, _NO_VALUE_FLAGS),
    "else": (0, _NO_VALUE_FLAGS),
    "elif": (0, _NO_VALUE_FLAGS),
    # `eval git commit` is visible; `eval 'git commit'` is one quoted token and is
    # not, which is the nested-shell gap rather than a new one.
    "eval": (0, _NO_VALUE_FLAGS),
    # Command wrappers. `time` and `exec` take no operand; `command -v git` lands
    # on a `git` with nothing after it, which `_git_subcommand` answers `None` for.
    "time": (0, _NO_VALUE_FLAGS),
    "command": (0, _NO_VALUE_FLAGS),
    "exec": (0, frozenset({"-a"})),
    "nohup": (0, _NO_VALUE_FLAGS),
    "nice": (0, frozenset({"-n", "--adjustment"})),
    "stdbuf": (0, frozenset({"-i", "--input", "-o", "--output", "-e", "--error"})),
    "sudo": (0, frozenset({
        "-u", "--user", "-g", "--group", "-U", "--other-user", "-p", "--prompt",
        "-r", "--role", "-t", "--type", "-C", "--close-from", "-h", "--host",
        "-D", "--chdir", "-R", "--chroot",
    })),
    "xargs": (0, frozenset({
        "-I", "-i", "--replace", "-n", "--max-args", "-L", "-l", "--max-lines",
        "-P", "--max-procs", "-s", "--max-chars", "-E", "-e", "--eof",
        "-d", "--delimiter", "-a", "--arg-file",
    })),
    # `env -i`, `env -u NAME`, `env --`: a round-1 audit measured `env -i git
    # commit` and `env -u FOO git commit` both passing through.
    "env": (0, frozenset({"-u", "--unset", "-C", "--chdir", "-S", "--split-string"})),
    # 🔴 THE ONE OPERAND-CONSUMING ROW. `timeout <duration> git commit` hides the
    # `git` behind a word that is not a flag. ⚠ AND THE COST OF THAT IS A FAIL-OPEN
    # ON A MALFORMED COMMAND, STATED RATHER THAN HIDDEN: `timeout git commit` (no
    # duration, which `timeout` itself rejects) consumes `git` as the duration and
    # lands on `commit`, which is not a program name, so the segment is not read as
    # a git call. Allowing a command that cannot run is the cheap direction.
    "timeout": (1, frozenset({"-s", "--signal", "-k", "--kill-after"})),
}


def _leading_assignments(segment: list[str]) -> tuple[dict[str, str], int]:
    """`VAR=value` prefixes and `_LEADING_WORDS` wrappers, and where argv starts.

    A wrapper is matched on its BASENAME as well as its spelling, so
    `/usr/bin/env` and `/usr/bin/time` are skipped like the bare words — the same
    reason `_git_subcommand` reads `basename(…) == "git"`.
    """
    assignments: dict[str, str] = {}
    i = 0
    while i < len(segment):
        word = segment[i]
        entry = _LEADING_WORDS.get(word)
        if entry is None:
            entry = _LEADING_WORDS.get(os.path.basename(word))
        if entry is not None:
            operands, value_flags = entry
            i += 1
            # The wrapper's OWN options, then its own operands. `--` has no entry
            # in any `value_flags`, so it is consumed as a one-word flag, which is
            # what it is.
            while i < len(segment) and segment[i].startswith("-"):
                i += 2 if segment[i] in value_flags else 1
            i += operands
            continue
        match = re.fullmatch(r"([A-Za-z_][A-Za-z_0-9]*)=(.*)", word)
        if match:
            assignments[match.group(1)] = match.group(2)
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


#: 🔴 ONE LEDGER FOR THE DRY-RUN PREDICATE, OVER EVERY REFUSED SUBCOMMAND THAT HAS
#: ONE — AND IT IS ONE BECAUSE THE FIRST VERSION WAS `clean`-ONLY AND THAT WAS A
#: FALSE POSITIVE. The exemption was written for the subcommand that happened to be
#: under discussion, so `git clean -n` was a read while `git rm -n` and
#: `git mv -n` — MEASURED, rc 0 and the repository bit-for-bit unchanged — were
#: REFUSED. `git rm -n` is exactly what somebody types to see what a `git rm` would
#: do *before* doing it; refusing the safe rehearsal alongside the dangerous
#: spelling is how a guard teaches that it is noise, which this file's own header
#: calls the worse failure. `claude/RULES.md`: a predicate open-coded at one of
#: three sites is wrong at the other two.
#:
#: The value is that subcommand's own SHORT flags that consume a value, which is the
#: ONLY thing that makes reading a COMBINED cluster (`-nd`, `-rn`) safe. Taken from
#: git's own `-h` output rather than from memory, on git 2.55.0:
#:
#:   * `clean` — `-q -n -f -i -d -e <pattern> -x -X`: `-e` is the one that takes a
#:     value, so the scan must stop there. 🔴 MEASURED, NOT REASONED: the attached
#:     spelling `git clean -fenjunk.txt` really does delete (repo CHANGED), while
#:     `-n`, `-nd`, `-dn` and `-xn` really do not (repo SAME) — so without the stop
#:     the pattern's own letters would read as flags and a real delete would be
#:     called a dry run.
#:   * `rm` — `-n -q -f -r`: NONE takes a value, so no stop is needed. Measured:
#:     `-rn`, `-nr`, `-qn` and `-fn` are all accepted and all leave the repository
#:     unchanged. ⚠ A BRIEF ASSERTED `git rm -rn` "is not a thing"; it is, on
#:     2.55.0, and it is a dry run — which is why this row carries a measurement
#:     instead of an opinion.
#:   * `mv` — `-v -n -f -k`: none takes a value either; `-nv`, `-vn` and `-kn`
#:     measured accepted and unchanged.
#:
#: ⚠ NOT CLOSED, and deliberately NOT widened to the rest of `_REFUSED`. Two more
#: members have a spelling measured to change nothing — `add -n`/`--dry-run` and
#: `apply --check`/`--stat` (positive control in the same run: a bare `apply`
#: CHANGED the tree). Those are an operator decision, not a mechanical consequence
#: of this table, and scope creep in a guard is its own hazard.
#: 🔴 AND `commit --dry-run` IS **NOT** ONE OF THEM — AN EARLIER VERSION OF THIS
#: COMMENT LISTED IT AS A GENUINE READ AND THAT WAS WRONG, WHICH MATTERS BECAUSE IT
#: IS THE SENTENCE THAT WOULD LICENSE WIDENING. Measured: `git commit --dry-run`
#: with a staged change WRITES A NEW TREE OBJECT into `.git/objects`. ⚠ THE ADDED
#: OBJECT IS THE WHOLE FINDING AND THE INDEX IS NOT PART OF IT: `.git/index` also
#: changes under a plain `git status` when the stat cache is stale — measured
#: BOTH ways on this machine, `status` leaving it untouched in one run and changing
#: it in another — so the index half cannot discriminate a read from a write and
#: quoting it would get the finding dismissed. The verdict is already correct
#: (`commit` is not in the table, so it still refuses); what was wrong was the
#: recorded reason. 🔴 AND TWO SPELLINGS THAT *LOOK* LIKE DRY RUNS ARE NOT, SO
#: THEY MUST NEVER BE ADDED: `git merge --no-commit` staged a merge AND moved HEAD
#: on a fast-forward, and `git cherry-pick -n` staged the picked file — both
#: measured CHANGED. A flag named for what it does not do is not a flag that does
#: nothing. `am`, `cherry-pick`, `checkout`, `switch`, `merge`, `rebase`, `reset`
#: and `stash` have no `--dry-run` at all (rc 129, unknown option).
#: 🔴 ONE MODEL OF GIT'S OPTION GRAMMAR, BECAUSE THREE EXACT-STRING TABLES WERE WRONG
#: IN THE SAME DIRECTION AND EACH ONE DELETED SOMETHING. `claude/RULES.md`: a predicate
#: open-coded at N sites is typically wrong at N−1 of them the same way, and
#: consolidating is what makes the disagreement audible. There were three — a dry-run
#: value-flag table, an `_is_dry_run` cluster walk, and `symbolic-ref`'s
#: `any(word in ("-d", "--delete"))` — and all three matched option SPELLINGS where
#: git matches an option GRAMMAR. Measured in an armed clone, the real effect beside
#: the verdict, every row ALLOWED before this:
#:
#:     git clean -f --exc -n              DELETED junk.txt   (prefix ate the `-n`)
#:     git clean -n --no-dry-run -f       DELETED junk.txt   (last-wins negation)
#:     git rm    -n --no-dry-run seed.txt DELETED seed.txt   (same)
#:     git symbolic-ref -qd <ref>         REF DELETED        (short bundling)
#:     git symbolic-ref --del <ref>       REF DELETED        (prefix)
#:     git clean --dry                    REFUSED            (a real dry run!)
#:
#: The three behaviours git has and spelling tables do not:
#:
#:   1. **a long option matches on any unambiguous PREFIX, and the prefix consumes its
#:      value identically.** Measured: `--exclude`, `--exc`, `--ex` and `--e` all ate
#:      the following word; `--dry`, `--d` and `--no-dry` all resolved too. It cuts
#:      BOTH ways, which is why the `--dry` row above is a false positive rather than
#:      a fail-open.
#:   2. **short options bundle.** `-qd` and `-dq` both deleted a ref.
#:   3. **last option wins.** `-n --no-dry-run` deletes; `--no-dry-run -n` does not.
#:      The old walk returned on the first `n` and never read the rest.
#:
#: ⚠ TWO SCOPE LIMITS, STATED BECAUSE THIS FILE RUNS ON STRANGERS' MACHINES. (a) The
#: tables are what `git <sub> -h` printed on **git 2.55.0** — ONE version at ONE point.
#: An option a later git ADDS is unknown here; an unknown `--xxx` is read as a bare
#: boolean, so a future value-taking long option whose value is literally `-n` would
#: be a fail-open. Chosen over the alternative, which silently refuses a dry run for
#: every future flag. (b) Ambiguity depends on the full option set, so a prefix
#: matching SEVERAL known options is treated as value-taking whenever ANY candidate
#: takes a value — a WIDENING rather than a resolution, and safe because git itself
#: refuses an ambiguous abbreviation (measured: `git clean --n` is rc 129), so no
#: write happens either way and only the verdict differs.
#: The refused subcommands whose DRY RUN is a read. 🔴 SEPARATE FROM
#: `_OPTION_GRAMMAR` ON PURPOSE: that models git's grammar for a subcommand, this is
#: the POLICY decision about which dry runs are exempt. `symbolic-ref` is in the
#: grammar and must never be in here — it has no dry run — and `commit` is in neither,
#: because `commit --dry-run` writes a tree object.
#:
#: ⚠ AND THE CHECK THAT READS THIS SET IS **UNREACHABLE TODAY**, MEASURED RATHER THAN
#: ASSUMED: a mutation sweep deleted it and every test stayed green, because no
#: grammar entry outside this set carries `--dry-run` in its `long_bool`, so the
#: lookup could not resolve one anyway. It is kept for ONE named future edit, which is
#: what makes it defence rather than decoration: the day somebody adds `commit` to
#: `_OPTION_GRAMMAR` — for any reason at all, including modelling its options for a
#: different check — `commit --dry-run` would resolve, and WITHOUT this set it would
#: silently become exempt. That is a write allowed by an edit nobody would connect to
#: this file. `test_the_DRY_RUN_exemption_covers_EXACTLY_THREE_SUBCOMMANDS_AND_NO_MORE`
#: is the other half of that defence; neither alone sees it.
_DRY_RUN_SUBCOMMANDS = frozenset({"clean", "mv", "rm"})

_OPTION_GRAMMAR: dict[str, dict[str, object]] = {
    "clean": {
        "long_value": frozenset({"--exclude"}),
        "long_bool": frozenset({"--dry-run", "--force", "--interactive", "--quiet"}),
        "short_value": frozenset({"e"}),
        "short_bool": {"n": "--dry-run", "f": "--force", "i": "--interactive",
                       "q": "--quiet"},
    },
    "rm": {
        "long_value": frozenset({"--pathspec-from-file"}),
        "long_bool": frozenset({"--dry-run", "--force", "--quiet", "--cached",
                                "--ignore-unmatch", "--sparse",
                                "--pathspec-file-nul"}),
        "short_value": frozenset(),
        "short_bool": {"n": "--dry-run", "f": "--force", "q": "--quiet"},
    },
    "mv": {
        "long_value": frozenset(),
        "long_bool": frozenset({"--dry-run", "--force", "--verbose", "--sparse"}),
        "short_value": frozenset(),
        "short_bool": {"n": "--dry-run", "f": "--force", "v": "--verbose"},
    },
    "symbolic-ref": {
        "long_value": frozenset(),
        "long_bool": frozenset({"--delete", "--quiet", "--short", "--recurse"}),
        # `-m <reason>` is this subcommand's one value-taking short option; modelled
        # so its value is never counted as an operand.
        "short_value": frozenset({"m"}),
        "short_bool": {"d": "--delete", "q": "--quiet"},
    },
    # 🔴 `merge` IS HERE SO THE `--ff-only` EXEMPTION STOPS MATCHING A SPELLING. It was
    # `"--ff-only" in rest`, and `git merge --ff-o origin/main` is a spelling git
    # ACCEPTS (measured: it reaches the same `not something we can merge` as the full
    # form, i.e. past option parsing) and was REFUSED — a false positive on the one
    # resync recipe `claudedocs/working-in-parallel.md` prescribes.
    #
    # ⚠ EVERY VALUE-TAKING OPTION IS LISTED AND THE OPTIONAL-ARGUMENT ONES ARE NOT,
    # which is the distinction that decides whether a word gets swallowed. `--log[=<n>]`
    # and `-S`/`--gpg-sign[=<key-id>]` take their argument ONLY attached, so modelling
    # them as value-taking would eat a following `--ff-only` and refuse a documented
    # recipe. Required-argument options are `--cleanup`, `-s`/`--strategy`,
    # `-X`/`--strategy-option`, `-m`/`--message`, `-F`/`--file`, `--into-name`.
    # 🔴 AND `-n` ON `merge` IS "do not show a diffstat", NOT a dry run — it is
    # deliberately absent from `short_bool`, and `merge` is absent from
    # `_DRY_RUN_SUBCOMMANDS`, so two independent things would have to be wrong for it
    # to be read as one.
    "merge": {
        "long_value": frozenset({"--cleanup", "--strategy", "--strategy-option",
                                 "--message", "--file", "--into-name"}),
        "long_bool": frozenset({
            "--ff-only", "--ff", "--stat", "--summary", "--compact-summary", "--log",
            "--squash", "--commit", "--edit", "--rerere-autoupdate",
            "--verify-signatures", "--verbose", "--quiet", "--abort", "--quit",
            "--continue", "--allow-unrelated-histories", "--progress", "--gpg-sign",
            "--autostash", "--overwrite-ignore", "--signoff", "--verify",
        }),
        "short_value": frozenset({"s", "X", "m", "F"}),
        "short_bool": {"e": "--edit", "v": "--verbose", "q": "--quiet"},
    },
}


def _resolve_long(token: str, grammar: dict[str, object]) -> tuple[str | None, bool]:
    """`(the canonical long option this token means, does it consume a value)`.

    Exact match first, then git's unambiguous-PREFIX rule. An unknown or ambiguous
    token resolves to `None`, and is reported as value-consuming if ANY candidate
    takes a value — the fail-CLOSED reading, and free, because git refuses an
    ambiguous abbreviation outright so nothing is written either way.

    ⚠ THAT LAST CLAUSE IS A WIDENING RATHER THAN A RESOLUTION, and what reaches it is
    ONE OPTION FAMILY IN **TEN** SPELLINGS — `--p --pa --pat --path --paths --pathsp
    --pathspe --pathspec --pathspec- --pathspec-f`, each a prefix of both
    `--pathspec-from-file` (value) and `--pathspec-file-nul` (boolean), and twenty once
    the `--no-` probes are counted. 🔴 AN EARLIER VERSION OF THIS SENTENCE SAID "EXACTLY
    ONE SHAPE", which was wrong by nine on a round whose subject was exhaustiveness
    claims being wrong by a count. The SAFETY conclusion is unchanged and is the part
    that matters: all twenty are one family, git answers every one
    `rc 129 ambiguous option` and writes nothing, so no verdict here can permit or
    prevent a write — which is why erring toward consuming the value costs nothing.
    """
    long_value: frozenset[str] = grammar["long_value"]        # type: ignore[assignment]
    long_bool: frozenset[str] = grammar["long_bool"]          # type: ignore[assignment]
    names = long_value | long_bool
    if token in names:
        return token, token in long_value
    candidates = sorted(name for name in names if name.startswith(token))
    if len(candidates) == 1:
        return candidates[0], candidates[0] in long_value
    return None, any(name in long_value for name in candidates)


def _option_state(subcommand: str,
                  rest: list[str]) -> tuple[dict[str, bool], list[str]]:
    """`(final boolean state per canonical long option, the OPERANDS)`.

    🔴 THE ONE PLACE THAT READS GIT'S OPTION GRAMMAR. Everything that used to match an
    option spelling now asks this: `_is_dry_run` reads `--dry-run` out of the flags,
    and `symbolic-ref`'s exemption reads `--delete` and counts the operands.

    Last-wins is why the flags are a dict rather than a set: `-n --no-dry-run` leaves
    `--dry-run` False and really deletes, measured. A value — attached, spaced, short
    or long — is consumed here and therefore can never be read as a flag NOR counted
    as an operand, which is what makes `git clean -f -e -n` and
    `git symbolic-ref -m <reason> HEAD <ref>` come out right. Everything after `--` is
    an operand, full stop.
    """
    grammar = _OPTION_GRAMMAR.get(subcommand)
    if grammar is None:
        return {}, list(rest)
    short_value: frozenset[str] = grammar["short_value"]      # type: ignore[assignment]
    short_bool: dict[str, str] = grammar["short_bool"]        # type: ignore[assignment]
    long_bool: frozenset[str] = grammar["long_bool"]          # type: ignore[assignment]
    flags: dict[str, bool] = {}
    operands: list[str] = []
    index = 0
    while index < len(rest):
        word = rest[index]
        index += 1
        if word == "--":
            operands.extend(rest[index:])
            break
        if word == "-" or not word.startswith("-"):
            operands.append(word)
            continue
        if word.startswith("--"):
            # 🔴 `sep`, NOT `attached`, AND `negated` IS CONSULTED — TWO ERRORS IN ONE
            # CONDITIONAL, EACH MEASURED TO DELETE A FILE IN THE BASE CLONE. The line
            # was `token, _, attached = …` with `if takes_value and not attached`:
            #
            #   * `partition` DISCARDS the separator, so `--exclude=` and
            #     `--pathspec-from-file=` gave `attached == ""` — falsy — and the model
            #     ate the NEXT word. git accepts the empty value and consumes nothing.
            #   * `negated` was computed one line up and never used here. `git rm -h`
            #     prints `--[no-]pathspec-from-file <file>`; the `--no-` form takes no
            #     argument, but stripping `--no-` to build `probe` resolved a
            #     value-taking canonical and the next word was eaten.
            #
            # Either way the swallowed word is read as NEITHER flag NOR operand, so a
            # following `--no-dry-run` went invisible and the dry-run exemption stood
            # while git deleted. Measured, with the verdict taken before the command:
            # `git rm -n --no-pathspec-from-file --no-dry-run seed.txt` was ALLOWED and
            # deleted a TRACKED file; `git rm -n --pathspec-from-file= --no-dry-run f`
            # and `git clean -n --exclude= --no-dry-run -f -d` the same. It cut the
            # other way too: `git clean --exclude= -n` and
            # `git rm --no-pathspec-from-file -n f` are real dry runs ("Would remove",
            # file present) and were REFUSED.
            token, sep, _attached = word.partition("=")
            negated = token.startswith("--no-")
            probe = "--" + token[len("--no-"):] if negated else token
            canonical, takes_value = _resolve_long(probe, grammar)
            if takes_value and not negated and not sep and index < len(rest):
                index += 1                      # its value, never a flag or operand
            if canonical is not None and canonical in long_bool:
                flags[canonical] = not negated
            continue
        for position, char in enumerate(word[1:], start=1):
            if char in short_value:
                # Attached (`-efoo`) when anything follows it in this word; spaced
                # (`-e foo`) when it ends the bundle.
                if position == len(word) - 1 and index < len(rest):
                    index += 1
                break
            canonical = short_bool.get(char)
            if canonical is not None:
                flags[canonical] = True
    return flags, operands


#: 🔴 `-h` AND `--help` ARE READS FOR EVERY MEMBER OF `_REFUSED`, AND THAT IS
#: MEASURED FOR ALL FOURTEEN RATHER THAN ASSUMED FROM "git uses parse-options".
#: On git 2.55.0, `git <sub> -h` answers **rc 129 with `usage:` on the first line and
#: the repository bit-for-bit unchanged** for `add am apply cherry-pick checkout clean
#: commit merge mv rebase reset rm stash switch` — all fourteen, no exception, so
#: there is no subcommand here needing `--help` only. (`git grep -h` means
#: `--no-filename`, which is why the question was asked; `grep` is not refused.)
#: `--help` execs the manual page: rc 0, repository unchanged, all fourteen.
#:
#: 🔴 REFUSING `--help` WAS THE PUREST FALSE POSITIVE THIS FILE COULD EMIT, and it
#: was live: the exemption existed for `stash` ALONE, so `git rm -h`, `git mv -h`,
#: `git clean -h`, `git commit --help` and `git rebase --help` were all DENIED while
#: the docstring's own list named `--help` as a read. A corpus replay over the
#: project's real Bash history found TWO commands that are exactly this shape, so it
#: had already fired falsely. One predicate at a second site; now at neither.
#: ⚠ AN EXACT-STRING SET, AND UNLIKE THE THREE THAT WERE REPLACED BY `_OPTION_GRAMMAR`
#: THAT IS CORRECT HERE — MEASURED, SO THE NEXT READER DOES NOT "FIX" IT. git does NOT
#: abbreviate `--help`: `git clean --hel` and `git clean --h` both answer
#: `error: unknown option` (rc 129). `-h` has no bundle form either, since parse-options
#: intercepts it before any bundle is read. So there is no prefix, bundle or negation to
#: route through a grammar, and routing it through one would only add a table to keep.
_HELP_SPELLINGS = frozenset({"-h", "--help"})


def _is_read_only_spelling(subcommand: str, rest: list[str]) -> bool:
    """🔴 THE ONE PLACE THAT ANSWERS "does a FLAG make this refused subcommand a
    read?" — consulted by `_is_exempt` for every subcommand, never per-subcommand.

    Two kinds of answer live here because they are the same question. Both used to be
    open-coded per subcommand and both were wrong at a site: the dry-run half was
    `clean`-only (so `git rm -n` was refused) and the help half was `stash`-only (so
    `git rm -h` was refused). What stays in `_is_exempt` is the per-RECIPE
    exemptions — `merge --ff-only`, the pathspec `checkout`, `stash list` — which are
    a different category: those name a documented workflow, these name a flag that
    makes the command write nothing.

    🔴 HELP IS READ FROM `rest[0]` ONLY, AND SCANNING THE WHOLE TAIL WOULD BE A
    FAIL-OPEN — MEASURED, with a positive control in the same run. `git commit -m -h`
    with a staged change **creates a commit**, subject `-h`: the `-h` is the MESSAGE,
    sitting in a flag's value position, and `git commit -m X` committed in the same
    run so the instrument was known to work. A tail scan would therefore allow a real
    base-clone commit. No flag's value can occupy `rest[0]`, which is what makes that
    position sound without modelling any subcommand's arity.

    ⚠ THE COST IS TWO RESIDUAL FALSE POSITIVES, STATED RATHER THAN HIDDEN:
    `git clean -i -h` and `git commit -m X -h` are help requests (rc 129, `usage:`,
    repository unchanged — measured) and stay REFUSED, because their `-h` is not
    first. Nobody types either; every spelling the corpus actually contains has the
    flag first. ⚠ AND A CLUSTER IS NOT READ FOR AN `h`: `git clean -fh` is also help
    (measured), and is also still refused. Widening to clusters would need `h` to
    mean help inside a cluster for all fourteen, which was not measured — and the
    cheap direction for an unmeasured widening is not to make it.
    """
    if rest and rest[0] in _HELP_SPELLINGS:
        return True
    return _is_dry_run(subcommand, rest)


def _is_dry_run(subcommand: str, rest: list[str]) -> bool:
    """Is this a DRY RUN of a refused subcommand, and therefore a read?

    ONE QUESTION ASKED OF ONE MODEL: is `--dry-run`'s final state True? The spellings
    that work are `-n`, `--dry-run`, any unambiguous PREFIX of it (`--dry`, `--d`), a
    bundle like `-nd`, and the LAST-WINS pair in both orders — each measured.

    🔴 "EVERY SPELLING GIT ACCEPTS WORKS FOR FREE, IN BOTH DIRECTIONS" IS WHAT THIS
    DOCSTRING SAID, AND IT WAS FALSE — a differential fuzz against real git found
    divergences on options PRESENT IN 2.55.0, not future ones, so the claim was not
    covered by `_OPTION_GRAMMAR`'s version caveat either. It is the claim that stopped
    anyone looking. What the model covers is **the option set written down in
    `_OPTION_GRAMMAR` for the subcommand in question, under git's prefix, bundling,
    value and last-wins rules**. What it does NOT cover, stated so the next reader
    starts here: an option absent from those tables (see the version limit there); an
    option whose argument is OPTIONAL rather than required, which is listed as a
    boolean on purpose and would be mis-modelled if listed as value-taking; and
    anything outside the option grammar entirely — a pathspec that looks like a flag, a
    configured alias, `-c` config overriding a default. The tables are DATA, and data
    is exactly as complete as somebody measured it to be.

    ⚠ ONLY `clean`, `mv` AND `rm` ARE IN THE DRY-RUN SET, and the grammar holding a
    `symbolic-ref` entry does not change that: `--dry-run` is not in that
    subcommand's `long_bool`, so it can never resolve there. 🔴 TWO CATEGORIES SIT
    OUTSIDE THE SET AND THEY ARE NOT THE SAME CATEGORY: `add -n` and `apply --check`
    are genuine reads left out by an operator DECISION about scope creep, while
    `commit --dry-run` WRITES A TREE OBJECT and must never be exempted at all. The
    bullet list in this module's docstring says so in those terms, because a single
    heading over both is what would license widening the wrong one.

    🔴 `rest` IS THE WORDS AFTER THE SUBCOMMAND, NOT THE SEGMENT, AND THAT IS A
    MEASURED FAIL-OPEN REPAIR RATHER THAN TIDINESS — `_is_exempt` narrows it; the
    measurement is there.
    """
    if subcommand not in _DRY_RUN_SUBCOMMANDS:
        return False
    flags, _ = _option_state(subcommand, rest)
    return flags.get("--dry-run", False)


def _is_exempt(subcommand: str, segment: list[str]) -> bool:
    """Is this SEGMENT one of the documented recipes that must not be refused?

    Per segment, so chaining two exempt recipes is not refused — it was, while
    the exemptions were gated on the whole command yielding a single hit.

    🔴 AND EVERY EXEMPTION READS ONLY THE WORDS **AFTER THE SUBCOMMAND**, WHICH IS A
    FAIL-OPEN REPAIR WITH A MEASUREMENT BEHIND IT. Three of these four used to scan
    the whole segment, so a flag belonging to a WRAPPER excused the git write behind
    it. Measured on a miniature clone with the bare spelling DENYing in the same run:

        nice -n 5 git clean -fd          ALLOW — `-n` is nice's adjustment flag
        xargs -n 1 git rm seed.txt       ALLOW — `-n` is xargs' max-args
        env -- git checkout other-branch ALLOW — `--` is env's end-of-options
        sudo --ff-only git merge <ref>   ALLOW — not even a real sudo flag

    The `env` row is the oldest: it was ALLOW before any of this work, because `env`
    was already skipped as a leading word while the `checkout` exemption searched the
    whole segment for a `--`. The other three became reachable when `_LEADING_WORDS`
    started skipping `nice`/`xargs`/`sudo` — a widening that turned a latent flaw in
    a NEIGHBOURING predicate into a live bypass, which is the shape worth recording:
    the defect was not in the code that changed. One narrowing closes all four,
    because there is one place that computes `rest`.

    ⚠ IT IS STILL APPROXIMATE IN THE OTHER DIRECTION: `segment.index(subcommand)`
    takes the FIRST occurrence, so a global option whose VALUE happens to equal the
    subcommand (`git -C clean clean -n`) starts the scan one word early. That can
    only make the scan WIDER, never narrower, so it cannot refuse a dry run — and a
    position-aware walk would have to model every global option's arity, which
    `_GIT_GLOBALS_WITH_VALUE` says outright that it does not.
    """
    try:
        rest = segment[segment.index(subcommand) + 1:]
    except ValueError:
        # The subcommand came out of this segment, so this cannot happen — and if it
        # somehow does, no exemption applies and the refusal stands, which is the
        # fail-CLOSED direction this function must take when it cannot tell.
        return False
    # 🔴 THE FLAG-SHAPED EXEMPTIONS ARE ASKED FIRST, FOR EVERY SUBCOMMAND, FROM ONE
    # PLACE. Both halves of `_is_read_only_spelling` used to be per-subcommand
    # branches here and both were wrong at a site: the dry run was `clean`-only
    # (refusing `git rm -n`) and the help was `stash`-only (refusing `git rm -h`).
    if _is_read_only_spelling(subcommand, rest):
        return True
    if subcommand == "merge":
        # 🔴 THROUGH THE GRAMMAR MODEL, NOT `"--ff-only" in rest`. That exact match
        # refused `git merge --ff-o origin/main` — a spelling git accepts — which is a
        # false positive on the base-clone resync recipe this repo prescribes. It also
        # went the other way: `git merge -m --ff-only <ref>` is a real merge whose
        # MESSAGE is `--ff-only`, and the string match exempted it.
        flags, _ = _option_state(subcommand, rest)
        return flags.get("--ff-only", False)
    if subcommand == "checkout":
        # The pathspec form. `--` is what makes it one, and it does not move HEAD.
        # ⚠ STILL A LITERAL COMPARISON, AND CORRECTLY SO: `--` is the end-of-options
        # SEPARATOR, not an option, so git does not abbreviate it and there is no
        # grammar to route it through.
        return "--" in rest
    if subcommand == "stash":
        # ⚠ `--help` IS DELIBERATELY GONE FROM THIS TUPLE, not lost: it is handled
        # above for every subcommand now, and leaving a copy here would be the third
        # site of the predicate that this change exists to remove.
        return bool(rest) and rest[0] in ("list", "show")
    if subcommand == "symbolic-ref":
        # 🔴 THE READ FORM AND THE WRITE FORM DIFFER BY ONE OPERAND, WHICH IS WHY
        # THIS SUBCOMMAND NEEDS AN EXEMPTION AT ALL. Measured on git 2.55.0:
        # `git symbolic-ref HEAD` (and `--short`/`-q`) PRINTS the ref and changes
        # nothing, while `git symbolic-ref HEAD refs/heads/<other>` rewrote
        # `.git/HEAD` — the shared HEAD moved under a peer, silently, which is the
        # exact failure this whole file exists for. Refusing the one-operand form
        # would refuse the ordinary way to ask which branch is checked out.
        #
        # ⚠ COUNTED, NOT PATTERN-MATCHED, and both halves come from the ONE option
        # model: a flag is not an operand, nor is a flag's value, so `--short HEAD`
        # and `-m <reason> HEAD` are still reads. `--delete` is a WRITE with one
        # operand, so it is read out of the resolved flags rather than by name.
        #
        # 🔴 READING IT BY NAME WAS WRONG IN FOUR SPELLINGS, EACH MEASURED TO DELETE A
        # REAL REF: `any(word in ("-d", "--delete"))` missed the bundles `-qd` and
        # `-dq` and the prefixes `--del` and `--d`. Branch refs live in the COMMON git
        # dir — the file's own stated reason `branch -D` could not be scoped — so that
        # was reachable from any worktree. `_option_state` resolves all four now, and
        # `--no-delete` correctly cancels (measured: it printed the ref).
        flags, operands = _option_state(subcommand, rest)
        return len(operands) <= 1 and not flags.get("--delete", False)
    return False


#: Memo for `_repo_root`. It is asked about every `-C` target as well as every
#: probed directory, and a command line may carry many of each. 🔴 THIS AND
#: `_PROBED` ARE WHAT MAKE "NO PROBE CAP" AFFORDABLE: the cost of a long command
#: line is distinct DIRECTORIES, not segments, so the shapes a cap was added for
#: are already cheap. Two caps were tried and both were measured to convert a DENY
#: into an ALLOW; the note above `_allow` carries the bisects.
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

    🔴 IT DOES NOT ASK WHETHER THE DIRECTORY EXISTS, AND THE REASON IS NO LONGER
    THE ONE THIS COMMENT GAVE FOR TWO ROUNDS. It said a second existence check here
    "could never change a verdict", because `_protected` has to make that check
    anyway. **RE-MEASURED, AND IT IS FALSE.** Driven by restoring exactly that
    check (`if not os.path.isdir(resolved): return None`) and running the whole
    suite plus a NUL battery against both copies:

      * `GIT_INDEX_FILE=<the clone>/.git/index git add <file>`, run from a linked
        worktree, is **deny** without the check and **ALLOW** with it — one test
        failure out of 176, and it is the one pinning a payload measured to rewrite
        the clone's index. The mechanism is that `_GIT_FILE_ENV_NAMES` values name
        a FILE, so `isdir` is false for a target that is perfectly real; the
        dirname that makes it a directory is taken downstream, in
        `_ambient_targets.record`. A check here is therefore not a duplicate of
        `_protected`'s at all — it asks a different question of a different value.
      * ⚠ THE EARLIER FALSIFICATION IS A SEPARATE ONE AND IT IS NOW CLOSED. A NUL
        byte in a target used to crash the hook (rc 1, a silent ALLOW); `_git` now
        catches `ValueError`, and all five NUL spellings — `-C`, `--git-dir`, `cd`,
        `GIT_DIR`, `GIT_INDEX_FILE` — answer **deny at rc 0 on both copies**, so
        that dimension no longer separates them. Measured from the base clone and
        from a linked worktree; naming both points because the verdict depends on
        which.

    So: the check stays out, but on the measurement above rather than on the
    "could never change a verdict" claim, which was wrong in the fail-OPEN
    direction. The existence check this guard DOES make lives once, in
    `_protected`, ahead of its three `git` spawns so a junk path costs nothing —
    and ⚠ a draft that ALSO put one here was scored **unreachable** by a mutation
    sweep, which is a fact about the sweep: it scored VERDICTS, and the two cases
    above are a crash and an environment variable it never varied.

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

    def record(name: str, value: str) -> None:
        """Judge one variable's value, as a directory or as a file's parent."""
        resolved = _abs_path(value, cwd)
        if not resolved:
            return
        # A FILE-valued variable identifies its repository by the directory it sits
        # in: `GIT_INDEX_FILE=<clone>/.git/index` is a write to `<clone>/.git`,
        # which `_protected` recognises as the main worktree. Taking the dirname
        # here rather than teaching `_protected` about files keeps that function
        # answering exactly one question about exactly one directory.
        if name in _GIT_FILE_ENV_NAMES:
            resolved = os.path.dirname(resolved)
        if resolved:
            env_dirs.append(resolved)

    for name in _GIT_DIR_ENV_NAMES + _GIT_FILE_ENV_NAMES:
        record(name, os.environ.get(name, ""))
    for segment in segments:
        for word in segment:
            for name in _GIT_DIR_ENV_NAMES + _GIT_FILE_ENV_NAMES:
                if word.startswith(name + "="):
                    record(name, word.split("=", 1)[1])
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

def _protected(path: str, own_repo: str) -> tuple[str, int] | None:
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
    # A path that is not a directory NOW cannot be the base clone — this hook runs
    # before the command does, so a directory the command is about to create does
    # not exist yet. A cheap `stat` ahead of up to three `git` spawns, and nothing
    # more: ⚠ IT CANNOT CHANGE A VERDICT, because `git` fails on a nonexistent cwd
    # by itself. A mutation sweep scored it unreachable for exactly that reason.
    if not os.path.isdir(path):
        return None
    if path in _PROBED:
        return _PROBED[path]
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

    # 🔴 ONE PASS OVER EVERY JUDGED DIRECTORY, AND THE ABSENCE OF A SECOND IS THE
    # FIX. Two earlier drafts split this into a primary pass and an additive pass
    # so that a probe BUDGET could be applied to one of them; both arrangements
    # were measured to turn a DENY into an ALLOW at the bound (the comment above
    # `_PROBED`'s neighbours carries the two bisects). With no budget there is
    # nothing for an ordering to protect, so there is no ordering to get wrong.
    #
    # The primary directory still comes FIRST within each segment's list, and that
    # is now purely cosmetic: it decides which directory the refusal MESSAGE names,
    # and naming the one the command actually runs in reads better than naming a
    # `cd` target. Nothing about the verdict depends on it.
    env_dirs, cd_dirs = _ambient_targets(segments, cwd)

    hits: list[str] = []
    found: tuple[str, int] | None = None
    for subcommand, segment, _ in candidates:
        for path in _judged_dirs(segment, cwd, env_dirs, cd_dirs):
            verdict = _protected(path, own_repo)
            if verdict:
                hits.append(subcommand)
                found = found or verdict
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


# 🔴 THE HEADER'S PROMISE — "every unexpected condition here exits 0 and says
# nothing" — IS MADE STRUCTURAL HERE RATHER THAN LEFT TO EVERY `except` CLAUSE
# BEING COMPLETE. One was not: a NUL byte in a `-C` value raised `ValueError`
# past `_git`'s handler and crashed the hook with rc 1, which SILENTLY ALLOWS —
# a payload the base ref denied. `_git` now catches it at the source, and this
# exists so the NEXT such gap is a quiet allow instead of a crash, which is the
# outcome this file already documents for a malformed payload.
#
# ⚠ `SystemExit` IS NOT AN `Exception` SUBCLASS, and that is what makes this safe
# rather than a disaster: the `sys.exit(0)` inside `_allow` and `_deny` passes
# straight through, so wrapping the body cannot swallow a REFUSAL and convert it
# into an allow. That is the one way this line could be wrong, so it is stated.
try:
    main()
except Exception:
    sys.exit(0)
