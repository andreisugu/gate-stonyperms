package stonyperms

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/robinbraemer/event"
	"go.minekube.com/gate/pkg/edition/java/proxy"

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

		// Initialize storage provider
		store, err := storage.NewJSONStorage(cfg.StorageDir)
		if err != nil {
			log.Error(err, "Failed to initialize StonyPerms JSON storage")
			return err
		}

		// Initialize manager
		mgr, err := manager.New(store)
		if err != nil {
			log.Error(err, "Failed to initialize StonyPerms manager")
			return err
		}
		globalManager.Store(mgr)

		var (
			serverContexts sync.Map
		)

		// 1. Hook Gate Permissions Setup Event
		if cfg.ApplyGatePermissions {
			event.Subscribe(p.Event(), 0, func(e *proxy.PermissionsSetupEvent) {
				subj := e.Subject()
				if pl, ok := subj.(proxy.Player); ok {
					uid := uuid.UUID(pl.ID())
					user := mgr.GetOrCreateUser(uid, pl.Username())
					e.SetFunc(mgr.GatePermissionFunc(user, func() map[string]string {
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

		// 2. Track dynamic server context on server switch
		if cfg.ApplyServerContexts {
			event.Subscribe(p.Event(), 0, func(e *proxy.ServerPostConnectEvent) {
				if s := e.Player().CurrentServer(); s != nil {
					serverContexts.Store(e.Player().ID(), s.Server().ServerInfo().Name())
				}
			})

			event.Subscribe(p.Event(), 0, func(e *proxy.DisconnectEvent) {
				serverContexts.Delete(e.Player().ID())
			})
		}

		// 3. Register Command aliases (/sp, /stonyperms, /spb, /spv, /perm, /perms)
		command.RegisterCommands(p, mgr)

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
