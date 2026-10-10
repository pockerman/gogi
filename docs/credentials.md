# Credentials

In this section we will go over how ``gogi`` handles the credentials it needs to call
LLMs and tools. There are three kinds of credentials:

- **Provider API keys**: the keys of the providers ``gogi`` supports out of the box,
  Anthropic and OpenAI. They are configuration of the ``llms`` service.
- **Credentials of registered models**: the API keys of self-hosted models that are
  registered with the platform (``RegisterLLM``), e.g. a vLLM, TGI or Ollama server behind
  an authenticating gateway. They live in a secrets manager and registrations only
  reference them by name.
- **Credentials of tools**: the API keys of the tools registered with the tool service
  (``RegisterTool``), and of the MCP servers it imports tools from (``RegisterMcpServer``).
  They are handled like the credentials of registered models, by the same credential
  store; see [Credentials of tools](#credentials-of-tools).

## Provider API keys

The ``llms`` service reads the keys of the built-in providers from its environment:

| Variable            | Provider                                                        |
|---------------------|-----------------------------------------------------------------|
| ``ANTHROPIC_API_KEY`` | Anthropic                                                     |
| ``OPENAI_API_KEY``    | OpenAI                                                        |
| ``OPENAI_BASE_URL``   | Optional: an OpenAI-compatible endpoint, including the API version, e.g. ``https://api.openai.com/v1`` |

- **Docker Compose**: the ``llms`` service in ``docker-compose.yml`` ships with placeholder
  keys. Replace them before making real calls.
- **Kubernetes**: the keys are in the ``gogi-secrets`` secret, ``k8/secrets.yaml``. Replace the
  ``REPLACE_ME`` placeholders before applying it.

Never commit real keys to the repository.

## Credentials of registered models

### Design

A registered model names its credential with ``credential_ref``, e.g. ``ml-inference-prod``;
the secret itself never appears in the registration. This follows the platform's design:
secrets are kept by a secrets manager, and the platform only holds references to them. As a
result:

- the secret never appears in registrations, the ``gogi_registered_llms`` table, logs or
  responses (``ListRegisteredLLMs`` returns the reference, not the secret)
- the team that owns a model server can hand its key to whoever manages the secrets without
  sharing it with the platform users who register and call the model
- a secret is rotated in the secrets manager, without re-registering the model

The ``llms`` service reads secrets through a credential store, the ``CredentialStore``
interface of the ``gogi/credentials`` package:

```
type CredentialStore interface {
	Store(ctx context.Context, name, value string) error
	Retrieve(ctx context.Context, name string) (Credential, error)
	Rotate(ctx context.Context, name, newValue string) error
}
```

The store is a thin layer over an existing secrets manager; ``gogi`` does not implement its
own secret storage. The following implementations are available:

| Implementation         | Secrets manager                     |
|------------------------|-------------------------------------|
| ``AWSSecretsManagerStore`` | AWS Secrets Manager             |
| ``VaultStore``             | HashiCorp Vault, KV v2 secrets engine |
| ``MemoryCredentialStore``  | In memory, for tests               |

The configured store is wrapped by ``CachingCredentialStore``, which caches credentials for a
short time, so that every request to a model does not call the secrets manager.

### How a credential is used

1. **Registration.** ``RegisterLLM`` checks that the credential store holds the referenced
   credential, and rejects the registration otherwise. It stores the reference with the
   registration.
2. **Requests.** For every request to the model, the ``llms`` service retrieves the credential
   from the store (or its cache) and sends it to the model's endpoint as the API key, in the
   ``Authorization: Bearer <credential>`` header. A model without a ``credential_ref`` is
   called without an ``Authorization`` header.
3. **Rotation.** When the secret is changed in the secrets manager, requests use the new
   secret once the cached one expires, after at most ``GOGI_CREDENTIAL_CACHE_TTL``.

### Configuration

The ``llms`` service selects the credential store with ``GOGI_CREDENTIAL_STORE``:

| Value              | Store                   | Settings                                                              |
|--------------------|-------------------------|-----------------------------------------------------------------------|
| ``none`` (default) | No credential store     | Registrations with a ``credential_ref`` are rejected                  |
| ``aws``            | AWS Secrets Manager     | The standard AWS variables: ``AWS_REGION``, the AWS credentials (or an IAM role), and ``AWS_ENDPOINT_URL`` for LocalStack |
| ``vault``          | HashiCorp Vault (KV v2) | ``VAULT_ADDR``, ``VAULT_TOKEN``, ``GOGI_VAULT_KV_MOUNT`` (default ``secret``) |

Settings common to all stores:

| Variable                      | Default               | Meaning                                         |
|-------------------------------|-----------------------|-------------------------------------------------|
| ``GOGI_CREDENTIAL_PREFIX``    | ``gogi/credentials/`` | Prefix of the secret names                      |
| ``GOGI_CREDENTIAL_CACHE_TTL`` | ``1m``                | How long credentials are cached (a Go duration, e.g. ``30s``, ``5m``) |

If no store is configured, the service logs so on start up. An invalid setting, e.g. an
unknown ``GOGI_CREDENTIAL_STORE``, stops the service.

### Where a credential is stored

A credential named ``<name>`` is the secret ``<prefix><name>``, by default
``gogi/credentials/<name>``:

- **AWS Secrets Manager**: the secret named ``gogi/credentials/<name>``, with the credential
  as its secret string.
- **HashiCorp Vault**: the secret at path ``gogi/credentials/<name>`` of the KV v2 engine
  mounted at ``GOGI_VAULT_KV_MOUNT``, with the credential under the ``value`` key.

Credentials are added to the secrets manager by whoever manages the secrets, with the secrets
manager's own tools; the platform API offers no way to create or read them.

## Credentials of tools

The tool service (``llm-tools``) uses a credential store too, configured with the same variables as the
``llms`` service (``GOGI_CREDENTIAL_STORE`` and the settings above); Docker Compose and the Kubernetes
manifests give both services the same store. A tool references its credential with ``credential_ref``,
e.g. ``scheduling-api-prod``:

- ``RegisterTool`` and ``RegisterMcpServer`` check that the store holds it
- for every call to the tool, the service retrieves it and sends it as ``Authorization: Bearer <credential>``
- the credential is removed from the results and errors returned to the caller

A tool's credential is stored like any other, e.g. in the Vault dev server (see
[Local development](#local-development)):

```
docker exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN=gogi-dev-root-token gogi-vault \
  vault kv put secret/gogi/credentials/scheduling-api-prod value=<the-api-key>
```

See [tool service](tool_service.md) for how tools are called.

## Local development

There is no cloud secrets manager on a development machine, so ``gogi`` runs a local stand-in:

- **HashiCorp Vault dev server** (default): Docker Compose (service ``vault``, container
  ``gogi-vault``) and the Kubernetes manifests (``k8/vault/``) run it, and the ``llms`` service
  uses it with ``GOGI_CREDENTIAL_STORE=vault``.
- **LocalStack**, which emulates AWS Secrets Manager: Docker Compose runs it with the ``aws``
  profile, to test the AWS store.

> **Warning:** the Vault dev server keeps secrets in memory, so they are lost when it restarts,
> and it uses the fixed root token ``gogi-dev-root-token``. Never use it outside development.

### Vault (Docker Compose)

Start the platform as usual (see [installation](installation.md)), then store a credential:

```
docker exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN=gogi-dev-root-token gogi-vault \
  vault kv put secret/gogi/credentials/ml-inference-prod value=<the-api-key>
```

Read it back with ``vault kv get secret/gogi/credentials/ml-inference-prod``, and rotate it by
running ``vault kv put`` again with the new value. The Vault UI is also available at
``http://localhost:8200`` (sign in with the token).

### Vault (Kubernetes)

``k8/vault/`` deploys the dev server; its root token is the ``VAULT_TOKEN`` key of
``k8/secrets.yaml``, and ``k8/configmap.yaml`` points the ``llms`` service at it. Store a
credential with:

```
kubectl exec -n gogi deploy/vault -- env VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=gogi-dev-root-token \
  vault kv put secret/gogi/credentials/ml-inference-prod value=<the-api-key>
```

Since the dev server keeps secrets in memory, store the credentials again after the vault pod
restarts.

### AWS Secrets Manager with LocalStack (Docker Compose)

Start the platform with the ``aws`` profile and the AWS store:

```
GOGI_CREDENTIAL_STORE=aws docker compose --profile aws up -d
```

The ``llms`` service then uses LocalStack, at ``AWS_ENDPOINT_URL=http://localstack:4566``, with
the dummy credentials LocalStack accepts. Store a credential with:

```
docker exec gogi-localstack awslocal secretsmanager create-secret \
  --name gogi/credentials/ml-inference-prod --secret-string <the-api-key>
```

and rotate it with:

```
docker exec gogi-localstack awslocal secretsmanager put-secret-value \
  --secret-id gogi/credentials/ml-inference-prod --secret-string <the-new-api-key>
```

## Registering a model with a credential

With the credential stored, register the model with its name in ``credential_ref``. With the
Python SDK:

```
registration = LLMRegisterRequest(
    info=LLMModelInfo(
        name="llama3.2",
        provider="ollama",
        capabilities=LLMCapabilities(context_window=128_000, supports_streaming=True),
    ),
    endpoint="https://inference.example.com/v1",
    health_check="/health",
    adapter_type="openai",
    credential_ref="ml-inference-prod",
)
platform.llm_clients.register_llm(registration)
```

Leave ``credential_ref`` empty for a model whose endpoint needs no API key, e.g. a local Ollama
server; see ``examples/intro/example_3`` in the Python SDK.

## Production

In production, point the ``llms`` service at the cloud's secrets manager instead of the dev
servers:

- **AWS**: set ``GOGI_CREDENTIAL_STORE=aws`` and ``AWS_REGION``, and do not set
  ``AWS_ENDPOINT_URL``. Prefer an IAM role (e.g. IRSA on EKS) to access keys. The role only
  needs ``secretsmanager:GetSecretValue`` on ``arn:aws:secretsmanager:<region>:<account>:secret:gogi/credentials/*``.
- **HashiCorp Vault**: set ``GOGI_CREDENTIAL_STORE=vault`` with the address of a production
  Vault, and give the service a token whose policy only allows reading
  ``<mount>/data/gogi/credentials/*``, rather than a root token.

Azure Key Vault and Google Secret Manager are not supported yet; they can be added as new
``CredentialStore`` implementations in ``gogi/credentials`` and selected in ``config.go``.

## Troubleshooting

| Error | Cause |
|-------|-------|
| ``RegisterLLM``: ``FAILED_PRECONDITION`` *credential "…" is referenced but no credential store is configured* | ``GOGI_CREDENTIAL_STORE`` is unset or ``none`` |
| ``RegisterLLM``: ``INVALID_ARGUMENT`` *credential "…" not found in the credential store* | The secret does not exist; check its name and the prefix, e.g. ``gogi/credentials/ml-inference-prod`` |
| ``RegisterLLM``: ``UNAVAILABLE`` *failed to check credential "…"* | The service cannot reach the secrets manager, or is not allowed to read the secret: check ``VAULT_ADDR``/``VAULT_TOKEN`` or the AWS settings and permissions |
| Requests to the model fail with ``401``/``403`` from its endpoint | The secret is wrong or stale; after a rotation, wait for ``GOGI_CREDENTIAL_CACHE_TTL`` |
| Registrations fail after the Vault dev server restarted | The dev server lost its secrets; store them again |

## Tests

The credential stores share one contract test, ``gogi/credentials/credential_store_test.go``.
The AWS and Vault stores run it against real services only when these variables are set:

| Variable                                         | Example                                     |
|--------------------------------------------------|---------------------------------------------|
| ``GOGI_TEST_AWS_ENDPOINT_URL``                   | ``http://localhost:4566`` (LocalStack)      |
| ``GOGI_TEST_VAULT_ADDR``, ``GOGI_TEST_VAULT_TOKEN`` | ``http://localhost:8200``, ``gogi-dev-root-token`` |
