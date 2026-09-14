package snapshot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/store"
)

func TestFormatPyFloat(t *testing.T) {
	// 🔴 THE STRING A READER PARSES BACK IS WHAT A GOLDEN PINS, so the formatting is
	// the contract and not a presentation choice. Two Go defaults are wrong for it and
	// both are here: `'g'` switches to an exponent at this magnitude, and `'f'` drops
	// the fraction entirely for a whole number where CPython keeps `.0`.
	cases := []struct {
		in   float64
		want string
	}{
		{946684800.0, "946684800.0"},
		{946684800.25, "946684800.25"},
		{946684800.75, "946684800.75"},
		{946857600.125, "946857600.125"},
		{0, "0.0"},
	}
	for _, tc := range cases {
		if got := formatPyFloat(tc.in); got != tc.want {
			t.Fatalf("formatPyFloat(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// 🔴 FOUR SILENT DIVERGENCES FROM CPython's OWN PAX WRITER, ALL INVISIBLE TO EVERY POSIX
// READER AND THEREFORE TO THE WHOLE CONFORMANCE CORPUS — the snapshot golden compares the
// EXTRACTED TREE, so a reader that honours extended records normalises every one of these
// away before the comparison happens. They were found by writing the two archives side by
// side and diffing BYTES.
//
// Reproduced by building the reference with `tarfile` configured exactly as
// `server.py:_serve_snapshot` configures it — `format=tarfile.PAX_FORMAT`, and per member
// `info.mtime = st.st_mtime` as a FLOAT, `mode=0o644`, `uid=gid=0`, `uname=gname=""`,
// `type=REGTYPE` — over a member list carrying all four divergence classes: a whole second,
// a `.25`, a `.5` on an EVEN second, a `.5` on an ODD one, a `.75`, a non-ASCII name and a
// name over 100 bytes. Before the fix 193 bytes differed across 7 header blocks; after it
// the two archives were BYTE-IDENTICAL at 20,480 bytes each.
// The assertions below are the FIELDS, spelled by hand from CPython's output, rather than
// a committed binary blob: each one fails on its own, which a blob cannot do, and a
// reviewer can check them against `tarfile` by eye.
func TestTheUstarHeaderMatchesCPythonWhereNoReaderCanSee(t *testing.T) {
	// 🔴 `round()`, HALF-TO-EVEN — three different functions give three different answers
	// for `x.5`, and the shipped code used the third. Values transcribed from
	// `round(<float>)` on the pinned interpreter.
	t.Run("the ustar mtime field is round() and not truncation", func(t *testing.T) {
		for _, tc := range []struct {
			mtime float64
			want  int64
		}{
			// Agree with truncation — the CONTROL rows. A table without them cannot tell
			// "rounds correctly" from "always rounds up".
			{946684800.0, 946684800},
			{946684800.25, 946684800},
			{946684800.5, 946684800}, // .5 on an EVEN second: the tie goes to even
			// Disagree with truncation — the rows that were wrong on the wire.
			{946684800.75, 946684801},
			{946684801.5, 946684802}, // .5 on an ODD second: up, to the even one
			{946684803.5, 946684804},
			{946684801.25, 946684801},
		} {
			if got := roundHalfToEven(tc.mtime); got != tc.want {
				t.Fatalf("roundHalfToEven(%v) = %d, want %d (Python's round())", tc.mtime, got, tc.want)
			}
		}
		// 🔴 THE TABLE'S OWN CONTROL, ASSERTED RATHER THAN ASSUMED: at least one row must
		// DISAGREE with truncation and at least one must disagree with half-away-from-zero,
		// or the table cannot distinguish the three functions and would pass with the bug
		// reinstated. Checked mechanically, because "I picked good values" is the claim
		// that fails silently when somebody trims a row.
		sawTruncationDiffer, sawHalfUpDiffer := false, false
		for _, mtime := range []float64{946684800.0, 946684800.25, 946684800.5,
			946684800.75, 946684801.5, 946684803.5, 946684801.25} {
			want := roundHalfToEven(mtime)
			if int64(mtime) != want {
				sawTruncationDiffer = true
			}
			if int64(math.Round(mtime)) != want {
				sawHalfUpDiffer = true
			}
		}
		if !sawTruncationDiffer {
			t.Fatal("no row distinguishes round() from int64() truncation — the shipped bug would pass")
		}
		if !sawHalfUpDiffer {
			t.Fatal("no row distinguishes half-to-EVEN from half-away-from-zero (math.Round)")
		}
	})

	// 🔴 A `path` RECORD FOR A NON-ASCII NAME AT ANY LENGTH — CPython tests
	// `encode("ascii", "strict")` BEFORE it tests the length, so an 18-byte name gets one.
	t.Run("a non-ASCII name gets a path record and an ascii-replaced ustar name", func(t *testing.T) {
		var out bytes.Buffer
		w := newPaxWriter(&out)
		// `café.md` — built from the code point, not pasted, so the source is reviewable.
		name := "beta-notes/caf" + string(rune(0x00e9)) + ".md"
		if err := w.WriteMember(member{
			Name: name, Size: 4, MTime: 946684802.125, Body: strings.NewReader("abc\n"),
		}); err != nil {
			t.Fatal(err)
		}
		blocks := out.Bytes()

		// The extended header's payload starts at block 1 (block 0 is its own ustar
		// header). Spelled whole, including the self-referential length, because the
		// LENGTH is the part an off-by-one gets wrong.
		wantRecords := "28 path=beta-notes/café.md\n23 mtime=946684802.125\n"
		if got := string(bytes.TrimRight(blocks[512:1024], "\x00")); got != wantRecords {
			t.Fatalf("extended records:\n got %q\nwant %q", got, wantRecords)
		}
		// 🔴 AND THE ORDER IS PART OF THAT STRING. Asserting the whole record block
		// rather than "it contains a path record" is what makes `mtime`-before-`path`
		// fail, which is how it shipped.

		// The ustar name field: one `?` per unencodable CHARACTER, so 18 bytes not 19.
		ustarName := string(bytes.TrimRight(blocks[1024:1124], "\x00"))
		if ustarName != "beta-notes/caf?.md" {
			t.Fatalf("ustar name = %q, want %q (ascii/replace)", ustarName, "beta-notes/caf?.md")
		}
	})

	// The long-name case, which DID emit a record before — so this row is the control
	// proving the fix above did not simply make every member take one code path.
	t.Run("a name over 100 bytes puts path FIRST and truncates the ustar field", func(t *testing.T) {
		var out bytes.Buffer
		w := newPaxWriter(&out)
		name := "beta-notes/deep-one-two-three-four-five-six-seven-eight-nine-ten-eleven-twelve-thirteen-fourteen-fifteen.md"
		if len(name) <= 100 {
			t.Fatalf("this fixture needs a name over 100 bytes, got %d", len(name))
		}
		if err := w.WriteMember(member{
			Name: name, Size: 5, MTime: 946684803.5, Body: strings.NewReader("long\n"),
		}); err != nil {
			t.Fatal(err)
		}
		blocks := out.Bytes()
		wantRecords := "117 path=" + name + "\n21 mtime=946684803.5\n"
		if got := string(bytes.TrimRight(blocks[512:1024], "\x00")); got != wantRecords {
			t.Fatalf("extended records:\n got %q\nwant %q", got, wantRecords)
		}
		if got := string(blocks[1024:1124]); got != name[:100] {
			t.Fatalf("ustar name = %q, want the first 100 bytes %q", got, name[:100])
		}
		// The ustar mtime for `.5` on an ODD second is the even one ABOVE it, and the
		// checksum moves with it — so reading the checksum back is what pins that the
		// header this test inspects is the header a reader would accept.
		if err := verifyChecksum(blocks[1024:1536]); err != nil {
			t.Fatalf("the member header's checksum does not verify: %v", err)
		}
		// 🔴 THE VERIFIER'S OWN NEGATIVE CONTROL. A checksum check that cannot go red is
		// indistinguishable from no check, and this one is hand-written in the test file —
		// so it is fed a block it MUST reject before its pass above is read as evidence.
		corrupt := append([]byte(nil), blocks[1024:1536]...)
		corrupt[0] ^= 0xff
		if err := verifyChecksum(corrupt); err == nil {
			t.Fatal("verifyChecksum accepted a corrupted block — it is testing nothing")
		}
	})
}

// verifyChecksum recomputes a ustar header's checksum the way every reader does — with the
// checksum field itself read as eight spaces — and compares it to what the block carries.
func verifyChecksum(block []byte) error {
	if len(block) != 512 {
		return errNotABlock
	}
	stated := strings.TrimRight(strings.TrimSpace(string(block[148:156])), "\x00")
	sum := 0
	for i, b := range block {
		if i >= 148 && i < 156 {
			sum += ' '
			continue
		}
		sum += int(b)
	}
	if want := strings.TrimLeft(strings.TrimSpace(stated), "0"); want != strings.TrimLeft(octal(sum), "0") {
		return errChecksum
	}
	return nil
}

func octal(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%8)}, out...)
		n /= 8
	}
	return string(out)
}

var (
	errNotABlock = errStr("not a 512-byte block")
	errChecksum  = errStr("checksum mismatch")
)

type errStr string

func (e errStr) Error() string { return string(e) }

func TestPaxRecordLengthIsSelfReferential(t *testing.T) {
	// The POSIX record length counts its own decimal digits, so it is a fixed point.
	// A record whose length lands on a digit boundary is the case an off-by-one gets
	// wrong, and it is the reason this is solved rather than computed once.
	for _, rec := range []string{
		paxRecord("mtime", "946684800.25"),
		paxRecord("path", strings.Repeat("x", 90)),
		paxRecord("k", "v"),
	} {
		declared, _, found := strings.Cut(rec, " ")
		if !found {
			t.Fatalf("record has no length prefix: %q", rec)
		}
		if declared != itoa(len(rec)) {
			t.Fatalf("record %q declares length %s but is %d bytes", rec, declared, len(rec))
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// writeEntry materialises one file with an exact mtime.
func writeEntry(t *testing.T, path, body string, sec int64, nsec int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(sec, int64(nsec))
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

// fixtureStore mirrors the conformance world's load-bearing shape: two entries that
// share a WHOLE SECOND and differ only in the fraction, plus a seed stamp whose mtime
// is an exact second.
func fixtureStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeEntry(t, filepath.Join(root, SeedStampName), "2000-01-04T00:00:00Z\n", 946684800, 0)
	writeEntry(t, filepath.Join(root, "alpha-notes", "gadget-one.md"), "one\n", 946684800, 250000000)
	writeEntry(t, filepath.Join(root, "alpha-notes", "gadget-two.md"), "two\n", 946684800, 750000000)
	writeEntry(t, filepath.Join(root, "beta-notes", "widget-three.md"), "three\n", 946771200, 500000000)
	return root
}

// members extracts the archive with the stdlib reader, which honours PAX records the
// same way the oracle's reader does.
func members(t *testing.T, archive []byte) []*tar.Header {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("the archive is not gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	var out []*tar.Header
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("the archive is not readable: %v", err)
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			t.Fatalf("member %s is not readable: %v", header.Name, err)
		}
		out = append(out, header)
	}
	return out
}

func TestSubSecondMtimesSurviveTheArchive(t *testing.T) {
	root := fixtureStore(t)
	result, err := Build(root, "", store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unreadable) != 0 {
		t.Fatalf("the fixture store is readable: %v", result.Unreadable)
	}
	got := members(t, result.Archive)

	// 🔴 THE STAMP FIRST, THEN THE ENTRIES IN SORTED ORDER, because the order a
	// client's index derives from is the ARCHIVE order for ties and the mtime order
	// otherwise.
	wantNames := []string{
		SeedStampName,
		"alpha-notes/gadget-one.md",
		"alpha-notes/gadget-two.md",
		"beta-notes/widget-three.md",
	}
	if len(got) != len(wantNames) {
		t.Fatalf("want %d members, got %d", len(wantNames), len(got))
	}
	for i, want := range wantNames {
		if got[i].Name != want {
			t.Fatalf("member %d: got %q want %q", i, got[i].Name, want)
		}
	}

	// 🔴 THE PROPERTY A NORMALISED TAR DESTROYS. The two alpha entries share a whole
	// second and differ only in the fraction; if the fraction is lost they TIE, the
	// reader falls back to its ref tie-break, and the extracted copy orders its index
	// DIFFERENTLY from the source — same bytes, same count, different order, no error.
	one := got[1].ModTime.UnixNano()
	two := got[2].ModTime.UnixNano()
	if one == two {
		t.Fatal("the two entries tie after the round trip: the sub-second fraction " +
			"was lost, which is the silent reordering this format choice exists to prevent")
	}
	if one != 946684800*int64(time.Second)+250000000 {
		t.Fatalf("gadget-one mtime round-tripped as %d", one)
	}
	if two != 946684800*int64(time.Second)+750000000 {
		t.Fatalf("gadget-two mtime round-tripped as %d", two)
	}
	// 🔴 A WHOLE-SECOND MTIME MUST ALSO RIDE AN EXTENDED RECORD, WHICH IS THE ONE Go's
	// OWN WRITER OMITS. It is invisible through a Go reader — both spellings parse to
	// the same instant — so the assertion is on the BYTES: the record has to be in the
	// stream, because a CPython reader reports an INTEGER mtime without it and the
	// golden records a float.
	raw := ungzip(t, result.Archive)
	if !bytes.Contains(raw, []byte("mtime=946684800.0\n")) {
		t.Fatal("the seed stamp's whole-second mtime carries no extended record, so a " +
			"POSIX reader would report an integer where the contract is a float")
	}
	for _, want := range []string{"mtime=946684800.25\n", "mtime=946684800.75\n", "mtime=946771200.5\n"} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Fatalf("the archive carries no %q record", want)
		}
	}
}

func ungzip(t *testing.T, data []byte) []byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestOwnershipAndModeAreNormalised(t *testing.T) {
	// uid/gid/uname/gname/mode ARE normalised, since none of them reaches the reader —
	// and leaving them as the serving process's own would make the archive a fact about
	// the pod rather than about the store.
	root := fixtureStore(t)
	if err := os.Chmod(filepath.Join(root, "alpha-notes", "gadget-one.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Build(root, "", store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range members(t, result.Archive) {
		if header.Mode != 0o644 {
			t.Fatalf("%s has mode %o, want 0644", header.Name, header.Mode)
		}
		if header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" {
			t.Fatalf("%s carries ownership %+v", header.Name, header)
		}
	}
}

func TestTheAllowlistFiltersTheCandidateList(t *testing.T) {
	root := fixtureStore(t)

	t.Run("a narrow principal gets only its own scope", func(t *testing.T) {
		// 🔴 THE FOURTH ENUMERATION CHANNEL, AND THE ONLY ONE THE INDEX CANNOT CLOSE.
		// This route walks the store root directly, so without the filter a caller
		// allowed one scope could download EVERY scope's entry files.
		result, err := Build(root, "", store.VisibleScopeSet([]string{"beta-notes"}))
		if err != nil {
			t.Fatal(err)
		}
		names := memberNames(t, result.Archive)
		if strings.Join(names, ",") != SeedStampName+",beta-notes/widget-three.md" {
			t.Fatalf("a narrowed snapshot must hold only the caller's scope: %v", names)
		}
		if result.Entries != 1 {
			t.Fatalf("the server's own count must describe the FILTERED set, got %d", result.Entries)
		}
	})

	t.Run("a refused scope filter yields the stamp and nothing else", func(t *testing.T) {
		result, err := Build(root, "alpha-notes", store.VisibleScopeSet([]string{"beta-notes"}))
		if err != nil {
			t.Fatal(err)
		}
		if names := memberNames(t, result.Archive); strings.Join(names, ",") != SeedStampName {
			t.Fatalf("got %v", names)
		}
	})

	t.Run("a refused scope and an absent one produce the SAME archive", func(t *testing.T) {
		// The enumeration property on this route: the answer must not tell an
		// unauthorised caller which scopes exist.
		refused, err := Build(root, "alpha-notes", store.VisibleScopeSet([]string{"beta-notes"}))
		if err != nil {
			t.Fatal(err)
		}
		absent, err := Build(root, "ghost-void", store.VisibleScopeSet([]string{"beta-notes"}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(memberNames(t, refused.Archive), ",") !=
			strings.Join(memberNames(t, absent.Archive), ",") ||
			refused.Entries != absent.Entries {
			t.Fatal("a refused scope filter is distinguishable from an absent one")
		}
	})

	t.Run("an empty allowlist ships no entry at all", func(t *testing.T) {
		result, err := Build(root, "", store.VisibleScopeSet(nil))
		if err != nil {
			t.Fatal(err)
		}
		if result.Entries != 0 {
			t.Fatalf("an empty allowlist is the OPPOSITE of unrestricted, got %d entries", result.Entries)
		}
	})
}

func memberNames(t *testing.T, archive []byte) []string {
	t.Helper()
	var out []string
	for _, header := range members(t, archive) {
		out = append(out, header.Name)
	}
	return out
}

func TestAnUnreadableThingIsREPORTEDAndNotOmitted(t *testing.T) {
	// 🔴 AN UNREADABLE SCOPE IS NOT AN EMPTY ONE, AND A DIRECTORY LISTING CANNOT TELL
	// YOU WHICH IT SAW. The first version of the oracle's handler answered 200 with exit
	// 0 for an unreadable scope, the tar silently omitted it, and the client rendered
	// "nothing recorded" — the exact lie this route's client exists to prevent.
	t.Run("a dangling link named *.md is refused, not skipped", func(t *testing.T) {
		root := fixtureStore(t)
		lock := filepath.Join(root, "alpha-notes", ".#gadget-one.md")
		if err := os.Symlink(filepath.Join(root, "nowhere"), lock); err != nil {
			t.Skipf("no symlinks here: %v", err)
		}
		result, err := Build(root, "", store.Unrestricted())
		if err != nil {
			t.Fatal(err)
		}
		// 🔴 NAME RULES ARE SEPARATE FROM TYPE RULES. The dotfile skip is what keeps an
		// editor lock file out of the CANDIDATE set; conflating the two is what made one
		// open buffer 503 the entire store for every caller.
		if len(result.Unreadable) != 0 {
			t.Fatalf("a DOTFILE is not a candidate entry, so it must not be reported: %v",
				result.Unreadable)
		}
		// …and the same dangling link WITHOUT the leading dot is a candidate, and is
		// refused rather than omitted.
		visible := filepath.Join(root, "alpha-notes", "gadget-nine.md")
		if err := os.Symlink(filepath.Join(root, "nowhere"), visible); err != nil {
			t.Fatal(err)
		}
		result, err = Build(root, "", store.Unrestricted())
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Unreadable) != 1 ||
			!strings.Contains(result.Unreadable[0], "gadget-nine.md: broken-link refused") {
			t.Fatalf("a dangling candidate must be REPORTED: %v", result.Unreadable)
		}
		if result.Archive != nil {
			t.Fatal("a partial snapshot must not be built at all")
		}
	})

	t.Run("a fifo named *.md is refused BEFORE it is opened", func(t *testing.T) {
		// Reading a fifo blocks until somebody writes to it, so this is not a
		// degradation — it is a hang, and it has to be refused by KIND rather than
		// discovered by opening.
		root := fixtureStore(t)
		fifo := filepath.Join(root, "alpha-notes", "hang.md")
		if err := syscall.Mkfifo(fifo, 0o644); err != nil {
			t.Skipf("no fifos here: %v", err)
		}
		done := make(chan Result, 1)
		go func() {
			result, _ := Build(root, "", store.Unrestricted())
			done <- result
		}()
		select {
		case result := <-done:
			if len(result.Unreadable) != 1 || !strings.Contains(result.Unreadable[0], "other refused") {
				t.Fatalf("want the fifo reported as refused, got %v", result.Unreadable)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Build did not return: a fifo named `*.md` wedged the walk, which is " +
				"the worst outcome available on a single-replica service")
		}
	})
}

func TestFreshnessNamesEveryFailureState(t *testing.T) {
	t.Run("a seeded store reports both facts", func(t *testing.T) {
		root := fixtureStore(t)
		header, prose := Freshness(root)
		want := "seeded=2000-01-04T00:00:00Z newest=2000-01-02T00:00:00Z entry-files=3"
		if header != want {
			t.Fatalf("\n got: %s\nwant: %s", header, want)
		}
		if !strings.Contains(prose, "SNAPSHOT, NOT THE SOURCE") {
			t.Fatalf("the prose must lead with the caveat: %s", prose)
		}
	})

	// 🔴 THE ORACLE WAS CORRECTED FOR THIS ONE, AND THIS SIDE MATCHES THE CORRECTION.
	// `seeded` goes out as `X-Store-Snapshot: seeded=<value>`; `http.server` encodes a
	// header value as latin-1, and nothing validated the value. Three undesigned outcomes,
	// all measured on the oracle: a latin-1-encodable character went on the wire as ONE
	// byte where a UTF-8 writer sends TWO (a silent divergence in a pinned header); an
	// astral character made `send_header` raise AFTER the status line, TRUNCATING the
	// response (`curl` exit 8); and an invalid UTF-8 byte 503'd a readable store. The value
	// is now constrained to printable ASCII on both sides, and anything else is the
	// `UNREADABLE` state this contract already defines.
	t.Run("a stamp that cannot go in a header is UNREADABLE", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			body []byte
		}{
			// Built from code points, never pasted: an invisible byte in source is
			// unreviewable, which is this repo's rule.
			{"latin-1 encodable", []byte("2000-01-01 caf" + string(rune(0x00e9)) + "\n")},
			{"astral", []byte("2000-01-01 " + string(rune(0x1F600)) + "\n")},
			{"not valid UTF-8", []byte{'2', '0', '0', '0', ' ', 0xff, '\n'}},
			{"a control character", []byte{'2', '0', '0', '0', 0x01, 'o', 'k', '\n'}},
			{"DEL", []byte{'2', '0', '0', '0', 0x7f, 'o', 'k', '\n'}},
			// 🔴 THE ROW A MUTATION SWEEP DEMANDED, AND IT IS THE ONLY ONE THAT REACHES
			// THE STRICT DECODE. Every row above is ALSO caught by `headerSafe`, because
			// a byte sequence that is not valid UTF-8 must contain a byte >= 0x80 and
			// `headerSafe` refuses anything above 0x7E — so deleting the decode SURVIVED
			// the whole table. Here the FIRST LINE is pure ASCII and the bad byte is on
			// the SECOND: `headerSafe` sees nothing wrong with `seeded`, while the
			// oracle's `read_text(encoding="utf-8")` is strict over the WHOLE FILE and
			// answers UNREADABLE. Measured on the oracle before this row was written.
			{"an ASCII first line and a bad byte later",
				[]byte("2000-01-01T00:00:00Z\nhost=\xff\n")},
		} {
			root := fixtureStore(t)
			if err := os.WriteFile(filepath.Join(root, SeedStampName), tc.body, 0o644); err != nil {
				t.Fatal(err)
			}
			header, _ := Freshness(root)
			if !strings.Contains(header, "seeded=UNREADABLE") {
				t.Fatalf("%s: got %q", tc.name, header)
			}
			// 🔴 AND THE WHOLE HEADER MUST BE PRINTABLE ASCII — the property the
			// truncation case violated. Asserting only the `UNREADABLE` substring would
			// pass for a header that still carried the offending bytes elsewhere.
			if !headerSafe(header) {
				t.Fatalf("%s: the header is not emittable: %q", tc.name, header)
			}
		}

		// 🔴 THE POSITIVE CONTROL. Without it the whole subtest passes with `seeded`
		// hardcoded to `UNREADABLE`, which would hide every real date — the exact
		// "reassuring value from a check wired to nothing" shape.
		root := fixtureStore(t)
		header, _ := Freshness(root)
		if !strings.Contains(header, "seeded=2000-01-04T00:00:00Z") {
			t.Fatalf("an ordinary ASCII stamp must still be reported verbatim: %q", header)
		}
	})

	t.Run("a missing stamp is UNSTAMPED and not a fabricated date", func(t *testing.T) {
		root := fixtureStore(t)
		if err := os.Remove(filepath.Join(root, SeedStampName)); err != nil {
			t.Fatal(err)
		}
		header, _ := Freshness(root)
		if !strings.Contains(header, "seeded=UNSTAMPED") {
			t.Fatalf("got %s", header)
		}
	})

	t.Run("an empty store is NONE with a zero count, which is not UNREADABLE", func(t *testing.T) {
		// 🔴 EVERY FAILURE IS ITS OWN NAMED STATE. The genuinely empty store and the
		// walk that could not look must not render alike, because one of them is a lie.
		root := t.TempDir()
		header, _ := Freshness(root)
		if header != "seeded=UNSTAMPED newest=NONE entry-files=0" {
			t.Fatalf("got %s", header)
		}
	})

	t.Run("an unreadable scope is UNREADABLE and not an empty store", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root: a mode-000 directory is still readable, so this " +
				"case cannot be produced and a green here would prove nothing")
		}
		root := fixtureStore(t)
		locked := filepath.Join(root, "alpha-notes")
		if err := os.Chmod(locked, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		header, _ := Freshness(root)
		if !strings.Contains(header, "newest=UNREADABLE") {
			t.Fatalf("an unreadable scope must be UNREADABLE, not a confident zero: %s", header)
		}
	})

	t.Run("a file deeper than the cap is not counted", func(t *testing.T) {
		root := fixtureStore(t)
		writeEntry(t, filepath.Join(root, "alpha-notes", "nested", "deep.md"), "x\n", 947000000, 0)
		header, _ := Freshness(root)
		if !strings.Contains(header, "entry-files=3") {
			t.Fatalf("only `<scope>/*.md` is counted: %s", header)
		}
		if strings.Contains(header, "UNREADABLE") {
			t.Fatalf("a readable subdirectory is not a failure: %s", header)
		}
	})
}

func TestTheActionTablesAreTotalAndTheEntryOneIsWider(t *testing.T) {
	for _, kind := range store.AllKinds {
		if _, ok := RootAction(kind); !ok {
			t.Fatalf("the root table has no row for %q", kind)
		}
		if _, ok := EntryAction(kind); !ok {
			t.Fatalf("the entry table has no row for %q", kind)
		}
	}
	// 🔴 THIS ROUTE'S ENTRY TABLE IS DELIBERATELY WIDER THAN THE INDEX LOADER'S, and
	// the one cell that says so is `link-to-file`: the loader READS a symlink to a
	// regular entry and has always done so, while an archive that followed one would
	// ship bytes from outside the store. The action is a property of the CONTEXT, and a
	// mutant that made the two agree would silently change one of them.
	snapshotAction, _ := EntryAction(store.KindLinkToFile)
	loaderAction, _ := store.LoaderAction(store.KindLinkToFile)
	if snapshotAction != store.Refuse {
		t.Fatalf("the snapshot must refuse a symlinked entry, got %q", snapshotAction)
	}
	if loaderAction != store.Take {
		t.Fatalf("the index loader must still read one, got %q", loaderAction)
	}
}
