# internal/redact

The host-side redactor for session transcripts (S1 of `claudedocs/plan-cairn-plugins.md`,
decisions 6 and 6a). One rule table, applied to DECODED strings, a keyed tag in place of every
match. stdlib only — the capture binary that imports it is under the import ban.

## The claims, and their scope

- **Decoded, not raw.** A record is decoded (`UseNumber`, key order kept), every string is scanned,
  and the record is re-encoded only when something matched; an untouched record keeps its exact
  bytes. A string that is itself a JSON document is re-entered and walked the same way.
- **`[redacted:<rule>:<tag>]`**, `<tag>` = first 8 hex of HMAC-SHA256 under the per-host key
  (`LoadOrCreateKey`, 0600, never regenerated silently).
- **Binary ships byte-identical (O12).** Text = no NUL in the first 8,000 bytes AND valid UTF-8.
  Base64 or a `data:` URL whose payload decodes to TEXT is scanned decoded; anything else is left
  as it is. UTF-16/Latin-1 text is "binary" by that rule and ships unredacted — a stated residual.
- **Structural rules**: a YAML `kind: Secret` document's `data`/`stringData` values (line-based,
  block style only — flow style is a residual), the same for a JSON Secret object, and an object
  key naming a secret (`password`, `client_secret`, `access_token`, …).
- **Per-host denylist** (`LoadDenylist`, 0600, never in the repo): `literal:` and `glob:` lines.

## How it is measured

`SelfTest` (run by `cairn-capture --self-test`) builds a synthetic corpus in both runtimes' shapes
with `DeclaredPlants` secrets GENERATED AT RUN TIME from a seeded RNG, redacts it, and prints
`SUMMARY redaction: planted=P caught=P clean-damaged=0`. Before the verdict it runs two controls
and exits 2 ("could not vouch") if either misbehaves: a redactor that changes nothing must catch
0, and a greedy rule must damage a clean value. P is asserted equal to the declaration.

The tests in `redact_test.go` add the plan's per-rule controls (dotenv removed → the miss is
named; floor raised to 64 → the 56-character encoding of a 40-character token is missed; a
StdEncoding-only decoder → one of four encodings missed; a raw-byte scan → the escaped secret is
missed; an extension sniff → a NUL-carrying `.txt` would be redacted), the residual pinned AS a
residual (an encoding embedded after other text is NOT decoded), and the containment relation
with `tests/leakscan.py`.

## Measured while building it

- **The containment relation is pinned to the TOKEN, not to leakscan's regex no longer matching.**
  Redacting only the word `Bearer` breaks leakscan's match while leaving the token — the dotenv
  rule alone does exactly that to the bearer control — so the test asserts the credential run
  inside leakscan's own match is gone.
- **Two rules cover leakscan's GitHub control** (the GitHub-token rule and the dotenv rule, via
  `GITHUB_TOKEN=`), so deleting the GitHub rule does NOT turn the containment test red; the
  corpus's bare base64-encoded token is what that rule alone catches. The authorization rule is the
  one whose loss the containment test sees.
- **A spelled JSON escape was decoded on the way into the source** in the first draft, so the
  "escaped" plant was a plain one. It is built from bytes now (`escapedHyphen`), and the raw-scan
  control is what caught the defect.

## What no rule here can see

Unshaped secrets (typed passwords, novel token formats), an encoded secret embedded in a longer
string, a flow-style YAML Secret, and anything inside binary content. Real recall is an
operator-side, count-only measurement (Q4, adopted).
