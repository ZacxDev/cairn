package worker

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/ZacxDev/cairn/internal/transcript/archive"
)

// The capture wire (the plan's contracts), decoded strictly: unknown keys refused, required keys
// required, nothing after the object. A record's `rec` is carried as the raw JSON value it is; in
// a FRAMED upload the one record carries `chunk` instead — that frame's slice of the record's
// bytes, base64 (a slice of UTF-8 can split a character, so it cannot ride as a JSON string).
//
// ⚠ THE CONTRACT DOES NOT SAY HOW A FRAME CARRIES ITS BYTES; `chunk` IS THIS SLICE'S READING, and
// so are 202 `{"frame":i,"of":n}` for a staged non-final frame and `from: ""` for a stream never
// written. The agent's upload (which lands once S2's `cmd/cairn-capture` is merged) is built
// against these.

// Schema is the one wire schema version.
const Schema = 1

type wireFrames struct {
	Index *int `json:"index"`
	Count *int `json:"count"`
}

type wireRecord struct {
	Src   *string         `json:"src"`
	Rec   json.RawMessage `json:"rec"`
	Chunk *string         `json:"chunk"`
}

type wireRecords struct {
	Schema   *int          `json:"schema"`
	Runtime  *string       `json:"runtime"`
	Host     *string       `json:"host"`
	From     *string       `json:"from"`
	To       *string       `json:"to"`
	Declared *[]string     `json:"declared_scopes"`
	Records  *[]wireRecord `json:"records"`
	Frames   *wireFrames   `json:"frames"`
}

type wireBlob struct {
	Schema   *int        `json:"schema"`
	Runtime  *string     `json:"runtime"`
	Host     *string     `json:"host"`
	From     *string     `json:"from"`
	To       *string     `json:"to"`
	Declared *[]string   `json:"declared_scopes"`
	Data     *string     `json:"data"`
	Frames   *wireFrames `json:"frames"`
}

type recordsRequest struct {
	Host    string
	Upload  archive.Upload
	Records []archive.Record
}

type blobRequest struct {
	Host   string
	Upload archive.Upload
	Data   []byte
}

func decodeStrict(body io.Reader, v any) error {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("body is not the expected JSON object: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("body carries data after its JSON object")
	}
	return nil
}

func frame(f *wireFrames) (*archive.Frame, error) {
	if f == nil {
		return nil, nil
	}
	if f.Index == nil || f.Count == nil {
		return nil, errors.New("frames carries exactly index and count")
	}
	return &archive.Frame{Index: *f.Index, Count: *f.Count}, nil
}

func common(schema *int, runtime, host, from, to *string, declared *[]string) (archive.Upload, string, error) {
	if schema == nil || runtime == nil || host == nil || from == nil || to == nil || declared == nil {
		return archive.Upload{}, "", errors.New("an upload carries schema, runtime, host, from, to and declared_scopes; one is missing or null")
	}
	if *schema != Schema {
		return archive.Upload{}, "", fmt.Errorf("schema %d is not %d", *schema, Schema)
	}
	return archive.Upload{Runtime: *runtime, From: *from, To: *to, Declared: *declared}, *host, nil
}

func decodeRecords(body io.Reader) (recordsRequest, error) {
	var w wireRecords
	if err := decodeStrict(body, &w); err != nil {
		return recordsRequest{}, err
	}
	u, host, err := common(w.Schema, w.Runtime, w.Host, w.From, w.To, w.Declared)
	if err != nil {
		return recordsRequest{}, err
	}
	if w.Records == nil {
		return recordsRequest{}, errors.New("a records upload carries records")
	}
	if u.Frame, err = frame(w.Frames); err != nil {
		return recordsRequest{}, err
	}
	out := recordsRequest{Host: host, Upload: u}
	for i, r := range *w.Records {
		if r.Src == nil {
			return recordsRequest{}, fmt.Errorf("record %d carries no src", i)
		}
		hasRec := len(r.Rec) > 0 && !bytes.Equal(r.Rec, []byte("null"))
		switch {
		case u.Frame == nil && (r.Chunk != nil || !hasRec):
			return recordsRequest{}, fmt.Errorf("record %d of an unframed upload carries rec and no chunk", i)
		case u.Frame != nil && (r.Chunk == nil || hasRec):
			return recordsRequest{}, fmt.Errorf("record %d of a framed upload carries chunk and no rec", i)
		}
		raw := []byte(r.Rec)
		if r.Chunk != nil {
			if raw, err = base64.StdEncoding.DecodeString(*r.Chunk); err != nil {
				return recordsRequest{}, fmt.Errorf("record %d's chunk is not standard base64", i)
			}
		}
		out.Records = append(out.Records, archive.Record{Src: *r.Src, Rec: raw})
	}
	return out, nil
}

func decodeBlob(body io.Reader) (blobRequest, error) {
	var w wireBlob
	if err := decodeStrict(body, &w); err != nil {
		return blobRequest{}, err
	}
	u, host, err := common(w.Schema, w.Runtime, w.Host, w.From, w.To, w.Declared)
	if err != nil {
		return blobRequest{}, err
	}
	if w.Data == nil {
		return blobRequest{}, errors.New("a blob upload carries data")
	}
	if u.Frame, err = frame(w.Frames); err != nil {
		return blobRequest{}, err
	}
	data, err := base64.StdEncoding.DecodeString(*w.Data)
	if err != nil {
		return blobRequest{}, errors.New("data is not standard base64")
	}
	return blobRequest{Host: host, Upload: u, Data: data}, nil
}

// quote renders a string as a JSON string.
func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
