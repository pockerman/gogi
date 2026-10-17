-- the documents of each index, so they can be listed, filtered by their metadata and deleted.
-- A document's status is its ingestion job's.
CREATE TABLE gogi_documents (
    index_id UUID NOT NULL REFERENCES gogi_indexes (id) ON DELETE CASCADE,
    -- chosen by the client, or a UUID
    id TEXT NOT NULL,
    filename TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}',
    job_id UUID REFERENCES jobs (id) ON DELETE SET NULL,
    chunk_count INTEGER NOT NULL DEFAULT 0,
    page_count INTEGER NOT NULL DEFAULT 0,
    word_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (index_id, id)
);
CREATE INDEX gogi_documents_metadata_idx ON gogi_documents USING GIN (metadata);

-- a document id is chosen by the client, so it need not be a UUID
ALTER TABLE jobs ALTER COLUMN document_id TYPE TEXT;
