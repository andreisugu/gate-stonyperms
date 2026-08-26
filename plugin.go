package stonyperms

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/robinbraemer/event"
	"go.minekube.com/brigodier"
	gatecmd "go.minekube.com/gate/pkg/command"
	"go.minekube.com/gate/pkg/edition/java/proto/packet"
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

		store, err := storage.NewJSONStorage(cfg.StorageDir)
		if err != nil {
			return err
		}

		mgr, err := manager.New(store)
		if err != nil {
			return err
		}
		globalManager.Store(mgr)

		var (
			serverContexts         sync.Map
			playerBaseCommandTrees sync.Map
		)

		// Helper to clone a Brigadier node tree
		var cloneNode func(src brigodier.CommandNode) brigodier.CommandNode
		cloneNode = func(src brigodier.CommandNode) brigodier.CommandNode {
			if src == nil {
				return nil
			}
			var dest brigodier.CommandNode
			if _, ok := src.(*brigodier.RootCommandNode); ok {
				dest = &brigodier.RootCommandNode{}
			} else {
				builder := src.CreateBuilder().Requires(func(context.Context) bool { return true })
				if src.Redirect() != nil {
					builder.Redirect(cloneNode(src.Redirect()))
				}
				dest = builder.Build()
			}

			src.ChildrenOrdered().Range(func(_ string, sourceChild brigodier.CommandNode) bool {
				destChild := cloneNode(sourceChild)
				if destChild != nil {
					dest.AddChild(destChild)
				}
				return true
			})
			return dest
		}

		// Helper to filter proxy commands against player permissions
		var filterNode func(src brigodier.CommandNode, cmdSrc gatecmd.Source, reqCtx context.Context) brigodier.CommandNode
		filterNode = func(src brigodier.CommandNode, cmdSrc gatecmd.Source, reqCtx context.Context) brigodier.CommandNode {
			var dest brigodier.CommandNode
			if _, ok := src.(*brigodier.RootCommandNode); ok {
				dest = &brigodier.RootCommandNode{}
			} else {
				if !src.CanUse(reqCtx) {
					return nil
				}
				builder := src.CreateBuilder().Requires(func(context.Context) bool { return true })
				if src.Redirect() != nil {
					builder.Redirect(filterNode(src.Redirect(), cmdSrc, reqCtx))
				}
				dest = builder.Build()
			}

			src.ChildrenOrdered().Range(func(_ string, sourceChild brigodier.CommandNode) bool {
				destChild := filterNode(sourceChild, cmdSrc, reqCtx)
				if destChild != nil {
					dest.AddChild(destChild)
				}
				return true
			})
			return dest
		}

		// refreshPlayerCommands sends an updated AvailableCommands packet directly to the player
		refreshPlayerCommands := func(pl proxy.Player) {
			if pl == nil {
				return
			}
			val, ok := playerBaseCommandTrees.Load(pl.ID())
			if !ok || val == nil {
				return
			}
			base, ok := val.(*brigodier.RootCommandNode)
			if !ok || base == nil {
				return
			}

			mergedRoot, ok := cloneNode(base).(*brigodier.RootCommandNode)
			if !ok || mergedRoot == nil {
				return
			}

			if p.Config().AnnounceProxyCommands {
				reqCtx := gatecmd.ContextWithSource(context.Background(), pl)
				proxyRoot := filterNode(&p.Command().Root, pl, reqCtx)
				if proxyRoot != nil {
					proxyRoot.ChildrenOrdered().Range(func(_ string, node brigodier.CommandNode) bool {
						if existing := mergedRoot.Children()[node.Name()]; existing != nil {
							mergedRoot.RemoveChild(existing.Name())
						}
						mergedRoot.AddChild(node)
						return true
					})
				}
			}

			_ = pl.WritePacket(&packet.AvailableCommands{
				RootNode: mergedRoot,
			})
		}

		// 1. Hook Gate Permissions Setup Event
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

		// 2. Cache backend commands & filter on AvailableCommands
		event.Subscribe(p.Event(), 0, func(e *proxy.PlayerAvailableCommandsEvent) {
			if e.Player() == nil || e.RootNode() == nil {
				return
			}
			// Clone the tree and remove proxy command nodes so we retain only pure backend server commands
			base, ok := cloneNode(e.RootNode()).(*brigodier.RootCommandNode)
			if ok && base != nil {
				p.Command().Root.ChildrenOrdered().Range(func(_ string, proxyNode brigodier.CommandNode) bool {
					base.RemoveChild(proxyNode.Name())
					return true
				})
				playerBaseCommandTrees.Store(e.Player().ID(), base)
			}
		})

		// 3. Track dynamic server context on server switch & disconnect cleanup
		if cfg.ApplyServerContexts {
			event.Subscribe(p.Event(), 0, func(e *proxy.ServerPostConnectEvent) {
				if s := e.Player().CurrentServer(); s != nil {
					serverContexts.Store(e.Player().ID(), s.Server().ServerInfo().Name())
				}
			})
		}

		event.Subscribe(p.Event(), 0, func(e *proxy.DisconnectEvent) {
			serverContexts.Delete(e.Player().ID())
			playerBaseCommandTrees.Delete(e.Player().ID())
		})

		// 4. Hook Manager Update Listener to live-resend command trees to connected players
		mgr.SetUpdateListener(func() {
			for _, pl := range p.Players() {
				refreshPlayerCommands(pl)
			}
		})

		// 5. Register Command aliases (/sp, /stonyperms, /spb, /spv, /perm, /perms)
		command.RegisterCommands(p, mgr)

		// 6. Check Gate configuration and log warning if builtin command permissions are disabled
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
