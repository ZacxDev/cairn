package presence

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"sync"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/netid"
)

// The two agent routes — the WHOLE surface of the second listener (decision 4, the contract).
const (
	PushPath  = "/agent/v1/presence"
	ClaimPath = "/agent/v1/rings/claim"
	// StatusHeader carries the push outcome's word, as the pod's write routes do.
	StatusHeader = "X-Presence-Status"
	// StatusReplaced is the one success word a push answers.
	StatusReplaced = "presence-replaced"
)

// unauthorizedBody is the ONE refusal for every authentication failure on this listener: an
// unknown token, a revoked one, a token of the other kind, a row the wall refused, a locked-out
// client and a client with no resolvable identity all read the same.
const unauthorizedBody = "unauthorized\n"

type agentRoute struct {
	kind   TokenKind
	handle func(a *Agent, w http.ResponseWriter, r *http.Request, row TokenRow)
}

// agentRoutes is THE dispatch table. [AgentRoutes] derives the ledger from it, so the ledger
// and the dispatcher cannot disagree; every route is `POST` because both CHANGE state (a claim
// removes the rings it returns).
var agentRoutes = map[string]agentRoute{
	PushPath:  {kind: KindPush, handle: (*Agent).handlePush},
	ClaimPath: {kind: KindClaim, handle: (*Agent).handleClaim},
}

// AgentRoutes is every `<METHOD> <path> <token kind>` the agent listener dispatches, sorted.
// `TestTheAgentRouteLedgerIsExactlyTwoRows` compares it to a hand-written ledger: adding a row
// here is adding an endpoint a host-side token can reach, and somebody has to write that down.
func AgentRoutes() []string {
	out := make([]string, 0, len(agentRoutes))
	for path, rt := range agentRoutes {
		out = append(out, http.MethodPost+" "+path+" "+string(rt.kind))
	}
	slices.Sort(out)
	return out
}

// AgentConfig is everything the agent listener needs.
type AgentConfig struct {
	// TokenFile is RE-READ on every request, so deleting a row revokes it with no restart.
	TokenFile string
	// Owner is the instance's SOLE presence owner (decision 15).
	Owner   Owner
	Service *Service
	// Limiter is the failed-token lockout; TrustedProxies is what makes the client-IP header
	// readable (see `netid.ResolveClient`). The caller has already refused a reachable bind with
	// no allowlist, so the lockout keys on the real client.
	Limiter        *netid.RateLimiter
	TrustedProxies []netip.Prefix
	// Log receives operator lines. Never a token; a digest only as its 12-hex prefix.
	Log func(string)
}

// Agent is the second listener's handler.
//
// 🔴 IT READS NO COOKIE AND SETS NONE. A host-side agent is not a browser, and the browser
// surface's cross-site gates are derived from the METHOD — a non-browser POST carries no
// `Origin` and could not pass them, and must not be made to. So the agent routes live here,
// behind their own ledger, authenticated by presence tokens alone.
type Agent struct {
	cfg AgentConfig

	mu        sync.Mutex
	announced map[string]bool
}

// ErrAgentMisconfigured is returned by [NewAgent] for a config missing a required part.
var ErrAgentMisconfigured = errors.New("presence agent: incomplete configuration")

// NewAgent builds the handler and performs the STARTUP read of the token file: a malformed row,
// a duplicate digest or a row for another owner refuses the listener (decision 15).
func NewAgent(cfg AgentConfig) (*Agent, []TokenRow, error) {
	if cfg.TokenFile == "" || cfg.Service == nil || cfg.Service.Store == nil || cfg.Service.Queue == nil ||
		cfg.Limiter == nil || !cfg.Owner.Kind.Valid() || cfg.Owner.ID == "" {
		return nil, nil, ErrAgentMisconfigured
	}
	rows, err := LoadTokens(cfg.TokenFile, cfg.Owner)
	if err != nil {
		return nil, nil, err
	}
	if cfg.Log == nil {
		cfg.Log = func(string) {}
	}
	return &Agent{cfg: cfg, announced: map[string]bool{}}, rows, nil
}

// once logs `line` the first time it is seen, so a standing refused row read on every request
// (a claim loop asks every ~5 s) is one line rather than thousands a day.
func (a *Agent) once(line string) {
	a.mu.Lock()
	said := a.announced[line]
	a.announced[line] = true
	a.mu.Unlock()
	if !said {
		a.cfg.Log(line)
	}
}

// authenticate re-reads the token file and matches `presented` against every admitted row of
// `kind`. It returns false for everything else, uniformly.
//
// 🔴 NO EARLY EXIT, for `control.Authenticate`'s reason: the loop runs over every row whether
// or not it has matched, so the response time does not say which row a token belongs to.
func (a *Agent) authenticate(presented string, kind TokenKind) (TokenRow, bool) {
	if presented == "" {
		return TokenRow{}, false
	}
	data, err := os.ReadFile(a.cfg.TokenFile)
	if err != nil {
		// FAIL CLOSED: a token file that vanished authenticates nobody.
		a.once(fmt.Sprintf("presence: the token file %s cannot be read (%v); every agent request is refused until it can",
			a.cfg.TokenFile, err))
		return TokenRow{}, false
	}
	rows, problems := parseTokens(data)
	for _, p := range problems {
		a.once("presence: token file row refused: " + p.Error())
	}
	digest := control.HashToken(presented)
	var matched *TokenRow
	for _, row := range rows {
		if err := admit(row, a.cfg.Owner); err != nil {
			a.once(fmt.Sprintf("presence: token file line %d (digest %s…) refused: %v", row.Line, row.DigestPrefix(), err))
			continue
		}
		hit := control.EqualHash(digest, row.Digest)
		if hit && row.Kind == kind {
			found := row
			matched = &found
		}
	}
	if matched == nil {
		return TokenRow{}, false
	}
	return *matched, true
}

func (a *Agent) refuse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, unauthorizedBody)
}

func badRequest(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = io.WriteString(w, err.Error()+"\n")
}

// ServeHTTP dispatches the two agent routes.
func (a *Agent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt, ok := agentRoutes[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	client, _, ok := netid.ResolveClient(r.Header, r.RemoteAddr, a.cfg.TrustedProxies)
	if !ok {
		a.cfg.Log(fmt.Sprintf("presence: refused: no client identity from peer %q", r.RemoteAddr))
		a.refuse(w)
		return
	}
	if a.cfg.Limiter.LockedOut(client) {
		a.cfg.Log("presence: refused: " + client + " is locked out")
		a.refuse(w)
		return
	}
	row, ok := a.authenticate(authz.PresentedToken(r.Header.Get("Authorization")), rt.kind)
	if !ok {
		if a.cfg.Limiter.RecordFailure(client) {
			a.cfg.Log("presence: refused: bad token from " + client + " — LOCKOUT TRIGGERED")
		} else {
			a.cfg.Log("presence: refused: bad token from " + client)
		}
		a.refuse(w)
		return
	}
	rt.handle(a, w, r, row)
}

func (a *Agent) handlePush(w http.ResponseWriter, r *http.Request, row TokenRow) {
	push, err := DecodePush(http.MaxBytesReader(w, r.Body, MaxPushBody))
	if err != nil {
		badRequest(w, err)
		return
	}
	// 🔴 THE HOST COMES FROM THE TOKEN ROW, AND A BODY NAMING ANOTHER IS REFUSED BEFORE ANY WRITE
	// (decision 6) — which is what catches a token file copied to the wrong machine.
	if push.Host != row.Host {
		badRequest(w, fmt.Errorf("host %q is not this token's host", push.Host))
		return
	}
	a.cfg.Service.Store.Replace(row.Owner, row.Host, push.Rows)
	w.Header().Set(StatusHeader, StatusReplaced)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "rows="+strconv.Itoa(len(push.Rows)))
}

func (a *Agent) handleClaim(w http.ResponseWriter, r *http.Request, row TokenRow) {
	if err := DecodeClaim(http.MaxBytesReader(w, r.Body, MaxClaimBody)); err != nil {
		badRequest(w, err)
		return
	}
	rings := a.cfg.Service.Queue.Claim(row.Owner, row.Host)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(EncodeClaim(rings))
}
