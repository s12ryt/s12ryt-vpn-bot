CREATE TABLE IF NOT EXISTS core_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    configured BOOLEAN NOT NULL DEFAULT FALSE,
    listen_ipv4 INET,
    listen_ipv6 INET,
    vless_port INTEGER NOT NULL DEFAULT 443 CHECK (vless_port BETWEEN 1 AND 65535),
    hysteria2_port INTEGER NOT NULL DEFAULT 443 CHECK (hysteria2_port BETWEEN 1 AND 65535),
    tuic_port INTEGER NOT NULL DEFAULT 8443 CHECK (tuic_port BETWEEN 1 AND 65535),
    anytls_port INTEGER NOT NULL DEFAULT 8443 CHECK (anytls_port BETWEEN 1 AND 65535),
    tls_server_name TEXT NOT NULL DEFAULT '',
    tls_certificate_path TEXT NOT NULL DEFAULT '',
    tls_key_path TEXT NOT NULL DEFAULT '',
    reality_server TEXT NOT NULL DEFAULT '',
    reality_server_port INTEGER NOT NULL DEFAULT 443 CHECK (reality_server_port BETWEEN 1 AND 65535),
    reality_private_key_nonce BYTEA NOT NULL DEFAULT ''::BYTEA,
    reality_private_key_ciphertext BYTEA NOT NULL DEFAULT ''::BYTEA,
    reality_short_id TEXT NOT NULL DEFAULT '',
    stats_listen TEXT NOT NULL DEFAULT '127.0.0.1:10085',
    allow_ipv4_outbound BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (listen_ipv4 IS NULL OR family(listen_ipv4) = 4),
    CHECK (listen_ipv6 IS NULL OR family(listen_ipv6) = 6),
    CHECK (vless_port <> anytls_port),
    CHECK (hysteria2_port <> tuic_port),
    CHECK (length(reality_short_id) <= 16 AND length(reality_short_id) % 2 = 0),
    CHECK (reality_short_id ~ '^[0-9A-Fa-f]*$'),
    CHECK (NOT configured OR (
        (listen_ipv4 IS NOT NULL OR listen_ipv6 IS NOT NULL)
        AND length(trim(tls_server_name)) > 0
        AND length(trim(tls_certificate_path)) > 0
        AND length(trim(tls_key_path)) > 0
        AND length(trim(reality_server)) > 0
        AND octet_length(reality_private_key_nonce) = 12
        AND octet_length(reality_private_key_ciphertext) > 16
        AND length(trim(stats_listen)) > 0
    ))
);

INSERT INTO core_settings (singleton)
VALUES (TRUE)
ON CONFLICT (singleton) DO NOTHING;
