package presence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/netid"
)

// Presence tokens for the agent tests. Synthetic and pairwise distinct; none has ever
// authorised anything.
const (
	pushA   = "fixture-presence-push-token-owner-a-host-a-not-real"
	claimA  = "fixture-presence-claim-token-owner-a-host-a-not-real"
	claimAB = "fixture-presence-claim-token-owner-a-host-b-not-real"
	pushAB  = "fixture-presence-push-token-owner-a-host-b-not-real"
	pushB   = "fixture-presence-push-token-owner-b-host-a-not-real"
	claimB  = "fixture-presence-claim-token-owner-b-host-a-not-real"
)

type agentRig struct {
	t      *testing.T
	agent  *Agent
	svc    *Service
	clk    *fakeClock
	tokens string
	mu     sync.Mutex
	log    []string
}

func (g *agentRig) logged() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return strings.Join(g.log, "\n")
}

func writeTokens(t *testing.T, path string, rows ...TokenRow) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# synthetic presence tokens\n")
	for _, r := range rows {
		b.WriteString(r.String() + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendTokens(t *testing.T, path string, rows ...TokenRow) {
	t.Helper()
	for _, r := range rows {
		if err := AppendTokenRow(path, r); err != nil {
			t.Fatal(err)
		}
	}
}

// standardRows is owner A's four tokens: push and claim on host-a, push and claim on host-b.
func standardRows() []TokenRow {
	return []TokenRow{
		NewTokenRow(KindPush, ownerA, "host-a", pushA),
		NewTokenRow(KindClaim, ownerA, "host-a", claimA),
		NewTokenRow(KindPush, ownerA, "host-b", pushAB),
		NewTokenRow(KindClaim, ownerA, "host-b", claimAB),
	}
}

func newRig(t *testing.T, limiter *netid.RateLimiter, rows ...TokenRow) *agentRig {
	t.Helper()
	if limiter == nil {
		limiter = netid.NewRateLimiter(1000, time.Minute, time.Minute)
	}
	g := &agentRig{t: t, clk: &fakeClock{now: clock0}, tokens: filepath.Join(t.TempDir(), "presence-tokens")}
	writeTokens(t, g.tokens, rows...)
	g.svc = &Service{Store: &Store{Now: g.clk.Now}, Queue: &Queue{Now: g.clk.Now}}
	a, _, err := NewAgent(AgentConfig{TokenFile: g.tokens, Owner: ownerA, Service: g.svc, Limiter: limiter,
		Log: func(s string) { g.mu.Lock(); g.log = append(g.log, s); g.mu.Unlock() }})
	if err != nil {
		t.Fatalf("the agent did not build: %v", err)
	}
	g.agent = a
	return g
}

// do sends one agent request from `peer` (a loopback-free documentation address, so the
// limiter keys on it directly: no trusted proxies are configured here).
func (g *agentRig) do(method, path, token, body, peer string) *httptest.ResponseRecorder {
	g.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = peer
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	g.agent.ServeHTTP(rec, r)
	return rec
}

const peer1 = "198.51.100.7:4000"

func pushBody(host string, rows ...string) string {
	return `{"schema":1,"host":"` + host + `","rows":[` + strings.Join(rows, ",") + `]}`
}

func wireRowJSON(session string) string {
	return `{"session":"` + session + `","runtime":"claude","target":"notes:3","label":"notes",` +
		`"hotkey":"Alt+n","last_activity":"2000-01-02T03:04:05Z"}`
}

func (g *agentRig) push(token, host string, sessions ...string) *httptest.ResponseRecorder {
	g.t.Helper()
	var rows []string
	for _, s := range sessions {
		rows = append(rows, wireRowJSON(s))
	}
	return g.do(http.MethodPost, PushPath, token, pushBody(host, rows...), peer1)
}

func (g *agentRig) claim(token string) (*httptest.ResponseRecorder, []string) {
	g.t.Helper()
	rec := g.do(http.MethodPost, ClaimPath, token, `{}`, peer1)
	if rec.Code != http.StatusOK {
		return rec, nil
	}
	var out struct {
		Schema int `json:"schema"`
		Rings  []struct {
			RingID  string `json:"ring_id"`
			Session string `json:"session"`
		} `json:"rings"`
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil || out.Schema != 1 || out.Rings == nil {
		g.t.Fatalf("claim response is not the contract's shape (%v): %s", err, rec.Body.String())
	}
	var sessions []string
	for _, r := range out.Rings {
		sessions = append(sessions, r.Session)
	}
	return rec, sessions
}

// sees asks the store as owner A's un-narrowed identity, built without a world: the agent
// tests do not exercise narrowing — the predicate tests do, through the real backends.
func (g *agentRig) sees(session string) (Presence, bool) {
	return g.svc.Store.For(identity.Identity{
		Principal: control.Principal{Kind: ownerA.Kind, ID: ownerA.ID, Display: "owner-a"}}, session)
}

// TestTheAgentRouteLedgerIsExactlyTwoRows is the second listener's route ledger. A third row
// is a new endpoint a host-side token can reach; this hand-written list is where somebody has
// to say so. It also proves every declared row is DISPATCHED (a POST with no token is the
// uniform 401, not the no-route 404) and that a path in no row is a 404.
func TestTheAgentRouteLedgerIsExactlyTwoRows(t *testing.T) {
	want := []string{
		"POST /agent/v1/presence push",
		"POST /agent/v1/rings/claim claim",
	}
	if got := AgentRoutes(); !slices.Equal(got, want) {
		t.Fatalf("the agent route ledger is\n  %q\nwant\n  %q", got, want)
	}
	g := newRig(t, nil, standardRows()...)
	for _, path := range []string{PushPath, ClaimPath} {
		if rec := g.do(http.MethodPost, path, "", "{}", peer1); rec.Code != http.StatusUnauthorized {
			t.Errorf("POST %s with no token answered %d, want the uniform 401", path, rec.Code)
		}
		if rec := g.do(http.MethodGet, path, pushA, "", peer1); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s answered %d, want 405", path, rec.Code)
		}
	}
	for _, path := range []string{"/", "/agent/v1/rings", "/agent/v1/presence/", "/sign-in", "/agent/v1/report"} {
		if rec := g.do(http.MethodPost, path, pushA, "{}", peer1); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s answered %d, want 404 — a path in no row must not be served", path, rec.Code)
		}
	}
}

// TestAPushTokenCannotClaimAndAClaimTokenCannotPush: each token kind authenticates on its own
// route only, and on the other it is refused exactly as garbage is.
func TestAPushTokenCannotClaimAndAClaimTokenCannotPush(t *testing.T) {
	g := newRig(t, nil, standardRows()...)
	garbagePush := g.push("fixture-not-a-presence-token-at-all-xxxxxxxx", "host-a", "s-0001")
	if rec := g.push(claimA, "host-a", "s-0001"); rec.Code != http.StatusUnauthorized ||
		rec.Body.String() != garbagePush.Body.String() {
		t.Fatalf("a CLAIM token pushed: %d %q", rec.Code, rec.Body.String())
	}
	if _, ok := g.sees("s-0001"); ok {
		t.Fatal("a claim token's push was written")
	}
	garbageClaim, _ := g.claim("fixture-not-a-presence-token-at-all-xxxxxxxx")
	if rec, _ := g.claim(pushA); rec.Code != http.StatusUnauthorized || rec.Body.String() != garbageClaim.Body.String() {
		t.Fatalf("a PUSH token claimed: %d %q", rec.Code, rec.Body.String())
	}
	// POSITIVE CONTROL: each token on its own route works.
	if rec := g.push(pushA, "host-a", "s-0001"); rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL: the push token could not push: %d %s", rec.Code, rec.Body.String())
	}
	if rec, rings := g.claim(claimA); rec.Code != http.StatusOK || len(rings) != 0 {
		t.Fatalf("POSITIVE CONTROL: the claim token could not claim: %d %s", rec.Code, rec.Body.String())
	}
}

// TestAPushIsTheContractsReplace: the success answer, whole-host replace through the route, and
// host-b's rows untouched by host-a's pushes.
func TestAPushIsTheContractsReplace(t *testing.T) {
	g := newRig(t, nil, standardRows()...)
	rec := g.push(pushA, "host-a", "s-0001", "s-0002")
	if rec.Code != http.StatusOK || rec.Header().Get(StatusHeader) != StatusReplaced || rec.Body.String() != "rows=2" {
		t.Fatalf("push answered %d %q %q", rec.Code, rec.Header().Get(StatusHeader), rec.Body.String())
	}
	if rec := g.push(pushAB, "host-b", "s-0003"); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if rec := g.push(pushA, "host-a", "s-0002"); rec.Body.String() != "rows=1" {
		t.Fatal(rec.Body.String())
	}
	if _, ok := g.sees("s-0001"); ok {
		t.Fatal("s-0001 survived a replace that omitted it")
	}
	if p, ok := g.sees("s-0003"); !ok || p.Target.Host != "host-b" {
		t.Fatal("host-b's row was touched by host-a's push")
	}
}

// TestAPushNamingAnotherHostIs400AndWritesNothing pins decision 6's host check: the token's
// host decides, and a body naming another one is refused BEFORE any write — including the write
// that would have cleared the token's own host.
func TestAPushNamingAnotherHostIs400AndWritesNothing(t *testing.T) {
	g := newRig(t, nil, standardRows()...)
	if rec := g.push(pushA, "host-a", "s-0001"); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	rec := g.push(pushA, "host-b", "s-0002")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not this token's host") {
		t.Fatalf("a push naming another host answered %d %q", rec.Code, rec.Body.String())
	}
	if _, ok := g.sees("s-0002"); ok {
		t.Fatal("the mismatched push was written")
	}
	if p, ok := g.sees("s-0001"); !ok || p.Target.Host != "host-a" {
		t.Fatal("the mismatched push disturbed host-a's existing set")
	}
}

// TestTheWallRefusesAForeignRowAtStartupAndAsARowAfterwards is decision 15's own control.
// At startup a row for owner B refuses the listener; the same row appended AFTER startup is
// refused as that ROW only — logged with its digest prefix and never the token — while A's push
// and claim keep working and B's token gets the uniform 401. RED by skipping `admit`'s owner
// check (the `presence-wall-*` rows).
func TestTheWallRefusesAForeignRowAtStartupAndAsARowAfterwards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens")
	foreign := NewTokenRow(KindPush, ownerB, "host-a", pushB)
	writeTokens(t, path, append(standardRows(), foreign)...)
	svc := &Service{Store: &Store{}, Queue: &Queue{}}
	_, _, err := NewAgent(AgentConfig{TokenFile: path, Owner: ownerA, Service: svc,
		Limiter: netid.NewRateLimiter(1000, time.Minute, time.Minute)})
	if !errors.Is(err, ErrForeignOwner) {
		t.Fatalf("a token row for owner B at startup did not refuse the listener: %v", err)
	}
	if strings.Contains(err.Error(), pushB) {
		t.Fatal("the startup refusal carries the token")
	}

	g := newRig(t, nil, standardRows()...)
	appendTokens(t, g.tokens, foreign, NewTokenRow(KindClaim, ownerB, "host-a", claimB))
	if rec := g.push(pushB, "host-a", "s-0001"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("owner B's push token, added after startup, answered %d", rec.Code)
	}
	if rec, _ := g.claim(claimB); rec.Code != http.StatusUnauthorized {
		t.Fatalf("owner B's claim token, added after startup, answered %d", rec.Code)
	}
	if _, ok := g.svc.Store.For(identity.Identity{Principal: control.Principal{Kind: ownerB.Kind, ID: ownerB.ID, Display: "b"}}, "s-0001"); ok {
		t.Fatal("B's refused push was written")
	}
	log := g.logged()
	if !strings.Contains(log, foreign.DigestPrefix()) || !strings.Contains(log, "refused") {
		t.Fatalf("the refused row is not logged by digest prefix:\n%s", log)
	}
	for _, secret := range []string{pushB, claimB, foreign.Digest} {
		if strings.Contains(log, secret) {
			t.Fatalf("the log carries a token or a whole digest:\n%s", log)
		}
	}
	// A's rows keep working.
	if rec := g.push(pushA, "host-a", "s-0001"); rec.Code != http.StatusOK {
		t.Fatalf("A's push stopped working after a foreign row appeared: %d", rec.Code)
	}
	if rec, _ := g.claim(claimA); rec.Code != http.StatusOK {
		t.Fatalf("A's claim stopped working after a foreign row appeared: %d", rec.Code)
	}
	// Logged ONCE across repeated reads.
	g.push(pushA, "host-a", "s-0001")
	if n := strings.Count(g.logged(), "line 6 (digest "+foreign.DigestPrefix()); n != 1 {
		t.Fatalf("the refused row was logged %d times, want once:\n%s", n, g.logged())
	}
}

// TestRevocationTakesEffectWithoutARestart: the token file is re-read on every request, so
// deleting a row refuses its token's next request.
func TestRevocationTakesEffectWithoutARestart(t *testing.T) {
	g := newRig(t, nil, standardRows()...)
	if rec, _ := g.claim(claimA); rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL: %d", rec.Code)
	}
	var kept []TokenRow
	for _, r := range standardRows() {
		if r.Kind == KindClaim && r.Host == "host-a" {
			continue
		}
		kept = append(kept, r)
	}
	writeTokens(t, g.tokens, kept...)
	if rec, _ := g.claim(claimA); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked claim token answered %d", rec.Code)
	}
	if rec := g.push(pushA, "host-a", "s-0001"); rec.Code != http.StatusOK {
		t.Fatal("revoking one row broke another")
	}
	// A token file that vanishes fails CLOSED.
	if err := os.Remove(g.tokens); err != nil {
		t.Fatal(err)
	}
	if rec := g.push(pushA, "host-a", "s-0001"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("with the token file gone a push answered %d", rec.Code)
	}
}

// TestClaimsAreScopedToTheTokensHost: A's host-b claim token gets none of A's host-a rings; the
// host-a token gets them exactly once.
func TestClaimsAreScopedToTheTokensHost(t *testing.T) {
	g := newRig(t, nil, standardRows()...)
	if _, _, err := g.svc.Queue.Enqueue(ownerA, "host-a", "s-0001"); err != nil {
		t.Fatal(err)
	}
	if _, rings := g.claim(claimAB); len(rings) != 0 {
		t.Fatalf("A's host-b claim token claimed host-a's rings: %v", rings)
	}
	if _, rings := g.claim(claimA); !slices.Equal(rings, []string{"s-0001"}) {
		t.Fatalf("A's host-a claim got %v", rings)
	}
	if _, rings := g.claim(claimA); len(rings) != 0 {
		t.Fatalf("a ring was claimed twice: %v", rings)
	}
}

// TestTheAgentReadsNoCookie: a browser session cookie authenticates nothing here, and no
// response sets a cookie.
func TestTheAgentReadsNoCookie(t *testing.T) {
	g := newRig(t, nil, standardRows()...)
	r := httptest.NewRequest(http.MethodPost, PushPath, strings.NewReader(pushBody("host-a", wireRowJSON("s-0001"))))
	r.RemoteAddr = peer1
	r.AddCookie(&http.Cookie{Name: identity.SessionCookieName, Value: pushA})
	rec := httptest.NewRecorder()
	g.agent.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a request carrying only a cookie answered %d", rec.Code)
	}
	for _, rec := range []*httptest.ResponseRecorder{rec, g.push(pushA, "host-a", "s-0001")} {
		if c := rec.Result().Cookies(); len(c) != 0 {
			t.Fatalf("the agent set a cookie: %v", c)
		}
	}
}

// TestFailedTokensLockTheClientOut: the `netid.RateLimiter` lockout, per client.
func TestFailedTokensLockTheClientOut(t *testing.T) {
	g := newRig(t, netid.NewRateLimiter(2, time.Minute, time.Hour), standardRows()...)
	for i := 0; i < 2; i++ {
		g.push("fixture-wrong-presence-token-xxxxxxxxxxxxxxxxx", "host-a", "s-0001")
	}
	if rec := g.push(pushA, "host-a", "s-0001"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a locked-out client's VALID token answered %d", rec.Code)
	}
	if !strings.Contains(g.logged(), "locked out") {
		t.Fatalf("the refusal was not the lockout:\n%s", g.logged())
	}
	// CONTROL: another client with the same valid token is served.
	rec := g.do(http.MethodPost, PushPath, pushA, pushBody("host-a", wireRowJSON("s-0001")), "198.51.100.8:4000")
	if rec.Code != http.StatusOK {
		t.Fatalf("CONTROL FAILED: a fresh client answered %d", rec.Code)
	}
}

// TestPushBodyBoundsEachWithAJustUnderControl: every refusal is paired with the body one step
// inside the bound, which must be accepted — so a refusal is about the bound, not the shape.
func TestPushBodyBoundsEachWithAJustUnderControl(t *testing.T) {
	g := newRig(t, nil, standardRows()...)
	send := func(body string) *httptest.ResponseRecorder {
		return g.do(http.MethodPost, PushPath, pushA, body, peer1)
	}
	rows := func(n int) string {
		var rs []string
		for i := 0; i < n; i++ {
			rs = append(rs, wireRowJSON(fmt.Sprintf("s-%04d", i)))
		}
		return pushBody("host-a", rs...)
	}
	field := func(key, value string) string {
		r := map[string]string{"session": "s-0001", "runtime": "claude", "target": "notes:3", "label": "notes",
			"hotkey": "Alt+n", "last_activity": ""}
		r[key] = value
		b, _ := json.Marshal(r)
		return pushBody("host-a", string(b))
	}
	for _, tc := range []struct {
		name     string
		ok, fail string
	}{
		{"row count", rows(MaxRows), rows(MaxRows + 1)},
		{"string length", field("label", strings.Repeat("x", MaxStringBytes)), field("label", strings.Repeat("x", MaxStringBytes+1))},
		{"session class", field("session", "s-0001"), field("session", "s 0001")},
		{"runtime set", field("runtime", "opencode"), field("runtime", "shell")},
		{"last_activity", field("last_activity", "2000-01-02T03:04:05Z"), field("last_activity", "yesterday")},
		{"control character", field("hotkey", "Alt+n"), field("hotkey", "Alt\u001b+n")},
		{"line separator U+2028", field("hotkey", "Alt+n"), field("hotkey", "Alt\u2028n")},
		{"paragraph separator U+2029", field("label", "notes"), field("label", "no\u2029tes")},
		{"empty target", field("target", "notes:3"), field("target", "")},
		{"schema", pushBody("host-a"), `{"schema":2,"host":"host-a","rows":[]}`},
		{"missing rows key", pushBody("host-a"), `{"schema":1,"host":"host-a"}`},
		{"null rows", pushBody("host-a"), `{"schema":1,"host":"host-a","rows":null}`},
		{"missing row key", field("hotkey", ""), pushBody("host-a", `{"session":"s-0001","runtime":"claude","target":"notes:3","label":"notes","last_activity":""}`)},
		{"duplicate session", pushBody("host-a", wireRowJSON("s-0001"), wireRowJSON("s-0002")), pushBody("host-a", wireRowJSON("s-0001"), wireRowJSON("s-0001"))},
		{"trailing data", pushBody("host-a"), pushBody("host-a") + `{}`},
		{"body size", rows(MaxRows), pushBody("host-a") + strings.Repeat(" ", MaxPushBody)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rec := send(tc.ok); rec.Code != http.StatusOK {
				t.Fatalf("POSITIVE CONTROL (just inside the bound) answered %d: %s", rec.Code, rec.Body.String())
			}
			if rec := send(tc.fail); rec.Code != http.StatusBadRequest {
				t.Fatalf("just outside the bound answered %d: %s", rec.Code, rec.Body.String())
			}
		})
	}

	// Every never-carried field (decision 8) is an unknown field, refused at the row and at the
	// top level — `pane_preview` first, because that is what the existing push carries.
	for _, key := range []string{"pane_preview", "pane_tty", "pane_id", "window_id", "tmux_pid", "cwd",
		"repo_path", "pane_contents", "transcript", "ledger"} {
		t.Run("unknown "+key, func(t *testing.T) {
			inRow := pushBody("host-a", strings.TrimSuffix(wireRowJSON("s-0001"), "}")+`,"`+key+`":"x"}`)
			if rec := send(inRow); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), key) {
				t.Fatalf("a row carrying %q answered %d %s", key, rec.Code, rec.Body.String())
			}
			top := strings.TrimSuffix(pushBody("host-a", wireRowJSON("s-0001")), "}") + `,"` + key + `":"x"}`
			if rec := send(top); rec.Code != http.StatusBadRequest {
				t.Fatalf("a push carrying %q answered %d", key, rec.Code)
			}
		})
	}
	// The control for that loop: the same row WITHOUT the extra key is accepted.
	if rec := send(pushBody("host-a", wireRowJSON("s-0001"))); rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL: the plain row answered %d", rec.Code)
	}
	// And a 400 writes nothing: the last good push above is still the set.
	before, _ := g.sees("s-0001")
	send(pushBody("host-a", wireRowJSON("s-0002"), wireRowJSON("s-0002")))
	if after, _ := g.sees("s-0001"); after.Target.PushedAt != before.Target.PushedAt {
		t.Fatal("a refused push replaced the set")
	}
}

// TestAClaimBodyIsExactlyAnEmptyObject.
func TestAClaimBodyIsExactlyAnEmptyObject(t *testing.T) {
	g := newRig(t, nil, standardRows()...)
	for _, body := range []string{``, `null`, `[]`, `{"x":1}`, `{}{}`} {
		if rec := g.do(http.MethodPost, ClaimPath, claimA, body, peer1); rec.Code != http.StatusBadRequest {
			t.Errorf("claim body %q answered %d", body, rec.Code)
		}
	}
	if rec := g.do(http.MethodPost, ClaimPath, claimA, ` {} `, peer1); rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL: `{}` answered %d", rec.Code)
	}
}

// TestTheStartupReadRefusesAMalformedFile: a malformed row, a raw token where the digest
// belongs (never echoed), a duplicate digest and a missing file each refuse the listener.
func TestTheStartupReadRefusesAMalformedFile(t *testing.T) {
	dir := t.TempDir()
	good := NewTokenRow(KindPush, ownerA, "host-a", pushA).String()
	for _, tc := range []struct{ name, body, want string }{
		{"three fields", "push " + ownerA.String() + " host-a\n", "want 4 fields"},
		{"bad kind", strings.Replace(good, "push", "ring", 1) + "\n", "field 1 (kind)"},
		{"raw token as digest", "push " + ownerA.String() + " host-a " + pushA + "\n", "not a 64-character hex"},
		{"duplicate digest", good + "\n" + strings.Replace(good, "push", "claim", 1) + "\n", "more than one row"},
		{"bad host", strings.Replace(good, "host-a", "host/a", 1) + "\n", "field 3 (host)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-"))
			if err := os.WriteFile(p, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadTokens(p, ownerA)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadTokens = %v, want an error containing %q", err, tc.want)
			}
			if strings.Contains(err.Error(), pushA) {
				t.Fatal("the refusal echoes a raw token")
			}
		})
	}
	if _, err := LoadTokens(filepath.Join(dir, "absent"), ownerA); err == nil {
		t.Fatal("a missing token file was accepted")
	}
	p := filepath.Join(dir, "good")
	if err := os.WriteFile(p, []byte("# comment\n\n"+good+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rows, err := LoadTokens(p, ownerA); err != nil || len(rows) != 1 {
		t.Fatalf("POSITIVE CONTROL: a good file was refused (%v, %d rows)", err, len(rows))
	}
}

// TestAMintedTokenAuthenticatesItsRowAndTheFileHoldsOnlyTheDigest.
func TestAMintedTokenAuthenticatesItsRowAndTheFileHoldsOnlyTheDigest(t *testing.T) {
	token, err := MintToken()
	if err != nil || len(token) != 43 {
		t.Fatalf("MintToken = %q, %v", token, err)
	}
	g := newRig(t, nil)
	appendTokens(t, g.tokens, NewTokenRow(KindPush, ownerA, "host-a", token))
	raw, _ := os.ReadFile(g.tokens)
	if strings.Contains(string(raw), token) {
		t.Fatal("the token file holds the token")
	}
	if rec := g.push(token, "host-a", "s-0001"); rec.Code != http.StatusOK {
		t.Fatalf("a freshly minted token did not authenticate: %d", rec.Code)
	}
}
