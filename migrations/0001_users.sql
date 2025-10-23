-- +goose Up
-- USERS
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

-- CATALOG: WAREHOUSES & CATEGORIES
CREATE TABLE IF NOT EXISTS warehouses (
                                          id         BIGSERIAL PRIMARY KEY,
                                          name       TEXT NOT NULL UNIQUE,
                                          type       TEXT NOT NULL CHECK (type IN ('consumables','client_service')),
                                          active     BOOLEAN NOT NULL DEFAULT TRUE,
                                          created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS material_categories (
                                                   id         BIGSERIAL PRIMARY KEY,
                                                   name       TEXT NOT NULL UNIQUE,
                                                   active     BOOLEAN NOT NULL DEFAULT TRUE,
                                                   created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS material_categories;
DROP TABLE IF EXISTS warehouses;
DROP TABLE IF EXISTS users;
