CREATE TABLE IF NOT EXISTS traffic_health (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    fail_closed BOOLEAN NOT NULL DEFAULT FALSE,
    failure_started_at TIMESTAMPTZ,
    failure_stage TEXT CHECK (failure_stage IN ('collect', 'spool', 'record', 'cleanup')),
    last_notified_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (NOT fail_closed OR failure_started_at IS NOT NULL),
    CHECK ((failure_started_at IS NULL) = (failure_stage IS NULL)),
    CHECK (last_notified_at IS NULL OR failure_started_at IS NOT NULL)
);

INSERT INTO traffic_health (singleton, fail_closed, failure_started_at)
VALUES (TRUE, FALSE, NULL)
ON CONFLICT (singleton) DO NOTHING;
