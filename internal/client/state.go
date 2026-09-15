package client

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
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
func ResolveState(cache string, noSync bool, scope string, timeout int) (State, error) {
	if noSync {
		age, known, fields := CacheAge(cache)
		if !StampExists(cache) {
			return State{StateNoCache, "--no-sync given and no cache exists", ExitUnreachableNoCache}, nil
		}
		return State{StateCached, fmt.Sprintf("--no-sync given; cache %s, revision %s",
			agePhrase(age, known), fieldOr(fields, "revision", "unknown")), 0}, nil
	}

	cfg, err := LoadConfig()
	if err == nil {
		body, h, fetchErr := FetchSnapshot(cfg, scope, timeout)
		err = fetchErr
		if fetchErr == nil {
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
	switch {
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF),
		errors.Is(err, gzip.ErrChecksum):
		return unreachable("%s sent a truncated archive: %s", storeURL, err)
	case errors.Is(err, gzip.ErrHeader), errors.Is(err, tar.ErrHeader):
		// 🔴 A 200 THAT IS NOT A TAR. Realistic in production precisely because this
		// host's edge answers 200 with HTML for some clients (see the User-Agent note).
		return unreachable("%s did not return an archive: %s", storeURL, err)
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
func Banner(state, detail string) string {
	marker := ""
	switch state {
	case StateNoCache:
		marker = "🔴"
	case StateCached:
		marker = "⚠"
	}
	line := marker + " cairn: " + state + " — " + detail
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

func fieldOr(fields map[string]string, key, fallback string) string {
	if v, ok := fields[key]; ok {
		return v
	}
	return fallback
}
