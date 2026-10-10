# Install ``gogi``

This document describes how to install ``gogi`` and some of its dependencies.

## Go install


``gogi`` is a Go based AI platform so you will need to have the ``go`` development environment installed on your machine as well as some dependencies for protobuffers:

```
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

## Submodule update

Additionally, before installing ``gogi`` you need to update the submodules it depends on.
Specifically, pull the proto files:

```
git submodule update --remote --recursive
```

Once the submodule(s) have been updated, you will need to build the protobuffers:
Use the script ```build_protobuf.sh```. 


## Temporal installation

``gogi`` uses <a href="https://temporal.io/">Temporal</a> to manage workflows and various other jobs.
Follow the instructions in the  <a href="https://docs.temporal.io/develop/go/set-up-your-local-go">Install Temporal CLI and start the development server</a> to launch the temporal server locally. Note that currently Temporal runs outside docker, so you need to use the following:

```
temporal server start-dev --ip 0.0.0.0 --port 7233 
```


## Docker installation

This is the fastest way to get the full ``gogi`` platform running locally. It uses
Docker Compose to start every service (gateway, data, LLM, tools, sessions, prompts,
workflows, ingest workers) plus their backing infrastructure (PostgreSQL, ChromaDB,
MinIO).

#### Prerequisites

- ``docker`` with the Compose plugin (``docker compose version`` should work)
- ``go`` and ``protoc`` on your ``PATH`` (needed once, to generate the protobuf
  bindings before the images are built — see [Go install](#go-install))
- ``migrate`` (golang-migrate), used to apply the database migrations — see
  [Database migration](#database-migration)
- Temporal running locally, since it is **not** started by Docker Compose — see
  [Temporal installation](#temporal-installation)

#### 1. Run the install script

From the project root:

```
./start_docker.sh
```

This script automates the whole local setup:

1. Updates the ``third_party/protos/gogi`` submodule.
2. Installs the ``protoc-gen-go`` / ``protoc-gen-go-grpc`` plugins and regenerates
   the ``.pb.go`` bindings via ``build_protobuf.sh``.
3. Warns if no Temporal dev server is reachable on ``127.0.0.1:7233``.
4. Runs ``docker compose up --build -d`` to build the images and start the platform.
5. Waits for PostgreSQL to become ready.
6. Applies the database migrations with ``migrate``.
7. Prints the status of every container.

Useful flags (combine as needed):

| Flag | Effect |
|------|--------|
| ``--skip-submodules`` | Don't update the ``third_party/protos/gogi`` submodule |
| ``--skip-protobuf`` | Don't (re)install protoc plugins or regenerate ``.pb.go`` files |
| ``--skip-build`` | Run ``docker compose up`` without ``--build`` |
| ``--skip-migrate`` | Don't run database migrations |
| ``--foreground`` | Run ``docker compose up`` attached instead of detached |
| ``-h``, ``--help`` | Show the script's help text |

Once it finishes, the gateway is reachable at ``http://localhost:8080`` (HTTP) and
``localhost:50051`` (gRPC).

> **Note:** ``docker-compose.yml``'s ``llms`` service ships with a placeholder
> ``ANTHROPIC_API_KEY`` and ``OPENAI_API_KEY``. Edit them there before you can make
> real Anthropic or OpenAI API calls. Set ``OPENAI_BASE_URL`` to point the OpenAI
> provider at an OpenAI-compatible endpoint; like in the OpenAI SDKs, the URL includes
> the API version, e.g. ``https://api.openai.com/v1``.

#### 2. Managing the deployment

```
docker compose logs -f <service>   # tail a specific service
docker compose ps                  # check container status
docker compose down                # tear the platform down
```

#### 3. Running Docker Compose manually

If you'd rather not use the script (for example, the protobuf bindings are already
generated and you only want to (re)start the containers), you can drive Docker
Compose directly:

```
docker compose up --build -d
docker compose exec -T postgres pg_isready -U gogi
migrate -path migrations -database "postgres://gogi:gogi@localhost:5432/gogi?sslmode=disable" up
```

## Kubernetes installation (development)

For the instructions below you will need ``minikube`` installed (see instructions how to install for tyour OS here: https://minikube.sigs.k8s.io/docs/start/?arch=%2Flinux%2Fx86-64%2Fstable%2Fbinary+download). You will also need ``kubectl`` see here: https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/ how to install this on your machine.


#### 1. Start the cluster

```
minikube start \
  --driver=docker \
  --kubernetes-version=stable \
  --cpus=4 \
  --memory=6g
```

#### 2. Create the namespace

Everything else below targets `namespace: gogi`, so it must exist first:

```
kubectl apply -f k8/namespace.yaml
```

#### 3. Build docker images directly into Minikube (recommended for development)

`minikube image build` builds inside the Minikube VM/container's own Docker
daemon, not your host's, so the images are already there for the kubelet to
pull with `imagePullPolicy: IfNotPresent`.

```
minikube image build -t gogi/gateway:latest -f docker/gateway.Dockerfile .
minikube image build -t gogi/documents:latest -f docker/documents.Dockerfile .
minikube image build -t gogi/indexes:latest -f docker/indexes.Dockerfile .
minikube image build -t gogi/llms:latest -f docker/llms.Dockerfile .
minikube image build -t gogi/prompts:latest -f docker/prompts.Dockerfile .
minikube image build -t gogi/llm-sessions:latest -f docker/llm_sessions.Dockerfile .
minikube image build -t gogi/llm-tools:latest -f docker/llm_tools.Dockerfile .
minikube image build -t gogi/workflows:latest -f docker/workflows.Dockerfile .
```

#### 4. Apply the config map and secret

Every service below reads its env vars from these via `envFrom`, so apply
them first — every other deployment will sit in `CreateContainerConfigError`
without them:

```
kubectl apply -f k8/configmap.yaml
kubectl apply -f k8/secrets.yaml
```

Edit `k8/secrets.yaml` first if you need a real `ANTHROPIC_API_KEY` or
`OPENAI_API_KEY` in place of the `REPLACE_ME` placeholders (the `llms` service
will start either way, but its calls to that provider will fail without a real key).

#### 5. Bring up the stateful infrastructure

```
kubectl apply -f k8/postgresql/
kubectl apply -f k8/chromadb/
kubectl apply -f k8/minio/
kubectl apply -f k8/vault/
```

`k8/vault/` runs a HashiCorp Vault dev server, the credential store of the `llms` service
in development (see [Credentials for registered models](#credentials-for-registered-models)).

#### 6. Run the database migrations

Postgres has no host port exposed in-cluster (unlike `docker compose`), so
port-forward to it first:

```
kubectl port-forward svc/postgres -n gogi 5432:5432 &
migrate \
  -path migrations \
  -database "postgres://gogi:gogi@localhost:5432/gogi?sslmode=disable" \
  up
```

#### 7. Deploy the platform services

```
kubectl apply -f k8/indexes/
kubectl apply -f k8/documents/
kubectl apply -f k8/gateway/
kubectl apply -f k8/llms/
kubectl apply -f k8/prompts/
kubectl apply -f k8/llm-sessions/
kubectl apply -f k8/llm-tools/
kubectl apply -f k8/workflows/
```

#### 8. Check the deployment

```
kubectl get pods -n gogi
```

All pods should reach `Running` / `1/1 Ready`. If any sit in
`CrashLoopBackOff`, `kubectl logs -n gogi <pod>` will usually point straight
at a missing env var or an unmigrated table.


## Database migration

``gogi`` uses PostgreSQL as a general backend. You need to apply the initial migrations before using it

```
curl -L https://github.com/golang-migrate/migrate/releases/latest/download/migrate.linux-amd64.tar.gz | tar xvz
sudo mv migrate /usr/local/bin/
```

Once the services are up and running apply the needed migrations. You need to be at the project root directory for this.

```
migrate \
  -path migrations \
  -database "postgres://gogi:gogi@localhost:5432/gogi?sslmode=disable" \
  up
```

Verify that the migrations have been applied. From your host machine:

```
psql postgres://gogi:gogi@localhost:5432/gogi
\d
```

You will need to have a postgres client installed for the above to work.
The migrations create the following tables:

| Migration | Tables |
|-----------|--------|
| 000001 / 000002 | `jobs` |
| 000003 | `gogi_indexes` |
| 000004 | `gogi_llm_sessions`, `gogi_llm_messages`, `gogi_user_memory` |
| 000005 | `gogi_prompts` |
| 000006 | `gogi_tools`, `gogi_tool_tasks` |
| 000007 | `gogi_workflows`, `gogi_workflow_deployments`, `gogi_routes`, `gogi_workflow_jobs` |
| 000008 | `gogi_registered_llms` |

## Credentials for registered models

A self-hosted model registered with the `llms` service (`RegisterLLM`) can reference a
credential by name with `credential_ref`, e.g. `ml-inference-prod`. The secret itself lives in
a secrets manager; the service reads it for every request to the model and sends it as the
API key, so the secret never appears in registrations, logs or responses, and a rotated secret
is used without re-registering the model.

The `llms` service selects the credential store with `GOGI_CREDENTIAL_STORE`:

| Value            | Store                | Settings                                                              |
|------------------|----------------------|-----------------------------------------------------------------------|
| `none` (default) | No credential store  | Registrations with a `credential_ref` are rejected                   |
| `aws`            | AWS Secrets Manager  | The standard AWS variables: `AWS_REGION`, credentials, and `AWS_ENDPOINT_URL` for LocalStack |
| `vault`          | HashiCorp Vault (KV v2) | `VAULT_ADDR`, `VAULT_TOKEN`, `GOGI_VAULT_KV_MOUNT` (default `secret`) |

A credential named `<name>` is the secret `gogi/credentials/<name>`: in AWS Secrets Manager its
secret string is the credential; in Vault the credential is the `value` key of the secret.
`GOGI_CREDENTIAL_PREFIX` changes the `gogi/credentials/` prefix. The service caches credentials
for `GOGI_CREDENTIAL_CACHE_TTL` (default `1m`), so a secret rotated in the secrets manager is
used within that time.

#### Local development

Docker Compose and the Kubernetes manifests run a Vault dev server, which the `llms` service
uses by default. The dev server keeps secrets in memory (they are lost on restart) and uses the
fixed root token `gogi-dev-root-token`: never use it outside development. Store a credential with:

```
docker exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN=gogi-dev-root-token gogi-vault \
  vault kv put secret/gogi/credentials/ml-inference-prod value=<the-api-key>
```

To use AWS Secrets Manager instead, start [LocalStack](https://www.localstack.cloud/), which
emulates it, with the `aws` profile, and store the credential there:

```
GOGI_CREDENTIAL_STORE=aws docker compose --profile aws up -d
docker exec gogi-localstack awslocal secretsmanager create-secret \
  --name gogi/credentials/ml-inference-prod --secret-string <the-api-key>
```

Rotate a credential with `vault kv put` (Vault) or `awslocal secretsmanager put-secret-value`
(LocalStack) on the same name.

## Run the tests

```
go test ./...
```

Some tests run against real services and are skipped unless these variables are set:

| Variable                     | Tests                                     | Example                                        |
|------------------------------|-------------------------------------------|------------------------------------------------|
| `GOGI_TEST_POSTGRES_DSN`     | Registered models repository (empties `gogi_registered_llms`; use a test database) | `postgres://postgres:test@localhost:5432/gogi?sslmode=disable` |
| `GOGI_TEST_AWS_ENDPOINT_URL` | AWS Secrets Manager credential store      | `http://localhost:4566` (LocalStack)           |
| `GOGI_TEST_VAULT_ADDR`, `GOGI_TEST_VAULT_TOKEN` | HashiCorp Vault credential store | `http://localhost:8200`, `root` (`vault server -dev -dev-root-token-id=root`) |
