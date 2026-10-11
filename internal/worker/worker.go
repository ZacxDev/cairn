// Package worker is `cairn-ui`'s THIRD listener (plan decision 8, `-worker-addr`): the routes a
// host-side machine reaches with a WORKER token rather than a browser credential. Slice S3 of
// `claudedocs/plan-cairn-plugins.md` gives it the capture upload routes; the plugin API (S5) and
// the withdrawal route (S6) join the same ledger later.
//
// 🔴 CAPTURE IS DISARMED BY DEFAULT, AND THE REFUSAL IS HERE. Operator decision O16: the arming
// gate (O15 — a fresh auditor-written held-back set scoring ≥ 90% of leaks caught and ≤ 15% of
// clean lines damaged) FAILS today, and the client read ledger (S11) does not exist, so capture is
// armed on NO instance. [Config.Armed] is false unless `cairn-ui` was started with the explicit
// `-arm-transcript-capture` flag (no default, no environment spelling); while it is false every
// upload route answers 503 [DisarmedBody] AFTER authenticating and BEFORE reading the body, and
// nothing is stored, created or staged. Arming is the operator's act, taken only once BOTH
// preconditions hold; nothing in this package or in `internal/transcript/archive` can do it.
//
// 🔴 IT READS NO COOKIE AND SETS NONE, AND IT IS OUTSIDE THE BROWSER'S CROSS-SITE GATES BY DESIGN
// (the presence precedent): a worker POST carries no `Origin` and must not be made to pass them.
// A worker token is unknown to `identity.Backends`, so every browser row and the pod answer it as
// they answer garbage — pinned by `TestOnlyTheBrowserProgramImportsWorker` and, against the
// running binary, by `cmd/cairn-ui`'s `TestAWorkerTokenIsGarbageToEveryBrowserRow`.
package worker

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"sync"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
)

// The capture routes (the plan's contracts).
const (
	RecordsPattern = "/capture/v1/sessions/{root}/streams/{stream}/records"
	BlobsPattern   = "/capture/v1/sessions/{root}/blobs/{name}"
)

// Bodies every refusal of a kind shares. Uniform on purpose: each says what happened and nothing
// about whom.
const (
	unauthorizedBody = "unauthorized\n"
	// DisarmedBody answers every upload while capture is unarmed (O16).
	DisarmedBody = `{"refused":"capture-disarmed"}` + "\n"
	// NotOwnerBody is decision 15's ownership refusal — ONE body that names neither the holder nor
	// its host (contracts: "409 uniform").
	NotOwnerBody = `{"refused":"held-by-another-uploader"}` + "\n"
)

type route struct {
	pattern string
	kind    TokenKind
	handle  func(w *Worker, rw http.ResponseWriter, r *http.Request, row TokenRow)
}

// routes is THE dispatch table; [Routes] derives the ledger from it and [New] the mux, so the
// two cannot disagree. Every route is `POST`: every one changes state.
var routes = []route{
	{RecordsPattern, KindCapture, (*Worker).handleRecords},
	{BlobsPattern, KindCapture, (*Worker).handleBlob},
}

// Routes is every `<METHOD> <pattern> <token kind>` this listener dispatches, sorted. Adding a row
// is adding an endpoint a host-side token can reach; `TestTheWorkerRouteLedgerIsExactlyThis` is
// where somebody has to write it down.
func Routes() []string {
	out := make([]string, 0, len(routes))
	for _, rt := range routes {
		out = append(out, http.MethodPost+" "+rt.pattern+" "+string(rt.kind))
	}
	slices.Sort(out)
	return out
}

// Config is everything the listener needs.
type Config struct {
	// TokenFile is RE-READ on every request, so deleting a row revokes it with no restart.
	TokenFile string
	// Owner is the instance's SOLE capture owner (Q7).
	Owner   Owner
	Archive *archive.Archive
	// Armed is the operator's explicit arming (`-arm-transcript-capture`). FALSE is every
	// instance today (O16): every upload is refused 503 and nothing is stored.
	Armed          bool
	Limiter        *netid.RateLimiter
	TrustedProxies []netip.Prefix
	// MaxBody bounds one request body before decoding; 0 is [DefaultMaxBody].
	MaxBody int64
	// Log receives operator lines — never a token, never content.
	Log func(string)
}

// DefaultMaxBody bounds a request. A record larger than what fits is sent as continuation frames.
const DefaultMaxBody = 8 << 20

// Worker is the listener's handler.
type Worker struct {
	cfg Config
	mux *http.ServeMux

	mu        sync.Mutex
	announced map[string]bool
}

// ErrMisconfigured is returned by [New] for a config missing a required part.
var ErrMisconfigured = errors.New("worker listener: incomplete configuration")

// New builds the handler and performs the STARTUP read of the token file.
func New(cfg Config) (*Worker, []TokenRow, error) {
	if cfg.TokenFile == "" || cfg.Archive == nil || cfg.Limiter == nil || !cfg.Owner.Kind.Valid() || cfg.Owner.ID == "" {
		return nil, nil, ErrMisconfigured
	}
	rows, err := LoadTokens(cfg.TokenFile, cfg.Owner)
	if err != nil {
		return nil, nil, err
	}
	if cfg.Log == nil {
		cfg.Log = func(string) {}
	}
	if cfg.MaxBody <= 0 {
		cfg.MaxBody = DefaultMaxBody
	}
	w := &Worker{cfg: cfg, mux: http.NewServeMux(), announced: map[string]bool{}}
	for _, rt := range routes {
		rt := rt
		w.mux.HandleFunc(http.MethodPost+" "+rt.pattern, func(rw http.ResponseWriter, r *http.Request) {
			w.serve(rw, r, rt)
		})
	}
	return w, rows, nil
}

// ServeHTTP dispatches through the ledger-built mux. A path in no row is 404; a known path with
// another method is 405.
func (w *Worker) ServeHTTP(rw http.ResponseWriter, r *http.Request) { w.mux.ServeHTTP(rw, r) }

func (w *Worker) once(line string) {
	w.mu.Lock()
	said := w.announced[line]
	w.announced[line] = true
	w.mu.Unlock()
	if !said {
		w.cfg.Log(line)
	}
}

// authenticate re-reads the token file and matches `presented` against every admitted row of
// `kind`. 🔴 No early exit: the loop visits every row whether or not it matched.
func (w *Worker) authenticate(presented string, kind TokenKind) (TokenRow, bool) {
	if presented == "" {
		return TokenRow{}, false
	}
	data, err := os.ReadFile(w.cfg.TokenFile)
	if err != nil {
		w.once(fmt.Sprintf("worker: the token file %s cannot be read (%v); every worker request is refused until it can",
			w.cfg.TokenFile, err))
		return TokenRow{}, false
	}
	rows, problems := parseTokens(data)
	for _, p := range problems {
		w.once("worker: token file row refused: " + p.Error())
	}
	digest := control.HashToken(presented)
	var matched *TokenRow
	for _, row := range rows {
		if err := admit(row, w.cfg.Owner); err != nil {
			w.once(fmt.Sprintf("worker: token file line %d (digest %s…) refused: %v", row.Line, row.DigestPrefix(), err))
			continue
		}
		if control.EqualHash(digest, row.Digest) && row.Kind == kind {
			found := row
			matched = &found
		}
	}
	if matched == nil {
		return TokenRow{}, false
	}
	return *matched, true
}

func (w *Worker) serve(rw http.ResponseWriter, r *http.Request, rt route) {
	client, _, ok := netid.ResolveClient(r.Header, r.RemoteAddr, w.cfg.TrustedProxies)
	if !ok {
		w.cfg.Log(fmt.Sprintf("worker: refused: no client identity from peer %q", r.RemoteAddr))
		refuseUnauthorized(rw)
		return
	}
	if w.cfg.Limiter.LockedOut(client) {
		w.cfg.Log("worker: refused: " + client + " is locked out")
		refuseUnauthorized(rw)
		return
	}
	row, ok := w.authenticate(authz.PresentedToken(r.Header.Get("Authorization")), rt.kind)
	if !ok {
		if w.cfg.Limiter.RecordFailure(client) {
			w.cfg.Log("worker: refused: bad token from " + client + " — LOCKOUT TRIGGERED")
		} else {
			w.cfg.Log("worker: refused: bad token from " + client)
		}
		refuseUnauthorized(rw)
		return
	}
	// 🔴 THE DISARM GATE: after authentication, so an unauthenticated caller learns nothing about
	// the instance's state and the lockout still counts its guesses; before the body is read, so a
	// disarmed instance stores, creates and stages NOTHING.
	if !w.cfg.Armed {
		w.once("worker: capture is DISARMED on this instance (no -arm-transcript-capture); uploads are refused 503")
		writeJSON(rw, http.StatusServiceUnavailable, DisarmedBody)
		return
	}
	rt.handle(w, rw, r, row)
}

func refuseUnauthorized(rw http.ResponseWriter) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(rw, unauthorizedBody)
}

func writeJSON(rw http.ResponseWriter, status int, body string) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_, _ = io.WriteString(rw, body)
}

func badRequest(rw http.ResponseWriter, err error) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.WriteHeader(http.StatusBadRequest)
	_, _ = io.WriteString(rw, err.Error()+"\n")
}

func (w *Worker) handleRecords(rw http.ResponseWriter, r *http.Request, row TokenRow) {
	req, err := decodeRecords(http.MaxBytesReader(rw, r.Body, w.cfg.MaxBody))
	if err != nil {
		badRequest(rw, err)
		return
	}
	// 🔴 THE HOST COMES FROM THE TOKEN ROW, AND A BODY NAMING ANOTHER IS REFUSED BEFORE ANY WRITE —
	// what catches a token copied to the wrong machine (S3's test plan).
	if req.Host != row.Host {
		badRequest(rw, fmt.Errorf("host %q is not this token's host", req.Host))
		return
	}
	req.Upload.Root = r.PathValue("root")
	st, err := w.cfg.Archive.AppendRecords(uploader(row), req.Upload, r.PathValue("stream"), req.Records)
	w.answer(rw, st, err, true)
}

func (w *Worker) handleBlob(rw http.ResponseWriter, r *http.Request, row TokenRow) {
	req, err := decodeBlob(http.MaxBytesReader(rw, r.Body, w.cfg.MaxBody))
	if err != nil {
		badRequest(rw, err)
		return
	}
	if req.Host != row.Host {
		badRequest(rw, fmt.Errorf("host %q is not this token's host", req.Host))
		return
	}
	req.Upload.Root = r.PathValue("root")
	st, err := w.cfg.Archive.PutBlob(uploader(row), req.Upload, r.PathValue("name"), req.Data)
	w.answer(rw, st, err, false)
}

func uploader(row TokenRow) archive.Uploader {
	return archive.Uploader{Owner: row.Owner.String(), Host: row.Host}
}

// answer maps the archive's outcome onto the contract's statuses.
func (w *Worker) answer(rw http.ResponseWriter, st archive.Stored, err error, records bool) {
	var (
		stale   *archive.StaleError
		recheck *archive.RecheckError
		quota   *archive.QuotaError
		inv     *archive.InvalidError
	)
	switch {
	case err == nil && st.Pending:
		writeJSON(rw, http.StatusAccepted, fmt.Sprintf(`{"frame":%d,"of":%d}`+"\n", st.FrameIndex, st.FrameCount))
	case err == nil && records:
		writeJSON(rw, http.StatusOK, fmt.Sprintf(`{"stored_to":%s,"seq_to":%d}`+"\n", quote(st.StoredTo), st.SeqTo))
	case err == nil:
		writeJSON(rw, http.StatusOK, fmt.Sprintf(`{"stored_to":%s}`+"\n", quote(st.StoredTo)))
	case errors.Is(err, archive.ErrNotOwner):
		writeJSON(rw, http.StatusConflict, NotOwnerBody)
	case errors.As(err, &stale):
		writeJSON(rw, http.StatusConflict, fmt.Sprintf(`{"stored_to":%s}`+"\n", quote(stale.StoredTo)))
	case errors.As(err, &recheck):
		// The index or the blob name — never the value.
		if recheck.Blob != "" {
			writeJSON(rw, http.StatusUnprocessableEntity, fmt.Sprintf(`{"refused":"recheck","blob":%s}`+"\n", quote(recheck.Blob)))
		} else {
			writeJSON(rw, http.StatusUnprocessableEntity, fmt.Sprintf(`{"refused":"recheck","record":%d}`+"\n", recheck.Record))
		}
		w.cfg.Log("worker: refused an upload the redaction table matched (" + recheck.Error() + ")")
	case errors.As(err, &quota):
		writeJSON(rw, http.StatusInsufficientStorage, fmt.Sprintf(`{"refused":"quota","quota_bytes":%d}`+"\n", quota.Quota))
		w.once("worker: the transcript quota is reached; uploads are refused 507 until retention frees space")
	case errors.As(err, &inv):
		badRequest(rw, inv)
	default:
		w.cfg.Log("worker: an upload failed: " + err.Error())
		rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
		rw.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(rw, "internal error\n")
	}
}
