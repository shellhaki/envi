#!/usr/bin/env bash
set -euo pipefail

export GOCACHE="${GOCACHE:-/tmp/envi-go-cache}"
(
  cd cli
  test -z "$(gofmt -l .)"
  go vet ./...
  go test -race ./...
)
(
  cd web
  bun install --frozen-lockfile
  bun test
  bun run lint
  bun run build --webpack
)
