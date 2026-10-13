package plugins

import (
	"slices"
	"strings"
	"time"
)

// 🔴 CONTINUOUS = INCREMENTAL, BY A STATELESS QUERY (decision 13). Revision 1's leases and acks are
// REMOVED (D2): the pod holds no per-plugin cursor. A session is pending for a plugin when the plugin
// is ON for it ([Effective], asked by the caller), its transcript holds a record past the plugin's
// latest output watermark, and it has been idle at least the entry's idle time OR grown by at least
// its record count since that watermark. Writing an output IS what advances the watermark.
//
// ⚠ IT ASSUMES ONE WORKER PER PLUGIN: two would receive the same items and duplicate work —
// harmless, costly, and the plan's stated trade. There is no "session ended" signal in either
// runtime's storage (R1–R3), so idleness is the trigger.

// Candidate is one stored session the caller already found ON for the plugin.
type Candidate struct {
	Root string
	// Streams is every stream's committed `seq_to`.
	Streams map[string]int64
	// UpdatedAt is the session's last accepted upload (the pod clock).
	UpdatedAt time.Time
	// Prev is the plugin's LATEST output for the session — the last one it wrote — or nil.
	Prev *Output
}

// StreamPos is one stream's position in a pending item.
type StreamPos struct {
	SeqTo int64 `json:"seq_to"`
}

// PrevOutput is the plugin's previous output for the session, so it summarises only the new records
// plus its own prior output.
type PrevOutput struct {
	ID        string           `json:"id"`
	Watermark map[string]int64 `json:"watermark"`
}

// PendingItem is one row of `GET /plugin/v1/pending`.
type PendingItem struct {
	Session    string               `json:"session"`
	Streams    map[string]StreamPos `json:"streams"`
	PrevOutput *PrevOutput          `json:"prev_output"`
}

// Bounds on `?limit=`.
const (
	DefaultPendingLimit = 10
	MaxPendingLimit     = 100
)

// grown is how many records the session holds past the watermark, summed over its streams. A stream
// the watermark does not name counts from zero.
func grown(streams map[string]int64, wm map[string]int64) int64 {
	var n int64
	for name, seq := range streams {
		if d := seq - wm[name]; d > 0 {
			n += d
		}
	}
	return n
}

// Pending is the query over candidates, oldest-updated first, at most `limit` items.
func Pending(e Entry, cands []Candidate, now time.Time, limit int) []PendingItem {
	idle := time.Duration(e.PendingIdleSeconds) * time.Second
	grow := int64(e.PendingGrowRecords)
	var due []Candidate
	for _, c := range cands {
		var wm map[string]int64
		if c.Prev != nil {
			wm = c.Prev.InputWatermark
		}
		n := grown(c.Streams, wm)
		if n == 0 {
			continue
		}
		if now.Sub(c.UpdatedAt) >= idle || n >= grow {
			due = append(due, c)
		}
	}
	slices.SortFunc(due, func(a, b Candidate) int {
		if c := a.UpdatedAt.Compare(b.UpdatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.Root, b.Root)
	})
	if len(due) > limit {
		due = due[:limit]
	}
	out := make([]PendingItem, 0, len(due))
	for _, c := range due {
		item := PendingItem{Session: c.Root, Streams: map[string]StreamPos{}}
		for name, seq := range c.Streams {
			item.Streams[name] = StreamPos{SeqTo: seq}
		}
		if c.Prev != nil {
			item.PrevOutput = &PrevOutput{ID: c.Prev.ID, Watermark: c.Prev.InputWatermark}
		}
		out = append(out, item)
	}
	return out
}
