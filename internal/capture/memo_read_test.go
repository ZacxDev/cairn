package capture

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/memo"
)

// TestAMemoTheHookShowedIsARead (#261 round 2), on the AGENT's side of decision 3: a session whose
// only cairn evidence is a gamma-notes memo the memo hook injected — a hook attachment, no
// `subsystem-recall:` header, no command line — has gamma-notes in its `V`, both in what --dry-run
// prints and in what a real run routes by. Before, both were empty and the session was visible to
// any viewer of whatever it wrote, memo text included. CONTROL: the same session without the record
// derives nothing, so the result is the memo's doing.
func TestAMemoTheHookShowedIsARead(t *testing.T) {
	w := loadWorld(t)
	s := w.Sessions["cc-plain"]
	at := time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
	block := memo.RenderPreview([]memo.Stored{memo.NewStored(memo.Fields{ID: 7, Scope: "gamma-notes",
		SenderKind: "user", SenderID: "u-1", SenderDisplay: "someone", Subject: "synthetic subject",
		Body: "synthetic body", CreatedAt: at, ExpiresAt: at.Add(48 * time.Hour)})})
	content, _ := json.Marshal(block)
	record := `{"type":"attachment","attachment":{"type":"hook_additional_context","content":[` + string(content) +
		`]},"sessionId":"` + s.ID + `"}`

	dryV := func(e env) string {
		a := newAgent(t, e, oneInstance, &fakeRunner{}, &bytes.Buffer{})
		var out bytes.Buffer
		if err := a.DryRun(&out); err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(out.String(), "\n") {
			if strings.Contains(line, s.ID) {
				i := strings.Index(line, " v=[")
				j := strings.Index(line[i:], "]")
				return line[i+len(" v=[") : i+j]
			}
		}
		t.Fatalf("--dry-run printed no line for the session:\n%s", out.String())
		return ""
	}

	control := materialize(t, w, "cc-plain")
	if got := dryV(control); got != "" {
		t.Fatalf("CONTROL: the session without the memo record has v=[%s] in --dry-run, want v=[]", got)
	}

	e := materialize(t, w, "cc-plain")
	appendTo(t, filepath.Join(e.claude, s.Project, s.ID+".jsonl"), record)
	if got := dryV(e); got != "gamma-notes" {
		t.Errorf("--dry-run: v=[%s], want v=[gamma-notes]", got)
	}
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	if got := a.State.Sessions[s.ID].V; !slices.Equal(got, []string{"gamma-notes"}) {
		t.Errorf("a real run: V = %v, want [gamma-notes]", got)
	}
}
