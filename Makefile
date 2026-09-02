# Envi development helpers.
#
# The databases run from docker-compose.yml — Postgres on :5342 and Redis on
# :6379, with the dev credentials the committed .env already points at
# (user / db / password all "envi"). There is no migration runner, so the schema
# is applied from here: schema.sql is the full current schema (use on a fresh
# database), and migrations/*.sql are the incremental catch-up scripts for an
# existing database (idempotent).
#
# Quick start:  make db        # start the databases and make the schema current
#               make db-reset  # wipe and recreate a clean database
# Run `make help` to list every target.

COMPOSE ?= docker compose
PG_EXEC  := $(COMPOSE) exec -T postgres
PSQL     := $(PG_EXEC) psql -U envi -d envi -v ON_ERROR_STOP=1

.PHONY: help build-envi db db-up db-down db-init db-schema db-migrate db-reset db-psql

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build-envi: ## Build the envi CLI into ~/.local/bin
	cd cmd/envi && go build -o ~/.local/bin/envi

db: db-up db-init ## Start the dev databases and bring the schema up to date

db-up: ## Start Postgres + Redis, waiting until both are healthy
	$(COMPOSE) up -d --wait
	@echo "ready: postgres :5342, redis :6379"

db-down: ## Stop the databases (the data volume is kept)
	$(COMPOSE) down

db-init: db-up ## Apply schema.sql on a fresh DB, else bring migrations up to date
	@if $(PG_EXEC) psql -U envi -d envi -c '\dt' 2>/dev/null | grep -qw sessions; then \
		echo "existing database -> applying migrations"; \
		$(MAKE) --no-print-directory db-migrate; \
	else \
		echo "fresh database -> applying schema.sql"; \
		$(PSQL) < schema.sql && echo "schema applied"; \
	fi

db-schema: db-up ## Apply schema.sql — the full current schema (fresh DB only)
	$(PSQL) < schema.sql
	@echo "schema applied"

db-migrate: db-up ## Apply migrations/*.sql in order (idempotent)
	@for f in migrations/*.sql; do \
		echo "applying $$f"; \
		$(PSQL) < $$f || exit 1; \
	done
	@echo "migrations applied"

db-reset: ## DESTROY the data volume and recreate a clean schema
	$(COMPOSE) down -v
	$(COMPOSE) up -d --wait
	$(PSQL) < schema.sql
	@echo "database reset to a clean schema"

db-psql: db-up ## Open a psql shell on the dev database
	$(COMPOSE) exec postgres psql -U envi -d envi
