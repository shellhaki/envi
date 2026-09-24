# Envi HTTP API

Every route the server serves, as registered by `Build` in
[internal/api/server.go](internal/api/server.go). 40 endpoints: 10 public, 30
behind authentication.

Base URL is `https://api.envisecrets.com`, or wherever you host it. Everything
speaks JSON.

## Authenticating

Protected routes go through `RequireAuth` in
[internal/api/middleware.go](internal/api/middleware.go), which accepts two
kinds of bearer token:

```
Authorization: Bearer <token>
```

- **A session token**, from an email login, the device flow, or a personal API
  key exchanged at `/auth/api-key`. Lasts 15 minutes; refresh at `/auth/refresh`.
  The request then acts as that user.
- **A service token**, minted per environment. It never expires unless you give
  it a TTL, and it can only reach the one environment it belongs to.

A session created from an API key carries that key's ceiling (`read`, `write` or
`manage`). A `read` ceiling refuses every non-`GET` request; a `write` ceiling
additionally refuses anything under `/api-keys`, `/invitations`,
`/service-tokens` or `/collaborators`, so a key cannot promote itself.

---

## Public

No credential required.

### `GET /health`

```json
{ "status": "ok" }
```

### `GET /config`

Whether this instance is in private beta. The dashboard reads it before anyone
has signed in, to show the right message.

```json
{ "beta": false }
```

Not to be confused with `GET /values`, which returns secrets.

### `POST /auth/request-otp`

Emails a six-digit login code, valid for 10 minutes.

```json
{ "email": "you@company.com" }
```

`202` on success. `403 not_invited` when a beta allow list is in force, `429
rate_limited` when asked too often.

### `POST /auth/verify-otp`

```json
{ "email": "you@company.com", "code": "123456" }
```

```json
{ "access_token": "...", "refresh_token": "...", "expires_in": 900 }
```

### `POST /auth/refresh`

Refresh tokens are single-use: this revokes the one you send and returns a new
pair. Two concurrent refreshes with the same token means one winner.

```json
{ "refresh_token": "..." }
```

### `POST /auth/logout`

```json
{ "refresh_token": "..." }
```

`204`. Ends the session, including its access token.

### `POST /auth/api-key`

Trades a personal API key for an ordinary session. The key is never a bearer
token itself.

```json
{ "key": "envi_..." }
```

```json
{ "access_token": "...", "refresh_token": "...", "expires_in": 900 }
```

### `POST /auth/device/code`

Starts the browser login the CLI uses.

```json
{
  "device_code": "...",
  "user_code": "WXYZ-ABCD",
  "verification_uri": "https://envisecrets.com/device",
  "verification_uri_complete": "https://envisecrets.com/device?code=WXYZ-ABCD",
  "expires_in": 600,
  "interval": 5
}
```

### `POST /auth/device/token`

The CLI polling. Returns a session once approved, or a `400` whose `code` says
why not: `authorization_pending`, `access_denied`, `expired_token`.

```json
{ "device_code": "..." }
```

### `GET /invitations/:token`

Shows what an invitation is for, before the recipient has an account.

```json
{ "email": "...", "project_name": "...", "environment_name": "...", "permission": "read", "expires_at": "..." }
```

---

## Account

### `GET /me`

```json
{ "ID": "...", "Email": "...", "OrganizationID": "..." }
```

Session tokens only — a service token has no user and gets a `500`.

### `POST /me/api-keys`

```json
{ "name": "laptop", "permission": "read", "ttl_seconds": 7776000 }
```

`permission` defaults to `read`. `ttl_seconds` omitted means 90 days; `0` never
expires.

```json
{ "id": "...", "name": "laptop", "permission": "read", "secret": "envi_...", "expires_at": "...", "created_at": "..." }
```

`secret` appears **only here**. Only a hash is stored.

### `GET /me/api-keys`

The caller's keys, newest first, without secrets. Revoked keys are omitted;
expired ones are kept so it is obvious why they stopped working.

### `DELETE /me/api-keys/:id`

`204`. Also ends every session that key created, so a leaked key stops being
useful immediately rather than when its session lapses.

### `POST /auth/device/approve`

The other half of the CLI login: the browser, where someone is already signed
in, approves the code the terminal is showing. The approving user is whoever
this request is authenticated as, which is who the CLI ends up logged in as.

```json
{ "user_code": "WXYZ-ABCD" }
```

`204`. `400 invalid_grant` if the code is unknown, already handled, or expired.
The code is normalised first, so `wxyz-abcd` and `WXYZ ABCD` both work.

### `POST /auth/device/deny`

```json
{ "user_code": "WXYZ-ABCD" }
```

`204`. The CLI stops polling with `access_denied`.

---

## Projects and environments

### `GET /projects`

Every project the caller can see, through org membership or a grant.

### `POST /projects`

```json
{ "org_id": "...", "name": "acme-api" }
```

`201`. `409 conflict` if the name is taken in that org.

### `DELETE /projects/:id`

`204`. Takes its environments, secrets, key-value entries and grants with it.
Owner or admin only.

### `GET /projects/:id/environments`

Ordered by creation, so the first is the project's default.

### `POST /projects/:id/environments`

```json
{ "name": "production", "is_production": true }
```

`is_production` is explicit, never inferred from the name. A production
environment refuses access through org membership alone — members need a grant.
Owners and admins still reach it by role, so whoever made it cannot be locked
out.

### `PATCH /environments/:id`

Same body. Renames, or changes the production flag. Owner or admin only.

### `DELETE /environments/:id`

`204`. Owner or admin only.

---

## Secrets

Per environment, versioned, with conflict detection. This is what `envi pull`
and `envi push` use.

### `GET /environments/:id/secrets`

```json
{ "DATABASE_URL": "postgres://...", "PORT": "8080" }
```

### `GET /environments/:id/secrets/snapshot`

The same values plus the revision, which `PUT .../snapshot` needs.

```json
{ "values": { "...": "..." }, "revision": 12 }
```

Logs one `secret.read` audit event for the environment, not one per key.

### `PUT /environments/:id/secrets`

Writes one or more keys without touching the revision.

```json
{ "API_KEY": "sk_live_..." }
```

### `PUT /environments/:id/secrets/snapshot`

```json
{ "values": { "...": "..." }, "expected_revision": 12 }
```

Compare-and-swap: if the environment has moved on since you read it, the write
is refused with `409 stale_revision` rather than overwriting someone else's
change.

Adds and updates the keys you send. It does **not** delete keys you leave out.

### `DELETE /environments/:id/secrets/:key`

`204`. A soft delete — the value stops appearing in reads, but its history stays.

---

## Key-value store

Project-wide, current value only, no history. Separate from secrets: if your
code writes it, it belongs here. This is what the SDK uses.

### `GET /kv?project=NAME`

```json
{ "project": "acme-api", "values": { "THEME": "dark" } }
```

`project` may be omitted with a service token, which names its own project.

### `PUT /kv`

```json
{ "project": "acme-api", "key": "THEME", "value": "dark" }
```

Replaces whatever was there. Keys up to 255 characters with no newlines or null
bytes; values up to 64KB. A read-only credential gets `403`.

### `DELETE /kv`

```json
{ "project": "acme-api", "key": "THEME" }
```

`204`. Deleting a key that was never set is not an error.

---

## Resolving by name

### `GET /values?project=NAME&environment=NAME`

Secrets for a project and environment named rather than identified by UUID, in
one request. Built for containers and the SDK, where nobody wants a UUID in
their configuration.

```json
{ "project": "acme-api", "environment": "production", "values": { "...": "..." }, "revision": 12 }
```

With a service token both parameters may be omitted — the token names its own
environment — and naming a *different* one returns `403` rather than quietly
serving its own. An unknown name returns `404` listing the names that do exist.

---

## Collaboration

### `POST /projects/:id/invitations`

```json
{ "email": "teammate@company.com", "environment_id": "...", "permission": "read", "ttl_seconds": 604800 }
```

`environment_id` may be empty to grant the whole project. Emails a link; the
response carries the token as a fallback if mail does not arrive.

### `POST /invitations/accept`

```json
{ "token": "..." }
```

The caller must be signed in as the invited address.

### `GET /projects/:id/collaborators`

Everyone with access, pending and active, with their permission.

### `DELETE /projects/:id/invitations/:invitationID`

`204`. Cancels a pending invitation.

### `DELETE /projects/:id/collaborators/:grantID`

`204`. Removes existing access.

---

## Service tokens

### `POST /projects/:id/environments/:env/service-tokens`

```json
{ "name": "ci", "permission": "read", "ttl_seconds": 0 }
```

`0` never expires. Returns the token value once; only a hash is stored. Requires
`manage` on that environment.

### `DELETE /service-tokens`

Revokes by value rather than id, so a script that holds a token can retire it
without knowing its id.

---

## Audit

### `GET /orgs/:id/audit-events`

The 200 most recent events, newest first. Org owners and admins only.

```json
[{ "action": "secret.read", "target_type": "environment", "target_id": "...", "actor": "you@company.com", "created_at": "...", "metadata": { "secrets": 12 } }]
```

Actions are `secret.read`, `secret.write` and `secret.delete`. Writes and
deletes name the exact secret; a read is one event for the environment with a
count, so a command run all day stays readable.

---

## Errors

Failures carry a machine-readable `code` and a human `error`:

```json
{ "code": "stale_revision", "error": "remote secrets changed; run envi diff or envi pull" }
```

| Code | Status | Means |
|---|---|---|
| `invalid_request` | 400 | Missing or malformed fields |
| `unauthenticated` | 401 | No valid credential |
| `invalid_key` | 401 | The API key is wrong, expired or revoked |
| `invalid_grant` | 400 | A device or invitation code is not usable |
| `forbidden` | 403 | Authenticated, but not permitted |
| `insufficient_permission` | 403 | An API key's ceiling refuses this |
| `not_invited` | 403 | Not on the beta allow list |
| `not_found` | 404 | No such thing |
| `project_not_found` | 404 | With the names that do exist |
| `environment_not_found` | 404 | With the names that do exist |
| `conflict` | 409 | A name is already taken |
| `stale_revision` | 409 | Someone wrote since you last read |
| `rate_limited` | 429 | Too many requests |
| `internal` / `server_error` | 500 | Our fault |

---

## A note on dead code

[internal/api/project.go](internal/api/project.go) defines a `Routes` method
registering all seven project endpoints **without** the auth middleware, beside
the `RoutesProtected` that `Build` actually calls. Nothing invokes it, so it is
harmless today, but wiring up the wrong method would expose project creation and
deletion to anyone. It should be deleted.
