package manager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andreisugu/gate-stonyperms/model"
)

func TestBackendCatalogFeedsEditorAutocomplete(t *testing.T) {
	mgr := newWebTestManager(t)
	mgr.SetWebConfig(WebConfig{BaseURL: "http://127.0.0.1:8737", DefaultGroup: "default", LiveSave: true})

	perms, ctx := mgr.RegisterBackendCatalog("survival", []string{
		"essentialcommands.home.set",
		"jobs.use",
	}, map[string][]string{
		"world":    {"world_nether"},
		"gamemode": {"creative"},
	})
	if perms != 2 || ctx != 2 {
		t.Fatalf("catalog counts = %d/%d, want 2/2", perms, ctx)
	}

	created, err := mgr.CreateEditorSession(WebSessionCreateOptions{Actor: "Admin", TargetType: "all"})
	if err != nil {
		t.Fatalf("CreateEditorSession: %v", err)
	}
	loaded, ok := mgr.GetEditorSession(created.Token)
	if !ok {
		t.Fatal("editor session not found")
	}
	if !containsString(loaded.KnownPermissions, "essentialcommands.home.set") ||
		!containsString(loaded.KnownPermissions, "jobs.use") {
		t.Fatalf("catalog permissions missing: %v", loaded.KnownPermissions)
	}
	if !containsString(loaded.PotentialContexts["world"], "world_nether") {
		t.Fatalf("catalog world context missing: %v", loaded.PotentialContexts)
	}
	if !containsString(loaded.PotentialContexts["gamemode"], "creative") {
		t.Fatalf("catalog gamemode context missing: %v", loaded.PotentialContexts)
	}
	for _, key := range []string{"dimension-type", "gamemode"} {
		if len(loaded.PotentialContexts[key]) == 0 {
			t.Fatalf("default context %q missing: %v", key, loaded.PotentialContexts)
		}
	}
}

func TestApplyEditorPayloadReportsItemizedDiff(t *testing.T) {
	mgr := newWebTestManager(t)
	group := model.NewGroup("vip", 10)
	if err := mgr.SaveGroup(group); err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	updated := *group
	updated.Nodes = append(updated.Nodes, model.NewNode("essentials.fly", true))
	updated.Weight = 25
	summary, err := mgr.ApplyEditorPayload("Admin", EditorSavePayload{Groups: []model.Group{updated}})
	if err != nil {
		t.Fatalf("ApplyEditorPayload: %v", err)
	}
	joined := strings.Join(summary.Diff, "\n")
	if !strings.Contains(joined, "+ group vip node essentials.fly=true") {
		t.Fatalf("missing node add diff:\n%s", joined)
	}
	if !strings.Contains(joined, "~ group vip weight: 10 -> 25") {
		t.Fatalf("missing weight diff:\n%s", joined)
	}
}

func TestDeltaApplyCodeAndFile(t *testing.T) {
	mgr := newWebTestManager(t)
	payload := EditorSavePayload{Groups: []model.Group{{
		Name:   "vip",
		Weight: 20,
		Nodes:  []model.Node{model.NewNode("essentials.fly", true)},
	}}}
	full, err := EncodePortableApplyCode(payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	delta := DeltaCodePrefix + strings.TrimPrefix(full, PortableCodePrefix)
	summary, err := mgr.ApplyEditCode("Admin", delta)
	if err != nil {
		t.Fatalf("ApplyEditCode(sp2_): %v", err)
	}
	if summary.GroupsUpdated != 1 {
		t.Fatalf("groups updated = %d", summary.GroupsUpdated)
	}

	dir := t.TempDir()
	mgr.SetImportDir(dir)
	payload.Groups[0].Weight = 30
	code, _ := EncodePortableApplyCode(payload)
	if err := os.WriteFile(filepath.Join(dir, "changes.spedit"), []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	summary, err = mgr.ApplyEditFile("Admin", "changes")
	if err != nil {
		t.Fatalf("ApplyEditFile: %v", err)
	}
	if summary.GroupsUpdated != 1 {
		t.Fatalf("file groups updated = %d", summary.GroupsUpdated)
	}
}
