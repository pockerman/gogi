# Tool service

In this section we will go over the tool service (`llm-tools`). Tools are the capabilities a model can call,
such as booking an appointment or creating an issue. Instead of every application defining its tools inline and
holding their API keys, gogi manages tools as platform entities: a tool is registered once, with a name, versions,
an owner and operational metadata, and any application can then discover it and call it through the platform.
The platform validates the arguments, injects the credentials, enforces rate and execution limits, and stops
calling tools that keep failing.

The service follows the design of chapter 6 of *Designing AI Systems*.

## Contract

The service implements `ToolServer` of `gogi/v1/llm_tool_service.proto`:

| RPC                 | What it does                                                                            |
|---------------------|-----------------------------------------------------------------------------------------|
| `RegisterTool`      | Adds a version of a tool to the registry                                                |
| `DiscoverTools`     | Finds tools by namespace, capabilities, tags, read-only behavior and version constraint |
| `ValidateTool`      | Checks arguments against a tool's parameters without calling it                         |
| `ExecuteTool`       | Calls a tool and returns its result                                                     |
| `ExecuteToolAsync`  | Starts a call in the background and returns a task id                                   |
| `GetTask`           | Reports the status and result of a background call                                     |
| `RegisterMcpServer` | Imports the tools of an MCP server into the registry                                    |

The gateway routes these calls to the service, at `GOGI_LLM_TOOLS_SERVICE_ADDR` (default `llm-tools:50060`).

## The tool definition

A `ToolServiceDefinition` has three parts:

| Part       | Fields                                                    | Used by                                         |
|------------|-----------------------------------------------------------|-------------------------------------------------|
| Identity   | `name`, `version`, `owner`                                | The registry: discovery, versions, ownership    |
| Schema     | `description`, `parameters_json`, `returns_json`          | Models, when they decide whether and how to call the tool |
| Operations | `endpoint`, `mcp_server_url`, `credential_ref`, `behavior`, `rate_limits`, `execution_limits`, `cost`, `capabilities`, `tags`, `required_permissions` | The platform, when it calls the tool |

Registration rules:

- `name` is fully qualified: dot separated segments of letters, digits, `_` and `-`, e.g.
  `healthcare.scheduling.book_appointment`. The leading segments are the tool's namespace.
- `version` is a semantic version, `1.0.0` by default. `owner` defaults to `gogi`.
- `description` is required: models read it to decide when to call the tool.
- `parameters_json` is the JSON Schema of an object, by default one without properties. Schemas are compiled when
  the tool is registered; references outside the schema (files, URLs) are not followed.
- A tool is called at `endpoint`, an http(s) URL, or through the MCP server at `mcp_server_url`; exactly one is set.
- `credential_ref` names a credential of the credential store (see [credentials](credentials.md)), which must
  exist when the tool is registered.

### Versions

Registered versions are immutable: registering a version that exists fails with `ALREADY_EXISTS`, and changing a
tool means registering a new version. Within a major version a version must not break the callers of the version
before it, so it cannot:

- remove a parameter, or a property of the result (`returns_json`)
- add a required parameter

Such a registration fails with `FAILED_PRECONDITION` and names the version to register instead, e.g. `2.0.0`. All
versions stay in the registry, so applications pinned to a version keep it while others move on.

## Discovery

`DiscoverTools` returns, for each tool, one version: the latest one, or the latest one that satisfies
`version_constraint` (e.g. `^1.0.0`, `~1.4`, `>=1.0.0 <3.0.0`). The filters:

- `namespace`: `healthcare.scheduling` or `healthcare.scheduling.*` select the tools named
  `healthcare.scheduling.<...>`; empty or `*` selects all
- `capabilities` and `tags`: a tool matches if it has at least one of the requested capabilities and at least one
  of the requested tags (case-insensitive). `relevance_scores` gives each tool the fraction of the requested
  capabilities and tags it has, and the tools are sorted by it
- `read_only`: only tools whose behavior is read-only

Discovered definitions do not include the tool's `endpoint` or `mcp_server_url`: applications call tools through
the platform. A tool whose circuit is open (see below) is left out until it recovers, so that models do not keep
choosing a broken tool.

## Execution

`ExecuteTool` resolves the tool (`version`, or the latest version) and then:

1. Validates the arguments against `parameters_json`
2. Requires `confirmed` for a tool whose behavior has `requires_confirmation`: a person approves the call
3. Enforces the tool's rate limits: calls per minute and per day across all callers, and calls per session
   (`session_id`, which a tool limited per session requires)
4. Retrieves the tool's credential from the credential store
5. Checks the tool's circuit breaker
6. Calls the tool, within its timeout and response size limit, retrying failed calls if the tool is idempotent

An HTTP tool is called with `POST <endpoint>`, the arguments as the JSON body, and the headers
`Authorization: Bearer <credential>` (if the tool has a credential), `X-Gogi-Tool`, `X-Gogi-Tool-Version` and
`X-Gogi-Session-Id`. A `2xx` response is the result: its body as is if it is JSON, as a JSON string otherwise.

An MCP tool is called with `tools/call` on its server, over the Streamable HTTP transport with the credential as a
bearer token. The result is the tool's structured content, or its text content.

Failures of the call are not gRPC errors: the response has `success=false` and an `error` the model can reason
about, e.g. invalid arguments, a rate limit, a timeout, or the tool's own error. The credential is removed from
results and errors, and errors do not name the tool's endpoint. gRPC errors are for requests that cannot be served:
an unknown tool or version (`NOT_FOUND`), or no tool name (`INVALID_ARGUMENT`).

`ExecuteToolAsync` does the same in the background and records the call in `gogi_tool_tasks`; `GetTask` reports its
status: `pending`, `running`, `succeeded`, `failed` or `timed_out`.

### Limits and retries

| Setting                  | From the tool                         | Platform default and ceiling                                  |
|--------------------------|---------------------------------------|---------------------------------------------------------------|
| Timeout of a call        | `execution_limits.timeout_seconds`    | `GOGI_TOOLS_DEFAULT_TIMEOUT` (`30s`), at most `GOGI_TOOLS_MAX_TIMEOUT` (`5m`) |
| Response size            | `execution_limits.max_response_size_kb` | `GOGI_TOOLS_DEFAULT_MAX_RESPONSE_KB` (`1024`), at most `GOGI_TOOLS_MAX_RESPONSE_KB` (`10240`) |
| Retries of a failed call | `execution_limits.max_retries`        | At most `GOGI_TOOLS_MAX_RETRIES` (`3`)                        |

The timeout covers the whole call, retries included. Only idempotent tools (`behavior.is_idempotent`) are retried,
since retrying another tool could repeat its side effects, and only after failures that may pass: the tool could
not be reached, timed out, or answered `429` or `5xx`. A response larger than the limit fails the call.
`memory_limit_mb` and `cpu_limit_millicores` are stored but not enforced, since tools run outside the service.

### Circuit breaker

The service tracks the consecutive failures of each tool version. After `GOGI_TOOLS_CIRCUIT_FAILURE_THRESHOLD`
(`5`) failures its circuit opens: calls fail at once and the tool is hidden from discovery. After
`GOGI_TOOLS_CIRCUIT_RECOVERY_TIMEOUT` (`60s`) up to `GOGI_TOOLS_CIRCUIT_HALF_OPEN_MAX` (`2`) trial calls go through;
a success closes the circuit, a failure opens it again. Failures that say nothing about the tool's health, e.g. it
rejected the arguments with a `4xx`, do not count.

## MCP servers

`RegisterMcpServer` connects the platform, as an MCP client, to the MCP server at `server_url` and imports its tools
as `<namespace>.<tool name>`, owned by the namespace and tagged `mcp`. The server's credential is `credential_ref`;
the tools' behavior comes from their MCP annotations (`readOnlyHint`, `idempotentHint`), extended by
`policy_overrides`, and their rate limits from `rate_limit_overrides`. Tools whose names cannot be platform names
are skipped.

A tool is imported as version `1.0.0`. Importing the server again changes nothing for tools whose definition is the
same; a tool whose definition changed on the server is registered as a new major version, so that applications
pinned to the definition they approved do not get a different tool silently.

## Storage

The registry is the `gogi_tools` table, one row per tool version, unique on `(name, version)`; background calls are
in `gogi_tool_tasks`. Migration `000009` extends the tables of migration `000006` to the definition above.

## Limitations

- Rate limits and circuits are kept in memory, per replica of the service.
- A background call runs in the replica that started it: if the replica stops, the task stays `running`.
- Applications are not authenticated yet, so discovery does not filter tools by the caller's permitted namespaces,
  and `required_permissions` is stored but not enforced.
- The model service does not yet send tool definitions to the model providers, so tools discovered for a model
  (e.g. with the Python SDK's `build_model_tools`) cannot yet be passed to a model through the platform.
