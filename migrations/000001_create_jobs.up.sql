CREATE TABLE jobs (
    id UUID PRIMARY KEY,
    document_id UUID,

    status VARCHAR(32) NOT NULL,

    worker_id VARCHAR(255),

    error_message TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);