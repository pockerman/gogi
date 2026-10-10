# gogi[AI] overview

## SDKs

An SDK connects to the platform servieces.
This is done via an API Gateway. Here is an example
of how to use the Python SDK

```
```

The SDK connects to platform services through a single, stable API Gateway, and it makes those remote calls feel like local Python method calls.

When you create platform = GenAIPlatform(), it mainly configures how to reach the gateway (from an explicit gateway_url, or the GENAI_GATEWAY_URL env var, with a sensible default) . It does not immediately open network connections; service clients are lazy-initialized only when you first access them (for example, platform.sessions or platform.models) .

On first access, the SDK constructs the appropriate service client (such as SessionClient or ModelClient). That client opens a secure gRPC channel to the API Gateway and attaches routing metadata like x-target-service: sessions or x-target-service: models . Each method call then builds a Protocol Buffers request, sends it over gRPC through the gateway along with that metadata, and converts the Protocol Buffers response back into normal Python objects . The gateway reads the x-target-service metadata and routes the request to the correct backend service without needing to deserialize the binary payload .

This is the same pattern described in chapter 3 for the Model Service: ModelClient translates Python method calls into Protocol Buffer messages over gRPC to the gateway, and the gateway routes them to the Model Service .

## Services exposed

### Model service

The model service provides the following utilities

- Model discovery
- Custom model registration
- Prompt registration

A registered model that needs an API key references a credential held by the platform's
credential store, rather than containing the secret; see [credentials](credentials.md).

A request can give the model tools to call. The model's tool calls are returned in the OpenAI
format whatever the provider, and the application runs them with the tool service; see
[tools for a model](tool_service.md#tools-for-a-model).


---
**Remark: OpenAI message format as platform standard**


The adapters translate between the platform's internal format and each provider's format. But what format does the platform use? We need a canonical representation for messages that flows through the Model Service.

We use OpenAI's format. This isn't because OpenAI is special or because we're favoring them over other providers. It's because their format has become a de facto industry standard. When Anthropic documents their API, they explain differences from OpenAI's format. When vLLM serves open-source models, it provides an OpenAI-compatible endpoint. When developers discuss LLM integration, they typically assume OpenAI's conventions.


### Tool service

The tool service manages tools as platform entities: a registry with namespaces and versions, discovery by
capability, and execution with injected credentials, rate and execution limits and circuit breakers. It also
imports the tools of MCP servers. See [tool service](tool_service.md).

### Session service

The Session Service provides conversation memory: the ability to remember what's been said so that follow-up questions make sense and the assistant can reference earlier parts of the conversation. A session represents a conversation between a user and an AI application. At minimum, it needs to track who the conversation belongs to, when it started, and what's been said.





