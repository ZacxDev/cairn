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
      # `server/Dockerfile` is what is deployed today; the image below is a
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
      serverEnv = {
        HOME = "/home/nonroot";
        PYTHONDONTWRITEBYTECODE = "1";
        PYTHONUNBUFFERED = "1";
        SUBSYSTEM_STORE_ROOT = "/data";
        SUBSYSTEM_STORE_PORT = "8102";
        SUBSYSTEM_STORE_TOKEN_FILE = "/run/secrets/subsystem-store/token";
      };
      serverUid = 65532;
      serverPort = 8102;

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
            || pkgs.lib.hasPrefix "cmd/" rel || pkgs.lib.hasPrefix "internal/" rel
          ))
          || (rel == "go.mod")
          || (rel == "tests/conformance/requests.json")
          || (rel == "internal/report/testdata/reader_fixtures.json")
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

      # 🔴 NO VENDOR HASH, BECAUSE THERE ARE NO DEPENDENCIES. `null` is
      # `buildGoModule`'s spelling for "this module requires nothing outside the
      # standard library". That is not a convenience: it is the property
      # `go.mod`'s missing `require` block states, and this is the line that makes
      # a new dependency a build FAILURE rather than a silent addition to the
      # serving path.
      mkGoServer = pkgs: (buildGoPinned pkgs) {
        pname = "cairn-server";
        inherit version;
        src = onlyGo pkgs;
        vendorHash = null;

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
        # module lives under `internal/` and `subPackages` had scoped the test
        # walk to the one directory that has none. That is the reassuring zero
        # this repository keeps finding, arriving through a build option whose
        # only documented job is to narrow what gets INSTALLED.
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

      # 🔴 THE GO CLIENT IS A SECOND ARTEFACT DURING P2, NOT A REPLACEMENT. `packages.cairn`
      # stays the Python client and `apps.default` stays pointed at it: swapping them changes
      # what `nix run github:…/cairn` executes for every existing consumer, which is a
      # CUTOVER and not a build. The plan puts the cutover after the parity gate has held
      # over real use and the deletion of Python at P8.
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
        vendorHash = null;

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
          # without a mount, which is what makes a smoke test of it possible.
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
    in
    {
      packages = forAll (pkgs:
        {
          cairn = mkCairn pkgs;
          default = mkCairn pkgs;
          # 🔴 `default` STAYS THE PYTHON CLIENT. The Go server AND the Go client are
          # SECOND artefacts during the dual-run, not replacements for anything: making
          # either the default would change what `nix run github:…/cairn` executes
          # for every existing consumer, which is a cutover and not a build.
          cairn-server-go = mkGoServer pkgs;
          cairn-go = mkGoClient pkgs;
        }
        // nixpkgs.lib.optionalAttrs (builtins.elem pkgs.stdenv.hostPlatform.system linuxSystems) {
          server-image = mkServerImage pkgs;
        });

      apps = forAll (pkgs: {
        cairn = {
          type = "app";
          program = "${nixpkgs.lib.getExe (mkCairn pkgs)}";
        };
        default = {
          type = "app";
          program = "${nixpkgs.lib.getExe (mkCairn pkgs)}";
        };
      });

      checks = forAll (pkgs: {
        cairn = mkCairn pkgs;
        cairn-server-go = mkGoServer pkgs;
        cairn-go = mkGoClient pkgs;

        # 🔴 THE GO CLIENT'S OWN LEDGER, READ OUT OF THE RUNNING BINARY — the one claim a
        # compile cannot make, and the one the PYTHON-side gates are structurally blind to.
        # `capability_ledger.cli_verbs_from_parser` asks the PYTHON argparse parser what
        # subcommands it has and has no equivalent for a compiled program, so a Go client
        # that GAINED a verb or silently LOST one leaves that gate green.
        #
        # 🔴 AND IT IS A PAIR, NOT A ZERO. A check that asserted only "the command exited 0"
        # would pass for a binary that printed nothing. The exact set is pinned by hand here
        # — the same nine verbs `tests/test_capability_ledger.py` discovers from the Python
        # parser, which is what makes the two implementations comparable at all.
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
