// Package ui is the browser surface: the FIRST place in this repository that
// renders arbitrary user text into HTML, and the ONLY package that links a
// third-party module.
//
// 🔴 THIS PACKAGE IS WHY `go.mod` NO LONGER HAS AN EMPTY `require` BLOCK, AND WHY
// `flake.nix` NO LONGER PASSES `vendorHash = null`. What replaced that guarantee,
// what it is stronger at and what it is weaker at, is stated once — in
// `internal/depspolicy`'s package doc. Read it there.
//
// What belongs here is the consequence for THIS package: it is reachable from
// `cmd/cairn-ui` and from nothing else, and that is a property
// `TestNoPackageTheCLIOrThePodLINKSReachesAThirdPartyModule` MEASURES rather than a
// convention this comment asks for. An import of this package from anywhere in
// `cmd/cairn`'s or `cmd/cairn-server`'s closure puts the HTML library in the pod's
// binary, and that is the refusal.
//
// # 🔴 THE ESCAPING RULE, AND WHY IT IS NARROWER THAN "gomponents ESCAPES"
//
// `gomponents.Text` and the value half of `gomponents.Attr` both run
// `template.HTMLEscapeString`, so text content and a quoted attribute VALUE are
// safe against breakout. Measured against v1.3.0's source, not assumed.
//
// ⚠ BUT THE ESCAPER IS NOT CONTEXT-AWARE THE WAY `html/template` IS, AND THE
// DIFFERENCE IS LIVE RATHER THAN THEORETICAL. `html/template` rewrites a
// `javascript:` URL in an href position to `#ZgotmplZ`; gomponents writes it
// through unchanged, because `template.HTMLEscapeString` has nothing to say about
// a URL scheme — every character in `javascript:alert(1)` survives escaping
// intact. A store entry's `tasks:` ref is `<system>:<id>`, which is exactly the
// shape of a URL scheme followed by an opaque part, so this is a REACHABLE input
// and not a constructed one.
//
// Three rules follow, and all three are checked by a test rather than asked for
// by this comment:
//
//  1. Every URL that reaches an href goes through [safeHref], which allowlists
//     schemes rather than denying them. A ref that does not pass renders as plain
//     TEXT — visible, inert, and not silently dropped.
//  2. `gomponents.Raw` and `gomponents.Rawf` appear nowhere under this package.
//     They are the library's documented "render this unescaped" constructors, and
//     an entry-content path that calls one has no escaping at all.
//     `TestNoRawNodeConstructorAppearsInTheUIPackage` is the ban.
//  3. No element or attribute NAME is built from user text. `gomponents` escapes
//     an attribute's value and writes its NAME verbatim, so a name is a breakout
//     the value escaping cannot see. The same test refuses a non-constant name
//     argument to `El` or `Attr`.
//
// # WHAT THIS PACKAGE DOES NOT DO, IN PHASE A
//
// One page, authenticated by the same `internal/identity` chain the pod uses,
// minus one backend. No cookie session, no sign-in, no share flow. See
// `internal/ui/README.md`.
package ui
