package manager

import (
	"os"
	"path/filepath"
	"testing"

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
	u.AddNode(model.NewNode("gate.command.server", true))
	u.AddNode(model.NewNode("gate.command.send", false))

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
