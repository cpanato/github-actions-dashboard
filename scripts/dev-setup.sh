#!/usr/bin/env bash
# Build a local demo or OAuth development instance backed by PostgreSQL.
set -euo pipefail

cd "$(dirname "$0")/.."
case "${1:-demo}" in
  demo) dashboard_config="config/demo.yml" ;;
  oauth) dashboard_config="config/local.dev.yml" ;;
  *) echo "Usage: $0 [demo|oauth]" >&2; exit 1 ;;
esac

docker compose up -d --wait postgres
go build -o github-actions-dashboard .

if [[ "${1:-demo}" == "oauth" ]]; then
  echo "Set GitHub App/OAuth credentials and DASHBOARD_ALLOWED_USERS and/or DASHBOARD_ALLOWED_ORGANIZATIONS."
  echo "See docs/AUTHENTICATION.md for callback and webhook configuration."
fi
printf 'Run: ./github-actions-dashboard -config %s\n' "$dashboard_config"
printf 'Open: http://127.0.0.1:8080\n'
printf 'Stop PostgreSQL: docker compose down (database volume retained)\n'
