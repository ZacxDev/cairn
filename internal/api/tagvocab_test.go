package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/write"
)

// entryWithTags is one entry body carrying the raw `tags:` flow text given.
func entryWithTags(service, scope, tagsFlow string) string {
	tagLine := ""
	if tagsFlow != "" {
		tagLine = "tags: [" + tagsFlow + "]\n"
	}
	return "---\nservice: " + service + "\nscope: " + scope + "\n" + tagLine +
		"---\n\n## What it is\nsynthetic.\n\n## Nuance / work-history\n- 2000-01-02: a note.\n"
}

// theVocabularyRefusal is the 422 body both servers must send, spelled out rather than
// built from the implementation. See `internal/write/tagvocab_test.go` for why a
// derived expectation would assert nothing.
const theVocabularyRefusal = "unprocessable: tag 'marketing' is not one of " +
	"infra|product|tooling — the tag vocabulary is CLOSED on the WRITE path, " +
	"so widening it is a code change. The index loader still READS this tag: an entry " +
	"already carrying it is unaffected\n"

// TestAPUTCarryingAnOffVocabularyTagIsRefusedOnTheWire pins the closed tag vocabulary at
// the HTTP surface, on BOTH halves of PUT, with the store observed unchanged.
//
// 🔴 422 AND `X-Store-Status: entry-shape`, REUSED RATHER THAN GIVEN A TOKEN OF ITS OWN,
// AND THAT IS A DECISION. `X-Store-Status` discriminates REMEDIES, which is the argument
// `entryExists` makes for not collapsing into `precondition-failed`: one of those two
// says "re-sync and re-apply" and the other says "you wanted a replace", and a client
// that retries the wrong one loops forever. An off-vocabulary tag and a body the loader
// rejects have the SAME remedy — fix the front matter and resend — so a second token
// would be a distinction with no branch behind it. The difference rides in the BODY,
// which is where the four valid terms have to be anyway.
//
// 🔴 AND THE FILE IS CHECKED AFTER EVERY REFUSAL. A 422 that has already written the
// bytes is the failure this design exists to prevent, and a status code is not evidence
// about the filesystem.
func TestAPUTCarryingAnOffVocabularyTagIsRefusedOnTheWire(t *testing.T) {
	h := newHarness(t)
	entryPath := filepath.Join(h.root, "alpha-notes", "gadget-one.md")
	original, err := os.ReadFile(entryPath)
	if err != nil {
		t.Fatal(err)
	}
	revision := write.EntryRevision(original)

	t.Run("replace", func(t *testing.T) {
		got := h.do(t, "PUT", "/api/v1/entry/alpha-notes/gadget-one", wideToken,
			map[string]string{"If-Match": `"` + revision + `"`},
			entryWithTags("gadget-one", "alpha-notes", "marketing"))
		if got.status != 422 {
			t.Fatalf("status=%d, want 422\nbody: %s", got.status, got.body)
		}
		if got.headers.Get("X-Store-Status") != "entry-shape" {
			t.Fatalf("X-Store-Status=%q, want entry-shape",
				got.headers.Get("X-Store-Status"))
		}
		if got.body != theVocabularyRefusal {
			t.Fatalf("the refused body is not the declared sentence.\nwant: %q\ngot:  %q",
				theVocabularyRefusal, got.body)
		}
		after, readErr := os.ReadFile(entryPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(after) != string(original) {
			t.Fatalf("a refused replace changed the entry on disk:\n%s", string(after))
		}
	})

	t.Run("create", func(t *testing.T) {
		target := filepath.Join(h.root, "alpha-notes", "newcomer.md")
		got := h.do(t, "PUT", "/api/v1/entry/alpha-notes/newcomer", wideToken,
			map[string]string{"If-None-Match": "*"},
			entryWithTags("newcomer", "alpha-notes", "marketing"))
		if got.status != 422 {
			t.Fatalf("status=%d, want 422\nbody: %s", got.status, got.body)
		}
		if got.headers.Get("X-Store-Status") != "entry-shape" {
			t.Fatalf("X-Store-Status=%q, want entry-shape",
				got.headers.Get("X-Store-Status"))
		}
		if got.body != theVocabularyRefusal {
			t.Fatalf("the refused body is not the declared sentence.\nwant: %q\ngot:  %q",
				theVocabularyRefusal, got.body)
		}
		// The name must still be FREE, so a caller that fixes the tag can retry into it.
		if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
			t.Fatalf("a refused create left something at %s (stat err %v)", target, statErr)
		}
	})

	// 🔴 THE POSITIVE CONTROL, AND WITHOUT IT EVERY ASSERTION ABOVE IS SATISFIED BY A
	// SERVER THAT REFUSES EVERY PUT. A declared tag must LAND: 201 for the create, 200
	// for the replace, and the tag readable afterwards.
	//
	// ⚠ IT USES A REF OF ITS OWN (`arrival`, not the `newcomer` the create subtest
	// addresses), AND THAT IS NOT TIDINESS. Sharing one ref made this control fail
	// UNDER A MUTATION rather than under the defect it measures: with the vocabulary
	// check deleted, the create subtest's refused PUT lands `newcomer.md` instead, and
	// this one then meets 412 `already-exists` — a second failure attributable to the
	// first, which is exactly the noise a control must not add.
	t.Run("a DECLARED tag lands", func(t *testing.T) {
		created := h.do(t, "PUT", "/api/v1/entry/alpha-notes/arrival", wideToken,
			map[string]string{"If-None-Match": "*"},
			entryWithTags("arrival", "alpha-notes", "infra"))
		if created.status != 201 {
			t.Fatalf("a create carrying `infra` answered %d\nbody: %s",
				created.status, created.body)
		}
		landed, readErr := os.ReadFile(filepath.Join(h.root, "alpha-notes", "arrival.md"))
		if readErr != nil {
			t.Fatalf("the create answered 201 and wrote nothing: %v", readErr)
		}
		if !strings.Contains(string(landed), "tags: [infra]") {
			t.Fatalf("the create dropped the tag:\n%s", string(landed))
		}
		replaced := h.do(t, "PUT", "/api/v1/entry/alpha-notes/arrival", wideToken,
			map[string]string{"If-Match": `"` + write.EntryRevision(landed) + `"`},
			entryWithTags("arrival", "alpha-notes", "ToOlInG"))
		if replaced.status != 200 {
			t.Fatalf("a replace carrying `ToOlInG` answered %d\nbody: %s",
				replaced.status, replaced.body)
		}
		// ⚠ AND THE UNFOLDED SPELLING IS ACCEPTED, which is what stops the gate being a
		// case-sensitive allowlist an operator cannot type.
		again, readErr := os.ReadFile(filepath.Join(h.root, "alpha-notes", "arrival.md"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !strings.Contains(string(again), "tags: [ToOlInG]") {
			t.Fatalf("the replace did not land:\n%s", string(again))
		}
	})
}

// TestAnEntryALREADYCarryingAnOffVocabularyTagStaysREADABLEAndAPPENDABLE is the outage
// guard at the surface an operator actually uses.
//
// 🔴 THIS IS THE CASE THAT DECIDED WHERE THE CHECK LIVES. A vocabulary refusal in
// `store.parseTagsField` would make such an entry MALFORMED: out of the index, so this
// recall would not show it and `--ref`/`--search` would lose it, AND unwritable, because
// `POST .../bullets` resolves its target through that same index and would answer 404
// `ref-unknown` for a file sitting right there. The pod's own notes store already holds
// entries with tags predating the vocabulary, so that is not a hypothetical.
//
// ⚠ IT ASSERTS THE 200 **AND** THE READ, because either alone is satisfiable by the
// broken design: a reader-side refusal that happened to keep the write route working
// would pass the append half, and a store that merely parses would pass nothing about
// the route.
func TestAnEntryALREADYCarryingAnOffVocabularyTagStaysREADABLEAndAPPENDABLE(t *testing.T) {
	h := newHarness(t)
	legacy := entryWithTags("legacy-note", "alpha-notes", "Marketing, project-xyz")
	if err := os.WriteFile(filepath.Join(h.root, "alpha-notes", "legacy-note.md"),
		[]byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	// READABLE: it is in the index, so a ref read finds it rather than reporting it
	// malformed.
	read := h.do(t, "GET", "/api/v1/recall/alpha-notes?ref=legacy-note", wideToken, nil, "")
	if read.status != 200 {
		t.Fatalf("reading an entry with off-vocabulary tags answered %d\nbody: %s",
			read.status, read.body)
	}
	if strings.Contains(read.body, "malformed index entry") {
		t.Fatalf("the entry was reported MALFORMED:\n%s", read.body)
	}
	if !strings.Contains(read.body, "legacy-note") {
		t.Fatalf("the recall did not name the entry:\n%s", read.body)
	}

	// APPENDABLE: the write route the store is actually written through.
	appended := h.do(t, "POST", "/api/v1/entry/alpha-notes/legacy-note/bullets", wideToken,
		map[string]string{"Content-Type": "application/json"},
		`{"text":"a synthetic observation.","session":"sess-1"}`)
	if appended.status != 200 {
		t.Fatalf("appending to an entry with off-vocabulary tags answered %d\nbody: %s",
			appended.status, appended.body)
	}
	if appended.headers.Get("X-Store-Status") != "appended" {
		t.Fatalf("X-Store-Status=%q, want appended", appended.headers.Get("X-Store-Status"))
	}
	after, err := os.ReadFile(filepath.Join(h.root, "alpha-notes", "legacy-note.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "a synthetic observation.") {
		t.Fatalf("the append answered 200 and wrote nothing:\n%s", string(after))
	}
	// ⚠ AND THE FRONT MATTER IS UNTOUCHED. An append that "fixed" the tags on the way
	// past would be a silent rewrite of somebody's file.
	if !strings.Contains(string(after), "tags: [Marketing, project-xyz]") {
		t.Fatalf("the append rewrote the front matter:\n%s", string(after))
	}

	// The CONTROL on this test's own reach: a PUT of that same body — the shape a
	// whole-file edit takes — IS refused, so the 200 above is a fact about the APPEND
	// route and not about a gate wired to nothing.
	refused := h.do(t, "PUT", "/api/v1/entry/alpha-notes/legacy-note", wideToken,
		map[string]string{"If-Match": `"` + write.EntryRevision(after) + `"`},
		string(after))
	if refused.status != 422 {
		t.Fatalf("re-PUTting the legacy body answered %d, so this test cannot tell a live "+
			"gate from an absent one\nbody: %s", refused.status, refused.body)
	}
}
