package store

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unicode"
	"unicode/utf8"
)

// PyOSError renders a Go file-system error the way CPython's `str(OSError)` renders it:
//
//	[Errno 13] Permission denied: '/path/to/thing'
//
// 🔴 IT LIVES HERE, BESIDE `PyRepr`, BECAUSE TWO PACKAGES NEED IT AND A SECOND COPY WOULD
// DRIFT. The CLI's cache-write refusal and `doctor`'s "could not be read" detail both
// interpolate one, and the parity gate compares those sentences BYTE for byte against the
// Python client's. One rule, one place — the same argument the `timeouts` module records for
// the predicate whose two copies disagreed about `bool`.
//
// 🔴 THE `strerror` TEXT IS GO'S OWN, CAPITALISED — MEASURED, NOT ASSUMED. Go's
// `syscall.Errno.Error()` and CPython's `os.strerror()` were compared over SIXTEEN errnos
// (EACCES, ENOENT, EISDIR, ENOTDIR, ENOSPC, ELOOP, EEXIST, ENOTEMPTY, EPERM, EROFS,
// ENAMETOOLONG, EBADF, EINVAL, EIO, EMFILE, EXDEV) and the ONLY difference at every one of
// them is the case of the first letter — including `Input/output error`, whose interior
// capital would break a naive title-case and does not break this, and `Read-only file
// system`, whose second word CPython also leaves lower-case. Two points is the rule; sixteen
// were taken because the whole message is being claimed, not one row of it.
//
// ⚠ WHAT THIS IS NOT: a general `repr` of every OSError CPython can raise. An error carrying
// no errno — a Go-level wrapper, a cancelled context — has no `[Errno N]` form, so the Go
// text is returned unchanged. That fallback is a NAMED residual difference rather than a
// silent one; see `tests/parity/README.md`.
func PyOSError(err error) string {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return err.Error()
	}
	text := CapitalizeFirst(errno.Error())
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return fmt.Sprintf("[Errno %d] %s: %s", int(errno), text, PyRepr(pathErr.Path))
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return fmt.Sprintf("[Errno %d] %s: %s -> %s", int(errno), text,
			PyRepr(linkErr.Old), PyRepr(linkErr.New))
	}
	return fmt.Sprintf("[Errno %d] %s", int(errno), text)
}

// CapitalizeFirst upper-cases the first RUNE and touches nothing else.
//
// ⚠ DELIBERATELY NOT A WORD-BY-WORD TITLE FOLD. `Input/output error` and `Read-only file
// system` both have lower-case interior words that CPython keeps, so a per-word fold would
// differ from the oracle at two of the sixteen measured errnos.
func CapitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}
