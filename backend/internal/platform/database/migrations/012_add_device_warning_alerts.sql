ALTER TABLE alerts
    DROP FOREIGN KEY fk_alerts_rule,
    ADD COLUMN plot_id BIGINT NULL AFTER rule_id,
    ADD COLUMN source VARCHAR(16) NOT NULL DEFAULT 'RULE' AFTER device_id,
    ADD COLUMN warning_type VARCHAR(32) NULL AFTER source,
    ADD COLUMN active_dedup_key VARCHAR(128) NULL AFTER warning_type;

UPDATE alerts AS a
JOIN alert_rules AS ar ON ar.id = a.rule_id
SET a.plot_id = ar.plot_id
WHERE a.plot_id IS NULL;

ALTER TABLE alerts
    MODIFY COLUMN plot_id BIGINT NOT NULL,
    MODIFY COLUMN rule_id BIGINT NULL,
    ADD UNIQUE KEY uk_alerts_active_dedup (active_dedup_key),
    ADD KEY idx_alerts_plot_status (plot_id, status),
    ADD CONSTRAINT fk_alerts_rule FOREIGN KEY (rule_id) REFERENCES alert_rules (id),
    ADD CONSTRAINT fk_alerts_plot FOREIGN KEY (plot_id) REFERENCES plots (id);
