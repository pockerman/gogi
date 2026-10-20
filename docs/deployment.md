# Deploying gogi

One gogi deployment serves many applications: each is registered with gogi, calls it with its own key, and sees
only what it made (see [applications](applications.md)). This is how to deploy it on one server, for a small number
of applications and users.

## What runs where

[`deploy/compose.yml`](../deploy/compose.yml) runs gogi on the server from its published images:

- **Caddy** serves the gateway's gRPC over TLS on port 443. It's the only thing reachable from outside, and only
  from the addresses applications call from.
- **The gateway** takes only calls with an application's key (`GOGI_APPLICATION_KEYS=required`), and the services
  take only calls that say which application they're for (`GOGI_REQUIRE_APPLICATION`).
- **Temporal and Chroma** run on the server, and the two small embedding models gogi runs, each in a text embeddings
  inference server of its own. Chroma's data, and the models the servers download, are kept on a disk of their own
  (`/data`), which outlives the server. The Python workers run no model: the model service embeds text, calling
  OpenAI with an application's key, or a model the application hosts, for any other model.
- **Files**, documents' uploads and prompts' texts, are kept in the cloud's object storage: S3, or Azure Blob
  Storage. MinIO is for development only.
- **Postgres** is the cloud's. gogi's services and Temporal share it, over TLS; Temporal makes its own two
  databases, `temporal` and `temporal_visibility`.
- **Applications' provider keys**, e.g. their Anthropic keys, are kept in the cloud's secrets manager.
- **The migrations** run before the services start, every time the deployment starts.

gogi has no domain, so no public certificate authority will certify its gateway. The deployment is its own
certificate authority instead: it certifies the gateway for the server's public IP address, and applications check
the gateway against the authority's certificate (`GOGI_GATEWAY_CA_FILE` in gogi-python, `rootCertificates` in
gogi-js).

## On AWS

[`deploy/aws`](../deploy/aws) is the Terraform for it:

| | |
|---|---|
| Server | one EC2 instance (`t3.medium`), Amazon Linux 2023, with an Elastic IP; reached with SSM Session Manager, not SSH |
| Data | a 30 GB encrypted EBS volume, snapshotted every day for a week |
| Files | two private, encrypted S3 buckets, `<name>-documents-<suffix>` and `<name>-prompts-<suffix>`, keeping deleted and replaced files for 30 days; the server's role may use only these |
| Postgres | RDS for PostgreSQL 16 (`db.t4g.small`), in private subnets, backed up for a week |
| Secrets | Secrets Manager: the deployment's own, and applications' provider keys under `gogi/credentials/` |
| Certificates | made by Terraform's `tls` provider |

This costs about $75 a month in eu-west-2 at on-demand prices, $34 of it the server. gogi needs about 2.5 GB of its
4 GB, the two small embedding models half a GB of it; a `t3.large` (8 GB, about $35 a month more) leaves room for
large ingestions.

### Deploying

You need Terraform 1.6 or later, and AWS credentials that can manage EC2, VPC, RDS, IAM, Secrets Manager, SSM and
Data Lifecycle Manager.

1. Keep the state somewhere private: it holds the database's password and the certificate authority's key. Copy
   `backend.tf.example` to `backend.tf` and name an S3 bucket of your own.
2. Say where applications will call from, e.g. in `terraform.tfvars`:

   ```hcl
   client_cidrs = ["203.0.113.7/32"]   # qubit-notes' server
   ```

3. Apply it:

   ```bash
   cd deploy/aws
   terraform init
   terraform apply
   terraform output -raw ca_certificate > gogi-ca.pem
   ```

4. If the images are private, put a token that can read them, then restart gogi (below):

   ```bash
   aws secretsmanager put-secret-value --secret-id gogi/deployment/registry \
     --secret-string '{"username":"<GitHub user>","token":"<token with read:packages>"}'
   ```

The server takes a minute or two longer to start the first time: the embedding servers download their models, a few
hundred MB, once.

### Operating it

Reach the server with Session Manager, and work in `/opt/gogi`:

```bash
aws ssm start-session --target "$(terraform output -raw instance_id)"
sudo -i
cd /opt/gogi
docker compose ps
docker compose logs --tail 100 gateway
```

Register an application and give it its key, which gogi shows only once:

```bash
docker compose exec gateway /root/gogi-admin register qubit-notes
```

The application stores its own provider keys in gogi, e.g. `qubit-notes store-provider-key anthropic`. Then let the
application's server in, by adding its address to `client_cidrs` and applying again.

- **Restart gogi**, e.g. after putting the registry's token: `sudo systemctl restart gogi`.
- **New images**: set `image_tag`, e.g. to `sha-<commit>`, and apply. The server is replaced; its data disk is
  moved to the new one.
- **The gateway's certificate** is renewed by `terraform apply` in its last 30 days, which replaces the server.
  The certificate authority's stays the same for ten years, so applications keep theirs.

## On Azure

[`deploy/azure`](../deploy/azure) is the Terraform for it, the same deployment as on AWS:

| | |
|---|---|
| Server | one VM (`Standard_B4ms`), Ubuntu 24.04, with a static public IP; nothing reaches it but the gateway's port, and the operator runs commands with `az vm run-command` |
| Data | a 32 GB managed disk, backed up every day for a week by Azure Backup |
| Files | a storage account with two private containers, `documents` and `prompts`, keeping deleted files for 30 days; it takes no shared keys, only the server's managed identity |
| Postgres | Azure Database for PostgreSQL 16, flexible server (`B_Standard_B2s`), in a subnet of its own, backed up for a week |
| Secrets | one Key Vault: the deployment's own, and applications' provider keys (`GOGI_CREDENTIAL_STORE=azure`) |
| Certificates | made by Terraform's `tls` provider |

The server signs in to Key Vault as its own managed identity. Key Vault's names take only letters, digits and
dashes, and ignore case, so gogi keeps a credential under an encoded name: `gogi/credentials/qubit-notes.anthropic`
is `gogi-2fcredentials-2fqubit-2dnotes-2eanthropic`.

This costs about $210 a month in UK South at pay-as-you-go prices, $140 of it the server.

### Deploying

You need Terraform 1.6 or later, and to be signed in with `az login` as someone who can manage the subscription's
resources and assign roles in it.

1. Keep the state somewhere private: copy `backend.tf.example` to `backend.tf` and name a storage account of your
   own.
2. Say which subscription, and where applications will call from, e.g. in `terraform.tfvars`:

   ```hcl
   subscription_id = "<your subscription>"
   client_cidrs    = ["203.0.113.7/32"]
   ```

3. Apply it, as on AWS:

   ```bash
   cd deploy/azure
   terraform init
   terraform apply
   terraform output -raw ca_certificate > gogi-ca.pem
   ```

4. If the images are private, put a token that can read them, then restart gogi:

   ```bash
   az keyvault secret set --vault-name "$(terraform output -raw key_vault_name)" --name deployment-registry \
     --value '{"username":"<GitHub user>","token":"<token with read:packages>"}'
   ```

### Operating it

Run commands on the server with `az vm run-command`, e.g.:

```bash
az vm run-command invoke --resource-group "$(terraform output -raw resource_group)" \
  --name "$(terraform output -raw vm_name)" --command-id RunShellScript \
  --scripts "cd /opt/gogi && docker compose exec -T gateway /root/gogi-admin register qubit-notes"
```

Restarting gogi (`systemctl restart gogi`), new images and the gateway's certificate work as on AWS. Postgres is
locked against being deleted while `deletion_protection` is set.

## Where files are kept

The documents, indexes and prompts services, and the Python workers, choose the object storage with
`GOGI_OBJECT_STORE`:

| Value | Store | Settings |
|---|---|---|
| `s3` (default) | S3, or an S3-compatible store | `GOGI_S3_ENDPOINT`, e.g. `s3.eu-west-2.amazonaws.com`; `AWS_REGION`; signed in as the server's role, or with `GOGI_S3_ACCESS_KEY` and `GOGI_S3_SECRET_KEY`; `GOGI_S3_SECURE=false` for plain HTTP |
| `azure` | Azure Blob Storage | `AZURE_STORAGE_ACCOUNT_URL`, e.g. `https://gogi.blob.core.windows.net/`; signed in as the VM's managed identity (`AZURE_CLIENT_ID`) |

The buckets are `GOGI_DOCUMENTS_BUCKET` and `GOGI_PROMPTS_BUCKET`, `gogi-documents` and `gogi-prompts` by default.
On Azure, a bucket is a container of the storage account. A store makes a bucket that isn't there when it first
puts a file in it, as development's MinIO needs; in the cloud, Terraform makes them.

Without `GOGI_S3_ENDPOINT`, the services use development's MinIO, as `docker-compose.yml` sets it: `GOGI_MINIO_HOST`,
`GOGI_MINIO_ROOT_USER` and `GOGI_MINIO_ROOT_PASSWORD` (`MINIO_HOST`, ... in the Python workers), over plain HTTP.

The stores' tests run against MinIO and Azurite, Azure's storage emulator, where they're given:

```bash
docker run -d -p 10000:10000 mcr.microsoft.com/azure-storage/azurite:3.35.0 azurite-blob --blobHost 0.0.0.0 --skipApiVersionCheck
GOGI_TEST_S3_ENDPOINT=localhost:9000 GOGI_TEST_S3_ACCESS_KEY=admin GOGI_TEST_S3_SECRET_KEY=password \
GOGI_TEST_AZURE_BLOB_URL=http://127.0.0.1:10000/devstoreaccount1 go test ./gogi/storage/objects/
```
