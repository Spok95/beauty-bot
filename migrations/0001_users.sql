-- +goose Up
CREATE TABLE IF NOT EXISTS users (
                                     id           BIGSERIAL PRIMARY KEY,
                                     telegram_id  BIGINT      NOT NULL UNIQUE,
                                     username     TEXT,
                                     first_name   TEXT,
                                     last_name    TEXT,
                                     role         TEXT        NOT NULL DEFAULT 'master' CHECK (role IN ('master','admin')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);

-- +goose Down
DROP TABLE IF EXISTS users;
