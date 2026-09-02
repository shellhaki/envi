#!/usr/bin/env bash
set -euo pipefail

export GOCACHE="${GOCACHE:-/tmp/envi-go-cache}"
test -z "$(gofmt -l cmd internal)"
go vet ./cmd/... ./internal/...
go test -race ./cmd/... ./internal/...
(
  cd ui
  bun install --frozen-lockfile
  bun test
  bun run lint
  bun run build --webpack
)
