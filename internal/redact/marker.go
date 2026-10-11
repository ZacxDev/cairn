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

// withoutMarkers is sp minus every marker region: the pieces of [lo, hi) no marker covers, in
// ascending order, each keeping sp's rule and prio. A span inside one marker yields nothing.
//
// 🔴 THIS IS WHAT MAKES THE TABLE QUIET ON ITS OWN OUTPUT, AND THE POD'S RE-CHECK DEPENDS ON IT
// (plan decision 6: the pod re-scans with the SAME table and refuses on any match). Without it a
// marker is read as a value — `password=[redacted:…]` is a named value, the `rule:tag` inside a
// marker is a `name:value` pair, a DSN regex reads its colons as userinfo — and 1,502 re-scan hits
// over corpus seeds 1–40 would have refused every upload the agent redacted anything in
// (`TestTheRedactorIsQuietOnItsOwnOutput`). What a marker would hide is already hidden.
//
// 🔴 IT SUBTRACTS, IT DOES NOT DROP — and dropping was measured WRONG. A first cut let through every
// span that TOUCHED a marker, and a key-context value glued directly BEFORE one
// (`password=<v>[redacted:…]`, and the same under `export`, YAML, a CLI flag, a JSON text field
// and a DSN's userinfo) was caught at the base and shipped by that cut: the rule's span was
// `<v>[redacted:…]`, it touched the marker, and the secret went with it. Subtracting keeps the
// marker byte-identical and still redacts `<v>` (`TestAMarkerDoesNotShieldASecretBesideIt`).
//
// ⚠ THE RESIDUAL, STATED AND MEASURED, AND NOT THIS FUNCTION'S: a low-entropy value glued
// directly AFTER a marker under a key — `password=[redacted:…]<v>`, `db_password: [redacted:…]<v>`
// — ships, at the base, at the dropping cut and here alike, because no rule produces a span for
// it: key-context reads `[redacted:…]<v>` as the value and its value filter refuses that shape, so
// there is nothing to subtract from. The same filter refuses a WORD-LIKE value glued before a
// marker (`password=tulipwagon42[redacted:…]` reads as an index expression), also at the base. A
// span that BEGINS inside a marker is dropped whole (below), which can only lose a tail glued
// after that marker — the same class. A value that is also high-entropy, or a vendor-shaped
// token, is its own span and is caught. Those rows are pinned as declared residuals in
// `TestAMarkerDoesNotShieldASecretBesideIt`. On text carrying no marker this function is never
// called, which is why the corpus, the frozen budget and the held-back gate do not move.
func withoutMarkers(sp span, regions [][]int) []span {
	var out []span
	lo := sp.lo
	for _, m := range regions {
		if m[1] <= lo {
			continue
		}
		if m[0] >= sp.hi {
			break
		}
		if m[0] < sp.lo {
			// The match BEGAN inside a marker, so its anchor is the marker's own text — the
			// `password` in `url-userinfo-password:<tag>]` read as a key, whose "value" runs on
			// past the `]`. Everything it read is ABOUT the marker; redacting the tail would
			// rewrite `@host:port/db` on every re-scan (measured: 80 re-scan hits over seeds 1–40).
			return nil
		}
		if m[0] > lo {
			out = append(out, span{lo: lo, hi: m[0], rule: sp.rule, prio: sp.prio})
		}
		lo = m[1]
	}
	if lo < sp.hi {
		out = append(out, span{lo: lo, hi: sp.hi, rule: sp.rule, prio: sp.prio})
	}
	return out
}
