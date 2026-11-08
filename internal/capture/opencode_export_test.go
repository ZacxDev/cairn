package capture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExportUsesV2SessionExportAndFallsBackToLegacy pins the argv the capture pass sends to the
// opencode binary.
//
// 🔴 MEASURED: v2 moved `export` under `session`, so the top-level `opencode export <id>` prints
// HELP and exits non-zero. cairn-capture still asked for the old form, so every opencode capture
// was refused — `capture: opencode [export ses_…]: exit status 1`, nothing reaching the store —
// while `opencode session export <ses_…>` answers the session JSON with exit 0.
func TestExportUsesV2SessionExportAndFallsBackToLegacy(t *testing.T) {
	t.Run("v2 canonical", func(t *testing.T) {
		stub := t.TempDir()
		if err := os.WriteFile(filepath.Join(stub, "export-ses_x.json"),
			[]byte(`{"info":{"id":"ses_x"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CAPTURE_OPENCODE_STUB", stub)
		out, err := ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()}.Export("ses_x")
		if err != nil {
			t.Fatalf("v2 export: %v", err)
		}
		if string(out) != `{"info":{"id":"ses_x"}}` {
			t.Fatalf("export returned %q; want the stub document unchanged", out)
		}
		called, err := os.ReadFile(filepath.Join(stub, "v2-export-called"))
		if err != nil || string(called) != "session export ses_x" {
			t.Fatalf("v2 export command was not called as specified: %q (%v)", called, err)
		}
		if _, err := os.Stat(filepath.Join(stub, "legacy-export-called")); !os.IsNotExist(err) {
			t.Fatalf("legacy fallback ran despite a successful v2 answer: %v", err)
		}
	})

	t.Run("legacy fallback when the v2 verb is unavailable", func(t *testing.T) {
		stub := t.TempDir()
		if err := os.WriteFile(filepath.Join(stub, "v1-only"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stub, "export-ses_y.json"),
			[]byte(`{"info":{"id":"ses_y"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CAPTURE_OPENCODE_STUB", stub)
		out, err := ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()}.Export("ses_y")
		if err != nil {
			t.Fatalf("legacy fallback export: %v", err)
		}
		if string(out) != `{"info":{"id":"ses_y"}}` {
			t.Fatalf("export returned %q; want the stub document unchanged", out)
		}
		if _, err := os.Stat(filepath.Join(stub, "v2-export-called")); err != nil {
			t.Fatalf("the v2 verb was not attempted before the fallback: %v", err)
		}
		called, err := os.ReadFile(filepath.Join(stub, "legacy-export-called"))
		if err != nil || string(called) != "export ses_y" {
			t.Fatalf("legacy fallback command: %q (%v)", called, err)
		}
	})
}

// TestParseExportAcceptsTheV2MessageShape pins the ONE thing the v2 export changed that the verb
// fix did not: the DOCUMENT. v2's `session export` returns `messages[]` flat — the message object
// itself carries `id`/`type`, and its parts live under `content`, with no `{info, parts}` wrapper —
// so the v1 reader refused every session as "not one complete JSON document" even though the exit
// code was 0 and the bytes were valid JSON.
//
// 🔴 THE TEST IS ON THE NORMALIZED UNITS, NOT ON "it parsed": the store and the transcript reader
// consume one model, so a message must still yield a `role` and each part a `messageID`.
func TestParseExportAcceptsTheV2MessageShape(t *testing.T) {
	doc := `{"info":{"id":"ses_v2"},
	  "messages":[
	    {"id":"msg_1","type":"user","time":{"created":1},"text":"hi","files":[]},
	    {"id":"msg_2","type":"assistant","time":{"created":2},
	     "content":[
	       {"type":"reasoning","text":"thinking"},
	       {"type":"tool","id":"call_1","name":"subagent",
	        "state":{"status":"completed","input":{},"metadata":{"sessionID":"ses_child"}}},
	       {"type":"text","text":"done"}
	     ]}
	  ]}`
	units, children, err := parseExport([]byte(doc))
	if err != nil {
		t.Fatalf("a v2 export must parse, got %v", err)
	}
	byKey := map[string]exportUnit{}
	var order []string
	for _, u := range units {
		byKey[u.Key] = u
		order = append(order, u.Key)
	}
	if _, ok := byKey["info"]; !ok {
		t.Fatalf("no `info` unit; keys=%v", order)
	}
	for _, want := range []struct{ key, role string }{
		{"msg:msg_1", "user"},
		{"msg:msg_2", "assistant"},
	} {
		u, ok := byKey[want.key]
		if !ok {
			t.Fatalf("no %s unit; keys=%v", want.key, order)
		}
		var got map[string]any
		if err := json.Unmarshal(u.Raw, &got); err != nil {
			t.Fatalf("%s is not an object: %v", want.key, err)
		}
		if got["role"] != want.role {
			t.Fatalf("%s role = %v, want %q (the reader keys the transcript off it)", want.key, got["role"], want.role)
		}
		if _, ok := got["content"]; ok {
			t.Fatalf("%s still carries `content`; parts must be separate units", want.key)
		}
	}
	// A part with its own id keeps it; a part WITHOUT one still lands, under the message's id.
	if u, ok := byKey["prt:call_1"]; !ok {
		t.Fatalf("no prt:call_1 unit; keys=%v", order)
	} else {
		var got map[string]any
		if err := json.Unmarshal(u.Raw, &got); err != nil {
			t.Fatalf("prt:call_1 is not an object: %v", err)
		}
		if got["messageID"] != "msg_2" {
			t.Fatalf("prt:call_1 messageID = %v, want msg_2", got["messageID"])
		}
		if got["tool"] != "subagent" {
			t.Fatalf("prt:call_1 tool = %v, want subagent (v2 names it `name`)", got["tool"])
		}
	}
	var textPart bool
	for key := range byKey {
		if strings.HasPrefix(key, "prt:") && key != "prt:call_1" {
			textPart = true
		}
	}
	if !textPart {
		t.Fatalf("the id-less `content` parts were dropped; keys=%v", order)
	}
	if len(children) != 1 || children[0] != "ses_child" {
		t.Fatalf("children = %v, want [ses_child] from the `subagent` part", children)
	}

	// Still FAIL-CLOSED: a message with no id is not a document this can vouch for.
	if _, _, err := parseExport([]byte(`{"info":{"id":"s"},"messages":[{"type":"user"}]}`)); err == nil {
		t.Fatal("a v2 message with no id must refuse, not ship")
	}
}

// v2SubagentExport is the STRUCTURE of a real opencode v2 (2.0.32) `session export` of a root that
// ran subagents: every key and nesting level as opencode wrote it, every value a placeholder. It
// carries the three subagent part shapes measured in real output:
//
//   - a first call: `name:"subagent"`, the child id in `state.metadata.sessionID`, none in `input`;
//   - a second call naming a DIFFERENT child;
//   - a RESUMED call: the child id repeated in `state.input.sessionID` AND `state.metadata.sessionID`.
//
// plus a `shell` tool part, whose real metadata (`exit`/`status`/`truncated`) names no session.
const v2SubagentExport = `{
 "info":{"id":"ses_root","projectID":"P","agent":"A","model":{"id":"M","providerID":"P","variant":"V"},
   "cost":0,"tokens":{"input":0,"output":0,"reasoning":0,"cache":{"read":0,"write":0}},
   "outcome":"O","time":{"created":1,"updated":1,"idle":1,"viewed":1},"title":"T","location":{"directory":"D"}},
 "messages":[
  {"id":"msg_u1","time":{"created":1},"text":"PLACEHOLDER","files":[],"agents":[],"type":"user"},
  {"id":"msg_a1","time":{"created":2,"streamed":2,"completed":2},"type":"assistant","agent":"A",
   "model":{"id":"M","providerID":"P","variant":"V"},
   "content":[
    {"type":"reasoning","text":"PLACEHOLDER","state":{"reasoningField":"R","reasoningDetails":[{"type":"R","text":"PLACEHOLDER","format":"F","index":0}]},"time":{"created":2,"completed":2}},
    {"type":"tool","id":"call_shell","name":"shell","executed":false,
     "state":{"status":"completed","input":{"command":"PLACEHOLDER"},"content":[{"type":"text","text":"PLACEHOLDER"}],
       "metadata":{"exit":0,"status":"completed","truncated":false}},
     "time":{"created":2,"ran":2,"completed":2}},
    {"type":"tool","id":"call_sub_a","name":"subagent","executed":false,
     "state":{"status":"completed","input":{"description":"PLACEHOLDER","agent":"general","prompt":"PLACEHOLDER"},
       "content":[{"type":"text","text":"PLACEHOLDER"}],
       "metadata":{"sessionID":"ses_child_a","status":"completed","truncated":false}},
     "time":{"created":2,"ran":2,"completed":2}},
    {"type":"tool","id":"call_sub_b","name":"subagent","executed":false,
     "state":{"status":"completed","input":{"description":"PLACEHOLDER","agent":"general","prompt":"PLACEHOLDER"},
       "content":[{"type":"text","text":"PLACEHOLDER"}],
       "metadata":{"sessionID":"ses_child_b","status":"completed","truncated":false}},
     "time":{"created":2,"ran":2,"completed":2}},
    {"type":"text","text":"PLACEHOLDER"}
   ],
   "snapshot":{"start":"S","end":"S","files":[]},"finish":"F","rawFinish":"F","cost":0,
   "tokens":{"input":0,"output":0,"reasoning":0,"cache":{"read":0,"write":0}}},
  {"id":"msg_a2","time":{"created":3,"streamed":3,"completed":3},"type":"assistant","agent":"A",
   "model":{"id":"M","providerID":"P","variant":"V"},
   "content":[
    {"type":"tool","id":"call_sub_a_resumed","name":"subagent","executed":false,
     "state":{"status":"completed","input":{"agent":"general","description":"PLACEHOLDER","sessionID":"ses_child_a","prompt":"PLACEHOLDER"},
       "content":[{"type":"text","text":"PLACEHOLDER"}],
       "metadata":{"sessionID":"ses_child_a","status":"completed","truncated":false}},
     "time":{"created":3,"ran":3,"completed":3}}
   ],
   "snapshot":{"start":"S","end":"S","files":[]},"finish":"F","rawFinish":"F","cost":0,
   "tokens":{"input":0,"output":0,"reasoning":0,"cache":{"read":0,"write":0}}}
 ]}`

// TestParseExportFindsTheChildrenOfARealV2Root is the regression test for opencode v2's subagent
// rename: v2 calls the tool `subagent` (v1: `task`) and keeps the child id in
// `state.metadata.sessionID` (v1: `sessionId`). The predicate still asked for the v1 spellings, so
// EVERY v2 root shipped with zero children — no child stream was ever captured.
func TestParseExportFindsTheChildrenOfARealV2Root(t *testing.T) {
	units, children, err := parseExport([]byte(v2SubagentExport))
	if err != nil {
		t.Fatalf("the real v2 shape must parse, got %v", err)
	}
	if len(units) == 0 {
		t.Fatal("no units from the real v2 shape")
	}
	if len(children) == 0 {
		t.Fatalf("children = %v: a v2 root with two subagent children yielded NONE "+
			"(the `subagent` part or its `state.metadata.sessionID` was not recognized)", children)
	}
	// Two distinct children; the resumed call repeats ses_child_a and must not double it.
	if len(children) != 2 || children[0] != "ses_child_a" || children[1] != "ses_child_b" {
		t.Fatalf("children = %v, want [ses_child_a ses_child_b] (the resumed call deduped, the shell part ignored)", children)
	}
}

// TestTaskChildReadsBothSpellings pins the ONE predicate across both opencode generations, so the
// v1 path the synthetic world exercises and the v2 path above stay one rule.
func TestTaskChildReadsBothSpellings(t *testing.T) {
	part := func(tool string, md map[string]any) map[string]any {
		return map[string]any{"type": "tool", "tool": tool, "state": map[string]any{"metadata": md}}
	}
	for _, c := range []struct {
		name string
		part map[string]any
		want string
	}{
		{"v1 task/sessionId", part("task", map[string]any{"sessionId": "ses_v1"}), "ses_v1"},
		{"v2 subagent/sessionID", part("subagent", map[string]any{"sessionID": "ses_v2"}), "ses_v2"},
		{"other tool with a session id", part("shell", map[string]any{"sessionID": "ses_no"}), ""},
		{"subagent with no id yet", part("subagent", map[string]any{"status": "running"}), ""},
		{"not a tool part", map[string]any{"type": "text", "tool": "subagent",
			"state": map[string]any{"metadata": map[string]any{"sessionID": "ses_no"}}}, ""},
	} {
		if got := taskChild(c.part); got != c.want {
			t.Errorf("%s: taskChild = %q, want %q", c.name, got, c.want)
		}
	}
}
