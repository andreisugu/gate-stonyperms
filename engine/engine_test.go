package engine

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/model"
)

func TestPermissionEngineDirectMatches(t *testing.T) {
	eng := New()
	u := model.NewUser(uuid.New(), "Steve")
	u.AddNode(model.NewNode("gate.command.server", true))
	u.AddNode(model.NewNode("gate.command.send", false)) // negated

	groups := make(map[string]*model.Group)

	// Test exact true
	res := eng.PermissionValue(u, groups, "gate.command.server", nil)
	if res != model.True {
		t.Fatalf("expected True, got %v", res)
	}

	// Test exact false (negation)
	res = eng.PermissionValue(u, groups, "gate.command.send", nil)
	if res != model.False {
		t.Fatalf("expected False, got %v", res)
	}

	// Test undefined
	res = eng.PermissionValue(u, groups, "gate.command.unknown", nil)
	if res != model.Undefined {
		t.Fatalf("expected Undefined, got %v", res)
	}
}

func TestPermissionEngineWildcards(t *testing.T) {
	eng := New()
	u := model.NewUser(uuid.New(), "Admin")
	u.AddNode(model.NewNode("gate.command.*", true))
	u.AddNode(model.NewNode("gate.command.restricted", false)) // specific override

	groups := make(map[string]*model.Group)

	// Wildcard match
	if res := eng.PermissionValue(u, groups, "gate.command.server", nil); res != model.True {
		t.Fatalf("expected True for wildcard, got %v", res)
	}

	// Specific override should win over wildcard
	if res := eng.PermissionValue(u, groups, "gate.command.restricted", nil); res != model.False {
		t.Fatalf("expected False for specific override, got %v", res)
	}

	// Unrelated permission
	if res := eng.PermissionValue(u, groups, "stonyperms.admin", nil); res != model.Undefined {
		t.Fatalf("expected Undefined, got %v", res)
	}
}

func TestPermissionEngineGroupInheritanceAndWeight(t *testing.T) {
	eng := New()

	defaultGroup := model.NewGroup("default", 0)
	defaultGroup.AddNode(model.NewNode("gate.command.server", true))
	defaultGroup.AddNode(model.NewNode("gate.command.glist", true))
	defaultGroup.AddPrefix(model.PriorityMeta{Priority: 10, Value: "&7[Member] "})

	vipGroup := model.NewGroup("vip", 50)
	vipGroup.AddParent(model.NewInheritance("default"))
	vipGroup.AddNode(model.NewNode("gate.command.server", true))
	vipGroup.AddPrefix(model.PriorityMeta{Priority: 50, Value: "&a[VIP] "})

	adminGroup := model.NewGroup("admin", 100)
	adminGroup.AddParent(model.NewInheritance("vip"))
	adminGroup.AddNode(model.NewNode("*", true))
	adminGroup.AddPrefix(model.PriorityMeta{Priority: 100, Value: "&c[Admin] "})

	groups := map[string]*model.Group{
		"default": defaultGroup,
		"vip":     vipGroup,
		"admin":   adminGroup,
	}

	// User in admin group
	u := model.NewUser(uuid.New(), "Owner")
	u.PrimaryGroup = "admin"

	if res := eng.PermissionValue(u, groups, "anything.really", nil); res != model.True {
		t.Fatalf("expected True from admin group wildcard, got %v", res)
	}

	// Prefix check
	prefix := eng.GetPrefix(u, groups, nil)
	if prefix != "&c[Admin] " {
		t.Fatalf("expected admin prefix, got %q", prefix)
	}
}

func TestPermissionEngineContexts(t *testing.T) {
	eng := New()
	u := model.NewUser(uuid.New(), "Player")

	nodeLobby := model.NewNode("gate.chat.color", true)
	nodeLobby.Contexts["server"] = "lobby"
	u.AddNode(nodeLobby)

	groups := make(map[string]*model.Group)

	// Context matches
	res := eng.PermissionValue(u, groups, "gate.chat.color", map[string]string{"server": "lobby"})
	if res != model.True {
		t.Fatalf("expected True in lobby context, got %v", res)
	}

	// Context does not match
	res = eng.PermissionValue(u, groups, "gate.chat.color", map[string]string{"server": "survival"})
	if res != model.Undefined {
		t.Fatalf("expected Undefined in survival context, got %v", res)
	}
}

func TestPermissionEngineExpiration(t *testing.T) {
	eng := New()
	u := model.NewUser(uuid.New(), "TempUser")

	// Expired node
	expiredNode := model.Node{
		Key:    "gate.vip.fly",
		Value:  true,
		Expiry: time.Now().Add(-1 * time.Hour).Unix(),
	}
	u.AddNode(expiredNode)

	// Active temp node
	activeNode := model.NewTempNode("gate.temp.command", true, 1*time.Hour)
	u.AddNode(activeNode)

	groups := make(map[string]*model.Group)

	if res := eng.PermissionValue(u, groups, "gate.vip.fly", nil); res != model.Undefined {
		t.Fatalf("expected Undefined for expired node, got %v", res)
	}

	if res := eng.PermissionValue(u, groups, "gate.temp.command", nil); res != model.True {
		t.Fatalf("expected True for active temp node, got %v", res)
	}
}
