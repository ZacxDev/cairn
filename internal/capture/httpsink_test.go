package capture

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/worker"
)

// The sink is tested against the REAL pod half — `internal/worker` over `internal/transcript/
// archive` — behind an httptest listener, so every request it builds is decoded by the decoder
// the pod runs, and every answer it branches on is one the pod really gives.

// Synthetic, pairwise-distinct tokens; none has ever authorised anything.
const (
	sinkTokenA  = "fixture-capture-token-host-a-sink-test-not-real"
	sinkTokenB  = "fixture-capture-token-host-b-sink-test-not-real"
	sinkRoot    = "s-0001"
	sinkRuntime = "claude"
)

var sinkOwner = worker.Owner{Kind: control.KindUser, ID: "u_alpha"}

type pod struct {
	t        *testing.T
	arc      *archive.Archive
	srv      *httptest.Server
	requests atomic.Int64
	// lose, when set, is the request number (1-based) whose answer is LOST: the pod handles it
	// and stores what it stores, and the client gets a 502 instead of its acknowledgement.
	lose atomic.Int64
	// down answers every request 502 without handling it; failFrom does so from that request on.
	down     atomic.Bool
	failFrom atomic.Int64
	// loseAfter422 loses the answer of the ONE request that follows the next 422.
	loseAfter422, loseNext atomic.Bool
	// at422 is the number of the last request answered 422.
	at422 atomic.Int64
}

type podOpts struct {
	disarmed bool
	quota    int64
}

func newPod(t *testing.T, o podOpts) *pod {
	t.Helper()
	red, err := redact.New(redact.SelfTestKey(11), nil)
	if err != nil {
		t.Fatal(err)
	}
	quota := o.quota
	if quota == 0 {
		quota = 1 << 30
	}
	arc, err := archive.Open(archive.Config{Dir: t.TempDir(), Retention: time.Hour, Quota: quota, Recheck: red,
		Now: func() time.Time { return time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	tokens := filepath.Join(t.TempDir(), "worker-tokens")
	rows := worker.NewTokenRow(worker.KindCapture, sinkOwner, "host-a", sinkTokenA).String() + "\n" +
		worker.NewTokenRow(worker.KindCapture, sinkOwner, "host-b", sinkTokenB).String() + "\n"
	if err := os.WriteFile(tokens, []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	w, _, err := worker.New(worker.Config{TokenFile: tokens, Owner: sinkOwner, OwnerHeld: func() bool { return true },
		Archive: arc, Armed: !o.disarmed, Limiter: netid.NewRateLimiter(1000, time.Minute, time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	p := &pod{t: t, arc: arc}
	p.srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		n := p.requests.Add(1)
		if p.down.Load() || (p.failFrom.Load() > 0 && n >= p.failFrom.Load()) {
			rw.WriteHeader(http.StatusBadGateway) // unreachable: nothing is handled, nothing stored
			return
		}
		if n == p.lose.Load() || p.loseNext.CompareAndSwap(true, false) {
			w.ServeHTTP(httptest.NewRecorder(), r)
			rw.WriteHeader(http.StatusBadGateway)
			return
		}
		rec := httptest.NewRecorder()
		w.ServeHTTP(rec, r)
		if rec.Code == http.StatusUnprocessableEntity {
			p.at422.Store(n)
			if p.loseAfter422.CompareAndSwap(true, false) {
				p.loseNext.Store(true)
			}
		}
		for k, v := range rec.Header() {
			rw.Header()[k] = v
		}
		rw.WriteHeader(rec.Code)
		_, _ = rw.Write(rec.Body.Bytes())
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *pod) sink(host, token string, maxRequest int) *HTTPSink {
	return &HTTPSink{Host: host, Endpoints: map[string]Endpoint{"personal": {URL: p.srv.URL, Token: token}},
		MaxRequest: maxRequest, Log: &bytes.Buffer{}}
}

func (p *pod) stored(stream string) []archive.StoredRecord {
	p.t.Helper()
	recs, err := p.arc.ReadStream(sinkRoot, stream)
	if err != nil {
		p.t.Fatal(err)
	}
	return recs
}

func up(from string) Upload {
	return Upload{Instance: "personal", Root: sinkRoot, Runtime: sinkRuntime, Declared: []string{"alpha-notes"}, From: from}
}

// synth is n Claude-Code-shaped records whose text pads each to about size bytes, with Claude
// offsets as src and end, starting at offset 0.
func synth(n, size int) []Record {
	var out []Record
	off := 0
	for i := 0; i < n; i++ {
		rec := fmt.Sprintf(`{"type":"user","uuid":"00000000-0000-4000-8000-%012d","message":{"role":"user","content":"%s"}}`,
			i, strings.Repeat("lorem ipsum ", max(1, size/12)))
		out = append(out, Record{Src: strconv.Itoa(off), Rec: []byte(rec), End: strconv.Itoa(off + len(rec) + 1)})
		off += len(rec) + 1
	}
	return out
}

func srcs(recs []archive.StoredRecord) []string {
	var out []string
	for _, r := range recs {
		out = append(out, r.Src)
	}
	return out
}

func wantSrcs(recs []Record) []string {
	var out []string
	for _, r := range recs {
		out = append(out, r.Src)
	}
	return out
}

func liveToken() string {
	const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 36)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alnum[int(b[i])%len(alnum)]
	}
	return "ghp_" + string(b)
}

// TestTheSinkStoresEveryRecordOnceAcrossRequests: 200 small records over a 64 KiB request bound
// (many requests), one record bigger than a request (continuation frames) and a blob bigger than
// one — every record stored once, in order, the framed record and the blob BYTE-IDENTICAL.
func TestTheSinkStoresEveryRecordOnceAcrossRequests(t *testing.T) {
	p := newPod(t, podOpts{})
	s := p.sink("host-a", sinkTokenA, 64<<10)
	recs := synth(200, 1500)
	big := synth(1, 300<<10)[0]
	last := recs[len(recs)-1].End
	n, _ := strconv.Atoi(last)
	big.Src, big.End = last, strconv.Itoa(n+len(big.Rec)+1)
	recs = append(recs, big)
	if _, err := s.Records(up(""), "main", recs); err != nil {
		t.Fatal(err)
	}
	got := p.stored("main")
	if strings.Join(srcs(got), ",") != strings.Join(wantSrcs(recs), ",") {
		t.Fatalf("stored %d records, want %d once each in order", len(got), len(recs))
	}
	if !bytes.Equal(got[len(got)-1].Rec, big.Rec) {
		t.Fatal("the framed record is not stored byte-identical")
	}
	if s.Requests < 10 {
		t.Fatalf("%d requests: the 64 KiB bound did not split the write", s.Requests)
	}
	data := bytes.Repeat([]byte("persisted tool output line\n"), 12<<10) // ~330 KiB, framed
	if err := s.Blob(up(""), "toolu_0001.txt", "b-v1", data); err != nil {
		t.Fatal(err)
	}
	stored, err := p.arc.ReadBlob(sinkRoot, "toolu_0001.txt")
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatalf("the framed blob is not stored byte-identical (%v)", err)
	}
	meta, _ := p.arc.ReadMeta(sinkRoot)
	if meta.Streams["main"].StoredTo != recs[len(recs)-1].End || meta.Blobs["toolu_0001.txt"].StoredTo != "b-v1" {
		t.Fatalf("the stored positions are not the cursors sent: %+v %+v", meta.Streams, meta.Blobs)
	}
}

// TestA409ResumesFromTheStoredPosition is clause (a) at the sink: a write whose acknowledgement
// was lost (the first 120 records ARE stored, the agent does not know it) is retried from the old
// `from`; the pod answers 409 naming where it is and the sink resumes there — every record once.
func TestA409ResumesFromTheStoredPosition(t *testing.T) {
	p := newPod(t, podOpts{})
	recs := synth(300, 400)
	if _, err := p.sink("host-a", sinkTokenA, 32<<10).Records(up(""), "main", recs[:120]); err != nil {
		t.Fatal(err)
	}
	s := p.sink("host-a", sinkTokenA, 32<<10)
	if _, err := s.Records(up(""), "main", recs); err != nil {
		t.Fatal(err)
	}
	got := p.stored("main")
	if strings.Join(srcs(got), ",") != strings.Join(wantSrcs(recs), ",") {
		t.Fatalf("after a resume %d records are stored, want %d exactly once", len(got), len(recs))
	}
	// The same when EVERYTHING was stored: the retry stores nothing and succeeds.
	if _, err := s.Records(up(""), "main", recs); err != nil || len(p.stored("main")) != len(recs) {
		t.Fatalf("a retry of a fully stored write: %v, %d stored", err, len(p.stored("main")))
	}
	// And a blob whose acknowledgement was lost: the 409 names the version sent, which is success.
	if err := s.Blob(up(""), "toolu_0002.txt", "b-v1", []byte("text\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.Blob(up(""), "toolu_0002.txt", "b-v1", []byte("text\n")); err != nil {
		t.Fatalf("a retried blob version the instance already holds: %v", err)
	}
	// A CHANGED blob from a stale `from` replaces the version the instance names.
	if err := s.Blob(up(""), "toolu_0002.txt", "b-v2", []byte("text two\n")); err != nil {
		t.Fatalf("a changed blob from a stale from: %v", err)
	}
	if b, _ := p.arc.ReadBlob(sinkRoot, "toolu_0002.txt"); string(b) != "text two\n" {
		t.Fatalf("the blob holds %q", b)
	}
}

// TestA409NamingAPositionNoRecordEndsAtIsRefused: the instance holds the stream at a position this
// write does not carry — refused, nothing sent past the 409, rather than guessed at.
func TestA409NamingAPositionNoRecordEndsAtIsRefused(t *testing.T) {
	p := newPod(t, podOpts{})
	other := synth(3, 100)
	other[2].End = "999999"
	if _, err := p.sink("host-a", sinkTokenA, 0).Records(up(""), "main", other); err != nil {
		t.Fatal(err)
	}
	_, err := p.sink("host-a", sinkTokenA, 0).Records(up(""), "main", synth(5, 100))
	var ue *UnresumableError
	if !errors.As(err, &ue) || ue.StoredTo != "999999" {
		t.Fatalf("got %v, want an UnresumableError naming 999999", err)
	}
	if len(p.stored("main")) != 3 {
		t.Fatal("records were stored past an unresumable 409")
	}
}

// TestAnOverBoundRecordIsAPlaceholderAndTheCursorMovesOn is the 413 decision (decision 15's AP2
// note): the pod refuses a 5 MiB record; the sink stores a placeholder naming it under the SAME
// src and cursor, and the records after it are stored — the stream is never wedged.
func TestAnOverBoundRecordIsAPlaceholderAndTheCursorMovesOn(t *testing.T) {
	p := newPod(t, podOpts{})
	recs := synth(3, 200)
	huge := synth(1, 5<<20)[0]
	n, _ := strconv.Atoi(recs[0].End)
	huge.Src, huge.End = recs[0].End, strconv.Itoa(n+len(huge.Rec)+1)
	tail := recs[1:]
	m, _ := strconv.Atoi(huge.End)
	for i := range tail { // re-offset the two records after the huge one
		tail[i].Src = strconv.Itoa(m)
		m += len(tail[i].Rec) + 1
		tail[i].End = strconv.Itoa(m)
	}
	all := append([]Record{recs[0], huge}, tail...)

	// (1) The POD's 413: the local bound is lifted, so the frame that crosses the pod's is refused.
	s := p.sink("host-a", sinkTokenA, 1<<20)
	s.MaxRecord = 64 << 20
	if _, err := s.Records(up(""), "main", all); err != nil {
		t.Fatal(err)
	}
	got := p.stored("main")
	if len(got) != 4 || got[1].Src != huge.Src || s.Omitted != 1 {
		t.Fatalf("stored %v, omitted %d — want 4 records, the second a placeholder at src %s", srcs(got), s.Omitted, huge.Src)
	}
	var ph map[string]any
	if err := json.Unmarshal(got[1].Rec, &ph); err != nil || ph["cairn_capture"] != "omitted" ||
		ph["max_bytes"] != float64(archive.MaxAssembledBytes) || bytes.Contains(got[1].Rec, []byte("lorem")) {
		t.Fatalf("the placeholder is %s", got[1].Rec)
	}
	meta, _ := p.arc.ReadMeta(sinkRoot)
	if meta.Streams["main"].StoredTo != all[3].End {
		t.Fatalf("the stream is stored to %q, want %q", meta.Streams["main"].StoredTo, all[3].End)
	}

	// (2) A 413 on a request of SEVERAL records finds the one: an 8 MiB request carrying a small
	// record and a 4.5 MiB one.
	p2 := newPod(t, podOpts{})
	s2 := p2.sink("host-a", sinkTokenA, 8<<20-4096)
	s2.MaxRecord = 64 << 20
	mid := synth(1, 4608<<10)[0]
	mid.Src, mid.End = recs[0].End, strconv.Itoa(n+len(mid.Rec)+1)
	if _, err := s2.Records(up(""), "main", []Record{recs[0], mid}); err != nil {
		t.Fatal(err)
	}
	got = p2.stored("main")
	if len(got) != 2 || !bytes.Equal(got[0].Rec, recs[0].Rec) || !bytes.Contains(got[1].Rec, []byte(`"omitted"`)) {
		t.Fatalf("a mixed 413 request stored %d records", len(got))
	}

	// (3) The LOCAL bound: a record over it never leaves the host — no request carries it.
	p3 := newPod(t, podOpts{})
	s3 := p3.sink("host-a", sinkTokenA, 1<<20)
	if _, err := s3.Records(up(""), "main", []Record{huge}); err != nil {
		t.Fatal(err)
	}
	if s3.Requests != 1 || s3.Omitted != 1 {
		t.Fatalf("%d requests, %d omitted: a record over the local bound was sent", s3.Requests, s3.Omitted)
	}
	// And a blob over the bound: a placeholder FILE.
	if err := s3.Blob(up(""), "toolu_0003.txt", "b-v1", bytes.Repeat([]byte("x"), 5<<20)); err != nil {
		t.Fatal(err)
	}
	if b, _ := p3.arc.ReadBlob(sinkRoot, "toolu_0003.txt"); !bytes.HasPrefix(b, []byte("cairn-capture: this file was omitted")) {
		t.Fatalf("the over-bound blob stored %d bytes, not the placeholder", len(b))
	}
}

// TestARecordThePodsRecheckRefusesBecomesAPlaceholder: a 422 naming a record replaces EXACTLY
// that record with a `refused-by-recheck` placeholder — within the write the refused bytes are sent
// once and they are never stored — and every other record of the write is stored. The same for a
// blob. (Across RUNS: `TestARefusedRecordsBytesAreNotResentByALaterRun`.)
// A 422 naming a request FIELD has no placeholder and is returned.
func TestARecordThePodsRecheckRefusesBecomesAPlaceholder(t *testing.T) {
	p := newPod(t, podOpts{})
	recs := synth(40, 300)
	secret := liveToken()
	recs[33].Rec = []byte(fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"export TOKEN=%s"}}`, secret))
	var bodies bytes.Buffer
	s := p.sink("host-a", sinkTokenA, 4<<10)
	s.Client = &http.Client{Transport: recordingTransport{&bodies}}
	if n, err := s.Records(up(""), "main", recs); err != nil || n != 40 {
		t.Fatalf("stored %d, %v — want all 40 with record 33 a placeholder", n, err)
	}
	got := p.stored("main")
	if strings.Join(srcs(got), ",") != strings.Join(wantSrcs(recs), ",") || s.Rechecked != 1 {
		t.Fatalf("stored %d records, rechecked %d", len(got), s.Rechecked)
	}
	for i, r := range got {
		placeholder := bytes.Contains(r.Rec, []byte(`"refused-by-recheck"`))
		if placeholder != (i == 33) || bytes.Contains(r.Rec, []byte(secret)) {
			t.Fatalf("record %d: placeholder=%v: %s", i, placeholder, r.Rec)
		}
	}
	if n := strings.Count(bodies.String(), secret); n != 1 {
		t.Fatalf("the refused record's bytes were sent %d times, want exactly once", n)
	}
	if err := s.Blob(up(""), "toolu_0004.txt", "b-v1", []byte("token "+liveToken()+"\n")); err != nil {
		t.Fatal(err)
	}
	if b, _ := p.arc.ReadBlob(sinkRoot, "toolu_0004.txt"); !bytes.HasPrefix(b, []byte("cairn-capture: this file was omitted")) || s.Rechecked != 2 {
		t.Fatalf("the refused blob stored %q", b)
	}
	// A FIELD: the stream cursor itself matches the table. No placeholder can answer that.
	bad := synth(1, 50)
	bad[0].End = liveToken()
	_, err := p.sink("host-a", sinkTokenA, 0).Records(up(""), "subagent:a1", bad)
	var rc *RecheckRefusal
	if !errors.As(err, &rc) || rc.Field == "" {
		t.Fatalf("got %v, want a RecheckRefusal naming a field", err)
	}
}

// recordingTransport copies every request body it carries.
type recordingTransport struct{ to *bytes.Buffer }

func (r recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	r.to.Write(body)
	req.Body = io.NopCloser(bytes.NewReader(body))
	return http.DefaultTransport.RoundTrip(req)
}

// TestADisarmedInstanceHoldsAndGetsNoFurtherRequest: 503 capture-disarmed is a HeldError, nothing
// is advanced, and the instance gets no other request in the run.
func TestADisarmedInstanceHoldsAndGetsNoFurtherRequest(t *testing.T) {
	p := newPod(t, podOpts{disarmed: true})
	s := p.sink("host-a", sinkTokenA, 0)
	_, err := s.Records(up(""), "main", synth(3, 100))
	var he *HeldError
	if !errors.As(err, &he) || !strings.Contains(he.Reason, "capture-disarmed") {
		t.Fatalf("got %v, want a HeldError for capture-disarmed", err)
	}
	before := p.requests.Load()
	if err := s.Blob(up(""), "toolu_0005.txt", "b-v1", []byte("x\n")); !errors.As(err, &he) {
		t.Fatalf("a later write: %v", err)
	}
	if p.requests.Load() != before {
		t.Fatal("a held instance got another request in the same run")
	}
}

// TestAFullInstanceHoldsAndGetsNoFurtherRequest: 507 quota, the same.
func TestAFullInstanceHoldsAndGetsNoFurtherRequest(t *testing.T) {
	p := newPod(t, podOpts{quota: 64})
	s := p.sink("host-a", sinkTokenA, 0)
	_, err := s.Records(up(""), "main", synth(3, 100))
	var he *HeldError
	if !errors.As(err, &he) || !strings.Contains(he.Reason, "507") {
		t.Fatalf("got %v, want a HeldError for the quota", err)
	}
	before := p.requests.Load()
	if _, err := s.Records(up(""), "subagent:a1", synth(1, 10)); !errors.As(err, &he) {
		t.Fatalf("a later write: %v", err)
	}
	if p.requests.Load() != before || len(p.stored("main")) != 0 {
		t.Fatal("a full instance got another request, or stored something")
	}
}

// TestARefusedTokenIsFatalAndNeverEchoed: 401 is a FatalError, the instance gets no further
// request (each would count toward the lockout), and the token is in no error and no log line.
func TestARefusedTokenIsFatalAndNeverEchoed(t *testing.T) {
	p := newPod(t, podOpts{})
	const wrong = "fixture-capture-token-that-no-row-holds-not-real"
	s := p.sink("host-a", wrong, 0)
	var log bytes.Buffer
	s.Log = &log
	_, err := s.Records(up(""), "main", synth(2, 100))
	var fe *FatalError
	if !errors.As(err, &fe) || fe.Status != http.StatusUnauthorized {
		t.Fatalf("got %v, want a FatalError 401", err)
	}
	before := p.requests.Load()
	err2 := s.Blob(up(""), "toolu_0006.txt", "b-v1", []byte("x\n"))
	if !errors.As(err2, &fe) || p.requests.Load() != before {
		t.Fatal("a refused token was presented again in the same run")
	}
	for _, text := range []string{err.Error(), err2.Error(), log.String()} {
		if strings.Contains(text, wrong) {
			t.Fatal("the token was echoed")
		}
	}
}

// TestARootHeldByAnotherHostIsNotOwner: the uniform 409.
func TestARootHeldByAnotherHostIsNotOwner(t *testing.T) {
	p := newPod(t, podOpts{})
	if _, err := p.sink("host-b", sinkTokenB, 0).Records(up(""), "main", synth(1, 50)); err != nil {
		t.Fatal(err)
	}
	_, err := p.sink("host-a", sinkTokenA, 0).Records(up(""), "main", synth(2, 50))
	if !errors.Is(err, ErrNotOwner) {
		t.Fatalf("got %v, want ErrNotOwner", err)
	}
}

// TestARecordThatIsNotJSONShipsWrappedAndExact: a line that is not JSON (and not UTF-8) cannot be
// a `rec` as it is; it ships as an object holding its exact bytes.
func TestARecordThatIsNotJSONShipsWrappedAndExact(t *testing.T) {
	p := newPod(t, podOpts{})
	raw := []byte("not json \xff\xfe at all")
	if _, err := p.sink("host-a", sinkTokenA, 0).Records(up(""), "main", []Record{{Src: "0", Rec: raw, End: "22"}}); err != nil {
		t.Fatal(err)
	}
	got := p.stored("main")
	var w struct {
		Parts []json.RawMessage `json:"parts"`
	}
	if err := json.Unmarshal(got[0].Rec, &w); err != nil {
		t.Fatal(err)
	}
	var back []byte
	for _, part := range w.Parts {
		var text string
		var h struct{ Hex string }
		if json.Unmarshal(part, &text) == nil {
			back = append(back, text...)
		} else if json.Unmarshal(part, &h) == nil {
			b, _ := hex.DecodeString(h.Hex)
			back = append(back, b...)
		}
	}
	if !bytes.Equal(back, raw) {
		t.Fatalf("the wrapped record decodes to %q", back)
	}
}

// TestDeclaredScopesFailClosed: a name the archive would refuse becomes `*` instead of refusing
// every request of the session.
func TestDeclaredScopesFailClosed(t *testing.T) {
	got := strings.Join(declared([]string{"alpha-notes", "has space", "*", "beta-notes"}), ",")
	if got != "alpha-notes,beta-notes,*" {
		t.Fatalf("declared = %s", got)
	}
	if got := declared(nil); got == nil || len(got) != 0 {
		t.Fatal("an empty V must declare [] (the wire requires the key)")
	}
}

// TestTheSinkBoundsFitThePod pins the sink's copies of the pod's bounds against the pod's own.
func TestTheSinkBoundsFitThePod(t *testing.T) {
	if MaxRecordBytes != archive.MaxAssembledBytes || maxDeclared != archive.MaxDeclared ||
		maxRecordsPerRequest != archive.MaxRecords || maxFrames != archive.MaxFrames || WireSchema != worker.Schema {
		t.Fatal("a sink bound drifted from the pod's")
	}
	if DefaultMaxRequest > worker.DefaultMaxBody || MaxRequestCeiling != worker.DefaultMaxBody {
		t.Fatal("the sink's request bound is over the worker's body bound")
	}
	// The scope grammar, asked of the ARCHIVE itself: a declared name it refuses is an
	// InvalidError before anything is read.
	p := newPod(t, podOpts{})
	for _, s := range []string{"a", "alpha-notes", "A.b_c-d", strings.Repeat("x", 128), "", "-x", ".x", "a b", "a/b",
		strings.Repeat("x", 129), "é"} {
		_, err := p.arc.AppendRecords(archive.Uploader{Owner: sinkOwner.String(), Host: "host-a"},
			archive.Upload{Root: "s-scope", Runtime: "claude", To: "1", Declared: []string{s}}, "main",
			[]archive.Record{{Src: "0", Rec: []byte(`{}`)}})
		var inv *archive.InvalidError
		pod := !errors.As(err, &inv)
		if nameOK(s) != pod {
			t.Errorf("scope %q: sink %v, archive pattern %v", s, nameOK(s), pod)
		}
	}
}

func TestEndpointsAndTokenFiles(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://worker.example.invalid": true, "http://127.0.0.1:9": true, "http://[::1]:9/": true,
		"http://localhost:9": true, "http://worker.example.invalid": false, "ftp://127.0.0.1": false,
		"https://user:pw@worker.example.invalid": false, "https://worker.example.invalid/?q=1": false,
	} {
		if _, err := ValidateEndpoint(raw); (err == nil) != ok {
			t.Errorf("%s: %v", raw, err)
		}
	}
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	_ = os.WriteFile(good, []byte(sinkTokenA+"\n"), 0o600)
	if tok, err := ReadTokenFile(good); err != nil || tok != sinkTokenA {
		t.Fatalf("a 0600 token file: %v", err)
	}
	open := filepath.Join(dir, "open")
	_ = os.WriteFile(open, []byte(sinkTokenA+"\n"), 0o644)
	two := filepath.Join(dir, "two")
	_ = os.WriteFile(two, []byte(sinkTokenA+"\n"+sinkTokenB+"\n"), 0o600)
	for _, p := range []string{open, two, filepath.Join(dir, "absent")} {
		_, err := ReadTokenFile(p)
		if err == nil || strings.Contains(err.Error(), sinkTokenA) {
			t.Errorf("%s: %v", p, err)
		}
	}
}
