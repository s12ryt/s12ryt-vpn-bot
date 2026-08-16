CREATE TABLE IF NOT EXISTS administrators (
    telegram_id BIGINT PRIMARY KEY CHECK (telegram_id > 0),
    role TEXT NOT NULL CHECK (role IN ('owner', 'administrator')),
    is_root BOOLEAN NOT NULL DEFAULT FALSE,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (NOT is_root OR role = 'owner')
);

CREATE TABLE IF NOT EXISTS admin_login_codes (
    telegram_id BIGINT PRIMARY KEY REFERENCES administrators (telegram_id) ON DELETE CASCADE,
    digest BYTEA NOT NULL UNIQUE CHECK (octet_length(digest) = 32),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS admin_sessions (
    digest BYTEA PRIMARY KEY CHECK (octet_length(digest) = 32),
    telegram_id BIGINT NOT NULL REFERENCES administrators (telegram_id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    CHECK (last_seen_at >= created_at),
    CHECK (absolute_expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS admin_sessions_telegram_id_idx ON admin_sessions (telegram_id);
CREATE INDEX IF NOT EXISTS admin_sessions_expiry_idx ON admin_sessions (absolute_expires_at);
