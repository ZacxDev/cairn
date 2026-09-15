package client

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// =============================================================================
// THE WRITE PATH. Everything in `state.go` reads.
// =============================================================================
//
// 🔴 WRITE-THROUGH, NO SPOOL, NO CACHE FALLBACK. The pod is the authority; the local cache is
// a read-through copy with no authority of its own. A write that cannot reach the pod is
// REFUSED, loudly, at a non-zero exit — never queued, never written to the cache, never
// reported as done. Reads keep working offline off the cache; only writes stop. The reason
// that trade is acceptable is that a refused write costs one retry while a silently-local
// write costs the bullet the next seed overwrites.
//
// 🔴 THE ACTOR IS NOT SENT AND CANNOT BE. The server derives it from the token that
// authenticated and DISCARDS any `actor` key in the body, so this client has no `--actor`
// flag: offering one would imply a control the caller does not have.

// WriteRefused is "the store answered, and the answer is a refusal". It carries its own code.
//
// 🔴 SEPARATE FROM `StoreUnreachable` FOR THE SAME REASON `StoreCorrupt` IS. "I could not
// ask" and "I asked and was told no" have different remedies — retry vs change the request —
// and a client that collapses them turns a permanently-malformed bullet into an infinite
// retry, or a transient outage into "the store rejected this, give up".
type WriteRefused struct {
	ExitCode int
	Status   string
	Detail   string
}

func (e *WriteRefused) Error() string { return e.Detail }

// writeStatusExits is HTTP status -> our exit code.
//
// 🔴 A TABLE, NOT AN `if` LADDER, so a test can walk it against the statuses the server
// actually emits on a write route. An unmapped code is NOT silently treated as success:
// `classifyWrite` falls through to `ExitWriteRefused` and SAYS the code was unrecognised,
// which is loud and wrong-in-the-safe-direction — the caller is told the write did not land.
var writeStatusExits = map[int]int{
	400: ExitWriteRefused,      // malformed body / ambiguous ref
	401: ExitWriteRefused,      // no or bad credential
	403: ExitWriteRefused,      // edge refusal (see the User-Agent note)
	404: ExitWriteRefused,      // scope-unknown or ref-unknown
	405: ExitWriteRefused,      // read-only image: the write path is NOT deployed
	412: ExitWritePrecondition, // If-Match named a revision the entry no longer has
	422: ExitWriteRefused,      // entry-shape: no `## Nuance / work-history`
	428: ExitWriteRefused,      // precondition required (a PUT with no If-Match)
	429: ExitWriteUnreachable,  // rate limited — a retry IS the right response
	// 🔴 500 IS A RETRY, NOT A "FIX YOUR REQUEST". The server turns any unhandled handler
	// exception into a 500, so it IS reachable on a write route. It was absent from the
	// oracle's table once and fell through to the "unrecognised" arm, which is
	// `ExitWriteRefused` — telling the caller to change a byte-identical request that would
	// very likely succeed on a retry. That fallthrough is labelled
	// wrong-in-the-safe-direction, and that is true for landed/not-landed and FALSE for
	// retry/don't-retry, which is the exact distinction 6 and 7 exist to carry.
	500: ExitWriteUnreachable,
	502: ExitWriteUnreachable, // a bad gateway is the edge, not the request
	503: ExitWriteUnreachable, // the server could not read its own store
	504: ExitWriteUnreachable,
}

// writeStatusTokenExits is a SECOND table, keyed on the server's `X-Store-Status`, consulted
// FIRST.
//
// It exists because one HTTP status can carry two outcomes with opposite remedies: `412` is
// `precondition-failed` (the entry moved — retry after a re-sync) on a `put`, and
// `already-exists` (it is there — do not retry) on a `create`. The code table cannot express
// that, and collapsing the two would give a `create` caller an exit that says "re-derive and
// try again" for a condition no retry can change.
//
// 🔴 A TOKEN NOT IN THIS TABLE FALLS THROUGH TO THE CODE TABLE, never to a pass. The token is
// advisory routing, not the verdict.
var writeStatusTokenExits = map[string]int{
	"already-exists": ExitWriteExists,
}

func classifyWrite(code int, status, detail string) *WriteRefused {
	if byToken, ok := writeStatusTokenExits[status]; ok {
		return &WriteRefused{ExitCode: byToken, Status: status, Detail: detail}
	}
	exitCode, ok := writeStatusExits[code]
	if !ok {
		named := status
		if named == "" {
			named = fmt.Sprintf("http-%d", code)
		}
		return &WriteRefused{
			ExitCode: ExitWriteRefused,
			Status:   named,
			Detail: fmt.Sprintf("unrecognised HTTP %d on a write route — treating the "+
				"write as NOT LANDED: %s", code, detail),
		}
	}
	named := status
	if named == "" {
		named = fmt.Sprintf("http-%d", code)
	}
	return &WriteRefused{ExitCode: exitCode, Status: named, Detail: detail}
}

// SendWrite is ONE request against a write route. It returns `(headers, body)` on 200, a
// `*WriteRefused` on any non-200 the server produced, and a `*StoreUnreachable` when there was
// no answer at all. Nothing else escapes.
func SendWrite(cfg Config, method, path string, body []byte, timeout int,
	extra map[string]string) (http.Header, []byte, error) {
	if bad := UnboundedTimeoutReason(timeout); bad != "" {
		// 🔴 THE SAME REFUSAL THE READ PATH MAKES, FOR A STRONGER REASON. An unbounded
		// WRITE is worse than an unbounded read: the request may already have been applied,
		// so a caller that eventually gives up cannot tell whether it landed.
		return nil, nil, unreachable("refusing to write to %s: %s", cfg.URL, bad)
	}
	req, err := http.NewRequest(method, cfg.URL+path, bytes.NewReader(body))
	if err != nil {
		return nil, nil, unreachable("%s unreachable: %s", cfg.URL, err)
	}
	applyStandardHeaders(req, cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	for name, value := range extra {
		req.Header.Set(name, value)
	}
	resp, err := httpClient(timeout).Do(req)
	if err != nil {
		return nil, nil, unreachable("%s unreachable: %s", cfg.URL, urlErrorReason(err))
	}
	defer resp.Body.Close()
	answer, readErr := io.ReadAll(resp.Body)
	// 🔴 ANY 2xx IS SUCCESS, AND `== 200` WAS A MEASURED DEFECT. `create` answers **201**, and
	// `urllib`'s `HTTPErrorProcessor` raises only for a code outside 200–299 — so the oracle
	// prints the created entry and exits 0 while a `!= 200` test here fell through
	// `classifyWrite`'s unrecognised-code arm and reported `unrecognised HTTP 201 on a write
	// route — treating the write as NOT LANDED` at exit 6. A SUCCESSFUL create reported as a
	// refusal, which is the worst direction for this verb: the caller's remedy for a 6 is to
	// change the request, and the entry is already there.
	//
	// Found by the parity harness's `create-ok` row, not by reading the code — the status is in
	// no conformance golden this client replays, and every write test written against `append`
	// passes on a 200.
	if resp.StatusCode/100 != 2 {
		detail := ""
		if readErr == nil {
			detail = oneLine(answer, 400)
		}
		if detail == "" {
			detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return nil, nil, classifyWrite(resp.StatusCode, resp.Header.Get("X-Store-Status"), detail)
	}
	if readErr != nil {
		return nil, nil, unreachable("%s unreachable: %s", cfg.URL, readErr)
	}
	return resp.Header, answer, nil
}
