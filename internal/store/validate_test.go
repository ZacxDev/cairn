package store

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// validateEntry writes one synthetic, well-formed entry file and returns its path.
func validateEntry(t *testing.T, dir, service, scope, extra string) string {
	t.Helper()
	head := "---\nservice: " + service + "\nscope: " + scope + "\n"
	if extra != "" {
		head += extra + "\n"
	}
	body := head + "---\n\n## What it is\n\nsynthetic.\n\n## Pointers\n\n- none\n\n" +
		"## Nuance / work-history\n\n- 2000-01-01: synthetic.\n"
	path := filepath.Join(dir, service+".md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// validateStore is a root holding ONE scope with ONE readable entry.
func validateStore(t *testing.T) (root, scopeDir string) {
	t.Helper()
	root = t.TempDir()
	scopeDir = filepath.Join(root, "widget-cfg")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	validateEntry(t, scopeDir, "thing-alpha", "widget-cfg", "")
	return root, scopeDir
}

func TestValidateScopeReportsWhatItWALKEDIncludingTheRejected(t *testing.T) {
	// 🔴 `checked` IS "FILES WALKED", NOT "FILES THAT PARSED". If a rejected file dropped
	// out of it the caller's `N of M` would take its numerator and denominator from two
	// different populations, and the zero it accompanies would mean nothing.
	root, scopeDir := validateStore(t)
	validateEntry(t, scopeDir, "thing-beta", "widget-cfg", "aliases: not-a-list")

	checked, malformed, err := ValidateScope(root, "widget-cfg")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"thing-alpha.md", "thing-beta.md"}; !reflect.DeepEqual(checked, want) {
		t.Fatalf("checked = %v, want %v", checked, want)
	}
	if len(malformed) != 1 || malformed[0].Filename != "thing-beta.md" {
		t.Fatalf("malformed = %+v, want exactly thing-beta.md", malformed)
	}
	if malformed[0].Reason != "`aliases:` must be a list, not a bare string" {
		t.Fatalf("reason = %q", malformed[0].Reason)
	}
}

func TestValidateScopeSkipsTheScopesREADME(t *testing.T) {
	// 🔴 THE MISCOUNT THIS FUNCTION EXISTS TO CLOSE. Every scope directory carries a
	// `README.md` as its policy sheet, `LoadIndex` skips it, and `/snapshot` ships it — so
	// the caller's old `Glob("*.md")` denominator counted a file nothing ever parsed. A
	// scope holding one entry beside its README printed `2 of 2 entry file(s) parse`.
	root, scopeDir := validateStore(t)
	if err := os.WriteFile(filepath.Join(scopeDir, "README.md"),
		[]byte("# the scope's own policy sheet\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	checked, malformed, err := ValidateScope(root, "widget-cfg")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"thing-alpha.md"}; !reflect.DeepEqual(checked, want) {
		t.Fatalf("checked = %v, want %v — README.md is in the denominator", checked, want)
	}
	if len(malformed) != 0 {
		t.Fatalf("malformed = %+v, want none", malformed)
	}
}

func TestValidateScopeAScopeHoldingONLYAREADMEIsTwoEmpties(t *testing.T) {
	// The sharpest form: without the exclusion this directory reports one file walked and
	// zero malformed — a clean bill of health over a scope with no entries in it at all.
	root, _ := validateStore(t)
	hollow := filepath.Join(root, "hollow-area")
	if err := os.MkdirAll(hollow, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hollow, "README.md"),
		[]byte("# policy sheet\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	checked, malformed, err := ValidateScope(root, "hollow-area")
	if err != nil {
		t.Fatal(err)
	}
	if len(checked) != 0 || len(malformed) != 0 {
		t.Fatalf("checked=%v malformed=%+v, want two empties", checked, malformed)
	}
}

func TestValidateScopeSeesTheDuplicateRefNoPerFileLoopCould(t *testing.T) {
	// 🔴 A DUPLICATE IS A RELATIONSHIP BETWEEN TWO FILES, which is why this goes through
	// the index build rather than looping a single-file check. Both files here are
	// individually well-formed — each one's filename slug agrees with its own `service:` —
	// and they collide only once both refs are folded.
	root, scopeDir := validateStore(t)
	validateEntry(t, scopeDir, "thing_alpha", "widget-cfg", "")

	checked, malformed, err := ValidateScope(root, "widget-cfg")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"thing-alpha.md", "thing_alpha.md"}; !reflect.DeepEqual(checked, want) {
		t.Fatalf("checked = %v, want %v", checked, want)
	}
	if len(malformed) != 1 || malformed[0].Filename != "thing_alpha.md" {
		t.Fatalf("malformed = %+v, want exactly the LATER of the pair", malformed)
	}
	if !strings.Contains(malformed[0].Reason, "duplicate 'thing-alpha'") {
		t.Fatalf("reason = %q, want the duplicate rejection", malformed[0].Reason)
	}
}

func TestValidateScopeAMissingROOTIsAnErrorNotACleanBill(t *testing.T) {
	// 🔴 NOT TWO EMPTIES. "the store is not there" and "the scope is empty" both produce no
	// rows, and one of them is a lie — so the message has to say so in its own words.
	root := t.TempDir()

	_, _, err := ValidateScope(filepath.Join(root, "no-such-store"), "widget-cfg")

	var missing *StoreMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %#v, want *StoreMissingError", err)
	}
	for _, want := range []string{"store root not found", "NOT 'the scope is clean'"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message %q does not carry %q", err.Error(), want)
		}
	}
}

func TestValidateScopeAMissingSCOPEDirectoryIsTwoEmpties(t *testing.T) {
	// The other side of the pair above: the store IS there, this scope simply holds nothing
	// yet. That is an honest empty, not an error.
	root, _ := validateStore(t)

	checked, malformed, err := ValidateScope(root, "never-created")
	if err != nil {
		t.Fatalf("a scope that does not exist is not an error: %v", err)
	}
	if len(checked) != 0 || len(malformed) != 0 {
		t.Fatalf("checked=%v malformed=%+v, want two empties", checked, malformed)
	}
}

func TestValidateScopeFoldsTheScopeNameBeforeOpeningTheDirectory(t *testing.T) {
	// Without this a caller spelling the scope `Widget_Cfg` gets "nothing here" about its
	// OWN scope, because the index key derived from a directory name is folded.
	root, _ := validateStore(t)

	checked, _, err := ValidateScope(root, "Widget_Cfg")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"thing-alpha.md"}; !reflect.DeepEqual(checked, want) {
		t.Fatalf("checked = %v, want %v", checked, want)
	}
}

func TestValidateScopeDoesNotReportAnotherScopesRejections(t *testing.T) {
	// The index is loaded whole, so the filter to one scope is load-bearing: without it a
	// broken file anywhere in the store would dirty every scope's verdict.
	root, _ := validateStore(t)
	other := filepath.Join(root, "gizmo-notes")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	validateEntry(t, other, "other-thing", "gizmo-notes", "aliases: not-a-list")

	checked, malformed, err := ValidateScope(root, "widget-cfg")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"thing-alpha.md"}; !reflect.DeepEqual(checked, want) {
		t.Fatalf("checked = %v, want %v", checked, want)
	}
	if len(malformed) != 0 {
		t.Fatalf("malformed = %+v, want none — that rejection is another scope's", malformed)
	}
}

func TestValidateScopeAFileIsNotAStoreRoot(t *testing.T) {
	// `os.Stat` succeeds on a regular file; only `IsDir()` separates it from a root.
	root := t.TempDir()
	notADir := filepath.Join(root, "a-file")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ValidateScope(notADir, "widget-cfg")

	var missing *StoreMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %#v, want *StoreMissingError", err)
	}
}
