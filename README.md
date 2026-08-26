# 💎 StonyPerms (`gate-stonyperms`)

> **Lightning-fast, ultra-deep LuckPerms-style permissions management engine for Minekube Gate Minecraft proxy.**

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/andreisugu/gate-stonyperms)](https://goreportcard.com/report/github.com/andreisugu/gate-stonyperms)
[![Platform](https://img.shields.io/badge/Platform-Gate%20Proxy-brightgreen.svg)](https://github.com/minekube/gate)

---

## ⚡ Overview

**StonyPerms** brings the power, flexibility, and intuitive command structure of **LuckPerms** to the **Minekube Gate proxy** ecosystem. It is built from the ground up in Go following [A Philosophy of Software Design](https://github.com/johnousterhout/aposd) and [The Pragmatic Programmer](https://pragprog.com) principles: deep modular architecture, atomic persistence, zero-allocation permission checks, dynamic context resolution, and full Brigodier auto-completions.

### 🌟 Key Features

* 🚀 **LuckPerms Command Compatibility**: Full `/sp` / `/stonyperms` command suite mirroring `/lp` syntax.
* 🌳 **Tree & Wildcard Resolution**: Supports exact nodes (`gate.command.server`), sub-wildcards (`gate.command.*`, `gate.*`), universal wildcards (`*`), and explicit denials (`false`).
* ⏳ **Temporary Permissions & Inheritances**: Time-limited permissions and group memberships with auto-expiration (`10m`, `2h`, `7d`, `30d`, `1y`).
* 🌐 **Dynamic Contexts**: Context-aware permissions (e.g. `server=lobby`, `world=hub`). Gate automatically passes the player's active backend server into the evaluation engine.
* 🛡️ **Weighted Group Inheritance**: Groups resolve in strict weight order (higher weight overrides lower weight). Infinite circular inheritance protection.
* 🎖️ **Promotion Tracks**: Configurable rank tracks (`default -> vip -> mod -> admin`) with `/sp user <user> promote <track>` and `demote`.
* 🎨 **Prefixes, Suffixes & Metadata**: Weighted prefix/suffix resolution and arbitrary custom metadata key-values.
* 📜 **Audit History Log**: Built-in action audit log tracking administrative changes (`/sp log recent`, `/sp log userhistory`).
* 🔍 **Live Verbose Diagnostic Engine**: Real-time permission check tracing to console or chat (`/sp verbose on`).
* 💾 **Atomic JSON Storage**: Zero-dependency, crash-resilient atomic file persistence in `config/stonyperms/`.

---

## ⌨️ Command Aliases

All commands can be executed using any of these aliases:
* `/sp` *(primary)*
* `/stonyperms`
* `/spb` *(BungeeCord style)*
* `/spv` *(Velocity style)*
* `/perm`
* `/perms`

---

## 📖 Command Reference

### 🛠️ General Commands

| Command | Description |
| :--- | :--- |
| `/sp` | Display StonyPerms version, status, and quick overview |
| `/sp info` | View detailed diagnostic statistics (loaded entities, storage health, verbose status) |
| `/sp sync` / `/sp reload` | Hot-reload all permissions data from disk |
| `/sp check <user> <permission> [context]` | Evaluate a permission for a user and print the full decision trace |
| `/sp verbose <on \| off \| record> [filter]` | Stream live permission checks to chat/console with source tracing |
| `/sp creategroup <group> [weight] [displayName]` | Create a new permission group |
| `/sp deletegroup <group>` | Delete an existing group |
| `/sp listgroups` | List all groups ordered by weight |
| `/sp createtrack <track>` | Create a new promotion track |
| `/sp deletetrack <track>` | Delete a promotion track |
| `/sp listtracks` | List all configured promotion tracks and their paths |

---

### 👤 User Commands (`/sp user <user> ...`)

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

---

### 👥 Group Commands (`/sp group <group> ...`)

```text
/sp group <group> info
/sp group <group> setweight <weight>
/sp group <group> setdisplayname <displayName>
/sp group <group> rename <newName>
/sp group <group> clone <newName>
/sp group <group> listmembers
/sp group <group> showtracks
/sp group <group> clear

/sp group <group> permission set <node> [true/false] [context...]
/sp group <group> permission unset <node> [context...]
/sp group <group> permission settemp <node> <true/false> <duration> [context...]
/sp group <group> permission unsettemp <node> [context...]
/sp group <group> permission check <node>
/sp group <group> permission clear

/sp group <group> parent info
/sp group <group> parent add <group> [context...]
/sp group <group> parent set <group> [context...]
/sp group <group> parent remove <group> [context...]
/sp group <group> parent addtemp <group> <duration> [context...]
/sp group <group> parent clear

/sp group <group> meta info
/sp group <group> meta set <key> <value>
/sp group <group> meta unset <key>
/sp group <group> meta setprefix [priority] <prefix>
/sp group <group> meta setsuffix [priority] <suffix>
/sp group <group> meta removeprefix <priority>
/sp group <group> meta removesuffix <priority>
/sp group <group> meta clear
```

---

### 🛤️ Track Commands (`/sp track <track> ...`)

```text
/sp track <track> info
/sp track <track> append <group>
/sp track <track> insert <group> <position>
/sp track <track> remove <group>
/sp track <track> rename <newName>
/sp track <track> clear
```

---

### 📜 Audit Log Commands (`/sp log ...`)

```text
/sp log recent [page]
/sp log search <query> [page]
/sp log userhistory <user> [page]
/sp log grouphistory <group> [page]
/sp log trackhistory <track> [page]
```

---

## 💡 Practical Examples

#### 1. Giving Admin Permissions to a User
```bash
/sp user Steve parent add admin
```

#### 2. Granting a Temporary VIP Rank for 7 Days
```bash
/sp user Alex parent addtemp vip 7d
```

#### 3. Creating a Group with Weight and Display Name
```bash
/sp creategroup moderator 50 "&9Moderator"
/sp group moderator parent add default
/sp group moderator permission set gate.command.kick true
/sp group moderator permission set gate.command.send true
/sp group moderator meta setprefix 50 "&9[Mod] "
```

#### 4. Setting Per-Server Context Permissions
```bash
# Allow /gate server only on the lobby backend server
/sp group default permission set gate.command.server true server=lobby
```

#### 5. Creating a Rank Track and Promoting
```bash
/sp createtrack ranks
/sp track ranks append default
/sp track ranks append vip
/sp track ranks append moderator
/sp track ranks append admin

/sp user Steve promote ranks
```

---

## ⚙️ Configuration (`config/stonyperms.toml`)

```toml
# The default group assigned to newly joined players
default_group = "default"

# Storage method to persist permissions data ("json")
storage_type = "json"

# Directory where permissions, users, groups, tracks, and logs are stored
storage_dir = "config/stonyperms"

# Automatically hook Gate's internal permission checks (/server, /send, etc.)
apply_gate_permissions = true

# Track player connected backend server as context (server=<name>)
apply_server_contexts = true

# Enable audit logging for administrative actions
enable_audit_log = true
```

---

## 🔌 Developer API (For Other Gate Plugins)

StonyPerms exposes a public global manager for third-party Gate plugins:

```go
import (
    "github.com/andreisugu/gate-stonyperms"
)

func CheckPlayer(player proxy.Player) {
    mgr := stonyperms.GetManager()
    if mgr == nil {
        return
    }

    user := mgr.GetUser(player.ID())
    if mgr.HasPermission(user, "myplugin.special", map[string]string{"server": "lobby"}) {
        // Player has permission
    }

    prefix := mgr.GetPrefix(user, nil)
    // Use prefix in chat or tablist
}
```

---

## 📦 Bundling with Gate

Add `github.com/andreisugu/gate-stonyperms` to your `plugins.txt` or bundle in `main.go`:

```go
package main

import (
    "go.minekube.com/gate/cmd/gate"
    "go.minekube.com/gate/pkg/edition/java/proxy"

    stonyperms "github.com/andreisugu/gate-stonyperms"
)

func main() {
    proxy.Plugins = append(proxy.Plugins,
        stonyperms.Plugin,
    )
    gate.Execute()
}
```

---

## 📄 License

Licensed under the **Apache License, Version 2.0**. See [`LICENSE`](LICENSE) for details.
