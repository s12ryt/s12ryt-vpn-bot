CREATE TABLE IF NOT EXISTS reality_health (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    target_domain TEXT NOT NULL CHECK (char_length(target_domain) BETWEEN 4 AND 253),
    healthy BOOLEAN NOT NULL,
    last_checked_at TIMESTAMPTZ NOT NULL,
    last_transition_at TIMESTAMPTZ NOT NULL,
	last_notification_at TIMESTAMPTZ,
	notification_pending TEXT CHECK (notification_pending IN ('failed', 'recovered')),
    CHECK (last_transition_at <= last_checked_at)
);
