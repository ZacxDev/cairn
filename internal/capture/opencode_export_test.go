package capture

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExportUsesV2SessionExportAndFallsBackToLegacy pins the argv the capture pass sends to the
// opencode binary.
//
// 🔴 MEASURED: v2 moved `export` under `session`, so the top-level `opencode export <id>` prints
// HELP and exits non-zero. cairn-capture still asked for the old form, so every opencode capture
// was refused — `capture: opencode [export ses_…]: exit status 1`, nothing reaching the store —
// while `opencode session export <ses_…>` answers the session JSON with exit 0.
func TestExportUsesV2SessionExportAndFallsBackToLegacy(t *testing.T) {
	t.Run("v2 canonical", func(t *testing.T) {
		stub := t.TempDir()
		if err := os.WriteFile(filepath.Join(stub, "export-ses_x.json"),
			[]byte(`{"info":{"id":"ses_x"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CAPTURE_OPENCODE_STUB", stub)
		out, err := ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()}.Export("ses_x")
		if err != nil {
			t.Fatalf("v2 export: %v", err)
		}
		if string(out) != `{"info":{"id":"ses_x"}}` {
			t.Fatalf("export returned %q; want the stub document unchanged", out)
		}
		called, err := os.ReadFile(filepath.Join(stub, "v2-export-called"))
		if err != nil || string(called) != "session export ses_x" {
			t.Fatalf("v2 export command was not called as specified: %q (%v)", called, err)
		}
		if _, err := os.Stat(filepath.Join(stub, "legacy-export-called")); !os.IsNotExist(err) {
			t.Fatalf("legacy fallback ran despite a successful v2 answer: %v", err)
		}
	})

	t.Run("legacy fallback when the v2 verb is unavailable", func(t *testing.T) {
		stub := t.TempDir()
		if err := os.WriteFile(filepath.Join(stub, "v1-only"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stub, "export-ses_y.json"),
			[]byte(`{"info":{"id":"ses_y"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CAPTURE_OPENCODE_STUB", stub)
		out, err := ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()}.Export("ses_y")
		if err != nil {
			t.Fatalf("legacy fallback export: %v", err)
		}
		if string(out) != `{"info":{"id":"ses_y"}}` {
			t.Fatalf("export returned %q; want the stub document unchanged", out)
		}
		if _, err := os.Stat(filepath.Join(stub, "v2-export-called")); err != nil {
			t.Fatalf("the v2 verb was not attempted before the fallback: %v", err)
		}
		called, err := os.ReadFile(filepath.Join(stub, "legacy-export-called"))
		if err != nil || string(called) != "export ses_y" {
			t.Fatalf("legacy fallback command: %q (%v)", called, err)
		}
	})
}
