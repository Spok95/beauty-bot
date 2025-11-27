package materials

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

/* Materials CRUD */

func (r *Repo) Create(ctx context.Context, name string, categoryID, brandID int64, unit Unit) (*Material, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO materials (name, category_id, brand_id, unit, active)
		VALUES ($1,$2,$3,$4,TRUE)
		RETURNING id, name, category_id, brand_id, unit, active, created_at, price_per_unit
	`, name, categoryID, brandID, unit)

	var m Material
	if err := row.Scan(
		&m.ID,
		&m.Name,
		&m.CategoryID,
		&m.BrandID,
		&m.Unit,
		&m.Active,
		&m.CreatedAt,
		&m.PricePerUnit,
	); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) GetByID(ctx context.Context, id int64) (*Material, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT m.id, m.name, m.category_id, m.brand_id, COALESCE(b.name,''), m.unit, m.active, m.created_at, m.price_per_unit
		FROM materials m
		LEFT JOIN material_brands b ON b.id = m.brand_id
		WHERE m.id = $1
	`, id)
	var m Material
	if err := row.Scan(
		&m.ID,
		&m.Name,
		&m.CategoryID,
		&m.BrandID,
		&m.Brand,
		&m.Unit,
		&m.Active,
		&m.CreatedAt,
		&m.PricePerUnit,
	); err != nil {
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
		RETURNING id, name, category_id, brand, unit, active, created_at
	`, id, name)
	var m Material
	if err := row.Scan(&m.ID, &m.Name, &m.CategoryID, &m.Brand, &m.Unit, &m.Active, &m.CreatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) UpdateUnit(ctx context.Context, id int64, unit Unit) (*Material, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE materials SET unit=$2 WHERE id=$1
		RETURNING id, name, category_id, brand, unit, active, created_at
	`, id, string(unit))
	var m Material
	if err := row.Scan(&m.ID, &m.Name, &m.CategoryID, &m.Brand, &m.Unit, &m.Active, &m.CreatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) SetActive(ctx context.Context, id int64, active bool) (*Material, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE materials SET active=$2 WHERE id=$1
		RETURNING id, name, category_id, brand, unit, active, created_at
	`, id, active)
	var m Material
	if err := row.Scan(&m.ID, &m.Name, &m.CategoryID, &m.Brand, &m.Unit, &m.Active, &m.CreatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) List(ctx context.Context, onlyActive bool) ([]Material, error) {
	q := `
		SELECT m.id, m.name, m.category_id, m.brand_id, COALESCE(b.name,''), m.unit, m.active, m.created_at, m.price_per_unit
		FROM materials m
		LEFT JOIN material_brands b ON b.id = m.brand_id
	`
	if onlyActive {
		q += " WHERE m.active = TRUE"
	}
	q += " ORDER BY b.name, m.name"

	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Material
	for rows.Next() {
		var m Material
		if err := rows.Scan(
			&m.ID,
			&m.Name,
			&m.CategoryID,
			&m.BrandID,
			&m.Brand,
			&m.Unit,
			&m.Active,
			&m.CreatedAt,
			&m.PricePerUnit,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type MatWithBal struct {
	ID         int64
	Name       string
	Brand      string
	Unit       Unit
	Balance    int64
	CategoryID int64
}

func (r *Repo) ListWithBalanceByWarehouse(ctx context.Context, warehouseID int64) ([]MatWithBal, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT m.id, m.name, COALESCE(b.name,''), m.unit, COALESCE(bal.qty,0), m.category_id
		FROM materials m
		LEFT JOIN material_brands b ON b.id = m.brand_id
		LEFT JOIN balances bal ON ...
		ORDER BY m.name
	`, warehouseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MatWithBal
	for rows.Next() {
		var it MatWithBal
		if err := rows.Scan(&it.ID, &it.Name, &it.Brand, &it.Unit, &it.Balance, &it.CategoryID); err != nil {
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

func (r *Repo) UpdatePrice(ctx context.Context, id int64, price float64) (*Material, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE materials SET price_per_unit=$2
		WHERE id=$1
		RETURNING id, name, category_id, brand, unit, active, created_at, price_per_unit
	`, id, price)
	var m Material
	if err := row.Scan(&m.ID, &m.Name, &m.CategoryID, &m.Brand, &m.Unit, &m.Active, &m.CreatedAt, &m.PricePerUnit); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) GetPrice(ctx context.Context, id int64) (float64, error) {
	row := r.pool.QueryRow(ctx, `SELECT price_per_unit FROM materials WHERE id=$1`, id)
	var p float64
	if err := row.Scan(&p); err != nil {
		return 0, err
	}
	return p, nil
}

// SearchByName ищет материалы по части названия/бренда, без учёта регистра.
func (r *Repo) SearchByName(ctx context.Context, q string, onlyActive bool) ([]Material, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	like := "%" + strings.ToLower(q) + "%"

	base := `
		SELECT m.id, m.name, m.category_id, m.brand_id, COALESCE(b.name,''), m.unit, m.active, m.created_at, m.price_per_unit
		FROM materials m
		LEFT JOIN material_brands b ON b.id = m.brand_id
		WHERE LOWER(m.name) LIKE $1 OR LOWER(b.name) LIKE $1
	`

	var rows pgx.Rows
	var err error
	if onlyActive {
		rows, err = r.pool.Query(ctx, base+` AND m.active = TRUE ORDER BY b.name, m.name`, like)
	} else {
		rows, err = r.pool.Query(ctx, base+` ORDER BY b.name, m.name`, like)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Material
	for rows.Next() {
		var m Material
		if err := rows.Scan(
			&m.ID, &m.Name, &m.CategoryID, &m.BrandID, &m.Brand,
			&m.Unit, &m.Active, &m.CreatedAt, &m.PricePerUnit,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListBrandsByCategory возвращает уникальные бренды по категории материалов.
func (r *Repo) ListBrandsByCategory(ctx context.Context, categoryID int64) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT brand
		FROM materials
		WHERE category_id = $1
		ORDER BY brand
	`, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var brands []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		if b != "" {
			brands = append(brands, b)
		}
	}
	return brands, nil
}

// ListByCategoryAndBrand возвращает материалы по категории и бренду.
func (r *Repo) ListByCategoryAndBrand(ctx context.Context, categoryID int64, brand string) ([]Material, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, category_id, brand, unit, active, created_at, price_per_unit
		FROM materials
		WHERE category_id = $1 AND brand = $2
		ORDER BY name
	`, categoryID, brand)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Material
	for rows.Next() {
		var m Material
		if err := rows.Scan(
			&m.ID,
			&m.Name,
			&m.CategoryID,
			&m.Brand,
			&m.Unit,
			&m.Active,
			&m.CreatedAt,
			&m.PricePerUnit,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (r *Repo) ListByBrand(ctx context.Context, brandID int64) ([]Material, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT m.id, m.name, m.category_id, m.brand_id, COALESCE(b.name,''), m.unit, m.active, m.created_at, m.price_per_unit
		FROM materials m
		LEFT JOIN material_brands b ON b.id = m.brand_id
		WHERE m.brand_id = $1
		ORDER BY m.name
	`, brandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Material
	for rows.Next() {
		var m Material
		if err := rows.Scan(
			&m.ID, &m.Name, &m.CategoryID, &m.BrandID, &m.Brand,
			&m.Unit, &m.Active, &m.CreatedAt, &m.PricePerUnit,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
