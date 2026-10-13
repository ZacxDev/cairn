package secretfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestACredentialFileIsOwnerOnlyOneLineAndNeverEchoed — the modes taken and refused, the content
// rules, and that no error names the contents.
func TestACredentialFileIsOwnerOnlyOneLineAndNeverEchoed(t *testing.T) {
	const secret = "credential-synthetic-0003"
	dir := t.TempDir()
	write := func(name, content string, mode os.FileMode) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, mode := range []os.FileMode{0o600, 0o400} {
		if got, err := Read(write("ok", secret+"\n", mode)); err != nil || got != secret {
			t.Errorf("mode %o: %q %v, want the trimmed secret", mode, got, err)
		}
		_ = os.Chmod(filepath.Join(dir, "ok"), 0o600)
	}
	for _, mode := range []os.FileMode{0o640, 0o604, 0o620, 0o602, 0o644, 0o660} {
		_, err := Read(write("loose", secret+"\n", mode))
		if !errors.Is(err, ErrLoose) {
			t.Errorf("mode %o: %v, want ErrLoose", mode, err)
		}
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Errorf("mode %o: the error carries the secret", mode)
		}
	}
	for name, content := range map[string]string{
		"empty": "", "blank": " \n\n", "two-lines": secret + "\nsecond\n", "large": strings.Repeat("x", maxSecret+1),
	} {
		_, err := Read(write(name, content, 0o600))
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Errorf("%s: %v, want a refusal that does not carry the contents", name, err)
		}
	}
	if _, err := Read(filepath.Join(dir, "absent")); err == nil {
		t.Error("a missing file was read")
	}
	if _, err := Read(""); err == nil {
		t.Error("no path was read")
	}
	if _, err := Read(dir); err == nil {
		t.Error("a directory was read")
	}
}
