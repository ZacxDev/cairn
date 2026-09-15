package client

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZacxDev/cairn/internal/store"
)

const (
	// MaxUnpackedBytes is a ceiling on what a snapshot may unpack to.
	//
	// 🔴 GZIP REMOVED THE NATURAL BOUND: before it, the response body limited what could
	// be extracted. An audit MEASURED a 203,934-byte body expanding to 209,715,200 bytes
	// on disk — 1028× — with the client reporting `live … 1 entries`, exit 0. Every other
	// hostile-archive guard here exists for a server we do not fully trust; a size bomb is
	// the same threat model and was the one hole.
	MaxUnpackedBytes = 256 * 1024 * 1024
	// MaxMembers is the companion ceiling on MEMBER COUNT, 🔴 because bytes alone bound
	// nothing: a member's declared size is 0 for an empty file, so a byte ceiling never
	// fires on an INODE bomb. Measured against the pre-fix client: a 282,282-byte body
	// carrying 60,000 zero-length `*.md` members wrote 60,000 files into the cache in
	// 3.7 s and reported `live … 60000 entries`, exit 0. `X-Store-Entries` cannot help,
	// since the same hostile server sets it.
	MaxMembers = 50_000

	// OrphanGraceSeconds is why reaping is safe at the START of a sync: a staging
	// directory younger than an hour may belong to a CONCURRENT sync, and deleting it
	// would reintroduce the very race the unique names removed. Anything older cannot be
	// live — the whole operation takes well under a second.
	OrphanGraceSeconds = 3600
)

// safeMemberName is true when `name` stays inside the extraction root.
//
// 🔴 NOT `strings.Contains(name, "..")`. That was the oracle's first version and it is
// over-broad: a legitimate entry called `widget-cfg/a..b.md` contains `..` as a SUBSTRING
// and aborted the entire sync, which then rendered as an outage. The server puts no
// constraint on entry filenames, so this is reachable with an ordinary file. Decide on path
// COMPONENTS, which is the actual property.
func safeMemberName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
		return false
	}
	// ⚠ THE PARTS ARE SPLIT ON `/`, NOT BY `filepath.Clean`+`Split`. Cleaning would
	// REMOVE a `..` by resolving it, which is the one thing this predicate must be able to
	// see. A tar member name is a `/`-joined string on every platform, so splitting on `/`
	// is also the right frame.
	parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if p == ".." || p == "" {
			return false
		}
	}
	return true
}

// ReapOrphans removes stale `<cache>.new-*` / `<cache>.old-*` trees and returns how many.
//
// 🔴 THE PER-RUN STAGING NAME TRADED ONE BUG FOR A LEAK. The pre-fix version used a FIXED
// staging name and removed it at the top of every run, which raced (that was the truncation
// bug) but was self-healing. Unique names fixed the race and removed the healing: an audit
// SIGKILLed three syncs mid-run and the orphaned trees were still there afterwards. A
// `defer` covers a panic; it does not cover SIGKILL, a power cut, or an OOM.
func ReapOrphans(cache string) int {
	cutoff := time.Now().Add(-OrphanGraceSeconds * time.Second)
	parent, name := filepath.Dir(cache), filepath.Base(cache)
	reaped := 0
	for _, prefix := range []string{name + ".new-", name + ".old-"} {
		matches, err := filepath.Glob(filepath.Join(parent, prefix+"*"))
		if err != nil {
			continue
		}
		for _, path := range matches {
			// `Lstat`, not `Stat`: a SYMLINK named like a staging dir dereferences under
			// a following stat, and removing a symlink's TARGET tree is not what this
			// means — while `RemoveAll` on the link itself removes only the link. Getting
			// this wrong makes the count the only evidence and the count wrong.
			info, err := os.Lstat(path)
			if err != nil || info.ModTime().After(cutoff) {
				continue
			}
			switch {
			case info.Mode()&fs.ModeSymlink != 0:
				if os.Remove(path) != nil {
					continue
				}
			case info.IsDir():
				if os.RemoveAll(path) != nil {
					continue
				}
			default:
				continue
			}
			reaped++
		}
	}
	return reaped
}

// openArchive is `tarfile.open(mode="r")` — TRANSPARENT compression detection.
//
// 🔴 THE SNAPSHOT ROUTE SHIPS GZIP AND THE OLDER CONTRACT DID NOT, so a client that assumed
// either one is wrong against the other. The gzip magic is two bytes and sniffing it is what
// `mode="r"` does; hardcoding `r:gz` would refuse an uncompressed archive, which is what a
// byte-identity harness replaying a recorded body is most likely to hand it.
func openArchive(body []byte) (*tar.Reader, error) {
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		return tar.NewReader(zr), nil
	}
	return tar.NewReader(bytes.NewReader(body)), nil
}

// checkCeilings is the FIRST of two passes: headers only, no body read.
//
// 🔴 BOTH CEILINGS ARE CHECKED AGAINST THE WHOLE MEMBER LIST BEFORE ANYTHING IS WRITTEN,
// which is the oracle's order (`tar.getmembers()`, then the two `if`s, then the extract
// loop) and the only order that bounds a bomb: a per-member check taken as you extract has
// already written `n−1` of them.
//
// 🔴 `sum`, NOT `max`. A `max`-based ceiling passes a 1000 × 250 MB archive, and the
// single-member bomb fixture cannot tell the two apart — that mutant SURVIVED a whole suite
// on the Python side.
//
// ⚠ TWO PASSES, BECAUSE `tar.Reader` IS FORWARD-ONLY. CPython's `getmembers()` buys the
// same thing by seeking; here the archive is already in memory, so a second reader over the
// same bytes costs a re-inflate and no more. The alternative — buffering every body to
// decide afterwards — would make the byte ceiling a comment, since the buffer it is meant to
// bound would already be full.
func checkCeilings(body []byte) error {
	reader, err := openArchive(body)
	if err != nil {
		return err
	}
	var declared int64
	members := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		declared += header.Size
		members++
	}
	if declared > MaxUnpackedBytes {
		return corrupt("archive unpacks to %d bytes, over the %d-byte ceiling "+
			"(%d bytes on the wire)", declared, MaxUnpackedBytes, len(body))
	}
	if members > MaxMembers {
		return corrupt("archive holds %d members, over the %d-member ceiling "+
			"(%d bytes on the wire)", members, MaxMembers, len(body))
	}
	return nil
}

// InstallSnapshot replaces the cache with the tar's contents and returns the `*.md` count.
//
// 🔴 CONCURRENT SYNCS USED TO SILENTLY TRUNCATE THIS CACHE. The staging directory had a
// FIXED name and was removed at the top of every run, so two syncs shared it: measured over
// 10 trials on a 305-entry store, 3 left a SHORT cache (183, 256, 292 entries) with no error.
// The reader then rendered that partial tree under its own "none omitted" header — the
// completeness lie, arriving by the one route this function exists to close.
//
// Two things make it safe: a PER-RUN staging directory (so runs cannot share one) and an
// exclusive LOCK around the swap (so the rename pair cannot interleave). The lock is
// advisory and held only for the swap, so a slow download never blocks another reader.
func InstallSnapshot(body []byte, cache string, headers http.Header) (int, error) {
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		return 0, err
	}
	ReapOrphans(cache)
	staging, err := os.MkdirTemp(filepath.Dir(cache), filepath.Base(cache)+".new-")
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(staging)
		}
	}()

	if err := checkCeilings(body); err != nil {
		return 0, err
	}
	reader, err := openArchive(body)
	if err != nil {
		return 0, err
	}

	count := 0
	seen := map[string]struct{}{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, err
		}
		name := header.Name
		// 🔴 A HOSTILE OR CORRUPT ARCHIVE IS NOT AN OUTAGE. Each of these is a
		// `StoreCorrupt`, never a `StoreUnreachable`, so it can never be quietly absorbed
		// into "serving from cache". THE ORDER IS THE CONTRACT: link, then not-a-file,
		// then traversal, then duplicate — each reachable by a member no earlier guard
		// rejects, and the message a caller sees for a member that is two of them at once
		// is the first one's.
		switch header.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			return 0, corrupt("tar member is a link: %s", store.PyRepr(name))
		case tar.TypeReg:
		default:
			return 0, corrupt("tar member is not a regular file: %s", store.PyRepr(name))
		}
		if !safeMemberName(name) {
			return 0, corrupt("tar member escapes the root: %s", store.PyRepr(name))
		}
		if _, dup := seen[name]; dup {
			return 0, corrupt("duplicate tar member: %s", store.PyRepr(name))
		}
		seen[name] = struct{}{}
		target := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, err
		}
		// 🔴 MODE 0644, NOT THE ARCHIVE'S. `filter="data"` on the oracle's side strips
		// setuid/setgid/sticky bits and clamps the mode for exactly this reason: a server
		// we do not fully trust must not decide the permissions of files on this disk.
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return 0, err
		}
		// `header.Size` bounds the copy, and `tar.Reader` will not hand over more than the
		// header declared — so the declared size is not being TRUSTED here, it is being
		// used as the same bound the reader already enforces.
		if _, err := io.Copy(out, reader); err != nil {
			out.Close()
			return 0, err
		}
		if err := out.Close(); err != nil {
			return 0, err
		}
		// 🔴 THE MEMBER'S MTIME IS RESTORED, AND DROPPING IT WAS A MEASURED DEFECT OF EXACTLY
		// THE KIND THIS WHOLE PROJECT EXISTS TO PREVENT. The reader orders its index by entry
		// mtime, so a cache whose files all carry the extraction time is ordered by TAR ORDER —
		// and the parity harness's `recall-digest` row caught it as a listing in the opposite
		// order with a DIFFERENT featured entry: no error, no missing entry, and it reads as a
		// stale cache. `tarfile.extract` calls `os.utime` from the header; this is that call.
		//
		// ⚠ SUB-SECOND PRECISION COMES FROM THE PAX RECORD, WHICH IS WHY IT SURVIVES AT ALL.
		// The ustar mtime field is whole seconds; the snapshot writer emits PAX, so
		// `header.ModTime` carries the fraction — and the fraction is what decides the tie-break
		// for two entries written in the same second. Measured on this world: `widget-cfg`
		// (.25) and `ledger-svc` (.75) share a whole second and order correctly only with the
		// fraction preserved.
		if !header.ModTime.IsZero() {
			if err := os.Chtimes(target, header.ModTime, header.ModTime); err != nil {
				return 0, err
			}
		}
		if strings.HasSuffix(name, ".md") {
			count++
		}
	}

	// 🔴 THE SERVER'S OWN COUNT, CHECKED. The server-side comment claims a truncated
	// transfer is "visible as a disagreement"; it is only visible if somebody compares, so
	// this is that comparison.
	if declared := headers.Get("X-Store-Entries"); declared != "" && isDigits(declared) {
		if n, convErr := strconv.Atoi(declared); convErr == nil && n != count {
			return 0, corrupt("server declared %s entries, archive held %d", declared, count)
		}
	}

	stamp := strings.Join([]string{
		fmt.Sprintf("synced=%d", time.Now().Unix()),
		"revision=" + headerOr(headers, "X-Store-Revision", "unknown"),
		"snapshot=" + headerOr(headers, "X-Store-Snapshot", "UNSTAMPED"),
		fmt.Sprintf("entries=%d", count),
		// Recorded so a filtered cache can never be mistaken for a complete one. Today
		// the CLI only ever writes ALL.
		"coverage=ALL",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(staging, SyncStamp), []byte(stamp), 0o644); err != nil {
		return 0, err
	}

	if err := swapIntoPlace(staging, cache); err != nil {
		return 0, err
	}
	committed = true
	return count, nil
}

// swapIntoPlace is the locked rename pair. 🔴 THE LOCK IS THE ONLY THING STOPPING TWO
// CONCURRENT SYNCS FROM INTERLEAVING THE TWO RENAMES, and an interleave is what produced the
// `FileExistsError` / `Directory not empty` half of the truncation incident.
func swapIntoPlace(staging, cache string) error {
	lockPath := filepath.Join(filepath.Dir(cache), filepath.Base(cache)+".lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	// 🔴 THE SAME `flock` THE ORACLE TAKES, ON THE SAME PATH, WHICH IS WHAT MAKES THE TWO
	// CLIENTS MUTUALLY EXCLUSIVE RATHER THAN MERELY EACH SAFE. Two implementations syncing
	// one cache root is precisely the parity harness's own workload, and a Go client
	// holding a different lock (or none) would reintroduce the truncation race across
	// implementations while each half looked correct on its own.
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	var retired string
	if _, err := os.Stat(cache); err == nil {
		retired, err = os.MkdirTemp(filepath.Dir(cache), filepath.Base(cache)+".old-")
		if err != nil {
			return err
		}
		// MkdirTemp reserved the name; the rename needs it free.
		if err := os.Remove(retired); err != nil {
			return err
		}
		if err := os.Rename(cache, retired); err != nil {
			return err
		}
	}
	if err := os.Rename(staging, cache); err != nil {
		return err
	}
	if retired != "" {
		os.RemoveAll(retired)
	}
	return nil
}

func headerOr(h http.Header, name, fallback string) string {
	if v := h.Get(name); v != "" {
		return v
	}
	return fallback
}

// isDigits is `str.isdigit()` for the ASCII subset. ⚠ `str.isdigit()` is TRUE for U+0660
// and friends; this is not, and the difference cannot matter for a header this server
// writes. Named so nobody reads `isDigits` as a faithful port of the method.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
