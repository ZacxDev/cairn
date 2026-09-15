package client

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/doctor"
	"github.com/ZacxDev/cairn/internal/report"
)

// 🔴 WHAT THIS FILE OWNS, AND WHAT THE PARITY GATE OWNS. `tests/parity/harness.py` runs both
// clients over one cache root and diffs the bytes — that is the deliverable's evidence and it
// covers every verb. It cannot reach an archive a real pod will not send, a table's own shape, or a
// message whose input the harness has no way to construct. Those are here. Where a case IS
// reachable from the harness, it is NOT duplicated here: a second assertion over one behaviour
// means neither can be observed to be wrong.

func gzTar(t *testing.T, build func(*tar.Writer)) []byte {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	build(tw)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	if _, err := zw.Write(raw.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func regular(tw *tar.Writer, name string, payload []byte, mtime time.Time) {
	_ = tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg, Name: name, Size: int64(len(payload)),
		Mode: 0o644, ModTime: mtime, Format: tar.FormatPAX,
	})
	_, _ = tw.Write(payload)
}

func TestTheGuardORDERIsTheContractForAHostileArchive(t *testing.T) {
	// 🔴 EACH ARCHIVE IS HOSTILE IN EXACTLY ONE WAY, AND THE MESSAGES ARE THE ORACLE'S. The parity
	// gate covers these four end to end; what it cannot cover is a member that is hostile in TWO
	// ways at once, which is the only input that makes the ORDER observable. `linked-and-unsafe`
	// below is a link WHOSE NAME ALSO ESCAPES — a client that checked the name first would answer
	// the traversal message, and both answers are refusals, so no exit code can tell them apart.
	stamp := time.Unix(946684800, 0)
	for _, tc := range []struct {
		name  string
		build func(*tar.Writer)
		want  string
	}{
		{"a link", func(tw *tar.Writer) {
			_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink,
				Name: "alpha/link.md", Linkname: "../../etc/passwd", ModTime: stamp})
		}, "tar member is a link: 'alpha/link.md'"},
		{"a hard link", func(tw *tar.Writer) {
			_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink,
				Name: "alpha/hard.md", Linkname: "alpha/other.md", ModTime: stamp})
		}, "tar member is a link: 'alpha/hard.md'"},
		{"a directory member", func(tw *tar.Writer) {
			_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "alpha/", ModTime: stamp})
		}, "tar member is not a regular file: 'alpha/'"},
		{"a traversal", func(tw *tar.Writer) {
			regular(tw, "../escaped.md", []byte("x"), stamp)
		}, "tar member escapes the root: '../escaped.md'"},
		{"an absolute name", func(tw *tar.Writer) {
			regular(tw, "/etc/passwd", []byte("x"), stamp)
		}, "tar member escapes the root: '/etc/passwd'"},
		{"a duplicate", func(tw *tar.Writer) {
			regular(tw, "alpha/one.md", []byte("x"), stamp)
			regular(tw, "alpha/one.md", []byte("y"), stamp)
		}, "duplicate tar member: 'alpha/one.md'"},
		// 🔴 THE ORDER, MADE OBSERVABLE. A link whose name also escapes must answer the LINK
		// message, because that is the guard the oracle runs first.
		{"linked-and-unsafe answers the LINK message", func(tw *tar.Writer) {
			_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink,
				Name: "../escaped.md", Linkname: "x", ModTime: stamp})
		}, "tar member is a link: '../escaped.md'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := filepath.Join(t.TempDir(), "cache")
			_, err := InstallSnapshot(gzTar(t, tc.build), cache, http.Header{})
			var corruptErr *StoreCorrupt
			if !errors.As(err, &corruptErr) {
				t.Fatalf("got %#v, want a *StoreCorrupt", err)
			}
			if corruptErr.Reason != tc.want {
				t.Fatalf("\n got: %s\nwant: %s", corruptErr.Reason, tc.want)
			}
			// 🔴 AND NOTHING WAS INSTALLED. A refusal that left a partial cache behind would be
			// the completeness lie arriving by the route this function exists to close.
			if _, statErr := os.Stat(cache); statErr == nil {
				t.Fatal("the cache exists after a refused archive")
			}
		})
	}
}

func TestBOTHCeilingsFireAndTheyMeasureDIFFERENTThings(t *testing.T) {
	// 🔴 BYTES ALONE BOUND NOTHING, AND THIS IS THE PAIR THAT SAYS SO. A member's declared size is
	// 0 for an empty file, so a byte ceiling can never fire on an INODE bomb: measured against the
	// pre-fix Python client, a 282,282-byte body carrying 60,000 zero-length `*.md` members wrote
	// 60,000 files and reported `live … 60000 entries`, exit 0.
	stamp := time.Unix(946684800, 0)

	// The BYTE ceiling: few members, huge declared sizes. 🔴 THE FIXTURE OVERSHOOTS THE CEILING
	// RATHER THAN LANDING ON IT, and the member count is deliberately NOT a divisor of
	// `MaxMembers`: a fixture that sat exactly on a boundary would leave the guard's own
	// comparison unreached while production's values executed it.
	bytesBomb := gzTar(t, func(tw *tar.Writer) {
		for i := 0; i < 3; i++ {
			payload := bytes.Repeat([]byte("z"), 1024)
			// A header that declares far more than it carries is what a decompression bomb
			// looks like to the FIRST pass, which reads headers only.
			_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg,
				Name: fmt.Sprintf("alpha/big-%d.md", i), Size: int64(len(payload)),
				ModTime: stamp, Format: tar.FormatPAX})
			_, _ = tw.Write(payload)
		}
	})
	// The declared sizes above are honest, so the byte ceiling must NOT fire — that is the
	// negative half of this pair, and without it "the ceiling fires" would be satisfied by a
	// ceiling that fires on everything.
	if _, err := InstallSnapshot(bytesBomb, filepath.Join(t.TempDir(), "c"), http.Header{}); err != nil {
		t.Fatalf("a small honest archive must install: %v", err)
	}

	// 🔴 `sum`, NOT `max`, AND THIS IS THE ONLY CASE WHERE THE TWO ANSWERS DIFFER. A `max`-based
	// ceiling passes a 1000 × 250 MB archive, and every other case here cannot tell them apart —
	// the mutant survived until this row existed. The limits are parameters for exactly this
	// reason: three 1024-byte members SUM to 3072 and MAX to 1024, so a 2048-byte ceiling
	// separates the two without a 256 MB fixture.
	sumFail := checkCeilings(bytesBomb, 2048, MaxMembers)
	var sumErr *StoreCorrupt
	if !errors.As(sumFail, &sumErr) ||
		!strings.Contains(sumErr.Reason, "archive unpacks to 3072 bytes") {
		t.Fatalf("the byte ceiling must SUM the declared sizes: %#v", sumFail)
	}
	// …and a ceiling above the sum but below nothing else must pass, which is what makes the row
	// above about the arithmetic rather than about the ceiling always firing.
	if err := checkCeilings(bytesBomb, 4096, MaxMembers); err != nil {
		t.Fatalf("a ceiling above the SUM must pass: %v", err)
	}
	// ⚠ THAT THE PRODUCTION CALL SITE PASSES THE PRODUCTION CONSTANTS IS ASSERTED BY THE MEMBER
	// CEILING BELOW, WHICH GOES THROUGH `InstallSnapshot` AND NAMES `MaxMembers` IN ITS MESSAGE. A
	// second assertion for the byte ceiling would need a 256 MB fixture to reach it, so the claim
	// is carried by the one ceiling that is cheap to reach through the real entry point.

	// The MEMBER ceiling, reached with zero-length members — the shape a byte ceiling is blind to.
	inodeBomb := gzTar(t, func(tw *tar.Writer) {
		for i := 0; i <= MaxMembers; i++ {
			regular(tw, fmt.Sprintf("alpha/item-%06d.md", i), nil, stamp)
		}
	})
	_, err := InstallSnapshot(inodeBomb, filepath.Join(t.TempDir(), "c"), http.Header{})
	var corruptErr *StoreCorrupt
	if !errors.As(err, &corruptErr) || !strings.Contains(corruptErr.Reason, "member ceiling") {
		t.Fatalf("the MEMBER ceiling must fire on zero-length members: %#v", err)
	}
	if strings.Contains(corruptErr.Reason, "byte ceiling") {
		t.Fatalf("the byte ceiling answered an inode bomb, so the two are not measuring "+
			"different things: %s", corruptErr.Reason)
	}
	if !strings.Contains(corruptErr.Reason, fmt.Sprintf("archive holds %d members", MaxMembers+1)) {
		t.Fatalf("the refusal must name the COUNT it measured: %s", corruptErr.Reason)
	}
}

func TestTheSERVERSOwnCountIsChecked(t *testing.T) {
	// 🔴 THE SERVER-SIDE COMMENT CLAIMS A TRUNCATED TRANSFER IS "VISIBLE AS A DISAGREEMENT", AND IT
	// IS ONLY VISIBLE IF SOMEBODY COMPARES. Two points, because a guard that fired on every value
	// would also "pass" the mismatch case: a header that AGREES must install.
	stamp := time.Unix(946684800, 0)
	body := gzTar(t, func(tw *tar.Writer) {
		regular(tw, "alpha/one.md", []byte("x"), stamp)
		regular(tw, "alpha/two.md", []byte("y"), stamp)
		// A non-`.md` member is NOT counted, which is what makes the count a count of ENTRIES
		// rather than of members — and is why this fixture carries one.
		regular(tw, "alpha/notes.txt", []byte("z"), stamp)
	})
	agreeing := http.Header{}
	agreeing.Set("X-Store-Entries", "2")
	n, err := InstallSnapshot(body, filepath.Join(t.TempDir(), "c"), agreeing)
	if err != nil || n != 2 {
		t.Fatalf("an agreeing header must install: n=%d err=%v", n, err)
	}
	disagreeing := http.Header{}
	disagreeing.Set("X-Store-Entries", "99")
	_, err = InstallSnapshot(body, filepath.Join(t.TempDir(), "c"), disagreeing)
	var corruptErr *StoreCorrupt
	if !errors.As(err, &corruptErr) ||
		corruptErr.Reason != "server declared 99 entries, archive held 2" {
		t.Fatalf("got %#v", err)
	}
	// An ABSENT header is not a mismatch: the count is then UNMEASURED, and refusing would make a
	// missing header an outage.
	if _, err := InstallSnapshot(body, filepath.Join(t.TempDir(), "c"), http.Header{}); err != nil {
		t.Fatalf("an absent header must not refuse: %v", err)
	}
}

func TestTheInstalledFilesCARRYTheMembersOwnMtimeToTheNANOSECOND(t *testing.T) {
	// 🔴 A MEASURED DEFECT, AND EXACTLY THE FAILURE MODE THIS PHASE EXISTS TO PREVENT. The reader
	// orders its index by entry mtime, so a cache whose files all carry the EXTRACTION time is
	// ordered by TAR ORDER — a different listing with a different featured entry, no error and no
	// missing entry, which reads as a stale cache. The parity gate caught it; this keeps the kill
	// in the same tier as the code.
	//
	// 🔴 SUB-SECOND PRECISION IS THE HALF THAT MATTERS, because the FRACTION is what decides the
	// tie-break for two entries written in the same second. The ustar mtime field is whole seconds
	// and the snapshot writer emits PAX, so the two members below share a second and differ only
	// in the fraction — a port that restored `header.ModTime.Unix()` would pass a whole-second
	// assertion and reorder this pair.
	early := time.Unix(946684800, 250_000_000)
	late := time.Unix(946684800, 750_000_000)
	body := gzTar(t, func(tw *tar.Writer) {
		regular(tw, "alpha/second.md", []byte("x"), late)
		regular(tw, "alpha/first.md", []byte("y"), early)
	})
	cache := filepath.Join(t.TempDir(), "cache")
	if _, err := InstallSnapshot(body, cache, http.Header{}); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]time.Time{"alpha/first.md": early, "alpha/second.md": late} {
		info, err := os.Stat(filepath.Join(cache, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.ModTime(); !got.Equal(want) {
			t.Errorf("%s: mtime %v, want %v (to the nanosecond)", name, got.UnixNano(), want.UnixNano())
		}
	}
}

func TestASafeMemberNameDecidesOnCOMPONENTSNotOnASubstring(t *testing.T) {
	// 🔴 NOT `strings.Contains(name, "..")`. That was the oracle's first version and it is
	// over-broad: a legitimate entry called `widget-cfg/a..b.md` contains `..` as a SUBSTRING and
	// aborted the entire sync, which then rendered as an outage. The server puts no constraint on
	// entry filenames, so this is reachable with an ordinary file.
	for _, name := range []string{
		"alpha/a..b.md", "alpha/..hidden.md", "alpha/trailing..md", "a/b/c.md", "alpha/...md",
	} {
		if !safeMemberName(name) {
			t.Errorf("%q is a legitimate name and must be accepted", name)
		}
	}
	for _, name := range []string{
		"", "/abs.md", "../escaped.md", "alpha/../../escaped.md", "alpha//double.md", "..",
	} {
		if safeMemberName(name) {
			t.Errorf("%q escapes the root and must be refused", name)
		}
	}
}

func TestTheStampIsWrittenWithEveryFieldAReaderNeeds(t *testing.T) {
	stamp := time.Unix(946684800, 0)
	body := gzTar(t, func(tw *tar.Writer) { regular(tw, "alpha/one.md", []byte("x"), stamp) })
	cache := filepath.Join(t.TempDir(), "cache")
	headers := http.Header{}
	headers.Set("X-Store-Revision", "abc123")
	headers.Set("X-Store-Snapshot", "seeded=2000-01-01T00:00:00Z entry-files=1")
	if _, err := InstallSnapshot(body, cache, headers); err != nil {
		t.Fatal(err)
	}
	lines, reason := ReadStamp(cache)
	if lines == nil {
		t.Fatalf("no stamp: %s", reason)
	}
	fields := StampFields(lines)
	if fields["revision"] != "abc123" || fields["entries"] != "1" {
		t.Fatalf("fields: %v", fields)
	}
	// 🔴 `coverage=ALL` IS RECORDED SO A FILTERED CACHE CAN NEVER BE MISTAKEN FOR A COMPLETE ONE.
	// The CLI only ever writes ALL today; the field exists so a future filtered mode cannot
	// silently inherit the completeness claim.
	if fields["coverage"] != "ALL" {
		t.Fatalf("coverage: %q", fields["coverage"])
	}
	// The two missing-header SENTINELS, which are not the same as an absent field.
	if _, err := InstallSnapshot(body, cache, http.Header{}); err != nil {
		t.Fatal(err)
	}
	lines, _ = ReadStamp(cache)
	fields = StampFields(lines)
	if fields["revision"] != "unknown" || fields["snapshot"] != "UNSTAMPED" {
		t.Fatalf("an absent header must produce a NAMED sentinel, not an empty value: %v", fields)
	}
}

func TestANEmptyStampIsABSENTAndNotAStampWithNoFields(t *testing.T) {
	// 🔴 "THE STORE IS STAMPED" MUST NOT BE SATISFIABLE BY A ZERO-BYTE FILE. A mutant that
	// returned an empty slice with no reason SURVIVED until this row existed, and what it produces
	// is the worst shape available: `doctor` grades `reader-resolution` OK ("carries a sync stamp")
	// and `cache-stamp` OK with `(the stamp is empty)`, so a store that cannot date itself reports
	// a clean bill of health. That is the exact silent zero the stamp exists to prevent.
	cache := t.TempDir()
	for _, tc := range []struct{ name, body string }{
		{"a zero-byte stamp", ""},
		{"a stamp of blank lines", "\n\n   \n"},
	} {
		if err := os.WriteFile(filepath.Join(cache, SyncStamp), []byte(tc.body), 0o644); err != nil {
			t.Fatal(err)
		}
		lines, reason := ReadStamp(cache)
		if lines != nil {
			t.Errorf("%s reported %d line(s) as a STAMP", tc.name, len(lines))
		}
		if !strings.Contains(reason, "is empty") {
			t.Errorf("%s: the reason must say so, got %q", tc.name, reason)
		}
	}
	// The positive control: one real field IS a stamp, so the rows above are about emptiness and
	// not about a reader that never returns anything.
	if err := os.WriteFile(filepath.Join(cache, SyncStamp),
		[]byte("synced=946684800\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if lines, reason := ReadStamp(cache); len(lines) != 1 || reason != "" {
		t.Fatalf("a one-field stamp must be a stamp: %v %q", lines, reason)
	}
	// An ABSENT file is a THIRD answer, and its reason names the file rather than the emptiness.
	if err := os.Remove(filepath.Join(cache, SyncStamp)); err != nil {
		t.Fatal(err)
	}
	if lines, reason := ReadStamp(cache); lines != nil || !strings.Contains(reason, "no `.sync-stamp` in") {
		t.Fatalf("an absent stamp: %v %q", lines, reason)
	}
}

func TestAnUnreadableOrFutureDatedStampIsUNKNOWNNotFRESH(t *testing.T) {
	// 🔴 "FRESH" IS THE ONE ANSWER A BROKEN CLOCK MUST NEVER PRODUCE HERE. The pre-fix Python
	// version clamped a negative age to `0s`, i.e. "just synced" — and a suspending laptop, which
	// is the case the whole degrade-to-cache design is written for, is where clock jumps happen.
	cache := t.TempDir()
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(cache, SyncStamp), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	future := time.Now().Unix() + 86400
	for _, tc := range []struct{ name, body string }{
		{"a future-dated stamp", fmt.Sprintf("synced=%d\n", future)},
		{"a non-numeric synced", "synced=tomorrow\n"},
		{"no synced field at all", "revision=abc\n"},
	} {
		write(tc.body)
		if _, known, _ := CacheAge(cache); known {
			t.Errorf("%s must be UNKNOWN", tc.name)
		}
	}
	// The positive control: a real stamp IS known, so the assertions above are about the input and
	// not about a function that always answers UNKNOWN.
	write(fmt.Sprintf("synced=%d\n", time.Now().Unix()-3600))
	age, known, _ := CacheAge(cache)
	if !known || age < 3500 || age > 3700 {
		t.Fatalf("a real stamp must be known: age=%d known=%v", age, known)
	}
	// 🔴 3600 s IS 60m, NOT 1h, AND THAT IS THE VOCABULARY'S OWN BOUNDARY. The minutes form runs
	// to 5400 s (90 minutes), so an expectation of `1h` here would have been a test agreeing with
	// a guess rather than with the function.
	if got := agePhrase(age, known); got != "cache 60m old" {
		t.Fatalf("agePhrase: %q", got)
	}
	if got := agePhrase(0, false); got != "cache age UNKNOWN" {
		// Never "cache age UNKNOWN old".
		t.Fatalf("agePhrase: %q", got)
	}
}

func TestTheAgeVOCABULARYChangesUnitAtItsOwnBoundARIES(t *testing.T) {
	// Each row is ON a boundary and one below it, which is the only way a `<` that should be `<=`
	// becomes observable.
	for _, tc := range []struct {
		seconds int64
		want    string
	}{
		{0, "0s"}, {89, "89s"}, {90, "1m"}, {5399, "89m"}, {5400, "1h"},
		{172799, "47h"}, {172800, "2d"},
	} {
		if got := HumanAge(tc.seconds, true); got != tc.want {
			t.Errorf("HumanAge(%d) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
	if got := HumanAge(0, false); got != "age UNKNOWN" {
		t.Errorf("the sentinel: %q", got)
	}
}

func TestTheWriteTablesMapEveryStatusTheServerCanEmit(t *testing.T) {
	// 🔴 A TABLE, NOT AN `if` LADDER, so this can walk it. The pairs below are the distinctions the
	// two codes exist to carry: 6 means "change the request", 7 means "retry".
	for code, want := range map[int]int{
		400: ExitWriteRefused, 401: ExitWriteRefused, 403: ExitWriteRefused,
		404: ExitWriteRefused, 405: ExitWriteRefused, 422: ExitWriteRefused,
		428: ExitWriteRefused, 412: ExitWritePrecondition,
		429: ExitWriteUnreachable, 500: ExitWriteUnreachable, 502: ExitWriteUnreachable,
		503: ExitWriteUnreachable, 504: ExitWriteUnreachable,
	} {
		if got := classifyWrite(code, "", "d").ExitCode; got != want {
			t.Errorf("HTTP %d -> %d, want %d", code, got, want)
		}
	}
	// 🔴 500 IS A RETRY, NOT A "FIX YOUR REQUEST", and it was ABSENT from the oracle's table once:
	// it fell through to the unrecognised arm, which is `ExitWriteRefused` — telling the caller to
	// change a byte-identical request that would very likely succeed on a retry.
	if classifyWrite(500, "", "d").ExitCode == ExitWriteRefused {
		t.Fatal("a 500 must be retryable")
	}
	// 🔴 THE TOKEN TABLE IS CONSULTED FIRST, AND THAT IS THE WHOLE REASON IT EXISTS: one HTTP
	// status carries two outcomes with OPPOSITE remedies.
	if got := classifyWrite(412, "already-exists", "d").ExitCode; got != ExitWriteExists {
		t.Fatalf("412 + already-exists -> %d, want %d", got, ExitWriteExists)
	}
	if got := classifyWrite(412, "precondition-failed", "d").ExitCode; got != ExitWritePrecondition {
		t.Fatalf("412 + precondition-failed -> %d, want %d", got, ExitWritePrecondition)
	}
	// 🔴 A TOKEN NOT IN THE TABLE FALLS THROUGH TO THE CODE TABLE, NEVER TO A PASS.
	if got := classifyWrite(404, "scope-unknown", "d").ExitCode; got != ExitWriteRefused {
		t.Fatalf("an unknown token must fall through: %d", got)
	}
	// An UNMAPPED code is not a success: it is a refusal that SAYS the code was unrecognised.
	unknown := classifyWrite(418, "", "brewing")
	if unknown.ExitCode != ExitWriteRefused ||
		!strings.Contains(unknown.Detail, "unrecognised HTTP 418") ||
		!strings.Contains(unknown.Detail, "NOT LANDED") {
		t.Fatalf("an unmapped code: %#v", unknown)
	}
	if unknown.Status != "http-418" {
		t.Fatalf("an absent token is named from the code: %q", unknown.Status)
	}
}

func TestANY2xxIsSuccessAndTheWriteRefusalsCarryTheServersToken(t *testing.T) {
	// 🔴 `create` ANSWERS **201**, AND A `!= 200` TEST WAS A MEASURED DEFECT: `urllib`'s
	// `HTTPErrorProcessor` raises only for a code OUTSIDE 200–299, so the oracle prints the created
	// entry and exits 0 while this client fell through the unrecognised-code arm and reported
	// `unrecognised HTTP 201 … treating the write as NOT LANDED` at exit 6 — a SUCCESSFUL create
	// reported as a refusal, whose documented remedy is to change a request that already landed.
	// The parity gate found it; this is the kill in the same tier as the code.
	var status int
	var token string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			w.Header().Set("X-Store-Status", token)
		}
		w.Header().Set("ETag", `"abc1234567890abc"`)
		w.WriteHeader(status)
		_, _ = w.Write([]byte("body\n"))
	}))
	defer server.Close()
	cfg := Config{URL: server.URL, Token: "t"}

	for _, ok := range []int{200, 201, 202, 299} {
		status, token = ok, ""
		headers, body, err := SendWrite(cfg, "PUT", "/x", nil, 5, nil)
		if err != nil {
			t.Fatalf("HTTP %d must be a SUCCESS: %v", ok, err)
		}
		if headers.Get("ETag") == "" || string(body) == "" {
			t.Fatalf("HTTP %d: the headers and body must reach the caller", ok)
		}
	}
	// ⚠ 204 IS DELIBERATELY NOT IN THAT LIST, AND THE REASON IS THE HARNESS, NOT THE CODE.
	// `net/http` refuses to send a body with a 204, so the row would assert a body the SERVER
	// never wrote — a test failing for a reason that has nothing to do with the classification.
	// It is still a success here, which is the narrower claim this row makes.
	status, token = 204, ""
	if _, _, err := SendWrite(cfg, "PUT", "/x", nil, 5, nil); err != nil {
		t.Fatalf("HTTP 204 must be a SUCCESS: %v", err)
	}
	// 🔴 AND THE TOKEN IS READ OFF THE REFUSAL, because it is the only place on the wire where
	// `precondition-failed` and `already-exists` differ — one 412, two opposite remedies.
	status, token = 412, "already-exists"
	_, _, err := SendWrite(cfg, "PUT", "/x", nil, 5, nil)
	var refused *WriteRefused
	if !errors.As(err, &refused) || refused.ExitCode != ExitWriteExists ||
		refused.Status != "already-exists" {
		t.Fatalf("412 + already-exists: %#v", err)
	}
	status, token = 412, "precondition-failed"
	_, _, err = SendWrite(cfg, "PUT", "/x", nil, 5, nil)
	if !errors.As(err, &refused) || refused.ExitCode != ExitWritePrecondition {
		t.Fatalf("412 + precondition-failed: %#v", err)
	}
	// The body becomes the one-line detail, truncated rather than dropped.
	if !strings.Contains(refused.Detail, "body") {
		t.Fatalf("the server's own body must reach the caller: %q", refused.Detail)
	}
	// A 3xx is NOT a success: it is outside 200–299 on both clients, and an unmapped code is a
	// refusal that SAYS the code was unrecognised rather than a pass.
	status, token = 302, ""
	_, _, err = SendWrite(cfg, "PUT", "/x", nil, 5, nil)
	if !errors.As(err, &refused) || !strings.Contains(refused.Detail, "unrecognised HTTP 302") {
		t.Fatalf("a 302 must not be read as success: %#v", err)
	}
}

func TestEveryWriteCodeIsDisjointFromEveryReadCode(t *testing.T) {
	// 🔴 THE WHOLE POINT OF 6/7/8/9 IS THAT A CALLER CANNOT READ "nothing was displayed" AS "the
	// bullet landed". Discovered from the table rather than hand-listed, so a tenth code cannot
	// slip past.
	reads := map[int]string{
		ExitOK: "EXIT_OK", ExitUnreachableNoCache: "EXIT_UNREACHABLE_NO_CACHE",
		ExitRefreshFailed: "EXIT_REFRESH_FAILED", ExitCorrupt: "EXIT_CORRUPT",
	}
	writes := map[int]string{
		ExitWriteRefused: "EXIT_WRITE_REFUSED", ExitWriteUnreachable: "EXIT_WRITE_UNREACHABLE",
		ExitWritePrecondition: "EXIT_WRITE_PRECONDITION", ExitWriteExists: "EXIT_WRITE_EXISTS",
	}
	for code, name := range writes {
		if code == ExitOK {
			continue // 0 is success everywhere, which is what 0 means
		}
		if other, clash := reads[code]; clash {
			t.Errorf("%s and %s are both %d", name, other, code)
		}
	}
	// And the table is the whole set, so a constant added without a table row fails here.
	all := ExitCodes()
	for _, name := range []string{
		"EXIT_OK", "EXIT_USAGE", "EXIT_UNREACHABLE_NO_CACHE", "EXIT_REFRESH_FAILED",
		"EXIT_CORRUPT", "EXIT_WRITE_REFUSED", "EXIT_WRITE_UNREACHABLE",
		"EXIT_WRITE_PRECONDITION", "EXIT_WRITE_EXISTS",
	} {
		if _, present := all[name]; !present {
			t.Errorf("%s is not in ExitCodes(), so `-exit-codes` cannot report it and the "+
				"shared-set ledger is blind to it", name)
		}
	}
	if len(all) != 9 {
		t.Fatalf("ExitCodes() has %d rows; nine constants are documented", len(all))
	}
}

func TestTheSHAREDEXITCODESETWithDoctorIsExactlyZeroAndNine(t *testing.T) {
	// 🔴 THE `{0, 9}` OVERLAP IS DELIBERATE AND DOCUMENTED, AND THIS PORT MUST NOT CHANGE IT. Both
	// operands are DISCOVERED, so the set goes wrong when either side grows or shrinks — which is
	// the failure a hand-listed ledger could not see.
	clientCodes := map[int]struct{}{}
	for _, code := range ExitCodes() {
		clientCodes[code] = struct{}{}
	}
	shared := map[int]struct{}{}
	for _, code := range doctor.ExitCodes() {
		if _, both := clientCodes[code]; both {
			shared[code] = struct{}{}
		}
	}
	if len(shared) != 2 {
		t.Fatalf("the shared set is %v; {0, 9} is the documented one", shared)
	}
	for _, want := range []int{0, 9} {
		if _, in := shared[want]; !in {
			t.Errorf("%d is not in the shared set", want)
		}
	}
	// 🔴 AND 1 MUST NOT BE ANY DOCTOR CODE, WHICH THE INTERSECTION STRUCTURALLY CANNOT SEE: 1 is
	// not a client code, so an `EXIT_DOCTOR_* = 1` leaves the intersection at {0, 9} and the
	// ledger green. A doctor code of 1 would be indistinguishable from a crash.
	for name, code := range doctor.ExitCodes() {
		if code == 1 {
			t.Errorf("%s is 1, which is the interpreter's own code for a crash", name)
		}
		if code == ExitUsage {
			t.Errorf("%s is %d, which is a usage error", name, ExitUsage)
		}
	}
}

func TestTheFlagReconciliationREFUSESRatherThanRECONCILING(t *testing.T) {
	// 🔴 EVERY COMBINATION HAS AN OBVIOUS "SENSIBLE" READING AND THEY ARE DIFFERENT READINGS, so
	// honouring one would give the caller output they did not ask for and no sign of it. The ORDER
	// matters: a call carrying two incoherent pairs gets ONE message.
	two := 2
	for _, tc := range []struct {
		name   string
		hasRef bool
		list   bool
		limit  *int
		page   *int
		want   string
	}{
		{"ref and list", true, true, nil, nil,
			"--ref and --list select different things (one entry's body vs the whole index). Pass one."},
		{"list and limit", false, true, &two, nil,
			"--limit is a cap on entry BODIES and --list prints none; the index is never truncated. Drop one."},
		{"page and ref", true, false, nil, &two,
			"--page pages the INDEX, and --ref/--limit print no index at all. Drop one."},
		{"page and limit", false, false, &two, &two,
			"--page pages the INDEX, and --ref/--limit print no index at all. Drop one."},
		// 🔴 THE ORDER, MADE OBSERVABLE: ref+list+limit is TWO violations and must answer the
		// selector one, because that is the guard the oracle runs first.
		{"ref and list and limit answers the SELECTOR message", true, true, &two, nil,
			"--ref and --list select different things (one entry's body vs the whole index). Pass one."},
	} {
		if got := RejectRecallFlags(tc.hasRef, tc.list, tc.limit, tc.page); got != tc.want {
			t.Errorf("%s:\n got: %s\nwant: %s", tc.name, got, tc.want)
		}
	}
	// The positive controls: each flag ALONE is coherent, so the refusals above are about the
	// COMBINATION and not about the flags.
	for _, tc := range []struct {
		name   string
		hasRef bool
		list   bool
		limit  *int
		page   *int
	}{
		{"ref alone", true, false, nil, nil},
		{"list alone", false, true, nil, nil},
		{"limit alone", false, false, &two, nil},
		{"page alone", false, false, nil, &two},
		{"list and page", false, true, nil, &two},
		{"nothing", false, false, nil, nil},
	} {
		if got := RejectRecallFlags(tc.hasRef, tc.list, tc.limit, tc.page); got != "" {
			t.Errorf("%s must be coherent, got %q", tc.name, got)
		}
	}
}

func TestTheSelectionMAPPINGIsTheOnlyPlaceModeIsDerived(t *testing.T) {
	// 🔴 `--limit` IS WHAT SELECTS FULL-BODY MODE AND NOTHING ELSE DOES. Defaulting `limit` at the
	// call site would make "the caller asked for a cap" indistinguishable from "the caller asked
	// for nothing", which is the distinction `mode` is derived from — so the nil case and the
	// `= DefaultEntryLimit` case must produce DIFFERENT modes.
	explicit := report.DefaultEntryLimit
	if got := RecallSelectionFor(false, nil, nil); got !=
		(Selection{Mode: report.DefaultMode, Limit: report.DefaultEntryLimit, Page: 1}) {
		t.Fatalf("no flags: %#v", got)
	}
	if got := RecallSelectionFor(false, &explicit, nil); got.Mode != "full" {
		t.Fatalf("an EXPLICIT limit equal to the default must still select full mode: %#v", got)
	}
	if got := RecallSelectionFor(true, nil, nil); got.Mode != "list" {
		t.Fatalf("--list: %#v", got)
	}
	// `--list` wins over `--limit` in the mapping, and the pair is REFUSED upstream — so this row
	// is about the mapping being total, not about the combination being legal.
	if got := RecallSelectionFor(true, &explicit, nil); got.Mode != "list" {
		t.Fatalf("--list and --limit: %#v", got)
	}
}

func TestAFocusWindowIsBuiltFromBACKTICKEDSpansOnly(t *testing.T) {
	// 🔴 HARVESTING BARE PROSE WOULD MINT TOKENS OUT OF ORDINARY ENGLISH. The failure direction
	// matters: a missed path costs the fallback, a FABRICATED one costs a wrong featured entry with
	// a basis claiming it was quoted.
	got := FocusPathsFromText(
		"Work touches `apps/widget/values.yaml` and apps/unquoted/values.yaml. " +
			"A URL `https://example.invalid/x`, a var `$HOME/x`, a host `git@example.invalid:o/r`, " +
			"a flag `--scope foo/bar`, a traversal `a/../b`, an absolute `/etc/passwd`, " +
			"a home path `~/x/y`, a bare word `nodirsep`, and a repeat " +
			"`apps/widget/values.yaml` again. Trailing punctuation: `docs/note.md,` and " +
			"a trailing slash `pkg/dir/`.")
	// ⚠ `foo/bar` IS IN THE ANSWER AND IT COMES OUT OF THE `--scope foo/bar` SPAN. A span is split
	// on whitespace and each token judged alone, so the FLAG token is rejected (leading `-`) and
	// its VALUE is accepted — it is path-shaped and the reader has no way to know it was an
	// argument. That is the oracle's behaviour, and an expectation that omitted it would have been
	// a test agreeing with a guess. The cost is bounded: an extra path in the window can only add
	// a candidate the matcher then fails to resolve, and the basis names the count either way.
	want := []string{"apps/widget/values.yaml", "foo/bar", "docs/note.md", "pkg/dir"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got: %v\nwant: %v", got, want)
	}
	// The flag token itself must NOT survive, which is what makes the split meaningful rather than
	// incidental.
	if inner := FocusPathsFromText("`--scope foo/bar`"); !reflect.DeepEqual(inner, []string{"foo/bar"}) {
		t.Fatalf("a span is split on whitespace and each token judged alone: %v", inner)
	}
}

func TestTheNEWESTHandoffDocWinsAndTheLowercaseFamilyIsFIRST(t *testing.T) {
	// 🔴 THE RESOLUTION ORDER IS THE CONTRACT: the lowercase family first, the caps family second,
	// newest WITHIN each. A caps doc that is newer than a lowercase one must still lose, which is
	// the only input that makes "first family wins" observable.
	repo := t.TempDir()
	docs := filepath.Join(repo, "claudedocs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoc := func(name, body string, when time.Time) {
		path := filepath.Join(docs, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Unix(946684800, 0)
	writeDoc("handoff-old.md", "quotes `apps/old/values.yaml`", base)
	writeDoc("handoff-new.md", "quotes `apps/new/values.yaml`", base.Add(time.Hour))
	writeDoc("A-HANDOFF-NEWEST.md", "quotes `apps/caps/values.yaml`", base.Add(2*time.Hour))

	window := Focus(repo)
	if window.Source != "claudedocs/handoff-new.md" {
		t.Fatalf("source: %q — the lowercase family is first and the newest within it wins",
			window.Source)
	}
	// 🔴 THE SOURCE IS THE WINDOW'S FIRST PATH, and it is there because the doc itself is evidence
	// about what is being worked on.
	if len(window.Paths) != 2 || window.Paths[0] != "claudedocs/handoff-new.md" ||
		window.Paths[1] != "apps/new/values.yaml" {
		t.Fatalf("paths: %v", window.Paths)
	}
	// With the lowercase family gone, the caps family is reached — the positive control on the
	// second glob, without which "the first family wins" would be satisfied by a second glob that
	// never runs.
	if err := os.Remove(filepath.Join(docs, "handoff-old.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(docs, "handoff-new.md")); err != nil {
		t.Fatal(err)
	}
	if got := Focus(repo).Source; got != "claudedocs/A-HANDOFF-NEWEST.md" {
		t.Fatalf("the caps family must be reached when the lowercase one is empty: %q", got)
	}
	// And an ABSENT doc is an ORDINARY outcome, not an error: most repos have no handoff at the
	// moment they are resumed, and the caller's fallback is a real answer.
	if got := Focus(t.TempDir()); len(got.Paths) != 0 || got.Source != "" {
		t.Fatalf("an empty repo must produce an empty window: %#v", got)
	}
}

func TestANonPositiveTimeoutIsREFUSEDOnBothPaths(t *testing.T) {
	// 🔴 A TYPE IS NOT A CODE PATH. On the Python side, mutating either the caller's or the socket
	// call's argument to `None` survived the whole suite, because `None` reaches the socket as NO
	// timeout — an unbounded wait rather than a default. An unbounded WRITE is strictly worse: the
	// request may already have been applied.
	cfg := Config{URL: "http://127.0.0.1:1", Token: "t"}
	for _, timeout := range []int{0, -1} {
		if _, _, err := FetchSnapshot(cfg, "", timeout); err == nil ||
			!strings.Contains(err.Error(), "refusing to fetch") {
			t.Errorf("read, timeout=%d: %v", timeout, err)
		}
		if _, _, err := SendWrite(cfg, "POST", "/x", nil, timeout, nil); err == nil ||
			!strings.Contains(err.Error(), "refusing to write") {
			t.Errorf("write, timeout=%d: %v", timeout, err)
		}
	}
	// The positive control: a POSITIVE bound gets PAST the refusal and fails for a transport
	// reason instead, which is what proves the guard is the bound and not the arguments around it.
	_, _, err := FetchSnapshot(cfg, "", 1)
	if err == nil || strings.Contains(err.Error(), "refusing to fetch") {
		t.Fatalf("a positive bound must reach the transport: %v", err)
	}
}

func TestQuoteAllEscapesEVERYReservedCharacterIncludingTheSlash(t *testing.T) {
	// 🔴 `safe=""` IS THE POINT. A scope or ref containing a slash must not silently address a
	// DIFFERENT route, which is what leaving `/` unescaped would do.
	for _, tc := range []struct{ in, want string }{
		{"alpha-notes", "alpha-notes"},
		{"a/b", "a%2Fb"},
		{"a b", "a%20b"},
		{"a:b@c", "a%3Ab%40c"},
		{"a~b.c-d_e", "a~b.c-d_e"},
		{"..", ".."},
		{"é", "%C3%A9"},
	} {
		if got := quoteAll(tc.in); got != tc.want {
			t.Errorf("quoteAll(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTheDeclaredVerbSetIsTheDispatchTable(t *testing.T) {
	// 🔴 THE LEDGER IS READ OUT OF THE RUNNING BINARY BY A PYTHON TEST, so what this owns is that
	// the printed set IS the set the parser dispatches — a printed set assembled separately could
	// agree with anything.
	declared := DeclaredVerbs()
	if len(declared) != len(Verbs()) {
		t.Fatalf("%d declared rows for %d verbs", len(declared), len(Verbs()))
	}
	for _, verb := range Verbs() {
		effect := "reads"
		if verb.Writes {
			effect = "writes"
		}
		want := verb.Name + " " + effect
		found := false
		for _, line := range declared {
			if line == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is dispatched but not declared", want)
		}
		if verb.Run == nil {
			t.Errorf("%q has a row and no handler, so it is unreachable", verb.Name)
		}
	}
	// The three write verbs, named: `Writes` decides whether an unreachable store exits 7 or 3, so
	// a verb that lost the flag would report a failed write as a stale read.
	writes := map[string]bool{}
	for _, verb := range Verbs() {
		writes[verb.Name] = verb.Writes
	}
	for name, want := range map[string]bool{
		"append": true, "put": true, "create": true,
		"sync": false, "recall": false, "search": false, "validate": false,
		"ls-entries": false, "doctor": false,
	} {
		if writes[name] != want {
			t.Errorf("%s: Writes=%v, want %v", name, writes[name], want)
		}
	}
}

func TestTheParserAcceptsBothFlagSpellingsAndRefusesTheRest(t *testing.T) {
	for _, argv := range [][]string{
		{"recall", "--scope", "alpha-notes", "--limit", "3"},
		{"recall", "--scope=alpha-notes", "--limit=3"},
	} {
		verb, opts, err := Parse(argv)
		if err != nil || verb.Name != "recall" || opts.Scope != "alpha-notes" ||
			opts.Limit == nil || *opts.Limit != 3 {
			t.Fatalf("%v: verb=%q opts=%#v err=%v", argv, verb.Name, opts, err)
		}
	}
	// 🔴 `--ref ''` IS A DIFFERENT REQUEST FROM NO `--ref`: it still narrows and finds nothing.
	_, opts, err := Parse([]string{"recall", "--ref", ""})
	if err != nil || !opts.HasRef || opts.Ref != "" {
		t.Fatalf("an empty ref must be SEEN: %#v %v", opts, err)
	}
	_, opts, _ = Parse([]string{"recall"})
	if opts.HasRef {
		t.Fatal("no --ref must leave HasRef false")
	}
	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"no subcommand", nil},
		{"unknown subcommand", []string{"telepathy"}},
		{"a flag the verb does not take", []string{"recall", "--all-scopes"}},
		{"doctor takes no --scope", []string{"doctor", "--scope", "x"}},
		{"a value for a boolean flag", []string{"recall", "--list=yes"}},
		{"a missing required flag", []string{"append", "--ref", "r", "--text", "t"}},
		{"a non-integer limit", []string{"recall", "--limit", "banana"}},
		{"a missing value", []string{"recall", "--scope"}},
		{"a stray positional", []string{"recall", "extra"}},
		{"a missing positional", []string{"search"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := Parse(tc.argv); err == nil {
				t.Fatalf("%v must be refused", tc.argv)
			} else {
				var usage *usageError
				if !errors.As(err, &usage) {
					t.Fatalf("a bad command line must be a usageError, got %#v", err)
				}
			}
		})
	}
	// `search` takes exactly one positional, and it is the QUERY.
	_, opts, err = Parse([]string{"search", "--scope", "alpha", "the query"})
	if err != nil || opts.Query != "the query" {
		t.Fatalf("search: %#v %v", opts, err)
	}
}

func TestAJSONBodyEscapesAnAstralCharacterAsASurrogatePAIR(t *testing.T) {
	// 🔴 `ensure_ascii=True` IS CPYTHON'S DEFAULT AND IT IS LOAD-BEARING ON THIS ROUTE. The
	// server's guard once could not tell a surrogate PAIR from a LONE surrogate and 400'd every
	// astral character — which is what `cairn append` sends. A client emitting raw UTF-8 would
	// exercise the other half of that guard, and both halves answer "a 200" on the happy path, so
	// a parity gate cannot see the difference.
	body, err := pyJSONObject([][2]string{{"text", "a map: \U0001F5FA"}, {"session", "s"}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"text": "a map: \ud83d\uddfa", "session": "s"}`
	if string(body) != want {
		t.Fatalf("\n got: %s\nwant: %s", body, want)
	}
}

func TestAnOSErrorReadsLikeCPythonsStrOSError(t *testing.T) {
	// The sentence a cache-write refusal and doctor's "could not be read" both interpolate.
	missing := filepath.Join(t.TempDir(), "absent", "file")
	_, err := os.ReadFile(missing)
	got := pyOSError(err)
	want := fmt.Sprintf("[Errno 2] No such file or directory: '%s'", missing)
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
	// An error with NO errno keeps its Go text — a NAMED residual, not a silent one.
	if got := pyOSError(io.ErrUnexpectedEOF); got != "unexpected EOF" {
		t.Fatalf("an errno-less error: %q", got)
	}
}

func TestAnInstallFailureIsCLASSIFIEDRatherThanEscaping(t *testing.T) {
	// 🔴 THE GZIP SWITCH REOPENED THIS IN A NEW SHAPE ON THE PYTHON SIDE. Before gzip a short body
	// raised a tar read error, handled; compressed, a truncated body raises an EOF from the
	// DECOMPRESSOR — which was NOT caught, so it escaped as a traceback at exit 1 with a healthy
	// cache sitting unused. Each arm below is a different failure and all four are mapped.
	full := gzTar(t, func(tw *tar.Writer) {
		regular(tw, "alpha/one.md", bytes.Repeat([]byte("x"), 4096), time.Unix(946684800, 0))
	})
	for _, tc := range []struct {
		name string
		body []byte
		want string
	}{
		{"a truncated gzip stream", full[:len(full)-20], "sent a truncated archive"},
		{"a 200 that is not an archive at all", []byte("<html>nope</html>"),
			"did not return an archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := filepath.Join(t.TempDir(), "cache")
			_, err := InstallSnapshot(tc.body, cache, http.Header{})
			if err == nil {
				t.Fatal("must fail")
			}
			classified := classifyInstallFailure("http://store.invalid", err)
			var unreachableErr *StoreUnreachable
			if !errors.As(classified, &unreachableErr) {
				t.Fatalf("got %#v, want a *StoreUnreachable so the cache is served", classified)
			}
			if !strings.Contains(unreachableErr.Reason, tc.want) {
				t.Fatalf("\n got: %s\nwant it to contain: %s", unreachableErr.Reason, tc.want)
			}
		})
	}
	// 🔴 A `StoreCorrupt` PASSES THROUGH UNCHANGED, because an outage is absorbed into "serving
	// from cache" at exit 0 and this must not be.
	corruptIn := corrupt("tar member is a link: 'x'")
	out := classifyInstallFailure("http://store.invalid", corruptIn)
	var corruptOut *StoreCorrupt
	if !errors.As(out, &corruptOut) || corruptOut.Reason != corruptIn.Reason {
		t.Fatalf("a corrupt archive must not be degraded: %#v", out)
	}
}

func TestOrphanStagingTreesAreReapedOnlyOnceTheyCannotBeLIVE(t *testing.T) {
	// 🔴 THE GRACE PERIOD IS WHY REAPING IS SAFE AT THE START OF A SYNC. A directory younger than
	// an hour may belong to a CONCURRENT sync, and deleting it would reintroduce the race the
	// unique names removed. The pair is the test: an old tree goes, a young one stays.
	parent := t.TempDir()
	cache := filepath.Join(parent, "cache")
	old := filepath.Join(parent, "cache.new-old")
	young := filepath.Join(parent, "cache.old-young")
	unrelated := filepath.Join(parent, "something-else")
	for _, dir := range []string{old, young, unrelated} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(unrelated, past, past); err != nil {
		t.Fatal(err)
	}
	if n := ReapOrphans(cache); n != 1 {
		t.Fatalf("reaped %d, want exactly the one old staging tree", n)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("the old staging tree survived")
	}
	if _, err := os.Stat(young); err != nil {
		t.Error("a YOUNG staging tree was reaped — that is the concurrency race, restored")
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Error("a directory that is not a staging tree was reaped")
	}
}
