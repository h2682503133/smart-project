INSERT IGNORE INTO roles (role_code, role_name)
VALUES ('WAREHOUSE_MANAGER', '仓库管理员');

CREATE TABLE IF NOT EXISTS materials (
    id BIGINT NOT NULL AUTO_INCREMENT,
    name VARCHAR(128) NOT NULL,
    category VARCHAR(64) NOT NULL,
    unit VARCHAR(32) NOT NULL,
    spec VARCHAR(255) NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT uk_materials_name UNIQUE (name),
    KEY idx_materials_status_category (status, category, id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS warehouses (
    id BIGINT NOT NULL AUTO_INCREMENT,
    name VARCHAR(128) NOT NULL,
    location VARCHAR(255) NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT uk_warehouses_name UNIQUE (name),
    KEY idx_warehouses_status (status, id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS stocks (
    id BIGINT NOT NULL AUTO_INCREMENT,
    warehouse_id BIGINT NOT NULL,
    material_id BIGINT NOT NULL,
    quantity DECIMAL(18, 3) NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT uk_stocks_warehouse_material UNIQUE (warehouse_id, material_id),
    CONSTRAINT chk_stocks_quantity_nonnegative CHECK (quantity >= 0),
    KEY idx_stocks_material_status (material_id, status, warehouse_id),
    CONSTRAINT fk_stocks_warehouse FOREIGN KEY (warehouse_id) REFERENCES warehouses (id),
    CONSTRAINT fk_stocks_material FOREIGN KEY (material_id) REFERENCES materials (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS stock_records (
    id BIGINT NOT NULL AUTO_INCREMENT,
    warehouse_id BIGINT NOT NULL,
    material_id BIGINT NOT NULL,
    type VARCHAR(8) NOT NULL,
    quantity DECIMAL(18, 3) NOT NULL,
    ref_type VARCHAR(32) NOT NULL,
    ref_id VARCHAR(128) NOT NULL,
    plot_id BIGINT NULL,
    operator_id BIGINT NOT NULL,
    remark VARCHAR(500) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT chk_stock_records_quantity_positive CHECK (quantity > 0),
    CONSTRAINT chk_stock_records_type CHECK (type IN ('IN', 'OUT')),
    CONSTRAINT chk_stock_records_ref_type CHECK (ref_type IN ('HARVEST', 'ORDER', 'ADJUSTMENT')),
    CONSTRAINT uk_stock_records_business_ref UNIQUE (ref_type, ref_id, warehouse_id, material_id),
    KEY idx_stock_records_material_created (material_id, created_at, id),
    KEY idx_stock_records_warehouse_created (warehouse_id, created_at, id),
    KEY idx_stock_records_plot_created (plot_id, created_at, id),
    CONSTRAINT fk_stock_records_warehouse FOREIGN KEY (warehouse_id) REFERENCES warehouses (id),
    CONSTRAINT fk_stock_records_material FOREIGN KEY (material_id) REFERENCES materials (id),
    CONSTRAINT fk_stock_records_plot FOREIGN KEY (plot_id) REFERENCES plots (id),
    CONSTRAINT fk_stock_records_operator FOREIGN KEY (operator_id) REFERENCES users (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
