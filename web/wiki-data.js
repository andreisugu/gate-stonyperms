window.STONYPERMS_WIKI = [
  {
    category: "HOME",
    slug: "home",
    title: "Home",
    html: `
      <h1>StonyPerms Documentation</h1>
      <p><strong>StonyPerms</strong> is a lightweight, high-performance permissions engine built natively for <strong>Minekube Gate</strong>, paired with <strong>StonyPermsBridge</strong> for Paper and Fabric backend servers and <strong>StonyPermsWeb</strong> for visual permission management.</p>
      <h2>Highlights</h2>
      <ul>
        <li>Weighted group inheritance trees with cycle detection</li>
        <li>Promotion &amp; demotion progression tracks (<code>donors</code>, <code>staff</code>) with clone &amp; web editor links</li>
        <li>Server &amp; custom context conditions (<code>server=lobby</code>, <code>world=hub</code>) with specificity tie-breaking</li>
        <li>Shorthand patterns (<code>{a,b}</code>, <code>(a|b)</code>, <code>{1-N}</code>) and regex permission matching (<code>r=...</code>)</li>
        <li>Read/Write HTTP API (<code>/v1/*</code>) with SSE live invalidation &amp; idempotency keys</li>
        <li>Built-in <strong>StonyPermsWeb</strong> UI: Web Editor, Verbose Viewer &amp; Permission Tree</li>
      </ul>
      <pre><code>/sp editor          # Launch live web permissions editor
/sp verbose record  # Record evaluated checks, then /sp verbose paste
/sp tree            # Inspect all known permissions as a collapsible tree</code></pre>
    `
  },
  {
    category: "GUIDES",
    slug: "installation",
    title: "Installation",
    html: `
      <h1>Installation</h1>
      <p>Run <code>gate-stonyperms</code> inside your custom Minekube Gate proxy binary and optionally install <code>stonyperms-bridge</code> on your Paper or Fabric backend servers.</p>
      <h2>1. Minekube Gate</h2>
      <pre><code>go get github.com/andreisugu/gate-stonyperms@latest</code></pre>
      <p>Register <code>stonyperms.Plugin</code> in your Gate proxy <code>Plugins</code> slice and start Gate once to generate <code>config/stonyperms.toml</code>.</p>
      <h2>2. Backend Servers (StonyPermsBridge)</h2>
      <p>Drop the Paper/Folia shaded JAR into <code>plugins/</code> or the Fabric mod JAR into <code>mods/</code>, then point <code>config.yml</code> at your Gate HTTP endpoint (e.g. <code>http://127.0.0.1:8737</code>) with matching read/write bearer tokens.</p>
    `
  },
  {
    category: "GUIDES",
    slug: "getting-started",
    title: "Getting Started",
    html: `
      <h1>Getting Started</h1>
      <p>StonyPerms ships with a built-in <code>default</code> group assigned automatically to new players.</p>
      <h2>Quick Group Setup</h2>
      <pre><code>/sp creategroup vip 30 &amp;3[VIP]
/sp group vip parent add default
/sp group vip permission set essentials.fly true server=hub
/sp group vip meta setprefix 30 "&amp;3[VIP] "
/sp user RestlessStone parent set vip</code></pre>
      <p>Or run <code>/sp editor</code> to manage everything visually in StonyPermsWeb.</p>
    `
  },
  {
    category: "SETUP",
    slug: "configuration",
    title: "Configuration",
    html: `
      <h1>Configuration (<code>config/stonyperms.toml</code>)</h1>
      <table>
        <thead><tr><th>Key</th><th>Default</th><th>Description</th></tr></thead>
        <tbody>
          <tr><td><code>default_group</code></td><td><code>"default"</code></td><td>Fallback group for players with no stored data</td></tr>
          <tr><td><code>primary_group_calculation</code></td><td><code>"stored"</code></td><td>Primary group resolution strategy: <code>stored</code>, <code>parents-by-weight</code>, or <code>all-parents-by-weight</code></td></tr>
          <tr><td><code>storage_dir</code></td><td><code>"plugins/stonyperms/data"</code></td><td>Directory holding groups, tracks, users JSON files</td></tr>
          <tr><td><code>api_enabled</code></td><td><code>false</code></td><td>Enables HTTP API &amp; embedded StonyPermsWeb on <code>api_listen</code></td></tr>
          <tr><td><code>api_listen</code></td><td><code>"127.0.0.1:8737"</code></td><td>TCP bind address for HTTP API + Web UI</td></tr>
          <tr><td><code>api_write_enabled</code></td><td><code>false</code></td><td>Enables mutating <code>/v1/*</code> endpoints for Bridge/webhooks</td></tr>
          <tr><td><code>api_editor_public_url</code></td><td><code>""</code></td><td>Public origin advertised by <code>/sp editor</code> (reverse proxy or GitHub Pages)</td></tr>
          <tr><td><code>api_sse_enabled</code></td><td><code>true</code></td><td>Streams live revision events at <code>GET /v1/events</code></td></tr>
          <tr><td><code>prefix_stacking</code></td><td><code>false</code></td><td>Concatenates active prefixes across groups/users by priority instead of single highest prefix</td></tr>
          <tr><td><code>prefix_start_spacer</code> / <code>prefix_middle_spacer</code> / <code>prefix_end_spacer</code></td><td><code>""</code></td><td>Spacers wrapped around and inserted between stacked prefixes</td></tr>
          <tr><td><code>suffix_stacking</code></td><td><code>false</code></td><td>Concatenates active suffixes by priority instead of single highest suffix</td></tr>
          <tr><td><code>suffix_start_spacer</code> / <code>suffix_middle_spacer</code> / <code>suffix_end_spacer</code></td><td><code>""</code></td><td>Spacers wrapped around and inserted between stacked suffixes</td></tr>
          <tr><td><code>stack_deduplicate</code></td><td><code>true</code></td><td>Deduplicates identical values when stacking prefixes or suffixes</td></tr>
        </tbody>
      </table>
    `
  },
  {
    category: "SETUP",
    slug: "storage-types",
    title: "Storage Types",
    html: `
      <h1>Storage Types</h1>
      <p>StonyPerms uses human-readable JSON flat files (<code>groups.json</code>, <code>tracks.json</code>, and per-UUID user files under <code>users/</code>) cached in memory with copy-on-write snapshot isolation and atomic writes.</p>
      <p>Reload external disk edits at any time using <code>/sp sync</code>.</p>
    `
  },
  {
    category: "COMMANDS",
    slug: "general-commands",
    title: "General Commands",
    html: `
      <h1>General Commands</h1>
      <table>
        <thead><tr><th>Command</th><th>Permission</th><th>Description</th></tr></thead>
        <tbody>
          <tr><td><code>/sp info</code></td><td><code>stonyperms.info</code></td><td>Show loaded user, group, track &amp; revision stats</td></tr>
          <tr><td><code>/sp sync</code> / <code>/sp networksync</code></td><td><code>stonyperms.sync</code></td><td>Reload data from storage &amp; broadcast live SSE update</td></tr>
          <tr><td><code>/sp search &lt;perm|group.&lt;name&gt;&gt; [page]</code></td><td><code>stonyperms.search</code></td><td>Search users &amp; groups holding a permission or parent group</td></tr>
          <tr><td><code>/sp bulkupdate &lt;scope&gt; &lt;update|delete&gt; ...</code></td><td><code>stonyperms.bulkupdate</code></td><td>Preview (60s <code>confirm &lt;code&gt;</code>) or <code>--confirm</code> bulk edit/delete nodes</td></tr>
          <tr><td><code>/sp export &lt;file&gt;</code></td><td><code>stonyperms.export</code></td><td>Export all groups, tracks &amp; users to JSON or <code>.json.gz</code></td></tr>
          <tr><td><code>/sp import &lt;file&gt; [--replace]</code></td><td><code>stonyperms.import</code></td><td>Restore or merge groups, tracks &amp; users from a snapshot file</td></tr>
          <tr><td><code>/sp check &lt;user&gt; &lt;perm&gt; [ctx]</code></td><td><code>stonyperms.check</code></td><td>Trace permission result &amp; inheritance source</td></tr>
          <tr><td><code>/sp editor [filter]</code></td><td><code>stonyperms.editor</code></td><td>Open StonyPermsWeb editor session</td></tr>
          <tr><td><code>/sp applyedits &lt;code&gt;</code></td><td><code>stonyperms.applyedits</code></td><td>Apply portable <code>sp1_...</code> or short <code>sp_...</code> edit code</td></tr>
          <tr><td><code>/sp verbose &lt;on|off|record|paste&gt;</code></td><td><code>stonyperms.verbose</code></td><td>Stream live checks or upload recording to Verbose Viewer</td></tr>
          <tr><td><code>/sp tree [root]</code></td><td><code>stonyperms.tree</code></td><td>Open registered permission tree in Permission Tree Viewer</td></tr>
        </tbody>
      </table>
    `
  },
  {
    category: "COMMANDS",
    slug: "user-commands",
    title: "User Commands",
    html: `
      <h1>User Commands</h1>
      <pre><code>/sp user &lt;user|uuid&gt; info
/sp user &lt;user&gt; editor
/sp user &lt;user&gt; permission info [page]
/sp user &lt;user&gt; permission set &lt;node&gt; [true|false] [context...]
/sp user &lt;user&gt; permission settemp &lt;node&gt; [true|false] &lt;dur&gt; [accumulate|replace|deny] [ctx...]
/sp user &lt;user&gt; permission unsettemp &lt;node&gt; [context...]
/sp user &lt;user&gt; parent &lt;add|set|remove|switchprimarygroup&gt; &lt;group&gt; [context...]
/sp user &lt;user&gt; parent addtemp &lt;group&gt; &lt;dur&gt; [accumulate|replace|deny] [context...]
/sp user &lt;user&gt; parent removetemp &lt;group&gt; [context...]
/sp user &lt;user&gt; parent &lt;settrack|cleartrack&gt; &lt;track&gt; [group] [context...]
/sp user &lt;user&gt; promote &lt;track&gt;
/sp user &lt;user&gt; demote &lt;track&gt;
/sp user &lt;user&gt; meta &lt;set|unset&gt; &lt;key&gt; [value] [context...]
/sp user &lt;user&gt; meta settemp &lt;key&gt; &lt;val&gt; &lt;dur&gt; [accumulate|replace|deny] [context...]
/sp user &lt;user&gt; meta &lt;addprefix|setprefix|addsuffix|setsuffix&gt; &lt;prio&gt; "&lt;val&gt;" [ctx...]
/sp user &lt;user&gt; meta &lt;addtempprefix|settempprefix&gt; &lt;prio&gt; "&lt;val&gt;" &lt;dur&gt; [mod] [ctx...]</code></pre>
    `
  },
  {
    category: "COMMANDS",
    slug: "group-commands",
    title: "Group Commands",
    html: `
      <h1>Group Commands</h1>
      <pre><code>/sp creategroup &lt;group&gt; [weight] [displayName]
/sp group &lt;group&gt; editor
/sp group &lt;group&gt; setweight &lt;weight&gt;
/sp group &lt;group&gt; setdisplayname &lt;name&gt;
/sp group &lt;group&gt; permission &lt;info|set|unset|settemp|unsettemp|check|clear&gt; ...
/sp group &lt;group&gt; parent &lt;add|set|remove|addtemp|removetemp|settrack|cleartrack&gt; ...
/sp group &lt;group&gt; meta &lt;info|set|unset|settemp|addprefix|setprefix|...&gt; ...
/sp group &lt;group&gt; listmembers</code></pre>
    `
  },
  {
    category: "COMMANDS",
    slug: "track-commands",
    title: "Track Commands",
    html: `
      <h1>Track Commands</h1>
      <pre><code>/sp createtrack &lt;track&gt;
/sp track &lt;track&gt; info
/sp track &lt;track&gt; editor
/sp track &lt;track&gt; clone &lt;newName&gt;
/sp track &lt;track&gt; append &lt;group&gt;
/sp track &lt;track&gt; insert &lt;group&gt; &lt;1-based-pos&gt;
/sp track &lt;track&gt; remove &lt;group&gt;
/sp track &lt;track&gt; rename &lt;newName&gt;
/sp track &lt;track&gt; clear</code></pre>
    `
  },
  {
    category: "COMMANDS",
    slug: "log-commands",
    title: "Log Commands",
    html: `
      <h1>Audit Log Commands</h1>
      <pre><code>/sp log recent [page]
/sp log search &lt;query&gt; [page]
/sp log userhistory &lt;user&gt; [page]
/sp log grouphistory &lt;group&gt; [page]
/sp log trackhistory &lt;track&gt; [page]</code></pre>
    `
  },
  {
    category: "FEATURES",
    slug: "matching-and-specificity",
    title: "Matching & Specificity",
    html: `
      <h1>Permission Matching &amp; Context Specificity</h1>
      <p>StonyPerms evaluates user nodes first, followed by inherited groups in descending weight order. Within any source, exact matches beat wildcards/patterns, and ties break by context specificity (<code>len(contexts)</code>) before explicit deny (<code>false</code>) overrides:</p>
      <ul>
        <li><strong>Exact &amp; Wildcards:</strong> <code>essentials.fly</code>, <code>essentials.*</code>, and global <code>*</code>.</li>
        <li><strong>Shorthand Patterns:</strong> <code>essentials.{home,warp}</code>, <code>plugin.(kick|ban)</code>, and numeric ranges <code>homes.{1-10}</code> (up to 128 expansions).</li>
        <li><strong>Regex Matching:</strong> Prefix any node key with <code>r=</code> for case-insensitive regular expressions, e.g. <code>r=arena\.\d+\.join</code>.</li>
      </ul>
    `
  },
  {
    category: "FEATURES",
    slug: "prefixes-suffixes-meta",
    title: "Prefixes, Suffixes & Meta",
    html: `
      <h1>Prefixes, Suffixes &amp; Meta</h1>
      <p>Prefixes, suffixes, and custom metadata support contexts (<code>server=lobby</code>) and expiring temporary entries (<code>accumulate</code>, <code>replace</code>, or <code>deny</code>). With <code>prefix_stacking</code> or <code>suffix_stacking</code> enabled, StonyPerms joins active affixes in descending priority order using configurable start/middle/end spacers and optional deduplication, while <code>setprefix</code> replaces prior entries in matching contexts and <code>addprefix</code> retains them. Legacy color codes (<code>&amp;3[VIP] </code>) are preserved directly for Gate, BetterTab, and Bridge chat/Vault adapters.</p>
    `
  },
  {
    category: "FEATURES",
    slug: "context",
    title: "Contexts",
    html: `
      <h1>Contextual Permissions</h1>
      <p>Restrict permissions, inheritance, custom metadata keys, prefixes, or suffixes to specific servers or worlds using key-value contexts like <code>server=lobby</code> or <code>world=world_nether</code>. Nodes with more matching context pairs take precedence over broader global nodes within the same entity.</p>
    `
  },
  {
    category: "FEATURES",
    slug: "tracks",
    title: "Promotion Tracks",
    html: `
      <h1>Promotion Tracks</h1>
      <p>Tracks define ordered promotion paths such as <code>default -&gt; helper -&gt; mod -&gt; admin</code>. Both <code>/sp user &lt;u&gt; promote &lt;track&gt;</code> and <code>POST /v1/users/{id}/promote</code> enforce atomic transitions. You can also assign or clear explicit group positions along a track via <code>settrack</code> / <code>cleartrack</code>, clone tracks with <code>/sp track &lt;t&gt; clone &lt;new&gt;</code>, or jump straight into the visual editor with <code>/sp track &lt;t&gt; editor</code>.</p>
    `
  },
  {
    category: "WEB SUITE",
    slug: "web-editor",
    title: "Web Editor",
    html: `
      <h1>Web Permissions Editor</h1>
      <p>Launch from Gate (<code>/sp editor</code>) or backend servers (<code>/spb editor</code>), or jump straight into a target with <code>/sp user &lt;name&gt; editor</code>, <code>/sp group &lt;name&gt; editor</code>, or <code>/sp track &lt;name&gt; editor</code>.</p>
      <ul>
        <li><strong>Live Color &amp; Inherited Inspector:</strong> Preview resolved chat/tab prefixes in real time and inspect inherited permissions from parent groups with one-click overrides.</li>
        <li><strong>Live Save (<code>Ctrl+S</code>):</strong> When Gate HTTP API is reachable, clicking <strong>Save</strong> applies changes immediately via <code>POST /v1/editor/sessions/{token}/save</code> and broadcasts SSE updates across backends.</li>
        <li><strong>Portable Offline Mode:</strong> Generates a compact gzipped <code>sp1_...</code> code you can apply in game via <code>/sp applyedits &lt;code&gt;</code> or <code>/spb applyedits &lt;code&gt;</code>.</li>
      </ul>
    `
  },
  {
    category: "WEB SUITE",
    slug: "verbose",
    title: "Verbose Viewer",
    html: `
      <h1>Verbose Recording &amp; Viewer</h1>
      <p>Silently record live permission checks without flooding chat, then open them in the filterable web viewer:</p>
      <pre><code>/sp verbose record [target] [filter]
... perform checks or reproduce bug ...
/sp verbose paste</code></pre>
      <p>Filter results by permission substring, user/subject, or exclude <code>true</code> / <code>false</code> / <code>undefined</code> results, inspect evaluation traces, and download JSON captures.</p>
    `
  },
  {
    category: "WEB SUITE",
    slug: "tree",
    title: "Permission Tree",
    html: `
      <h1>Permission Tree Viewer</h1>
      <p>Build an interactive hierarchy of all known Gate proxy permissions, stored nodes, and backend Paper/Bukkit/Fabric registered nodes:</p>
      <pre><code>/sp tree [prefix]
/spb tree [prefix]</code></pre>
    `
  },
  {
    category: "DEVELOPERS",
    slug: "http-api",
    title: "HTTP API v0.2",
    html: `
      <h1>HTTP API Reference</h1>
      <ul>
        <li><strong>Read endpoints (read bearer token):</strong> <code>GET /v1/health</code>, <code>GET /v1/players/{uuid}</code>, <code>POST /v1/check</code>, <code>GET /v1/search?q=...</code>, <code>GET /v1/users</code>, <code>GET /v1/groups</code>, <code>GET /v1/tracks</code>, <code>GET /v1/logs</code>, <code>GET /v1/events</code> (SSE)</li>
        <li><strong>Write endpoints (write bearer token + <code>api_write_enabled</code>):</strong> Full CRUD for users, groups, tracks, promote/demote, <code>POST /v1/bulkupdate</code>, track set/clear routes, temp modifiers (<code>accumulate|replace|deny</code>) &amp; <code>tempOnly</code> deletes, plus <code>POST /v1/editor/sessions</code>, <code>POST /v1/editor/applyedits</code>, <code>POST /v1/verbose/sessions</code>, <code>POST /v1/tree/sessions</code></li>
        <li><strong>Web UI Session endpoints (ephemeral token + CORS):</strong> <code>GET /v1/editor/sessions/{token}</code>, <code>POST /v1/editor/sessions/{token}/save</code>, <code>GET /v1/verbose/sessions/{token}</code>, <code>GET /v1/tree/sessions/{token}</code></li>
      </ul>
    `
  },
  {
    category: "DEVELOPERS",
    slug: "stonyperms-bridge",
    title: "StonyPermsBridge",
    html: `
      <h1>StonyPermsBridge (Paper &amp; Fabric)</h1>
      <p>Installs directly onto Paper/Folia and Fabric backends to hook native permission lookups, Vault prefixes/suffixes, PlaceholderAPI placeholders (<code>%stonyperms_*%</code>), and forward <code>/spb</code> commands (including <code>search</code>, <code>bulkupdate</code>, <code>sync</code>, <code>editor</code>, <code>applyedits</code>, and <code>tree</code>) directly to Gate's authoritative HTTP API.</p>
    `
  }
];
