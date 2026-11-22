package stonyperms

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigMigratesOldFileAndPreservesValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stonyperms.toml")
	old := `# old config
default_group = "vip"
storage_type = "json"
storage_dir = "data"
api_enabled = true
api_listen = "127.0.0.1:8737"
api_editor_public_url = "http://restedfreevm2nd:8737"
`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ConfigVersion != CurrentConfigVersion {
		t.Fatalf("config version = %d, want %d", cfg.ConfigVersion, CurrentConfigVersion)
	}
	if cfg.DefaultGroup != "vip" || cfg.StorageDir != "data" {
		t.Fatalf("existing values lost: default=%q dir=%q", cfg.DefaultGroup, cfg.StorageDir)
	}
	if cfg.PrimaryGroupCalculation != "stored" {
		t.Fatalf("primary group calculation = %q", cfg.PrimaryGroupCalculation)
	}
	if cfg.APIEditorPublicURL != "http://restedfreevm2nd:8737" {
		t.Fatalf("editor URL = %q", cfg.APIEditorPublicURL)
	}
	if cfg.MigrationBackup == "" {
		t.Fatal("expected a backup path")
	}
	if _, err := os.Stat(cfg.MigrationBackup); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	upgraded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"config_version = 4", "primary_group_calculation = \"stored\"", "[database]", "prefix_stacking = false"} {
		if !strings.Contains(string(upgraded), want) {
			t.Fatalf("upgraded config missing %q:\n%s", want, upgraded)
		}
	}
	if strings.Count(string(upgraded), "config_version =") != 1 {
		t.Fatalf("expected exactly one config_version line:\n%s", upgraded)
	}
}

func TestLoadConfigDoesNotMigrateCurrentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stonyperms.toml")
	current := `config_version = 4
default_group = "default"
storage_type = "json"
storage_dir = "config/stonyperms"
`
	if err := os.WriteFile(path, []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.MigrationBackup != "" {
		t.Fatalf("unexpected backup %q", cfg.MigrationBackup)
	}
	matches, _ := filepath.Glob(path + ".bak-v*")
	if len(matches) != 0 {
		t.Fatalf("unexpected migration backups: %v", matches)
	}
}
