# rules for makig changes to the api:
* the API server is the TypeScript app in server/ (Hono). src/app.ts holds every route; src/worker.ts runs it on Cloudflare Workers (production), src/server.ts on Bun (local). src/install.ts is the install-script Worker. The only Go left is the CLI (cmd/envi, internal/cli).
* if you make a new endpoint, or any new endpoints added, report back to me what they do and the endpoint (method & param/body & middleware if possible)
* when an endpoint is added, removed or changed, update architecture/ENDPOINTS.md (method, path, middleware, input, what it does) and run `make diagram` to rebuild architecture/excalidraw.json. `bun test` in server/ fails until both match src/app.ts.
* when the schema changes, add the numbered migration, update migrations/schema.sql to match, and run `make diagram` to redraw the data model. `bun test` fails if a table is missing from architecture/excalidraw.json. Tests build a throwaway local database from schema.sql, never a real one.
* when a service is added under server/src/services, start the file with a one-line `//` comment; its first sentence is what the diagram shows. The same goes for a CLI package (`// Package x ...`). Run `make diagram`.
* when the deployment changes (a new service, domain, port or outside provider), update the architecture section of scripts/diagram.py and run `make diagram`.
* before committing server changes, run `bun test` and `bun run typecheck` in server/.
