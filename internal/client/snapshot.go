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

	// SyncETagFile holds the validator for the archive this cache was built from, so
	// the next sync can ask "still this?" instead of re-downloading it.
	//
	// 🔴 ITS OWN FILE, BESIDE `.sync-stamp` RATHER THAN A LINE INSIDE IT, AND THE
	// REASON IS THE READER. Every non-blank line of `.sync-stamp` is RENDERED, as
	// `  stamp: <line>`, in the header of every report both clients print — so an
	// `etag=` field there would put a 78-character digest on every recall, search and
	// validate a human or an agent ever reads, for a value neither of them can act on.
	// The two files cannot disagree: they are written into the same staging directory
	// and installed by the same atomic swap, so a cache either has both or has neither.
	//
	// ⚠ AND ITS ABSENCE IS NOT AN ERROR. A cache written by an older client, or by a
	// pod that sends no `ETag`, simply has no validator: the next sync sends no
	// `If-None-Match` and is answered the full 200 it always was.
	SyncETagFile = ".sync-etag"

	// maxETagBytes bounds what this client will store or re-send. An entity-tag this
	// long is already pathological; the cap is here so a hostile server cannot make the
	// client write an unbounded file or emit an unbounded request header.
	maxETagBytes = 128
)

// StorableETag is `value` if it is safe to write to disk and send back, else "".
//
// 🔴 THE VALIDATOR COMES FROM THE SERVER, WHICH IS THE PARTY THIS CLIENT ALREADY DOES
// NOT FULLY TRUST — the same threat model as the link, traversal, duplicate and size
// guards a few lines down. The rule is the RFC 9110 `etagc` character set plus the
// quotes: every byte printable ASCII and none of them a space. That is exactly what
// makes the value round-trip as ONE line of a file and as ONE header, so a server
// cannot use it to inject a second header or a second stamp field.
//
// ⚠ IT IS A FILTER, NOT A REFUSAL. An unusable tag means "this cache has no validator",
// never "this response is corrupt": the content arrived fine and the only cost is that
// the next sync cannot be conditional. Refusing here would let a malformed header
// break a sync that otherwise worked.
func StorableETag(value string) string {
	if value == "" || len(value) > maxETagBytes {
		return ""
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x21 || value[i] > 0x7e {
			return ""
		}
	}
	return value
}

// StoredETag is the validator recorded beside `cache`, or "" for "there is none".
//
// 🔴 IT DOES **NOT** FILTER, AND THE FILTER IS AT THE POINT OF USE INSTEAD —
// `FetchSnapshot`, which is where the value becomes a header. A filter here as WELL was
// written first and then deleted: it was a second copy of one predicate, and a mutation
// sweep measured the consequence — removing it changed nothing observable, because the
// surviving copy caught the same value, so the guard could not be watched to fail and
// read as coverage it did not provide. One rule, one place, at the boundary that is
// actually crossed.
func StoredETag(cache string) string {
	data, err := os.ReadFile(filepath.Join(cache, SyncETagFile))
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(data), "\n")
}

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
//
// 🔴 THE CACHE'S PARENT IS ENUMERATED AND THE PREFIX IS MATCHED LITERALLY — AND THAT IS A FIX,
// NOT A REFACTOR. This was `filepath.Glob(filepath.Join(parent, prefix+"*"))`, which put BOTH
// the parent directory AND the cache's own basename inside a pattern. With a `[` anywhere in
// the cache path `filepath.Match` returned `ErrBadPattern`, the error went to `continue`, and
// this function reaped NOTHING while reporting 0 — so the staging trees this mechanism exists
// to remove accumulate without bound, on exactly the hosts whose directory names are least
// ordinary. The oracle reaps them: `cache.parent.glob(...)` treats its anchor literally, and
// `fnmatch` reads an UNTERMINATED `[` as a literal character where `filepath.Match` refuses the
// whole pattern. See `anchor.go` for the class.
//
// ⚠ AND THE PREFIX IS NOW LITERAL, WHICH IS A DECLARED DIVERGENCE RATHER THAN A SIDE EFFECT —
// `tests/parity/README.md` residual 9. The name searched for is one this code CREATED, with
// `os.MkdirTemp(parent, base+".new-")`, so a literal prefix is what it always meant and the
// glob spelling was incidental on both clients. Measured, both directions: with a BALANCED
// class in the cache's basename (`wid[ge]t`) `fnmatch` and `filepath.Match` AGREE — both read
// `[ge]` as a character class, so both miss `wid[ge]t.old-…` and both would reap a
// `widgt.old-…` belonging to a different cache. This client now does neither; the oracle still
// does both. Narrow, in the safe direction, and the price of the fix above.
//
// ⚠ `[ge]` IS ONE MEMBER OF THE FAMILY AND NOT THE FAMILY: A `*` OR A `?` IN THE CACHE'S
// BASENAME IS THE SAME DIVERGENCE IN THE SAME DIRECTION, and naming only the balanced class
// read as though they were covered. MEASURED on CPython 3.12.14 over one directory holding
// `wid*t.old-mine`, `wid?et.old-mine2`, `widget.old-sibling`, `widIt.old-sibling2` and
// `widXet.old-other`: for a cache named `wid*t`, `Path.glob("wid*t.old-*")` and
// `fnmatch("wid*t.old-*")` each return ALL FIVE, and for `wid?et` each returns the three whose
// fourth character is anything at all; `strings.HasPrefix` returns exactly the one tree the
// cache created, in both cases. So the oracle offers a SIBLING cache's staging trees for
// removal and this client does not. ⚠ The direction is not identical to the `[ge]` case and the
// difference is worth the clause: with a balanced class the oracle MISSES its own trees, while
// with `*`/`?` it finds its own AND over-reaps its neighbours'. And unlike an UNTERMINATED `[`
// there is no `ErrBadPattern` on either side to make it visible — it is the quiet arm of the
// class `anchor.go`'s header names.
func ReapOrphans(cache string) int {
	cutoff := time.Now().Add(-OrphanGraceSeconds * time.Second)
	parent, name := filepath.Dir(cache), filepath.Base(cache)
	reaped := 0
	for _, prefix := range []string{name + ".new-", name + ".old-"} {
		for _, entry := range anchoredNames(parent, func(candidate string) bool {
			return strings.HasPrefix(candidate, prefix)
		}) {
			path := filepath.Join(parent, entry)
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
	if isGzip(body) {
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, gzipLayerError{err}
		}
		return tar.NewReader(zr), nil
	}
	return tar.NewReader(bytes.NewReader(body)), nil
}

func isGzip(body []byte) bool {
	return len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b
}

// gzipLayerError marks a failure in the COMPRESSION layer rather than in the archive.
//
// 🔴 THE TWO LAYERS GET DIFFERENT SENTENCES, AND THE ORACLE IS WHY. `tarfile.open(mode="r")`
// raises `EOFError`/`zlib.error` when the DECOMPRESSOR runs out — which the client reports as
// `sent a truncated archive` — and `tarfile.ReadError` when the bytes are not an archive at all,
// which it reports as `did not return an archive`. Go surfaces both as a bare
// `io.ErrUnexpectedEOF` out of `tar.Next`, so a client that classified on the error VALUE reported
// an HTML error page as a truncated tar. Measured: `<html>nope</html>` answered
// `sent a truncated archive: unexpected EOF` where the oracle says `did not return an archive`.
// The layer is therefore recorded at the point it is known.
type gzipLayerError struct{ err error }

func (e gzipLayerError) Error() string { return e.err.Error() }
func (e gzipLayerError) Unwrap() error { return e.err }

// validateGzipLayer streams the whole compressed body to nowhere, purely to learn whether the
// COMPRESSION layer is complete.
//
// 🔴 TO `io.Discard`, WITH A LIMIT, BECAUSE THE ALTERNATIVES ARE BOTH WRONG. Decompressing into
// memory to inspect it would make a decompression bomb a memory bomb BEFORE either ceiling is
// consulted — the ceilings read headers, which a truncated stream never reaches. Not validating at
// all is what produced the misclassification above. The limit is the byte ceiling plus one, so a
// bomb stops being read here and is refused by `checkCeilings`, which owns the MESSAGE.
func validateGzipLayer(body []byte) error {
	if !isGzip(body) {
		return nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return gzipLayerError{err}
	}
	defer zr.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(zr, MaxUnpackedBytes+1)); err != nil {
		return gzipLayerError{err}
	}
	return nil
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
// ⚠ THE LIMITS ARE PARAMETERS, AND THAT IS A TESTABILITY SEAM RATHER THAN A SETTING. Nothing in
// production passes anything but the two constants — `InstallSnapshot` is the only caller — and a
// test that had to build a 256 MB archive to reach the byte ceiling would not exist, which is
// exactly what happened: the `sum` → `max` mutant SURVIVED a suite whose only byte-ceiling case
// asserted that an honest archive is NOT refused. `sum`, not `max`, is the whole point (a `max`
// ceiling passes a 1000 × 250 MB archive) and it needs a case where the two answers differ.
func checkCeilings(body []byte, maxBytes int64, maxMembers int) error {
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
	if declared > maxBytes {
		return corrupt("archive unpacks to %d bytes, over the %d-byte ceiling "+
			"(%d bytes on the wire)", declared, maxBytes, len(body))
	}
	if members > maxMembers {
		return corrupt("archive holds %d members, over the %d-member ceiling "+
			"(%d bytes on the wire)", members, maxMembers, len(body))
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

	// The compression layer FIRST, so a truncated stream is classified as one rather than as an
	// archive that is not an archive. See `gzipLayerError`.
	if err := validateGzipLayer(body); err != nil {
		return 0, err
	}
	if err := checkCeilings(body, MaxUnpackedBytes, MaxMembers); err != nil {
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
	// 🔴 WRITTEN INTO THE STAGING TREE, SO IT MOVES WITH THE SWAP. A validator written
	// into the LIVE cache afterwards could survive a failed install and describe a tree
	// that was never put there — which is the direction that answers 304 for content
	// this host does not hold. Here it is installed by the same rename as the content it
	// describes, or not at all.
	if validator := StorableETag(headers.Get("ETag")); validator != "" {
		if err := os.WriteFile(filepath.Join(staging, SyncETagFile),
			[]byte(validator+"\n"), 0o644); err != nil {
			return 0, err
		}
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
