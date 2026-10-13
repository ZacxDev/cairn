package worker

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/transcript/archive"
)

// The withdrawal route (S6, decision 16, clause m) over a REAL archive. Everything is synthetic:
// sessions `s-0001` (uploaded by owner A from `host-a`), `s-other` (stored for owner B), the host
// labels `host-a`/`host-b`, and one output line a plugin would have written.

const withdrawPath = "/capture/v1/sessions/s-0001/withdraw"

const fixtureOutput = `{"id":"o-000000000000000000000001","plugin":"summary","plugin_version":"0.1.0","model":"fake",` +
	`"session":"s-0001","input_watermark":{"main":1},"type":"summary","body":{"text":"a synthetic summary"},` +
	`"produced_at":"2000-01-02T03:04:05Z"}`

const fixtureEdge = `{"id":"o-000000000000000000000002","plugin":"clickup","plugin_version":"0.1.0","model":"none",` +
	`"session":"s-0001","input_watermark":{"main":1},"type":"ticket-edge","body":{"system":"clickup","ticket_id":"abc123",` +
	`"signal":"branch-name","locator":{"pr":7}},"produced_at":"2000-01-02T03:04:05Z"}`

// storeOwned uploads `s-0001` as owner A from host-a through the real upload route, with a blob,
// and gives it one summary and one ticket edge.
func storeOwned(t *testing.T, g *rig) string {
	t.Helper()
	if rec := g.do(http.MethodPost, recordsPath, captureA, recordsBody("host-a", "", "100", "synthetic turn")); rec.Code != http.StatusOK {
		t.Fatalf("upload answered %d %q", rec.Code, rec.Body.String())
	}
	if rec := g.do(http.MethodPost, blobPath, captureA, blobBody("host-a", "", "v1", []byte("synthetic output\n"))); rec.Code != http.StatusOK {
		t.Fatalf("blob upload answered %d %q", rec.Code, rec.Body.String())
	}
	for plugin, line := range map[string]string{"summary": fixtureOutput, "clickup": fixtureEdge} {
		if err := g.arc.AppendOutput("s-0001", plugin, []byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(g.dir, "sessions", "s-0001")
}

func journalLines(t *testing.T, g *rig) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(g.dir, "journal.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

// TestAWithdrawalDeletesOnlyTheCallersOwnRoot is clause (m)'s pod half and the route's whole
// contract. A capture token's withdrawal of its OWN (owner, host)'s root answers `{"deleted":true}`
// and runs the deletion cascade — the directory, so the streams, the blob, the summary AND the
// ticket edge — journaled with who and when and no content. The same call
//
//   - for the SAME owner's root from ANOTHER host label (mutant `transcript-withdraw-host-unchecked`),
//   - for ANOTHER owner's root (mutant `transcript-withdraw-deletes-another-owners-root`),
//   - for a root that never existed, and for an id that is not a session id at all
//
// each answers the IDENTICAL `{"deleted":false}` and deletes nothing. And the owner's own
// withdrawal REPEATED (the lost-acknowledgement case) answers `true` again and changes nothing
// (mutant `transcript-withdraw-repeat-answers-false`).
func TestAWithdrawalDeletesOnlyTheCallersOwnRoot(t *testing.T) {
	g := newRig(t, true, nil)
	dir := storeOwned(t, g)
	// Another owner's root, stored straight into the archive: the listener's wall admits one capture
	// owner, so no token of B's could have uploaded it here — the archive is where the rule must hold.
	if _, err := g.arc.AppendRecords(archive.Uploader{Owner: ownerB.String(), Host: "host-a"},
		archive.Upload{Root: "s-other", Runtime: "claude", From: "", To: "1"}, "main",
		[]archive.Record{{Src: "0", Rec: []byte(`{"type":"user","message":{"role":"user","content":"synthetic"}}`)}}); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(g.dir, "sessions", "s-other")
	used := g.arc.Used()

	// Every "no", and each is the same bytes.
	for name, c := range map[string]struct{ path, token string }{
		"the same owner from another host label": {withdrawPath, captureAB},
		"another owner's root":                   {"/capture/v1/sessions/s-other/withdraw", captureA},
		"a root that never existed":              {"/capture/v1/sessions/s-never/withdraw", captureA},
		"an id that is not a session id":         {"/capture/v1/sessions/.not-an-id/withdraw", captureA},
	} {
		rec := g.do(http.MethodPost, c.path, c.token, "")
		if rec.Code != http.StatusOK || rec.Body.String() != NotWithdrawnBody {
			t.Errorf("%s: answered %d %q, want 200 %q", name, rec.Code, rec.Body.String(), NotWithdrawnBody)
		}
	}
	for _, d := range []string{dir, other} {
		if _, err := os.Stat(filepath.Join(d, "meta.json")); err != nil {
			t.Fatalf("a refused withdrawal deleted %s: %v", d, err)
		}
	}
	if !g.arc.HasOutputs("s-0001") || g.arc.Used() != used || len(journalLines(t, g)) != 0 {
		t.Fatalf("a refused withdrawal changed the store: outputs=%v used %d→%d journal=%v",
			g.arc.HasOutputs("s-0001"), used, g.arc.Used(), journalLines(t, g))
	}

	// The owner's own, from the host that uploaded it.
	rec := g.do(http.MethodPost, withdrawPath, captureA, "")
	if rec.Code != http.StatusOK || rec.Body.String() != WithdrawnBody {
		t.Fatalf("the uploader's own withdrawal answered %d %q, want 200 %q", rec.Code, rec.Body.String(), WithdrawnBody)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the withdrawn session's directory is still there (%v) — its streams, blob, summary and ticket edge with it", err)
	}
	if _, err := os.Stat(filepath.Join(other, "meta.json")); err != nil {
		t.Fatalf("withdrawing s-0001 touched another owner's root: %v", err)
	}
	lines := journalLines(t, g)
	want := `{"event":"transcript-deleted","root":"s-0001","owner":"user:u_alpha","host":"host-a","reason":"withdrawn","at":"2000-01-02T03:04:05Z"}`
	if len(lines) != 1 || lines[0] != want {
		t.Fatalf("the journal holds\n%v\nwant the one fact\n%s", lines, want)
	}
	if g.arc.Used() >= used {
		t.Fatalf("the withdrawal freed nothing: %d → %d bytes", used, g.arc.Used())
	}
	after := g.arc.Used()

	// WITHDRAW-TWICE: the acknowledgement was lost, the agent asks again.
	for i := 0; i < 2; i++ {
		rec = g.do(http.MethodPost, withdrawPath, captureA, "")
		if rec.Code != http.StatusOK || rec.Body.String() != WithdrawnBody {
			t.Fatalf("repeat %d of the uploader's own withdrawal answered %d %q, want %q again", i+1, rec.Code, rec.Body.String(), WithdrawnBody)
		}
	}
	if got := journalLines(t, g); len(got) != 1 || g.arc.Used() != after {
		t.Fatalf("a repeated withdrawal changed something: journal %v, used %d→%d", got, after, g.arc.Used())
	}
	// …and it is still `false` for the other host label, which never held it.
	if rec := g.do(http.MethodPost, withdrawPath, captureAB, ""); rec.Body.String() != NotWithdrawnBody {
		t.Fatalf("host-b, which never held the root, was answered %q after host-a withdrew it", rec.Body.String())
	}

}

// TestADeletedSessionIsNotResurrectedByALaterUpload — #273's audit (F2). After a session's transcript
// is deleted here, the root has no `meta.json`, and the upload path used to read that as "a first
// upload": anything sent with `from == ""` — a subagent stream that began after the delete, a new or
// changed blob, the first frame of a large record — was ACCEPTED and re-created the directory, a
// fragment of the session its owner had deleted, to which a plugin could attach an output. RED before
// the fix: each of those answered 200 and the directory came back.
//
// Now a journaled deletion is FINAL for its root: the uploader that held it gets 410 `deleted` — the
// agent's signal to stop for good — on every route; another host label asking about that id gets
// the uniform 409 (no oracle); nothing is stored, staged or created.
func TestADeletedSessionIsNotResurrectedByALaterUpload(t *testing.T) {
	g := newRig(t, true, nil)
	dir := storeOwned(t, g)
	if rec := g.do(http.MethodPost, withdrawPath, captureA, ""); rec.Body.String() != WithdrawnBody {
		t.Fatalf("control: the withdrawal answered %q", rec.Body.String())
	}
	used := g.arc.Used()
	for name, c := range map[string]struct{ path, body string }{
		"the main stream again, from the start": {recordsPath, recordsBody("host-a", "", "100", "synthetic turn")},
		"a subagent stream that began later":    {"/capture/v1/sessions/s-0001/streams/subagent:late01/records", recordsBody("host-a", "", "100", "synthetic turn")},
		"a new blob":                            {"/capture/v1/sessions/s-0001/blobs/toolu_new.txt", blobBody("host-a", "", "v1", []byte("synthetic output\n"))},
		"the old blob, changed":                 {blobPath, blobBody("host-a", "", "v2", []byte("synthetic output, changed\n"))},
		"the first frame of a large record": {recordsPath, `{"schema":1,"runtime":"claude","host":"host-a","from":"","to":"100","declared_scopes":[],` +
			`"records":[{"src":"0","chunk":"e30="}],"frames":{"index":0,"count":2}}`},
	} {
		rec := g.do(http.MethodPost, c.path, captureA, c.body)
		if rec.Code != http.StatusGone || rec.Body.String() != DeletedBody {
			t.Errorf("%s: answered %d %q, want 410 %q", name, rec.Code, rec.Body.String(), DeletedBody)
		}
	}
	// Another host label of the same owner: the uniform refusal, never "deleted".
	if rec := g.do(http.MethodPost, recordsPath, captureAB, recordsBody("host-b", "", "100", "synthetic turn")); rec.Code != http.StatusConflict ||
		rec.Body.String() != NotOwnerBody {
		t.Errorf("host-b's upload under the deleted id answered %d %q, want the uniform 409", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("an upload after the deletion re-created the session's directory (%v)", err)
	}
	if staged, _ := os.ReadDir(filepath.Join(g.dir, "frames")); len(staged) != 0 {
		t.Fatalf("an upload after the deletion staged %d frame file(s)", len(staged))
	}
	if g.arc.Used() != used {
		t.Fatalf("refused uploads changed the quota's count: %d → %d", used, g.arc.Used())
	}
	// A plugin cannot attach an output to it either.
	if err := g.arc.AppendOutput("s-0001", "summary", []byte(fixtureOutput)); !errors.Is(err, archive.ErrNoSession) {
		t.Fatalf("an output for the deleted session: %v, want no stored session", err)
	}
	// CONTROL: a root that was never deleted is stored as ever.
	if rec := g.do(http.MethodPost, "/capture/v1/sessions/s-0002/streams/main/records", captureA, recordsBody("host-a", "", "100", "synthetic turn")); rec.Code != http.StatusOK {
		t.Fatalf("control: a fresh root answered %d %q", rec.Code, rec.Body.String())
	}
}

// TestTheWithdrawalRouteIsBehindTheTokenAndNotTheArmingGate: an unauthenticated caller and a caller
// whose owner the authority no longer holds get the uniform 401 and delete nothing; a DISARMED
// instance that holds a transcript still honours its uploader's withdrawal (it stores nothing — the
// arming gate is the upload routes'), while the same instance refuses an upload 503.
func TestTheWithdrawalRouteIsBehindTheTokenAndNotTheArmingGate(t *testing.T) {
	g := newRig(t, false, nil)
	if _, err := g.arc.AppendRecords(archive.Uploader{Owner: ownerA.String(), Host: "host-a"},
		archive.Upload{Root: "s-0001", Runtime: "claude", From: "", To: "1"}, "main",
		[]archive.Record{{Src: "0", Rec: []byte(`{"type":"user","message":{"role":"user","content":"synthetic"}}`)}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(g.dir, "sessions", "s-0001")
	for name, token := range map[string]string{"no token": "", "a garbage token": garbage} {
		if rec := g.do(http.MethodPost, withdrawPath, token, ""); rec.Code != http.StatusUnauthorized || rec.Body.String() != unauthorizedBody {
			t.Fatalf("%s: answered %d %q, want the uniform 401", name, rec.Code, rec.Body.String())
		}
	}
	g.unheld.Store(true)
	if rec := g.do(http.MethodPost, withdrawPath, captureA, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an owner the authority no longer holds was answered %d, want 401", rec.Code)
	}
	g.unheld.Store(false)
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		t.Fatalf("a refused caller deleted the session: %v", err)
	}
	// CONTROL: this instance IS disarmed — an upload is the 503.
	if rec := g.do(http.MethodPost, recordsPath, captureA, recordsBody("host-a", "1", "2", "more")); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("control: the disarmed instance answered %d to an upload, want 503", rec.Code)
	}
	if rec := g.do(http.MethodPost, withdrawPath, captureA, ""); rec.Code != http.StatusOK || rec.Body.String() != WithdrawnBody {
		t.Fatalf("a disarmed instance answered %d %q to its uploader's withdrawal, want 200 %q", rec.Code, rec.Body.String(), WithdrawnBody)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the directory survived the withdrawal: %v", err)
	}
	if got := journalLines(t, g); len(got) != 1 || bytes.Contains([]byte(got[0]), []byte("synthetic")) {
		t.Fatalf("the journal holds %v — want one line, and no content", got)
	}
}

// TestARepeatedWithdrawalIsNotLoggedAsADeletion — the idempotent `true` (a lost acknowledgement, asked
// again) deletes nothing, and the operator's log says a session was withdrawn exactly ONCE: a second
// line would report a deletion that did not happen.
func TestARepeatedWithdrawalIsNotLoggedAsADeletion(t *testing.T) {
	g := newRig(t, true, nil)
	storeOwned(t, g)
	for i := 0; i < 3; i++ {
		if rec := g.do(http.MethodPost, withdrawPath, captureA, ""); rec.Code != http.StatusOK || rec.Body.String() != WithdrawnBody {
			t.Fatalf("withdrawal %d answered %d %q", i+1, rec.Code, rec.Body.String())
		}
	}
	n := 0
	for _, line := range g.log {
		if strings.Contains(line, "withdrawn by its uploader") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("three withdrawals of one root logged %d deletions, want 1:\n%s", n, strings.Join(g.log, "\n"))
	}
}
