package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/client"
)

// The agent's withdrawal on a routing change (S6, decision 16, clause m). Synthetic throughout: the
// generated world's `cc-plain` session, the instances `personal` and `client`, the scopes
// `alpha-notes` (personal) and `beta-notes` (client).

// shipThenRead ships `cc-plain` to the personal instance (it read `alpha-notes`), then appends a
// read of `beta-notes`, so the NEXT run's routing answer is no longer the instance holding the
// prefix. It returns the agent, its log and the session's id.
func shipThenRead(t *testing.T) (*Agent, env, *bytes.Buffer, string) {
	t.Helper()
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	s := w.Sessions["cc-plain"]
	path := filepath.Join(e.claude, s.Project, s.ID+".jsonl")
	appendTo(t, path, strings.Replace(betaRead, "beta-notes", "alpha-notes", 1))
	var log bytes.Buffer
	a := newAgent(t, e, twoInstances, &fakeRunner{}, &log)
	mustRun(t, a)
	if len(spooled(t, e, "personal", s.ID, "main")) == 0 || a.State.Sessions[s.ID].Instance != "personal" {
		t.Fatal("control: run 1 did not ship the alpha-notes session to the personal instance")
	}
	appendTo(t, path, betaRead)
	return a, e, &log, s.ID
}

// TestAWithdrawalAnsweredFalseHoldsTheSessionForGood: the instance answers `{"deleted": false}` to
// the withdrawal of a prefix this agent has an ACKNOWLEDGED upload of (the token was reissued under
// another host label, say). That answer is FINAL: the session is HELD on this host, logged loudly,
// nothing of it is shipped anywhere else, and no later run asks again or ships.
func TestAWithdrawalAnsweredFalseHoldsTheSessionForGood(t *testing.T) {
	a, e, log, id := shipThenRead(t)
	e.sink.notHeld = true
	sum := mustRun(t, a)
	ss := a.State.Sessions[id]
	if !ss.Held || !strings.Contains(ss.HeldReason, "prefix not withdrawn: not held by this owner and host") || ss.Instance != "personal" {
		t.Fatalf("after a `false` answer the session is held=%v (%q) bound to %q — want held, with the reason, still naming where the prefix is",
			ss.Held, ss.HeldReason, ss.Instance)
	}
	if sum.Held != 1 || sum.Withdrawn != 0 || sum.Records != 0 || e.sink.has("client", id) {
		t.Fatalf("run 2: %+v, shipped to client=%v — want held, nothing withdrawn, nothing shipped", sum, e.sink.has("client", id))
	}
	if !strings.Contains(log.String(), "HOLD "+id+": PREFIX NOT WITHDRAWN: not held by this owner and host") {
		t.Fatalf("the hold is not logged loudly:\n%s", log.String())
	}
	asked := len(e.sink.withdrawn["personal"])
	e.sink.notHeld = false
	if sum := mustRun(t, a); sum.Records != 0 || len(e.sink.withdrawn["personal"]) != asked || e.sink.has("client", id) {
		t.Fatalf("a later run acted on a session held for good: %+v, withdrawals %d → %d", sum, asked, len(e.sink.withdrawn["personal"]))
	}
}

// TestAFailedWithdrawalShipsNothingAndIsRetried: a withdrawal that got NO answer is not done. The
// session stays bound where its prefix is, nothing of it reaches the new instance, and the next run
// asks again — and then re-ships the whole session.
func TestAFailedWithdrawalShipsNothingAndIsRetried(t *testing.T) {
	a, e, _, id := shipThenRead(t)
	// The session's new answer is HELD (it read both instances' scopes), so drive the MOVE case too:
	// a second world below. Here: the held case, with the instance unreachable.
	e.sink.withdrawErr = errors.New("synthetic: the instance did not answer")
	sum, err := a.Run()
	if err != nil {
		t.Fatalf("a failed withdrawal ended the run: %v", err)
	}
	ss := a.State.Sessions[id]
	if sum.Refused != 1 || sum.Withdrawn != 0 || ss.Held || ss.Instance != "personal" || e.sink.has("client", id) {
		t.Fatalf("after a withdrawal with no answer: %+v held=%v instance=%q client=%v — want the session refused for this run and bound where it was",
			sum, ss.Held, ss.Instance, e.sink.has("client", id))
	}
	e.sink.withdrawErr = nil
	if sum := mustRun(t, a); sum.Withdrawn != 1 || !slices.Contains(e.sink.withdrawn["personal"], id) || a.State.Sessions[id].Instance != "" {
		t.Fatalf("the next run did not withdraw: %+v, bound to %q", sum, a.State.Sessions[id].Instance)
	}
}

// TestWhereASessionIsBoundIsSavedBeforeItsFirstUpload is S3b's audit finding, closed: a run KILLED
// after its first upload left must leave a state that still says where the prefix went, or the next
// run — whose routing answer changed — has no instance to withdraw from.
//
// The kill is modelled exactly: the state "on disk" is the last checkpoint taken BEFORE the first
// records write arrived at the sink, and the next process starts from it. RED before the fix: no
// checkpoint had been taken at all by then (the state was saved up to two seconds after a session
// changed), so the next run found the session bound nowhere and withdrew nothing.
func TestWhereASessionIsBoundIsSavedBeforeItsFirstUpload(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	s := w.Sessions["cc-plain"]
	path := filepath.Join(e.claude, s.Project, s.ID+".jsonl")
	statePath := filepath.Join(t.TempDir(), "state.json")
	a := newAgent(t, e, twoInstances, &fakeRunner{}, &bytes.Buffer{})
	a.Checkpoint = func() error { return a.State.Save(statePath) }
	a.CheckpointEvery = 1 << 62 // never by the clock: only the saves the agent makes AT ONCE
	var atFirstUpload []byte
	e.sink.onRecords = func() {
		if atFirstUpload == nil {
			atFirstUpload, _ = os.ReadFile(statePath)
			if atFirstUpload == nil {
				atFirstUpload = []byte{}
			}
		}
	}
	mustRun(t, a) // V is empty: it ships to the default instance, `personal`
	if len(spooled(t, e, "personal", s.ID, "main")) == 0 {
		t.Fatal("control: run 1 shipped nothing")
	}
	if len(atFirstUpload) == 0 {
		t.Fatal("no state had been saved when the first upload left: a kill here loses where the prefix went")
	}
	// THE KILL: the next process starts from what was on disk when the first upload left.
	if err := os.WriteFile(statePath, atFirstUpload, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Sessions[s.ID]; got == nil || got.Instance != "personal" {
		t.Fatalf("the state that survives the kill binds the session to %+v, want personal", got)
	}
	appendTo(t, path, betaRead) // its only scope now routes to `client`
	next := newAgent(t, e, twoInstances, &fakeRunner{}, &bytes.Buffer{})
	next.State = st
	sum := mustRun(t, next)
	if sum.Withdrawn != 1 || !slices.Contains(e.sink.withdrawn["personal"], s.ID) {
		t.Fatalf("the run after the kill withdrew nothing from the instance holding the prefix: %+v", sum)
	}
	full, _ := os.ReadFile(path)
	if got := spooled(t, e, "client", s.ID, "main"); len(got) != lineCount(full) || got[0].Src != "0" {
		t.Fatalf("the session was not re-shipped whole to the client instance: %d of %d records", len(got), lineCount(full))
	}
}

// TestASessionThatNeverLandedIsNotHeldByAFalseAnswer: a session BOUND for an instance of which no
// upload was ever acknowledged (the instance was not taking uploads) has nothing there to withdraw.
// The instance's `false` is then the truth, not a reason to hold: the session goes where its routing
// answer now says.
func TestASessionThatNeverLandedIsNotHeldByAFalseAnswer(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	s := w.Sessions["cc-plain"]
	path := filepath.Join(e.claude, s.Project, s.ID+".jsonl")
	a := newAgent(t, e, twoInstances, &fakeRunner{}, &bytes.Buffer{})
	e.sink.failRoot = s.ID // run 1: bound for `personal`, and every write of it fails
	if sum, err := a.Run(); err != nil || sum.Refused == 0 || a.State.Sessions[s.ID].Instance != "personal" {
		t.Fatalf("control: run 1 = %+v, %v, bound to %q — want refused and bound for personal", sum, err, a.State.Sessions[s.ID].Instance)
	}
	e.sink.failRoot = ""
	e.sink.notHeld = true // the instance never held it
	appendTo(t, path, betaRead)
	sum := mustRun(t, a)
	ss := a.State.Sessions[s.ID]
	if ss.Held || ss.Instance != "client" || sum.Held != 0 || len(spooled(t, e, "client", s.ID, "main")) == 0 {
		t.Fatalf("a session that never landed anywhere was held by a `false` answer: %+v, held=%v (%q), bound to %q",
			sum, ss.Held, ss.HeldReason, ss.Instance)
	}
}

// TestTheAgentWithdrawsThroughTheRealRoute is clause (m) over the real pod half (`internal/worker`
// and the archive): run 1 ships the session to the instance; after the routing answer changes, run 2
// sends the withdrawal, the instance answers `{"deleted": true}`, and its directory for the session
// is gone. A SECOND host's agent asking for the same root is answered `false` and holds.
func TestTheAgentWithdrawsThroughTheRealRoute(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	s := w.Sessions["cc-plain"]
	path := filepath.Join(e.claude, s.Project, s.ID+".jsonl")
	appendTo(t, path, strings.Replace(betaRead, "beta-notes", "alpha-notes", 1))
	p := newPod(t, podOpts{})
	var log bytes.Buffer
	a := newAgent(t, e, twoInstances, &fakeRunner{}, &log)
	sink := p.sink("host-a", sinkTokenA, 0)
	sink.Log = &log
	a.Sink = sink
	mustRun(t, a)
	if m, err := p.arc.ReadMeta(s.ID); err != nil || m == nil {
		t.Fatalf("control: run 1 stored nothing on the instance (%v)", err)
	}
	appendTo(t, path, betaRead)

	// Another host's agent, with its own state claiming the same prefix: `false`, held, nothing deleted.
	other := newAgent(t, e, twoInstances, &fakeRunner{}, &bytes.Buffer{})
	other.Sink = p.sink("host-b", sinkTokenB, 0)
	raw, err := json.Marshal(a.State) // host-a's own watermarks, as if this host had shipped them
	if err != nil {
		t.Fatal(err)
	}
	other.State = &State{}
	if err := json.Unmarshal(raw, other.State); err != nil {
		t.Fatal(err)
	}
	if sum := mustRun(t, other); sum.Held != 1 || sum.Withdrawn != 0 || !other.State.Sessions[s.ID].Held {
		t.Fatalf("host-b's withdrawal of host-a's root: %+v, held=%v — want `false`, held", sum, other.State.Sessions[s.ID].Held)
	}
	if m, _ := p.arc.ReadMeta(s.ID); m == nil {
		t.Fatal("host-b's withdrawal deleted host-a's root")
	}

	sum := mustRun(t, a)
	if sum.Withdrawn != 1 || sum.Held != 1 || sum.Records != 0 {
		t.Fatalf("run 2: %+v — want one withdrawal, held, no record shipped\n%s", sum, log.String())
	}
	if m, err := p.arc.ReadMeta(s.ID); err != nil || m != nil {
		t.Fatalf("the instance still holds the withdrawn session (%v, %v)", m != nil, err)
	}
}

// TestARefusedTokenEndsWithdrawalsForTheRun — a 401 (or 403) on a withdrawal stops the instance for
// the run, and that stop covers WITHDRAWALS too: every further request with a refused token counts
// toward the worker's lockout. A second withdrawal sends nothing. CONTROL: a 507 (quota) stops
// uploads and NOT withdrawals — a full instance is exactly when a prefix that must go should go.
func TestARefusedTokenEndsWithdrawalsForTheRun(t *testing.T) {
	for _, tc := range []struct {
		status, want int
	}{{http.StatusUnauthorized, 1}, {http.StatusForbidden, 1}, {http.StatusInsufficientStorage, 2}} {
		var requests int
		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			requests++
			http.Error(rw, "refused", tc.status)
		}))
		sink := &HTTPSink{Host: "host-a", Endpoints: map[string]Endpoint{"personal": {URL: srv.URL, Token: sinkTokenA}}, Log: &bytes.Buffer{}}
		for _, root := range []string{"s-0001", "s-0002"} {
			if deleted, err := sink.Withdraw("personal", root); deleted || err == nil {
				t.Errorf("%d: a withdrawal the instance refused answered %v %v", tc.status, deleted, err)
			}
		}
		srv.Close()
		if requests != tc.want {
			t.Errorf("after a %d, two withdrawals sent %d request(s), want %d", tc.status, requests, tc.want)
		}
	}
}

// TestARouteBackToAnInstanceThatDeletedTheSessionHoldsItWhereItIs — #273's audit F1. The session's
// prefix is on `personal`; its routing answer now names `client`, which holds a FINAL deletion of
// this root (it was withdrawn from there once, and a routing-TABLE edit sends it back). Red before:
// the agent withdrew from `personal` FIRST, then `client` answered 410 and the session was marked
// deleted — gone from both, captured nowhere. Now the destination is asked first: the session is
// HELD on `personal`, nothing is withdrawn, nothing is shipped, and it is said once.
// CONTROL (`TestTheAgentWithdrawsThroughTheRealRoute` and its siblings): a destination that did NOT
// delete it is withdrawn-then-shipped exactly as before.
func TestARouteBackToAnInstanceThatDeletedTheSessionHoldsItWhereItIs(t *testing.T) {
	// Run 1 ships the session (it touched `alpha-notes` only) to `personal`. Then the TABLE is edited:
	// `alpha-notes` now routes to `client`.
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	id := w.Sessions["cc-plain"].ID
	path := filepath.Join(e.claude, w.Sessions["cc-plain"].Project, id+".jsonl")
	appendTo(t, path, strings.Replace(betaRead, "beta-notes", "alpha-notes", 1))
	log := &bytes.Buffer{}
	a := newAgent(t, e, twoInstances, &fakeRunner{}, log)
	mustRun(t, a)
	if a.State.Sessions[id].Instance != "personal" {
		t.Fatal("control: run 1 did not ship the session to the personal instance")
	}
	a.Routing = client.Routing{Instances: twoInstances.Instances, RoutesSource: "the test's edited table",
		Routes: map[string]string{"alpha-notes": "client", "beta-notes": "client"}}
	appendTo(t, path, strings.Replace(betaRead, "beta-notes", "alpha-notes", 1))
	e.sink.deletedOn = map[string]bool{"client/" + id: true}
	before := len(spooled(t, e, "personal", id, "main"))
	sum := mustRun(t, a)
	ss := a.State.Sessions[id]
	if !ss.Held || ss.Instance != "personal" || ss.Deleted != "" || !strings.Contains(ss.HeldReason, "routed back to client") {
		t.Fatalf("held=%v (%q) bound to %q, deleted=%q — want HELD on personal, not deleted", ss.Held, ss.HeldReason, ss.Instance, ss.Deleted)
	}
	if n := len(e.sink.withdrawn["personal"]); n != 0 || sum.Withdrawn != 0 || sum.Deleted != 0 || sum.Held != 1 {
		t.Fatalf("%d withdrawals from the instance that holds the prefix; %+v — want none, and one hold", n, sum)
	}
	if got := len(spooled(t, e, "personal", id, "main")); got != before || e.sink.has("client", id) {
		t.Fatalf("the prefix on personal went %d → %d records, shipped to client=%v", before, got, e.sink.has("client", id))
	}
	if !slices.Contains(e.sink.probes, "client/"+id+"/main") {
		t.Fatalf("the destination was not asked: %v", e.sink.probes)
	}
	if n := strings.Count(log.String(), "HOLD "+id+": its routing answer now names instance client"); n != 1 {
		t.Fatalf("the hold is logged %d times, want once:\n%s", n, log.String())
	}
	// A later run does nothing more — and does not ask again.
	probes := len(e.sink.probes)
	if sum := mustRun(t, a); sum.Records != 0 || len(e.sink.withdrawn["personal"]) != 0 || len(e.sink.probes) != probes ||
		strings.Count(log.String(), "HOLD "+id) != 1 {
		t.Fatalf("a later run acted on the held session: %+v", sum)
	}
}

// TestThePositionProbeOfADeletedRootIsAnswered410ByTheRealPod — the signal the test above fakes is
// the real instance's: after its own (owner, host) withdrew a root, a position probe of that root
// (which stores nothing) is answered [ErrDeleted], and a root never stored is answered "".
func TestThePositionProbeOfADeletedRootIsAnswered410ByTheRealPod(t *testing.T) {
	p := newPod(t, podOpts{})
	sink := p.sink("host-a", sinkTokenA, 0)
	if at, err := sink.Position(up(""), "main"); err != nil || at != "" {
		t.Fatalf("CONTROL: a root never stored answered %q %v", at, err)
	}
	recs := synth(2, 200)
	if _, err := sink.Records(up(""), "main", recs); err != nil {
		t.Fatal(err)
	}
	if deleted, err := sink.Withdraw("personal", sinkRoot); !deleted || err != nil {
		t.Fatalf("the withdrawal: %v %v", deleted, err)
	}
	if _, err := sink.Position(up(""), "main"); !errors.Is(err, ErrDeleted) {
		t.Fatalf("a position probe of a withdrawn root answered %v, want ErrDeleted", err)
	}
	if m, _ := p.arc.ReadMeta(sinkRoot); m != nil {
		t.Fatal("the probe stored something")
	}
}
