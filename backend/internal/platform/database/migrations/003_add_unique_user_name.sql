ALTER TABLE users
    ADD CONSTRAINT uk_users_name UNIQUE (name);
