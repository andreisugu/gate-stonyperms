package command

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/andreisugu/gate-stonyperms/engine"
	"github.com/andreisugu/gate-stonyperms/model"
)

func TestTempModifiersUnsettempAndPermissionInfo(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	src := &dummySource{}
	id := uuid.New()
	_ = mgr.GetOrCreateUser(id, "Alex")

	// Permanent permission should not be removed by unsettemp.
	_ = Dispatch(mgr, src, []string{"user", "Alex", "permission", "set", "perm.perm", "true"})
	_ = Dispatch(mgr, src, []string{"user", "Alex", "permission", "unsettemp", "perm.perm"})
	u := mgr.GetUser(id)
	if !mgr.HasPermission(u, "perm.perm", nil) {
		t.Fatal("unsettemp must not delete permanent permission")
	}

	// Temporary accumulate, deny, replace.
	if err := Dispatch(mgr, src, []string{"user", "Alex", "permission", "settemp", "temp.node", "true", "1h", "accumulate"}); err != nil {
		t.Fatalf("settemp initial: %v", err)
	}
	u = mgr.GetUser(id)
	firstExp := findNodeExpiry(u.Nodes, "temp.node")

	if err := Dispatch(mgr, src, []string{"user", "Alex", "permission", "settemp", "temp.node", "true", "30m", "accumulate"}); err != nil {
		t.Fatalf("settemp accumulate: %v", err)
	}
	u = mgr.GetUser(id)
	accExp := findNodeExpiry(u.Nodes, "temp.node")
	if accExp < firstExp+int64((29*time.Minute).Seconds()) {
		t.Fatalf("expected accumulate to add ~30m, before=%d after=%d", firstExp, accExp)
	}

	src.messages = nil
	_ = Dispatch(mgr, src, []string{"user", "Alex", "permission", "settemp", "temp.node", "true", "1h", "deny"})
	if len(src.messages) == 0 || !strings.Contains(strings.ToLower(src.messages[0]), "cannot set temporary permission") {
		t.Fatalf("expected deny rejection, got %v", src.messages)
	}

	src.messages = nil
	_ = Dispatch(mgr, src, []string{"user", "Alex", "permission", "info"})
	joined := strings.Join(src.messages, "\n")
	if !strings.Contains(joined, "perm.perm") || !strings.Contains(joined, "temp.node") {
		t.Fatalf("expected permission info to list perm.perm & temp.node, got:\n%s", joined)
	}

	_ = Dispatch(mgr, src, []string{"user", "Alex", "permission", "unsettemp", "temp.node"})
	u = mgr.GetUser(id)
	if mgr.HasPermission(u, "temp.node", nil) {
		t.Fatal("expected unsettemp to remove temporary permission")
	}
}

func findNodeExpiry(nodes []model.Node, key string) int64 {
	for _, n := range nodes {
		if n.Key == key {
			return n.Expiry
		}
	}
	return 0
}

func TestTrackParentsSwitchPrimaryGroupAndMetaAffixes(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	src := &dummySource{}
	_, _ = mgr.CreateGroup("vip", 20, "")
	_, _ = mgr.CreateGroup("mvp", 30, "")
	tr, _ := mgr.CreateTrack("donors")
	_ = tr.Append("default")
	_ = tr.Append("vip")
	_ = tr.Append("mvp")
	_ = mgr.SaveTrack(tr)

	id := uuid.New()
	_ = mgr.GetOrCreateUser(id, "Steve")

	// switchprimarygroup auto-inherits group
	if err := Dispatch(mgr, src, []string{"user", "Steve", "parent", "switchprimarygroup", "vip"}); err != nil {
		t.Fatalf("switchprimarygroup: %v", err)
	}
	u := mgr.GetUser(id)
	if u.PrimaryGroup != "vip" {
		t.Fatalf("expected primary group vip, got %s", u.PrimaryGroup)
	}
	foundParent := false
	for _, p := range u.Parents {
		if p.Group == "vip" {
			foundParent = true
		}
	}
	if !foundParent {
		t.Fatal("expected switchprimarygroup to ensure vip is inherited as parent")
	}

	// settrack replaces prior group on track
	if err := Dispatch(mgr, src, []string{"user", "Steve", "parent", "settrack", "donors", "mvp"}); err != nil {
		t.Fatalf("settrack: %v", err)
	}
	u = mgr.GetUser(id)
	hasVIP, hasMVP := false, false
	for _, p := range u.Parents {
		if p.Group == "vip" {
			hasVIP = true
		}
		if p.Group == "mvp" {
			hasMVP = true
		}
	}
	if hasVIP || !hasMVP {
		t.Fatalf("expected settrack to swap vip -> mvp, got parents %+v", u.Parents)
	}

	// addtemp & removetemp parent
	_ = Dispatch(mgr, src, []string{"group", "mvp", "parent", "addtemp", "vip", "1h", "accumulate"})
	_ = Dispatch(mgr, src, []string{"group", "mvp", "parent", "removetemp", "vip"})
	if grp := mgr.GetGroup("mvp"); len(grp.Parents) != 0 {
		t.Fatalf("expected removetemp to clear temp parent on group, got %+v", grp.Parents)
	}

	// cleartrack
	_ = Dispatch(mgr, src, []string{"user", "Steve", "parent", "cleartrack", "donors"})

	// Contextual & temp meta + addprefix vs setprefix + stacked output
	_ = Dispatch(mgr, src, []string{"user", "Steve", "meta", "set", "clan", "Builders", "server=lobby"})
	_ = Dispatch(mgr, src, []string{"user", "Steve", "meta", "settemp", "boost", "2x", "1h", "accumulate"})
	_ = Dispatch(mgr, src, []string{"user", "Steve", "meta", "addprefix", "10", "&7[A] "})
	_ = Dispatch(mgr, src, []string{"user", "Steve", "meta", "addprefix", "20", "&e[B] "})
	u = mgr.GetUser(id)
	if len(u.Prefixes) != 2 {
		t.Fatalf("expected addprefix to preserve both priorities, got %+v", u.Prefixes)
	}
	if val, ok := mgr.Engine().GetMetaWithContext(u, nil, "clan", map[string]string{"server": "lobby"}); !ok || val != "Builders" {
		t.Fatalf("expected contextual meta Builders on server=lobby, got %q (%v)", val, ok)
	}

	mgr.Engine().SetStackConfig(engine.StackConfig{
		PrefixEnabled:   true,
		PrefixMidSpacer: "",
		Deduplicate:     true,
	})
	if stacked := mgr.GetPrefix(u, nil); stacked != "&e[B] &7[A] &7[Member] " {
		t.Fatalf("expected stacked prefix '&e[B] &7[A] &7[Member] ', got %q", stacked)
	}

	// setprefix replaces existing entries in matching context
	_ = Dispatch(mgr, src, []string{"user", "Steve", "meta", "setprefix", "50", "&c[Only] "})
	u = mgr.GetUser(id)
	if len(u.Prefixes) != 1 || u.Prefixes[0].Value != "&c[Only] " {
		t.Fatalf("expected setprefix to replace same-context prefixes, got %+v", u.Prefixes)
	}

	// removetempprefix skips permanent prefixes
	_ = Dispatch(mgr, src, []string{"user", "Steve", "meta", "removetempprefix", "50"})
	u = mgr.GetUser(id)
	if len(u.Prefixes) != 1 {
		t.Fatal("removetempprefix should not delete permanent prefix")
	}
	_ = Dispatch(mgr, src, []string{"user", "Steve", "meta", "addtempsuffix", "5", " &b★", "1h"})
	_ = Dispatch(mgr, src, []string{"user", "Steve", "meta", "removetempsuffix", "5"})
	u = mgr.GetUser(id)
	if len(u.Suffixes) != 0 {
		t.Fatalf("expected removetempsuffix to delete temporary suffix, got %+v", u.Suffixes)
	}
}

func TestSearchBulkUpdateConfirmAndExportImportCommands(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	src := &dummySource{}
	_, _ = mgr.CreateGroup("vip", 20, "")
	_ = Dispatch(mgr, src, []string{"group", "vip", "permission", "set", "essentials.fly", "true"})
	_ = Dispatch(mgr, src, []string{"user", "Bob", "permission", "set", "essentials.fly", "true"})
	_ = Dispatch(mgr, src, []string{"user", "Bob", "parent", "add", "vip"})

	// /sp search
	src.messages = nil
	if err := Dispatch(mgr, src, []string{"search", "essentials.fly"}); err != nil {
		t.Fatalf("search: %v", err)
	}
	out := strings.Join(src.messages, "\n")
	if !strings.Contains(out, "vip") || !strings.Contains(out, "Bob") {
		t.Fatalf("expected search results to mention vip & Bob, got:\n%s", out)
	}

	// /sp bulkupdate preview + confirm <code>
	src.messages = nil
	if err := Dispatch(mgr, src, []string{"bulkupdate", "all", "update", "permission", "essentials.fly.v2", "permission == essentials.fly"}); err != nil {
		t.Fatalf("bulkupdate preview: %v", err)
	}
	joined := strings.Join(src.messages, "\n")
	idx := strings.Index(joined, "/sp bulkupdate confirm ")
	if idx < 0 {
		t.Fatalf("expected confirm code prompt, got:\n%s", joined)
	}
	codePart := strings.Fields(joined[idx+len("/sp bulkupdate confirm "):])[0]
	code := strings.TrimRight(codePart, "]")

	// Bad code fails gracefully
	src.messages = nil
	_ = Dispatch(mgr, src, []string{"bulkupdate", "confirm", "badcode"})
	if !strings.Contains(strings.ToLower(src.messages[0]), "invalid or expired") {
		t.Fatalf("expected invalid code message, got %v", src.messages)
	}

	// Valid confirm code applies update
	if err := Dispatch(mgr, src, []string{"bulkupdate", "confirm", code}); err != nil {
		t.Fatalf("bulkupdate confirm: %v", err)
	}
	bob := mgr.GetUserByName("Bob")
	if !mgr.HasPermission(bob, "essentials.fly.v2", nil) {
		t.Fatal("expected Bob to now have essentials.fly.v2")
	}

	// /sp export -> wipe -> /sp import --replace -> /sp networksync
	if err := Dispatch(mgr, src, []string{"export", "snapshot.json.gz"}); err != nil {
		t.Fatalf("export: %v", err)
	}
	_ = Dispatch(mgr, src, []string{"bulkupdate", "all", "delete", "permission", "==", "essentials.fly.v2", "--confirm"})
	if mgr.HasPermission(mgr.GetUserByName("Bob"), "essentials.fly.v2", nil) {
		t.Fatal("expected --confirm bulk delete to remove essentials.fly.v2")
	}
	if err := Dispatch(mgr, src, []string{"import", "snapshot.json.gz", "--replace"}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if !mgr.HasPermission(mgr.GetUserByName("Bob"), "essentials.fly.v2", nil) {
		t.Fatal("expected import --replace to restore essentials.fly.v2")
	}
	if err := Dispatch(mgr, src, []string{"networksync"}); err != nil {
		t.Fatalf("networksync: %v", err)
	}
}

func TestTrackCloneAndEditor(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()
	_, _ = mgr.CreateGroup("vip", 10, "")
	tr, _ := mgr.CreateTrack("staff")
	_ = tr.Append("default")
	_ = tr.Append("vip")
	_ = mgr.SaveTrack(tr)

	src := &dummySource{}
	if err := Dispatch(mgr, src, []string{"track", "staff", "clone", "staff_copy"}); err != nil {
		t.Fatalf("track clone: %v", err)
	}
	cloned := mgr.GetTrack("staff_copy")
	if cloned == nil || len(cloned.Groups) != 2 || cloned.Groups[1] != "vip" {
		t.Fatalf("expected cloned track with 2 groups, got %+v", cloned)
	}
	if err := Dispatch(mgr, src, []string{"track", "staff", "editor"}); err != nil {
		t.Fatalf("track editor: %v", err)
	}
}
