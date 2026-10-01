# Working this repository in parallel

Several sessions and dispatched agents work cairn at the same time, through linked
worktrees of ONE clone. This file is the recipe and the reasons. It is **read on demand
and therefore free** — `AGENTS.md` sits a handful of bytes under an enforced ceiling, so
nothing here is paid for by every session, and the routing to it is a hook's refusal
message rather than a sentence somebody has to have read.

Most of these rules were **ported from a sibling infrastructure repository in the same
fleet**, where they were learned the expensive way. They are attributed by ROLE rather
than by name throughout: that repo's name is a denied identifier here, and this repo's
leak gate is correct to refuse it. Where a rule ported with its direction REVERSED, that
is called out — an inherited rule applied to the wrong world is the failure mode this
document is most likely to cause.

🔴 **The one structural guard.** `.claude/hooks/base-clone-write-guard.py` refuses
tree-mutating git in the base clone while linked worktrees exist. It is prose-independent
on purpose, it **fails OPEN**, and it is not a security boundary. Read its docstring
before changing it; the refused-subcommand ledger below is pinned against it by
`tests/test_base_clone_write_guard.py`, so the two cannot drift.

## The recipe, for every change

```bash
REPO=/path/to/cairn                       # the base clone
WT=<scratchpad>/wt-<topic>                # any path; see the nested-worktree note
git -C "$REPO" fetch origin
git -C "$REPO" worktree add "$WT" -b <branch> origin/main   # base on the REMOTE tip
git -C "$WT" push -u origin HEAD:<branch>                   # push the branch NOW, empty
# …edit, test and commit INSIDE $WT, staging explicit paths…
git -C "$WT" add <specific paths> && git -C "$WT" commit -m "…"
git -C "$WT" push origin HEAD:<branch>                      # HEAD:<branch>, never `origin main`
git -C "$REPO" worktree remove "$WT"      # 🔴 ONLY after the push SUCCEEDED
```

**Push the branch the moment you create it, before doing the work.** A branch is visible
to `git ls-remote` the instant it lands, and that is the only thing that makes a
concurrent duplicate detectable. The claim lock does not cover this — see the last
section.

## Why each step is the way it is

### 🔴 Never commit, stage or switch in the base clone

The branch checked out there is **unpredictable**: a peer session or your own dispatched
subagent may have moved it, and a docs-only or read-only agent is exactly the case where
worktree isolation gets skipped, so its `git checkout -b` lands in the shared tree.

*Why a wrong-branch commit is the silent one, and the `git branch --show-current` /
`git reflog` habits, are the fleet 🔴 rule in `claude/RULES.md` — not restated here. The
guard's refusal message carries the one copy that reaches a reader at the moment it
matters.*

The refused set, which the guard enforces and its test pins against this table:

| refused | why it collides |
|---|---|
| `add` | stages into the shared index |
| `commit` | lands work on whatever branch the peer left checked out |
| `checkout` *(bare)* / `switch` | moves the shared HEAD under a peer |
| `reset` | moves the shared HEAD and index |
| `rebase` / `merge` / `cherry-pick` / `am` / `apply` | rewrites or advances the shared tree |
| `stash` | the stack is repo-GLOBAL, not per-worktree — see below |

**Not refused, deliberately**, because each is a documented recipe and a guard that breaks
one trains everybody to route around it: `git merge --ff-only <ref>` (the base-clone
re-sync — it cannot conflict or autostash, it either fast-forwards or refuses, and the
refusal is the signal that the clone diverged); `git checkout <ref> -- <paths>` (the
pathspec form does not move HEAD — bare `git checkout <branch>` IS refused, because that
moves the shared HEAD); `git stash list`, `show` and `--help`; and every other read,
including `push`, which touches no file in the clone.

⚠ **`git restore` is NOT in the refused set at all**, so it never reaches an exemption. An
earlier version of this paragraph listed it among the deliberate exemptions, which reads as a
licence to add it to the ledger — and doing so would refuse **every** `git restore <path>`,
because the ordinary form carries no `--`. The hook's docstring carries the same warning
beside the code; this is the copy the refusal message routes readers to, so the two have to
agree.

### ⚠ A worktree nested inside the repo root is HANDLED — this used to be a 🔴 rule and it was wrong

**Do not re-derive the rule that worktrees must live outside the repo root.** It was
true once and is not now, and the harness's own `isolation: "worktree"` places agent
worktrees at `.claude/worktrees/` — *inside* the root — so a 🔴 forbidding that would
forbid the default mechanism.

Both hazards it named are closed, re-measured with a nested worktree actually present:

- `go test ./...` from the base clone → **19 ok / 0 FAIL**, where the root-walking guard
  previously reported 24 offenders naming real source files. Closed by
  `depspolicy.NestedModuleDirs` asking git instead of walking, in `#135` / `c6aed4e`, whose
  comment explicitly rejects denylist-widening as the wrong fix.
- `tests/leakscan.py` → **rc 0, 0 findings**, by **two independent mechanisms**, which is
  worth separating because the second one is this change's own doing:
  - `directory_skip_reason` (`#127`) *names* an enumerated nested checkout as a skip, with
    its reason — git collapses an untracked nested repository to one entry whose contents
    belong to that repository. This is what protects a clone that does **not** ignore the
    directory.
  - and since this change gitignores `.claude/worktrees/`, the directory is no longer
    enumerated at all here: `git ls-files --others --exclude-standard` yields **0** matches
    for it, so the scan reads `406 file(s) scanned, 1 skipped` and that skip is
    `tests/leakscan.py` itself.

  ⚠ **An earlier draft of this section cited `402 scanned, 2 skipped` and "it names the
  nested checkout as a skip" as the evidence — and this change falsified its own citation.**
  The conclusion held; the quoted measurement stopped reproducing the moment the ignore rule
  landed two files away. **A measurement is scoped to the tree it was taken on, and a
  `.gitignore` line is a change to what every enumerating tool can see.**

What survives is a **convention, not a rule**: a scratchpad worktree keeps `git status`
quiet and never interacts with the source filters at all. Either location works.

🔴 **The lesson worth more than the rule: this section shipped as a 🔴 because it was written
from the handoff's `Defects` section, which still carried the entry, rather than from the
ranked item recording the fix.** That section REPLACES rather than appends, so a closed
entry survives every update that does not retype it — and a closing condition met by a
merged PR closes nothing until somebody edits the entry. **Before porting a hazard from a
defect list, check whether the item that closes it is marked done.**

### 🔴 Gate `worktree remove` on a SUCCESSFUL push

Removing a worktree after a **failed** push (a busy remote, a non-fast-forward) deletes
the branch ref and **orphans the commit**. Recovery is `git fsck --dangling`, matching the
subject, then `git cherry-pick <sha>` in a fresh worktree. Ported from the sibling repo,
where it happened twice in one day. If the push is rejected: fetch and rebase *in the
worktree*, re-push, and only then remove.

### 🔴 A branch another worktree holds needs neither a force-push nor a removal

Branch alongside it. A new local branch off `origin/<branch>`, committed and pushed as
`HEAD:<branch>`, is a **fast-forward** — assert it with
`git merge-base --is-ancestor origin/<branch> HEAD` BEFORE the push. The remote advances,
nothing is rewritten, the other worktree is undisturbed. A rebase would have needed
`--force-with-lease` and bought nothing.

### 🔴 `git stash` is repo-GLOBAL — never use it here, for any reason

The fleet 🔴 rule in `claude/RULES.md` applies unchanged and is not restated here; every
agent on this host loads it every session. It is in the refused set so the rule survives on
a host that does not run the fleet guard. `git stash list`/`show`/`--help` stay allowed — they
are reads, and
a non-empty stack is itself proof the stack is shared.

### ⚠ `.envrc` — the ported rule REVERSES here, and a brief already got it wrong

The general fleet rule is that `.envrc` is gitignored, so a fresh worktree lacks the dev
shell and you must copy it in. The sibling repo's rule is the opposite: `.envrc` is
TRACKED there, so `worktree add` already provides one and a "helpful" copy-then-cleanup
`rm` stages the deletion of a tracked file.

**Neither applies to cairn: this repo has no `.envrc` at all.** A dispatch brief asserted
it had one and was wrong. So there is nothing to copy and nothing to protect — instead,
**run every toolchain-dependent gate through the flake explicitly**:

```bash
nix develop <repo> -c go test ./...
nix develop <repo> -c python3 -m pytest tests -q
```

🔴 This is not hygiene. Measured on this host: bare `go version` is **1.26.7** while
`nix develop -c go version` is **1.25.14**, which is the version `go.mod`, `flake.nix` and
CI pin. Every `go vet`/`go test` run outside the devShell is a green about a toolchain this
repo does not ship, and **nothing warns**. Ask which toolchain a green came from.

### 🔴 `(cd $WT && …)`, never `<tool> $WT/scripts/…`

A script invoked by absolute path still resolves its inputs from your **CWD**, so running
one tree's tool while standing in another grades the tree you are standing in — and says so
in a perfectly ordinary green banner. cairn has exactly this shape: `tests/leakscan.py`,
`tests/pgtest/run.sh` and `tests/conformance/run_go.sh` all read repo-relative data. **A
verdict naming a file count you did not expect is the tell; read the count, not the
checkmark.**

### 🔴 `git checkout <ref> -- <file>` gives you a FILE, not a working TOOL

For READING a doc at a ref it is fine. For RUNNING anything it is not: a script pulled that
way arrives without its sidecar data (ignore lists, baselines, fixtures) and then executes
against the rest of that clone's *stale* tree, so a new script silently scans an old repo.
In the sibling repo that combination produced a whole false-alarm cycle. To run something
against a ref, use a throwaway worktree.

### 🔴 Never `nix build --out-link` into the repo root

`result`/`result-1` are untracked symlinks to store **directories**; `leakscan`'s
`--others` enumeration reads them and dies `[Errno 21]`, taking the repo's own tests red
with it. Build with `--out-link <scratchpad>/…`. To retire an existing one without losing
the store path: `nix-store --add-root <a path outside the repo> --indirect --realise <the
store path>`, and only then `rm result`.

## ✅ The base-clone staleness hook covers this repo now — it did not, and the gap is worth knowing

The fleet runs a `SessionStart` hook that refreshes the agent-context files of whatever
repo the session's cwd is in, so a stale base clone cannot serve stale, authoritative-looking
instructions. **Its path list used to be `CLAUDE.md` and `.claude/skills`, and that missed
cairn entirely — by a filename.** `CLAUDE.md` here is a **267-byte stub** whose whole payload
is `@AGENTS.md`, while `AGENTS.md` is **31,330 bytes** and is what actually loads. The hook
faithfully refreshed a file that never changes and never touched the one that matters — the
exact failure it exists to prevent, walked around rather than triggered.

**Half fixed, and the open half is the one this change creates.** `AGENTS.md` was added to
that list and **is live on this host** — so the 31 KB file that actually loads is refreshed
now.

🔴 **`.claude/settings.json` and `.claude/hooks` are NOT in the list, and this change is what
makes that matter.** Those paths are tracked here as of this PR and they **execute**, so a
stale clone serves a stale GUARD — strictly worse than a stale doc, because a hook that
silently fails to fire is indistinguishable from one that allows and there is **no symptom at
all**. Widening the list for them is an open PR in the repo that owns the hook, not a landed
change; the `dirname` shape differs between a directory entry and a nested FILE entry, and
getting it wrong deletes `.claude`, so it is not a one-line edit. **Until it lands, treat this
directory as unrefreshed** and read it from the ref like anything else.

🔴 **Verify rather than trust this paragraph**, because it is a claim about another repo's
state and about a `home.file` copy that only a `home-manager switch` makes live:

```bash
grep -n 'REFRESH_PATHS=' ~/.claude/hooks/base-clone-staleness.sh   # the DEPLOYED copy
```

⚠ **A refreshed file reads as dirty.** The hook does not move HEAD, so a tracked file it
fixed shows as modified-vs-HEAD until the clone's branch catches up. **A tracked context file
dirty in the base clone whose content is byte-identical to `origin/main` is the hook's doing,
not somebody's unsaved work** — do not "rescue" it, and do not let it mask real WIP.

Whatever the list says, for any OTHER load-bearing doc claim the rest of the tree is still as
stale as the clone, so read it from the ref:

```bash
git -C <repo> fetch origin && git show origin/main:<path>
```

The tell that you need to is a claim *doing work* in your plan — a limit, a pin, a "this is
impossible", a gate's stated blind spot.

🔴 **The lesson, because this section was itself stale within half an hour of being written:**
an earlier draft asserted the gap as open and prescribed a workaround "until the fleet fix
lands" — while the fix had merged **27 minutes earlier** and was already live. A round-1 audit
caught it. **A sentence about another repo's state is a claim with a shelf life measured in
minutes when you are the one changing that repo.**


## ⚠ The claim lock cannot separate two sessions on one clone

`claim-work` keys ownership on **host + worktree**, not on conversation. Measured on this
arc: two concurrent sessions working from the same base clone both received
**rc 12 — "ALREADY YOURS, carry on"** for the same ranked item, because the owner-id is
identical. Read the printed `who:`/`when:`/`where:` before treating rc 12 as proof you took
it, and treat `gh pr list --state open` as the only surface that can see an **unclaimed**
duplicate. Run that sweep twice: at orientation, and again immediately before
`gh pr create` — the window is around twenty minutes and the second moment is where the
sunk cost is highest.

## 🔴 What the guard judges, and the one thing it still cannot see

It judges **the directory the command WRITES to**, not the one the shell is standing in.
That was the other way round for two rounds, and keying on the cwd was wrong in both
directions at once — it refused writes that went elsewhere, and it missed writes that came
back in. Both halves are closed; the table below is what each spelling does now, and every
row was MEASURED against a miniature clone with a real base-clone write denying in the
same run as the control.

| spelling | verdict |
|---|---|
| `git -C <the base clone> commit …` from a worktree, or from another repo | REFUSED |
| `git -C <a linked worktree> commit …` from the base clone | allowed |
| `git -C <ANOTHER repo's worktree> commit …` from the base clone | allowed |
| `git -C "$WT" …` — any `$VAR` target, however it is assigned | REFUSED — never resolved |
| `git --git-dir=<the clone>/.git …` / `--work-tree=<the clone> …` | REFUSED, from anywhere |
| `git --git-dir=<a linked worktree's git dir> …` | allowed |
| `GIT_DIR=<the clone>/.git git …`, or `GIT_DIR` already exported | REFUSED |
| `cd <the base clone> && git commit …` | REFUSED |
| `cd <a worktree> && git commit …` from the clone | REFUSED — see below |
| `bash -c 'cd <the base clone> && git commit …'` | NOT SEEN — still open |

🔴 **`$VAR` TARGETS ARE REFUSED, NOT RESOLVED, AND THE RECIPE ABOVE IS WRITTEN IN
EXACTLY THAT SPELLING — so `git -C "$WT" commit` from the base clone is refused and
you must pass an absolute path.** This is the one place the guard is deliberately
less convenient than it could be, and the reason is measured. A version that
resolved what the command text assigns opened four fail-opens, because **knowing a
name is assigned somewhere in the text is not knowing the shell will have assigned
it**: a subshell assignment is discarded, a short-circuited one never runs, one in
an untaken branch never runs, and a command *prefix* scopes to that command only.
In each, bash leaves `WT` unset, so git runs `git -C ""` — **in the current
directory, the clone**. All four were ALLOW with the resolver and DENY without it;
one was proved end to end, the clone going 1 → 2 commits while the worktree stayed
at 1. The resolver did not relax the fail-closed fallback — it made it
**unreachable** in those shapes, which is the harder failure to see.

⚠ **Two rows are deliberately asymmetric and neither is an oversight.**

A **`cd` target is judged IN ADDITION to the caller's directory, never instead of it.**
Deciding that a `cd` *replaces* the caller needs bash's positional model — `( … )` does not
persist, `{ … }` does — and a wrong model there fails OPEN, which is the one direction this
guard may not fail in. So a `cd` into the clone is caught, while a `cd` *out* of it is still
refused. **Use `-C` instead**, which the guard resolves exactly.

**`bash -c '…'` is one quoted token** to this parser, so nothing inside it is read as a
command. The fleet's `guard_core.py` closes the equivalent, and spends two separate
recursion budgets to do it.

🔴 **"Reuse the fleet guard's resolution" was this document's advice for two rounds, and it
is not available — saying so is what unblocked the repair.** `guard_core.py` lives in the
operator's `~/.claude/hooks/`, is not tracked here, and ships to two known hosts; this hook
is tracked in a **public** repository and must run on a stranger's clone with nothing beside
it. An import would be a missing module everywhere else, and a `PreToolUse` hook that fails
to start is silently an **allow** — so the guard would go inert exactly where it is the only
one present. The resolution here is a second implementation on purpose, and much smaller:
four redirection spellings, **no variable resolution at all**, no sourced files, and
**everything it cannot resolve falls back to judging the caller's directory** — the
fail-CLOSED direction, which is what lets it stay small.

⚠ **And it is inert in the other runtime.** Only Claude Code reads `.claude/settings.json`;
the opencode plugin spawns `guard_core.py` and never consults this file. A rule the fleet
states for *both* runtimes is enforced here in one.

⚠ **It costs ~23 ms on every Bash call** — a full `python3` spawn on the hot path, which
roughly doubles per-call guard latency in a clone where condition 3 is satisfied. Accepted,
and recorded so it is a decision rather than a surprise.

## ⚠ What this document does not do

- It does not make the base clone safe to read. Nothing here prevents a stale tree from
  answering a question wrongly; the staleness section above is the mitigation, and it is
  partial.
- It does not stop two sessions editing the SAME FILE from two worktrees. Worktree
  isolation prevents clobbering a working tree, not a semantic conflict — the handoff doc
  in this repo was nearly lost that way, by two sessions writing it within one hour, and
  the only thing that caught it was a write tool warning about durable lines it was about
  to drop.
- The guard is lexical and approximate. It cannot see a subcommand held in a variable,
  behind `eval`, or inside a shell function, and it allows anything it cannot parse.
