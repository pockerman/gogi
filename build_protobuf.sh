#!/bin/bash

export PATH="$PATH:$(go env GOPATH)/bin"

protos=$(find vendor/protos/gogi/v1 -name "*.proto")

protoc \
  -I=vendor/protos \
  --go_out=. \
  --go-grpc_out=. \
  --experimental_allow_proto3_optional \
  $protos
