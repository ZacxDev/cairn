package archive

import (
	"os"
	"testing"

	"github.com/ZacxDev/cairn/internal/racegate"
)

// raceExempt: the tests `go test -race` skips here and the `go` CI job runs without the detector
// (`CAIRN_RACE_EXEMPT_ONLY=1`); the mechanism and its limits are `internal/racegate`'s doc.
//
// One test, and its cost is the REDACTOR's, not this package's: it pushes a 3 MB record through
// `redact.(*Redactor).Record`, which is where a race-built CPU profile of it spends its time. It
// took ~34 s of the package's ~41 s under `-race` on one 24-core machine, against ~5 s for the
// whole package without it. It is single-goroutine; the guard below holds that.
//
// 🔴 `TestTwoConcurrentUploadsFromOnePositionOneWins` MUST STAY OUT: it drives two
// `AppendRecords` — and so one shared `*redact.Redactor` — from two goroutines, which makes it
// this package's race check of the archive's locking AND of the redactor it calls. It is the
// guard's positive control.
var raceExempt = []string{
	"TestAFramedRecordIsStoredAsOneRecordByteIdentical",
}

func TestMain(m *testing.M) { os.Exit(racegate.Main(m, raceExempt)) }

// TestNoRaceExemptTestCanReachAGoroutine: the seam guard between the list and the code.
func TestNoRaceExemptTestCanReachAGoroutine(t *testing.T) {
	racegate.Check(t, ".", raceExempt, []string{"TestTwoConcurrentUploadsFromOnePositionOneWins"})
}
