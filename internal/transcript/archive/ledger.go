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

// LedgerScopes is the ledger's half of `V`: `L(s)`, plus `*` when F1 fires — some stored tool input
// names the program and the session's uploaded ledger holds ZERO records (decision 3: "it asks only
// is the ledger empty").
//
// ⚠ F1's `*` IS THE ONE PART OF `V` THAT CAN GO AWAY, AND THAT IS THE DECISION, NOT A LEAK: it stands
// for "the program ran and nothing says what it read", and a ledger record is what says it. A host
// that forges one record defeats it — the ledger is self-reported (threat T3), like the trailer.
//
// ⚠ ONLY THE POD'S OWN F1 GOES AWAY. The agent declares its own `L ∪ R_header ∪ F`, and a `*` it
// declared while ITS ledger was empty is in the grow-only [Meta.Declared] for good.
func (m *Meta) LedgerScopes() []string {
	if m.NamesProgram && m.LedgerRecords == 0 {
		return union(m.Ledger, []string{Star})
	}
	return m.Ledger
}
