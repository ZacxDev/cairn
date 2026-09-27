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
WT=<scratchpad>/wt-<topic>                # 🔴 OUTSIDE the repo root — see below
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

**A `commit` onto the wrong branch is the silent one** — no conflict, no error, and
`git log` afterwards shows exactly what you expect, because you are reading the branch you
landed on. `git branch --show-current` immediately before a commit removes the whole
class; `git reflog` is the one-command diagnosis when a branch looks like it moved
backwards.

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
refusal is the signal that the clone diverged); `git checkout <ref> -- <paths>` and
`git restore` (the pathspec form does not move HEAD); and every read, including `push`,
which touches no file in the clone.

### 🔴 The worktree goes OUTSIDE the repo root

**This is a cairn-specific rule and it inverts the convenient default.** A checkout of
cairn sitting *inside* cairn is indistinguishable, to anything that walks from the repo
root, from cairn's own files. Measured here: a root-walking Go guard reported two dozen
offenders naming real source files, and `tests/leakscan.py` exits **2** — "could not
vouch", not "passed" — on directory entries it cannot read. The tell is a FAIL naming
paths that contain the repo's own name twice. Put worktrees in the session scratchpad.

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

`refs/stash` lives in the **common** git dir (`git rev-parse --git-common-dir`), not the
per-worktree dir, so every worktree of the clone pushes and pops the SAME stack. Your own
worktree gives you **zero** isolation: a stash inside it sweeps up another worktree's
uncommitted work, and a later `pop` drops someone else's changes into your tree. To set
work aside, **copy it aside** (`cp <file> <scratchpad>/…`, restore by copying back) or
commit it to a throwaway branch. `git stash list` is a safe READ, and a non-empty stack is
itself proof the stack is shared.

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

## 🔴 The base-clone staleness hook does NOT cover this repo's instructions

The fleet runs a `SessionStart` hook that refreshes the agent-context files of whatever
repo the session's cwd is in, so a stale base clone cannot serve stale, authoritative-looking
instructions. Its path list is `CLAUDE.md` and `.claude/skills`.

**That list misses cairn entirely, by a filename.** Measured: `CLAUDE.md` here is a
**267-byte stub** whose whole payload is `@AGENTS.md`, while `AGENTS.md` is **31,330 bytes**
and is what actually loads. So the hook faithfully refreshes a file that never changes and
never touches the file that matters — which is the exact failure it exists to prevent,
walked around rather than triggered.

Until the fleet fix lands, **read a load-bearing instruction from the ref rather than from
the working tree**:

```bash
git -C <repo> fetch origin && git show origin/main:AGENTS.md | less
```

The tell that you need to is an `AGENTS.md` claim *doing work* in your plan — a limit, a
pin, a "this is impossible", a gate's stated blind spot.

⚠ **And a refreshed file reads as dirty.** The hook does not move HEAD, so a tracked file
it fixed shows as modified-vs-HEAD until the clone's branch catches up. **A tracked
context file dirty in the base clone whose content is byte-identical to `origin/main` is
the hook's doing, not somebody's unsaved work** — do not "rescue" it, and do not let it
mask the files that ARE real WIP.

## ⚠ The claim lock cannot separate two sessions on one clone

`claim-work` keys ownership on **host + worktree**, not on conversation. Measured on this
arc: two concurrent sessions working from the same base clone both received
**rc 12 — "ALREADY YOURS, carry on"** for the same ranked item, because the owner-id is
identical. Read the printed `who:`/`when:`/`where:` before treating rc 12 as proof you took
it, and treat `gh pr list --state open` as the only surface that can see an **unclaimed**
duplicate. Run that sweep twice: at orientation, and again immediately before
`gh pr create` — the window is around twenty minutes and the second moment is where the
sunk cost is highest.

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
