# Envi on Cloudflare Workers

A second implementation of the Envi API, on Hono + Workers + D1 + KV. The Go
server in the repository root remains the reference; this is an alternative
deploy target that speaks the same wire protocol, so the same CLI and dashboard
work against either.

## Why the storage is split the way it is

| Concern | Where | Why |
|---|---|---|
| All records | D1 | Transactions and a strongly consistent compare-and-swap on `environments.revision`, which is what stops two people silently clobbering each other's secrets. |
| Secret reads | KV cache | Keyed `env:<id>:rev:<n>`. Entries are immutable, because a write bumps the revision and nobody reads the old key again — so an eventually consistent global cache is safe in front of secrets. |
| OTP codes, rate limits | D1 | Attempt counters have to be consistent or they can be raced past. KV is eventually consistent and therefore wrong for this. |
| Sessions | D1 | Workers isolates are ephemeral and per-colo; in-memory sessions would not survive a request. |

## Deploy

```bash
bun install
bunx wrangler login
bunx wrangler d1 create envi              # id goes in wrangler.jsonc, binding DB
bunx wrangler kv namespace create CACHE   # id goes in wrangler.jsonc, binding CACHE
bun run db:remote
```

The bindings must be named `DB` and `CACHE`; that is what the code reads. When
wrangler offers to write the config for you it may add a *second* entry under a
different binding name — keep one, named `DB`.

```bash
bunx wrangler secret put ENVI_ENCRYPTION_KEY   # exactly 32 characters
bunx wrangler secret put RESEND_API_KEY
bunx wrangler secret put RESEND_FROM
bunx wrangler deploy
```

Each `secret put` prompts for the value, so nothing lands in shell history. The
values are stored encrypted at Cloudflare and cannot be read back.

A brand new `workers.dev` subdomain takes a minute or two to get its TLS
certificate. Until then every request fails the handshake, which looks like a
broken deploy and is not.

## Configuration

`wrangler.jsonc` `vars` holds the production values and is committed in plain
text, so nothing secret goes there:

```jsonc
"vars": {
  "ENVI_WEB_URL": "https://your-dashboard.example.com",
  "ENVIRONMENT": "production"
},
"preview_urls": false
```

`preview_urls: false` matters: without it every deploy publishes a second public
hostname serving the same API against the same database.

`ENVI_WEB_URL` is what invitation and device-approval links are built from. With
`ENVIRONMENT` set to anything but a development label, a loopback URL makes those
requests fail outright rather than mailing links nobody can open.

## Local development

`.dev.vars` is gitignored and **overrides** `vars` under `wrangler dev`. That is
how local work stays on localhost while deploys stay on the real site.

```
ENVI_WEB_URL=http://localhost:3000
ENVIRONMENT=development
ENVI_ENCRYPTION_KEY=01234567890123456789012345678901
RESEND_API_KEY=re_test_key_not_real
RESEND_FROM=Envi <noreply@example.com>
```

Use throwaway values. Local D1 is a separate, empty database, so the real
encryption key buys nothing here — and a real Resend key means local testing can
email real people.

```bash
bun run db:local
bun run dev                               # http://localhost:8787
ENVI_API_URL=http://127.0.0.1:8787 envi auth
```

Changing the D1 `database_id` gives you a fresh, empty *local* database, because
local state is keyed by that id. Re-run `bun run db:local` when it changes.

## Encryption

`src/crypto.ts` is byte-compatible with `internal/crypto` in the Go server:
AES-256-GCM, no AAD, `nonce(12) ‖ ciphertext ‖ tag(16)`. Both test suites pin to
`test/fixtures/go-sealed.json` — `test/crypto.test.ts` opens it with WebCrypto
and `internal/crypto/compat_test.go` opens it with Go. Either side drifting
fails a test rather than silently producing unreadable secrets.

The `secret_versions.nonce` column stays empty, matching the Go server, which
prepends the nonce to `ciphertext`.

## Layout

```
src/crypto.ts        AES-256-GCM, token hashing
src/db.ts            D1 helpers; `changed()` is the atomic primitive
src/access.ts        permission checks
src/auth.ts          sessions, refresh rotation, OTP, rate limits
src/device.ts        device authorization grant
src/secrets.ts       snapshots, revision CAS, KV cache
src/projects.ts      projects and environments
src/invitations.ts   invitations and collaborators
src/tokens.ts        service tokens, audit events
src/index.ts         Hono routes
```

## Differences from the Go server

- `beta_testers.json` is compiled in as `src/beta.ts`; Workers have no
  filesystem, so changing the list means a redeploy.
- D1 has no `SELECT ... FOR UPDATE`. Single-use guarantees (refresh rotation,
  device redemption, invitation accept) come from conditional `UPDATE`
  statements and a `changes()` check instead.
- D1 and Neon are separate databases. Running both servers against one dataset
  needs a migration; they do not share storage automatically.

## Not done yet

- Neon → D1 migration script
- A conformance suite running identical assertions against both servers
- Audit events are written for secret reads and writes by the Go server; the
  port exposes `recordAudit` but does not yet call it on every path
