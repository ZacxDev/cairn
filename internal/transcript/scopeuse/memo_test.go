package scopeuse

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/memo"
)

var memoT0 = time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)

func memoOn(id int64, scope, display string, at time.Time, retracted bool) memo.Stored {
	f := memo.Fields{ID: id, Scope: scope, SenderKind: "user", SenderID: "u-1", SenderDisplay: display,
		Subject: "synthetic subject", Body: "synthetic body", CreatedAt: at, ExpiresAt: at.Add(48 * time.Hour)}
	if retracted {
		f.RetractedAt = at.Add(time.Hour)
		f.RetractorKind, f.RetractorID, f.RetractorDisplay = "user", "u-1", display
	}
	return memo.NewStored(f)
}

func vOf(feed func(*Deriver)) []string {
	d := NewDeriver()
	feed(d)
	return d.Result().V
}

// TestAMemoBlockIsARead (#261 round 2): a memo block the memo hook injects names its scope only as
// `· <scope> ·` and `--scope <scope>`, never in a `subsystem-recall:` header, so before this the
// derivation added NOTHING for it and a session shown a gamma-notes memo stayed visible to an
// alpha-only viewer, memo text included. Every block here is rendered by the REAL renderer
// (`internal/memo`), so a change to its format that this parse stops reading fails here rather than
// silently shrinking `V`. Each literal is distinct, so no shape can stand in for another.
func TestAMemoBlockIsARead(t *testing.T) {
	one := []memo.Stored{memoOn(7, "gamma-notes", "someone", memoT0, false)}
	// Five shown on gamma-notes, two older ones on epsilon-notes summarised as "and 2 more on".
	var seven []memo.Stored
	for i := range 5 {
		seven = append(seven, memoOn(int64(10+i), "gamma-notes", "someone", memoT0.Add(time.Duration(10+i)*time.Minute), false))
	}
	seven = append(seven, memoOn(1, "epsilon-notes", "someone", memoT0, false), memoOn(2, "epsilon-notes", "someone", memoT0, false))
	for _, tc := range []struct {
		name  string
		block string
		want  []string
	}{
		{"a preview", memo.RenderPreview(one), []string{"gamma-notes"}},
		{"a full read", memo.RenderFull(one), []string{"gamma-notes"}},
		{"the `and N more on` line", memo.RenderPreview(seven), []string{"epsilon-notes", "gamma-notes"}},
		{"a sender carrying the separator", memo.RenderPreview([]memo.Stored{memoOn(7, "gamma-notes", "a · b · c", memoT0, false)}),
			[]string{"gamma-notes"}},
		{"a retracted memo", memo.RenderPreview([]memo.Stored{memoOn(7, "gamma-notes", "someone", memoT0, true)}),
			[]string{"gamma-notes"}},
		// A block this cannot read a scope out of is still a memo: `*`.
		{"an unreadable block", MemoBlockOpen + " untrusted nonce=0 count=1>>>\n| garbage\n" + MemoBlockClose + " nonce=0>>>\n",
			[]string{Star}},
	} {
		if !strings.Contains(tc.block, MemoBlockOpen) {
			t.Fatalf("%s: the renderer's block does not begin with %q:\n%s", tc.name, MemoBlockOpen, tc.block)
		}
		if got := vOf(func(d *Deriver) { d.Content(tc.block) }); !slices.Equal(got, tc.want) {
			t.Errorf("%s: V = %v, want %v\n%s", tc.name, got, tc.want, tc.block)
		}
	}
	// The hook shape: the block inside a Claude Code attachment record, fed as the decoded record
	// AND as the raw line (where its newlines are the two characters `\n`).
	content, _ := json.Marshal(memo.RenderPreview(one))
	raw := `{"type":"attachment","attachment":{"type":"hook_additional_context","content":[` + string(content) + `]}}`
	var rec map[string]any
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatal(err)
	}
	if got := vOf(func(d *Deriver) { d.ClaudeRecord(rec) }); !slices.Equal(got, []string{"gamma-notes"}) {
		t.Errorf("the hook attachment, decoded: V = %v, want [gamma-notes]", got)
	}
	if got := vOf(func(d *Deriver) { d.Content(raw) }); !slices.Equal(got, []string{"gamma-notes"}) {
		t.Errorf("the hook attachment, raw: V = %v, want [gamma-notes]", got)
	}
	// CONTROL: the same text with no memo block derives nothing, so the results above are the
	// block's doing.
	if got := vOf(func(d *Deriver) { d.Content("| [m-7] from someone · gamma-notes · x · expires y") }); len(got) != 0 {
		t.Errorf("CONTROL: a heading with no block derived %v, want nothing", got)
	}
}

// TestMemoScopesIsLinearOnUnterminatedBlocks: a string of block OPENS with no close is scanned
// once — each block is bounded by the next open before its close is looked for. Growth ratio at
// two sizes, as for headers.
func TestMemoScopesIsLinearOnUnterminatedBlocks(t *testing.T) {
	best := func(size int) time.Duration {
		s := strings.Repeat(MemoBlockOpen, size/len(MemoBlockOpen))
		var min time.Duration
		for run := 0; run < 5; run++ {
			start := time.Now()
			got := MemoScopes(s)
			took := time.Since(start)
			if took > 5*time.Second {
				t.Fatalf("%d bytes of opens took %v", size, took)
			}
			if !slices.Equal(asSet(got), []string{Star}) {
				t.Fatalf("a run of empty blocks answered %v, want only *", asSet(got))
			}
			if run == 0 || took < min {
				min = took
			}
		}
		return min
	}
	// 1 MiB and 8 MiB, not 256 KiB: measured below ~1 MiB this scan is under an allocation/cache
	// knee (64 KiB 0.3 ms, 256 KiB 1.1 ms, 1 MiB 17.9 ms, 2 MiB 30.8 ms, 4 MiB 45.3 ms, 8 MiB 114 ms),
	// and a pair straddling it read 25× for a scan that is linear above it.
	s, l := best(1<<20), best(8<<20)
	ratio := float64(l) / float64(max(s, time.Microsecond))
	t.Logf("1 MiB best %v, 8 MiB best %v, growth %.1f×", s, l, ratio)
	if ratio > 24 {
		t.Fatalf("growth %.1f× for 8× the input (linear ~8×, quadratic ~64×)", ratio)
	}
}
