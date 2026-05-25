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

- Use Docker to bring the services up

```
docker compose up --build
```

Unless you do some sort of development on the platform itself, you will need one of the supported SDKs.
This is what your application uses to interact with the platform:

- <a href="https://github.com/pockerman/gogi-python">Python</a>
- <a href="#">Java</a>
- <a href="#">TypeScript</a>


## Run the tests

```
go test ./...
```

