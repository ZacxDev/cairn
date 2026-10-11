package redact

import (
	"regexp"
	"strings"
)

// Marker matches the marker [Redactor.marker] writes: `[redacted:<rule>:<8 hex>]`. The rule part
// admits `/` because the base64 path names `base64/<inner rule>`.
var Marker = regexp.MustCompile(`\[redacted:[a-z0-9/-]+:[0-9a-f]{8}\]`)

// isMarker reports whether s is exactly one marker.
func isMarker(s string) bool {
	if !strings.HasPrefix(s, "[redacted:") {
		return false
	}
	loc := Marker.FindStringIndex(s)
	return loc != nil && loc[0] == 0 && loc[1] == len(s)
}

// markerRegions is every marker's [lo, hi) in s.
func markerRegions(s string) [][]int {
	if !strings.Contains(s, "[redacted:") {
		return nil
	}
	return Marker.FindAllStringIndex(s, -1)
}

// touchesMarker reports whether [lo, hi) overlaps any region.
//
// 🔴 THIS IS WHAT MAKES THE TABLE QUIET ON ITS OWN OUTPUT, AND THE POD'S RE-CHECK DEPENDS ON IT
// (plan decision 6: the pod re-scans with the SAME table and refuses on any match). Without it a
// marker is read as a value — `password=[redacted:…]` is a named value, the `rule:tag` inside a
// marker is a `name:value` pair, a DSN regex reads its colons as userinfo — and 1,502 re-scan hits
// over corpus seeds 1–40 would have refused every upload the agent redacted anything in
// (`TestTheRedactorIsQuietOnItsOwnOutput`). A span that touches a marker is ABOUT the marker: what
// it would hide is already hidden.
//
// ⚠ THE RESIDUAL, STATED: a value a rule reads only THROUGH a marker — a key-context value glued
// directly after one, `pw=[redacted:…]tail` — is dropped with the span. A vendor-shaped or random
// token beside a marker is its own span and is still caught (`TestAMarkerDoesNotShieldASecretBesideIt`).
// On text carrying no marker this changes nothing, which is why the corpus, the frozen budget and
// the held-back gate do not move.
func touchesMarker(lo, hi int, regions [][]int) bool {
	for _, m := range regions {
		if lo < m[1] && m[0] < hi {
			return true
		}
	}
	return false
}
