# Envi development helpers.
#
# Postgres is expected to be running natively (no docker-compose right now)
# with DATABASE_URL in .env pointing at it. There is no migration runner, so
# the schema is applied from here: migrations/schema.sql is the full current
# schema (use on a fresh database), and the numbered migrations/NNN_*.sql
# files are the incremental catch-up scripts for an existing database
# (idempotent).
#
# Quick start:  make db-init  # apply the schema, or bring migrations up to date
# Run `make help` to list every target.

DATABASE_URL ?= $(shell grep -E '^DATABASE_URL=' .env 2>/dev/null | cut -d= -f2-)
PSQL         := psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1

.PHONY: help build-envi build-api build-install db-init db-schema db-migrate db-psql

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

# Build-time settings for the CLI. Every KEY=VALUE becomes
# -X internal/cli.KEY=VALUE, so adding a setting means declaring the Go var and
# adding the key to the source.
#
# The source is the "cli" origin of this project in Envi, fetched with
# `envi copy` so the settings never have to exist as a file on this machine.
# CI has no Envi session: the release workflow writes .env.cli from a
# repository secret, and GitHub Actions sets CI=true, which switches this to
# read that file without the workflow having to say so.
#
#   make build-envi                 settings from Envi (needs envi 1.3.0+)
#   make build-envi CLI_ENV=file    settings from .env.cli, e.g. offline
CLI_PKG    = shellhaki/envi/internal/cli
CLI_ENV    ?= $(if $(CI),file,envi)
CLI_ORIGIN ?= cli

.PHONY: cli-env cli-ldflags
cli-env: ## Print the CLI's build settings as KEY=VALUE (Envi origin "cli"; .env.cli in CI)
	@if [ "$(CLI_ENV)" = "file" ]; then \
	  [ -f .env.cli ] && cat .env.cli; true; \
	else \
	  err=$$(mktemp); \
	  if envi copy --origin $(CLI_ORIGIN) --stdout 2>"$$err"; then rm -f "$$err"; \
	  else \
	    cat "$$err" >&2; rm -f "$$err"; \
	    echo "could not read the '$(CLI_ORIGIN)' origin from Envi." >&2; \
	    echo "needs envi 1.3.0 or newer (envi update now) and a session (envi auth)," >&2; \
	    echo "or build from the file instead: make build-envi CLI_ENV=file" >&2; \
	    exit 1; \
	  fi; \
	fi

# Fetched once and parsed here rather than piped, so a failed fetch stops the
# build instead of producing an empty flag list — which would silently compile
# the production API URL into a binary meant for local development.
cli-ldflags: ## Print the -X flags the CLI is built with
	@settings="$$($(MAKE) -s cli-env)" || exit 1; \
	printf '%s\n' "$$settings" | \
	sed -e 's/[[:space:]]*#.*$$//' -e '/^[[:space:]]*$$/d' | \
	while IFS='=' read -r k v; do \
	  k=$$(printf '%s' "$$k" | tr -d '[:space:]'); \
	  v=$${v#\"}; v=$${v%\"}; \
	  [ -n "$$k" ] && [ -n "$$v" ] && printf ' -X %s.%s=%s' "$(CLI_PKG)" "$$k" "$$v"; \
	done; true

build-envi: ## Build the envi CLI into ~/.local/bin (settings from Envi's "cli" origin)
	@flags="$$($(MAKE) -s cli-ldflags)" || exit 1; \
	go build -ldflags "-X main.version=dev-local$$flags" -o ~/.local/bin/envitest ./cmd/envi; \
	echo "built dev-local ($(CLI_ENV)) with:$${flags:- (no settings; compiled defaults stand)}"

build-api: ## Build the API server into bin/envi-api (for pm2, see ecosystem.config.js)
	go build -o bin/envi-api ./cmd/api

build-install: ## Build the install-script server into bin/envi-install (for pm2, see ecosystem.config.js)
	go build -o bin/envi-install ./cmd/install

db-init: ## Apply schema.sql on a fresh DB (DATABASE_URL from .env), else bring migrations up to date
	@if [ -z "$(DATABASE_URL)" ]; then echo "DATABASE_URL is not set (check .env)"; exit 1; fi
	@if $(PSQL) -c '\dt' 2>/dev/null | grep -qw sessions; then \
		echo "existing database -> applying migrations"; \
		$(MAKE) --no-print-directory db-migrate; \
	else \
		echo "fresh database -> applying schema.sql"; \
		$(PSQL) < migrations/schema.sql && echo "schema applied"; \
	fi

db-schema: ## Apply schema.sql — the full current schema (fresh DB only)
	@if [ -z "$(DATABASE_URL)" ]; then echo "DATABASE_URL is not set (check .env)"; exit 1; fi
	$(PSQL) < migrations/schema.sql
	@echo "schema applied"

db-migrate: ## Apply migrations/*.sql against DATABASE_URL (idempotent)
	@if [ -z "$(DATABASE_URL)" ]; then echo "DATABASE_URL is not set (check .env)"; exit 1; fi
	@for f in migrations/[0-9]*.sql; do \
		echo "applying $$f"; \
		$(PSQL) < $$f || exit 1; \
	done
	@echo "migrations applied"

db-psql: ## Open a psql shell on the database
	@if [ -z "$(DATABASE_URL)" ]; then echo "DATABASE_URL is not set (check .env)"; exit 1; fi
	psql "$(DATABASE_URL)"


run-api:
	envi run -- go run ./cmd/api
run-ui:
	cd ui && envi run -- bun run dev