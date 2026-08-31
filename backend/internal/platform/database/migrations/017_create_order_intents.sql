-- 采购意向订单（供应链最开端：顾客发意向 → 审批 → 生产 → 面谈成交）
INSERT IGNORE INTO roles (role_code, role_name)
VALUES ('CUSTOMER', '顾客');
CREATE TABLE IF NOT EXISTS order_headers (
    id BIGINT NOT NULL AUTO_INCREMENT,
    order_no VARCHAR(32) NOT NULL COMMENT '意向单号，Go 生成如 INT-20260830-001',
    status VARCHAR(16) NOT NULL DEFAULT 'PENDING' COMMENT 'PENDING/APPROVED/TRADING/CONFIRMED/CLOSED/REJECTED/DELETED',
    customer_id BIGINT NOT NULL,
    expected_time DATETIME(6) NULL COMMENT '期望时间（可选）',
    remark VARCHAR(500) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT uk_order_headers_order_no UNIQUE (order_no),
    KEY idx_order_headers_status (status, created_at, id),
    KEY idx_order_headers_customer (customer_id, created_at, id),
    CONSTRAINT fk_order_headers_customer FOREIGN KEY (customer_id) REFERENCES users (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS order_items (
    id BIGINT NOT NULL AUTO_INCREMENT,
    order_id BIGINT NOT NULL,
    material_id BIGINT NOT NULL,
    quantity DECIMAL(18, 3) NOT NULL COMMENT '意向数量',
    warehouse_id BIGINT NULL COMMENT '成交时指定扣库仓库（意向阶段可空）',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT uk_order_items_order_material UNIQUE (order_id, material_id),
    CONSTRAINT chk_order_items_quantity_positive CHECK (quantity > 0),
    CONSTRAINT fk_order_items_order FOREIGN KEY (order_id) REFERENCES order_headers (id),
    CONSTRAINT fk_order_items_material FOREIGN KEY (material_id) REFERENCES materials (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
