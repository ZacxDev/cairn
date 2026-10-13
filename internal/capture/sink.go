package capture

// Record is one redacted unit bound for a stream: `Src` names where it came from (a byte offset,
// or an opencode unit key and version), `Rec` is the redacted record's EXACT bytes, and `End` is
// the stream CURSOR once this record is stored — the `to` of a request ending with it, and the
// `from` of the request after it (decision 15's compare-and-swap).
//
// 🔴 BYTES, NOT A JSON VALUE. A line that is not JSON — and in particular one that is not valid
// UTF-8 — must ship byte-identical; carrying it as a JSON string would have `encoding/json` turn
// every invalid byte into U+FFFD on the way out (review round 1). How the wire carries a record
// that is not JSON is the sink's business ([HTTPSink]); the agent hands it the bytes.
type Record struct {
	Src string
	Rec []byte
	End string
}

// Upload is what every write of one session shares: where it goes, whose session it is, what the
// session declares, and the stored position the write starts from.
//
// `From` is the cursor the instance is believed to hold for the stream or blob ("" for one never
// written). A Claude Code stream's cursor is its byte offset in decimal; an opencode stream's is a
// hash chain over the redacted units shipped ([chainCursor]); a blob's is a digest of its redacted
// bytes ([blobCursor]). None carries content.
type Upload struct {
	Instance, Root, Runtime string
	Declared                []string
	From                    string
	// Rechecked names what the pod's re-check ALREADY refused, by record `src` ("" is the blob of
	// a Blob write): the sink sends its placeholder at once and never its bytes.
	Rechecked map[string]bool
	// OnRecheck, when set, is called when the pod's re-check refuses a record (its `src`) or the
	// blob (""), BEFORE the placeholder is sent in its place — so the decision is saved first, and
	// a run that dies before the placeholder is acknowledged does not resend the bytes.
	OnRecheck func(src string) error
}

// Sink receives what the agent uploads. [HTTPSink] is the real one (the capture/v1 contract); the
// agent's tests also drive an in-memory one.
//
// A Sink write is the "acknowledgement": a watermark advances only over what a write reported
// stored. A write that fails part-way reports the prefix that WAS acknowledged and the agent
// commits it; a request whose answer was lost is not in that prefix, so the next run starts before
// it, the instance's compare-and-swap answers 409 naming the position it really holds, and the
// sink resumes from there — nothing is stored twice.
type Sink interface {
	// Records appends recs to one stream, starting at u.From. `stored` is how many LEADING records
	// are known stored — on a nil error all of them, on an error the prefix acknowledged before it
	// — and the agent commits exactly that prefix.
	Records(u Upload, stream string, recs []Record) (stored int, err error)
	// Blob replaces one persisted tool-result file, from the version u.From to the version `to`.
	Blob(u Upload, name, to string, data []byte) error
	// Position asks where the instance holds a stream ("" for one never written). It stores
	// NOTHING: it is how an unacknowledged opencode write is settled before another is sent.
	Position(u Upload, stream string) (string, error)
	// Withdraw is decision 16's withdrawal of a shipped prefix (S6): the instance is asked to forget
	// the session. `deleted` is the instance's own answer — true when this host's (owner, host) held
	// the root (it is deleted now) or had it deleted there already, so a repeat after a lost
	// acknowledgement is true again; FALSE is FINAL: the root is not this uploader's there, and the
	// agent holds the session rather than shipping it anywhere else. An error is a request that did
	// not get an answer; the agent retries it on the next run.
	Withdraw(instance, root string) (deleted bool, err error)
}
