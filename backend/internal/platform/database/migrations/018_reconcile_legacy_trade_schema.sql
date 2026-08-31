-- 兼容曾经由 015_create_trade_schema.sql 创建的旧交易表。
-- 当前仓储服务以 015_create_warehouse_schema.sql 与 017_create_order_intents.sql 为准。

SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE materials ADD COLUMN status VARCHAR(32) NOT NULL DEFAULT ''ACTIVE'' AFTER spec',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'materials' AND column_name = 'status';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE warehouses ADD COLUMN location VARCHAR(255) NULL AFTER name',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'warehouses' AND column_name = 'location';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE warehouses ADD COLUMN status VARCHAR(32) NOT NULL DEFAULT ''ACTIVE'' AFTER location',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'warehouses' AND column_name = 'status';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

SELECT IF(
    COUNT(*) > 0,
    'ALTER TABLE warehouses MODIFY COLUMN code VARCHAR(64) NULL',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'warehouses' AND column_name = 'code';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE stocks ADD COLUMN status VARCHAR(32) NOT NULL DEFAULT ''ACTIVE'' AFTER quantity',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'stocks' AND column_name = 'status';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE stock_records ADD COLUMN type VARCHAR(8) NOT NULL DEFAULT ''IN'' AFTER material_id',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'stock_records' AND column_name = 'type';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE stock_records ADD COLUMN ref_type VARCHAR(32) NULL AFTER quantity',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'stock_records' AND column_name = 'ref_type';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE stock_records ADD COLUMN ref_id VARCHAR(128) NULL AFTER ref_type',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'stock_records' AND column_name = 'ref_id';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE stock_records ADD COLUMN plot_id BIGINT NULL AFTER ref_id',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'stock_records' AND column_name = 'plot_id';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE stock_records ADD COLUMN operator_id BIGINT NULL AFTER plot_id',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'stock_records' AND column_name = 'operator_id';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE stock_records ADD COLUMN remark VARCHAR(500) NULL AFTER operator_id',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'stock_records' AND column_name = 'remark';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE stock_records ADD COLUMN updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6) AFTER created_at',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'stock_records' AND column_name = 'updated_at';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

SELECT IF(
    COUNT(*) > 0,
    'ALTER TABLE stock_records MODIFY COLUMN direction VARCHAR(16) NULL',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'stock_records' AND column_name = 'direction';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

SELECT IF(
    COUNT(*) > 0,
    'UPDATE stock_records SET type = CASE WHEN direction = ''OUT'' THEN ''OUT'' ELSE ''IN'' END',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'stock_records' AND column_name = 'direction';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

UPDATE stock_records
SET ref_type = 'ADJUSTMENT'
WHERE ref_type IS NULL OR ref_type = '';
UPDATE stock_records
SET ref_id = CONCAT('LEGACY-', id)
WHERE ref_id IS NULL OR ref_id = '';
UPDATE stock_records
SET operator_id = 0
WHERE operator_id IS NULL;
ALTER TABLE stock_records MODIFY COLUMN ref_type VARCHAR(32) NOT NULL;
ALTER TABLE stock_records MODIFY COLUMN ref_id VARCHAR(128) NOT NULL;
ALTER TABLE stock_records MODIFY COLUMN operator_id BIGINT NOT NULL;

SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE order_items ADD COLUMN warehouse_id BIGINT NULL AFTER quantity',
    'SELECT 1'
) INTO @migration_sql
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = 'order_items' AND column_name = 'warehouse_id';
PREPARE migration_statement FROM @migration_sql;
EXECUTE migration_statement;
DEALLOCATE PREPARE migration_statement;

ALTER TABLE order_headers MODIFY COLUMN remark VARCHAR(500) NULL;
