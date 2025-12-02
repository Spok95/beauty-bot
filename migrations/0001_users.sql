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

-- MATERIAL BRANDS
CREATE TABLE IF NOT EXISTS material_brands (
                                               id          BIGSERIAL PRIMARY KEY,
                                               category_id BIGINT      NOT NULL REFERENCES material_categories(id) ON DELETE RESTRICT,
                                               name        TEXT        NOT NULL,
                                               active      BOOLEAN     NOT NULL DEFAULT TRUE,
                                               created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
                                               CONSTRAINT uq_material_brands_cat_name UNIQUE (category_id, name)
);

CREATE INDEX IF NOT EXISTS idx_material_brands_cat ON material_brands(category_id);

-- MATERIALS
CREATE TABLE IF NOT EXISTS materials (
                                         id             BIGSERIAL     PRIMARY KEY,
                                         name           TEXT          NOT NULL,
                                         category_id    BIGINT        NOT NULL REFERENCES material_categories(id) ON DELETE RESTRICT,
                                         brand_id       BIGINT        NOT NULL REFERENCES material_brands(id) ON DELETE RESTRICT,
                                         unit           TEXT          NOT NULL DEFAULT 'pcs',
                                         price_per_unit NUMERIC(12,2) NOT NULL DEFAULT 0, -- ₽ за g / шт
                                         active         BOOLEAN       NOT NULL DEFAULT TRUE,
                                         created_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
                                         CONSTRAINT uq_materials_brand_name UNIQUE (brand_id, name)
);

CREATE INDEX IF NOT EXISTS idx_materials_category ON materials(category_id);
CREATE INDEX IF NOT EXISTS idx_materials_brand ON materials(brand_id);

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

-- Поставки — шапка поставки (один Excel / одна корзина)
CREATE TABLE IF NOT EXISTS supply_batches (
                                              id           BIGSERIAL   PRIMARY KEY,
                                              created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                              added_by     BIGINT      NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
                                              warehouse_id BIGINT      NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
                                              comment      TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_supply_batches_time ON supply_batches(created_at DESC);

-- Поставки (приёмка материалов с ценой)
CREATE TABLE IF NOT EXISTS supplies (
                                        id           BIGSERIAL   PRIMARY KEY,
                                        created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
                                        added_by     BIGINT      NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
                                        warehouse_id BIGINT      NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
                                        material_id  BIGINT      NOT NULL REFERENCES materials(id)  ON DELETE RESTRICT,
                                        qty          NUMERIC(18,3) NOT NULL CHECK (qty > 0),
                                        unit_cost    NUMERIC(12,2) NOT NULL DEFAULT 0,
                                        total_cost   NUMERIC(14,2) NOT NULL DEFAULT 0,
                                        comment      TEXT          NOT NULL DEFAULT '',
                                        batch_id     BIGINT       REFERENCES supply_batches(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_supplies_wh_mat_time ON supplies(warehouse_id, material_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_supplies_batch ON supplies(batch_id);

-- Тарифы аренды (ступени)
CREATE TABLE IF NOT EXISTS rent_rates (
                                          id BIGSERIAL PRIMARY KEY,
                                          place TEXT NOT NULL CHECK (place IN ('hall','cabinet')),
                                          unit  TEXT NOT NULL CHECK (unit  IN ('hour','day')),
                                          with_subscription BOOLEAN NOT NULL,              -- TRUE = по абонементу, FALSE = разовая аренда
                                          min_qty INT NOT NULL,                            -- нижняя граница (включительно) — часы/дни
                                          max_qty INT,                                     -- верхняя граница (NULL = без верха)
                                          per_unit BOOLEAN NOT NULL DEFAULT TRUE,          -- TRUE = цена за единицу; FALSE = фикс за всю сессию
                                          threshold_materials NUMERIC(12,2) NOT NULL,      -- базовый порог: 100 (за час) / 1000 (за день) или фикс на сессию
                                          price_with_materials NUMERIC(12,2) NOT NULL,     -- «наши материалы»
                                          price_own_materials  NUMERIC(12,2) NOT NULL,     -- «со своими»
                                          active_from DATE NOT NULL DEFAULT CURRENT_DATE,
                                          active_to   DATE
);

CREATE INDEX IF NOT EXISTS idx_rent_rates_key
    ON rent_rates(place, unit, with_subscription, min_qty);

-- АБОНЕМЕНТЫ: ЗАЛ / ЧАС — каждая строка = конкретный лимит
INSERT INTO rent_rates(
    place, unit, with_subscription,
    min_qty, max_qty,
    per_unit,
    threshold_materials,
    price_with_materials,
    price_own_materials
) VALUES
      ('hall','hour',TRUE, 30,30, TRUE, 100, 450, 590),
      ('hall','hour',TRUE, 35,35, TRUE, 100, 450, 590),
      ('hall','hour',TRUE, 40,40, TRUE, 100, 450, 590),
      ('hall','hour',TRUE, 45,45, TRUE, 100, 450, 590),
      ('hall','hour',TRUE, 50,50, TRUE, 100, 420, 530),
      ('hall','hour',TRUE, 55,55, TRUE, 100, 420, 530),
      ('hall','hour',TRUE, 60,60, TRUE, 100, 420, 530),
      ('hall','hour',TRUE, 65,65, TRUE, 100, 420, 530),
      ('hall','hour',TRUE, 70,70, TRUE, 100, 420, 530),
      ('hall','hour',TRUE, 75,75, TRUE, 100, 420, 530),
      ('hall','hour',TRUE, 80,80, TRUE, 100, 390, 480),
      ('hall','hour',TRUE, 85,85, TRUE, 100, 390, 480),
      ('hall','hour',TRUE, 90,90, TRUE, 100, 390, 480),
      ('hall','hour',TRUE, 95,95, TRUE, 100, 390, 480),
      ('hall','hour',TRUE,100,100, TRUE, 100, 390, 480);

-- АБОНЕМЕНТЫ: КАБИНЕТ / ДЕНЬ
INSERT INTO rent_rates(
    place, unit, with_subscription,
    min_qty, max_qty,
    per_unit,
    threshold_materials,
    price_with_materials,
    price_own_materials
) VALUES
      ('cabinet','day',TRUE,10,10, TRUE, 1000, 5000, 6250),
      ('cabinet','day',TRUE,11,11, TRUE, 1000, 5000, 6250),
      ('cabinet','day',TRUE,12,12, TRUE, 1000, 5000, 6250),
      ('cabinet','day',TRUE,13,13, TRUE, 1000, 5000, 6250),
      ('cabinet','day',TRUE,14,14, TRUE, 1000, 5000, 6250),
      ('cabinet','day',TRUE,15,15, TRUE, 1000, 4500, 6000),
      ('cabinet','day',TRUE,16,16, TRUE, 1000, 4500, 6000),
      ('cabinet','day',TRUE,17,17, TRUE, 1000, 4500, 6000),
      ('cabinet','day',TRUE,18,18, TRUE, 1000, 4500, 6000),
      ('cabinet','day',TRUE,19,19, TRUE, 1000, 4500, 6000),
      ('cabinet','day',TRUE,20,20, TRUE, 1000, 4500, 6000);

-- БЕЗ АБОНЕМЕНТА: ЗАЛ / ЧАС — почасово 1..9 (per_unit=TRUE), с 10 часов — фикс за сессию (per_unit=FALSE)
INSERT INTO rent_rates(place,unit,with_subscription,min_qty,max_qty,per_unit,threshold_materials,price_with_materials,price_own_materials) VALUES
                                                                                                                                               ('hall','hour',FALSE, 1, 1, TRUE, 100, 500, 650),
                                                                                                                                               ('hall','hour',FALSE, 2, 2, TRUE, 100, 495, 645),
                                                                                                                                               ('hall','hour',FALSE, 3, 3, TRUE, 100, 490, 640),
                                                                                                                                               ('hall','hour',FALSE, 4, 4, TRUE, 100, 485, 635),
                                                                                                                                               ('hall','hour',FALSE, 5, 5, TRUE, 100, 480, 630),
                                                                                                                                               ('hall','hour',FALSE, 6, 6, TRUE, 100, 475, 625),
                                                                                                                                               ('hall','hour',FALSE, 7, 7, TRUE, 100, 470, 620),
                                                                                                                                               ('hall','hour',FALSE, 8, 8, TRUE, 100, 465, 615),
                                                                                                                                               ('hall','hour',FALSE, 9, 9, TRUE, 100, 460, 610),
                                                                                                                                               ('hall','hour',FALSE,10,NULL,FALSE,1000,4550,6050);  -- ≥10 часов: дневной фикс

-- БЕЗ АБОНЕМЕНТА: КАБИНЕТ / ДЕНЬ — фикс за сессию
INSERT INTO rent_rates(place,unit,with_subscription,min_qty,max_qty,per_unit,threshold_materials,price_with_materials,price_own_materials)
VALUES ('cabinet','day',FALSE,1,NULL,FALSE,1000,5500,6500);

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

CREATE TABLE IF NOT EXISTS subscriptions (
                                             id          BIGSERIAL PRIMARY KEY,
                                             user_id     BIGINT      NOT NULL,
                                             place       TEXT        NOT NULL,   -- 'hall' | 'cabinet'
                                             unit        TEXT        NOT NULL,   -- 'hour' | 'day'
                                             month       TEXT        NOT NULL,   -- 'YYYY-MM'
                                             plan_limit  INT         NOT NULL,   -- номинальный лимит плана (30, 50, ...)
                                             total_qty   INT         NOT NULL,   -- всего куплено часов/дней по этому плану за месяц
                                             used_qty    INT         NOT NULL DEFAULT 0,
                                             threshold_materials_total NUMERIC(14,2) NOT NULL DEFAULT 0, -- общий порог (часы * порог/час)
                                             materials_sum_total       NUMERIC(14,2) NOT NULL DEFAULT 0, -- фактически набранная сумма за материалы
                                             threshold_met             BOOLEAN       NOT NULL DEFAULT FALSE, -- условие выполнено/нет
                                             created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                                             updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_subs_user_month ON subscriptions (user_id, month);


-- +goose Down
DROP TABLE IF EXISTS balances;
DROP TABLE IF EXISTS materials;
DROP TABLE IF EXISTS material_brands;
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