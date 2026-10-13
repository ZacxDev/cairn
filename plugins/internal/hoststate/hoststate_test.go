package hoststate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)

// TestABackoffDoublesToItsCeilingAndASessionThatGrewStartsOver.
func TestABackoffDoublesToItsCeilingAndASessionThatGrewStartsOver(t *testing.T) {
	var none *Backoff
	if none.Active(t0, 7) {
		t.Fatal("a session nothing is remembered about is backing off")
	}
	b := &Backoff{}
	for i, want := range []time.Duration{15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour, 16 * time.Hour, 24 * time.Hour, 24 * time.Hour} {
		if got := b.Fail(t0, 7); got != want {
			t.Fatalf("failure %d backs off %s, want %s", i+1, got, want)
		}
	}
	if !b.Active(t0.Add(24*time.Hour-time.Second), 7) || b.Active(t0.Add(24*time.Hour), 7) {
		t.Error("the backoff does not end exactly at its retry time")
	}
	// The same streak at ANOTHER record count is not a backoff: the session grew.
	if b.Active(t0, 8) {
		t.Error("a session that grew is still backing off")
	}
	if got := b.Fail(t0, 8); got != BackoffBase || b.Failures != 1 {
		t.Errorf("the first failure after growth backs off %s with %d failures, want the base interval and 1", got, b.Failures)
	}
	// Forty failures must not overflow the shift into a zero or negative wait.
	c := &Backoff{}
	for range 40 {
		if got := c.Fail(t0, 1); got <= 0 || got > BackoffMax {
			t.Fatalf("a long streak backs off %s", got)
		}
	}
}

// TestTheStateFileIsWrittenWholeAndAnUnreadableOneIsAnError — a missing file is empty; a damaged one
// refuses rather than silently forgetting every backoff.
func TestTheStateFileIsWrittenWholeAndAnUnreadableOneIsAnError(t *testing.T) {
	type doc struct {
		Sessions map[string]*Backoff `json:"sessions"`
	}
	path := filepath.Join(t.TempDir(), "state", "sessions.json")
	var empty doc
	if err := Load(path, &empty); err != nil || empty.Sessions != nil {
		t.Fatalf("a missing file: %v %+v", err, empty)
	}
	in := doc{Sessions: map[string]*Backoff{"s-0001": {Failures: 2, RetryAt: "2000-01-02T03:34:05Z", Records: 9}}}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the state file is mode %v (%v), want 0600", st.Mode().Perm(), err)
	}
	var out doc
	if err := Load(path, &out); err != nil || *out.Sessions["s-0001"] != *in.Sessions["s-0001"] {
		t.Fatalf("read back %+v (%v)", out.Sessions["s-0001"], err)
	}
	left, _ := os.ReadDir(filepath.Dir(path))
	if len(left) != 1 {
		t.Errorf("%d files beside the state file, want no temporary left behind", len(left)-1)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(path, &out); err == nil || !strings.Contains(err.Error(), "does not parse") {
		t.Fatalf("a damaged state file loaded: %v", err)
	}
}
