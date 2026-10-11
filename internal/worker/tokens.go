package worker

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/ZacxDev/cairn/internal/control"
)

// TokenKind is what a worker token may do (plan decision 2). S3 ships ONE kind; `plugin` is S5's
// and a row naming it is refused until S5 gives it routes — a kind with no route would be a
// credential that authenticates nothing and is still worth stealing the day one is added.
type TokenKind string

// KindCapture may append to its owner's transcripts from its ONE host. It cannot read anything.
const KindCapture TokenKind = "capture"

// Valid reports whether k is a kind this build serves.
func (k TokenKind) Valid() bool { return k == KindCapture }

// Owner is a principal's stable key, `(Kind, ID)` — never `Display`, which is mutable.
type Owner struct {
	Kind control.Kind
	ID   control.ID
}

// String renders the owner as the flag and the token file spell it.
func (o Owner) String() string { return string(o.Kind) + ":" + string(o.ID) }

// ParseOwner reads `<kind>:<id>`.
func ParseOwner(s string) (Owner, error) {
	kind, id, ok := strings.Cut(s, ":")
	if !ok || id == "" {
		return Owner{}, fmt.Errorf("owner %q is not <kind>:<id>", s)
	}
	o := Owner{Kind: control.Kind(kind), ID: control.ID(id)}
	if !o.Kind.Valid() {
		return Owner{}, fmt.Errorf("owner %q names kind %q, which is not %q or %q", s, kind, control.KindUser, control.KindProject)
	}
	if strings.ContainsAny(id, " \t\r\n:") {
		return Owner{}, fmt.Errorf("owner %q has an id carrying whitespace or a second ':'", s)
	}
	return o, nil
}

// TokenRow is one line of the `-worker-tokens` file: `capture <kind>:<id> <host> <sha256-hex>`.
// The token itself is never stored.
type TokenRow struct {
	Kind   TokenKind
	Owner  Owner
	Host   string
	Digest string
	Line   int
}

// DigestPrefix is what a log line may carry about a row.
func (r TokenRow) DigestPrefix() string { return r.Digest[:12] }

// String renders the row as the file stores it.
func (r TokenRow) String() string {
	return fmt.Sprintf("%s %s %s %s", r.Kind, r.Owner, r.Host, r.Digest)
}

var (
	hostLabel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	hexDigest = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

// ValidHostLabel reports whether s may be a host label.
func ValidHostLabel(s string) bool { return hostLabel.MatchString(s) }

// parseLine reads one non-comment line. 🔴 No field's VALUE is echoed — a raw token pasted into
// the wrong column is exactly the mistake these refuse, and a refusal reaches a log.
func parseLine(line string, n int) (TokenRow, error) {
	fields := strings.Fields(line)
	if len(fields) != 4 {
		return TokenRow{}, fmt.Errorf("line %d: want 4 fields `capture <kind>:<id> <host> <sha256-hex>`, got %d", n, len(fields))
	}
	kind := TokenKind(fields[0])
	if !kind.Valid() {
		return TokenRow{}, fmt.Errorf("line %d: field 1 (kind) is not %q — the only worker token kind this build serves "+
			"(a `plugin` row arrives with the plugin API)", n, KindCapture)
	}
	owner, err := ParseOwner(fields[1])
	if err != nil {
		return TokenRow{}, fmt.Errorf("line %d: field 2 (owner) is not <user|project>:<id>", n)
	}
	if !ValidHostLabel(fields[2]) {
		return TokenRow{}, fmt.Errorf("line %d: field 3 (host) does not match %s", n, hostLabel)
	}
	if !hexDigest.MatchString(fields[3]) {
		return TokenRow{}, fmt.Errorf("line %d: the fourth field is not a 64-character hex SHA-256 digest "+
			"(a raw token pasted here is refused, and is deliberately not echoed)", n)
	}
	return TokenRow{Kind: kind, Owner: owner, Host: fields[2], Digest: strings.ToLower(fields[3]), Line: n}, nil
}

// parseTokens reads the whole file. A digest on more than one row binds no single row and every
// row carrying it is refused.
func parseTokens(data []byte) (rows []TokenRow, problems []error) {
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		row, err := parseLine(trimmed, i+1)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		rows = append(rows, row)
	}
	seen := map[string]int{}
	for _, r := range rows {
		seen[r.Digest]++
	}
	kept := rows[:0]
	for _, r := range rows {
		if seen[r.Digest] > 1 {
			problems = append(problems, fmt.Errorf("line %d: digest %s… appears on more than one row; every row carrying it is refused",
				r.Line, r.DigestPrefix()))
			continue
		}
		kept = append(kept, r)
	}
	return kept, problems
}

// ErrTokenFileContent marks a startup refusal caused by what the token file SAYS, as opposed to a
// file that cannot be read.
var ErrTokenFileContent = errors.New("the worker token file's content is refused")

// ErrForeignOwner is the single-capture-owner wall (plan Q7, adopted by O13): a row for anybody
// but the instance's `-worker-owner` is refused. It also removes decision 15's squatting case — a
// second owner cannot be first to a root id the owner has not uploaded yet.
var ErrForeignOwner = errors.New("the row names an owner other than this instance's -worker-owner")

// admit is THE WALL, one function for startup and for every re-read (the presence precedent).
func admit(row TokenRow, owner Owner) error {
	if row.Owner != owner {
		return ErrForeignOwner
	}
	return nil
}

// LoadTokens is the STARTUP read: any malformed row, duplicate digest or foreign owner refuses the
// whole file ([ErrTokenFileContent]); an unreadable path is refused without that mark.
func LoadTokens(path string, owner Owner) ([]TokenRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("the worker token file %s cannot be read: %w", path, err)
	}
	rows, problems := parseTokens(data)
	if len(problems) > 0 {
		return nil, fmt.Errorf("%w: %s is malformed: %w", ErrTokenFileContent, path, errors.Join(problems...))
	}
	for _, r := range rows {
		if err := admit(r, owner); err != nil {
			return nil, fmt.Errorf("%w: %s line %d (digest %s…) names owner %s: %w (%s)",
				ErrTokenFileContent, path, r.Line, r.DigestPrefix(), r.Owner, err, owner)
		}
	}
	return rows, nil
}

// MintToken returns a fresh token: 32 random bytes, raw-URL base64 — 43 characters.
func MintToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// NewTokenRow binds a token's digest to `(kind, owner, host)`.
func NewTokenRow(kind TokenKind, owner Owner, host, token string) TokenRow {
	return TokenRow{Kind: kind, Owner: owner, Host: host, Digest: control.HashToken(token)}
}

// AppendTokenRow adds one row, creating the file at 0600. A hand-edited file whose last line has
// no newline gets one first, so the new row is never glued onto it.
func AppendTokenRow(path string, row TokenRow) error {
	if !row.Kind.Valid() || !row.Owner.Kind.Valid() || !ValidHostLabel(row.Host) || !hexDigest.MatchString(row.Digest) {
		return errors.New("refusing to write a malformed worker token row")
	}
	line := row.String() + "\n"
	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 && existing[len(existing)-1] != '\n' {
		line = "\n" + line
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
