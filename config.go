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
`

// Config represents the top-level StonyPerms configuration.
type Config struct {
	DefaultGroup         string `toml:"default_group"`
	StorageType          string `toml:"storage_type"`
	StorageDir           string `toml:"storage_dir"`
	ApplyGatePermissions bool   `toml:"apply_gate_permissions"`
	ApplyServerContexts  bool   `toml:"apply_server_contexts"`
	EnableAuditLog       bool   `toml:"enable_audit_log"`
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
		DefaultGroup:         "default",
		StorageType:          "json",
		StorageDir:           "config/stonyperms",
		ApplyGatePermissions: true,
		ApplyServerContexts:  true,
		EnableAuditLog:       true,
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
