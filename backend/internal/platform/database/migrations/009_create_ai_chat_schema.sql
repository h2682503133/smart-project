CREATE TABLE chat_sessions (
    id VARCHAR(64) NOT NULL,
    user_id BIGINT NOT NULL,
    plot_id BIGINT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE' COMMENT 'ACTIVE/CLOSED',
    summary TEXT NULL,
    last_message_at DATETIME(6) NULL,
    closed_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_chat_sessions_user_updated (user_id, updated_at),
    KEY idx_chat_sessions_plot_updated (plot_id, updated_at),
    CONSTRAINT fk_chat_sessions_user FOREIGN KEY (user_id) REFERENCES users (id),
    CONSTRAINT fk_chat_sessions_plot FOREIGN KEY (plot_id) REFERENCES plots (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE chat_messages (
    id BIGINT NOT NULL AUTO_INCREMENT,
    session_id VARCHAR(64) NOT NULL,
    role VARCHAR(16) NOT NULL COMMENT 'USER/ASSISTANT/SYSTEM/TOOL',
    content LONGTEXT NOT NULL,
    citations_json JSON NULL,
    plot_id BIGINT NULL,
    model_version VARCHAR(64) NULL,
    trace_id VARCHAR(64) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_chat_messages_session_created (session_id, created_at, id),
    KEY idx_chat_messages_plot_created (plot_id, created_at),
    CONSTRAINT fk_chat_messages_session FOREIGN KEY (session_id) REFERENCES chat_sessions (id),
    CONSTRAINT fk_chat_messages_plot FOREIGN KEY (plot_id) REFERENCES plots (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE ai_suggestions (
    id VARCHAR(64) NOT NULL,
    session_id VARCHAR(64) NOT NULL,
    plot_id BIGINT NOT NULL,
    action VARCHAR(32) NOT NULL,
    duration_seconds INT NULL,
    confidence DECIMAL(5, 4) NULL,
    reason VARCHAR(500) NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'PENDING' COMMENT 'PENDING/ACCEPTED/REJECTED/EXPIRED',
    accepted_by BIGINT NULL,
    accepted_at DATETIME(6) NULL,
    command_id VARCHAR(64) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_ai_suggestions_session_created (session_id, created_at),
    KEY idx_ai_suggestions_plot_status (plot_id, status),
    CONSTRAINT fk_ai_suggestions_session FOREIGN KEY (session_id) REFERENCES chat_sessions (id),
    CONSTRAINT fk_ai_suggestions_plot FOREIGN KEY (plot_id) REFERENCES plots (id),
    CONSTRAINT fk_ai_suggestions_accept_user FOREIGN KEY (accepted_by) REFERENCES users (id),
    CONSTRAINT fk_ai_suggestions_command FOREIGN KEY (command_id) REFERENCES device_commands (command_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
