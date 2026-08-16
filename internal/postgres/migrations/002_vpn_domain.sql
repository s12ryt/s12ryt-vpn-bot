CREATE TABLE IF NOT EXISTS qualification_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    mode TEXT NOT NULL DEFAULT 'any' CHECK (mode IN ('any', 'all')),
    recheck_interval_minutes INTEGER NOT NULL DEFAULT 60 CHECK (recheck_interval_minutes BETWEEN 1 AND 10080),
    inactivity_threshold_days INTEGER NOT NULL DEFAULT 0 CHECK (inactivity_threshold_days >= 0),
    quota_limit_bytes BIGINT NOT NULL DEFAULT 50000000000 CHECK (quota_limit_bytes > 0),
    quota_period_seconds BIGINT NOT NULL DEFAULT 2592000 CHECK (quota_period_seconds > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO qualification_settings (singleton)
VALUES (TRUE)
ON CONFLICT (singleton) DO NOTHING;

CREATE TABLE IF NOT EXISTS qualification_rules (
    chat_id BIGINT PRIMARY KEY CHECK (chat_id <> 0),
    chat_type TEXT NOT NULL CHECK (chat_type IN ('supergroup', 'channel')),
    title TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    bot_admin_verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (NOT enabled OR bot_admin_verified_at IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS qualification_rules_enabled_idx
    ON qualification_rules (enabled, chat_id);

CREATE TABLE IF NOT EXISTS vpn_users (
    telegram_id BIGINT PRIMARY KEY CHECK (telegram_id > 0),
    eligible BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'unclaimed'
        CHECK (status IN ('unclaimed', 'active', 'pending_approval', 'self_service', 'permanently_blocked')),
    credential_generation BIGINT NOT NULL DEFAULT 0 CHECK (credential_generation >= 0),
    period_started_at TIMESTAMPTZ,
    last_vpn_activity_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((credential_generation = 0 AND period_started_at IS NULL AND last_vpn_activity_at IS NULL)
        OR (credential_generation > 0 AND period_started_at IS NOT NULL AND last_vpn_activity_at IS NOT NULL)),
    CHECK (last_vpn_activity_at IS NULL OR period_started_at IS NULL OR last_vpn_activity_at >= period_started_at)
);

CREATE INDEX IF NOT EXISTS vpn_users_status_idx ON vpn_users (status, telegram_id);
CREATE INDEX IF NOT EXISTS vpn_users_activity_idx ON vpn_users (last_vpn_activity_at) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS credential_bundles (
    telegram_id BIGINT PRIMARY KEY REFERENCES vpn_users (telegram_id) ON DELETE CASCADE,
    generation BIGINT NOT NULL CHECK (generation > 0),
    subscription_token_digest BYTEA NOT NULL UNIQUE CHECK (octet_length(subscription_token_digest) = 32),
    nonce BYTEA NOT NULL CHECK (octet_length(nonce) = 12),
    ciphertext BYTEA NOT NULL CHECK (octet_length(ciphertext) > 16),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS quota_windows (
    telegram_id BIGINT PRIMARY KEY REFERENCES vpn_users (telegram_id) ON DELETE CASCADE,
    period_started_at TIMESTAMPTZ NOT NULL,
    period_seconds BIGINT NOT NULL DEFAULT 2592000 CHECK (period_seconds > 0),
    limit_bytes BIGINT NOT NULL DEFAULT 50000000000 CHECK (limit_bytes > 0),
    used_bytes BIGINT NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    blocked BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (blocked = (used_bytes >= limit_bytes))
);

CREATE INDEX IF NOT EXISTS quota_windows_period_idx ON quota_windows (period_started_at);

CREATE TABLE IF NOT EXISTS audit_events (
    id BIGSERIAL PRIMARY KEY,
    actor_telegram_id BIGINT REFERENCES administrators (telegram_id) ON DELETE SET NULL,
    action TEXT NOT NULL CHECK (length(action) BETWEEN 1 AND 120),
    target_type TEXT NOT NULL CHECK (length(target_type) BETWEEN 1 AND 80),
    target_id TEXT NOT NULL DEFAULT '',
    details JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS audit_events_created_at_idx ON audit_events (created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS audit_events_actor_idx ON audit_events (actor_telegram_id, created_at DESC);
