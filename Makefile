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

# Build-time settings for the CLI. .env.cli is the source of truth: every
# KEY=VALUE in it becomes -X internal/cli.KEY=VALUE, so adding a setting means
# declaring the Go var and adding a line there. .env.cli.example is only an
# example of what the file usually holds.
#
# CI has no .env.cli of its own — the release workflow writes one from the
# repository secret before building, so both paths read the same file.
CLI_PKG = shellhaki/envi/internal/cli

.PHONY: cli-ldflags
cli-ldflags: ## Print the -X flags the CLI is built with
	@[ -f .env.cli ] || exit 0; \
	sed -e 's/[[:space:]]*#.*$$//' -e '/^[[:space:]]*$$/d' .env.cli | \
	while IFS='=' read -r k v; do \
	  k=$$(printf '%s' "$$k" | tr -d '[:space:]'); \
	  [ -n "$$k" ] && [ -n "$$v" ] && printf ' -X %s.%s=%s' "$(CLI_PKG)" "$$k" "$$v"; \
	done; true

build-envi: ## Build the envi CLI into ~/.local/bin (settings from .env.cli)
	@flags="$$($(MAKE) -s cli-ldflags)"; \
	go build -ldflags "-X main.version=dev-local$$flags" -o ~/.local/bin/envitest ./cmd/envi; \
	echo "built dev-local with:$${flags:- (no .env.cli; compiled defaults stand)}"

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
