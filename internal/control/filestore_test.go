package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tempStore(t *testing.T) *FileStore {
	t.Helper()
	s, err := OpenFileStore(filepath.Join(t.TempDir(), "control", "journal.jsonl"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// A pinned clock, so a stamped event is reproducible. `at(100)` is past every
	// timestamp the fixture world uses.
	s.Now = func() time.Time { return at(100) }
	return s
}

func TestAnAppendedJournalSurvivesReopening(t *testing.T) {
	ctx := context.Background()
	s := tempStore(t)

	m, err := s.Append(ctx, worldEvents()...)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if m.Epoch != uint64(len(worldEvents())) {
		t.Fatalf("epoch = %d, want %d", m.Epoch, len(worldEvents()))
	}

	reopened, err := OpenFileStore(s.Path())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	back, err := reopened.Model(ctx)
	if err != nil {
		t.Fatalf("model after reopen: %v", err)
	}
	// Compare the authority, not the struct: a durable format that round-trips its
	// fields and produces different answers is the failure this is guarding.
	for _, principal := range sortedLedgerPrincipals() {
		a := Resolve(m, user(principal))
		b := Resolve(back, user(principal))
		for scopeID := range expectedMatrix[principal] {
			if verbsOrEmpty(a.VerbsOn(scopeID)) != verbsOrEmpty(b.VerbsOn(scopeID)) {
				t.Errorf("%s on %s: %q in memory, %q after reopening",
					principal, scopeID, verbsOrEmpty(a.VerbsOn(scopeID)), verbsOrEmpty(b.VerbsOn(scopeID)))
			}
		}
	}
}

// TestARejectedBatchLeavesNeitherBytesNorState is the regression test for a defect
// this package shipped with and had to have removed.
//
// 🔴 RED AT BASELINE BY A ONE-WORD CHANGE: replace `current.clone()` with `current`
// in `FileStore.Append` and this test fails. A `Model` is six maps behind a struct
// header, so a struct copy shares every bucket — and `Append` validates a batch by
// applying it to what it believes is a scratch copy. With a shallow copy the scratch
// IS the live cache, so a batch rejected at its third event leaves the first two
// permanently applied to the authority the process is serving, with nothing in the
// journal recording them. The served model is then WIDER than the file it claims to
// project, and a restart silently "loses" grants that were never written.
//
// The batch below is built so the partial application is OBSERVABLE: event 1 grants
// alice admin on a beacon scope, and event 3 is rejected. If event 1 leaked into the
// cache, alice's authority moves.
func TestARejectedBatchLeavesNeitherBytesNorState(t *testing.T) {
	ctx := context.Background()
	s := tempStore(t)
	if _, err := s.Append(ctx, worldEvents()...); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	before, err := s.Model(ctx)
	if err != nil {
		t.Fatalf("model: %v", err)
	}
	beforeEpoch := before.Epoch
	beforeAlice := verbsOrEmpty(Resolve(before, user(uAlice)).VerbsOn(sBeaconNotes))
	if beforeAlice != "" {
		t.Fatalf("the fixture already gives alice %q on beacon-notes — this test cannot see the leak it is for", beforeAlice)
	}
	beforeBytes, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}

	_, err = s.Append(ctx,
		// Valid, and observable if it leaks.
		Event{Kind: EventGranted, At: at(60), GrantID: "grt_leak", Actor: uDave,
			SubjectKind: KindUser, SubjectID: uAlice,
			ObjectKind: ObjectScope, ObjectID: sBeaconNotes, Verbs: AllSet},
		// Valid.
		Event{Kind: EventUserCreated, At: at(61), UserID: "usr_grace", Provider: "example", Subject: "grace"},
		// REJECTED: names a project that does not exist.
		Event{Kind: EventMemberSet, At: at(62), ProjectID: "prj_nowhere", UserID: "usr_grace", Role: RoleMember},
	)
	if err == nil {
		t.Fatal("a batch whose third event cannot replay was accepted")
	}

	after, err := s.Model(ctx)
	if err != nil {
		t.Fatalf("model after the rejected batch: %v", err)
	}
	if after.Epoch != beforeEpoch {
		t.Errorf("epoch moved from %d to %d on a REJECTED batch", beforeEpoch, after.Epoch)
	}
	if got := verbsOrEmpty(Resolve(after, user(uAlice)).VerbsOn(sBeaconNotes)); got != "" {
		t.Errorf("alice gained %q on beacon-notes from a batch that was REJECTED — the first event of the batch leaked into the live cache, which is the shallow-copy defect this test exists for", got)
	}
	if _, leaked := after.Users["usr_grace"]; leaked {
		t.Error("the batch's second event leaked into the live cache")
	}
	afterBytes, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if string(afterBytes) != string(beforeBytes) {
		t.Errorf("the journal grew by %d bytes on a REJECTED batch — an append-only log cannot take an event back, so nothing may reach it unvalidated",
			len(afterBytes)-len(beforeBytes))
	}
}

// TestAnUnreadableJournalKeepsTheLastKnownGoodAuthority.
//
// 🔴 AN EMPTY MODEL AUTHORISES NOBODY, so replacing a good one with it turns a parse
// error into a total outage that reads like a permissions problem. The error is
// returned so the caller can shout; the served authority stays the last one that was
// known good, and its epoch says how old that is.
func TestAnUnreadableJournalKeepsTheLastKnownGoodAuthority(t *testing.T) {
	ctx := context.Background()
	s := tempStore(t)
	if _, err := s.Append(ctx, worldEvents()...); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	goodEpoch, err := s.Model(ctx)
	if err != nil {
		t.Fatalf("model: %v", err)
	}

	// Corrupt the file out from under the process — the shape a truncated write or
	// a hand-edit produces.
	if err := os.WriteFile(s.Path(), []byte("{not json}\n"), 0o600); err != nil {
		t.Fatalf("corrupting: %v", err)
	}

	m, err := s.Reload(ctx)
	if err == nil {
		t.Fatal("a corrupt journal reloaded cleanly")
	}
	if m.Epoch != goodEpoch.Epoch {
		t.Errorf("the reload returned epoch %d, want the last known good %d", m.Epoch, goodEpoch.Epoch)
	}
	// And the served model — what every request reads — must be unchanged.
	served, _ := s.Model(ctx)
	if got := verbsOrEmpty(Resolve(served, user(uAlice)).VerbsOn(sAtlasNotes)); got != "read,write,admin" {
		t.Errorf("after a failed reload alice has %q on atlas-notes, want read,write,admin — a parse error must not empty the authority", got)
	}
}

// TestAnEventWithNoTimestampIsStampedOnTheWayIn.
//
// The journal refuses an event with no time (it cannot answer "when", which is most
// of what a grant log is for), so the store stamps one rather than making every
// caller carry a clock.
func TestAnEventWithNoTimestampIsStampedOnTheWayIn(t *testing.T) {
	ctx := context.Background()
	s := tempStore(t)
	m, err := s.Append(ctx, Event{Kind: EventUserCreated, UserID: "usr_a", Provider: "example", Subject: "a"})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if !m.Users["usr_a"].CreatedAt.Equal(at(100)) {
		t.Errorf("CreatedAt = %v, want the store's clock %v", m.Users["usr_a"].CreatedAt, at(100))
	}
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), `"at":"2000-01-01T01:40:00Z"`) {
		t.Errorf("the stamp did not reach the durable line: %s", raw)
	}
}

// TestAppendingNothingIsNotAWrite.
func TestAppendingNothingIsNotAWrite(t *testing.T) {
	ctx := context.Background()
	s := tempStore(t)
	if _, err := s.Append(ctx, worldEvents()...); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	before, _ := os.ReadFile(s.Path())
	if _, err := s.Append(ctx); err != nil {
		t.Fatalf("empty append: %v", err)
	}
	after, _ := os.ReadFile(s.Path())
	if string(before) != string(after) {
		t.Error("an empty append wrote to the journal")
	}
}

// TestTheJournalFileIsNotWorldReadable.
//
// It holds credential digests — not secrets in the sense a token is — and the
// complete membership and sharing graph of every tenant, which is not world-readable
// material.
func TestTheJournalFileIsNotWorldReadable(t *testing.T) {
	ctx := context.Background()
	s := tempStore(t)
	if _, err := s.Append(ctx, worldEvents()...); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("journal mode is %04o, want no group or other bits", mode)
	}
}
