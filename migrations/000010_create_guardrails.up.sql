-- named guardrail policies; the rules' shape depends on the policy's type
CREATE TABLE gogi_guardrail_policies (
    name TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    -- empty means the policy applies to every application
    application_id TEXT NOT NULL DEFAULT '',
    -- enforce, or shadow: evaluated and recorded, not enforced
    mode TEXT NOT NULL DEFAULT 'enforce',
    rules JSONB NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- the author's label for the version, e.g. 1.2.0
    version TEXT NOT NULL DEFAULT '',
    -- incremented every time the policy changes
    revision INTEGER NOT NULL DEFAULT 1,
    -- api, or file for policies loaded from the service's policy directory
    source TEXT NOT NULL DEFAULT 'api',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX gogi_guardrail_policies_application_idx ON gogi_guardrail_policies (application_id, type);

-- every guardrail evaluation, whatever its outcome. The content is not stored, only its hash
CREATE TABLE gogi_guardrail_evaluations (
    id UUID PRIMARY KEY,
    -- input, output or action
    inspection_point TEXT NOT NULL,
    application_id TEXT NOT NULL DEFAULT '',
    policy_names TEXT[] NOT NULL DEFAULT '{}',
    action TEXT NOT NULL DEFAULT '',
    -- allowed, blocked, modified or approval_required
    outcome TEXT NOT NULL,
    triggered_rules JSONB NOT NULL DEFAULT '[]',
    content_sha256 TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    turn_id TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL DEFAULT '',
    context JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- the actions allowed in a session, for per-session limits and conflicting actions
CREATE INDEX gogi_guardrail_evaluations_session_idx ON gogi_guardrail_evaluations (session_id, created_at)
    WHERE session_id <> '' AND inspection_point = 'action';
CREATE INDEX gogi_guardrail_evaluations_created_idx ON gogi_guardrail_evaluations (created_at);

-- violations reported by applications
CREATE TABLE gogi_guardrail_violations (
    id UUID PRIMARY KEY,
    policy_name TEXT NOT NULL,
    action TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT 'low',
    context JSONB NOT NULL DEFAULT '{}',
    details TEXT NOT NULL DEFAULT '',
    -- the evaluation that found the violation, if any; not a foreign key, as callers report it
    evaluation_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX gogi_guardrail_violations_created_idx ON gogi_guardrail_violations (created_at DESC);
CREATE INDEX gogi_guardrail_violations_policy_idx ON gogi_guardrail_violations (policy_name, created_at DESC);
