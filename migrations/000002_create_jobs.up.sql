ALTER TABLE jobs
DROP COLUMN worker_id;

ALTER TABLE jobs
ADD COLUMN job_type VARCHAR(64) NOT NULL DEFAULT 'UNKNOWN';
-- the ingestions waiting for the worker where gogi runs it as a process polling Postgres (in the cloud
-- they wait in SQS or an Azure Storage queue instead). The worker takes a message by hiding it until
-- visible_at, and deletes it once done, so a message it failed on, or died holding, is taken again
CREATE TABLE gogi_ingestion_queue (
    job_id UUID PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
    message JSONB NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    visible_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    enqueued_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX gogi_ingestion_queue_visible ON gogi_ingestion_queue (visible_at, enqueued_at);
