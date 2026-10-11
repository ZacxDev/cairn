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
   prose (round 4 stripped them and redacted `password reset`). grep's context form `path-N-` is its
   own layer (round 6): in front of a line that itself starts `host:5432:` the `path:N:` reading
   swallows both, and only a separate layer gives the other reading a view. A rule may ASK what a
   view set aside (`view.prefix`, `onlyPrefix`) — the `.netrc` rule and key context's INI test do —
   without spelling a prefix.
2. **Key context** (`keyed.go`, rule `key-context`). A value attached to a name `SecretKey`
   accepts is redacted, in any notation: `K=v`, `K: v`, `K := v`, `K => v`, quoted names
   (`"K": "v"`, `['K'] = 'v'`), a call's first two arguments (`os.Setenv("K", "v")`), flags
   (`--K=v`, `--K v`, `-K v`), SQL (`PASSWORD 'v'`, `IDENTIFIED BY 'v'`), XML (`<K>v</K>`), .NET
   (`key="K" value="v"`). `Environment=K=v` and `-e K=v` need nothing special. **Not ANY value:**
   one whose shape is code, a placeholder or prose is refused (below), and a WEAK name's value must
   also look like a credential (below). **A bare value of ANY length is taken** (round 6): one over
   1 KiB is judged by its first 64 bytes and taken whole. *Round 5 refused every bare value over
   1 KiB "to the entropy rule", which reads neither hex nor a URL-encoded document:
   `MASTER_KEY=<2048 hex>` and `AUTH_TOKEN=<1317 URL-encoded characters>` shipped 93–100% intact.
   Retracted: the bound is now on the WORK per name, not on the value*
   (`TestRoundSixLongNamedValuesAreRedacted`: hex, URL-encoded JSON and lower-case/digit values at
   1024, 1025, 1317, 2048 and 65,536 bytes in six notations, 90/90; 72 of those 90 shipped at round
   5). It runs in LINEAR time: round 4's XML join and bracket strip were quadratic (1 MB took 18 s
   and 8 s), and `password=` repeated took over two minutes for 256 KiB;
   `TestRoundFiveKeyContextIsLinearTime` and two operation counts pin it
   (`TestKeyContextIsLinearTime`, `TestRoundSixLongValuesKeepTheFinderLinear`), and a name inside a
   value already taken is not read at all.
3. **Entropy** (`entropy.go`, rule `entropy`, LAST). A run of the base64/base64url alphabet of at
   least 20 characters is redacted wherever it stands when it carries upper case, lower case AND a
   digit, is not wordy (70% of it in word-shaped letter runs, or 45% with a third of its letters
   vowels), changes
   character class at ≥ 0.35 of its positions, is not an identifier/slug/path by its `_`/`-`/`/`
   segments (its word segments hold two thirds of its characters, or are two thirds of its segments
   AND hold 35% of its characters — round 6, below) **unless its first or last segment is 16+
   characters and passes the token test ON ITS OWN** (round 7, `randomEdgeSegment`: words must not
   vouch for a random tail — `prod-billing-service-api-token-<24 random>` shipped at round 6), not
   an alphabet literal, not a transcript ID
   (`toolu_`, `msg_`, `req_`, `ses_`, `prt_`,
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
  `netrc-password` (positional — and since round 6 each read only with the STRUCTURE of its file,
  `positional.go`, below), `pem-private-key` (a block), `authorization`/`bearer`, and the YAML
  structure (`k8s-secret`, `k8s-env`, `yaml-block-secret`) and JSON walk (`secret-field`,
  `jwk-private`).

When two rules' spans overlap they merge, and the EARLIER rule in the table names the merged span
(the YAML structure sits after the table rules, entropy after everything).

## The claims, and their scope

- **Decoded, not raw.** A record is decoded (`UseNumber`, key order kept, DUPLICATE members kept),
  every string value AND every object key is scanned, and the record is re-encoded only when
  something matched; an untouched record keeps its exact bytes. A string that is itself a JSON
  document, and a JSON blob, are walked the same way and **redacted IN PLACE** (round 6,
  `spliceJSON`): only the string tokens that changed are rewritten, so the document's indentation,
  spacing, number text and the escapes of its other strings are the original's. *Before round 6 one
  hit re-serialised the whole document — 1,186 changed lines for the hits in this repository's
  one large JSON fixture, and 5,008 for the Go standard library's `vectors.json`.* ⚠ A RECORD
  (`Redactor.Record`, one JSONL line) with a hit is still re-encoded compact: one line either way.
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
  …).
- **A WEAK name takes only a CREDENTIAL-SHAPED value** (round 6, `credentialShaped`; `cred`/`creds`
  are held to it too). Not one: text with a space, a URL, an expression opening; letters only (a
  word, a mode, `TokenClient`); an identifier or slug (two or more pieces joined by single
  `_`/`-`/`.`/`:`/`/`, each letters with at most three trailing digits, or a short number —
  `foo_bar9`, `s3-backups-01`); a number, duration, version or hex constant; a timestamp, an e-mail
  address, a domain name; a regex literal, a generic or pointer type. *Round 5 refused only words,
  slugs, numbers and URLs there, and 19 of its audit's 53 clean probes were damaged (0 at round
  4).* ⚠ **Both directions, measured:** clean — 0 of 33 weak-name probes damaged (21 at round 5),
  with two pinned costs that ARE damaged (`TOKEN_SIGNING_KID=kid-2f9a01c3`,
  `password_salt_hex: 9f2c4e1a7b3d5f6e`: a weak name over a value that does look random); recall —
  generated values under 8 weak names × 3 notations, 200 each: lower-case/digit, alnum-12,
  alnum-32, hex-32 and `Word20NN` 200/200, symbol-bearing 199–200 (the miss is `x&&y`, refused as
  an expression under every name). 🔴 **The stated cost:** a weak name's value that is itself
  identifier- or slug-shaped ships — `DB_PASSWORD_PROD=tiger_2024`,
  `API_TOKEN_CI=correct-horse-battery`: 0/200 (`TestRoundSixWeakNameRecall` pins the zero; round 5
  caught 75/200 of these, all under `cred`/`creds`, which it treated as strong). A STRONG name is
  held to none of this.
- **A STRONG name takes a digits-only value** of four or more digits outside code notation
  (`DB_PASSWORD=482915`, `password: 123456`, `"password": "482915"`; round 6 — it shipped at
  rounds 4 and 5), **and a quoted value under an ALL_CAPS environment-style weak name is a
  literal** (`DB_PASSWORD_PROD="dragon"`). Not in code notation (`token = 12345`, `Token: 4096,`),
  and a signed, decimal or short hex constant is a number under every name (`SCM_CREDS = 0x03`,
  `…_TOKEN = 0x000D`). ⚠ Cost: `API_KEY=0xdeadbeef` — a hex constant of at most 8 digits — ships.
- **Code and prose are not secrets — refused by SHAPE** (`notCode`, `keyedValueOK`, and the
  join-aware refusals in `bareValue`): calls/indexes/literals with a code-shaped head, shell
  expansions (`${…}`, `$(…)`, a whole `$VAR` or `$VAR/…`), format templates (only verbs, escapes and
  punctuation, or two of them glued at the start), placeholders (`<…>`, `***`, `your…`), YAML
  aliases of a lower-case word (`*db_password`), paths, package-qualified names, names that
  themselves STRONGLY name a secret (`CAIRN_TOKEN` is an env var's NAME), sentences (stop words, a
  secret word, end punctuation), and code-only joins. 🔴 **What reads as code is decided by
  EVIDENCE ABOUT THE LINE** (round 6; round 5 used the join's spacing and trailing punctuation
  alone). A dereference `*p`/`&v` is read only where the join is code notation (a Go `:=`, a Ruby
  `=>`, a spaced `=`, a value ending in code punctuation, a name inside a string literal) AND the
  line is not an ALL_CAPS name glued to `=` (an env assignment, whatever follows the value) AND it
  is not the INI layout (the name starts its line — behind tool prefixes only — a spaced `=`, the
  value ends the line) AND the operand reads as an identifier rather than a random string. A
  leading printf verb or an escape is a template only INSIDE A STRING LITERAL. Round 4 refused
  these everywhere (`*…` 20–40/200, `&…` 31–42/200, `%verb…` 0/200); round 5 fixed the quoted and
  glued forms (`TestRoundFiveValuesStartingWithASymbol`, 200/200) and left `password =
  *Zq9xK2mL7pQw` and `DB_PASSWORD=*…;` shipping (28–43/200, and 0/200 for `%verb`); round 6:
  200/200 in seven of eight config layouts for five leading shapes, and 194–200 in the eighth —
  an indented `password: v,` is a struct literal as often as YAML, so a dereference is still read
  there unless its operand looks random (`TestRoundSixSymbolLeadingValuesInConfigLayouts`). ⚠ **The cost,
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
- **`.pgpass` needs pgpass structure** (round 6, `positional.go`): a host-like first field (no
  `/`, or an absolute socket directory), a port (`*` or 1–5 digits), a database and a user that
  start with a letter or `_`. *Round 5 read `src/pkg/file.go:100:7://go:noescape` and
  `a.go:12:3://nolint:errcheck` as rows in their unstripped form.* Measured over this repository's
  neutral lines behind `path:N:C:` and `path:N:`: 6 of 22,887 at round 5, 0 at round 6; pinned
  since round 7 over the frozen corpus's 8,407 colon-bearing lines, 0
  (`TestRoundSixPgpassOverPrefixedNeutralLines` — it read the live tree before, the same
  brittleness as the budget's). ⚠ Residual, pinned: `grep -n` over a
  line that is itself `word:word:word` (`a.go:12:foo:bar:bazqux7`) still reads as a row; a row
  whose database or user starts with a digit is not read.
- **`.netrc`: a plain-word password needs netrc STRUCTURE or FILE CONTEXT** (round 6): the same
  line (a record with `machine`, or `default` with `login`), the BLOCK (a `machine …`/`default`
  record line within 12 lines either side), the FILE (the prefix the view set aside, or one of the
  12 lines above, names a netrc), or the record embedded in a command. A value that is not a word
  is taken in any view unless it is a type (`*uint16`, `[]byte`). And the lone line — exactly
  `password <word>`, keyword lower-case, no prefix or a line NUMBER only — is taken unless the word
  is an attribute word (`reset`, `rotation`, `policy`, `managers`). *Round 4 took every `password
  <word>` a stripped prefix exposed; round 5 required structure within three lines above and lost
  the lone line, a far `machine` line and a `password` written first.* ⚠ **Both directions,
  measured:** 340/340 plain-word passwords over 17 layouts (80/340 at round 5); 0 of 35 prose and
  declaration lines damaged (7 at round 5). 🔴 **Stated residuals, pinned**
  (`TestRoundSixNetrcStatedResiduals`): a lone password that IS an attribute word, sits behind a
  quote/diff/compose/path prefix with no netrc context, or follows a capitalised `Password`, ships;
  a clean line that is exactly `password <non-attribute word>`, bare or behind a line number, IS
  redacted; a block of prose holding both a line `machine <word>` and a line `password <word>`
  reads as a record.
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
  run exits 2 if any misbehaves (an identity redactor must catch 0; since round 6 two redactors
  that redact nothing and only CORRUPT THE ENCODING must catch 0 too; a greedy rule must damage a
  clean value), or if P is not the declaration. *Round 5's scorer looked for a plant in the raw
  output and in its decoded strings; output that did not decode kept a plant holding `&` or a quote
  in escaped form — and a UTF-16 blob with a byte appended — where neither search looked, and
  credited them: 256 credits over 60 seeds and two corruptions — every UTF-16 plant, and the 16
  `symbol-password` credits the audit counted as 8 with one corruption
  (`TestRoundSixACorruptingRedactorScoresNothing`; 0 now).*
- **`TestTheEntropyRuleThresholds`** pins the entropy rule's recall on random tokens in prose (seed
  15, 500 each: alnum 20/24/32/40 chars 478/479/496/499; base64 of 18/32/64 bytes 482/497/498;
  base64url 32 bytes 499 — round 5 moved the base64 rows up, by counting identifier segments by
  character) and the clean shapes it must leave alone, with a positive control. 🔴 **Round 6 kept
  every one of those numbers and gave the damage back:** counting characters alone made the rule
  redact real identifiers (`ClientCert-RSA-AES256-GCM-SHA384`, `_cgo_be59f0f25121_Cfunc_puts`,
  `GO_NID_X9_62_prime256v1`, `BSD-Systemics-W3Works`) — over the Go standard library's source,
  1,068 entropy lines that round 4 had left alone, against 207 it fixed. An identifier is now one
  by EITHER count, with a character floor on the segment count
  (`TestRoundSixIdentifierSegments`, `TestIdentifierSegmentsBothCounts`): lines of that corpus
  the entropy rule changes — 4,826 (round 4) → 5,687 (round 5) → 4,617 (round 6; 8 that round 4
  left alone, 217 fewer that it took). 🔴 **Round 7 closed the hole that count opened:** a run of
  word segments shielded a random END segment, so `prod-billing-service-api-token-<24 random>`,
  `Correct-Horse-Battery-Staple-<20 random>` and `svc_xxx_deploy_key_<26 random>` shipped (caught at
  round 5's head). An end segment of 16+ characters is now judged as a token of its own
  (`TestRoundSevenWordsDoNotShieldARandomEdgeSegment`: of 480 generated tokens, 16–32-character
  tails leading or trailing, 434 kept their tail at round 6's head, 110 at round 5's, **10** now —
  the 10 are tails that fail the token test standing alone, the rule's documented per-token cost).
  Measured zero change on the frozen corpus below, and every round-6 identifier above is still
  left alone. 🔴 **But it has a COST the frozen corpus cannot see** (round-7 audit, an opt-in sweep
  of the WHOLE go1.25.14 standard library): damaged lines 4,790 → 4,902, all **+112** from the
  entropy rule, over 63 distinct ordinary identifiers whose 16–19-character camelCase or acronym
  END segment now reads as random — mostly compiler SSA rewrite names (`rewriteValueARM64_Op…`),
  plus a benchmark name, two JSON test names and mangled C++ symbols. This repository's own tree
  is unchanged (19 neutral lines). Stated, not fixed: O16 ended the heuristic rounds, and raising
  the edge minimum for camelCase segments would reopen the round-7 test.
- **The clean-damage BUDGET** (`budget_test.go`, round 6; rescoped round 7) — damage measured on
  text nobody wrote for the purpose, and pinned so a later round that widens it fails a test
  rather than an audit. 🔴 **What `go test` pins is a FROZEN corpus** (round 7):
  `testdata/budget_corpus_repo.txt` (a sample of this repository's neutral text and code — no
  `claudedocs/`, no tests or fixtures) and `testdata/budget_corpus_go.txt` (go1.25.14 standard
  library files under the Go BSD license, which the file reproduces: a hash sample plus the files
  densest in tokens the entropy rule considers), 197 sections, 41,477 lines, of which the redactor
  changes 631 (587 by entropy — the library's test keys and vectors). Every one is ENUMERATED in
  `budget_data_test.go` by the line AND its redacted form, and the match is EXACT both ways: a new
  damaged line fails (naming it), and so does an entry nothing matches any more. It reads nothing
  else, so its answer does not depend on the tree or the toolchain. *Round 6 pinned the LIVE tree
  and GOROOT instead, and that gate was permanently one unrelated edit from red: a doc gaining a
  changed line failed it, deleting three retired docs failed it ("6 budget entries are unused"),
  and go1.26 failed it (entropy 1,063 against a ceiling of 845) with the redactor unchanged.* The
  hash sample alone was measured too thin — lowering the entropy bit floor to 3.0 changed no line
  of it — hence the near-miss files; and a key over the input line alone missed a mutant that
  added a span to an already-damaged line, hence the redacted form in the key. Mutants that widen
  the entropy rule (identifier test off, class-change floor 0.25, bit floor 3.0, minimum length 16)
  each fail it. ⚠ It cannot see text unlike its own lines — prose above all.
  **The LIVE sweeps are OPT-IN** (`-redact.live-budget`; skipped by default, so they never fail
  `go test ./...`): this repository's tracked text, enumerated in `budget_live_data_test.go`, and
  the toolchain's standard library (a 1-in-8 sample; the whole tree with `-redact.stdlib-full`),
  a ceiling per rule exact at go1.25.14. Whether CI should run them is open (plan T1). Measured
  with them at round 6, whole standard library (3,027,865 lines): 10,285 damaged lines at round 4,
  11,182 at round 5, 4,790 at round 6 (key-context 117 → 156 → 54 lines, `.netrc` 9 → 12 → 8);
  this repository (300,774 lines): 1,422 → 1,419 → 224, of which neutral files 31 → 31 → 19.
  ⚠ "Damaged" counts every changed line; it does not say the line was clean.
- **The round-6 tests** (`round6_test.go`): each of review round 5's findings, shown RED at round
  5's head unless labelled an invariant guard or a cost pin; `round6_internal_test.go` holds the
  guards on this round's internals. Each new guard was mutation-tested by NAME against its own
  test's own message: 61 mutants, 61 killed, with a no-op mutant reported SURVIVED as the
  harness's own control (the battery is in the round-6 commit message). Round 5's one stated
  survivor — restoring the per-character bracket recount — still survives, for the reason given
  where it lives.
- **The round-5 tests** (`round5_test.go`): each of review round 4's findings, shown RED at round
  4's head unless labelled an invariant guard or a cost pin; `round5_internal_test.go` holds the
  guards on this round's own internals (an operation count for linear time, the name prefilter
  against `SecretKey`). Each new guard was mutation-tested against its own test; ⚠ ONE mutant
  survives and is stated where it lives — restoring the per-character bracket recount, which the
  1 KiB bound on a value judged WHOLE bounds whatever the strip does (a longer value is not
  stripped at all since round 6).
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
Since round 6 the oracle masks redaction MARKERS out of the output before counting windows: a
fully redacted fine-grained GitHub PAT was scored MISSED because `[redacted:github-token:…]` spells
`github`, the first window of `github_pat_…` (`TestRoundSixHeldBackOracleIgnoresMarkers`, a
known-answer test with controls for what a marker must not excuse).
⚠ `go run` reports every non-zero exit as 1 — build it. ⚠ A pass certifies the CASE FILE it was
given; that the file is fresh and held back is a fact about who wrote it, recorded beside the run.

### 🔴 The gate FAILS, and capture is UNARMED everywhere (operator decision O16)

A fresh auditor-written held-back set (190 leak lines, 175 clean) scored **`leaks caught=157/190
clean damaged=18/175`** at round 6's head (`13219d8`): 82.6% caught, under the 90% floor (damage,
10.3%, is inside its 15% ceiling). Rounds 4–6 moved that set only 154 → 155 → 157. The operator
stopped heuristic fix rounds after round 7: this package merges with capture UNARMED on every
instance, O15's gate stays the arming condition, and arming is a separate future decision —
most likely about capturing LESS (excluding or truncating raw tool output) rather than redacting
better. Round 7 was not re-scored against that set (the fixer has not seen it).

**The measured residuals — what that set's misses and damage were, by category:**

| leaks MISSED (33 of 190) | missed / in category |
|---|---|
| a secret in prose | 11 / 15 |
| a PIN | 9 / 10 |
| a positional argument in code | 6 / 12 |
| a CLI flag no rule reads | 2 |
| a hex secret | 2 |
| a long value | 1 |
| a value under a strong name | 1 |
| a symbol-led value | 1 |

| clean lines DAMAGED (18 of 175) | damaged / in category |
|---|---|
| base64 of non-secret data | 9 / 10 |
| a public key | 8 / 10 |
| a type annotation | 1 |

**Round 6's other findings, NOT fixed (O16) and stated here as residuals:**

1. The `.netrc` context rule damages prose: `See netrc(5).` then a line `password field`; a grep
   path containing `netrc` before `password storage`; `machine translation` then
   `password storage` (the last two, and the first in that two-line form, reproduced here).
2. A digits-only password in INI, `.properties`, TOML or Makefile layout ships:
   `password = 482915`, `db.password = 482915`, `DB_PASSWORD ?= 482915` (reproduced).
3. A Makefile `:=`, an indented INI line and an INI line with a trailing `; comment` fall
   through config detection (`DB_PASSWORD := <12 alphanumerics>` ships, reproduced; the
   symbol-led forms probed here were caught, so the audit's shapes are the ones to read).
4. Minified JavaScript after `password:` is damaged (as reported; two probes here did not
   reproduce it).

## What no rule here can see

A secret that is neither NAMED nor RANDOM-LOOKING: a typed password in prose, an all-lower-case or
hex token with no key in front of it, an unnamed token under 20 characters. **An unnamed
password-manager password with symbols is in that set**: the symbols split it into base64-alphabet
runs under 20 characters, so the entropy rule sees none of it — 0–1/200 caught at 16, 20, 24 and
32 characters (seed 9, alphanumerics plus 28 symbols; round 4's audit measured about 13/200 with its
own alphabet). A symbol-token rule was prototyped (175/200 at 24 characters) and NOT adopted: over
this repository's own tracked text it damaged 275 tokens — regex literals, SRI digests, transcript
IDs, URL-encoded paths. **And the named values the key-context rule refuses by design, each
measured where it is pinned:** a value whose shape is code, a placeholder or prose; a WEAK name's
identifier- or slug-shaped value (`DB_PASSWORD_PROD=tiger_2024`, 0/200); a hex constant of at
most 8 digits or a signed/decimal number under any name; a digits-only value in code notation or
under four digits; a generated password that is `identifier&&identifier` (1–2 in 200); a
letters-only value after a spaced `=` (read as a variable); a bracket-led value the line does not
close after a spaced `=`; the `.netrc` and `.pgpass` residuals above. Also a flow-style YAML
Secret, text in an encoding other than UTF-8 or UTF-16, and anything inside a signature-bearing
payload (a PNG text chunk included). Real recall is an operator-side, count-only measurement (Q4).
