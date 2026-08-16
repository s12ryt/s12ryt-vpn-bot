ALTER TABLE qualification_settings
    ADD COLUMN IF NOT EXISTS recheck_requests_per_second INTEGER NOT NULL DEFAULT 10
        CHECK (recheck_requests_per_second BETWEEN 1 AND 20),
    ADD COLUMN IF NOT EXISTS recheck_batch_size INTEGER NOT NULL DEFAULT 50
        CHECK (recheck_batch_size BETWEEN 10 AND 200);
