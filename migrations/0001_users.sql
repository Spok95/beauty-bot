-- +goose Up
CREATE TABLE IF NOT EXISTS users(
                                    id BIGSERIAL PRIMARY KEY,
                                    tg_id BIGINT UNIQUE NOT NULL,
                                    role TEXT NOT NULL CHECK (role IN ('master','admin')),
    username TEXT,
    first_name TEXT,
    created_at TIMESTAMPTZ DEFAULT now()
    );

-- +goose Down
DROP TABLE IF EXISTS users;
