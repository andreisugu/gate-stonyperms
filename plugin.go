package stonyperms

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/robinbraemer/event"
	"go.minekube.com/gate/pkg/edition/java/proxy"

	"github.com/andreisugu/gate-stonyperms/api"
	"github.com/andreisugu/gate-stonyperms/command"
	"github.com/andreisugu/gate-stonyperms/manager"
	"github.com/andreisugu/gate-stonyperms/storage"
)

var (
	globalManager atomic.Pointer[manager.Manager]
)

// GetManager returns the active StonyPerms Manager instance, allowing other plugins to query permissions & metadata.
func GetManager() *manager.Manager {
	return globalManager.Load()
}

func buildAPIOptions(cfg *Config, log logr.Logger) (api.Options, error) {
	readTokens, err := api.LoadTokens(cfg.APIToken, cfg.APITokenFile, "STONYPERMS_API_TOKEN")
	if err != nil {
		return api.Options{}, err
	}

	writeEnabled := cfg.APIWriteEnabled
	var writeTokens []string
	if writeEnabled {
		wt, writeErr := api.LoadTokens(cfg.APIWriteToken, cfg.APIWriteTokenFile, "STONYPERMS_API_WRITE_TOKEN")
		if writeErr != nil {
			// Fail closed: never fall back to read tokens when write token loading fails.
			log.Error(writeErr, "StonyPerms HTTP write endpoints disabled; failed to load write token")
			writeEnabled = false
		} else {
			writeTokens = wt
		}
	}

	return api.Options{
		Listen:               cfg.APIListen,
		Tokens:               readTokens,
		WriteEnabled:         writeEnabled,
		WriteTokens:          writeTokens,
		RateLimitPerSec:      cfg.APIRateLimitPerSec,
		RateLimitBurst:       cfg.APIRateLimitBurst,
		WriteRateLimitPerSec: cfg.APIWriteRateLimitPerSec,
		WriteRateLimitBurst:  cfg.APIWriteRateLimitBurst,
		SSEEnabled:           cfg.APISSEEnabled,
		SSEMaxClients:        cfg.APISSEMaxClients,
		AllowRemote:          cfg.APIAllowRemote,
		TLSCertFile:          cfg.APITLSCert,
		TLSKeyFile:           cfg.APITLSKey,
		SnapshotTTL:          time.Duration(cfg.APISnapshotTTLSeconds) * time.Second,
		DefaultGroup:         cfg.DefaultGroup,
		Logger:               log,
	}, nil
}

// ResolveWebBaseURL returns the base URL used by /sp editor, /sp verbose paste, and /sp tree.
func ResolveWebBaseURL(cfg *Config) string {
	if cfg == nil {
		return manager.DefaultPublicWebURL
	}
	if trimmed := strings.TrimSpace(cfg.APIEditorPublicURL); trimmed != "" {
		return strings.TrimRight(trimmed, "/")
	}
	if !cfg.APIEnabled {
		return manager.DefaultPublicWebURL
	}
	listen := strings.TrimSpace(cfg.APIListen)
	if listen == "" {
		listen = "127.0.0.1:8737"
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return manager.DefaultPublicWebURL
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	scheme := "http"
	if strings.TrimSpace(cfg.APITLSCert) != "" && strings.TrimSpace(cfg.APITLSKey) != "" {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

// Plugin is the primary Gate proxy plugin entrypoint for StonyPerms.
var Plugin = proxy.Plugin{
	Name: "StonyPerms",
	Init: func(ctx context.Context, p *proxy.Proxy) error {
		log := logr.FromContextOrDiscard(ctx)
		configPath := filepath.Join("config", "stonyperms.toml")

		cfg, err := LoadConfig(configPath)
		if err != nil {
			log.Error(err, "Failed to load stonyperms.toml, using defaults")
			cfg = defaultConfig()
		}

		store, err := storage.NewJSONStorage(cfg.StorageDir)
		if err != nil {
			return err
		}

		mgr, err := manager.New(store)
		if err != nil {
			return err
		}
		globalManager.Store(mgr)
		mgr.SetWebConfig(manager.WebConfig{
			BaseURL:       ResolveWebBaseURL(cfg),
			DefaultGroup:  cfg.DefaultGroup,
			LiveSave:      cfg.APIEnabled,
			PluginVersion: "0.3.0",
		})

		// Start the optional HTTP API used by backend adapters and integrations.
		// A configuration error disables the API (or write tier) without
		// preventing Gate from starting with local permission enforcement intact.
		if cfg.APIEnabled {
			apiOpts, tokenErr := buildAPIOptions(cfg, log)
			if tokenErr != nil {
				log.Error(tokenErr, "StonyPerms HTTP API not started")
			} else {
				apiServer, apiErr := api.New(mgr, apiOpts)
				if apiErr != nil {
					log.Error(apiErr, "StonyPerms HTTP API not started")
				} else if startErr := apiServer.Start(ctx); startErr != nil {
					log.Error(startErr, "StonyPerms HTTP API not started")
				}
			}
		}

		var serverContexts sync.Map

		// 1. Hook Gate Permissions Setup Event (Core Permission Provider)
		if cfg.ApplyGatePermissions {
			event.Subscribe(p.Event(), 0, func(e *proxy.PermissionsSetupEvent) {
				subj := e.Subject()
				if pl, ok := subj.(proxy.Player); ok {
					uid := uuid.UUID(pl.ID())
					_ = mgr.GetOrCreateUser(uid, pl.Username())
					e.SetFunc(mgr.GatePermissionFunc(uid, func() map[string]string {
						ctxs := make(map[string]string)
						if cfg.ApplyServerContexts {
							if val, ok := serverContexts.Load(pl.ID()); ok {
								if sName, ok := val.(string); ok && sName != "" {
									ctxs["server"] = sName
								}
							} else if s := pl.CurrentServer(); s != nil {
								ctxs["server"] = s.Server().ServerInfo().Name()
							}
						}
						return ctxs
					}))
				}
			})
		}

		// 2. Track dynamic server context on server switch & disconnect cleanup
		if cfg.ApplyServerContexts {
			event.Subscribe(p.Event(), 0, func(e *proxy.ServerPostConnectEvent) {
				if s := e.Player().CurrentServer(); s != nil {
					serverContexts.Store(e.Player().ID(), s.Server().ServerInfo().Name())
				}
			})
		}

		event.Subscribe(p.Event(), 0, func(e *proxy.DisconnectEvent) {
			serverContexts.Delete(e.Player().ID())
		})

		// 3. Register Command aliases (/sp, /stonyperms, /spb, /spv, /perm, /perms)
		command.RegisterCommands(p, mgr)

		// 4. Check Gate configuration and log warning if builtin command permissions are disabled
		if !p.Config().RequireBuiltinCommandPermissions {
			log.Info("NOTE: Gate's 'requireBuiltinCommandPermissions' is set to false in config.yml. Built-in proxy commands (/server, /send, /glist) will bypass permissions until enabled.")
		}

		uCount, gCount, tCount, _ := mgr.Stats()
		log.Info("StonyPerms successfully initialized",
			"users", uCount,
			"groups", gCount,
			"tracks", tCount,
			"storage", cfg.StorageType,
			"gate_hooks", cfg.ApplyGatePermissions,
		)

		return nil
	},
}
