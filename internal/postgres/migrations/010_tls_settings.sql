CREATE TABLE IF NOT EXISTS tls_settings (
    singleton BOOLEAN NOT NULL PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    configured BOOLEAN NOT NULL DEFAULT FALSE,
    mode TEXT NOT NULL DEFAULT 'custom' CHECK (mode IN ('sslip_io', 'duckdns', 'custom')),
    domain TEXT NOT NULL DEFAULT '',
    challenge TEXT NOT NULL DEFAULT 'http_01' CHECK (challenge IN ('http_01', 'dns_01')),
    email TEXT NOT NULL DEFAULT '',
    ca_directory_urls TEXT[] NOT NULL DEFAULT '{}',
    terms_accepted BOOLEAN NOT NULL DEFAULT FALSE,
    duckdns_token_nonce BYTEA,
    duckdns_token_ciphertext BYTEA,
    state TEXT NOT NULL DEFAULT 'unissued' CHECK (state IN ('unissued', 'issued', 'failed')),
    certificate_expires_at TIMESTAMPTZ,
    last_issued_ca TEXT NOT NULL DEFAULT '',
    last_failure_at TIMESTAMPTZ,
    last_failure_reason TEXT NOT NULL DEFAULT '' CHECK (last_failure_reason IN ('', 'all_cas_failed')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tls_settings_token_pair CHECK ((duckdns_token_nonce IS NULL) = (duckdns_token_ciphertext IS NULL)),
    CONSTRAINT tls_settings_issued_complete CHECK (state <> 'issued' OR (certificate_expires_at IS NOT NULL AND last_issued_ca <> '')),
    CONSTRAINT tls_settings_duckdns_requires_dns CHECK (mode <> 'duckdns' OR (challenge = 'dns_01' AND duckdns_token_nonce IS NOT NULL)),
    CONSTRAINT tls_settings_sslip_requires_http CHECK (mode <> 'sslip_io' OR challenge = 'http_01')
);

INSERT INTO tls_settings (singleton) VALUES (TRUE) ON CONFLICT (singleton) DO NOTHING;
