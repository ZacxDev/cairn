package ui

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/identity"
)

// 🔴 THE INSTALLABLE HALF OF THE MOBILE PLAN (S2): a web app manifest, a closed set of icon
// variants, and the head elements that point a browser at both. There is NO service worker and
// NO script here — `claudedocs/plan-cairn-mobile-pwa.md`, O13 and decision 4. Installability on
// Chromium needs a manifest with a name, 192 and 512 px icons, a `start_url` and a `display`;
// nothing in this file is stored on the device.
//
// 🔴 IT IS INERT UNLESS A DEPLOYMENT ARMS IT. [App] with no `Name` renders no head element and the
// manifest row answers the dispatcher's own 404 — byte-identical to a path that is not a row — so
// an unarmed surface is the surface that existed before this file, and the ROWS are still in the
// ledger, which never depends on configuration.

// App is one deployment's installable identity, from `cmd/cairn-ui`'s `-app-*` flags.
//
// 🔴 THE ZERO VALUE IS "NOT INSTALLABLE", and `Name` is what arms it: a deployment that sets no
// name gets no manifest link, no theme colour, no icon link and a 404 for [ManifestPath]. The
// two instances are told apart by `Name` and `IconVariant`; their origins already make them two
// apps (the manifest `id` is origin-bound), so nothing else is configurable.
//
// ⚠ `Name` IS PUBLIC. The manifest is served before authentication, so whatever is configured
// here is readable by anybody who can reach the sign-in page — put nothing in it you would not
// put there.
type App struct {
	// Name is the manifest `name`, the install dialog's title. "" = unarmed.
	Name string
	// ShortName is the manifest `short_name`, at most [AppShortNameMax] characters; "" omits it.
	ShortName string
	// IconVariant selects one of [IconVariants]; REQUIRED when `Name` is set.
	IconVariant string
}

// Armed answers whether this deployment is installable at all.
func (a App) Armed() bool { return a.Name != "" }

// AppShortNameMax is the longest `short_name` accepted, in characters (runes). Twelve is the
// length a home-screen label shows without truncating on common launchers; a longer one is
// refused rather than silently cut, because a cut name is a name nobody chose.
const AppShortNameMax = 12

// The refusals [App.Validate] gives, one sentinel each so `cmd/cairn-ui` can name the FLAG an
// operator has to change and a test can tell which check fired.
var (
	// ErrAppNoVariant: `Name` is set and `IconVariant` is not. Refused rather than defaulted (O6):
	// a default variant would give two instances that both forgot the flag the same icon, which
	// is the confusion the variant exists to prevent.
	ErrAppNoVariant = errors.New("ui: an app name is set but no icon variant is, and there is no default")
	// ErrAppUnknownVariant: `IconVariant` is not a member of [IconVariants].
	ErrAppUnknownVariant = errors.New("ui: the icon variant is not one of the committed variants")
	// ErrAppShortNameTooLong: `ShortName` is longer than [AppShortNameMax] characters.
	ErrAppShortNameTooLong = errors.New("ui: the short name is longer than the launcher label allows")
	// ErrAppNotArmed: a short name or a variant is set with no `Name`, which arms nothing — the
	// operator configured something that would have no effect, and saying so is cheaper than
	// letting them look for an install prompt that cannot appear.
	ErrAppNotArmed = errors.New("ui: an app short name or icon variant is set without an app name, which arms nothing")
)

// Validate is the ONE place the shape of an [App] is judged. [New] calls it, so a server cannot be
// built around an app the manifest could not describe; `cmd/cairn-ui` calls it to word the refusal
// in flag names. A BLANK value is not judged here: that is a property of the operator's LINE (a
// whitespace flag or variable), and `cmd/cairn-ui` refuses it before a value ever reaches this.
func (a App) Validate() error {
	if !a.Armed() {
		if a.ShortName != "" || a.IconVariant != "" {
			return ErrAppNotArmed
		}
		return nil
	}
	if a.IconVariant == "" {
		return ErrAppNoVariant
	}
	if !slices.Contains(IconVariants(), a.IconVariant) {
		return fmt.Errorf("%w: %q is not one of %s", ErrAppUnknownVariant, a.IconVariant,
			strings.Join(IconVariants(), ", "))
	}
	if n := utf8.RuneCountInString(a.ShortName); n > AppShortNameMax {
		return fmt.Errorf("%w: %q is %d characters, at most %d are accepted", ErrAppShortNameTooLong,
			a.ShortName, n, AppShortNameMax)
	}
	return nil
}

// ---- icons ------------------------------------------------------------------------------------

// 🔴 THE ICONS ARE BUILD OUTPUT OF A COMMITTED TEMPLATE, AND `variants.json` IS THE ONE LIST BOTH
// READERS USE. `flake.nix`'s `uiIcons` reads it with `builtins.fromJSON`, substitutes each
// variant's colours into `any.svg`/`full.svg` and renders every variant × kind with resvg;
// `checks.ui-icons-are-current` re-renders and byte-compares against the committed PNGs. This
// package reads the SAME file to know which PNGs exist — so a variant added to the list and not
// rendered fails to start here, and a PNG rendered for a variant nobody listed fails
// `TestTheEmbeddedIconSetIsExactlyVariantsTimesKinds`.
//
// 🔴 PROVENANCE, NOT A SCAN: `tests/leakscan.py` skips binaries by name, so a PNG is invisible to
// it. What keeps a committed icon free of anything private is that its bytes must EQUAL what the
// derivation renders from the template and the colours — both text, both scanned.
//
// ⚠ THE SAME FLAKE RULE AS `app.css`: each `//go:embed`ed file must be in `flake.nix`'s `onlyGo`
// filter or the sandbox build stops compiling. The filter derives the PNG names from this same
// JSON rather than listing them, so there is no third spelling of the set.
//
//go:embed icons/variants.json
var iconSpecJSON []byte

//go:embed icons/*.png
var iconFS embed.FS

// iconKind is one rendered size/purpose of every variant.
type iconKind struct {
	Kind string `json:"kind"`
	Px   int    `json:"px"`
	// Template is the SVG this kind is rendered from — read by the nix derivation only.
	Template string `json:"template"`
	// ManifestPurpose is the manifest `purpose`; "" means the file is not a manifest icon (the
	// 180 px apple-touch icon, which only `<link rel="apple-touch-icon">` names — iOS reads that
	// link and it OVERRIDES the manifest icons there, plan R2).
	ManifestPurpose string `json:"manifest_purpose"`
}

type iconVariantSpec struct {
	Name string `json:"name"`
}

type iconSpec struct {
	Kinds    []iconKind        `json:"kinds"`
	Variants []iconVariantSpec `json:"variants"`
}

// iconFile is one committed PNG and the row that serves it.
type iconFile struct {
	Variant string
	Kind    iconKind
	// Name is the embedded file name, `<variant>-<kind>.png`.
	Name  string
	Bytes []byte
	// Path is the content-hashed row it is served at.
	Path string
}

// The kinds this package LINKS by name. They must exist in `variants.json`; [loadIcons] refuses
// a spec that lacks one, because a head element pointing at a row that does not exist is a 404
// every page load.
const (
	iconKindFavicon    = "192"
	iconKindAppleTouch = "180-apple"
)

var (
	iconSpecParsed = mustParseIconSpec(iconSpecJSON)
	iconFiles      = mustLoadIcons(iconSpecParsed, iconFS)
)

func mustParseIconSpec(raw []byte) iconSpec {
	var spec iconSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		panic("ui: icons/variants.json does not parse: " + err.Error())
	}
	if len(spec.Variants) == 0 || len(spec.Kinds) == 0 {
		panic("ui: icons/variants.json declares no variants or no kinds, so no deployment could be armed")
	}
	for _, want := range []string{iconKindFavicon, iconKindAppleTouch} {
		if !slices.ContainsFunc(spec.Kinds, func(k iconKind) bool { return k.Kind == want }) {
			panic("ui: icons/variants.json has no kind " + strconv.Quote(want) + ", which pwaHead links")
		}
	}
	return spec
}

// mustLoadIcons reads every variant × kind out of the embedded tree. A missing file is a PANIC at
// package initialisation — the binary does not start and every test in the package fails — rather
// than a row that 404s for the one deployment that picked that variant.
func mustLoadIcons(spec iconSpec, fsys embed.FS) []iconFile {
	var out []iconFile
	for _, v := range spec.Variants {
		for _, k := range spec.Kinds {
			name := v.Name + "-" + k.Kind + ".png"
			b, err := fsys.ReadFile("icons/" + name)
			if err != nil {
				panic("ui: variants.json lists " + name + " and it is not embedded — regenerate with " +
					"`nix run .#build-ui-icons`: " + err.Error())
			}
			out = append(out, iconFile{Variant: v.Name, Kind: k, Name: name, Bytes: b,
				Path: "/static/icon-" + v.Name + "-" + k.Kind + "." + hashAsset(string(b)) + ".png"})
		}
	}
	return out
}

// IconVariants is the closed set `-app-icon-variant` may name, in `variants.json`'s order. A copy.
func IconVariants() []string {
	out := make([]string, 0, len(iconSpecParsed.Variants))
	for _, v := range iconSpecParsed.Variants {
		out = append(out, v.Name)
	}
	return out
}

// IconPaths is every icon row's served path — EVERY variant's, armed or not, because the ledger
// must never depend on configuration. `uiaudit` reads it to classify the rows as not-a-document.
func IconPaths() []string {
	out := make([]string, 0, len(iconFiles))
	for _, f := range iconFiles {
		out = append(out, f.Path)
	}
	return out
}

// iconFor finds one variant's file of one kind. ok=false only for a variant outside the set,
// which [App.Validate] has already refused for any armed server.
func iconFor(variant, kind string) (iconFile, bool) {
	for _, f := range iconFiles {
		if f.Variant == variant && f.Kind.Kind == kind {
			return f, true
		}
	}
	return iconFile{}, false
}

// iconHandler serves one icon's bytes. PUBLIC, content-hashed and `immutable` for the filter
// script's reasons (`script.go`): identical bytes to everybody, no authority consulted, and a
// changed icon is a NEW URL. It serves a Go value and touches no filesystem.
func iconHandler(f iconFile) handler {
	return func(_ *Server, w http.ResponseWriter, _ *http.Request, _ identity.Identity) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", stylesheetCacheImmutable)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(f.Bytes)
	}
}

// 🔴 ONE ROW PER ICON FILE, FOR EVERY VARIANT, ADDED TO THE SAME `routes` MAP THE DISPATCHER READS.
// Computed keys, each still an EXACT match — the stylesheet row's argument (`routes`): the served
// set stays finite and enumerable, and `TestEveryServedPathComesFromTheLedger` probes their
// near-misses. All variants are rows whatever the configuration, so the ledger is the same on
// every deployment; only the selected variant is LINKED.
func init() {
	for _, f := range iconFiles {
		routes[routeKey{http.MethodGet, f.Path}] = route{iconHandler(f), classPublic}
	}
}

// ---- the theme colour -------------------------------------------------------------------------

// 🔴 THE THEME COLOUR IS DERIVED FROM THE BUILT STYLESHEET, NEVER TYPED (plan B6). The browser
// chrome and the Android splash want ONE sRGB colour; the palette is `oklch` in `tailwind.css`, so a
// hand-converted hex here would be a second spelling of `--color-surface` that a palette change
// leaves behind. This reads the token out of the EMBEDDED `app.css` at init and converts it, so the
// two cannot disagree. The palette is dark always (no colour-scheme preference, by operator
// decision), which is why there is one colour and no `media` variant.
var themeColour = mustSurfaceColour(stylesheet)

var surfaceToken = regexp.MustCompile(`--color-surface:\s*oklch\(\s*([0-9.]+)\s+([0-9.]+)\s+([0-9.]+)\s*\)`)

func mustSurfaceColour(css string) string {
	hex, err := surfaceColour(css)
	if err != nil {
		panic("ui: " + err.Error())
	}
	return hex
}

// surfaceColour finds `--color-surface: oklch(L C H)` in a stylesheet and returns it as `#rrggbb`.
func surfaceColour(css string) (string, error) {
	m := surfaceToken.FindStringSubmatch(css)
	if m == nil {
		return "", errors.New("the stylesheet carries no `--color-surface: oklch(L C H)` token, so there is " +
			"no theme colour to derive")
	}
	var v [3]float64
	for i := range v {
		f, err := strconv.ParseFloat(m[i+1], 64)
		if err != nil {
			return "", fmt.Errorf("--color-surface component %q: %w", m[i+1], err)
		}
		v[i] = f
	}
	r, gr, b := oklchToSRGB(v[0], v[1], v[2])
	return fmt.Sprintf("#%02x%02x%02x", r, gr, b), nil
}

// oklchToSRGB is Björn Ottosson's OKLab → linear sRGB matrices plus the sRGB transfer function,
// clamped to the gamut and rounded to 8 bits. `h` is in degrees.
func oklchToSRGB(l, c, hDeg float64) (uint8, uint8, uint8) {
	rad := hDeg * math.Pi / 180
	a, bb := c*math.Cos(rad), c*math.Sin(rad)
	l1 := math.Pow(l+0.3963377774*a+0.2158037573*bb, 3)
	m1 := math.Pow(l-0.1055613458*a-0.0638541728*bb, 3)
	s1 := math.Pow(l-0.0894841775*a-1.2914855480*bb, 3)
	lin := [3]float64{
		4.0767416621*l1 - 3.3077115913*m1 + 0.2309699292*s1,
		-1.2684380046*l1 + 2.6097574011*m1 - 0.3413193965*s1,
		-0.0041960863*l1 - 0.7034186147*m1 + 1.7076147010*s1,
	}
	var out [3]uint8
	for i, x := range lin {
		if x <= 0.0031308 {
			x *= 12.92
		} else {
			x = 1.055*math.Pow(x, 1/2.4) - 0.055
		}
		out[i] = uint8(math.Round(math.Min(1, math.Max(0, x)) * 255))
	}
	return out[0], out[1], out[2]
}

// ---- the manifest -----------------------------------------------------------------------------

// ManifestPath is the manifest's FIXED path: a public row, because Chromium fetches a manifest
// WITHOUT credentials unless its link says `crossorigin="use-credentials"`, and a manifest behind
// the chain would make the surface uninstallable from its own sign-in page.
const ManifestPath = "/manifest.webmanifest"

// manifestDescription is a CONSTANT: a deployment configures its name, never prose about itself.
const manifestDescription = "Per-subsystem engineering notes, scoped by authority, read in a browser."

// webManifest is the manifest's wire shape, marshalled by `encoding/json` and never assembled as a
// string — so a name carrying `"`, `<` or a newline stays one JSON value.
//
// ⚠ NO `shortcuts` AND NO `screenshots` YET: both are S4's, beside the script that S4 adds.
type webManifest struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	ShortName       string         `json:"short_name,omitempty"`
	Description     string         `json:"description"`
	StartURL        string         `json:"start_url"`
	Scope           string         `json:"scope"`
	Display         string         `json:"display"`
	ThemeColor      string         `json:"theme_color"`
	BackgroundColor string         `json:"background_color"`
	Icons           []manifestIcon `json:"icons"`
}

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
}

// buildManifest is the manifest for one armed [App]. Only the SELECTED variant's manifest kinds
// are listed: every variant is a served row, but linking another variant's icon would let a
// browser pick the wrong instance's picture.
func buildManifest(a App) webManifest {
	m := webManifest{
		ID: RootPath, StartURL: RootPath, Scope: RootPath, Display: "standalone",
		Name: a.Name, ShortName: a.ShortName, Description: manifestDescription,
		ThemeColor: themeColour, BackgroundColor: themeColour,
		Icons: []manifestIcon{},
	}
	for _, f := range iconFiles {
		if f.Variant != a.IconVariant || f.Kind.ManifestPurpose == "" {
			continue
		}
		px := strconv.Itoa(f.Kind.Px)
		m.Icons = append(m.Icons, manifestIcon{Src: f.Path, Sizes: px + "x" + px, Type: "image/png",
			Purpose: f.Kind.ManifestPurpose})
	}
	return m
}

// handleManifest serves [ManifestPath]. UNARMED, it answers exactly what a path that is not a row
// answers — the dispatcher's own 404 and body — so an unarmed deployment does not even disclose
// that the feature exists in its build.
func (s *Server) handleManifest(w http.ResponseWriter, _ *http.Request, _ identity.Identity) {
	if !s.app.Armed() {
		writePlain(w, http.StatusNotFound, noSuchRoute)
		return
	}
	body, err := json.Marshal(buildManifest(s.app))
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the manifest could not be rendered")
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// `no-cache`, not `immutable`: the path is fixed, so a renamed deployment must be able to
	// revalidate it. Browsers re-read an installed app's manifest on their own schedule anyway.
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// ---- the head ---------------------------------------------------------------------------------

// pwaHead is the ONE place the installable head elements are spelled, and every frame calls it —
// `shell`, `SignInPage` and `JoinPage` — so a public page is installable from as much as a
// signed-in one. `TestEveryFrameCallsPWAHead` walks the package's `c.HTML5Props` literals, so a
// fourth frame cannot be added without it.
//
// 🔴 IT EMITS NO `<script>`, and that is a decision with a guard rather than an omission: the
// install button's script is S4's, and until then [AllowedScriptSources] holds ONE entry and
// `TestTheArmedPWAHeadAddsNoScript` reads every armed frame for a script tag.
//
// Unarmed, it renders NOTHING — not an empty group with a comment, no node at all.
func pwaHead(a App) g.Node {
	if !a.Armed() {
		return nil
	}
	favicon, _ := iconFor(a.IconVariant, iconKindFavicon)
	apple, _ := iconFor(a.IconVariant, iconKindAppleTouch)
	px := strconv.Itoa(favicon.Kind.Px)
	return g.Group([]g.Node{
		h.Link(h.Rel("manifest"), h.Href(ManifestPath)),
		h.Meta(h.Name("theme-color"), h.Content(themeColour)),
		h.Link(h.Rel("icon"), h.Type("image/png"), g.Attr("sizes", px+"x"+px), h.Href(favicon.Path)),
		h.Link(h.Rel("apple-touch-icon"), h.Href(apple.Path)),
	})
}
