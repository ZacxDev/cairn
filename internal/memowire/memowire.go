// Package memowire is the scope-mail CLIENT LISTENER's wire contract — its paths and its JSON
// bodies — spelled ONCE for both ends: `internal/clientapi` (the listener, `cairn-ui
// -client-api-addr`) encodes these types and `internal/client` (the memo verbs, S3 of
// `claudedocs/plan-cairn-scope-mail.md`) decodes them.
//
// 🔴 A PACKAGE OF ITS OWN BECAUSE THE CLIENT CANNOT IMPORT THE LISTENER. `internal/clientapi`
// imports `internal/pgstore`, which imports a third-party driver, and `internal/depspolicy` bans
// any third-party module from `cmd/cairn`'s import graph. A client-side COPY of the bodies was the
// alternative, and a second spelling of a wire is two definitions that drift in the direction
// nobody tests: a field renamed on one side decodes as empty on the other with no error. Here
// there is one definition, and `clientapi` re-exports these names so S2's callers did not move.
//
// It is stdlib-only and holds NO stored memo text: a memo crosses the wire only as
// `memo.Rendered`, every text field already passed through `memo.Unsafe`'s replacement (decision
// 19, Guard C). The JSON keys of a memo are `memo.Rendered`'s Go field names, deliberately — the
// type IS the wire format.
package memowire

import "github.com/ZacxDev/cairn/internal/memo"

// The four routes' paths (decision 8). `MemosPath` carries two methods: GET lists, POST sends.
const (
	MemosPath   = "/client/v1/memos"
	MemoPath    = "/client/v1/memo"
	RetractPath = "/client/v1/memo/retract"
)

// ErrorNotFound is the `error` value of the ONE miss (T12): a scope the caller cannot read, a
// scope that does not exist, an unknown id, an id under the wrong scope and an expired memo.
const ErrorNotFound = "not-found"

// ErrorRefused is the `error` value of a send or retraction the store's rules refused.
const ErrorRefused = "refused"

// ErrorBody is the shape every non-2xx answer of the listener shares: `{"error": …}`, with a
// reason and rule on a refusal and a detail on a malformed request.
type ErrorBody struct {
	Error  string `json:"error"`
	Reason string `json:"reason,omitempty"`
	Rule   string `json:"rule,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// ListBody is `GET /client/v1/memos`'s answer: the scope's memos, oldest first, RENDERED.
type ListBody struct {
	Memos []memo.Rendered `json:"memos"`
}

// MemoBody is `GET /client/v1/memo`'s answer: one memo, RENDERED.
type MemoBody struct {
	Memo memo.Rendered `json:"memo"`
}

// SendRequest is `POST /client/v1/memos`'s body. `ttl_seconds` 0 means the default TTL.
type SendRequest struct {
	Scope      string `json:"scope"`
	Subject    string `json:"subject"`
	Body       string `json:"body"`
	TTLSeconds int64  `json:"ttl_seconds"`
}

// SentBody is a send's answer: the new memo's id and its server-stamped creation time.
type SentBody struct {
	ID        int64  `json:"id"`
	CreatedAt string `json:"created_at"`
}

// RetractRequest is `POST /client/v1/memo/retract`'s body: the scope the caller routed by, and the id.
type RetractRequest struct {
	Scope string `json:"scope"`
	ID    int64  `json:"id"`
}

// RetractedBody is a retraction's answer.
type RetractedBody struct {
	Retracted int64 `json:"retracted"`
}
