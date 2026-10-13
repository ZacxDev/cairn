// Package transcripttext renders transcript records as LABELLED lines — what a model reads in place of
// the raw records — for every plugin in this module that sends transcript content to a provider.
package transcripttext

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/plugins/internal/pluginapi"
)

// 🔴 WHAT THE MODEL READS IS A LABELLED RENDERING, NEVER THE RAW RECORDS. Every Claude Code line says WHO wrote it
// — the human, the assistant, a tool, or the agent RUNTIME — because a transcript is not one voice:
// the runtime's compaction summary and its notes are `user` records, and a model reading them
// unlabelled would summarise the runtime's own summary as something the human asked for. So a
// compaction summary is labelled RUNTIME-GENERATED (the plan's S9 test), and so is every `system`
// record.
//
// Bookkeeping never reaches here: the plugin reads `view=conversation`, which leaves it out as a
// placeholder, and a placeholder renders as nothing. Thinking is left out too — it is the model's own
// scratch work and the longest thing in most transcripts.
//
// ⚠ TOOL INPUTS AND RESULTS ARE CLIPPED to [ToolClip] bytes each, and every other text to
// [TextClip]: what was done matters to a summary, a 300 KiB file the agent read does not, and the
// clip is what bounds one request's cost. A clip says how much it dropped.

// ToolClip and TextClip bound one rendered tool input/result and one other text.
const (
	ToolClip = 2 << 10
	TextClip = 32 << 10
)

// Clip cuts s to at most n bytes on a character boundary and says how much it left out.
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf(" […%d more bytes]", len(s)-cut)
}

// Entry is one rendered record.
type Entry struct {
	Seq  int64
	Text string
}

// Record is one record's labelled lines; "" when it carries nothing a summary reads.
func Record(r pluginapi.Record) string {
	if len(r.Rec) == 0 {
		return "" // a placeholder: the view left the whole record out
	}
	var rec map[string]any
	if json.Unmarshal(r.Rec, &rec) != nil {
		return ""
	}
	if strings.HasPrefix(r.Src, "prt:") || strings.HasPrefix(r.Src, "msg:") {
		return renderOpencode(rec)
	}
	return renderClaude(rec)
}

func str(v any) string { s, _ := v.(string); return s }

func truthy(v any) bool { b, _ := v.(bool); return b }

// textOf is a content value's text: a string, or the `text` of each text block in a list.
func textOf(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, b := range c {
			if bm, ok := b.(map[string]any); ok && (bm["type"] == "text" || bm["type"] == nil) {
				if t := str(bm["text"]); t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func compactJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

func line(label, text string, n int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return "[" + label + "] " + Clip(text, n)
}

func join(lines []string) string {
	var out []string
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func renderClaude(rec map[string]any) string {
	msg, _ := rec["message"].(map[string]any)
	switch str(rec["type"]) {
	case "user":
		if truthy(rec["isCompactSummary"]) {
			return line("RUNTIME-GENERATED compaction summary — written by the agent runtime, not by the human", textOf(msg["content"]), TextClip)
		}
		if truthy(rec["isMeta"]) {
			return line("runtime note", textOf(msg["content"]), ToolClip)
		}
		blocks, isList := msg["content"].([]any)
		if !isList {
			return line("human", textOf(msg["content"]), TextClip)
		}
		hasResult := false
		for _, b := range blocks {
			if bm, _ := b.(map[string]any); bm["type"] == "tool_result" {
				hasResult = true
			}
		}
		var out []string
		for _, b := range blocks {
			bm, _ := b.(map[string]any)
			switch bm["type"] {
			case "tool_result":
				label := "tool result"
				if truthy(bm["is_error"]) {
					label = "tool result, error"
				}
				out = append(out, line(label, textOf(bm["content"]), ToolClip))
			case "text":
				if hasResult {
					out = append(out, line("runtime note", str(bm["text"]), ToolClip))
				} else {
					out = append(out, line("human", str(bm["text"]), TextClip))
				}
			}
		}
		return join(out)
	case "assistant":
		blocks, _ := msg["content"].([]any)
		var out []string
		for _, b := range blocks {
			bm, _ := b.(map[string]any)
			switch bm["type"] {
			case "text":
				out = append(out, line("assistant", str(bm["text"]), TextClip))
			case "tool_use":
				out = append(out, line("tool call "+str(bm["name"]), compactJSON(bm["input"]), ToolClip))
			}
		}
		return join(out)
	case "attachment":
		// The one attachment the conversation view keeps: a message the human typed mid-turn.
		att, _ := rec["attachment"].(map[string]any)
		if att["type"] == "queued_command" && att["commandMode"] == "prompt" {
			return line("human, typed while the agent worked", str(att["prompt"]), TextClip)
		}
		return ""
	case "system":
		sub := strings.ReplaceAll(str(rec["subtype"]), "_", " ")
		if sub == "" {
			sub = "system"
		}
		text := str(rec["content"])
		if text == "" {
			text = "(" + sub + ")"
		}
		return line("RUNTIME-GENERATED "+sub, text, ToolClip)
	}
	return ""
}

func renderOpencode(part map[string]any) string {
	switch str(part["type"]) {
	case "text":
		if truthy(part["synthetic"]) {
			return line("runtime note", str(part["text"]), ToolClip)
		}
		// ⚠ NOT ATTRIBUTED: the role is on the message's `info` record, which the conversation view
		// leaves out as a placeholder, so an opencode text part says so rather than guessing.
		return line("text — opencode, speaker not recorded in this view", str(part["text"]), TextClip)
	case "tool":
		state, _ := part["state"].(map[string]any)
		return join([]string{
			line("tool call "+str(part["tool"]), compactJSON(state["input"]), ToolClip),
			line("tool result", str(state["output"]), ToolClip),
		})
	case "compaction":
		return "[RUNTIME-GENERATED compaction — the agent runtime compacted the conversation here]"
	}
	return ""
}

// Chunks splits entries into runs of at most `limit` rendered bytes. An entry is ITSELF clipped to
// the limit first: the per-line clips above bound each LINE, and one record can carry many lines (a
// user record answering N parallel tool calls renders N results), so without this a single entry
// could make one request any size.
func Chunks(entries []Entry, limit int) [][]Entry {
	var out [][]Entry
	var cur []Entry
	size := 0
	for _, e := range entries {
		e.Text = Clip(e.Text, limit-64)
		n := len(e.Text) + 16
		if len(cur) > 0 && size+n > limit {
			out = append(out, cur)
			cur, size = nil, 0
		}
		cur = append(cur, e)
		size += n
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}
