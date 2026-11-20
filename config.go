package stonyperms

import (
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

const defaultConfigTemplate = `# =============================================================================
#                              StonyPerms
#         High-Performance Permissions Engine for Gate Minecraft Proxy
# =============================================================================

# The default group assigned to newly joined players if no group is assigned.
default_group = "default"

# Storage method to persist permissions data.
# Supported options: "json" (flat-file)
storage_type = "json"

# Directory where permissions, users, groups, tracks, and logs are stored.
storage_dir = "config/stonyperms"

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
# Read-only HTTP API (for trusted Paper/Fabric permission adapters)
# =============================================================================
# Disabled by default. The API exposes user/group permission snapshots and
# permission checks; it never mutates data.
api_enabled = false

# Bind address. Loopback is strongly recommended. If the backend runs on
# another host, use a private network (tailnet/WireGuard) and enable TLS.
api_listen = "127.0.0.1:8737"

# Shared bearer token. Prefer api_token_file or the STONYPERMS_API_TOKEN
# environment variable so the secret is not stored in this file.
api_token = ""
api_token_file = "config/stonyperms-api.token"

# Allow binding to a non-loopback address. Authentication is still required.
api_allow_remote = false

# Optional TLS certificate and key. Both must be set to enable HTTPS.
api_tls_cert = ""
api_tls_key = ""

# Advisory snapshot cache lifetime returned to clients, in seconds.
api_snapshot_ttl_seconds = 30
`

// Config represents the top-level StonyPerms configuration.
type Config struct {
	DefaultGroup         string `toml:"default_group"`
	StorageType          string `toml:"storage_type"`
	StorageDir           string `toml:"storage_dir"`
	ApplyGatePermissions bool   `toml:"apply_gate_permissions"`
	ApplyServerContexts  bool   `toml:"apply_server_contexts"`
	EnableAuditLog       bool   `toml:"enable_audit_log"`

	APIEnabled            bool   `toml:"api_enabled"`
	APIListen             string `toml:"api_listen"`
	APIToken              string `toml:"api_token"`
	APITokenFile          string `toml:"api_token_file"`
	APIAllowRemote        bool   `toml:"api_allow_remote"`
	APITLSCert            string `toml:"api_tls_cert"`
	APITLSKey             string `toml:"api_tls_key"`
	APISnapshotTTLSeconds int    `toml:"api_snapshot_ttl_seconds"`
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

	cfg := &Config{
		DefaultGroup:          "default",
		StorageType:           "json",
		StorageDir:            "config/stonyperms",
		ApplyGatePermissions:  true,
		ApplyServerContexts:   true,
		EnableAuditLog:        true,
		APIListen:             "127.0.0.1:8737",
		APITokenFile:          "config/stonyperms-api.token",
		APISnapshotTTLSeconds: 30,
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
