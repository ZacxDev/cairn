package capture

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
)

// TestAMoveWhoseDestinationIsHeldByAnotherUploaderCountsAsRecurring: a session whose routing
// answer moves it is first PROBED on the destination (`Agent.route`). A destination that answers
// the uniform 409 — another uploader holds this root there — answers it again on every run, and
// that error reaches the session's report without passing through `Agent.held`, so it is
// classified where the session's error is (`Agent.isolate`'s fallback). An unreachable
// destination is the control: the same path, a transient cause, not counted.
func TestAMoveWhoseDestinationIsHeldByAnotherUploaderCountsAsRecurring(t *testing.T) {
	for _, tc := range []struct {
		name      string
		probe     error
		notOwners int
	}{
		{"held by another uploader", ErrNotOwner, 1},
		{"unreachable (control)", errors.New("the request to instance client failed: connection refused"), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := loadWorld(t)
			e := materialize(t, w, "cc-plain")
			s := w.Sessions["cc-plain"]
			a := newAgent(t, e, twoInstances, &fakeRunner{}, &bytes.Buffer{})
			if sum := mustRun(t, a); sum.Shipped != 1 || a.State.Sessions[s.ID].Instance != "personal" {
				t.Fatalf("control: run 1 = %+v, bound to %q — want shipped to personal", sum, a.State.Sessions[s.ID].Instance)
			}
			appendTo(t, filepath.Join(e.claude, s.Project, s.ID+".jsonl"), betaRead) // now routes to `client`
			e.sink.probeErr = tc.probe
			sum, err := a.Run()
			if err != nil || sum.Refused != 1 || sum.WithdrawalPending != 1 {
				t.Fatalf("CONTROL: the move's probe was not reached and refused: %+v, %v", sum, err)
			}
			if sum.NotOwner != tc.notOwners || sum.Unresumable != 0 {
				t.Fatalf("%+v — want NotOwner=%d", sum, tc.notOwners)
			}
		})
	}
}
