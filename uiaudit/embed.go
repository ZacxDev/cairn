package main

import _ "embed"

// 🔴 ALL THREE ARE THE HUB'S OWN BYTES, VERBATIM, AND THE VERBATIM PART IS A CONTRACT
// RATHER THAN CONVENIENCE. `vendor-js/VENDOR.md` records where they came from and the
// commit. Two of them are wire contracts the server validates:
//
//   - `a11y-digest.js` produces the digest the hub VALIDATES and then uses to DROP
//     findings. A partial digest silently deletes its owner's findings — an element the
//     producer failed to list is an element the gate reads as "not present", which is a
//     positive refutation the producer never intended. An EMPTY one is a 400 that rejects
//     the whole multi-page push. Editing this file is how a harness produces one.
//   - `layout-smells.js` returns exactly `plugin.PushLayout`'s snake_case keys. The hub
//     derives the layout FINDINGS from these raw counts through `internal/signals`, which
//     is its declared single source of truth for the thresholds; a harness that computed
//     a finding here would be a second copy of a threshold.
//
// `axe.min.js` is pinned to the same build the hub's native crawl injects, so the rule
// set a pushed run is diffed against is the rule set it was measured with.

//go:embed vendor-js/axe.min.js
var axeJS string

//go:embed vendor-js/a11y-digest.js
var a11yDigestJS string

//go:embed vendor-js/layout-smells.js
var layoutSmellsJS string
