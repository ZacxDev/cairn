package capture

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestTheRedactionOffBuildSendsOnlyToLoopback: the positive-control build refuses every URL that
// is not loopback, so it cannot be pointed at a real instance.
func TestTheRedactionOffBuildSendsOnlyToLoopback(t *testing.T) {
	if _, err := ValidateEndpoint("https://worker.example.invalid"); err != nil {
		t.Fatalf("control: a release build refuses an https URL: %v", err)
	}
	redactionOff = true
	defer func() { redactionOff = false }()
	if _, err := ValidateEndpoint("https://worker.example.invalid"); err == nil {
		t.Fatal("the redaction-off build accepted a non-loopback URL")
	}
	if _, err := ValidateEndpoint("http://127.0.0.1:9"); err != nil {
		t.Fatalf("the redaction-off build refused loopback: %v", err)
	}
}

// TestASecondRunIsLockedOut: two overlapping runs never share the state file.
func TestASecondRunIsLockedOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	release, err := LockState(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockState(path); !errors.Is(err, ErrRunning) {
		t.Fatalf("a second lock while the first is held: %v", err)
	}
	release()
	again, err := LockState(path)
	if err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
	again()
}
