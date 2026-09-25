package store

import (
	"errors"
	"io/fs"
	"syscall"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// osErrorTypeName is CPython's exception CLASS NAME for an OS error, which the
// store-unreachable sentences interpolate as `{type(exc).__name__}`.
//
// ✅ THAT RESIDUAL IS CLOSED, AND THE PARAGRAPH IT REPLACES IS DELETED RATHER THAN LEFT TO READ
// AS COVERAGE NOBODY TOOK. It said the exception's own `[Errno N] text: 'path'` tail was "NOT
// reproduced byte-for-byte here" and that "a future round that wants it will have to add a corpus
// case that reaches it". That round happened — twice, at two depths — and the cases are parity
// rows rather than conformance ones, which is the distinction the old wording could not make:
//
//   - `validate-unreadable-entry` / `recall-unreadable-entry` (#111) chmod a CACHED ENTRY FILE
//     to 000 for the measured run and compare stdout, stderr AND the exit code between the two
//     clients, so the whole sentence — type name and `[Errno 13] Permission denied: '<path>'`
//     tail included — is compared BYTE FOR BYTE. `PyOSError` exists for that tail.
//   - `validate-unreadable-scope-dir` / `recall-unreadable-scope-dir` (#119) do the same one
//     directory up, over a cached SCOPE DIRECTORY at 000.
//
// The mode cannot be COMMITTED (git does not preserve 000, so a fixture would arrive readable in
// CI), which is why these are harness-built rows and not corpus files — the old paragraph's "the
// fixture store is fully readable" was a fact about the corpus, not a limit on the gate. A
// CONTENT-FLOOR sentinel per family, each keyed on the row's own `Case` field, refuses to vouch
// for a run in which either `chmod` stopped happening.
//
// ⚠ WHAT IS STILL UNPINNED IS THE `X-Store-*` HEADER SIDE, and that claim survives unchanged: no
// conformance case reaches these sentences, so what the SERVER pins is the STATUS and the header
// set (`store-unreachable`, `X-Store-Exit: 3`). It is the two CLIENTS' rendering that is now
// gated, which is where this function's output goes.
func osErrorTypeName(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "PermissionError"
	case errors.Is(err, fs.ErrNotExist):
		return "FileNotFoundError"
	case errors.Is(err, syscall.EISDIR):
		return "IsADirectoryError"
	case errors.Is(err, syscall.ENOTDIR):
		return "NotADirectoryError"
	}
	return "OSError"
}

// ignoredStatErrnos is `pathlib._IGNORED_ERRNOS`, measured on the pinned interpreter
// rather than transcribed from documentation:
//
//	>>> [errno.errorcode[e] for e in pathlib._IGNORED_ERRNOS]
//	['ENOENT', 'ENOTDIR', 'EBADF', 'ELOOP']
//
// 🔴 IT IS THE **WHOLE** DIFFERENCE BETWEEN "NOT A DIRECTORY" AND "I COULD NOT LOOK",
// and it is why `is_dir()`/`is_file()` cannot be spelled as "err == nil && …" in Go.
// Those predicates fail in two different ways: they return False for these four, and
// they RAISE for every other errno. A port that collapses both into `continue` claims
// an absence it did not establish, which is the one claim this store is built not to
// make.
//
// ⚠ `_IGNORED_WINERRORS` EXISTS TOO AND IS DELIBERATELY NOT PORTED. This server runs
// on Linux in a container; a Windows error-code table here would be untested code
// asserting a platform nobody builds for.
var ignoredStatErrnos = []syscall.Errno{
	syscall.ENOENT,
	syscall.ENOTDIR,
	syscall.EBADF,
	syscall.ELOOP,
}

// isIgnoredStatErrno reports whether a stat failure is one CPython's path predicates
// swallow into a plain `False`, as opposed to one they raise.
func isIgnoredStatErrno(err error) bool {
	for _, e := range ignoredStatErrnos {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// DecodeReplace is Python's `read_text(encoding="utf-8", errors="replace")`.
//
// It delegates to `pytext`, which is where the CPython string rules live: the query
// parser in `internal/api` needs the SAME decode for a percent-escaped byte run, and a
// second copy is the duplicated predicate this codebase keeps finding. See
// `pytext.DecodeUTF8Replace` for why it is one replacement per invalid BYTE.
func DecodeReplace(data []byte) string {
	return pytext.DecodeUTF8Replace(data)
}
