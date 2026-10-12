//go:build cairn_capture_noredact

package capture

// 🔴 A TEST-ONLY BUILD: every record and blob ships UNREDACTED. It exists for one purpose — the
// positive control of `tests/plugins/e2e.sh` clause (b) — and refuses every non-loopback URL.
func init() { redactionOff = true }
