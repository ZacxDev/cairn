package main

import (
	"fmt"
	"strings"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/plugins"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/transcript"
)

// THE PLUGIN REGISTRY AND ITS TOGGLE JOURNAL (slice S5 of `claudedocs/plan-cairn-plugins.md`).
// Flags only, NO default and NO environment spelling, for the worker listener's reason: registering
// a plugin IS the instance-level decision (decision 10, D3), and it has to be a deliberate,
// reviewable line rather than something a copied environment switches on.
const (
	flagPluginRegistry = "plugin-registry"
	flagPluginJournal  = "plugin-journal"
	// flagWorkerPlugin names the ONE registered plugin a token minted by `-issue-worker-token plugin`
	// is bound to.
	flagWorkerPlugin = "worker-plugin"
)

// pluginSettings is what the operator wrote.
type pluginSettings struct {
	registry, journal string
}

// pluginsConfigured decides the registry and the journal.
//
// 🔴 BOTH OR NEITHER, AND ONLY WITH THE WORKER LISTENER. A registry with no journal has toggles
// nobody can set (every plugin OFF forever, while `/plugins` lists it as registered); a journal with
// no registry gates nothing; and either without the worker listener is a plugin API with nowhere to
// run. Each is a half-configuration and refuses, naming what is missing. Neither flag set is the
// designed OFF state: no plugin row is admitted, `/plugins` says so, and no toggle form renders.
//
// 🔴 THE JOURNAL IS REFUSED INSIDE THE STORE ROOT (`arcs.ResolveJournalPathNamed`, the one rule for
// every journal on both binaries) and as any other file this program writes — a toggle line appended
// to the control journal is a refused restart.
func pluginsConfigured(s pluginSettings, storeRoot string, workerOn bool, others map[string]string) (*plugins.Registry, plugins.Journal, bool, error) {
	for name, v := range map[string]string{flagPluginRegistry: s.registry, flagPluginJournal: s.journal} {
		if v != "" && identity.ValueReducesToNothing(v) {
			return nil, plugins.Journal{}, false, fmt.Errorf("-%s is set to a value that reduces to nothing. Refusing to "+
				"start rather than reading it as unset; give it a value or remove the flag", name)
		}
	}
	switch {
	case s.registry == "" && s.journal == "":
		return nil, plugins.Journal{}, false, nil
	case s.registry == "" || s.journal == "":
		return nil, plugins.Journal{}, false, fmt.Errorf("-%s and -%s configure ONE thing together (the registered plugins "+
			"and the toggles that gate them); only one is set, so this is a half-configuration. Refusing to start",
			flagPluginRegistry, flagPluginJournal)
	case !workerOn:
		return nil, plugins.Journal{}, false, fmt.Errorf("-%s is set but there is no worker listener (-%s and its five "+
			"companions), so the plugin API it registers has nowhere to run. Refusing to start", flagPluginRegistry, flagWorkerAddr)
	}
	reg, err := plugins.LoadRegistry(s.registry)
	if err != nil {
		return nil, plugins.Journal{}, false, fmt.Errorf("-%s: %v. Refusing to start", flagPluginRegistry, err)
	}
	path, err := arcs.ResolveJournalPathNamed("plugin toggle journal", storeRoot, s.journal)
	if err != nil {
		return nil, plugins.Journal{}, false, fmt.Errorf("-%s: %v. Refusing to start", flagPluginJournal, err)
	}
	// The registry itself is one more file the journal may not be: a toggle line appended to it makes
	// the next start refuse the registry (audit round 1).
	if resolved, err := resolveMaybeAbsent(s.registry); err == nil && resolved == path {
		return nil, plugins.Journal{}, false, fmt.Errorf("-%s resolves to %s, which is the -%s itself. Give the plugin "+
			"journal its own file. Refusing to start", flagPluginJournal, path, flagPluginRegistry)
	}
	for flagName, other := range others {
		if other == "" {
			continue
		}
		if resolved, err := resolveMaybeAbsent(other); err == nil && resolved == path {
			return nil, plugins.Journal{}, false, fmt.Errorf("-%s resolves to %s, which is this program's %s: a toggle "+
				"record appended there would corrupt it. Give the plugin journal its own file. Refusing to start",
				flagPluginJournal, path, flagName)
		}
	}
	return reg, plugins.Journal{Path: path}, true, nil
}

// knownScope is [plugins.Effective]'s `known`: does the authority hold a scope of that STORE name?
// Asked of the CURRENT model on every decision, so a scope deleted while its toggle says `on` stops
// counting the moment the authority stops holding it.
func knownScope(authority interface{ Model() control.Model }) func(string) bool {
	return func(name string) bool {
		want := store.NormalizeRef(name)
		for _, sc := range authority.Model().Scopes {
			if store.NormalizeRef(sc.DisplayName) == want {
				return true
			}
		}
		return false
	}
}

// newPluginService builds the ONE service the worker listener's plugin API and the browser's toggle
// forms share. The arc journal is read per decision (nil when none is configured, and then no arc
// lists any session); `known` asks the CURRENT model.
func newPluginService(reg *plugins.Registry, journal plugins.Journal, transcripts *transcript.Store, arcJournal string,
	authority interface{ Model() control.Model }) *plugins.Service {
	svc := &plugins.Service{Registry: reg, Journal: journal, Transcripts: transcripts, Known: knownScope(authority)}
	if arcJournal != "" {
		svc.Arcs = arcs.Journal{Path: arcJournal}.Read
	}
	return svc
}

// pluginMode is the startup line's clause about plugins.
func pluginMode(reg *plugins.Registry, journal plugins.Journal) string {
	if reg == nil {
		return "plugins off (no -" + flagPluginRegistry + ")"
	}
	var names []string
	for _, e := range reg.Entries() {
		names = append(names, e.Name)
	}
	listed := "none"
	if len(names) > 0 {
		listed = strings.Join(names, ", ")
	}
	return fmt.Sprintf("%d plugin(s) registered (%s), toggles in %s — every toggle OFF until a scope admin sets it",
		len(names), listed, journal.Path)
}
