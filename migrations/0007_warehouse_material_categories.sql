-- +goose Up
CREATE TABLE IF NOT EXISTS warehouse_material_categories (
                                                             warehouse_id BIGINT NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    category_id BIGINT NOT NULL REFERENCES material_categories(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (warehouse_id, category_id)
    );

CREATE INDEX IF NOT EXISTS idx_warehouse_material_categories_warehouse_id
    ON warehouse_material_categories(warehouse_id);

CREATE INDEX IF NOT EXISTS idx_warehouse_material_categories_category_id
    ON warehouse_material_categories(category_id);

-- +goose Down
DROP TABLE IF EXISTS warehouse_material_categories;