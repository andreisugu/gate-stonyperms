package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/model"
)

func TestJSONStorageOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "stonyperms_storage_test_*")
	if err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	js, err := NewJSONStorage(filepath.Join(tmpDir, "stonyperms"))
	if err != nil {
		t.Fatalf("failed to init json storage: %v", err)
	}

	bundle, err := js.LoadAll()
	if err != nil {
		t.Fatalf("failed to load all: %v", err)
	}

	// Verify defaults
	if _, ok := bundle.Groups["default"]; !ok {
		t.Fatalf("expected default group to be created")
	}
	if _, ok := bundle.Groups["admin"]; !ok {
		t.Fatalf("expected admin group to be created")
	}
	if _, ok := bundle.Tracks["ranks"]; !ok {
		t.Fatalf("expected ranks track to be created")
	}

	// Test user save and load
	u := model.NewUser(uuid.New(), "Alex")
	u.AddNode(model.NewNode("test.node", true))
	if err := js.SaveUser(u); err != nil {
		t.Fatalf("failed to save user: %v", err)
	}

	reloaded, err := js.LoadAll()
	if err != nil {
		t.Fatalf("failed to reload: %v", err)
	}
	loadedUser, ok := reloaded.Users[u.UUID]
	if !ok {
		t.Fatalf("expected user to be loaded")
	}
	if loadedUser.Username != "Alex" || len(loadedUser.Nodes) != 1 {
		t.Fatalf("mismatched user data: %+v", loadedUser)
	}

	// Test group creation and delete
	modGroup := model.NewGroup("moderator", 50)
	if err := js.SaveGroup(modGroup); err != nil {
		t.Fatalf("failed to save group: %v", err)
	}

	reloaded, _ = js.LoadAll()
	if _, ok := reloaded.Groups["moderator"]; !ok {
		t.Fatalf("expected moderator group in reloaded")
	}

	if err := js.DeleteGroup("moderator"); err != nil {
		t.Fatalf("failed to delete group: %v", err)
	}

	reloaded, _ = js.LoadAll()
	if _, ok := reloaded.Groups["moderator"]; ok {
		t.Fatalf("expected moderator group to be deleted")
	}
}
