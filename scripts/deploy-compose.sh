#!/usr/bin/env bash

set -euo pipefail

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "missing required env var: $name" >&2
    exit 1
  fi
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="${PHOTON_ENV_FILE:-/etc/photon/photon.env}"
project="${PHOTON_COMPOSE_PROJECT:-photon}"

if [[ ! -r "$env_file" ]]; then
  echo "missing readable Photon env file: $env_file" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "$env_file"
set +a

require_env PHOTON_POSTGRES_PASSWORD
require_env PHOTON_STORAGE_ACCESS_KEY
require_env PHOTON_STORAGE_SECRET_KEY

compose=(
  docker compose
  --project-name "$project"
  --env-file "$env_file"
  -f "$repo_root/deploy/docker/docker-compose.yml"
  -f "$repo_root/deploy/docker/docker-compose.production.yml"
)

"${compose[@]}" config >/dev/null
"${compose[@]}" up -d --build --remove-orphans

api_port="${PHOTON_API_PUBLISHED_PORT:-18080}"
deadline=$((SECONDS + 300))

until curl -fsS "http://127.0.0.1:${api_port}/readyz" >/dev/null; do
  if (( SECONDS >= deadline )); then
    "${compose[@]}" ps >&2 || true
    "${compose[@]}" logs --tail=120 api worker migrate createbuckets >&2 || true
    echo "Photon did not become ready before timeout" >&2
    exit 1
  fi
  sleep 5
done

"${compose[@]}" ps
