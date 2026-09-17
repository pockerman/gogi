CREATE TABLE gogi_tools (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '1.0.0',
    owner TEXT NOT NULL DEFAULT 'gogi',
    description TEXT,
    is_read_only BOOLEAN NOT NULL DEFAULT FALSE,
    is_idempotent BOOLEAN NOT NULL DEFAULT FALSE,
    capabilities TEXT[],
    tags TEXT[],
    endpoint TEXT,
    schema_json TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(name, version, owner)
);

CREATE TABLE gogi_tool_tasks (
    id UUID PRIMARY KEY,
    tool_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    input_json TEXT,
    result_json TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
