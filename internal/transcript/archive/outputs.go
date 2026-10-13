package archive

import (
	"bufio"
	"bytes"
	"encoding/json"
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
// The archive does not interpret a line beyond its `plugin` field: `internal/plugins` validates and
// stamps the envelope before it reaches [Archive.AppendOutput].
//
// 🔴 OUTPUTS CANNOT FILL THE CAPTURE QUOTA (#270 round 1). They are counted against the instance
// quota like every stored byte, AND bounded twice more: ONE plugin's outputs at most
// 1/[OutputPluginShareDivisor] of the quota (5%), and ALL plugins' together at most
// 1/[OutputTotalShareDivisor] (20%). So capture always keeps at least 80% of the quota whatever the
// registered plugins — or a stolen plugin token (T6) — write, and one plugin cannot starve another.
// The numbers are a judgement, not a measurement: a summary is a few KiB per session, so 5% of the
// personal instance's ~20 GB is far beyond honest use and well short of harming capture.

const outputsName = "outputs.jsonl"

// MaxOutputLine bounds one stored output line.
const MaxOutputLine = 128 << 10

// The output shares of the quota.
const (
	OutputPluginShareDivisor = 20
	OutputTotalShareDivisor  = 5
)

// ErrNoSession is an output for a root the archive does not hold (never stored, swept, or deleted
// since the caller decided). One value, so the caller answers it as it answers every other miss.
var ErrNoSession = errors.New("no stored session")

// outputUsage is every stored output byte, by plugin, read off the disk. It is the counters' truth:
// the in-memory ones only ever OVER-count (a deleted session's outputs are not subtracted), and an
// append that would cross a cap recounts from here before refusing.
func (a *Archive) outputUsage() (map[string]int64, error) {
	out := map[string]int64{}
	roots, err := a.Roots()
	if err != nil {
		return nil, err
	}
	for _, root := range roots {
		if err := a.ScanOutputs(root, func(line []byte) error {
			var head struct {
				Plugin string `json:"plugin"`
			}
			_ = json.Unmarshal(line, &head)
			out[head.Plugin] += int64(len(line) + 1)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// reserveOutput charges n bytes of `plugin`'s outputs against both shares and the quota.
func (a *Archive) reserveOutput(plugin string, n int64) error {
	pluginCap, totalCap := a.cfg.Quota/OutputPluginShareDivisor, a.cfg.Quota/OutputTotalShareDivisor
	over := func() bool { // a.mu held
		var total int64
		for _, b := range a.outputs {
			total += b
		}
		return a.outputs[plugin]+n > pluginCap || total+n > totalCap
	}
	a.mu.Lock()
	stale := a.outputs == nil || over()
	a.mu.Unlock()
	if stale {
		usage, err := a.outputUsage()
		if err != nil {
			return err
		}
		a.mu.Lock()
		a.outputs = usage
		a.mu.Unlock()
	}
	a.mu.Lock()
	if over() {
		limit := pluginCap
		if a.outputs[plugin]+n <= pluginCap {
			limit = totalCap
		}
		a.mu.Unlock()
		return &QuotaError{Quota: limit}
	}
	a.outputs[plugin] += n
	a.mu.Unlock()
	if err := a.reserve(n); err != nil {
		a.mu.Lock()
		a.outputs[plugin] -= n
		a.mu.Unlock()
		return err
	}
	return nil
}

func (a *Archive) releaseOutput(plugin string, n int64) {
	a.mu.Lock()
	a.outputs[plugin] -= n
	a.mu.Unlock()
	a.release(n)
}

// AppendOutput appends ONE output line, written by `plugin`, to a stored session's `outputs.jsonl`,
// counted against the plugin's share, all outputs' share and the quota ([QuotaError] past any of
// them). The line must not contain a newline.
func (a *Archive) AppendOutput(root, plugin string, line []byte) error {
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
	if err := a.reserveOutput(plugin, n); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(a.sessionDir(root), outputsName),
		os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, fileMode)
	if err != nil {
		a.releaseOutput(plugin, n)
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(append([]byte{}, line...), '\n')); err != nil {
		a.releaseOutput(plugin, n)
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
