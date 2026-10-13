package worker

import "net/http"

// WithdrawPattern is the capture WITHDRAWAL route (slice S6 of `claudedocs/plan-cairn-plugins.md`,
// decision 16; the plan's contracts): the capture agent asking this instance to forget a prefix it
// shipped before the session's routing answer changed.
//
//	POST /capture/v1/sessions/{root}/withdraw      capture token (owner, host)
//	  200 {"deleted": true}    this token's own (owner, host) held the root — it is deleted now, by
//	                           the SAME cascade the owner's "delete transcript" runs (the directory,
//	                           so every stream, blob, plugin output and ticket edge; the fact
//	                           journaled) — OR had it deleted here already (idempotent: a lost
//	                           acknowledgement followed by a retry answers `true` again)
//	  200 {"deleted": false}   held by another owner, by the same owner under ANOTHER host label, or
//	                           never deleted here for this caller — ONE answer, nothing deleted
//
// 🔴 HOST-BOUND AND OWNER-BOUND BY THE TOKEN ROW, NEVER BY THE REQUEST. The request carries no body
// that is read: who is asking is the row the bearer token matched — its owner and its host — and
// the archive compares BOTH against the root's stored uploader (`archive.Withdraw`, through the
// upload path's own `owned`). A token copied to another machine, or minted under another host
// label, withdraws nothing.
//
// 🔴 NO ORACLE. Every "no" is the same 200 body, and a root id that is not a session id at all gets
// it too; a `true` only ever speaks about the caller's own root.
//
// ⚠ NOT BEHIND THE ARMING GATE (`route.upload`): it stores nothing. It IS behind everything else a
// capture route is — the client identity, the lockout, the token, and the sole capture owner still
// being a principal the authority holds.
const WithdrawPattern = "/capture/v1/sessions/{root}/withdraw"

// The withdrawal's two answers.
const (
	WithdrawnBody    = `{"deleted":true}` + "\n"
	NotWithdrawnBody = `{"deleted":false}` + "\n"
)

func (w *Worker) handleWithdraw(rw http.ResponseWriter, r *http.Request, row TokenRow) {
	root := r.PathValue("root")
	// For the LOG LINE only: was anything stored under the root when the request arrived? A repeat
	// (the idempotent `true`) deletes nothing and must not be logged as a deletion. Not a decision
	// input — the archive decides under the root's lock.
	held, _ := w.cfg.Archive.ReadMeta(root)
	deleted, err := w.cfg.Archive.Withdraw(uploader(row), root)
	if err != nil {
		w.cfg.Log("worker: a withdrawal failed: " + err.Error())
		rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
		rw.WriteHeader(http.StatusInternalServerError)
		_, _ = rw.Write([]byte("internal error\n"))
		return
	}
	if !deleted {
		writeJSON(rw, http.StatusOK, NotWithdrawnBody)
		return
	}
	if held != nil {
		// Who and which root — the journal holds the same fact; never content.
		w.cfg.Log("worker: session " + root + " withdrawn by its uploader " + row.Owner.String() + " on " + row.Host +
			" (decision 16): its transcript, plugin outputs and ticket edges are deleted")
	}
	writeJSON(rw, http.StatusOK, WithdrawnBody)
}
