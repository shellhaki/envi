# Envi API endpoints

Every route the API server registers, grouped by area. 63 in total.

All source paths are under `server/src/routes/`. Every route is registered in one place: `server/src/app.ts`.
`server/test/routes.test.ts` fails when this file and the routes stop matching.

## Middleware

| Name | Meaning |
|---|---|
| none | Public. No credential needed. |
| Auth | `signedIn` (`server/src/middleware.ts`). Accepts a user session (`Authorization: Bearer <access token>`) or a service token. Sets `user_id`, or `service_env` and the token permission for a service token. |
| Owner | Auth, plus the caller must own the personal org. Service tokens are refused. |
| Operator | Auth, plus the caller's email is in `ENVI_ADMINS` or the `operators` table (`operator`). Refusal is a 404, so the admin area looks like it does not exist. |
| Manage | Operator with the `admin` role (`canManage`). 404 on refusal. |
| Announce | Operator with the `admin` or `announcer` role (`canAnnounce`). 404 on refusal. |
| Instance admin | Auth, plus the caller is the `admin` in a self-hosted instance's users file (`instanceAdmin`, `server/src/routes/instance.ts`). 404 when the server signs in by email, 403 for anyone else. |
| Signature | No credential. Body must carry a valid `X-Bachs-Signature-V2` (HMAC-SHA256 over `t.body`, 5 minute tolerance). |

---

## Health

Source: `server/src/app.ts`

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| GET | `/health` | none | none | Liveness check. Returns `{"status":"ok"}`. |
| GET | `/instance` | none | none | How this server signs people in: `{mode, sign_in: "email" or "password", billing}`. The dashboard and CLI read it. |

## Sign-in (email code)

Source: `auth.ts`

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| POST | `/auth/request-otp` | none | body `{email}` | Emails a 6 digit login code. Rate limited. 404 `password_sign_in` on a self-hosted instance with a users file. |
| POST | `/auth/verify-otp` | none | body `{email, code}` | Checks the code, creates the account on first sign-in (and sends the welcome email), returns access and refresh tokens. 404 on a password instance. |
| POST | `/auth/refresh` | none | body `{refresh_token}` | Swaps a refresh token for a new pair. Refresh tokens are single use. |
| POST | `/auth/logout` | none | body `{refresh_token}` | Revokes the refresh token. |

## Sign-in (password, self-hosted)

Source: `instance.ts`. Only when `ENVI_USERS_FILE` is set; otherwise these answer 404.

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| POST | `/auth/password` | none | body `{email, password}` | Checks the password against the users file and returns access and refresh tokens. 401 `invalid_credentials` when wrong; 429 after 10 tries in 15 minutes. |
| POST | `/me/password` | Auth | body `{current_password, new_password}` | Changes your own password, written to the users file as a hash. Needs the current one; at least 10 characters. |

## CLI sign-in (device flow)

Source: `auth.ts`

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| POST | `/auth/device/code` | none | none | Starts a CLI login. Returns a device code, a user code and the URL to approve it in the browser. |
| POST | `/auth/device/token` | none | body `{device_code}` | The CLI polls this. Returns pending, denied, expired, or the tokens once approved. |
| POST | `/auth/device/approve` | Auth | body `{user_code}` | The signed-in browser approves a waiting CLI. |
| POST | `/auth/device/deny` | Auth | body `{user_code}` | The signed-in browser rejects a waiting CLI. |

## API keys

Source: `auth.ts` (exchange) and `account.ts`

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| POST | `/auth/api-key` | none | body `{key}` | Exchanges a personal API key for a session (access and refresh tokens), capped at the key's permission. |
| POST | `/me/api-keys` | Auth | body `{name, permission, ttl_seconds}` | Creates an API key. The raw key is shown once. |
| GET | `/me/api-keys` | Auth | none | Lists your API keys (no raw values). |
| DELETE | `/me/api-keys/:id` | Auth | path `id` | Revokes an API key. |

## Account

Source: `account.ts`

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| GET | `/me` | Auth | none | The signed-in user, their personal org and `Role` (`admin` or `contributor` on a self-hosted instance, empty otherwise). Needs a user session; a service token has no user. |

## Projects and environments

Source: `projects.ts`

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| POST | `/projects` | Auth | body `{org_id, name}` | Creates a project with one default environment. On a self-hosted instance with a users file, only the admin may. |
| GET | `/projects` | Auth | none | Projects you own or were invited to. |
| DELETE | `/projects/:id` | Auth | path `id` | Deletes a project and everything in it. Owner only. |
| POST | `/projects/:id/environments` | Auth | path `id`, body `{name, is_production}` | Adds an environment. |
| GET | `/projects/:id/environments` | Auth | path `id` | Lists the environments you can see in the project. |
| PATCH | `/environments/:id` | Auth | path `id`, body `{name, is_production}` | Renames an environment or flips its production flag. |
| DELETE | `/environments/:id` | Auth | path `id` | Deletes an environment and its secrets. |

## Secrets

Source: `secrets.ts`

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| GET | `/environments/:id/secrets` | Auth | path `id` | Lists secret keys and values for the environment. |
| GET | `/environments/:id/secrets/snapshot` | Auth | path `id` | All values as `{KEY: value}` plus the current `revision`. This is what `envi run` and `envi pull` read. |
| PUT | `/environments/:id/secrets` | Auth | path `id`, body `{KEY: value, ...}` | Sets or updates the given keys. Others are left alone. |
| PUT | `/environments/:id/secrets/snapshot` | Auth | path `id`, body `{values, expected_revision}` | Writes every given key in one write; keys left out are kept. Returns 409 `stale_revision` if someone else wrote first. This is what `envi push` uses. |
| DELETE | `/environments/:id/secrets/:key` | Auth | path `id`, `key` | Deletes one secret. |

A service token only reaches its own environment (`service_env`). A read-only service token or key cannot use the PUT and DELETE routes. Every read and write is recorded in the audit log.

## Lookup by name

Source: `secrets.ts`

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| GET | `/values` | Auth | query `project`, `environment` | Values for an environment found by project and environment name instead of ids. A service token ignores the names and gets its own environment. |
| GET | `/kv` | Auth | query `project` | Lists the project-level key-value entries. |
| PUT | `/kv` | Auth | body `{project, key, value}` | Sets one key-value entry. |
| DELETE | `/kv` | Auth | body `{project, key}` | Deletes one key-value entry. |

## Service tokens

Source: `sharing.ts`

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| POST | `/projects/:id/environments/:env/service-tokens` | Auth | path `id`, `env`, body `{name, permission, ttl_seconds}` | Creates a token bound to one environment, for CI and servers. The raw token is shown once. |
| DELETE | `/service-tokens` | Auth | header `X-Service-Token` | Revokes the token passed in the header. 404 if it is not yours. |

## Invitations and collaborators

Source: `sharing.ts`

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| GET | `/invitations/:token` | none | path `token` | Preview of an invitation (project, inviter, environment) for the accept page. |
| POST | `/projects/:id/invitations` | Auth | path `id`, body `{email, environment_id, permission, ttl_seconds}` | Invites someone by email to one environment. Returns 402 if billing refuses the extra member. |
| POST | `/invitations/accept` | Auth | body `{token}` | Accepts an invitation and creates the access grant. Can also return 402. |
| GET | `/projects/:id/collaborators` | Auth | path `id` | People with access to the project, and pending invitations. |
| POST | `/projects/:id/collaborators` | Auth | path `id`, body `{user_id, permission, environment_id}` | Self-hosted only: gives a user of this instance the project (or one environment of it) at `read`, `write` or `manage`, replacing what they had there. Needs manage on the project. 404 on an email-sign-in server. |
| DELETE | `/projects/:id/invitations/:invitationID` | Auth | path `id`, `invitationID` | Cancels a pending invitation. |
| DELETE | `/projects/:id/collaborators/:grantID` | Auth | path `id`, `grantID` | Removes someone's access. |

## Instance users (self-hosted)

Source: `instance.ts`. The people in the users file. Nothing is emailed: a new or reset password is in the reply, once.

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| GET | `/instance/users` | Instance admin | none | Everyone who can sign in, with their role and the projects each one has been given. |
| POST | `/instance/users` | Instance admin | body `{email}` | Adds a contributor and returns `{id, email, password}` with a generated password. |
| POST | `/instance/users/:id/password` | Instance admin | path `id` | Gives the user a new generated password, returned once. |
| DELETE | `/instance/users/:id` | Instance admin | path `id` | Removes a contributor: they can't sign in, their sessions and API keys stop working, and their project access is removed. The admin can't be removed. |

## Audit log

Source: `account.ts`

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| GET | `/orgs/:id/audit-events` | Auth | path `id` | Recent audit events for the org: who read or changed what, and when. |

## Billing

Source: `billing.ts`. All `/billing` routes act on the caller's personal org.

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| GET | `/billing` | Owner | none | Plan, currency, billable members this month, amount due, card on file, failed charges. |
| POST | `/billing/payg` | Owner | body `{currency}` (`USD` or `NGN`) | Starts pay as you go. Returns a Bachs checkout URL that saves a card. |
| POST | `/billing/team` | Owner | none | Starts the flat Team plan. Returns a Bachs subscription checkout URL. |
| POST | `/billing/cancel` | Owner | none | Cancels at the end of the period. |
| POST | `/billing/portal` | Owner | none | Returns a Bachs customer portal URL to manage the card. |
| POST | `/billing/retry` | Owner | none | Retries a failed charge now instead of waiting for the next attempt. |
| POST | `/webhooks/bachs` | Signature | raw body | Bachs payment events. Records the plan and charges. Duplicate events are ignored. |

## Operator (admin area)

Source: `admin.ts`

| Method | Path | Middleware | Input | What it does |
|---|---|---|---|---|
| GET | `/admin/whoami` | Operator | none | Your operator role. The dashboard uses it to show or hide the admin link. |
| GET | `/admin/stats` | Manage | none | Live numbers: users, projects, secrets, revenue, expenses, recent activity. |
| POST | `/admin/expenses` | Manage | body `{amount_minor, currency, category, description, spent_on, recurring}` | Records an expense. |
| DELETE | `/admin/expenses/:id` | Manage | path `id` | Deletes an expense. |
| GET | `/admin/operators` | Manage | none | Lists operators and their roles. |
| POST | `/admin/operators` | Manage | body `{email, name, role}` (`admin` or `announcer`) | Adds an operator. |
| DELETE | `/admin/operators/:id` | Manage | path `id` | Removes an operator. People in `ENVI_ADMINS` cannot be removed here. |
| GET | `/admin/announcements` | Announce | none | Past announcements. |
| POST | `/admin/announcements` | Announce | body `{subject, body, confirm: true}` | Emails an announcement to every user. `confirm` must be true. |

---

## Notes

- Error bodies use one shape: `{"code": "...", "error": "..."}`.
- Common codes: 400 bad input, 401 bad or expired credential, 402 billing refused, 403 not allowed, 404 not found (also used to hide the admin area), 409 stale revision.
