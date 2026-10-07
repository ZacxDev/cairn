package report

import (
	"reflect"
	"testing"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/store"
)

// The arcs-first answer's own guards, over `sessionAcrossStore`'s world: north and south readable,
// east NOT. `s-roam-01`'s newest bullets are north 2000-01-03, south 2000-01-07, east 2000-01-20;
// `s-hide-02` wrote ONLY in east (2000-01-22). Every name and date is synthetic.

func arcsAcrossSnapshot() *arcs.Snapshot {
	reg := func(home, slug, status string, declared []string, members ...string) arcs.Registration {
		var ms []arcs.Member
		for _, m := range members {
			ms = append(ms, arcs.Member{Session: m, Role: "wrote"})
		}
		return arcs.Registration{Schema: arcs.Schema, Home: home, Slug: slug, Status: status,
			ClosingKind: arcs.ClosingCheck, DeclaredScopes: declared, Members: ms,
			ReportedAt: "2000-01-02T00:00:00Z", RegisteredBy: "fixture-registrar", RegisteredAt: "2000-01-06T07:08:09Z"}
	}
	snap := arcs.Snapshot{Latest: map[arcs.Key]arcs.Registration{}}
	for _, r := range []arcs.Registration{
		reg("north-notes", "kettle-arc", arcs.StatusOpen, []string{"east-notes", "north-notes", "south-notes"}, "s-roam-01"),
		reg("south-notes", "hush-arc", arcs.StatusClosed, []string{"south-notes"}, "s-hide-02"),
		reg("east-notes", "spoon-arc", arcs.StatusClosed, []string{"east-notes"}, "s-roam-01"),
	} {
		snap.Latest[r.Key()] = r
	}
	return &snap
}

// TestArcsAcrossListsReadableHomesWithTheirNewestReadableMemberBullet pins the whole answer as a
// literal value. RED with `visible` replaced by `store.Unrestricted()` in the member walk
// (kettle-arc's bullet becomes east's 2000-01-20, hush-arc's 2000-01-22), and RED with the home
// check dropped (spoon-arc appears).
func TestArcsAcrossListsReadableHomesWithTheirNewestReadableMemberBullet(t *testing.T) {
	rep, err := ArcsAcross(sessionAcrossStore(t), northSouth, arcsAcrossSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	want := []ArcAcross{
		{Home: "north-notes", Slug: "kettle-arc", Status: arcs.StatusOpen, Members: 1,
			DeclaredVisible: []string{"north-notes", "south-notes"}, RegisteredAt: "2000-01-06T07:08:09Z",
			ReportedAt: "2000-01-02T00:00:00Z", NewestMemberBullet: "2000-01-07"},
		{Home: "south-notes", Slug: "hush-arc", Status: arcs.StatusClosed, Members: 1,
			DeclaredVisible: []string{"south-notes"}, RegisteredAt: "2000-01-06T07:08:09Z",
			ReportedAt: "2000-01-02T00:00:00Z", NewestMemberBullet: ""},
	}
	if !reflect.DeepEqual(rep.Arcs, want) {
		t.Errorf("arcs\n got %+v\nwant %+v", rep.Arcs, want)
	}
	if !rep.Configured {
		t.Error("a configured journal answered Configured=false")
	}
	// POSITIVE CONTROL: a caller who reads east sees east's newer bullets move both arcs, and the
	// east-homed arc — so the narrowing above is a measurement, not a walk that reads nothing.
	all, err := ArcsAcross(sessionAcrossStore(t), store.Unrestricted(), arcsAcrossSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, a := range all.Arcs {
		got[a.Slug] = a.NewestMemberBullet
	}
	if want := map[string]string{"kettle-arc": "2000-01-20", "hush-arc": "2000-01-22", "spoon-arc": "2000-01-20"}; !reflect.DeepEqual(got, want) {
		t.Errorf("POSITIVE CONTROL: the unrestricted answer is %v, want %v", got, want)
	}
}

// TestArcsAcrossWithNoJournalReadsNothing: the off state is the zero value, Configured false.
func TestArcsAcrossWithNoJournalReadsNothing(t *testing.T) {
	rep, err := ArcsAcross("/nonexistent-store-root-for-the-off-state", northSouth, nil)
	if err != nil {
		t.Fatalf("the off state read the store: %v", err)
	}
	if !reflect.DeepEqual(rep, ArcsAcrossReport{}) {
		t.Errorf("the off state is %+v, want the zero value", rep)
	}
}
