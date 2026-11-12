DATABASE_URL ?= $(shell grep -E '^DATABASE_URL=' .env 2>/dev/null | cut -d= -f2-)
CLI_PKG    = shellhaki/envi/internal/cli
CLI_ENV    ?= $(if $(CI),file,envi)
CLI_ORIGIN ?= cli

.PHONY: help dev-api dev-ui dev-worker test test-api test-install test-ui test-cli \
	secrets deploy deploy-api deploy-install logs cli cli-env cli-ldflags db db-migrate diagram

help: ## List every target
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

dev-api: ## Run the API on Bun with secrets from Envi
	cd server && envi run -- bun run dev

dev-ui: ## Run the site: localhost:3000, docs on docs.localhost:3000
	cd ui && envi run -- bun run dev

dev-worker: ## Run the API Worker locally against stand-ins for Neon and Upstash
	docker compose -f server/test/worker/compose.yml up -d
	cd server && bun run worker:local

test: test-api test-install test-ui test-cli ## Run every test

test-api: ## API tests and typecheck
	cd server && bun test && bun run typecheck

test-install: ## Install Worker tests and typecheck
	cd install && bun test && bun run typecheck

test-ui: ## Site tests and lint
	cd ui && bun test && bun run lint

test-cli: ## CLI vet and tests
	cd cli && go vet ./... && go test ./...

# Names set under [vars] in server/wrangler.toml are left out: Cloudflare refuses a secret and a var with one name.
secrets: ## Upload .env as the API Worker's secrets
	@vars=$$(sed -n '/^\[vars\]/,/^\[/p' server/wrangler.toml | grep -oE '^[A-Z_]+' | paste -sd'|' -); \
	file=$$(mktemp); \
	grep -vE "^($$vars)=" .env > $$file; \
	(cd server && bunx wrangler secret bulk $$file); status=$$?; \
	rm -f $$file; exit $$status

deploy: deploy-api deploy-install ## Deploy both Workers

deploy-api: ## Deploy the API Worker (api.envisecrets.com)
	cd server && bunx wrangler deploy

deploy-install: ## Deploy the install Worker (install.envisecrets.com)
	cd install && bunx wrangler deploy

logs: ## Stream the API Worker's logs
	cd server && bunx wrangler tail

cli: ## Build the CLI into ~/.local/bin/envitest (settings from Envi's "cli" origin)
	@flags="$$($(MAKE) -s cli-ldflags)" || exit 1; \
	cd cli && go build -ldflags "-X main.version=dev-local$$flags" -o ~/.local/bin/envitest ./cmd/envi; \
	echo "built dev-local ($(CLI_ENV)) with:$${flags:- (no settings; compiled defaults stand)}"

cli-env: ## Print the CLI's build settings (cli/.env.cli in CI)
	@if [ "$(CLI_ENV)" = "file" ]; then \
	  [ -f cli/.env.cli ] && cat cli/.env.cli; true; \
	else \
	  err=$$(mktemp); \
	  if envi copy --origin $(CLI_ORIGIN) --stdout 2>"$$err"; then rm -f "$$err"; \
	  else \
	    cat "$$err" >&2; rm -f "$$err"; \
	    echo "could not read the '$(CLI_ORIGIN)' origin from Envi; try: make cli CLI_ENV=file" >&2; \
	    exit 1; \
	  fi; \
	fi

# A failed fetch must stop the build, not compile the production API URL into a dev binary.
cli-ldflags: ## Print the -X flags the CLI is built with
	@settings="$$($(MAKE) -s cli-env)" || exit 1; \
	printf '%s\n' "$$settings" | \
	sed -e 's/[[:space:]]*#.*$$//' -e '/^[[:space:]]*$$/d' | \
	while IFS='=' read -r k v; do \
	  k=$$(printf '%s' "$$k" | tr -d '[:space:]'); \
	  v=$${v#\"}; v=$${v%\"}; \
	  [ -n "$$k" ] && [ -n "$$v" ] && printf ' -X %s.%s=%s' "$(CLI_PKG)" "$$k" "$$v"; \
	done; true

db: ## Open psql on DATABASE_URL from .env
	psql "$(DATABASE_URL)"

db-migrate: ## Apply migrations/*.sql to DATABASE_URL from .env
	@for f in migrations/[0-9]*.sql; do echo "applying $$f"; psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -q < $$f || exit 1; done

diagram: ## Rebuild docs/excalidraw.json
	python3 scripts/diagram.py
