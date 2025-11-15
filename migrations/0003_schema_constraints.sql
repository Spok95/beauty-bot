-- +goose Up

-- 1. subscriptions: связать с users и добавить проверки
ALTER TABLE subscriptions
    ADD CONSTRAINT fk_subscriptions_user
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE subscriptions
    ADD CONSTRAINT chk_subscriptions_place
        CHECK (place IN ('hall','cabinet'));

ALTER TABLE subscriptions
    ADD CONSTRAINT chk_subscriptions_unit
        CHECK (unit IN ('hour','day'));

ALTER TABLE subscriptions
    ADD CONSTRAINT chk_subscriptions_qty
        CHECK (
            total_qty > 0
                AND used_qty >= 0
                AND used_qty <= total_qty
            );

-- индекс под поиск активного абонемента по пользователю/месяцу/помещению/типу
CREATE INDEX IF NOT EXISTS idx_subs_user_month_place_unit
    ON subscriptions(user_id, month, place, unit);

-- 2. movements: зафиксировать правило со знаком qty
ALTER TABLE movements
    ADD CONSTRAINT chk_movements_qty_sign
        CHECK (
            (type = 'in'  AND qty > 0) OR
            (type = 'out' AND qty < 0)
            );

-- 3. Неотрицательные цены/количества

ALTER TABLE materials
    ADD CONSTRAINT chk_materials_price_nonneg
        CHECK (price_per_unit >= 0);

ALTER TABLE balances
    ADD CONSTRAINT chk_balances_qty_nonneg
        CHECK (qty >= 0);

ALTER TABLE supplies
    ADD CONSTRAINT chk_supplies_costs_nonneg
        CHECK (unit_cost >= 0 AND total_cost >= 0);

ALTER TABLE rent_rates
    ADD CONSTRAINT chk_rent_rates_nonneg
        CHECK (
            min_qty > 0
                AND (max_qty IS NULL OR max_qty >= min_qty)
                AND threshold_materials >= 0
                AND price_with_materials >= 0
                AND price_own_materials  >= 0
            );

ALTER TABLE consumption_sessions
    ADD CONSTRAINT chk_consumption_sessions_nonneg
        CHECK (
            materials_sum >= 0
                AND rounded_materials_sum >= 0
                AND rent >= 0
                AND total >= 0
            );

ALTER TABLE consumption_items
    ADD CONSTRAINT chk_consumption_items_nonneg
        CHECK (
            unit_price >= 0
                AND cost >= 0
            );

ALTER TABLE invoices
    ADD CONSTRAINT chk_invoices_amount_nonneg
        CHECK (amount >= 0);

-- 4. invoices: один счёт на сессию + индексы

ALTER TABLE invoices
    ADD CONSTRAINT uq_invoices_session
        UNIQUE (session_id);

CREATE INDEX IF NOT EXISTS idx_invoices_user_status
    ON invoices(user_id, status);


-- +goose Down

-- 4. invoices
DROP INDEX IF EXISTS idx_invoices_user_status;
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS uq_invoices_session;
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS chk_invoices_amount_nonneg;

-- 3. неотрицательные значения
ALTER TABLE consumption_items DROP CONSTRAINT IF EXISTS chk_consumption_items_nonneg;
ALTER TABLE consumption_sessions DROP CONSTRAINT IF EXISTS chk_consumption_sessions_nonneg;
ALTER TABLE rent_rates DROP CONSTRAINT IF EXISTS chk_rent_rates_nonneg;
ALTER TABLE supplies DROP CONSTRAINT IF EXISTS chk_supplies_costs_nonneg;
ALTER TABLE balances DROP CONSTRAINT IF EXISTS chk_balances_qty_nonneg;
ALTER TABLE materials DROP CONSTRAINT IF EXISTS chk_materials_price_nonneg;

-- 2. movements
ALTER TABLE movements DROP CONSTRAINT IF EXISTS chk_movements_qty_sign;

-- 1. subscriptions
DROP INDEX IF EXISTS idx_subs_user_month_place_unit;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS chk_subscriptions_qty;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS chk_subscriptions_unit;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS chk_subscriptions_place;
ALTER TABLE subscriptions DROP CONSTRAINT IF EXISTS fk_subscriptions_user;
