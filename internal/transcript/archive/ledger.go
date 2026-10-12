package archive

import (
	"bytes"

	"github.com/ZacxDev/cairn/internal/transcript/scopeuse"
)

// LedgerStream is the stream a capture agent ships a session's CLIENT READ LEDGER as
// (`claudedocs/plan-cairn-plugins.md`, decision 3a; slice S11): the JSON lines both `cairn` clients
// append to `$XDG_STATE_HOME/cairn/read-ledger/<session>.jsonl` — for the root session and, for an
// opencode root, every child session it recorded (decision 7) — one stored record per line.
const LedgerStream = "ledger"

// foldLedger folds one accepted ledger request into the session's grow-only `L(s)`
// ([Meta.Ledger]) and its record count ([Meta.LedgerRecords]).
//
// 🔴 THE POD READS THE LEDGER ITSELF, WITH THE AGENT'S OWN PARSER (`scopeuse.(*Deriver).Ledger`, the
// one the agent routes with — decision 3's "one function, two callers"), so an agent that uploads a
// ledger and DECLARES less than it names cannot shrink `V`. A record that does not parse, or names
// something that is not a scope, contributes `*` — fail closed, owner-only.
//
// ⚠ IT ONLY ADDS. The ledger is SELF-REPORTED by the host (threat T3): a host that uploads none, or
// a short one, makes `V` smaller and nothing here can tell; F1 covers only an EMPTY ledger.
func foldLedger(m *Meta, lines [][]byte) {
	d := scopeuse.NewDeriver()
	d.Ledger(bytes.Join(lines, []byte("\n")))
	m.Ledger = union(m.Ledger, d.Result().Ledger)
	m.LedgerRecords += len(lines)
}
