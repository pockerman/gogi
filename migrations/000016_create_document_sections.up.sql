-- the sections of each document, as its ingestion parsed them: chapters, the headings within them,
-- or the pages of a PDF without an outline. Documents are read section by section.
CREATE TABLE gogi_document_sections (
    index_id UUID NOT NULL,
    document_id TEXT NOT NULL,
    -- 1-based, in the order of the document
    position INTEGER NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    level INTEGER NOT NULL DEFAULT 1,
    page_start INTEGER NOT NULL DEFAULT 0,
    page_end INTEGER NOT NULL DEFAULT 0,
    -- the section's own text, without its subsections'
    content TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (index_id, document_id, position),
    FOREIGN KEY (index_id, document_id) REFERENCES gogi_documents (index_id, id) ON DELETE CASCADE
);

-- the models each document is embedded by. A model's chunks of the document are in a collection of
-- the index's for that model; ingesting the document with the model again replaces them
CREATE TABLE gogi_document_embeddings (
    index_id UUID NOT NULL,
    document_id TEXT NOT NULL,
    -- as the application named it, e.g. "bge-small-en-v1.5"
    model TEXT NOT NULL,
    -- the job that embedded them
    job_id UUID,
    chunk_count INTEGER NOT NULL DEFAULT 0,
    embedded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (index_id, document_id, model),
    FOREIGN KEY (index_id, document_id) REFERENCES gogi_documents (index_id, id) ON DELETE CASCADE
);
