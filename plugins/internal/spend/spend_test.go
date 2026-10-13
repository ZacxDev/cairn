package spend

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// These two tests moved here with the ledger (S10: the spend cap is shared by both example plugins);
// a cap of 0 sending nothing is asserted by each plugin's own suite against its fake provider.

// TestTheLedgerIsTakenUnderItsLock — while another process holds the ledger's lock, `Take` WAITS; it
// neither proceeds nor fails. That is what stops two runs both reading "one slot left" and both taking
// it. And with the lock free, twenty concurrent takers against a cap of five get exactly five.
func TestTheLedgerIsTakenUnderItsLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spend.log")
	l := &Ledger{Path: path, Cap: 5, Now: func() time.Time { return time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC) }}
	holder, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- l.Take() }()
	select {
	case err := <-done:
		t.Fatalf("take returned (%v) while another holder had the ledger locked", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("take after the lock was released: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("take never returned after the lock was released")
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	taken := 1
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Take() == nil {
				mu.Lock()
				taken++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if taken != 5 {
		t.Errorf("%d requests were taken against a cap of 5", taken)
	}
}

// TestAnUnreadableLedgerLineCountsAsSpent — a damaged ledger must not read as "nothing spent".
func TestAnUnreadableLedgerLineCountsAsSpent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spend.log")
	now := time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC)
	if err := os.WriteFile(path, []byte("not a time\n\x00\x01garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := &Ledger{Path: path, Cap: 2, Now: func() time.Time { return now }}
	if err := l.Take(); err != ErrCapped {
		t.Fatalf("two unreadable lines against a cap of 2: take answered %v, want ErrCapped", err)
	}
	l.Cap = 3
	if err := l.Take(); err != nil {
		t.Fatalf("POSITIVE CONTROL: a cap of 3 over two lines must admit one more: %v", err)
	}
}
