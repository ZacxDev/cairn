package transcript

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
)

// world is the synthetic fixture `tests/transcripts/gen.py` writes. Regenerate, never hand-edit.
type world struct {
	Sessions map[string]struct {
		Runtime string `json:"runtime"`
		ID      string `json:"id"`
	} `json:"sessions"`
	Claude struct {
		Files map[string]map[string]string `json:"files"`
	} `json:"claude"`
	Opencode struct {
		Exports map[string]map[string]json.RawMessage `json:"exports"`
	} `json:"opencode"`
}

func loadWorld(t *testing.T) world {
	t.Helper()
	raw, err := os.ReadFile("testdata/synthetic_world.json")
	if err != nil {
		t.Fatalf("the synthetic world is missing (%v). It is written by tests/transcripts/gen.py and must be "+
			"named in flake.nix's onlyGo filter, or this test is green on a developer host and RED in the sandbox.", err)
	}
	var w world
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatalf("the synthetic world does not parse: %v", err)
	}
	return w
}

// decode parses one JSON document the way every consumer here must: numbers kept as
// json.Number, so a 20-digit id is never rounded through a float.
func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v map[string]any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("a fixture record does not parse: %v", err)
	}
	return v
}

// vocab is what the generator EMITTED: type name -> union of the keys seen on it.
type vocab map[string]map[string]bool

func (v vocab) add(kind string, keys map[string]any) {
	if v[kind] == nil {
		v[kind] = map[string]bool{}
	}
	for k := range keys {
		v[kind][k] = true
	}
}

func (v vocab) names() []string {
	var out []string
	for k := range v {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// diffShapes compares an emitted vocabulary against a declared one IN BOTH DIRECTIONS and
// returns one line per disagreement. It is the whole instrument this file's guard rests on, so
// `TestTheShapeComparatorGoesRedInBothDirections` proves it can see each direction first.
func diffShapes(what string, emitted vocab, declared map[string]Shape) []string {
	var out []string
	for _, kind := range emitted.names() {
		shape, ok := declared[kind]
		if !ok {
			out = append(out, what+" "+kind+": EMITTED by the generator but not declared in the table")
			continue
		}
		for k := range emitted[kind] {
			if !slices.Contains(shape.Fields, k) {
				out = append(out, what+" "+kind+": field "+k+" EMITTED but not declared")
			}
		}
		for _, k := range shape.Fields {
			if !emitted[kind][k] {
				out = append(out, what+" "+kind+": field "+k+" DECLARED but never emitted")
			}
		}
	}
	for kind := range declared {
		if emitted[kind] == nil {
			out = append(out, what+" "+kind+": DECLARED in the table but never emitted by the generator")
		}
	}
	sort.Strings(out)
	return out
}

func diffNames(what string, emitted map[string]bool, declared []string) []string {
	var out []string
	for k := range emitted {
		if !slices.Contains(declared, k) {
			out = append(out, what+" "+k+": EMITTED but not declared")
		}
	}
	for _, k := range declared {
		if !emitted[k] {
			out = append(out, what+" "+k+": DECLARED but never emitted")
		}
	}
	sort.Strings(out)
	return out
}

type claudeVocab struct {
	records, blocks, items vocab
	subtypes, attachments  map[string]bool
	lines                  int
}

func claudeEmitted(t *testing.T, w world) claudeVocab {
	t.Helper()
	cv := claudeVocab{records: vocab{}, blocks: vocab{}, items: vocab{}, subtypes: map[string]bool{},
		attachments: map[string]bool{}}
	for path, file := range w.Claude.Files {
		if !strings.HasSuffix(path, ".jsonl") {
			continue
		}
		for _, line := range strings.SplitAfter(file["utf8"], "\n") {
			if line == "" {
				continue
			}
			rec := decode(t, []byte(line))
			cv.lines++
			kind, _ := rec["type"].(string)
			cv.records.add(kind, rec)
			switch kind {
			case "system":
				cv.subtypes[rec["subtype"].(string)] = true
			case "attachment":
				cv.attachments[rec["attachment"].(map[string]any)["type"].(string)] = true
			case "user", "assistant":
				blocks, _ := rec["message"].(map[string]any)["content"].([]any)
				for _, b := range blocks {
					bm := b.(map[string]any)
					cv.blocks.add(bm["type"].(string), bm)
					if items, ok := bm["content"].([]any); ok && bm["type"] == "tool_result" {
						for _, it := range items {
							im := it.(map[string]any)
							cv.items.add(im["type"].(string), im)
						}
					}
				}
			}
		}
	}
	return cv
}

type opencodeVocab struct {
	parts  vocab
	states map[string]map[string]bool
	docs   int
}

func opencodeEmitted(t *testing.T, w world) opencodeVocab {
	t.Helper()
	ov := opencodeVocab{parts: vocab{}, states: map[string]map[string]bool{}}
	for _, docs := range w.Opencode.Exports {
		for _, raw := range docs {
			doc := decode(t, raw)
			ov.docs++
			for _, m := range doc["messages"].([]any) {
				for _, p := range m.(map[string]any)["parts"].([]any) {
					pm := p.(map[string]any)
					kind := pm["type"].(string)
					ov.parts.add(kind, pm)
					if kind == "tool" {
						st := pm["state"].(map[string]any)
						status := st["status"].(string)
						if ov.states[status] == nil {
							ov.states[status] = map[string]bool{}
						}
						for k := range st {
							ov.states[status][k] = true
						}
					}
				}
			}
		}
	}
	return ov
}

// TestTheGeneratorAndTheClassificationTableAgree is S0's shape ledger.
//
// 🔴 BOTH DIRECTIONS, BECAUSE EACH CATCHES A DIFFERENT DRIFT. A type or field the generator
// emits and the table does not declare means a fixture is exercising a shape nothing classifies
// on purpose; a type or field the table declares and the generator never emits means a
// declared rule no test ever reaches. Either makes the table read as coverage it does not
// provide.
func TestTheGeneratorAndTheClassificationTableAgree(t *testing.T) {
	w := loadWorld(t)
	cv := claudeEmitted(t, w)
	ov := opencodeEmitted(t, w)
	if cv.lines == 0 || ov.docs == 0 {
		t.Fatalf("the instrument read nothing (claude lines=%d, opencode documents=%d): every comparison "+
			"below would pass vacuously", cv.lines, ov.docs)
	}
	var problems []string
	problems = append(problems, diffShapes("claude record", cv.records, ClaudeRecords)...)
	problems = append(problems, diffShapes("claude block", cv.blocks, ClaudeBlocks)...)
	problems = append(problems, diffShapes("claude tool_result item", cv.items, ClaudeToolResultItems)...)
	problems = append(problems, diffNames("claude system subtype", cv.subtypes, ClaudeSystemSubtypes)...)
	problems = append(problems, diffNames("claude attachment type", cv.attachments, ClaudeAttachmentTypes)...)
	problems = append(problems, diffShapes("opencode part", ov.parts, OpencodeParts)...)
	states := map[string]Shape{}
	for status, keys := range OpencodeToolStates {
		states[status] = Shape{Fields: keys}
	}
	emittedStates := vocab{}
	for status, keys := range ov.states {
		emittedStates[status] = keys
	}
	problems = append(problems, diffShapes("opencode tool state", emittedStates, states)...)
	if len(problems) > 0 {
		t.Fatalf("the synthetic world and internal/transcript's classification table DISAGREE:\n  %s\n"+
			"Fix the side that is wrong: a new runtime shape is declared in classify.go AND emitted by "+
			"tests/transcripts/gen.py (then regenerate); a removed one leaves both.",
			strings.Join(problems, "\n  "))
	}
	t.Logf("agree: %d claude lines (%d record types, %d block types), %d opencode documents (%d part types)",
		cv.lines, len(cv.records), len(cv.blocks), ov.docs, len(ov.parts))
}

// TestTheShapeComparatorGoesRedInBothDirections is the instrument control for the test above.
// A comparator that only looked one way would pass a table that silently stopped declaring a
// type, which is the drift the ledger exists to see.
func TestTheShapeComparatorGoesRedInBothDirections(t *testing.T) {
	declared := map[string]Shape{"a": {Fields: []string{"type", "x"}}}
	same := vocab{"a": {"type": true, "x": true}}
	if got := diffShapes("t", same, declared); len(got) != 0 {
		t.Fatalf("an identical vocabulary reported differences: %v", got)
	}
	extraType := vocab{"a": {"type": true, "x": true}, "b": {"type": true}}
	extraField := vocab{"a": {"type": true, "x": true, "y": true}}
	missingType := vocab{}
	missingField := vocab{"a": {"type": true}}
	for name, v := range map[string]vocab{"emitted type": extraType, "emitted field": extraField,
		"declared type": missingType, "declared field": missingField} {
		if got := diffShapes("t", v, declared); len(got) == 0 {
			t.Errorf("the comparator is BLIND to an undeclared/unemitted %s", name)
		}
	}
	if got := diffNames("t", map[string]bool{"x": true}, []string{"y"}); len(got) != 2 {
		t.Errorf("the name comparator must see both directions, got %v", got)
	}
}

// TestEveryFixtureUnitIsClassified asserts that, over the synthetic world, no unit falls into
// Unknown — the fixture uses only declared shapes, so an Unknown here is a classifier defect.
func TestEveryFixtureUnitIsClassified(t *testing.T) {
	w := loadWorld(t)
	counts := map[Class]int{}
	for path, file := range w.Claude.Files {
		if !strings.HasSuffix(path, ".jsonl") {
			continue
		}
		for _, line := range strings.SplitAfter(file["utf8"], "\n") {
			if line == "" {
				continue
			}
			for _, u := range ClassifyClaude(decode(t, []byte(line))) {
				counts[u.Class]++
				if u.Class == Unknown {
					t.Errorf("%s: a declared shape classified Unknown at %q", path, u.Path)
				}
			}
		}
	}
	for _, docs := range w.Opencode.Exports {
		for _, raw := range docs {
			doc := decode(t, raw)
			for _, m := range doc["messages"].([]any) {
				mm := m.(map[string]any)
				role := mm["info"].(map[string]any)["role"].(string)
				for _, p := range mm["parts"].([]any) {
					for _, u := range ClassifyOpencodePart(role, p.(map[string]any)) {
						counts[u.Class]++
						if u.Class == Unknown {
							t.Errorf("opencode: a declared part classified Unknown")
						}
					}
				}
			}
		}
	}
	// Every class but Unknown must be REACHED by the fixture, or a rule is untested.
	for _, c := range []Class{HumanText, AssistantText, Thinking, ToolCall, ToolResult, Duplicate, Runtime,
		Bookkeeping, Binary, ChildLink} {
		if counts[c] == 0 {
			t.Errorf("class %s is never reached by the synthetic world", c)
		}
	}
	t.Logf("unit counts by class: %v", counts)
}

func rec(t *testing.T, js string) map[string]any { return decode(t, []byte(js)) }

func classes(units []Unit) []string {
	var out []string
	for _, u := range units {
		out = append(out, string(u.Class)+"@"+u.Path)
	}
	return out
}

// TestDecision17UnitRules pins decision 17's rules with LITERAL records and literal answers.
func TestDecision17UnitRules(t *testing.T) {
	cases := []struct {
		name string
		rec  string
		want []string
	}{
		{"a typed prompt is the only human text",
			`{"type":"user","message":{"role":"user","content":"hello"}}`,
			[]string{"human-text@/message/content"}},
		{"a human text BLOCK is human text too",
			`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`,
			[]string{"human-text@/message/content/0"}},
		{"a tool_result block is NOT a user message, and its duplicate field is one unit",
			`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"x"}]},"toolUseResult":{"stdout":"x"}}`,
			[]string{"tool-result@/message/content/0", "duplicate@/toolUseResult"}},
		{"text beside a tool result is runtime, never human",
			`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"x"},{"type":"text","text":"interrupted"}]}}`,
			[]string{"tool-result@/message/content/0", "runtime@/message/content/1"}},
		{"the compaction summary is runtime",
			`{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"summary"}}`,
			[]string{"runtime@/message/content"}},
		{"isMeta content is runtime",
			`{"type":"user","isMeta":true,"message":{"role":"user","content":[{"type":"text","text":"x"}]}}`,
			[]string{"runtime@/message/content/0"}},
		{"an image item and the file.base64 duplicate are binary units",
			`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":[{"type":"image","source":{}}]}]},"toolUseResult":{"file":{"base64":"AAAA"}}}`,
			[]string{"tool-result@/message/content/0", "binary@/message/content/0/content/0", "duplicate@/toolUseResult",
				"binary@/toolUseResult/file/base64"}},
		{"assistant blocks",
			`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"t","signature":"s"},{"type":"text","text":"x"},{"type":"tool_use","id":"t","name":"Bash","input":{}}]}}`,
			[]string{"thinking@/message/content/0", "assistant-text@/message/content/1", "tool-call@/message/content/2"}},
		{"system is runtime", `{"type":"system","subtype":"compact_boundary"}`, []string{"runtime@"}},
		{"attachment is bookkeeping", `{"type":"attachment","attachment":{"type":"file"}}`, []string{"bookkeeping@"}},
		{"an undeclared record type is unknown, never dropped", `{"type":"brand-new-record"}`, []string{"unknown@"}},
		{"an undeclared block type is unknown", `{"type":"assistant","message":{"content":[{"type":"brand-new"}]}}`,
			[]string{"unknown@/message/content/0"}},
	}
	for _, c := range cases {
		if got := classes(ClassifyClaude(rec(t, c.rec))); !slices.Equal(got, c.want) {
			t.Errorf("%s:\n  got  %v\n  want %v", c.name, got, c.want)
		}
	}

	oc := []struct {
		name, role, part string
		want             []string
	}{
		{"user text", "user", `{"type":"text","text":"x"}`, []string{"human-text@"}},
		{"assistant text", "assistant", `{"type":"text","text":"x"}`, []string{"assistant-text@"}},
		{"synthetic text is runtime", "user", `{"type":"text","text":"x","synthetic":true}`, []string{"runtime@"}},
		{"reasoning", "assistant", `{"type":"reasoning","text":"x"}`, []string{"thinking@"}},
		{"a tool part is a call, a result and one binary unit per attachment", "assistant",
			`{"type":"tool","tool":"read","state":{"status":"completed","input":{},"output":"x","attachments":[{"url":"data:"}]}}`,
			[]string{"tool-call@/state/input", "tool-result@/state/output", "binary@/state/attachments/0"}},
		{"a task tool links its child session", "assistant",
			`{"type":"tool","tool":"task","state":{"status":"completed","input":{},"output":"x","metadata":{"sessionId":"ses_x"}}}`,
			[]string{"tool-call@/state/input", "tool-result@/state/output", "child-link@/state/metadata/sessionId"}},
		{"step parts are bookkeeping", "assistant", `{"type":"step-finish"}`, []string{"bookkeeping@"}},
		{"compaction is runtime", "assistant", `{"type":"compaction"}`, []string{"runtime@"}},
		{"subtask is undeclared (unmeasured shape) so unknown", "assistant", `{"type":"subtask"}`, []string{"unknown@"}},
	}
	for _, c := range oc {
		if got := classes(ClassifyOpencodePart(c.role, rec(t, c.part))); !slices.Equal(got, c.want) {
			t.Errorf("opencode %s:\n  got  %v\n  want %v", c.name, got, c.want)
		}
	}
}
