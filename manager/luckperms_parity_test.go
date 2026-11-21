package manager

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/model"
	"github.com/andreisugu/gate-stonyperms/storage"
)

func TestSearchBulkUpdateExportImportAndTracks(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.NewJSONStorage(filepath.Join(tmpDir, "sp"))
	if err != nil {
		t.Fatalf("NewJSONStorage: %v", err)
	}
	mgr, err := New(store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer mgr.Close()

	_, _ = mgr.CreateGroup("vip", 100, "&a[VIP]")
	_, _ = mgr.CreateGroup("mvp", 200, "&b[MVP]")
	tr, _ := mgr.CreateTrack("donors")
	trCopy := mgr.GetTrackCopy(tr.Name)
	trCopy.Append("default")
	trCopy.Append("vip")
	trCopy.Append("mvp")
	_ = mgr.SaveTrack(trCopy)

	uid := uuid.New()
	_ = mgr.GetOrCreateUser(uid, "ParityUser")
	if _, err := mgr.SetUserTrackGroup(uid, "donors", "vip", nil, "Console"); err != nil {
		t.Fatalf("SetUserTrackGroup: %v", err)
	}

	// Search by permission and group.<name>
	u := mgr.GetUserCopy(uid)
	n := model.NewNode("essentials.home", true)
	n.Contexts = map[string]string{"server": "survival"}
	u.AddNode(n)
	_ = mgr.SaveUser(u)

	searchPerm := mgr.Search("essentials.home")
	if len(searchPerm.UserHits) != 1 || searchPerm.UserHits[0].Target != "ParityUser" {
		t.Fatalf("unexpected permission search result: %+v", searchPerm)
	}
	searchGroup := mgr.Search("group.vip")
	if len(searchGroup.UserHits) == 0 {
		t.Fatalf("expected user hit for group.vip")
	}

	// BulkUpdate permission rename
	sum, err := mgr.BulkUpdate(BulkUpdateOptions{
		Scope:  "users",
		Action: "update",
		Field:  "permission",
		Value:  "essential_commands.home",
		Constraints: []BulkConstraint{
			{Field: "permission", Operator: "==", Pattern: "essentials.home"},
			{Field: "server", Operator: "==", Pattern: "survival"},
		},
	}, "Console")
	if err != nil {
		t.Fatalf("BulkUpdate: %v", err)
	}
	if sum.UsersChanged != 1 || sum.NodesAffected != 1 {
		t.Fatalf("unexpected bulk summary: %+v", sum)
	}
	if got := mgr.Search("essential_commands.home"); len(got.UserHits) != 1 {
		t.Fatalf("expected updated node in search, got %+v", got)
	}

	// Export and Import into a clean second manager
	if _, _, err := mgr.ExportToFile("backup.json.gz", "Console"); err != nil {
		t.Fatalf("ExportToFile: %v", err)
	}

	secondDir := t.TempDir()
	secondStore, _ := storage.NewJSONStorage(filepath.Join(secondDir, "sp2"))
	secondMgr, err := New(secondStore)
	if err != nil {
		t.Fatalf("New secondMgr: %v", err)
	}
	defer secondMgr.Close()
	secondMgr.SetExportDir(mgr.ExportDir())

	imp, err := secondMgr.ImportFromFile("backup", true, "Console")
	if err != nil {
		t.Fatalf("ImportFromFile: %v", err)
	}
	if imp.Users != 1 || secondMgr.GetUserByName("ParityUser") == nil {
		t.Fatalf("expected ParityUser imported into secondMgr, got %+v", imp)
	}

	// ClearUserTrack removes track membership & resets PrimaryGroup
	if _, cleared, err := mgr.ClearUserTrack(uid, "donors", nil, "Console"); err != nil || !cleared {
		t.Fatalf("ClearUserTrack failed: cleared=%v err=%v", cleared, err)
	}
	if got := mgr.GetUser(uid); got.PrimaryGroup != "default" {
		t.Fatalf("expected primary group reset to default, got %q", got.PrimaryGroup)
	}
}
