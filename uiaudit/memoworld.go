package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/memowire"
	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 THE THIRD WORLD: A DATABASE AND A CONTROL JOURNAL, SO THE MEMOS TAB HAS A STORE.
//
// Neither world the walk boots has a database, so `?tab=memos` only ever rendered
// `ui.NoMemoStore` in a browser: the send form, the Retract button and a tombstone had never been
// laid out, typed into or clicked. This world is the deployment shape the tab needs — `-db-dsn`,
// `-control-journal` and a client listener — and `memos_browser_test.go` drives it.
//
// 🔴 THIS MODULE STILL IMPORTS NO DATABASE DRIVER, AND THAT IS THE DESIGN RATHER THAN A WORKAROUND.
// `internal/ui`'s own comment says why it may not import `internal/pgstore`: the driver would enter
// this nested module's graph. So nothing here opens a connection. The DSN is handed to the REAL
// `cairn-ui` binary, which migrates the schema itself at start; the seeded memos are SENT through
// that binary's own client listener over HTTP, the way an agent sends one; and everything else is
// read off the page. The schema the DSN names is created (and dropped) by `memos_check.sh`, with a
// root-module tool.
//
// ⚠ IT IS NOT PART OF THE WALK: no capture of it is pushed, and `main.go` never boots it. The walk's
// signals stay the two worlds' they were measured over; this world exists for one test.

// The memo world's people and its one machine credential. `writer-a`, `reader-b` and
// `agents@host-a` are the scope-mail suites' own synthetic names (`tests/memo/world.sh`), reused so
// no new name enters the tree.
const (
	memoWriterDisplay = "writer-a"
	memoReaderDisplay = "reader-b"
	memoAgentLabel    = "agents@host-a"
)

// MemoWorld is a booted memo world: the pod, its client listener, and the three credentials.
type MemoWorld struct {
	*World
	// ClientAPI is the base URL of the client listener, where the seed memos are sent.
	ClientAPI string
	// Scope is the ONE fixture scope the test works in, by name and by control id.
	Scope   string
	ScopeID control.ID
	// WriterToken signs in the scope's OWNER (read, write, admin); ReaderToken a user holding a READ
	// grant on the scope and nothing else; AgentToken is the writer's machine credential, labelled
	// [memoAgentLabel], which the seed memos are sent with.
	WriterToken, ReaderToken, AgentToken string
}

// BootMemoWorld boots `cairn-ui` over the fixture store with a control journal, the database `dsn`
// names, and a client listener.
//
// 🔴 THE JOURNAL IS WRITTEN THROUGH `internal/control`'S OWN WRITERS, FOR `BootJournalWorld`'S REASON:
// `ProvisionUser` and `IssueCredential` for the people and credentials, and for the reader's READ
// grant an `EventGranted` appended through `FileStore.Append` — the same event, through the same
// validating append, that the share flow's `ControlSharing.Share` writes. `Append` validates a batch
// against a clone of the model before a byte reaches disk, so a record the pod would drop at replay
// is refused HERE rather than captured as an authority; and the test asserts the reader's state on
// the page, so a grant that did not take is a red test, never a quiet one.
func BootMemoWorld(ctx context.Context, repoRoot, uiBinary, dir string, port int, dsn string) (*MemoWorld, error) {
	if dsn == "" {
		return nil, fmt.Errorf("the memo world needs a database: no DSN was given, and without one the tab renders %q", "NoMemoStore")
	}
	storeRoot, scopes, err := buildFixtureStore(ctx, repoRoot, dir)
	if err != nil {
		return nil, err
	}
	// The scope the test works in: the first fixture scope whose name is already normalized (the memo
	// store keys rows by the normalized name) and that is not the layout fixture.
	scope := ""
	for _, s := range scopes {
		if s != unbreakableScope && store.NormalizeRef(s) == s {
			scope = s
			break
		}
	}
	if scope == "" {
		return nil, fmt.Errorf("no fixture scope has an already-normalized name, so no memo can be addressed to one: %v", scopes)
	}

	journal := filepath.Join(dir, "control", "journal.jsonl")
	fs, err := control.OpenFileStore(journal)
	if err != nil {
		return nil, err
	}
	writer, err := control.ProvisionUser(ctx, fs, control.NewUser{
		Provider: journalProvider, Subject: "memo-" + memoWriterDisplay, DisplayName: memoWriterDisplay,
		ProjectName: fixtureIdentity, ScopeNames: scopes, At: journalEpoch,
	})
	if err != nil {
		return nil, fmt.Errorf("seeding the memo world's writer: %w", err)
	}
	var scopeID control.ID
	for _, s := range writer.Scopes {
		if s.Name == scope {
			scopeID = s.ID
		}
	}
	if scopeID == "" {
		return nil, fmt.Errorf("the writer's project was not given scope %q: %+v", scope, writer.Scopes)
	}
	// The reader owns an EMPTY project of its own and reaches the scope only by the grant below.
	reader, err := control.ProvisionUser(ctx, fs, control.NewUser{
		Provider: journalProvider, Subject: "memo-" + memoReaderDisplay, DisplayName: memoReaderDisplay,
		ProjectName: memoReaderDisplay, At: journalEpoch,
	})
	if err != nil {
		return nil, fmt.Errorf("seeding the memo world's reader: %w", err)
	}
	grant, err := control.NewID(control.PrefixGrant)
	if err != nil {
		return nil, err
	}
	if _, err := fs.Append(ctx, control.Event{
		Kind: control.EventGranted, At: journalEpoch, Actor: writer.User, GrantID: grant,
		SubjectKind: control.KindUser, SubjectID: reader.User,
		ObjectKind: control.ObjectScope, ObjectID: scopeID, Verbs: control.NewVerbSet(control.VerbRead),
	}); err != nil {
		return nil, fmt.Errorf("granting the memo world's reader read on %s: %w", scope, err)
	}
	issue := func(user control.ID, label string) (string, error) {
		issued, err := control.IssueCredential(ctx, fs, control.NewCredential{
			SubjectKind: control.KindUser, SubjectID: user, Label: label, At: journalEpoch,
		})
		if err != nil {
			return "", fmt.Errorf("issuing the memo world's %q credential: %w", label, err)
		}
		return issued.Token(), nil
	}
	writerToken, err := issue(writer.User, memoWriterDisplay)
	if err != nil {
		return nil, err
	}
	readerToken, err := issue(reader.User, memoReaderDisplay)
	if err != nil {
		return nil, err
	}
	agentToken, err := issue(writer.User, memoAgentLabel)
	if err != nil {
		return nil, err
	}

	apiPort, err := aFreeLoopbackPort()
	if err != nil {
		return nil, err
	}
	w := &MemoWorld{
		World: &World{
			BaseURL: fmt.Sprintf("http://%s:%d", bindHost, port),
			Store:   storeRoot,
			Scopes:  scopes,
			Token:   writerToken,
			dir:     dir,
			logPath: filepath.Join(dir, "cairn-ui.log"),
		},
		ClientAPI: fmt.Sprintf("http://%s:%d", bindHost, apiPort),
		Scope:     scope, ScopeID: scopeID,
		WriterToken: writerToken, ReaderToken: readerToken, AgentToken: agentToken,
	}
	// ⚠ `-token-file` NAMES A FILE THAT DOES NOT EXIST, as on the deployment this world mirrors: the
	// journal is the only authority, so no ambient token file can add a second one. No
	// `-session-file`: with a database the session table lives there.
	if err := w.start(ctx, uiBinary, port,
		"-store", storeRoot,
		"-control-journal", journal,
		"-db-dsn", dsn,
		"-token-file", filepath.Join(dir, "absent-token-file"),
		"-host", bindHost,
		"-port", fmt.Sprint(port),
		"-client-api-addr", fmt.Sprintf("%s:%d", bindHost, apiPort),
	); err != nil {
		return nil, err
	}
	if err := w.waitClientAPI(ctx, 20*time.Second); err != nil {
		w.Stop()
		return nil, fmt.Errorf("%w\n--- cairn-ui log ---\n%s", err, w.Log())
	}
	return w, nil
}

// waitClientAPI waits until the client listener ANSWERS — any HTTP status: an unauthenticated
// request is refused, and a refusal is a listener.
func (w *MemoWorld) waitClientAPI(ctx context.Context, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	var last error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, w.ClientAPI+memowire.MemosPath, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	return fmt.Errorf("the client listener at %s never answered within %s: %v", w.ClientAPI, budget, last)
}

// SendAsAgent sends one memo to the world's scope through the CLIENT LISTENER with the agent
// credential — the path an agent's `cairn memo-send` takes — and returns its id. It is how the
// test's seeded memos get there: sent, by the real binary's own write path, never planted.
func (w *MemoWorld) SendAsAgent(ctx context.Context, subject, body string) (int64, error) {
	payload, err := json.Marshal(memowire.SendRequest{Scope: w.Scope, Subject: subject, Body: body})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.ClientAPI+memowire.MemosPath, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+w.AgentToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("sending a seed memo: %w", err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var sent memowire.SentBody
	if resp.StatusCode != http.StatusCreated || json.Unmarshal(answer, &sent) != nil || sent.ID < 1 {
		return 0, fmt.Errorf("the client listener answered a seed send %d %s, want 201 with an id", resp.StatusCode, truncateForLog(string(answer), 300))
	}
	return sent.ID, nil
}
