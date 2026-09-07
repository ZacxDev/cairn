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
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
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
            --replace-fail '#!/usr/bin/env python3' '#!${pkgs.python3}/bin/python3'

          # `git` is invoked by BARE NAME (`lib/entry_shape.py::_git`) to derive
          # a repo's scope. Prefixed rather than suffixed so the package carries
          # its own answer instead of inheriting whatever the caller's PATH holds.
          makeWrapper $out/libexec/cairn/cairn $out/bin/cairn \
            --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.git ]}

          runHook postInstall
        '';

        # 🔴 AT BUILD TIME, NOT ONLY IN `checks`, AND THAT IS NOT REDUNDANT.
        # A consumer pinning this flake — devrc's `home-manager switch` is the
        # first — builds the PACKAGE and never runs `nix flake check`. A
        # sibling-import break would therefore reach a machine and be found by
        # the operator, at the moment they wanted to read a note. Failing here
        # means such a build cannot produce an artefact at all.
        #
        # `--help` is enough for THIS depth because the imports are at module
        # scope and `build_parser` additionally calls `_doctor_epilog()`
        # eagerly, pulling in `cairn_doctor`. It is NOT enough for the depth
        # `checks.client-resolves-its-lib` covers, which runs a real subcommand
        # to completion — see the note there.
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
          contents = [ tree ];

          # `/data` is where the PVC mounts and `/home/nonroot` is what HOME
          # names; both are created and owned here so the image is runnable
          # without a mount, which is what makes a smoke test of it possible.
          fakeRootCommands = ''
            mkdir -p ./data ./home/nonroot
            chown -R ${toString serverUid}:${toString serverUid} ./data ./home/nonroot
          '';
          enableFakechroot = true;

          config = {
            Cmd = [ "${pkgs.python3}/bin/python3" "/app/server/server.py" ];
            Env = pkgs.lib.mapAttrsToList (k: v: "${k}=${v}") serverEnv;
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

        # 🔴 THIS IS THE CHECK THAT EARNS ITS KEEP, AND IT IS NOT A TAUTOLOGY.
        # The one thing packaging can silently break is the sibling-import
        # mechanism above: drop `lib/` from the install, or put the real script
        # somewhere `lib/` is not, and `cairn` still EXISTS and still has a
        # `--help` — argparse is built before any subcommand imports anything.
        # So `--help` alone is not evidence. `doctor` is used because it drives
        # the imports for real: it reaches `cairn_doctor`, `subsystem_read_store`
        # and `entry_shape`, which is the closure a store-path build would lose.
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
          packages = [
            (pkgs.python3.withPackages (ps: [ ps.pytest ]))
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
