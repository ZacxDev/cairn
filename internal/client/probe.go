package client

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/ZacxDev/cairn/internal/doctor"
)

// ProbeStore is ONE snapshot fetch, read for facts and THROWN AWAY. Never installed.
//
// 🔴 IT REUSES `FetchSnapshot` RATHER THAN BUILDING A SECOND REQUEST. That keeps the mandatory
// User-Agent — and the 403 measurement behind it — on this path without a second site to forget
// it at.
//
// 🔴 IT NEVER CALLS `InstallSnapshot`. A diagnostic that repaired the cache would destroy the
// staleness it was run to measure — and it would be the one command you must not run twice.
// 🔴 IT SENDS NO VALIDATOR, AND THE EMPTY ARGUMENT IS THE WHOLE STATEMENT. A conditional
// probe could be answered 304, and this function's entire job is to COUNT WHAT THE POD
// WOULD SEND — visible scopes and visible entries, read out of the archive itself. A
// diagnostic that accepted "nothing changed" would report the cache's contents as the
// pod's and call the two in agreement by construction, which is the shape of check that
// agrees with itself.
func ProbeStore(cfg Config, timeout int) doctor.PodFacts {
	body, headers, _, err := FetchSnapshot(cfg, "", "", timeout)
	if err != nil {
		facts := doctor.PodFacts{Reason: err.Error()}
		var unreachableErr *StoreUnreachable
		if errors.As(err, &unreachableErr) {
			facts.HTTPStatus = unreachableErr.HTTPStatus
		}
		return facts
	}

	snapshot := headers.Get("X-Store-Snapshot")
	scopes := map[string]struct{}{}
	reader, openErr := openArchive(body)
	if openErr != nil {
		return doctor.PodFacts{Reason: fmt.Sprintf(
			"%s answered, but the archive could not be read: %s", cfg.URL, openErr)}
	}
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return doctor.PodFacts{Reason: fmt.Sprintf(
				"%s answered, but the archive could not be read: %s", cfg.URL, nextErr)}
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		parts := strings.Split(header.Name, "/")
		if len(parts) == 2 && strings.HasSuffix(parts[1], ".md") {
			scopes[parts[0]] = struct{}{}
		}
	}

	facts := doctor.PodFacts{Reached: true, SnapshotHeader: snapshot}
	// 🔴 NO FALLBACK TO THE ARCHIVE'S OWN COUNT. Without `X-Store-Entries` the only number
	// available is how many members ARRIVED, which is exactly the side of the comparison a
	// truncated transfer moves — substituting it would make the cache-vs-pod check agree with
	// itself and report OK over a short answer. Absent header ⇒ the count is UNMEASURED, with
	// that as the stated reason.
	if raw := headers.Get("X-Store-Entries"); raw != "" && isDigits(raw) {
		if n, convErr := strconv.Atoi(raw); convErr == nil {
			facts.VisibleEntries = &n
		}
	}
	// 🔴 `entry-files=` OUT OF THE STORE-WIDE HEADER, AND THE STORE-WIDE-NESS IS THE POINT. The
	// server's freshness stamp counts the WHOLE served store, not the caller's slice — a
	// documented, deliberate residual count leak — so the gap between it and `X-Store-Entries`
	// is the only client-side evidence that entries exist which this credential cannot reach.
	for _, field := range strings.Fields(snapshot) {
		key, value, _ := strings.Cut(field, "=")
		if key == "entry-files" && isDigits(value) {
			if n, convErr := strconv.Atoi(value); convErr == nil {
				facts.StoreWideEntries = &n
			}
		}
	}
	names := make([]string, 0, len(scopes))
	for name := range scopes {
		names = append(names, name)
	}
	sort.Strings(names)
	facts.VisibleScopes = names
	return facts
}
