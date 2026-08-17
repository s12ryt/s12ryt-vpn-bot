CREATE TABLE IF NOT EXISTS backup_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    retention_days INTEGER NOT NULL DEFAULT 7 CHECK (retention_days BETWEEN 1 AND 3650),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO backup_settings (singleton, retention_days)
VALUES (TRUE, 7)
ON CONFLICT (singleton) DO NOTHING;
