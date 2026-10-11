# Session service

The session service (`llm-sessions`) keeps the conversations of users with AI applications, so that an application
can carry a conversation over many turns, devices and visits (chapter 4 of the book). It stores:

- **Sessions**: one conversation of a user.
- **Messages**: what was said in a session, in the OpenAI format, tool calls and tool results included.
- **Memories**: facts saved about a user, by a model or by the application, for every session or for one.

It also assembles the context of the next model call from a long conversation, within a token budget.

## Sessions

`GetOrCreateSession` takes a `user_id` and, optionally, a `session_id`:

- Without a `session_id`, it creates a session with a new id.
- With a `session_id` of the user, it returns that session.
- With a `session_id` that doesn't exist, it creates the session with that id, so a client can choose its own ids,
  e.g. one per chat window. Ids are up to 128 letters, digits, `.`, `_`, `:` or `-`.
- With the `session_id` of another user's session, it fails with `PERMISSION_DENIED`.

The response's `created` says whether the call created the session. Two requests that create the same session at
the same moment both get it, and only one of them reports `created`.

`ListSessions` returns a user's sessions, most recently active first, with `limit` and `offset` for paging and
`total_count`. Each session has its `message_count`, and its `updated_at` is the last time messages were added.

`DeleteSession` deletes a session with its messages and cached summaries. With `delete_memories`, it also deletes
the memories saved for that session. The user's own memories are kept either way.

## Messages

`AddMessages` appends messages to a session, all of them or none. One user turn often produces several messages,
e.g. the question, an assistant message calling a tool, the tool's result and the answer, and they belong
together. Messages are checked first:

| Role        | Needs                                                      |
|-------------|------------------------------------------------------------|
| `system`, `user` | `content`                                             |
| `assistant` | `content`, or `tool_calls` with an `id` and a function name each |
| `tool`      | the `tool_call_id` of the call it answers                  |

Only assistant messages call tools. A message without a `timestamp` gets the time it is stored, in unix millis.
The service orders messages by when they were stored, not by their timestamps. At most 1000 messages can be added
in one call.

`GetMessages` without a strategy returns a page of the history, oldest first: `offset` messages are skipped and
at most `limit` returned. `total_count` is the number of messages in the session.

## Context strategies

A long conversation doesn't fit a model's context window, and the conversation's share of the window is smaller
still once the system prompt, tools and retrieved documents take theirs. `GetMessages` with a `strategy`
assembles the context of the next model call within `token_budget` estimated tokens. The default is
`GOGI_SESSIONS_DEFAULT_TOKEN_BUDGET`.

| Strategy       | Context                                                                         |
|----------------|---------------------------------------------------------------------------------|
| `truncate`     | The most recent messages that fit                                               |
| `summarize`    | A summary of the older messages, then the most recent messages                  |
| `hierarchical` | The user's memories, then the summary, then the most recent messages            |

The messages returned are ready to send to a model: the summary and the memories come first, as system messages.
Every strategy:

- **keeps the system prompt**, i.e. the system messages before the conversation starts, whatever the budget;
- **never starts with tool results** whose call it would leave out, since providers reject them;
- **always keeps the latest message**, even if it alone exceeds the budget, and says so in `notes`;
- **reports** `omitted_count` (stored messages left out, or covered by the summary), `estimated_tokens`, and the
  `strategy` it applied.

With `truncate`, `limit` caps how many recent messages are kept.

### Summaries

`summarize` and `hierarchical` use a summary model, set with `GOGI_SESSIONS_SUMMARY_PROVIDER` and
`GOGI_SESSIONS_SUMMARY_MODEL`, which the session service calls through the model service. A small, fast model
does the job.

The service sets aside a share of the budget for the summary (`GOGI_SESSIONS_SUMMARY_SHARE`), but only when the
conversation doesn't fit without one. The summary covers every message before the recent ones, and
`summarized_count` says how many.

Summaries are cached and rolled forward. When more messages age out of the recent window, only those are
summarized, onto the previous summary, so a long conversation isn't summarized from the start on every request.
A long history is summarized in chunks of at most `GOGI_SESSIONS_SUMMARY_CHUNK_TOKENS` tokens.

`summary_focus` says what the summary must keep, e.g. `dates, prices and what was agreed`. Each focus has its own
cached summary.

Without a summary model, `summarize` truncates instead, and `hierarchical` keeps the memories but leaves the
older messages out. If the summary model fails, the service does the same for that request. In both cases
`strategy` and `notes` say so.

### Memories in the context

`hierarchical` puts the user's memories first: those of every session and those of this session. They get up to
`GOGI_SESSIONS_MEMORY_SHARE` of the budget, the most recently updated first, and `notes` says if some don't fit.
Whatever the memories and the summary leave goes to the recent messages.

### Token estimates

Tokens are estimated from the length of the messages: about `GOGI_SESSIONS_CHARS_PER_TOKEN` characters per token,
plus a few tokens per message. Every model tokenizes differently, so leave some headroom in the budget.

## Memories

A memory is a fact saved about a user under a key, with a JSON value, e.g. `{"currency": "EUR"}`, `"aisle seat"`
or `3`. Values that aren't valid JSON are rejected.

- `SaveMemory` saves a memory for the user, in every session, or with `session_id` for one of the user's sessions.
  A key can be saved in both scopes at once. Saving an existing key replaces its value.
- `GetMemory` returns the memories of every scope, or of one session with `session_id`, optionally for one `key`.
  `entries` has every memory with its scope and times. `memories` maps each key to its value; if a key is saved in
  several scopes, it maps to the most recently updated value, or the session's on a tie.
- `DeleteMemory` deletes a key in every scope, or in one session with `session_id`, and reports how many memories
  it deleted.
- `ClearUserMemory` deletes every memory of a user, e.g. when the user asks to be forgotten.

Two sessions of a user can update the same memory at once, and by default the last save wins. To avoid
overwriting a change, pass the `updated_at` you read as `expected_updated_at`. The save then fails with `ABORTED`
if the memory changed since, and `expected_updated_at` 0 saves only if the memory doesn't exist yet. `updated_at`
increases with every save.

Every save and deletion is logged, without the value, so it can be traced why the system knows a fact.

## Retrieval across sessions

Searching a user's earlier sessions for context, e.g. when the user refers to something said weeks ago, needs the
data service to index messages. It isn't part of the session service yet.

## Configuration

| Variable                              | Default       | Meaning                                                  |
|---------------------------------------|---------------|----------------------------------------------------------|
| `GOGI_LLMS_SESSION_PORT`              | `:50059`      | Port of the gRPC server                                  |
| `GOGI_SESSIONS_DEFAULT_TOKEN_BUDGET`  | `8000`        | Budget of a strategy when the request gives none         |
| `GOGI_SESSIONS_CHARS_PER_TOKEN`       | `4`           | Characters per estimated token                           |
| `GOGI_SESSIONS_MEMORY_SHARE`          | `0.1`         | Share of the budget memories may take                    |
| `GOGI_SESSIONS_SUMMARY_SHARE`         | `0.2`         | Share of the budget the summary may take                 |
| `GOGI_SESSIONS_SUMMARY_PROVIDER`      | (none)        | Provider of the summary model, e.g. `openai`             |
| `GOGI_SESSIONS_SUMMARY_MODEL`         | (none)        | Summary model, e.g. `gpt-4o-mini`; none means no summaries |
| `GOGI_SESSIONS_SUMMARY_CHUNK_TOKENS`  | `6000`        | Most tokens of messages summarized in one call           |
| `GOGI_SESSIONS_SUMMARY_TIMEOUT`       | `60s`         | How long a summary may take                              |
| `GOGI_LLM_SERVICE_ADDR`               | `llms:50057`  | The model service                                        |
| `DATABASE_URL`                        | `postgres://gogi:gogi@postgres:5432/gogi` | The database                 |

## Errors

| Code                | When                                                            |
|---------------------|-----------------------------------------------------------------|
| `INVALID_ARGUMENT`  | A missing or malformed field, an invalid message, an unknown strategy, a value that isn't JSON |
| `NOT_FOUND`         | The session doesn't exist                                       |
| `PERMISSION_DENIED` | The session belongs to another user                             |
| `ABORTED`           | The memory changed since `expected_updated_at`                  |
