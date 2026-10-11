package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/ui"
)

// 🔴 THE INSTALLABLE SURFACE'S THREE FLAGS (S2 of the mobile plan, decision 1). `-app-name` ARMS
// the feature and has NO default: unset, there is no manifest link, no theme colour, no icon link,
// and `/manifest.webmanifest` answers 404 (the AUTHENTICATED no-route answer — not invisible to an
// anonymous caller; `internal/ui/pwa.go` says why).
// `-app-icon-variant` has no default either and is REQUIRED with a name (O6): a default would give
// two instances that both forgot it the same icon. `-app-short-name` is optional.
//
// 🔴 ALL THREE ARE READ RAW, AND A BLANK LINE IS A REFUSAL — `controlJournalDefault`'s ruling. A
// whitespace-only `CAIRN_UI_APP_NAME` resolved through `envalias` would read as UNSET, so an
// operator who meant to arm the app would get a surface that silently is not installable. And an
// explicit `-app-name ""` is refused for the same reason: it is a line somebody wrote. The shape of
// the values (the variant set, the short-name length) is judged by `ui.App.Validate` — the ONE
// place — and this file only rewords its sentinels in the flag names an operator has to change.
const (
	flagAppName        = "app-name"
	flagAppShortName   = "app-short-name"
	flagAppIconVariant = "app-icon-variant"

	// EnvUIAppName, EnvUIAppShortName and EnvUIAppIconVariant are the three flags' variables.
	EnvUIAppName        = "CAIRN_UI_APP_NAME"
	EnvUIAppShortName   = "CAIRN_UI_APP_SHORT_NAME"
	EnvUIAppIconVariant = "CAIRN_UI_APP_ICON_VARIANT"
)

// appLine is one of the three settings as the operator wrote it.
type appLine struct {
	flag, env string
	// value is the flag's value, whose default is the variable's RAW value.
	value string
	// written is whether the operator wrote this line at all: the flag was given (even as ""), or
	// the variable is present and non-empty. It is what separates a BLANK line from an absent one.
	written bool
}

func (l appLine) spelling() string { return "-" + l.flag + " / $" + l.env }

// resolveApp turns the three lines into a `ui.App`, refusing a blank line and any shape
// `ui.App.Validate` refuses, worded in the flags' names. `warning` is non-empty when settings were
// IGNORED: a short name or a variant with no name starts the surface UNARMED rather than refusing,
// because deleting the name line is how an operator disarms a deployment, and a refusal there would
// crash-loop the pod on that very edit. The warning names what was ignored.
func resolveApp(name, short, variant appLine) (app ui.App, warning string, err error) {
	for _, l := range []appLine{name, short, variant} {
		if l.written && identity.ValueReducesToNothing(l.value) {
			return ui.App{}, "", fmt.Errorf("%s is set to %q, which reduces to nothing, so this surface would read "+
				"it as UNSET and serve no manifest while the deployment said otherwise. Refusing to start; set "+
				"a value or delete the line", l.spelling(), l.value)
		}
	}
	if name.value == "" {
		var ignored []string
		for _, l := range []appLine{short, variant} {
			if l.value != "" {
				ignored = append(ignored, fmt.Sprintf("%s=%q", l.spelling(), l.value))
			}
		}
		if len(ignored) > 0 {
			warning = "WARNING " + strings.Join(ignored, " and ") + " IGNORED: " + name.spelling() +
				" is not set, so the surface is UNARMED (no manifest, not installable). Set the name to arm " +
				"it, or delete the ignored line(s)"
		}
		return ui.App{}, warning, nil
	}
	app = ui.App{Name: name.value, ShortName: short.value, IconVariant: variant.value}
	err = app.Validate()
	switch {
	case err == nil:
		return app, "", nil
	case errors.Is(err, ui.ErrAppNoVariant):
		return ui.App{}, "", fmt.Errorf("%s is set and %s is not. There is NO default variant — two instances "+
			"that both left it out would install with the same icon. Refusing to start; choose one of: %s",
			name.spelling(), variant.spelling(), strings.Join(ui.IconVariants(), ", "))
	case errors.Is(err, ui.ErrAppUnknownVariant):
		return ui.App{}, "", fmt.Errorf("%s is %q, which is not a committed icon variant. Refusing to start; "+
			"choose one of: %s", variant.spelling(), variant.value, strings.Join(ui.IconVariants(), ", "))
	case errors.Is(err, ui.ErrAppShortNameTooLong):
		return ui.App{}, "", fmt.Errorf("%s: %v. Refusing to start rather than letting a launcher cut it",
			short.spelling(), err)
	default:
		return ui.App{}, "", err
	}
}

// appMode is the startup line's half about the installable surface, read off the CONFIG the server
// was handed — the rule the line's other halves follow — so a resolved app that never reached
// `ui.Config` reads as unarmed rather than as what the flags said.
func appMode(app ui.App) string {
	if !app.Armed() {
		return "app unarmed (no -" + flagAppName + ": no manifest, not installable)"
	}
	return fmt.Sprintf("app %q (icon variant %s)", app.Name, app.IconVariant)
}

// 🔴 THE INSTANCE LABEL: `-instance-name` / $CAIRN_UI_INSTANCE_NAME, OPTIONAL, NO DEFAULT. Set, every
// page title reads `<instance> — <page> · cairn` and the header shows it beside the wordmark;
// unset, every title and header is what it was before the flag existed (`internal/ui/title.go`).
//
// ⚠ IT IS DELIBERATELY NOT `-app-name`. That flag ARMS installability — a manifest, a theme
// colour, an icon — and requires a variant; a deployment that only wants its tabs told apart must
// not have to become installable to get it, and one that is installable keeps the manifest's name
// independent of its tab label.
//
// Read RAW, with the `-app-*` lines' blank policy and for their reason: a whitespace-only value
// resolved through `envalias` would read as UNSET, so an operator who wrote the line would get
// unlabelled tabs and no signal. A brand-new name has no deprecated spelling, so it needs no
// `envalias` pair — and `TestTheRawReadVariablesAreNotInTheAliasLedger` pins that it has none.
const (
	flagInstanceName = "instance-name"
	// EnvUIInstanceName is `-instance-name`'s variable.
	EnvUIInstanceName = "CAIRN_UI_INSTANCE_NAME"
)

// resolveInstance judges the instance line: a blank is refused, the shape is `ui.App.Validate`'s
// (the ONE place it is judged), reworded in the flag's name.
func resolveInstance(l appLine) (string, error) {
	if l.written && identity.ValueReducesToNothing(l.value) {
		return "", fmt.Errorf("%s is set to %q, which reduces to nothing, so this surface would read it as "+
			"UNSET and label no page while the deployment said otherwise. Refusing to start; set a value or "+
			"delete the line", l.spelling(), l.value)
	}
	if err := (ui.App{Instance: l.value}).Validate(); err != nil {
		if errors.Is(err, ui.ErrInstanceName) {
			return "", fmt.Errorf("%s: %v. Refusing to start", l.spelling(), err)
		}
		return "", err
	}
	return l.value, nil
}

// instanceMode is the startup line's half about the label, read off the CONFIG the server was
// handed — `appMode`'s rule — so a resolved label that never reached `ui.Config` reads as unset.
func instanceMode(app ui.App) string {
	if app.Instance == "" {
		return "instance unlabelled (no -" + flagInstanceName + ")"
	}
	return fmt.Sprintf("instance %q", app.Instance)
}
