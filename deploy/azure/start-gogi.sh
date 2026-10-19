#!/bin/bash
# Starts gogi on its server, from /opt/gogi: run by gogi.service at every boot, and by the operator to restart it,
# e.g. after a token was put in the vault for the registry: sudo systemctl restart gogi
#
# It mounts the data disk, reads the deployment's secrets from Key Vault as the server's managed identity, writes .env
# and the gateway's certificate, then starts compose.yml. A Terraform template: Terraform fills in what's in $${...}.
set -euo pipefail

vault_url="${vault_url}"
client_id="${client_id}"
cd /opt/gogi

# the data disk, at its LUN; it's formatted the first time only. Ubuntu names it as one of these, by its version
device=""
for _ in $(seq 1 120); do
  for candidate in /dev/disk/azure/data/by-lun/${data_disk_lun} /dev/disk/azure/scsi1/lun${data_disk_lun}; do
    if [ -e "$candidate" ]; then
      device="$(readlink -f "$candidate")"
    fi
  done
  [ -n "$device" ] && break
  echo "waiting for the data disk"
  sleep 5
done
if [ -z "$device" ]; then
  echo "the data disk is not attached" >&2
  exit 1
fi
if ! blkid "$device" >/dev/null; then
  mkfs.xfs "$device"
fi
uuid="$(blkid -s UUID -o value "$device")"
mkdir -p /data
if ! mountpoint -q /data; then
  mount "$device" /data
fi
grep -q " /data " /etc/fstab || echo "UUID=$uuid /data xfs defaults,nofail 0 2" >> /etc/fstab
mkdir -p /data/chroma /data/minio /data/caddy /data/huggingface
# Bitnami's MinIO runs as uid 1001
chown 1001:1001 /data/minio

# a token for Key Vault, as the server's identity, from the instance metadata service
token="$(curl -fsS -H Metadata:true \
  "http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01&resource=https%3A%2F%2Fvault.azure.net&client_id=$client_id" |
  jq -r .access_token)"

secret() {
  curl -fsS -H "Authorization: Bearer $token" "$${vault_url}secrets/$1?api-version=7.4" | jq -r .value
}

database="$(secret deployment-database)"
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
GOGI_MINIO_PASSWORD=$(secret deployment-minio)
GOGI_TLS_NAME=${tls_name}
GOGI_CREDENTIAL_STORE=azure
GOGI_CREDENTIAL_PREFIX=${name}/credentials/
AZURE_KEY_VAULT_URL=$vault_url
AZURE_CLIENT_ID=$client_id
EOF

mkdir -p certs
tls="$(secret deployment-tls)"
jq -r .certificate <<<"$tls" > certs/gateway.crt
jq -r .key <<<"$tls" > certs/gateway.key
umask 022

# the registry's token, if the operator put one: without it, the images are pulled without signing in
if login="$(secret deployment-registry 2>/dev/null)"; then
  jq -r .token <<<"$login" | docker login "${registry}" --username "$(jq -r .username <<<"$login")" --password-stdin
fi

docker compose pull --quiet
docker compose up --detach --remove-orphans
