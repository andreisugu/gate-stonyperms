package stonyperms

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/andreisugu/gate-stonyperms/storage"
)

const defaultConfigTemplate = `# =============================================================================
#                              StonyPerms
#         High-Performance Permissions Engine for Gate Minecraft Proxy
# =============================================================================

# Schema version. StonyPerms backs up and upgrades older configs on startup.
config_version = 4

# The default group assigned to newly joined players if no group is assigned.
default_group = "default"

# How a player's primary group is calculated: "stored", "parents-by-weight", or "all-parents-by-weight".
primary_group_calculation = "stored"

# Storage method to persist permissions data.
# Supported options: "json" (flat-file), "sqlite", "mysql", "mariadb", "postgresql"
storage_type = "json"

# Directory where flat-file JSON permissions, default SQLite DB, and exports are stored.
storage_dir = "config/stonyperms"

# Relational SQL database settings (used when storage_type is sqlite/mysql/mariadb/postgresql).
# Passwords may also be provided via STONYPERMS_DB_PASSWORD or database.password_file.
[database]
file = ""                  # Optional override for sqlite; defaults to <storage_dir>/stonyperms.db
host = "127.0.0.1"
port = 0                   # 0 uses engine default (3306 for mysql/mariadb, 5432 for postgresql)
database = "stonyperms"
username = "stonyperms"
password = ""
password_file = ""
table_prefix = "stonyperms_"
ssl_mode = "prefer"        # disable | prefer | require | verify-ca | verify-full | skip-verify
max_open_conns = 10
max_idle_conns = 5
conn_max_lifetime_seconds = 1800
connect_timeout_seconds = 5

# Whether to automatically inject StonyPerms into Gate's internal permission system.
# When enabled, Gate commands (/server, /send, /glist, etc.) and player checks
# resolve via StonyPerms rules.
apply_gate_permissions = true

# Whether to track the player's connected backend server as context (server=<name>).
# Enables per-server permissions (e.g. permission only valid on server=lobby).
apply_server_contexts = true

# Enable automatic audit logging for all administrative commands.
enable_audit_log = true

# =============================================================================
# Scoped HTTP API (for trusted Paper/Fabric adapters and admin integrations)
# =============================================================================
# Disabled by default. The API exposes read-only snapshots/checks and, when
# api_write_enabled is explicitly true with a separate write token, scoped writes.
api_enabled = false

# Bind address. Loopback is strongly recommended. If the backend runs on
# another host, use a private network (tailnet/WireGuard) and enable TLS.
api_listen = "127.0.0.1:8737"

# Shared read-tier bearer token(s), newline-separated for rotation. Prefer
# api_token_file or STONYPERMS_API_TOKEN so secrets are not stored here.
api_token = ""
api_token_file = "config/stonyperms-api.token"

# Allow binding to a non-loopback address. Authentication is still required.
api_allow_remote = false

# Optional TLS certificate and key. Both must be set to enable HTTPS.
api_tls_cert = ""
api_tls_key = ""

# Advisory snapshot cache lifetime returned to clients, in seconds.
api_snapshot_ttl_seconds = 30

# Write-tier endpoints (fail-closed by default). Requires distinct write token(s)
# via STONYPERMS_API_WRITE_TOKEN, api_write_token_file, or api_write_token.
# api_write_enabled = false
# api_write_token = ""
# api_write_token_file = ""

# Per-IP token-bucket rate limits (requests/sec and burst).
# api_rate_limit_per_sec = 60
# api_rate_limit_burst = 120
# api_write_rate_limit_per_sec = 15
# api_write_rate_limit_burst = 30

# Server-Sent Events (/v1/events) for instant permission invalidation.
# api_sse_enabled = true
# api_sse_max_clients = 32

# Public origin used in /sp editor, /sp verbose paste, and /sp tree links.
# Leave empty to derive it from api_listen. Set this when players reach Gate
# through a different hostname, a reverse proxy, or HTTPS termination.
# Example: api_editor_public_url = "https://perms.example.com"
api_editor_public_url = ""

# Prefix / Suffix stacking spacers & deduplication
prefix_stacking = false
prefix_start_spacer = ""
prefix_middle_spacer = ""
prefix_end_spacer = ""
suffix_stacking = false
suffix_start_spacer = ""
suffix_middle_spacer = ""
suffix_end_spacer = ""
stack_deduplicate = true
`

// DatabaseConfig configures relational SQL backends (sqlite, mysql, mariadb, postgresql).
type DatabaseConfig struct {
	File                   string `toml:"file"`
	Host                   string `toml:"host"`
	Port                   int    `toml:"port"`
	Database               string `toml:"database"`
	Username               string `toml:"username"`
	Password               string `toml:"password"`
	PasswordFile           string `toml:"password_file"`
	TablePrefix            string `toml:"table_prefix"`
	SSLMode                string `toml:"ssl_mode"`
	MaxOpenConns           int    `toml:"max_open_conns"`
	MaxIdleConns           int    `toml:"max_idle_conns"`
	ConnMaxLifetimeSeconds int    `toml:"conn_max_lifetime_seconds"`
	ConnectTimeoutSeconds  int    `toml:"connect_timeout_seconds"`
}

// Config represents the top-level StonyPerms configuration.
// CurrentConfigVersion is bumped whenever new keys/sections need migration.
const CurrentConfigVersion = 4

type Config struct {
	ConfigVersion           int            `toml:"config_version"`
	MigratedFrom            int            `toml:"-"`
	MigrationBackup         string         `toml:"-"`
	DefaultGroup            string         `toml:"default_group"`
	PrimaryGroupCalculation string         `toml:"primary_group_calculation"`
	StorageType             string         `toml:"storage_type"`
	StorageDir              string         `toml:"storage_dir"`
	Database                DatabaseConfig `toml:"database"`
	ApplyGatePermissions    bool           `toml:"apply_gate_permissions"`
	ApplyServerContexts     bool           `toml:"apply_server_contexts"`
	EnableAuditLog          bool           `toml:"enable_audit_log"`

	APIEnabled              bool    `toml:"api_enabled"`
	APIListen               string  `toml:"api_listen"`
	APIToken                string  `toml:"api_token"`
	APITokenFile            string  `toml:"api_token_file"`
	APIAllowRemote          bool    `toml:"api_allow_remote"`
	APITLSCert              string  `toml:"api_tls_cert"`
	APITLSKey               string  `toml:"api_tls_key"`
	APISnapshotTTLSeconds   int     `toml:"api_snapshot_ttl_seconds"`
	APIWriteEnabled         bool    `toml:"api_write_enabled"`
	APIWriteToken           string  `toml:"api_write_token"`
	APIWriteTokenFile       string  `toml:"api_write_token_file"`
	APIRateLimitPerSec      float64 `toml:"api_rate_limit_per_sec"`
	APIRateLimitBurst       int     `toml:"api_rate_limit_burst"`
	APIWriteRateLimitPerSec float64 `toml:"api_write_rate_limit_per_sec"`
	APIWriteRateLimitBurst  int     `toml:"api_write_rate_limit_burst"`
	APISSEEnabled           bool    `toml:"api_sse_enabled"`
	APISSEMaxClients        int     `toml:"api_sse_max_clients"`
	APIEditorPublicURL      string  `toml:"api_editor_public_url"`

	PrefixStacking     bool   `toml:"prefix_stacking"`
	PrefixStartSpacer  string `toml:"prefix_start_spacer"`
	PrefixMiddleSpacer string `toml:"prefix_middle_spacer"`
	PrefixEndSpacer    string `toml:"prefix_end_spacer"`
	SuffixStacking     bool   `toml:"suffix_stacking"`
	SuffixStartSpacer  string `toml:"suffix_start_spacer"`
	SuffixMiddleSpacer string `toml:"suffix_middle_spacer"`
	SuffixEndSpacer    string `toml:"suffix_end_spacer"`
	StackDeduplicate   bool   `toml:"stack_deduplicate"`
}

func defaultConfig() *Config {
	return &Config{
		ConfigVersion:           CurrentConfigVersion,
		DefaultGroup:            "default",
		PrimaryGroupCalculation: "stored",
		StorageType:             "json",
		StorageDir:              "config/stonyperms",
		Database: DatabaseConfig{
			Host:                   "127.0.0.1",
			Database:               "stonyperms",
			Username:               "stonyperms",
			TablePrefix:            "stonyperms_",
			SSLMode:                "prefer",
			MaxOpenConns:           10,
			MaxIdleConns:           5,
			ConnMaxLifetimeSeconds: 1800,
			ConnectTimeoutSeconds:  5,
		},
		ApplyGatePermissions:    true,
		ApplyServerContexts:     true,
		EnableAuditLog:          true,
		APIListen:               "127.0.0.1:8737",
		APITokenFile:            "config/stonyperms-api.token",
		APISnapshotTTLSeconds:   30,
		APIWriteEnabled:         false,
		APIRateLimitPerSec:      60,
		APIRateLimitBurst:       120,
		APIWriteRateLimitPerSec: 15,
		APIWriteRateLimitBurst:  30,
		APISSEEnabled:           true,
		APISSEMaxClients:        32,
		StackDeduplicate:        true,
	}
}

// LoadConfig loads the config from filePath or writes default configuration if missing.
func LoadConfig(filePath string) (*Config, error) {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		_ = os.MkdirAll(filepath.Dir(filePath), 0o755)
		_ = os.WriteFile(filePath, []byte(defaultConfigTemplate), 0o644)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	cfg, err := decodeConfig(data)
	if err != nil {
		return nil, err
	}
	if cfg.ConfigVersion < CurrentConfigVersion {
		oldVersion := cfg.ConfigVersion
		backup, migrateErr := migrateConfigFile(filePath, oldVersion)
		if migrateErr != nil {
			return nil, migrateErr
		}
		data, err = os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		cfg, err = decodeConfig(data)
		if err != nil {
			return nil, err
		}
		cfg.MigratedFrom = oldVersion
		cfg.MigrationBackup = backup
	}
	return cfg, nil
}

func decodeConfig(data []byte) (*Config, error) {
	cfg := defaultConfig()
	// Missing config_version must read as 0 so old files trigger migration.
	cfg.ConfigVersion = 0
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	editorURL, err := normalizeEditorPublicURL(cfg.APIEditorPublicURL)
	if err != nil {
		return nil, err
	}
	cfg.APIEditorPublicURL = editorURL
	primaryCalc, err := normalizePrimaryGroupCalculation(cfg.PrimaryGroupCalculation)
	if err != nil {
		return nil, err
	}
	cfg.PrimaryGroupCalculation = primaryCalc
	storageType, err := storage.NormalizeStorageType(cfg.StorageType)
	if err != nil {
		return nil, err
	}
	cfg.StorageType = storageType
	if cfg.Database.TablePrefix == "" {
		cfg.Database.TablePrefix = "stonyperms_"
	}
	if err := storage.ValidateTablePrefix(cfg.Database.TablePrefix); err != nil {
		return nil, err
	}
	return cfg, nil
}

// migrateConfigFile backs up an older config and appends missing keys without
// rewriting existing values or comments. It returns the backup path.
func migrateConfigFile(filePath string, oldVersion int) (string, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	backup := fmt.Sprintf("%s.bak-v%d-%s", filePath, oldVersion, time.Now().UTC().Format("20060102-150405"))
	if err := os.WriteFile(backup, raw, 0o600); err != nil {
		return "", err
	}

	text := string(raw)
	var leading []string
	addKey := func(key, line string) {
		if !hasTopLevelConfigKey(text, key) {
			leading = append(leading, line)
		}
	}
	addKey("config_version", fmt.Sprintf("config_version = %d", CurrentConfigVersion))
	addKey("primary_group_calculation", `primary_group_calculation = "stored"`)
	addKey("api_editor_public_url", `api_editor_public_url = ""`)
	addKey("prefix_stacking", "prefix_stacking = false")
	addKey("prefix_start_spacer", `prefix_start_spacer = ""`)
	addKey("prefix_middle_spacer", `prefix_middle_spacer = ""`)
	addKey("prefix_end_spacer", `prefix_end_spacer = ""`)
	addKey("suffix_stacking", "suffix_stacking = false")
	addKey("suffix_start_spacer", `suffix_start_spacer = ""`)
	addKey("suffix_middle_spacer", `suffix_middle_spacer = ""`)
	addKey("suffix_end_spacer", `suffix_end_spacer = ""`)
	addKey("stack_deduplicate", "stack_deduplicate = true")

	// Keep the existing version line in sync when it is already present.
	if hasTopLevelConfigKey(text, "config_version") {
		text = replaceTopLevelConfigLine(text, "config_version", fmt.Sprintf("config_version = %d", CurrentConfigVersion))
	}

	header := "# --- migrated by StonyPerms (schema v" + fmt.Sprint(CurrentConfigVersion) + ") ---\n"
	if len(leading) > 0 {
		text = header + strings.Join(leading, "\n") + "\n\n" + text
	} else {
		text = header + text
	}
	if !strings.Contains(text, "[database]") {
		text = strings.TrimRight(text, "\n") + `

# Relational SQL backends (used when storage_type is sqlite/mysql/mariadb/postgresql).
[database]
file = ""
host = "127.0.0.1"
port = 0
database = "stonyperms"
username = "stonyperms"
password = ""
password_file = ""
table_prefix = "stonyperms_"
ssl_mode = "prefer"
max_open_conns = 10
max_idle_conns = 5
conn_max_lifetime_seconds = 1800
connect_timeout_seconds = 5
`
	}

	tmp := filePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, filePath); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return backup, nil
}

func hasTopLevelConfigKey(text, key string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			return false
		}
		if strings.HasPrefix(trimmed, key+" ") || strings.HasPrefix(trimmed, key+"=") {
			return true
		}
	}
	return false
}

func replaceTopLevelConfigLine(text, key, replacement string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		if strings.HasPrefix(trimmed, key+" ") || strings.HasPrefix(trimmed, key+"=") {
			lines[i] = replacement
			break
		}
	}
	return strings.Join(lines, "\n")
}

func normalizeEditorPublicURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("api_editor_public_url must be an absolute http(s) URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("api_editor_public_url must not contain a query or fragment")
	}
	return strings.TrimRight(trimmed, "/"), nil
}

func normalizePrimaryGroupCalculation(raw string) (string, error) {
	clean := strings.ToLower(strings.TrimSpace(raw))
	if clean == "" {
		return "stored", nil
	}
	switch clean {
	case "stored", "parents-by-weight", "all-parents-by-weight":
		return clean, nil
	default:
		return "", fmt.Errorf("primary_group_calculation must be stored, parents-by-weight, or all-parents-by-weight")
	}
}
