package ui

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/presence"
)

// 🔴 PRESENCE BADGES (S4), THROUGH THE REAL DISPATCHER OVER `sessionStore`'s WORLD. The roamer reads
// north and south and is the presence OWNER; `s-roam-01` wrote in both and is the one member of the
// open `kettle-arc` homed in north, so ONE session reaches every surface that renders a badge. Every
// host label, target, hotkey and time below is SYNTHETIC, and the badge's fields are pairwise distinct
// (and distinct from every constant the renderer spells) so a field rendered in another field's place
// cannot pass.

const (
	badgeSession = "s-roam-01"
	badgeHotkey  = "Alt+n"
	badgeLabel   = "notes-label"
)

// badgeSurfaces is every page S4 decorates, keyed by a name for the failure line. The session page
// and the two session-row lists show the pane badge; `/arcs` and the arc page's summary show "live pane".
func badgeSurfaces() map[string]string {
	return map[string]string{
		"session page":            sessionURL(badgeSession),
		"scope page sessions tab": scopeTabHref(Scope{ID: sessNorth, Name: "north-notes"}, TabSessions),
		"arc page (scopes tab)":   arcHref(sessNorth, "kettle-arc"),
		"arc page sessions tab":   arcTabHref(sessNorth, "kettle-arc", TabSessions),
		"arcs-first page":         ArcsPath,
	}
}

// badgeClock is a presence store whose clock the test moves: `at` is read on every Replace and For.
type badgeClock struct{ at time.Time }

func (c *badgeClock) now() time.Time { return c.at }

// pushRows decodes a real push body, so rows carry the parsed `last_activity` the target pick reads —
// exactly as the agent route installs them.
func pushRows(t *testing.T, host string, rows ...string) []presence.Row {
	t.Helper()
	p, err := presence.DecodePush(strings.NewReader(`{"schema":1,"host":"` + host + `","rows":[` + strings.Join(rows, ",") + `]}`))
	if err != nil {
		t.Fatalf("the fixture push does not decode: %v", err)
	}
	return p.Rows
}

func wireRow(session, runtime, target, hotkey, lastActivity string) string {
	return `{"session":"` + session + `","runtime":"` + runtime + `","target":"` + target + `","label":"` + badgeLabel +
		`","hotkey":"` + hotkey + `","last_activity":"` + lastActivity + `"}`
}

func ownerOf(id identity.Identity) presence.Owner { return presence.OwnerOf(id.Principal) }

// badgeServer is `sessionServer` with a presence service handed in, exactly as `cmd/cairn-ui` hands it.
func badgeServer(t *testing.T, src Source, viewer identity.Identity, svc *presence.Service) *Server {
	t.Helper()
	cfg := testConfig(t, staticAuth{viewer})
	cfg.Source = src
	cfg.Now = func() time.Time { return sessNow }
	cfg.Presence = svc
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func bodyOf(t *testing.T, srv *Server, path string) string {
	t.Helper()
	rec := getAs(t, srv, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s answered %d: %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

var paneText = regexp.MustCompile(`data-presence="pane"[^>]*>([^<]*)<`)

// TestPresenceIsInvisibleToEveryoneButItsOwner — decision 5 as a RELATIONSHIP over one store, on every
// surface: for a non-owner, a narrowed owner, expired presence and absent presence, each page's bytes
// EQUAL the bytes of the same viewer's page with presence OFF. The owner's page is the positive
// control — it differs, and carries the badge — so each equality measures the predicate rather than a
// surface that never renders presence. RED with the predicate's answer ignored
// (`ui-presence-badge-ignores-the-predicate`, `ui-presence-live-pane-ignores-the-predicate`) and
// with the viewer rebuilt from its principal (`ui-presence-viewer-rebuilt-from-the-principal`).
func TestPresenceIsInvisibleToEveryoneButItsOwner(t *testing.T) {
	roamer, easterner := sessionWorld(t)
	src := StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}
	narrowed := roamer
	narrowed.Auth = control.Narrow(roamer.Auth, []control.ID{sessNorth, sessSouth})
	if !narrowed.Auth.Narrowed() || !narrowed.Auth.Allows(sessNorth, control.VerbRead) || !narrowed.Auth.Allows(sessSouth, control.VerbRead) {
		t.Fatal("INSTRUMENT: the narrowed roamer is not a narrowing to EVERY scope the roamer reads")
	}
	if ownerOf(roamer) == ownerOf(easterner) {
		t.Fatal("INSTRUMENT: the two principals share an owner key")
	}

	row := wireRow(badgeSession, "opencode", "notes:3", badgeHotkey, "2000-01-10T11:00:00Z")
	// service builds a store holding `rows` for `owner` on host-a, pushed `age` before the UI clock and
	// read at the UI clock.
	service := func(owner presence.Owner, age time.Duration, rows ...string) *presence.Service {
		clock := &badgeClock{at: sessNow.Add(-age)}
		st := &presence.Store{Now: clock.now}
		st.Replace(owner, "host-a", pushRows(t, "host-a", rows...))
		clock.at = sessNow
		return &presence.Service{Store: st, Queue: &presence.Queue{}}
	}

	for name, path := range badgeSurfaces() {
		t.Run(name, func(t *testing.T) {
			off := bodyOf(t, badgeServer(t, src, roamer, nil), path)
			// The baseline is ABSOLUTE as well as relative: a renderer that drew a badge from a
			// lookup's zero value would draw it on the off page too, and every equality below would
			// then hold between two wrong pages.
			if strings.Contains(off, `data-presence=`) {
				t.Fatalf("the page with presence OFF carries a presence badge")
			}
			// The off state has two spellings — no service, and a service whose store is empty — and
			// they are one page.
			if empty := bodyOf(t, badgeServer(t, src, roamer, &presence.Service{Store: &presence.Store{}, Queue: &presence.Queue{}}), path); empty != off {
				t.Fatalf("an EMPTY presence store renders different bytes from presence OFF")
			}

			// POSITIVE CONTROL: the owner, 42 s after a push, sees the badge and different bytes.
			owned := bodyOf(t, badgeServer(t, src, roamer, service(ownerOf(roamer), 42*time.Second, row)), path)
			if owned == off {
				t.Fatalf("POSITIVE CONTROL FAILED: the OWNER's page equals the no-presence page, so every equality below is vacuous")
			}
			if !strings.Contains(owned, `data-presence="`) {
				t.Fatalf("POSITIVE CONTROL FAILED: the owner's page carries no presence badge")
			}

			for arm, got := range map[string]string{
				"another owner's presence": bodyOf(t, badgeServer(t, src, roamer, service(ownerOf(easterner), 42*time.Second, row)), path),
				"expired presence (181 s after a 180 s TTL push)": bodyOf(t, badgeServer(t, src, roamer,
					service(ownerOf(roamer), presence.DefaultTTL+time.Second, row)), path),
				"no presence for THIS session": bodyOf(t, badgeServer(t, src, roamer, service(ownerOf(roamer), 42*time.Second,
					wireRow("s-other-09", "opencode", "notes:3", badgeHotkey, ""))), path),
			} {
				if got != off {
					t.Errorf("%s: the page's bytes differ from the no-presence page — presence leaked through the predicate", arm)
				}
			}
			// The narrowed owner is compared with ITS OWN no-presence page: the narrowing could change
			// what else the page shows, and the claim is only that presence adds nothing.
			narrowedOff := bodyOf(t, badgeServer(t, src, narrowed, nil), path)
			if got := bodyOf(t, badgeServer(t, src, narrowed, service(ownerOf(roamer), 42*time.Second, row)), path); got != narrowedOff {
				t.Errorf("a NARROWED bearer credential of the owner sees presence: its page differs from its no-presence page")
			}
		})
	}
}

// TestTheBadgeSaysWhereTheSessionRuns pins the badge's TEXT literally on every row surface, at two
// ages inside the TTL (42 s and 179 s — the boundary's inside edge), and the "live pane" badge on
// the two arc surfaces.
func TestTheBadgeSaysWhereTheSessionRuns(t *testing.T) {
	roamer, _ := sessionWorld(t)
	src := StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}
	for _, age := range []int{42, 179} {
		clock := &badgeClock{at: sessNow.Add(-time.Duration(age) * time.Second)}
		st := &presence.Store{Now: clock.now}
		st.Replace(ownerOf(roamer), "host-a", pushRows(t, "host-a",
			wireRow(badgeSession, "opencode", "notes:3", badgeHotkey, "2000-01-10T11:00:00Z")))
		clock.at = sessNow
		srv := badgeServer(t, src, roamer, &presence.Service{Store: st, Queue: &presence.Queue{}})
		want := "host-a · notes:3 · Alt+n · opencode · seen " + map[int]string{42: "42", 179: "179"}[age] + "s ago"

		for _, name := range []string{"session page", "scope page sessions tab", "arc page sessions tab"} {
			body := bodyOf(t, srv, badgeSurfaces()[name])
			got := paneText.FindAllStringSubmatch(body, -1)
			if len(got) != 1 || got[0][1] != want {
				t.Errorf("%s at %ds: pane badges %q, want exactly one reading %q", name, age, got, want)
			}
			if !strings.Contains(body, "label: "+badgeLabel) || !strings.Contains(body, "last activity: 2000-01-10T11:00:00Z") {
				t.Errorf("%s: the badge's tooltip does not carry the label and the host's last activity", name)
			}
			if strings.Contains(body, `data-presence="also-on"`) {
				t.Errorf("%s: ONE live host renders an \"also on\" badge", name)
			}
		}
		for _, name := range []string{"arcs-first page", "arc page (scopes tab)"} {
			body := bodyOf(t, srv, badgeSurfaces()[name])
			if n := strings.Count(body, `data-presence="live-pane"`); n != 1 || !strings.Contains(body, ">live pane</span>") {
				t.Errorf("%s: %d live-pane badges, want exactly one reading \"live pane\"", name, n)
			}
			if paneText.MatchString(body) {
				t.Errorf("%s renders a pane badge; the arc surfaces say only \"live pane\"", name)
			}
		}
	}
	// No hotkey is no segment, not an empty one.
	st := &presence.Store{Now: func() time.Time { return sessNow }}
	st.Replace(ownerOf(roamer), "host-c", pushRows(t, "host-c", wireRow(badgeSession, "claude", "notes:5", "", "")))
	body := bodyOf(t, badgeServer(t, src, roamer, &presence.Service{Store: st, Queue: &presence.Queue{}}), sessionURL(badgeSession))
	if got := paneText.FindStringSubmatch(body); got == nil || got[1] != "host-c · notes:5 · claude · seen 0s ago" {
		t.Errorf("a row with no hotkey renders %q", got)
	}
}

// TestTwoLiveHostsShowTheTargetAndAlsoOn — decision 7 reflected on the page: `s-roam-01` is live on
// host-a (older activity) and host-b (newer). The badge names host-b's row — its OWN target, notes:7,
// so a page that rendered host-a's row under host-b's label is red — and "also on host-a". RED with
// the "also on" badge dropped (`ui-presence-also-on-dropped`).
func TestTwoLiveHostsShowTheTargetAndAlsoOn(t *testing.T) {
	roamer, _ := sessionWorld(t)
	src := StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}
	st := &presence.Store{Now: func() time.Time { return sessNow }}
	st.Replace(ownerOf(roamer), "host-a", pushRows(t, "host-a",
		wireRow(badgeSession, "opencode", "notes:3", badgeHotkey, "2000-01-10T11:00:00Z")))
	st.Replace(ownerOf(roamer), "host-b", pushRows(t, "host-b",
		wireRow(badgeSession, "claude", "notes:7", "Alt+m", "2000-01-10T11:30:00Z")))
	srv := badgeServer(t, src, roamer, &presence.Service{Store: st, Queue: &presence.Queue{}})
	for _, name := range []string{"session page", "scope page sessions tab", "arc page sessions tab"} {
		body := bodyOf(t, srv, badgeSurfaces()[name])
		if got := paneText.FindAllStringSubmatch(body, -1); len(got) != 1 || got[0][1] != "host-b · notes:7 · Alt+m · claude · seen 0s ago" {
			t.Errorf("%s: pane badges %q, want host-b's row (the newer last_activity)", name, got)
		}
		also := regexp.MustCompile(`data-presence="also-on"[^>]*>([^<]*)<`).FindAllStringSubmatch(body, -1)
		if len(also) != 1 || also[0][1] != "also on host-a" {
			t.Errorf("%s: also-on badges %q, want exactly one reading \"also on host-a\"", name, also)
		}
	}
}

// TestPresenceNeverMakesAnUnseenSessionAPage — the plan's P5. An INVARIANT GUARD, not regression
// coverage: the handler binds presence only after its refusals, and no mutant of this change
// reorders that. The owner has a LIVE pane for a session that wrote only where the owner cannot
// read, and for one nobody ever wrote; both stay the uniform 404, byte for byte.
func TestPresenceNeverMakesAnUnseenSessionAPage(t *testing.T) {
	roamer, _ := sessionWorld(t)
	src := StoreSource{Root: sessionStore(t), ArcJournal: sessionJournal(t)}
	st := &presence.Store{Now: func() time.Time { return sessNow }}
	st.Replace(ownerOf(roamer), "host-a", pushRows(t, "host-a",
		wireRow("s-hide-02", "claude", "notes:3", badgeHotkey, ""), wireRow("s-never-03", "claude", "notes:4", "", "")))
	srv := badgeServer(t, src, roamer, &presence.Service{Store: st, Queue: &presence.Queue{}})
	for _, id := range []string{"s-hide-02", "s-never-03"} {
		if _, ok := st.For(roamer, id); !ok {
			t.Fatalf("INSTRUMENT: the owner has no live presence for %s, so the 404 below measures nothing", id)
		}
		rec := getAs(t, srv, sessionURL(id))
		if rec.Code != http.StatusNotFound || rec.Body.String() != sessionUnseenBody {
			t.Errorf("%s with a live pane answered %d %q, want the uniform 404", id, rec.Code, rec.Body.String())
		}
	}
}

// TestAPresenceServiceWithNoStoreIsRefused: nil `Presence` is the off state; a service with no store
// is a construction error rather than a nil dereference on the first session row.
func TestAPresenceServiceWithNoStoreIsRefused(t *testing.T) {
	cfg := testConfig(t, staticAuth{testIdentity()})
	cfg.Presence = &presence.Service{Queue: &presence.Queue{}}
	if _, err := New(cfg); err != ErrPresenceWithoutStore {
		t.Fatalf("New with a store-less presence service answered %v, want ErrPresenceWithoutStore", err)
	}
	cfg.Presence = nil
	if _, err := New(cfg); err != nil {
		t.Fatalf("POSITIVE CONTROL: New with presence off answered %v", err)
	}
}
