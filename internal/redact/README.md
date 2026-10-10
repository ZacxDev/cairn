# internal/redact

The host-side redactor for session transcripts (S1 of `claudedocs/plan-cairn-plugins.md`,
decisions 6 and 6a). One rule table, applied to DECODED strings, a keyed tag in place of every
match. stdlib only — the capture binary that imports it is under the import ban.

## The claims, and their scope

- **Decoded, not raw.** A record is decoded (`UseNumber`, key order kept, DUPLICATE members kept),
  every string value AND every object key is scanned, and the record is re-encoded only when
  something matched; an untouched record keeps its exact bytes. A string that is itself a JSON
  document is re-entered and walked the same way.
- **`[redacted:<rule>:<tag>]`**, `<tag>` = first 8 hex of HMAC-SHA256 under the per-host key
  (`LoadOrCreateKey`, 0600, never regenerated silently).
- **Binary = a known file signature** (`Signatures`: PNG, JPEG, GIF, WebP, PDF, ZIP, gzip, bzip2,
  xz, zstd, 7z, ELF) — the coordinator's reading of O12. Binary ships byte-identical; everything
  else is TEXT and is scanned: NUL-separated text segment by segment, text with stray invalid
  bytes with those bytes carried through (only matched spans change), BOM-marked UTF-16 decoded
  and re-encoded. Base64 or a `data:` URL is scanned decoded unless its payload is binary.
- **ONE predicate for "this name names a secret"** — `SecretKey` — for `KEY=value` lines, YAML
  keys, `docker -e`, k8s env `name:` entries and JSON object keys. Case- and style-insensitive
  (`dbPassword`, `SecretAccessKey`, Docker `auths.*.auth`), and the secret word must END the name
  (`max_tokens`, `TOKEN_URL`, `DB_PASSWORD_FILE` are not secrets). The structural field rule takes
  values of ≥ 4 characters, spaces allowed — the dotenv rule's floor.
- **Line rules read through copy prefixes**: Read's numbered copy (`     1\t`, `1→`), `grep -n`
  (`path:12:`) and diffs (`+`/`-`) — for the dotenv rule and the YAML Secret rule.
- **Code is not a secret.** The line rules refuse a value that is a call, an index, a `$`
  reference, an attribute path or a bare word (`Password: password,`), so source a session reads
  survives: the corpus carries synthetic Go, Python, Markdown and YAML and measures them
  undamaged. ⚠ Cost: a digit-free dictionary-word password in a `KEY=value` line is not caught.
- **A private-key match is BOUNDED** to header, header lines, base64 body and END; a file that
  merely mentions a header loses the header only.
- **Per-host denylist** (`LoadDenylist`, 0600, never in the repo). A `glob:` covers a blob by name
  and, in the record object naming the path (`filePath`/`file_path`/`path`), every string under
  `content`, `originalFile`, `base64`, `oldString`, `newString`, `old_string`, `new_string`,
  `structuredPatch`, `edits`. ⚠ **It does NOT cover Read's numbered `tool_result` copy or a
  `cat` of the file**: those are other records, joined only by `tool_use_id`, and the redactor
  sees one record at a time. Chosen over a cross-record join here; the claim is narrowed to what
  is built, and a test pins the uncovered case AS uncovered.

## How it is measured

`SelfTest` (run by `cairn-capture --self-test`) builds a synthetic corpus in both runtimes' shapes
with `DeclaredPlants` (49) secrets GENERATED AT RUN TIME from a seeded RNG, redacts it, and prints
`SUMMARY redaction: planted=P caught=P clean-damaged=0`. A plant counts as caught only when its
value is gone AND its OWN rule fired on the item that carried it (`Score` asserts `Plant.Rule`).
Before the verdict it runs two controls and exits 2 ("could not vouch") if either misbehaves — a
redactor that changes nothing must catch 0, a greedy rule must damage a clean value — or if P is
not the declaration; each branch has a test.

`TestAFreshAttackSet` runs shapes the corpus does not carry, from a seed it never uses, and logs
caught/total per class. ⚠ Same author as the rules: a regression set, not blind recall.

## Measured while building it

- **The containment relation is pinned to the TOKEN**, not to leakscan's regex no longer matching:
  redacting only the word `Bearer` breaks leakscan's match while leaving the token. Its control is
  the private-key rule — the only rule that reads leakscan's OPENSSH header control; the GitHub and
  authorization controls are each covered by two rules (GitHub token + dotenv, authorization +
  bearer), so deleting either one alone stays green.
- **A spelled JSON escape was decoded on the way into the source** in the first draft, so the
  "escaped" plant was a plain one. It is built from bytes now (`escapedHyphen`).

## What no rule here can see

Unshaped secrets (typed passwords, novel token formats), an encoded secret embedded in a longer
string, a flow-style YAML Secret, text in an encoding other than UTF-8 or BOM-marked UTF-16, and
anything inside a signature-bearing payload (a PNG text chunk included). Real recall is an
operator-side, count-only measurement (Q4, adopted).
