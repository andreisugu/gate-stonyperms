window.STONYPERMS_DEMO = {
  editor: {
    token: "demo",
    metadata: {
      commandAlias: "sp",
      actor: "DemoAdmin",
      targetType: "group",
      target: "vip",
      time: Date.now(),
      expiresAt: Date.now() + 7200000,
      defaultGroup: "default",
      revision: 42,
      liveSave: false,
      pluginVersion: "0.3.0"
    },
    tracks: [
      { name: "donors", groups: ["vip", "vipplus"] },
      { name: "staff", groups: ["helper", "mod", "admin", "operator"] }
    ],
    groups: [
      {
        name: "operator", displayName: "Operator", weight: 110,
        parents: [{ group: "admin" }],
        nodes: [{ key: "*", value: true }],
        prefixes: [{ priority: 110, value: "&4[OP] " }]
      },
      {
        name: "admin", displayName: "Admin", weight: 100,
        parents: [{ group: "mod" }],
        nodes: [
          { key: "stonyperms.*", value: true },
          { key: "gate.*", value: true },
          { key: "bettertab.admin", value: true }
        ],
        prefixes: [{ priority: 100, value: "&c[Admin] " }]
      },
      {
        name: "mod", displayName: "Mod", weight: 80,
        parents: [{ group: "helper" }],
        nodes: [
          { key: "minecraft.command.kick", value: true },
          { key: "minecraft.command.ban", value: true },
          { key: "gate.command.send", value: true }
        ],
        prefixes: [{ priority: 80, value: "&9[Mod] " }]
      },
      {
        name: "helper", displayName: "Helper", weight: 70,
        parents: [{ group: "vipplus" }],
        nodes: [
          { key: "gate.command.find", value: true },
          { key: "stonyperms.check", value: true }
        ],
        prefixes: [{ priority: 70, value: "&b[Helper] " }]
      },
      {
        name: "vipplus", displayName: "VIP+", weight: 40,
        parents: [{ group: "vip" }],
        nodes: [
          { key: "essentials.nick", value: true },
          { key: "skinschanger.url", value: true }
        ],
        prefixes: [{ priority: 40, value: "&a[VIP+] " }]
      },
      {
        name: "vip", displayName: "VIP", weight: 30,
        parents: [{ group: "default" }],
        nodes: [
          { key: "essentials.fly", value: true, contexts: { server: "hub" } },
          { key: "essentials.back", value: true },
          { key: "skinschanger.use", value: true }
        ],
        prefixes: [{ priority: 0, value: "&3[VIP] " }]
      },
      {
        name: "default", displayName: "default", weight: 10,
        parents: [],
        nodes: [
          { key: "gate.command.server", value: true },
          { key: "gate.command.glist", value: true },
          { key: "gate.command.ping", value: true }
        ],
        prefixes: [{ priority: 0, value: "&7" }]
      }
    ],
    users: [
      { uuid: "c1d60c50-70b5-4722-8057-000000000001", username: "RestlessStone", primaryGroup: "operator", parents: [{ group: "operator" }], nodes: [] },
      { uuid: "069a79f4-44e9-4726-a5be-fca90e38aaf5", username: "Notch", primaryGroup: "admin", parents: [{ group: "admin" }], nodes: [] },
      { uuid: "f84c6a79-0a4e-45e0-879b-cd49ebd4c4e2", username: "Dinnerbone", primaryGroup: "mod", parents: [{ group: "mod" }], nodes: [] },
      { uuid: "853c80ef-3c37-49fd-aa49-938b674adae6", username: "jeb_", primaryGroup: "helper", parents: [{ group: "helper" }], nodes: [] },
      { uuid: "61699b2e-d327-4a01-9f1e-0ea8c3f06bc6", username: "Larryy_", primaryGroup: "mod", parents: [{ group: "mod" }], nodes: [{ key: "essentials.fly", value: true, contexts: { server: "survival" } }] },
      { uuid: "e6b5c088-0680-44df-9e1b-9bf9dc792290", username: "Alex", primaryGroup: "vip", parents: [{ group: "vip" }], nodes: [] },
      { uuid: "8667ba71-b85a-4004-af54-457a9734eed7", username: "Steve", primaryGroup: "default", parents: [{ group: "default" }], nodes: [] }
    ],
    knownPermissions: [
      "*", "bettertab.admin", "bettertab.info", "bettertab.name", "bettertab.name.color",
      "bettertab.reload", "bettertab.reset", "bettertab.toggle",
      "bukkit.broadcast", "bukkit.broadcast.admin", "bukkit.broadcast.user",
      "bukkit.command.help", "bukkit.command.plugins", "bukkit.command.reload", "bukkit.command.version",
      "discord4gate.admin", "discord4gate.discord", "discord4gate.reload", "discord4gate.status",
      "essentials.back", "essentials.fly", "essentials.home", "essentials.nick", "essentials.tpa",
      "gate.*", "gate.command.alert", "gate.command.find", "gate.command.glist", "gate.command.ping", "gate.command.send", "gate.command.server",
      "hybridforwarding.admin", "hybridforwarding.reload",
      "minecraft.command.ban", "minecraft.command.gamemode", "minecraft.command.kick", "minecraft.command.teleport",
      "simplewhitelist.add", "simplewhitelist.admin", "simplewhitelist.bypass", "simplewhitelist.list", "simplewhitelist.reload", "simplewhitelist.remove",
      "skinschanger.admin", "skinschanger.set", "skinschanger.url", "skinschanger.use",
      "smartlimbo.admin", "smartlimbo.bypass", "smartlimbo.queue", "smartlimbo.status",
      "stonyperms.*", "stonyperms.admin", "stonyperms.applyedits", "stonyperms.check", "stonyperms.creategroup",
      "stonyperms.createtrack", "stonyperms.deletegroup", "stonyperms.deletetrack", "stonyperms.editor",
      "stonyperms.group", "stonyperms.info", "stonyperms.listgroups", "stonyperms.listtracks", "stonyperms.log",
      "stonyperms.sync", "stonyperms.track", "stonyperms.tree", "stonyperms.use", "stonyperms.user", "stonyperms.verbose",
      "stonyperms.bridge.admin", "stonyperms.bridge.applyedits", "stonyperms.bridge.editor", "stonyperms.bridge.info", "stonyperms.bridge.sync", "stonyperms.bridge.tree"
    ],
    potentialContexts: {
      server: ["hub", "lobby", "survival", "creative"],
      world: ["world", "world_nether", "world_the_end", "hub"]
    }
  },
  verbose: {
    token: "demo",
    metadata: {
      uploadedBy: "Console",
      commandAlias: "sp",
      startTime: Date.now() - 10000,
      endTime: Date.now(),
      durationMs: 10000,
      totalCount: 16,
      shownCount: 16,
      filter: "any",
      truncated: false
    },
    entries: [
      { subject: "RestlessStone", permission: "stonyperms.user.info", result: "true", source: "group:operator (*)", timestamp: Date.now() - 9500 },
      { subject: "Larryy_", permission: "stonyperms.editor", result: "false", source: "default:none", timestamp: Date.now() - 8800 },
      { subject: "Larryy_", permission: "gate.command.send", result: "true", source: "group:mod", timestamp: Date.now() - 8100 },
      { subject: "Alex", permission: "essentials.fly", result: "true", source: "group:vip (server=hub)", timestamp: Date.now() - 7400 },
      { subject: "Alex", permission: "stonyperms.track.info", result: "undefined", source: "none", timestamp: Date.now() - 6900 },
      { subject: "Steve", permission: "gate.command.server", result: "true", source: "group:default", timestamp: Date.now() - 6200 },
      { subject: "Steve", permission: "stonyperms.admin", result: "false", source: "none", timestamp: Date.now() - 5500 },
      { subject: "Console", permission: "stonyperms.log.recent", result: "true", source: "console", timestamp: Date.now() - 4800 },
      { subject: "Console", permission: "stonyperms.sync", result: "true", source: "console", timestamp: Date.now() - 4100 },
      { subject: "Console", permission: "stonyperms.tree", result: "true", source: "console", timestamp: Date.now() - 3200 },
      { subject: "Steve", permission: "skinschanger.url", result: "undefined", source: "none", timestamp: Date.now() - 2500 },
      { subject: "Larryy_", permission: "minecraft.command.kick", result: "true", source: "group:mod", timestamp: Date.now() - 1900 },
      { subject: "Alex", permission: "bettertab.admin", result: "false", source: "none", timestamp: Date.now() - 1200 },
      { subject: "Console", permission: "stonyperms.applyedits", result: "true", source: "console", timestamp: Date.now() - 700 },
      { subject: "Steve", permission: "gate.command.ping", result: "true", source: "group:default", timestamp: Date.now() - 400 },
      { subject: "Larryy_", permission: "stonyperms.verbose", result: "false", source: "none", timestamp: Date.now() - 100 }
    ]
  },
  tree: {
    token: "demo",
    metadata: {
      uploadedBy: "Console",
      commandAlias: "sp",
      time: Date.now(),
      root: ".",
      count: 64
    },
    permissions: []
  }
};
window.STONYPERMS_DEMO.tree.permissions = window.STONYPERMS_DEMO.editor.knownPermissions.slice();
window.STONYPERMS_DEMO.tree.metadata.count = window.STONYPERMS_DEMO.tree.permissions.length;
