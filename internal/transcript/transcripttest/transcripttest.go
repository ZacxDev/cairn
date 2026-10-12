// Package transcripttest builds a transcript archive for tests: a real `archive.Archive` in a temp
// directory, filled through the SAME append path the worker listener uses.
//
// 🔴 A TEST PACKAGE IN A NON-TEST FILE, AND NOTHING THAT SHIPS MAY IMPORT IT. It exists because the
// reading model (`internal/transcript`) and the page that renders it (`internal/ui`) are tested in
// two packages over one store, and a `_test.go` helper is invisible across that boundary; a second
// hand-rolled writer in the UI's tests would be a fixture the store never produced.
// `internal/worker`'s `TestOnlyTheBrowserProgramImportsWorker` pins the "nothing that ships" half:
// this package has NO non-test importer.
//
// Every value a caller passes is SYNTHETIC: session ids `s-…`, the host `host-a`, year-2000 clocks.
package transcripttest

import (
	"fmt"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
)

// Host is the host label every fixture upload carries.
const Host = "host-a"

// Clock is the archive's fixed time.
var Clock = time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)

// Retention is how long the fixture archive keeps a session after its last upload.
const Retention = 90 * 24 * time.Hour

// Open returns an empty archive in a temp directory, with the pod's refusing re-check wired as in
// production (its own redactor, a key that is nobody's), at the fixed [Clock].
func Open(t testing.TB) *archive.Archive {
	t.Helper()
	return OpenAt(t, func() time.Time { return Clock })
}

// OpenAt is [Open] with the clock chosen, so a test can move past [Retention] and sweep.
func OpenAt(t testing.TB, now func() time.Time) *archive.Archive {
	t.Helper()
	red, err := redact.New(redact.SelfTestKey(41), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := archive.Open(archive.Config{Dir: t.TempDir(), Retention: Retention, Quota: 1 << 30, Recheck: red, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// Session is one stored session to write.
type Session struct {
	Root string
	// Owner is the uploader, `<kind>:<id>`.
	Owner string
	// Runtime is `claude` (the default) or `opencode`.
	Runtime string
	// Declared is what the capture agent declares (`D`); the pod derives the rest itself.
	Declared []string
}

// Append stores `records` — each one JSON value — as the next records of one stream of `s`, and
// fails the test if the store refuses them. `src` values are the record's index in the stream
// unless `srcs` names them (an opencode unit's `<key>@<digest>`).
func Append(t testing.TB, a *archive.Archive, s Session, stream string, records []string, srcs ...string) {
	t.Helper()
	runtime := s.Runtime
	if runtime == "" {
		runtime = "claude"
	}
	from := ""
	var base int64
	if m, err := a.ReadMeta(s.Root); err != nil {
		t.Fatal(err)
	} else if m != nil {
		from, base = m.Streams[stream].StoredTo, m.Streams[stream].SeqTo
	}
	recs := make([]archive.Record, len(records))
	for i, r := range records {
		src := fmt.Sprint(base + int64(i))
		if i < len(srcs) {
			src = srcs[i]
		}
		recs[i] = archive.Record{Src: src, Rec: []byte(r)}
	}
	up := archive.Upload{Root: s.Root, Runtime: runtime, From: from, To: fmt.Sprint(base + int64(len(records))), Declared: s.Declared}
	if _, err := a.AppendRecords(archive.Uploader{Owner: s.Owner, Host: Host}, up, stream, recs); err != nil {
		t.Fatalf("storing %d record(s) of %s/%s: %v", len(records), s.Root, stream, err)
	}
}
