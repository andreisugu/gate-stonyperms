package manager

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.minekube.com/gate/pkg/util/permission"

	"github.com/andreisugu/gate-stonyperms/model"
	"github.com/andreisugu/gate-stonyperms/storage"
)

func TestManagerPromoteDemote(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "stonyperms_mgr_test_*")
	defer os.RemoveAll(tmpDir)

	store, _ := storage.NewJSONStorage(filepath.Join(tmpDir, "stonyperms"))
	mgr, err := New(store)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer mgr.Close()

	// Add VIP group to track
	_, _ = mgr.CreateGroup("vip", 50, "&aVIP")
	track := mgr.GetTrack("ranks")
	track.Clear()
	track.Append("default")
	track.Append("vip")
	track.Append("admin")
	_ = mgr.SaveTrack(track)

	u := mgr.GetOrCreateUser(uuid.New(), "PlayerOne")
	if u.PrimaryGroup != "default" {
		t.Fatalf("expected primary group default, got %s", u.PrimaryGroup)
	}

	// Promote: default -> vip
	from, to, err := mgr.PromoteUser(u, "ranks", "Console")
	if err != nil {
		t.Fatalf("unexpected promotion error: %v", err)
	}
	if from != "default" || to != "vip" {
		t.Fatalf("expected default -> vip, got %s -> %s", from, to)
	}

	// Promote: vip -> admin
	from, to, err = mgr.PromoteUser(u, "ranks", "Console")
	if err != nil {
		t.Fatalf("unexpected promotion error: %v", err)
	}
	if from != "vip" || to != "admin" {
		t.Fatalf("expected vip -> admin, got %s -> %s", from, to)
	}

	// Promote past admin -> should error
	_, _, err = mgr.PromoteUser(u, "ranks", "Console")
	if err == nil {
		t.Fatalf("expected error promoting past highest rank")
	}

	// Demote: admin -> vip
	from, to, err = mgr.DemoteUser(u, "ranks", "Console")
	if err != nil {
		t.Fatalf("unexpected demotion error: %v", err)
	}
	if from != "admin" || to != "vip" {
		t.Fatalf("expected admin -> vip, got %s -> %s", from, to)
	}
}

func TestManagerGatePermissionFunc(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "stonyperms_mgr_test2_*")
	defer os.RemoveAll(tmpDir)

	store, _ := storage.NewJSONStorage(filepath.Join(tmpDir, "stonyperms"))
	mgr, _ := New(store)
	defer mgr.Close()

	u := mgr.GetOrCreateUser(uuid.New(), "Tester")
	edited := mgr.GetUserCopy(u.UUID)
	edited.AddNode(model.NewNode("gate.command.server", true))
	edited.AddNode(model.NewNode("gate.command.send", false))
	if err := mgr.SaveUser(edited); err != nil {
		t.Fatalf("save user: %v", err)
	}

	permFunc := mgr.GatePermissionFunc(u.UUID, nil)

	if permFunc("gate.command.server") != permission.True {
		t.Fatalf("expected gate.command.server to be True, got %v", permFunc("gate.command.server"))
	}
	if permFunc("gate.command.send") != permission.False {
		t.Fatalf("expected gate.command.send to be False, got %v", permFunc("gate.command.send"))
	}
	if permFunc("gate.unknown") != permission.Undefined {
		t.Fatalf("expected gate.unknown to be Undefined, got %v", permFunc("gate.unknown"))
	}
}

func TestCleanupExpiredAdvancesRevision(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := storage.NewJSONStorage(filepath.Join(tmpDir, "stonyperms"))
	mgr, err := New(store)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	u := mgr.GetOrCreateUser(uuid.New(), "Expiring")
	edited := mgr.GetUserCopy(u.UUID)
	edited.AddNode(model.Node{Key: "temp.node", Value: true, Expiry: time.Now().Add(-time.Second).Unix()})
	if err := mgr.SaveUser(edited); err != nil {
		t.Fatalf("save user: %v", err)
	}

	before := mgr.Revision()
	mgr.cleanupExpired(time.Now())
	if mgr.Revision() <= before {
		t.Fatalf("cleanup did not advance revision: before=%d after=%d", before, mgr.Revision())
	}

	eff, _, _ := mgr.EffectiveUser(u.UUID, nil, "default")
	for _, node := range eff.Nodes {
		if node.Node.Key == "temp.node" {
			t.Fatal("expired node remained in effective snapshot")
		}
	}
}

func TestConcurrentCommandStyleMutationAndSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := storage.NewJSONStorage(filepath.Join(tmpDir, "stonyperms"))
	mgr, err := New(store)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	id := uuid.New()
	mgr.GetOrCreateUser(id, "Concurrent")
	if _, err := mgr.CreateGroup("vip", 50, ""); err != nil {
		t.Fatalf("create group: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 150; i++ {
			copy := mgr.GetUserCopy(id)
			copy.AddNode(model.NewNode("race.node", i%2 == 0))
			_ = mgr.SaveUser(copy)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 150; i++ {
			g := mgr.GetGroupCopy("vip")
			g.Weight = i
			_ = mgr.SaveGroup(g)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 150; i++ {
			_, _, _ = mgr.EffectiveUser(id, map[string]string{"server": "lobby"}, "default")
			_, _ = mgr.GroupsSnapshot()
			_, _, _ = mgr.EvaluateUser(id, "race.node", nil, "default")
		}
	}()
	wg.Wait()
}

func TestPersistenceFailureKeepsVisibleStateAndRevision(t *testing.T) {
	tmpDir := t.TempDir()
	base, _ := storage.NewJSONStorage(filepath.Join(tmpDir, "stonyperms"))
	store := failingGroupStorage{StorageProvider: base, err: errors.New("disk full")}
	mgr, err := New(store)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	before := mgr.Revision()
	if _, err := mgr.CreateGroup("persistfail", 10, ""); err == nil {
		t.Fatal("expected persistence failure")
	}
	if mgr.Revision() <= before {
		t.Fatalf("visible group change did not advance revision: before=%d after=%d", before, mgr.Revision())
	}
	if mgr.GetGroup("persistfail") == nil {
		t.Fatal("group should remain visible with its new revision")
	}
}

func TestGenerationAndCopiesAreIsolated(t *testing.T) {
	firstDir := t.TempDir()
	firstStore, _ := storage.NewJSONStorage(filepath.Join(firstDir, "stonyperms"))
	first, err := New(firstStore)
	if err != nil {
		t.Fatalf("first manager: %v", err)
	}
	defer first.Close()

	secondDir := t.TempDir()
	secondStore, _ := storage.NewJSONStorage(filepath.Join(secondDir, "stonyperms"))
	second, err := New(secondStore)
	if err != nil {
		t.Fatalf("second manager: %v", err)
	}
	defer second.Close()

	if first.Generation() == "" || second.Generation() == "" {
		t.Fatal("generation must not be empty")
	}
	if first.Generation() == second.Generation() {
		t.Fatal("independent managers must not share a generation")
	}

	id := uuid.New()
	u := first.GetOrCreateUser(id, "Isolated")
	copy := first.GetUserCopy(id)
	copy.AddNode(model.NewNode("only.copy", true))
	if len(first.GetUser(id).Nodes) != 0 {
		t.Fatal("mutating a copy changed the published user")
	}
	if err := first.SaveUser(copy); err != nil {
		t.Fatalf("save copy: %v", err)
	}
	if len(first.GetUser(id).Nodes) != 1 {
		t.Fatal("saved copy was not published")
	}
	_ = u
}

type failingGroupStorage struct {
	storage.StorageProvider
	err error
}

func (f failingGroupStorage) SaveGroup(*model.Group) error {
	return f.err
}

func TestGatePermissionFuncSyntheticGroups(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := storage.NewJSONStorage(filepath.Join(tmpDir, "stonyperms"))
	mgr, err := New(store)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	_, _ = mgr.CreateGroup("default", 0, "&7")
	_, _ = mgr.CreateGroup("vip", 100, "&a[VIP] ")
	eliteGroup, _ := mgr.CreateGroup("elite", 120, "&6[Elite] ")
	eliteCopy := mgr.GetGroupCopy(eliteGroup.Name)
	eliteCopy.AddParent(model.NewInheritance("default"))
	if err := mgr.SaveGroup(eliteCopy); err != nil {
		t.Fatalf("save elite group: %v", err)
	}

	id := uuid.New()
	_ = mgr.GetOrCreateUser(id, "RankPlayer")
	u := mgr.GetUserCopy(id)
	u.PrimaryGroup = "elite"
	u.AddParent(model.NewInheritance("vip"))
	// Explicit negation should still win over synthetic group permission.
	u.AddNode(model.NewNode("group.vip", false))
	if err := mgr.SaveUser(u); err != nil {
		t.Fatalf("save user: %v", err)
	}

	permFn := mgr.GatePermissionFunc(id, func() map[string]string {
		return map[string]string{"server": "lobby"}
	})

	if got := permFn("stonyperms.primarygroup.elite"); got != permission.True {
		t.Fatalf("expected stonyperms.primarygroup.elite True, got %v", got)
	}
	if got := permFn("bettertab.role.elite"); got != permission.True {
		t.Fatalf("expected bettertab.role.elite True, got %v", got)
	}
	if got := permFn("bettertab.role.default"); got != permission.Undefined {
		t.Fatalf("expected bettertab.role.default Undefined, got %v", got)
	}
	if got := permFn("stonyperms.group.elite"); got != permission.True {
		t.Fatalf("expected stonyperms.group.elite True, got %v", got)
	}
	if got := permFn("group.default"); got != permission.True {
		t.Fatalf("expected group.default True via inheritance, got %v", got)
	}
	if got := permFn("group.vip"); got != permission.False {
		t.Fatalf("expected explicit negation group.vip False, got %v", got)
	}
}

func TestPolishRenameAuditDefaultAndGroupMembers(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.NewJSONStorage(filepath.Join(tmpDir, "stonyperms"))
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	mgr, err := New(store)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	_, _ = mgr.CreateGroup("member", 5, "")
	mgr.SetDefaultGroup("member")
	if got := mgr.DefaultGroup(); got != "member" {
		t.Fatalf("DefaultGroup = %q, want member", got)
	}
	if _, _, err := mgr.RenameGroup("member", "other", "Console"); err != ErrCannotRenameDefault {
		t.Fatalf("expected ErrCannotRenameDefault for configured default group, got %v", err)
	}
	if err := mgr.DeleteGroup("member"); err == nil {
		t.Fatal("expected DeleteGroup to reject configured default group")
	}

	// Disable audit logging temporarily
	beforeLogs := len(mgr.GetLogs(100))
	mgr.SetAuditLogEnabled(false)
	mgr.LogAction("Console", "general", "test", "suppressed entry")
	if logs := mgr.GetLogs(100); len(logs) != beforeLogs {
		t.Fatalf("expected audit logs unchanged (%d) when disabled, got %d", beforeLogs, len(logs))
	}
	mgr.SetAuditLogEnabled(true)

	// Setup group hierarchy + track + users for RenameGroup cascade & GetGroupMembers filtering
	_, _ = mgr.CreateGroup("oldvip", 50, "OldVIP")
	_, _ = mgr.CreateGroup("child", 60, "Child")
	child := mgr.GetGroupCopy("child")
	child.AddParent(model.NewInheritance("oldvip"))
	if err := mgr.SaveGroup(child); err != nil {
		t.Fatalf("save child: %v", err)
	}
	tr, _ := mgr.CreateTrack("donors")
	_ = tr.Append("member")
	_ = tr.Append("oldvip")
	if err := mgr.SaveTrack(tr); err != nil {
		t.Fatalf("save track: %v", err)
	}

	uZed := mgr.GetOrCreateUser(uuid.New(), "Zed")
	if uZed.PrimaryGroup != "member" {
		t.Fatalf("GetOrCreateUser PrimaryGroup = %q, want member", uZed.PrimaryGroup)
	}
	uZedCopy := mgr.GetUserCopy(uZed.UUID)
	uZedCopy.PrimaryGroup = "oldvip"
	uZedCopy.AddParent(model.NewInheritance("oldvip"))
	_ = mgr.SaveUser(uZedCopy)

	uExpired := mgr.GetOrCreateUser(uuid.New(), "Alice")
	uExpCopy := mgr.GetUserCopy(uExpired.UUID)
	uExpCopy.Parents = append(uExpCopy.Parents, model.Inheritance{
		Group:  "oldvip",
		Expiry: time.Now().Add(-10 * time.Minute).Unix(),
	})
	_ = mgr.SaveUser(uExpCopy)

	members := mgr.GetGroupMembers("oldvip")
	if len(members) != 1 || members[0].Username != "Zed" {
		t.Fatalf("expected only active member Zed, got %+v", members)
	}

	if _, _, err := mgr.RenameGroup("oldvip", "newvip", "Console"); err != nil {
		t.Fatalf("RenameGroup: %v", err)
	}
	if mgr.GetGroup("oldvip") != nil || mgr.GetGroup("newvip") == nil {
		t.Fatal("expected oldvip removed and newvip present")
	}
	if got := mgr.GetGroup("child"); got == nil || len(got.Parents) == 0 || got.Parents[0].Group != "newvip" {
		t.Fatalf("expected child group parent cascaded to newvip, got %+v", got)
	}
	if got := mgr.GetTrack("donors"); got == nil || got.Groups[1] != "newvip" {
		t.Fatalf("expected track donors cascaded to newvip, got %+v", got)
	}
	if got := mgr.GetUser(uZed.UUID); got.PrimaryGroup != "newvip" || got.Parents[0].Group != "newvip" {
		t.Fatalf("expected user PrimaryGroup and Parents cascaded to newvip, got %+v", got)
	}

	if _, _, err := mgr.RenameTrack("donors", "supporters", "Console"); err != nil {
		t.Fatalf("RenameTrack: %v", err)
	}
	if mgr.GetTrack("donors") != nil || mgr.GetTrack("supporters") == nil {
		t.Fatal("expected track donors renamed to supporters")
	}
	if logs := mgr.GetLogs(10); len(logs) == 0 {
		t.Fatal("expected audit logs when re-enabled")
	}

	// Reload from disk to verify persistence of cascaded rename
	reloaded, err := New(store)
	if err != nil {
		t.Fatalf("reload manager: %v", err)
	}
	defer reloaded.Close()
	if reloaded.GetGroup("newvip") == nil || reloaded.GetTrack("supporters") == nil || reloaded.GetUser(uZed.UUID).PrimaryGroup != "newvip" {
		t.Fatal("expected renamed group/track and cascaded user preserved on disk reload")
	}
}
