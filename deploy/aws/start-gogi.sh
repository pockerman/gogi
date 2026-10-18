#!/bin/bash
# Starts gogi on its server, from /opt/gogi: run by gogi.service at every boot, and by the operator to restart it,
# e.g. after a key was put in the registry's secret: sudo systemctl restart gogi
#
# It mounts the data disk, reads the deployment's secrets and writes .env and the gateway's certificate, then starts
# compose.yml. A Terraform template: Terraform fills in what's in $${...}.
set -euo pipefail

export AWS_REGION="${region}"
cd /opt/gogi

# the data disk, as the server's NVMe device of the volume's id; it's formatted the first time only
volume_id="${data_volume_id}"
device="/dev/disk/by-id/nvme-Amazon_Elastic_Block_Store_$${volume_id//-/}"
for _ in $(seq 1 120); do
  [ -e "$device" ] && break
  echo "waiting for the data disk $volume_id"
  sleep 5
done
if [ ! -e "$device" ]; then
  echo "the data disk $volume_id is not attached" >&2
  exit 1
fi
if ! blkid "$device" >/dev/null; then
  mkfs.xfs "$device"
fi
mkdir -p /data
if ! mountpoint -q /data; then
  mount "$device" /data
fi
grep -q " /data " /etc/fstab || echo "$device /data xfs defaults,nofail 0 2" >> /etc/fstab
mkdir -p /data/chroma /data/caddy /data/huggingface

secret() {
  aws secretsmanager get-secret-value --secret-id "$1" --query SecretString --output text
}

database="$(secret "${database_secret}")"
field() {
  jq -r ".$1" <<<"$database"
}

umask 077
cat > .env <<EOF
GOGI_IMAGES=${images}
GOGI_IMAGE_TAG=${image_tag}
GOGI_DATA_DIR=/data
GOGI_DB_HOST=$(field host)
GOGI_DB_PORT=$(field port)
GOGI_DB_NAME=$(field dbname)
GOGI_DB_USER=$(field username)
GOGI_DB_PASSWORD=$(field password)
GOGI_OBJECT_STORE=s3
GOGI_S3_ENDPOINT=s3.${region}.amazonaws.com
GOGI_DOCUMENTS_BUCKET=${documents}
GOGI_PROMPTS_BUCKET=${prompts}
GOGI_TLS_NAME=${tls_name}
GOGI_CREDENTIAL_STORE=aws
GOGI_CREDENTIAL_PREFIX=${name}/credentials/
AWS_REGION=${region}
EOF

mkdir -p certs
tls="$(secret "${tls_secret}")"
jq -r .certificate <<<"$tls" > certs/gateway.crt
jq -r .key <<<"$tls" > certs/gateway.key
umask 022

# the registry's token, if the operator put one: without it, the images are pulled without signing in
if login="$(secret "${registry_secret}" 2>/dev/null)"; then
  jq -r .token <<<"$login" | docker login "${registry}" --username "$(jq -r .username <<<"$login")" --password-stdin
fi

docker compose pull --quiet
docker compose up --detach --remove-orphans
