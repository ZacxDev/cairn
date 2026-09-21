// 🔴 THIS FILE USED TO HAVE NO `require` BLOCK, AND THAT WAS THE GUARANTEE. The
// sentence that stood here said `go.sum` staying absent "makes 'no third-party code
// in the serving path' checkable in one glance instead of by reading a lock file".
// That is no longer true of this file, and it is written out rather than deleted
// because a maintainer who remembers the old property has to be told where it went
// instead of inferring that nothing replaced it.
//
// `internal/ui` renders HTML with `maragu.dev/gomponents`. There is ONE module, so
// the requirement below is carried by the pod's and the CLI's derivations as well as
// the UI's, and `flake.nix` passes a real `vendorHash` to all three where it used to
// pass `null`. That loss was an operator decision, taken explicitly.
//
// 🔴 THE REFUSAL NOW LIVES IN `internal/depspolicy`, AND IT IS TWO CLAIMS BECAUSE
// ONE CANNOT DO THE JOB OF THE OTHER:
//
//   - `TestTheModuleSetIsExactlyTheAllowlist` compares the module set this file and
//     `go.sum` declare against `depspolicy.DeclaredModules`, and fails when it GROWS
//     *or* SHRINKS, with a separate message per direction. **A module added here
//     without a line there is a RED test, not a review comment.**
//   - `TestNoPackageTheCLIOrThePodLINKSReachesAThirdPartyModule` walks the import
//     graph out of `cmd/cairn` and `cmd/cairn-server` and refuses a third-party
//     import anywhere in either closure. THAT is the one that keeps the pod clean:
//     the allowlist alone is satisfied by a tree where `internal/api` imports the
//     HTML library on every route.
//
// `go mod verify` in the `go` CI job is the third part, over a different question
// again — see that step's own comment, which records what it does and does NOT catch.
//
// 🔴 THE CONSTRAINT ITSELF HAS NOT MOVED, ONLY WHAT ENFORCES IT. `server/server.py`
// is stdlib Python because it runs in a pod built from a base image nobody audits
// per-dependency, and the reader it shares with the client must keep its no-network,
// no-subprocess property. Every dependency added here is a dependency a binary in
// this repository carries; the import ban is what decides WHICH binary.
//
// The Go version is pinned to the toolchain CI installs. It is deliberately NOT
// the newest available: `flake.nix` pins `python312` because the Dockerfile and
// the CI matrix pin 3.12, and the same discipline applies here — move the
// toolchain in the CI workflow, the flake and this file together or not at all.
module github.com/ZacxDev/cairn

go 1.25

require maragu.dev/gomponents v1.3.0
