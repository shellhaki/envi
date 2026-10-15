# rules for makig changes to the api:
* if you make a new endpoint, or any new endpoints added, report back to me what they do and the endpoint (method & param/body & middleware if possible)
* when an endpoint is added, removed or changed, update ENDPOINTS.md (method, path, middleware, input, what it does) and run `make diagram` to rebuild excalidraw.json. `go test ./internal/api` fails until both match the router.
* when the schema changes, add the numbered migration, update migrations/schema.sql to match, and run `make diagram` to redraw the data model. `go test ./internal/api` fails if a table is missing from excalidraw.json.
* when a package or program is added under internal/ or cmd/, give it a package comment (`// Package x ...` or `// Command x ...`); its first sentence is what the diagram shows. Run `make diagram`.
* when the deployment changes (a new service, domain, port or outside provider), update the architecture section of scripts/diagram.py and run `make diagram`.
