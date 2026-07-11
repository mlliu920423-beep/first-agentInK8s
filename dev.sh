#!/usr/bin/env bash
# Local dev bootstrap. Two-terminal workflow.
set -euo pipefail
cd "$(dirname "$0")"

if [ "${1:-}" = "web" ]; then
  cd web && npm install && exec npm run dev
fi

if [ "${1:-}" = "server" ]; then
  : "${ARK_API_KEY:?set ARK_API_KEY}"
  : "${ARK_MODEL_ID:?set ARK_MODEL_ID}"
  exec go run ./cmd/server
fi

cat <<'USAGE'
Usage:
  ./dev.sh web        # in one terminal — starts Vite on :5173 with /api proxy to :8080
  ./dev.sh server     # in another  — starts Go on :8080 (needs ARK_API_KEY/ARK_MODEL_ID)

Browser: http://localhost:5173 (dev, hot reload)  or  http://localhost:8080 (single binary)
USAGE
