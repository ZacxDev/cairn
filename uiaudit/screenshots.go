package main

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/ui"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// 🔴 THE INSTALL SCREENSHOTS' GENERATOR (S4 of the mobile plan, decision 16, O7), AND THE ONLY ONE.
// `uiaudit -screenshots <dir>` boots the SAME synthetic world the walk measures (`BootWorld`, over
// `tests/reader_fixtures.py`'s own builder), signs in through the real form, and captures every entry
// of `ui.ScreenshotSpecs()` — the one list `internal/ui` embeds and `flake.nix` reads — at its exact
// size. `flake.nix`'s `uiScreenshots` runs it inside the nix sandbox with the pinned chromium and a
// pinned font set, and `checks.ui-screenshots-are-current` byte-compares the result with the committed
// files. Run outside the sandbox it produces pictures under THIS host's fonts and chromium, which are
// not the committed ones — `nix run .#build-ui-screenshots` is the way to regenerate.
//
// What is held still, because each would otherwise move the bytes between two runs:
//   - device scale 1, and the size is the spec's, never the page's (no `captureBeyondViewport`);
//   - `prefers-reduced-motion: reduce`, which `tailwind.css` honours by stopping the entrance
//     animations — a capture mid-animation is a different picture every time;
//   - `document.fonts.ready` awaited before the capture;
//   - scrollbars HIDDEN: a 390 px capture's overlay scrollbar is mid-fade at a time nobody controls —
//     MEASURED, two back-to-back runs differed in 3348 pixels, every one in the 4 px column at x 386–389;
//   - the world: its fixture content is fixed text, and its one clock-relative arc renders "1h ago"
//     for an hour after boot, which a capture seconds later always reads.
//
// 🔴 THE WORLD IS *UNARMED* (no `-app-name`), A DEPARTURE FROM THE PLAN'S "a FIXED synthetic
// -app-name", AND IT IS MEASURED RATHER THAN PREFERRED. Armed, chromium fired `beforeinstallprompt` on
// its own (no engagement bypass) and `pwa.js` revealed the header's Install button in the captures — a
// control whose appearance is a race between the browser's installability check and the capture, which
// is exactly the kind of input a byte-compared picture cannot carry. Unarmed, the only pixels that
// differ from an armed page are that button's (hidden until the event), and no name of any deployment
// can reach a picture at all, which is the plan's reason for fixing the name in the first place.
//
// ⚠ THE `narrow` CAPTURES ARE TOUCH CAPTURES: `(pointer: coarse)` matches, so they show what a phone
// renders (S1's touch header and targets), not a desktop squeezed to 390 px.
func runScreenshots(repoRoot, uiBinary, workDir, outDir string, port int, budget time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	world, err := BootWorld(ctx, abs, uiBinary, filepath.Join(workDir, "world"), port)
	if err != nil {
		return err
	}
	defer world.Stop()
	browser, err := NewBrowser(ctx, world.BaseURL, budget)
	if err != nil {
		return err
	}
	defer browser.Close()
	version, err := browser.Version()
	if err != nil {
		return err
	}
	fmt.Printf("uiaudit: screenshots with %s\n", version)
	if _, err := browser.SignIn(fixtureToken); err != nil {
		return fmt.Errorf("%w\n--- cairn-ui log ---\n%s", err, world.Log())
	}
	specs := ui.ScreenshotSpecs()
	if len(specs) == 0 {
		return fmt.Errorf("ui.ScreenshotSpecs() is EMPTY, so there is nothing to capture")
	}
	for _, s := range specs {
		shot, err := browser.CaptureInstallScreenshot(s)
		if err != nil {
			return err
		}
		out := filepath.Join(outDir, s.Name+".png")
		if err := os.WriteFile(out, shot, 0o644); err != nil {
			return err
		}
		fmt.Printf("uiaudit: screenshot %s (%s %dx%d of %s): %d byte(s)\n", out, s.FormFactor, s.Width, s.Height,
			s.Page, len(shot))
	}
	return nil
}

// CaptureInstallScreenshot captures one spec at exactly its size and REFUSES a capture that is not
// the page asked for (a redirect, an error page) or not the size declared.
func (b *Browser) CaptureInstallScreenshot(s ui.ScreenshotSpec) ([]byte, error) {
	touch := s.FormFactor == "narrow"
	var landed string
	var shot []byte
	var fontsReady bool
	if err := chromedp.Run(b.ctx,
		emulation.SetDeviceMetricsOverride(int64(s.Width), int64(s.Height), 1, touch),
		touchEmulation(Viewport{Touch: touch}),
		emulation.SetScrollbarsHidden(true),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{
			{Name: "prefers-reduced-motion", Value: "reduce"},
		}),
		chromedp.Navigate(b.base+s.Page),
		chromedp.WaitReady("body"),
		chromedp.Evaluate(`document.fonts.ready.then(() => true)`, &fontsReady, awaitPromise),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Location(&landed),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			shot, err = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatPng).Do(ctx)
			return err
		}),
	); err != nil {
		return nil, fmt.Errorf("capturing %s for %s: %w", s.Page, s.Name, err)
	}
	wantPath, _, _ := strings.Cut(s.Page, "?")
	if lu, err := url.Parse(landed); err != nil || lu.Path != wantPath {
		return nil, fmt.Errorf("screenshot %s asked for %s and landed on %s: a picture of the wrong page", s.Name,
			s.Page, landed)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(shot))
	if err != nil {
		return nil, fmt.Errorf("screenshot %s is not a PNG: %w", s.Name, err)
	}
	if cfg.Width != s.Width || cfg.Height != s.Height {
		return nil, fmt.Errorf("screenshot %s is %dx%d, its spec says %dx%d", s.Name, cfg.Width, cfg.Height,
			s.Width, s.Height)
	}
	return shot, nil
}
