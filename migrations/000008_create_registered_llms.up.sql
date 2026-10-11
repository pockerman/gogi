CREATE TABLE gogi_registered_llms (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    provider TEXT NOT NULL,
    context_window INTEGER NOT NULL DEFAULT 0,
    supports_vision BOOLEAN NOT NULL DEFAULT FALSE,
    supports_tools BOOLEAN NOT NULL DEFAULT FALSE,
    supports_streaming BOOLEAN NOT NULL DEFAULT FALSE,
    supports_json_mode BOOLEAN NOT NULL DEFAULT FALSE,
    endpoint TEXT NOT NULL,
    health_check TEXT NOT NULL DEFAULT '',
    adapter_type TEXT NOT NULL,
    -- the name of the credential in the credential store, never the secret itself
    credential_ref TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'provisioning',
    last_checked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
