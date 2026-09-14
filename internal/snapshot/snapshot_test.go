package snapshot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
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
