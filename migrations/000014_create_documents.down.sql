DROP TABLE gogi_documents;
ALTER TABLE jobs ALTER COLUMN document_id TYPE UUID USING document_id::uuid;
