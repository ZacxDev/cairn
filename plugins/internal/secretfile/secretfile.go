// Package secretfile reads a credential from a file — the ONLY way a plugin in this module takes one.
//
// 🔴 NEVER ARGV, NEVER AN ENVIRONMENT VARIABLE, NEVER A LOG LINE. A flag value is readable by every
// local user through the process table and is kept by shell history; an environment variable is
// inherited by every child and dumped by every crash reporter that dumps the environment. A file the
// operator owns, mode 0600, is the one place a key can sit that neither of those reaches. The error
// texts below name the PATH and never the contents.
package secretfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// maxSecret bounds what is read: an API key or a token is short, and a path that names something
// large is a mistake, not a key.
const maxSecret = 4096

// ErrLoose is a credential file that a group or other user can read or write.
var ErrLoose = errors.New("the credential file is readable or writable by a group or other users (want mode 0600 or 0400)")

// Read returns the file's single line, trimmed. It refuses a file other users can reach, an empty
// file, and a file holding more than one line.
func Read(path string) (string, error) {
	if path == "" {
		return "", errors.New("no credential file was named")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("the credential file %s cannot be opened: %w", path, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("the credential file %s cannot be read: %w", path, err)
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("the credential file %s is not a regular file", path)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s: %w", path, ErrLoose)
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxSecret+1))
	if err != nil {
		return "", fmt.Errorf("the credential file %s cannot be read: %w", path, err)
	}
	if len(raw) > maxSecret {
		return "", fmt.Errorf("the credential file %s is over %d bytes — not a key", path, maxSecret)
	}
	s := strings.TrimSpace(string(raw))
	switch {
	case s == "":
		return "", fmt.Errorf("the credential file %s is empty", path)
	case strings.ContainsAny(s, "\r\n"):
		return "", fmt.Errorf("the credential file %s holds more than one line", path)
	}
	return s, nil
}
