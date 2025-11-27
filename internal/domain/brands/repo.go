package brands

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) GetByID(ctx context.Context, id int64) (*Brand, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, category_id, name, active, created_at
		FROM material_brands
		WHERE id = $1
	`, id)
	var b Brand
	if err := row.Scan(&b.ID, &b.CategoryID, &b.Name, &b.Active, &b.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

// GetOrCreate возвращает бренд по имени в категории, создавая его при отсутствии.
func (r *Repo) GetOrCreate(ctx context.Context, categoryID int64, name string) (*Brand, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "" // допускаем бренд "без имени"
	}
	// пробуем найти
	row := r.pool.QueryRow(ctx, `
		SELECT id, category_id, name, active, created_at
		FROM material_brands
		WHERE category_id = $1 AND name = $2
	`, categoryID, name)
	var b Brand
	err := row.Scan(&b.ID, &b.CategoryID, &b.Name, &b.Active, &b.CreatedAt)
	if err == nil {
		return &b, nil
	}
	if err != pgx.ErrNoRows {
		return nil, err
	}

	// создаём
	row = r.pool.QueryRow(ctx, `
		INSERT INTO material_brands (category_id, name, active)
		VALUES ($1, $2, TRUE)
		RETURNING id, category_id, name, active, created_at
	`, categoryID, name)
	if err := row.Scan(&b.ID, &b.CategoryID, &b.Name, &b.Active, &b.CreatedAt); err != nil {
		return nil, err
	}
	return &b, nil
}

// ListByCategory — все бренды категории.
func (r *Repo) ListByCategory(ctx context.Context, categoryID int64, onlyActive bool) ([]Brand, error) {
	var rows pgx.Rows
	var err error

	if onlyActive {
		rows, err = r.pool.Query(ctx, `
			SELECT id, category_id, name, active, created_at
			FROM material_brands
			WHERE category_id = $1 AND active = TRUE
			ORDER BY name
		`, categoryID)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, category_id, name, active, created_at
			FROM material_brands
			WHERE category_id = $1
			ORDER BY name
		`, categoryID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Brand
	for rows.Next() {
		var b Brand
		if err := rows.Scan(&b.ID, &b.CategoryID, &b.Name, &b.Active, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *Repo) SetActive(ctx context.Context, id int64, active bool) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE material_brands SET active = $2 WHERE id = $1
	`, id, active)
	return err
}

func (r *Repo) Rename(ctx context.Context, id int64, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE material_brands SET name = $2 WHERE id = $1
	`, id, newName)
	return err
}
