package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// The agent read API on the browser surface (S8 of the transcripts plan), through the REAL
// dispatcher over a store on disk and a real transcript archive — `transcript_test.go`'s world:
//
//	owner  reads alpha            — the capture owner of every stored session
//	every  reads alpha beta gamma — reads every scope s-full-01 touched
//	one    reads alpha            — reads ONE scope s-full-01 wrote
//	none   reads delta            — a stranger
//
//	s-full-01  V = {alpha, beta, gamma}    s-solo-02  V = {} (owner-only)    s-bare-09  no transcript

func agentURL(path string, kv ...string) string {
	q := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		q.Set(kv[i], kv[i+1])
	}
	return path + "?" + q.Encode()
}

// agentPaths is every agent read row with the operands that reach a REAL answer for s-full-01.
func agentPaths(session string) map[string]string {
	return map[string]string{
		"skeleton": agentURL(TranscriptSkeletonPath, transcript.ParamSession, session),
		"records":  agentURL(TranscriptRecordsPath, transcript.ParamSession, session),
		"raw":      agentURL(TranscriptRecordsPath, transcript.ParamSession, session, transcript.ParamView, transcript.ViewRaw),
		"tool":     agentURL(TranscriptToolPath, transcript.ParamSession, session, transcript.ParamSeq, "2"),
	}
}

// narrowedTo is `user`'s credential cut down to `scopes` — an agent credential (`-narrow-scope`).
func (w trWorld) narrowedTo(user string, scopes ...string) identity.Identity {
	id := w.ids[user]
	only := []control.ID{} // never nil: `Narrow` returns a nil list unchanged
	for _, s := range scopes {
		only = append(only, trID(control.PrefixScope, s))
	}
	id.Auth = control.Narrow(id.Auth, only, nil)
	return id
}

func (w trWorld) serverAs(t *testing.T, id identity.Identity, a *archive.Archive) *Server {
	t.Helper()
	w.ids["agent"] = id
	return w.server(t, "agent", a)
}

// TestTheAgentReadRoutesAskThePredicate — decision 4 over the three JSON rows, S4's matrix replayed:
// the owner and the reader of every scope get the answer; the reader of ONE written scope and a
// stranger get the one miss; an owner-only session is the owner's alone; a NARROWED credential of
// the owner gets no owner arm, and a narrowed credential reads exactly the sessions its narrowing
// covers. RED under `transcript-read-api-without-predicate`.
func TestTheAgentReadRoutesAskThePredicate(t *testing.T) {
	w := transcriptWorld(t)
	a := w.archive(t)
	admitted := map[string]identity.Identity{
		"owner":                       w.ids["owner"],
		"every":                       w.ids["every"],
		"every narrowed to all three": w.narrowedTo("every", "alpha-notes", "beta-notes", "gamma-notes"),
	}
	refused := map[string]identity.Identity{
		"one":                       w.ids["one"],
		"none":                      w.ids["none"],
		"the owner, narrowed":       w.narrowedTo("owner", "alpha-notes"),
		"every narrowed to alpha":   w.narrowedTo("every", "alpha-notes"),
		"every narrowed to nothing": w.narrowedTo("every"),
	}
	for name, id := range admitted {
		srv := w.serverAs(t, id, a)
		for route, path := range agentPaths("s-full-01") {
			rec := getAs(t, srv, path)
			if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" || !json.Valid(rec.Body.Bytes()) {
				t.Errorf("%s GET %s: %d %q, want 200 JSON", name, route, rec.Code, rec.Header().Get("Content-Type"))
			}
			if (route == "records" || route == "raw") && !strings.Contains(rec.Body.String(), trSecretText) {
				t.Errorf("%s GET %s: the answer does not carry the session's records", name, route)
			}
		}
	}
	for name, id := range refused {
		srv := w.serverAs(t, id, a)
		for route, path := range agentPaths("s-full-01") {
			if rec := getAs(t, srv, path); rec.Code != http.StatusNotFound || rec.Body.String() != transcript.UnseenBody {
				t.Errorf("%s GET %s: %d %q, want 404 and the one miss", name, route, rec.Code, rec.Body.String())
			}
		}
	}
	// The owner-only session (clause g over the JSON rows): the owner alone, not even its own
	// narrowed credential.
	for name, id := range map[string]identity.Identity{"owner": w.ids["owner"], "every": w.ids["every"], "the owner, narrowed": w.narrowedTo("owner", "alpha-notes")} {
		rec := getAs(t, w.serverAs(t, id, a), agentPaths("s-solo-02")["records"])
		if want := name == "owner"; (rec.Code == http.StatusOK) != want {
			t.Errorf("%s GET records of the owner-only session: %d (admitted=%v)", name, rec.Code, want)
		}
	}
}

// TestARefusedAgentGetsTheBytesOfASessionWithNoTranscript — hidden, never stored, never written, no
// archive at all, and every miss AFTER the predicate (a stream not held, a record past the end, a
// record holding no tool call, an id the grammar cannot produce) are ONE answer, byte for byte.
// POSITIVE CONTROL first: the admitted answer is not that answer.
func TestARefusedAgentGetsTheBytesOfASessionWithNoTranscript(t *testing.T) {
	w := transcriptWorld(t)
	holds := w.archive(t)
	empty := transcripttest.OpenAt(t, w.clock.Now)
	if rec := getAs(t, w.server(t, "owner", holds), agentPaths("s-full-01")["skeleton"]); rec.Code != http.StatusOK ||
		rec.Body.String() == transcript.UnseenBody {
		t.Fatalf("POSITIVE CONTROL FAILED: the owner's skeleton answered %d %q", rec.Code, rec.Body.String())
	}
	for _, user := range []string{"one", "none"} {
		for route, path := range agentPaths("s-full-01") {
			hidden := getAs(t, w.server(t, user, holds), path)
			for name, other := range map[string]*httptest.ResponseRecorder{
				"an archive that never held it": getAs(t, w.server(t, user, empty), path),
				"no archive":                    getAs(t, w.server(t, user, nil), path),
				"a never-written id":            getAs(t, w.server(t, user, holds), strings.Replace(path, "s-full-01", "s-never-00", 1)),
			} {
				if hidden.Code != other.Code || hidden.Body.String() != other.Body.String() ||
					hidden.Header().Get("Content-Type") != other.Header().Get("Content-Type") {
					t.Errorf("%s GET %s: hidden answers %d %q; %s answers %d %q", user, route, hidden.Code,
						hidden.Body.String(), name, other.Code, other.Body.String())
				}
			}
		}
	}
	owner := w.server(t, "owner", holds)
	for name, path := range map[string]string{
		"a stream the session does not hold": agentURL(TranscriptRecordsPath, transcript.ParamSession, "s-full-01", transcript.ParamStream, "subagent:nope"),
		"a tool record past the end":         agentURL(TranscriptToolPath, transcript.ParamSession, "s-full-01", transcript.ParamSeq, "99"),
		"a record holding no tool call":      agentURL(TranscriptToolPath, transcript.ParamSession, "s-full-01", transcript.ParamSeq, "1"),
		"an id the grammar cannot produce":   agentURL(TranscriptSkeletonPath, transcript.ParamSession, "<s>"),
		"a session with no transcript":       agentURL(TranscriptSkeletonPath, transcript.ParamSession, "s-bare-09"),
	} {
		if rec := getAs(t, owner, path); rec.Code != http.StatusNotFound || rec.Body.String() != transcript.UnseenBody {
			t.Errorf("%s: %d %q, want the one miss", name, rec.Code, rec.Body.String())
		}
	}
}

// TestTheRawRecordsViewAsksThePredicate — `view=raw` is the stored bytes, served to exactly the
// viewers the predicate admits: the reader of one written scope is refused it like every other
// view. RED under `transcript-raw-view-without-predicate`.
func TestTheRawRecordsViewAsksThePredicate(t *testing.T) {
	w := transcriptWorld(t)
	a := w.archive(t)
	stored, err := a.ReadStream("s-full-01", "main")
	if err != nil || len(stored) != 4 {
		t.Fatalf("the control: %d stored records (%v)", len(stored), err)
	}
	path := agentPaths("s-full-01")["raw"]
	body := getAs(t, w.server(t, "every", a), path).Body.Bytes()
	for _, rec := range stored {
		if !bytes.Contains(body, append([]byte(`"rec":`), rec.Rec...)) {
			t.Errorf("record %d is not in the raw answer byte-exact", rec.Seq)
		}
	}
	for _, user := range []string{"one", "none"} {
		if rec := getAs(t, w.server(t, user, a), path); rec.Code != http.StatusNotFound || rec.Body.String() != transcript.UnseenBody {
			t.Errorf("%s: the raw view answered %d %q, want the one miss", user, rec.Code, rec.Body.String())
		}
	}
}
