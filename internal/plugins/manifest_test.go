package plugins

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// summaryManifest is plugin A's shape, SYNTHETIC.
func summaryManifest() Manifest {
	return Manifest{Schema: 1, Name: "summary", Version: "0.1.0", Description: "continuous session summaries",
		Capabilities: []string{CapPendingList, CapTranscriptRead, CapOutputWrite(TypeSummary)}, CallsModel: true}
}

func registryJSON(t *testing.T, ms ...Manifest) []byte {
	t.Helper()
	var entries []Entry
	for _, m := range ms {
		entries = append(entries, Entry{Manifest: m})
	}
	raw, err := json.Marshal(registryFile{Schema: 1, Plugins: entries})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestTheVocabulariesAreClosed: every capability and output type outside the in-tree sets refuses
// the WHOLE registry, as do a disagreeing write capability, a borrowed toggle key and an unknown key.
func TestTheVocabulariesAreClosed(t *testing.T) {
	if _, err := ParseRegistry(registryJSON(t, summaryManifest())); err != nil {
		t.Fatalf("POSITIVE CONTROL: plugin A's manifest is refused: %v", err)
	}
	if got := OutputTypes(); strings.Join(got, ",") != "summary,ticket-edge,ticket-scan,ticket-suggestion" {
		t.Fatalf("the output-type set is %v", got)
	}
	bad := map[string]func(*Manifest){
		"an unknown capability":       func(m *Manifest) { m.Capabilities = append(m.Capabilities, "store:read") },
		"a write for an unknown type": func(m *Manifest) { m.Capabilities = append(m.Capabilities, "output:write:essay") },
		"a read for an unknown type":  func(m *Manifest) { m.Capabilities = append(m.Capabilities, "output:read:essay") },
		"a capability twice":          func(m *Manifest) { m.Capabilities = append(m.Capabilities, CapPendingList) },
		"a bad name":                  func(m *Manifest) { m.Name = "Summary!" },
		"schema 2":                    func(m *Manifest) { m.Schema = 2 },
		"a control character":         func(m *Manifest) { m.Description = "a\x07b" },
	}
	for name, mutate := range bad {
		m := summaryManifest()
		mutate(&m)
		if _, err := ParseRegistry(registryJSON(t, m)); err == nil {
			t.Errorf("%s was admitted to the registry", name)
		}
	}
	if _, err := ParseRegistry(registryJSON(t, summaryManifest(), summaryManifest())); err == nil {
		t.Error("a plugin registered twice was admitted")
	}
	unknown := strings.Replace(string(registryJSON(t, summaryManifest())), `"calls_model":true`, `"calls_model":true,"mcp":true`, 1)
	if _, err := ParseRegistry([]byte(unknown)); err == nil {
		t.Error("a manifest carrying a key the schema does not name was admitted")
	}
	reg, _ := ParseRegistry(registryJSON(t, summaryManifest()))
	e, _ := reg.Lookup("summary")
	if e.PendingIdleSeconds != 600 || e.PendingGrowRecords != 200 {
		t.Fatalf("the pending defaults are %d s / %d records, want decision 13's 600 / 200", e.PendingIdleSeconds, e.PendingGrowRecords)
	}
	if !e.Writes(TypeSummary) || e.Writes(TypeTicketEdge) || strings.Join(e.OutputTypes(), ",") != TypeSummary {
		t.Fatal("Writes / OutputTypes do not answer the manifest's own write capabilities")
	}
	for _, removed := range []string{`"toggle_key":"summary"`, `"output_types":["summary"]`} {
		withKey := strings.Replace(string(registryJSON(t, summaryManifest())), `"calls_model":true`, removed+`,"calls_model":true`, 1)
		if _, err := ParseRegistry([]byte(withKey)); err == nil {
			t.Errorf("a manifest carrying the deleted key %s was admitted (it is no longer part of the schema)", removed)
		}
	}
}

// TestTheEnvelopeIsStampedFromTheTokenNotTheBody: a body naming ANOTHER plugin, version or clock is
// overwritten with the registry entry the token resolved to (decision 9's "a plugin cannot claim to
// be another"); the mutant `plugins-plugin-name-from-body-not-token` trusts the body.
func TestTheEnvelopeIsStampedFromTheTokenNotTheBody(t *testing.T) {
	raw := []byte(`{"plugin":"clickup","plugin_version":"9.9.9","model":"fake-model-1","session":"s-0001",` +
		`"input_watermark":{"main":3},"type":"summary","body":{"text":"three turns, synthetic"}}`)
	env, err := DecodeEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Stamp(env, Entry{Manifest: summaryManifest()}, time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if out.Plugin != "summary" || out.PluginVersion != "0.1.0" || out.ProducedAt != "2000-01-02T03:04:05Z" ||
		!strings.HasPrefix(out.ID, "o-") || out.Model != "fake-model-1" || out.InputWatermark["main"] != 3 {
		t.Fatalf("stamped %+v, want plugin=summary version=0.1.0 at the pod clock", out)
	}
}

// TestBodiesAreValidatedAgainstTheirTypesSchema: unknown fields, bounds and the canonical-id rule.
func TestBodiesAreValidatedAgainstTheirTypesSchema(t *testing.T) {
	good := map[string]string{
		TypeSummary:          `{"text":"ok"}`,
		TypeTicketEdge:       `{"system":"clickup","ticket_id":"abc123","signal":"branch-name","locator":{"stream":"main","seq":4}}`,
		TypeTicketSuggestion: `{"ticket_id":"abc123","confidence":0.5,"rationale":"synthetic"}`,
		TypeTicketScan:       `{"system":"clickup","edges":0,"suggestions":0,"model_asked":false}`,
	}
	for typ, body := range good {
		if err := ValidateBody(typ, json.RawMessage(body)); err != nil {
			t.Fatalf("POSITIVE CONTROL: %s %s refused: %v", typ, body, err)
		}
	}
	bad := map[string]string{
		TypeSummary:          `{"text":"ok","html":"<b>x</b>"}`,
		TypeTicketEdge:       `{"system":"clickup","ticket_id":"javascript:alert(1)","signal":"branch-name","locator":{"pr":1}}`,
		TypeTicketSuggestion: `{"ticket_id":"abc123","confidence":1.5,"rationale":"synthetic"}`,
		TypeTicketScan:       `{"system":"clickup","edges":-1,"suggestions":0,"model_asked":false}`,
		"essay":              `{"text":"ok"}`,
	}
	for typ, body := range bad {
		if err := ValidateBody(typ, json.RawMessage(body)); err == nil {
			t.Errorf("%s %s was accepted", typ, body)
		}
	}
	twoPlaces := `{"system":"clickup","ticket_id":"abc123","signal":"pr-body","locator":{"pr":1,"commit":"abcdef0"}}`
	if ValidateBody(TypeTicketEdge, json.RawMessage(twoPlaces)) == nil {
		t.Error("a locator naming two places was accepted")
	}
}

// TestTheStoredBodyIsTheValidatedValue: a body with a duplicate key is stored as the value the decoder
// validated (the LAST), never as the bytes sent — RED before #270 round 1, when the raw bytes, both
// keys included, were what reached the store.
func TestTheStoredBodyIsTheValidatedValue(t *testing.T) {
	raw := []byte(`{"plugin":"summary","plugin_version":"0.1.0","model":"fake","session":"s-0001","input_watermark":{"main":1},` +
		`"type":"summary","body":{"text":"FIRST","text":"ok"}}`)
	env, err := DecodeEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(env.Body) != `{"text":"ok"}` {
		t.Fatalf("the body to store is %s, want {\"text\":\"ok\"}", env.Body)
	}
}

// TestADisplayStringCarryingABidiControlIsNeverStored — #274's audit. A ticket title, a rationale and a
// model's name are printed INSIDE the page's own sentences, and a bidirectional control (U+202E and
// its family) is not markup: it reorders how what follows it is displayed. Each is refused at the ONE
// schema check, so none is ever stored. Red before: `printable` looked only for control characters
// (category Cc) and these are format characters (Cf). CONTROL: the same bodies without it are stored.
func TestADisplayStringCarryingABidiControlIsNeverStored(t *testing.T) {
	const rlo = "\u202e"
	envelope := func(model, typ, body string) []byte {
		return []byte(`{"plugin":"clickup","plugin_version":"0.1.0","model":"` + model + `","session":"s-0001","input_watermark":{"main":1},"type":"` + typ + `","body":` + body + `}`)
	}
	edge := func(title string) string {
		return `{"system":"clickup","ticket_id":"abc123","title":"` + title + `","signal":"branch-name","locator":{"stream":"main","seq":1}}`
	}
	suggestion := func(title, rationale string) string {
		return `{"ticket_id":"abc123","title":"` + title + `","confidence":0.5,"rationale":"` + rationale + `"}`
	}
	for name, raw := range map[string][]byte{
		"an edge's title":          envelope("none", TypeTicketEdge, edge("gadget "+rlo+"export")),
		"a suggestion's title":     envelope("fake-model", TypeTicketSuggestion, suggestion("gadget "+rlo+"export", "synthetic")),
		"a suggestion's rationale": envelope("fake-model", TypeTicketSuggestion, suggestion("gadget", "synthetic "+rlo+"reason")),
		"the model's name":         envelope("fake-"+rlo+"model", TypeTicketSuggestion, suggestion("gadget", "synthetic")),
		"an isolate (U+2066)":      envelope("none", TypeTicketEdge, edge("gadget \u2066export")),
		"a mark (U+200F)":          envelope("none", TypeTicketEdge, edge("gadget \u200fexport")),
	} {
		if _, err := DecodeEnvelope(raw); err == nil {
			t.Errorf("%s carrying a bidirectional control was accepted", name)
		}
	}
	for name, raw := range map[string][]byte{
		"an edge":      envelope("none", TypeTicketEdge, edge("gadget export")),
		"a suggestion": envelope("fake-model", TypeTicketSuggestion, suggestion("gadget — «export» é", "synthetic reason")),
	} {
		if _, err := DecodeEnvelope(raw); err != nil {
			t.Errorf("POSITIVE CONTROL: %s without one was refused: %v", name, err)
		}
	}
}
