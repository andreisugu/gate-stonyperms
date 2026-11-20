package stonyperms

import (
	"context"
	"path/filepath"
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

// Plugin is the primary Gate proxy plugin entrypoint for StonyPerms.
var Plugin = proxy.Plugin{
	Name: "StonyPerms",
	Init: func(ctx context.Context, p *proxy.Proxy) error {
		log := logr.FromContextOrDiscard(ctx)
		configPath := filepath.Join("config", "stonyperms.toml")

		cfg, err := LoadConfig(configPath)
		if err != nil {
			log.Error(err, "Failed to load stonyperms.toml, using defaults")
			cfg = &Config{
				DefaultGroup:         "default",
				StorageType:          "json",
				StorageDir:           "config/stonyperms",
				ApplyGatePermissions: true,
				ApplyServerContexts:  true,
				EnableAuditLog:       true,
			}
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

		// Start the optional read-only HTTP API used by backend adapters.
		// A configuration error disables the API but never prevents Gate
		// from starting with local permission enforcement intact.
		if cfg.APIEnabled {
			token, tokenErr := api.LoadToken(cfg.APIToken, cfg.APITokenFile)
			if tokenErr != nil {
				log.Error(tokenErr, "StonyPerms HTTP API not started")
			} else {
				apiServer, apiErr := api.New(mgr, api.Options{
					Listen:       cfg.APIListen,
					Token:        token,
					AllowRemote:  cfg.APIAllowRemote,
					TLSCertFile:  cfg.APITLSCert,
					TLSKeyFile:   cfg.APITLSKey,
					SnapshotTTL:  time.Duration(cfg.APISnapshotTTLSeconds) * time.Second,
					DefaultGroup: cfg.DefaultGroup,
					Logger:       log,
				})
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
