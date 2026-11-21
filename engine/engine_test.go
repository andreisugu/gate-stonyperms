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

func TestHigherGroupWildcardOverridesLowerGroupExactNegation(t *testing.T) {
	eng := New()

	defaultGroup := model.NewGroup("default", 0)
	defaultGroup.AddNode(model.NewNode("gate.command.send", false)) // negated send on default

	adminGroup := model.NewGroup("admin", 100)
	adminGroup.AddNode(model.NewNode("gate.*", true)) // admin wildcard

	groups := map[string]*model.Group{
		"default": defaultGroup,
		"admin":   adminGroup,
	}

	u := model.NewUser(uuid.New(), "AdminUser")
	u.PrimaryGroup = "default"
	u.AddParent(model.NewInheritance("admin"))

	// Evaluating gate.command.send for an admin user:
	// Admin group (weight 100) has gate.* = true
	// Default group (weight 0) has gate.command.send = false
	// Expected: True, because admin group has higher weight!
	res := eng.Evaluate(u, groups, "gate.command.send", nil)
	if res.Result != model.True {
		t.Fatalf("expected True from admin group wildcard, got %v (source: %s)", res.Result, res.Source)
	}
}

func TestPrimaryGroupAffixPrecedence(t *testing.T) {
	eng := New()

	def := model.NewGroup("default", 0)
	def.Prefixes = []model.PriorityMeta{{Priority: 50, Value: "&7"}}
	def.Suffixes = []model.PriorityMeta{{Priority: 50, Value: " &8[Def]"}}

	vip := model.NewGroup("vip", 100)
	vip.Prefixes = []model.PriorityMeta{{Priority: 10, Value: "&a[VIP] "}}
	vip.Suffixes = []model.PriorityMeta{{Priority: 10, Value: " &a*"}}

	staff := model.NewGroup("mod", 200)
	staff.Prefixes = []model.PriorityMeta{{Priority: 99, Value: "&9[Mod] "}}
	staff.Suffixes = []model.PriorityMeta{{Priority: 99, Value: " &9+"}}

	groups := map[string]*model.Group{
		"default": def,
		"vip":     vip,
		"mod":     staff,
	}

	u := model.NewUser(uuid.New(), "PrimaryWins")
	u.PrimaryGroup = "vip"
	u.AddParent(model.NewInheritance("mod"))
	if got := eng.GetPrefix(u, groups, nil); got != "&a[VIP] " {
		t.Fatalf("expected primary group prefix &a[VIP] , got %q", got)
	}
	if got := eng.GetSuffix(u, groups, nil); got != " &a*" {
		t.Fatalf("expected primary group suffix ' &a*', got %q", got)
	}

	uDefault := model.NewUser(uuid.New(), "DefaultPrimary")
	uDefault.PrimaryGroup = "default"
	uDefault.AddParent(model.NewInheritance("mod"))
	if got := eng.GetPrefix(uDefault, groups, nil); got != "&9[Mod] " {
		t.Fatalf("expected inherited prefix &9[Mod] , got %q", got)
	}

	u.Prefixes = []model.PriorityMeta{{Priority: 1, Value: "&d[Custom] "}}
	u.Suffixes = []model.PriorityMeta{{Priority: 1, Value: " &d!"}}
	if got := eng.GetPrefix(u, groups, nil); got != "&d[Custom] " {
		t.Fatalf("expected direct user prefix &d[Custom] , got %q", got)
	}
	if got := eng.GetSuffix(u, groups, nil); got != " &d!" {
		t.Fatalf("expected direct user suffix ' &d!', got %q", got)
	}
}

func TestStackingAndContextualMeta(t *testing.T) {
	eng := New()
	eng.SetStackConfig(StackConfig{
		PrefixEnabled:     true,
		PrefixStartSpacer: "[",
		PrefixMidSpacer:   " ",
		PrefixEndSpacer:   "] ",
		SuffixEnabled:     true,
		SuffixMidSpacer:   " ",
		Deduplicate:       true,
	})

	def := model.NewGroup("default", 0)
	def.Prefixes = []model.PriorityMeta{{Priority: 10, Value: "Member"}}
	def.SetMeta("homes", "1")

	vip := model.NewGroup("vip", 100)
	vip.AddParent(model.NewInheritance("default"))
	vip.Prefixes = []model.PriorityMeta{{Priority: 100, Value: "VIP"}}
	_, _ = vip.SetMetaEntry(model.MetaEntry{
		Key:      "homes",
		Value:    "5",
		Priority: 50,
		Contexts: map[string]string{"server": "survival"},
	}, model.TempModifierReplace, time.Now())

	staff := model.NewGroup("mod", 200)
	staff.Prefixes = []model.PriorityMeta{
		{Priority: 200, Value: "Mod"},
		{Priority: 50, Value: "VIP"}, // duplicate value should be deduplicated
	}
	staff.Suffixes = []model.PriorityMeta{{Priority: 200, Value: "[Staff]"}}

	groups := map[string]*model.Group{
		"default": def,
		"vip":     vip,
		"mod":     staff,
	}

	u := model.NewUser(uuid.New(), "StackedUser")
	u.PrimaryGroup = "mod"
	u.AddParent(model.NewInheritance("vip"))

	if got := eng.GetPrefix(u, groups, nil); got != "[Mod VIP Member] " {
		t.Fatalf("stacked prefix = %q, want %q", got, "[Mod VIP Member] ")
	}
	if got := eng.GetSuffix(u, groups, nil); got != "[Staff]" {
		t.Fatalf("stacked suffix = %q, want [Staff]", got)
	}

	// Context meta resolves server=survival override vs global fallback
	if v, ok := eng.GetMetaWithContext(u, groups, "homes", map[string]string{"server": "survival"}); !ok || v != "5" {
		t.Fatalf("survival homes = (%q, %v), want (5, true)", v, ok)
	}
	if v, ok := eng.GetMetaWithContext(u, groups, "homes", map[string]string{"server": "lobby"}); !ok || v != "1" {
		t.Fatalf("lobby homes = (%q, %v), want (1, true)", v, ok)
	}
}

func TestContextSpecificityShorthandRegexAndPrimaryGroupCalc(t *testing.T) {
	eng := New()

	def := model.NewGroup("default", 0)
	vip := model.NewGroup("vip", 30)
	vip.Prefixes = []model.PriorityMeta{{Priority: 30, Value: "[VIP] "}}
	mod := model.NewGroup("mod", 80)
	mod.Parents = []model.Inheritance{model.NewInheritance("vip")}
	mod.Prefixes = []model.PriorityMeta{{Priority: 80, Value: "[Mod] "}}
	groups := map[string]*model.Group{
		"default": def,
		"vip":     vip,
		"mod":     mod,
	}

	u := model.NewUser(uuid.New(), "Parity2User")
	u.PrimaryGroup = "default"
	u.AddNode(model.Node{Key: "essentials.fly", Value: false}) // global deny
	u.AddNode(model.Node{
		Key:      "essentials.fly",
		Value:    true,
		Contexts: map[string]string{"server": "creative"},
	}) // more specific context allow should beat global exact
	u.AddNode(model.Node{Key: "essentials.{home,warp}", Value: true})
	u.AddNode(model.Node{Key: "r=essentials\\.kit\\.[a-z]+", Value: true})

	if res := eng.Evaluate(u, groups, "essentials.fly", map[string]string{"server": "lobby"}); res.Result != model.False {
		t.Fatalf("lobby essentials.fly = %v, want false", res.Result)
	}
	if res := eng.Evaluate(u, groups, "essentials.fly", map[string]string{"server": "creative"}); res.Result != model.True {
		t.Fatalf("creative essentials.fly = %v, want true (context override)", res.Result)
	}
	if res := eng.Evaluate(u, groups, "essentials.warp", nil); res.Result != model.True {
		t.Fatalf("shorthand essentials.warp = %v, want true", res.Result)
	}
	if res := eng.Evaluate(u, groups, "essentials.kit.starter", nil); res.Result != model.True {
		t.Fatalf("regex essentials.kit.starter = %v, want true", res.Result)
	}

	// Primary group calculation modes
	u.AddParent(model.Inheritance{
		Group:    "vip",
		Contexts: map[string]string{"server": "lobby"},
	})
	u.AddParent(model.Inheritance{
		Group:    "mod",
		Contexts: map[string]string{"server": "survival"},
	})

	if pg := eng.ResolvePrimaryGroup(u, groups, map[string]string{"server": "survival"}, "default"); pg != "default" {
		t.Fatalf("stored mode primary = %q, want default", pg)
	}
	eng.SetPrimaryGroupCalculation("parents-by-weight")
	if pg := eng.ResolvePrimaryGroup(u, groups, map[string]string{"server": "lobby"}, "default"); pg != "vip" {
		t.Fatalf("parents-by-weight lobby primary = %q, want vip", pg)
	}
	if got := eng.GetPrefix(u, groups, map[string]string{"server": "survival"}); got != "[Mod] " {
		t.Fatalf("survival prefix with parents-by-weight = %q, want [Mod] ", got)
	}
	eng.SetPrimaryGroupCalculation("all-parents-by-weight")
	if pg := eng.ResolvePrimaryGroup(u, groups, map[string]string{"server": "survival"}, "default"); pg != "mod" {
		t.Fatalf("all-parents-by-weight survival primary = %q, want mod", pg)
	}
}

func TestAffixContextSpecificityTieBreaking(t *testing.T) {
	eng := New()
	g := model.NewGroup("vip", 10)
	// Insert global affix before contextual affix at equal priority 100.
	g.AddPrefix(model.PriorityMeta{Priority: 100, Value: "[Global] "})
	g.AddPrefix(model.PriorityMeta{Priority: 100, Value: "[Lobby] ", Contexts: map[string]string{"server": "lobby"}})
	g.AddSuffix(model.PriorityMeta{Priority: 50, Value: " [G]"})
	g.AddSuffix(model.PriorityMeta{Priority: 50, Value: " [L]", Contexts: map[string]string{"server": "lobby"}})

	groups := map[string]*model.Group{"vip": g}
	u := model.NewUser(uuid.New(), "AffixTieUser")
	u.PrimaryGroup = "vip"

	if got := eng.GetPrefix(u, groups, map[string]string{"server": "lobby"}); got != "[Lobby] " {
		t.Fatalf("expected contextual prefix [Lobby]  to beat global at equal priority, got %q", got)
	}
	if got := eng.GetSuffix(u, groups, map[string]string{"server": "lobby"}); got != " [L]" {
		t.Fatalf("expected contextual suffix ' [L]' to beat global at equal priority, got %q", got)
	}

	eng.SetStackConfig(StackConfig{PrefixEnabled: true, SuffixEnabled: true, Deduplicate: true})
	if got := eng.GetPrefix(u, groups, map[string]string{"server": "lobby"}); got != "[Lobby] [Global] " {
		t.Fatalf("stacked prefix order = %q, want '[Lobby] [Global] '", got)
	}
}
