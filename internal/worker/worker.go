// Package worker is `cairn-ui`'s THIRD listener (plan decision 8, `-worker-addr`): the routes a
// host-side machine reaches with a WORKER token rather than a browser credential. Slice S3 of
// `claudedocs/plan-cairn-plugins.md` gives it the capture upload routes, slice S5 the PLUGIN API
// (`plugin.go`) and slice S6 the capture WITHDRAWAL route (`withdraw.go`).
//
// 🔴 CAPTURE IS DISARMED BY DEFAULT, AND THE REFUSAL IS HERE. Operator decision O16 armed NO
// instance. The redactor's held-back gate (O15 — ≥ 90% of leaks caught, ≤ 15% of clean lines
// damaged on a fresh auditor-written set) still FAILS as a measurement, but operator decision O17
// WAIVED it as an arming precondition; the plan's remaining preconditions are in its Rollout
// section, and nothing here can tell whether they hold. [Config.Armed] is false unless `cairn-ui`
// was started with the explicit `-arm-transcript-capture` flag (no default, no environment
// spelling); while it is false every upload route answers 503 [DisarmedBody] AFTER authenticating
// and BEFORE reading the body, and nothing is stored, created or staged. Arming is the operator's
// act; nothing in this package or in `internal/transcript/archive` can do it.
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
	"github.com/ZacxDev/cairn/internal/plugins"
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
	// DeletedBody answers an upload for a root whose stored session was deleted on this instance
	// (S6): final for that root — the uploader stops sending the session.
	DeletedBody = `{"refused":"deleted"}` + "\n"
)

type route struct {
	method  string
	pattern string
	kind    TokenKind
	// upload marks a route that STORES what a capture token sends, and so sits behind the arming
	// gate ([Config.Armed]). The withdrawal route is a capture route that is not one: it only
	// deletes (`withdraw.go`).
	upload bool
	handle func(w *Worker, rw http.ResponseWriter, r *http.Request, row TokenRow)
}

// routes is THE dispatch table; [Routes] derives the ledger from it and [New] the mux, so the
// two cannot disagree. Every CAPTURE route is `POST` (every one changes state); the plugin API's
// reads are `GET` and its one write is `POST` (`plugin.go`).
var routes = []route{
	{http.MethodPost, RecordsPattern, KindCapture, true, (*Worker).handleRecords},
	{http.MethodPost, BlobsPattern, KindCapture, true, (*Worker).handleBlob},
	{http.MethodPost, WithdrawPattern, KindCapture, false, (*Worker).handleWithdraw},
	{http.MethodGet, PendingPattern, KindPlugin, false, (*Worker).handlePending},
	{http.MethodGet, TranscriptPattern, KindPlugin, false, (*Worker).handleTranscript},
	{http.MethodGet, OutputsPattern, KindPlugin, false, (*Worker).handleOutputsRead},
	{http.MethodPost, OutputsPattern, KindPlugin, false, (*Worker).handleOutputsWrite},
}

// Routes is every `<METHOD> <pattern> <token kind>` this listener dispatches, sorted. Adding a row
// is adding an endpoint a host-side token can reach; `TestTheWorkerRouteLedgerIsExactlyThis` is
// where somebody has to write it down.
func Routes() []string {
	out := make([]string, 0, len(routes))
	for _, rt := range routes {
		out = append(out, rt.method+" "+rt.pattern+" "+string(rt.kind))
	}
	slices.Sort(out)
	return out
}

// Config is everything the listener needs.
type Config struct {
	// TokenFile is RE-READ on every request, so deleting a row revokes it with no restart.
	TokenFile string
	// Owner is the instance's SOLE capture owner (Q7).
	Owner Owner
	// OwnerHeld answers whether the authority STILL holds [Config.Owner] as a principal. It is
	// asked on EVERY request (AP3): startup checks it once, and a principal the authority stops
	// holding afterwards must stop being able to upload without a restart. Required.
	OwnerHeld func() bool
	Archive   *archive.Archive
	// Armed is the operator's explicit arming (`-arm-transcript-capture`). FALSE is every
	// instance today (O16): every upload is refused 503 and nothing is stored.
	Armed          bool
	Limiter        *netid.RateLimiter
	TrustedProxies []netip.Prefix
	// MaxBody bounds one request body before decoding; 0 is [DefaultMaxBody].
	MaxBody int64
	// Log receives operator lines — never a token, never content.
	Log func(string)
	// Plugins is the plugin registry, toggles and outputs (slice S5); nil is "no -plugin-registry",
	// under which no plugin row is admitted: one present at startup refuses the whole file (as any
	// refused row does), and one appearing afterwards authenticates nothing — the uniform 401.
	Plugins *plugins.Service
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
	if cfg.TokenFile == "" || cfg.Archive == nil || cfg.Limiter == nil || !cfg.Owner.Kind.Valid() || cfg.Owner.ID == "" ||
		cfg.OwnerHeld == nil {
		return nil, nil, ErrMisconfigured
	}
	rows, err := LoadTokens(cfg.TokenFile, cfg.wall())
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
		w.mux.HandleFunc(rt.method+" "+rt.pattern, func(rw http.ResponseWriter, r *http.Request) {
			w.serve(rw, r, rt)
		})
	}
	return w, rows, nil
}

// wall is the token file's wall for this configuration.
func (c Config) wall() Wall {
	w := Wall{Owner: c.Owner}
	if c.Plugins != nil && c.Plugins.Registry != nil {
		w.Plugins = c.Plugins.Registry.Holds
	}
	return w
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
		if err := admit(row, w.cfg.wall()); err != nil {
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
	// 🔴 THE CAPTURE GATES BELOW ARE CAPTURE'S, AND A PLUGIN ROUTE NEVER REACHES THEM: a plugin token
	// uploads nothing, so neither the sole capture owner nor the arming flag says anything about it.
	// What a plugin may do is its manifest and its toggles, decided in `plugin.go`.
	if rt.kind == KindPlugin {
		rt.handle(w, rw, r, row)
		return
	}
	// 🔴 THE OWNER IS RE-ASKED PER REQUEST (AP3). The token rows bind to `-worker-owner`, and that
	// principal was checked against the authority only at startup; a principal removed from the
	// authority afterwards kept uploading until a restart. Refused as an unknown token is — the
	// uniform 401 — but NOT counted toward the lockout: the caller guessed nothing.
	if !w.cfg.OwnerHeld() {
		w.once(fmt.Sprintf("worker: the sole capture owner %s is no longer a principal the authority holds; "+
			"every upload is refused 401", w.cfg.Owner))
		refuseUnauthorized(rw)
		return
	}
	// 🔴 THE DISARM GATE: after authentication, so an unauthenticated caller learns nothing about
	// the instance's state and the lockout still counts its guesses; before the body is read, so a
	// disarmed instance stores, creates and stages NOTHING.
	//
	// 🔴 UPLOAD ROUTES ONLY. The withdrawal route stores nothing — it deletes the caller's own root —
	// so an instance that was armed, holds transcripts, and was then disarmed still honours a
	// withdrawal: refusing it 503 would keep a prefix the routing rule says must go for as long as
	// the instance stayed disarmed.
	if rt.upload && !w.cfg.Armed {
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
		large   *archive.TooLargeError
		inv     *archive.InvalidError
	)
	switch {
	case err == nil && st.Pending:
		writeJSON(rw, http.StatusAccepted, fmt.Sprintf(`{"frame":%d,"of":%d}`+"\n", st.FrameIndex, st.FrameCount))
	case err == nil && records:
		writeJSON(rw, http.StatusOK, fmt.Sprintf(`{"stored_to":%s,"seq_to":%d}`+"\n", quote(st.StoredTo), st.SeqTo))
	case err == nil:
		writeJSON(rw, http.StatusOK, fmt.Sprintf(`{"stored_to":%s}`+"\n", quote(st.StoredTo)))
	case errors.Is(err, archive.ErrDeleted):
		// 🔴 410, AND ONLY TO THE DELETED ROOT'S OWN (owner, host) (`archive.gone`): the session's
		// transcript was deleted here — by its owner, a withdrawal or retention — and the agent must
		// stop sending it for good. Anybody else asking about that id got the uniform 409 below.
		writeJSON(rw, http.StatusGone, DeletedBody)
	case errors.Is(err, archive.ErrNotOwner):
		writeJSON(rw, http.StatusConflict, NotOwnerBody)
	case errors.As(err, &stale):
		writeJSON(rw, http.StatusConflict, fmt.Sprintf(`{"stored_to":%s}`+"\n", quote(stale.StoredTo)))
	case errors.As(err, &recheck):
		// The index, the blob name or the FIELD — never the value. A field hit is checked first:
		// there the name IS the value.
		if recheck.Field != "" {
			writeJSON(rw, http.StatusUnprocessableEntity, fmt.Sprintf(`{"refused":"recheck","field":%s}`+"\n", quote(recheck.Field)))
		} else if recheck.Blob != "" {
			writeJSON(rw, http.StatusUnprocessableEntity, fmt.Sprintf(`{"refused":"recheck","blob":%s}`+"\n", quote(recheck.Blob)))
		} else {
			writeJSON(rw, http.StatusUnprocessableEntity, fmt.Sprintf(`{"refused":"recheck","record":%d}`+"\n", recheck.Record))
		}
		w.cfg.Log("worker: refused an upload the redaction table matched (" + recheck.Error() + ")")
	case errors.As(err, &quota):
		writeJSON(rw, http.StatusInsufficientStorage, fmt.Sprintf(`{"refused":"quota","quota_bytes":%d}`+"\n", quota.Quota))
		w.once("worker: the INSTANCE transcript quota is reached; uploads are refused 507 until retention frees space")
	case errors.As(err, &large):
		// AP2: one record or blob over the bound — refused before its staged bytes were read.
		writeJSON(rw, http.StatusRequestEntityTooLarge, fmt.Sprintf(`{"refused":"too-large","max_bytes":%d}`+"\n", large.Limit))
	case errors.As(err, &inv):
		badRequest(rw, inv)
	default:
		w.cfg.Log("worker: an upload failed: " + err.Error())
		rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
		rw.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(rw, "internal error\n")
	}
}
