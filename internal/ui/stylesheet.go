package ui

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

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

// 🔴 THE URL THE PAGES LINK CARRIES A DIGEST OF THE BYTES IT SERVES, AND THAT IS THE WHOLE
// MECHANISM — NOT A DECORATION ON THE PATH. A cache entry is keyed on the URL, so a URL that
// does not move when the bytes move is a cache entry nothing can invalidate: a browser holding
// the previous stylesheet keeps rendering against it for as long as its entry lives, with the
// origin serving the new bytes to nobody who already has the old URL. THIS HAPPENED, and it is
// recorded because the failure is silent: after a deploy the origin served the current
// stylesheet (28,109 B on this tree) while a returning browser went on applying a far smaller
// predecessor that had one rule for the sign-in page's classes where the current one has
// dozens — an unstyled page, no error anywhere, nothing red.
//
// 🔴 AND THE HEADER WAS NOT THE LEVER, WHICH IS WHY THE FIX IS THE URL. The edge in front of
// the origin answered `max-age=14400` where this process asked for `max-age=300`. A
// `Cache-Control` is a REQUEST to every cache between here and the browser, and an
// intermediary is free to lengthen it; the stale window on an unversioned path is therefore
// whatever some cache downstream decides, not what this line says. A URL that changes with the
// bytes needs no cache to cooperate — the browser has never seen it, so there is nothing to
// serve stale. (Both figures beyond the 28,109 B are operator measurements against the
// deployed surface and are not reproducible from this tree; the mechanism is what binds.)
//
// ⚠ AND THE DIGEST IS OVER THE BYTES, NOT OVER THE BUILD. Hashing a revision, a timestamp or a
// version string would change the URL on every deploy that changed nothing, which is a cache
// miss for every visitor and a different defect in the same place. `hashStylesheet` reads the
// embedded bytes and nothing else, so two builds of the same stylesheet produce the same URL.

// stylesheetHashWidth is how many hex characters of the digest reach the URL.
//
// TWELVE, and the reason is a collision budget rather than taste. 12 hex characters is 48 bits;
// the URL only has to distinguish successive versions of ONE file, so the relevant question is
// whether two stylesheets this project will ever ship collide in their first 48 bits. At a
// thousand revisions the birthday probability is about 1.8e-9. The full 64 characters would
// work equally well and make every page's `<link>` 52 bytes longer for no property gained.
//
// ⚠ IT IS NOT A SECURITY BOUNDARY AND MUST NOT BE READ AS ONE. Truncated SHA-256 here answers
// "are these the same bytes I already have", asked by a cache. Nothing authenticates a response
// against this digest — a caller who can choose the served bytes has already won, and the bytes
// are build output reaching the binary through `//go:embed`, so no caller can.
const stylesheetHashWidth = 12

// hashStylesheet is the digest function, and it takes the bytes as an ARGUMENT rather than
// reading the package variable, so a test can drive it at more than one point. A derivation
// that could only ever be exercised over the one stylesheet this tree happens to carry could
// not be told apart from a frozen literal.
func hashStylesheet(css string) string {
	sum := sha256.Sum256([]byte(css))
	return hex.EncodeToString(sum[:])[:stylesheetHashWidth]
}

// hashedStylesheetPathFor spells the served path for a given stylesheet body. The two affixes
// are spelled here once: `routes`, `stylesheetLink` and the ledger all reach the result through
// [StylesheetHashedPath], never by re-assembling it.
func hashedStylesheetPathFor(css string) string {
	return "/static/app." + hashStylesheet(css) + ".css"
}
