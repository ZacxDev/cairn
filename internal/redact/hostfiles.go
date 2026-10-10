package redact

import (
	"bufio"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"syscall"
)

// Denylist is the per-host list of literal strings and path globs, read from a local 0600 file
// that never enters the repository.
//
// The file is one entry per line: `literal:<text>` (redacted wherever it appears, rule
// `denylist`) or `glob:<pattern>` (`path.Match` syntax, matched against a persisted blob's name and
// against a record object's `filePath`/`file_path`; a match redacts the blob, or that object's
// `content`/`originalFile`/`base64` value, WHOLE — rule `denylist-path`). Blank lines and `#`
// comments are ignored; anything else is an error.
type Denylist struct {
	Literals []string
	Globs    []string
}

// MinLiteral is the shortest literal accepted: shorter ones would redact ordinary words.
const MinLiteral = 4

// LoadDenylist reads a denylist file.
//
// 🔴 IT REFUSES A FILE ANYONE BUT ITS OWNER CAN READ, AND A SYMLINK. The file holds the secrets
// the operator most wants caught, so a group- or world-readable copy is a leak of exactly those;
// and `O_NOFOLLOW` keeps the open on the path that was checked.
func LoadDenylist(p string) (*Denylist, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("redact: the denylist %s could not be opened: %w", p, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("redact: the denylist %s is not a regular file", p)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("redact: the denylist %s is readable by others (mode %o); it must be 0600", p, st.Mode().Perm())
	}
	return parseDenylist(f, p)
}

func parseDenylist(r io.Reader, name string) (*Denylist, error) {
	d := &Denylist{}
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "literal:"):
			lit := strings.TrimPrefix(line, "literal:")
			if len(lit) < MinLiteral {
				return nil, fmt.Errorf("redact: %s:%d: a literal shorter than %d characters would redact ordinary text", name, n, MinLiteral)
			}
			d.Literals = append(d.Literals, lit)
		case strings.HasPrefix(line, "glob:"):
			g := strings.TrimPrefix(line, "glob:")
			if _, err := path.Match(g, ""); err != nil {
				return nil, fmt.Errorf("redact: %s:%d: bad glob: %v", name, n, err)
			}
			d.Globs = append(d.Globs, g)
		default:
			return nil, fmt.Errorf("redact: %s:%d: an entry must start with `literal:` or `glob:`", name, n)
		}
	}
	return d, sc.Err()
}

// HostKeyBytes is the size of a generated host key.
const HostKeyBytes = 32

// LoadOrCreateKey returns the per-host tag key at p, creating it (0600, exclusive) on first use.
//
// 🔴 THE KEY IS WHAT MAKES A TAG UNCONFIRMABLE (T15), so the file gets the denylist's discipline:
// no symlink, owner-only, and a short or unreadable key is an ERROR rather than a regenerated one —
// a silently new key would make every tag on this host stop matching its earlier occurrences.
func LoadOrCreateKey(p string) ([]byte, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, HostKeyBytes)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		w, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
		if err != nil {
			return nil, fmt.Errorf("redact: the host key %s could not be created: %w", p, err)
		}
		if _, err := w.Write(key); err != nil {
			w.Close()
			return nil, err
		}
		if err := w.Sync(); err != nil {
			w.Close()
			return nil, err
		}
		return key, w.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("redact: the host key %s could not be opened: %w", p, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("redact: the host key %s must be a regular 0600 file (mode %o)", p, st.Mode().Perm())
	}
	key, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if len(key) < MinKeyBytes {
		return nil, fmt.Errorf("redact: the host key %s holds %d bytes; refusing rather than regenerating it", p, len(key))
	}
	return key, nil
}
