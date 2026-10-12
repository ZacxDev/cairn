package capture

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// The capture/v1 contract (`claudedocs/plan-cairn-plugins.md`, "The contracts"; served by
// `internal/worker`, stored by `internal/transcript/archive`). This file is its CLIENT, S3b.
const (
	// WireSchema is the one wire schema version the worker decodes.
	WireSchema = 1
	// DefaultMaxRequest is how large one request body this sink builds may be. A record (or blob)
	// whose request would be larger is sent as numbered continuation frames.
	//
	// It is deliberately well UNDER the worker's bound (`worker.DefaultMaxBody`, 8 MiB; pinned by
	// `TestTheSinkBoundsFitThePod`): the pod's memory per request is ~16× the body (#258's
	// measurement), and a smaller body is what makes framing — not a giant unframed request — the
	// path a large record takes.
	DefaultMaxRequest = 1 << 20
	// MaxRecordBytes is the largest ONE record or blob the pod stores, framed or not:
	// `archive.MaxAssembledBytes` (AP2), pinned equal by `TestTheSinkBoundsFitThePod`. A larger one
	// is replaced by a placeholder BEFORE any of it is sent (see [HTTPSink.omit]); a pod whose bound
	// is smaller answers 413 and gets the same placeholder.
	MaxRecordBytes = 4 << 20
	// maxDeclared mirrors `archive.MaxDeclared`; maxRecordsPerRequest mirrors `archive.MaxRecords`;
	// maxFrames mirrors `archive.MaxFrames`. All three pinned by `TestTheSinkBoundsFitThePod`.
	maxDeclared          = 256
	maxRecordsPerRequest = 10000
	maxFrames            = 4096
	// answerLimit bounds how much of a response body is read: every answer the worker gives is a
	// one-line JSON object or a short line of text.
	answerLimit = 64 << 10
)

// Endpoint is one instance's worker listener and the capture token for it.
type Endpoint struct {
	// URL is the worker listener's base URL (`https://…`, or `http://` on a loopback address).
	URL string
	// Token is the capture token. 🔴 It is read from a FILE ([ReadTokenFile]) and is never logged,
	// printed or put in an error.
	Token string
}

// HTTPSink uploads over the capture/v1 contract.
//
// What each answer means, and what the sink does — 🔴 a watermark advances ONLY on a 200:
//
//   - 200: stored. 202 is a staged non-final continuation frame.
//   - 409 naming a position (`{"stored_to":…}`): the instance holds the stream somewhere other than
//     `from` — a run whose acknowledgement was lost. The sink RESUMES from that position when it is
//     the end of a record this write carries, so every record lands exactly once (clause a); any
//     other position is refused ([UnresumableError]) rather than guessed at.
//   - 409 uniform (`{"refused":"held-by-another-uploader"}`): another (owner, host) holds the root.
//     [ErrNotOwner]; logged by the agent every run, nothing stored.
//   - 413 (`too-large`): one record or blob is over the pod's bound. It is replaced by a
//     PLACEHOLDER naming its size and the bound, sent with the SAME `src` and cursor, so the stream
//     moves on and the session's cursor is never wedged (decision 15's AP2 note). Counted in
//     [HTTPSink.Omitted] and logged; the record's bytes never leave the host.
//   - 422 (`recheck`): the pod's own redaction table matched something this agent sent.
//     [RecheckRefusal]; the agent HOLDS that stream or blob for good and logs it — it never
//     resends, and it has nothing else to send: every byte it sends was redacted on this host.
//   - 503 (`capture-disarmed`) and 507 (`quota`): the instance is not taking uploads. [HeldError]:
//     nothing is advanced, so nothing is lost — the next run retries — and the instance gets NO
//     further request in this run.
//   - 401 / 403: the token is refused. [FatalError]: no further request to that instance in this
//     run (each 401 counts toward the worker's lockout), and the run exits non-zero, loudly.
//   - anything else (400, 5xx, a network error): an error for this write; the next run retries.
type HTTPSink struct {
	// Host is this host's label — the one the capture token was minted for.
	Host string
	// Endpoints maps an instance alias to its worker listener.
	Endpoints map[string]Endpoint
	// Client is the HTTP client; nil is one with a 2-minute timeout.
	Client *http.Client
	// MaxRequest bounds one request body; 0 is [DefaultMaxRequest].
	MaxRequest int
	// MaxRecord bounds one record or blob before it is sent; 0 is [MaxRecordBytes].
	MaxRecord int
	// Log receives operator lines — never a token, never content.
	Log io.Writer

	// Omitted counts records and blobs replaced by a placeholder (413).
	Omitted int
	// Requests counts the requests sent.
	Requests int

	stopped map[string]error
}

// ErrNotOwner is the uniform 409: the root is held by another (owner, host).
var ErrNotOwner = errors.New("the instance answered that this session is held by another uploader (409); nothing was stored")

// UnresumableError is a 409 naming a position this write cannot resume from.
type UnresumableError struct{ StoredTo string }

func (e *UnresumableError) Error() string {
	return fmt.Sprintf("the instance holds this stream at %q, which is not the start or the end of any record this run "+
		"carries; nothing more of it is sent until that is resolved", e.StoredTo)
}

// RecheckRefusal is the pod's 422: its own redaction table matched what was sent. It names the
// record (an index into the write's records), the blob, or the request field — never a value.
type RecheckRefusal struct {
	Record int
	Blob   bool
	Field  string
}

func (e *RecheckRefusal) Error() string {
	switch {
	case e.Field != "":
		return fmt.Sprintf("the pod's re-check refused the request's %s (422); nothing was stored", e.Field)
	case e.Blob:
		return "the pod's re-check refused the blob (422); nothing was stored"
	}
	return fmt.Sprintf("the pod's re-check refused record %d (422); nothing of that request was stored", e.Record)
}

// HeldError is a 503 (capture disarmed) or a 507 (quota): retry later, nothing lost.
type HeldError struct {
	Instance, Reason string
}

func (e *HeldError) Error() string {
	return fmt.Sprintf("instance %s is not taking uploads (%s); nothing advanced, the next run retries", e.Instance, e.Reason)
}

// FatalError is a 401 or 403: the capture token is refused.
type FatalError struct {
	Instance string
	Status   int
}

func (e *FatalError) Error() string {
	return fmt.Sprintf("instance %s REFUSED the capture token (%d): no further upload is sent to it; check the token "+
		"file and that the token's row is still in the instance's -worker-tokens", e.Instance, e.Status)
}

// ErrWithdrawalUnbuilt is the answer to a withdrawal until S6 builds the route.
var ErrWithdrawalUnbuilt = errors.New("the withdraw route is S6's and is not built: the shipped prefix stays where it " +
	"is, and NOTHING more of this session is sent anywhere until it can be withdrawn")

// ValidateEndpoint refuses a URL that is not `https`, or plain `http` to anything but a loopback
// address: the capture token rides in a header.
func ValidateEndpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("worker URL %q is not an absolute http(s) URL with no credentials, query or fragment", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !loopbackHost(u.Hostname()) {
			return "", fmt.Errorf("worker URL %q is plain http to a non-loopback host; the capture token would cross the "+
				"network in clear — use https", raw)
		}
	default:
		return "", fmt.Errorf("worker URL %q is not http(s)", raw)
	}
	if RedactionOff() && !loopbackHost(u.Hostname()) {
		return "", fmt.Errorf("this is a REDACTION-OFF test build; it sends only to a loopback address, not %q", raw)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func loopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// ReadTokenFile reads a capture token from a file that only its owner may read. 🔴 The token is
// never echoed: not in an error, not in a log.
func ReadTokenFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("the capture token file %s cannot be read: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("the capture token file %s is not a regular file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("the capture token file %s is readable by group or others (%#o); chmod 600 it", path, info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("the capture token file %s cannot be read: %w", path, err)
	}
	tok := strings.TrimSpace(string(raw))
	if tok == "" || strings.ContainsAny(tok, " \t\r\n") {
		return "", fmt.Errorf("the capture token file %s does not hold exactly one token on one line", path)
	}
	return tok, nil
}

func (s *HTTPSink) logf(format string, args ...any) {
	if s.Log != nil {
		fmt.Fprintf(s.Log, format+"\n", args...)
	}
}

func (s *HTTPSink) maxRequest() int {
	if s.MaxRequest > 0 {
		return s.MaxRequest
	}
	return DefaultMaxRequest
}

func (s *HTTPSink) maxRecord() int {
	if s.MaxRecord > 0 {
		return s.MaxRecord
	}
	return MaxRecordBytes
}

func (s *HTTPSink) endpoint(instance string) (Endpoint, error) {
	if err := s.stopped[instance]; err != nil {
		return Endpoint{}, err
	}
	ep, ok := s.Endpoints[instance]
	if !ok {
		return Endpoint{}, fmt.Errorf("no worker listener is configured for instance %s (-worker %s=<url>); nothing was sent", instance, instance)
	}
	return ep, nil
}

func (s *HTTPSink) stop(instance string, err error) error {
	if s.stopped == nil {
		s.stopped = map[string]error{}
	}
	s.stopped[instance] = err
	return err
}

// answer is every key a worker answer carries.
type answer struct {
	StoredTo *string `json:"stored_to"`
	Frame    *int    `json:"frame"`
	Of       *int    `json:"of"`
	Refused  string  `json:"refused"`
	Record   *int    `json:"record"`
	Blob     *string `json:"blob"`
	Field    *string `json:"field"`
	MaxBytes int64   `json:"max_bytes"`
}

// post sends one request and classifies the answer. A status this function returns with a nil
// error is one the caller branches on (200, 202, 409 stale, 413); every other outcome is an error.
func (s *HTTPSink) post(instance, path string, body []byte) (int, answer, error) {
	ep, err := s.endpoint(instance)
	if err != nil {
		return 0, answer{}, err
	}
	req, err := http.NewRequest(http.MethodPost, ep.URL+path, bytes.NewReader(body))
	if err != nil {
		return 0, answer{}, err
	}
	req.Header.Set("Authorization", "Bearer "+ep.Token)
	req.Header.Set("Content-Type", "application/json")
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	s.Requests++
	resp, err := client.Do(req)
	if err != nil {
		// The URL is ours; the error never carries the token (it is a header).
		return 0, answer{}, fmt.Errorf("the upload to instance %s failed: %w", instance, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, answerLimit))
	var a answer
	_ = json.Unmarshal(raw, &a)
	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusRequestEntityTooLarge:
		return resp.StatusCode, a, nil
	case http.StatusConflict:
		if a.StoredTo != nil {
			return resp.StatusCode, a, nil
		}
		return resp.StatusCode, a, ErrNotOwner
	case http.StatusUnprocessableEntity:
		r := &RecheckRefusal{Record: -1}
		switch {
		case a.Field != nil:
			r.Field = *a.Field
		case a.Blob != nil:
			r.Blob = true
		case a.Record != nil:
			r.Record = *a.Record
		}
		return resp.StatusCode, a, r
	case http.StatusServiceUnavailable:
		reason := "503"
		if a.Refused != "" {
			reason = a.Refused
		}
		return resp.StatusCode, a, s.stop(instance, &HeldError{Instance: instance, Reason: reason})
	case http.StatusInsufficientStorage:
		return resp.StatusCode, a, s.stop(instance, &HeldError{Instance: instance, Reason: "quota reached (507)"})
	case http.StatusUnauthorized, http.StatusForbidden:
		return resp.StatusCode, a, s.stop(instance, &FatalError{Instance: instance, Status: resp.StatusCode})
	}
	return resp.StatusCode, a, fmt.Errorf("instance %s answered %d: %s", instance, resp.StatusCode, firstLine(raw))
}

func firstLine(b []byte) string {
	line, _, _ := bytes.Cut(bytes.TrimSpace(b), []byte("\n"))
	if len(line) > 200 {
		line = line[:200]
	}
	return string(line)
}

// declared is the session's `V` as the wire admits it: every name the archive would refuse
// (`archive.scopeName`) becomes `*`, and a set over the bound keeps `*` — both FAIL CLOSED (a `*`
// makes the session owner-only, decision 4), where sending the name as it is would refuse every
// request of the session (400) forever.
func declared(v []string) []string {
	out := []string{}
	star := false
	for _, s := range v {
		if s == "*" || !nameOK(s) {
			star = true
			continue
		}
		out = append(out, s)
	}
	if len(out) >= maxDeclared {
		out, star = out[:maxDeclared-1], true
	}
	if star {
		out = append(out, "*")
	}
	return out
}

// nameOK is `archive.scopeName` AND `archive.blobName` — the two are the one grammar
// `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$` — each pinned against the archive itself by
// `TestTheSinkBoundsFitThePod`.
func nameOK(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		alnum := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
		if !alnum && (i == 0 || (c != '.' && c != '_' && c != '-')) {
			return false
		}
	}
	return true
}

// header renders the fields every request carries, as the opening of a JSON object (no closing
// brace) — the body is assembled by hand so a record's bytes are never re-encoded.
func (s *HTTPSink) header(u Upload, from, to string) []byte {
	type head struct {
		Schema   int      `json:"schema"`
		Runtime  string   `json:"runtime"`
		Host     string   `json:"host"`
		From     string   `json:"from"`
		To       string   `json:"to"`
		Declared []string `json:"declared_scopes"`
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(head{Schema: WireSchema, Runtime: u.Runtime, Host: s.Host, From: from, To: to, Declared: declared(u.Declared)})
	out := bytes.TrimSpace(b.Bytes())
	return out[:len(out)-1] // drop the closing brace
}

func quoteJSON(v string) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimSpace(b.Bytes())
}

// wireRec is a record's bytes as the wire's `rec` carries them: the record itself when it is ONE
// JSON value (byte-identical), else an object holding its exact bytes — the pod stores only JSON
// values, and a line that is not JSON would otherwise refuse every request of its stream (400)
// forever. The object is `{"cairn_capture":"not-json","parts":[…]}`: the line's valid UTF-8 runs as
// JSON strings, and each run of bytes that is not UTF-8 as `{"hex":"…"}`, so the bytes are exact
// AND the pod's re-check reads the text as text.
//
// ⚠ NOT base64 of the whole line, which was built first and measured wrong: the pod's re-check
// runs its entropy rule over a long base64 string and refused (422) a line with nothing secret in
// it, holding the stream for good.
func wireRec(rec []byte) []byte {
	if json.Valid(rec) {
		return rec
	}
	var parts []any
	for len(rec) > 0 {
		n := 0
		for n < len(rec) {
			r, size := utf8.DecodeRune(rec[n:])
			if r == utf8.RuneError && size <= 1 {
				break
			}
			n += size
		}
		if n > 0 {
			parts = append(parts, string(rec[:n]))
			rec = rec[n:]
			continue
		}
		for n < len(rec) {
			if r, size := utf8.DecodeRune(rec[n:]); r != utf8.RuneError || size > 1 {
				break
			}
			n++
		}
		parts = append(parts, map[string]string{"hex": hex.EncodeToString(rec[:n])})
		rec = rec[n:]
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{"cairn_capture": "not-json", "parts": parts})
	return bytes.TrimSpace(b.Bytes())
}

// placeholder stands in for a record over the pod's bound: it names the omission, the size and
// the bound, and carries none of the record's bytes.
func placeholder(size int, limit int64) []byte {
	return []byte(fmt.Sprintf(`{"cairn_capture":"omitted","reason":"too-large","bytes":%d,"max_bytes":%d}`, size, limit))
}

func blobPlaceholder(size int, limit int64) []byte {
	return []byte(fmt.Sprintf("cairn-capture: this file was omitted — %d bytes is over the instance's %d-byte bound on one stored file\n", size, limit))
}

func (s *HTTPSink) recordsPath(root, stream string) string {
	return "/capture/v1/sessions/" + url.PathEscape(root) + "/streams/" + url.PathEscape(stream) + "/records"
}

func (s *HTTPSink) blobPath(root, name string) string {
	return "/capture/v1/sessions/" + url.PathEscape(root) + "/blobs/" + url.PathEscape(WireBlobName(name))
}

// WireBlobName maps a blob's name in the agent — its path RELATIVE to the session directory,
// `tool-results/toolu_….txt` or `subagents/agent-….json` — onto the one path component the pod
// stores a blob under (`archive.blobName`, `^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`).
//
// 🔴 S2 NAMED BLOBS BY RELATIVE PATH AND S3's POD REFUSES A `/` — measured: every blob of the
// synthetic world answered 400, forever. So: a persisted tool result keeps its BASE name (what a
// reader joins to a `tool_use_id`); any other path has its `/` spelled `.`; and a name that still
// does not fit is a digest of the path, so a strange file name refuses nothing.
func WireBlobName(rel string) string {
	if base, ok := strings.CutPrefix(rel, "tool-results/"); ok && !strings.Contains(base, "/") && nameOK(base) {
		return base
	}
	if dotted := strings.ReplaceAll(rel, "/", "."); nameOK(dotted) && !strings.HasPrefix(rel, "tool-results/") {
		return dotted
	}
	return "f-" + digest([]byte(rel))[:40]
}

// Records implements [Sink].
func (s *HTTPSink) Records(u Upload, stream string, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	if _, err := s.endpoint(u.Instance); err != nil {
		return err
	}
	// The wire bytes of every record, decided ONCE, so a resent request carries the same bytes.
	wire := make([][]byte, len(recs))
	for i, r := range recs {
		if r.End == "" {
			return fmt.Errorf("record %d of %s/%s carries no end cursor", i, u.Root, stream)
		}
		wire[i] = wireRec(r.Rec)
		if len(wire[i]) > s.maxRecord() {
			wire[i] = s.omit(u, stream, r.Src, len(wire[i]), int64(s.maxRecord()))
		}
	}
	path := s.recordsPath(u.Root, stream)
	from := u.From
	solo := false
	resumes := 0
	for i := 0; i < len(recs); {
		// One request: as many records as fit, or one record framed.
		envelope := len(s.header(u, from, recs[i].End)) + 32
		j := i
		size := envelope
		for j < len(recs) && j-i < maxRecordsPerRequest {
			n := len(wire[j]) + len(recs[j].Src) + 24
			if j > i && (solo || size+n > s.maxRequest()) {
				break
			}
			size += n
			j++
		}
		var (
			status int
			ans    answer
			err    error
		)
		if j == i+1 && size > s.maxRequest() {
			status, ans, err = s.framedRecord(u, path, from, recs[i], wire[i])
		} else {
			status, ans, err = s.post(u.Instance, path, s.recordsBody(u, from, recs[i:j], wire[i:j]))
		}
		var rc *RecheckRefusal
		if errors.As(err, &rc) && rc.Record >= 0 {
			rc.Record += i
		}
		if err != nil {
			return err
		}
		switch status {
		case http.StatusOK:
			if ans.StoredTo == nil || *ans.StoredTo != recs[j-1].End {
				return fmt.Errorf("instance %s answered 200 without storing to %q", u.Instance, recs[j-1].End)
			}
			from, i = recs[j-1].End, j
		case http.StatusConflict:
			k := resumeAt(*ans.StoredTo, from, recs, i)
			if k < 0 || resumes > len(recs) {
				return &UnresumableError{StoredTo: *ans.StoredTo}
			}
			resumes++
			s.logf("resume %s/%s at %s: the instance already holds it (a lost acknowledgement)", u.Root, stream, *ans.StoredTo)
			from, i = *ans.StoredTo, k
		case http.StatusRequestEntityTooLarge:
			if j-i > 1 {
				solo = true // find WHICH record: one per request from here
				continue
			}
			if bytes.HasPrefix(wire[i], []byte(`{"cairn_capture":"omitted"`)) {
				return fmt.Errorf("instance %s refused even the placeholder for record %s as too large", u.Instance, recs[i].Src)
			}
			wire[i] = s.omit(u, stream, recs[i].Src, len(wire[i]), ans.MaxBytes)
		default:
			return fmt.Errorf("instance %s answered %d to an unframed upload", u.Instance, status)
		}
	}
	return nil
}

// resumeAt is the index of the first record NOT yet stored when the instance holds the stream at
// `stored` — the record after the one whose end it is — or -1 when `stored` is no such end.
func resumeAt(stored, from string, recs []Record, i int) int {
	if stored == from {
		return -1
	}
	for k := i; k < len(recs); k++ {
		if recs[k].End == stored {
			return k + 1
		}
	}
	return -1
}

func (s *HTTPSink) omit(u Upload, stream, src string, size int, limit int64) []byte {
	s.Omitted++
	s.logf("omitted record %s of %s/%s: %d bytes is over the instance's %d-byte bound on one record; a placeholder "+
		"naming the omission is stored in its place", src, u.Root, stream, size, limit)
	return placeholder(size, limit)
}

func (s *HTTPSink) recordsBody(u Upload, from string, recs []Record, wire [][]byte) []byte {
	var b bytes.Buffer
	b.Write(s.header(u, from, recs[len(recs)-1].End))
	b.WriteString(`,"records":[`)
	for k, r := range recs {
		if k > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"src":`)
		b.Write(quoteJSON(r.Src))
		b.WriteString(`,"rec":`)
		b.Write(wire[k])
		b.WriteByte('}')
	}
	b.WriteString("]}")
	return b.Bytes()
}

// chunks splits data into the frames one request each may carry, base64 inflating 4/3.
func (s *HTTPSink) chunks(data []byte, envelope int) ([][]byte, error) {
	per := (s.maxRequest() - envelope - 64) * 3 / 4
	if per < 1 {
		return nil, fmt.Errorf("a %d-byte request bound leaves no room for a frame", s.maxRequest())
	}
	var out [][]byte
	for len(data) > 0 {
		n := min(per, len(data))
		out = append(out, data[:n])
		data = data[n:]
	}
	if len(out) == 1 { // a sequence is at least two frames (`archive.validateUpload`)
		half := len(out[0]) / 2
		out = [][]byte{out[0][:half], out[0][half:]}
	}
	if len(out) > maxFrames {
		return nil, fmt.Errorf("%d frames is over the instance's %d-frame bound", len(out), maxFrames)
	}
	return out, nil
}

// framedRecord sends one record as continuation frames. It returns the FINAL frame's answer, or
// the first answer that is not a 202.
func (s *HTTPSink) framedRecord(u Upload, path, from string, r Record, wire []byte) (int, answer, error) {
	envelope := len(s.header(u, from, r.End)) + len(r.Src) + 96
	parts, err := s.chunks(wire, envelope)
	if err != nil {
		return 0, answer{}, err
	}
	for attempt := 0; ; attempt++ {
		status, ans, err := s.frames(len(parts), func(k int) []byte {
			var b bytes.Buffer
			b.Write(s.header(u, from, r.End))
			b.WriteString(`,"records":[{"src":`)
			b.Write(quoteJSON(r.Src))
			b.WriteString(`,"chunk":"`)
			b.WriteString(base64.StdEncoding.EncodeToString(parts[k]))
			fmt.Fprintf(&b, `"}],"frames":{"index":%d,"count":%d}}`, k, len(parts))
			return b.Bytes()
		}, u.Instance, path)
		// A 400 on a frame past the first is the instance having dropped the staged sequence (it
		// restarted, or swept it): the record restarts at frame 0, once.
		if err != nil && status == http.StatusBadRequest && attempt == 0 {
			continue
		}
		return status, ans, err
	}
}

// frames sends frames 0..n-1, each expected to answer 202 until the last.
func (s *HTTPSink) frames(n int, body func(int) []byte, instance, path string) (int, answer, error) {
	for k := 0; k < n; k++ {
		status, ans, err := s.post(instance, path, body(k))
		if err != nil {
			if k == 0 && status == http.StatusBadRequest {
				status = 0 // only a mid-sequence 400 is a dropped sequence
			}
			return status, ans, err
		}
		if k < n-1 {
			if status == http.StatusAccepted && ans.Frame != nil && *ans.Frame == k && ans.Of != nil && *ans.Of == n {
				continue
			}
			if status == http.StatusAccepted {
				return 0, ans, fmt.Errorf("instance %s acknowledged frame %d of %d as another frame", instance, k, n)
			}
			return status, ans, nil // 409 or 413 on a frame: the caller branches on it
		}
		if status == http.StatusAccepted {
			return 0, ans, fmt.Errorf("instance %s staged the final frame instead of storing the record", instance)
		}
		return status, ans, nil
	}
	return 0, answer{}, errors.New("no frames")
}

// Blob implements [Sink].
func (s *HTTPSink) Blob(u Upload, name, to string, data []byte) error {
	if _, err := s.endpoint(u.Instance); err != nil {
		return err
	}
	if len(data) > s.maxRecord() {
		data = s.omitBlob(u, name, len(data), int64(s.maxRecord()))
	}
	path := s.blobPath(u.Root, name)
	from := u.From
	for attempt := 0; attempt < 4; attempt++ {
		var (
			status int
			ans    answer
			err    error
		)
		envelope := len(s.header(u, from, to)) + 64
		if envelope+base64.StdEncoding.EncodedLen(len(data)) <= s.maxRequest() {
			var b bytes.Buffer
			b.Write(s.header(u, from, to))
			fmt.Fprintf(&b, `,"data":"%s"}`, base64.StdEncoding.EncodeToString(data))
			status, ans, err = s.post(u.Instance, path, b.Bytes())
		} else {
			status, ans, err = s.framedBlob(u, path, from, to, data)
		}
		if err != nil {
			return err
		}
		switch status {
		case http.StatusOK:
			if ans.StoredTo == nil || *ans.StoredTo != to {
				return fmt.Errorf("instance %s answered 200 without storing blob version %q", u.Instance, to)
			}
			return nil
		case http.StatusConflict:
			if *ans.StoredTo == to {
				s.logf("resume %s blob %s: the instance already holds this version (a lost acknowledgement)", u.Root, name)
				return nil
			}
			// A whole-object replace: the instance holds another version, so this one replaces it.
			from = *ans.StoredTo
		case http.StatusRequestEntityTooLarge:
			if bytes.HasPrefix(data, []byte("cairn-capture: this file was omitted")) {
				return fmt.Errorf("instance %s refused even the placeholder for blob %s as too large", u.Instance, name)
			}
			data = s.omitBlob(u, name, len(data), ans.MaxBytes)
		default:
			return fmt.Errorf("instance %s answered %d to a blob upload", u.Instance, status)
		}
	}
	return fmt.Errorf("blob %s of %s did not settle in four attempts", name, u.Root)
}

func (s *HTTPSink) omitBlob(u Upload, name string, size int, limit int64) []byte {
	s.Omitted++
	s.logf("omitted blob %s of %s: %d bytes is over the instance's %d-byte bound on one file; a placeholder naming the "+
		"omission is stored in its place", name, u.Root, size, limit)
	return blobPlaceholder(size, limit)
}

func (s *HTTPSink) framedBlob(u Upload, path, from, to string, data []byte) (int, answer, error) {
	parts, err := s.chunks(data, len(s.header(u, from, to))+96)
	if err != nil {
		return 0, answer{}, err
	}
	for attempt := 0; ; attempt++ {
		status, ans, err := s.frames(len(parts), func(k int) []byte {
			var b bytes.Buffer
			b.Write(s.header(u, from, to))
			fmt.Fprintf(&b, `,"data":"%s","frames":{"index":%d,"count":%d}}`, base64.StdEncoding.EncodeToString(parts[k]), k, len(parts))
			return b.Bytes()
		}, u.Instance, path)
		if err != nil && status == http.StatusBadRequest && attempt == 0 {
			continue
		}
		return status, ans, err
	}
}

// Withdraw implements [Sink]. 🔴 THE ROUTE IS S6's: until it exists the withdrawal fails, which
// the agent treats as "withdrawal not yet done" — the session is not re-shipped anywhere and
// nothing more of it is sent, every run, logged.
func (s *HTTPSink) Withdraw(instance, root string) error {
	return ErrWithdrawalUnbuilt
}
