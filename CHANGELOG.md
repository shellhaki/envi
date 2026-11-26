# Changelog

What changed in Envi, newest first. The same list is published at https://envisecrets.com/changelog.

## 2026-11-26 · v1.7.0: Send secrets to Cloudflare Workers

`envi workers` puts an origin's secrets on a Cloudflare Worker through your own wrangler, and runs `wrangler dev` with them, so there is no `.dev.vars` to keep in sync.

- `envi workers push` sets the origin's secrets on the Worker. Values go to wrangler on standard input, never in a file or an argument.
- `envi workers dev` runs `wrangler dev` with the secrets in its environment and nothing written to disk.
- `envi workers deploy` pushes, then runs `wrangler deploy`.
- `envi workers status` compares the names in Envi with the Worker's, and `envi workers delete <key>` removes one.
- A `[workers]` table in `envi.toml` maps each origin to a wrangler environment. Production asks before sending; `--yes` skips the question in CI.
- Names already in `[vars]` are skipped with a warning, `--prune` deletes secrets that aren't in Envi, and `--dry-run` shows the changes without sending anything.

## 2026-11-19 · v1.6.0: Self-hosted first, with Envi Cloud next to it

Envi is now made to run on your own server first. The hosted dashboard is Envi Cloud, at cloud.envisecrets.com, and it keeps its free tier and paid plans.

- envisecrets.com now shows how to install your own instance and the CLI.
- The dashboard moved to cloud.envisecrets.com. Old links, including invitations, take you there. Sign in once more, since sign-ins belong to the old address.
- Nothing changes for the CLI or CI: the API stays at api.envisecrets.com.
- `envi auth` asks whether you sign in to Envi Cloud or your own instance, and remembers the answer. `envi auth --cloud` or `envi auth --url <api-url>` skip the question.

## 2026-11-19: The shell hook is gone

Loading secrets when you cd into a folder is removed. `envi run -- <command>` does the same job for one command, with nothing left in your shell afterwards.

- `envi hook`, `envi allow`, `envi deny` and `envi reload` are removed.
- If you installed the hook, update the CLI with `envi update now`. The next shell you open takes the hook out of its startup file, with one line saying so.
- The install script no longer asks about the hook.

## 2026-11-14 · v1.5.0: Self-host Envi on your own server

Envi now runs on a server you own, with everything free: no billing, no email provider, no limit on people or projects. One command installs it.

- Install an instance with `curl -fsSL https://install.envisecrets.com | sh -s instance`. The same link still installs the CLI.
- Choose how people reach it: a domain with HTTPS from Let's Encrypt, a domain behind the reverse proxy you already run, or the server's IP on a private network.
- Docker keeps the instance to itself on two private networks. Postgres and Redis open no ports on the server. pm2 is there for servers without Docker.
- People sign in with an email and password. The admin adds people on the new Users page, gets a password to hand them, and picks which projects each one sees.
- A forgotten password is reset by writing a new one into the users file. Nothing is emailed.
- `envi set-url <api-url>` points the CLI at your instance; `envi set-url --reset` points it back at Envi Cloud.
- `envi-instance` looks after the server: status, logs, restart, backups and updates.

## 2026-11-12: A faster API, closer to you

The API moved to Cloudflare Workers, with the database on Neon and a Redis cache on Upstash. Nothing changes for you: the same address, the same CLI, the same data.

- api.envisecrets.com now answers from Cloudflare's network instead of a single server.
- The docs now live at docs.envisecrets.com, served by the same site as the dashboard.
- install.envisecrets.com is served by Cloudflare too.
- The npm package is no longer maintained. Install and update the CLI with the install script, which also works in Docker images and CI.

## 2026-10-13 · v1.3.0: Copy secrets to the clipboard

`envi copy` puts secrets on your clipboard without writing a file.

- `envi copy` copies every secret as .env lines; `envi copy KEY` copies one value.
- `envi copy --single` lets you pick one from a list.

## 2026-10-02 · v1.2.0: Delete a local .env safely

`envi clean` deletes your local .env only once Envi already holds the same secrets, so nothing is lost.

- `envi clean` checks the server first and refuses if the file and the server differ.
- `envi push --clean` pushes, then deletes the file.
