// Package snapshot builds the gzipped tar `/api/v1/snapshot` ships, and dates the
// copy this process is serving.
package snapshot

import (
	"fmt"
	"io"
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
	records := paxRecord("mtime", formatPyFloat(m.MTime))
	// A name too long for the 100-byte ustar field, or a size too large for its
	// 11-octal-digit field, rides an extended record too — the same rule CPython
	// applies, so an archive of a deep store is readable by the same readers.
	ustarName := m.Name
	if len(m.Name) > 100 {
		records += paxRecord("path", m.Name)
		ustarName = m.Name[:100]
	}
	ustarSize := m.Size
	if m.Size > 0o77777777777 {
		records += paxRecord("size", strconv.FormatInt(m.Size, 10))
		ustarSize = 0
	}

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

	// The file's own header. `MTime` here is the TRUNCATED integer second, which the
	// extended record above overrides for every POSIX-aware reader.
	//
	// ⚠ CPython WRITES ZERO IN THIS FIELD (it clears the value it moved into the
	// record) AND THIS WRITES THE REAL SECONDS. The difference is invisible to every
	// reader that honours extended records — CPython's own `tarfile`, Go's
	// `archive/tar` and GNU tar all prefer the record — so no conformance case can
	// see it, and it is recorded here for that reason rather than left as an
	// unexplained divergence. The real seconds are written because a reader that
	// ignores extended headers gets a true timestamp instead of the epoch.
	if err := w.writeHeader(header{
		Name:     ustarName,
		Mode:     0o644,
		Size:     ustarSize,
		MTime:    int64(m.MTime),
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
