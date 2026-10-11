CREATE TABLE gogi_workflows (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    api_path TEXT NOT NULL,
    container_image TEXT NOT NULL,
    response_mode TEXT NOT NULL DEFAULT 'sync',
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE gogi_workflow_deployments (
    id UUID PRIMARY KEY,
    workflow_id UUID NOT NULL REFERENCES gogi_workflows(id) ON DELETE CASCADE,
    version INT NOT NULL,
    status TEXT NOT NULL DEFAULT 'deploying',
    desired_replicas INT NOT NULL DEFAULT 1,
    current_replicas INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE gogi_routes (
    id UUID PRIMARY KEY,
    workflow_id UUID NOT NULL REFERENCES gogi_workflows(id) ON DELETE CASCADE,
    path TEXT NOT NULL UNIQUE,
    method TEXT NOT NULL DEFAULT 'POST',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE gogi_workflow_jobs (
    id UUID PRIMARY KEY,
    workflow_id UUID REFERENCES gogi_workflows(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    progress INT NOT NULL DEFAULT 0,
    checkpoint_json TEXT,
    result_json TEXT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
