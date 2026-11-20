# StonyPerms read-only HTTP API (v1)

The optional API exposes StonyPerms' **evaluated** user and group data so a
Paper plugin or Fabric mod can act as a server-side permission provider. It is
strictly read-only: there is no endpoint that creates, updates, or deletes a
user, group, track, or log entry.

The first consumer is
[`andreisugu/stonyperms-bridge`](https://github.com/andreisugu/stonyperms-bridge).

## Why an API instead of a shared database

- StonyPerms already owns inheritance, weights, wildcards, contexts, expiry,
  prefixes, and audit state. Re-implementing or reading its JSON storage
  directly would fork that logic and risk write contention.
- The proxy must remain authoritative. Backends get a resolved snapshot, not a
  second source of truth.
- A read-only HTTP contract is language-agnostic, testable, and easy to put
  behind a private network or TLS.

## Enabling

`config/stonyperms.toml`:

```toml
api_enabled = true
api_listen = "127.0.0.1:8737"
api_token_file = "config/stonyperms-api.token"
api_allow_remote = false
api_tls_cert = ""
api_tls_key = ""
api_snapshot_ttl_seconds = 30
```

Create the token file with a random value of at least 32 characters and mode
`0600`:

```bash
openssl rand -hex 32 > config/stonyperms-api.token
chmod 600 config/stonyperms-api.token
```

Resolution order for the token:

1. `STONYPERMS_API_TOKEN` environment variable
2. `api_token_file`
3. inline `api_token`

Prefer the environment variable or the file. A missing/short token disables the
API; Gate and local StonyPerms enforcement keep running.

By default the API refuses non-loopback bind addresses. For a backend on
another host, bind a tailnet/WireGuard address (or use a reverse proxy) and set
`api_allow_remote = true`. TLS is strongly recommended off-loopback.

## Authentication

Every `/v1/*` request needs:

```http
Authorization: Bearer <token>
```

`/healthz` is unauthenticated and returns only `{"status":"ok"}`. Tokens are
compared in constant time and are never written to logs or response bodies.
There is no CORS support, so a browser cannot read the API.

## Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/healthz` | Liveness only, no auth |
| GET | `/v1/health` | Status, schema version, revision |
| GET | `/v1/revision` | Cheap change counter for polling |
| GET | `/v1/players/{uuid}?server=lobby&world=hub` | Resolved snapshot for one context |
| POST | `/v1/check` | Authoritative single-permission check |
| GET | `/v1/groups` | All groups (read-only copies) |
| GET | `/v1/groups/{name}` | One group |

### GET /v1/players/{uuid}

All query parameters are treated as StonyPerms contexts. `server` and `world`
are conventional; the engine supports arbitrary context keys. Up to 16
contexts, keys 1–32 characters, values up to 128 bytes.

Unknown UUIDs are resolved synthetically against `default_group` without being
persisted. This matches first-login semantics and avoids a write on every
backend request. `user.synthetic` is `true` in that case.

```bash
curl -s \
  -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:8737/v1/players/069a79f4-44e9-4726-a5be-fca90e38aaf5?server=lobby&world=hub"
```

```json
{
  "schemaVersion": 1,
  "revision": 42,
  "generatedAt": "2026-11-20T12:00:00Z",
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

Node ordering is part of the contract:

1. `user` nodes come first.
2. Then groups in the order they appear in `groups` (descending weight, stable).
3. Within one source, an exact key beats a wildcard; the longest matching
   wildcard beats a shorter one.

That is the same order as `engine.Evaluate` in this repository. Nodes that are
expired or do not match the requested contexts are omitted. `expiry` is Unix
seconds and omitted for permanent nodes.

`ETag` is `"sp-<revision>"`. Send `If-None-Match` to get `304 Not Modified`.
Any user/group change increments the revision. Snapshots are context-specific;
include the context in your cache key.

### POST /v1/check

```bash
curl -s \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"uuid":"069a79f4-44e9-4726-a5be-fca90e38aaf5","permission":"example.node","contexts":{"server":"lobby"}}' \
  http://127.0.0.1:8737/v1/check
```

```json
{"value":"true","source":"group:admin:wildcard(*)","revision":42}
```

`value` is `true`, `false`, or `undefined`. `undefined` means StonyPerms has no
opinion and the caller should fall back to its platform default.

Unknown fields are rejected with `400`. Maximum body size is 64 KiB.

### GET /v1/groups

Returns full group records (nodes, parents, metadata, prefixes, suffixes) as
read-only copies. Useful for admin tooling and diagnostics. Consumers should
use `/v1/players/{uuid}` for permission decisions, not reimplement inheritance
from this endpoint.

## Errors

```json
{"error":"unauthorized","message":"a valid bearer token is required"}
```

| Status | `error` |
| --- | --- |
| 400 | `invalid_uuid`, `invalid_context`, `invalid_body`, `invalid_permission`, `invalid_group` |
| 401 | `unauthorized` |
| 404 | `group_not_found` |
| 405 | Method not allowed (ServeMux) |
| 500 | Internal errors are logged server-side, never returned with details |

## Security properties

- Read-only: no mutating routes, no audit-log exposure.
- Loopback by default; non-loopback requires explicit `api_allow_remote`.
- Constant-time bearer comparison, 16-character minimum, 32+ recommended.
- Request limits: 5 s header, 10 s read, 15 s write, 16 KiB headers, 64 KiB
  JSON body.
- No CORS, no query-string tokens, no token logging.
- TLS optional via `api_tls_cert` / `api_tls_key`.
- The API returns permission data only; it never returns storage contents,
  audit logs, or configuration.

## Versioning

`schemaVersion` is currently `1`. Additive fields may appear within v1; clients
must ignore unknown fields. A breaking change gets `/v2` and a new schema
version.

## Limitations

- Snapshots are point-in-time and context-specific. A backend that needs
  another context (for example a region or dimension) must request it.
- Temporary nodes expire on the proxy's cleanup tick and are also filtered at
  snapshot time, so a snapshot can go stale exactly at an expiry boundary.
- The API is not a general-purpose admin API. Mutations stay on the Gate
  command surface.
