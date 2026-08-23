ALTER TABLE plots
    ADD COLUMN code VARCHAR(32) NULL AFTER owner_id;

UPDATE plots
SET code = CONCAT('P', id)
WHERE code IS NULL;

ALTER TABLE plots
    MODIFY code VARCHAR(32) NOT NULL,
    ADD CONSTRAINT uk_plots_owner_code UNIQUE (owner_id, code);
