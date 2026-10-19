-- the applications that use gogi, e.g. qubit-notes and xact-marine. Every call to the gateway
-- carries one of its application's keys; gogi keeps only their SHA-256 hashes.
CREATE TABLE gogi_applications (
    -- chosen by the operator: lowercase letters, digits and dashes
    name TEXT PRIMARY KEY,
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE gogi_application_keys (
    id UUID PRIMARY KEY,
    application TEXT NOT NULL REFERENCES gogi_applications (name) ON DELETE CASCADE,
    hash TEXT NOT NULL UNIQUE,
    -- the first characters of the key, so the operator can tell keys apart
    prefix TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ
);
CREATE INDEX gogi_application_keys_application_idx ON gogi_application_keys (application);

-- prompts are found by id, so each remembers its application
ALTER TABLE gogi_prompts ADD COLUMN application TEXT NOT NULL DEFAULT 'default';

-- an application's registered model is kept under its qualified name; its server knows it by the
-- name it was registered with. Empty for the models registered before: their server knows them by name
ALTER TABLE gogi_registered_llms ADD COLUMN served_name TEXT NOT NULL DEFAULT '';

-- the credentials each application has stored; their values are in the credential store
CREATE TABLE gogi_credentials (
    application TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (application, name)
);
