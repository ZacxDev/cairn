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
// # WHAT THIS PACKAGE DOES, AND WHAT IT STILL DOES NOT
//
// Six pages and one stylesheet behind the same `internal/identity` chain the pod
// uses, minus one backend: the BROWSE trio, the sign-in page with TWO doors, and
// the share flow. Cookie sessions arrived in phase B; the share flow — "who has
// access to this scope", a grant, a revocation, and the notice qualifying all
// three — is phase C and is this package's `sharing.go`, `sharehandlers.go` and
// [SharePage]; phase D is the GitHub sign-in in `oauth.go`, an authorization-code
// flow with PKCE whose verifier lives in this package's own [flights] table and
// whose exchanged token is verified by `identity.SupabaseJWT` like any other.
//
// 🔴 PHASE E WIDENED THE USER-TEXT SURFACE, AND THAT MATTERS MORE HERE THAN THE PAGE
// COUNT DOES. Until it landed, an entry's BODY never reached a browser at all — only
// its ref, title, aliases and task refs did. `GET /entry` now renders the file's `##`
// sections and its journal bullets, which is prose somebody else wrote reaching both
// text content and attribute positions on a page with cookie authentication and no
// content-security policy. The three rules above are unchanged and cover it;
// `TestHostileEntryTextIsEscapedOnEveryBrowsePage` is what measures that they do,
// per page rather than over the root alone.
//
// ⚠ AND THE STRUCTURE IS PARSED BY `internal/store`, NOT BY `internal/report` AND NOT
// BY A MARKDOWN READER WRITTEN HERE. That is a decision with a written record —
// `internal/ui/README.md`'s phase E section — taken because `report` renders TEXT whose
// bytes are pinned against the Python oracle, and because `store.ExtractSections` and
// `store.ParseJournalBullets` already answer the two questions a hand-rolled reader gets
// wrong (a `#` inside a code fence is not a heading; an indented `-` is a continuation).
//
// 🔴 THE SECOND DOOR DID NOT REPLACE THE FIRST, AND THAT IS A REQUIREMENT RATHER
// THAN AN ACCIDENT. The credential form stays: it is the door that verified the
// deployment, the only one that works while the identity provider is unreachable,
// and the only one an automated harness can drive — the provider flow is
// cross-origin to a third party with MFA. `internal/ui/README.md`'s phase D
// section states all three, and a test pins the field's SELECTOR rather than its
// presence.
//
// 🔴 WHAT IT STILL DOES NOT DO, STATED RATHER THAN LEFT TO BE INFERRED:
//
//   - No INVITE. `ControlSharing.Candidates` offers only principals the actor
//     already shares a project with, so a scope cannot be shared with somebody
//     outside every project the actor belongs to. That is a deliberate narrowing
//     against enumeration, and lifting it is an invite flow.
//   - No PROJECT page, so a grant whose object is a PROJECT is visible in an
//     audience and cannot be revoked from here.
//   - No multi-replica story. This is a SINGLE-REPLICA surface and the pages say
//     so — see [ReplicaHonesty], which is pinned whole by a test.
//
// See `internal/ui/README.md`.
package ui
