# Prompt service

The prompt service (`prompts`) keeps the prompts of applications as managed, versioned artifacts. A prompt that
lives in the platform, rather than as a string in application code, can be reviewed, versioned and changed without
redeploying the applications that use it. It also carries a record of what it was written and tested for.

Prompts are the prompt service's concern only. The model service doesn't store or resolve them: an application
fetches its prompt here, and sends it to the model service as a message, usually the system message.

## A prompt

| Field          | Meaning                                                                  |
|----------------|--------------------------------------------------------------------------|
| `prompt_name`  | The prompt's name, e.g. `support-triage`                                 |
| `prompt_version` | A version label, e.g. `v1.2.0`                                         |
| `gogi_index`   | The index the prompt belongs with, if it is written for one              |
| `content`      | The prompt itself                                                        |
| `metadata`     | Who wrote it, and what it was written and tested for (below)             |

The metadata records:

- `author`;
- `model`, the model the prompt was written for;
- `parameters`, the generation parameters it was tuned with: `temperature`, `max_tokens`, `stop_sequences`,
  `frequency_penalty` and `presence_penalty`;
- `test_info`, the evaluation behind the version: the `test_set_id` and `test_set_path` of the test set, and the
  `metrics` it scored, e.g. `{"accuracy": 0.91}`.

The metadata describes the prompt; it isn't applied by the platform. An application that wants the recorded
parameters reads them along with the content and passes them to the model service.

## Operations

| RPC              | What it does                                                          |
|------------------|-----------------------------------------------------------------------|
| `RegisterPrompt` | Stores a prompt and returns its `prompt_id`                           |
| `GetPrompt`      | Returns a prompt, with its content and metadata, by `prompt_id`       |
| `DeletePrompt`   | Deletes a prompt and its content, by `prompt_id`                      |

A prompt's content is stored in MinIO, in the `prompts` bucket under `<prompt_name>/<prompt_version>`, and its
metadata in Postgres (`gogi_prompts`). An unknown `prompt_id` fails with `NOT_FOUND`.

## Using a prompt

With the Python SDK:

```python
from gogi.models import LLMMessage, LLMRunRequest, LLMRunRequestConfig, PromptGetRequest

prompt = platform.prompts.get_prompt(PromptGetRequest(prompt_id=prompt_id))
parameters = prompt.metadata.parameters

response = platform.llm_clients.run(
    LLMRunRequest(
        config=LLMRunRequestConfig(
            provider="openai",
            model=prompt.metadata.model,
            temperature=parameters.temperature,
            max_tokens=parameters.max_tokens,
        ),
        messages=[
            LLMMessage(role="system", content=prompt.content.decode()),
            LLMMessage(role="user", content=question),
        ],
    )
)
```

Example 4 of the Python SDK registers, gets and deletes a prompt.
