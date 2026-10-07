package presence

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

// TokenKind is what a presence token may do. Exactly two, one per agent route (decision 3):
// least privilege PER PROCESS on the host — the push unit cannot suppress rings and the claim
// service cannot make a badge lie.
type TokenKind string

const (
	// KindPush may only REPLACE its one host's presence set.
	KindPush TokenKind = "push"
	// KindClaim may only claim the rings queued for its one host.
	KindClaim TokenKind = "claim"
)

// Valid reports whether k is one of the two kinds.
func (k TokenKind) Valid() bool { return k == KindPush || k == KindClaim }

// TokenRow is one line of the `-presence-tokens` file: a DIGEST bound to a kind, an owner and
// ONE host label. The token itself is never stored.
type TokenRow struct {
	Kind   TokenKind
	Owner  Owner
	Host   string
	Digest string
	// Line is the 1-based line number, for a refusal to name.
	Line int
}

// DigestPrefix is what a log line may carry about a row: enough to find it in the file, never
// the token and never the whole digest.
func (r TokenRow) DigestPrefix() string { return r.Digest[:12] }

// String renders the row exactly as the file stores it.
func (r TokenRow) String() string {
	return fmt.Sprintf("%s %s %s %s", r.Kind, r.Owner, r.Host, r.Digest)
}

// hostLabel is the class a host label must match: the same shape as a session id's leading
// class, which admits `host-a` and refuses whitespace, `/` and anything a log or a page could
// be confused by.
var hostLabel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

var hexDigest = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// ValidHostLabel reports whether s may be a host label.
func ValidHostLabel(s string) bool { return hostLabel.MatchString(s) }

// parseTokenLine reads one non-comment line.
func parseTokenLine(line string, n int) (TokenRow, error) {
	fields := strings.Fields(line)
	if len(fields) != 4 {
		return TokenRow{}, fmt.Errorf("line %d: want 4 fields `<push|claim> <kind>:<id> <host> <sha256-hex>`, got %d", n, len(fields))
	}
	kind := TokenKind(fields[0])
	if !kind.Valid() {
		return TokenRow{}, fmt.Errorf("line %d: token kind %q is not %q or %q", n, fields[0], KindPush, KindClaim)
	}
	owner, err := ParseOwner(fields[1])
	if err != nil {
		return TokenRow{}, fmt.Errorf("line %d: %v", n, err)
	}
	if !ValidHostLabel(fields[2]) {
		return TokenRow{}, fmt.Errorf("line %d: host label %q does not match %s", n, fields[2], hostLabel)
	}
	if !hexDigest.MatchString(fields[3]) {
		// The value is NOT echoed: a raw token pasted where the digest belongs is exactly
		// the mistake this refuses, and echoing it would put the secret in a log.
		return TokenRow{}, fmt.Errorf("line %d: the fourth field is not a 64-character hex SHA-256 digest "+
			"(a raw token pasted here is refused, and is deliberately not echoed)", n)
	}
	return TokenRow{Kind: kind, Owner: owner, Host: fields[2], Digest: strings.ToLower(fields[3]), Line: n}, nil
}

// parseTokens reads the whole file into rows and per-line problems. Duplicate digests are a
// problem for EVERY row carrying one — an ambiguous digest names no single binding.
func parseTokens(data []byte) (rows []TokenRow, problems []error) {
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		row, err := parseTokenLine(trimmed, i+1)
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
			problems = append(problems, fmt.Errorf("line %d: digest %s… appears on more than one row, so it "+
				"binds no single (kind, owner, host); every row carrying it is refused", r.Line, r.DigestPrefix()))
			continue
		}
		kept = append(kept, r)
	}
	return kept, problems
}

// ErrForeignOwner is the single-owner wall's refusal (decision 15).
var ErrForeignOwner = errors.New("the row names an owner other than this instance's -presence-owner")

// admit is THE WALL: may this row authenticate on a listener whose sole owner is `owner`.
//
// 🔴 ONE FUNCTION FOR BOTH TIMES THE QUESTION IS ASKED. At startup a refusal here refuses the
// whole listener ([LoadTokens]); for a row that appears in the re-read file AFTER startup it
// refuses THAT ROW only ([Agent]), so one bad edit cannot take the owner's own hosts offline.
// Two spellings of the check would be two answers the day one of them moved.
func admit(row TokenRow, owner Owner) error {
	if row.Owner != owner {
		return ErrForeignOwner
	}
	return nil
}

// LoadTokens is the STARTUP read: any malformed row, any duplicate digest and any row for an
// owner other than `owner` refuses the whole file. A missing file is refused too — a typo'd
// path is not an empty token set.
func LoadTokens(path string, owner Owner) ([]TokenRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("the presence token file %s cannot be read: %w", path, err)
	}
	rows, problems := parseTokens(data)
	if len(problems) > 0 {
		return nil, fmt.Errorf("the presence token file %s is malformed: %w", path, errors.Join(problems...))
	}
	for _, r := range rows {
		if err := admit(r, owner); err != nil {
			return nil, fmt.Errorf("the presence token file %s line %d (digest %s…) names owner %s: %w (%s)",
				path, r.Line, r.DigestPrefix(), r.Owner, err, owner)
		}
	}
	return rows, nil
}

// MintToken returns a fresh presence token: 32 random bytes, raw-URL base64 — 43 characters,
// the same floor `authz.MinTokenChars` sets for a bearer token.
func MintToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// AppendTokenRow adds one row to the token file, creating it at mode 0600 if absent. The row
// carries the DIGEST; the caller prints the token once and stores it nowhere.
func AppendTokenRow(path string, row TokenRow) error {
	if !row.Kind.Valid() || !row.Owner.Kind.Valid() || !ValidHostLabel(row.Host) || !hexDigest.MatchString(row.Digest) {
		return fmt.Errorf("refusing to write a malformed presence token row")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(row.String() + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// NewTokenRow binds a freshly minted token's digest to `(kind, owner, host)`.
func NewTokenRow(kind TokenKind, owner Owner, host, token string) TokenRow {
	return TokenRow{Kind: kind, Owner: owner, Host: host, Digest: control.HashToken(token)}
}
