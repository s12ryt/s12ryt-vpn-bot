CREATE TABLE IF NOT EXISTS traffic_ingestion_batches (
    batch_id CHAR(64) PRIMARY KEY
        CHECK (batch_id ~ '^[0-9a-f]{64}$'),
    collected_at TIMESTAMPTZ NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS traffic_ingestion_batches_committed_at_idx
    ON traffic_ingestion_batches (committed_at);
