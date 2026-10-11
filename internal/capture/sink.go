package capture

// Record is one redacted unit bound for a stream: `Src` names where it came from (a byte offset,
// or an opencode unit key and version) and `Rec` is the redacted record's EXACT bytes.
//
// 🔴 BYTES, NOT A JSON VALUE. A line that is not JSON — and in particular one that is not valid
// UTF-8 — must ship byte-identical; carrying it as a JSON string would have `encoding/json` turn
// every invalid byte into U+FFFD on the way out (review round 1).
type Record struct {
	Src string
	Rec []byte
}

// Sink receives what the agent would upload.
//
// ⚠ THIS SLICE HAS NO SINK OF ITS OWN. S2 built a local file spool as a stand-in for the upload;
// review round 0 asked for it to go until S3 brings the real one (D1), so the agent is exercised
// through an in-memory Sink in its tests and the binary offers `--dry-run` and `--self-test` only.
type Sink interface {
	Records(instance, root, stream string, recs []Record) error
	Blob(instance, root, name string, data []byte) error
	Withdraw(instance, root string) error
}
