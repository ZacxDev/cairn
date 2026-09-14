// Package snapshot builds the gzipped tar `/api/v1/snapshot` ships, and dates the
// copy this process is serving.
package snapshot

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// blockSize is the tar block. `recordSize` is the trailer padding a POSIX writer
// rounds the archive up to; it is matched so a byte-level diff against an archive
// the Python oracle produced differs only where the CONTENT does.
const (
	blockSize  = 512
	recordSize = 20 * blockSize
)

// paxWriter writes a PAX tar stream.
//
// 🔴 IT EXISTS BECAUSE Go's `archive/tar` CANNOT EXPRESS THIS CONTRACT, AND THAT IS
// A MEASURED FACT RATHER THAN A PREFERENCE. The contract is that member mtimes are
// preserved with SUB-SECOND precision, because the reader orders its index
// newest-first by entry mtime and a normalised tar reorders every digest rendered
// from the extracted copy — same bytes, same count, different order, no error. The
// oracle satisfies it by emitting an `mtime` extended record for EVERY member,
// unconditionally, because its own timestamps are floats.
//
// Go's writer emits that record only when `ModTime.Nanosecond() != 0`. For a member
// whose mtime is a whole second it writes the integer octal ustar field instead, and
// a POSIX reader then reports an INTEGER where the oracle reports a float — measured
// against CPython's `tarfile`: `946684800` versus `946684800.0`, on the seed stamp,
// which is exactly such a member. `Header.PAXRecords` cannot fix it either: Go
// ignores a user record whose key collides with a basic field ("Ignore local records
// that may conflict"), and writing the extended header by hand through `tar.Writer`
// is refused outright ("cannot manually encode TypeXHeader"). Both were measured.
//
// The alternative considered and REJECTED was nudging a whole-second mtime by one
// nanosecond, which happens to render as `946684800.000000001` and to parse back to
// the same float. That is a falsified timestamp written into an archive a client
// extracts and then orders by; a suite-invisible lie is worse than a hundred lines
// of format code.
//
// ⚠ AND IT IS DELIBERATELY NOT A GENERAL TAR WRITER. It writes regular files and
// nothing else — no directories, links, devices or sparse members — because that is
// the whole of what this route ships (`<scope>/<entry>.md` at depth 2 plus the seed
// stamp). A general writer would be a format implementation nobody exercises;
// refusing everything else keeps the tested surface and the written surface the same
// size.
type paxWriter struct {
	out     io.Writer
	written int
}

func newPaxWriter(out io.Writer) *paxWriter { return &paxWriter{out: out} }

// member is one regular file in the archive.
type member struct {
	Name  string
	Size  int64
	MTime float64
	Body  io.Reader
}

// formatPyFloat renders a float the way CPython's `str()` does, because the extended
// record's VALUE is what a reader parses back into the mtime a golden pins.
//
// 🔴 `'f'` WITH SHORTEST-ROUND-TRIP PRECISION, THEN A FORCED `.0`. Go's `'g'` format
// switches to an exponent for a value of this magnitude (`9.466848e+08`), which
// CPython does not, and Go's `'f'` drops the fraction entirely for a whole number
// (`946684800`) where CPython keeps it (`946684800.0`). Both spellings parse back to
// the same float, so neither is "wrong" — but the golden records the STRING a reader
// produces, so matching CPython is the requirement.
//
// ⚠ THE EXPONENT THRESHOLD IS NOT REPRODUCED, and it does not need to be: CPython
// switches to an exponent at 1e16, which as a Unix timestamp is the year 3×10^8. A
// filesystem cannot carry one, and if it ever could, this would render it as a long
// digit string rather than as an exponent — a divergence with no reachable input, and
// it is named here rather than left as a silent bound.
func formatPyFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// roundHalfToEven is Python's `round(<float>)` — the value CPython puts in the ustar
// numeric field for a float it also writes as an extended record.
//
// 🔴 PYTHON'S `round()` IS HALF-TO-**EVEN**, NOT HALF-UP, AND NOT TRUNCATION. That is
// three distinct functions and they give three different answers for `x.5`:
// `round(946684801.5)` is 946684802 while `round(946684800.5)` is 946684800, because the
// tie goes to the even integer. `math.RoundToEven` is that rule exactly; `math.Round`
// (half away from zero) and `int64(f)` (truncate) are both wrong, and the truncation is
// what shipped.
//
// ⚠ ONE FUNCTION, TWO WRONG NEIGHBOURS — so a reader reaching for a "simpler" spelling
// has both named here rather than having to rediscover which one CPython uses.
func roundHalfToEven(f float64) int64 {
	return int64(math.RoundToEven(f))
}

// asciiReplace is `s.encode("ascii", "replace")` as a Go string: every code point outside
// ASCII becomes exactly ONE `?`.
//
// 🔴 PER CODE POINT, NOT PER BYTE, and the difference is observable in the FIELD LENGTH.
// CPython's `replace` handler substitutes one `?` for each unencodable CHARACTER, so
// `café.md` is `caf?.md` — 7 bytes from 8 UTF-8 bytes. A byte-wise loop would emit two
// `?` for `é` and shift every following byte of the 100-byte name field.
//
// Returning the input unchanged for pure-ASCII input is also how the caller decides
// whether a `path` record is needed: `asciiReplace(name) != name` IS
// "`name.encode('ascii', 'strict')` would have raised", which is the test CPython makes.
func asciiReplace(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x80 {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('?')
	}
	return b.String()
}

// paxRecord renders one extended record: `<len> <key>=<value>\n`, where `<len>`
// counts the WHOLE record including its own decimal digits. The length is therefore
// self-referential and is solved by iterating to a fixed point, which is what the
// POSIX format requires and what CPython does.
func paxRecord(key, value string) string {
	tail := " " + key + "=" + value + "\n"
	l := len(tail)
	p := 0
	for {
		n := l + len(strconv.Itoa(p))
		if n == p {
			break
		}
		p = n
	}
	return strconv.Itoa(p) + tail
}

func (w *paxWriter) write(p []byte) error {
	n, err := w.out.Write(p)
	w.written += n
	return err
}

// pad writes NUL bytes up to the next block boundary.
func (w *paxWriter) pad() error {
	if rem := w.written % blockSize; rem != 0 {
		return w.write(make([]byte, blockSize-rem))
	}
	return nil
}

// WriteMember writes one regular file, preceded by the extended header that carries
// its mtime.
func (w *paxWriter) WriteMember(m member) error {
	// 🔴 THE RECORD ORDER IS `path`, `size`, `mtime`, AND IT IS NOT A STYLE CHOICE —
	// IT IS WHAT CPython EMITS, MEASURED BYTE FOR BYTE. `create_pax_header` builds
	// `pax_headers` as a dict and Python dicts preserve insertion order: it runs the
	// STRING field loop first (`path`, `linkpath`, `uname`, `gname`) and the NUMBER
	// loop second (`uid`, `gid`, `size`, `mtime`). This used to emit `mtime` first,
	// which produced a byte-different extended header for every long-named member and
	// is invisible to any POSIX reader, because a reader parses records by key:
	//
	//	go : "21 mtime=946684803.5\n117 path=beta-notes/deep-…-fifteen.md\n"
	//	py : "117 path=beta-notes/deep-…-fifteen.md\n21 mtime=946684803.5\n"
	//
	// `linkpath`/`uname`/`gname` are not emitted: this writer writes regular files
	// only, and uname/gname are normalised to "" (which is ASCII and within 32 bytes,
	// so CPython emits no record for them either).
	records := ""

	// 🔴 A `path` RECORD IS EMITTED FOR A NON-ASCII NAME **AT ANY LENGTH**, not only
	// above 100 bytes, and missing that cost two divergences in one member. CPython
	// tests the string field twice: `info[name].encode("ascii", "strict")` first — any
	// failure means a record — and only THEN the length. So `beta-notes/café.md`, 18
	// bytes and far inside the field, still gets one. MEASURED:
	//
	//	go : (no record)                    ustar name "beta-notes/café.md"
	//	py : "28 path=beta-notes/café.md\n"  ustar name "beta-notes/caf?.md"
	//
	// …and the ustar name is the second half: `_create_header` writes it through
	// `stn(…, 100, "ascii", "replace")`, so every unencodable CHARACTER becomes ONE
	// `?` — `é` is two UTF-8 bytes and one `?`, which is why the two name fields also
	// differ in LENGTH. The extended record is what carries the real name; the ustar
	// field is the lossy fallback for a reader that ignores records.
	ustarName := asciiReplace(m.Name)
	if len(m.Name) > 100 || ustarName != m.Name {
		records += paxRecord("path", m.Name)
	}
	if len(ustarName) > 100 {
		ustarName = ustarName[:100]
	}
	ustarSize := m.Size
	if m.Size > 0o77777777777 {
		records += paxRecord("size", strconv.FormatInt(m.Size, 10))
		ustarSize = 0
	}
	// The mtime record is emitted UNCONDITIONALLY, which is what CPython does for a
	// FLOAT mtime (`needs_pax = True` on the `val_is_float` arm) and what the contract
	// needs: the reader orders its index newest-first by entry mtime, and the ustar
	// field carries whole seconds only.
	ustarMTime := roundHalfToEven(m.MTime)
	if ustarMTime < 0 || ustarMTime >= 1<<33 {
		// CPython's overflow arm: `not 0 <= val_int < 8 ** (digits - 1)` with
		// `digits = 12` is `0 <= v < 8589934592`, so it zeroes the ustar field and
		// relies on the record. UNREACHABLE FROM ANY FILESYSTEM TODAY — 8589934592 is
		// the year 2242 — and written anyway, because the alternative is an octal
		// field that silently wraps. `1<<33` is 8589934592 spelled as a shift.
		ustarMTime = 0
	}
	records += paxRecord("mtime", formatPyFloat(m.MTime))

	// The extended header's own member. CPython writes it with name
	// `././@PaxHeader`, mode 0, uid/gid 0 and mtime 0 — matched exactly, because
	// nothing is gained by differing and a byte-level comparison against an oracle
	// archive is a cheap future control.
	if err := w.writeHeader(header{
		Name:     "././@PaxHeader",
		Mode:     0,
		Size:     int64(len(records)),
		MTime:    0,
		TypeFlag: 'x',
	}); err != nil {
		return err
	}
	if err := w.write([]byte(records)); err != nil {
		return err
	}
	if err := w.pad(); err != nil {
		return err
	}

	// The file's own header. `MTime` here is the ROUNDED integer second; the extended
	// record above carries the full precision for every POSIX-aware reader.
	//
	// 🔴 THE COMMENT THAT USED TO SIT HERE WAS FALSE, AND IT WAS FALSE IN A WAY THAT
	// EXCUSED A REAL DIVERGENCE. It said "CPython WRITES ZERO IN THIS FIELD (it clears
	// the value it moved into the record)" — it does not. `create_pax_header` computes
	// `val_int = round(val)` and assigns `info[name] = val_int`, so the ustar field
	// carries the ROUNDED seconds; zero is written only on the OVERFLOW arm. This code
	// wrote `int64(m.MTime)`, a TRUNCATION, and the two disagree whenever rounding and
	// truncation do:
	//
	//	.25 on any second  -> agree (round down == truncate)
	//	.5  on an EVEN second -> agree (Python's round() is half-to-EVEN)
	//	.5  on an ODD second  -> DIFFER by one second
	//	.75 on any second  -> DIFFER by one second, always
	//
	// MEASURED against CPython's own writer on a member list carrying all four: three
	// of seven headers differed, and the CHECKSUM moved with each of them, so the
	// divergence is 2 fields × 3 members. Invisible to every reader that honours
	// extended records — which is all of them — and that invisibility is exactly why
	// a false comment about it survived.
	if err := w.writeHeader(header{
		Name:     ustarName,
		Mode:     0o644,
		Size:     ustarSize,
		MTime:    ustarMTime,
		TypeFlag: '0',
	}); err != nil {
		return err
	}
	if _, err := io.Copy(countingWriter{w}, m.Body); err != nil {
		return err
	}
	return w.pad()
}

// Close writes the two zero blocks that end an archive, then pads to the record
// size a POSIX writer rounds up to.
func (w *paxWriter) Close() error {
	if err := w.write(make([]byte, 2*blockSize)); err != nil {
		return err
	}
	if rem := w.written % recordSize; rem != 0 {
		return w.write(make([]byte, recordSize-rem))
	}
	return nil
}

type countingWriter struct{ w *paxWriter }

func (c countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.out.Write(p)
	c.w.written += n
	return n, err
}

type header struct {
	Name     string
	Mode     int64
	Size     int64
	MTime    int64
	TypeFlag byte
}

// writeHeader emits one 512-byte ustar header block with a POSIX magic.
//
// The field layout is fixed by the format: name(100) mode(8) uid(8) gid(8) size(12)
// mtime(12) chksum(8) typeflag(1) linkname(100) magic(6) version(2) uname(32)
// gname(32) devmajor(8) devminor(8) prefix(155) pad(12).
//
// 🔴 THE CHECKSUM IS COMPUTED WITH ITS OWN FIELD READ AS EIGHT SPACES, which is what
// the format specifies and what every reader verifies against; it is then written as
// six octal digits, a NUL and a space — CPython's spelling, which GNU tar accepts.
// Getting this wrong produces an archive that every reader rejects, which is the one
// failure mode of a hand-written tar that cannot be silent.
func (w *paxWriter) writeHeader(h header) error {
	var block [blockSize]byte
	copyString := func(offset, length int, s string) {
		if len(s) > length {
			s = s[:length]
		}
		copy(block[offset:offset+length], s)
	}
	// An octal numeric field is `%0*o` over `length-1` digits, then a NUL — the
	// POSIX/GNU spelling CPython uses.
	copyOctal := func(offset, length int, v int64) {
		copyString(offset, length, fmt.Sprintf("%0*o", length-1, v))
	}
	copyString(0, 100, h.Name)
	copyOctal(100, 8, h.Mode)
	copyOctal(108, 8, 0) // uid — NORMALISED, see the snapshot docstring
	copyOctal(116, 8, 0) // gid
	copyOctal(124, 12, h.Size)
	copyOctal(136, 12, h.MTime)
	for i := 148; i < 156; i++ {
		block[i] = ' '
	}
	block[156] = h.TypeFlag
	// linkname stays NUL. magic+version is one eight-byte POSIX field.
	copyString(257, 8, "ustar\x0000")
	// uname/gname stay NUL — normalised to the empty string, not to a name this
	// process happens to run as.
	// devmajor/devminor stay NUL, which is what a non-device member carries.

	sum := 0
	for _, b := range block {
		sum += int(b)
	}
	copyString(148, 8, fmt.Sprintf("%06o\x00 ", sum))
	return w.write(block[:])
}

// unixFloat is a file modification time as the archive carries it: seconds since the
// epoch with the nanosecond fraction folded in.
//
// 🔴 `1e-9 * nsec`, NOT `nsec / 1e9`, BECAUSE CPython's `os.stat` COMPUTES IT THAT
// WAY. `1e-9` is not exactly representable, so the two spellings can differ in the
// last bit — and the last bit is what `repr()` prints, which is what the golden
// pins. The fixture's three fractions (.25, .75, .125) were checked to agree under
// both spellings; the one that matches the oracle is written anyway, because "these
// particular inputs agree" is a property of the fixture rather than of the function.
func unixFloat(t time.Time) float64 {
	return float64(t.Unix()) + 1e-9*float64(t.Nanosecond())
}
