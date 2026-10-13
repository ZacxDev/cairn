package archive

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/ZacxDev/cairn/internal/write"
)

// 🔴 PLUGIN OUTPUTS LIVE IN THE SESSION'S OWN DIRECTORY (decision 15: `outputs.jsonl`), AND THAT IS
// THE WHOLE DELETION CASCADE'S MECHANISM. Retention, a withdrawal and the owner's delete are each an
// `rm -r` of `sessions/<root>/`, so an output can never outlive the transcript it was derived from.
// Slice S5 adds only the minimal storage the plugin API's WRITE route needs to be meaningful (the
// pending query reads the plugin's latest watermark back from here); S6 renders outputs and owns the
// cascade's journal fact.
//
// The archive does not interpret a line: `internal/plugins` validates and stamps the envelope before
// it reaches [Archive.AppendOutput], and the archive only bounds, counts and appends it.

const outputsName = "outputs.jsonl"

// MaxOutputLine bounds one stored output line.
const MaxOutputLine = 128 << 10

// ErrNoSession is an output for a root the archive does not hold (never stored, swept, or deleted
// since the caller decided). One value, so the caller answers it as it answers every other miss.
var ErrNoSession = errors.New("no stored session")

// AppendOutput appends ONE output line to a stored session's `outputs.jsonl`, counted against the
// quota. The line must not contain a newline.
func (a *Archive) AppendOutput(root string, line []byte) error {
	if !write.SessionComponent.MatchString(root) {
		return ErrNoSession
	}
	if len(line) == 0 || len(line) > MaxOutputLine || bytes.IndexByte(line, '\n') >= 0 {
		return invalid("an output line must be 1–%d bytes with no newline", MaxOutputLine)
	}
	l := a.rootLock(root)
	l.Lock()
	defer l.Unlock()
	m, err := a.readMeta(root)
	if err != nil {
		return err
	}
	if m == nil {
		return ErrNoSession
	}
	n := int64(len(line) + 1)
	if err := a.reserve(n); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(a.sessionDir(root), outputsName),
		os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, fileMode)
	if err != nil {
		a.release(n)
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(append([]byte{}, line...), '\n')); err != nil {
		a.release(n)
		return err
	}
	return f.Sync()
}

// ScanOutputs calls fn for every stored output line of a session, in the order written. A session
// with no outputs (or none stored) calls fn zero times.
func (a *Archive) ScanOutputs(root string, fn func([]byte) error) error {
	if !write.SessionComponent.MatchString(root) {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(a.sessionDir(root), outputsName), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), MaxOutputLine+1)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		if err := fn(sc.Bytes()); errors.Is(err, StopScan) {
			return nil
		} else if err != nil {
			return err
		}
	}
	return sc.Err()
}
