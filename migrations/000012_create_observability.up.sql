-- the spans of traces; generations are spans too, with their LLM fields in obs_generations
CREATE TABLE obs_spans (
    trace_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT NOT NULL DEFAULT '',
    -- span or generation
    kind TEXT NOT NULL DEFAULT 'span',
    service TEXT NOT NULL DEFAULT '',
    operation TEXT NOT NULL DEFAULT '',
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    duration_ms DOUBLE PRECISION NOT NULL,
    -- ok or error
    status TEXT NOT NULL DEFAULT 'ok',
    error_message TEXT NOT NULL DEFAULT '',
    attributes JSONB NOT NULL DEFAULT '{}',
    numeric_attributes JSONB NOT NULL DEFAULT '{}',
    events JSONB NOT NULL DEFAULT '[]',
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (trace_id, span_id)
);
CREATE INDEX obs_spans_service_idx ON obs_spans (service, start_time DESC);
CREATE INDEX obs_spans_start_idx ON obs_spans (start_time DESC);

CREATE TABLE obs_generations (
    trace_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    service TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    requested_model TEXT NOT NULL DEFAULT '',
    prompt_tokens BIGINT NOT NULL DEFAULT 0,
    completion_tokens BIGINT NOT NULL DEFAULT 0,
    cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    -- true if the platform priced the generation; priced is false when it had no price
    cost_estimated BOOLEAN NOT NULL DEFAULT FALSE,
    priced BOOLEAN NOT NULL DEFAULT TRUE,
    cache_hit BOOLEAN NOT NULL DEFAULT FALSE,
    fallback_used BOOLEAN NOT NULL DEFAULT FALSE,
    time_to_first_token_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
    session_id TEXT NOT NULL DEFAULT '',
    workflow_id TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL DEFAULT '',
    -- the span's attributes, so cost can be grouped by any of them, e.g. team
    attributes JSONB NOT NULL DEFAULT '{}',
    start_time TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (trace_id, span_id)
);
CREATE INDEX obs_generations_start_idx ON obs_generations (start_time DESC);
CREATE INDEX obs_generations_model_idx ON obs_generations (model, start_time DESC);
CREATE INDEX obs_generations_workflow_idx ON obs_generations (workflow_id, start_time DESC);

-- traces, assembled from their spans as they arrive
CREATE TABLE obs_traces (
    trace_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL DEFAULT '',
    workflow_id TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL DEFAULT '',
    input TEXT NOT NULL DEFAULT '',
    output TEXT NOT NULL DEFAULT '',
    tags TEXT[] NOT NULL DEFAULT '{}',
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    duration_ms DOUBLE PRECISION GENERATED ALWAYS AS (EXTRACT(EPOCH FROM (end_time - start_time)) * 1000) STORED,
    total_cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_tokens BIGINT NOT NULL DEFAULT 0,
    span_count INTEGER NOT NULL DEFAULT 0,
    error_count INTEGER NOT NULL DEFAULT 0,
    services TEXT[] NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX obs_traces_start_idx ON obs_traces (start_time DESC, trace_id);
CREATE INDEX obs_traces_session_idx ON obs_traces (session_id, start_time DESC) WHERE session_id <> '';
CREATE INDEX obs_traces_workflow_idx ON obs_traces (workflow_id, start_time DESC) WHERE workflow_id <> '';

CREATE TABLE obs_scores (
    score_id TEXT PRIMARY KEY,
    trace_id TEXT NOT NULL,
    span_id TEXT NOT NULL DEFAULT '',
    generation_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    numeric_value DOUBLE PRECISION,
    categorical_value TEXT,
    boolean_value BOOLEAN,
    source TEXT NOT NULL,
    comment TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}',
    timestamp TIMESTAMPTZ NOT NULL
);
CREATE INDEX obs_scores_trace_idx ON obs_scores (trace_id);
CREATE INDEX obs_scores_name_idx ON obs_scores (name, timestamp DESC);

CREATE TABLE obs_logs (
    event_id TEXT PRIMARY KEY,
    trace_id TEXT NOT NULL DEFAULT '',
    span_id TEXT NOT NULL DEFAULT '',
    timestamp TIMESTAMPTZ NOT NULL,
    service TEXT NOT NULL DEFAULT '',
    severity SMALLINT NOT NULL DEFAULT 0,
    event_type TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    attributes JSONB NOT NULL DEFAULT '{}',
    numeric_attributes JSONB NOT NULL DEFAULT '{}',
    workflow_id TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX obs_logs_time_idx ON obs_logs (timestamp DESC, event_id);
CREATE INDEX obs_logs_trace_idx ON obs_logs (trace_id) WHERE trace_id <> '';
CREATE INDEX obs_logs_service_idx ON obs_logs (service, timestamp DESC);
CREATE INDEX obs_logs_attributes_idx ON obs_logs USING GIN (attributes jsonb_path_ops);

CREATE TABLE obs_metric_points (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    -- counter or histogram
    type TEXT NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    labels JSONB NOT NULL DEFAULT '{}',
    service TEXT NOT NULL DEFAULT '',
    timestamp TIMESTAMPTZ NOT NULL
);
CREATE INDEX obs_metric_points_name_idx ON obs_metric_points (name, timestamp);
CREATE INDEX obs_metric_points_labels_idx ON obs_metric_points USING GIN (labels jsonb_path_ops);

CREATE TABLE obs_budget_alerts (
    name TEXT PRIMARY KEY,
    filters JSONB NOT NULL DEFAULT '{}',
    -- daily, weekly or monthly
    period TEXT NOT NULL,
    limit_usd DOUBLE PRECISION NOT NULL,
    thresholds DOUBLE PRECISION[] NOT NULL,
    webhook_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- the thresholds already notified in a budget's period, so each is notified once
CREATE TABLE obs_budget_notifications (
    name TEXT NOT NULL REFERENCES obs_budget_alerts (name) ON DELETE CASCADE,
    period_start TIMESTAMPTZ NOT NULL,
    threshold DOUBLE PRECISION NOT NULL,
    notified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (name, period_start, threshold)
);
