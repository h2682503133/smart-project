ALTER TABLE plots
    ADD COLUMN owner_id BIGINT NULL AFTER id,
    ADD KEY idx_plots_owner (owner_id),
    ADD CONSTRAINT fk_plots_owner FOREIGN KEY (owner_id) REFERENCES users (id);

UPDATE plots AS p
JOIN farms AS f ON f.id = p.farm_id
SET p.owner_id = f.owner_id;

ALTER TABLE plots
    MODIFY owner_id BIGINT NOT NULL,
    DROP FOREIGN KEY fk_plots_farm,
    DROP INDEX idx_plots_farm,
    DROP COLUMN farm_id;

ALTER TABLE audit_logs
    DROP FOREIGN KEY fk_audit_logs_farm,
    DROP INDEX idx_audit_logs_farm_created,
    DROP COLUMN farm_id;

INSERT IGNORE INTO user_roles (user_id, role_id)
SELECT ur.user_id, farmer.id
FROM user_roles AS ur
JOIN roles AS old_role ON old_role.id = ur.role_id AND old_role.role_code = 'FARM_ADMIN'
JOIN roles AS farmer ON farmer.role_code = 'FARMER';

DELETE ur
FROM user_roles AS ur
JOIN roles AS role_to_remove ON role_to_remove.id = ur.role_id
WHERE role_to_remove.role_code = 'FARM_ADMIN';

DELETE FROM roles WHERE role_code = 'FARM_ADMIN';

DROP TABLE farm_users;
DROP TABLE farms;
