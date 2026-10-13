package ui

import (
	"fmt"
	"net/http"

	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/transcript"
)

// 🔴 THE AGENT READ API ON THE BROWSER SURFACE (S8 of `claudedocs/plan-cairn-plugins.md`, decision
// 17): three JSON rows over one stored session — its skeleton, a bounded page of its records, and one
// tool call. They are the plan's contract ("BROWSER listener, the reader's own credential, decision
// 4's predicate"); the same three answers are ALSO served on the client listener
// (`internal/clientapi`), which is what `CAIRN_UI_URL` names and what the `transcript` verb asks —
// scope mail's Q3 put agent reads there so an agent holds one URL and one credential.
//
// 🔴 NOTHING HERE DECIDES ANYTHING. Each row hands the request's own authority — the principal and the
// RESOLVED, possibly narrowed `control.Authorization` its credential produced — to
// `transcript.Store.Answer`, the ONE function both listeners serve through: it asks
// `transcript.Store.View` (decision 4's predicate), and every miss is `transcript.UnseenBody`. A
// narrowed agent credential reads exactly the sessions its narrowing covers, and never one by the
// owner arm (`Viewer.owns` refuses a narrowed credential).
//
// They are GET rows, so neither cross-site gate applies to them (both derive from the method), and
// a bearer credential reaches them through the header-borne backends before the cookie one.
//
// 🔴 THE BODY IS A TRANSCRIPT AND IS SERVED `Cache-Control: no-store`, every page's rule: a JSON
// answer is as much somebody's text as the rendered section, and no cache on the device keeps it.

// The three rows' paths. Fixed paths with every operand in a query parameter, for the browse pair's
// reason (`routes`).
const (
	TranscriptSkeletonPath = "/transcript/" + transcript.RouteSkeleton
	TranscriptRecordsPath  = "/transcript/" + transcript.RouteRecords
	TranscriptToolPath     = "/transcript/" + transcript.RouteTool
)

func (s *Server) handleTranscriptSkeleton(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	s.agentRead(w, r, id, transcript.RouteSkeleton)
}

func (s *Server) handleTranscriptRecords(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	s.agentRead(w, r, id, transcript.RouteRecords)
}

func (s *Server) handleTranscriptTool(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	s.agentRead(w, r, id, transcript.RouteTool)
}

// agentRead answers one read route through `transcript.Store.Answer`. A nil store — a deployment
// with no `-transcript-dir` — answers every named session with the one miss, like the raw-record row.
func (s *Server) agentRead(w http.ResponseWriter, r *http.Request, id identity.Identity, route string) {
	status, body, err := s.transcripts.Answer(transcript.Viewer{Principal: id.Principal, Auth: id.Auth}, route, r.URL.Query())
	if err != nil {
		fmt.Fprintf(s.log, "ui: a transcript read (%s) failed: %v\n", route, err)
	}
	writeAgentJSON(w, status, body)
}

// writeAgentJSON writes one read API answer.
func writeAgentJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", htmlCacheControl)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
