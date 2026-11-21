package manager

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/storage"
)

func TestManagerWithSQLiteBackend(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewSQLStorage(storage.SQLConfig{
		Type:        "sqlite",
		File:        filepath.Join(dir, "perms.db"),
		TablePrefix: "sp_",
	})
	if err != nil {
		t.Fatalf("NewSQLStorage: %v", err)
	}
	mgr, err := New(store)
	if err != nil {
		t.Fatalf("manager.New: %v", err)
	}
	mgr.SetExportDir(filepath.Join(dir, "exports"))

	if _, _, err := mgr.CreateGroupWithActor("builder", 20, "&6Builder", "admin"); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if _, _, err := mgr.CreateTrackWithActor("build", []string{"default", "builder"}, "admin"); err != nil {
		t.Fatalf("CreateTrack: %v", err)
	}
	uid := uuid.New()
	if _, _, _, err := mgr.ResolveOrCreateUser(uid, "Mason", "builder", "admin"); err != nil {
		t.Fatalf("ResolveOrCreateUser: %v", err)
	}
	if _, _, err := mgr.RenameGroup("builder", "architect", "admin"); err != nil {
		t.Fatalf("RenameGroup: %v", err)
	}
	if _, _, err := mgr.RenameTrack("build", "creative_path", "admin"); err != nil {
		t.Fatalf("RenameTrack: %v", err)
	}
	if _, _, err := mgr.ExportToFile("backup.json", "admin"); err != nil {
		t.Fatalf("ExportToFile: %v", err)
	}
	mgr.Close()

	reopenStore, err := storage.NewSQLStorage(storage.SQLConfig{
		Type:        "sqlite",
		File:        filepath.Join(dir, "perms.db"),
		StorageDir:  dir,
		TablePrefix: "sp_",
	})
	if err != nil {
		t.Fatalf("reopen NewSQLStorage: %v", err)
	}
	reopenMgr, err := New(reopenStore)
	if err != nil {
		t.Fatalf("reopen manager.New: %v", err)
	}
	defer reopenMgr.Close()

	u, ok, _ := reopenMgr.UserSnapshot(uid)
	if !ok || u.PrimaryGroup != "architect" {
		t.Fatalf("expected user primaryGroup=architect after rename & reopen, got ok=%v u=%+v", ok, u)
	}
	tr, ok, _ := reopenMgr.TrackSnapshot("creative_path")
	if !ok || len(tr.Groups) != 2 || tr.Groups[1] != "architect" {
		t.Fatalf("expected renamed track with architect, got ok=%v tr=%+v", ok, tr)
	}
	if _, err := reopenMgr.ImportFromFile("backup.json", true, "admin"); err != nil {
		t.Fatalf("ImportFromFile: %v", err)
	}
	if u2 := reopenMgr.GetUser(uid); u2 == nil || u2.PrimaryGroup != "architect" {
		t.Fatalf("expected architect after import, got %+v", u2)
	}
}
