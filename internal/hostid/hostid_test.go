package hostid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheLabelIsSANITISEDPerCODEPOINT(t *testing.T) {
	// 🔴 THE LABEL LANDS IN A RENDERED HEADER, so anything outside `[A-Za-z0-9._-]` becomes
	// `-`. Per CODE POINT and not per RUN, which is what the oracle's `re.sub` does: two
	// adjacent offending characters become two dashes, not one.
	for _, tc := range []struct{ in, want string }{
		{"plain-host", "plain-host"},
		{"has space", "has-space"},
		{"two  spaces", "two--spaces"},
		{"slash/colon:", "slash-colon-"},
		{"dots.and_under", "dots.and_under"},
		// A non-ASCII label is sanitised BY BYTE on the oracle only if the regex were
		// bytes; it is a `str` pattern, so one CODE POINT becomes one dash.
		{"héllo", "h-llo"},
	} {
		if got := sanitizeLabel(tc.in); got != tc.want {
			t.Errorf("sanitizeLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTheLabelPrefersTheFIRSTSetEnvironmentVariable(t *testing.T) {
	// The precedence order is the contract: an instance that sets two of them must get the
	// first, not whichever the map happened to iterate to.
	t.Setenv("CAIRN_HOST", "")
	t.Setenv("ASIB_HOST", "second-choice")
	t.Setenv("ACTIVITY_HOST", "third-choice")
	if got := Label(); got != "second-choice" {
		t.Fatalf("with CAIRN_HOST empty the next one wins: got %q", got)
	}
	t.Setenv("CAIRN_HOST", "first-choice")
	if got := Label(); got != "first-choice" {
		t.Fatalf("got %q", got)
	}
	// 🔴 A VALUE THAT IS ONLY WHITESPACE IS NOT SET. An operator-set variable holding a
	// space would otherwise produce a label of `-`, which reads as a real host name.
	t.Setenv("CAIRN_HOST", "   ")
	if got := Label(); got != "second-choice" {
		t.Fatalf("a whitespace-only value must fall through: got %q", got)
	}
}

func TestTheMachineIDIsSHAPECHECKEDAndNotMerelyNonEmpty(t *testing.T) {
	// 🔴 RETURNING WHATEVER JUNK A FILE HELD would make a caller's "does this prefix belong
	// to this host" answer true for any prefix containing it — an error in the FALSE
	// DATA-LOSS direction, which is the one that gets someone to act destructively.
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	good := write("good", "0123456789abcdef0123456789abcdef\n")
	short := write("short", "0123456789abcdef\n")
	upper := write("upper", "0123456789ABCDEF0123456789ABCDEF\n")
	junk := write("junk", "not a machine id at all\n")

	saved := MachineIDFiles
	t.Cleanup(func() { MachineIDFiles = saved })

	for _, tc := range []struct {
		name  string
		files []string
		want  string
	}{
		{"a well-formed id", []string{good}, "0123456789abcdef0123456789abcdef"},
		{"too short", []string{short}, ""},
		{"UPPERCASE hex is not the defined shape", []string{upper}, ""},
		{"junk", []string{junk}, ""},
		{"absent files", []string{filepath.Join(dir, "nope")}, ""},
		// The FIRST readable, well-shaped candidate wins; a junk first file does not
		// poison a good second one.
		{"falls through junk to a good one", []string{junk, good}, "0123456789abcdef0123456789abcdef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			MachineIDFiles = tc.files
			if got := MachineID(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestThisHostNeverDEGRADESToABareHostname(t *testing.T) {
	// 🔴 THE WHOLE POINT OF THE PACKAGE. A hostname may be SHARED across machines
	// provisioned from one image, so an identity that fell back to it would be identical on
	// the very machines it exists to distinguish. Every branch below still carries something
	// beyond the label, or the label already carries the id.
	saved := MachineIDFiles
	t.Cleanup(func() { MachineIDFiles = saved })
	dir := t.TempDir()
	idFile := filepath.Join(dir, "machine-id")
	if err := os.WriteFile(idFile, []byte("0123456789abcdef0123456789abcdef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAIRN_HOST", "workbench")
	t.Setenv("ASIB_HOST", "")
	t.Setenv("ACTIVITY_HOST", "")

	MachineIDFiles = []string{idFile}
	if got := ThisHost(); got != "workbench-0123456789ab" {
		t.Fatalf("got %q — the label joined to a 12-character PREFIX of the id", got)
	}

	// 🔴 UNREADABLE IS A SENTENCE, NOT AN EMPTY STRING. `workbench-` or a bare `workbench`
	// would read as a complete identity.
	MachineIDFiles = []string{filepath.Join(dir, "absent")}
	got := ThisHost()
	if got != "workbench-"+MachineIDUnreadable {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, MachineIDUnreadable) {
		t.Fatalf("the unreadable case must SAY so: %q", got)
	}

	// An operator whose label already carries the id keeps it as they set it, rather than
	// having it appended twice.
	t.Setenv("CAIRN_HOST", "box-0123456789abcdef0123456789abcdef")
	MachineIDFiles = []string{idFile}
	if got := ThisHost(); got != "box-0123456789abcdef0123456789abcdef" {
		t.Fatalf("got %q", got)
	}
}

func TestTheHostLineIsONESpelling(t *testing.T) {
	// The line is compared byte for byte against the oracle's in
	// `report.TestTheRenderedBytes…`; this pins its SHAPE where it is built, so a reader of
	// this package can see the contract without loading the renderer's fixture.
	got := StoreHostLine("some-host-000000000000", "  ")
	want := "  host: some-host-000000000000  (" + StoreIsPerHost + ")"
	if got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
	// TWO spaces before the parenthesis, which is the one piece of this string a
	// transcription loses silently.
	if !strings.Contains(got, "000  (the store") {
		t.Fatalf("the double space before the caveat is part of the line: %q", got)
	}
}
