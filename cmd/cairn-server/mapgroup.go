package main

// `cairn-server -map-group` / `-unmap-group`: the operator-written GROUP MAP that group
// federation (`CAIRN_OIDC_GROUPS_CLAIM`) resolves a verified token's claimed groups against.
//
// 🔴 COMMANDS, NOT ROUTES, for `-create-scope`'s reason: a route that widens what every
// caller claiming a group can reach would be a public endpoint over the authority itself.
// `api.DeclaredRoutes()` is unchanged by this file. Every rule lives in `control.MapGroup`
// and `control.UnmapGroup`; this file parses, calls one and says what happened.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

type groupMapFlags struct {
	mapGroup   *bool
	unmapGroup *bool
	group      *string
	provider   *string
	object     *string
	verbs      *string
}

// registerGroupMapFlags declares BOTH modes' flags: the two mode switches and the four
// values they share. One function because the values mean the same thing in both — the
// key of one mapping — and two registrations of one name on the default FlagSet panic.
func registerGroupMapFlags() *groupMapFlags {
	return &groupMapFlags{
		mapGroup: flag.Bool("map-group", false,
			"MAP ONE GROUP to ONE scope or project: append ONE group-mapped record to the control "+
				"journal named by $"+EnvControlJournal+", and exit. Every caller whose verified token "+
				"claims the group reaches the object on their next request, when group federation is on, "+
				"at -group-verbs CAPPED AT READ; a PROJECT object also covers every scope added to it later. Requires "+
				"-group, -group-provider, -group-object and -group-verbs"),
		unmapGroup: flag.Bool("unmap-group", false,
			"REMOVE ONE mapping: append ONE group-unmapped record naming -group, -group-provider and "+
				"-group-object, and exit. Callers claiming the group lose that object on their next "+
				"request; nothing else changes"),
		group: flag.String("group", "",
			"the group `name` exactly as the issuer asserts it in the groups claim: [a-z0-9-], at "+
				"most 40 characters"),
		provider: flag.String("group-provider", "",
			"the provider `namespace` the mapping belongs to. It MUST equal the API pod's "+
				"$"+identity.EnvSupabaseProvider+" (or its default), or no token ever matches it — "+
				"nothing can check that the two agree"),
		object: flag.String("group-object", "",
			"the `id` of the scope (scp_…) or project (prj_…) the group reaches; the kind is read "+
				"from the prefix"),
		verbs: flag.String("group-verbs", "",
			"for -map-group: the comma-separated `verbs` RECORDED for the group on the object (read, "+
				"write, admin). A federated principal is CAPPED AT READ when the mapping is resolved, so "+
				"write and admin are recorded and have no effect, and a set without read is refused. "+
				"Required — there is no default; -unmap-group refuses it"),
	}
}

// runGroupMap performs one map or unmap and returns the exit code (`runCreateUser`'s ruling:
// a code, so a test can run it in-process and read the journal back).
func runGroupMap(env map[string]string, unmap bool, f *groupMapFlags, out, errOut io.Writer) int {
	mode := "-map-group"
	if unmap {
		mode = "-unmap-group"
	}
	warn := func(line string) { fmt.Fprintln(errOut, reloadSafe(line)) }

	journal, err := controlJournalPath(env)
	if err != nil {
		warn("subsystem-store-api: " + err.Error())
		return exitConfig
	}
	if journal == "" {
		warn(fmt.Sprintf(
			"subsystem-store-api: %s needs a control journal and $%s is not set. Set it to the path "+
				"the running API pod reads its journal from — a mapping written anywhere else is one no "+
				"running server will ever resolve", mode, EnvControlJournal))
		return exitConfig
	}

	group := strings.TrimSpace(*f.group)
	provider := strings.TrimSpace(*f.provider)
	object := control.ID(strings.TrimSpace(*f.object))
	var missing []string
	for _, m := range []struct{ flag, value string }{
		{"-group", group}, {"-group-provider", provider}, {"-group-object", string(object)},
	} {
		if m.value == "" {
			missing = append(missing, m.flag)
		}
	}
	if !unmap && strings.TrimSpace(*f.verbs) == "" {
		missing = append(missing, "-group-verbs")
	}
	if len(missing) > 0 {
		warn(fmt.Sprintf("subsystem-store-api: %s refused: %s required, and nothing was written. "+
			"None has a default: a mapping nobody spelled out is authority nobody chose",
			mode, strings.Join(missing, ", ")))
		return exitConfig
	}
	if unmap && strings.TrimSpace(*f.verbs) != "" {
		warn("subsystem-store-api: -unmap-group refused: -group-verbs is not accepted. An unmap " +
			"removes the whole mapping it names; to change a mapping's verbs, unmap it and map it again")
		return exitConfig
	}

	kind, ok := control.ObjectKindOfID(object)
	if !ok {
		warn(fmt.Sprintf("subsystem-store-api: %s refused: -group-object %q is neither a scope id "+
			"(%s_…) nor a project id (%s_…), and nothing was written",
			mode, object, control.PrefixScope, control.PrefixProject))
		return exitConfig
	}

	var verbs control.VerbSet
	if !unmap {
		set, err := narrowedVerbs(*f.verbs)
		if err != nil {
			// `narrowedVerbs` names its own flag; the rule is the same one, so the message is
			// relayed with the flag this mode actually takes.
			warn("subsystem-store-api: " + mode + " refused: " +
				strings.ReplaceAll(err.Error(), "-narrow-verbs", "-group-verbs"))
			return exitConfig
		}
		verbs = *set
	}

	ctx := context.Background()
	store, err := control.OpenFileStore(journal)
	if err != nil {
		warn("subsystem-store-api: " + mode + " could not open the control journal: " + err.Error())
		return exitConfig
	}
	req := control.GroupMapRequest{
		Provider: provider, Group: group, ObjectKind: kind, ObjectID: object, Verbs: verbs,
	}
	var written control.GroupMapWritten
	if unmap {
		written, err = control.UnmapGroup(ctx, store, req)
	} else {
		written, err = control.MapGroup(ctx, store, req)
	}
	if err != nil {
		switch {
		case errors.Is(err, control.ErrGroupMapReachesNothing):
			warn("subsystem-store-api: -map-group refused, and nothing was written: -group-verbs " +
				verbs.String() + " has no read in it, and a federated mapping without read reaches " +
				"nothing under the read ceiling. Add read")
		case errors.Is(err, control.ErrNoSuchObject):
			warn(fmt.Sprintf("subsystem-store-api: %s refused: this control journal holds no %s with "+
				"id %q, so nothing was written. Journal %s", mode, kind, object, journal))
		default:
			warn("subsystem-store-api: " + mode + " refused, and nothing was written: " + err.Error())
		}
		return exitConfig
	}

	event := control.EventGroupMapped
	if unmap {
		event = control.EventGroupUnmapped
	}
	fmt.Fprintln(out, reloadSafe(fmt.Sprintf(
		"cairn-control: %s provider=%s group=%s object=%s:%s verbs=%s effective=%s epoch=%d",
		event, written.Key.Provider, written.Key.Group, written.Key.ObjectKind, written.Key.ObjectID,
		written.Verbs, written.Effective, written.Epoch)))
	// 🔴 SAID FOR EVERY RECORD WHOSE VERBS EXCEED WHAT THEY CONFER, on an unmap too: the line
	// above prints both sets, and this names the difference so nobody reads `verbs=` as access.
	if inert := written.Verbs.Minus(written.Effective); !inert.Empty() {
		warn(fmt.Sprintf("subsystem-store-api: WARNING — %s is recorded and has NO EFFECT: a federated "+
			"principal is capped at read when the mapping is resolved, so this mapping confers %s",
			inert, written.Effective))
	}
	if !unmap && kind == control.ObjectProject {
		warn(fmt.Sprintf("subsystem-store-api: group %q now reaches EVERY scope of project %s, "+
			"including every scope added to it later — that is what mapping a project means", group, object))
	}
	warn(fmt.Sprintf("subsystem-store-api: this has an effect only on a pod with $%s set and "+
		"$%s equal to %q; the running server picks it up within %s",
		identity.EnvSupabaseGroupsClaim, identity.EnvSupabaseProvider, provider, refreshInterval))
	return 0
}
