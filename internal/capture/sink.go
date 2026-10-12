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
}

// Sink receives what the agent uploads. [HTTPSink] is the real one (the capture/v1 contract); the
// agent's tests also drive an in-memory one.
//
// A Sink write is the "acknowledgement": every watermark advances only after its write returned
// nil. A write that returned an error may have stored a PREFIX on the instance (one request of
// several); the agent does not count on it, and the next run starts again from the same `From`,
// where the instance's compare-and-swap answers 409 naming the position it really holds and the
// sink resumes from there — so nothing is stored twice.
type Sink interface {
	// Records appends recs to one stream, starting at u.From; on nil the stream is stored up to
	// recs[len(recs)-1].End.
	Records(u Upload, stream string, recs []Record) error
	// Blob replaces one persisted tool-result file, from the version u.From to the version `to`.
	Blob(u Upload, name, to string, data []byte) error
	// Withdraw is decision 16's withdrawal of a shipped prefix (S6).
	Withdraw(instance, root string) error
}
