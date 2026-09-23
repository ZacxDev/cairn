package client

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// CacheAge is `(ageSeconds, ok, stampFields)`; `ok` is false when the age is UNKNOWN.
//
// 🔴 AN UNREADABLE STAMP IS NOT AGE 0. "Fresh" is the one answer a broken clock must never
// produce here.
//
// 🔴 AND A FUTURE-DATED STAMP IS UNKNOWN, NOT FRESH. The pre-fix version clamped a negative
// age to `0s`, i.e. "just synced" — precisely the answer this function exists to refuse, and
// a suspending laptop (the case the whole degrade-to-cache design is written for) is where
// clock jumps actually happen.
func CacheAge(cache string) (int64, bool, map[string]string) {
	lines, _ := ReadStamp(cache)
	fields := StampFields(lines)
	raw, present := fields["synced"]
	if !present {
		return 0, false, fields
	}
	synced, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false, fields
	}
	age := time.Now().Unix() - synced
	if age < 0 {
		return 0, false, fields
	}
	return age, true, fields
}

// HumanAge is the age vocabulary, and it is the one the banner's freshness claim is
// denominated in.
func HumanAge(seconds int64, known bool) string {
	if !known {
		return "age UNKNOWN" // callers phrase this as "cache <x> old"
	}
	switch {
	case seconds < 90:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 5400:
		return fmt.Sprintf("%dm", seconds/60)
	case seconds < 172800:
		return fmt.Sprintf("%dh", seconds/3600)
	default:
		return fmt.Sprintf("%dd", seconds/86400)
	}
}

// agePhrase is `cache 3h old` / `cache age UNKNOWN` — never `cache age UNKNOWN old`.
func agePhrase(seconds int64, known bool) string {
	if !known {
		return "cache age UNKNOWN"
	}
	return "cache " + HumanAge(seconds, true) + " old"
}

// StampExists answers "is there a cache at all" from the one fact that discriminates it.
//
// 🔴 THE DISCRIMINATOR IS THE STAMP, NOT THE DIRECTORY. A store that carries a stamp can
// state its own freshness; one that cannot must not be served silently — which is exactly
// how a frozen pre-cutover mirror spent a day masquerading as current.
func StampExists(cache string) bool {
	_, err := os.Stat(filepath.Join(cache, SyncStamp))
	return err == nil
}

// State is the four-state decision, resolved in ONE place.
//
// `ExitHint` is non-zero only for the NO-CACHE outcome, and it is a READ verdict: it means
// "this is what a reader should exit with". 🔴 IT HAS NO MEANING FOR A WRITE, and returning
// it from one was a measured defect on the Python side — `put` on a fresh host exited 3, the
// one code the whole design insists a write must never return, on its likeliest path.
type State struct {
	Name     string
	Detail   string
	ExitHint int
}

// ResolveState is the four-state decision. Every caller reads its state from here so the
// four cannot drift apart per subcommand, which is how three of them end up rendering alike.
//
// 🔴 A `*StoreCorrupt` LEAVES BY THE ERROR RETURN AND IS NEVER A STATE. The oracle lets it
// propagate out of `resolve_state` for exactly this reason: an outage is absorbed into
// "serving from cache" at exit 0, and a server shipping a link, a traversal member, a
// duplicate or a count disagreeing with its own header must instead STOP the run — cache or
// no cache. Returning it as a fourth state would put the decision in every caller.
//
// 🔴 `instance` NAMES WHICH CONFIGURED INSTANCE TO FETCH FROM, AND OMITTING IT WAS A MEASURED
// SILENT MISROUTE. This function takes a CACHE DIRECTORY and, until this parameter existed,
// derived its credentials from `LoadConfig()` — which is always the DEFAULT instance. Any
// caller that walks instances therefore fetched `personal`'s store N times and unpacked it
// into each alias's sibling cache root in turn: `routes --check` on a two-instance host read
// the second instance's banner as `fetched from <personal's URL>`, OVERWROTE that instance's
// cache with the default instance's snapshot, and then graded the table against a scope set
// that was the default instance's — inventing the "exists on no configured instance" finding
// for every scope that only lives on the other one. The oracle's `resolve_state` has taken
// `instance` from the start and `cmd_routes` passes `instance.alias`; this is the port
// following.
//
// `""` is the default instance, which is what `instance=None` means on the oracle — the one
// path the `SUBSYSTEM_STORE_URL`/`_TOKEN` environment override applies to (`LoadConfigFor`).
func ResolveState(cache string, noSync bool, scope string, timeout int, instance string) (State, error) {
	if noSync {
		age, known, fields := CacheAge(cache)
		if !StampExists(cache) {
			return State{StateNoCache, "--no-sync given and no cache exists", ExitUnreachableNoCache}, nil
		}
		return State{StateCached, fmt.Sprintf("--no-sync given; cache %s, revision %s",
			agePhrase(age, known), fieldOr(fields, "revision", "unknown")), 0}, nil
	}

	cfg, err := LoadConfigFor(aliasOrDefault(instance))
	if err == nil {
		// 🔴 THE VALIDATOR IS OFFERED ONLY WHEN THERE IS A CACHE TO VALIDATE. A `304`
		// answered to a host holding nothing would be a confirmation of an absence, and
		// this client would have no content and no error — the one outcome the
		// four-state design exists to make impossible. `StoredETag` is written into the
		// cache by the install that produced it, so it cannot outlive its tree; the
		// `StampExists` guard is the belt to that braces, covering a cache root that was
		// half-deleted by hand.
		validator := ""
		if StampExists(cache) {
			validator = StoredETag(cache)
		}
		body, h, notModified, fetchErr := FetchSnapshot(cfg, scope, validator, timeout)
		err = fetchErr
		switch {
		case fetchErr != nil:
			// fall through to the degrade-to-cache ladder below
		case notModified:
			// 🔴 A 304 IS A FOURTH THING AND IT SAYS SO. The cache is not merely being
			// served — it has just been CONFIRMED CURRENT against the pod, which is a
			// stronger claim than `⚠ cached` (the pod could not be reached) and a
			// different one from `live — fetched … just now` (bytes arrived). Both are
			// `live` because every consumer of that name is asking "did we reach the
			// store", and the answer is yes; the DETAIL is where what happened lives,
			// exactly as it already distinguishes `--no-sync given` from `SERVED FROM
			// CACHE` within `cached`. A fifth state name would have to be handled at
			// each of the call sites that compare against `StateLive`, and the one
			// missed would report a successful sync as a failure.
			//
			// ⚠ NOTHING IS WRITTEN. The cache is not touched, so `synced=` still dates
			// the last DOWNLOAD rather than this confirmation — which makes a later
			// offline banner say the cache is older than it has been proven to be.
			// That is the safe direction (this repository's rule is that freshness may
			// never be OVER-claimed) and it is stated rather than fixed, because
			// re-stamping means writing into a live cache that a concurrent sync may be
			// renaming out from under it.
			return State{StateLive, fmt.Sprintf(
				"already current at %s — not modified, snapshot %s",
				cfg.URL, headerOr(h, "X-Store-Snapshot", "UNSTAMPED")), 0}, nil
		default:
			count, installErr := InstallSnapshot(body, cache, h)
			if installErr == nil {
				return State{StateLive, fmt.Sprintf(
					"fetched from %s just now — %d entries, snapshot %s",
					cfg.URL, count, headerOr(h, "X-Store-Snapshot", "UNSTAMPED")), 0}, nil
			}
			err = classifyInstallFailure(cfg.URL, installErr)
		}
	}

	var corruptErr *StoreCorrupt
	if errors.As(err, &corruptErr) {
		return State{}, err
	}

	age, known, fields := CacheAge(cache)
	if !StampExists(cache) {
		// 🔴 NOTHING WAS READ. Not an empty digest, not "nothing recorded".
		return State{StateNoCache, err.Error(), ExitUnreachableNoCache}, nil
	}
	return State{StateCached, fmt.Sprintf("%s — SERVED FROM CACHE, %s, revision %s",
		err.Error(), agePhrase(age, known), fieldOr(fields, "revision", "unknown")), 0}, nil
}

// classifyInstallFailure turns a failure DURING installation into the family it belongs to.
//
// 🔴 THREE FAMILIES, AND THE FIRST TWO ARE BOTH "WE DID NOT RECEIVE A STORE" — so the cache
// is served and the exit is 0. The third is a local disk problem, which is the same family
// again: we have no new store and the old one is still good.
//
// 🔴 THE GZIP SWITCH REOPENED THIS IN A NEW SHAPE ON THE PYTHON SIDE. Before gzip a short
// body raised a tar read error; compressed, a truncated body raises an EOF from the
// decompressor — which was NOT caught, so it escaped as a traceback at exit 1 with a healthy
// cache sitting unused. Go's shapes are `gzip.ErrHeader`, `gzip.ErrChecksum`,
// `io.ErrUnexpectedEOF` and `tar.ErrHeader`, and all four are mapped rather than left to a
// default arm.
func classifyInstallFailure(storeURL string, err error) error {
	var corruptErr *StoreCorrupt
	if errors.As(err, &corruptErr) {
		return err // a REFUSAL, never degraded — see ResolveState.
	}
	// 🔴 THE LAYER, NOT THE ERROR VALUE. Go surfaces a truncated gzip stream and an HTML error
	// page as the SAME `io.ErrUnexpectedEOF` out of `tar.Next`, so classifying on the value
	// reported `<html>…` as a truncated tar where the oracle says `did not return an archive`.
	// `gzipLayerError` records which layer failed at the point that is known.
	var gzipErr gzipLayerError
	if errors.As(err, &gzipErr) {
		return unreachable("%s sent a truncated archive: %s", storeURL, err)
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) || errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrExist) {
		// Disk full / permission denied while extracting. Same family: we have no new
		// store, and the old one is still good.
		return unreachable("could not write the cache: %s", pyOSError(err))
	}
	// ⚠ THE DEFAULT ARM IS THE `did not return an archive` FAMILY, NOT A RE-RAISE. An
	// unclassified failure while unpacking a body the server sent is still "we did not
	// receive a store", and the cache is still the honest answer; escaping here as an
	// unhandled error is the exact defect the two arms above were added to fix, one
	// exception type over.
	return unreachable("%s did not return an archive: %s", storeURL, err)
}

// Banner is the state line printed before every report. 🔴 IT IS NOT OPTIONAL. Running the
// reader locally against a cache DROPS the server's provenance banner, and the reader's "none
// omitted" is a truthful claim about whatever bytes it was pointed at — so the client states,
// in the output itself, which of four states produced it.
func Banner(state, detail string) string { return BannerNamed(state, detail, "") }

// BannerNamed is the state line with an INSTANCE label, and `instance == ""` is the
// single-instance host — which renders exactly what this client has always rendered.
//
// 🔴 ONE SPELLING OF THE MARKER LADDER, BECAUSE A SECOND ONE WOULD DRIFT. The label is the only
// difference between the two forms; duplicating the `🔴`/`⚠` decision beside it would put the
// state vocabulary in two places, and the symptom of those disagreeing is a line that calls the
// same state by two names on one screen.
//
// ⚠ IT IS GATED ON THE INSTANCE COUNT BY ITS CALLERS, NEVER ON A ROUTING TABLE'S PRESENCE. A
// host that has merely written a table still has one place an answer can come from.
func BannerNamed(state, detail, instance string) string {
	marker := ""
	switch state {
	case StateNoCache:
		marker = "🔴"
	case StateCached:
		marker = "⚠"
	}
	name := "cairn"
	if instance != "" {
		name = "cairn[" + instance + "]"
	}
	line := marker + " " + name + ": " + state + " — " + detail
	return trimSpaceBothEnds(line)
}

func trimSpaceBothEnds(s string) string {
	// `str.strip()` on the assembled line, which is what removes the leading space when
	// there is no marker. Spelled out rather than `strings.TrimSpace` so the intent — this
	// is the oracle's `.strip()` on exactly this string — is visible.
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}

// aliasOrDefault is the oracle's `instance or DEFAULT_ALIAS`. 🔴 IT IS NOT COSMETIC:
// `LoadConfigFor("")` would compare `"" == "personal"`, decide this is NOT the default
// instance, and go looking for `instances/.env` — a file nobody writes — so an empty alias
// would fail with "config incomplete" rather than reading the host's long-standing config.
func aliasOrDefault(alias string) string {
	if alias == "" {
		return DefaultAlias
	}
	return alias
}

func fieldOr(fields map[string]string, key, fallback string) string {
	if v, ok := fields[key]; ok {
		return v
	}
	return fallback
}
