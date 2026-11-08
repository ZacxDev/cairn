package capture

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/client"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
)

// F1's evidence rides EVERY upload (#312 round 1). The pod takes `names_program` from the stored
// tool-INPUT records it derives over; a blob is derived as content only. So when the one stream
// whose tool input names the program fails to upload while a blob and the rest still ship, the pod
// never learns F1's evidence, applies no F1 (`archive.Meta.LedgerScopes`), and `V` is whatever the
// headers named — readable by a non-owner who reads those scopes. The agent knows the evidence
// (`Evidence.NamesProgram`); every upload now carries it and the pod ORs it in.

// failStream is the real sink, except that every records write to ONE stream fails before
// anything is sent — an unreachable moment for exactly that stream.
type failStream struct {
	*HTTPSink
	stream string
}

func (f failStream) Records(u Upload, stream string, recs []Record) (int, error) {
	if stream == f.stream {
		return 0, errors.New("fixture: this stream's upload failed")
	}
	return f.HTTPSink.Records(u, stream, recs)
}

// f1Reader is a control model with ONE principal who is not the uploader and reads alpha-notes,
// as `transcript.Viewer` takes it.
func f1Reader(t *testing.T) transcript.Viewer {
	t.Helper()
	at := time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
	reader := control.DerivedID(control.PrefixUser, "f1wire-reader")
	founder := control.DerivedID(control.PrefixUser, "f1wire-founder")
	project := control.DerivedID(control.PrefixProject, "f1wire-alpha")
	events := []control.Event{
		{Kind: control.EventUserCreated, At: at, UserID: reader, Provider: "fixture-provider",
			Subject: "00000000-0000-4000-8000-0000000000f1", Email: "reader@notes.example.invalid"},
		{Kind: control.EventUserCreated, At: at, UserID: founder, Provider: "fixture-provider",
			Subject: "00000000-0000-4000-8000-0000000000f2", Email: "founder@notes.example.invalid"},
		{Kind: control.EventProjectCreated, At: at, ProjectID: project, Name: "p-alpha-notes", UserID: founder},
		{Kind: control.EventScopeCreated, At: at, ScopeID: control.DerivedID(control.PrefixScope, "f1wire-alpha"),
			DisplayName: "alpha-notes", ProjectID: project},
		{Kind: control.EventMemberSet, At: at, ProjectID: project, UserID: reader, Role: control.RoleOwner},
	}
	m, err := control.Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	p, known := m.PrincipalFor(control.KindUser, reader)
	if !known {
		t.Fatal("the fixture: no reader principal")
	}
	v := transcript.Viewer{Principal: p, Auth: control.Resolve(m, p)}
	// POSITIVE CONTROL: this viewer DOES see another owner's session whose V is [alpha-notes], so a
	// refusal below is the session's `V`, not a viewer who can see nothing.
	if !transcript.Visible(v, &archive.Meta{Owner: "user:someone-else"}, []string{"alpha-notes"}) {
		t.Fatal("the fixture: the reader cannot read alpha-notes at all, so a refusal would prove nothing")
	}
	return v
}

// f1BlobWorld is the F1 fixture session (a tool input names `cairn`, its ledger is EMPTY) plus one
// persisted TEXT tool result whose header names alpha-notes and which then carries output no
// header names — what an unledgered `cairn` call printed.
func f1BlobWorld(t *testing.T) (env, string) {
	t.Helper()
	return f1BlobWorldNaming(t, "alpha-notes")
}

func f1BlobWorldNaming(t *testing.T, scope string) (env, string) {
	t.Helper()
	w := loadWorld(t)
	e := materialize(t, w, "cc-f1-empty-ledger")
	s := w.Sessions["cc-f1-empty-ledger"]
	dir := filepath.Join(e.claude, s.Project, s.ID, "tool-results")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	blob := "subsystem-recall: status=recalled scope=" + scope + " entries=1\n- 2000-01-01: synthetic entry\n\n" +
		"lorem ipsum output that no header names\n"
	if err := os.WriteFile(filepath.Join(dir, "toolu_F1wireBlob0000000000000.txt"), []byte(blob), 0o600); err != nil {
		t.Fatal(err)
	}
	return e, s.ID
}

// TestAFailedMainUploadStillCarriesF1ToThePod is the round-1 🔴 as a regression test, through the
// REAL pod (worker decoder over the archive): the main stream — the only record naming the program
// — fails, the blob ships, and a NON-OWNER reader of alpha-notes must not see the session. On ONE
// instance (no `*` is ever declared there) and on TWO (#309 stopped declaring F1's `*`).
//
// RED before the fix at the round-1 head (both configurations) and at origin/main with one
// instance: the pod holds names_program=false and V=[alpha-notes].
func TestAFailedMainUploadStillCarriesF1ToThePod(t *testing.T) {
	reader := f1Reader(t)
	for _, tc := range []struct {
		name    string
		routing client.Routing
	}{{"one instance", oneInstance}, {"two instances", twoInstances}} {
		t.Run(tc.name, func(t *testing.T) {
			e, id := f1BlobWorld(t)
			p := newPod(t, podOpts{})
			var log bytes.Buffer
			a := newAgent(t, e, tc.routing, &fakeRunner{}, &log)
			s := p.sink("host-a", sinkTokenA, 0)
			s.Log = &log
			a.Sink = failStream{HTTPSink: s, stream: "main"}
			_, _ = a.Run() // the session's own failure is counted, not returned

			ss := a.State.Sessions[id]
			if ss == nil || !ss.Evidence.NamesProgram || ss.Instance != "personal" {
				t.Fatalf("the fixture: the agent must hold F1's evidence and bind the session to personal: %+v\n%s", ss, log.String())
			}
			meta, err := p.arc.ReadMeta(id)
			if err != nil {
				t.Fatal(err)
			}
			if meta == nil || len(meta.Blobs) == 0 || len(meta.Streams["main"].StoredTo) != 0 {
				t.Fatalf("the fixture: the blob must be stored and main must not: %+v\n%s", meta, log.String())
			}
			v := transcript.VisibilitySet(meta, transcript.Written{})
			if transcript.Visible(reader, meta, v) {
				t.Fatalf("a NON-OWNER reader of alpha-notes sees an F1 session whose main stream never landed: "+
					"pod V %v (names_program %v, ledger records %d, declared %v)",
					v, meta.NamesProgram, meta.LedgerRecords, meta.Declared)
			}

			// INVARIANT GUARD, not regression coverage (#309 under the new field): once the ledger
			// line lands naming the session's one read, F1 lifts on the pod — the agent's
			// `names_program` keeps arriving and must not freeze `*` the way a declared `*` did. It
			// has not been shown red against any build.
			a.Sink = s
			if err := os.WriteFile(filepath.Join(e.ledger, id+".jsonl"), []byte(ledgerLine(id, "recall", "alpha-notes")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			mustRun(t, a)
			if meta, err = p.arc.ReadMeta(id); err != nil {
				t.Fatal(err)
			}
			v = transcript.VisibilitySet(meta, transcript.Written{})
			if meta.LedgerRecords == 0 || len(meta.Streams["main"].StoredTo) == 0 || !transcript.Visible(reader, meta, v) {
				t.Fatalf("run 2 (ledger landed, main shipped): the reader of alpha-notes is refused — pod V %v, "+
					"ledger records %d, declared %v\n%s", v, meta.LedgerRecords, meta.Declared, log.String())
			}
		})
	}
}

// npSink records, for every write the agent makes, whether it carried `NamesProgram`.
type npSink struct {
	Sink
	writes map[string][]bool // stream, or "blob:"+name
}

func (s *npSink) Records(u Upload, stream string, recs []Record) (int, error) {
	s.writes[stream] = append(s.writes[stream], u.NamesProgram)
	return s.Sink.Records(u, stream, recs)
}

func (s *npSink) Blob(u Upload, name, to string, data []byte) error {
	s.writes["blob:"+name] = append(s.writes["blob:"+name], u.NamesProgram)
	return s.Sink.Blob(u, name, to, data)
}

// assertEveryWriteCarried requires at least `want` distinct writes (streams and blobs), every one
// of which carried NamesProgram.
func assertEveryWriteCarried(t *testing.T, s *npSink, want ...string) {
	t.Helper()
	for _, k := range want {
		if len(s.writes[k]) == 0 {
			t.Fatalf("the fixture: no %s write was made (writes: %v)", k, s.writes)
		}
	}
	for k, seen := range s.writes {
		for i, np := range seen {
			if !np {
				t.Errorf("write %d of %s did not carry the session's F1 evidence (names_program)", i, k)
			}
		}
	}
}

// TestEveryUploadOfAnF1SessionCarriesItsEvidence: the Claude Code site (its main stream, its blob
// and, once it lands, its ledger stream) and the opencode site (every stream of the root). Whichever
// upload lands first must carry `names_program`, so EVERY one does.
func TestEveryUploadOfAnF1SessionCarriesItsEvidence(t *testing.T) {
	t.Run("claude", func(t *testing.T) {
		e, id := f1BlobWorld(t)
		s := &npSink{Sink: e.sink, writes: map[string][]bool{}}
		a := newAgent(t, e, oneInstance, &fakeRunner{}, &bytes.Buffer{})
		a.Sink = s
		mustRun(t, a)
		if err := os.WriteFile(filepath.Join(e.ledger, id+".jsonl"), []byte(ledgerLine(id, "recall", "alpha-notes")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		mustRun(t, a)
		assertEveryWriteCarried(t, s, "main", LedgerStream, "blob:tool-results/toolu_F1wireBlob0000000000000.txt")
	})
	t.Run("opencode", func(t *testing.T) {
		w := loadWorld(t)
		e := materialize(t, w, "oc-root")
		s := &npSink{Sink: e.sink, writes: map[string][]bool{}}
		a := newAgent(t, e, oneInstance, &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList}, &bytes.Buffer{})
		a.Sink, a.ClaudeRoot, a.OpencodeDirs = s, "", []string{"/work/alpha"}
		mustRun(t, a)
		if ss := a.State.Sessions[w.Sessions["oc-root"].ID]; ss == nil || !ss.Evidence.NamesProgram {
			t.Fatalf("the fixture: the opencode root must carry F1 evidence: %+v", ss)
		}
		assertEveryWriteCarried(t, s, "main")
	})
}

// TestAnF1SessionWhoseHeaderNamesTheOtherInstance (round 1's 🟢 F2): F1 beside ONE header scope of
// the NON-default instance, on a two-instance host, through two real pods. Run 1: F1 forces the
// default instance, owner-only, and the `*` is NOT declared (#309) — so it is the pod's F1, fed by
// `names_program`, that keeps a reader of beta-notes out. Run 2: the ledger names beta-notes, and
// the session MOVES — withdrawn from personal, shipped whole to client, no `*` anywhere.
func TestAnF1SessionWhoseHeaderNamesTheOtherInstance(t *testing.T) {
	e, id := f1BlobWorldNaming(t, "beta-notes")
	personal, clientPod := newPod(t, podOpts{}), newPod(t, podOpts{})
	var log bytes.Buffer
	a := newAgent(t, e, twoInstances, &fakeRunner{}, &log)
	a.Sink = &HTTPSink{Host: "host-a", Log: &log, Endpoints: map[string]Endpoint{
		"personal": {URL: personal.srv.URL, Token: sinkTokenA}, "client": {URL: clientPod.srv.URL, Token: sinkTokenA}}}
	mustRun(t, a)
	meta, err := personal.arc.ReadMeta(id)
	if err != nil || meta == nil {
		t.Fatalf("run 1: nothing on personal (%v)\n%s", err, log.String())
	}
	v := transcript.VisibilitySet(meta, transcript.Written{})
	if slices.Contains(meta.Declared, "*") || !meta.NamesProgram || !slices.Contains(v, "*") || !slices.Contains(v, "beta-notes") {
		t.Fatalf("run 1 on personal: declared %v, names_program %v, V %v — want no declared `*`, F1 from "+
			"names_program, V ⊇ {*, beta-notes}", meta.Declared, meta.NamesProgram, v)
	}
	if m, _ := clientPod.arc.ReadMeta(id); m != nil {
		t.Fatal("run 1 shipped the F1 session to the non-default instance")
	}

	if err := os.WriteFile(filepath.Join(e.ledger, id+".jsonl"), []byte(ledgerLine(id, "recall", "beta-notes")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, a)
	if m, _ := personal.arc.ReadMeta(id); m != nil {
		t.Fatalf("run 2: the prefix is still on personal\n%s", log.String())
	}
	moved, err := clientPod.arc.ReadMeta(id)
	if err != nil || moved == nil || len(moved.Streams["main"].StoredTo) == 0 || len(moved.Blobs) == 0 {
		t.Fatalf("run 2: the session did not move whole to client: %+v %v\n%s", moved, err, log.String())
	}
	if v := transcript.VisibilitySet(moved, transcript.Written{}); slices.Contains(v, "*") || !slices.Equal(moved.Declared, []string{"beta-notes"}) {
		t.Fatalf("run 2 on client: V %v, declared %v — want no `*` and [beta-notes] declared", v, moved.Declared)
	}
}
