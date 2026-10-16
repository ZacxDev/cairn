package control

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestAProjectMappingCoversAScopeCreatedAfterIt is the reason a group can be mapped to a
// PROJECT: a reserved admin group mapped once must reach every scope the project gains
// later, without anybody editing the map.
//
// The differential is the point — the SAME later scope, resolved with and without the
// project mapping — so "the new scope is reached" cannot be satisfied by something other
// than the mapping (a stray grant, an over-wide resolver).
func TestAProjectMappingCoversAScopeCreatedAfterIt(t *testing.T) {
	const later = ID("scp_beacon_later")
	mapping := Event{Kind: EventGroupMapped, At: at(40), Provider: fedProvider, Group: "admins",
		ObjectKind: ObjectProject, ObjectID: pBeacon, Verbs: AllSet}
	createdLater := Event{Kind: EventScopeCreated, At: at(41), ScopeID: later, DisplayName: "beacon-later", ProjectID: pBeacon}

	with, err := Replay(append(worldEvents(), mapping, createdLater))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	_, a, err := ResolveFederated(with, fedProvider, "someone", []string{"admins"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := verbsOrEmpty(a.VerbsOn(later)); got != "read,write,admin" {
		t.Fatalf("a scope created AFTER the project mapping reads %q, want read,write,admin — a mapping expanded at write time freezes the project's scope set", got)
	}
	if got := len(a.ScopeIDs(VerbRead)); got != 3 {
		t.Fatalf("the admins group reaches %d scopes, want beacon's 3", got)
	}

	without, err := Replay(append(worldEvents(), createdLater))
	if err != nil {
		t.Fatalf("replay without the mapping: %v", err)
	}
	_, b, err := ResolveFederated(without, fedProvider, "someone", []string{"admins"})
	if err != nil {
		t.Fatalf("resolve without the mapping: %v", err)
	}
	if got := verbsOrEmpty(b.VerbsOn(later)); got != "" {
		t.Fatalf("WITHOUT the mapping the later scope reads %q — something other than the mapping reaches it, so the arm above proves nothing", got)
	}
}

// TestAMalformedClaimIsRefusedWhole pins that one bad member refuses the WHOLE claim — a
// partial read of a claim the issuer got wrong is invisible, a refusal is a 401.
func TestAMalformedClaimIsRefusedWhole(t *testing.T) {
	m, err := Replay(federatedWorldEvents())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	tooMany := make([]string, 0, MaxClaimedGroups+1)
	for i := 0; i <= MaxClaimedGroups; i++ {
		tooMany = append(tooMany, fmt.Sprintf("g%d", i))
	}
	for name, claim := range map[string][]string{
		"an upper-case name beside a valid one": {"readers", "Readers"},
		"a name with a slash":                   {"readers", "team/ops"},
		"an empty name":                         {"readers", ""},
		"a name over the length bound":          {"readers", strings.Repeat("a", MaxGroupNameLen+1)},
		"more distinct groups than the bound":   tooMany,
	} {
		_, a, err := ResolveFederated(m, fedProvider, "someone", claim)
		if !errors.Is(err, ErrGroupsClaim) {
			t.Errorf("%s: err = %v, want ErrGroupsClaim", name, err)
		}
		if len(a.ScopeIDs(VerbRead)) != 0 {
			t.Errorf("%s: a refused claim still reached %v — a partial read", name, a.ScopeIDs(VerbRead))
		}
	}

	// POSITIVE CONTROLS: the bounds are bounds, not refusals of everything. A name AT the
	// length bound, and MaxClaimedGroups+6 entries that deduplicate to under the bound,
	// both resolve — and the valid `readers` still reaches atlas-notes.
	atBound := strings.Repeat("a", MaxGroupNameLen)
	repeated := []string{"readers", atBound}
	for i := 0; i < MaxClaimedGroups+5; i++ {
		repeated = append(repeated, "readers")
	}
	_, a, err := ResolveFederated(m, fedProvider, "someone", repeated)
	if err != nil {
		t.Fatalf("a claim that deduplicates under the bound was refused: %v", err)
	}
	if got := verbsOrEmpty(a.VerbsOn(sAtlasNotes)); got != "read" {
		t.Fatalf("the valid claim reads %q on atlas-notes, want read", got)
	}
}

// TestAFederatedPrincipalHasAStableIDAndNoJournalRow pins the principal's shape: one caller
// is one id across requests, and the id names no entity the journal holds.
func TestAFederatedPrincipalHasAStableIDAndNoJournalRow(t *testing.T) {
	m := world(t)
	p1, _, err := ResolveFederated(m, fedProvider, "subject-a", []string{})
	if err != nil {
		t.Fatal(err)
	}
	p2, _, _ := ResolveFederated(m, fedProvider, "subject-a", []string{"ghost"})
	p3, _, _ := ResolveFederated(m, fedProvider, "subject-b", []string{})
	p4, _, _ := ResolveFederated(m, fedOther, "subject-a", []string{})
	if p1.ID != p2.ID {
		t.Errorf("one (provider, subject) resolved to two ids: %s and %s", p1.ID, p2.ID)
	}
	if p1.ID == p3.ID || p1.ID == p4.ID {
		t.Errorf("two different callers share an id: %s / %s / %s", p1.ID, p3.ID, p4.ID)
	}
	if !MustPrefix(p1.ID, PrefixFederated) || p1.Kind != KindFederated {
		t.Errorf("principal %+v is not a federated principal", p1)
	}
	if _, held := m.PrincipalFor(p1.Kind, p1.ID); held {
		t.Errorf("the model holds an entity for a federated principal — it was written somewhere")
	}
	if _, _, err := ResolveFederated(m, "", "subject-a", nil); !errors.Is(err, ErrFederatedSubject) {
		t.Errorf("an empty provider resolved: %v", err)
	}
	if _, _, err := ResolveFederated(m, fedProvider, "", nil); !errors.Is(err, ErrFederatedSubject) {
		t.Errorf("an empty subject resolved: %v", err)
	}
}

// TestAFederatedKindIsNeverAGrantOrCredentialSubject pins `KindFederated`'s comment: a
// record naming one as a subject is a replay refusal, so the group map stays the ONLY
// source of a federated principal's authority.
func TestAFederatedKindIsNeverAGrantOrCredentialSubject(t *testing.T) {
	for _, e := range []Event{
		{Kind: EventGranted, At: at(40), GrantID: "grt_fed", SubjectKind: KindFederated, SubjectID: "fed_x",
			ObjectKind: ObjectScope, ObjectID: sAtlasNotes, Verbs: NewVerbSet(VerbRead)},
		{Kind: EventCredentialIssued, At: at(40), CredentialID: "crd_fed", SubjectKind: KindFederated, SubjectID: "fed_x",
			TokenHash: strings.Repeat("a", HashHexLen)},
	} {
		if _, err := Replay(append(worldEvents(), e)); err == nil || !strings.Contains(err.Error(), "unknown subject_kind") {
			t.Errorf("%s naming a federated subject replayed: %v", e.Kind, err)
		}
	}
}

// TestTheGroupMapJournalRefusesEachMalformedOrConflictingRecord walks every refusal the two
// kinds carry, each with a record no EARLIER check rejects, asserting a fragment of THAT
// refusal's own text so a neighbouring one cannot satisfy it.
func TestTheGroupMapJournalRefusesEachMalformedOrConflictingRecord(t *testing.T) {
	ok := Event{Kind: EventGroupMapped, At: at(40), Provider: fedProvider, Group: "readers",
		ObjectKind: ObjectScope, ObjectID: sAtlasNotes, Verbs: NewVerbSet(VerbRead)}
	with := func(f func(*Event)) Event { e := ok; f(&e); return e }
	unmap := with(func(e *Event) { e.Kind = EventGroupUnmapped; e.Verbs = 0 })

	// Positive control: the base record replays, and its unmap after it replays too.
	if _, err := Replay(append(worldEvents(), ok, unmap)); err != nil {
		t.Fatalf("the base map+unmap did not replay, so every refusal below is unattributable: %v", err)
	}

	for name, c := range map[string]struct {
		tail     []Event
		fragment string
	}{
		"no provider":           {[]Event{with(func(e *Event) { e.Provider = "" })}, "provider is required"},
		"no group":              {[]Event{with(func(e *Event) { e.Group = "" })}, "group is required"},
		"a bad group name":      {[]Event{with(func(e *Event) { e.Group = "Ops Team" })}, "not a usable group name"},
		"no object":             {[]Event{with(func(e *Event) { e.ObjectID = "" })}, "object_id is required"},
		"an unknown kind":       {[]Event{with(func(e *Event) { e.ObjectKind = "user" })}, "unknown object_kind"},
		"an object that is not": {[]Event{with(func(e *Event) { e.ObjectID = "scp_nowhere" })}, "object scope scp_nowhere does not exist"},
		"empty verbs":           {[]Event{with(func(e *Event) { e.Verbs = 0 })}, "mapping conferring nothing"},
		"a duplicate live map":  {[]Event{ok, with(func(e *Event) { e.At = at(41); e.Verbs = AllSet })}, "already mapped"},
		"an unmap of nothing":   {[]Event{unmap}, "has no live mapping"},
		"an unmap with verbs":   {[]Event{ok, with(func(e *Event) { e.Kind = EventGroupUnmapped })}, "verbs must be absent"},
	} {
		_, err := Replay(append(worldEvents(), c.tail...))
		if err == nil || !strings.Contains(err.Error(), c.fragment) {
			t.Errorf("%s: replay err = %v, want a refusal containing %q", name, err, c.fragment)
		}
	}

	// And re-mapping AFTER an unmap is allowed — the unmap really removed the key.
	if _, err := Replay(append(worldEvents(), ok, unmap, with(func(e *Event) { e.At = at(42) }))); err != nil {
		t.Fatalf("map, unmap, map again was refused: %v", err)
	}
}

// TestMapGroupAndUnmapGroupRefuseBeforeWritingAnything walks the writer's early refusals by
// sentinel, and asserts the journal is byte-identical after each.
func TestMapGroupAndUnmapGroupRefuseBeforeWritingAnything(t *testing.T) {
	store, path, owner := aProvisionedOwner(t)
	ctx := context.Background()
	scope := owner.Scopes[0].ID
	good := GroupMapRequest{Provider: fedProvider, Group: "readers", ObjectKind: ObjectScope, ObjectID: scope,
		Verbs: NewVerbSet(VerbRead), At: provisionClock}

	if _, err := UnmapGroup(ctx, store, good); !errors.Is(err, ErrGroupNotMapped) {
		t.Fatalf("unmapping a key that was never mapped: %v", err)
	}
	written, err := MapGroup(ctx, store, good)
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	if written.Epoch != owner.Epoch+1 || written.Key.ObjectID != scope {
		t.Fatalf("the map wrote %+v, want one event over epoch %d", written, owner.Epoch)
	}

	for name, c := range map[string]struct {
		req      GroupMapRequest
		unmap    bool
		sentinel error
	}{
		"a duplicate map":        {good, false, ErrGroupAlreadyMapped},
		"an unknown object":      {func() GroupMapRequest { r := good; r.ObjectID = "scp_nowhere"; return r }(), false, ErrNoSuchObject},
		"a project as a scope":   {func() GroupMapRequest { r := good; r.ObjectID = owner.Project; return r }(), false, ErrNoSuchObject},
		"an unknown object kind": {func() GroupMapRequest { r := good; r.ObjectKind = "user"; return r }(), false, ErrNoSuchObject},
		"no verbs":               {func() GroupMapRequest { r := good; r.Group = "other"; r.Verbs = 0; return r }(), false, ErrGroupMapVerbs},
		"a bad group name":       {func() GroupMapRequest { r := good; r.Group = "UPPER"; return r }(), false, ErrGroupName},
		"no provider":            {func() GroupMapRequest { r := good; r.Provider = ""; return r }(), false, ErrFederatedSubject},
		"an unmap of another":    {func() GroupMapRequest { r := good; r.Group = "other"; r.Verbs = 0; return r }(), true, ErrGroupNotMapped},
	} {
		before := journalBytes(t, path)
		var err error
		if c.unmap {
			_, err = UnmapGroup(ctx, store, c.req)
		} else {
			_, err = MapGroup(ctx, store, c.req)
		}
		if !errors.Is(err, c.sentinel) {
			t.Errorf("%s: err = %v, want %v", name, err, c.sentinel)
		}
		if !bytes.Equal(before, journalBytes(t, path)) {
			t.Errorf("%s: a refused write changed the journal", name)
		}
	}

	unmapped, err := UnmapGroup(ctx, store, GroupMapRequest{Provider: fedProvider, Group: "readers",
		ObjectKind: ObjectScope, ObjectID: scope, At: provisionClock})
	if err != nil {
		t.Fatalf("unmapping the live mapping: %v", err)
	}
	if unmapped.Verbs != NewVerbSet(VerbRead) {
		t.Errorf("the unmap reports verbs %s, want the removed mapping's read", unmapped.Verbs)
	}
	m, err := store.Model(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.GroupMaps) != 0 {
		t.Errorf("after map+unmap the model holds %d mapping(s)", len(m.GroupMaps))
	}
}

// TestTheGroupMapRoundTripsThroughTheDurableFormat reads a map back through a SECOND store,
// so nothing is answered from this process's memory, and through `clone` (which `Append`'s
// scratch validation uses — a shallow copy there would alias the live map).
func TestTheGroupMapRoundTripsThroughTheDurableFormat(t *testing.T) {
	store, path, owner := aProvisionedOwner(t)
	ctx := context.Background()
	if _, err := MapGroup(ctx, store, GroupMapRequest{Provider: fedProvider, Group: "admins",
		ObjectKind: ObjectProject, ObjectID: owner.Project, Verbs: NewVerbSet(VerbRead, VerbWrite), At: provisionClock}); err != nil {
		t.Fatal(err)
	}
	reread, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := reread.Model(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := m.GroupMapsOf()
	if len(got) != 1 || got[0].Group != "admins" || got[0].ObjectKind != ObjectProject ||
		got[0].ObjectID != owner.Project || got[0].Verbs != NewVerbSet(VerbRead, VerbWrite) {
		t.Fatalf("read back %+v", got)
	}
	c := m.clone()
	delete(c.GroupMaps, got[0].GroupMapKey)
	if len(m.GroupMaps) != 1 {
		t.Fatal("deleting from a clone's group map changed the original — clone shares the map")
	}
}

// TestNarrowCarriesTheFederationReport pins that a narrowed federated authority still says
// how it was computed — the field a later verb ceiling and self-view both read.
func TestNarrowCarriesTheFederationReport(t *testing.T) {
	m, err := Replay(federatedWorldEvents())
	if err != nil {
		t.Fatal(err)
	}
	_, a, err := ResolveFederated(m, fedProvider, "someone", []string{"readers", "writers", "ghost"})
	if err != nil {
		t.Fatal(err)
	}
	read := NewVerbSet(VerbRead)
	n := Narrow(a, nil, &read)
	report, ok := n.Federation()
	if !ok || report.Status != FederationMapped || strings.Join(report.Unmapped, ",") != "ghost" {
		t.Fatalf("narrowing lost or changed the federation report: %+v %v", report, ok)
	}
	if got := verbsOrEmpty(n.VerbsOn(sAtlasNotes)); got != "read" {
		t.Fatalf("narrowed to read, atlas-notes reads %q", got)
	}
	if _, ok := Resolve(m, user(uAlice)).Federation(); ok {
		t.Fatal("a user's authority claims to be federated")
	}
}
