<div align="center">

# 💎 Gate StonyPerms

**Lightning-fast, LuckPerms-compatible permissions engine & cross-server HTTP + SSE platform for Minekube Gate.**

<p>
  <a href="https://github.com/andreisugu/gate-stonyperms/actions/workflows/ci.yml"><img src="https://github.com/andreisugu/gate-stonyperms/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://pkg.go.dev/github.com/andreisugu/gate-stonyperms"><img src="https://pkg.go.dev/badge/github.com/andreisugu/gate-stonyperms.svg" alt="Go Reference" /></a>
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go 1.26" />
  <a href="https://andreisugu.github.io/gate-stonyperms/"><img src="https://img.shields.io/badge/StonyPermsWeb-Live_Demo-94df03?style=flat-square&logoColor=181a1f" alt="StonyPermsWeb" /></a>
  <a href="https://github.com/minekube/gate"><img src="https://img.shields.io/badge/Platform-Minekube_Gate-7c3aed?style=flat-square" alt="Minekube Gate" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg?style=flat-square" alt="Apache 2.0" /></a>
</p>

<p>
  <a href="#-key-features">Features</a> •
  <a href="#-installation">Installation</a> •
  <a href="#-stonypermsweb-suite">Web Suite</a> •
  <a href="#-http-api-v02--live-sse">HTTP API v0.2</a> •
  <a href="#-configuration">Configuration</a> •
  <a href="#-commands">Commands</a> •
  <a href="#-minekube-gate-extensions-suite">Gate Suite</a>
</p>

</div>

---

## ✨ Key Features

- **LuckPerms-Style CLI (`/sp`):** Familiar `user`, `group`, `track`, `check`, `verbose`, and `log` trees with Brigadier auto-completion.
- **Exact + Wildcard + Explicit Deny Evaluation:** Supports exact nodes (`gate.command.server`), sub-wildcards (`gate.command.*`, `*`), explicit `false` denials, and circular inheritance protection.
- **Dynamic Contexts & Temporary Grants:** Per-server/world contexts (`server=lobby`) and expiring permissions, parent memberships, prefixes, and suffixes (`10m`, `2h`, `7d`, `30d`, `1y`).
- **Weighted Inheritance, Tracks & Affixes:** Groups resolve in descending weight order alongside promotion tracks, priority-ordered prefixes/suffixes, and metadata keys.
- **v0.2 Scoped HTTP API & Live SSE Stream:** Opt-in cross-server synchronization (`GET /v1/events` SSE invalidation, `ETag` snapshots, batch queries, idempotent audited writes, and multi-token rotation) for Paper, Fabric, web stores, and stats services.
- **Built-In StonyPermsWeb Suite (`https://andreisugu.github.io/gate-stonyperms/`):** Embedded (`//go:embed`) & GitHub-Pages-hosted Web Permissions Editor (`/sp editor`), Verbose Viewer (`/sp verbose record` + `paste`), Permission Tree Viewer (`/sp tree`), built-in Wiki, and `Ctrl+K` quick search with one-click **Live Save** and portable `sp1_` `/sp applyedits` codes.
- **Atomic Persistence & Live Verbose:** Crash-safe atomic JSON storage under `config/stonyperms/` and real-time permission decision tracing (`/sp verbose on`).

---

## 📦 Installation

### Using `gate-plugin-builder` / Pelican Egg
Add to your `plugins.txt` or `GATE_PLUGINS`:
```text
github.com/andreisugu/gate-stonyperms
```

### In Go Code
```bash
go get github.com/andreisugu/gate-stonyperms
```

```go
package main

import (
	"go.minekube.com/gate/cmd/gate"
	"go.minekube.com/gate/pkg/edition/java/proxy"

	stonyperms "github.com/andreisugu/gate-stonyperms"
)

func main() {
	proxy.Plugins = append(proxy.Plugins, stonyperms.Plugin)
	gate.Execute()
}
```

> Enable `requireBuiltinCommandPermissions: true` in Gate's `config.yml` so built-in proxy commands (`/server`, `/send`, `/glist`) obey StonyPerms nodes.

---

## 🌐 StonyPermsWeb Suite

| Tool | Command | What It Does |
| :--- | :--- | :--- |
| **Web Permissions Editor** | `/sp editor [filter]`<br>`/sp user <user> editor`<br>`/sp group <group> editor` | Edit groups, tracks, users, weights, prefixes/suffixes, expiry & contexts with autocomplete and **Live Save** (`POST /v1/editor/sessions/{token}/save`) or portable `/sp applyedits <code>`. |
| **Verbose Viewer** | `/sp verbose record [filter]`<br>`/sp verbose paste` | Silently capture evaluated checks in Gate (`2000` bounded ring) and inspect `true` / `false` / `undefined` results & resolution sources in the browser. |
| **Permission Tree** | `/sp tree [root]` | Browse registered Gate + stored + Paper/Fabric plugin permissions as an expandable tree. |
| **Built-In Wiki & Demo** | [Live StonyPermsWeb](https://andreisugu.github.io/gate-stonyperms/) | Hosted on GitHub Pages with interactive **View Demo**, full command wiki, and `Ctrl+K` global search; embedded directly on Gate's HTTP server when `api_enabled = true`. |

Set `api_editor_public_url` in `config/stonyperms.toml` if Gate sits behind an HTTPS reverse proxy; when `api_enabled = false`, `/sp editor` automatically encodes a compressed `sp1_` snapshot URL pointing at [StonyPermsWeb on GitHub Pages](https://andreisugu.github.io/gate-stonyperms/editor) so edits can be applied in chat via `/sp applyedits <code>`.

---

## 🌉 HTTP API v0.2 & Live SSE

StonyPerms exposes an optional, tailnet/TLS-ready JSON + Server-Sent Events API so backend adapters ([`andreisugu/stonyperms-bridge`](https://github.com/andreisugu/stonyperms-bridge)), web stores, and stats pipelines keep Gate as their **single source of truth**:

- **Dual-Tier Scoped Bearer Auth:** Separate read tokens (`api_token_file` / `STONYPERMS_API_TOKEN`) and opt-in write/admin tokens (`api_write_enabled = false` by default, `api_write_token_file` / `STONYPERMS_API_WRITE_TOKEN`). Startup refuses overlapping tokens; read tokens return `403 insufficient_scope` on write or `/v1/logs` routes.
- **Zero-Downtime Rotation & Rate Limits:** Up to 4 newline-separated tokens per tier (`#` comments ignored) and per-IP token-bucket limits (`60 req/s` burst `120` read; `15 req/s` burst `30` write).
- **Live SSE Invalidation (`GET /v1/events`):** Sub-second `ChangeEvent` push (`user_updated`, `group_updated`, `track_updated`, `reload`, `expiry_cleanup`) with `25s` keepalives alongside `ETag` snapshot caching.
- **Batch, Store & Audited Mutations (`X-StonyPerms-Actor`, `Idempotency-Key`):** `POST /v1/players/batch` (up to 100 UUIDs), `GET /v1/users/by-name/{username}`, `accumulate` donor time stacking, atomic `/v1/users/{id}/batch` transactions (up to 32 ops), Group/Track CRUD, and canonicalized query+body idempotency (`409 idempotency_conflict` on mismatch).

```toml
# Optional HTTP API & SSE (config/stonyperms.toml)
api_enabled = true
api_listen = "127.0.0.1:8737"
api_token_file = "config/stonyperms-api.token"
api_sse_enabled = true
# Public link origin for /sp editor when players use a hostname/reverse proxy
# api_editor_public_url = "https://perms.example.com"

# Opt-in write tier (keep false unless trusted store/bridge writers need remote mutations)
api_write_enabled = false
# api_write_token_file = "config/stonyperms-api-write.token"
```

Read the complete contract, endpoints, security rules, and token generation steps in [`docs/http-api.md`](docs/http-api.md).

---

## ⚙️ Configuration

Generated at `config/stonyperms.toml`:

```toml
default_group = "default"
storage_type = "json"
storage_dir = "config/stonyperms"
apply_gate_permissions = true
apply_server_contexts = true
enable_audit_log = true

api_enabled = false
api_listen = "127.0.0.1:8737"
api_token_file = "config/stonyperms-api.token"
api_allow_remote = false
api_tls_cert = ""
api_tls_key = ""
api_snapshot_ttl_seconds = 30
api_editor_public_url = ""

api_write_enabled = false
api_write_token_file = ""
api_rate_limit_per_sec = 60
api_rate_limit_burst = 120
api_write_rate_limit_per_sec = 15
api_write_rate_limit_burst = 30
api_sse_enabled = true
api_sse_max_clients = 32
```

---

## ⌨️ Commands

**Aliases:** `/sp`, `/stonyperms`, `/spb`, `/spv`, `/perm`, `/perms`

### General Commands

| Command | Description |
| :--- | :--- |
| `/sp info` | Show loaded counts, storage status, and verbose state |
| `/sp sync` / `/sp reload` | Hot-reload permissions data from disk |
| `/sp check <user> <permission> [context...]` | Trace an evaluated permission check |
| `/sp verbose <on \| off \| record> [filter]` | Stream live permission checks to chat/console |
| `/sp creategroup <group> [weight] [displayName]` | Create a new permission group |
| `/sp deletegroup <group>` | Delete an existing group |
| `/sp listgroups` | List all groups ordered by weight |
| `/sp createtrack <track>` | Create a new promotion track |
| `/sp deletetrack <track>` | Delete a promotion track |
| `/sp listtracks` | List all configured promotion tracks |

### Quick Examples

```text
/sp user Steve parent add admin
/sp user Alex parent addtemp vip 7d
/sp group default permission set gate.command.server true server=lobby
/sp group moderator meta setprefix 50 "&9[Mod] "
/sp user Steve promote ranks
```

<details>
<summary><strong>Full User, Group, Track & Audit Log CLI Reference</strong></summary>

#### User (`/sp user <user> ...`)
```text
/sp user <user> info
/sp user <user> permission set <node> [true/false] [context...]
/sp user <user> permission unset <node> [context...]
/sp user <user> permission settemp <node> <true/false> <duration> [context...]
/sp user <user> permission unsettemp <node> [context...]
/sp user <user> permission check <node>
/sp user <user> permission clear [context...]

/sp user <user> parent info
/sp user <user> parent set <group> [context...]
/sp user <user> parent add <group> [context...]
/sp user <user> parent remove <group> [context...]
/sp user <user> parent switchprimarygroup <group>
/sp user <user> parent addtemp <group> <duration> [context...]
/sp user <user> parent clear [context...]

/sp user <user> meta info
/sp user <user> meta set <key> <value>
/sp user <user> meta unset <key>
/sp user <user> meta setprefix [priority] <prefix>
/sp user <user> meta setsuffix [priority] <suffix>
/sp user <user> meta removeprefix <priority>
/sp user <user> meta removesuffix <priority>
/sp user <user> meta clear

/sp user <user> promote <track>
/sp user <user> demote <track>
/sp user <user> showtracks
/sp user <user> clone <target_user>
/sp user <user> clear
```

#### Group (`/sp group <group> ...`)
```text
/sp group <group> info
/sp group <group> setweight <weight>
/sp group <group> setdisplayname <displayName>
/sp group <group> rename <newName>
/sp group <group> clone <newName>
/sp group <group> listmembers
/sp group <group> showtracks
/sp group <group> clear

/sp group <group> permission <set|unset|settemp|unsettemp|check|clear> ...
/sp group <group> parent <info|add|set|remove|addtemp|clear> ...
/sp group <group> meta <info|set|unset|setprefix|setsuffix|removeprefix|removesuffix|clear> ...
```

#### Tracks & Audit Log
```text
/sp track <track> <info|append|insert|remove|rename|clear> ...
/sp log <recent|search|userhistory|grouphistory|trackhistory> ...
```

</details>

---

## 🔌 Go Plugin API

```go
if mgr := stonyperms.GetManager(); mgr != nil {
    user := mgr.GetUser(player.ID())
    if mgr.HasPermission(user, "myplugin.special", map[string]string{"server": "lobby"}) {
        // Allowed
    }
    prefix := mgr.GetPrefix(user, nil)
}
```

---

## 🌐 Minekube Gate Extensions Suite

| Project | Role | Stack |
| :--- | :--- | :--- |
| **[`gate-stonyperms`](https://github.com/andreisugu/gate-stonyperms)** *(this repo)* | LuckPerms-style permissions, contexts, tracks & v0.2 R/W HTTP + SSE API | `Go 1.26` |
| [`stonyperms-bridge`](https://github.com/andreisugu/stonyperms-bridge) | Paper & Fabric permission adapters, live SSE, `/spb` commands, Vault & PAPI | `Java 25` |
| [`gate-bettertab`](https://github.com/andreisugu/gate-bettertab) | Animated RGB / MiniMessage TAB lists, server groups, weights & placeholders | `Go 1.26` |
| [`gate-discord4gate`](https://github.com/andreisugu/gate-discord4gate) | Bidirectional Discord <> Minecraft chat, slash commands & webhook avatars | `Go 1.26` |
| [`gate-hybridforwarding`](https://github.com/andreisugu/gate-hybridforwarding) | Per-backend Velocity Modern, Legacy BungeeCord, BungeeGuard & None forwarding | `Go 1.26` |
| [`gate-simplewhitelist`](https://github.com/andreisugu/gate-simplewhitelist) | Lock-free `atomic.Pointer` whitelist by username, UUID, IP & CIDR | `Go 1.26` |
| [`gate-skinschanger`](https://github.com/andreisugu/gate-skinschanger) | Resilient multi-provider & Mineskin URL skin switcher with live backend refresh | `Go 1.26` |
| [`gate-smartlimbo`](https://github.com/andreisugu/gate-smartlimbo) | Limbo failover, protected sandbox & batched backend auto-reconnect queues | `Go 1.26` |
| [`gate-plugin-builder`](https://github.com/andreisugu/gate-plugin-builder) | Zero-config static Gate bundler (`build.sh`) + Pelican / Pterodactyl egg | `Shell` |

---

## 📄 License

Licensed under the [Apache License 2.0](LICENSE) © Andrei.
