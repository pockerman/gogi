# Model service

## Retries and fallback

Calls to a provider fail: providers rate limit, get overloaded and have outages. The model
service retries failed calls and, if a model keeps failing, can fall back to other models
(section 3.4 of the book). This happens in the service, not in the SDKs, so every caller gets
the same behaviour.

### Retries

Only errors that may go away are retried:

| Error                                                  | Retried |
|--------------------------------------------------------|---------|
| 429 rate limited, 408 timeout, 409 conflict            | yes     |
| 5xx, including Anthropic's 529 overloaded              | yes     |
| network errors, no response                            | yes     |
| 400, 401, 403, 404 and other 4xx                       | no      |
| an invalid request, e.g. a malformed tool definition   | no      |

The delay before the first retry doubles on every retry, up to a maximum, with jitter so that
callers that failed together do not retry together. If the provider says how long to wait
(``retry-after-ms`` or ``Retry-After``) the service waits that long; if that is longer than
the maximum delay it does not wait, and falls back instead.

The provider SDKs' own retries are turned off, so a call is retried by the service only.

### Fallback

If the model still fails after its retries, the service tries the models to fall back to,
in order, each with its own retries, until one answers. The response's ``provider`` and
``model`` say which model answered; in a stream, every chunk names its ``provider``.

- A request can name its models to fall back to in ``config.fallback.models``. They replace the
  platform's default models.
- If it names none, the platform's default models are used: ``GOGI_LLM_FALLBACK_MODELS``. These
  are empty unless the operator sets them, so by default a failed call is retried but does not
  fall back: answering with another model changes quality, cost, tool calling behaviour and
  where the data goes, so it is opted into.
- ``config.fallback.enabled = false`` turns fallback off, including to the default models.

A request may name at most 5 models to fall back to. A model to fall back to that the platform
does not know is skipped; the requested model must be known. An invalid request is not fallen
back, as it fails with every model.

A stream is retried, or falls back, only until its first chunk is sent. From then on the caller
has part of the response, and an error ends the stream.

If every model fails, the error lists each model's error, and its gRPC code is the one of the
last model's error: ``UNAVAILABLE`` for errors that may go away, ``RESOURCE_EXHAUSTED`` for rate
limits.

### Configuration

| Variable                        | Default | Meaning                                                    |
|---------------------------------|---------|------------------------------------------------------------|
| ``GOGI_LLM_MAX_RETRIES``        | 2       | retries of a failed call to a model                        |
| ``GOGI_LLM_RETRY_INITIAL_DELAY``| 500ms   | delay before the first retry (Go duration)                 |
| ``GOGI_LLM_RETRY_MAX_DELAY``    | 20s     | longest delay between two retries (Go duration)            |
| ``GOGI_LLM_FALLBACK_MODELS``    | empty   | default models to fall back to, e.g. ``openai/gpt-4o,anthropic/claude-haiku-4-5`` |

A request can lower these in ``config.fallback.retry`` (``max_retries``, ``initial_delay_ms``,
``max_delay_ms``), but not raise them.

### Example

```json
{
  "messages": [{"role": "user", "content": "Hello"}],
  "config": {
    "provider": "anthropic",
    "model": "claude-sonnet-5-5",
    "fallback": {
      "models": [{"provider": "openai", "model": "gpt-4o"}],
      "retry": {"max_retries": 1}
    }
  }
}
```

If Claude Sonnet is overloaded, the call is retried once, then sent to GPT-4o; the response
then has ``"provider": "openai"``.
