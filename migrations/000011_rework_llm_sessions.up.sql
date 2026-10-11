-- The session tables of 000004 were never written to: the session service was a stub. They
-- are replaced by tables that keep tool calls, order messages as they are stored, scope
-- memories to a user or a session, and cache summaries.
DROP TABLE gogi_user_memory;
DROP TABLE gogi_llm_messages;
DROP TABLE gogi_llm_sessions;

CREATE TABLE gogi_llm_sessions (
    -- chosen by the client, or a UUID
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    message_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- the last time messages were added
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX gogi_llm_sessions_user_idx ON gogi_llm_sessions (user_id, updated_at DESC);

CREATE TABLE gogi_llm_messages (
    -- orders the messages of a session as they were stored
    seq BIGSERIAL PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES gogi_llm_sessions (id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT,
    -- the tools an assistant message calls: [{"id", "type", "name", "arguments"}]
    tool_calls JSONB NOT NULL DEFAULT '[]',
    tool_call_id TEXT,
    name TEXT,
    -- when the message was produced, in unix millis
    timestamp BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX gogi_llm_messages_session_idx ON gogi_llm_messages (session_id, seq);

-- summaries of the oldest messages of a session, rolled forward as more messages age out
CREATE TABLE gogi_llm_session_summaries (
    session_id TEXT NOT NULL REFERENCES gogi_llm_sessions (id) ON DELETE CASCADE,
    -- identifies what the summary was asked to keep
    focus_hash TEXT NOT NULL,
    -- the summary covers the messages up to and including this seq
    up_to_seq BIGINT NOT NULL,
    message_count INTEGER NOT NULL,
    summary TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (session_id, focus_hash)
);

-- facts saved about a user, for every session (session_id '') or for one
CREATE TABLE gogi_llm_memories (
    user_id TEXT NOT NULL,
    -- not a foreign key: user-level memories have none, and session memories may outlive their session
    session_id TEXT NOT NULL DEFAULT '',
    key TEXT NOT NULL,
    value JSONB NOT NULL,
    -- unix millis; updated_at strictly increases with every save, for optimistic concurrency
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (user_id, session_id, key)
);
