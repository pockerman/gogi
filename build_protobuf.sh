#!/bin/bash
set -euo pipefail

export PATH="$PATH:$(go env GOPATH)/bin"

protos=$(find third_party/protos/gogi/gogi/v1 -name "*.proto")

protoc \
  -I=third_party/protos/gogi \
  --go_out=. \
  --go-grpc_out=. \
  --experimental_allow_proto3_optional \
  $protos
