package snapshot

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/pytext"
	"github.com/ZacxDev/cairn/internal/store"
)

// SeedStampName is the file the seed script writes to date the copy.
const SeedStampName = ".seed-stamp"

// freshnessMaxDepth is how deep to walk when dating the served copy. Deliberately
// the SAME depth the seed script uses for its own entry listings, so the two numbers
// are answers to the same question and a disagreement between them means something
// real rather than a units mismatch. The invariant, stated rather than quoted from a
// sibling script that has already rotted once: BOTH count `<scope>/*.md`, excluding
// the store root and any dot-directory.
const freshnessMaxDepth = 2

// rootActions maps every path kind to what the candidate walk does with it.
//
// 🔴 A BROKEN POINTER IS A SCOPE THAT SHOULD BE THERE AND IS NOT. Skipping it is the
// regression this table's fourth round produced: a directory test is false for a
// dangling link AND for a loop, so both vanished and the scope read as `scope-empty`
// at exit 0 — an absence claimed about a scope nobody could look at.
//
// 🔴 `indeterminate` IS REFUSE, not SKIP. We do not know whether it is a scope, so we
// cannot claim its absence — that claim is the whole defect class.
var rootActions = map[store.Kind]store.Action{
	store.KindBrokenLink:    store.Refuse,
	store.KindLinkToDir:     store.Refuse, // a real scope we will not follow off-store
	store.KindDirectory:     store.Take,
	store.KindLinkToFile:    store.Skip, // a file is not a scope, link or no link
	store.KindRegularFile:   store.Skip, // …and neither is a plain README.md
	store.KindLinkToOther:   store.Skip,
	store.KindOther:         store.Skip,
	store.KindIndeterminate: store.Refuse,
	store.KindAbsent:        store.Skip, // it is genuinely gone; nothing to report
}

// entryActions is the table for a path the `*.md` name test has ALREADY selected, so
// anything here CLAIMS to be an entry. A claim we cannot serve is refused, not
// skipped.
//
// It is deliberately WIDER than the index loader's table: this route refuses a
// symlinked entry, which the loader takes. The action is a property of the CONTEXT,
// not of the path — the loader has always read a symlink to a regular file and
// refusing it there would be a behaviour change for every local caller, while an
// archive that followed one would ship bytes from outside the store.
var entryActions = map[store.Kind]store.Action{
	store.KindBrokenLink:    store.Refuse,
	store.KindLinkToDir:     store.Refuse,
	store.KindLinkToFile:    store.Refuse,
	store.KindLinkToOther:   store.Refuse,
	store.KindDirectory:     store.Refuse, // a directory named `*.md` blocked `open()`
	store.KindOther:         store.Refuse, // a FIFO named `*.md` blocked it forever
	store.KindRegularFile:   store.Take,
	store.KindIndeterminate: store.Refuse,
	store.KindAbsent:        store.Skip,
}

// RootAction and EntryAction expose the two tables so a test can assert each is
// complete over the closed kind set, and that the entry table is the WIDER one.
func RootAction(kind store.Kind) (store.Action, bool) {
	action, ok := rootActions[kind]
	return action, ok
}

func EntryAction(kind store.Kind) (store.Action, bool) {
	action, ok := entryActions[kind]
	return action, ok
}

// Freshness dates the copy this process is serving: the header value and the prose
// line that opens every report.
//
// 🔴 WHY THIS EXISTS, AND IT IS NOT A NICETY. The server does not serve the
// authoritative store — it serves a COPY pushed into a volume, and NOTHING syncs that
// copy continuously. Yet every report it renders opens with a COMPLETENESS assertion
// ("ALL N entries in `<scope>/`, none omitted") that is truthful about the bytes on
// this disk and unfalsifiable from outside it. That combination was measured live:
// the endpoint answered 200 claiming ALL 5 entries while the source held 9, and one
// served entry was a 40-day-old version of a file edited that morning. Nothing in the
// payload, the headers or the status was wrong; nothing in it was current either.
//
// Two INDEPENDENT facts, because each covers the other's blind spot: `seeded` answers
// "how old is this COPY" and `newest` answers "how old is this CONTENT", derived from
// the files themselves and owing nothing to the stamp. A quiet week makes `newest` old
// while the copy is perfectly current; a forgotten re-seed makes `seeded` old while
// `newest` merely lags. Neither alone is the answer.
//
// 🔴 EVERY FAILURE IS ITS OWN NAMED STATE — never a silent omission and never a
// fabricated date. A missing stamp says UNSTAMPED, an unreadable one UNREADABLE, and a
// walk that hit an error says `newest=UNREADABLE` — each distinguishable from the
// genuinely empty store, which says `newest=NONE entry-files=0`. An absent block would
// read as "this is the source", which is the exact confusion the block removes.
// headerSafe reports whether every character is PRINTABLE ASCII (0x20..0x7E) — what a
// header value can carry identically in any implementation. It is `server.py`'s
// `_header_safe`, and the two must stay in step.
//
// 🔴 THE BOUND IS THE HEADER, NOT THE FILESYSTEM, and it is deliberately NARROWER than
// "encodable". U+0000..U+001F and U+007F frame-break or vanish; U+0080..U+00FF is the
// range where two CORRECT implementations disagree silently, because `http.server`
// encodes a header value as latin-1 (one byte) and a UTF-8 writer sends two. Above
// U+00FF the oracle's encode raises mid-response. Restricting to printable ASCII is the
// only bound under which the byte sequence is the same on both.
func headerSafe(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

func Freshness(storeRoot string) (header, prose string) {
	seeded := "UNSTAMPED"
	if data, err := os.ReadFile(filepath.Join(storeRoot, SeedStampName)); err == nil {
		// 🔴 A STRICT DECODE, BECAUSE THE ORACLE'S `read_text(encoding="utf-8")` IS ONE.
		// A stamp carrying a byte that is not valid UTF-8 is `UNREADABLE` on both sides;
		// `string(data)` would have carried the byte into a header.
		if pytext.DecodeStrictProblem(data) != "" {
			seeded = "UNREADABLE"
		} else if text := pytext.StripWhitespace(string(data)); text == "" {
			seeded = "UNREADABLE"
		} else {
			seeded = pytext.StripWhitespace(pytext.SplitLines(text)[0])
		}
	} else if !os.IsNotExist(err) {
		seeded = "UNREADABLE"
	}
	// 🔴 A STAMP THAT CANNOT GO IN A HEADER CLEANLY IS `UNREADABLE` — AND THE ORACLE WAS
	// CHANGED TO AGREE, rather than this side being bent to reproduce an accident. Three
	// measured outcomes there for a stamp nobody validated: a latin-1-encodable character
	// went on the wire as ONE byte where any UTF-8 writer sends TWO (a silent divergence
	// in a pinned header); an emoji made `send_header` raise AFTER the status line was
	// written, TRUNCATING the response (`curl` exit 8); and an invalid UTF-8 byte 503'd a
	// store that was perfectly readable. None is designed behaviour, and a contract cannot
	// include "sometimes truncate the response mid-stream" — so the value is constrained to
	// PRINTABLE ASCII, which is what a header carries identically everywhere, and anything
	// else is the `UNREADABLE` state this block already defines.
	//
	// ⚠ NO LEGITIMATE STAMP IS AFFECTED: `server/seed.sh` writes an ISO-8601 timestamp.
	if seeded != "UNSTAMPED" && seeded != "UNREADABLE" && !headerSafe(seeded) {
		seeded = "UNREADABLE"
	}

	var newest float64
	haveNewest := false
	count := 0
	walkFailed := false

	// 🔴 EVERY DIRECTORY READ IS EXPLICIT AND EVERY FAILURE IS RECORDED. A recursive
	// helper that swallows a permission error and yields nothing makes an UNREADABLE
	// scope indistinguishable from an EMPTY one — it would report
	// `newest=NONE entry-files=0`, a confident zero from a walk that saw nothing.
	// That is the precise failure this function was written to stop committing, and
	// the first Python draft committed it.
	scopes, err := os.ReadDir(storeRoot)
	if err != nil {
		walkFailed = true
	}
	var scopeNames []string
	for _, d := range scopes {
		if strings.HasPrefix(d.Name(), ".") {
			continue
		}
		// The Python side asks `p.is_dir()`, which FOLLOWS a symlink; a directory
		// entry's own type bit does not, so the stat is explicit here.
		info, statErr := os.Stat(filepath.Join(storeRoot, d.Name()))
		if statErr != nil || !info.IsDir() {
			continue
		}
		scopeNames = append(scopeNames, d.Name())
	}
	slices.Sort(scopeNames)

	// 🔴 THE WALK DESCENDS PAST THE DEPTH CAP AND ONLY THE *COUNTING* STOPS THERE,
	// which is what the oracle does and is not the same thing as not descending. A
	// subdirectory below the cap holds no entry the reader would load, so its files
	// are not counted — but if it cannot be READ that is still "the store was not
	// fully read", and reporting `newest=NONE entry-files=0` over it would be the
	// confident zero this function exists to refuse. The cap is therefore applied to
	// the file loop and never to the recursion.
	//
	// ⚠ UNMEASURED AGAINST THE ORACLE, AND SAID SO: no scope in this store has a
	// subdirectory at all, so nothing in the conformance corpus exercises the
	// recursion or the failure bookkeeping inside it. What IS exercised is the
	// depth-2 count and every one of the four named states, which is what the
	// goldens pin.
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			walkFailed = true
			return
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			// The classification FOLLOWS a symlink, because that is what the oracle's
			// walk does; the RECURSION does not, because that walk is configured not
			// to. Two questions, two different answers about the same path, and
			// collapsing them is how a symlinked scope directory gets traversed twice
			// or not at all.
			info, statErr := os.Stat(path)
			if statErr != nil {
				walkFailed = true
				continue
			}
			if info.IsDir() {
				if lst, lstatErr := os.Lstat(path); lstatErr == nil && lst.Mode()&os.ModeSymlink == 0 {
					walk(path, depth+1)
				}
				continue
			}
			if depth > freshnessMaxDepth || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			mtime := unixFloat(info.ModTime())
			count++
			if !haveNewest || mtime > newest {
				newest, haveNewest = mtime, true
			}
		}
	}
	for _, name := range scopeNames {
		// A scope directory sits at depth 2 relative to the store root, which is the
		// depth its own `*.md` children are counted at.
		walk(filepath.Join(storeRoot, name), freshnessMaxDepth)
	}

	newestText := "NONE"
	switch {
	case walkFailed:
		newestText = "UNREADABLE"
	case haveNewest:
		sec := int64(newest)
		newestText = time.Unix(sec, 0).UTC().Format("2006-01-02T15:04:05Z")
	}

	header = fmt.Sprintf("seeded=%s newest=%s entry-files=%d", seeded, newestText, count)
	prose = "🔴 SNAPSHOT, NOT THE SOURCE — seeded=" + seeded + " newest-entry=" + newestText +
		fmt.Sprintf(" entry-files=%d", count) +
		". This host serves a COPY of the authoritative store, " +
		"pushed by `seed.sh`; nothing syncs it continuously, so it can be " +
		"arbitrarily behind and it CANNOT KNOW BY HOW MUCH. The " +
		"\"none omitted\" below is true of THIS DISK and says nothing about the " +
		"source. Before trusting an absence — a missing entry, a missing badge, " +
		"a zero — re-run the read against the local store, or re-seed. " +
		"UNSTAMPED/UNREADABLE/NONE each mean the stated fact could not be " +
		"established, never that it is fine."
	return header, prose
}

// Result is a built snapshot, or the reason it could not be built.
type Result struct {
	// Archive is the gzipped tar.
	Archive []byte
	// Entries is the SERVER's own count of what it put in, which the client compares
	// against its extracted count and refuses a mismatch.
	Entries int
	// Unreadable is non-empty when the store was NOT fully read. A partial snapshot
	// served as 200 is worse than no snapshot, so any entry here makes the whole
	// response a 503 carrying the same store-unreachable state a report uses.
	Unreadable []string
}

// Build assembles the archive.
//
// 🔴 THE ALLOWLIST IS APPLIED TO THE CANDIDATE LIST, AND THIS ROUTE IS THE FOURTH
// ENUMERATION CHANNEL — the only one the index cannot close. It never builds an index;
// it walks the store root directly, so the narrowing that covers every other read does
// not reach here at all. Without this filter a caller allowed one scope could download
// EVERY scope's entry files, which is a wider leak than any of the three channels the
// index filter closes.
//
// It is applied BEFORE anything is classified or opened, so an out-of-allowlist scope
// is never classified, never opened, and can never reach the `Unreadable` list either
// — a refused scope must not be able to 503 somebody else's snapshot, which is both a
// leak and a denial of service.
//
// 🔴 MTIMES ARE PRESERVED, AND THAT IS LOAD-BEARING, NOT TIDINESS. The reader orders
// its index newest-first by entry mtime. A tar built with normalised mtimes — the usual
// move for reproducibility — would reorder every digest rendered from the extracted
// copy, so a client's output would differ from the pod's for content that is
// byte-identical. That failure is invisible: no error, no missing entry, just a
// different order that reads as a stale cache. uid/gid/uname/gname/mode ARE normalised,
// since none of them reaches the reader.
//
// What is shipped is exactly what the reader consumes — `<scope>/<x>.md` at depth 2 —
// plus the seed stamp, so the client can date the copy for itself instead of trusting a
// header. Nothing else: no `.git`, no dot directories, no deeper paths.
//
// 🔴 GZIPPED, because PAX is expensive per member and these members are tiny.
// MEASURED on the Python side: 305 entries totalling 62,821 bytes of markdown produced
// a 634,880-byte uncompressed tar — 10.1x the payload — since PAX spends ~2 KB of
// headers on a ~200-byte entry. The whole tar is held in memory here and again on the
// client, and a timer re-transfers the entire store every tick, so the multiplier is
// the thing that matters, not the absolute size.
func Build(storeRoot, scopeFilter string, visible store.ScopeSet) (Result, error) {
	dirents, err := os.ReadDir(storeRoot)
	if err != nil {
		// 🔴 The store was NOT read. The same state and the same reasoning as a
		// report's: an empty tar and an unreadable store must never render alike,
		// because one of them is a lie.
		return Result{}, err
	}

	var candidates []string
	for _, d := range dirents {
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if scopeFilter != "" && name != scopeFilter {
			continue
		}
		if !visible.Allows(name) {
			continue
		}
		candidates = append(candidates, name)
	}
	slices.Sort(candidates)

	var unreadable []string
	var scopes []string
	for _, name := range candidates {
		// One classification, one mapping, no ordering to get wrong.
		kind := store.ClassifyPath(filepath.Join(storeRoot, name))
		action, actionErr := store.ActionFor(kind, rootActions)
		if actionErr != nil {
			return Result{}, actionErr
		}
		switch action {
		case store.Take:
			scopes = append(scopes, name)
		case store.Refuse:
			unreadable = append(unreadable, fmt.Sprintf("%s/: %s refused", name, kind))
		}
	}

	type selected struct {
		path    string
		arcname string
	}
	var chosen []selected
	for _, scope := range scopes {
		scopePath := filepath.Join(storeRoot, scope)
		entries, readErr := os.ReadDir(scopePath)
		if readErr != nil {
			unreadable = append(unreadable, fmt.Sprintf("%s/: %s", scope, errnoText(readErr)))
			continue
		}
		var names []string
		for _, entry := range entries {
			// 🔴 NAME RULES ARE SEPARATE FROM TYPE RULES, and keeping them separate
			// is the point. The `.md` suffix and the dotfile skip decide whether a
			// path CLAIMS to be an entry; the classifier decides whether the claim
			// can be served. Conflating them is what made an editor lock file —
			// `.#entry.md`, a DANGLING SYMLINK whose name ends in `.md` — 503 the
			// entire store for every caller because one buffer was open.
			if strings.HasSuffix(entry.Name(), ".md") && !strings.HasPrefix(entry.Name(), ".") {
				names = append(names, entry.Name())
			}
		}
		slices.Sort(names)
		for _, name := range names {
			entryPath := filepath.Join(scopePath, name)
			kind := store.ClassifyPath(entryPath)
			action, actionErr := store.ActionFor(kind, entryActions)
			if actionErr != nil {
				return Result{}, actionErr
			}
			if action == store.Refuse {
				unreadable = append(unreadable, fmt.Sprintf("%s/%s: %s refused", scope, name, kind))
				continue
			}
			if action != store.Take {
				continue
			}
			chosen = append(chosen, selected{path: entryPath, arcname: scope + "/" + name})
		}
	}

	if len(unreadable) > 0 {
		return Result{Unreadable: unreadable}, nil
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := newPaxWriter(gz)
	count := 0

	stampPath := filepath.Join(storeRoot, SeedStampName)
	if info, statErr := os.Lstat(stampPath); statErr == nil && info.Mode().IsRegular() {
		if err := addFile(tw, stampPath, SeedStampName); err != nil {
			return Result{}, err
		}
	}
	for _, item := range chosen {
		if err := addFile(tw, item.path, item.arcname); err != nil {
			return Result{}, err
		}
		count++
	}
	if err := tw.Close(); err != nil {
		return Result{}, err
	}
	if err := gz.Close(); err != nil {
		return Result{}, err
	}
	return Result{Archive: buf.Bytes(), Entries: count}, nil
}

func addFile(tw *paxWriter, path, arcname string) error {
	handle, err := os.Open(path)
	if err != nil {
		return err
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return err
	}
	return tw.WriteMember(member{
		Name:  arcname,
		Size:  info.Size(),
		MTime: unixFloat(info.ModTime()),
		Body:  handle,
	})
}

// errnoText is the short reason a scope-level read failure carries, mirroring the
// oracle's `exc.strerror or exc`: the OS's own sentence for the errno rather than the
// full wrapped path, which would name a file inside a scope the caller may not be able
// to see.
func errnoText(err error) string {
	if pathErr, ok := err.(*os.PathError); ok {
		return pathErr.Err.Error()
	}
	return err.Error()
}
