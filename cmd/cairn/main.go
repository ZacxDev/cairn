// Command cairn is the Go client — P2 of the control-plane plan.
//
// 🔴 IT EXISTS TO DELETE THE SECOND RENDERER. The pod (P1) and this CLI now run ONE renderer,
// `internal/report`, so byte-identity between pod output and local output is a property of there
// being one implementation rather than a discipline two implementations are held to.
//
// ⚠ THE PYTHON CLIENT IS STILL SHIPPED AND IS STILL THE ORACLE. `tests/parity/run.sh` runs both
// against ONE cache root and diffs stdout, stderr and exit code per verb and per flag
// combination. Until that gate has held over real use, `packages.cairn` stays the Python client
// and this binary is `packages.cairn-go` beside it — swapping them is P2's LAST step, not its
// first, and the plan puts the deletion of Python at P8.
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
