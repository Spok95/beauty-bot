package catalog

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

/* Warehouses */

func (r *Repo) CreateWarehouse(ctx context.Context, name string, t WarehouseType) (*Warehouse, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO warehouses (name, type) VALUES ($1,$2)
		ON CONFLICT (name) DO NOTHING
		RETURNING id, name, type, active, created_at
	`, name, string(t))
	var w Warehouse
	err := row.Scan(&w.ID, &w.Name, &w.Type, &w.Active, &w.CreatedAt)
	if err == pgx.ErrNoRows {
		// Уже есть — вернём существующий
		return r.GetWarehouseByName(ctx, name)
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *Repo) GetWarehouseByName(ctx context.Context, name string) (*Warehouse, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, type, active, created_at
		FROM warehouses WHERE name = $1
	`, name)
	var w Warehouse
	if err := row.Scan(&w.ID, &w.Name, &w.Type, &w.Active, &w.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &w, nil
}

func (r *Repo) ListWarehouses(ctx context.Context) ([]Warehouse, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, type, active, created_at
		FROM warehouses
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Warehouse
	for rows.Next() {
		var w Warehouse
		if err := rows.Scan(&w.ID, &w.Name, &w.Type, &w.Active, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

/* Categories */

func (r *Repo) CreateCategory(ctx context.Context, name string) (*Category, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO material_categories (name) VALUES ($1)
		ON CONFLICT (name) DO NOTHING
		RETURNING id, name, active, created_at
	`, name)
	var c Category
	err := row.Scan(&c.ID, &c.Name, &c.Active, &c.CreatedAt)
	if err == pgx.ErrNoRows {
		// Уже существует
		return r.GetCategoryByName(ctx, name)
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) GetCategoryByName(ctx context.Context, name string) (*Category, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, active, created_at
		FROM material_categories WHERE name = $1
	`, name)
	var c Category
	if err := row.Scan(&c.ID, &c.Name, &c.Active, &c.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *Repo) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, active, created_at
		FROM material_categories
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Active, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// GetWarehouseByID Warehouses: helpers
func (r *Repo) GetWarehouseByID(ctx context.Context, id int64) (*Warehouse, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, type, active, created_at
		FROM warehouses WHERE id=$1
	`, id)
	var w Warehouse
	if err := row.Scan(&w.ID, &w.Name, &w.Type, &w.Active, &w.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &w, nil
}

func (r *Repo) UpdateWarehouseName(ctx context.Context, id int64, name string) (*Warehouse, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE warehouses SET name=$2 WHERE id=$1
		RETURNING id, name, type, active, created_at
	`, id, name)
	var w Warehouse
	if err := row.Scan(&w.ID, &w.Name, &w.Type, &w.Active, &w.CreatedAt); err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *Repo) SetWarehouseActive(ctx context.Context, id int64, active bool) (*Warehouse, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE warehouses SET active=$2 WHERE id=$1
		RETURNING id, name, type, active, created_at
	`, id, active)
	var w Warehouse
	if err := row.Scan(&w.ID, &w.Name, &w.Type, &w.Active, &w.CreatedAt); err != nil {
		return nil, err
	}
	return &w, nil
}

// GetCategoryByID Categories: helpers
func (r *Repo) GetCategoryByID(ctx context.Context, id int64) (*Category, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, active, created_at
		FROM material_categories WHERE id=$1
	`, id)
	var c Category
	if err := row.Scan(&c.ID, &c.Name, &c.Active, &c.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *Repo) UpdateCategoryName(ctx context.Context, id int64, name string) (*Category, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE material_categories SET name=$2 WHERE id=$1
		RETURNING id, name, active, created_at
	`, id, name)
	var c Category
	if err := row.Scan(&c.ID, &c.Name, &c.Active, &c.CreatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) SetCategoryActive(ctx context.Context, id int64, active bool) (*Category, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE material_categories SET active=$2 WHERE id=$1
		RETURNING id, name, active, created_at
	`, id, active)
	var c Category
	if err := row.Scan(&c.ID, &c.Name, &c.Active, &c.CreatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}
