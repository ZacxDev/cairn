// Package hostid answers WHICH MACHINE AM I — one implementation, every consumer.
//
// 🔴 A HOSTNAME IS NOT AN IDENTITY. Machines provisioned from one image commonly
// report the SAME hostname, and cairn's local cache is PER-HOST and independently
// STALE: two machines holding the same scope can hold different entries in it at any
// given instant. The caches DO converge — the hosted pod is canonical and each
// machine's cache is a synced read-through copy — but nothing makes two caches agree at
// the moment a tool reads one of them, so a tool that reads such a cache and reports a
// GLOBAL fact ("the store has no `billing/` scope") is stating one machine's disk as
// though it were the fleet's. That is the defect this package exists to make
// un-writable: every consumer prints the identity of the machine it actually read.
//
// 🔴 WHY THE LABEL ALONE IS NOT ENOUGH. Label reads an operator-set environment
// variable and falls back to the hostname. Under a service manager that variable is
// typically set to something machine-specific; in an INTERACTIVE shell it is usually
// unset and the label degrades to the hostname — exactly the value that may be shared.
// So a header printing the label alone would read as coverage while providing none:
// identical on the very machines it is supposed to distinguish. ThisHost joins the
// readable label to the machine id, which is distinct per host by construction.
package hostid

import (
	"os"
	"regexp"
	"strings"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// MachineIDFiles is where the machine id is read from, in order. A package variable so
// a test can point a reader at synthetic files and exercise the REAL function, rather
// than re-implementing its shape check in the test — which would only ever prove the
// test agrees with itself.
var MachineIDFiles = []string{"/etc/machine-id", "/var/lib/dbus/machine-id"}

// machineIDShape is what `/etc/machine-id` is DEFINED to hold: 32 lowercase hex digits.
//
// 🔴 SHAPE-CHECKED, NOT JUST NON-EMPTY. Returning whatever junk a file happened to hold
// would make a caller's "does this prefix belong to this host" answer true for any
// prefix containing it — an error in the FALSE DATA-LOSS direction, which is the one
// that gets someone to act destructively.
var machineIDShape = regexp.MustCompile(`\A[0-9a-f]{32}\z`)

// MachineIDUnreadable is printed instead of an id when no file could be read or none had
// the right shape. A SENTENCE, not an empty string: the whole point of this package is
// that a host claim is never silently unqualified.
const MachineIDUnreadable = "machine-id-unreadable"

// HostLabelEnv is the environment consulted for a readable label, in precedence order. A
// slice rather than a hardcoded pair so a deployment can add its own without editing the
// function.
var HostLabelEnv = []string{"CAIRN_HOST", "ASIB_HOST", "ACTIVITY_HOST"}

// MachineIDDisplayChars is how much of the machine id ThisHost prints.
//
// 🔴 A PREFIX, NEVER THE WHOLE ID. `/etc/machine-id` is a stable, unique installation
// identifier, and this is a DISPLAY value: it lands in rendered headers and gets pasted
// into issues and pull requests routinely. A dozen hex characters separate machines with
// room to spare; the job is to tell a fleet apart, not to identify hardware. 🔴 It does
// NOT touch MachineID or Label — a caller that builds a storage key from the full id
// must keep doing so, since truncating a key prefix would repoint every future object,
// which is a data-loss shape rather than a privacy fix.
const MachineIDDisplayChars = 12

// StoreIsPerHost is the caveat clause every verdict about the store carries.
const StoreIsPerHost = "the store is read through a PER-HOST CACHE, only as fresh as " +
	"its last sync; this run read THIS machine's disk and consulted no other"

var unsafeLabelChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// MachineID is the only reliable "which machine am I" signal available without config,
// or "" when no candidate file is readable or none parses. The caller decides what an
// unknown machine means; this never guesses.
func MachineID() string {
	for _, path := range MachineIDFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// ⚠ THE READ IS STRICT UTF-8 ON THE ORACLE, but the shape check refuses
		// everything a decode failure could produce, so an invalid byte cannot become an
		// accepted id either way. Bytes are compared directly for that reason.
		value := pytext.StripWhitespace(string(data))
		if machineIDShape.MatchString(value) {
			return value
		}
	}
	return ""
}

// Label is the READABLE name of this machine, for a human reading a header.
//
// 🔴 The fallback is the hostname, which may be SHARED across machines — which is
// precisely why ThisHost exists and why nothing should print this value on its own as a
// per-host claim.
func Label() string {
	for _, name := range HostLabelEnv {
		value := os.Getenv(name)
		if value != "" && pytext.StripWhitespace(value) != "" {
			return sanitizeLabel(pytext.StripWhitespace(value))
		}
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}
	return sanitizeLabel(hostname)
}

func sanitizeLabel(value string) string {
	return unsafeLabelChars.ReplaceAllString(value, "-")
}

// ThisHost is an identity that DIFFERS between machines even on a hand-run:
// `<label>-<machine-id-prefix>`.
//
// It collapses to just the label when the label already carries the id (an
// operator-chosen shape, left as they set it), and to `<label>-machine-id-unreadable`
// when the id cannot be read at all — never to a bare, possibly-shared hostname that
// would read as a fact about the fleet.
func ThisHost() string {
	label := Label()
	id := MachineID()
	if id == "" {
		return label + "-" + MachineIDUnreadable
	}
	if strings.Contains(label, id) {
		return label
	}
	return label + "-" + id[:MachineIDDisplayChars]
}

// StoreHostLine is `<indent>host: <identity>  (<the per-host caveat>)` — printed under
// every `store:` line, in ONE spelling.
//
// 🔴 ONE SEAM, because the reader and a writer describe the SAME directory: two
// spellings of "whose disk is this" would disagree the first time one of them was
// edited, and this is a claim about the SCOPE of every verdict below it.
func StoreHostLine(identity, indent string) string {
	return indent + "host: " + identity + "  (" + StoreIsPerHost + ")"
}
