# Vendored verbatim — do not edit

These three files are copies of the hub's own crawler scripts, taken from its public
repository at commit `cd772e8fae3ceda74e35aaee706e05c956b187ce`
(`internal/crawler/`). They are byte-identical copies, and **editing one is a wire-contract
change, not a local tweak.**

| file | bytes | sha256 (first 16) | what breaks if you edit it |
|---|---|---|---|
| `axe.min.js` | 572599 | `66a8aaa95a8b044a` | the rule set a pushed run is diffed against stops being the rule set it was measured with |
| `a11y-digest.js` | 7964 | `ce0426e5d43485bd` | a **partial** digest silently DELETES its owner's findings; an **empty** one is a 400 on the whole push |
| `layout-smells.js` | 3215 | `3baa48e589717b53` | the raw keys stop matching `PushLayout`, and the hub derives its layout findings from them |

## Why verbatim, per file

- **`a11y-digest.js`** produces the digest the hub validates and then uses to **drop**
  findings. An element the producer fails to list is an element the gate reads as "not
  present" — a positive refutation the producer never intended. The hub's own producer
  contract says the easiest correct producer is this file, ported verbatim, plus an
  emptiness check before attaching the ref. `payload.go` does the emptiness check;
  `Capture.HasDigest` is where.
- **`layout-smells.js`** returns exactly `plugin.PushLayout`'s snake_case keys. The hub
  turns those raw counts into `type=layout` findings server-side through
  `internal/signals`, which is its declared single source of truth for the thresholds
  (44px taps, 12px text, the mobile-only gating). A harness that computed a finding here
  would be a second copy of a threshold — **and an invisible one**, because the server drops
  a pushed layout finding whenever the raw block is present.
- **`axe.min.js`** is pinned so that `new_a11y_rules` compares like with like. A different
  axe build reports a different rule set, and every rule that moved would flag "new" once.

## Refreshing them

Re-copy all three from the same upstream commit and record the new commit and digests in
the table above. Do not refresh one alone: the digest script and the layout script are two
halves of one push contract, and axe's rule set moving under a stale baseline produces a
diff that reads as a regression.

The upstream URL is deliberately not written here — see `push.go` on why no the hub
hostname appears anywhere in this repository.
