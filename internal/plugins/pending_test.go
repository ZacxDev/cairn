package plugins

import (
	"testing"
	"time"
)

var t0 = time.Date(2000, 1, 2, 3, 0, 0, 0, time.UTC)

func entryA() Entry {
	return Entry{Manifest: summaryManifest(), PendingIdleSeconds: DefaultPendingIdleSeconds, PendingGrowRecords: DefaultPendingGrowRecords}
}

func roots(items []PendingItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Session)
	}
	return out
}

// TestPendingAtBothTriggerBoundaries is decision 13's two triggers, each measured on BOTH sides of
// its boundary (9 min 59 s vs 10 min; 199 vs 200 records).
func TestPendingAtBothTriggerBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		idle    time.Duration
		records int64
		want    bool
	}{
		{"idle 9m59s, grown 5", 9*time.Minute + 59*time.Second, 5, false},
		{"idle 10m, grown 5", 10 * time.Minute, 5, true},
		{"idle 1m, grown 199", time.Minute, 199, false},
		{"idle 1m, grown 200", time.Minute, 200, true},
		{"idle 1h, grown 0", time.Hour, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cand := Candidate{Root: "s-0001", Streams: map[string]int64{"main": c.records}, UpdatedAt: t0}
			got := Pending(entryA(), []Candidate{cand}, t0.Add(c.idle), 10)
			if (len(got) == 1) != c.want {
				t.Fatalf("pending = %v, want listed=%v", roots(got), c.want)
			}
		})
	}
}

// TestWritingAnOutputRemovesTheSessionAndAnOlderWatermarkDoesNot: the watermark is the plugin's
// LATEST output; counting from it, a session summarised up to its newest record is not pending, and
// one whose latest output covers less is.
func TestWritingAnOutputRemovesTheSessionAndAnOlderWatermarkDoesNot(t *testing.T) {
	streams := map[string]int64{"main": 250, "subagent:a1": 40}
	now := t0.Add(time.Hour)
	current := &Output{ID: "o-new", Plugin: "summary", InputWatermark: map[string]int64{"main": 250, "subagent:a1": 40}}
	older := &Output{ID: "o-old", Plugin: "summary", InputWatermark: map[string]int64{"main": 249, "subagent:a1": 40}}
	if got := Pending(entryA(), []Candidate{{Root: "s-0001", Streams: streams, UpdatedAt: t0}}, now, 10); len(got) != 1 ||
		got[0].PrevOutput != nil || got[0].Streams["main"].SeqTo != 250 {
		t.Fatalf("with no output: %+v, want one item, no previous output, seq_to 250", got)
	}
	if got := Pending(entryA(), []Candidate{{Root: "s-0001", Streams: streams, UpdatedAt: t0, Prev: current}}, now, 10); len(got) != 0 {
		t.Fatalf("after an output at the newest record: %v, want nothing pending", roots(got))
	}
	got := Pending(entryA(), []Candidate{{Root: "s-0001", Streams: streams, UpdatedAt: t0, Prev: older}}, now, 10)
	if len(got) != 1 || got[0].PrevOutput == nil || got[0].PrevOutput.ID != "o-old" || got[0].PrevOutput.Watermark["main"] != 249 {
		t.Fatalf("after an OLDER output: %+v, want the session still pending, carrying o-old's watermark", got)
	}
}

// TestPendingIsOldestFirstAndLimited.
func TestPendingIsOldestFirstAndLimited(t *testing.T) {
	var cands []Candidate
	for i, root := range []string{"s-c", "s-a", "s-b"} {
		cands = append(cands, Candidate{Root: root, Streams: map[string]int64{"main": 1}, UpdatedAt: t0.Add(time.Duration(i) * time.Minute)})
	}
	got := roots(Pending(entryA(), cands, t0.Add(time.Hour), 2))
	if len(got) != 2 || got[0] != "s-c" || got[1] != "s-a" {
		t.Fatalf("pending = %v, want [s-c s-a] (oldest-updated first, limit 2)", got)
	}
}
