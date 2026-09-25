package ui

import _ "embed"

// stylesheet is the surface's whole appearance, and it is GENERATED rather than written.
//
// 🔴 `app.css` IS BUILD OUTPUT. THE SOURCE IS `tailwind.css` AND THE RULE IS REGENERATE
// AND DIFF. Editing `app.css` by hand is not a smaller version of changing the theme —
// it is a change the next regeneration silently discards, and `flake.nix`'s
// `ui-stylesheet-is-current` check refuses the tree in the meantime. The generator is
// `nix run .#build-ui-stylesheet`, which runs the pinned Tailwind CLI over `tailwind.css`
// and rewrites this file in place.
//
// ⚠ IT IS NOT USER TEXT AND COULD NEVER BE, WHICH IS WHAT MAKES SERVING IT AS A STATIC
// ASSET SAFE. No input reaches it: every byte comes from `tailwind.css` and from the
// Tailwind distribution — TWO sources, and there is no third. That fact is what the earlier
// `const stylesheet` rested on too, and it survives the move to a build step unchanged;
// what changed is only who types the bytes.
//
// ⚠ THIS SENTENCE LISTED A THIRD SOURCE — "the class literals in this package's own `.go`
// files" — AND THAT WAS TRUE OF A DRAFT ONLY. The `@source "./*.go"` line it described is
// deleted; `tailwind.css` is the generator's only input. Corrected here rather than left,
// because a reader who believes a `.go` file feeds the stylesheet will write a raw utility
// into a `Class()` call and ship an element with no rule behind it. See
// `TestEveryRenderedClassHasARuleInTheStylesheet`, which is the check that catches that.
//
// 🔴 THE FILE MUST BE IN `flake.nix`'s `onlyGo` FILTER, AND ITS ABSENCE IS A BUILD FAILURE
// RATHER THAN A SILENT ONE — the good direction, and worth naming because the failure
// reads as "the flake is broken". `//go:embed` on a path the filtered source tree does not
// carry stops compilation dead, so a sandbox build would go red for every derivation. The
// filter names it explicitly, beside `reader_fixtures.json`, for the reason that comment
// gives: a `.css` suffix rule would be an allowlist that says something wider than it
// means.
//
//go:embed app.css
var stylesheet string
