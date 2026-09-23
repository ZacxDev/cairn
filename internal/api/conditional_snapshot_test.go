package api

import (
	"strings"
	"testing"
)

// snapshotETag is the validator this server hands out for `token` at `target`.
func snapshotETag(t *testing.T, h *harness, target, token string) string {
	t.Helper()
	got := h.do(t, "GET", target, token, nil, "")
	if got.status != 200 {
		t.Fatalf("the unconditional fetch must be a 200, got %d", got.status)
	}
	tag := got.headers.Get("ETag")
	if !strings.HasPrefix(tag, `"sha256:`) {
		t.Fatalf("a 200 on this route carries a strong sha256 validator, got %q", tag)
	}
	return tag
}

func TestAConditionalSnapshotIsAnswered304WithNoBody(t *testing.T) {
	h := newHarness(t)
	tag := snapshotETag(t, h, "/api/v1/snapshot", wideToken)

	got := h.do(t, "GET", "/api/v1/snapshot", wideToken,
		map[string]string{"If-None-Match": tag}, "")
	if got.status != 304 {
		t.Fatalf("a matching validator is a 304, got %d", got.status)
	}
	if got.body != "" {
		t.Fatalf("a 304 carries no content, got %d bytes", len(got.body))
	}
	if got.headers.Get("ETag") != tag {
		t.Fatalf("a 304 restates the validator, got %q", got.headers.Get("ETag"))
	}
	if got.headers.Get("X-Store-Status") != "not-modified" {
		t.Fatalf("X-Store-Status %q", got.headers.Get("X-Store-Status"))
	}
	// 🔴 THE NAMED TRAP. `X-Store-Entries` is the server's count of what it put IN THE
	// BODY, and the client refuses a disagreement between it and its own extracted
	// count. A 304 sent nothing, so the only tree it could be compared against is one
	// this response never described.
	if got.headers.Get("X-Store-Entries") != "" {
		t.Fatalf("a 304 must not describe a body it did not send: X-Store-Entries=%q",
			got.headers.Get("X-Store-Entries"))
	}
	// …and the freshness block DOES ride along, because it dates the copy this pod
	// serves rather than the archive it did not send.
	if !strings.HasPrefix(got.headers.Get("X-Store-Snapshot"), "seeded=") {
		t.Fatalf("X-Store-Snapshot %q", got.headers.Get("X-Store-Snapshot"))
	}
}

// 🔴 READ OFF THE WIRE, NOT OUT OF `resp.Header`. The framing headers are exactly the
// ones an HTTP client normalises away, so the parsed view would be a test of the client.
func TestA304CarriesNeitherFramingHeader(t *testing.T) {
	h := newHarness(t)
	tag := snapshotETag(t, h, "/api/v1/snapshot", wideToken)
	raw := h.raw(t,
		"GET /api/v1/snapshot HTTP/1.1",
		"Host: "+strings.TrimPrefix(h.tsrv.URL, "http://"),
		"Authorization: Bearer "+wideToken,
		"CF-Connecting-IP: 203.0.113.7",
		"If-None-Match: "+tag,
		"Connection: close",
		"", "")
	head, _, _ := strings.Cut(raw, "\r\n\r\n")
	if !strings.HasPrefix(raw, "HTTP/1.1 304 Not Modified\r\n") {
		t.Fatalf("status line: %q", strings.SplitN(raw, "\r\n", 2)[0])
	}
	for _, banned := range []string{"Content-Length:", "Content-Type:", "Transfer-Encoding:"} {
		if strings.Contains(head, banned) {
			t.Fatalf("a 304 describes a body it did not send — %s is present:\n%s",
				banned, head)
		}
	}
	if !strings.Contains(head, "ETag: "+tag) {
		t.Fatalf("the validator is missing from the 304:\n%s", head)
	}
}

func TestAStaleOrAbsentValidatorGetsTheWholeArchive(t *testing.T) {
	h := newHarness(t)
	tag := snapshotETag(t, h, "/api/v1/snapshot", wideToken)
	for _, tc := range []struct{ name, header string }{
		{"a stale tag", `"sha256:0000000000000000000000000000000000000000000000000000000000000000"`},
		{"a weak spelling of the current tag", "W/" + tag},
		{"a tag that is not quoted", strings.Trim(tag, `"`)},
	} {
		got := h.do(t, "GET", "/api/v1/snapshot", wideToken,
			map[string]string{"If-None-Match": tc.header}, "")
		if got.status != 200 {
			t.Errorf("%s must be answered the archive, got %d", tc.name, got.status)
		}
		if got.headers.Get("ETag") != tag {
			t.Errorf("%s: the 200 must carry the CURRENT validator, got %q",
				tc.name, got.headers.Get("ETag"))
		}
		if len(got.body) == 0 {
			t.Errorf("%s: the archive is empty", tc.name)
		}
	}
}

func TestAStarValidatorMatchesAnyRepresentation(t *testing.T) {
	h := newHarness(t)
	got := h.do(t, "GET", "/api/v1/snapshot", wideToken,
		map[string]string{"If-None-Match": "*"}, "")
	if got.status != 304 {
		t.Fatalf("RFC 9110 13.1.2's `*` means any current representation, got %d", got.status)
	}
}

// 🔴 A CONDITIONAL REQUEST IS NOT A WAY PAST A REFUSAL. `*` is the one validator that
// always matches, so if the conditional were evaluated first every one of these would
// become a reassuring 304 over a request the server refused to answer.
//
// ⚠ THIS IS AN **INVARIANT GUARD**, NOT REGRESSION COVERAGE, AND THE LABEL IS THE HONEST
// ONE: measured GREEN at the pre-change base, because a server that never reads
// `If-None-Match` cannot be walked past a refusal by one. It pins the ORDERING the new
// handler chose — refusals first, conditional last — against a future edit that hoists
// the cheap check to the top of the function, which is the obvious "optimisation" and
// the one that would turn a 400 into a 304.
func TestAConditionalDoesNotBypassASnapshotRefusal(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		name, target string
		want         int
	}{
		{"a ?scope= that is not a safe path component", "/api/v1/snapshot?scope=a.b", 400},
		{"a ?scope= that tries to climb", "/api/v1/snapshot?scope=%2e%2e", 400},
	} {
		got := h.do(t, "GET", tc.target, wideToken,
			map[string]string{"If-None-Match": "*"}, "")
		if got.status != tc.want {
			t.Errorf("%s: want %d with a conditional attached, got %d",
				tc.name, tc.want, got.status)
		}
	}
	// …and unauthenticated stays the uniform 401: the conditional is read inside the
	// handler, which an unauthenticated request never reaches.
	got := h.do(t, "GET", "/api/v1/snapshot", "", map[string]string{"If-None-Match": "*"}, "")
	if got.status != 401 {
		t.Fatalf("a conditional must not be a second, weaker door: %d", got.status)
	}
}

// 🔴 THE CROSS-TENANT CHECK, BESIDE THE ONE THAT DOCUMENTS THE LEAK THIS MUST NOT WIDEN.
// `TestTheSnapshotIsNarrowedButFreshnessIsStoreWide` asserts that a wide and a narrow
// principal get the SAME `X-Store-Snapshot` — a store-wide count, deliberate and
// documented. The validator is the opposite by construction: it digests the archive THIS
// caller receives, so it differs, and presenting one caller's tag as another's is
// answered with that other caller's own archive rather than a 304.
func TestTwoPrincipalsDoNotShareASnapshotValidator(t *testing.T) {
	h := newHarness(t)
	wideTag := snapshotETag(t, h, "/api/v1/snapshot", wideToken)
	narrowTag := snapshotETag(t, h, "/api/v1/snapshot", narrowToken)
	if wideTag == narrowTag {
		t.Fatalf("two different visible sets produced one validator: %s", wideTag)
	}
	got := h.do(t, "GET", "/api/v1/snapshot", narrowToken,
		map[string]string{"If-None-Match": wideTag}, "")
	if got.status != 200 {
		t.Fatalf("a narrowed caller presenting the wide validator must be answered its "+
			"OWN archive, got %d", got.status)
	}
	if got.headers.Get("ETag") != narrowTag {
		t.Fatalf("…and with its own validator, got %q", got.headers.Get("ETag"))
	}
	// The `?scope=` filter moves it too, on one principal.
	filtered := snapshotETag(t, h, "/api/v1/snapshot?scope=beta-notes", wideToken)
	if filtered == wideTag {
		t.Fatalf("a scope filter left the validator unchanged at %s", filtered)
	}
}

// A DUPLICATED header is read as ABSENT — `soleHeader`'s rule, which exists because a
// bare "get the header" takes the FIRST value and a working smuggle was built out of
// exactly that. Here the consequence is a full 200: the safe direction.
func TestADuplicatedIfNoneMatchIsReadAsAbsent(t *testing.T) {
	h := newHarness(t)
	tag := snapshotETag(t, h, "/api/v1/snapshot", wideToken)
	raw := h.raw(t,
		"GET /api/v1/snapshot HTTP/1.1",
		"Host: "+strings.TrimPrefix(h.tsrv.URL, "http://"),
		"Authorization: Bearer "+wideToken,
		"CF-Connecting-IP: 203.0.113.7",
		"If-None-Match: "+tag,
		"If-None-Match: "+tag,
		"Connection: close",
		"", "")
	if !strings.HasPrefix(raw, "HTTP/1.1 200 OK\r\n") {
		t.Fatalf("a duplicated conditional must be read as absent: %q",
			strings.SplitN(raw, "\r\n", 2)[0])
	}
}
