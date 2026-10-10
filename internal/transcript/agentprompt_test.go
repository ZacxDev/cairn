package transcript

import (
	"strings"
	"testing"
)

// TestHumanTextComesOnlyFromRootSessions walks the synthetic world: every subagent file's and
// every opencode CHILD session's `user` text is an agent's prompt, so NO human-text unit may come
// from one — and the root sessions must still yield human text (the positive control, so a
// classifier that called nothing human-text cannot pass).
func TestHumanTextComesOnlyFromRootSessions(t *testing.T) {
	w := loadWorld(t)
	rootHuman, subHuman, subPrompt := 0, 0, 0
	for path, file := range w.Claude.Files {
		if !strings.HasSuffix(path, ".jsonl") {
			continue
		}
		sub := strings.Contains(path, "/subagents/")
		for _, line := range strings.SplitAfter(file["utf8"], "\n") {
			if line == "" {
				continue
			}
			for _, u := range ClassifyClaude(decode(t, []byte(line))) {
				switch {
				case u.Class == HumanText && sub:
					subHuman++
				case u.Class == HumanText:
					rootHuman++
				case u.Class == AgentPrompt && sub:
					subPrompt++
				}
			}
		}
	}
	childHuman, childPrompt := 0, 0
	for _, docs := range w.Opencode.Exports {
		for _, raw := range docs {
			doc := decode(t, raw)
			_, child := doc["info"].(map[string]any)["parentID"]
			for _, m := range doc["messages"].([]any) {
				mm := m.(map[string]any)
				role := mm["info"].(map[string]any)["role"].(string)
				for _, p := range mm["parts"].([]any) {
					for _, u := range ClassifyOpencodePart(role, child, p.(map[string]any)) {
						if child && u.Class == HumanText {
							childHuman++
						}
						if child && u.Class == AgentPrompt {
							childPrompt++
						}
					}
				}
			}
		}
	}
	if rootHuman == 0 {
		t.Fatal("positive control: no human text in any ROOT session — the walk proves nothing")
	}
	if subHuman != 0 || childHuman != 0 {
		t.Fatalf("an agent's prompt was classed as human text: %d in subagent files, %d in child sessions", subHuman, childHuman)
	}
	if subPrompt == 0 || childPrompt == 0 {
		t.Fatalf("agent-prompt never reached: %d subagent, %d child", subPrompt, childPrompt)
	}
}
