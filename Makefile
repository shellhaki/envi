# Envi development helpers.
#
# Postgres is expected to be running natively (not via docker-compose — that
# setup is on hold for now; see docker-compose.yml if you bring it back) with
# DATABASE_URL in .env pointing at it. There is no migration runner, so the
# schema is applied from here: schema.sql is the full current schema (use on a
# fresh database), and migrations/*.sql are the incremental catch-up scripts
# for an existing database (idempotent).
#
# Quick start:  make db-init  # apply schema.sql, or bring migrations up to date
# Run `make help` to list every target.

DATABASE_URL ?= $(shell grep -E '^DATABASE_URL=' .env 2>/dev/null | cut -d= -f2-)
PSQL         := psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1

.PHONY: help build-envi db-init db-schema db-migrate db-psql

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build-envi: ## Build the envi CLI into ~/.local/bin
	cd cmd/envi && go build -o ~/.local/bin/envi

db-init: ## Apply schema.sql on a fresh DB (DATABASE_URL from .env), else bring migrations up to date
	@if [ -z "$(DATABASE_URL)" ]; then echo "DATABASE_URL is not set (check .env)"; exit 1; fi
	@if $(PSQL) -c '\dt' 2>/dev/null | grep -qw sessions; then \
		echo "existing database -> applying migrations"; \
		$(MAKE) --no-print-directory db-migrate; \
	else \
		echo "fresh database -> applying schema.sql"; \
		$(PSQL) < schema.sql && echo "schema applied"; \
	fi

db-schema: ## Apply schema.sql — the full current schema (fresh DB only)
	@if [ -z "$(DATABASE_URL)" ]; then echo "DATABASE_URL is not set (check .env)"; exit 1; fi
	$(PSQL) < schema.sql
	@echo "schema applied"

db-migrate: ## Apply migrations/*.sql against DATABASE_URL (idempotent)
	@if [ -z "$(DATABASE_URL)" ]; then echo "DATABASE_URL is not set (check .env)"; exit 1; fi
	@for f in migrations/*.sql; do \
		echo "applying $$f"; \
		$(PSQL) < $$f || exit 1; \
	done
	@echo "migrations applied"

db-psql: ## Open a psql shell on the database
	@if [ -z "$(DATABASE_URL)" ]; then echo "DATABASE_URL is not set (check .env)"; exit 1; fi
	psql "$(DATABASE_URL)"
