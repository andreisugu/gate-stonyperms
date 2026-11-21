package manager

import (
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/model"
	"github.com/andreisugu/gate-stonyperms/storage"
)

func newTestManager(t *testing.T, wrap func(storage.StorageProvider) storage.StorageProvider) *Manager {
	t.Helper()
	store, err := storage.NewJSONStorage(filepath.Join(t.TempDir(), "stonyperms"))
	if err != nil {
		t.Fatalf("new json storage: %v", err)
	}
	var provider storage.StorageProvider = store
	if wrap != nil {
		provider = wrap(store)
	}
	mgr, err := New(provider)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(mgr.Close)
	return mgr
}

func expectEvent(t *testing.T, ch <-chan ChangeEvent, wantType string) ChangeEvent {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatalf("event channel closed waiting for %q", wantType)
		}
		if ev.Type != wantType {
			t.Fatalf("event type = %q, want %q (%+v)", ev.Type, wantType, ev)
		}
		if ev.Revision == 0 || ev.Timestamp == 0 {
			t.Fatalf("event missing revision/timestamp: %+v", ev)
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for event %q", wantType)
		return ChangeEvent{}
	}
}

func TestSubscribeEventsAllMutationsAndTracks(t *testing.T) {
	mgr := newTestManager(t, nil)

	var listenerCalls atomic.Int32
	mgr.SetUpdateListener(func() {
		listenerCalls.Add(1)
	})

	ch, unsub := mgr.SubscribeEvents(32)
	defer unsub()

	id := uuid.New()
	_, _, _, err := mgr.ResolveOrCreateUser(id, "Alpha", "default", "tester")
	if err != nil {
		t.Fatalf("resolve/create user: %v", err)
	}
	ev := expectEvent(t, ch, EventUserUpdated)
	if ev.UUID != id.String() || ev.Username != "Alpha" || ev.Actor != "tester" {
		t.Fatalf("unexpected user_updated event: %+v", ev)
	}

	if _, err := mgr.DeleteUserByID(id, "tester"); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	ev = expectEvent(t, ch, EventUserDeleted)
	if ev.UUID != id.String() || ev.Username != "Alpha" {
		t.Fatalf("unexpected user_deleted event: %+v", ev)
	}

	if _, err := mgr.CreateGroup("vip", 50, "VIP"); err != nil {
		t.Fatalf("create group: %v", err)
	}
	ev = expectEvent(t, ch, EventGroupUpdated)
	if ev.Group != "vip" {
		t.Fatalf("unexpected group_updated event: %+v", ev)
	}

	if err := mgr.DeleteGroup("vip"); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	ev = expectEvent(t, ch, EventGroupDeleted)
	if ev.Group != "vip" {
		t.Fatalf("unexpected group_deleted event: %+v", ev)
	}

	revBefore := mgr.Revision()
	tr, err := mgr.CreateTrack("staff")
	if err != nil {
		t.Fatalf("create track: %v", err)
	}
	if mgr.Revision() <= revBefore {
		t.Fatalf("CreateTrack did not bump revision")
	}
	ev = expectEvent(t, ch, EventTrackUpdated)
	if ev.Track != "staff" {
		t.Fatalf("unexpected track_updated event: %+v", ev)
	}

	trCopy := mgr.GetTrackCopy(tr.Name)
	trCopy.Append("admin")
	if err := mgr.SaveTrack(trCopy); err != nil {
		t.Fatalf("save track: %v", err)
	}
	expectEvent(t, ch, EventTrackUpdated)

	if err := mgr.DeleteTrack("staff"); err != nil {
		t.Fatalf("delete track: %v", err)
	}
	ev = expectEvent(t, ch, EventTrackDeleted)
	if ev.Track != "staff" {
		t.Fatalf("unexpected track_deleted event: %+v", ev)
	}

	if err := mgr.ReloadWithActor("ops"); err != nil {
		t.Fatalf("reload: %v", err)
	}
	ev = expectEvent(t, ch, EventReload)
	if ev.Actor != "ops" {
		t.Fatalf("unexpected reload event: %+v", ev)
	}

	// Expiry cleanup event
	id2 := uuid.New()
	_, _, err = mgr.MutateUser(id2, "TempPlayer", true, "default", "ops", func(u *model.User) (bool, string, error) {
		u.AddNode(model.Node{Key: "temp.perm", Value: true, Expiry: time.Now().Add(-time.Second).Unix()})
		return true, "added expired node", nil
	})
	if err != nil {
		t.Fatalf("mutate temp user: %v", err)
	}
	expectEvent(t, ch, EventUserUpdated)

	mgr.cleanupExpired(time.Now())
	expectEvent(t, ch, EventExpiryCleanup)

	// Unsubscribe is idempotent and closes channel cleanly.
	unsub()
	unsub()
	if _, ok := <-ch; ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}
	if listenerCalls.Load() == 0 {
		t.Fatal("expected SetUpdateListener to be invoked")
	}
}

func TestSlowSubscriberDoesNotBlockMutations(t *testing.T) {
	mgr := newTestManager(t, nil)
	ch, unsub := mgr.SubscribeEvents(1)
	defer unsub()

	for i := 0; i < 20; i++ {
		if _, err := mgr.CreateGroup("g"+string(rune('a'+i)), i, ""); err != nil {
			t.Fatalf("create group %d: %v", i, err)
		}
	}

	// Only 1 buffered item should be queued; channel did not block.
	select {
	case <-ch:
	default:
		t.Fatal("expected at least 1 buffered event")
	}
}

func TestMutateRollbackOnErrorAndPersistenceFailure(t *testing.T) {
	var failUserSave atomic.Bool
	mgr := newTestManager(t, func(base storage.StorageProvider) storage.StorageProvider {
		return &toggleFailingStorage{StorageProvider: base, failUserSave: &failUserSave}
	})

	id := uuid.New()
	_, _, _, err := mgr.ResolveOrCreateUser(id, "OldName", "default", "init")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	revBefore := mgr.Revision()
	// 1. Callback error must leave state & revision untouched.
	_, _, err = mgr.MutateUser(id, "NewName", false, "default", "ops", func(u *model.User) (bool, string, error) {
		u.AddNode(model.NewNode("should.rollback", true))
		return true, "bad", errors.New("validation boom")
	})
	if err == nil {
		t.Fatal("expected callback error")
	}
	if mgr.Revision() != revBefore {
		t.Fatalf("revision changed after error: %d -> %d", revBefore, mgr.Revision())
	}
	if _, ok, _ := mgr.ResolveUserByName("NewName"); ok {
		t.Fatal("username index changed on callback error")
	}
	u, ok, _ := mgr.ResolveUserByName("OldName")
	if !ok || len(u.Nodes) != 0 {
		t.Fatalf("unexpected user mutation on callback error: %+v", u)
	}

	// 2. Storage failure must persist-before-publish and roll back cleanly.
	failUserSave.Store(true)
	_, _, err = mgr.MutateUser(id, "PersistedFail", false, "default", "ops", func(u *model.User) (bool, string, error) {
		u.AddNode(model.NewNode("disk.fail", true))
		return true, "should not publish", nil
	})
	if err == nil {
		t.Fatal("expected storage save failure")
	}
	if mgr.Revision() != revBefore {
		t.Fatalf("revision changed after storage error: %d -> %d", revBefore, mgr.Revision())
	}
	if _, ok, _ := mgr.ResolveUserByName("PersistedFail"); ok {
		t.Fatal("failed save leaked into nameToUUID")
	}
	u, ok, _ = mgr.UserSnapshot(id)
	if !ok || u.Username != "OldName" || len(u.Nodes) != 0 {
		t.Fatalf("failed save mutated visible user: %+v", u)
	}

	// 3. Successful rename cleans stale lowercase username index.
	failUserSave.Store(false)
	renamed, _, err := mgr.MutateUser(id, "RenamedUser", false, "default", "ops", func(u *model.User) (bool, string, error) {
		return true, "renamed", nil
	})
	if err != nil || renamed.Username != "RenamedUser" {
		t.Fatalf("rename failed: %v %+v", err, renamed)
	}
	if _, ok, _ := mgr.ResolveUserByName("oldname"); ok {
		t.Fatal("stale username still resolved after rename")
	}
	if got, ok, _ := mgr.ResolveUserByName("renameduser"); !ok || got.UUID != id {
		t.Fatalf("new username failed lookup: ok=%v got=%+v", ok, got)
	}
}

func TestReadQueriesAndLogCopyIsolation(t *testing.T) {
	mgr := newTestManager(t, nil)
	if _, err := mgr.CreateGroup("vip", 50, "VIP"); err != nil {
		t.Fatalf("create vip: %v", err)
	}

	u1 := uuid.New()
	u2 := uuid.New()
	u3 := uuid.New()
	_, _, _ = mgr.MutateUser(u1, "Zara", true, "default", "paynow", func(u *model.User) (bool, string, error) {
		u.AddParent(model.Inheritance{Group: "vip", Expiry: time.Now().Add(time.Hour).Unix()})
		return true, "grant vip", nil
	})
	_, _, _ = mgr.MutateUser(u2, "Amy", true, "default", "admin", func(u *model.User) (bool, string, error) {
		u.AddParent(model.Inheritance{Group: "vip", Expiry: time.Now().Add(-time.Minute).Unix()})
		return true, "expired vip", nil
	})
	_, _, _ = mgr.MutateUser(u3, "Ben", true, "vip", "paynow", func(u *model.User) (bool, string, error) {
		return true, "primary vip", nil
	})

	// ListUsers without filter: sorted Amy, Ben, Zara
	all, total, _ := mgr.ListUsers("", 0, 2)
	if total != 3 || len(all) != 2 || all[0].Username != "Amy" || all[1].Username != "Ben" {
		t.Fatalf("unexpected ListUsers page 1: total=%d users=%+v", total, all)
	}
	page2, _, _ := mgr.ListUsers("", 2, 2)
	if len(page2) != 1 || page2[0].Username != "Zara" {
		t.Fatalf("unexpected ListUsers page 2: %+v", page2)
	}

	// Filter by vip: Amy's parent is expired, so only Ben and Zara match.
	vips, vipTotal, _ := mgr.ListUsers("vip", 0, 50)
	if vipTotal != 2 || len(vips) != 2 || vips[0].Username != "Ben" || vips[1].Username != "Zara" {
		t.Fatalf("unexpected vip ListUsers: total=%d users=%+v", vipTotal, vips)
	}

	members, found, _ := mgr.GroupMembersSnapshot("vip")
	if !found || len(members) != 2 || !members[0].Permanent || members[1].Permanent {
		t.Fatalf("unexpected vip members: found=%v members=%+v", found, members)
	}
	if _, found, _ := mgr.GroupMembersSnapshot("nonexistent"); found {
		t.Fatal("expected missing group members snapshot to report found=false")
	}

	tracks, _ := mgr.TracksSnapshot()
	if len(tracks) != 1 || tracks[0].Name != "ranks" {
		t.Fatalf("unexpected tracks snapshot: %+v", tracks)
	}
	if _, ok, _ := mgr.TrackSnapshot("missing"); ok {
		t.Fatal("expected missing track to return false")
	}

	// QueryLogs filters newest-first and GetLogs returns an isolated copy.
	paynowLogs, _ := mgr.QueryLogs(10, "user", "", "paynow")
	if len(paynowLogs) != 2 || paynowLogs[0].Target != "Ben" || paynowLogs[1].Target != "Zara" {
		t.Fatalf("unexpected QueryLogs output: %+v", paynowLogs)
	}
	logsCopy := mgr.GetLogs(10)
	origTarget := logsCopy[0].Target
	logsCopy[0].Target = "corrupted"
	if mgr.GetLogs(10)[0].Target != origTarget {
		t.Fatal("GetLogs returned slice sharing backing array")
	}
}

type toggleFailingStorage struct {
	storage.StorageProvider
	failUserSave *atomic.Bool
}

func (t *toggleFailingStorage) SaveUser(u *model.User) error {
	if t.failUserSave.Load() {
		return errors.New("injected save user failure")
	}
	return t.StorageProvider.SaveUser(u)
}
