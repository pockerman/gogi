ALTER TABLE gogi_tool_tasks DROP COLUMN tool_version, DROP COLUMN session_id, DROP COLUMN error;

ALTER TABLE gogi_tools DROP CONSTRAINT gogi_tools_name_version_key;
ALTER TABLE gogi_tools ADD CONSTRAINT gogi_tools_name_version_owner_key UNIQUE (name, version, owner);

ALTER TABLE gogi_tools
    ALTER COLUMN description DROP NOT NULL, ALTER COLUMN description DROP DEFAULT,
    ALTER COLUMN endpoint DROP NOT NULL, ALTER COLUMN endpoint DROP DEFAULT,
    ALTER COLUMN parameters_json DROP NOT NULL, ALTER COLUMN parameters_json DROP DEFAULT,
    ALTER COLUMN capabilities DROP NOT NULL, ALTER COLUMN capabilities DROP DEFAULT,
    ALTER COLUMN tags DROP NOT NULL, ALTER COLUMN tags DROP DEFAULT;

ALTER TABLE gogi_tools
    ADD COLUMN is_read_only BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN is_idempotent BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE gogi_tools SET
    is_read_only = COALESCE((behavior->>'is_read_only')::BOOLEAN, FALSE),
    is_idempotent = COALESCE((behavior->>'is_idempotent')::BOOLEAN, FALSE);

ALTER TABLE gogi_tools
    DROP COLUMN returns_json, DROP COLUMN behavior, DROP COLUMN rate_limits, DROP COLUMN cost,
    DROP COLUMN execution_limits, DROP COLUMN required_permissions, DROP COLUMN credential_ref,
    DROP COLUMN mcp_server_url, DROP COLUMN mcp_tool_name;

ALTER TABLE gogi_tools RENAME COLUMN parameters_json TO schema_json;
