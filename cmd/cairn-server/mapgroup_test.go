package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

func groupMapFlagsFor(unmap bool, group, provider, object, verbs string) *groupMapFlags {
	on, off := true, false
	f := &groupMapFlags{group: &group, provider: &provider, object: &object, verbs: &verbs}
	if unmap {
		f.mapGroup, f.unmapGroup = &off, &on
	} else {
		f.mapGroup, f.unmapGroup = &on, &off
	}
	return f
}

// TestMapGroupThenUnmapGroupWriteOneRecordEach is the success contract of both modes: the
// stable stdout line, the record read back from the journal, and the project warning.
func TestMapGroupThenUnmapGroupWriteOneRecordEach(t *testing.T) {
	journal, env, a, _ := aJournalWithTwoSeparateUsers(t)
	before := modelOf(t, journal)
	var out, errOut bytes.Buffer

	if rc := runGroupMap(env, false, groupMapFlagsFor(false, "admins", "issuer", string(a.Project), "read,write"), &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	line := regexp.MustCompile(`^cairn-control: group-mapped provider=issuer group=admins object=project:` +
		regexp.QuoteMeta(string(a.Project)) + ` verbs=read,write effective=read epoch=([0-9]+)\n$`)
	if !line.MatchString(out.String()) {
		t.Fatalf("stdout is not the one machine-readable line: %q", out.String())
	}
	after := modelOf(t, journal)
	if after.Epoch != before.Epoch+1 || len(after.GroupMaps) != 1 {
		t.Fatalf("the journal grew by %d event(s) and holds %d mapping(s)", after.Epoch-before.Epoch, len(after.GroupMaps))
	}
	for _, want := range []string{"reaches EVERY scope of project", "including every scope added to it later",
		"WARNING — write is recorded and has NO EFFECT", "this mapping confers read",
		"$" + identity.EnvSupabaseGroupsClaim} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr does not say %q:\n%s", want, errOut.String())
		}
	}

	out.Reset()
	errOut.Reset()
	if rc := runGroupMap(env, true, groupMapFlagsFor(true, "admins", "issuer", string(a.Project), ""), &out, &errOut); rc != 0 {
		t.Fatalf("unmap rc=%d stderr=%s", rc, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "cairn-control: group-unmapped provider=issuer group=admins object=project:") {
		t.Fatalf("unmap stdout: %q", out.String())
	}
	if m := modelOf(t, journal); len(m.GroupMaps) != 0 || m.Epoch != before.Epoch+2 {
		t.Fatalf("after the unmap: %d mapping(s), epoch %d", len(m.GroupMaps), m.Epoch)
	}
}

// TestEveryGroupMapRefusalWritesNothing covers each refusal by a fragment unique to it, and
// asserts exit 78, an empty stdout and a byte-identical journal for every one.
func TestEveryGroupMapRefusalWritesNothing(t *testing.T) {
	journal, env, a, _ := aJournalWithTwoSeparateUsers(t)
	scope := string(a.Scopes[0].ID)
	// A live mapping, so the duplicate arm has something to collide with.
	var o, e bytes.Buffer
	if rc := runGroupMap(env, false, groupMapFlagsFor(false, "readers", "issuer", scope, "read"), &o, &e); rc != 0 {
		t.Fatalf("precondition: the first map failed: %s", e.String())
	}
	for _, c := range []struct {
		name     string
		unmap    bool
		f        *groupMapFlags
		env      map[string]string
		fragment string
	}{
		{"no journal", false, groupMapFlagsFor(false, "x", "issuer", scope, "read"), map[string]string{}, "needs a control journal"},
		{"no group", false, groupMapFlagsFor(false, "", "issuer", scope, "read"), env, "refused: -group required"},
		{"no provider", false, groupMapFlagsFor(false, "x", "", scope, "read"), env, "-group-provider"},
		{"no object", false, groupMapFlagsFor(false, "x", "issuer", "", "read"), env, "-group-object"},
		{"no verbs on a map", false, groupMapFlagsFor(false, "x", "issuer", scope, ""), env, "-group-verbs required"},
		{"verbs on an unmap", true, groupMapFlagsFor(true, "readers", "issuer", scope, "read"), env, "-group-verbs is not accepted"},
		{"an object of no kind", false, groupMapFlagsFor(false, "x", "issuer", "usr_someone", "read"), env, "neither a scope id"},
		{"an unknown object", false, groupMapFlagsFor(false, "x", "issuer", "scp_nowhere", "read"), env, "holds no scope with id"},
		{"a bad verb", false, groupMapFlagsFor(false, "x", "issuer", scope, "read,own"), env, "-group-verbs names"},
		{"a bad group name", false, groupMapFlagsFor(false, "Ops Team", "issuer", scope, "read"), env, "not a usable group name"},
		{"a duplicate map", false, groupMapFlagsFor(false, "readers", "issuer", scope, "read,write"), env, "already mapped"},
		{"an unmap of nothing", true, groupMapFlagsFor(true, "ghost", "issuer", scope, ""), env, "no live mapping"},
	} {
		before, err := os.ReadFile(journal)
		if err != nil {
			t.Fatal(err)
		}
		var out, errOut bytes.Buffer
		rc := runGroupMap(c.env, c.unmap, c.f, &out, &errOut)
		if rc != exitConfig || out.Len() != 0 {
			t.Errorf("%s: rc=%d stdout=%q, want %d and nothing", c.name, rc, out.String(), exitConfig)
		}
		if !strings.Contains(errOut.String(), c.fragment) {
			t.Errorf("%s: stderr lacks %q, so a different refusal answered:\n%s", c.name, c.fragment, errOut.String())
		}
		after, _ := os.ReadFile(journal)
		if !bytes.Equal(before, after) {
			t.Errorf("%s: a refused command changed the journal", c.name)
		}
	}
	if m := modelOf(t, journal); len(m.GroupMaps) != 1 || m.GroupMaps[control.GroupMapKey{
		Provider: "issuer", Group: "readers", ObjectKind: control.ObjectScope, ObjectID: a.Scopes[0].ID}].Verbs !=
		control.NewVerbSet(control.VerbRead) {
		t.Fatalf("the precondition mapping changed: %+v", m.GroupMaps)
	}
}

// TestTheStartupAnnouncementCountsUsersTheModeMakesUnreachable pins the known-user
// decision's operator half: the mode is said, and so is the count it shadows.
func TestTheStartupAnnouncementCountsUsersTheModeMakesUnreachable(t *testing.T) {
	journal, _, _, _ := aJournalWithTwoSeparateUsers(t)
	sessions, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatal(err)
	}
	m := modelOf(t, journal)
	provider := ""
	for _, u := range m.Users {
		provider = u.Provider
	}
	n := m.UsersUnderProvider(provider)
	if n == 0 {
		t.Fatal("precondition: the fixture holds no users")
	}
	for _, c := range []struct {
		provider string
		warns    bool
	}{{provider, true}, {"some-other-namespace", false}} {
		backend, err := identity.NewSupabaseJWT(identity.SupabaseConfig{
			Authority: cacheOver(t, sessions), Keys: &identity.KeySet{}, Issuer: "https://issuer.example.test",
			Audience: "cairn", Provider: c.provider, GroupsClaim: "groups",
		})
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		announceGroupFederation(backend, cacheOver(t, sessions), func(l string) { lines = append(lines, l) })
		all := strings.Join(lines, "\n")
		if !strings.Contains(all, "GROUP FEDERATION IS ON") || !strings.Contains(all, `"groups" claim`) {
			t.Errorf("%s: the mode is not announced:\n%s", c.provider, all)
		}
		warned := strings.Contains(all, "NONE of them is reachable through this verifier")
		if warned != c.warns {
			t.Errorf("provider %q: warned=%v, want %v:\n%s", c.provider, warned, c.warns, all)
		}
	}
}

func cacheOver(t *testing.T, s control.Source) *control.Cache {
	t.Helper()
	c := control.NewCache(s, control.CacheOptions{})
	if err := c.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestTheBinaryActuallyDispatchesMapGroupAndUnmapGroup runs the REAL binary: a mode that is
// registered and ledgered but never dispatched falls through to SERVING, which every
// in-process test of `runGroupMap` is blind to.
func TestTheBinaryActuallyDispatchesMapGroupAndUnmapGroup(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	journal, _, a, _ := aJournalWithTwoSeparateUsers(t)
	run := func(args ...string) (int, string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		child := exec.CommandContext(ctx, self, args...)
		child.Env = []string{
			runServerEnv + "=1",
			testRefreshEnv + "=" + testRefreshPeriod.String(),
			EnvControlJournal + "=" + journal,
		}
		body, _ := child.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("`%v` did not exit within 20s — it fell through to serving:\n%s", args, body)
		}
		return child.ProcessState.ExitCode(), string(body)
	}
	key := []string{"-group", "admins", "-group-provider", "issuer", "-group-object", string(a.Project)}
	if code, body := run(append([]string{"-map-group", "-unmap-group"}, key...)...); code != exitConfig ||
		!strings.Contains(body, "are both/all set") {
		t.Fatalf("`-map-group -unmap-group` exited %d:\n%s", code, body)
	}
	if code, body := run(append(append([]string{"-map-group"}, key...), "-group-verbs", "read")...); code != 0 ||
		!strings.Contains(body, "cairn-control: group-mapped provider=issuer group=admins") {
		t.Fatalf("`-map-group` exited %d:\n%s", code, body)
	}
	if n := len(modelOf(t, journal).GroupMaps); n != 1 {
		t.Fatalf("the child reported success and the journal holds %d mapping(s)", n)
	}
	// The flag help is a contract: it must say verbs are RECORDED and CAPPED, never that the
	// group "gets" them.
	_, help := run("-h")
	for _, want := range []string{
		"the comma-separated verbs RECORDED for the group on the object (read, write, admin). A federated " +
			"principal is CAPPED AT READ when the mapping is resolved, so write and admin are recorded and " +
			"have no effect, and a set without read is refused.",
		"at -group-verbs CAPPED AT READ;",
	} {
		if !strings.Contains(strings.Join(strings.Fields(help), " "), want) {
			t.Errorf("`-h` does not say %q:\n%s", want, help)
		}
	}
	if code, body := run(append([]string{"-unmap-group"}, key...)...); code != 0 ||
		!strings.Contains(body, "cairn-control: group-unmapped provider=issuer group=admins") {
		t.Fatalf("`-unmap-group` exited %d:\n%s", code, body)
	}
	if n := len(modelOf(t, journal).GroupMaps); n != 0 {
		t.Fatalf("after the unmap the journal holds %d mapping(s)", n)
	}
}

// TestAGroupMappingWithoutReadIsRefusedAndWritesNothing: under the read ceiling a set with
// no `read` reaches nothing, so it is a refusal rather than an inert record.
//
// RED at 2e9c545: there `-group-verbs write` exited 0 and appended a mapping that conferred
// nothing on any request.
func TestAGroupMappingWithoutReadIsRefusedAndWritesNothing(t *testing.T) {
	journal, env, a, _ := aJournalWithTwoSeparateUsers(t)
	for _, verbs := range []string{"write", "admin", "write,admin"} {
		before := modelOf(t, journal).Epoch
		var out, errOut bytes.Buffer
		rc := runGroupMap(env, false, groupMapFlagsFor(false, "writers", "issuer", string(a.Scopes[0].ID), verbs), &out, &errOut)
		if rc != exitConfig || out.Len() != 0 {
			t.Errorf("-group-verbs %s: rc=%d stdout=%q, want %d and nothing", verbs, rc, out.String(), exitConfig)
		}
		if !strings.Contains(errOut.String(), "a federated mapping without read reaches nothing under the read ceiling") {
			t.Errorf("-group-verbs %s: the refusal does not name the reason:\n%s", verbs, errOut.String())
		}
		if after := modelOf(t, journal).Epoch; after != before {
			t.Errorf("-group-verbs %s: the journal moved %d -> %d", verbs, before, after)
		}
	}
}

// TestAReadOnlyGroupMappingPrintsNoInertVerbWarning is an INVARIANT guard (true at 2e9c545
// too): with nothing above the ceiling there is nothing to warn about, and the line's two
// sets agree.
func TestAReadOnlyGroupMappingPrintsNoInertVerbWarning(t *testing.T) {
	_, env, a, _ := aJournalWithTwoSeparateUsers(t)
	var out, errOut bytes.Buffer
	if rc := runGroupMap(env, false, groupMapFlagsFor(false, "readers", "issuer", string(a.Scopes[0].ID), "read"), &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errOut.String())
	}
	if !strings.Contains(out.String(), " verbs=read effective=read epoch=") {
		t.Errorf("stdout: %q", out.String())
	}
	if strings.Contains(errOut.String(), "NO EFFECT") {
		t.Errorf("a read-only mapping was warned about:\n%s", errOut.String())
	}
}
