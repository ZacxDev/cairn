package hoststate

import (
	"errors"
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

// TestAnUnlistedEntryGoesOnlyOnAPositiveSignalOrItsTTL — and its stamp is NOT renewed by being asked
// about: twenty unlisted runs two days apart cross the TTL, where a stamp refreshed on every run
// would never expire.
func TestAnUnlistedEntryGoesOnlyOnAPositiveSignalOrItsTTL(t *testing.T) {
	here := func() (bool, error) { return false, nil }
	gone := func() (bool, error) { return true, nil }
	broken := func() (bool, error) { return true, errors.New("the pod could not be reached") }

	b := &Backoff{Failures: 1}
	if b.Unlisted(t0, here) || b.Seen == "" {
		t.Fatalf("an entry the pod still answers for was dropped, or left unstamped: %+v", b)
	}
	stamped := b.Seen
	if b.Unlisted(t0.Add(time.Hour), broken) {
		t.Fatal("a probe that FAILED dropped the entry")
	}
	if !(&Backoff{Seen: stamped}).Unlisted(t0.Add(time.Hour), gone) {
		t.Fatal("the pod's refusal did not drop the entry")
	}
	dropped := -1
	for i := range 20 {
		if b.Unlisted(t0.Add(time.Duration(i)*48*time.Hour), here) {
			dropped = i
			break
		}
	}
	if b.Seen != stamped {
		t.Errorf("being asked about refreshed the stamp: %s → %s", stamped, b.Seen)
	}
	if dropped != 16 { // 32 days: the first run past the 30-day TTL
		t.Errorf("the entry was dropped at run %d, want 16 (the first past %s)", dropped, StateTTL)
	}
}

// TestTouchSaysWhenTheStampMovedEnoughToSave — a listed session's stamp must reach the file, but not
// on every run.
func TestTouchSaysWhenTheStampMovedEnoughToSave(t *testing.T) {
	b := &Backoff{}
	if !b.Touch(t0) {
		t.Fatal("the first stamp is not reported")
	}
	if b.Touch(t0.Add(23 * time.Hour)) {
		t.Error("a stamp less than a day old was reported as moved")
	}
	if !b.Touch(t0.Add(25*time.Hour)) || b.Stale(t0.Add(25*time.Hour+StateTTL)) {
		t.Errorf("a stamp a day old was not renewed: %+v", b)
	}
}
