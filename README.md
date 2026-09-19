[![gogi](https://github.com/pockerman/gogi/actions/workflows/build.yml/badge.svg)](https://github.com/pockerman/gogi/actions/workflows/build.yml)
# gogi[AI]

gogi is a Go based platform for Generative AI/Agentic  applications. In other words, gogi
is an infrastructire layer that supports applications that use LLM/ML models.
Schematically, this is shown below.

![gogi high level](./docs/imgs/gogi_high_level.png)

gogi evolves around a number of services.

- Model service
- Data service
- Tools service
- Session service
- Prompt service
- Guardrails service
- Monitoring service

## How to install locally

- Pull the source code form <a href="https://github.com/pockerman/gogi">gogi[AI]</a>

```
git pull https://github.com/pockerman/gogi.git .
```

- Update the associated submodules. Specifically, pull the proto files

```
git submodule update --remote --recursive
```

- Build the protofiles by using the ```build_protobuf.sh``` script. You will need to have ```go``` installed
on your machine and also install some dependencies

```
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

- gogi uses <a href="https://temporal.io/">Temporal</a> to manage workflows and various other jobs.

Follow the instructions in the  <a href="https://docs.temporal.io/develop/go/set-up-your-local-go">Install Temporal CLI and start the development server</a> to launch the temporal server locally. Note that currently Temporal runs outside docker, so you need to use the following

```
temporal server start-dev --ip 0.0.0.0 --port 7233 
```

- Use Docker to bring the services up

```
docker compose up --build
```

- gogi uses PostgreSQL as a general backend. You need to apply the initial migrations before using it

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

Unless you do some sort of development on the platform itself, you will need one of the supported SDKs.
This is what your application uses to interact with the platform:

- <a href="https://github.com/pockerman/gogi-python">Python</a>
- <a href="#">Java</a>
- <a href="#">TypeScript</a>


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

Edit `k8/secrets.yaml` first if you need a real `ANTHROPIC_API_KEY` in place
of the `REPLACE_ME` placeholder (the `llms` service will start either way,
but its calls to Anthropic will fail without a real key).

#### 5. Bring up the stateful infrastructure

```
kubectl apply -f k8/postgresql/
kubectl apply -f k8/chromadb/
kubectl apply -f k8/minio/
```

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

## Run the tests

```
go test ./...
```

