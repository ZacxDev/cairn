package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// 🔴 THE INSTALLABLE SURFACE (S2 of the mobile plan, `pwa.go`). Every expectation below is a
// LITERAL — the variant names, the kinds, the sizes, the theme colour, the manifest members — never
// read back off the implementation, because a manifest test that derived its expectation from
// `buildManifest` would agree with whatever `buildManifest` produced.

// The two armed apps every test here uses: two names, two variants, so a constant anywhere in the
// manifest path shows up as the two manifests agreeing where they must differ.
var (
	appAlpha = App{Name: "cairn (alpha)", ShortName: "alpha", IconVariant: "amber"}
	appBeta  = App{Name: "cairn (beta)", IconVariant: "teal"}
)

// theSurfaceHex is `--color-surface: oklch(0.21 0.008 75)` converted to sRGB — pinned as a literal,
// so a palette change (or a broken conversion) is a red test naming both values.
const theSurfaceHex = "#1a1814"

// armedServerWith builds `testConfig`'s server with `app` armed.
func armedServerWith(t *testing.T, cfg Config, app App) *Server {
	t.Helper()
	cfg.App = app
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("the armed server did not build: %v", err)
	}
	return srv
}

// fetch drives one GET through the server and returns the recorder.
func fetch(srv *Server, path string, hdr map[string]string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	srv.ServeHTTP(rec, r)
	return rec
}

// TestTheManifestIsBuiltFromTheConfiguredApp pins every manifest member to a literal, for TWO apps in
// one process — so a hardcoded name, a hardcoded variant, or every variant's icons linked at once
// each make the two manifests wrong in a way a single app could not show.
func TestTheManifestIsBuiltFromTheConfiguredApp(t *testing.T) {
	type icon struct{ Src, Sizes, Type, Purpose string }
	type wire struct {
		ID, Name, ShortName, Description, StartURL, Scope, Display, ThemeColor, BackgroundColor string
		ShortNamePresent                                                                        bool
	}
	decode := func(t *testing.T, body []byte) (wire, []icon) {
		t.Helper()
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatalf("the manifest is not a JSON object: %v\n%s", err, body)
		}
		var w wire
		str := func(k string) string {
			var s string
			if v, ok := raw[k]; ok {
				if err := json.Unmarshal(v, &s); err != nil {
					t.Fatalf("manifest member %q is not a string: %s", k, v)
				}
			}
			return s
		}
		w.ID, w.Name, w.ShortName = str("id"), str("name"), str("short_name")
		_, w.ShortNamePresent = raw["short_name"]
		w.Description, w.StartURL, w.Scope = str("description"), str("start_url"), str("scope")
		w.Display, w.ThemeColor, w.BackgroundColor = str("display"), str("theme_color"), str("background_color")
		var icons []icon
		if err := json.Unmarshal(raw["icons"], &icons); err != nil {
			t.Fatalf("manifest icons: %v", err)
		}
		allowed := []string{"id", "name", "short_name", "description", "start_url", "scope", "display",
			"theme_color", "background_color", "icons"}
		for k := range raw {
			if !slices.Contains(allowed, k) {
				t.Errorf("the manifest carries an undeclared member %q — S2 declares exactly %v", k, allowed)
			}
		}
		return w, icons
	}

	for _, tc := range []struct {
		app       App
		wantShort string
		variant   string
	}{
		{appAlpha, "alpha", "amber"},
		{appBeta, "", "teal"},
	} {
		t.Run(tc.app.Name, func(t *testing.T) {
			srv := armedServerWith(t, testConfig(t, refusingAuth{}), tc.app)
			rec := fetch(srv, ManifestPath, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("an ARMED manifest answered %d, want 200: %s", rec.Code, rec.Body.String())
			}
			for k, want := range map[string]string{
				"Content-Type":           "application/manifest+json",
				"Cache-Control":          "no-cache",
				"X-Content-Type-Options": "nosniff",
			} {
				if got := rec.Header().Get(k); got != want {
					t.Errorf("%s: %q, want %q", k, got, want)
				}
			}
			got, gotIcons := decode(t, rec.Body.Bytes())
			want := wire{
				ID: "/", StartURL: "/", Scope: "/", Display: "standalone",
				Name: tc.app.Name, ShortName: tc.wantShort, ShortNamePresent: tc.wantShort != "",
				Description:     "Per-subsystem engineering notes, scoped by authority, read in a browser.",
				ThemeColor:      theSurfaceHex,
				BackgroundColor: theSurfaceHex,
			}
			wantIcons := []icon{
				{iconRowFromBytes(t, tc.variant, "192"), "192x192", "image/png", "any"},
				{iconRowFromBytes(t, tc.variant, "512"), "512x512", "image/png", "any"},
				{iconRowFromBytes(t, tc.variant, "512-maskable"), "512x512", "image/png", "maskable"},
			}
			// The name is compared against the LITERAL in the table, not against `tc.app.Name` read
			// back through the server, so the two arms differing is what proves it is not a constant.
			if got.Name != map[string]string{"amber": "cairn (alpha)", "teal": "cairn (beta)"}[tc.variant] {
				t.Errorf("manifest name is %q; this server was armed with %q. A name that does not follow "+
					"the flag is two installed instances under one title", got.Name, tc.app.Name)
			}
			if !slices.Equal(gotIcons, wantIcons) {
				t.Errorf("manifest icons are\n  %v\nwant exactly the %s variant's three manifest kinds\n  %v\n"+
					"Linking any OTHER variant's icon lets a browser pick another instance's picture",
					gotIcons, tc.variant, wantIcons)
			}
			if got != want {
				t.Errorf("manifest members\n  %+v\nwant\n  %+v", got, want)
			}
		})
	}
}

// TestTheManifestEscapesAHostileName: the manifest is `encoding/json` over a struct, so a name
// carrying a quote, an angle bracket and a newline stays ONE JSON string value.
func TestTheManifestEscapesAHostileName(t *testing.T) {
	hostile := "a\"b<script>\nc"
	srv := armedServerWith(t, testConfig(t, refusingAuth{}), App{Name: hostile, IconVariant: "slate"})
	rec := fetch(srv, ManifestPath, nil)
	var m struct{ Name string }
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("a hostile name broke the manifest's JSON: %v\n%s", err, rec.Body.String())
	}
	if m.Name != hostile {
		t.Errorf("the name round-tripped as %q, want %q", m.Name, hostile)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("<script>")) {
		t.Errorf("the raw `<script>` reached the manifest bytes unescaped: %s", rec.Body.String())
	}
}

// TestTheManifestAnswersAnAnonymousCaller is the regression guard for the row's CLASS: Chromium
// fetches a manifest with NO credentials, so a manifest behind the chain makes the sign-in page —
// the page an install starts from — uninstallable. Driven through an authenticator that refuses
// EVERYTHING, as a plain fetch and as a browser navigation (which the chain would answer 303).
func TestTheManifestAnswersAnAnonymousCaller(t *testing.T) {
	srv := armedServerWith(t, testConfig(t, refusingAuth{}), appAlpha)
	for _, hdr := range []map[string]string{nil, {"Accept": "text/html,application/xhtml+xml"}} {
		rec := fetch(srv, ManifestPath, hdr)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/manifest+json" {
			t.Errorf("an anonymous GET %s (headers %v) answered %d %q, want 200 application/manifest+json — "+
				"a manifest behind the authentication chain cannot be fetched by the browser that installs "+
				"the app", ManifestPath, hdr, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	// And every icon row, for the same reason: the install dialog fetches them anonymously too.
	for _, p := range IconPaths() {
		if rec := fetch(srv, p, nil); rec.Code != http.StatusOK {
			t.Errorf("an anonymous GET %s answered %d, want 200", p, rec.Code)
		}
	}
}

// htmlRows drives every declared GET row through `srv` and returns the HTML bodies by row, plus the
// pages that are not reached by a bare GET row (the sign-in page's own refusal render is one).
func htmlRows(t *testing.T, srv *Server) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, row := range DeclaredRouteLedger() {
		method, path, _ := splitRoute(row)
		if method != http.MethodGet {
			continue
		}
		rec := fetch(srv, path, nil)
		if strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			out[row] = rec.Body.String()
		}
	}
	return out
}

// TestAnUnarmedServerServesNoManifestAndNoPWAHead: with no `App.Name` the feature is INERT — the
// manifest row answers exactly what a path that is not a row answers, and no page carries a head
// element pointing at it.
func TestAnUnarmedServerServesNoManifestAndNoPWAHead(t *testing.T) {
	srv := newTestServer(t, staticAuth{testIdentity()})
	rec := fetch(srv, ManifestPath, nil)
	if rec.Code != http.StatusNotFound || rec.Body.String() != noSuchRoute {
		t.Errorf("an UNARMED manifest answered %d %q, want 404 %q — the dispatcher's own no-route answer, so "+
			"an unarmed deployment is indistinguishable from one built before the feature", rec.Code,
			rec.Body.String(), noSuchRoute)
	}
	pages := htmlRows(t, srv)
	if len(pages) < 10 {
		t.Fatalf("only %d HTML page(s) rendered over the ledger walk, so the absence below is measured over "+
			"almost nothing", len(pages))
	}
	for row, html := range pages {
		for _, marker := range []string{`rel="manifest"`, `name="theme-color"`, `rel="apple-touch-icon"`, `rel="icon"`} {
			if strings.Contains(html, marker) {
				t.Errorf("%s: an UNARMED server rendered %s", row, marker)
			}
		}
	}
	if node := pwaHead(App{}); node != nil {
		t.Errorf("pwaHead of the zero App returned %#v, want NO node at all", node)
	}
}

// TestEveryArmedHTMLPageCarriesThePWAHead is the BEHAVIOURAL half of "every frame calls pwaHead":
// every GET row that answers HTML on an ARMED server carries the four head elements exactly once,
// naming THIS server's variant. The positive control is the page count.
func TestEveryArmedHTMLPageCarriesThePWAHead(t *testing.T) {
	srv := armedServerWith(t, testConfig(t, staticAuth{testIdentity()}), appAlpha)
	pages := htmlRows(t, srv)
	if len(pages) < 10 {
		t.Fatalf("only %d HTML page(s) rendered over the ledger walk; the assertion below needs the frame set", len(pages))
	}
	want := []string{
		`<link rel="manifest" href="/manifest.webmanifest">`,
		`<meta name="theme-color" content="` + theSurfaceHex + `">`,
		`<link rel="icon" type="image/png" sizes="192x192" href="` + iconRowFromBytes(t, "amber", "192") + `">`,
		`<link rel="apple-touch-icon" href="` + iconRowFromBytes(t, "amber", "180-apple") + `">`,
	}
	// The public pages are in the ledger walk as GET rows; the two not reached by a bare GET row
	// (a refused sign-in, and the join page with a token) are rendered here through the server's own
	// path, so the PUBLIC frames are covered by behaviour and not only by the AST walk.
	pages["POST /sign-in (refused)"] = func() string {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, SignInPath, strings.NewReader("token=wrong"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "http://"+r.Host)
		srv.ServeHTTP(rec, r)
		return rec.Body.String()
	}()
	pages["GET /join?token=…"] = fetch(srv, JoinPath+"?token="+fixtureInviteToken, nil).Body.String()
	sawSignIn := false
	for row, html := range pages {
		if strings.HasPrefix(row, "GET /sign-in public") {
			sawSignIn = true
		}
		for _, w := range want {
			if n := strings.Count(html, w); n != 1 {
				t.Errorf("%s: %d × %s, want exactly 1", row, n, w)
			}
		}
	}
	if !sawSignIn {
		t.Error("the walk never rendered the sign-in page, which is the page an install starts from")
	}
	t.Logf("%d armed HTML page(s) each carry the four PWA head elements once", len(pages))
}

// TestEveryFrameCallsPWAHead is the STRUCTURAL half: every `c.HTML5Props` literal in this package
// passes `pwaHead(…)` in its `Head`, so a fourth frame — a public page building its own `c.HTML5`
// for `TestNoPublicPageOffersAuthenticatedNavigation`'s reason — cannot skip it. It reads the AST,
// not a word: a comment or a string spelling `pwaHead` does not satisfy it.
func TestEveryFrameCallsPWAHead(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	frames := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			sel, ok := lit.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "HTML5Props" {
				return true
			}
			frames++
			calls := false
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok || kv.Key.(*ast.Ident).Name != "Head" {
					continue
				}
				ast.Inspect(kv.Value, func(m ast.Node) bool {
					if call, ok := m.(*ast.CallExpr); ok {
						if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "pwaHead" {
							calls = true
						}
					}
					return true
				})
			}
			if !calls {
				t.Errorf("%s: a `c.HTML5Props` frame whose Head does not call pwaHead — an armed deployment "+
					"would serve this page with no manifest link, so it is uninstallable from it",
					fset.Position(lit.Pos()))
			}
			return true
		})
	}
	// The positive control: the walk found the three frames this package has (`shell`, `SignInPage`,
	// `JoinPage`). Fewer means it is reading nothing.
	if frames < 3 {
		t.Fatalf("found %d `c.HTML5Props` frame(s), want at least 3 — the AST walk is not reaching the frames", frames)
	}
}

// TestTheArmedPWAHeadAddsNoScript: S2 adds NO script (decision 4) — `pwa.js` is S4's. The head
// itself renders no `<script`, and no armed page carries a script element other than the ONE
// allowlisted filter tag.
//
// ⚠ The first assertion is an INVARIANT GUARD, labelled: `AllowedScriptSources` already held one
// entry before S2. It is here because S2 is where a second entry would most plausibly be slipped in.
func TestTheArmedPWAHeadAddsNoScript(t *testing.T) {
	if n := len(AllowedScriptSources()); n != 1 {
		t.Errorf("AllowedScriptSources has %d entries, want 1 until S4 adds pwa.js", n)
	}
	for _, app := range []App{appAlpha, appBeta} {
		if html := renderNode(t, pwaHead(app)); strings.Contains(html, "<script") || !strings.Contains(html, `rel="manifest"`) {
			t.Errorf("pwaHead(%+v) rendered %q: it must carry the manifest link and no script element", app, html)
		}
	}
	srv := armedServerWith(t, testConfig(t, staticAuth{testIdentity()}), appAlpha)
	for row, html := range htmlRows(t, srv) {
		rest := strings.ReplaceAll(html, allowedScriptTag(FilterScriptPath), "")
		if strings.Contains(rest, "<script") {
			t.Errorf("%s: an armed page carries a script element beyond the allowlisted filter tag", row)
		}
	}
}

// TestAppValidateRefusesEachShape: every refusal fires with ITS OWN sentinel, so a check that is
// removed is visible as a DIFFERENT sentinel (or none) rather than as some refusal still firing.
func TestAppValidateRefusesEachShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		app  App
		want error
	}{
		{"unarmed", App{}, nil},
		{"armed", App{Name: "n", IconVariant: "amber"}, nil},
		{"every variant in the set", App{Name: "n", IconVariant: "slate"}, nil},
		{"a 12-character short name", App{Name: "n", ShortName: "abcdefghijkl", IconVariant: "teal"}, nil},
		// Characters, not bytes: twelve two-byte runes are twelve characters.
		{"12 multibyte characters", App{Name: "n", ShortName: strings.Repeat("é", 12), IconVariant: "teal"}, nil},
		{"a 13-character short name", App{Name: "n", ShortName: "abcdefghijklm", IconVariant: "teal"}, ErrAppShortNameTooLong},
		{"a name and no variant", App{Name: "n"}, ErrAppNoVariant},
		{"a variant outside the set", App{Name: "n", IconVariant: "chartreuse"}, ErrAppUnknownVariant},
		{"a short name and no name", App{ShortName: "s"}, ErrAppNotArmed},
		{"a variant and no name", App{IconVariant: "amber"}, ErrAppNotArmed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.app.Validate()
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Validate(%+v) = %v, want nil", tc.app, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate(%+v) = %v, want %v", tc.app, err, tc.want)
			}
			// And the server refuses to be built around it, which is what makes the check a gate.
			cfg := testConfig(t, refusingAuth{})
			cfg.App = tc.app
			if _, err := New(cfg); !errors.Is(err, tc.want) {
				t.Errorf("New with %+v = %v, want %v", tc.app, err, tc.want)
			}
		})
	}
	// The unknown-variant refusal names the set, so an operator can fix the line from the message.
	err := App{Name: "n", IconVariant: "chartreuse"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "amber, teal, violet, slate") {
		t.Errorf("the unknown-variant refusal %v does not name the set", err)
	}
}

// TestTheEmbeddedIconSetIsExactlyVariantsTimesKinds: the embedded PNGs are EXACTLY the literal
// variants × kinds — failing on GROW (a stray PNG nobody listed) and on SHRINK. The variant set is
// also pinned literally: dropping a variant breaks every deployment configured with it.
func TestTheEmbeddedIconSetIsExactlyVariantsTimesKinds(t *testing.T) {
	variants := []string{"amber", "teal", "violet", "slate"}
	kinds := []string{"192", "512", "512-maskable", "180-apple"}
	if got := IconVariants(); !slices.Equal(got, variants) {
		t.Errorf("IconVariants() = %v, want %v", got, variants)
	}
	var want []string
	for _, v := range variants {
		for _, k := range kinds {
			want = append(want, v+"-"+k+".png")
		}
	}
	slices.Sort(want)
	entries, err := iconFS.ReadDir("icons")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("the embedded icon set is\n  %v\nwant exactly\n  %v\nRegenerate with `nix run .#build-ui-icons`", got, want)
	}
	if len(IconPaths()) != len(want) {
		t.Errorf("%d icon rows are served, want %d", len(IconPaths()), len(want))
	}
}

// TestEveryIconIsAPNGOfItsDeclaredSize reads each file's IHDR. The sizes are literals here, so a
// `variants.json` edit that changes a `px` without re-rendering, or a render at the wrong size, is red.
func TestEveryIconIsAPNGOfItsDeclaredSize(t *testing.T) {
	px := map[string]int{"192": 192, "512": 512, "512-maskable": 512, "180-apple": 180}
	checked := 0
	for _, f := range iconFiles {
		cfg, err := png.DecodeConfig(bytes.NewReader(f.Bytes))
		if err != nil {
			t.Errorf("%s is not a PNG: %v", f.Name, err)
			continue
		}
		want, ok := px[f.Kind.Kind]
		if !ok {
			t.Errorf("%s: kind %q is not one this test knows", f.Name, f.Kind.Kind)
			continue
		}
		if cfg.Width != want || cfg.Height != want || f.Kind.Px != want {
			t.Errorf("%s is %dx%d (declared %d), want %dx%d", f.Name, cfg.Width, cfg.Height, f.Kind.Px, want, want)
		}
		checked++
	}
	if checked != 16 {
		t.Errorf("checked %d icon(s), want 16", checked)
	}
}

// TestTwoVariantsNeverShareAnIcon: within each kind every variant's bytes differ — two instances
// with byte-identical icons are the confusion O6 exists to prevent.
func TestTwoVariantsNeverShareAnIcon(t *testing.T) {
	pairs := 0
	for i, a := range iconFiles {
		for _, b := range iconFiles[i+1:] {
			if a.Kind.Kind != b.Kind.Kind || a.Variant == b.Variant {
				continue
			}
			pairs++
			if bytes.Equal(a.Bytes, b.Bytes) {
				t.Errorf("%s and %s are byte-identical", a.Name, b.Name)
			}
		}
	}
	if pairs != 4*6 {
		t.Errorf("compared %d pair(s), want 24 (4 kinds × C(4,2))", pairs)
	}
}

// TestTheIconRowsServeTheCommittedBytes: each row answers the file on DISK (not the embedded copy
// read back), as a PNG, `immutable`, `nosniff`.
func TestTheIconRowsServeTheCommittedBytes(t *testing.T) {
	srv := newTestServer(t, refusingAuth{})
	served := 0
	for _, f := range iconFiles {
		disk, err := os.ReadFile(filepath.Join("icons", f.Name))
		if err != nil {
			t.Fatal(err)
		}
		rec := fetch(srv, f.Path, nil)
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), disk) {
			t.Errorf("%s answered %d with %d byte(s), want 200 and the %d committed bytes", f.Path, rec.Code,
				rec.Body.Len(), len(disk))
			continue
		}
		for k, want := range map[string]string{
			"Content-Type":           "image/png",
			"Cache-Control":          "public, max-age=31536000, immutable",
			"X-Content-Type-Options": "nosniff",
		} {
			if got := rec.Header().Get(k); got != want {
				t.Errorf("%s %s: %q, want %q", f.Path, k, got, want)
			}
		}
		served++
	}
	if served != 16 {
		t.Errorf("served %d icon(s), want 16", served)
	}
}

// TestTheThemeColourIsTheStylesheetSurface: the theme colour is DERIVED from the embedded stylesheet
// (plan B6). Pinned as a literal for this palette, and the conversion is driven at two more points a
// constant could not satisfy — white and black — plus the refusal when the token is absent.
func TestTheThemeColourIsTheStylesheetSurface(t *testing.T) {
	if themeColour != theSurfaceHex {
		t.Errorf("themeColour = %q, want %q (oklch(0.21 0.008 75))", themeColour, theSurfaceHex)
	}
	for css, want := range map[string]string{
		"--color-surface: oklch(1 0 0);":        "#ffffff",
		"--color-surface: oklch(0 0 0);":        "#000000",
		"--color-surface: oklch(0.21 0.008 75)": theSurfaceHex,
	} {
		got, err := surfaceColour(css)
		if err != nil || got != want {
			t.Errorf("surfaceColour(%q) = %q, %v; want %q", css, got, err, want)
		}
	}
	if _, err := surfaceColour("--color-raised: oklch(0.25 0.009 75);"); err == nil {
		t.Error("a stylesheet with no --color-surface token produced a colour")
	}
}
