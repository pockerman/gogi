#!/usr/bin/env bash
#
# Builds the ingestion Function app: this folder's function_app.py and host.json, the worker's code, gogi's
# generated client and the requirements, in one folder to publish, e.g.
#
#   workers/azure_function/build.sh dist/azure-function
#   cd dist/azure-function && func azure functionapp publish <app name> --python
#
# Needs python with grpcio-tools, to generate the client. Azure installs the requirements when it builds the app.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
OUT=$(realpath -m "${1:-$ROOT/dist/azure-function}")

rm -rf "$OUT"
mkdir -p "$OUT/workers"
cp "$ROOT/workers/azure_function/function_app.py" "$ROOT/workers/azure_function/host.json" "$OUT/"
for package in ingest_document worker_utils; do
  cp -r "$ROOT/workers/$package" "$OUT/workers/"
done
find "$OUT" -name __pycache__ -type d -prune -exec rm -rf {} +

{
  echo "azure-functions"
  cat "$ROOT/workers/worker_utils/requirements.txt" "$ROOT/workers/ingest_document/requirements.txt"
} > "$OUT/requirements.txt"

python -m grpc_tools.protoc -I "$ROOT/third_party/protos/gogi" --python_out="$OUT" --grpc_python_out="$OUT" \
  $(find "$ROOT/third_party/protos/gogi/gogi/v1" -name '*.proto')
find "$OUT/gogi" -type d -exec touch {}/__init__.py \;

echo "built the ingestion Function app in $OUT"
