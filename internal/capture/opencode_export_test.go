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
	       {"type":"tool","id":"call_1","name":"task",
	        "state":{"status":"completed","input":{},"metadata":{"sessionId":"ses_child"}}},
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
		if got["tool"] != "task" {
			t.Fatalf("prt:call_1 tool = %v, want task (v2 names it `name`)", got["tool"])
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
		t.Fatalf("children = %v, want [ses_child] from the `task` part", children)
	}

	// Still FAIL-CLOSED: a message with no id is not a document this can vouch for.
	if _, _, err := parseExport([]byte(`{"info":{"id":"s"},"messages":[{"type":"user"}]}`)); err == nil {
		t.Fatal("a v2 message with no id must refuse, not ship")
	}
}
