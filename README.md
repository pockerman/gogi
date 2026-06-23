[![gogi](https://github.com/pockerman/gogi/actions/workflows/build.yml/badge.svg)](https://github.com/pockerman/gogi/actions/workflows/build.yml)
# gogi[AI]

gogi is a Go based platform for Generative AI/Agentic  applications. In other words, gogi
is an infrastructire layer that supports applucations that use LLM/ML models.
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



Unless you do some sort of development on the platform itself, you will need one of the supported SDKs.
This is what your application uses to interact with the platform:

- <a href="https://github.com/pockerman/gogi-python">Python</a>
- <a href="#">Java</a>
- <a href="#">TypeScript</a>


## Run the tests

```
go test ./...
```

