CREATE TABLE gogi_llm_sessions (
    id UUID PRIMARY KEY,
    user_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_gogi_llm_sessions_user_id ON gogi_llm_sessions(user_id);

CREATE TABLE gogi_llm_messages (
    id UUID PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES gogi_llm_sessions(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT,
    name TEXT,
    timestamp BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_gogi_llm_messages_session_id ON gogi_llm_messages(session_id);

CREATE TABLE gogi_user_memory (
    id UUID PRIMARY KEY,
    user_id TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, key)
);
CREATE INDEX idx_gogi_user_memory_user_id ON gogi_user_memory(user_id);
