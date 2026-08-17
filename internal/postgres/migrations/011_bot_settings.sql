CREATE TABLE IF NOT EXISTS bot_settings (
    singleton BOOLEAN NOT NULL PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    bot_username TEXT NOT NULL DEFAULT '',
    token_nonce BYTEA,
    token_ciphertext BYTEA,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT bot_settings_token_pair CHECK ((token_nonce IS NULL) = (token_ciphertext IS NULL)),
    CONSTRAINT bot_settings_stored_complete CHECK ((token_nonce IS NULL) OR (bot_username <> ''))
);

INSERT INTO bot_settings (singleton) VALUES (TRUE) ON CONFLICT (singleton) DO NOTHING;
