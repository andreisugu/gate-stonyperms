package stonyperms

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"

	"github.com/andreisugu/gate-stonyperms/api"
	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/storage"
)

func TestLoadConfigDefaultsAndNewFields(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "stonyperms.toml")

	// Missing file writes template and parses safe fail-closed defaults.
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if cfg.APIEnabled || cfg.APIWriteEnabled {
		t.Fatalf("expected api_enabled=false and api_write_enabled=false by default, got enabled=%v write=%v", cfg.APIEnabled, cfg.APIWriteEnabled)
	}
	if cfg.APIRateLimitPerSec != 60 || cfg.APIRateLimitBurst != 120 {
		t.Fatalf("unexpected read rate limit defaults: %v / %d", cfg.APIRateLimitPerSec, cfg.APIRateLimitBurst)
	}
	if cfg.APIWriteRateLimitPerSec != 15 || cfg.APIWriteRateLimitBurst != 30 {
		t.Fatalf("unexpected write rate limit defaults: %v / %d", cfg.APIWriteRateLimitPerSec, cfg.APIWriteRateLimitBurst)
	}
	if !cfg.APISSEEnabled || cfg.APISSEMaxClients != 32 {
		t.Fatalf("unexpected SSE defaults: enabled=%v max=%d", cfg.APISSEEnabled, cfg.APISSEMaxClients)
	}

	custom := `
api_enabled = true
api_write_enabled = true
api_rate_limit_per_sec = 90
api_rate_limit_burst = 180
api_write_rate_limit_per_sec = 20
api_write_rate_limit_burst = 40
api_sse_enabled = false
api_sse_max_clients = 12
`
	if err := os.WriteFile(cfgPath, []byte(custom), 0o600); err != nil {
		t.Fatalf("write custom config: %v", err)
	}
	cfg, err = LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load custom config: %v", err)
	}
	if !cfg.APIEnabled || !cfg.APIWriteEnabled || cfg.APISSEEnabled {
		t.Fatalf("unexpected custom flags: %+v", cfg)
	}
	if cfg.APIRateLimitPerSec != 90 || cfg.APIRateLimitBurst != 180 || cfg.APIWriteRateLimitPerSec != 20 || cfg.APIWriteRateLimitBurst != 40 || cfg.APISSEMaxClients != 12 {
		t.Fatalf("unexpected custom limits: %+v", cfg)
	}
}

func TestBuildAPIOptionsFailClosedOnMissingOrOverlappingWriteToken(t *testing.T) {
	t.Setenv("STONYPERMS_API_TOKEN", "")
	t.Setenv("STONYPERMS_API_WRITE_TOKEN", "")

	cfg := defaultConfig()
	cfg.APIEnabled = true
	cfg.APIToken = "read-token-0123456789abcdef"
	cfg.APITokenFile = ""
	cfg.APIWriteEnabled = true
	cfg.APIWriteTokenFile = filepath.Join(t.TempDir(), "missing-write.token")

	// Missing write token must fail closed (WriteEnabled=false), never reusing read token.
	opts, err := buildAPIOptions(cfg, logr.Discard())
	if err != nil {
		t.Fatalf("buildAPIOptions unexpected error: %v", err)
	}
	if opts.WriteEnabled || len(opts.WriteTokens) != 0 {
		t.Fatalf("expected writes disabled when write token fails to load, got %+v", opts)
	}

	// Overlapping read & write token must cause api.New to refuse startup.
	cfg.APIWriteTokenFile = ""
	cfg.APIWriteToken = cfg.APIToken
	opts, err = buildAPIOptions(cfg, logr.Discard())
	if err != nil {
		t.Fatalf("buildAPIOptions with inline write token error: %v", err)
	}
	store, _ := storage.NewJSONStorage(filepath.Join(t.TempDir(), "stonyperms"))
	mgr, err := manager.New(store)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	if _, err := api.New(mgr, opts); err == nil {
		t.Fatal("expected api.New to refuse startup when read and write tokens overlap")
	}
}

func TestLoadConfigDatabaseAndValidation(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sp.toml")

	dbToml := `
storage_type = "postgres"
storage_dir = "config/sp"

[database]
host = "pg.internal"
port = 5432
database = "perms"
username = "sp"
table_prefix = "sp_prod_"
ssl_mode = "require"
max_open_conns = 16
`
	if err := os.WriteFile(cfgPath, []byte(dbToml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig postgres: %v", err)
	}
	if cfg.StorageType != "postgresql" || cfg.Database.Host != "pg.internal" || cfg.Database.TablePrefix != "sp_prod_" || cfg.Database.MaxOpenConns != 16 {
		t.Fatalf("unexpected database config: %+v", cfg)
	}

	if err := os.WriteFile(cfgPath, []byte(`storage_type = "invalid"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(cfgPath); err == nil {
		t.Fatal("expected error for invalid storage_type")
	}

	if err := os.WriteFile(cfgPath, []byte("storage_type = \"sqlite\"\n[database]\ntable_prefix = \"bad-prefix!\""), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(cfgPath); err == nil {
		t.Fatal("expected error for invalid database.table_prefix")
	}
}
