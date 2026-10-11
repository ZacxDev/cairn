//go:build pgtest

// The CLIENT LISTENER (S2 of `claudedocs/plan-cairn-scope-mail.md`) against a real PostgreSQL —
// everything about it that a stub store cannot show. Two kinds of case, both in-process Go
// (decision 18: the e2e script with client exit codes is S3's):
//
//   - the REAL handler over the REAL `pgstore.MemoStore`, the REAL machine-token backend and a REAL
//     journal-backed `control.Cache`, served by `httptest`: the wire is rendered for a row planted
//     by SQL, every miss is one body, the id routes match the scope, and two credentials of one
//     principal are told apart and revoked separately;
//   - the REAL binary, re-exec'd as `cairn-ui` against a migrated database: the listener exists only
//     when configured, a cookie signed in on the browser listener next door is garbage to it, and
//     without a journal it refuses rather than serve the token file's projection.
//
// Each test gets a SCHEMA of its own (`schemaDSN`), so the memo rows of one cannot be another's
// "absent id", and nothing lands in `public`.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/clientapi"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/pgstore"
	"github.com/ZacxDev/cairn/internal/ui"
)

// schemaCounter makes each test's schema name unique within a run of this package.
var (
	schemaMu      sync.Mutex
	schemaCounter int
)

// schemaDSN creates a schema for THIS test and returns the tier's DSN pointed at it, dropping the
// schema when the test ends.
//
// ⚠ A SECOND SPELLING OF `internal/pgstore`'s harness (`nextSchema` + `withSearchPath`), which a
// `_test` package cannot export. What must not drift is the `options=-csearch_path=<schema>` form —
// attached, no space, because libpq splits `options` on whitespace — and that `public` is NOT on the
// path, so a missing table cannot resolve to a leftover one. The pgstore harness says why for both.
func schemaDSN(t *testing.T) string {
	t.Helper()
	dsn := requirePgtestDSN(t)
	schemaMu.Lock()
	schemaCounter++
	schema := fmt.Sprintf("capi%d_%d", schemaCounter, os.Getpid())
	schemaMu.Unlock()
	admin, err := sql.Open(pgstore.DriverName, dsn)
	if err != nil {
		t.Fatalf("could not vouch: opening an admin pool: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		t.Fatalf("could not vouch: creating schema %q: %v", schema, err)
	}
	t.Cleanup(func() {
		drop, err := sql.Open(pgstore.DriverName, dsn)
		if err != nil {
			t.Logf("leaked schema %q: %v", schema, err)
			return
		}
		defer drop.Close()
		if _, err := drop.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); err != nil {
			t.Logf("leaked schema %q: %v", schema, err)
		}
	})
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		return dsn + sep + "options=-csearch_path%3D" + schema
	}
	return dsn + " options=-csearch_path=" + schema
}

// The synthetic memo world: writer-a OWNS a project holding alpha-notes and beta-notes; reader-b
// reads alpha-notes; agents@host-a and agents@host-b are writer-a's, each narrowed to ONE scope and
// to read,write (decisions 16, 17).
var capiAt = time.Date(2000, 3, 4, 5, 6, 7, 0, time.UTC)

const (
	capiWriter = control.ID("usr_capiwriter")
	capiReader = control.ID("usr_capireader")
	capiProj   = control.ID("prj_capinotes")
	capiAlpha  = control.ID("scp_capialpha")
	capiBeta   = control.ID("scp_capibeta")

	capiTokWriter = "pgtest-capi-writer-full-credential-0000000000"
	capiTokReader = "pgtest-capi-reader-credential-000000000000000"
	capiTokAgentA = "pgtest-capi-agent-host-a-credential-000000000"
	capiTokAgentB = "pgtest-capi-agent-host-b-credential-000000000"
	capiGarbage   = "pgtest-capi-garbage-never-issued-000000000000"
)

func memoJournal(t *testing.T) (string, *control.FileStore) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memo.journal")
	fs, err := control.OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	rw := control.NewVerbSet(control.VerbRead, control.VerbWrite)
	cred := func(id, user control.ID, tok, label string, scopes []control.ID) control.Event {
		return control.Event{Kind: control.EventCredentialIssued, At: capiAt, CredentialID: id, SubjectKind: control.KindUser,
			SubjectID: user, TokenHash: control.HashToken(tok), Label: label, NarrowedScopes: scopes}
	}
	if _, err := fs.Append(context.Background(),
		control.Event{Kind: control.EventUserCreated, At: capiAt, UserID: capiWriter, Provider: "p", Subject: "capi-1", DisplayName: "writer-a"},
		control.Event{Kind: control.EventUserCreated, At: capiAt, UserID: capiReader, Provider: "p", Subject: "capi-2", DisplayName: "reader-b"},
		control.Event{Kind: control.EventProjectCreated, At: capiAt, ProjectID: capiProj, Name: "notes", UserID: capiWriter},
		control.Event{Kind: control.EventMemberSet, At: capiAt, ProjectID: capiProj, UserID: capiWriter, Role: control.RoleOwner},
		control.Event{Kind: control.EventScopeCreated, At: capiAt, ScopeID: capiAlpha, DisplayName: "alpha-notes", ProjectID: capiProj},
		control.Event{Kind: control.EventScopeCreated, At: capiAt, ScopeID: capiBeta, DisplayName: "beta-notes", ProjectID: capiProj},
		control.Event{Kind: control.EventGranted, At: capiAt, GrantID: "grt_capireader", SubjectKind: control.KindUser,
			SubjectID: capiReader, ObjectKind: control.ObjectScope, ObjectID: capiAlpha, Verbs: control.NewVerbSet(control.VerbRead)},
		cred("crd_capiwriter", capiWriter, capiTokWriter, "operator laptop", nil),
		cred("crd_capireader", capiReader, capiTokReader, "reader laptop", nil),
		cred("crd_capiagent_a", capiWriter, capiTokAgentA, "agents@host-a", []control.ID{capiAlpha}),
		control.Event{Kind: control.EventCredentialVerbsNarrowed, At: capiAt, CredentialID: "crd_capiagent_a", Verbs: rw},
		cred("crd_capiagent_b", capiWriter, capiTokAgentB, "agents@host-b", []control.ID{capiBeta}),
		control.Event{Kind: control.EventCredentialVerbsNarrowed, At: capiAt, CredentialID: "crd_capiagent_b", Verbs: rw},
	); err != nil {
		t.Fatal(err)
	}
	return path, fs
}

// listenerRig is the real handler over the real store, cache and backend.
type listenerRig struct {
	db      *pgstore.DB
	journal *control.FileStore
	cache   *control.Cache
	base    string
}

func newListenerRig(t *testing.T) *listenerRig {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgstore.Open(ctx, schemaDSN(t))
	if err != nil {
		t.Fatalf("could not vouch: opening the tier's database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, fs := memoJournal(t)
	cache := control.NewCache(fs, control.CacheOptions{})
	if err := cache.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	machine, err := identity.NewMachineToken(cache)
	if err != nil {
		t.Fatal(err)
	}
	scan, err := memo.NewScanner()
	if err != nil {
		t.Fatal(err)
	}
	store, err := pgstore.NewMemoStore(db, scan)
	if err != nil {
		t.Fatal(err)
	}
	h, err := clientapi.New(clientapi.Config{Auth: machine, Model: cache.Model, Memos: store,
		Limiter: netid.NewRateLimiter(1000, time.Minute, time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &listenerRig{db: db, journal: fs, cache: cache, base: srv.URL}
}

type wire struct {
	status int
	body   string
}

// call sends one request to a client listener at `base` and returns the status and the RAW body.
func call(t *testing.T, base, method, target, token, body string, headers ...string) wire {
	t.Helper()
	req, err := http.NewRequest(method, base+target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return wire{resp.StatusCode, string(b)}
}

func sendBody(scope, subject, body string) string {
	b, _ := json.Marshal(clientapi.SendRequest{Scope: scope, Subject: subject, Body: body})
	return string(b)
}

func mustSendVia(t *testing.T, base, token, scope string) int64 {
	t.Helper()
	got := call(t, base, http.MethodPost, clientapi.MemosPath, token, sendBody(scope, "schema change", "the rename ships next"))
	if got.status != http.StatusCreated {
		t.Fatalf("send to %s answered %d %s", scope, got.status, got.body)
	}
	var sent clientapi.SentBody
	if err := json.Unmarshal([]byte(got.body), &sent); err != nil || sent.ID < 1 {
		t.Fatalf("send answered %s (%v)", got.body, err)
	}
	return sent.ID
}

// TestTheListenerServesRenderedTextForARowPlantedBySQL — decision 19 on the wire, against the real
// store: a memo row planted by SQL — so nothing on the send path could have refused or cleaned it —
// carries U+200B in its subject and a variation-selector payload (U+E0100, U+FE0F) in its body; read
// through the listener's list AND one-memo routes, the RAW response bytes contain none of them (as
// UTF-8 or as a `\u` escape) and do contain U+FFFD. RED with the listener encoding `memo.Stored`,
// which has no exported field and so encodes as `{}`.
func TestTheListenerServesRenderedTextForARowPlantedBySQL(t *testing.T) {
	r := newListenerRig(t)
	var id int64
	if err := r.db.SQL().QueryRow(`INSERT INTO memos (scope_name, sender_kind, sender_id, sender_credential_id,
	        sender_display_at_send, subject, body, created_at, expires_at)
	      VALUES ('alpha-notes', 'user', $1, 'crd_capiagent_a', 'writer-a', $2, $3, now(), now() + interval '1 hour')
	      RETURNING id`, string(capiWriter), "deploy\u200bnotes", "use the\U000E0100 rename\ufe0f next").Scan(&id); err != nil {
		t.Fatal(err)
	}
	hostile := map[string]string{"\u200b": `\u200b`, "\U000E0100": `\udb40\udd00`, "\ufe0f": `\ufe0f`}
	for _, target := range []string{clientapi.MemosPath + "?scope=alpha-notes", fmt.Sprintf("%s?scope=alpha-notes&id=%d", clientapi.MemoPath, id)} {
		got := call(t, r.base, http.MethodGet, target, capiTokReader, "")
		if got.status != http.StatusOK {
			t.Fatalf("%s: %d %s", target, got.status, got.body)
		}
		raw := []byte(got.body)
		for cp, esc := range hostile {
			if bytes.Contains(raw, []byte(cp)) || bytes.Contains(bytes.ToLower(raw), []byte(esc)) {
				t.Errorf("%s: the raw response carries U+%04X: %s", target, []rune(cp)[0], raw)
			}
		}
		if !bytes.Contains(raw, []byte("\ufffd")) {
			t.Errorf("%s: no U+FFFD in the raw response — the planted code points were not rendered: %s", target, raw)
		}
		if !bytes.Contains(raw, []byte(`"Sender":"writer-a via agents@host-a (user)"`)) {
			t.Errorf("%s: the rendered sender line is absent: %s", target, raw)
		}
	}
}

// TestEveryMissOnTheClientListenerIsOneBody — T12 and decision 4: an unreadable scope, an absent
// scope, an unknown id, an id under the WRONG scope (asked by a caller who reads both), and a retract
// of each, all answer the same status and the byte-identical body. The send route's refusals for an
// unreadable, an absent and a read-only scope are likewise one body. POSITIVE CONTROL: the right
// (scope, id) by a reader answers the memo.
func TestEveryMissOnTheClientListenerIsOneBody(t *testing.T) {
	r := newListenerRig(t)
	id := mustSendVia(t, r.base, capiTokWriter, "alpha-notes")
	// A memo that EXISTS in the scope reader-b cannot read, asked for under its own scope — so the
	// "unreadable" arms below are answered by the read check and not by the scope match.
	inBeta := mustSendVia(t, r.base, capiTokWriter, "beta-notes")
	if got := call(t, r.base, http.MethodGet, fmt.Sprintf("%s?scope=alpha-notes&id=%d", clientapi.MemoPath, id), capiTokReader, ""); got.status != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL: the reader cannot read the memo: %d %s", got.status, got.body)
	}
	retract := func(scope string, id int64) string {
		b, _ := json.Marshal(clientapi.RetractRequest{Scope: scope, ID: id})
		return string(b)
	}
	misses := map[string]wire{
		"list, unreadable scope":   call(t, r.base, http.MethodGet, clientapi.MemosPath+"?scope=beta-notes", capiTokReader, ""),
		"list, absent scope":       call(t, r.base, http.MethodGet, clientapi.MemosPath+"?scope=gamma-notes", capiTokWriter, ""),
		"get, unreadable scope":    call(t, r.base, http.MethodGet, fmt.Sprintf("%s?scope=beta-notes&id=%d", clientapi.MemoPath, inBeta), capiTokReader, ""),
		"get, absent scope":        call(t, r.base, http.MethodGet, fmt.Sprintf("%s?scope=gamma-notes&id=%d", clientapi.MemoPath, id), capiTokWriter, ""),
		"get, unknown id":          call(t, r.base, http.MethodGet, fmt.Sprintf("%s?scope=alpha-notes&id=%d", clientapi.MemoPath, id+1000), capiTokWriter, ""),
		"get, the WRONG scope":     call(t, r.base, http.MethodGet, fmt.Sprintf("%s?scope=beta-notes&id=%d", clientapi.MemoPath, id), capiTokWriter, ""),
		"retract, unreadable":      call(t, r.base, http.MethodPost, clientapi.RetractPath, capiTokReader, retract("beta-notes", inBeta)),
		"retract, absent scope":    call(t, r.base, http.MethodPost, clientapi.RetractPath, capiTokWriter, retract("gamma-notes", id)),
		"retract, unknown id":      call(t, r.base, http.MethodPost, clientapi.RetractPath, capiTokWriter, retract("alpha-notes", id+1000)),
		"retract, the WRONG scope": call(t, r.base, http.MethodPost, clientapi.RetractPath, capiTokWriter, retract("beta-notes", id)),
	}
	want := wire{http.StatusNotFound, clientapi.NotFoundBody}
	for what, got := range misses {
		if got != want {
			t.Errorf("%s: %d %q, want %d %q", what, got.status, got.body, want.status, want.body)
		}
	}
	sends := map[string]wire{
		"send, unreadable scope": call(t, r.base, http.MethodPost, clientapi.MemosPath, capiTokReader, sendBody("beta-notes", "s", "b")),
		"send, absent scope":     call(t, r.base, http.MethodPost, clientapi.MemosPath, capiTokWriter, sendBody("gamma-notes", "s", "b")),
		"send, read-only scope":  call(t, r.base, http.MethodPost, clientapi.MemosPath, capiTokReader, sendBody("alpha-notes", "s", "b")),
	}
	wantSend := wire{http.StatusForbidden, `{"error":"refused","reason":"no-write"}` + "\n"}
	for what, got := range sends {
		if got != wantSend {
			t.Errorf("%s: %d %q, want %d %q", what, got.status, got.body, wantSend.status, wantSend.body)
		}
	}
	if got := call(t, r.base, http.MethodGet, fmt.Sprintf("%s?scope=alpha-notes&id=%d", clientapi.MemoPath, id), capiTokReader, ""); got.status != http.StatusOK || strings.Contains(got.body, "retracted by") {
		t.Fatalf("after the refused retractions the memo is not live and unretracted: %d %s", got.status, got.body)
	}
}

// TestTheIdRoutesMatchTheScope — round 1 🔴3 at the listener: a memo in alpha-notes asked for as
// `?scope=beta-notes&id=<its id>` by a caller who reads BOTH is the uniform not-found, byte-identical
// to an absent id; the same request with `scope=alpha-notes` is the memo. Retract the same way: under
// beta-notes it is not-found and the memo stays live; under alpha-notes it is retracted and reads back
// as a tombstone. RED with the store's `m.scope_name = $2` clause dropped from `Get`.
func TestTheIdRoutesMatchTheScope(t *testing.T) {
	r := newListenerRig(t)
	id := mustSendVia(t, r.base, capiTokWriter, "alpha-notes")
	absent := call(t, r.base, http.MethodGet, fmt.Sprintf("%s?scope=beta-notes&id=%d", clientapi.MemoPath, id+1000), capiTokWriter, "")
	wrong := call(t, r.base, http.MethodGet, fmt.Sprintf("%s?scope=beta-notes&id=%d", clientapi.MemoPath, id), capiTokWriter, "")
	if wrong != absent || wrong.status != http.StatusNotFound {
		t.Fatalf("the id under the wrong scope answered %d %q; an absent id answers %d %q", wrong.status, wrong.body, absent.status, absent.body)
	}
	right := call(t, r.base, http.MethodGet, fmt.Sprintf("%s?scope=alpha-notes&id=%d", clientapi.MemoPath, id), capiTokWriter, "")
	var one clientapi.MemoBody
	if right.status != http.StatusOK || json.Unmarshal([]byte(right.body), &one) != nil || one.Memo.ID != id || one.Memo.Scope != "alpha-notes" {
		t.Fatalf("POSITIVE CONTROL: the right (scope, id) answered %d %s", right.status, right.body)
	}
	retract := func(scope string) wire {
		b, _ := json.Marshal(clientapi.RetractRequest{Scope: scope, ID: id})
		return call(t, r.base, http.MethodPost, clientapi.RetractPath, capiTokWriter, string(b))
	}
	if got := retract("beta-notes"); got != absent {
		t.Fatalf("a retract under the wrong scope answered %d %q, want the uniform miss", got.status, got.body)
	}
	if got := call(t, r.base, http.MethodGet, fmt.Sprintf("%s?scope=alpha-notes&id=%d", clientapi.MemoPath, id), capiTokWriter, ""); strings.Contains(got.body, "retracted by") {
		t.Fatalf("a retract under the WRONG scope retracted the memo: %s", got.body)
	}
	if got := retract("alpha-notes"); got.status != http.StatusOK || got.body != fmt.Sprintf(`{"retracted":%d}`+"\n", id) {
		t.Fatalf("the retract under the right scope answered %d %q", got.status, got.body)
	}
	got := call(t, r.base, http.MethodGet, fmt.Sprintf("%s?scope=alpha-notes&id=%d", clientapi.MemoPath, id), capiTokWriter, "")
	if got.status != http.StatusOK || !strings.Contains(got.body, `"Tombstone":"retracted by writer-a via operator laptop (user) at `) ||
		strings.Contains(got.body, "the rename ships next") {
		t.Fatalf("after retraction the memo does not read back as a tombstone: %d %s", got.status, got.body)
	}
}

// TestTwoCredentialsOfOnePrincipalOnTheClientListener — O9 at the listener: agents@host-a (alpha-notes)
// and agents@host-b (beta-notes) are ONE principal's two credentials. Each sends to and reads only its
// own scope; their memos store the same `sender_kind`/`sender_id` and DIFFERENT
// `sender_credential_id`, and render `via <label>` on the wire (RED with the label dropped from the
// render). Then host-a is revoked and ONE `control.Cache.Refresh` is forced: its token answers exactly
// what a random token answers, and host-b keeps working.
func TestTwoCredentialsOfOnePrincipalOnTheClientListener(t *testing.T) {
	r := newListenerRig(t)
	a := mustSendVia(t, r.base, capiTokAgentA, "alpha-notes")
	b := mustSendVia(t, r.base, capiTokAgentB, "beta-notes")
	noWrite := wire{http.StatusForbidden, `{"error":"refused","reason":"no-write"}` + "\n"}
	if got := call(t, r.base, http.MethodPost, clientapi.MemosPath, capiTokAgentA, sendBody("beta-notes", "s", "b")); got != noWrite {
		t.Errorf("host-a sending to beta-notes: %d %q", got.status, got.body)
	}
	if got := call(t, r.base, http.MethodPost, clientapi.MemosPath, capiTokAgentB, sendBody("alpha-notes", "s", "b")); got != noWrite {
		t.Errorf("host-b sending to alpha-notes: %d %q", got.status, got.body)
	}
	miss := wire{http.StatusNotFound, clientapi.NotFoundBody}
	if got := call(t, r.base, http.MethodGet, clientapi.MemosPath+"?scope=beta-notes", capiTokAgentA, ""); got != miss {
		t.Errorf("host-a reading beta-notes: %d %q", got.status, got.body)
	}
	if got := call(t, r.base, http.MethodGet, clientapi.MemosPath+"?scope=alpha-notes", capiTokAgentB, ""); got != miss {
		t.Errorf("host-b reading alpha-notes: %d %q", got.status, got.body)
	}
	var senders, creds int
	if err := r.db.SQL().QueryRow(`SELECT count(DISTINCT (sender_kind, sender_id)), count(DISTINCT sender_credential_id) FROM memos`).Scan(&senders, &creds); err != nil {
		t.Fatal(err)
	}
	if senders != 1 || creds != 2 {
		t.Fatalf("%d distinct senders and %d distinct credentials stored, want 1 and 2", senders, creds)
	}
	for scope, want := range map[string]string{
		"alpha-notes": fmt.Sprintf(`"ID":%d,"Scope":"alpha-notes","Sender":"writer-a via agents@host-a (user)"`, a),
		"beta-notes":  fmt.Sprintf(`"ID":%d,"Scope":"beta-notes","Sender":"writer-a via agents@host-b (user)"`, b),
	} {
		got := call(t, r.base, http.MethodGet, clientapi.MemosPath+"?scope="+scope, capiTokWriter, "")
		if got.status != http.StatusOK || !strings.Contains(got.body, want) {
			t.Errorf("listing %s: %d %s\nwant it to carry %s", scope, got.status, got.body, want)
		}
	}
	if _, err := control.RevokeCredential(context.Background(), r.journal, "crd_capiagent_a", capiWriter, capiAt); err != nil {
		t.Fatal(err)
	}
	if err := r.cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	revoked := call(t, r.base, http.MethodGet, clientapi.MemosPath+"?scope=alpha-notes", capiTokAgentA, "")
	random := call(t, r.base, http.MethodGet, clientapi.MemosPath+"?scope=alpha-notes", capiGarbage, "")
	if revoked.status != http.StatusUnauthorized || revoked != random {
		t.Fatalf("the revoked credential answered %d %q; a random token answers %d %q", revoked.status, revoked.body, random.status, random.body)
	}
	if got := call(t, r.base, http.MethodGet, clientapi.MemosPath+"?scope=beta-notes", capiTokAgentB, ""); got.status != http.StatusOK {
		t.Fatalf("host-b stopped working when host-a was revoked: %d %s", got.status, got.body)
	}
}

// ---------------------------------------------------------------- the binary

var clientAPILine = regexp.MustCompile(`client API on (127\.0\.0\.1:\d+) \(4 route\(s\), machine tokens only\)`)

func startMemoChild(t *testing.T, journal, dsn string, extra ...string) *presenceChild {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	args := append([]string{
		"-control-journal", journal, "-db-dsn", dsn,
		"-store", t.TempDir(), "-token-file", filepath.Join(t.TempDir(), "absent-token"),
		"-host", "127.0.0.1", "-port", "0",
	}, extra...)
	cmd := exec.CommandContext(ctx, self, args...)
	cmd.Env = []string{reexecEnv + "=1"}
	return startChild(t, cmd, cancel, &syncBuffer{}, &syncBuffer{})
}

func (c *presenceChild) clientAPIBase(t *testing.T) string {
	t.Helper()
	m := clientAPILine.FindStringSubmatch(c.out.String())
	if m == nil || strings.HasSuffix(m[1], ":0") {
		t.Fatalf("the startup line does not announce the client API listener on a bound loopback port:\n%s", c.out.String())
	}
	return "http://" + m[1]
}

// TestTheClientListenerExistsOnlyWhenConfigured — the S2 listener bullets on the real binary: with
// `-client-api-addr` a THIRD listener answers the memo routes (a machine token sends 201 and lists its
// memo — the POSITIVE CONTROL — and garbage is 401); without it the startup line says the client API
// is off and the same port is connection-refused.
func TestTheClientListenerExistsOnlyWhenConfigured(t *testing.T) {
	journal, _ := memoJournal(t)
	dsn := schemaDSN(t)
	on := startMemoChild(t, journal, dsn, "-client-api-addr", "127.0.0.1:0")
	on.serving(t)
	base := on.clientAPIBase(t)
	id := mustSendVia(t, base, capiTokAgentA, "alpha-notes")
	got := call(t, base, http.MethodGet, clientapi.MemosPath+"?scope=alpha-notes", capiTokAgentA, "")
	if got.status != http.StatusOK || !strings.Contains(got.body, fmt.Sprintf(`"ID":%d,`, id)) {
		t.Fatalf("POSITIVE CONTROL: the sent memo does not list back: %d %s", got.status, got.body)
	}
	if got := call(t, base, http.MethodGet, clientapi.MemosPath+"?scope=alpha-notes", capiGarbage, ""); got.status != http.StatusUnauthorized {
		t.Fatalf("garbage answered %d", got.status)
	}
	on.cancel()
	<-on.done

	off := startMemoChild(t, journal, dsn)
	off.serving(t)
	if !strings.Contains(off.out.String(), "client API off (no -client-api-addr)") {
		t.Fatalf("an unconfigured child does not say the client API is off:\n%s", off.out.String())
	}
	if conn, err := net.DialTimeout("tcp", strings.TrimPrefix(base, "http://"), time.Second); err == nil {
		_ = conn.Close()
		t.Fatalf("something answers on %s with no -client-api-addr", base)
	}
}

// TestACookieSignedInNextDoorIsGarbageToTheClientListener — decision 3 on the real binary: a browser
// session signed in on THIS process's browser listener (a real cookie) is presented to the client
// listener and answers EXACTLY what a garbage token answers; so does a JWT-shaped bearer. RED with
// `main` handing the browser's `chain` (machine token, provider JWT, cookie) to the client listener —
// the cookie then authenticates. POSITIVE CONTROLS: the cookie IS a live session on the browser
// listener, and the machine token answers 200 here.
//
// ⚠ THE JWT ARM IS AN INVARIANT GUARD: no provider is armed in this child, so a JWT is garbage to
// EVERY listener of it, and the arm cannot go red under that mutant. It is kept for the byte-identity
// of the refusal, and labelled so it is not counted as coverage of the provider path.
func TestACookieSignedInNextDoorIsGarbageToTheClientListener(t *testing.T) {
	journal, _ := memoJournal(t)
	c := startMemoChild(t, journal, schemaDSN(t), "-client-api-addr", "127.0.0.1:0")
	browser := c.serving(t)
	base := c.clientAPIBase(t)

	noRedirect := &http.Client{Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest(http.MethodPost, browser+ui.SignInPath,
		strings.NewReader(url.Values{ui.FieldToken: {capiTokWriter}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", browser)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}
	resp.Body.Close()
	var cookies []string
	for _, ck := range resp.Cookies() {
		cookies = append(cookies, ck.Name+"="+ck.Value)
	}
	if resp.StatusCode != http.StatusSeeOther || len(cookies) == 0 {
		t.Fatalf("POSITIVE CONTROL: sign-in answered %d with %d cookie(s)", resp.StatusCode, len(cookies))
	}
	cookie := strings.Join(cookies, "; ")
	page, _ := http.NewRequest(http.MethodGet, browser+ui.RootPath, nil)
	page.Header.Set("Cookie", cookie)
	if got, err := noRedirect.Do(page); err != nil || got.StatusCode != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL: the cookie is not a live browser session (%v, %v)", got, err)
	} else {
		got.Body.Close()
	}

	target := clientapi.MemosPath + "?scope=alpha-notes"
	garbage := call(t, base, http.MethodGet, target, capiGarbage, "")
	if garbage.status != http.StatusUnauthorized {
		t.Fatalf("garbage answered %d", garbage.status)
	}
	if got := call(t, base, http.MethodGet, target, "", "", "Cookie", cookie); got != garbage {
		t.Fatalf("a browser session cookie answered %d %q on the client listener; garbage answers %d %q",
			got.status, got.body, garbage.status, garbage.body)
	}
	jwt := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJjYXBpLTEifQ.c2lnbmF0dXJl"
	if got := call(t, base, http.MethodGet, target, jwt, ""); got != garbage {
		t.Fatalf("a JWT-shaped bearer answered %d %q; garbage answers %d %q", got.status, got.body, garbage.status, garbage.body)
	}
	if got := call(t, base, http.MethodGet, target, capiTokWriter, ""); got.status != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL: the machine token answered %d %s", got.status, got.body)
	}
}

// TestTheClientListenerNeverServesTheTokenFileProjection — the S2 plan's named RED (round 1 🟡1),
// with a database that ANSWERS: `-client-api-addr` and `-db-dsn` with a VALID token file and NO
// `-control-journal` exits 78 naming the journal. On a build without the refusal this child does not
// exit: `openAuthority` takes the token-file branch, the database opens, and the client listener comes
// up authenticating the token file's shared row — O5, which O9 reversed.
func TestTheClientListenerNeverServesTheTokenFileProjection(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(strings.Repeat("s", 43)), 0o600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	cmd := exec.CommandContext(ctx, self, "-token-file", tokenFile, "-db-dsn", schemaDSN(t),
		"-client-api-addr", "127.0.0.1:0", "-store", t.TempDir(),
		"-session-file", filepath.Join(t.TempDir(), "sessions"), "-host", "127.0.0.1", "-port", "0")
	cmd.Env = []string{reexecEnv + "=1"}
	c := startChild(t, cmd, cancel, &syncBuffer{}, &syncBuffer{})
	if code := c.exit(t); code != exitConfig {
		t.Fatalf("exit %d, want %d:\n%s", code, exitConfig, c.out.String())
	}
	if !strings.Contains(c.out.String(), "needs -control-journal") {
		t.Fatalf("the refusal does not name the missing journal:\n%s", c.out.String())
	}
}
