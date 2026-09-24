// Command uiaudit renders the browser surface in a real Chromium and pushes what it
// captured to the hub.
//
// 🔴 IT EXISTS FOR THE FOUR THINGS NO EXISTING GATE IN THIS REPOSITORY CAN SEE, AND FOR
// NOTHING ELSE. Six CI jobs and eleven nix checks already read the renderer's bytes, the
// authz predicate, the route ledger and the served HTTP contract; none of them has ever
// laid out a page. Adding a renderer assertion or an authz assertion here would be a
// second spelling of a gate that already exists, so this program deliberately asserts
// neither. What it measures instead:
//
//  1. THE COOKIE FLAGS AS A BROWSER HONOURS THEM. `internal/identity/session.go` chooses
//     `__Host-`, `Secure`, `HttpOnly` and `SameSite=Lax` and its own comment flags the
//     `Secure`-over-`http://localhost` half as "a claim about browsers and no test here
//     has measured it". A header assertion cannot close that; a jar read after a real
//     navigation can. See `spike/main.go`, which is the measurement.
//  2. AXE-CORE OVER THE RENDERED DOM. There is no accessibility check of any kind in this
//     repository today, and there is no way to add one without a browser: the violations
//     axe reports are functions of computed style and layout, not of the HTML string.
//  3. LAYOUT AT TWO WIDTHS. This surface has never been rendered at any width. The
//     `<meta viewport>` comes from gomponents' HTML5 template and nothing pins it.
//  4. A SIGNAL `/healthz` STRUCTURALLY CANNOT GIVE. `internal/ui/README.md` names a
//     deployment in which the session volume disappears after start: readiness passes,
//     every login fails. A harness that signs in through the form is the only thing that
//     distinguishes those two worlds.
//
// # 🔴 THIS IS A NESTED MODULE, AND THAT IS AN ESCAPE FROM FOUR GATES
//
// The escape, and why it was taken anyway, is declared where the gate it escapes lives:
// `internal/depspolicy`'s package doc, section "WHAT A NESTED MODULE ESCAPES". Read it
// there. It is not restated here for the reason that file gives about six spellings of
// one claim — but it is worth knowing that the declaration exists, because an escape
// nobody wrote down is the shape this repository refuses everywhere.
//
// # WHAT IT DOES NOT MEASURE, STATED RATHER THAN LEFT TO BE INFERRED
//
// Two of the signals it captures are zero BY CONSTRUCTION rather than by passing, and
// reporting them as green would be the "reassuring zero" this repository's own evidence
// rules name:
//
//   - CONSOLE AND NETWORK. The hermetic page has no scripts and no subresources — the
//     stylesheet is inline, and `internal/ui`'s own XSS guard asserts that `"<img"` can
//     never render. So a console count of 0 and a first-party network-failure count of 0
//     are facts about the page's shape, not about its correctness. `control_test.go`
//     serves a page that MUST produce non-zero counts and watches them move; that pair,
//     never the zero alone, is what the README reports.
//   - THE A11Y DIGEST'S REACH. `report.ConcreteKeys` in the hub indexes only `#id` and
//     `[name=…]` anchors, and every button on this surface carries neither. The digest is
//     therefore captured, validated and pushed while anchoring almost nothing. That is a
//     declared residual with a proposed diff, in `README.md`; it is not a bug in this
//     program and this program cannot fix it, because `internal/ui/render.go` is owned by
//     other work in flight.
//
// # GATING
//
// Nothing here blocks, in this change or as a side effect of a later one. The reason is
// mechanical rather than cautious: the first diff against a nonexistent baseline flags
// every axe rule "new" exactly once, so a gate promoted on day one is red for a reason
// that has nothing to do with the tree — the permanently-red gate arrived at from a new
// direction. `README.md` names the ONE predicate that is a promotion candidate after two
// baseline runs, and names what stays advisory indefinitely.
package main
