CREATE TABLE IF NOT EXISTS core_action_outbox (
    id BIGSERIAL PRIMARY KEY,
    telegram_id BIGINT NOT NULL REFERENCES vpn_users (telegram_id) ON DELETE CASCADE,
    action TEXT NOT NULL CHECK (action IN ('revoke')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS core_action_outbox_pending_user_action_idx
    ON core_action_outbox (telegram_id, action)
    WHERE completed_at IS NULL;

CREATE INDEX IF NOT EXISTS core_action_outbox_pending_idx
    ON core_action_outbox (available_at, id)
    WHERE completed_at IS NULL;
