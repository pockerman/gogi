-- A tool is a platform entity identified by its namespaced name and semantic version.
-- Versions are immutable: changing a tool means registering a new version
ALTER TABLE gogi_tools RENAME COLUMN schema_json TO parameters_json;

ALTER TABLE gogi_tools
    ADD COLUMN returns_json TEXT NOT NULL DEFAULT '',
    ADD COLUMN behavior JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN rate_limits JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN cost JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN execution_limits JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN required_permissions TEXT[] NOT NULL DEFAULT '{}',
    -- the name of the credential in the credential store, never the secret itself
    ADD COLUMN credential_ref TEXT NOT NULL DEFAULT '',
    -- set for tools imported from an MCP server, which are called through it
    ADD COLUMN mcp_server_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN mcp_tool_name TEXT NOT NULL DEFAULT '';

UPDATE gogi_tools SET behavior = jsonb_build_object('is_read_only', is_read_only, 'is_idempotent', is_idempotent);
ALTER TABLE gogi_tools DROP COLUMN is_read_only, DROP COLUMN is_idempotent;

UPDATE gogi_tools SET description = '' WHERE description IS NULL;
UPDATE gogi_tools SET endpoint = '' WHERE endpoint IS NULL;
UPDATE gogi_tools SET parameters_json = '' WHERE parameters_json IS NULL;
UPDATE gogi_tools SET capabilities = '{}' WHERE capabilities IS NULL;
UPDATE gogi_tools SET tags = '{}' WHERE tags IS NULL;
ALTER TABLE gogi_tools
    ALTER COLUMN description SET NOT NULL, ALTER COLUMN description SET DEFAULT '',
    ALTER COLUMN endpoint SET NOT NULL, ALTER COLUMN endpoint SET DEFAULT '',
    ALTER COLUMN parameters_json SET NOT NULL, ALTER COLUMN parameters_json SET DEFAULT '',
    ALTER COLUMN capabilities SET NOT NULL, ALTER COLUMN capabilities SET DEFAULT '{}',
    ALTER COLUMN tags SET NOT NULL, ALTER COLUMN tags SET DEFAULT '{}';

-- the fully qualified name identifies a tool across owners
ALTER TABLE gogi_tools DROP CONSTRAINT gogi_tools_name_version_owner_key;
ALTER TABLE gogi_tools ADD CONSTRAINT gogi_tools_name_version_key UNIQUE (name, version);

ALTER TABLE gogi_tool_tasks
    ADD COLUMN tool_version TEXT NOT NULL DEFAULT '',
    ADD COLUMN session_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN error TEXT NOT NULL DEFAULT '';
