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

// decodeReplace is Python's `read_text(encoding="utf-8", errors="replace")`.
//
// It delegates to `pytext`, which is where the CPython string rules live: the query
// parser in `internal/api` needs the SAME decode for a percent-escaped byte run, and a
// second copy is the duplicated predicate this codebase keeps finding. See
// `pytext.DecodeUTF8Replace` for why it is one replacement per invalid BYTE.
func decodeReplace(data []byte) string {
	return pytext.DecodeUTF8Replace(data)
}
