package capture

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Record is one redacted unit bound for a stream: `Src` names where it came from (a byte offset,
// or an opencode unit key and version) and `Rec` is the redacted record.
type Record struct {
	Src string          `json:"src"`
	Rec json.RawMessage `json:"rec,omitempty"`
	// Raw carries a line that was not JSON, as text.
	Raw string `json:"raw,omitempty"`
}

// Sink receives what the agent would upload. S2's only Sink is [FileSpool]; S3 adds the pod.
type Sink interface {
	Records(instance, root, stream string, recs []Record) error
	Blob(instance, root, name string, data []byte) error
	Withdraw(instance, root string) error
}

// FileSpool writes `<dir>/<instance>/<root>/stream-<stream>.jsonl`, `…/blobs/<name>` and
// `<dir>/<instance>/withdrawals.jsonl`, directories 0700 and files 0600.
type FileSpool struct{ Dir string }

var errUnsafeName = errors.New("capture: a stream or blob name that is not a plain relative path")

func safeJoin(base string, parts ...string) (string, error) {
	for _, p := range parts {
		if p == "" || strings.Contains(p, "..") || filepath.IsAbs(p) {
			return "", errUnsafeName
		}
	}
	return filepath.Join(append([]string{base}, parts...)...), nil
}

func appendLines(path string, lines [][]byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	for _, l := range lines {
		if _, err := f.Write(append(l, '\n')); err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Records appends recs to the stream's spool file.
func (s FileSpool) Records(instance, root, stream string, recs []Record) error {
	name := "stream-" + strings.ReplaceAll(stream, ":", "_") + ".jsonl"
	p, err := safeJoin(s.Dir, instance, root, name)
	if err != nil {
		return err
	}
	lines := make([][]byte, 0, len(recs))
	for _, r := range recs {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		lines = append(lines, b)
	}
	return appendLines(p, lines)
}

// Blob writes one blob, replacing an earlier version.
func (s FileSpool) Blob(instance, root, name string, data []byte) error {
	p, err := safeJoin(s.Dir, instance, root, "blobs", name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// Withdraw records a withdrawal of root's prefix from instance (decision 16, clause m's agent
// half). The pod's `withdraw` route that would run the deletion cascade is S6.
func (s FileSpool) Withdraw(instance, root string) error {
	p, err := safeJoin(s.Dir, instance, "withdrawals.jsonl")
	if err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]string{"withdraw": root})
	return appendLines(p, [][]byte{b})
}
