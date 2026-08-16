ALTER TABLE vpn_users
    DROP CONSTRAINT IF EXISTS vpn_users_status_check;

ALTER TABLE vpn_users
    ADD CONSTRAINT vpn_users_status_check CHECK (status IN (
        'unclaimed',
        'active',
        'pending_approval',
        'approval_rejected',
        'self_service',
        'permanently_blocked'
    ));
