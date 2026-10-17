-- a document's uploaded file is kept in the object store; documents ingested before have no object
ALTER TABLE gogi_documents
    ADD COLUMN size_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN object_key TEXT NOT NULL DEFAULT '';
