package client

// THE EXIT-CODE CONTRACT. 🔴 IT IS PRINTED, SO IT IS A WIRE FACT AND NOT AN IMPLEMENTATION
// DETAIL — `/resume` step 4 and the index-write protocol both branch on these numbers, and
// `cairn doctor --help` renders its own legend from `doctor.ExitLegend`.
//
// Four buckets, and the boundaries between them are the whole point:
//
//	READ outcomes   0  live / cached / scope-empty — content was served
//	                3  store-unreachable, no cache — nothing was read at all
//	                4  `sync` only: not refreshed, though a usable cache survived
//	                5  the server's archive was REFUSED (link, traversal, duplicate, count)
//	USAGE           2  the caller must change the command line
//	WRITE outcomes  6  the store refused the request
//	                7  the write did NOT happen and the reason is not the request
//	                8  the precondition failed (412 on a `put`)
//	                9  `create` only: the entry ALREADY EXISTS
//	DOCTOR          0/9/10 — see `internal/doctor`
//
// 🔴 THE `{0, 9}` OVERLAP WITH `doctor` IS DELIBERATE AND DOCUMENTED, AND THIS PORT MUST NOT
// CHANGE IT. 0 means success in both because that is what 0 means; 9 is doctor's "a check
// MEASURED a problem" and `create`'s "already exists". Neither is ambiguous where a code is
// actually read — at ONE call site, which already knows the verb it invoked. `doctor` never
// creates an entry and `create` never runs diagnostics.
//
// 🔴 AND THE LEDGER THAT PINS THAT SET IS PYTHON-ONLY UNTIL SOMETHING READS THIS FILE.
// `tests/test_cairn_doctor.py` DISCOVERS both operands — doctor's codes and the client's —
// but it discovers the CLIENT's by walking the `cairn` script's AST, which cannot see a
// compiled binary. That is a real gap, not a detail: a Go-only code colliding with 10 would
// leave that ledger green. It is closed by `ExitCodes()` below, which is read out of the
// RUNNING binary by `cairn -exit-codes` and compared against the Python set by
// `tests/test_cairn_doctor.py::TestTheSharedExitCodeSetCoversTheGoClientToo` — the same
// shape, and for the same reason, as `api.DeclaredRoutes()` and `cairn-server -routes`.
const (
	ExitOK = 0
	// ExitUsage is the third bucket, not a read outcome: the caller must change the
	// command line. An earlier draft of the oracle's own comment folded it in with the
	// reads and contradicted the bullet two paragraphs below it.
	ExitUsage = 2
	// ExitUnreachableNoCache is "nothing was read at all" — never an empty digest and
	// never "nothing recorded".
	ExitUnreachableNoCache = 3
	// ExitRefreshFailed is `sync` ONLY: the store was not reached but a usable cache
	// survived. Distinct from 3 so a caller can tell "stale but serviceable" from
	// "nothing at all". A `sync` that exited 0 on an outage is how a timer reports
	// success forever while the cache silently ages out.
	ExitRefreshFailed = 4
	// ExitCorrupt is "the server answered and what it sent is not a store we accept".
	// Distinct from every other code so it can never be read as an outage or a clean run.
	ExitCorrupt = 5

	// ExitWriteRefused — the STORE refused and the caller must change the request.
	// Retrying byte-for-byte cannot help.
	ExitWriteRefused = 6
	// ExitWriteUnreachable — the write did NOT happen and the reason is not the request.
	// Distinct from 6 because a retry is exactly the right response to this one and
	// exactly the wrong response to that one.
	ExitWriteUnreachable = 7
	// ExitWritePrecondition — 412: the entry moved under the revision this write was
	// based on. Its own code because the remedy is unique (re-sync, re-derive, re-apply).
	ExitWritePrecondition = 8
	// ExitWriteExists — `create` only: the entry ALREADY EXISTS, so nothing was created
	// and nothing was overwritten. 🔴 ITS OWN CODE EVEN THOUGH THE SERVER ANSWERS THE SAME
	// 412 AS 8, because the two remedies are opposites and the identical retry loops
	// forever. They are told apart by `X-Store-Status`, which is the only place on the
	// wire the difference exists.
	ExitWriteExists = 9
)

// ExitCodes is the client's own code set, as data, so a test can DISCOVER it instead of
// hand-listing it.
//
// 🔴 A HAND LIST IS BLIND TO A CONSTANT ADDED AFTER IT WAS WRITTEN, which is exactly the
// defect the Python side's `cairn_source.module_constants` was extracted to close. Keeping
// the table beside the constants means a new code has to be added in two adjacent lines,
// and `TestTheExitTableNamesEveryExitConstant` fails when it is added in one.
func ExitCodes() map[string]int {
	return map[string]int{
		"EXIT_OK":                    ExitOK,
		"EXIT_USAGE":                 ExitUsage,
		"EXIT_UNREACHABLE_NO_CACHE":  ExitUnreachableNoCache,
		"EXIT_REFRESH_FAILED":        ExitRefreshFailed,
		"EXIT_CORRUPT":               ExitCorrupt,
		"EXIT_WRITE_REFUSED":         ExitWriteRefused,
		"EXIT_WRITE_UNREACHABLE":     ExitWriteUnreachable,
		"EXIT_WRITE_PRECONDITION":    ExitWritePrecondition,
		"EXIT_WRITE_EXISTS":          ExitWriteExists,
	}
}

// The four states a READ can be in, and 🔴 THE FOURTH IS THE ONE THAT MUST NEVER BE
// COLLAPSED INTO THE THIRD. `scope-empty` and `store-unreachable` both "print no entries",
// and one of them is a lie.
const (
	StateLive    = "live"
	StateCached  = "cached"
	StateEmpty   = "scope-empty"
	StateNoCache = "store-unreachable, no cache"
)
