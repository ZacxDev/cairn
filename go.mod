// 🔴 STDLIB ONLY, AND THE ABSENCE OF A `require` BLOCK IS THE POINT.
//
// `server/server.py` is stdlib Python for a stated reason — it runs in a pod
// built from a base image nobody audits per-dependency, and the reader it shares
// with the client must keep its no-network, no-subprocess property. The Go port
// inherits that constraint rather than relaxing it: every dependency added here
// is a dependency the deployed pod carries, and `go.sum` staying absent is what
// makes "no third-party code in the serving path" checkable in one glance
// instead of by reading a lock file.
//
// The Go version is pinned to the toolchain CI installs. It is deliberately NOT
// the newest available: `flake.nix` pins `python312` because the Dockerfile and
// the CI matrix pin 3.12, and the same discipline applies here — move the
// toolchain in the CI workflow, the flake and this file together or not at all.
module github.com/ZacxDev/cairn

go 1.25
