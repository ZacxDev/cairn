// Command seed is `tests/refaudit/e2e.sh`'s HELPER: it writes the code-sources journal through its ONE
// writer, `codesrc.Journal.Set`, standing in for the browser surface's POST (S4 covers the POST itself).
// A TEST FIXTURE, never shipped: nothing in `flake.nix` builds it and no binary imports it.
//
//	seed <journal> <scope> [<source>…]   declare <scope>'s sources (none: an explicit empty list)
//
// The revision is the scope's current one, read through the same package, so a second declaration of
// one scope supersedes the first rather than being refused as stale.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/ZacxDev/cairn/internal/codesrc"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: seed <journal> <scope> [<source>…]")
		os.Exit(2)
	}
	j := codesrc.Journal{Path: os.Args[1]}
	snap, err := j.Read()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	scope := codesrc.Key(os.Args[2])
	sources := append([]string{}, os.Args[3:]...)
	if _, err := j.Set(scope, sources, snap.RevisionFor(scope), "e2e-admin",
		time.Date(2000, 1, 3, 0, 0, 0, 0, time.UTC), nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
