// Command cairn is the Go client — P2 of the control-plane plan.
//
// 🔴 IT EXISTS TO DELETE THE SECOND RENDERER. The pod (P1) and this CLI now run ONE renderer,
// `internal/report`, so byte-identity between pod output and local output is a property of there
// being one implementation rather than a discipline two implementations are held to.
//
// ⚠ THIS BINARY IS NOW `packages.default`/`apps.default`, AND THE PYTHON CLIENT IS STILL
// SHIPPED AND STILL THE ORACLE. The cutover moved which client a consumer gets by default; it
// deleted nothing. `packages.cairn` remains the Python client, `tests/parity/harness.py` still
// runs both against ONE cache root and diffs stdout, stderr and exit code per verb and flag
// combination, and the plan puts the deletion of Python at P8.
//
// 🔴 THE GREEN GATE IS NOT WHAT LICENSED THE FLIP, AND SAYING SO IS THE POINT: a branch took it
// on that reading and was REVERTED. It was taken on an operator decision, after residual 8's
// closure removed its one MEASURED blocker — "every READ verb here refuses at exit 11 on a host
// with more than one instance configured", which made `nix run github:…/cairn -- doctor`, the
// quickstart, refuse on such a host. The read verbs route now and that row is deleted; that is
// a PRECONDITION, not a licence, and the gate being green never was one.
//
// ⚠ WHAT THE FLIP CARRIED, AND NOW HAS DELIVERED: residual 7's CLI-contract widening. `-verbs`
// and `-exit-codes` exit 0 with a table here where the oracle's argparse exits 2 with `usage:`,
// so a single-dash token `nix run github:…/cairn` refused before the flip answers 0 after it,
// for every consumer who does not name `#cairn`. `README.md` carries the announcement.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ZacxDev/cairn/internal/client"
	"github.com/ZacxDev/cairn/internal/doctor"
)

func main() {
	argv := os.Args[1:]

	// 🔴 TWO LEDGER FLAGS, READ OUT OF THE RUNNING BINARY, BECAUSE THE PYTHON-SIDE DISCOVERY
	// CANNOT SEE A COMPILED PROGRAM. `tests/testlib/capability_ledger.py` asks the PYTHON
	// argparse parser what subcommands it has, and `tests/test_cairn_doctor.py` walks the
	// `cairn` script's AST for its `EXIT_*` constants — neither has an equivalent here, so a
	// Go-only verb, a Go client that silently LOST a verb, or a Go exit code colliding with
	// `doctor`'s 10 would each leave those gates green. These flags are what closes that, the
	// same way `cairn-server -routes` closes the route ledger's blind spot.
	//
	// ⚠ THEY ARE NOT SUBCOMMANDS AND THEY ARE SINGLE-DASH ON PURPOSE. Every real verb is a
	// bare word and every real option is `--`-prefixed, so `-verbs` cannot collide with either,
	// and it does not appear in the verb table it prints — which is what keeps the table a
	// statement about capabilities rather than about this binary's own introspection.
	if len(argv) == 1 {
		switch argv[0] {
		case "-verbs":
			for _, line := range client.DeclaredVerbs() {
				fmt.Println(line)
			}
			return
		case "-exit-codes":
			// `<NAME> <value>` per line, sorted, over BOTH code sets this binary can
			// return. Printing them together is the point: the shared set is the claim,
			// and a ledger that read only one source could not compute it.
			var lines []string
			for name, code := range client.ExitCodes() {
				lines = append(lines, fmt.Sprintf("client %s %d", name, code))
			}
			for name, code := range doctor.ExitCodes() {
				lines = append(lines, fmt.Sprintf("doctor %s %d", name, code))
			}
			sort.Strings(lines)
			fmt.Println(strings.Join(lines, "\n"))
			return
		}
	}

	env := client.Env{Stdout: os.Stdout, Stderr: os.Stderr}
	os.Exit(client.Run(env, argv))
}
