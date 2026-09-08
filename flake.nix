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
        { cairn = mkCairn pkgs; default = mkCairn pkgs; }
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
          ];
          shellHook = ''
            echo "cairn dev shell — run the suite with:"
            echo "    python3 -m pytest tests -q -p no:randomly"
            echo "and the leak gate, which must pass before any push:"
            echo "    python3 tests/leakscan.py --self-test && python3 tests/leakscan.py"
          '';
        };
      });
    };
}
