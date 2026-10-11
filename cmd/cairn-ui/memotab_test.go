package main

import (
	"strings"
	"testing"
)

// TestWithNoDatabaseTheBrowserHasNoMemoStore — `wireBrowserMemos`' first condition without a
// database, and the typed-nil trap it exists against: the answer must be the INTERFACE nil, or
// `ui.Config.Memos != nil` would be true and the tab would call a store that is not there. The
// database-and-journal arms are the pgtest tier's (`TestWireBrowserMemosNeedsADatabaseAndAJournal`).
func TestWithNoDatabaseTheBrowserHasNoMemoStore(t *testing.T) {
	m, err := wireBrowserMemos(nil, "a-journal-path", nil)
	if err != nil || m != nil {
		t.Fatalf("no database wired %#v (%v), want the interface nil", m, err)
	}
	if got := memoMode(m); !strings.HasPrefix(got, "NO memo store") {
		t.Errorf("the startup clause for no store reads %q", got)
	}
	if got := memoMode(browserMemos{}); got != "memos in postgres" {
		t.Errorf("the startup clause for a store reads %q", got)
	}
}
