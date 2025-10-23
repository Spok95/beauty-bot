package materials

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

/* Materials CRUD */

func (r *Repo) Create(ctx context.Context, name string, categoryID int64, unit Unit) (*Material, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO materials (name, category_id, unit)
		VALUES ($1,$2,$3)
		ON CONFLICT (name) DO NOTHING
		RETURNING id, name, category_id, unit, active, created_at
	`, name, categoryID, string(unit))
	var m Material
	err := row.Scan(&m.ID, &m.Name, &m.CategoryID, &m.Unit, &m.Active, &m.CreatedAt)
	if err == pgx.ErrNoRows {
		// уже есть — вернём
		return r.GetByName(ctx, name)
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) GetByID(ctx context.Context, id int64) (*Material, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, category_id, unit, active, created_at
		FROM materials WHERE id=$1
	`, id)
	var m Material
	if err := row.Scan(&m.ID, &m.Name, &m.CategoryID, &m.Unit, &m.Active, &m.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func (r *Repo) GetByName(ctx context.Context, name string) (*Material, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, category_id, unit, active, created_at
		FROM materials WHERE name=$1
	`, name)
	var m Material
	if err := row.Scan(&m.ID, &m.Name, &m.CategoryID, &m.Unit, &m.Active, &m.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func (r *Repo) UpdateName(ctx context.Context, id int64, name string) (*Material, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE materials SET name=$2 WHERE id=$1
		RETURNING id, name, category_id, unit, active, created_at
	`, id, name)
	var m Material
	if err := row.Scan(&m.ID, &m.Name, &m.CategoryID, &m.Unit, &m.Active, &m.CreatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) UpdateUnit(ctx context.Context, id int64, unit Unit) (*Material, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE materials SET unit=$2 WHERE id=$1
		RETURNING id, name, category_id, unit, active, created_at
	`, id, string(unit))
	var m Material
	if err := row.Scan(&m.ID, &m.Name, &m.CategoryID, &m.Unit, &m.Active, &m.CreatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) SetActive(ctx context.Context, id int64, active bool) (*Material, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE materials SET active=$2 WHERE id=$1
		RETURNING id, name, category_id, unit, active, created_at
	`, id, active)
	var m Material
	if err := row.Scan(&m.ID, &m.Name, &m.CategoryID, &m.Unit, &m.Active, &m.CreatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) List(ctx context.Context, onlyActive bool) ([]Material, error) {
	query := `
		SELECT id, name, category_id, unit, active, created_at
		FROM materials
	`
	if onlyActive {
		query += " WHERE active = true"
	}
	query += " ORDER BY name"
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Material
	for rows.Next() {
		var m Material
		if err := rows.Scan(&m.ID, &m.Name, &m.CategoryID, &m.Unit, &m.Active, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

/* Balances (read-only for now) */

func (r *Repo) ListBalancesByWarehouse(ctx context.Context, warehouseID int64) ([]Balance, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT warehouse_id, material_id, qty
		FROM balances
		WHERE warehouse_id = $1
		ORDER BY material_id
	`, warehouseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Balance
	for rows.Next() {
		var b Balance
		if err := rows.Scan(&b.WarehouseID, &b.MaterialID, &b.Qty); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

type MatWithBal struct {
	ID         int64
	Name       string
	Unit       Unit
	Balance    int64
	CategoryID int64
}

func (r *Repo) ListWithBalanceByWarehouse(ctx context.Context, warehouseID int64) ([]MatWithBal, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT m.id, m.name, m.unit, COALESCE(b.qty,0), m.category_id
		FROM materials m
		LEFT JOIN balances b ON b.material_id = m.id AND b.warehouse_id = $1
		ORDER BY m.name
	`, warehouseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MatWithBal
	for rows.Next() {
		var it MatWithBal
		if err := rows.Scan(&it.ID, &it.Name, &it.Unit, &it.Balance, &it.CategoryID); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, nil
}

// GetBalance Возврат остатка по складу/материалу (может быть отрицательным)
func (r *Repo) GetBalance(ctx context.Context, warehouseID, materialID int64) (float64, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT COALESCE((
			SELECT qty FROM balances WHERE warehouse_id=$1 AND material_id=$2
		), 0)
	`, warehouseID, materialID)
	var q float64
	if err := row.Scan(&q); err != nil {
		return 0, err
	}
	return q, nil
}
