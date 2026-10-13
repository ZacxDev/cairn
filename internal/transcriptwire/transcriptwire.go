// Package transcriptwire is the agent read API's wire contract — its routes, query parameters,
// views and its one miss — spelled ONCE for both ends: `internal/transcript` answers it (on the
// browser surface and on the client listener) and `internal/client`'s `transcript` verb asks it
// (S8 of `claudedocs/plan-cairn-plugins.md`, decision 17).
//
// 🔴 A PACKAGE OF ITS OWN BECAUSE THE CLIENT SHOULD NOT LINK THE ARCHIVE. `internal/transcript` brings
// the transcript archive, the redactor and the scope derivation with it; the client needs none of
// them, only the spellings, and a client-side COPY of the spellings is two definitions that drift
// in the direction nobody tests — a parameter renamed on one side is silently ignored by the other.
// `internal/memowire` is the same split for the memo routes, for the same reason.
//
// It is stdlib-free and holds no type: the answers' JSON shapes are `internal/transcript`'s, and the
// client prints them verbatim rather than decoding them.
package transcriptwire

// The three read routes, by the last segment of their path.
const (
	RouteSkeleton = "skeleton"
	RouteRecords  = "records"
	RouteTool     = "tool"
)

// ClientPathPrefix is where the client listener serves the read routes: `ClientPathPrefix + route`.
// The browser surface serves them under `/transcript/` (`internal/ui`'s ledger).
const ClientPathPrefix = "/client/v1/transcript/"

// The query parameters.
const (
	ParamSession  = "session"
	ParamStream   = "stream"
	ParamFromSeq  = "from_seq"
	ParamToSeq    = "to_seq"
	ParamClass    = "class"
	ParamRole     = "role"
	ParamTool     = "tool"
	ParamView     = "view"
	ParamMaxBytes = "max_bytes"
	ParamSeq      = "seq"
	ParamID       = "id"
	ParamRange    = "range"
)

// The two views of a records page. Conversation is the default.
const (
	ViewConversation = "conversation"
	ViewRaw          = "raw"
)

// ErrorNotFound is the `error` value of the read API's ONE miss — the client listener's own
// `not-found`, so an agent reads one miss whichever listener it asked.
const ErrorNotFound = "not-found"

// UnseenBody is that miss's whole body.
const UnseenBody = `{"error":"` + ErrorNotFound + `"}` + "\n"
