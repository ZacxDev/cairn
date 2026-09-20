package report

import (
	"slices"
	"strconv"

	"github.com/ZacxDev/cairn/internal/hostid"
	"github.com/ZacxDev/cairn/internal/store"
)

// Reader is the Renderer, and it is the ONLY type in this package that knows it is being
// called by a server.
//
// 🔴 IT IS DELIBERATELY THIN, BECAUSE THE CLI IS THE OTHER CALLER. The pod needs "give me
// the four-state status, the exit code and the bytes"; a CLI needs the REPORT — to emit
// JSON, to add its own header lines, to decide its own exit. So the work lives in `Recall`,
// `Search`, `RecallReport.RenderText`, `SearchReport.RenderText` and `ExitFor`, each of
// which takes and returns plain values, and this type only composes them. Nothing in this
// package imports `net/http`, reads server configuration, or returns an error a non-HTTP
// caller cannot classify.
type Reader struct {
	// Host is "whose disk is this", injected so a test can pin the one line of output that
	// is deliberately machine-dependent. nil means hostid.ThisHost.
	//
	// ⚠ IT IS A SEAM AND NOT A SETTING. A deployment does not choose its identity here —
	// `CAIRN_HOST` and `/etc/machine-id` do, through hostid — and a caller that passed a
	// constant would be stating one host's store as the fleet's, which is the one claim the
	// host line exists to prevent.
	Host func() string
}

func (rd Reader) host() string {
	if rd.Host != nil {
		return rd.Host()
	}
	return hostid.ThisHost()
}

// Recall renders one `/recall` request.
//
// 🔴 IT AND `Search` PASS NO INSTANCE TO `RenderText`, AND THAT IS A STATEMENT ABOUT THE POD
// RATHER THAN A DEFAULT NOBODY REVISITED. This is the `Renderer` the SERVER hands
// `internal/api`, and a pod serves exactly ONE store: there is no second place its answer
// could have come from, so the caveat's multi-instance clause would be false of it on every
// request. The CLI is the caller that can have several, and it reaches `RenderText` directly
// rather than through this type — see `internal/client`.
func (rd Reader) Recall(storeRoot string, opts RecallOptions, visible store.ScopeSet) (Rendered, error) {
	rep, err := Recall(storeRoot, opts, visible)
	if err != nil {
		return Rendered{}, err
	}
	// ⚠ THE LABEL IS THE REPORT'S NORMALIZED SCOPE, NOT THE CALLER'S SPELLING, and it is
	// the recall side's only difference from the search side here. The exit decision reads
	// it, and the sentence it lands in names a directory.
	code, warning := ExitFor(rep.Status, rep.Scope+"/", rep.Malformed)
	return Rendered{
		Status:  rep.Status,
		Scope:   rep.Scope,
		Exit:    code,
		Text:    rep.RenderText(rd.host(), nil, ""),
		Warning: warning,
	}, nil
}

// Search renders one `/search` request.
func (rd Reader) Search(storeRoot string, opts SearchOptions, visible store.ScopeSet) (Rendered, error) {
	rep, err := Search(storeRoot, opts, visible)
	if err != nil {
		return Rendered{}, err
	}
	code, warning := ExitFor(rep.Status, rep.Label(), rep.Malformed)
	return Rendered{
		Status:  rep.Status,
		Scope:   rep.Scope,
		Exit:    code,
		Text:    rep.RenderText(rd.host(), nil, ""),
		Warning: warning,
	}, nil
}

// ExitFor is the exit code, and the one warning line that goes with it. ONE decision site,
// for both report types.
//
// 🔴 CONTENT SERVED ⇒ 0; NOTHING READABLE ⇒ 3. A consumer branches on zero/non-zero and its
// instruction is "print the warning verbatim, note that recall was unavailable, and
// continue". That instruction is TRUE only when recall really was unavailable — a scope with
// 2 good entries and 1 malformed one has just served both good ones, and exiting non-zero
// would throw them away to report a defect the output already names, loudly, in band.
//
// 🔴 IT REUSES 3 RATHER THAN MINTING A CODE. 3 is already "the store is broken" for this
// CLI, the documented handling is identical for all of them, and a fourth code would need
// every consumer to learn it before it changed any behaviour — a declaration no code path
// honours.
//
// 🔴 THE WARNING IS RETURNED, NOT PRINTED, AND THAT IS THE ONE DELIBERATE DIFFERENCE FROM
// THE ORACLE. There it writes to `sys.stderr` from inside the library, which is why the pod
// gets the sentence in its log for free — and which is also a library deciding where a
// caller's diagnostics go. Returning it keeps the sentence identical while letting the pod
// put it on stderr and a CLI put it wherever its own convention says. The caller that drops
// it is the caller that loses the signal, so both callers forward it.
func ExitFor(status, label string, malformed []store.MalformedEntry) (int, string) {
	if !slices.Contains(UnreadableStatuses, status) {
		return 0, ""
	}
	n := len(malformed)
	return 3, "subsystem-recall: " + status + ": all " + strconv.Itoa(n) + " entry file" +
		plural(n) + " under `" + label + "` are MALFORMED — nothing could be read, so recall " +
		"was unavailable. This is NOT an empty scope and NOT 'nothing recorded yet'. " +
		"Per-entry reasons are on stdout; check a scope with `cairn validate --scope <scope>`, " +
		"which names each file that fails to parse."
}
