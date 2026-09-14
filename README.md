<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="ui/public/envi-logo-with-text-dark-mode.png">
    <img src="ui/public/envi-logo-with-text-light-mode.png" alt="Envi" width="280">
  </picture>
</p>

<p align="center"><strong>Environment secrets, encrypted, scoped, and audited — as a hosted product, or run entirely on your own infrastructure.</strong></p>

<p align="center">
  <img alt="License" src="https://img.shields.io/badge/license-MIT-0c111d">
  <img alt="Go" src="https://img.shields.io/badge/go-1.25-00ADD8?logo=go&logoColor=white">
  <img alt="Next.js" src="https://img.shields.io/badge/next.js-16-000000?logo=next.js&logoColor=white">
  <img alt="Postgres" src="https://img.shields.io/badge/postgres-16-4169E1?logo=postgresql&logoColor=white">
  <img alt="Docker Compose" src="https://img.shields.io/badge/docker%20compose-ready-2496ED?logo=docker&logoColor=white">
</p>

---

<img src="ui/public/envi-icon.png" width="20" align="left">

**Envi** is an environment-secret manager: encrypted `.env` values, scoped access, an audit trail, and a CLI that gets a secret from your database into a running process without anyone typing it into Slack. Envi is built and run as a product — this repository is also the entire self-hostable stack, for teams who'd rather keep everything on infrastructure they control.

Full documentation: **[docs.envisecrets.com](https://docs.envisecrets.com)**

## Install the CLI

```bash
curl -fsSL https://install.envisecrets.com | sh
```

```powershell
irm https://install.envisecrets.com/install.ps1 | iex
```

The script detects your OS and CPU architecture, verifies a SHA-256 checksum before installing, and works on macOS, Linux, Termux, and Windows.

## What it does

- **Encrypted at rest** — every secret is sealed with AES-256-GCM before it touches Postgres. Nothing is ever stored in plaintext, including in the version history kept for every change.
- **Scoped access** — grant a collaborator `read`, `write`, or `manage` on a project or a single environment. Production doesn't get touched by accident.
- **Passwordless auth** — sign in with a one-time email code or approve a device from your browser. No passwords exist anywhere in the system to leak.
- **Service tokens** — long-lived, scoped credentials for CI/CD pipelines and deploy scripts, independent of any human's session.
- **Full audit trail** — every read, write, and delete is logged against the org, the actor, and the exact secret touched.
- **Email invitations** — invite a teammate by address; they click through, sign in (or sign up on the spot if they're new), and land with access already waiting.
- **CLI and dashboard, one API** — `envi pull`/`push`/`diff` your `.env` files from the terminal, or manage everything visually. Same backend, same permissions, your choice of interface.

## Self-host it in one command

```bash
git clone https://github.com/shellhaki/envi.git
cd envi
cp .env.example .env     # fill in the four required values
docker compose up -d
```

That brings up Postgres, Redis, the API, the install-script server, and the dashboard together. The database schema is applied automatically on first start. The dashboard lands on [localhost:3000](http://localhost:3000).

You need four values in `.env` before it will start:

| Variable | What it is |
|---|---|
| `ENVI_ENCRYPTION_KEY` | Exactly 32 characters — `openssl rand -hex 16`. Decrypts every secret you store. |
| `ENVI_SESSION_SECRET` | At least 32 characters — signs dashboard session cookies. |
| `RESEND_API_KEY` | For one-time sign-in codes and invitations. |
| `RESEND_FROM` | Sender address, on a domain verified with Resend. |

Prefer running the binaries directly under a process manager instead? See the [deployment walkthrough](https://docs.envisecrets.com/self-hosting/deployment).

## Or run it on Cloudflare Workers

Envi ships two implementations of the same API. The Go server is the reference; `workers/` is a port to Hono, D1 and KV that speaks the identical wire protocol — the CLI and dashboard work against either, and only `ENVI_API_URL` changes. There is no server to patch and nothing to keep running.

```bash
cd workers
bun install
bunx wrangler d1 create envi          # put the id in wrangler.jsonc as binding DB
bunx wrangler kv namespace create CACHE
bun run db:remote
bunx wrangler secret put ENVI_ENCRYPTION_KEY
bunx wrangler secret put RESEND_API_KEY
bunx wrangler secret put RESEND_FROM
bunx wrangler deploy
```

D1 replaces Postgres and, with two small tables, Redis. Secret reads are cached in KV keyed by environment revision, so entries are immutable and a stale one is never read.

Two things to know: the two deployments **do not share a database**, so a Workers instance starts empty; and `ENVI_ENCRYPTION_KEY` must be byte-identical across both if they will ever read the same data. Full walkthrough: [docs.envisecrets.com/self-hosting/workers](https://docs.envisecrets.com/self-hosting/workers).

## How it fits together

```mermaid
flowchart LR
    CLI["envi CLI"] -->|bearer token| API["Go API"]
    Web["Next.js dashboard"] -->|session cookie| API
    API --> PG[("Postgres\nencrypted secrets")]
    API --> Redis[("Redis\nOTP codes")]
    API --> Mail["Resend\nemail delivery"]
```

The CLI and the dashboard are two clients of the same API — neither one is a special case. Secrets are encrypted before they reach Postgres and decrypted only in memory, on demand, for a request that has already passed the access-grant check.

## Repository layout

| Path | What it is |
|---|---|
| `cmd/api` | The API server — the entire backend |
| `cmd/envi` | The CLI, released for macOS, Linux, Termux, and Windows |
| `cmd/install` | Tiny server for the `curl \| sh` install scripts |
| `internal/` | Domain packages: secrets, auth, access, invitations, audit, mail |
| `ui/` | The Next.js dashboard (deployable separately, e.g. to Vercel) |
| `docs/` | The documentation site, its own Next.js app |
| `migrations/` | `schema.sql` for a fresh database, numbered files to catch one up |
| `workers/` | The same API on Cloudflare Workers, D1 and KV — an alternative to `cmd/api` |
| `traefik/` | Reverse-proxy config for a non-Docker deployment |

## Security, briefly

- Secret values: AES-256-GCM, one key, sealed before every write.
- Tokens: access, refresh, service, and invitation tokens are all stored as hashes — the plaintext exists only once, at issuance, in the response the caller already has.
- Sessions: HMAC-signed, HTTP-only, `SameSite=Strict` cookies on the web; rotating refresh tokens everywhere.
- Rate limits: OTP requests and attempts are capped; invitations are capped per sender, so a compromised account can't be used to burn a sending domain's reputation.

## The CLI

| Command | What it does |
|---|---|
| `envi auth` | Sign in — browser device flow by default, `--email` for a one-time code |
| `envi init` | Link the current directory to a project and environment |
| `envi pull` | Write the environment's secrets to `.env` |
| `envi push` | Send local `.env` changes up, with conflict detection |
| `envi diff` | Show what's changed between local and remote before you push |
| `envi project create <name>` | Create a project |
| `envi env create <name>` | Create an environment under the current project |
| `envi share <email>` | Invite a collaborator with scoped access |
| `envi invite accept <token>` | Accept an invitation |
| `envi token create --name <name>` | Mint a scoped service token for CI/CD |
| `envi activity` | Recent reads and writes across your org |
| `envi logout` | Revoke the current session |

Full reference, including flags and exit codes: [docs.envisecrets.com/cli](https://docs.envisecrets.com/cli).

## Development

Postgres and Redis running locally, then:

```bash
make db-init                  # apply the schema, or bring migrations up to date
make build-api build-install  # binaries into bin/
cd ui && bun dev              # dashboard on :3000
cd docs && bun dev            # docs site on :3001
```

`make help` lists every target.

## License

MIT — see [LICENSE](LICENSE).
