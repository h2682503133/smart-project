ALTER TABLE alerts
    ADD COLUMN confirmation_remark VARCHAR(500) NULL AFTER acknowledged_at;
