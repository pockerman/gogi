#!/usr/bin/env bash
#
# Brings up the gogi platform on a local Minikube cluster, following the
# "Kubernetes installation (development)" steps in README.md.
#
# Usage: ./start.sh [options]
#
#   --skip-minikube-start   Don't start/create the Minikube cluster (use if it's already running)
#   --skip-build            Don't rebuild the docker images into Minikube
#   --skip-migrate          Don't port-forward Postgres and run migrations
#   -h, --help              Show this help
#
# Requires: minikube, kubectl, docker, migrate (golang-migrate) on PATH.

set -euo pipefail

NAMESPACE=gogi
CPUS=4
MEMORY=6g
POSTGRES_DSN="postgres://gogi:gogi@localhost:5432/gogi?sslmode=disable"

SKIP_MINIKUBE_START=false
SKIP_BUILD=false
SKIP_MIGRATE=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-minikube-start) SKIP_MINIKUBE_START=true; shift ;;
    --skip-build) SKIP_BUILD=true; shift ;;
    --skip-migrate) SKIP_MIGRATE=true; shift ;;
    -h|--help)
      sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'
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

for cmd in minikube kubectl docker; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "error: '$cmd' is required but not found on PATH" >&2; exit 1; }
done
if [[ "$SKIP_MIGRATE" == false ]]; then
  command -v migrate >/dev/null 2>&1 || { echo "error: 'migrate' is required but not found on PATH (see README for install instructions)" >&2; exit 1; }
fi

PORT_FORWARD_PID=""
cleanup() {
  if [[ -n "$PORT_FORWARD_PID" ]]; then
    kill "$PORT_FORWARD_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

echo "==> 1. Starting the Minikube cluster"
if [[ "$SKIP_MINIKUBE_START" == true ]]; then
  echo "    skipped (--skip-minikube-start)"
else
  minikube start \
    --driver=docker \
    --kubernetes-version=stable \
    --cpus="$CPUS" \
    --memory="$MEMORY"
fi

echo "==> 2. Creating the namespace"
kubectl apply -f k8/namespace.yaml

echo "==> 3. Building docker images into Minikube"
if [[ "$SKIP_BUILD" == true ]]; then
  echo "    skipped (--skip-build)"
else
  minikube image build -t gogi/gateway:latest -f docker/gateway.Dockerfile .
  minikube image build -t gogi/documents:latest -f docker/documents.Dockerfile .
  minikube image build -t gogi/indexes:latest -f docker/indexes.Dockerfile .
  minikube image build -t gogi/llms:latest -f docker/llms.Dockerfile .
  minikube image build -t gogi/prompts:latest -f docker/prompts.Dockerfile .
  minikube image build -t gogi/llm-sessions:latest -f docker/llm_sessions.Dockerfile .
  minikube image build -t gogi/llm-tools:latest -f docker/llm_tools.Dockerfile .
  minikube image build -t gogi/workflows:latest -f docker/workflows.Dockerfile .
fi

echo "==> 4. Applying the config map and secret"
kubectl apply -f k8/configmap.yaml
kubectl apply -f k8/secrets.yaml

echo "==> 5. Bringing up the stateful infrastructure"
kubectl apply -f k8/postgresql/
kubectl apply -f k8/chromadb/
kubectl apply -f k8/minio/

echo "    waiting for postgres to become ready..."
kubectl wait --for=condition=Ready pod -l app=postgres -n "$NAMESPACE" --timeout=180s

echo "==> 6. Running database migrations"
if [[ "$SKIP_MIGRATE" == true ]]; then
  echo "    skipped (--skip-migrate)"
else
  kubectl port-forward svc/postgres -n "$NAMESPACE" 5432:5432 &
  PORT_FORWARD_PID=$!
  sleep 2

  migrate -path migrations -database "$POSTGRES_DSN" up

  kill "$PORT_FORWARD_PID" 2>/dev/null || true
  PORT_FORWARD_PID=""
fi

echo "==> 7. Deploying the platform services"
kubectl apply -f k8/indexes/
kubectl apply -f k8/documents/
kubectl apply -f k8/gateway/
kubectl apply -f k8/llms/
kubectl apply -f k8/prompts/
kubectl apply -f k8/llm-sessions/
kubectl apply -f k8/llm-tools/
kubectl apply -f k8/workflows/

echo "==> 8. Checking the deployment"
kubectl get pods -n "$NAMESPACE"

echo "==> gogi platform deployment applied. Use 'kubectl get pods -n $NAMESPACE' to watch rollout status."
