# A tour of the Envi codebase

This is a guided walk through how Envi is built: what each part does, how a
request travels through it, and where to go to change things. Read it top to
bottom once, then use the last two sections as a reference.

If Go syntax is what's slowing you down, skip ahead to
[Reading Go: a cheat sheet](#reading-go-a-cheat-sheet) first.

---

## 1. What Envi is, in one paragraph

Envi stores environment secrets (the `API_KEY=...` lines in a `.env` file)
encrypted on a server, and lets people and machines fetch them. People use a
CLI (`envi pull`, `envi push`, `envi run`) or a web dashboard. Machines, like CI
or a Docker container, use service tokens. Every secret is encrypted before it
reaches the database, every read and write is logged, and production
environments need explicit permission.

## 2. The pieces

Five programs, two databases:

```
  you, in a terminal           you, in a browser
         |                            |
         v                            v
   +-----------+              +----------------+
   |  envi CLI |              |   dashboard    |   ui/          (Next.js)
   | cmd/envi  |              | talks to the   |
   +-----------+              | API for you    |
         |                    +----------------+
         |   HTTPS + JSON             |
         v                            v
   +------------------------------------------+
   |              API server                  |   cmd/api + internal/
   |  every rule lives here: who you are,     |   (Go)
   |  what you may touch, encryption, audit   |
   +------------------------------------------+
         |                     |
         v                     v
   +------------+        +-----------+
   |  Postgres  |        |   Redis   |
   | everything |        | login     |
   | permanent  |        | codes     |
   +------------+        +-----------+

   docs/        the documentation website (Next.js), separate deploy
   cmd/install  serves the `curl ... | sh` install script, separate deploy
```

The single most important idea: **the API server is the only thing that
enforces rules.** The CLI and dashboard are just two ways of sending it
requests. If something must never be allowed, the check belongs in `internal/`,
not in the CLI or the UI.

## 3. The folder map

| Folder | What's in it |
|---|---|
| [cmd/api](cmd/api/main.go) | Starts the API server. Reads config, connects to Postgres and Redis, wires everything together. |
| [cmd/envi](cmd/envi/main.go) | Starts the CLI. Twelve lines: it hands the command line to `internal/cli`. |
| [cmd/install](cmd/install/main.go) | A tiny server that hands out the install scripts in [scripts/](scripts/). |
| [internal/api](internal/api/) | The web addresses (routes). Each file reads a request, calls the matching package below, and writes JSON back. |
| [API.md](API.md) | Every endpoint, with request and response shapes. |
| [internal/auth](internal/auth/) | Logging in and staying logged in: email codes, sessions, and CLI browser login. |
| [internal/secret](internal/secret/service.go) | Reading and writing secrets: encryption, versions, conflict detection, audit logging. |
| [internal/kv](internal/kv/kv.go) | The project-wide key-value store the SDK reads and writes. Encrypted like secrets, but no environments and no history. |
| [internal/apikey](internal/apikey/apikey.go) | Personal API keys: created by a user, exchanged for a session. |
| [internal/access](internal/access/service.go) | The one function that decides "may this user do this to this environment?" |
| [internal/project](internal/project/service.go) | Projects and their environments. |
| [internal/invitation](internal/invitation/service.go) | Inviting a collaborator by email, and accepting or revoking that. |
| [internal/service_token](internal/service_token/service.go) | Tokens for machines (CI, Docker), each locked to one environment. |
| [internal/workspace](internal/workspace/service.go) | Creates a user's account and personal organization on their first login. |
| [internal/crypto](internal/crypto/secret.go) | AES-256-GCM encryption. Forty lines, and nothing else touches the key. |
| [internal/otp](internal/otp/) | Making, storing (in Redis), and checking 6-digit login codes. |
| [internal/mailer](internal/mailer/) | Sending email through Resend. |
| [internal/audit](internal/audit/service.go) | Reading the activity log back out. |
| [internal/beta](internal/beta/allowlist.go) | The private-beta email allow list. |
| [internal/config](internal/config/config.go) | Reads the server's settings from environment variables. |
| [internal/cli](internal/cli/) | Every CLI command. The biggest package. |
| [internal/cli/project](internal/cli/project/) | `envi init` and the `envi.toml` file. |
| [internal/cli/session](internal/cli/session/store.go) | Where the CLI keeps your login on disk. |
| [internal/e2e](internal/e2e/cli_test.go) | One big test that runs the real CLI against a real server. |
| [migrations](migrations/) | The database layout. `schema.sql` is the whole thing; the numbered files are changes, in order. |
| [ui](ui/) | The dashboard and landing page. |
| [sdk](sdk/) | `@shellhaki/envi-sdk`, the JavaScript client for the key-value store. |
| [npm](npm/) | Builds the npm packages that install the CLI binary. |
| [docs](docs/) | The documentation site. |

## 4. Follow one request: `envi pull`

The best way to learn the codebase is to trace one command all the way through.
Here is everything that happens when you type `envi pull`.

**On your machine (the CLI)**

1. [cmd/envi/main.go](cmd/envi/main.go) passes `["pull"]` to `App.Run`.
2. [internal/cli/app.go](internal/cli/app.go) has one big `switch` on the first
   word. `case "pull"` calls `a.authenticated(...)`, which shows the spinner
   and gets you a logged-in client.
3. `authorize` in [internal/cli/auth.go](internal/cli/auth.go) loads your
   tokens from `session.db`. If the access token has run out, it quietly swaps
   the refresh token for new ones first (more on this in section 5).
4. `Pull` in [internal/cli/secrets.go](internal/cli/secrets.go) reads
   `envi.toml` in the current folder to learn which project and environment
   you're linked to, then calls `fetchSnapshot`.
5. `Client.Do` in [internal/cli/client.go](internal/cli/client.go) sends
   `GET /environments/<id>/secrets/snapshot` with the header
   `Authorization: Bearer <your access token>`.

**On the server**

6. [internal/api/server.go](internal/api/server.go) `Build` registered that
   address when the server started. The route lives in
   [internal/api/secret.go](internal/api/secret.go).
7. Before the handler runs, `RequireAuth` in
   [internal/api/middleware.go](internal/api/middleware.go) reads the token and
   asks `auth.UserForAccessToken` who it belongs to. No valid token: 401, and
   the request stops here.
8. The handler calls `Snapshot` in
   [internal/secret/service.go](internal/secret/service.go), which:
   - asks `access.Allow` whether you may **read** this environment,
   - loads every secret's current encrypted value,
   - decrypts each one with [internal/crypto](internal/crypto/secret.go),
   - writes one `secret.read` row to the audit log,
   - returns the values and the environment's revision number.
9. The handler sends that back as JSON.

**Back on your machine**

10. `Pull` writes the values into `.env` (readable only by you), and records the
    revision number in `envi.toml`, which `envi push` uses later to detect
    conflicts.

Nearly every command follows this same shape: CLI command → HTTP request →
`RequireAuth` → handler in `internal/api` → a package in `internal/` that checks
access and talks to Postgres → JSON back.

## 5. Logging in and staying logged in

This is all in [internal/auth](internal/auth/), which is written in the plain
style the rest of the code is moving toward: ordinary functions, long names,
lots of comments. It's a good place to start reading Go.

### The two tokens

Logging in gives you two random tokens ([sessions.go](internal/auth/sessions.go)):

- **Access token**: sent with every request, lasts **15 minutes**.
- **Refresh token**: used only to get a new pair when the access token runs
  out. Each one works **once**.

Every refresh starts the clock again, so you stay logged in while you keep
using Envi and get logged out after a stretch of not using it. That stretch is
`RefreshTokenLifetime` in [sessions.go](internal/auth/sessions.go), currently
30 days. The database only stores a hash of each token, never the token.

### Three ways to log in

| Who | How | Code |
|---|---|---|
| A person, in the browser | Types email, gets a 6-digit code, types it back | [login.go](internal/auth/login.go), [api/auth.go](internal/api/auth.go) |
| A person, in the CLI | `envi auth` shows a code like `WXYZ-ABCD`; you approve it in the browser | [device.go](internal/auth/device.go), [api/device.go](internal/api/device.go) |
| A machine | A service token in `ENVI_TOKEN`, locked to one environment | [service_token](internal/service_token/service.go) |

All three people-paths end in `StartSession`, so a session is a session no
matter how it started.

### Where the login is kept

- **CLI**: a small SQLite file, `session.db`, in your config folder. On macOS
  that's `~/Library/Application Support/envi/`, on Linux `~/.config/envi/`.
  Set `ENVI_CONFIG_DIR` to put it elsewhere. See
  [internal/cli/session/store.go](internal/cli/session/store.go). It holds
  exactly one login, not one per server; see
  [Known rough edges](#10-known-rough-edges).
- **Dashboard**: a signed, `httpOnly` cookie called `envi_session`, made in
  [ui/lib/web-session.ts](ui/lib/web-session.ts). JavaScript in the page can't
  read it; only the dashboard's own server can.

### How the dashboard talks to the API

The browser never calls the API directly. It calls `/api/envi/...` on the
dashboard's own server, [ui/app/api/envi/[...path]/route.ts](ui/app/api/envi/[...path]/route.ts),
which reads the cookie, adds the access token, and forwards the request. If the
API says 401, that same file refreshes the tokens, updates the cookie, and
retries, so the page never notices.

## 6. Secrets

All in [internal/secret/service.go](internal/secret/service.go).

- **Encryption.** Every value is encrypted with AES-256-GCM before it's saved,
  using `ENVI_ENCRYPTION_KEY`. Someone who steals the database but not that key
  gets nothing readable.
- **Versions.** Changing a secret never overwrites it. It adds a new row to
  `secret_versions` and points the secret at it, so the history survives.
  Deleting is a "soft delete": the secret stops showing up but its history stays.
- **Revisions and conflicts.** Each environment has a counter that goes up on
  every write. `envi push` sends the number it last saw; if someone else pushed
  in between, the numbers don't match and the push is refused instead of
  quietly overwriting their work. `envi push --force` skips that check.
- **Audit.** Writes and deletes log one row per secret. A read logs one row for
  the whole environment, with the count.

## 7. Who may do what

One function decides: `Allow` in [internal/access/service.go](internal/access/service.go).
It's a single SQL query. You get in if any of these is true:

1. You have a **grant** for this environment (or the whole project) with
   enough permission: `read` < `write` < `manage`.
2. You're a **member of the organization** and the environment is **not**
   production.
3. You're an **owner or admin** of the organization. This includes production:
   the person who created an environment must never be locked out of it
   ([internal/e2e/cli_test.go](internal/e2e/cli_test.go) tests this on
   purpose).

So "production" means: ordinary members need an explicit grant, from an
invitation or from someone with `manage`.

## 8. Projects, environments, invitations

- An **organization** is created for you on first login
  ([workspace](internal/workspace/service.go)). Projects belong to it.
- A **project** holds **environments** (`dev`, `staging`, `prod`...). The CLI
  calls environments *origins*: [internal/cli/origin.go](internal/cli/origin.go).
- **Invitations** ([invitation](internal/invitation/service.go)): you invite an
  email with a permission. They get a link. If they don't have an account yet,
  following the link creates one. Accepting turns the invitation into a grant.

## 9. The CLI

Every command starts in the `switch` in [internal/cli/app.go](internal/cli/app.go).

| Command | Main file |
|---|---|
| `envi auth`, `envi logout` | [auth.go](internal/cli/auth.go) |
| `envi init` | [project/init.go](internal/cli/project/init.go) |
| `envi pull`, `envi push`, `envi diff` | [secrets.go](internal/cli/secrets.go) |
| `envi mod` | [mod.go](internal/cli/mod.go), editor in [editor.go](internal/cli/editor.go) |
| `envi origin list / switch / create`, `envi push ... origin <name>` | [origin.go](internal/cli/origin.go) |
| `envi run -- <command>` | [run.go](internal/cli/run.go) |
| `envi share`, `envi invite accept` | [invitation.go](internal/cli/invitation.go) |
| `envi token create` | [service_token.go](internal/cli/service_token.go) |
| `envi activity` | [activity.go](internal/cli/activity.go) |
| `envi update`, `envi uninstall` | [update.go](internal/cli/update.go), [uninstall.go](internal/cli/uninstall.go) |

Other files: [client.go](internal/cli/client.go) sends HTTP requests,
[ui.go](internal/cli/ui.go) draws colours and the spinner,
[config.go](internal/cli/config.go) decides which server to talk to.

**Which server the CLI talks to**, first match wins:

1. `ENVI_API_URL` if it's set.
2. A development build (`make build-envi`, `go run`) uses `http://127.0.0.1:8080`.
3. A released build uses `https://api.envisecrets.com`.

Released builds get that URL from `.env.cli`, written from the `ENV_CLI`
repository secret during a release.

**Files the CLI writes:** `envi.toml` in your project folder (which project and
environment; safe to commit, holds no secrets), `.env` when you `pull`, and
`session.db` in your config folder.

## 10. Known rough edges

Worth knowing before you're surprised by them:

- **The CLI keeps one login for every server.** A development build (talking to
  your local server) and a released build (talking to production) share the
  same `session.db` and overwrite each other's login, so switching between
  them logs you out of the other.
- **A failed refresh always says "session expired".** If the server is simply
  unreachable (not running, or restarting during a deploy), the CLI still
  tells you to run `envi auth`, even though your login is fine.
- **Pushing never deletes.** `envi push` adds and updates keys but leaves keys
  that your file doesn't have. `envi diff` still reports them as `removed`.

## 11. The database

Fourteen tables, all in [migrations/schema.sql](migrations/schema.sql):

| Table | Holds |
|---|---|
| `users` | One row per email address. |
| `organizations`, `memberships` | Who belongs to which organization, and as what (`owner`, `admin`, `member`). |
| `projects`, `environments` | Projects and their environments. `environments.revision` is the conflict counter. |
| `secrets`, `secret_versions` | A secret's name, and every encrypted value it has ever had. |
| `access_grants` | Explicit permissions: this user may `read`/`write`/`manage` this project or environment. |
| `invitations` | Pending, accepted, or revoked invites. |
| `sessions` | Logins: token hashes and when they expire. |
| `device_authorizations` | In-progress `envi auth` browser logins. |
| `service_identities`, `api_tokens` | Machine tokens and the environment each is locked to. |
| `audit_events` | The activity log. |

There's no migration tool. A fresh database gets `schema.sql`; an existing one
gets the numbered files it hasn't had yet. `make db-init` does the right one.

## 12. Tests, deploys, releases

- `go test ./...` runs everything that doesn't need a database.
- `ENVI_INTEGRATION=1 go test ./...` with `DATABASE_URL` and `REDIS_URL` set
  also runs the database tests, including the end-to-end CLI test.
- [.github/workflows/deploy.yml](.github/workflows/deploy.yml): on every push
  to `main`, runs both of the above against a throwaway Postgres and Redis,
  then tells Coolify to redeploy whichever of the API, dashboard, and docs
  changed.
- [.github/workflows/release.yml](.github/workflows/release.yml): pushing a
  `v*` tag builds the CLI for seven platforms with GoReleaser and publishes a
  GitHub release.

## 13. Where to change common things

| I want to... | Go to |
|---|---|
| Change how long before an inactive user is logged out | `RefreshTokenLifetime` in [internal/auth/sessions.go](internal/auth/sessions.go), and `sessionTTL` / `maxAge` in [ui/lib/web-session.ts](ui/lib/web-session.ts) to match |
| Change how long a login code lasts, or how many tries | the `otp.Service{...}` line in [cmd/api/main.go](cmd/api/main.go) |
| Add a CLI command | a new `case` in [internal/cli/app.go](internal/cli/app.go), the work in its own file, and the help text at the bottom of `app.go` |
| Add an API endpoint | the matching file in [internal/api](internal/api/), registered from [server.go](internal/api/server.go) |
| Change who may access what | [internal/access/service.go](internal/access/service.go) |
| Change the database | a new numbered file in [migrations/](migrations/), and the same change in `schema.sql` |
| Add or change a key-value rule | [internal/kv/kv.go](internal/kv/kv.go), with the endpoints in [internal/api/kv.go](internal/api/kv.go) |
| Change the SDK | [sdk/src/envi.ts](sdk/src/envi.ts), tests in [sdk/test](sdk/test/) |
| Change a dashboard page | [ui/app/dashboard/workspace.tsx](ui/app/dashboard/workspace.tsx) (every dashboard page is this one component) |
| Change the landing page | [ui/app/page.tsx](ui/app/page.tsx) |
| Change the docs | [docs/app](docs/app/), and the sidebar in [docs/lib/docs-nav.ts](docs/lib/docs-nav.ts) |

---

## Reading Go: a cheat sheet

Every pattern below appears all over this codebase. The TypeScript line next to
each is roughly the same idea.

**A function that can fail returns an error as its last value.** Go has no
exceptions.

```go
func UserForAccessToken(db *pgxpool.Pool, token string) (userID string, err error)
```
```ts
function userForAccessToken(db: Pool, token: string): string   // throws on failure
```

**Check the error right away, every time.** `nil` means "nothing", so
`err != nil` means "something went wrong".

```go
userID, err := auth.UserForAccessToken(db, token)
if err != nil {
    return err        // stop and pass the problem up
}
```

**`:=` makes a new variable; `=` changes an existing one.**

```go
count := 0      // let count = 0
count = 5       // count = 5
```

**A struct is a group of named fields**, like a TypeScript object type. The
text in backticks says what the field is called in JSON.

```go
type Env struct {
    ID   string `json:"id"`
    Name string `json:"name"`
}
```
```ts
type Env = { id: string; name: string }
```

**A method is a function attached to a type.** Most packages outside
`internal/auth` are written this way. Read the part in the first brackets as
"this function belongs to":

```go
func (s Service) Put(ctx context.Context, user, env, key, value string) error
```
```ts
class Service { put(ctx, user, env, key, value): void }
```

It's called as `secrets.Put(ctx, user, env, "API_KEY", "abc")`. Inside, `s` is
the `Service` it was called on, the way `this` works in TypeScript.

**`*` and `&` are about "where a value lives".** `*Thing` means "the real
`Thing`, not a copy". `&x` means "the place `x` lives", which is how the
database fills in your variables:

```go
var name string
row.Scan(&name)     // "put the result into name"
```

**Capital letters mean public.** `StartSession` can be used from other packages;
`useUpRefreshToken` only inside its own.

**`defer` runs a line when the function ends**, however it ends. It's used for
cleanup that must not be forgotten:

```go
tx, err := db.Begin(ctx)
defer tx.Rollback(ctx)   // undo the transaction if we exit early
```

**An interface is a list of methods.** Anything that has them fits. The one you
see most is `error`: anything with an `Error() string` method is an error.

```go
func (pending DevicePending) Error() string { return pending.Reason }
```

**`context.Context` is almost always the first argument.** It carries "this
request was cancelled" and deadlines. You can mostly pass it along and ignore it.

**A function written inside another function** is common when registering
routes: it lets a handler get extra arguments.

```go
router.POST("/auth/refresh", func(c *gin.Context) { handleRefresh(c, db) })
```
```ts
router.post("/auth/refresh", (c) => handleRefresh(c, db))
```
