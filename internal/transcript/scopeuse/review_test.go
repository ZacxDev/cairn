package scopeuse

import (
	"slices"
	"testing"
)

// TestRestoreFoldsPersistedEvidence (F3): a later run feeds only NEW records, so the evidence of
// earlier runs comes back through Restore — the headers it saw and the "names the program" flag
// F1 reads.
func TestRestoreFoldsPersistedEvidence(t *testing.T) {
	d := NewDeriver()
	d.Restore(Evidence{Headers: []string{"beta-notes"}, NamesProgram: true})
	// No ledger: F1 fires on the RESTORED flag; the restored header stays.
	if r := d.Result(); !slices.Equal(r.V, []string{"*", "beta-notes"}) || !r.F1 {
		t.Fatalf("V = %v F1=%v, want [* beta-notes] and F1", r.V, r.F1)
	}
	// Round trip: what Evidence() persists, Restore() reads back.
	d2 := NewDeriver()
	d2.Restore(d.Evidence())
	if !slices.Equal(d2.Result().V, d.Result().V) {
		t.Fatal("Evidence/Restore does not round-trip")
	}
}

// TestAHeaderInsideAJSONStringNamesItsScope (F8): a tool output that is itself JSON carries the
// header's newline as the two characters `\n`; the string is decoded first, so the scope is read
// exactly — not as `*` — and a header in the FIRST of two duplicate members is still seen.
func TestAHeaderInsideAJSONStringNamesItsScope(t *testing.T) {
	d := NewDeriver()
	d.Content(`{"result":"subsystem-recall: status=recalled scope=beta-notes\n  store: /srv\n"}`)
	if r := d.Result(); !slices.Equal(r.V, []string{"beta-notes"}) {
		t.Fatalf("V = %v, want [beta-notes]", r.V)
	}
	d = NewDeriver()
	d.Content(`{"r":"subsystem-recall: status=recalled scope=alpha-notes\nx","r":"ok"}`)
	if r := d.Result(); !slices.Equal(r.V, []string{"alpha-notes"}) {
		t.Fatalf("duplicate member: V = %v, want [alpha-notes]", r.V)
	}
	// Control: undecodable text with the same escape still fails CLOSED.
	d = NewDeriver()
	d.Content(`not json: subsystem-recall: status=recalled scope=beta-notes\n`)
	if r := d.Result(); !slices.Equal(r.V, []string{"*"}) {
		t.Fatalf("raw text: V = %v, want [*]", r.V)
	}
}
