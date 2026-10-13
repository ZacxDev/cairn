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
// `rm -r` of `sessions/<root>/` ([Archive.deleteLocked], `sweep.go`), so an output — a summary, a
// ticket EDGE, a suggestion: every type is a line of this one file — can never outlive the
// transcript it was derived from, and there is no second place a cascade could forget.
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

// 🔴 THE CAPS ARE CHECKED AGAINST A RUNNING TOTAL, NEVER A RESCAN (#270's audit, closed in S6). The
// totals are counted off the disk ONCE, by [Open] — before anything can append — and are then moved
// by exactly two things, each under the session's own lock: an append adds its line, and a deletion
// (retention, the owner's delete, a withdrawal — [Archive.deleteLocked]) subtracts that ONE
// session's lines, read from its `outputs.jsonl` before the directory goes. As first built, the
// in-memory totals only ever grew, so a write that would cross a cap re-read EVERY session's
// `outputs.jsonl` before refusing — and a plugin sitting at its cap (or a stolen token, T6) made
// each of its refused writes a walk of the whole archive.

// outputUsage is every stored output byte, by plugin, read off the disk — [Open]'s one count.
func (a *Archive) outputUsage() (map[string]int64, error) {
	out := map[string]int64{}
	roots, err := a.Roots()
	if err != nil {
		return nil, err
	}
	for _, root := range roots {
		one := map[string]int64{}
		if err := a.sessionOutputBytes(root, one, 1); err != nil {
			// 🔴 ONE SESSION'S UNREADABLE `outputs.jsonl` MUST NOT STOP `cairn-ui` FROM STARTING — this
			// runs in [Open]. Its bytes are counted, whole, as the file's on-disk size against NO
			// plugin (the key ""): they still weigh on every plugin's shared 20% and on the quota,
			// and no plugin's own share is charged for bytes nobody could attribute.
			one = map[string]int64{"": fileSize(filepath.Join(a.sessionDir(root), outputsName))}
		}
		for plugin, n := range one {
			out[plugin] += n
		}
	}
	return out, nil
}

// sessionOutputBytes adds (sign 1) or subtracts (sign -1) ONE session's stored output bytes, by
// plugin, to `into`.
func (a *Archive) sessionOutputBytes(root string, into map[string]int64, sign int64) error {
	return a.ScanOutputs(root, func(line []byte) error {
		var head struct {
			Plugin string `json:"plugin"`
		}
		_ = json.Unmarshal(line, &head)
		into[head.Plugin] += sign * int64(len(line)+1)
		return nil
	})
}

// forgetOutputs takes a session's output bytes out of the running totals. The caller holds the
// root's lock and is about to remove the directory. A file it cannot read is forgotten as [Open]
// counted it: whole, against no plugin — a deletion is never refused over an output.
func (a *Archive) forgetOutputs(root string) error {
	gone := map[string]int64{}
	if err := a.sessionOutputBytes(root, gone, 1); err != nil {
		gone = map[string]int64{"": fileSize(filepath.Join(a.sessionDir(root), outputsName))}
	}
	a.mu.Lock()
	for plugin, n := range gone {
		if a.outputs[plugin] -= n; a.outputs[plugin] <= 0 {
			delete(a.outputs, plugin)
		}
	}
	a.mu.Unlock()
	return nil
}

// reserveOutput charges n bytes of `plugin`'s outputs against both shares and the quota. The
// refusal says WHICH limit it hit ([QuotaError.Share]).
func (a *Archive) reserveOutput(plugin string, n int64) error {
	pluginCap, totalCap := a.cfg.Quota/OutputPluginShareDivisor, a.cfg.Quota/OutputTotalShareDivisor
	a.mu.Lock()
	var total int64
	for _, b := range a.outputs {
		total += b
	}
	switch {
	case a.outputs[plugin]+n > pluginCap:
		a.mu.Unlock()
		return &QuotaError{Quota: pluginCap, Share: SharePlugin}
	case total+n > totalCap:
		a.mu.Unlock()
		return &QuotaError{Quota: totalCap, Share: ShareOutputs}
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

// HasOutputs reports whether a session holds a non-empty `outputs.jsonl` — one `stat`, no read.
func (a *Archive) HasOutputs(root string) bool {
	if !write.SessionComponent.MatchString(root) {
		return false
	}
	info, err := os.Lstat(filepath.Join(a.sessionDir(root), outputsName))
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
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
