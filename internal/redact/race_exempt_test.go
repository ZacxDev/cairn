package redact

import (
	"os"
	"testing"

	"github.com/ZacxDev/cairn/internal/racegate"
)

// raceExempt is the DECLARED set of this package's tests that `go test -race` skips and the `go`
// CI job runs WITHOUT the detector instead (`CAIRN_RACE_EXEMPT_ONLY=1`). Each is single-goroutine
// and CPU-bound: the detector multiplies its cost and has no second goroutine to compare against.
// `TestNoRaceExemptTestCanReachAGoroutine` below is what holds the "single-goroutine" half.
//
// The criterion is cost, not kind: the tests that took ≥5 s each under `-race` on one 24-core
// machine (the heaviest ~58 s, the set ~260 s of the package's ~287 s), against ~15 s for the
// WHOLE package without it. Cheap single-goroutine tests stay under the detector on purpose —
// they cost little, and they hedge the blind spots `internal/racegate` states.
//
// 🔴 NOTHING THAT CALLS `measureCorpus` MAY BE HERE: it shares one `*Redactor` across GOMAXPROCS
// goroutines, so its callers are the package's real check of "a Redactor holds no mutable state".
// The guard refuses them, and names them as its positive control.
var raceExempt = []string{
	"TestTheRedactorIsQuietOnItsOwnOutput",
	"TestRoundFiveSelfTestHoldsAcrossSeeds",
	"TestRoundSixTheWholePipelineIsLinear",
	"TestTheRateRangeHoldsOverSeedsFourToEight",
	"TestKeyContextIsLinearTime",
	"TestRoundSixLongValuesKeepTheFinderLinear",
	"TestRoundSixLongNamedValuesAreRedacted",
	"TestRoundSixSelfTestHoldsAcrossSeeds",
	"TestTheCorpusIsFullyCaughtAndNothingCleanIsDamaged",
	"TestRoundSixPgpassOverPrefixedNeutralLines",
	"TestRoundSixSymbolLeadingValuesInConfigLayouts",
	"TestRoundFiveKeyContextIsLinearTime",
	"TestRoundSixWeakNameRecall",
}

func TestMain(m *testing.M) { os.Exit(racegate.Main(m, raceExempt)) }

// TestNoRaceExemptTestCanReachAGoroutine is the seam guard between the exempt list and the code:
// a test that starts a goroutine, or calls anything here that does, cannot be in the list. The
// positive control is `measureCorpus` and its three callers — the analysis must see them, or it
// is seeing nothing.
func TestNoRaceExemptTestCanReachAGoroutine(t *testing.T) {
	racegate.Check(t, ".", raceExempt, []string{
		"measureCorpus",
		"TestTheCleanDamageBudgetOverTheFrozenCorpus",
		"TestTheCleanDamageBudgetOverThisRepository",
		"TestTheCleanDamageCeilingOverTheGoStandardLibrary",
	})
}
