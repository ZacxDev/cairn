package capture

import (
	"time"

	"github.com/ZacxDev/cairn/internal/client"
)

// The seams `f1move_test.go` reaches the F1 deferral through. They exist so that file compiles
// UNCHANGED against the code before #317 — with a stand-in for this file that answers what that code
// did (no window, no counter, F1 routed by the base rule) — and its tests can be watched FAIL there on
// their own assertions rather than on a compile error. Nothing else should call them.

const f1Window = F1DeferWindow

func deferredCount(s Summary) int            { return s.Deferred }
func summaryDeferred(n, refused int) Summary { return Summary{Deferred: n, Refused: refused} }
func f1Since(ss *SessionState) int64         { return ss.F1Since }
func f1Lapsed(ss *SessionState) bool         { return ss.F1Lapsed }

// f1Decision is the part of a [Decision] the F1 tests read.
type f1Decision struct {
	Instance                   string
	Held, Deferred, F1Deferred bool
	F1Lapsed                   bool
	Reason                     string
}

func decideF1(v []string, f1 bool, repo Repo, routing client.Routing, bound string, since, now time.Time) f1Decision {
	d := DecideRun(v, f1, repo, routing, RepoRules{Bound: bound, F1Since: since, Now: now})
	return f1Decision{Instance: d.Instance, Held: d.Held, Deferred: d.Deferred, F1Deferred: d.F1Deferred,
		F1Lapsed: d.F1Lapsed, Reason: d.Reason}
}
