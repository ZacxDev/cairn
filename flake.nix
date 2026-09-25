{
  description = "cairn — a hosted store for per-subsystem engineering notes: a scoped, token-authorised API pod and the client that syncs and reads it";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      # Darwin is listed because the CLIENT is stdlib Python and has no reason
      # not to build there. The IMAGE is Linux-only, and is exposed only on
      # Linux rather than failing at evaluation with a `dockerTools` error.
      # 🔴 `x86_64-darwin` IS DELIBERATELY ABSENT, AND ITS ABSENCE IS LOAD-BEARING.
      # nixpkgs 26.11 — which `flake.lock` pins — DROPPED that platform, so
      # listing it made `nix flake show` and `nix flake check --all-systems`
      # exit 1 for the whole flake, not just for that attribute. The canonical
      # "is this flake healthy" commands were therefore red from the first
      # commit, and the CI `nix` job could not see it because that job builds
      # three explicit `x86_64-linux` attributes and never evaluates the rest.
      # Re-add it only against a nixpkgs that still has it.
      systems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ];
      linuxSystems = [ "x86_64-linux" "aarch64-linux" ];

      forSystems = list: f: nixpkgs.lib.genAttrs list (system: f nixpkgs.legacyPackages.${system});
      forAll = forSystems systems;

      # 🔴 THE VERSION IS DERIVED, NEVER A LITERAL. A hand-maintained version
      # string is a claim nobody re-checks: it keeps its old value across the
      # commit that should have moved it, so an artefact ships mislabelled while
      # looking right, and every later reading of "which code is this" is wrong.
      # A git revision cannot disagree with the tree it was built from.
      # `dirtyShortRev` appears when the build came from a modified working
      # tree, which is itself the fact a reader of the tag most wants.
      version = self.shortRev or self.dirtyShortRev or "unknown";

      # 🔴 THE RUNTIME CONTRACT, IN ONE PLACE, BECAUSE THERE ARE NOW TWO BUILDS.
      # ⚠ This read "`server/Dockerfile` is what is deployed today" until the Go
      # cutover. NEITHER Python image is deployed now — the cluster pulls
      # `cairn-store-go`. The pin below still earns its place, because both
      # builds state the contract and can still disagree.
      # The image below is a
      # second way to produce the same pod, and two build paths that can
      # disagree about env/port/user is the whole hazard of adding one. These
      # values are the single nix-side authority for it, and
      # `tests/test_flake_image_matches_dockerfile.py` pins them against the
      # Dockerfile so neither can move alone.
      #
      # HOME is not decoration: `lib/subsystem_read_store.py` evaluates
      # `Path.home() / ".cache" / …` at IMPORT time, and `Path.home()` raises
      # when it can resolve neither $HOME nor a passwd entry — the ordinary
      # state of a numeric-UID container. The value is never used by the server
      # (every call passes `--store`), but the import would fail before
      # anything could say so.
      #
      # 🔴 NEITHER IMAGE SETS A STORE VARIABLE IN EITHER SPELLING, AND THAT IS A DECISION
      # RATHER THAN AN OMISSION. `SUBSYSTEM_STORE_ROOT=/data`,
      # `SUBSYSTEM_STORE_PORT=8102` and
      # `SUBSYSTEM_STORE_TOKEN_FILE=/run/secrets/subsystem-store/token` were here, and are
      # gone. Two measurements decided it:
      #
      #   * THEY CONFIGURED NOTHING. Each value was byte-identical to the code default the
      #     server falls back to with the variable unset — `server/server.py`'s
      #     `DEFAULT_STORE`/`DEFAULT_PORT`/`DEFAULT_TOKEN_FILE` and
      #     `cmd/cairn-server/main.go`'s `defaultStore`/`defaultPort`/`defaultTokenFile`.
      #     Measured on both by running each with `env -i`: the oracle prints
      #     `listening on 0.0.0.0:8102 store=/data`, and the Go server names
      #     `/run/secrets/subsystem-store/token` in its token-file refusal. So removing
      #     them moves no resolved value. `tests/test_flake_image_matches_dockerfile.py`
      #     pins that agreement against BOTH implementations now that the image no longer
      #     states it.
      #   * THEY COST THREE DEPRECATION WARNINGS AT EVERY POD START. The resolver sweeps
      #     the WHOLE process environment, and the image's own `ENV` is part of it — so the
      #     pod emitted three unactionable lines nobody could clear from a manifest.
      #     Measured: `deprecations(image_env)` is 3, and it is still 3 after a Deployment
      #     migrates its own `env:` to `CAIRN_*`, because the image half is still there.
      #     With the image setting nothing, that same migrated Deployment measures 0.
      #
      # 🔴 AND IT CLOSES THE SHADOWING HAZARD AT THE ROOT RATHER THAN DEFERRING IT TO P8.
      # The note that used to sit here explained why the image had to keep the OLD spelling:
      # an image `ENV` is a DEFAULT present whether or not the manifest mentions it, and
      # new-name-wins would have let a `CAIRN_STORE_ROOT` baked here outrank a Deployment
      # that explicitly set `SUBSYSTEM_STORE_ROOT`. That reasoning was correct and is now
      # moot: with no image default there is nothing to outrank a manifest, in either
      # spelling, so the images could adopt `CAIRN_*` later with no window at all.
      #
      # ⚠ THE ACCEPTED COST, STATED SO IT IS NOT REDISCOVERED AS A BUG. `docker inspect` and
      # this file no longer show where the store lives, which port it binds or where the
      # token is read from. The operator took that trade knowingly. What replaced the
      # env-based assertion is a pin on the CODE defaults in both implementations —
      # `STORE_DEFAULTS` in `tests/test_flake_image_matches_dockerfile.py`, ONE declared
      # constant holding all THREE values, spelled by hand and checked against each
      # implementation's own constants. (`DEPLOY_CONTRACT` in
      # `tests/test_flake_go_image_runtime_contract.py` is an alias for it, `= STORE_DEFAULTS`,
      # so the sibling module's assertions read in the same vocabulary; the declaration has
      # one home and this is not it.)
      #
      # ⚠ AND THE STARTUP LINE COVERS TWO OF THE THREE, NOT THREE — a sentence here used to
      # say it printed all of them. Both pods print
      # `listening on <host>:<port> store=<root> token-ids=…`; `token-ids=` is the credential
      # FINGERPRINTS, and neither implementation's startup line prints the token PATH. The Go
      # server's `-h` prints it as a flag default, the oracle's `--help` does not, and both
      # emit it on stderr only in the `token file <path> absent` fallback notice.
      serverEnv = {
        HOME = "/home/nonroot";
        PYTHONDONTWRITEBYTECODE = "1";
        PYTHONUNBUFFERED = "1";
      };
      serverUid = 65532;
      serverPort = 8102;

      # 🔴 THE UI'S PORT AND SESSION DIRECTORY ARE THEIR OWN BINDINGS, AND THEY ARE
      # PINNED AGAINST THE GO SOURCE RATHER THAN DERIVED FROM THE POD'S.
      #
      # They cannot come from `serverPort`: the UI binds a DIFFERENT port (the pod is
      # 8102, this is 8103), and collapsing them is the collision the separate number
      # exists to avoid. So this IS a second statement of a value — the exact hazard
      # `mkGoServerImage`'s header warns about — and the answer is not to pretend it is
      # derived but to make the duplication MEASURED:
      # `tests/test_flake_ui_image_runtime_contract.py` reads `defaultPort` and
      # `defaultSessionFile` out of `cmd/cairn-ui/main.go` and fails when either side
      # moves alone. There is no Dockerfile for this surface, so the Go source is the
      # only other statement of these values and is therefore what to pin against.
      #
      # ⚠ `uiSessionDir` IS THE *DIRECTORY* WHILE THE SOURCE DECLARES A *FILE*
      # (`/var/lib/cairn-ui/sessions`). An image can create and own a directory, not a
      # file the process has yet to write, and the deployment mounts a volume there — so
      # the binding is the dirname and the test derives it from the source constant
      # rather than carrying the path twice. 🔴 IT IS DELIBERATELY NOT UNDER `/data`:
      # `cmd/cairn-ui/main.go` gives the long reason, and the short one is that `/data`'s
      # documented operations are "enumerate" and "overwrite wholesale", which is not
      # where a table of live session credentials goes.
      uiPort = 8103;
      uiSessionDir = "/var/lib/cairn-ui";

      # 🔴 THE GO POD'S ENV IS `serverEnv` MINUS A NAMED SET — A SUBTRACTION, NEVER
      # A SECOND LITERAL. There are now THREE builds of a pod and only ONE statement
      # of the contract: `serverEnv` above. Deriving the Go image's env from it means
      # a variable added there reaches BOTH pods and cannot be forgotten on one.
      # A second attrset holding copies would invert that — the drift would be silent
      # and in the direction that matters, a pod missing an env its Deployment sets.
      #
      # 🔴 IT NOW SUBTRACTS EVERYTHING, SO `serverEnvGo` IS `{ }`, AND THE MECHANISM IS
      # KEPT ANYWAY RATHER THAN COLLAPSED TO A LITERAL. Dropping the three store variables
      # from `serverEnv` left it holding only CPython knobs and a `HOME` for them, all
      # three of which are Python-only — so the Go image's `Env` is its `//` override
      # alone (`PATH`, `SSL_CERT_FILE`). Replacing this with a hardcoded two-element env
      # would read as a simplification and would silently stop the NEXT shared variable
      # from reaching the Go pod, which is the exact drift the subtraction exists for. The
      # empty result is a fact about today's `serverEnv`, not about the derivation.
      #
      # WHY EACH NAME LEAVES, READ OUT OF THE CODE RATHER THAN ASSUMED:
      #   * `PYTHONDONTWRITEBYTECODE` / `PYTHONUNBUFFERED` are CPython knobs. A Go
      #     binary never reads either; carrying them would assert the image runs an
      #     interpreter, which is the one thing this image is for NOT doing.
      #   * `HOME` is in `serverEnv` because `lib/subsystem_read_store.py` evaluates
      #     `Path.home()` at IMPORT time and it raises in a numeric-UID container.
      #     MEASURED on the Go side rather than inferred, and stated as the METHOD
      #     rather than a count that would rot: nothing in `go list -deps
      #     ./cmd/cairn-server` calls `os.UserHomeDir` or reads `$HOME` — every such
      #     call lives under `internal/client`, which the server does not import, and
      #     `internal/hostid` reads only `CAIRN_HOST`/`ASIB_HOST`/`ACTIVITY_HOST`. Re-run
      #     that pair before moving a name out of this list. So there is no
      #     import-time home to resolve, and an env var nothing reads in an image
      #     whose env IS the deploy contract is a claim about the pod that is false.
      #
      # ⚠ WHAT THIS DOES NOT CLAIM: that `$HOME` is unreachable in the container.
      # `kubectl exec … -- sh` lands in a shell with no `HOME`. busybox `ash` does not
      # need one, and both documented procedures name absolute paths (`tar -xf -` into
      # `/data`, `kill -HUP 1`). If a procedure ever needs `~`, it goes in `serverEnv`
      # and both pods get it — which is the point of the subtraction.
      #
      # ⚠ KEEPING `HOME` AND DROPPING ONLY THE TWO `PYTHON*` KNOBS WAS PROPOSED IN REVIEW
      # AND DECLINED; RECORDED SO IT IS NOT RE-LITIGATED FROM SCRATCH. The proposal's cost
      # argument is real — this comment, and a standing obligation to re-run `go list -deps`
      # whenever the server's imports change. Three things decided it the other way.
      # (1) The failure mode of a WRONG drop is LOUD, not silent: `os.UserHomeDir` returns
      #     an explicit "$HOME is not defined" error. The silent-drift class the subtraction
      #     exists to prevent is the OTHER direction — a variable that never reaches the Go
      #     pod at all — and that one is closed structurally by `removeAttrs`.
      # (2) Keeping it is MORE derivation, not less: `mkServerImage` creates and chowns
      #     `/home/nonroot` in `fakeRootCommands` precisely because it sets `HOME`. A `HOME`
      #     naming a directory the image does not contain is false in two ways instead of
      #     one, so "keep" means restoring that mkdir here too.
      # (3) The image's `Env` IS the deploy contract, and the next operator debugging a
      #     buffering or home-directory question reads it as one.
      serverEnvPythonOnly = [ "HOME" "PYTHONDONTWRITEBYTECODE" "PYTHONUNBUFFERED" ];
      serverEnvGo = builtins.removeAttrs serverEnv serverEnvPythonOnly;

      # 🔴 THE INTERPRETER IS PINNED TO THE ONE THE SUITE ACTUALLY RUNS UNDER.
      # `pkgs.python3` follows nixpkgs and was 3.14 here, while
      # `server/Dockerfile` is `python:3.12-slim` and CI pins `python-version:
      # "3.12"` — so the packaged client and the flake image shipped an
      # interpreter NOTHING in this repo had ever run the suite against, and a
      # lock bump could move it again silently. The suite already emits a 3.14
      # tar-extraction DeprecationWarning, so the gap is behaviourally live,
      # not theoretical. Change this in the same commit as the Dockerfile and
      # the CI matrix, never alone.
      python = pkgs: pkgs.python312;

      # 🔴 THE POD IS OPERATED THROUGH `kubectl exec`, SO THE IMAGE MUST CARRY A
      # SHELL AND A TAR. This is not convenience: with `contents` set to the
      # code alone, the image has NO `PATH` and no `sh`/`tar`/`find`/`cut`, and
      # every documented operational procedure breaks on an image that
      # otherwise starts, passes health checks and serves —
      #   * `server/seed.sh` pushes the store with `kubectl exec … -- tar -xf -`
      #     and runs its containment guard through `sh -c` — so a pod built this
      #     way CANNOT BE SEEDED, and `server/README.md` says seeding by hand is
      #     the only path there is;
      #   * `server/README.md`'s token-revocation procedure is
      #     `kubectl exec … -- sh -c 'kill -HUP 1'` — so a LEAKED CREDENTIAL
      #     could not be revoked without deleting the pod.
      # busybox rather than coreutils+gnutar+findutils: it supplies every one of
      # those applets in ~2 MB, and the procedures use only POSIX spellings.
      #
      # 🔴 `serverPath` MUST NAME A DIRECTORY THE APPLETS ACTUALLY LAND IN, and
      # `serverTools` MUST REACH `contents`. Both are asserted by
      # `tests/test_flake_image_matches_dockerfile.py`, and BOTH assertions
      # exist because their absence was measured: with the guard pinning only
      # the two bindings, a mutant that reverted `contents` to `[ tree ]` and a
      # mutant that set `serverPath = "/nonexistent"` EACH SURVIVED the whole
      # suite while restoring the exact defect the guard was written for — a
      # pod that starts, serves, and cannot be seeded or rotated.
      #
      # ⚠ An earlier version of this comment said `serverPath` was "asserted
      # against this list". It was not: nothing connected the path to the tools,
      # and the sentence was a coverage claim wider than the code. That is the
      # same shape as the defect below it, one level up.
      serverPath = "/bin";
      serverTools = pkgs: [ pkgs.busybox ];

      # 🔴 THE GO POD CARRIES `serverTools` PLUS CA ROOTS, AND THE EXTRA IS NOT
      # DECORATION. `mkServerImage` ships no `/etc`, so a container built from it has
      # NO CA bundle at any path Go's `crypto/x509` searches. That costs the Python pod
      # nothing — it makes no outbound TLS connection — but `internal/identity/jwks.go`
      # fetches a JWKS over `https` for the Supabase backend, and without roots that
      # fetch fails certificate verification on an image that otherwise starts, passes
      # health checks and serves. Same shape as the missing-`sh` defect `serverTools`
      # exists for: four pinned values agreeing over a pod that cannot do its documented
      # job.
      #
      # ⚠ SCOPE, STATED SO NOBODY READS THIS AS MORE THAN IT IS. What is built and
      # pinned here is the CLOSURE's presence in `contents` — which is what gives the
      # image an `/etc/ssl/certs`, and therefore the roots; see the measurement over
      # `goServerCaBundle` below, and note that the env var is NOT the mechanism. A live
      # JWKS fetch against a real issuer is NOT verified by anything in this repository —
      # it needs a pod, a network and an issuer, none of which a nix build has.
      #
      # ⚠ AND IT IS GO-ONLY ON PURPOSE. Adding `cacert` to `serverTools` would change
      # `packages.server-image`'s layer contents, which is the one thing
      # `tests/test_flake_image_matches_dockerfile.py` is written against, for a pod
      # that has no use for it.
      goServerTools = pkgs: serverTools pkgs ++ [ pkgs.cacert ];

      # 🔴 NAMED BY ABSOLUTE STORE PATH, AND `SSL_CERT_FILE` IS *NOT* WHAT MAKES THE
      # ROOTS REACHABLE — THAT IS MEASURED, AND IT RETRACTS WHAT THIS COMMENT USED TO
      # SAY. The retracted theory, written down rather than quietly replaced: "Go's
      # `crypto/x509` reads `$SSL_CERT_FILE` first and otherwise searches system
      # locations, NONE OF WHICH THIS IMAGE HAS, BECAUSE IT HAS NO `/etc` — so shipping
      # the closure without declaring the variable leaves the bundle unreachable."
      # Both halves of that are false for THIS image. `pkgs.cacert` in `contents` is
      # root-merged by `buildLayeredImage`, so the image DOES have an `/etc`, and it
      # holds `/etc/ssl/certs/ca-bundle.crt` as a symlink into the store. `/etc/ssl/certs`
      # is in Go's `certDirectories`, which `loadSystemRoots` walks IN ADDITION to
      # whatever `$SSL_CERT_FILE` names, and the symlink targets contain `/` so
      # `readUniqueDirectoryEntries` does not filter them out.
      #
      # Measured on the built image, uid 65532, with an `x509.SystemCertPool()` probe
      # built CGO-off by the pinned toolchain:
      #
      #   SSL_CERT_FILE as shipped here .................. 121 roots
      #   SSL_CERT_FILE unset ............................ 121 roots
      #   SSL_CERT_FILE = "/nonexistent/ca-bundle.crt" ... 121 roots
      #   positive control — the SAME binary in an image
      #   with no CA roots at all ........................   0 roots
      #
      # The control is what makes the 121 a measurement rather than a harness printing a
      # reassuring number: the probe can report zero.
      #
      # 🔴 SO THIS DECLARATION HAS NO MEASURABLE EFFECT TODAY, AND THAT IS WHAT IS
      # WRITTEN HERE RATHER THAN A FRESH MECHANISM TO JUSTIFY IT. It is KEPT anyway, for
      # one reason that is about the FAILURE DIRECTION and not about today's behaviour:
      # the directory fallback depends on two things this repository does not control —
      # Go's `certDirectories` list, and `buildLayeredImage` continuing to root-merge
      # `contents` into `/etc`. If either changes, an image WITHOUT this variable loses
      # its roots silently; an image with it does not. The cost is one env var whose
      # value is correct by construction, because it is interpolated from the same
      # `pkgs.cacert` that `goServerTools` puts in `contents` — which is also the only
      # thing a guard can honestly assert about it, since the third row above shows a
      # WRONG path here is behaviourally invisible.
      #
      # The store path rather than an `/etc/…` spelling for the same reason `Cmd` uses
      # one: the file is present because `contents` carries the closure, not because
      # anything arranged a filesystem layout around it.
      goServerCaBundle = pkgs: "${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt";

      # 🔴 AN ALLOWLIST, NOT AN EXCLUDE LIST, FOR THE SAME REASON
      # `server/Dockerfile.dockerignore` is one: a working tree of this repo
      # holds store CONTENT (entry files) and caches that must never reach a
      # build artefact, and an exclude list ships whatever nobody thought to
      # name. Flake source is already git-filtered — untracked files are not
      # copied — but that is a property of how the source was FETCHED, not of
      # this derivation, and it does not hold for `nix build path:.`. Stating
      # the filter here makes the property true of the derivation itself.
      onlyCode = pkgs: pkgs.lib.cleanSourceWith {
        src = ./.;
        name = "cairn-source";
        filter = path: type:
          let rel = pkgs.lib.removePrefix (toString ./. + "/") (toString path);
          in
          (type == "directory" && (rel == "lib" || rel == "server"))
          || (rel == "cairn")
          || (pkgs.lib.hasPrefix "lib/" rel && pkgs.lib.hasSuffix ".py" rel)
          || (rel == "server/server.py");
      };

      # 🔴 A SECOND ALLOWLIST FOR THE GO TREE, AND IT IS SEPARATE FROM
      # `onlyCode` ON PURPOSE. `onlyCode` feeds the PYTHON artefacts — the
      # packaged client and the pod image — and widening it to carry `go.mod`,
      # `cmd/` and `internal/` would put the Go sources into both of those
      # closures for nothing, and would change the `server-image` layer contents
      # that `tests/test_flake_image_matches_dockerfile.py` is written against.
      # Two filters is the honest shape while two implementations are alive; the
      # day Python is retired, one of them goes away rather than being widened.
      #
      # An allowlist rather than an exclude list, for the same reason as
      # `onlyCode`: a working tree of this repo holds store CONTENT and caches
      # that must never reach a build artefact, and an exclude list ships
      # whatever nobody thought to name.
      #
      # 🔴 `tests/conformance/requests.json` IS IN THIS FILTER ON PURPOSE, AND IT
      # IS THE ONE NON-GO FILE HERE. `TestTheRouteLedgerMatchesTheConformance
      # Corpus` reads that list and compares it against the routes this server
      # dispatches, in BOTH directions — it is the guard that closes the
      # "a route added after the fixtures were generated" blind spot for a
      # non-Python implementation, which `tests/conformance/README.md` names as
      # explicitly open on this side. Leaving the file out would make the
      # package's own test run skip its most load-bearing guard, in a sandbox,
      # silently. A sandbox that pins a dimension cannot be read as coverage of
      # it — so the dimension is supplied instead.
      #
      # 🔴 AND `internal/report/testdata/reader_fixtures.json` IS HERE FOR EXACTLY THE
      # SAME REASON, WHICH IS WHY THE TWO SIT TOGETHER. That file is the ORACLE'S OWN
      # rendered bytes for fifty report shapes the HTTP corpus cannot send — every
      # badge, the pagination branches, the search rungs, the difflib ratios — and
      # `TestTheRenderedBytesMatchTheORACLE…` is the differential gate over it.
      # Leaving it out would not skip quietly: the test calls `t.Fatalf`, so the
      # sandbox build would go RED. That is the good direction, and it is still worth
      # naming, because the failure would read as "the flake is broken" rather than
      # "the fixture is not in the filter". A `.json` suffix test was declined for the
      # reason the `.go` one below gives: it would say something wider than it means.
      onlyGo = pkgs: pkgs.lib.cleanSourceWith {
        src = ./.;
        name = "cairn-go-source";
        filter = path: type:
          let rel = pkgs.lib.removePrefix (toString ./. + "/") (toString path);
          in
          (type == "directory" && (
            rel == "cmd" || rel == "internal"
            || rel == "tests" || rel == "tests/conformance"
            # 🔴 THE DIRECTORY ROW IS REQUIRED FOR THE TWO FILE ROWS BELOW TO MEAN
            # ANYTHING, AND THAT IS MEASURED RATHER THAN REASONED. `cleanSourceWith`
            # never visits a path whose PARENT the filter rejected, so naming
            # `uiaudit/go.mod` while `uiaudit` itself was excluded allowed exactly
            # nothing — the filtered tree still held no such file. Verified by
            # materialising the filtered source and listing it.
            || rel == "uiaudit"
            || pkgs.lib.hasPrefix "cmd/" rel || pkgs.lib.hasPrefix "internal/" rel
          ))
          || (rel == "go.mod")
          # 🔴 `go.sum` IS NAMED EXPLICITLY, AND ITS ABSENCE FROM THIS LIST WAS A
          # BUILD FAILURE RATHER THAN A SILENT ONE — which is the good direction and
          # still worth writing down. `buildGoModule` with a real `vendorHash` needs
          # the lock file to resolve the module graph, so a filter that carried
          # `go.mod` alone stops the fetch phase dead. It is spelled as its own row
          # for the reason the `.go` row below gives: a `go.*` glob would be an
          # allowlist that says something wider than it means.
          #
          # ⚠ AND IT IS READ BY A TEST, NOT ONLY BY THE FETCHER.
          # `internal/depspolicy` parses BOTH lock files and compares them against
          # each other and against its allowlist, so a sandbox build without this
          # row would fail that test for a reason that has nothing to do with the
          # tree — the same shape as `reader_fixtures.json` two rows down.
          || (rel == "go.sum")
          || (rel == "tests/conformance/requests.json")
          || (rel == "internal/report/testdata/reader_fixtures.json")
          # 🔴 AND A THIRD ONE, FOUND BY A RED SANDBOX BUILD RATHER THAN BY ANYONE
          # READING THE PARAGRAPH ABOVE. `internal/store/markersweep_test.go` is the
          # differential sweep behind `marker.go`'s narrowing ledger: it replays the
          # ORACLE's verdicts for every generated line against two hand-rolled
          # transcriptions of CPython regexes. Its fixture was first written to
          # `tests/fixtures/`, which this allowlist does not carry — so the sweep was
          # green on the dev host and RED here, which is the two-tier split the
          # comment above describes, arriving one commit after it was written. Moved
          # under the package it tests, like `reader_fixtures.json`, and named here
          # for the same reason: the dimension is supplied, not pinned away.
          || (rel == "internal/store/testdata/marker_oracle_sweep.json")
          # 🔴 THE NESTED MODULE'S TWO LOCK FILES, AND NOTHING ELSE FROM THAT
          # DIRECTORY. `internal/depspolicy`'s
          # `TestTheNestedModuleSetIsExactlyTheAllowlist` walks the tree for
          # `go.mod` files and compares BOTH lock files against a declared
          # allowlist, failing on grow or shrink — which is what closes the
          # nested-module escape its package doc describes. That test runs inside
          # these derivations, so without these two rows it would read a tree with
          # zero nested modules and report "the set SHRANK": red for a reason that
          # has nothing to do with the code. The test REFUSES rather than skipping
          # in that case and names this filter, because a comparison against an
          # absent operand reports SAME rather than MISSING.
          #
          # ⚠ THE `.go` FILES UNDER `uiaudit/` ARE DELIBERATELY STILL EXCLUDED, and
          # the asymmetry is the whole point of the arrangement. Nothing in any nix
          # derivation builds that module — it is not a package, not an output and
          # not on a deploy path — so shipping its sources into a sandbox that
          # cannot build them would add `chromedp` to every derivation's source
          # closure for no gain. What these rows buy is that the LEDGER over that
          # module is checkable here, which makes it a build failure rather than an
          # advisory tick.
          || (rel == "uiaudit/go.mod")
          || (rel == "uiaudit/go.sum")
          # A `.go` file only under the two directories this module is made of. A
          # bare suffix test would also carry a stray `.go` anywhere in the tree,
          # which is an allowlist that says something wider than it means.
          || ((pkgs.lib.hasPrefix "cmd/" rel || pkgs.lib.hasPrefix "internal/" rel)
              && pkgs.lib.hasSuffix ".go" rel);
      };

      # 🔴 THE GO TOOLCHAIN IS PINNED THE SAME WAY THE INTERPRETER IS, AND FOR
      # THE SAME REASON. `pkgs.go` follows nixpkgs and is **1.26.7** against the
      # pinned lock, while `go.mod` declares 1.25 and the tests were run under
      # 1.25.14 — so the default would have built the server under a compiler
      # nothing in this repo had ever run the tests under, and a lock bump would
      # move it again silently. That is the exact shape of the defect
      # `pkgs.python3` produced here before the interpreter was pinned. Move
      # this, `go.mod`'s `go` directive and the CI job's `go-version` together or
      # not at all.
      #
      # 🔴 AND IT IS `buildGo125Module`, NOT `buildGoModule` WITH THE COMPILER IN
      # `nativeBuildInputs`. That spelling was tried and is a NO-OP for the
      # pin: `buildGoModule` uses the `go` from its OWN scope, so the build
      # fetched 1.26.7 while the derivation advertised 1.25 in its inputs —
      # a pin that reads as one and is not.
      #
      # ⚠ `go_1_25` TRACKS A SERIES, NOT A PATCH RELEASE. Measured against this
      # lock it is exactly 1.25.14, the version the tests ran under; the property
      # this pin holds is "the MINOR version", which is what a language-version
      # skew would break.
      buildGoPinned = pkgs: pkgs.buildGo125Module;

      # 🔴 ONE BINDING, READ BY ALL THREE GO DERIVATIONS, BECAUSE THERE IS ONE
      # MODULE. Three literals would be three chances for one to go stale, and a
      # stale vendor hash is a build failure whose message points at the derivation
      # rather than at the `go.mod` change that caused it.
      #
      # ⚠ IT COVERS `cairn-go` AND `cairn-server-go` TOO, NEITHER OF WHICH IMPORTS
      # ANYTHING THIRD-PARTY. That is the cost of one module rather than three, and
      # it is accepted rather than worked around: a second module for the UI would
      # split `internal/` in two and put a version skew between the renderer the pod
      # links and the one the CLI links, which is the exact failure `internal/report`
      # being ONE package exists to prevent.
      #
      # To move it: change the dependency, set this to
      # `lib.fakeHash`, run `nix build .#cairn-ui`, and copy the hash nix prints.
      goVendorHash = "sha256-La+SYvwXEPSEbmLbsEJfXdMzYfs/3Df3tbSfNsrlkzU=";

      # 🔴 THIS USED TO BE `vendorHash = null`, AND THE SENTENCE THAT STOOD HERE —
      # "this is the line that makes a new dependency a build FAILURE rather than a
      # silent addition to the serving path" — IS NO LONGER TRUE OF ANY LINE IN THIS
      # FILE. It is written out rather than deleted because a maintainer who
      # remembers the old guarantee has to be told where it went, not left to infer
      # that nothing replaced it.
      #
      # `internal/ui` renders HTML with `maragu.dev/gomponents`, there is ONE module,
      # and so all three Go derivations carry a real vendor hash — including the two
      # that import nothing third-party and pay it anyway. That loss was an operator
      # decision, taken explicitly.
      #
      # 🔴 WHERE THE REFUSAL LIVES NOW: `internal/depspolicy`, whose package doc is
      # the ONE place it is stated. Not restated here — that comment already had five
      # copies and this was one of them.
      #
      # What this file contributes to it, and the reason the pointer is here at all:
      # all three derivations below set `doCheck = true` with `checkPhase` spelled out
      # as `go vet ./...` then `go test ./...`, so the policy's tests run in the
      # PACKAGE build a consumer makes rather than only in CI. That is what makes an
      # import-ban failure a BUILD failure for anyone building through nix — the same
      # consequence `vendorHash = null` had, which is the strongest thing that can be
      # said for the replacement and is said in full over there.
      #
      # ⚠ A VENDOR HASH IS NOT A DEPENDENCY GATE AND MUST NOT BE READ AS ONE. It
      # pins the BYTES of whatever the module graph resolves to; it says nothing
      # about which modules are in that graph, and adding one just changes the hash.
      mkGoServer = pkgs: (buildGoPinned pkgs) {
        pname = "cairn-server";
        inherit version;
        src = onlyGo pkgs;
        vendorHash = goVendorHash;

        subPackages = [ "cmd/cairn-server" ];

        # 🔴 THE UNIT TESTS RUN IN THE BUILD, NOT ONLY IN `checks`. A consumer
        # that pins this flake builds the PACKAGE and never runs
        # `nix flake check`, so a broken token parser or a broken tar writer
        # would otherwise reach a machine — and `checks` is also where a
        # `--no-link` CI job is easiest to forget.
        doCheck = true;

        # 🔴 `go test ./...`, SPELLED OUT, BECAUSE THE DEFAULT CHECK PHASE
        # HONOURS `subPackages` AND THAT MADE IT VACUOUS. MEASURED: with
        # `subPackages = [ "cmd/cairn-server" ]` and `doCheck = true`, the build
        # log read
        #
        #     Running phase: checkPhase
        #     ?  github.com/ZacxDev/cairn/cmd/cairn-server  [no test files]
        #     Running phase: installPhase
        #
        # — a GREEN check phase that ran ZERO tests, because every test in this
        # module LIVED under `internal/` at the time and `subPackages` had scoped
        # the test walk to the one directory that had none. That is the reassuring
        # zero this repository keeps finding, arriving through a build option whose
        # only documented job is to narrow what gets INSTALLED.
        #
        # ⚠ `cmd/cairn-server` HAS TESTS NOW, and that makes the trap WORSE rather
        # than better: the same narrowing would today run one package and report a
        # plausible-looking PASS line, so the tell that made it visible — the
        # `[no test files]` line quoted above — is gone.
        #
        # `go vet` is here too: it is the cheapest check that reads the code
        # rather than running it, and a printf-shaped mistake in a refusal
        # message is a refusal the goldens would catch much later.
        checkPhase = ''
          runHook preCheck
          go vet ./...
          go test ./...
          runHook postCheck
        '';

        meta = with pkgs.lib; {
          description = "The Go port of the cairn store API (P1: dual-run against the Python oracle)";
          homepage = "https://github.com/ZacxDev/cairn";
          license = licenses.mit;
          mainProgram = "cairn-server";
          platforms = platforms.unix;
        };
      };

      # 🔴 THE GO CLIENT IS NOW `packages.default`/`apps.default`, AND THE PYTHON ONE IS STILL
      # SHIPPED AS `packages.cairn`. That is a DEFAULT CUTOVER, not a deletion: the oracle, its
      # `lib/`, `checks.client-resolves-its-lib` and the parity gate are all untouched, and
      # Python is retired at P8.
      #
      # 🔴 WHAT LICENSED THE FLIP WAS AN OPERATOR DECISION, NOT A GREEN GATE — RECORDED HERE
      # SO NOBODY RE-DERIVES IT FROM THE GATE. An earlier draft took the flip on exactly that
      # reading ("the gate held, so the default can move") and was REVERTED; the gate being
      # green has never licensed this and still does not, however green it gets. The decision
      # was taken after residual 8 closed, which removed the flip's one MEASURED blocker: on a
      # host with more than one instance configured, every read verb — `cairn doctor`
      # included, the quickstart this repository's own README recommends — refused at exit 11.
      # The read verbs route now and that guard is deleted. That closure is a PRECONDITION,
      # not the licence.
      #
      # ⚠ THE FLIP WIDENS THE CLI CONTRACT, AND THAT IS ITS COST RATHER THAN A SIDE EFFECT
      # (`tests/parity/README.md` residual 7). `cairn -verbs` and `cairn -exit-codes` exit 0
      # with a table on stdout here where the oracle's argparse exits 2 with `usage:`, so a
      # single-dash token `nix run github:…/cairn` refused BEFORE this commit answers 0 AFTER
      # it, for every consumer who does not name `#cairn`. `README.md` carries the
      # announcement; measured at both points on the flip's branch.
      #
      # 🔴 `gitMinimal` ON THE WRAPPER'S PATH, FOR THE SAME REASON THE PYTHON PACKAGE HAS IT.
      # The client invokes `git` by BARE NAME to derive a repo's scope
      # (`internal/client/reposcope.go`), so a package that did not carry its own answer
      # would inherit whatever the caller's PATH holds — and `cairn recall` with no `--scope`
      # would fail differently on two machines. `gitMinimal`, not `git`: the only invocations
      # are two `rev-parse`s, and full `git` drags perl's CGI/libwww stack.
      mkGoClient = pkgs: (buildGoPinned pkgs) {
        pname = "cairn-go";
        inherit version;
        src = onlyGo pkgs;
        vendorHash = goVendorHash;

        subPackages = [ "cmd/cairn" ];

        nativeBuildInputs = [ pkgs.makeWrapper ];

        # 🔴 THE UNIT TESTS RUN IN THE BUILD, AND `checkPhase` IS SPELLED OUT BECAUSE THE
        # DEFAULT HONOURS `subPackages` AND THAT MADE IT VACUOUS ON THE SERVER DERIVATION —
        # measured there: a GREEN check phase that ran ZERO tests, because `subPackages` had
        # scoped the test walk to the one directory with none. Every test in this module
        # lives under `internal/`.
        doCheck = true;
        checkPhase = ''
          runHook preCheck
          go vet ./...
          go test ./...
          runHook postCheck
        '';

        postInstall = ''
          wrapProgram $out/bin/cairn \
            --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.gitMinimal ]}
        '';

        meta = with pkgs.lib; {
          description = "The Go port of the cairn client (P2: parity-gated against the Python one)";
          homepage = "https://github.com/ZacxDev/cairn";
          license = licenses.mit;
          mainProgram = "cairn";
          platforms = platforms.unix;
        };
      };

      # 🔴 THE BROWSER SURFACE, AND THE ONLY ARTEFACT HERE THAT LINKS A THIRD-PARTY
      # MODULE. `cmd/cairn-ui` carries the entries page, cookie sessions with a
      # sign-in pair, a GitHub sign-in through the operator's GoTrue, one static
      # stylesheet and the SHARE FLOW — over one authentication chain and one
      # rendering path.
      #
      # ⚠ IT IS PUBLISHED AND IT IS DEPLOYED, AND THIS COMMENT HAS NOW BEEN WRONG
      # ABOUT THAT IN EVERY DIRECTION IT COULD BE. It said "and no image wraps it",
      # which died when `packages.ui-image` landed. It then said "It is DEPLOYED BY
      # NOTHING — saying so is part of the change", with a ⚠ re-affirming that of the
      # two claims "only the second still holds". Both halves are false now: the image
      # is pushed by `.github/workflows/publish-image.yml`, which then proves it
      # pullable with no credentials, and a manifest in the operator's GitOps
      # repository points a pod at it. ⚠ A third sentence here was retired earlier for
      # cross-referencing `cmd/cairn-server`'s doc comment — and the lesson generalises
      # to this very block: a claim about what DEPLOYS an artefact is a claim about a
      # repository nothing in this file can see.
      #
      # ⚠ AND THE PHASE COUNT IS GONE FROM THIS BLOCK, ELEVEN LINES ABOVE THE SENTENCE
      # THAT SAYS IT IS NOT REPEATED ANYWHERE. It read "now carries three phases" while
      # the paragraph below said "THE PHASE COUNT IS NOT REPEATED AS A NUMBER ANYWHERE
      # ELSE HERE" — the count and its own prohibition, in one comment. It said "PHASE
      # A: one page" through two phases before that. `ui.DeclaredRouteLedger()` is the
      # count that cannot go stale; this block names what the surface DOES.
      #
      # 🔴 NO `gitMinimal` ON A WRAPPER, AND THE ABSENCE IS DELIBERATE RATHER THAN
      # FORGOTTEN. `packages.cairn` and `packages.cairn-go` carry one because their
      # code invokes `git` by BARE NAME to derive a repo's scope
      # (`internal/client/reposcope.go`), so a package without its own answer would
      # behave differently on two machines. This binary has no repo, no cwd that
      # means anything, and imports neither `internal/client` nor anything that
      # shells out — it reads a store root it is told about. A wrapper here would be
      # a PATH entry with no caller, which is the shape this repository refuses
      # elsewhere as exported API with no consumer.
      #
      # ⚠ WHAT `doCheck` HERE BUYS THAT THE OTHER TWO DERIVATIONS DO NOT: nothing.
      # `checkPhase` is `go test ./...` in all three, so the UI's tests already run
      # in the pod's and the client's builds too. It is spelled out identically
      # anyway, because the failure that made it necessary — `subPackages` silently
      # scoping the test walk — is a property of this builder, not of a package.
      mkGoUI = pkgs: (buildGoPinned pkgs) {
        pname = "cairn-ui";
        inherit version;
        src = onlyGo pkgs;
        vendorHash = goVendorHash;

        subPackages = [ "cmd/cairn-ui" ];

        doCheck = true;
        checkPhase = ''
          runHook preCheck
          go vet ./...
          go test ./...
          runHook postCheck
        '';

        meta = with pkgs.lib; {
          description = "The cairn browser surface: entries page, sign-in (credential form and GitHub), share flow; published and deployed";
          homepage = "https://github.com/ZacxDev/cairn";
          license = licenses.mit;
          mainProgram = "cairn-ui";
          platforms = platforms.unix;
        };
      };

      mkCairn = pkgs: pkgs.stdenv.mkDerivation {
        pname = "cairn";
        inherit version;
        src = onlyCode pkgs;

        strictDeps = true;
        nativeBuildInputs = [ pkgs.makeWrapper ];
        dontBuild = true;

        # 🔴 `lib/` MUST SIT BESIDE THE REAL SCRIPT, AND PYTHONPATH IS NOT A
        # SUBSTITUTE. The client finds its siblings with
        # `Path(__file__).resolve().parent / "lib"`, and `.resolve()` follows
        # symlinks — so the directory that must contain `lib/` is the one
        # holding the REAL file, never the one holding whatever was invoked.
        # Hence real script and `lib/` together under `libexec`, with `bin/`
        # holding a wrapper that execs it. Exporting PYTHONPATH instead would
        # make the modules reachable by a SECOND mechanism shadowing the first,
        # leaving the file's own stated one silently dead — so the next person
        # to move this layout would see nothing break until they also dropped
        # the wrapper.
        installPhase = ''
          runHook preInstall

          install -Dm755 cairn $out/libexec/cairn/cairn
          install -Dm644 -t $out/libexec/cairn/lib lib/*.py

          # `--replace-fail`, never `--replace`: a substitution that matches
          # nothing must be an error. A silently-unmatched shebang rewrite
          # leaves `/usr/bin/env python3` in a store path, which then works on
          # the machine that built it and fails on one with no system python —
          # a failure landing nowhere near its cause.
          substituteInPlace $out/libexec/cairn/cairn \
            --replace-fail '#!/usr/bin/env python3' '#!${(python pkgs)}/bin/python3'

          # `git` is invoked by BARE NAME (`lib/entry_shape.py::_git`) to derive
          # a repo's scope. Prefixed rather than suffixed so the package carries
          # its own answer instead of inheriting whatever the caller's PATH holds.
          #
          # `gitMinimal`, not `git`: the only invocations are `rev-parse` and
          # `merge-base`, and full `git` drags perl's CGI/libwww stack — 385 MiB
          # of a 386 MiB closure, paid on every consumer's switch, for an 800 KB
          # stdlib script. gitMinimal supplies both and closes at 159 MiB.
          makeWrapper $out/libexec/cairn/cairn $out/bin/cairn \
            --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.gitMinimal ]}

          runHook postInstall
        '';

        # 🔴 WITHOUT THIS THE DERIVATION IS NOT REPRODUCIBLE, and `nix build
        # --rebuild` says so: the install check below IMPORTS the whole closure,
        # CPython writes `lib/__pycache__/*.pyc` into `$out` before the daemon
        # seals it, and each `.pyc` header records the source mtime as build
        # WALL-CLOCK while the installed `.py` are normalised to mtime 1. Two
        # builds of one input then differ in exactly those seven files.
        # Worse, that bytecode is permanently INVALID and never used — the
        # recorded mtime never matches the sealed source, so every invocation
        # recompiles anyway and the files are dead weight in the closure.
        PYTHONDONTWRITEBYTECODE = "1";

        # 🔴 AT BUILD TIME, NOT ONLY IN `checks`. A consumer pinning this flake
        # builds the PACKAGE and never runs `nix flake check`, so a
        # sibling-import break would otherwise reach a machine and be found by
        # the operator at the moment they wanted to read a note.
        #
        # ⚠ `--help` IS SUFFICIENT FOR THAT BREAK — an earlier version of this
        # comment claimed the opposite ("argparse is built before any subcommand
        # imports anything, so `--help` alone is not evidence") and it was
        # MEASURED FALSE: the imports are at module scope, so removing `lib/`
        # kills `--help` at line 91 with `ModuleNotFoundError: timeouts`, right
        # here, before `checks.client-resolves-its-lib` can even be built.
        # `checks.client-resolves-its-lib` therefore earns its place on the
        # POSITIVE side — it runs a real subcommand to completion against a real
        # cache root — not as a deeper detector of a missing `lib/`.
        doInstallCheck = true;
        installCheckPhase = ''
          runHook preInstallCheck
          $out/bin/cairn --help > /dev/null
          runHook postInstallCheck
        '';

        meta = with pkgs.lib; {
          description = "Read-through client for the cairn subsystem-note store";
          homepage = "https://github.com/ZacxDev/cairn";
          license = licenses.mit;
          mainProgram = "cairn";
          platforms = platforms.unix;
        };
      };

      mkServerImage = pkgs:
        let
          # ALL of `lib/`, not an enumeration. The Dockerfile enumerates because
          # a docker build context is a directory it must not slurp wholesale,
          # and `test_the_image_copies_every_module_it_needs` exists to keep
          # that hand-written list honest. Here the source filter has already
          # excluded everything that is not code, so there is no list to keep
          # honest and no closure for it to get wrong — the module set cannot
          # drift from the imports because it is not stated separately.
          tree = pkgs.runCommand "cairn-server-tree" { } ''
            mkdir -p $out/app/lib $out/app/server
            cp ${onlyCode pkgs}/lib/*.py       $out/app/lib/
            cp ${onlyCode pkgs}/server/server.py $out/app/server/
          '';
        in
        pkgs.dockerTools.buildLayeredImage {
          name = "cairn-store";
          tag = version;
          contents = [ tree ] ++ serverTools pkgs;

          # `/data` is where the PVC mounts and `/home/nonroot` is what HOME
          # names; both are created and owned here so the image is runnable
          # without a mount. ⚠ UNGUARDED: nothing in this repo checks either
          # line. `publish-image.yml`'s smoke run starts this image with no
          # token source and expects a NON-ZERO exit, and `server/server.py`
          # returns on the missing token before it opens the store root.
          fakeRootCommands = ''
            mkdir -p ./data ./home/nonroot
            chown -R ${toString serverUid}:${toString serverUid} ./data ./home/nonroot
          '';
          enableFakechroot = true;

          config = {
            Cmd = [ "${(python pkgs)}/bin/python3" "/app/server/server.py" ];
            # 🔴 `PATH` IS DECLARED, and its absence was not cosmetic. Without
            # it `kubectl exec … -- tar` resolves nothing even once busybox is
            # in the image, so the seed path fails with `executable file not
            # found in $PATH` rather than anything naming the real cause.
            Env = pkgs.lib.mapAttrsToList (k: v: "${k}=${v}")
              (serverEnv // { PATH = serverPath; });
            User = "${toString serverUid}:${toString serverUid}";
            ExposedPorts = { "${toString serverPort}/tcp" = { }; };
            WorkingDir = "/app";
          };
        };

      # 🔴 THE GO POD'S IMAGE. IT IS AN ADDITION, AND UNTIL IT LANDED THERE WAS NO
      # GO SERVER IMAGE AT ALL — `packages.cairn-server-go` is a bare binary package.
      # ⚠ IT IS NOW PUBLISHED: `.github/workflows/publish-image.yml` pushes this output
      # to the `cairn-store-go` ghcr package beside the Python pod's `cairn-store`,
      # under the same `sha-<40-hex>` tag scheme. 🔴 AND IT IS NOW DEPLOYED: the
      # cluster pulls `cairn-store-go`. ⚠ This read "PUBLISHED IS NOT DEPLOYED — no
      # manifest references it, and the cutover is a separate decision"; the cutover
      # was taken and both halves went false. It is the site an earlier draft of the
      # sweep MISSED while editing this very file — and then cited, from
      # `publish-image.yml`, as proof `flake.nix` had never made the claim.
      #
      # 🔴 A THIRD BUILD, NOT A THIRD STATEMENT OF THE CONTRACT, AND THAT DISTINCTION
      # IS THE WHOLE DESIGN. `server/README.md` says "do not add a third way to produce
      # this pod: a third statement of the runtime contract would be outside that pin,
      # and it would be the one that actually ships". The hazard it names is a COPY.
      # This derivation states nothing: uid, port, exposed port and every environment
      # variable come from `serverUid`/`serverPort`/`serverEnvGo`, which are the same
      # bindings `tests/test_flake_image_matches_dockerfile.py` pins against
      # `server/Dockerfile`. `tests/test_flake_go_image_runtime_contract.py` is what
      # closes the loop on this side, because being DERIVED is a property of today's
      # source and a copy is one edit away.
      #
      # 🔴 IT CARRIES BUSYBOX AND DECLARES `PATH` FOR EXACTLY THE REASON `mkServerImage`
      # DOES — see the 🔴 above `serverPath`. The operational procedures are the pod's,
      # not the interpreter's: `server/seed.sh` pushes the store through
      # `kubectl exec … -- tar -xf -` and runs its containment guard through `sh -c`,
      # and `server/README.md`'s credential revocation is
      # `kubectl exec … -- sh -c 'kill -HUP 1'`. Both survive the port —
      # `cmd/cairn-server/main.go` installs a `SIGHUP` handler and logs a reload
      # verdict — so an image without a shell would take away a revocation path that
      # the binary itself still implements.
      #
      # ⚠ `Cmd` NAMES THE BINARY BY ABSOLUTE STORE PATH, like `mkServerImage` names its
      # interpreter. `/bin/cairn-server` would also resolve, and that is the reason not
      # to use it: the entrypoint would then depend on `contents` placing the package's
      # `bin/` at the image root AND on `PATH`, so a change to either would turn a
      # wiring mistake into a container that does not start.
      #
      # ⚠ `WorkingDir = "/"`, not `/app`: there is no `/app`. The Python pod's `/app` is
      # where its SOURCE is copied, and a single binary has no source tree. `/` matches
      # what `server/Dockerfile` leaves the Python pod at, and every path the server
      # touches — the store, the token file — is absolute.
      mkGoServerImage = pkgs:
        let goServer = mkGoServer pkgs;
        in
        pkgs.dockerTools.buildLayeredImage {
          name = "cairn-store-go";
          tag = version;
          contents = [ goServer ] ++ goServerTools pkgs;

          # `/data` is where the PVC mounts; created and owned here so the image is
          # runnable without a mount. No `/home/nonroot`: this pod has no `HOME` — see
          # `serverEnvPythonOnly`.
          #
          # ⚠ THE `chown` IS UNGUARDED, AND THE RETRACTED CLAIM WAS THAT A SMOKE TEST
          # WOULD CATCH IT. The sentence that used to carry this — "nothing runs this
          # image" — is NO LONGER TRUE: `.github/workflows/publish-image.yml` runs it
          # three ways before it publishes. The claim NARROWS rather than lapses,
          # because none of the three reads OWNERSHIP: `ls -A /data | wc -l` counts
          # NAMES and a root-owned `/data` is still listable by this uid;
          # `cairn-server -routes` never touches `/data`; and the no-token start
          # refuses at `authz.LoadTokens` (78) BEFORE `api.New` reads the store root,
          # so nothing writes. A `chown` mutant therefore still survives — now with
          # three smoke runs that structurally cannot notice. `ci.yml` remains a build
          # assertion only, and says so in its own comment. (The `mkdir` is not a
          # survivor — dropping it while keeping the `chown` fails the build.) Whether
          # a PVC mounting over `/data` would mask it is a fact about a manifest
          # outside this repo and is not verifiable here.
          #
          # ⚠ SCOPE OF THAT NARROWING: it was derived by READING the three commands in
          # the publish workflow's Go control step against `cmd/cairn-server/main.go`'s
          # startup order, not by building a `chown`-less image and watching the step
          # stay green. If you close it, close it with a control that reads ownership.
          fakeRootCommands = ''
            mkdir -p ./data
            chown -R ${toString serverUid}:${toString serverUid} ./data
          '';
          enableFakechroot = true;

          config = {
            Cmd = [ "${pkgs.lib.getExe goServer}" ];
            Env = pkgs.lib.mapAttrsToList (k: v: "${k}=${v}") (serverEnvGo // {
              PATH = serverPath;
              SSL_CERT_FILE = goServerCaBundle pkgs;
            });
            User = "${toString serverUid}:${toString serverUid}";
            ExposedPorts = { "${toString serverPort}/tcp" = { }; };
            WorkingDir = "/";
          };
        };

      # A FOURTH IMAGE, AND THE FIRST THAT IS NOT A POD: `cmd/cairn-ui` is the browser
      # surface. It reuses `mkGoServerImage`'s shape wherever the shape is genuinely the
      # same thing — the nonroot uid, the busybox toolchain, the named CA bundle, an
      # absolute-store-path `Cmd`, `WorkingDir = "/"` — and diverges in exactly two
      # places, both of which are properties of what this program IS rather than
      # preferences:
      #
      # 🔴 (1) IT HAS A SECOND WRITABLE PATH, AND THAT IS THE WHOLE DIFFERENCE. The pod
      # writes nothing: its `/data` is a read path and its token file is a mounted
      # secret. This surface OWNS A SESSION TABLE — `identity.FileSessionStore` rewrites
      # it under an exclusive `flock` on every sign-in, sign-out and expiry sweep — so
      # `uiSessionDir` is created AND chowned here. ⚠ The `chown` is what the pod's
      # equivalent comment calls an unguarded mutant; here it is NOT, because
      # `OpenFileSessionStore` refuses at startup (exit 78) when the directory is not
      # writable by this uid, which is a control that reads the effect of ownership
      # rather than the ownership bit. A `chown`-less image therefore fails to start
      # instead of serving — the loud direction.
      #
      # 🔴 (2) IT DECLARES NO STORE OR JOURNAL PATH, DELIBERATELY. Both are compiled-in
      # defaults the program already falls back to (`/data`, and a control journal that
      # REFUSES rather than defaults), and `mkGoServerImage`'s env history is the reason
      # not to restate them: three `SUBSYSTEM_STORE_*` variables were set to values
      # byte-identical to the code defaults, so they configured nothing while emitting a
      # deprecation warning at every pod start that no manifest could clear. The env here
      # is `PATH` and the CA bundle and nothing else; everything operational arrives as a
      # flag or an env var from the Deployment.
      #
      # ⚠ THE CA BUNDLE IS CARRIED WITHOUT A CONSUMER OF ANY KIND, AND THE HONEST VERSION
      # IS BLUNTER THAN "UNTESTED". This binary has NO WIRED TLS-EGRESS PATH AT ALL:
      # `internal/ui/auth.go` passes `nil` in the Supabase slot of `identity.Backends`, and
      # there is no `http.Client` or `http.Get` anywhere under `cmd/cairn-ui` or
      # `internal/ui` — measured. So this is not a fetch that has never been exercised
      # against a real issuer; it is a fetch that cannot currently happen. It is here because the surface
      # this image exists to publish is the one that will do it, and because the pod's
      # own bundle was measured INERT IN BOTH DIRECTIONS (121 roots with the variable set,
      # unset, and pointed at `/nonexistent`) — so its presence is cheap and its absence
      # would be discovered by a handshake failure in production. Do not read it as
      # evidence that TLS egress works here.
      mkGoUIImage = pkgs:
        let goUI = mkGoUI pkgs;
        in
        pkgs.dockerTools.buildLayeredImage {
          name = "cairn-ui";
          tag = version;
          contents = [ goUI ] ++ goServerTools pkgs;

          fakeRootCommands = ''
            mkdir -p .${uiSessionDir}
            chown -R ${toString serverUid}:${toString serverUid} .${uiSessionDir}
          '';
          enableFakechroot = true;

          config = {
            Cmd = [ "${pkgs.lib.getExe goUI}" ];
            Env = pkgs.lib.mapAttrsToList (k: v: "${k}=${v}") {
              PATH = serverPath;
              SSL_CERT_FILE = goServerCaBundle pkgs;
            };
            User = "${toString serverUid}:${toString serverUid}";
            ExposedPorts = { "${toString uiPort}/tcp" = { }; };
            WorkingDir = "/";
          };
        };
    in
    {
      packages = forAll (pkgs:
        {
          cairn = mkCairn pkgs;
          # 🔴 `default` IS THE GO CLIENT, AND `cairn` IS STILL THE PYTHON ONE. The
          # cutover was an OPERATOR DECISION taken after residual 8 closed — never a
          # gate outcome; see the block above `mkGoClient` for the reverted draft that
          # read it the other way. `packages.cairn` is NOT deleted here: it is still
          # the oracle the parity gate measures against, and it goes at P8.
          #
          # 🔴 THIS LINE AND `apps.default` MOVE TOGETHER OR NOT AT ALL — see the block
          # above `apps` for why one alone gives two clients under one name.
          default = mkGoClient pkgs;
          cairn-server-go = mkGoServer pkgs;
          cairn-go = mkGoClient pkgs;
          # 🔴 THE BROWSER SURFACE HAS NO `apps` ENTRY AND IS NOT IN `default`: it is
          # built by name or not at all. ⚠ THAT IS A FACT ABOUT THIS FLAKE AND NOT ABOUT
          # WHAT IS RUNNING, WHICH IS THE INFERENCE TWO EARLIER WORDINGS MADE HERE. The
          # first said "AND NOTHING ELSE — no `apps` entry, no image"; the second said
          # this is "what 'deployed by nothing' means concretely". There IS an image
          # (`packages.ui-image`), `publish-image.yml` PUSHES it, and a manifest in the
          # operator's GitOps repository DEPLOYS it. A missing `apps` entry never
          # implied any of that.
          cairn-ui = mkGoUI pkgs;
        }
        // nixpkgs.lib.optionalAttrs (builtins.elem pkgs.stdenv.hostPlatform.system linuxSystems) {
          server-image = mkServerImage pkgs;
          # 🔴 THE PYTHON POD KEEPS THE UNSUFFIXED NAME, and that is not inertia:
          # `.github/workflows/publish-image.yml` builds `packages.server-image` by
          # name, `server/README.md` documents it, and consumers pin it. Renaming it to
          # make room for this one would change what gets PUBLISHED, which is a deploy
          # decision and not a build. `server-image-go` is published under its OWN
          # package name (`cairn-store-go`), which is what keeps that true.
          server-image-go = mkGoServerImage pkgs;

          # 🔴 `ui-image`, NOT `ui-image-go`. The `-go` suffix on the pod distinguishes
          # it from a PYTHON sibling that exists and is still published; this surface has
          # no second implementation and never had one, so a suffix would imply a
          # counterpart a reader would then go looking for.
          #
          # ⚠ IT IS PUBLISHED, AND THE CLAIM THAT IT WAS NOT IS RETRACTED — the wording
          # here has now been wrong in BOTH directions, which is why the record is kept.
          # It first read "It publishes to its own ghcr package", present tense, for a leg
          # the same change deliberately excluded. It was then corrected to "NOTHING
          # PUBLISHES IT YET — `publish-image.yml` pushes `server-image` and
          # `server-image-go` and no third leg exists", which is the sentence that went
          # stale: that workflow now builds this derivation, pushes it under the OCI name
          # below, and PROVES the result pullable with no credentials. So the name below
          # is a package anybody can pull.
          ui-image = mkGoUIImage pkgs;
        });

      # 🔴 `apps.default` MOVES WITH `packages.default` OR NOT AT ALL — AND TODAY THAT
      # MEANS BOTH HAVE MOVED, TO THE GO CLIENT. `nix run github:…/cairn` resolves
      # `apps.default` FIRST and only falls back to `packages.default`'s `mainProgram`,
      # so flipping one alone would leave `nix run` on one client while
      # `nix profile install` and every flake input got the other — one name, two
      # clients, differing by which command the consumer happened to use.
      #
      # 🔴 `apps.cairn` DELIBERATELY DID NOT MOVE, AND IT IS PINNED RATHER THAN LEFT TO
      # PROSE. It is the escape hatch the announcement names: `nix run
      # github:…/cairn#cairn` is still the Python client, which is what makes residual
      # 7's widening opt-out-able rather than forced. Because `README.md` now PROMISES
      # that, repointing this one line at `mkGoClient` would defeat the promise while
      # `packages.default`/`apps.default`/`packages.cairn` all stayed exactly right —
      # the half-flip class one attribute over. `default-is-the-go-client` asserts it.
      #
      # ⚠ THERE IS NO `apps.cairn-go`, DELIBERATELY. `nix run .#cairn-go` already
      # resolves through `packages.cairn-go`'s `mainProgram = "cairn"` — measured, not
      # assumed — so an entry here would be a third name for one binary with nothing to
      # buy, and `apps.default` above is now a fourth.
      apps = forAll (pkgs: {
        cairn = {
          type = "app";
          program = "${nixpkgs.lib.getExe (mkCairn pkgs)}";
        };
        default = {
          type = "app";
          program = "${nixpkgs.lib.getExe (mkGoClient pkgs)}";
        };
      });

      checks = forAll (pkgs: {
        cairn = mkCairn pkgs;
        cairn-server-go = mkGoServer pkgs;
        cairn-go = mkGoClient pkgs;
        cairn-ui = mkGoUI pkgs;

        # 🔴 WHICH CLIENT THE UNQUALIFIED NAMES RESOLVE TO, PINNED AS A RELATIONSHIP
        # BETWEEN RESOLVED DERIVATIONS. Before this check, NOTHING in the repository
        # asserted it: `packages.default`/`apps.default` appeared in `flake.nix` and in no
        # test, the two other `checks.*` entries name `mkCairn`/`mkGoClient` explicitly,
        # every CI job builds by explicit attribute, and the parity harness builds with
        # `go build ./cmd/cairn` and runs the oracle script directly. The wiring was pinned
        # by PROSE, and prose is walkable three ways:
        #
        #   • a flip taken on the wrong licence, REVERTED once already, could land fully
        #     green — nothing here would have noticed either the flip or the revert;
        #   • a HALF-FLIP — `packages.default` moved, `apps.default` not, or the reverse —
        #     is also fully green, and it is the dangerous half: `nix run github:…/cairn`
        #     resolves `apps.default` FIRST and only falls back to `packages.default`'s
        #     `mainProgram`, so the run path and the build path would serve two different
        #     clients under ONE name, differing by which command the consumer used;
        #   • a later accidental revert of either line would be invisible;
        #   • and the SAME half-flip one attribute over: repointing `apps.cairn` at the Go
        #     client leaves all three of the assertions above green — `packages.cairn` is
        #     untouched by it — while the ESCAPE HATCH `README.md` promises silently
        #     becomes the Go client. That is why `apps.cairn` is compared here too.
        #
        # 🔴 IT COMPARES RESOLVED DERIVATIONS, NOT SPELLINGS. A guard matching the string
        # `mkGoClient` in this file is walkable by renaming the binding while the wiring
        # stays wrong, and is red on a rename that changed nothing. `apps.default` is an
        # APP rather than a package, so its `program` is resolved to the executable it
        # actually points at and compared against the DEFAULT PACKAGE's `mainProgram`
        # executable — two things of the same kind.
        #
        # ⚠ IT IS DELIBERATELY BUILD-FREE: the paths are taken at EVALUATION time with the
        # string context discarded, so this derivation depends on no client and cannot be
        # red for a reason that is not a wiring difference. A kill for the wrong reason —
        # the Go compiler failing, say — would read here as "the default moved", which is
        # the one thing this check must never say by accident.
        #
        # 🔴 AND THE TWO `getExe`-DERIVED OPERANDS OF THE `apps.default` EQUALITY ARE THE
        # SAME EXPRESSION, SO THEY MOVE TOGETHER — a COMMON-MODE blind spot rather than a
        # comparison. Measured: `lib.getExe` on a derivation whose `meta.mainProgram` has
        # been removed does not error — it emits a DEPRECATION WARNING and falls back to
        # the pname, returning `…/bin/cairn-go`. Both sides of that equality then hold the
        # same wrong path and compare equal while `nix run github:ZacxDev/cairn` fails on
        # a missing binary. Nothing else covers it: `go-client-declares-its-verbs` puts
        # the package on `PATH` and calls `cairn`, which proves `bin/cairn` EXISTS and
        # says nothing about `mainProgram`, and no CI job runs `.#` bare. The NAME check
        # below is the second, independent reading.
        #
        # ⚠ WHAT IT DOES NOT CLAIM: nothing about either client's BEHAVIOUR; and the name
        # check is insensitive on the PYTHON side, where `pname` is already `cairn`, so a
        # `mainProgram` removed from `mkCairn` would leave it green. That is a real gap,
        # named rather than papered over — the hazard it exists for is `mkGoClient`, whose
        # pname and mainProgram DIFFER.
        default-is-the-go-client =
          let
            system = pkgs.stdenv.hostPlatform.system;
            # `builtins.unsafeDiscardStringContext` is what makes this a wiring assertion
            # rather than a build: the store path is known without realising anything.
            path = drv: builtins.unsafeDiscardStringContext drv.outPath;
            exe = drv: builtins.unsafeDiscardStringContext (nixpkgs.lib.getExe drv);
            defaultPkg = path self.packages.${system}.default;
            goPkg = path self.packages.${system}.cairn-go;
            pyPkg = path self.packages.${system}.cairn;
            defaultExe = exe self.packages.${system}.default;
            pyExe = exe self.packages.${system}.cairn;
            appProgram =
              builtins.unsafeDiscardStringContext self.apps.${system}.default.program;
            pyAppProgram =
              builtins.unsafeDiscardStringContext self.apps.${system}.cairn.program;
            # Both clients declare `mainProgram = "cairn"`, which is the single name every
            # documented invocation uses. `mkGoClient`'s pname is `cairn-go`, so this is
            # the one operand where the fallback is DISTINGUISHABLE from the intent.
            wantExeName = "cairn";
          in
          pkgs.runCommand "cairn-default-is-the-go-client" { } ''
            failed=0

            # ⚠ AN INVARIANT GUARD, AND LABELLED AS ONE BECAUSE THE HAZARD ITS FIRST
            # COMMENT NAMED IS UNREACHABLE. That comment read as regression coverage of a
            # missing flake attribute resolving to `""`. Measured: a missing attribute is
            # an EVAL ERROR (`error: attribute 'nosuchattr' missing`), so this derivation
            # is never built and no run of it can observe that case. What it DOES pin is
            # the floor every equality below rests on — two empty strings compare equal,
            # so the comparisons are only as good as their operands being store paths.
            # Keep it, and do not count it as coverage of a defect anyone has observed.
            for p in '${defaultPkg}' '${goPkg}' '${pyPkg}' '${defaultExe}' '${pyExe}' \
                     '${appProgram}' '${pyAppProgram}'; do
              case "$p" in
                /nix/store/?*) ;;
                *)
                  echo "FAIL: '$p' is not a store path — an attribute resolved to nothing,"
                  echo "      and every comparison below would then be an equality between"
                  echo "      two empty strings."
                  failed=1
                  ;;
              esac
            done

            if [ "${defaultPkg}" != "${goPkg}" ]; then
              echo "FAIL: packages.default is NOT packages.cairn-go."
              echo "      packages.default  = ${defaultPkg}"
              echo "      packages.cairn-go = ${goPkg}"
              echo "      The default client is whatever a consumer gets from"
              echo "      \`nix profile install github:…/cairn\` and from a flake input that"
              echo "      names no attribute. Moving it is a cutover decision, not a build."
              failed=1
            fi

            if [ "${appProgram}" != "${defaultExe}" ]; then
              echo "FAIL: apps.default and packages.default resolve to DIFFERENT executables."
              echo "      apps.default.program        = ${appProgram}"
              echo "      getExe packages.default     = ${defaultExe}"
              echo "      \`nix run github:…/cairn\` resolves apps.default FIRST and only falls"
              echo "      back to packages.default's mainProgram, so this state gives ONE name"
              echo "      two clients — which one you get depends on the command you ran."
              failed=1
            fi

            if [ "${pyPkg}" = "${defaultPkg}" ]; then
              echo "FAIL: packages.cairn and packages.default are the SAME derivation."
              echo "      packages.cairn = packages.default = ${pyPkg}"
              echo "      \`#cairn\` is the opt-out README.md announces, and this state points"
              echo "      it at the Go client. It would NOT redden the parity gate —"
              echo "      tests/parity/harness.py runs the oracle SCRIPT directly and never"
              echo "      builds this package — which is exactly why it is asserted here."
              failed=1
            fi

            # 🔴 THE ESCAPE HATCH'S `nix run` SPELLING, WHICH THE THREE ABOVE LEAVE FREE.
            # `nix run github:…/cairn#cairn` resolves `apps.cairn` FIRST, the same
            # precedence that makes `apps.default` load-bearing. README.md now PROMISES
            # this spelling opts out, and a flake input taking `apps.${system}.cairn` gets
            # the same attribute, so a repoint here is a silent flip of the documented
            # escape hatch with every other assertion in this file still green.
            if [ "${pyAppProgram}" != "${pyExe}" ]; then
              echo "FAIL: apps.cairn does NOT point at packages.cairn — the announced opt-out"
              echo "      resolves to something else."
              echo "      apps.cairn.program      = ${pyAppProgram}"
              echo "      getExe packages.cairn   = ${pyExe}"
              echo "      README.md names \`#cairn\` as the opt-out from the default flip, in"
              echo "      both the CLI and the flake-input spelling. If this pair disagrees,"
              echo "      the opt-out is a promise the tree does not keep."
              failed=1
            fi

            # 🔴 THE NAME, NOT ANOTHER EQUALITY — the common-mode reading. See the block
            # above: a removed `meta.mainProgram` moves BOTH sides of an equality at once,
            # so the equalities cannot see it. `getExe` falls back to the pname with only a
            # deprecation warning, which turns the Go client's executable into `cairn-go`
            # and breaks every documented invocation while the comparisons stay green.
            for spec in \
              'getExe packages.default|${builtins.baseNameOf defaultExe}' \
              'apps.default.program|${builtins.baseNameOf appProgram}' \
              'getExe packages.cairn|${builtins.baseNameOf pyExe}' \
              'apps.cairn.program|${builtins.baseNameOf pyAppProgram}'; do
              label="''${spec%%|*}"
              name="''${spec##*|}"
              if [ "$name" != '${wantExeName}' ]; then
                echo "FAIL: $label resolves to an executable named '$name', not"
                echo "      '${wantExeName}'. meta.mainProgram is missing or wrong, and getExe"
                echo "      fell back to the pname with a deprecation warning rather than an"
                echo "      error. Every equality in this check still passes — both operands"
                echo "      move together — while \`nix run\` serves a binary under a name"
                echo "      nothing documented here uses."
                failed=1
              fi
            done

            [ "$failed" -eq 0 ] || exit 1

            echo "ok: packages.default == packages.cairn-go == ${defaultPkg}"
            echo "    apps.default.program == ${appProgram}"
            echo "    packages.cairn is distinct == ${pyPkg}"
            echo "    apps.cairn.program == getExe packages.cairn == ${pyAppProgram}"
            echo "    all four resolved programs are named '${wantExeName}'"
            {
              echo "packages.default ${defaultPkg}"
              echo "packages.cairn-go ${goPkg}"
              echo "packages.cairn ${pyPkg}"
              echo "apps.default.program ${appProgram}"
              echo "apps.cairn.program ${pyAppProgram}"
              echo "exe-name ${wantExeName}"
            } > $out
          '';

        # 🔴 THE GO CLIENT'S OWN LEDGER, READ OUT OF THE RUNNING BINARY — the one claim a
        # compile cannot make, and the one the PYTHON-side gates are structurally blind to.
        # `capability_ledger.cli_verbs_from_parser` asks the PYTHON argparse parser what
        # subcommands it has and has no equivalent for a compiled program, so a Go client
        # that GAINED a verb or silently LOST one leaves that gate green.
        #
        # 🔴 AND IT IS A PAIR, NOT A ZERO. A check that asserted only "the command exited 0"
        # would pass for a binary that printed nothing. The exact set is pinned by hand here
        # — the same verbs `tests/test_capability_ledger.py` discovers from the Python parser,
        # which is what makes the two implementations comparable at all. 🔴 THE SET IS THE
        # HEREDOC BELOW AND THE COUNT IS NOT WRITTEN DOWN ANYWHERE IN THIS COMMENT: a number
        # here would be a hand list one word long, which is the thing the heredoc replaced.
        #
        # ⚠ WHAT THIS SANDBOX CANNOT HAVE: no store, no token, no network and no HOME with a
        # cache root, so it exercises the LEDGERS and nothing about reading or writing. The
        # parity harness is what measures behaviour, and it needs a running pod that a nix
        # sandbox is the wrong place for.
        go-client-declares-its-verbs = pkgs.runCommand "cairn-go-client-declares-its-verbs"
          { nativeBuildInputs = [ (mkGoClient pkgs) ]; } ''
          set -o pipefail
          cairn -verbs > verbs.txt
          cairn -exit-codes > codes.txt

          if ! grep -q . verbs.txt || ! grep -q . codes.txt; then
            echo "FAIL: the binary printed NO verb or NO exit code, so a ledger built from"
            echo "      this output would agree with anything."
            exit 1
          fi

          cat > want-verbs.txt <<'EOF'
          append writes
          create writes
          doctor reads
          ls-entries reads
          put writes
          recall reads
          routes reads
          search reads
          sync reads
          validate reads
          EOF
          sed -i 's/^ *//' want-verbs.txt

          if ! diff -u want-verbs.txt verbs.txt; then
            echo "FAIL: the Go client's declared verb set is not the set this check names."
            echo "      A verb is a CAPABILITY — the ledger in tests/testlib/capability_ledger.py"
            echo "      has a row per capability and asserts it against the HTTP route table."
            echo "      Adding or removing one here means updating that ledger in the same change."
            # 🔴 SINGLE-QUOTED, BECAUSE BACKTICKS IN A DOUBLE-QUOTED `echo` ARE A COMMAND
            # SUBSTITUTION. Measured while running this check's own negative control: the
            # failure message printed `writes: command not found` and lost the word it was
            # about — a refusal that mangles its own explanation, on the one path nobody reads
            # until something is already broken.
            echo '      The `writes` flag is not decoration either: it decides whether an'
            echo "      unreachable store exits 7 (the record was NOT made) or 3 (nothing was"
            echo "      displayed)."
            exit 1
          fi

          # 🔴 THE SHARED EXIT-CODE SET, COMPUTED FROM THE BINARY'S OWN TWO TABLES. The Python
          # ledger computes its intersection over the PYTHON client's nine codes, so a Go-only
          # code colliding with `doctor`'s 10 leaves it green. `{0, 9}` is the documented set
          # and this is where the Go side's version of it is checked.
          grep '^client ' codes.txt | awk '{print $3}' | sort -u > client-values.txt
          grep '^doctor ' codes.txt | awk '{print $3}' | sort -u > doctor-values.txt
          comm -12 client-values.txt doctor-values.txt > shared.txt
          printf '0\n9\n' > want-shared.txt
          if ! diff -u want-shared.txt shared.txt; then
            echo "FAIL: the Go client's exit codes share $(wc -l < shared.txt) value(s) with"
            echo "      its doctor's, not exactly {0, 9}. GROWN means a new overlap nobody"
            echo "      documented; SHRUNK means the 9 was renumbered and the comment that"
            echo "      states the overlap is now false."
            exit 1
          fi

          echo "ok: $(wc -l < verbs.txt) declared verbs, $(wc -l < codes.txt) exit codes,"
          echo "    shared set exactly {0, 9}"
          cat verbs.txt codes.txt > $out
        '';

        # 🔴 THE POSITIVE HALF FOR THE GO SERVER, AND IT IS A DIFFERENT CLAIM
        # FROM "IT COMPILES". The package's own `checkPhase` runs the unit
        # tests; this runs the BINARY and reads what it prints, which is the one
        # thing a compile cannot establish — that the dispatch tables were
        # actually wired and agree with the ledger the conformance suite reads.
        #
        # 🔴 AND IT IS A PAIR, NOT A ZERO. A check that asserted only "the
        # command exited 0" would pass for a binary that printed nothing, which
        # is the reassuring zero this repository keeps finding. So the exact
        # route set is pinned, spelled out by hand — the same set
        # `tests/test_conformance_suite.py` spells for the oracle, which is what
        # makes the two implementations comparable at all.
        #
        # ⚠ WHAT THIS SANDBOX CANNOT HAVE, stated rather than assumed away: no
        # store, no token file and no network, so it exercises the LEDGER and
        # nothing about serving. The conformance corpus is what measures the
        # served behaviour, and it needs a running pair of servers that a nix
        # sandbox is the wrong place for.
        go-server-declares-its-routes = pkgs.runCommand "cairn-go-server-declares-its-routes"
          { nativeBuildInputs = [ (mkGoServer pkgs) ]; } ''
          set -o pipefail
          cairn-server -routes > routes.txt

          if ! grep -q . routes.txt; then
            echo "FAIL: the binary printed NO route at all, so a ledger built from"
            echo "      this output would agree with anything."
            exit 1
          fi

          cat > want.txt <<'EOF'
          GET recall
          GET search
          GET snapshot
          HEAD recall
          HEAD search
          HEAD snapshot
          POST entry
          PUT entry
          EOF
          sed -i 's/^ *//' want.txt

          if ! diff -u want.txt routes.txt; then
            echo "FAIL: the Go server's declared route set is not the set this check"
            echo "      names. Adding a row to a dispatch table is adding a public,"
            echo "      internet-reachable endpoint, and this is where somebody has"
            echo "      to think about it — update the conformance request list and"
            echo "      regenerate in the same change."
            exit 1
          fi

          echo "ok: $(wc -l < routes.txt) declared routes, matching the ledger"
          cp routes.txt $out
        '';

        # 🔴 THE UI IMAGE'S SESSION DIRECTORY, READ OUT OF THE BUILT LAYERS — AND THIS
        # IS A `checks.*` ENTRY RATHER THAN A PYTEST ASSERTION FOR ONE MEASURED REASON.
        # `tests/test_flake_ui_image_runtime_contract.py` carries the same assertion, and
        # in CI it can only ever SKIP: the `tests` job installs Python and pytest and has
        # NO nix, so that test's own `nix build` fails and it skips — and a skip nobody
        # counts is a pass. This runs in the `nix` job, where a built image exists by
        # construction.
        #
        # 🔴 IT IS THE ASSERTION THE CONFIG BLOCK CANNOT MAKE. Everything else about this
        # image — uid, port, env, entrypoint — is a metadata field whose only statement is
        # `flake.nix` itself, so reading the text proves as much as reading the artefact.
        # Ownership of a DIRECTORY is not: it is produced by `fakeRootCommands` under
        # `enableFakechroot`, and the sibling pod's comment records its own `chown` mutant
        # SURVIVING three smoke runs because none of them read ownership. This reads it.
        #
        # ⚠ OPERATIONALLY: `identity.OpenFileSessionStore` refuses to start (78) when it
        # cannot write the session table, so a root-owned directory here does not degrade
        # the surface — it produces a pod that never comes up. Loud, but only if somebody
        # notices before the deploy.
        ui-image-owns-its-session-dir =
          pkgs.runCommand "cairn-ui-image-owns-its-session-dir"
            # 🔴 `(python pkgs)`, NEVER `pkgs.python3`. The bare attribute follows nixpkgs
            # — measured 3.14.7 against this lock, where `python` is 3.12.14 and CI pins
            # 3.12 — so a check written with it would take its verdict from an interpreter
            # nothing else in this repository runs. `AGENTS.md` records that exact incident
            # ("A bare `pkgs.python3` followed nixpkgs to 3.14 and shipped an interpreter
            # NOTHING in this repo had ever run the suite under"), and this line was
            # written with it anyway, ~1100 lines below the comment recording it. An audit
            # caught it before merge.
            { nativeBuildInputs = [ (python pkgs) ]; } ''
            set -o pipefail
            python3 "${./tests/ui_image_session_dir_check.py}" \
              "${mkGoUIImage pkgs}" "${uiSessionDir}" ${toString serverUid} | tee $out
          '';

        # ⚠ THERE IS NO `go-ui-declares-its-routes` HERE, AND ITS ABSENCE IS A
        # DECISION RATHER THAN A GAP — a draft of this file carried one, modelled on
        # `go-server-declares-its-routes` above. That check earns its place because
        # the pod's route set is the SERVED CONTRACT `tests/conformance/requests.json`
        # replays and the corpus builder is Python, blind to a compiled binary: the
        # ledger read out of the running program is a genuinely second instrument in
        # a second environment. The UI has neither — `ui.DeclaredRoutes()` derives
        # from the same map its dispatcher reads, and the only expectation anywhere
        # is a hand-written list. A check that ran the binary to print that list and
        # diffed it against a third hand-written copy would restate one claim down a
        # longer path — and that reasoning does not change with the row COUNT, which
        # is why this comment no longer quotes one. `internal/ui`'s
        # `TestTheRouteLedgerMatchesTheDispatchTable` is the one mechanism kept, and
        # it runs inside `mkGoUI`'s own check phase, so `nix build .#cairn-ui` gates
        # it. When a later phase gives this surface an external corpus, the second
        # tier comes back with it.

        # 🔴 THIS CHECK IS THE POSITIVE HALF, AND IT IS *NOT* THE DETECTOR FOR A
        # MISSING `lib/` — `doInstallCheck` above is, and it fires first.
        #
        # An earlier version of this comment claimed the opposite: that dropping
        # `lib/` would leave `cairn` with a working `--help` because "argparse is
        # built before any subcommand imports anything". MEASURED FALSE — the
        # imports are at MODULE SCOPE (`cairn:91`), so `--help` dies with
        # `ModuleNotFoundError: timeouts` during the package's own install check,
        # and this derivation never gets built. The sentence is deleted rather
        # than reworded because a maintainer who believed it would conclude
        # `doInstallCheck` proves nothing and delete the only build-time gate
        # standing between a broken client and a consumer's `home-manager switch`.
        #
        # What this check adds is DEPTH THE INSTALL CHECK DOES NOT REACH: it runs
        # a real subcommand to completion against a real cache root, driving
        # `cairn_doctor`, `subsystem_read_store` and `entry_shape` through their
        # logic rather than merely importing them.
        #
        # Its EXIT CODE is deliberately not asserted. `doctor` reports on an
        # operator's configuration, and in a build sandbox there is no store, no
        # token and no pod — a non-zero verdict is the CORRECT answer here, and
        # demanding zero would either pin a wrong expectation or push the check
        # into faking an environment. What is asserted is the thing that
        # distinguishes a working package from a broken one: it ran far enough
        # to produce its own report rather than dying in an import.
        client-resolves-its-lib = pkgs.runCommand "cairn-client-resolves-its-lib"
          { nativeBuildInputs = [ (mkCairn pkgs) ]; } ''
          set -o pipefail
          # HOME is unset in the sandbox and `Path.home()` is evaluated at
          # import time by `subsystem_read_store` — the same reason the image
          # sets it. Without this the check would fail for a reason that has
          # nothing to do with what it is testing.
          export HOME=$TMPDIR

          if ! cairn doctor > out.txt 2> err.txt; then
            echo "note: doctor exited non-zero, which is expected with no store configured"
          fi

          # 🔴 NEGATIVE-CONTROL-SHAPED ASSERTION: an ImportError is exactly what
          # a lost `lib/` produces, and it is what this check exists to catch.
          # Asserting its ABSENCE is only meaningful alongside the positive
          # assertion below — "no error appeared" is also what a command that
          # never ran produces.
          if grep -qE 'ModuleNotFoundError|ImportError' err.txt; then
            echo "FAIL: the packaged client could not import its own lib/:"
            cat err.txt
            exit 1
          fi

          # Positive: the report was actually produced. `doctor` prints one
          # check line per subject, so a run that imported and executed says
          # something; a run that did nothing says nothing, and that is the
          # zero this pair exists to tell apart.
          if ! grep -q . out.txt; then
            echo "FAIL: doctor produced NO output — it cannot have run."
            echo "--- stderr ---"; cat err.txt
            exit 1
          fi

          echo "ok: packaged client imported its own lib/ and produced a report"
          cp out.txt $out
        '';
      });

      devShells = forAll (pkgs: {
        default = pkgs.mkShell {
          # 🔴 THE SAME PIN AS THE ARTEFACTS, AND THIS WAS THE FOURTH THING.
          # `pkgs.python3` here resolved to 3.14 while the package shebang, the
          # image `Cmd`, `server/Dockerfile` and the CI matrix were all 3.12 —
          # so the shell `shellHook` tells you to run the suite in was on a
          # DIFFERENT interpreter from the one shipped, and a lock bump would
          # move it again silently. That is precisely what pinning the other
          # three was supposed to close.
          packages = [
            ((python pkgs).withPackages (ps: [ ps.pytest ]))
            pkgs.git
            # The same toolchain the package and the CI job use — see
            # `buildGoPinned` above for why it is pinned rather than inherited.
            pkgs.go_1_25
          ];
          shellHook = ''
            echo "cairn dev shell — run the suite with:"
            echo "    python3 -m pytest tests -q -p no:randomly"
            echo "the Go port's own tests:"
            echo "    go test ./... && go vet ./..."
            echo "the conformance corpus against the Go server (the P1 gate):"
            echo "    tests/conformance/run_go.sh"
            echo "and the leak gate, which must pass before any push:"
            echo "    python3 tests/leakscan.py --self-test && python3 tests/leakscan.py"
          '';
        };
      });
    };
}
