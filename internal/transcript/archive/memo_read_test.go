package archive

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/memo"
)

// TestThePodCountsAMemoBlockAsARead (#261 round 2), on the POD's side of decision 3: a record
// carrying a gamma-notes memo block the memo hook injected — no header, no command line — derives
// gamma-notes into the stored meta, as a text blob carrying one does. The pod re-derives so an old
// agent cannot shrink `V`, which only holds if it derives the same reads the agent does.
func TestThePodCountsAMemoBlockAsARead(t *testing.T) {
	g := newRig(t, 1<<30)
	at := time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
	block := memo.RenderPreview([]memo.Stored{memo.NewStored(memo.Fields{ID: 7, Scope: "gamma-notes",
		SenderKind: "user", SenderID: "u-1", SenderDisplay: "someone", Subject: "synthetic subject",
		Body: "synthetic body", CreatedAt: at, ExpiresAt: at.Add(48 * time.Hour)})})
	content, _ := json.Marshal(block)
	rec := `{"type":"attachment","attachment":{"type":"hook_additional_context","content":[` + string(content) + `]}}`
	if _, err := g.a.AppendRecords(ownerA, up("s-0001", "", "1"), "main", []Record{{Src: "0", Rec: []byte(rec)}}); err != nil {
		t.Fatal(err)
	}
	if m, _ := g.a.ReadMeta("s-0001"); !slices.Equal(m.Derived, []string{"gamma-notes"}) {
		t.Errorf("a record carrying a gamma-notes memo block derived %v, want [gamma-notes]", m.Derived)
	}
	if _, err := g.a.PutBlob(ownerA, up("s-0002", "", "b1"), "toolu_s0001.txt", []byte(block)); err != nil {
		t.Fatal(err)
	}
	if m, _ := g.a.ReadMeta("s-0002"); !slices.Equal(m.Derived, []string{"gamma-notes"}) {
		t.Errorf("a text blob carrying a gamma-notes memo block derived %v, want [gamma-notes]", m.Derived)
	}
}
