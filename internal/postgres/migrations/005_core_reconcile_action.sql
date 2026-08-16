ALTER TABLE core_action_outbox
    DROP CONSTRAINT IF EXISTS core_action_outbox_action_check;

ALTER TABLE core_action_outbox
    ADD CONSTRAINT core_action_outbox_action_check
    CHECK (action IN ('revoke', 'reconcile'));
