# Observability service

The observability service collects the telemetry of the platform and of the applications built on it, and makes
it queryable (chapter 7 of the book). It answers three questions about an AI application: what it costs, whether
it works well, and where a request spent its time.

| Telemetry        | What it is                                                                     |
|------------------|--------------------------------------------------------------------------------|
| **Traces**       | One request's path through the platform: the spans every service recorded for it, assembled by trace id |
| **Generations**  | The spans of LLM calls, with model, provider, tokens, cost, fallback and time to first token |
| **Scores**       | Quality signals about a trace, span or generation, from code, a model judge, a reviewer or the end user |
| **Logs**         | Structured events, linked to the span that emitted them                        |
| **Metrics**      | Counters and histograms over time, with labels                                 |

On top of these it reports cost, watches budgets and reports the health of the platform's services. Sessions
aren't a separate entity: traces carry the session id of the session service.

## How the platform reports telemetry

Reporting is automatic, and it never slows a request down or fails it.

- **Every RPC gets a span.** The gateway and the services (`llms`, `llm-sessions`, `llm-tools`, `guardrails`,
  `prompts`, `workflows`) install gRPC interceptors that record a span per RPC, named `service.Method`, e.g.
  `llms.Run`. Health checks are left out. A call that fails because of the caller, e.g. `INVALID_ARGUMENT` or
  `NOT_FOUND`, leaves the span ok, since the service worked, and records the code in `rpc.code`.
- **Traces cross services.** The trace context travels in gRPC metadata: `x-trace-id`, `x-span-id` (the
  caller's span), and `x-session-id`, `x-workflow-id`, `x-user-id`. A request arriving without a trace gets one at
  the first service it reaches, usually the gateway. Every call a service makes continues the trace, so the spans
  nest into one tree. Malformed ids are ignored and start a new trace.
- **Model calls are generations.** The model service records a generation for every `Run` and `RunStream`, nested
  under the RPC's span. It carries the provider and model that answered, the model asked for, the tokens, the cost
  from the price table, whether a fallback answered, and the time to the first token of a stream. A fallback also
  produces a `model_fallback` warning log event.
- **The model service publishes metrics**, labelled with provider, model and workflow:

  | Metric                               | Type      |
  |--------------------------------------|-----------|
  | `gogi.models.requests_total`         | counter   |
  | `gogi.models.errors_total`           | counter   |
  | `gogi.models.fallbacks_total`        | counter   |
  | `gogi.models.prompt_tokens_total`    | counter   |
  | `gogi.models.completion_tokens_total`| counter   |
  | `gogi.models.cost_usd_total`         | counter   |
  | `gogi.models.request_duration_ms`    | histogram |

- **Telemetry is batched.** Each service holds a client (`gogi/observability`) that buffers spans, generations,
  logs and metric points in memory. It sends them in batches of 100, or every 5 seconds. If the observability
  service can't be reached, the client keeps up to 10,000 items of each kind and retries. Beyond that it drops the
  oldest, and logs how many.

Telemetry is off in a service without `GOGI_OBSERVABILITY_ADDR`; Docker Compose and the Kubernetes config map set
it. The observability service doesn't trace itself.

Metric names follow `gogi.{service}.{measurement}`. Names ending in `_total` are counters; the others are
histograms.

## Traces

`RecordSpan` and `RecordGeneration` take batches of up to 1000. Each item is checked on its own, and the response
lists the ones `rejected`, with why, so one bad item doesn't lose the batch. A span needs a `trace_id`, a
`span_id`, an `operation`, and a `start_time` no later than its `end_time` (unix millis). A span or generation sent
twice, e.g. by a client retrying, is stored once.

There is no `RecordTrace`: the service assembles a trace from its spans as they arrive.

- The trace spans the earliest start to the latest end of its spans.
- Its cost and tokens are the sums of its generations'.
- It takes its `input` and `output` from the root span, the one without a parent.
- It takes its `session_id`, `workflow_id`, `user_id` and `tags` from any span that has them.

Generations that lack a session, workflow or user take the trace's. This holds even when the root span arrives
after them, which is usual, since the root span ends last.

- `GetTrace` returns a trace with its spans, generations and scores.
- `QueryTraces` finds traces by time, session, workflow, user, a service they passed through, tag, minimum duration,
  minimum cost, failed spans, or a score's value, e.g. `helpfulness` at most 0.5. It returns them newest first, or
  by `duration` or `cost`, 50 to a page by default, with `next_cursor` for the next page.
- `QueryGenerations` finds LLM calls by model, provider, workflow, user, session, minimum cost or fallback, e.g.
  every `claude-sonnet-5-5` call that cost more than $0.05 this week.

Spans hold at most 64 KB of `input` and `output`; longer text is truncated. Inputs and outputs are stored as
sent, so pseudonymize user ids, and leave out content that mustn't be kept, before reporting.

## Scores

`RecordScore` attaches quality signals to a trace (`trace_id`), or one of its spans (`span_id`) or generations
(`generation_id`, the generation's span id).

- **Value:** `numeric_value` (0.0 to 1.0 for continuous dimensions), `categorical_value` or `boolean_value`.
- **Source** (required): `AUTOMATED` (a heuristic in code), `MODEL_JUDGE`, `HUMAN` or `USER_FEEDBACK`, so automated
  scores can be calibrated against human ones.

Scores can arrive at any time after their trace. `QueryScores` filters by trace, name, source, workflow and time.
Its `summaries` cover every matching score, not only the page. For each name and source they give the count, the
mean of numeric values, the share of true boolean values, and the count of each category.

## Logs

`IngestLogs` takes structured events.

- **Fields:** a `service`, a `severity`, an `event_type` (e.g. `model_fallback`), a `message`, and string and numeric
  attributes.
- **Correlation:** the `trace_id` and `span_id` link an event to the span that emitted it.
- **Defaults:** events without an id get one, events without a timestamp get the time they arrive, and events
  without a severity are `INFO`.

`QueryLogs` filters by trace, span, service, event type, minimum severity, workflow, user, attribute values and
time, and returns events newest first, with a cursor. Querying by trace id gives every event of every service for
one request.

## Metrics

`RecordMetrics` takes metric points: a name, a value, labels and a timestamp. A point without a type is a counter
if its name ends in `_total`, and a histogram otherwise.

`QueryMetrics` aggregates a metric over a time range (`start_time` is required).

- **Filters and series:** `label_filters` keep the points with those label values. `group_by` gives a series per
  combination of the labels named.
- **Buckets:** `step_ms` gives a sample per time bucket of that width, at least 1 second and at most 10,000 buckets
  in the range. Without a step there is one sample for the whole range.
- **Aggregation:** `sum`, `count`, `avg`, `min`, `max`, `p50`, `p90`, `p95` or `p99`; the default is `sum` for
  counters and `avg` otherwise. Use percentiles rather than averages for latency, whose distribution is heavy-tailed.

## Cost

Every generation has a cost:

- the reporter's `cost_usd`, if set;
- otherwise the price table's, from its tokens, marked `cost_estimated`;
- otherwise nothing: a model without a price costs nothing, and cost reports count such generations as
  `unpriced_generations`.

The model service prices its own calls with the same table.

The default price table covers the built-in providers' models, in USD per million input and output tokens. The
Claude prices are Anthropic's first-party list prices. Check every default against your own contract, and set the
prices of other models, e.g. self-hosted ones, in a YAML file named by `GOGI_MODEL_PRICES_FILE`, read by the model
service and the observability service:

```yaml
anthropic:
  claude-sonnet-5-5: {input_per_mtok: 2, output_per_mtok: 10}
my-vllm:
  llama3.2: {input_per_mtok: 0, output_per_mtok: 0}
```

A versioned model name a provider reports, e.g. `gpt-4o-mini-2024-07-18`, gets the price of the longest model name
it starts with.

`GetCostReport` sums the cost and tokens of generations over a time range (`start_time` is required).

- **Grouping:** `group_by` takes any of `model`, `provider`, `workflow_id`, `user_id`, `session_id` and `service`,
  or the name of any attribute of the generations' spans, e.g. `team`.
- **Filtering:** `filters` takes the same dimensions, with the values to keep.
- **Granularity:** `hourly`, `daily`, `weekly` or `monthly` gives a bucket per period, in UTC; without one there is
  a bucket per group for the whole range.

Buckets are ordered by period, then most expensive first. This lets you drill down: total by team, then one team's
workflows, then one workflow's models.

## Budgets

`SetBudgetAlert` sets a spending limit (`limit_usd`) over a `daily`, `weekly` (Monday to Sunday) or `monthly` period
in UTC, for the generations that match its `filters`, e.g. `{"workflow_id": "support-bot"}`.

- **Thresholds:** shares of the limit, default 0.7, 0.9 and 1.0.
- **Status:** `GetBudgetStatus` returns the spend so far, the share of the limit, the thresholds crossed, the
  `state` (`ok`, `warning` or `exceeded`), and the spend projected to the end of the period at the rate so far.
  The projection extrapolates from at least an hour of spend, so it stays sane at the start of a period.
- **Webhook:** with a `webhook_url`, the service checks its budgets every `GOGI_OBSERVABILITY_BUDGET_CHECK_INTERVAL`.
  The first time spend crosses a threshold in a period, it POSTs a JSON notification. The payload's `text` field
  reads as a Slack message, and the other fields carry the numbers. A notification that fails is sent again at the
  next check.

Budgets inform; they don't block requests. To cap spending, use rate limits. Webhook URLs are called from the
observability service's network, so only let trusted people set budgets.

## Service health

`GetServiceHealth` reports the health of every service that recorded spans in a window, 15 minutes by default, or
of one service. A service is:

- `unhealthy` when at least `GOGI_OBSERVABILITY_UNHEALTHY_ERROR_RATE` of its spans failed;
- `degraded` from `GOGI_OBSERVABILITY_DEGRADED_ERROR_RATE`;
- `healthy` otherwise;
- `unknown` when it recorded no span in the window, with when it was last seen.

Each report also gives the span count and the p95 duration. This complements, rather than replaces, each service's
own liveness endpoint.

## Retention

Telemetry is deleted hourly once older than its retention period: log events after
`GOGI_OBSERVABILITY_LOG_RETENTION`, metric points after `GOGI_OBSERVABILITY_METRIC_RETENTION`, and traces with
their spans, generations and scores after `GOGI_OBSERVABILITY_TRACE_RETENTION`.

## Configuration

Of the observability service:

| Variable                                    | Default  | Meaning                                              |
|---------------------------------------------|----------|------------------------------------------------------|
| `GOGI_OBSERVABILITY_PORT`                   | `:50062` | Port of the gRPC server                              |
| `GOGI_OBSERVABILITY_DEGRADED_ERROR_RATE`    | `0.05`   | Share of failed spans from which a service is degraded |
| `GOGI_OBSERVABILITY_UNHEALTHY_ERROR_RATE`   | `0.25`   | Share of failed spans from which a service is unhealthy |
| `GOGI_OBSERVABILITY_LOG_RETENTION`          | `720h`   | How long log events are kept                         |
| `GOGI_OBSERVABILITY_METRIC_RETENTION`       | `2160h`  | How long metric points are kept                      |
| `GOGI_OBSERVABILITY_TRACE_RETENTION`        | `2160h`  | How long traces are kept                             |
| `GOGI_OBSERVABILITY_BUDGET_CHECK_INTERVAL`  | `5m`     | How often budgets are checked for notifications      |
| `GOGI_MODEL_PRICES_FILE`                    | (none)   | YAML price file; also read by the model service      |
| `DATABASE_URL`                              | `postgres://gogi:gogi@postgres:5432/gogi` | The database        |

Of every other service:

| Variable                          | Meaning                                                              |
|-----------------------------------|----------------------------------------------------------------------|
| `GOGI_OBSERVABILITY_ADDR`         | Where to send telemetry, e.g. `observability:50062`; unset turns it off |
| `GOGI_OBSERVABILITY_SERVICE_ADDR` | The gateway's route to the observability service                     |
