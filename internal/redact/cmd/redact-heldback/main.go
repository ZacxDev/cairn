// Command redact-heldback scores a held-back case file against the redactor and enforces O15's
// arming gate: exit 0 when at least 90% of leaks are caught AND at most 15% of clean lines are
// damaged, 1 outside either bound, 2 when the instrument cannot vouch. The case format, the oracle
// and the controls are documented on [redact.HeldBackGate] (internal/redact/heldback.go).
//
//	go build -o redact-heldback ./internal/redact/cmd/redact-heldback && ./redact-heldback cases.jsonl
//
// ⚠ Not `go run`: it reports every non-zero exit as 1, which erases the 1-versus-2 distinction.
package main

import (
	"fmt"
	"os"

	"github.com/ZacxDev/cairn/internal/redact"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: redact-heldback <cases.jsonl>")
		os.Exit(redact.HeldBackNoVouch)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stdout, "COULD NOT VOUCH: %v\n", err)
		os.Exit(redact.HeldBackNoVouch)
	}
	os.Exit(redact.HeldBackGate(os.Stdout, f))
}
