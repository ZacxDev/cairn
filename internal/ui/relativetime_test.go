package ui

import (
	"testing"
	"time"
)

// TestRelativeTimeBucketsAtTheirBoundariesAndMiddles drives `relativeTime` at TWO points per
// bucket at least — a boundary and a middle — against a fixed clock, because a bucket edge is
// exactly where an off-by-one (`<=` for `<`, a round for a floor) hides.
func TestRelativeTimeBucketsAtTheirBoundariesAndMiddles(t *testing.T) {
	now := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	const day = 24 * time.Hour
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{
		// "just now": the bucket's floor, middle and last instant — and a small FUTURE skew.
		{0, "just now"},
		{30 * time.Second, "just now"},
		{time.Minute - time.Nanosecond, "just now"},
		{-30 * time.Second, "just now"},
		{-time.Minute, "just now"},
		// beyond a minute in the FUTURE is the date, never rounded to "now"
		{-time.Minute - time.Second, "2000-06-01"},
		{-2 * day, "2000-06-03"},
		// minutes: boundary, middle, last instant (FLOORED, never rounded up to an hour)
		{time.Minute, "1m ago"},
		{30 * time.Minute, "30m ago"},
		{time.Hour - time.Second, "59m ago"},
		// hours
		{time.Hour, "1h ago"},
		{7*time.Hour + 59*time.Minute, "7h ago"},
		{day - time.Second, "23h ago"},
		// days
		{day, "1d ago"},
		{15 * day, "15d ago"},
		{30*day - time.Second, "29d ago"},
		// past 30 days: the absolute UTC date
		{30 * day, "2000-05-02"},
		{400 * day, "1999-04-28"},
	} {
		if got := relativeTime(now.Add(-tc.age), now); got != tc.want {
			t.Errorf("age %v: got %q, want %q", tc.age, got, tc.want)
		}
	}
	// No clock is "no distance": the date, never "2000 years ago".
	if got := relativeTime(now, time.Time{}); got != "2000-06-01" {
		t.Errorf("with a zero clock: got %q, want the date", got)
	}
}

// TestAnUnknownMtimeRendersNoTimestamp: 0 is "the stat failed", and the page says nothing
// rather than the epoch.
func TestAnUnknownMtimeRendersNoTimestamp(t *testing.T) {
	if n := timeAgo(0, time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)); n != nil {
		t.Errorf("an mtime of 0 rendered a timestamp: %v", renderNode(t, n))
	}
	if n := timeAgo(1, time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)); n == nil {
		t.Fatal("POSITIVE CONTROL FAILED: a real mtime rendered nothing, so the zero above is vacuous")
	}
}
