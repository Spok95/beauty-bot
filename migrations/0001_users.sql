-- +goose Up
CREATE TABLE IF NOT EXISTS users (
                                     id           BIGSERIAL PRIMARY KEY,
                                     telegram_id  BIGINT      NOT NULL UNIQUE,
    -- username хранит ФИО, не допускаем NULL
                                     username     TEXT        NOT NULL DEFAULT '',
                                     role         TEXT        NOT NULL DEFAULT 'master'
                                     CHECK (role IN ('master','administrator','admin')),
    status       TEXT        NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','approved','rejected')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);

-- +goose Down
DROP TABLE IF EXISTS users;
