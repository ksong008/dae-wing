#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$ROOT"

if rg -n -i \
  -e 'graphql' \
  -e 'graphiql' \
  -e '/graphql' \
  -e 'graph-gophers' \
  -e 'graphql-go' \
  -e 'machinebox/graphql' \
  -e '99designs/gqlgen' \
  -e 'gqlgen' \
  --glob 'go.mod' \
  --glob 'go.sum' \
  --glob 'README.md' \
  --glob 'THREE_LAYER_REFACTOR.md' \
  --glob 'cmd/**' \
  --glob 'engine/**' \
  --glob 'orchestrator/**' \
  --glob 'transport/**' \
  .; then
  echo "retired transport markers are not allowed in active dae-wing runtime/control-plane files"
  exit 1
fi
