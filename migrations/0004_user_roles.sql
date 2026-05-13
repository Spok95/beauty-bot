-- +goose Up

CREATE TABLE IF NOT EXISTS user_roles (
                                          user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                          role TEXT NOT NULL CHECK (role IN ('master','administrator','admin')),
                                          created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                                          PRIMARY KEY (user_id, role)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_role
    ON user_roles(role);

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS active_role TEXT CHECK (active_role IN ('master','administrator','admin'));

UPDATE users
SET active_role = role
WHERE active_role IS NULL;

INSERT INTO user_roles (user_id, role)
SELECT id, role
FROM users
ON CONFLICT DO NOTHING;

ALTER TABLE users
    ALTER COLUMN active_role SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_users_active_role
    ON users(active_role);

-- +goose Down

DROP INDEX IF EXISTS idx_users_active_role;

ALTER TABLE users
    DROP COLUMN IF EXISTS active_role;

DROP TABLE IF EXISTS user_roles;