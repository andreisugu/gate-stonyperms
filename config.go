package stonyperms

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const defaultConfigTemplate = `# =============================================================================
#                              StonyPerms
#         High-Performance Permissions Engine for Gate Minecraft Proxy
# =============================================================================

# The default group assigned to newly joined players if no group is assigned.
default_group = "default"

# How a player's primary group is calculated: "stored", "parents-by-weight", or "all-parents-by-weight".
primary_group_calculation = "stored"

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

// Config represents the top-level StonyPerms configuration.
type Config struct {
	DefaultGroup            string `toml:"default_group"`
	PrimaryGroupCalculation string `toml:"primary_group_calculation"`
	StorageType             string `toml:"storage_type"`
	StorageDir              string `toml:"storage_dir"`
	ApplyGatePermissions    bool   `toml:"apply_gate_permissions"`
	ApplyServerContexts     bool   `toml:"apply_server_contexts"`
	EnableAuditLog          bool   `toml:"enable_audit_log"`

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
		DefaultGroup:            "default",
		PrimaryGroupCalculation: "stored",
		StorageType:             "json",
		StorageDir:              "config/stonyperms",
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

	cfg := defaultConfig()
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

	return cfg, nil
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
