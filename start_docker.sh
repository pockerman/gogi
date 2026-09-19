#!/usr/bin/env bash
#
# Brings up the gogi platform with Docker Compose, following the
# "How to install locally" steps in README.md.
#
# Usage: ./start_docker.sh [options]
#
#   --skip-submodules   Don't update the third_party/protos/gogi submodule
#   --skip-protobuf     Don't (re)install protoc plugins or regenerate .pb.go files
#   --skip-build        Run 'docker compose up' without '--build'
#   --skip-migrate      Don't run database migrations
#   --foreground         Run docker compose attached (default: detached with -d)
#   -h, --help          Show this help
#
# Requires: docker (with the compose plugin), migrate (golang-migrate).
# The protobuf step additionally requires go and protoc on PATH.
#
# Not started by this script: gogi uses Temporal to manage workflows, and
# Temporal runs outside Docker (see README). Start it yourself first:
#   temporal server start-dev --ip 0.0.0.0 --port 7233

set -euo pipefail

POSTGRES_DSN="postgres://gogi:gogi@localhost:5432/gogi?sslmode=disable"
TEMPORAL_HOST=127.0.0.1
TEMPORAL_PORT=7233

SKIP_SUBMODULES=false
SKIP_PROTOBUF=false
SKIP_BUILD=false
SKIP_MIGRATE=false
FOREGROUND=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-submodules) SKIP_SUBMODULES=true; shift ;;
    --skip-protobuf) SKIP_PROTOBUF=true; shift ;;
    --skip-build) SKIP_BUILD=true; shift ;;
    --skip-migrate) SKIP_MIGRATE=true; shift ;;
    --foreground) FOREGROUND=true; shift ;;
    -h|--help)
      sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

command -v docker >/dev/null 2>&1 || { echo "error: 'docker' is required but not found on PATH" >&2; exit 1; }
docker compose version >/dev/null 2>&1 || { echo "error: 'docker compose' plugin is required but not available" >&2; exit 1; }
if [[ "$SKIP_MIGRATE" == false ]]; then
  command -v migrate >/dev/null 2>&1 || { echo "error: 'migrate' is required but not found on PATH (see README for install instructions)" >&2; exit 1; }
fi

echo "==> 1. Updating the proto submodule"
if [[ "$SKIP_SUBMODULES" == true ]]; then
  echo "    skipped (--skip-submodules)"
else
  git submodule update --init --recursive
fi

echo "==> 2. Generating protobuf Go bindings"
if [[ "$SKIP_PROTOBUF" == true ]]; then
  echo "    skipped (--skip-protobuf)"
else
  command -v go >/dev/null 2>&1 || { echo "error: 'go' is required to generate protobuf bindings (or pass --skip-protobuf if already generated)" >&2; exit 1; }
  command -v protoc >/dev/null 2>&1 || { echo "error: 'protoc' is required to generate protobuf bindings (or pass --skip-protobuf if already generated)" >&2; exit 1; }
  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
  ./build_protobuf.sh
fi

echo "==> 3. Checking for a local Temporal dev server"
if (exec 3<>"/dev/tcp/${TEMPORAL_HOST}/${TEMPORAL_PORT}") 2>/dev/null; then
  exec 3<&- 3>&-
  echo "    found on ${TEMPORAL_HOST}:${TEMPORAL_PORT}"
else
  echo "    warning: no Temporal server reachable on ${TEMPORAL_HOST}:${TEMPORAL_PORT}."
  echo "    Document ingestion workflows need it. Start it in another terminal with:"
  echo "      temporal server start-dev --ip 0.0.0.0 --port ${TEMPORAL_PORT}"
fi

echo "==> 4. Bringing up the platform with Docker Compose"
COMPOSE_UP_ARGS=(up)
[[ "$SKIP_BUILD" == false ]] && COMPOSE_UP_ARGS+=(--build)
[[ "$FOREGROUND" == false ]] && COMPOSE_UP_ARGS+=(-d)
docker compose "${COMPOSE_UP_ARGS[@]}"

if [[ "$FOREGROUND" == true ]]; then
  # Compose is attached and blocking; migrations/status happen on a separate run.
  exit 0
fi

echo "==> 5. Waiting for Postgres to become ready"
for _ in $(seq 1 30); do
  if docker compose exec -T postgres pg_isready -U gogi >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
docker compose exec -T postgres pg_isready -U gogi

echo "==> 6. Running database migrations"
if [[ "$SKIP_MIGRATE" == true ]]; then
  echo "    skipped (--skip-migrate)"
else
  migrate -path migrations -database "$POSTGRES_DSN" up
fi

echo "==> 7. Checking the deployment"
docker compose ps

echo "==> gogi platform is up. Gateway: http://localhost:8080 (gRPC :50051)."
echo "    Note: docker-compose.yml's llms service ships with a placeholder"
echo "    ANTHROPIC_API_KEY; edit it there for real Anthropic calls to work."
echo "    Use 'docker compose logs -f <service>' to tail a service, and"
echo "    'docker compose down' to tear the platform down."
