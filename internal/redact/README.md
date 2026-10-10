# internal/redact

The host-side redactor for session transcripts (S1 of `claudedocs/plan-cairn-plugins.md`,
decisions 6 and 6a, and operator decision O15). One rule table, applied to DECODED strings, a keyed
tag in place of every match. stdlib only — the capture binary that imports it is under the import ban.

## The approach (O15): key context plus entropy, over normalised lines

Three review rounds of per-format rules each fixed the cases they named while a fresh held-back
set stayed flat and damage to clean text grew. O15 changed the approach:

1. **Structural first — normalisation** (`normalise.go`). Before any rule matches, a tool's line
   prefixes are set aside: Read's numbered copy (`  12\t`, `12→`); a tool's prefix — grep
   (`path:12:`, `path:12:3:`, `path-12-`, `12:`, `12:3:`, `12-`, `path:`), docker compose
   (`svc-1  | `), `kubectl logs --prefix`, `git blame`; a log timestamp (ISO 8601, syslog); a diff
   (`< `, `> `, `+`, `-`). Every subset of those layers a line carries yields a VIEW; the rules that
   read a line's start (`Anchored`) run over every view, and every match is mapped back to the
   ORIGINAL byte offsets, where the redaction is applied once. The original text is always a view
   too, because a prefix reading can be wrong (`password: x` also parses as a grep `path:`). No
   rule spells a prefix any more. A prefix is recognised by its STRUCTURE, never a word list: a
   bare `path:` must contain a `/` or a `.`, because `fix:`, `TODO:` and `Q:` are the same bytes in
   prose (round 4 stripped them and redacted `password reset`).
2. **Key context** (`keyed.go`, rule `key-context`). A value attached to a name `SecretKey`
   accepts is redacted, in any notation: `K=v`, `K: v`, `K := v`, `K => v`, quoted names
   (`"K": "v"`, `['K'] = 'v'`), a call's first two arguments (`os.Setenv("K", "v")`), flags
   (`--K=v`, `--K v`, `-K v`), SQL (`PASSWORD 'v'`, `IDENTIFIED BY 'v'`), XML (`<K>v</K>`), .NET
   (`key="K" value="v"`). `Environment=K=v` and `-e K=v` need nothing special. **Not ANY value:**
   one whose shape is code, a placeholder or prose is refused (below), a bare value over 1 KiB is
   left to the entropy rule, and a WEAK name's value must also look like a secret (below). It runs
   in LINEAR time: round 4's XML join and bracket strip were quadratic (1 MB took 18 s and 8 s),
   and `password=` repeated took over two minutes for 256 KiB; `TestRoundFiveKeyContextIsLinearTime`
   and an operation count pin it.
3. **Entropy** (`entropy.go`, rule `entropy`, LAST). A run of the base64/base64url alphabet of at
   least 20 characters is redacted wherever it stands when it carries upper case, lower case AND a
   digit, is not wordy (70% of it in word-shaped letter runs, or 45% with a third of its letters
   vowels), changes
   character class at ≥ 0.35 of its positions, is not an identifier/slug/path by its `_`/`-`/`/`
   segments, not an alphabet literal, not a transcript ID (`toolu_`, `msg_`, `req_`, `ses_`, `prt_`,
   `call_`), not an integrity digest (`sha512-…`, `h1:…`), and not the base64 of a binary payload.
   A thinking block's `signature` is exempt by STRUCTURE, not by shape.

## What was retired and what was narrowed

- **Retired into `key-context`:** `dotenv`, `source-literal`, `libpq-password`, `docker-env`,
  `npmrc-auth`. Each was one notation of "a value after a secret name".
- **Narrowed:** `query-param` now reads only the parameters that carry a credential WITHOUT a
  secret-sounding name (`sig`, `signature`, `X-Amz-Signature`, `X-Goog-Signature`,
  `X-Amz-Security-Token`, `code`); a bare `key` is no longer one (`?key=getting-started` was round
  3's damage). `docker-login` became `cli-flag`, a closed per-tool list of SHORT flags that carry no
  name (`docker login -p`, `mysql -p<pw>`, `redis-cli -a`, `sshpass -p`).
- **Prefix handling removed from every rule** (the old `linePrefix`/`copyPrefix` grammar inside the
  dotenv, PEM, pgpass, netrc and YAML rules) in favour of the views.
- **Kept, because neither general rule can see them:** vendor token formats (they tag a token by its
  own name, and catch short ones), `url-userinfo-password`, `curl-user`, `pgpass` and
  `netrc-password` (positional), `pem-private-key` (a block), `authorization`/`bearer`, and the YAML
  structure (`k8s-secret`, `k8s-env`, `yaml-block-secret`) and JSON walk (`secret-field`,
  `jwk-private`).

When two rules' spans overlap they merge, and the EARLIER rule in the table names the merged span
(the YAML structure sits after the table rules, entropy after everything).

## The claims, and their scope

- **Decoded, not raw.** A record is decoded (`UseNumber`, key order kept, DUPLICATE members kept),
  every string value AND every object key is scanned, and the record is re-encoded only when
  something matched; an untouched record keeps its exact bytes. A string that is itself a JSON
  document is walked the same way.
- **`[redacted:<rule>:<tag>]`**, `<tag>` = first 8 hex of HMAC-SHA256 of the redacted span under the
  per-host key (`LoadOrCreateKey`, 0600, never regenerated silently).
- **Binary = a known file signature** (`Signatures`) — the coordinator's reading of O12. Binary
  ships byte-identical; everything else is TEXT and is scanned (NUL-separated text segment by
  segment, invalid bytes carried through, UTF-16 decoded and re-encoded). An ASCII-spellable magic
  counts only with a non-text byte in the first 1 KiB. A whole base64 value or `data:` URL is
  scanned decoded unless its payload is binary; a binary payload's encoding is also exempt from the
  entropy rule.
- **ONE predicate for "this name names a secret"** — `SecretKey` — case- and style-insensitive; the
  secret word ends the name up to a closed suffix set (STRONG); a long word may be GLUED
  (`PGPASSWORD`); `<VENDOR>_KEY` is a closed list (now including `CLIENT`, `TLS`, `SSL`, `SSH`); the
  abbreviations `creds`, `cred`, `privkey`, `pwd`, `passwd`, `pass` end a name too. Upper-case
  `PWD`, `OLDPWD` and `PASS` alone are not secrets (`--- PASS:` is a test verdict). Since round 5 a
  long secret word that is a whole SEGMENT of the name (`DB_PASSWORD_PROD`, `apiTokenStaging`) makes
  it WEAK, unless a later segment is on the closed ATTRIBUTE list (`_URL`, `_FILE`, `_HASH`, `_TTL`,
  …); a weak name's value must also not be a word, a word slug, a number/duration/version, a URL, an
  expression opening or a sentence. ⚠ **The cost, measured** (`TestRoundFiveSegmentNamesCostOnCleanProbes`):
  0 of 27 attribute-shaped clean probes damaged, and one pinned cost —
  `SECRET_BACKUP_BUCKET=s3-backups-01` IS redacted, because `BUCKET` is not on the list and the
  value is not a word slug. Over this repository's own tracked text the round adds no damaged line
  that round 4 left alone (diffed file by file against round 4's head).
- **Code and prose are not secrets — refused by SHAPE** (`notCode`, `keyedValueOK`, and the
  join-aware refusals in `bareValue`): calls/indexes/literals with a code-shaped head, shell
  expansions (`${…}`, `$(…)`, a whole `$VAR` or `$VAR/…`), format templates (only verbs, escapes and
  punctuation, or two of them glued at the start), placeholders (`<…>`, `***`, `your…`), YAML
  aliases of a lower-case word (`*db_password`), paths, package-qualified names, names that
  themselves STRONGLY name a secret (`CAIRN_TOKEN` is an env var's NAME), sentences (stop words, a
  secret word, end punctuation), and code-only joins. 🔴 **Code notation is decided by the JOIN**
  (a Go `:=`, a Ruby `=>`, a spaced `=`, a value ending in code punctuation, a name inside a string
  literal): only there are a dereference `*p`/`&v`, a leading printf verb (`%T-…`) and an escape
  read as code. In a quoted string, a dotenv/shell value or a YAML value they are a password's
  first character — round 4 refused them everywhere and caught `*…` 20–40/200, `&…` 31–42/200 and
  `%verb…` 0/200; now 200/200 each (`TestRoundFiveValuesStartingWithASymbol`). ⚠ **The cost,
  measured** (`TestRedactorRecallOnRealisticPasswords` at seed 4, `TestTheRateRangeHoldsOverSeedsFourToEight`
  over seeds 4–8; 200 values per cell; oracle: no 6-character window of the value survives):
  200/200 for alnum, base64, hex, dotted and dashed diceware at every one of those seeds;
  symbol-bearing passwords (`pm-20`, `pm-16-3symbols`, `pm-16-lead-symbol`) **193–200/200** over
  seeds 4–8, the libpq string included (round 4's code: 192–200 over the same seeds). At seed 4
  alone: 195–199.
  *Correction: rounds 4 and earlier stated "195–199/200 in every line shape" — true at seed 4 only;
  one seed is one measurement.*
  *Correction: through round 3 this README and the plan said 176–197/200 for the libpq string. That
  figure came from a weaker oracle than the test's own doc stated (whole value or its first half,
  not any 6-character window); under the stated oracle round 3's code measured **139/200** for
  `pm-20` there. The test now implements the oracle its doc names.*
- **A private-key match is BOUNDED** to header, header lines, base64 body and END.
- **Per-host denylist** (`LoadDenylist`, 0600, never in the repo). A `glob:` covers a blob by name
  and, in the record object naming the path, every string under the structured-copy keys. ⚠ **It
  does NOT cover Read's numbered `tool_result` copy or a `cat` of the file** (a cross-record join
  this redactor does not do); a test pins the uncovered case AS uncovered.

## How it is measured

- **`SelfTest`** (run by `cairn-capture --self-test`) builds a synthetic corpus in both runtimes'
  shapes with `DeclaredPlants` (79) secrets generated at run time, redacts it, and prints
  `SUMMARY redaction: planted=P caught=P clean-damaged=0` — measured at every seed 1–400, and
  pinned at seeds 1–40 by `TestRoundFiveSelfTestHoldsAcrossSeeds`. *Round 4 scored 78/79 on about
  one seed in six; the cause was the SCORER (a plant's `&` is JSON-escaped in its record, so the
  "carried" check never found it and its rule was never credited), plus two rare rule misses (a
  `/`-split base64 run, a random dotted head before `(`) — all three fixed.* A plant counts as caught only when its
  value is gone AND its OWN rule fired on the item that carried it. Two controls run first and the
  run exits 2 if either misbehaves (an identity redactor must catch 0; a greedy rule must damage a
  clean value), or if P is not the declaration.
- **`TestTheEntropyRuleThresholds`** pins the entropy rule's recall on random tokens in prose (seed
  15, 500 each: alnum 20/24/32/40 chars 478/479/496/499; base64 of 18/32/64 bytes 482/497/498;
  base64url 32 bytes 499 — round 5 moved the base64 rows up, by counting identifier segments by
  character) and the clean shapes it must leave alone, with a positive control.
- **The round-5 tests** (`round5_test.go`): each of review round 4's findings, shown RED at round
  4's head unless labelled an invariant guard or a cost pin; `round5_internal_test.go` holds the
  guards on this round's own internals (an operation count for linear time, the name prefilter
  against `SecretKey`). Each new guard was mutation-tested against its own test; ⚠ ONE mutant
  survives and is stated where it lives — restoring the per-character bracket recount, which the
  1 KiB bare-value cap bounds whatever the strip does.
- **The round-4 tests** (`round4_test.go`): every tool prefix round 3 named, before a rule only a
  line-anchored match can satisfy; PEM bodies behind each prefix; the named-key notations; round 3's
  clean probes — each measured RED on the pre-O15 code. Plus the clean lines an intermediate round-4
  build damaged (labelled as this round's own guards, not round-3 coverage).
- Review round 2's adopted auditor tests (`audit_*_test.go`) still run.

## The arming gate (O15)

Capture is not armed on ANY instance until a FRESH held-back case set — written by an auditor,
never seen by the fixer, replaced each round — shows **≥ 90% of leaks caught AND ≤ 15% of clean
lines damaged**. `heldback.go` makes that a deterministic check:

```bash
go build -o redact-heldback ./internal/redact/cmd/redact-heldback
./redact-heldback cases.jsonl      # prints `leaks caught=X/Y clean damaged=A/B`; exit 0 / 1 / 2
```

The case format, the window oracle and the controls (an identity redactor must score nothing; an
eraser must score everything; fewer than 20 leaks or 20 clean lines refuses) are in the file's doc.
⚠ `go run` reports every non-zero exit as 1 — build it. ⚠ A pass certifies the CASE FILE it was
given; that the file is fresh and held back is a fact about who wrote it, recorded beside the run.

## What no rule here can see

A secret that is neither NAMED nor RANDOM-LOOKING: a typed password in prose, an all-lower-case or
hex token with no key in front of it, an unnamed token under 20 characters. **An unnamed
password-manager password with symbols is in that set**: the symbols split it into base64-alphabet
runs under 20 characters, so the entropy rule sees none of it — 0–1/200 caught at 16, 20, 24 and
32 characters (seed 9, alphanumerics plus 28 symbols; round 4's audit measured about 13/200 with its
own alphabet). A symbol-token rule was prototyped (175/200 at 24 characters) and NOT adopted: over
this repository's own tracked text it damaged 275 tokens — regex literals, SRI digests, transcript
IDs, URL-encoded paths. Also a flow-style YAML
Secret, text in an encoding other than UTF-8 or UTF-16, and anything inside a signature-bearing
payload (a PNG text chunk included). Real recall is an operator-side, count-only measurement (Q4).
