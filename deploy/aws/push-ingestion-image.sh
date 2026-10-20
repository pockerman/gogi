#!/usr/bin/env bash
#
# Copies the ingestion Lambda's image to the deployment's ECR repository, where Lambda can run it, e.g.
#
#   deploy/aws/push-ingestion-image.sh ghcr.io/pockerman/gogi-ingest-lambda:sha-<commit>
#
# then set ingestion_image_tag to the tag it prints and run terraform apply. Without an image it builds one from the
# working tree. Needs docker, the AWS CLI signed in, and terraform's state (it's run in deploy/aws).
set -euo pipefail

cd "$(dirname "$0")"
repository=$(terraform output -raw ingestion_repository)
region=$(terraform output -json | jq -r '.ingestion_repository.value | split(".")[3]')

if [ $# -ge 1 ]; then
  source_image=$1
  docker pull --platform linux/amd64 "$source_image"
else
  source_image=gogi-ingest-lambda:local
  docker build --platform linux/amd64 --provenance=false -f ../../docker/ingest_document_lambda.Dockerfile \
    -t "$source_image" ../..
fi

tag=$(docker image inspect --format '{{.Id}}' "$source_image" | cut -c8-19)
aws ecr get-login-password --region "$region" | docker login --username AWS --password-stdin "${repository%%/*}"
docker tag "$source_image" "$repository:$tag"
docker push "$repository:$tag"

echo "pushed $repository:$tag; set ingestion_image_tag = \"$tag\" and run terraform apply"
