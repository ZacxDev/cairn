// Package transcript holds the ONE classification table for session transcripts — Claude Code
// JSONL and opencode export documents — and nothing else yet.
//
// 🔴 IT CLASSIFIES UNITS, NOT RECORDS, BECAUSE A RECORD IS NOT ONE KIND OF THING
// (`claudedocs/plan-cairn-plugins.md`, decision 17). A `tool_result` block lives INSIDE a `user`
// record, and the duplicate `toolUseResult` is a FIELD of that record, so a table keyed on the
// record type would render every tool result as a user message and would have no record to omit
// for "omit the duplicate". A unit is a content block, the duplicate field, an opencode part, or
// — for every record type that is neither `user` nor `assistant` — the whole record.
//
// 🔴 THE TABLE IS A CLOSED DECLARATION OVER AN OPEN FORMAT, AND THE SHAPE TEST IS WHAT KEEPS THE
// TWO HONEST. Both runtimes' record vocabularies are theirs and grow across versions (R1: whether
// either is a documented contract is unverified). So an undeclared record type, block type or
// part type classifies as [Unknown] — SHOWN in raw views and COUNTED, never dropped — and
// `classify_test.go` pins this table against the synthetic world `tests/transcripts/gen.py`
// writes in BOTH directions: a type or field the generator emits and the table does not declare
// is red, and so is one the table declares and the generator never emits. That is what turns
// drift into a red test rather than a silent drop. ⚠ It pins the table to the GENERATOR, and the
// generator to the key sets measured on ONE host at ONE version of each runtime; a real session
// can still carry a type neither has seen, which is exactly why [Unknown] exists.
//
// ⚠ WHAT IS NOT HERE. Binary content is classified by POSITION only (an `image` item, the
// `toolUseResult.file.base64` value, an opencode attachment). Whether a value IS binary by
// CONTENT — the text/binary sniff — is `internal/redact`'s. Under operator decision O12 binary
// content is SHIPPED, not withheld; the [Binary] class is a DISPLAY label (collapsed), nothing
// more.
package transcript

import "strconv"

// Class is decision 17's display class of one unit.
type Class string

// The classes. The set is closed; a renderer switches over it.
const (
	HumanText     Class = "human-text"
	// AgentPrompt is text an AGENT wrote to a subagent: a sidechain `user` record, an opencode
	// child session's `user` text. It is never a user message — only what a human typed into a
	// ROOT session is.
	AgentPrompt Class = "agent-prompt"
	AssistantText Class = "assistant-text"
	Thinking      Class = "thinking"
	ToolCall      Class = "tool-call"
	ToolResult    Class = "tool-result"
	Duplicate     Class = "duplicate"
	Runtime       Class = "runtime"
	Bookkeeping   Class = "bookkeeping"
	Binary        Class = "binary"
	ChildLink     Class = "child-link"
	Unknown       Class = "unknown"
)

// Unit is one classified piece of a record: its class and a JSON-pointer-ish path into the
// record naming WHERE it is (`/message/content/2`, `/toolUseResult`, `/state/attachments/0`).
type Unit struct {
	Class Class
	Path  string
}

// Shape declares one type: the class a WHOLE record or part of this type takes (empty for the
// types that split into finer units), and the keys the type is declared to carry.
type Shape struct {
	Class  Class
	Fields []string
}

// envelope is the key set every Claude Code CONVERSATION record carries (R1), and agentId,
// which only a subagent's records do.
var envelope = []string{
	"agentId", "cwd", "entrypoint", "gitBranch", "isSidechain", "parentUuid", "sessionId",
	"timestamp", "type", "userType", "uuid", "version",
}

func with(base []string, extra ...string) []string {
	return append(append([]string(nil), base...), extra...)
}

// ClaudeRecords declares every top-level `type` value R1 measured, with the class a whole record
// takes and its key set. `user` and `assistant` have no whole-record class: they split into
// blocks ([ClaudeBlocks]) and, for `user`, the duplicate field.
var ClaudeRecords = map[string]Shape{
	"user": {Fields: with(envelope, "message", "promptId", "isMeta", "isCompactSummary",
		"isVisibleInTranscriptOnly", "toolUseResult", "sourceToolAssistantUUID")},
	"assistant":  {Fields: with(envelope, "message", "requestId")},
	"attachment": {Class: Bookkeeping, Fields: with(envelope, "attachment")},
	"system": {Class: Runtime, Fields: with(envelope, "subtype", "content", "isMeta", "level",
		"compactMetadata", "logicalParentUuid", "durationMs", "messageCount", "hookCount", "hookInfos",
		"hookErrors", "preventedContinuation", "stopReason", "hasOutput", "toolUseID")},
	"queue-operation":           {Class: Bookkeeping, Fields: []string{"type", "sessionId", "operation", "timestamp", "content"}},
	"last-prompt":               {Class: Bookkeeping, Fields: []string{"type", "sessionId", "lastPrompt", "leafUuid"}},
	"permission-mode":           {Class: Bookkeeping, Fields: []string{"type", "sessionId", "permissionMode"}},
	"mode":                      {Class: Bookkeeping, Fields: []string{"type", "sessionId", "mode"}},
	"pr-link":                   {Class: Bookkeeping, Fields: []string{"type", "sessionId", "prNumber", "prUrl", "prRepository", "timestamp"}},
	"ai-title":                  {Class: Bookkeeping, Fields: []string{"type", "sessionId", "aiTitle"}},
	"atis-latch":                {Class: Bookkeeping, Fields: []string{"type", "sessionId", "atis"}},
	"bridge-session":            {Class: Bookkeeping, Fields: []string{"type", "sessionId", "bridgeSessionId", "lastSequenceNum", "noHistoryBackfill", "ownerAccountUuid", "ownerOrganizationUuid"}},
	"history-suppression":       {Class: Bookkeeping, Fields: []string{"type", "sessionId", "cause", "vetoedAgainstAccountUuid", "ts"}},
	"frame-link":                {Class: Bookkeeping, Fields: []string{"type", "sessionId", "path", "frameUrl", "title", "artifactCount", "timestamp"}},
	"cost-state":                {Class: Bookkeeping, Fields: []string{"type", "sessionId", "totalCostUSD", "totalAPIDuration", "totalDuration", "totalLinesAdded", "totalLinesRemoved", "startTime"}},
	"agent-name":                {Class: Bookkeeping, Fields: []string{"type", "sessionId", "agentName"}},
	"artifact-autoreact-ledger": {Class: Bookkeeping, Fields: []string{"type", "sessionId", "v", "accountUuid"}},
	"artifact-comment-monitor":  {Class: Bookkeeping, Fields: []string{"type", "sessionId", "v", "artifacts"}},
	"continued-in":              {Class: Bookkeeping, Fields: []string{"type", "sessionId", "timestamp", "continuedInSessionId"}},
}

// ClaudeBlocks declares the content-block types of `user`/`assistant` records. `text` has no
// fixed class: it is human, assistant or runtime text depending on the record (see
// [ClassifyClaude]).
var ClaudeBlocks = map[string]Shape{
	"text":        {Fields: []string{"type", "text"}},
	"thinking":    {Class: Thinking, Fields: []string{"type", "thinking", "signature"}},
	"tool_use":    {Class: ToolCall, Fields: []string{"type", "id", "name", "input", "caller"}},
	"tool_result": {Class: ToolResult, Fields: []string{"type", "tool_use_id", "content", "is_error"}},
}

// ClaudeToolResultItems declares the item types of a `tool_result` block whose content is a
// LIST (R1). An `image` item is its own [Binary] unit.
var ClaudeToolResultItems = map[string]Shape{
	"text":           {Class: ToolResult, Fields: []string{"type", "text"}},
	"image":          {Class: Binary, Fields: []string{"type", "source"}},
	"tool_reference": {Class: ToolResult, Fields: []string{"type", "tool_name"}},
}

// ClaudeSystemSubtypes declares the `system` record subtypes R2 measured. Every one is [Runtime].
var ClaudeSystemSubtypes = []string{
	"agents_killed", "away_summary", "compact_boundary", "informational", "local_command",
	"scheduled_task_fire", "stop_hook_summary", "turn_duration",
}

// ClaudeAttachmentTypes declares the `attachment` subtypes the synthetic world carries. The real
// set is wider (R1 measured more than thirty); every attachment is [Bookkeeping] whatever its
// subtype, so this list governs the FIXTURE's coverage, not classification.
var ClaudeAttachmentTypes = []string{"credential_org", "file", "hook_additional_context", "hook_success"}

// partEnvelope is the key set every opencode part carries.
var partEnvelope = []string{"type", "id", "sessionID", "messageID"}

// OpencodeParts declares the `part.type` values of an opencode export (R3) with their class and
// keys. `text` has no fixed class (human, assistant or runtime by role and `synthetic`), and
// `tool` splits into a call, a result and one binary unit per attachment.
//
// ⚠ `subtask` IS DELIBERATELY ABSENT. R3 counted two such parts in one database, and none of the
// 120 most recent sessions' exports carried one, so its keys are unmeasured; a declared shape
// would be a guess. It classifies as [Unknown] — shown and counted. The child-session link that
// WAS measured is the `task` tool's `state.metadata.sessionId` (45 of 45 child sessions across six
// roots), which is what decision 7 reads.
var OpencodeParts = map[string]Shape{
	"text":        {Fields: with(partEnvelope, "text", "time", "synthetic")},
	"reasoning":   {Class: Thinking, Fields: with(partEnvelope, "text", "time")},
	"tool":        {Fields: with(partEnvelope, "callID", "tool", "state")},
	"step-start":  {Class: Bookkeeping, Fields: with(partEnvelope, "snapshot")},
	"step-finish": {Class: Bookkeeping, Fields: with(partEnvelope, "reason", "snapshot", "cost", "tokens")},
	"patch":       {Class: Bookkeeping, Fields: with(partEnvelope, "hash", "files")},
	"file":        {Class: Binary, Fields: with(partEnvelope, "mime", "filename", "url")},
	"compaction":  {Class: Runtime, Fields: partEnvelope},
}

// OpencodeToolStates declares the keys of a `tool` part's `state` per `status`.
var OpencodeToolStates = map[string][]string{
	"running":   {"status", "input", "time"},
	"completed": {"status", "input", "output", "metadata", "title", "time", "attachments"},
	"error":     {"status", "input", "error", "time"},
}

// ClassifyClaude splits one decoded Claude Code record into its units.
//
// The rules, in decision 17's words: a `text` block, or string content, of a `user` record is
// [HumanText] only when that record has NO `tool_result` block, NO `toolUseResult`, and is not
// `isMeta` or `isCompactSummary` — so "a user message is a human-text unit and nothing else". The
// runtime's compaction summary and `isMeta` content are [Runtime]. The `toolUseResult` field is
// one [Duplicate] unit, plus a [Binary] unit for `toolUseResult.file.base64` when present.
func ClassifyClaude(rec map[string]any) []Unit {
	kind, _ := rec["type"].(string)
	shape, declared := ClaudeRecords[kind]
	if !declared {
		return []Unit{{Class: Unknown, Path: ""}}
	}
	if shape.Class != "" {
		return []Unit{{Class: shape.Class, Path: ""}}
	}
	msg, _ := rec["message"].(map[string]any)
	var units []Unit
	if kind == "user" {
		runtime := truthy(rec["isMeta"]) || truthy(rec["isCompactSummary"])
		_, hasDuplicate := rec["toolUseResult"]
		hasResult := false
		if blocks, ok := msg["content"].([]any); ok {
			for _, b := range blocks {
				if bm, ok := b.(map[string]any); ok && bm["type"] == "tool_result" {
					hasResult = true
				}
			}
		}
		textClass := HumanText
		if truthy(rec["isSidechain"]) {
			// Every `user` record of a subagent file was written by the PARENT agent, not a
			// human: the prompt, and anything the runtime relays into the sidechain.
			textClass = AgentPrompt
		}
		switch {
		case runtime:
			textClass = Runtime
		case hasResult || hasDuplicate:
			// Text beside a tool result is runtime-injected (an interruption note, a
			// rejection), never something the human typed.
			textClass = Runtime
		}
		switch content := msg["content"].(type) {
		case string:
			units = append(units, Unit{Class: textClass, Path: "/message/content"})
		case []any:
			units = append(units, blockUnits(content, textClass)...)
		default:
			units = append(units, Unit{Class: Unknown, Path: "/message/content"})
		}
		if dup, ok := rec["toolUseResult"]; ok {
			units = append(units, Unit{Class: Duplicate, Path: "/toolUseResult"})
			if dm, ok := dup.(map[string]any); ok {
				if file, ok := dm["file"].(map[string]any); ok {
					if _, ok := file["base64"].(string); ok {
						units = append(units, Unit{Class: Binary, Path: "/toolUseResult/file/base64"})
					}
				}
			}
		}
		return units
	}
	// assistant
	if content, ok := msg["content"].([]any); ok {
		return blockUnits(content, AssistantText)
	}
	return []Unit{{Class: Unknown, Path: "/message/content"}}
}

func blockUnits(blocks []any, textClass Class) []Unit {
	var units []Unit
	for i, b := range blocks {
		path := "/message/content/" + strconv.Itoa(i)
		bm, _ := b.(map[string]any)
		kind, _ := bm["type"].(string)
		shape, declared := ClaudeBlocks[kind]
		switch {
		case !declared:
			units = append(units, Unit{Class: Unknown, Path: path})
		case kind == "text":
			units = append(units, Unit{Class: textClass, Path: path})
		default:
			units = append(units, Unit{Class: shape.Class, Path: path})
		}
		if kind == "tool_result" {
			if items, ok := bm["content"].([]any); ok {
				for j, it := range items {
					im, _ := it.(map[string]any)
					ik, _ := im["type"].(string)
					if is, ok := ClaudeToolResultItems[ik]; ok && is.Class == Binary {
						units = append(units, Unit{Class: Binary, Path: path + "/content/" + strconv.Itoa(j)})
					} else if !ok {
						units = append(units, Unit{Class: Unknown, Path: path + "/content/" + strconv.Itoa(j)})
					}
				}
			}
		}
	}
	return units
}

// ClassifyOpencodePart splits one opencode part into its units. `role` is the owning message's
// `info.role`; `child` is true for a part of a CHILD session (one whose export carries a
// `parentID`), whose `user` text an agent wrote — [AgentPrompt], never [HumanText].
func ClassifyOpencodePart(role string, child bool, part map[string]any) []Unit {
	kind, _ := part["type"].(string)
	shape, declared := OpencodeParts[kind]
	switch {
	case !declared:
		return []Unit{{Class: Unknown}}
	case kind == "text":
		switch {
		case truthy(part["synthetic"]):
			return []Unit{{Class: Runtime}}
		case role == "user" && child:
			return []Unit{{Class: AgentPrompt}}
		case role == "user":
			return []Unit{{Class: HumanText}}
		case role == "assistant":
			return []Unit{{Class: AssistantText}}
		default:
			return []Unit{{Class: Unknown}}
		}
	case kind == "tool":
		units := []Unit{{Class: ToolCall, Path: "/state/input"}, {Class: ToolResult, Path: "/state/output"}}
		state, _ := part["state"].(map[string]any)
		if atts, ok := state["attachments"].([]any); ok {
			for i := range atts {
				units = append(units, Unit{Class: Binary, Path: "/state/attachments/" + strconv.Itoa(i)})
			}
		}
		if md, ok := state["metadata"].(map[string]any); ok && part["tool"] == "task" {
			if child, ok := md["sessionId"].(string); ok && child != "" {
				units = append(units, Unit{Class: ChildLink, Path: "/state/metadata/sessionId"})
			}
		}
		return units
	default:
		return []Unit{{Class: shape.Class}}
	}
}

func truthy(v any) bool {
	b, ok := v.(bool)
	return ok && b
}
