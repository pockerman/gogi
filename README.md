[![gogi](https://github.com/pockerman/gogi/actions/workflows/build.yml/badge.svg)](https://github.com/pockerman/gogi/actions/workflows/build.yml)
# gogi[AI]

gogi is a platform, written in Go, for building generative AI and agentic applications. It is the infrastructure
layer underneath an application that uses LLMs. Instead of talking to model providers, vector stores and secret
managers itself, the application calls the platform's services through an SDK. The platform takes care of what
every such application needs: running models reliably, keeping conversations, retrieving knowledge, calling tools
safely, enforcing policies, and seeing what every request did and cost.

![gogi high level](./docs/imgs/gogi_high_level.png)

## Services

| Service                                              | Responsibility                                                        |
|------------------------------------------------------|-----------------------------------------------------------------------|
| [Model service](docs/model_service.md)               | Runs models from any provider with one API; retries, fallbacks, registered self-hosted models |
| [Prompt service](docs/prompt_service.md)             | Versioned prompts and their metadata                                  |
| [Session service](docs/session_service.md)           | Conversations, memories, and context that fits a token budget         |
| Data services (indexes, documents)                   | Indexes, document ingestion and search                                |
| [Tool service](docs/tool_service.md)                 | A registry of tools, executed with injected credentials               |
| [Guardrails service](docs/guardrails_service.md)     | Input, output and action policies, with an audit trail                |
| [Observability service](docs/observability_service.md) | Traces, scores, logs, metrics, cost and budgets                     |
| Workflow service                                     | Workflows, their deployments, and long-running jobs                   |

Applications reach every service through a single gateway. The [overview](docs/gogi_overview.md) explains how the
pieces fit together and how a request flows through them.

## Getting started

- [Installation](docs/installation.md): run the platform with Docker Compose or on Kubernetes.
- [Credentials](docs/credentials.md): the API keys of registered models and tools are kept in a secrets manager
  (HashiCorp Vault or AWS Secrets Manager), and registrations refer to them by name. This explains how to set it
  up, including locally.

## SDKs

Applications use an SDK to talk to the platform:

- [Python](https://github.com/pockerman/gogi-python), with an example per service
- Java and TypeScript (planned)

The services' APIs are defined in Protocol Buffers in [gogi-protos](https://github.com/pockerman/gogi-protos).
