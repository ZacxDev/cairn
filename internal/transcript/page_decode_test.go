package transcript

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// largeStream stores a synthetic main stream of n records, each a tool call or its result carrying a
// ~1 KiB input, and returns its View.
func largeStream(t testing.TB, n int) *View {
	t.Helper()
	a := transcripttest.Open(t)
	pad := strings.Repeat("lorem ipsum ", 90)
	recs := make([]string, 0, n)
	for i := 0; len(recs) < n; i++ {
		recs = append(recs,
			fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_%06d","name":"Bash","input":{"command":"echo %s"}}]}}`, i, pad),
			fmt.Sprintf(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_%06d","content":"%s"}]}}`, i, pad))
	}
	for i := 0; i < n; i += 5000 {
		transcripttest.Append(t, a, transcripttest.Session{Root: "s-large-01", Owner: readOwner}, "main", recs[i:min(i+5000, n)])
	}
	meta, err := a.ReadMeta("s-large-01")
	if err != nil || meta == nil {
		t.Fatalf("not stored: %v", err)
	}
	return &View{Meta: meta, a: a}
}

// TestALatePageDecodesOnlyItsOwnRecords — a page that starts late in a stream skips the records before
// it UNDECODED, unless the Claude `tool=` filter needs their call names. Measured by allocations: the
// conversation view of the LAST record of a 2,000-record stream must cost no more than the raw view of
// it (which never decodes) plus a bound one record's decode fits in many times over. At the base it
// cost every earlier record's decode (~46 allocations each); the `tool=` page is the positive
// control — it MUST still read the earlier calls, and so must cost far more.
func TestALatePageDecodesOnlyItsOwnRecords(t *testing.T) {
	const n = 2000
	v := largeStream(t, n)
	allocs := func(q RecordsQuery) float64 {
		q.Stream, q.FromSeq, q.MaxBytes = "main", n, DefaultPageBytes
		return testing.AllocsPerRun(3, func() {
			p, err := v.Page(q)
			if err != nil || len(p.Records) != 1 {
				t.Fatalf("the late page: %v, %d records", err, len(p.Records))
			}
		})
	}
	raw := allocs(RecordsQuery{View: ViewRaw})
	conv := allocs(RecordsQuery{View: ViewConversation})
	class := allocs(RecordsQuery{View: ViewRaw, Class: ToolResult})
	tool := allocs(RecordsQuery{View: ViewConversation, Tool: "Bash"})
	const slack = 500
	t.Logf("allocations for the last record's page: raw %.0f, conversation %.0f, class= %.0f, tool= %.0f", raw, conv, class, tool)
	if conv > raw+slack || class > raw+slack {
		t.Errorf("a late page decodes records before it: conversation %.0f / class= %.0f allocations against raw %.0f (bound +%d)",
			conv, class, raw, slack)
	}
	if tool < raw+10*slack {
		t.Errorf("POSITIVE CONTROL: the tool= page cost %.0f allocations against raw %.0f — it must decode every earlier call "+
			"for its names, so this measurement cannot see a skipped decode", tool, raw)
	}
}

func BenchmarkPageLastRecordOfALargeStream(b *testing.B) {
	const n = 20000
	v := largeStream(b, n)
	for _, c := range []struct {
		name string
		q    RecordsQuery
	}{
		{"conversation", RecordsQuery{Stream: "main", FromSeq: n, View: ViewConversation, MaxBytes: DefaultPageBytes}},
		{"tool-filter", RecordsQuery{Stream: "main", FromSeq: n, Tool: "Bash", View: ViewConversation, MaxBytes: DefaultPageBytes}},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if _, err := v.Page(c.q); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
