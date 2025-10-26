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
                                         id             BIGSERIAL   PRIMARY KEY,
                                         name           TEXT        NOT NULL UNIQUE,
                                         category_id    BIGINT      NOT NULL REFERENCES material_categories(id) ON DELETE RESTRICT,
                                         unit           TEXT        NOT NULL DEFAULT 'pcs',
                                         price_per_unit NUMERIC(12,2) NOT NULL DEFAULT 0, -- ₽ за g / шт
                                         active         BOOLEAN     NOT NULL DEFAULT TRUE,
                                         created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
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

-- Поставки (приёмка материалов с ценой)
CREATE TABLE IF NOT EXISTS supplies (
                                        id           BIGSERIAL   PRIMARY KEY,
                                        created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                        added_by     BIGINT      NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
                                        warehouse_id BIGINT      NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
                                        material_id  BIGINT      NOT NULL REFERENCES materials(id)  ON DELETE RESTRICT,
                                        qty          NUMERIC(18,3) NOT NULL CHECK (qty > 0),
                                        unit_cost    NUMERIC(12,2) NOT NULL DEFAULT 0,
                                        total_cost   NUMERIC(14,2) NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_supplies_wh_mat_time ON supplies(warehouse_id, material_id, created_at DESC);

-- Тарифы аренды
CREATE TABLE IF NOT EXISTS rent_rates (
                                          id BIGSERIAL PRIMARY KEY,
                                          place TEXT NOT NULL CHECK (place IN ('hall','cabinet')),
                                          with_subscription BOOLEAN NOT NULL,
                                          unit TEXT NOT NULL CHECK (unit IN ('hour','day')),
                                          threshold_materials NUMERIC(12,2) NOT NULL,         -- 100 для hall/hour, 1000 для cabinet/day
                                          price_with_materials NUMERIC(12,2) NOT NULL,        -- «наши материалы»
                                          price_own_materials NUMERIC(12,2) NOT NULL,         -- «со своими материалами»
                                          active_from DATE NOT NULL DEFAULT CURRENT_DATE,
                                          active_to   DATE
);

-- Сессия расхода/аренды
CREATE TABLE IF NOT EXISTS consumption_sessions (
                                                    id BIGSERIAL PRIMARY KEY,
                                                    user_id BIGINT NOT NULL REFERENCES users(id),
                                                    place TEXT NOT NULL CHECK (place IN ('hall','cabinet')),
                                                    unit  TEXT NOT NULL CHECK (unit IN ('hour','day')),
                                                    qty INT NOT NULL CHECK (qty > 0),
                                                    with_subscription BOOLEAN NOT NULL DEFAULT false,
                                                    materials_sum NUMERIC(12,2) NOT NULL DEFAULT 0,
                                                    rounded_materials_sum NUMERIC(12,2) NOT NULL DEFAULT 0,
                                                    rent NUMERIC(12,2) NOT NULL DEFAULT 0,
                                                    total NUMERIC(12,2) NOT NULL DEFAULT 0,
                                                    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','confirmed','canceled')),
                                                    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
                                                    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS consumption_items (
                                                 id BIGSERIAL PRIMARY KEY,
                                                 session_id BIGINT NOT NULL REFERENCES consumption_sessions(id) ON DELETE CASCADE,
                                                 material_id BIGINT NOT NULL REFERENCES materials(id),
                                                 qty NUMERIC(18,3) NOT NULL CHECK (qty > 0),
                                                 unit_price NUMERIC(12,2) NOT NULL,
                                                 cost NUMERIC(12,2) NOT NULL
);

-- Инвойс по сессии
CREATE TABLE IF NOT EXISTS invoices (
                                        id BIGSERIAL PRIMARY KEY,
                                        user_id BIGINT NOT NULL REFERENCES users(id),
                                        session_id BIGINT NOT NULL REFERENCES consumption_sessions(id),
                                        amount NUMERIC(14,2) NOT NULL,
                                        currency TEXT NOT NULL DEFAULT 'RUB',
                                        status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','paid','canceled')),
                                        payment_link TEXT NOT NULL DEFAULT '',
                                        created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ===== СИДЫ (без абонемента) =====
-- Общий зал (почасово)
INSERT INTO rent_rates (place, with_subscription, unit, threshold_materials, price_with_materials, price_own_materials, active_from)
SELECT 'hall', FALSE, 'hour', 100, 490, 640, CURRENT_DATE
WHERE NOT EXISTS (
    SELECT 1 FROM rent_rates
    WHERE place='hall' AND with_subscription=FALSE AND unit='hour'
      AND (active_to IS NULL OR active_to>=CURRENT_DATE)
);

-- Кабинет (посуточно)
INSERT INTO rent_rates (place, with_subscription, unit, threshold_materials, price_with_materials, price_own_materials, active_from)
SELECT 'cabinet', FALSE, 'day', 1000, 5500, 6500, CURRENT_DATE
WHERE NOT EXISTS (
    SELECT 1 FROM rent_rates
    WHERE place='cabinet' AND with_subscription=FALSE AND unit='day'
      AND (active_to IS NULL OR active_to>=CURRENT_DATE)
);

CREATE TABLE IF NOT EXISTS subscriptions (
                                             id          BIGSERIAL PRIMARY KEY,
                                             user_id     BIGINT      NOT NULL,
                                             place       TEXT        NOT NULL,   -- 'hall' | 'cabinet'
                                             unit        TEXT        NOT NULL,   -- 'hour' | 'day'
                                             month       TEXT        NOT NULL,   -- 'YYYY-MM'
                                             total_qty   INT         NOT NULL,   -- куплено часов/дней
                                             used_qty    INT         NOT NULL DEFAULT 0,
                                             created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                                             updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                                             CONSTRAINT uq_subs UNIQUE (user_id, place, unit, month)
);

CREATE INDEX IF NOT EXISTS idx_subs_user_month ON subscriptions (user_id, month);

-- +goose Down
DROP TABLE IF EXISTS balances;
DROP TABLE IF EXISTS materials;
DROP TABLE IF EXISTS material_categories;
DROP TABLE IF EXISTS warehouses;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS movements;
DROP TABLE IF EXISTS supplies;
DROP TABLE IF EXISTS rent_rates;
DROP TABLE IF EXISTS consumption_sessions;
DROP TABLE IF EXISTS consumption_items;
DROP TABLE IF EXISTS invoices;
DROP TABLE IF EXISTS subscriptions;