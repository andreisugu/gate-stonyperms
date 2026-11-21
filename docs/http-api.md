# StonyPerms HTTP API (v0.2 / Schema v1)

The optional HTTP API exposes StonyPerms' **evaluated** permission snapshots, live change events, and opt-in cross-server read/write management so Paper plugins, Fabric mods, web services, and store delivery agents can integrate with StonyPerms on Gate as the single source of truth.

First-party consumers:

- [`andreisugu/stonyperms-bridge`](https://github.com/andreisugu/stonyperms-bridge) (Paper & Fabric adapters + `/spb` + `StonyPermsBridgeAPI` + Vault / PlaceholderAPI)
- Chaonius web profile/stats & PayNow store fulfillment

## Design principles

- **Proxy-authoritative:** StonyPerms on Gate continues to own users, groups, tracks, weights, inheritance, wildcards, contexts, expiry, affixes, metadata, and the audit log. Backends never read or share Gate's JSON storage directly.
- **Schema v1 compatible:** All v0.1 read contracts (`schemaVersion = 1`) remain intact while v0.2 adds Server-Sent Events (`GET /v1/events`), batch snapshots (`POST /v1/players/batch`), user/track queries, per-IP rate limiting, multi-token rotation, and scoped fail-closed writes.
- **Fail-closed write tier:** Write and audit-log endpoints are disabled by default (`api_write_enabled = false`) and require a distinct bearer token tier that never falls back to read tokens.

---

## Enabling & Configuration (`config/stonyperms.toml`)

```toml
# Read-tier HTTP API
api_enabled = true
api_listen = "127.0.0.1:8737"
api_token = ""
api_token_file = "config/stonyperms-api.token"
api_allow_remote = false
api_tls_cert = ""
api_tls_key = ""
api_snapshot_ttl_seconds = 30

# Optional Write/Admin tier (disabled by default; must use distinct token(s))
api_write_enabled = false
api_write_token = ""
api_write_token_file = ""

# Per-IP token-bucket rate limits (set < 0 to disable; 0 uses defaults shown)
api_rate_limit_per_sec = 60
api_rate_limit_burst = 120
api_write_rate_limit_per_sec = 15
api_write_rate_limit_burst = 30

# Live SSE change stream
api_sse_enabled = true
api_sse_max_clients = 32
```

### Generating Token Files (`0600`)

Create separate random tokens (at least 16 characters; 32+ bytes hex recommended):

```bash
umask 077
openssl rand -hex 32 > config/stonyperms-api.token
openssl rand -hex 32 > config/stonyperms-api-write.token
chmod 600 config/stonyperms-api.token config/stonyperms-api-write.token
```

Token resolution precedence per tier:

- **Read tier:** `STONYPERMS_API_TOKEN` env var -> `api_token_file` -> inline `api_token`.
- **Write tier:** `STONYPERMS_API_WRITE_TOKEN` env var -> `api_write_token_file` -> inline `api_write_token`.

Startup failure semantics:

- Missing/short/unreadable read tokens log an error and prevent the HTTP API from starting while Gate and local `/sp` enforcement continue unaffected.
- When `api_write_enabled = true`, any failure loading write tokens logs an error and **disables the write tier (`writeEnabled = false`)** — it never falls back to read tokens.
- `api.New` rejects overlapping read and write tokens (`"api: write tokens must be distinct from read tokens"`).
- Non-loopback `api_listen` addresses are rejected unless `api_allow_remote = true`. Prefer loopback or a WireGuard/Tailscale address with `api_tls_cert` / `api_tls_key` (or TLS reverse proxy) off-loopback.

---

## Authentication, Scopes & Multi-Token Rotation

All `/v1/*` routes require:

```http
Authorization: Bearer <token>
```

`GET /healthz` is the sole unauthenticated endpoint and returns `{"status":"ok"}`. Every HTTP response includes `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, and `Cache-Control: no-store` (SSE sets `Cache-Control: no-cache` instead). CORS headers are never emitted.

### Dual-Tier Scope Enforcement

| Tier | Token Config | Allowed Routes |
| --- | --- | --- |
| **Read** | `api_token*` / `STONYPERMS_API_TOKEN` | All `GET /v1/*` read routes (except `/v1/logs`), `POST /v1/players/batch`, `POST /v1/check`, `GET /v1/events` |
| **Write / Admin** | `api_write_token*` / `STONYPERMS_API_WRITE_TOKEN` (only when `api_write_enabled = true`) | All write routes (`POST`/`PUT`/`PATCH`/`DELETE` on users/groups/tracks, `POST /v1/reload`), `GET /v1/logs`, **plus** all Read-tier routes |

Error responses for auth & scope failures:

- Missing, malformed, or unrecognized bearer token -> `401 Unauthorized` (`{"error":"unauthorized"}` + `WWW-Authenticate: Bearer realm="stonyperms"`).
- Valid token hitting a write/logs route when `api_write_enabled = false` -> `403 Forbidden` (`{"error":"writes_disabled"}`).
- Read-only token hitting a write route or `GET /v1/logs` when `api_write_enabled = true` -> `403 Forbidden` (`{"error":"insufficient_scope"}`).

### Zero-Downtime Multi-Token Rotation

`LoadTokens` accepts up to **4 newline-separated tokens** (`maxTokensPerTier = 4`) per tier in env vars, token files, or inline strings:

- Blank lines and `#` comment lines are ignored.
- Duplicate tokens within the same tier are deduplicated; every loaded token must be at least 16 characters.
- Constant-time verification checks all active tier tokens (`subtle.ConstantTimeCompare`).

Zero-downtime rotation flow:

1. Append the new token as a second line in `config/stonyperms-api.token` (or `stonyperms-api-write.token`), leaving the existing token intact.
2. Restart/reload Gate so it accepts both tokens.
3. Roll out the new token to consumers and restart them.
4. Remove the old token line from the Gate token file and reload/restart Gate.

> **Important `stonyperms-bridge` compatibility note:** While `gate-stonyperms` supports up to 4 newline-separated tokens in its token files, `stonyperms-bridge` (Paper and Fabric) currently reads each backend token file as a **single-line trimmed string**. Never copy a multi-line proxy rotation file directly onto a Paper or Fabric backend; give each bridge backend its own **single-token `0600` file** (for example, rotating a backend onto a second active line staged on the proxy, then pruning the old line on the proxy).

---

## Per-IP Rate Limiting & Server Timeouts

- Requests to `/v1/*` are rate-limited per client IP extracted from `r.RemoteAddr` (`net.SplitHostPort`, fallback raw `RemoteAddr`; `X-Forwarded-For` is intentionally ignored).
- Read tier default: **60 req/s**, burst **120** (`api_rate_limit_per_sec`, `api_rate_limit_burst`).
- Write tier default: **15 req/s**, burst **30** (`api_write_rate_limit_per_sec`, `api_write_rate_limit_burst`). Note: `GET /v1/logs` and `POST /v1/reload` are classified under write-tier routing/auth and write rate limits.
- Exceeded limit returns `429 Too Many Requests` with `Retry-After: 1` and `{"error":"rate_limited","message":"rate limit exceeded; retry shortly"}`.
- Limiter state tracks up to `4096` IPs with `5m` stale-bucket eviction.
- HTTP server hardening: `ReadHeaderTimeout: 5s`, `ReadTimeout: 10s`, `WriteTimeout: 15s` (cleared per connection for SSE streams via `http.ResponseController.SetWriteDeadline`), `IdleTimeout: 60s`, `MaxHeaderBytes: 16 KiB`, `maxRequestBodyBytes: 64 KiB`.

---

## Live Change Stream (`GET /v1/events`)

Requires read-tier auth (write tokens also accepted).

- Negotiated headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`.
- If `api_sse_enabled = false` -> `503 Service Unavailable` (`{"error":"sse_disabled"}`).
- If active subscribers reach `api_sse_max_clients` (default `32`) -> `503 Service Unavailable` (`{"error":"sse_capacity_exceeded"}`).
- Immediately on connect, the server emits:

```text
event: connected
data: {"revision":42,"generation":"1763640000000000000-1"}

```

- Every `25s` of inactivity, the server sends `: keepalive\n\n`.
- Each subscriber has a 64-event non-blocking buffer; slow consumers drop overflow events so Gate mutations never stall.

### `ChangeEvent` Event Types & Payload

| `event:` Name | Emitted When |
| --- | --- |
| `user_updated` | User created/ensured, parent/primaryGroup/perm/meta/affix/promote/demote/clear/batch modified |
| `user_deleted` | Stored user deleted (`DELETE /v1/users/{id}`) or purged |
| `group_updated` | Group created/patched, or group perm/parent/meta/affix modified |
| `group_deleted` | Group deleted (`DELETE /v1/groups/{name}`) |
| `track_updated` | Track created or updated |
| `track_deleted` | Track deleted |
| `reload` | `POST /v1/reload` or proxy `/sp reload` |
| `expiry_cleanup` | Background expiry ticker removed expired nodes/inheritances |

Payload JSON:

```json
{
  "revision": 43,
  "type": "user_updated",
  "uuid": "069a79f4-44e9-4726-a5be-fca90e38aaf5",
  "username": "Notch",
  "group": "vip",
  "track": "",
  "actor": "chaonius-store",
  "timestamp": 1763726400
}
```

Optional string fields (`uuid`, `username`, `group`, `track`, `actor`) use `omitempty`. Clients can subscribe to `/v1/events` for near-instant invalidation while retaining `/v1/players/{uuid}` + `ETag` polling as a self-healing fallback across reconnects or dropped events.

---

## Read Endpoints (Read Tier or Write Tier)

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | Unauthenticated liveness probe (`{"status":"ok"}`) |
| `GET` | `/v1/health` | Status, `schemaVersion`, `revision`, `writeEnabled`, `sseEnabled` |
| `GET` | `/v1/revision` | Monotonic change revision counter |
| `GET` | `/v1/events` | Server-Sent Events stream of `ChangeEvent`s |
| `GET` | `/v1/players/{uuid}?<contexts>` | Evaluated effective snapshot with `ETag` / `If-None-Match` (`304`) |
| `POST` | `/v1/players/batch` | Batch evaluated snapshots for up to 100 UUIDs |
| `POST` | `/v1/check` | Authoritative single-permission evaluation |
| `GET` | `/v1/users` | Paginated stored user summaries (`?group=&limit=&offset=`) |
| `GET` | `/v1/users/by-name/{username}` | Lookup stored user by username, or evaluated snapshot with `?effective=true` |
| `GET` | `/v1/users/{id}` | Lookup stored user record by UUID or username |
| `GET` | `/v1/groups` | List all group records (read-only copies) |
| `GET` | `/v1/groups/{name}` | Fetch one group record |
| `GET` | `/v1/groups/{name}/members` | List direct members of a group |
| `GET` | `/v1/tracks` | List all promotion/demotion tracks |
| `GET` | `/v1/tracks/{name}` | Fetch one track |

### `GET /v1/health` & `GET /v1/revision`

```json
{
  "status": "ok",
  "schemaVersion": 1,
  "revision": 42,
  "writeEnabled": false,
  "sseEnabled": true
}
```

`GET /v1/revision` returns `{"revision":42}`.

### `GET /v1/players/{uuid}?server=lobby&world=hub`

All query parameters are evaluated as StonyPerms contexts (up to 16 entries, keys matching `^[a-z0-9_-]{1,32}$`, UTF-8 values <= 128 bytes with no control chars `< 0x20` or `0x7f`). Unknown UUIDs resolve synthetically against `default_group` (`"synthetic": true`) without persisting to storage.

Response headers include `ETag: "sp-<generation>-<revision>-<fingerprint>"` and `X-StonyPerms-Revision: <revision>`. Sending `If-None-Match` returns `304 Not Modified` when the effective snapshot state (including expiry) is unchanged.

```json
{
  "schemaVersion": 1,
  "revision": 42,
  "generatedAt": "2026-11-21T12:00:00Z",
  "ttlSeconds": 30,
  "user": {
    "uuid": "069a79f4-44e9-4726-a5be-fca90e38aaf5",
    "username": "Notch",
    "primaryGroup": "default",
    "synthetic": false,
    "meta": {"rank": "gold"},
    "prefix": "&aVIP ",
    "suffix": ""
  },
  "groups": [
    {"name": "admin", "displayName": "&cAdmin", "weight": 100},
    {"name": "default", "displayName": "&7Member", "weight": 0}
  ],
  "nodes": [
    {"key": "*", "value": true, "source": "group:admin", "weight": 100, "user": false},
    {"key": "gate.command.server", "value": true, "source": "group:default", "weight": 0, "user": false},
    {"key": "example.lobby.only", "value": true, "contexts": {"server": "lobby"}, "source": "user", "weight": 0, "user": true}
  ]
}
```

Node precedence contract (`engine.Evaluate` parity):
1. Direct `user` nodes (`user: true`) first.
2. Inherited `groups` in array order (descending weight, stable).
3. Within each source, exact keys beat wildcards, and the longest matching wildcard wins.

### `POST /v1/players/batch`

Evaluates effective snapshots for **1 to 100** valid non-nil UUIDs in one request. Sets `X-StonyPerms-Revision`.

Request:

```json
{
  "uuids": [
    "069a79f4-44e9-4726-a5be-fca90e38aaf5",
    "f51d0bc4-8018-4c5c-8852-98aed2807587"
  ],
  "contexts": {"server": "survival"}
}
```

Response (`200 OK`):

```json
{
  "schemaVersion": 1,
  "revision": 42,
  "players": {
    "069a79f4-44e9-4726-a5be-fca90e38aaf5": { "...": "snapshotResponse" },
    "f51d0bc4-8018-4c5c-8852-98aed2807587": { "...": "snapshotResponse" }
  }
}
```

### `POST /v1/check`

Request: `{"uuid":"...","permission":"example.node","contexts":{"server":"lobby"}}`

Response (`200 OK`): `{"value":"true","source":"group:admin:wildcard(*)","revision":42}` where `value` is `"true"`, `"false"`, or `"undefined"`.

### Users, Groups & Tracks Read Endpoints

- **`GET /v1/users?group=<name>&limit=<1..200, default 50>&offset=<>=0>`**
  Returns `{revision, total, limit, offset, users}` where each item in `users` is `{uuid, username, primaryGroup, parents?, nodeCount}`.
- **`GET /v1/users/by-name/{username}`**
  - Default: resolves a stored user by username -> `{revision, user}` (`404 user_not_found` if absent).
  - With `?effective=true` (remaining query parameters treated as contexts): returns an evaluated `snapshotResponse` with `ETag` / `If-None-Match` (`304`) and `X-StonyPerms-Revision`, or `404 user_not_found` if the username is unknown.
- **`GET /v1/users/{id}`**
  Accepts a UUID or stored username -> `{revision, user}` (`404 user_not_found`).
- **`GET /v1/groups`**, **`GET /v1/groups/{name}`**
  Returns `{revision, groups}` or `{revision, group}` (`404 group_not_found`).
- **`GET /v1/groups/{name}/members`**
  Returns `{revision, group, members}` where each entry is `{uuid, username, primaryGroup, permanent, expiry?, contexts?}`.
- **`GET /v1/tracks`**, **`GET /v1/tracks/{name}`**
  Returns `{revision, tracks}` or `{revision, track}` (`404 track_not_found`).

---

## Write & Admin Endpoints (Write Tier, `api_write_enabled = true`)

All endpoints below require a valid **write-tier** bearer token (`401 unauthorized`, `403 writes_disabled`, or `403 insufficient_scope`).

### Request Headers

- **`X-StonyPerms-Actor`** *(optional, default `"api"`)*: 1–64 printable ASCII characters (`0x20..0x7e`; control characters or > 64 bytes return `400 invalid_actor`). Recorded in StonyPerms' audit log and SSE events.
- **`Idempotency-Key`** *(optional on all mutating `POST`/`PUT`/`PATCH`/`DELETE` routes; ignored on `GET /v1/logs`)*: 1–128 printable ASCII characters (`0x20..0x7e`, else `400 invalid_idempotency_key`).
  - Cached in memory for **15 minutes** (up to **4,096 entries**), keyed by `Method + " " + Path + "\x00" + Key`.
  - Computes a SHA-256 request digest over **canonicalized sorted query parameters (`url.ParseQuery`)** + `\0` + raw request body.
  - Reusing the same `Idempotency-Key` on the same method/path with a **different** query string or body returns `409 Conflict` (`{"error":"idempotency_conflict"}`).
  - Matching retries return the exact cached HTTP status and JSON body with header `X-Idempotency-Replayed: true`. `5xx` responses are never cached. Concurrent identical in-flight requests block until the first finishes.

### Subject Resolution Rules (Fail-Closed)

- `POST /v1/users` takes `{"uuid":"...","username":"...","primaryGroup":"...","createOffline":false}`:
  - With a valid non-nil `uuid`: returns existing or newly created user (`200 OK` or `201 Created` with `{revision, created, user}`), optionally updating `username` and `primaryGroup`.
  - Without `uuid`: requires `username` **and** `createOffline: true` to synthesize a deterministic v3 offline UUID (if `username` does not already exist). Without `createOffline: true`, omitting `uuid` fails with `400 invalid_uuid`.
- All `/v1/users/{id}/...` mutation endpoints accept a valid non-nil UUID (auto-creating a persisted user record on first write) **or an existing stored username**. Unknown usernames **fail closed with `404 user_not_found`** — silent offline-UUID synthesis is disabled.

### Complete Write & Admin Route Reference

#### User Endpoints

| Method & Path | Body / Query | Response (`200` unless noted) |
| --- | --- | --- |
| `POST /v1/users` | `{"uuid":"...","username":"...","primaryGroup":"...","createOffline":false}` | `201`/`200` `{revision, created, user}` |
| `DELETE /v1/users/{id}` | — | `{revision, deleted: true, uuid}` |
| `POST /v1/users/{id}/parents` | `{"group":"vip","mode":"add"\|"set"\|"accumulate","durationSeconds":2592000,"expiry":0,"contexts":{},"setPrimary":false}` | `{revision, user}` |
| `DELETE /v1/users/{id}/parents/{group}` | Contexts via JSON `{"contexts":{...}}` or query params `?<contexts>` | `{revision, user}` (`404 parent_not_found`) |
| `PUT /v1/users/{id}/primary-group` | `{"group":"vip"}` | `{revision, user}` |
| `POST /v1/users/{id}/permissions` | `{"key":"node","value":true,"durationSeconds":0,"expiry":0,"contexts":{}}` (`value` defaults `true`) | `{revision, user}` |
| `DELETE /v1/users/{id}/permissions` | JSON `{"key":"node","contexts":{}}` or query `?key=node&<contexts>` | `{revision, user}` (`404 node_not_found`) |
| `PUT /v1/users/{id}/meta/{key}` | `{"value":"..."}` | `{revision, user}` |
| `DELETE /v1/users/{id}/meta/{key}` | — | `{revision, user}` (`404 meta_not_found`) |
| `POST /v1/users/{id}/prefixes` | `{"priority":100,"value":"&a[VIP] ","durationSeconds":0,"expiry":0,"contexts":{}}` | `{revision, user}` |
| `DELETE /v1/users/{id}/prefixes/{priority}` | Integer `{priority}` in path | `{revision, user}` (`404 prefix_not_found`) |
| `POST /v1/users/{id}/suffixes` | `{"priority":100,"value":" &7*","durationSeconds":0,"expiry":0,"contexts":{}}` | `{revision, user}` |
| `DELETE /v1/users/{id}/suffixes/{priority}` | Integer `{priority}` in path | `{revision, user}` (`404 suffix_not_found`) |
| `POST /v1/users/{id}/promote` | `{"track":"ranks"}` | `{revision, fromGroup, toGroup, user}` |
| `POST /v1/users/{id}/demote` | `{"track":"ranks"}` | `{revision, fromGroup, toGroup, user}` |
| `POST /v1/users/{id}/clear` | `{"scope":"all"\|"permissions"\|"parents"\|"meta"}` (empty defaults `"all"`) | `{revision, user}` |
| `POST /v1/users/{id}/batch` | `{"operations":[...]}` (1–32 ops executed atomically under one lock) | `{revision, applied, user}` |

Parent assignment `mode` behavior:

- **`add`** *(default)*: adds/updates parent `(group, contexts)` with the resolved expiry; if `setPrimary: true`, updates `primaryGroup`.
- **`set`**: replaces all existing parents with `[group]` and sets `primaryGroup = group`.
- **`accumulate`**: stacks `durationSeconds` onto an existing matching `(group, contexts)` parent if unexpired (`existing.expiry > now`) or preserves `0` if already permanent; expired/missing parents start at `now + durationSeconds`. Explicit `expiry > 0` is rejected with `400 invalid_expiry`.

Supported `POST /v1/users/{id}/batch` operation names (`op`):
`parent_add`, `parent_set`, `parent_accumulate`, `parent_remove`, `primary_group_set`, `permission_set`, `permission_unset`, `meta_set`, `meta_unset`, `prefix_set`, `prefix_remove`, `suffix_set`, `suffix_remove`.

#### Group Endpoints

| Method & Path | Body / Query | Response (`200` unless noted) |
| --- | --- | --- |
| `POST /v1/groups` | `{"name":"vip","weight":50,"displayName":"&aVIP"}` | `201` `{revision, group}` (`409 group_exists`) |
| `PATCH /v1/groups/{name}` | `{"weight":60,"displayName":"&aVIP+"}` (at least one field required) | `{revision, group}` |
| `DELETE /v1/groups/{name}` | — | `{revision, deleted: true, group}` (`400 cannot_delete_default`) |
| `POST /v1/groups/{name}/permissions` | `{"key":"node","value":true,"durationSeconds":0,"expiry":0,"contexts":{}}` | `{revision, group}` |
| `DELETE /v1/groups/{name}/permissions` | JSON `{"key":"...","contexts":{}}` or query `?key=...&<contexts>` | `{revision, group}` |
| `POST /v1/groups/{name}/parents` | `{"group":"default","mode":"add"\|"set"\|"accumulate",...}` | `{revision, group}` |
| `DELETE /v1/groups/{name}/parents` / `/{parent}` | Path `{parent}`, `?group=`, or JSON `{"group":"...","contexts":{}}` | `{revision, group}` |
| `PUT /v1/groups/{name}/meta/{key}` | `{"value":"..."}` | `{revision, group}` |
| `DELETE /v1/groups/{name}/meta/{key}` | — | `{revision, group}` |
| `POST /v1/groups/{name}/prefixes` | `{"priority":50,"value":"&a[VIP] ","durationSeconds":0,"expiry":0,"contexts":{}}` | `{revision, group}` |
| `DELETE /v1/groups/{name}/prefixes` / `/{priority}` | Path `{priority}`, `?priority=`, or JSON `{"priority":50}` | `{revision, group}` |
| `POST /v1/groups/{name}/suffixes` | `{"priority":50,"value":"...","durationSeconds":0,"expiry":0,"contexts":{}}` | `{revision, group}` |
| `DELETE /v1/groups/{name}/suffixes` / `/{priority}` | Path `{priority}`, `?priority=`, or JSON `{"priority":50}` | `{revision, group}` |

#### Track & Admin Endpoints

| Method & Path | Query / Body | Response (`200` unless noted) |
| --- | --- | --- |
| `POST /v1/tracks` | `{"name":"staff","groups":["helper","mod","admin"]}` | `201` `{revision, track}` (`409 track_exists`) |
| `PUT /v1/tracks/{name}` | `{"groups":["helper","mod","admin"]}` | `{revision, track}` (`404 track_not_found`) |
| `DELETE /v1/tracks/{name}` | — | `{revision, deleted: true, track}` |
| `GET /v1/logs` | `?limit=50` (`1..1000`, default `50`), `&targetType=`, `&target=`, `&actor=` | `{revision, logs}` (write-tier auth; never cached by `Idempotency-Key`) |
| `POST /v1/reload` | Empty body | `{revision, reloaded: true}` |

---

## Validation Bounds & Error Reference

### Input Limits

| Input | Constraint |
| --- | --- |
| Request body | Max `65536` bytes (64 KiB), unknown JSON fields rejected (`DisallowUnknownFields`) |
| Group / track name | Regex `^[a-z0-9_+-]{1,64}$` (lowercased) |
| Meta key | Regex `^[a-zA-Z0-9._:-]{1,64}$` |
| Meta value | `<= 1024` UTF-8 bytes, no NUL byte (`0x00`) |
| Affix (`prefix` / `suffix`) & `displayName` | `<= 256` UTF-8 bytes, no control chars `< 0x20` or `0x7f`; priority must be finite integer |
| Permission key | 1–256 printable non-whitespace ASCII bytes (`0x21..0x7e`), lowercased |
| Contexts | Max `16` entries; keys `^[a-z0-9_-]{1,32}$`; values `<= 128` UTF-8 bytes, no control chars `< 0x20` or `0x7f` |
| Duration / Expiry | `durationSeconds` in `[0, 315360000]` (max 10 years); cannot supply both `durationSeconds > 0` and `expiry > 0`; `expiry` must be in the future if `> 0` |
| Username | Non-empty, `<= 64` printable ASCII (`0x21..0x7e`) |
| `X-StonyPerms-Actor` | `1..64` printable ASCII (`0x20..0x7e`) |
| `Idempotency-Key` | `1..128` printable ASCII (`0x20..0x7e`) |
| Batch operations | `POST /v1/players/batch`: `1..100` UUIDs; `POST /v1/users/{id}/batch`: `1..32` operations |
| Multi-token files | Max `4` tokens per tier, each `>= 16` chars |

### JSON Error Envelope & Codes

```json
{"error":"insufficient_scope","message":"write token required for this endpoint"}
```

| HTTP Status | `error` Codes |
| --- | --- |
| `400 Bad Request` | `invalid_body`, `invalid_uuid`, `invalid_username`, `invalid_user`, `invalid_group`, `invalid_parent`, `invalid_permission`, `invalid_context`, `invalid_mode`, `invalid_expiry`, `invalid_meta_key`, `invalid_meta_value`, `invalid_priority`, `invalid_affix`, `invalid_track`, `invalid_scope`, `invalid_batch`, `invalid_query`, `invalid_actor`, `invalid_idempotency_key`, `cannot_delete_default` |
| `401 Unauthorized` | `unauthorized` |
| `403 Forbidden` | `writes_disabled`, `insufficient_scope` |
| `404 Not Found` | `user_not_found`, `group_not_found`, `track_not_found`, `parent_not_found`, `node_not_found`, `meta_not_found`, `prefix_not_found`, `suffix_not_found` |
| `405 Method Not Allowed` | Method not registered for route (`ServeMux`) |
| `409 Conflict` | `idempotency_conflict`, `group_exists`, `track_exists`, `track_conflict` |
| `429 Too Many Requests` | `rate_limited` (includes `Retry-After: 1`) |
| `500 Internal Server Error` | `internal_error` (details logged server-side only) |
| `503 Service Unavailable` | `sse_disabled`, `sse_capacity_exceeded` |
