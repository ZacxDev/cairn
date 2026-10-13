// Package clientapi is scope mail's CLIENT LISTENER: the four memo routes a host-side agent
// reaches with its own narrowed journal credential (S2 of `claudedocs/plan-cairn-scope-mail.md`,
// decisions 3, 8, 10, 12 and 19; threats T3, T9 and T12).
//
// 🔴 IT IS A THIRD LISTENER ON `cmd/cairn-ui`, NEVER A ROUTE ON THE POD AND NEVER A ROW OF THE
// BROWSER SURFACE. The memo store is `internal/pgstore`'s, which only `cairn-ui` opens, so the pod
// does not move: `api.DeclaredRoutes()` and the conformance corpus are untouched (the pod's
// `-routes` output is byte-identical across this slice). And a send is a bearer POST from a CLI,
// which carries no `Origin` and could not pass the browser surface's method-derived cross-site
// gates — and must not be made to. So the routes live here, behind their own ledger,
// [ClientRoutes].
//
// 🔴 IT AUTHENTICATES THE MACHINE-TOKEN BACKEND AND NOTHING ELSE — BY TYPE, NOT BY WIRING.
// [Config.Auth] is a `*identity.MachineToken`, not an `identity.Authenticator`, so a cookie
// session or a provider JWT cannot be handed to this listener without changing this package:
// a browser that is signed in to the surface next door presents a cookie here and is refused
// exactly like a garbage token. The token is matched by the ONE `control.Authenticate`, over the
// SAME control-journal authority the browser surface reads (decision 3) — `cmd/cairn-ui` refuses
// to start this listener without a journal, because without one that authority is the token
// file's projection, whose shared rows would make every sender the same sender (O9 reversed O5).
//
// 🔴 EVERY AUTHORISATION DECISION IS THE STORE'S. `pgstore.MemoStore` asks `memo.MaySend`,
// `memo.MayRead` (now, on every read — decision 12) and `memo.MayRetract` itself; this package
// parses, dispatches and renders, and asks no predicate of its own, so it cannot become a second
// place to get one wrong.
//
// 🔴 THE WIRE CARRIES `memo.Rendered`, NEVER `memo.Stored` (decision 19, Guard C): every memo in a
// response is `memo.Render` of what the store returned, so each text field has already had
// `memo.Unsafe`'s replacement applied. The JSON keys are `memo.Rendered`'s own field names on
// purpose: the type IS the wire format, so there is no copy step between the renderer and the
// encoder that could drop a field's sanitising.
//
// 🔴 AND THREE READ ROUTES THAT ARE NOT MEMOS: the transcripts plan's agent read API (S8, its decision
// 17) — a stored session's skeleton, a page of its records, one tool call — under
// [transcriptwire.ClientPathPrefix]. Scope mail's Q3 put agent reads on THIS listener so an agent holds
// one `CAIRN_UI_URL` and one credential; the browser surface serves the same three answers. Both serve
// them through `transcript.Store.Answer` and nothing else, so this package asks no transcript
// predicate of its own either: the answer is decision 4's (`transcript.Store.View`) over the request's
// own — narrowed — authority, and every miss is `transcriptwire.UnseenBody`, which is [NotFoundBody]
// byte for byte. With no transcript store configured every named session is that miss.
//
// 🔴 ONE ANSWER FOR EVERY MISS (decision 4, T12). A scope the caller cannot read, a scope that
// does not exist, an id that does not exist, an id asked under a scope it does not belong to and
// an expired memo are all `404` with [NotFoundBody], byte for byte. A send to a scope the caller
// cannot write — absent and unreadable included — is the `no-write` refusal, also one body.
package clientapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/memowire"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/pgstore"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/transcriptwire"
)

// The four routes' paths (decision 8), re-exported from `internal/memowire`, which is where the
// wire is spelled ONCE for the listener and the client (S3). `MemosPath` carries two methods: GET
// lists, POST sends.
const (
	MemosPath   = memowire.MemosPath
	MemoPath    = memowire.MemoPath
	RetractPath = memowire.RetractPath
)

// MaxRequestBody bounds a POST body. A memo is at most `memo.MaxBodyBytes` + `memo.MaxSubjectBytes`
// after normalisation; JSON can spell one byte as six (`\u00XX`), so the bound is generous on
// purpose and still finite — a body past it is refused before it is decoded.
const MaxRequestBody = 64 << 10

// The response bodies that must be byte-identical wherever they are given.
const (
	// UnauthorizedBody is the ONE refusal for every authentication failure: no credential, an
	// unknown or revoked token, a cookie, a provider JWT, a locked-out client and a client with no
	// resolvable identity all read the same.
	UnauthorizedBody = `{"error":"unauthorized"}` + "\n"
	// NotFoundBody is the ONE miss (T12).
	NotFoundBody = `{"error":"` + memowire.ErrorNotFound + `"}` + "\n"
)

// Store is what the listener needs of the memo store; `*pgstore.MemoStore` is the production one.
//
// 🔴 AN INTERFACE ONLY SO THE LISTENER'S OWN GUARDS — authentication, lockout, the route ledger,
// what reaches the wire — can be measured without a database. Every AUTHORISATION guard lives in
// the store, and is measured against the real one in the Postgres tier
// (`cmd/cairn-ui/clientapi_pgtest_test.go`).
type Store interface {
	Send(ctx context.Context, caller control.Principal, auth control.Authorization,
		scope, subject, body string, ttl time.Duration) (int64, time.Time, error)
	List(ctx context.Context, m control.Model, auth control.Authorization, q pgstore.MemoQuery) ([]memo.Stored, error)
	Get(ctx context.Context, m control.Model, auth control.Authorization, scope string, id int64) (memo.Stored, error)
	Retract(ctx context.Context, caller control.Principal, auth control.Authorization, scope string, id int64) error
}

var _ Store = (*pgstore.MemoStore)(nil)

// Config is everything the listener needs.
type Config struct {
	// Auth is the machine-token backend over the control-journal authority — the ONLY backend,
	// and a concrete type so no other can be passed (package doc).
	Auth *identity.MachineToken
	// Model is the same authority's model, read at request time to resolve a sender's display and
	// credential label (decision 5). `(*control.Cache).Model` in production.
	Model func() control.Model
	Memos Store
	// Transcripts is the transcript archive's read side for the agent read routes, NIL on a deployment
	// with no `-transcript-dir` — every named session is then the one miss.
	Transcripts *transcript.Store
	// Limiter is the failed-token lockout; TrustedProxies is what makes the client-IP header
	// readable. The caller has already refused a reachable bind with no allowlist, so the lockout
	// keys on the real client and not on a proxy (`netid.ResolveClient`).
	Limiter        *netid.RateLimiter
	TrustedProxies []netip.Prefix
	// Log receives operator lines. Never a token, never memo text.
	Log func(string)
}

// ErrMisconfigured is returned by [New] for a config missing a required part.
var ErrMisconfigured = errors.New("clientapi: incomplete configuration")

// Handler is the client listener.
type Handler struct {
	cfg Config
}

// New builds the handler, refusing an incomplete config: a nil part would panic on the first
// request — a listener that came up healthy and crashed when used.
func New(cfg Config) (*Handler, error) {
	if cfg.Auth == nil || cfg.Model == nil || cfg.Memos == nil || cfg.Limiter == nil {
		return nil, ErrMisconfigured
	}
	if cfg.Log == nil {
		cfg.Log = func(string) {}
	}
	return &Handler{cfg: cfg}, nil
}

type routeKey struct{ method, path string }

// routes is THE dispatch table, and [ClientRoutes] derives the ledger from it, so the ledger and
// the dispatcher cannot disagree.
var routes = map[routeKey]func(*Handler, http.ResponseWriter, *http.Request, identity.Identity){
	{http.MethodGet, MemosPath}:    (*Handler).list,
	{http.MethodPost, MemosPath}:   (*Handler).send,
	{http.MethodGet, MemoPath}:     (*Handler).get,
	{http.MethodPost, RetractPath}: (*Handler).retract,
	// The agent read API (transcripts S8): GET only, every operand in a query parameter.
	{http.MethodGet, transcriptwire.ClientPathPrefix + transcriptwire.RouteSkeleton}: (*Handler).transcriptSkeleton,
	{http.MethodGet, transcriptwire.ClientPathPrefix + transcriptwire.RouteRecords}:  (*Handler).transcriptRecords,
	{http.MethodGet, transcriptwire.ClientPathPrefix + transcriptwire.RouteTool}:     (*Handler).transcriptTool,
}

// ClientRoutes is every `<METHOD> <path>` the client listener dispatches, sorted.
// `TestTheClientRouteLedgerMatchesTheDispatchTable` compares it with a hand-written list: adding a
// row here is adding an endpoint an agent credential can reach, and somebody has to write that
// down. ⚠ It is NOT `api.DeclaredRoutes()` and nothing about it moves the pod's ledger.
func ClientRoutes() []string {
	out := make([]string, 0, len(routes))
	for k := range routes {
		out = append(out, k.method+" "+k.path)
	}
	slices.Sort(out)
	return out
}

// allowed lists the methods dispatched for `path`, or nil for a path that is not a route.
func allowed(path string) []string {
	var out []string
	for k := range routes {
		if k.path == path {
			out = append(out, k.method)
		}
	}
	slices.Sort(out)
	return out
}

// ServeHTTP dispatches: route, method, client identity, lockout, authentication, then the handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	methods := allowed(r.URL.Path)
	if methods == nil {
		http.NotFound(w, r)
		return
	}
	handle, ok := routes[routeKey{r.Method, r.URL.Path}]
	if !ok {
		w.Header().Set("Allow", strings.Join(methods, ", "))
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	client, _, ok := netid.ResolveClient(r.Header, r.RemoteAddr, h.cfg.TrustedProxies)
	if !ok {
		h.cfg.Log(fmt.Sprintf("client-api: refused: no client identity from peer %q", r.RemoteAddr))
		refuse(w)
		return
	}
	if h.cfg.Limiter.LockedOut(client) {
		h.cfg.Log("client-api: refused: " + client + " is locked out")
		refuse(w)
		return
	}
	id, err := h.cfg.Auth.Authenticate(r)
	if err != nil || !id.Valid() {
		if h.cfg.Limiter.RecordFailure(client) {
			h.cfg.Log("client-api: refused: bad credential from " + client + " — LOCKOUT TRIGGERED")
		} else {
			h.cfg.Log("client-api: refused: bad credential from " + client)
		}
		refuse(w)
		return
	}
	handle(h, w, r, id)
}

func refuse(w http.ResponseWriter) {
	writeRaw(w, http.StatusUnauthorized, UnauthorizedBody)
}

func notFound(w http.ResponseWriter) {
	writeRaw(w, http.StatusNotFound, NotFoundBody)
}

func writeRaw(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// badRequest names what was wrong with the REQUEST's shape. It never echoes a value the caller
// sent, so it can carry nothing of a memo.
func badRequest(w http.ResponseWriter, what string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request", "detail": what})
}

// RefusalBody is a send or retraction the store's rules refused (exit 6 at the client, decision 8):
// the reason names a LIMIT, and `rule` a redaction RULE NAME for a secret — never content.
type RefusalBody = memowire.ErrorBody

// storeError answers an error from the store: the uniform miss, a refusal, or — for anything else
// — a 500 whose body says nothing and whose log line says what failed.
func (h *Handler) storeError(w http.ResponseWriter, route string, err error) {
	var ref *memo.Refusal
	switch {
	case errors.Is(err, memo.ErrNotFound):
		notFound(w)
	case errors.As(err, &ref):
		writeJSON(w, http.StatusForbidden, RefusalBody{Error: memowire.ErrorRefused, Reason: string(ref.Reason), Rule: ref.Rule})
	default:
		h.cfg.Log("client-api: " + route + " failed: " + err.Error())
		writeRaw(w, http.StatusInternalServerError, `{"error":"internal"}`+"\n")
	}
}

// ListBody is `GET /client/v1/memos`'s answer: the scope's memos, oldest first, RENDERED.
type ListBody = memowire.ListBody

// MemoBody is `GET /client/v1/memo`'s answer: one memo, RENDERED.
type MemoBody = memowire.MemoBody

// list answers `GET /client/v1/memos?scope=&after=&limit=&tombstones=`.
//
//   - `scope` is required: the scope's NAME (decision 4).
//   - `after` (RFC 3339) returns only memos created STRICTLY after it; the client passes its
//     high-water mark minus `memo.CursorMargin` (decision 6). Absent means from the start.
//   - `limit` is 1..`memo.MaxLivePerScope`; absent means the cap.
//   - `tombstones=1` also returns unexpired RETRACTED memos as tombstones — what `memo-read`'s
//     listing shows (decision 10); without it, as for the check, a retracted memo is never returned.
//     ⚠ A PARAMETER THE PLAN'S ROUTE TABLE DOES NOT NAME: S1 left "how the listener exposes" the
//     store's `IncludeRetracted` to S2 (plan, S2 corrections).
func (h *Handler) list(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	q := r.URL.Query()
	scope := q.Get("scope")
	if scope == "" {
		badRequest(w, "scope is required")
		return
	}
	query := pgstore.MemoQuery{Scope: scope}
	if raw := q.Get("after"); raw != "" {
		after, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			badRequest(w, "after must be an RFC 3339 time")
			return
		}
		query.After = after
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > memo.MaxLivePerScope {
			badRequest(w, fmt.Sprintf("limit must be an integer from 1 to %d", memo.MaxLivePerScope))
			return
		}
		query.Limit = n
	}
	switch q.Get("tombstones") {
	case "", "0":
	case "1":
		query.IncludeRetracted = true
	default:
		badRequest(w, "tombstones must be 0 or 1")
		return
	}
	stored, err := h.cfg.Memos.List(r.Context(), h.cfg.Model(), id.Auth, query)
	if err != nil {
		h.storeError(w, "list", err)
		return
	}
	out := ListBody{Memos: make([]memo.Rendered, 0, len(stored))}
	for _, st := range stored {
		out.Memos = append(out.Memos, memo.Render(st))
	}
	writeJSON(w, http.StatusOK, out)
}

// get answers `GET /client/v1/memo?scope=&id=` — the memo only if it belongs to THAT scope
// (decision 8, round 1 🔴3); the store makes the match part of the answer.
func (h *Handler) get(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	q := r.URL.Query()
	scope, memoID, ok := scopeAndID(w, q.Get("scope"), q.Get("id"))
	if !ok {
		return
	}
	st, err := h.cfg.Memos.Get(r.Context(), h.cfg.Model(), id.Auth, scope, memoID)
	if err != nil {
		h.storeError(w, "get", err)
		return
	}
	writeJSON(w, http.StatusOK, MemoBody{Memo: memo.Render(st)})
}

func scopeAndID(w http.ResponseWriter, scope, rawID string) (string, int64, bool) {
	if scope == "" {
		badRequest(w, "scope is required")
		return "", 0, false
	}
	n, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || n < 1 {
		badRequest(w, "id must be a positive integer")
		return "", 0, false
	}
	return scope, n, true
}

// SendRequest is `POST /client/v1/memos`'s body. `ttl_seconds` 0 means the default TTL.
type SendRequest = memowire.SendRequest

// SentBody is a send's answer: the new memo's id and its server-stamped creation time.
type SentBody = memowire.SentBody

// RetractRequest is `POST /client/v1/memo/retract`'s body: the scope the caller routed by, and the id.
type RetractRequest = memowire.RetractRequest

// RetractedBody is a retraction's answer.
type RetractedBody = memowire.RetractedBody

// decode reads one JSON object into `into`, bounded, refusing unknown fields and ANY trailing data.
//
// 🔴 END OF INPUT IS REQUIRED, NOT `!dec.More()`. `More` answers "is there another element in the
// CURRENT array or object", and at the top level it reports false before a `]` or `}` — so `{…}}`
// passed a `More` check while its doc said trailing data was refused (S2 round 1). Asking for the
// next token and requiring `io.EOF` refuses every trailing byte that is not whitespace.
func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxRequestBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		badRequest(w, "the body must be one JSON object of the documented shape, at most "+
			strconv.Itoa(MaxRequestBody)+" bytes")
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		badRequest(w, "the body must be ONE JSON object and nothing after it")
		return false
	}
	return true
}

// send answers `POST /client/v1/memos`. The caller's principal and credential come from the
// authentication, never from the body (T4): the body names only the scope and the content.
func (h *Handler) send(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	var req SendRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Scope == "" {
		badRequest(w, "scope is required")
		return
	}
	memoID, created, err := h.cfg.Memos.Send(r.Context(), id.Principal, id.Auth, req.Scope, req.Subject, req.Body,
		memo.TTLOf(req.TTLSeconds, time.Second))
	if err != nil {
		h.storeError(w, "send", err)
		return
	}
	writeJSON(w, http.StatusCreated, SentBody{ID: memoID, CreatedAt: created.UTC().Format(time.RFC3339Nano)})
}

// retract answers `POST /client/v1/memo/retract` — scope AND id, for the same reason `get` takes
// both. Who may retract is `memo.MayRetract`, asked by the store.
func (h *Handler) retract(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	var req RetractRequest
	if !decode(w, r, &req) {
		return
	}
	scope, memoID, ok := scopeAndID(w, req.Scope, strconv.FormatInt(req.ID, 10))
	if !ok {
		return
	}
	if err := h.cfg.Memos.Retract(r.Context(), id.Principal, id.Auth, scope, memoID); err != nil {
		h.storeError(w, "retract", err)
		return
	}
	writeJSON(w, http.StatusOK, RetractedBody{Retracted: memoID})
}

func (h *Handler) transcriptSkeleton(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	h.transcriptRead(w, r, id, transcriptwire.RouteSkeleton)
}

func (h *Handler) transcriptRecords(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	h.transcriptRead(w, r, id, transcriptwire.RouteRecords)
}

func (h *Handler) transcriptTool(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	h.transcriptRead(w, r, id, transcriptwire.RouteTool)
}

// transcriptRead answers one agent read route through `transcript.Store.Answer` — the browser
// surface's one function — with the request's own authority. The body is a transcript: no cache keeps
// it.
func (h *Handler) transcriptRead(w http.ResponseWriter, r *http.Request, id identity.Identity, route string) {
	status, body, err := h.cfg.Transcripts.Answer(transcript.Viewer{Principal: id.Principal, Auth: id.Auth}, route, r.URL.Query())
	if err != nil {
		h.cfg.Log("client-api: transcript " + route + " failed: " + err.Error())
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
