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
// ⚠ A KNOWN, UNPINNED RESIDUAL — stated because "it is not in the normalization
// table" must not read as "somebody measured it". No case in the conformance
// corpus reaches these sentences: the fixture store is fully readable, so the
// only inputs that produce one are a mode-000 entry, a FIFO named `*.md` or a
// vanished store — none of which the corpus builds. What IS pinned is the STATUS
// and the header set (`store-unreachable`, `X-Store-Exit: 3`), which is the part a
// client branches on. The exception's own `[Errno N] text: 'path'` tail is NOT
// reproduced byte-for-byte here and a future round that wants it will have to add
// a corpus case that reaches it, which is the honest way round.
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
