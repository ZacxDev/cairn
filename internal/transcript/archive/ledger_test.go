package archive

import (
	"fmt"
	"slices"
	"testing"
)

// ledgerRec is one client read-ledger line (decision 3a) as the capture agent ships it.
func ledgerRec(n int, scope string) Record {
	return Record{Src: fmt.Sprintf("s-0001@%d", n*100), Rec: []byte(fmt.Sprintf(
		`{"schema":1,"session":"s-0001","verb":"sessions","instance":"personal","scope":%q,`+
			`"client":"go","client_version":"(devel)","at":"2000-01-02T03:04:05Z"}`, scope))}
}

// TestAnUploadedLedgerPutsItsScopesInMeta is the S11 pod fold (mutant
// `transcript-pod-ignores-ledger`): a `ledger` stream naming only `beta-notes`, for a session whose
// transcript carries no header and no trailer and whose upload DECLARES nothing, puts `beta-notes`
// in `meta.json`'s `ledger_scopes` — the pod reads the ledger itself, so the agent's declaration is
// not what carries it.
//
// RED before S11: the stream name `ledger` was refused (400) and `Meta` had no `Ledger`.
func TestAnUploadedLedgerPutsItsScopesInMeta(t *testing.T) {
	g := newRig(t, 1<<30)
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "1"), "main", recs(0, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "l1"), LedgerStream,
		[]Record{ledgerRec(0, "beta-notes")}); err != nil {
		t.Fatalf("the pod refused a ledger stream: %v", err)
	}
	m, err := g.a.ReadMeta("s-0001")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.Ledger, []string{"beta-notes"}) || m.LedgerRecords != 1 || len(m.Declared) != 0 {
		t.Fatalf("meta ledger=%v records=%d declared=%v — the uploaded ledger was not folded", m.Ledger, m.LedgerRecords, m.Declared)
	}
	// The records are STORED, like any other stream's, so the fold can be re-derived from them.
	if got := g.readBack("s-0001", LedgerStream); len(got) != 1 {
		t.Fatalf("the ledger stream stored %d records", len(got))
	}

	// GROW-ONLY, and FAIL CLOSED: a later request with a record naming no scope it can read adds `*`
	// and keeps `beta-notes`.
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "l1", "l2"), LedgerStream,
		[]Record{{Src: "s-0001@100", Rec: []byte(`{"schema":1,"verb":"recall"}`)}, ledgerRec(2, "../escape")}); err != nil {
		t.Fatal(err)
	}
	m, _ = g.a.ReadMeta("s-0001")
	if !slices.Equal(m.Ledger, []string{Star, "beta-notes"}) || m.LedgerRecords != 3 {
		t.Fatalf("meta ledger=%v records=%d, want [* beta-notes] over 3 records", m.Ledger, m.LedgerRecords)
	}
}

// TestALedgerIsOnlyReadFromTheLedgerStream: the same record uploaded on `main` is transcript
// content, not a ledger — it adds nothing to `ledger_scopes` (an INVARIANT guard on the stream
// check: it fails if the fold ran over every stream).
func TestALedgerIsOnlyReadFromTheLedgerStream(t *testing.T) {
	g := newRig(t, 1<<30)
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "1"), "main", []Record{ledgerRec(0, "beta-notes")}); err != nil {
		t.Fatal(err)
	}
	m, _ := g.a.ReadMeta("s-0001")
	if len(m.Ledger) != 0 || m.LedgerRecords != 0 {
		t.Fatalf("a `main` record was folded as a ledger: %v / %d", m.Ledger, m.LedgerRecords)
	}
}
