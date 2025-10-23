-- +goose Up
-- USERS
CREATE TABLE IF NOT EXISTS users (
                                     id           BIGSERIAL PRIMARY KEY,
                                     telegram_id  BIGINT      NOT NULL UNIQUE,
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

-- MATERIALS
CREATE TABLE IF NOT EXISTS materials (
                                         id          BIGSERIAL PRIMARY KEY,
                                         name        TEXT        NOT NULL UNIQUE,
                                         category_id BIGINT      NOT NULL REFERENCES material_categories(id) ON DELETE RESTRICT,
                                         unit        TEXT        NOT NULL DEFAULT 'pcs',
                                         active      BOOLEAN     NOT NULL DEFAULT TRUE,
                                         created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_materials_category ON materials(category_id);

-- BALANCES
CREATE TABLE IF NOT EXISTS balances (
                                        warehouse_id BIGINT NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
                                        material_id  BIGINT NOT NULL REFERENCES materials(id)  ON DELETE CASCADE,
                                        qty          NUMERIC(18,3) NOT NULL DEFAULT 0,
                                        PRIMARY KEY (warehouse_id, material_id)
);

-- MOVEMENTS (приход/списание)
CREATE TABLE IF NOT EXISTS movements (
                                         id           BIGSERIAL PRIMARY KEY,
                                         created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                         actor_id     BIGINT      NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
                                         warehouse_id BIGINT      NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
                                         material_id  BIGINT      NOT NULL REFERENCES materials(id)  ON DELETE RESTRICT,
                                         qty          NUMERIC(18,3) NOT NULL, -- >0 для приходов, <0 для списаний
                                         type         TEXT NOT NULL CHECK (type IN ('in','out')),
                                         note         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_movements_wh_mat_time ON movements(warehouse_id, material_id, created_at DESC);


-- +goose Down
DROP TABLE IF EXISTS balances;
DROP TABLE IF EXISTS materials;
DROP TABLE IF EXISTS material_categories;
DROP TABLE IF EXISTS warehouses;
DROP TABLE IF EXISTS users;
