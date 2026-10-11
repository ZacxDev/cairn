package worker

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/depspolicy"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
)

// Synthetic, pairwise-distinct tokens; none has ever authorised anything.
const (
	captureA  = "fixture-capture-token-owner-a-host-a-not-real"
	captureAB = "fixture-capture-token-owner-a-host-b-not-real"
	foreignB  = "fixture-capture-token-owner-b-host-a-not-real"
	garbage   = "fixture-garbage-token-for-the-worker-xxxxxxx"
	peer1     = "198.51.100.7:4000"
)

var (
	ownerA = Owner{Kind: control.KindUser, ID: "u_alpha"}
	ownerB = Owner{Kind: control.KindUser, ID: "u_beta"}
)

type rig struct {
	t      *testing.T
	w      *Worker
	arc    *archive.Archive
	tokens string
	log    []string
}

func writeRows(t *testing.T, path string, rows ...TokenRow) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# synthetic worker tokens\n")
	for _, r := range rows {
		b.WriteString(r.String() + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func standardRows() []TokenRow {
	return []TokenRow{
		NewTokenRow(KindCapture, ownerA, "host-a", captureA),
		NewTokenRow(KindCapture, ownerA, "host-b", captureAB),
	}
}

func newRig(t *testing.T, armed bool, limiter *netid.RateLimiter) *rig {
	t.Helper()
	if limiter == nil {
		limiter = netid.NewRateLimiter(1000, time.Minute, time.Minute)
	}
	red, err := redact.New(redact.SelfTestKey(7), nil)
	if err != nil {
		t.Fatal(err)
	}
	arc, err := archive.Open(archive.Config{Dir: t.TempDir(), Retention: time.Hour, Quota: 1 << 30, Recheck: red,
		Now: func() time.Time { return time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	g := &rig{t: t, arc: arc, tokens: filepath.Join(t.TempDir(), "worker-tokens")}
	writeRows(t, g.tokens, standardRows()...)
	w, _, err := New(Config{TokenFile: g.tokens, Owner: ownerA, Archive: arc, Armed: armed, Limiter: limiter,
		Log: func(s string) { g.log = append(g.log, s) }})
	if err != nil {
		t.Fatal(err)
	}
	g.w = w
	return g
}

func (g *rig) do(method, path, token, body string) *httptest.ResponseRecorder {
	g.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = peer1
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	g.w.ServeHTTP(rec, r)
	return rec
}

func recordsBody(host, from, to string, texts ...string) string {
	var recs []string
	for i, text := range texts {
		rec, _ := json.Marshal(map[string]any{"type": "assistant", "uuid": fmt.Sprintf("00000000-0000-4000-8000-%012d", i),
			"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}}})
		recs = append(recs, fmt.Sprintf(`{"src":"%d","rec":%s}`, i, rec))
	}
	return fmt.Sprintf(`{"schema":1,"runtime":"claude","host":%q,"from":%q,"to":%q,"declared_scopes":[],"records":[%s]}`,
		host, from, to, strings.Join(recs, ","))
}

func blobBody(host, from, to string, data []byte) string {
	return fmt.Sprintf(`{"schema":1,"runtime":"claude","host":%q,"from":%q,"to":%q,"declared_scopes":[],"data":%q}`,
		host, from, to, base64.StdEncoding.EncodeToString(data))
}

const (
	recordsPath = "/capture/v1/sessions/s-0001/streams/main/records"
	blobPath    = "/capture/v1/sessions/s-0001/blobs/toolu_x.txt"
)

func randToken(seed uint64) string {
	rng := rand.New(rand.NewPCG(seed, ^seed))
	const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 36)
	for i := range b {
		b[i] = alnum[rng.IntN(len(alnum))]
	}
	return "ghp_" + string(b)
}

// TestTheWorkerRouteLedgerIsExactlyThis is the third listener's route ledger. A new row is a new
// endpoint a host-side token can reach; this hand-written list is where somebody has to say so.
// Every row is DISPATCHED (a POST with no token is the uniform 401, not the no-route 404), a GET on
// a row is 405, and a path in no row is 404.
func TestTheWorkerRouteLedgerIsExactlyThis(t *testing.T) {
	want := []string{
		"POST /capture/v1/sessions/{root}/blobs/{name} capture",
		"POST /capture/v1/sessions/{root}/streams/{stream}/records capture",
	}
	if got := Routes(); !slices.Equal(got, want) {
		t.Fatalf("the worker route ledger is\n%v\nwant\n%v", got, want)
	}
	g := newRig(t, true, nil)
	for _, p := range []string{recordsPath, blobPath} {
		if rec := g.do(http.MethodPost, p, "", "{}"); rec.Code != http.StatusUnauthorized || rec.Body.String() != unauthorizedBody {
			t.Fatalf("POST %s with no token answered %d %q, want the uniform 401", p, rec.Code, rec.Body.String())
		}
		if rec := g.do(http.MethodGet, p, captureA, ""); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s answered %d, want 405", p, rec.Code)
		}
	}
	if rec := g.do(http.MethodPost, "/capture/v1/sessions/s-0001/withdraw", captureA, "{}"); rec.Code != http.StatusNotFound {
		t.Fatalf("a path in no row (S6's withdraw) answered %d, want 404", rec.Code)
	}
}

// TestADisarmedListenerRefusesEveryUploadAndStoresNothing is O16 at the route: with the zero-value
// arming (the default every instance has), a VALID token with a VALID body is refused 503 with the
// one disarmed body on both routes, and the archive holds nothing — no session, no byte. A garbage
// token is still the uniform 401 (the disarmed state is told only to an authenticated host). The
// positive control is the same request, armed: 200.
func TestADisarmedListenerRefusesEveryUploadAndStoresNothing(t *testing.T) {
	var zero Config
	if zero.Armed {
		t.Fatal("the zero Config is armed")
	}
	g := newRig(t, false, nil)
	for _, c := range []struct{ path, body string }{
		{recordsPath, recordsBody("host-a", "", "100", "synthetic turn")},
		{blobPath, blobBody("host-a", "", "v1", []byte("synthetic output\n"))},
	} {
		rec := g.do(http.MethodPost, c.path, captureA, c.body)
		if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != DisarmedBody {
			t.Fatalf("a disarmed listener answered %d %q to %s, want 503 %q", rec.Code, rec.Body.String(), c.path, DisarmedBody)
		}
		if rec := g.do(http.MethodPost, c.path, garbage, c.body); rec.Code != http.StatusUnauthorized {
			t.Fatalf("a garbage token on a disarmed listener answered %d, want the uniform 401", rec.Code)
		}
	}
	if roots, _ := g.arc.Roots(); len(roots) != 0 || g.arc.Used() != 0 {
		t.Fatalf("a disarmed listener stored %v (%d bytes)", roots, g.arc.Used())
	}
	armed := newRig(t, true, nil)
	if rec := armed.do(http.MethodPost, recordsPath, captureA, recordsBody("host-a", "", "100", "synthetic turn")); rec.Code != http.StatusOK ||
		rec.Body.String() != `{"stored_to":"100","seq_to":1}`+"\n" {
		t.Fatalf("POSITIVE CONTROL: the armed listener answered %d %q", rec.Code, rec.Body.String())
	}
}

// TestAHostTheTokenIsNotBoundToIs400AndWritesNothing: a host-a token posting `host: host-b` is
// refused before any write; revocation by deleting the row then answers 401 with no restart.
func TestAHostTheTokenIsNotBoundToIs400AndWritesNothing(t *testing.T) {
	g := newRig(t, true, nil)
	rec := g.do(http.MethodPost, recordsPath, captureA, recordsBody("host-b", "", "100", "synthetic turn"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not this token's host") {
		t.Fatalf("a body naming another host answered %d %q", rec.Code, rec.Body.String())
	}
	if rec := g.do(http.MethodPost, blobPath, captureA, blobBody("host-b", "", "v1", []byte("x\n"))); rec.Code != http.StatusBadRequest {
		t.Fatalf("a blob naming another host answered %d", rec.Code)
	}
	if roots, _ := g.arc.Roots(); len(roots) != 0 {
		t.Fatalf("a refused host wrote %v", roots)
	}
	if rec := g.do(http.MethodPost, recordsPath, captureA, recordsBody("host-a", "", "100", "synthetic turn")); rec.Code != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL: the token's own host answered %d %s", rec.Code, rec.Body.String())
	}
	writeRows(t, g.tokens, NewTokenRow(KindCapture, ownerA, "host-b", captureAB))
	if rec := g.do(http.MethodPost, recordsPath, captureA, recordsBody("host-a", "100", "200", "turn two")); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked row answered %d, want 401 with no restart", rec.Code)
	}
}

// TestTheOwnershipRefusalIsOneBody: owner A's host-b token on a root host-a uploaded is 409 with
// the ONE body naming neither, from the right position AND from a wrong one (a stale answer would
// disclose the holder's position).
func TestTheOwnershipRefusalIsOneBody(t *testing.T) {
	g := newRig(t, true, nil)
	if rec := g.do(http.MethodPost, recordsPath, captureA, recordsBody("host-a", "", "100", "turn")); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	for _, from := range []string{"100", "", "7"} {
		rec := g.do(http.MethodPost, recordsPath, captureAB, recordsBody("host-b", from, "200", "turn"))
		if rec.Code != http.StatusConflict || rec.Body.String() != NotOwnerBody {
			t.Fatalf("from %q: another host answered %d %q, want 409 %q", from, rec.Code, rec.Body.String(), NotOwnerBody)
		}
	}
	rec := g.do(http.MethodPost, recordsPath, captureA, recordsBody("host-a", "", "200", "turn"))
	if rec.Code != http.StatusConflict || rec.Body.String() != `{"stored_to":"100"}`+"\n" {
		t.Fatalf("the owner's stale upload answered %d %q, want 409 naming 100", rec.Code, rec.Body.String())
	}
}

// TestTheRecheckRefusalNamesTheIndexNotTheValue: clause (c) over the wire — 422 naming the record
// index or the blob, never the value, and nothing stored.
func TestTheRecheckRefusalNamesTheIndexNotTheValue(t *testing.T) {
	g := newRig(t, true, nil)
	token := randToken(11)
	rec := g.do(http.MethodPost, recordsPath, captureA, recordsBody("host-a", "", "100", "clean", "header Authorization: token "+token))
	if rec.Code != http.StatusUnprocessableEntity || rec.Body.String() != `{"refused":"recheck","record":1}`+"\n" {
		t.Fatalf("a record carrying a token answered %d %q", rec.Code, rec.Body.String())
	}
	rec = g.do(http.MethodPost, blobPath, captureA, blobBody("host-a", "", "v1", []byte("GITHUB_TOKEN="+token+"\n")))
	if rec.Code != http.StatusUnprocessableEntity || rec.Body.String() != `{"refused":"recheck","blob":"toolu_x.txt"}`+"\n" {
		t.Fatalf("a blob carrying a token answered %d %q", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.Join(g.log, "\n"), token) {
		t.Fatal("a log line carries the value")
	}
	if roots, _ := g.arc.Roots(); len(roots) != 0 {
		t.Fatalf("a refused upload stored %v", roots)
	}
}

// TestAFramedRecordCrossesTheWire: two frames — 202 then 200 — store one record.
func TestAFramedRecordCrossesTheWire(t *testing.T) {
	g := newRig(t, true, nil)
	src := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"framed synthetic turn"}]}}`)
	half := len(src) / 2
	body := func(i int, chunk []byte) string {
		return fmt.Sprintf(`{"schema":1,"runtime":"claude","host":"host-a","from":"","to":"100","declared_scopes":[],`+
			`"records":[{"src":"0","chunk":%q}],"frames":{"index":%d,"count":2}}`, base64.StdEncoding.EncodeToString(chunk), i)
	}
	if rec := g.do(http.MethodPost, recordsPath, captureA, body(0, src[:half])); rec.Code != http.StatusAccepted ||
		rec.Body.String() != `{"frame":0,"of":2}`+"\n" {
		t.Fatalf("frame 0 answered %d %q", rec.Code, rec.Body.String())
	}
	if rec := g.do(http.MethodPost, recordsPath, captureA, body(1, src[half:])); rec.Code != http.StatusOK {
		t.Fatalf("frame 1 answered %d %q", rec.Code, rec.Body.String())
	}
	got, err := g.arc.ReadStream("s-0001", "main")
	if err != nil || len(got) != 1 || string(got[0].Rec) != string(src) {
		t.Fatalf("stored %v (%v)", got, err)
	}
}

// TestTheWireIsStrict: unknown keys, a missing key and a framed/unframed mix are each 400.
func TestTheWireIsStrict(t *testing.T) {
	g := newRig(t, true, nil)
	for name, body := range map[string]string{
		"an unknown key":    strings.Replace(recordsBody("host-a", "", "1", "t"), `"schema":1`, `"schema":1,"cwd":"/work"`, 1),
		"no declared":       strings.Replace(recordsBody("host-a", "", "1", "t"), `"declared_scopes":[],`, "", 1),
		"chunk unframed":    `{"schema":1,"runtime":"claude","host":"host-a","from":"","to":"1","declared_scopes":[],"records":[{"src":"0","chunk":"eA=="}]}`,
		"trailing data":     recordsBody("host-a", "", "1", "t") + "{}",
		"another schema":    strings.Replace(recordsBody("host-a", "", "1", "t"), `"schema":1`, `"schema":2`, 1),
		"an unknown stream": "",
	} {
		path := recordsPath
		if name == "an unknown stream" {
			path, body = "/capture/v1/sessions/s-0001/streams/ledger/records", recordsBody("host-a", "", "1", "t")
		}
		if rec := g.do(http.MethodPost, path, captureA, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d %q, want 400", name, rec.Code, rec.Body.String())
		}
	}
	if roots, _ := g.arc.Roots(); len(roots) != 0 {
		t.Fatalf("a refused body stored %v", roots)
	}
}

// TestFailedTokensLockTheClientOut: the failed-token lockout keys on the client and then refuses
// even the real token, uniformly.
func TestFailedTokensLockTheClientOut(t *testing.T) {
	g := newRig(t, true, netid.NewRateLimiter(3, time.Minute, time.Minute))
	for range 3 {
		g.do(http.MethodPost, recordsPath, garbage, "{}")
	}
	rec := g.do(http.MethodPost, recordsPath, captureA, recordsBody("host-a", "", "1", "t"))
	if rec.Code != http.StatusUnauthorized || rec.Body.String() != unauthorizedBody {
		t.Fatalf("a locked-out client with a real token answered %d %q", rec.Code, rec.Body.String())
	}
}

// TestTheWallRefusesAForeignRowAtStartupAndAsARowAfterwards: a row for another owner refuses the
// whole file at startup; appended after startup it authenticates nothing while the owner's rows
// keep working.
func TestTheWallRefusesAForeignRowAtStartupAndAsARowAfterwards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker-tokens")
	writeRows(t, path, NewTokenRow(KindCapture, ownerA, "host-a", captureA), NewTokenRow(KindCapture, ownerB, "host-a", foreignB))
	if _, err := LoadTokens(path, ownerA); err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("a foreign row at startup answered %v", err)
	}
	g := newRig(t, true, nil)
	if err := AppendTokenRow(g.tokens, NewTokenRow(KindCapture, ownerB, "host-a", foreignB)); err != nil {
		t.Fatal(err)
	}
	if rec := g.do(http.MethodPost, recordsPath, foreignB, recordsBody("host-a", "", "1", "t")); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a foreign row appended after startup answered %d", rec.Code)
	}
	if rec := g.do(http.MethodPost, recordsPath, captureA, recordsBody("host-a", "", "1", "t")); rec.Code != http.StatusOK {
		t.Fatalf("the owner's own row stopped working: %d", rec.Code)
	}
}

// TestTheTokenFileHoldsDigestsOnly: a `plugin` row (S5's kind) and a raw token in the digest
// column are each refused at startup, and the refusal does not echo the value.
func TestTheTokenFileHoldsDigestsOnly(t *testing.T) {
	for name, line := range map[string]string{
		"a plugin row":       "plugin " + ownerA.String() + " host-a " + control.HashToken(captureA),
		"a raw token pasted": "capture " + ownerA.String() + " host-a " + captureA,
	} {
		path := filepath.Join(t.TempDir(), "worker-tokens")
		if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadTokens(path, ownerA)
		if err == nil || strings.Contains(err.Error(), captureA) {
			t.Fatalf("%s: answered %v", name, err)
		}
	}
}

// TestOnlyTheBrowserProgramImportsWorker is the importer ledger of BOTH new packages (non-test
// imports, through the walk the dependency ban trusts). A worker token means something only where
// `internal/worker` is reachable, so a new importer — `internal/identity`, `internal/ui`, the pod —
// is a new place a capture token could come to authenticate. Fails on SHRINK too.
func TestOnlyTheBrowserProgramImportsWorker(t *testing.T) {
	root, err := depspolicy.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	graph, err := depspolicy.ImportGraph(root, true)
	if err != nil {
		t.Fatal(err)
	}
	mod := depspolicy.ModulePath
	for _, c := range []struct {
		pkg  string
		want []string
	}{
		{mod + "/internal/worker", []string{mod + "/cmd/cairn-ui"}},
		{mod + "/internal/transcript/archive", []string{mod + "/cmd/cairn-ui", mod + "/internal/worker"}},
	} {
		if _, ok := graph[c.pkg]; !ok || len(graph) < 10 {
			t.Fatalf("POSITIVE CONTROL: the walk saw %d package(s) and %s=%v", len(graph), c.pkg, ok)
		}
		var importers []string
		for pkg, imports := range graph {
			if slices.Contains(imports, c.pkg) {
				importers = append(importers, pkg)
			}
		}
		slices.Sort(importers)
		if !slices.Equal(importers, c.want) {
			t.Fatalf("%s is imported by %v, want exactly %v", c.pkg, importers, c.want)
		}
	}
}
