package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// requirementsWorld writes one entry carrying a `## Requirements` section whose bullets are
// deliberately NOT in open-first order on disk, plus a nuance section carrying a bullet
// byte-identical to one of the requirements.
//
// 🔴 THE DISK ORDER IS THE POINT. If the fixture were already open-first, a partition that
// did nothing would pass — the order on screen would be right by accident of the input, which
// is the shape a test cannot distinguish from a working partition.
func requirementsWorld(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the scope dir: %v", err)
	}
	body := strings.Join([]string{
		"---",
		"service: runbook",
		"scope: alpha-notes",
		"---",
		"",
		"## What it is",
		"",
		"The rollout runbook.",
		"",
		"## Requirements",
		"",
		// met first, then unmarked, then open — the INVERSE of what must render.
		"- RESOLVED def5678: (operator) met, and it is written FIRST on disk",
		"- 2000-01-09: unmarked, and it is written SECOND",
		"- OPEN: (inferred) open, and it is written THIRD",
		"- OPEN: (operator) open, and it is written FOURTH",
		"",
		"## Nuance / work-history",
		"",
		// Byte-identical to the fourth requirement above: the section boundary is the
		// only thing that stops it being counted twice.
		"- OPEN: (operator) open, and it is written FOURTH",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "runbook.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	return root
}

func theRequirementsSection(t *testing.T, item Entry) Section {
	t.Helper()
	for _, s := range item.Sections {
		if s.Heading == store.RequirementsHeading {
			return s
		}
	}
	t.Fatalf("the entry carries no %q section; it has %d sections",
		store.RequirementsHeading, len(item.Sections))
	return Section{}
}

// TestTheEntryPagePutsOpenRequirementsFirst is MY requirement, not the card's — labelled so,
// because the card's criterion 6 asks only that `internal/ui`'s tests stay green plus a grep.
//
// 🔴 WHAT EARNS IT: the open-first partition is NEW, NON-OBVIOUS logic that no other guard
// covers, and a regression in it is invisible — the page still renders every requirement, just
// in an order that buries the ones needing action. Nothing about the output would look wrong.
//
// ⚠ IT ASSERTS THE ORDER *AND* THE WITHIN-RUN ORDER, because the partition promises both:
// open items first, and FILE ORDER preserved inside each run. A `sort` would satisfy the first
// and silently invent a priority for the second.
func TestTheEntryPagePutsOpenRequirementsFirst(t *testing.T) {
	src := StoreSource{Root: requirementsWorld(t)}
	item := readTheOneEntry(t, src)
	section := theRequirementsSection(t, item)

	if len(section.Bullets) != 4 {
		t.Fatalf("the section rendered %d bullets, want 4", len(section.Bullets))
	}

	// The two OPEN bullets must come first, and in the order the FILE has them —
	// `(inferred)` was written third and `(operator)` fourth, so inferred precedes operator.
	wantOrder := []struct{ population, provenance, marker string }{
		{store.PopulationOpen, store.ProvenanceInferred, "written THIRD"},
		{store.PopulationOpen, store.ProvenanceOperator, "written FOURTH"},
		{store.PopulationResolved, store.ProvenanceOperator, "written FIRST"},
		{store.PopulationNone, store.ProvenanceAbsent, "written SECOND"},
	}
	for i, want := range wantOrder {
		got := section.Bullets[i]
		if got.Population != want.population {
			t.Errorf("bullet %d population = %q, want %q", i, got.Population, want.population)
		}
		if got.Provenance != want.provenance {
			t.Errorf("bullet %d provenance = %q, want %q", i, got.Provenance, want.provenance)
		}
		if !strings.Contains(strings.Join(got.Lines, "\n"), want.marker) {
			t.Errorf("bullet %d is not the one %q — got %q", i, want.marker, got.Lines)
		}
	}
}

// TestANuanceBulletNeverBecomesARequirementOnThePage is the section boundary, on THIS surface.
//
// 🔴 IT NEEDS ITS OWN GUARD BECAUSE THE PAGE DOES NOT GO THROUGH `report.Recall`. `readEntry`
// builds the sections itself, so a boundary that holds in the renderer says nothing about here
// — the same reason the tag page needed its own authorisation-order test rather than inheriting
// `internal/report`'s.
//
// ⚠ IT IS A PAIR, NOT A ZERO: the identical line must appear ONCE under each heading. Asserting
// only that requirements holds 4 would pass if the nuance section had silently lost its copy.
func TestANuanceBulletNeverBecomesARequirementOnThePage(t *testing.T) {
	src := StoreSource{Root: requirementsWorld(t)}
	item := readTheOneEntry(t, src)

	const shared = "open, and it is written FOURTH"
	counts := map[string]int{}
	for _, s := range item.Sections {
		for _, b := range s.Bullets {
			if strings.Contains(strings.Join(b.Lines, "\n"), shared) {
				counts[s.Heading]++
			}
		}
	}
	if counts[store.RequirementsHeading] != 1 {
		t.Errorf("the shared line appears %d times under %q, want 1",
			counts[store.RequirementsHeading], store.RequirementsHeading)
	}
	if counts[store.NuanceHeading] != 1 {
		t.Errorf("the shared line appears %d times under %q, want 1 — if this is 0 the test "+
			"above proves nothing, because there would be no second copy to confuse",
			counts[store.NuanceHeading], store.NuanceHeading)
	}
	// And the nuance bullet carries NO provenance, however it is spelled: provenance is a
	// requirements-section concept and a nuance bullet states no requirement.
	for _, s := range item.Sections {
		if s.Heading != store.NuanceHeading {
			continue
		}
		for _, b := range s.Bullets {
			if b.Provenance != store.ProvenanceAbsent {
				t.Errorf("a nuance bullet carries provenance %q, want none", b.Provenance)
			}
		}
	}
}
