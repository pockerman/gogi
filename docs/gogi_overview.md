# gogi[AI] overview

gogi is the infrastructure layer underneath applications that use LLMs. An application built on gogi doesn't talk
to model providers, vector stores or secret managers itself. It asks the platform: run this model, remember this
conversation, search these documents, call this tool, check this input. The platform answers through one gRPC API,
the same for every application.

This keeps the hard, shared problems in one place, solved once:

- **Model calls:** retries, fallbacks and cost.
- **Conversations:** long ones that no longer fit a context window.
- **Credentials:** they must never reach application code.
- **Safety:** policies that span every application.
- **Visibility:** knowing what every request did and what it cost.

## How it fits together

```
 application ──► SDK ──gRPC──► gateway ──┬──► model service ──────► model providers
                                         ├──► prompt service
                                         ├──► session service
                                         ├──► data services ───────► vector store, ingestion workers
                                         ├──► tool service ────────► tools, MCP servers
                                         ├──► guardrails service
                                         ├──► workflow service
                                         └──► observability service ◄── telemetry from every service
```

- **Applications use an SDK.** The SDK turns method calls into gRPC requests to the gateway. Applications never
  address services directly.
- **The gateway is the single entry point.** It exposes the API of every service and forwards each call to the
  service that owns it.
- **Services own one concern each.** Each keeps its own state, mostly in Postgres, and calls other services
  through their gRPC APIs like any other client would.
- **Every service reports telemetry** to the observability service, without application code doing anything.

## A request, end to end

Take an assistant that answers questions about a user's documents. One turn of the conversation touches most of
the platform:

1. The application loads the system prompt it registered with the **prompt service**.
2. It asks the **session service** for the conversation so far, assembled to fit a token budget.
3. It searches the user's documents through the **data services**.
4. It checks the user's message with the **guardrails service**.
5. It asks the **model service** to run a model with all of this. If the model calls a tool, the application
   executes the call through the **tool service**, which injects the tool's credential, and runs the model again
   with the result.
6. It filters the answer with the guardrails service, and stores the turn in the session service.

Throughout, every service records spans for its part of the request under one trace id, so the
**observability service** can show the whole request, its latency and its cost.

With the Python SDK, the model call of step 5 looks like this:

```python
from gogi.gogi import Gogi
from gogi.models import LLMMessage, LLMRunRequest, LLMRunRequestConfig

platform = Gogi(gateway_url="localhost:50051")

response = platform.llm_clients.run(
    LLMRunRequest(
        config=LLMRunRequestConfig(provider="anthropic", model="claude-sonnet-5-5", max_tokens=1000),
        messages=[
            LLMMessage(role="system", content=system_prompt),
            *history,
            LLMMessage(role="user", content=question),
        ],
    )
)
print(response.provider, response.model, response.content)
```

## The services

| Service        | Port   | Owns                                                              | Documentation |
|----------------|--------|-------------------------------------------------------------------|---------------|
| gateway        | 50051  | The single entry point; forwards calls to the services            |               |
| workflows      | 50053  | Workflows, their deployments and routes, and long-running jobs    |               |
| documents      | 50054  | Documents: ingestion jobs and search                              |               |
| indexes        | 50055  | Indexes, the collections documents belong to                      |               |
| llms           | 50057  | Running models, model discovery, registered models               | [model service](model_service.md) |
| prompts        | 50058  | Versioned prompts and their metadata                              | [prompt service](prompt_service.md) |
| llm-sessions   | 50059  | Conversations, memories, context assembly                         | [session service](session_service.md) |
| llm-tools      | 50060  | The tool registry and tool execution                              | [tool service](tool_service.md) |
| guardrails     | 50061  | Input, output and action policies, and their audit                | [guardrails service](guardrails_service.md) |
| observability  | 50062  | Traces, scores, logs, metrics, cost and budgets                   | [observability service](observability_service.md) |

### Gateway

Applications reach every service through the gateway. It serves the gRPC API of each service and forwards a call
to the service that owns it, so applications need one address and the services can move without them noticing. A
request that arrives without a trace gets one at the gateway.

The gateway also serves the standard gRPC health protocol, `grpc.health.v1.Health`, for the whole platform:

- `Check` with an empty service reports the gateway itself.
- `Check` with a service's name, e.g. `llms`, asks that service's own health server.
- `List` reports the gateway and every service at once.

A service that doesn't answer within two seconds is reported as `NOT_SERVING`. An unknown name fails with
`NOT_FOUND`. Every service runs a health server of its own as well, for container and Kubernetes probes. With the
Python SDK, `platform.health.status()` returns the status of every service, and `platform.health.check("llms")`
the status of one.

### Model service

The model service runs models, with a single API whatever the provider. The built-in providers are OpenAI and
Anthropic. Self-hosted models with an OpenAI-compatible API, e.g. vLLM, TGI or Ollama, can be registered and are
then used exactly like the built-in ones.

- **Running models:** a call can run synchronously or stream its tokens, and can give the model tools. The model's
  tool calls come back in one format whatever the provider; the model service doesn't run them.
- **Resilience:** failed calls are retried, and can fall back to other models.
- **Registered models:** a registered model that needs an API key refers to a credential in the platform's
  credential store, never to the key itself.
- **Accounting:** every call is recorded as a generation, with its tokens and its cost from the platform's price
  table.

The model service doesn't store or resolve prompts: that is the prompt service's job. An application gets its
prompt from the prompt service and sends it to the model service as a message. See [model service](model_service.md).

### Prompt service

The prompt service keeps prompts as versioned, managed artifacts rather than strings in application code. A prompt
is registered under a name and a version, with its content and metadata. The metadata records who wrote it, which
model and parameters it was written for, and the test set and metrics it was evaluated with. Applications fetch a
prompt when they need it, so a prompt can change without redeploying them. See [prompt service](prompt_service.md).

### Session service

The session service keeps conversations: sessions, their messages with tool calls and results, and memories, the
facts saved about a user for every session or for one. Before a model call it assembles a long conversation into a
token budget, by keeping the most recent messages, summarizing the older ones, or putting the user's memories
first. See [session service](session_service.md).

### Data services

The indexes and documents services hold the knowledge an application retrieves from.

- **Indexes:** an index is a named collection of documents. Its creator is its first owner, and it can be shared
  with other users. Each member is an owner, who may change the index's documents and members, or a reader, who
  may read and search them. An index always keeps at least one owner. The platform records the members; the
  application checks a caller's role before it acts on their behalf.
- **Ingestion:** documents are ingested asynchronously. An ingestion job, run by workers on Temporal, splits a
  document into chunks, embeds them, and stores them in the vector store, while the application follows the job's
  progress.
- **Search:** the documents service searches an index for the chunks relevant to a query.

### Tool service

The tool service manages tools as platform entities: a registry with namespaces and versions, discovery by
capability, and the import of MCP servers' tools. Applications never hold a tool's credential. The tool service
executes the call, injecting the credential from the credential store, within the tool's rate and execution
limits, and stops calling a tool that keeps failing. See [tool service](tool_service.md).

### Guardrails service

The guardrails service checks content and actions against named policies:

- **input policies** before a message reaches a model;
- **output policies** that redact or block a response before it reaches a user;
- **tool validation, behavioural and human approval policies** before an action runs.

Policies are added through the API or loaded from YAML files, can run in shadow mode, and every evaluation is
recorded for audit. See [guardrails service](guardrails_service.md).

### Observability service

The observability service collects the platform's telemetry: traces of every request across the services, the
generations of model calls with their tokens and cost, quality scores, structured logs and metrics. Services report
it automatically, in the background, so it never slows a request down. On top of it the service reports cost,
watches budgets and reports the health of each service. See [observability service](observability_service.md).

### Workflow service

The workflow service registers an application's workflows, deploys and rolls them back, and maps routes to them. It
also tracks long-running jobs: their progress, checkpoints, and completion or failure.

## Conventions across the platform

- **Contracts:** every service's API is defined in Protocol Buffers in the
  [gogi-protos](https://github.com/pockerman/gogi-protos) repository, shared by the services and the SDKs.
  Timestamps are unix milliseconds.
- **Messages:** conversations use one message format everywhere, the OpenAI chat format: a role (`system`, `user`,
  `assistant` or `tool`), content, the tools an assistant message calls, and the call a tool message answers. Most
  providers and self-hosted servers speak it or a close variant, so the model service translates to each provider
  at the edge, and nothing else in the platform has to know which provider is behind a model.
- **Traces:** the trace context travels in gRPC metadata (`x-trace-id`, `x-span-id`, `x-session-id`,
  `x-workflow-id`, `x-user-id`), so every service continues the trace of the request it serves.
- **Credentials:** API keys and tool credentials live in a credential store (HashiCorp Vault or AWS Secrets
  Manager). Registrations refer to them by name. See [credentials](credentials.md).
- **Errors:** services answer with standard gRPC codes. `INVALID_ARGUMENT`, `NOT_FOUND` and `PERMISSION_DENIED` are
  the caller's to fix. `UNAVAILABLE` and `RESOURCE_EXHAUSTED` may go away if the call is retried later.

## Infrastructure

| Component                | Used for                                                       |
|--------------------------|----------------------------------------------------------------|
| PostgreSQL               | The state of the services; migrations in `migrations/`         |
| ChromaDB                 | The vector store of the data services                          |
| MinIO                    | The content of prompts                                         |
| Temporal                 | Durable workflows, e.g. document ingestion                     |
| HashiCorp Vault or AWS Secrets Manager | The credential store; LocalStack stands in for AWS locally |

See [installation](installation.md) for running the platform with Docker Compose or on Kubernetes.

## SDKs

Applications use an SDK rather than gRPC directly. The SDK holds one connection to the gateway, creates each
service's client the first time it is used (`platform.llm_clients`, `platform.prompts`, `platform.llm_session`,
`platform.tools`, `platform.guardrails`, `platform.observability`, ...), and converts between Protocol Buffers and
plain objects. The [Python SDK](https://github.com/pockerman/gogi-python) comes with an example per service.
