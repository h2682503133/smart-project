ALTER TABLE users
    ADD COLUMN interaction_style VARCHAR(16) NULL
        COMMENT '语言风格：plain/casual/professional' AFTER status,
    ADD COLUMN knowledge_reliance VARCHAR(16) NULL
        COMMENT '决策依据：experience/document/data' AFTER interaction_style;
