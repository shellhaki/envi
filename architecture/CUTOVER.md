# Moving the API to Cloudflare Workers, Neon and Upstash

A one-time plan for switching api.envisecrets.com from the Go server on the VPS
to the TypeScript server in `server/`. Delete this file once the move is done.

Only one backend serves production at a time. The Go server keeps running
until the switch in part 3, and is stopped before the Worker takes over.

What moves:

| | Before | After |
|---|---|---|
| API | Go container on the VPS (Coolify) | `server/src/worker.ts` on Cloudflare Workers |
| Postgres | VPS | Neon |
| Redis | VPS | Upstash |
| Dashboard, docs, install server | VPS | VPS (unchanged) |

Redis holds only login codes and rate-limit counters, so nothing is copied.
Anyone partway through signing in at the moment of the switch requests a new code.

## Part 1: accounts and secrets (no effect on production)

1. Log wrangler in on your laptop: `cd server && bunx wrangler login`.
2. In Neon, create a project in the region closest to your users, on Postgres 18.
   Copy the **direct** connection string, the one without `-pooler` in the host.
   The billing job takes a session advisory lock, which a pooled connection loses.
3. In Upstash, copy the database's REST URL and REST token.
4. Copy these values from the Go API's environment in Coolify. They must be the
   same values, byte for byte:
   - `ENVI_ENCRYPTION_KEY`. A different key cannot decrypt any existing secret.
   - `RESEND_API_KEY`, `RESEND_FROM`, `ENVI_ADMINS`
   - `BACHS_SECRET_KEY`, `BACHS_WEBHOOK_SECRET`, `BACHS_TEAM_PRODUCT_ID`
5. Create the Worker with billing off for now. Run each command from `server/`;
   each one prompts for the value, so nothing lands in shell history:

   ```sh
   bunx wrangler deploy
   bunx wrangler secret put DATABASE_URL            # Neon direct string, rehearsal branch (part 2)
   bunx wrangler secret put UPSTASH_REDIS_REST_URL
   bunx wrangler secret put UPSTASH_REDIS_REST_TOKEN
   bunx wrangler secret put ENVI_ENCRYPTION_KEY
   bunx wrangler secret put RESEND_API_KEY
   bunx wrangler secret put RESEND_FROM
   bunx wrangler secret put ENVI_ADMINS
   ```

   Leave the Bachs secrets out until part 3. Without `BACHS_SECRET_KEY`, the hourly
   cron does nothing, so the rehearsal copy is never charged.

   The Worker is now at `https://envi-api.<your-subdomain>.workers.dev`. Nothing
   points at it yet.

## Part 2: rehearsal on a Neon branch (no effect on production)

1. In Neon, create a branch called `rehearsal`, and copy its direct connection string.
2. Copy production into it. Run this from a machine that can reach the VPS
   Postgres. `pg_dump` must be version 18 or newer:

   ```sh
   pg_dump --no-owner --no-acl "$VPS_DATABASE_URL" > envi.sql
   psql "$NEON_REHEARSAL_URL" -v ON_ERROR_STOP=1 -q < envi.sql
   ```

   `envi.sql` holds encrypted secrets and user emails. Keep it off shared drives
   and delete it when the move is done.

3. Point the Worker at the branch: `bunx wrangler secret put DATABASE_URL`.
4. Check row counts match:

   ```sh
   for db in "$VPS_DATABASE_URL" "$NEON_REHEARSAL_URL"; do
     psql "$db" -tA -c "select (select count(*) from users), (select count(*) from secrets), (select count(*) from secret_versions)"
   done
   ```

5. Drive the real CLI against the Worker with your own account:

   ```sh
   export ENVI_API_URL=https://envi-api.<your-subdomain>.workers.dev
   export ENVI_CONFIG_DIR=$(mktemp -d)   # a separate login, so your normal one is untouched
   envi auth --email you@example.com
   cd some-project && envi run -- printenv SOME_KEY
   ```

   A secret that comes back readable proves the encryption key and the data copy are right.
   Also sign in to a local dashboard with `ENVI_API_URL` set to the Worker URL, and open
   the billing and admin pages.

6. Watch the Worker's logs while doing this: `bunx wrangler tail`.

Repeat part 2 as often as needed. It never touches production.

## Part 3: the switch (about 15 minutes of API downtime)

Pick a quiet hour. Do these in order.

1. **Stop the Go API** in Coolify. From now on `api.envisecrets.com` returns an
   error and nothing writes to the VPS database.
2. **Copy the final data** into Neon's `main` branch, the same way as part 2 step 2,
   then check the row counts.
3. **Point the Worker at production data**:

   ```sh
   bunx wrangler secret put DATABASE_URL            # Neon main branch, direct string
   bunx wrangler secret put BACHS_SECRET_KEY
   bunx wrangler secret put BACHS_WEBHOOK_SECRET
   bunx wrangler secret put BACHS_TEAM_PRODUCT_ID
   ```

4. **Move the domain.** In Cloudflare DNS, delete the `api` record that points at the VPS.
   Then add the route to `server/wrangler.toml`:

   ```toml
   routes = [{ pattern = "api.envisecrets.com", custom_domain = true }]
   ```

   and run `bunx wrangler deploy`. Cloudflare creates the DNS record and the certificate.
5. **Point the dashboard at the new API.** In Coolify, set the dashboard's `ENVI_API_URL`
   to `https://api.envisecrets.com` and redeploy it.
6. **Check it works**:

   ```sh
   curl https://api.envisecrets.com/health
   envi run -- printenv SOME_KEY   # in a project, with your normal login
   ```

   Sign in to the dashboard, and open billing and admin. In the Bachs dashboard, send a
   test webhook and check that `bunx wrangler tail` shows a 200.
7. **Merge** `bun-rewrite` into `main`, including the `routes` line. Before that, add
   `CLOUDFLARE_API_TOKEN` (with the "Edit Cloudflare Workers" template) and
   `CLOUDFLARE_ACCOUNT_ID` to the GitHub repository secrets. From then on,
   `.github/workflows/deploy.yml` deploys `server/` with wrangler after the tests pass.

## Going back

If part 3 goes wrong before users write anything new:

1. In Cloudflare, remove the Worker's custom domain and restore the `api` DNS record to the VPS.
2. Set the dashboard's `ENVI_API_URL` back, and start the Go API in Coolify.

Once users have written to Neon, going back first needs those writes copied back:
`pg_dump` from Neon and restore into the VPS database, with the Go API still stopped.

## After a week without problems

- Stop the old Postgres and Redis containers on the VPS. Keep a final `pg_dump` of the VPS
  database somewhere safe for a month.
- Remove the Go API's Coolify resource and its `COOLIFY_API_UUID` GitHub secret.
- Delete the Go server code that the CLI does not use (`cmd/api`, `internal/api` and the
  packages only it imports), and its tests in the deploy workflow.
- Delete this file.
