package command

import (
	"strings"
	"testing"

	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/model"
	"github.com/andreisugu/gate-stonyperms/storage"
)

func newCommandTestManager(t *testing.T) *manager.Manager {
	t.Helper()
	store, err := storage.NewJSONStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONStorage: %v", err)
	}
	mgr, err := manager.New(store)
	if err != nil {
		t.Fatalf("manager.New: %v", err)
	}
	t.Cleanup(mgr.Close)
	mgr.SetWebConfig(manager.WebConfig{
		BaseURL:  "http://127.0.0.1:8737",
		LiveSave: true,
	})
	return mgr
}

func TestWebCommandsDispatchAndPermissions(t *testing.T) {
	mgr := newCommandTestManager(t)
	if _, _, err := mgr.CreateGroupWithActor("vip", 30, "&a[VIP]", "Console"); err != nil {
		t.Fatalf("CreateGroupWithActor: %v", err)
	}
	src := &dummySource{}

	if err := Dispatch(mgr, src, []string{"editor"}); err != nil {
		t.Fatalf("Dispatch editor: %v", err)
	}
	joined := strings.Join(src.messages, "\n")
	if !strings.Contains(joined, "http://127.0.0.1:8737/editor?session=") {
		t.Fatalf("expected editor link in messages, got: %s", joined)
	}

	src.messages = nil
	if err := Dispatch(mgr, src, []string{"group", "vip", "editor"}); err != nil {
		t.Fatalf("Dispatch group editor: %v", err)
	}
	joined = strings.Join(src.messages, "\n")
	if !strings.Contains(joined, "target=group%3Avip") {
		t.Fatalf("expected group target query in link, got: %s", joined)
	}

	src.messages = nil
	if err := Dispatch(mgr, src, []string{"user", "Larryy_", "editor"}); err != nil {
		t.Fatalf("Dispatch user editor: %v", err)
	}
	joined = strings.Join(src.messages, "\n")
	if !strings.Contains(joined, "target=user%3A") {
		t.Fatalf("expected user target query in link, got: %s", joined)
	}

	src.messages = nil
	code, err := manager.EncodePortableApplyCode(manager.EditorSavePayload{
		Groups: []model.Group{*model.NewGroup("helper", 70)},
	})
	if err != nil {
		t.Fatalf("EncodePortableApplyCode: %v", err)
	}
	if err := Dispatch(mgr, src, []string{"applyedits", code}); err != nil {
		t.Fatalf("Dispatch applyedits: %v", err)
	}
	if mgr.GetGroup("helper") == nil {
		t.Fatal("expected helper group created by /sp applyedits")
	}

	src.messages = nil
	if err := Dispatch(mgr, src, []string{"verbose", "record", "stonyperms.*"}); err != nil {
		t.Fatalf("Dispatch verbose record: %v", err)
	}
	_ = mgr.PermissionValue(mgr.GetUserByName("Larryy_"), "stonyperms.editor", nil)
	src.messages = nil
	if err := Dispatch(mgr, src, []string{"verbose", "paste"}); err != nil {
		t.Fatalf("Dispatch verbose paste: %v", err)
	}
	joined = strings.Join(src.messages, "\n")
	if !strings.Contains(joined, "http://127.0.0.1:8737/verbose?session=") {
		t.Fatalf("expected verbose link in messages, got: %s", joined)
	}

	src.messages = nil
	if err := Dispatch(mgr, src, []string{"tree", "gate"}); err != nil {
		t.Fatalf("Dispatch tree: %v", err)
	}
	joined = strings.Join(src.messages, "\n")
	if !strings.Contains(joined, "http://127.0.0.1:8737/tree?session=") {
		t.Fatalf("expected tree link in messages, got: %s", joined)
	}

	limited := &limitedSource{perms: map[string]bool{"stonyperms.use": true}}
	if err := Dispatch(mgr, limited, []string{"editor"}); err != nil {
		t.Fatalf("Dispatch denied: %v", err)
	}
	if len(limited.messages) == 0 || !strings.Contains(strings.ToLower(limited.messages[0]), "permission") {
		t.Fatalf("expected permission denial message, got: %v", limited.messages)
	}
}
