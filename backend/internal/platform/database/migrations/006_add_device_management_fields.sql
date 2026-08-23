ALTER TABLE devices
    ADD COLUMN name VARCHAR(128) NOT NULL DEFAULT '' AFTER serial_no,
    ADD COLUMN battery INT NULL AFTER status,
    ADD COLUMN `signal` INT NULL AFTER battery,
    ADD COLUMN firmware_version VARCHAR(64) NULL AFTER `signal`,
    ADD COLUMN status_message VARCHAR(255) NULL AFTER firmware_version;
