package users

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) GetByTelegramID(ctx context.Context, tgID int64) (*User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, telegram_id, COALESCE(username, ''), role, status, created_at, updated_at
		FROM users WHERE telegram_id = $1
	`, tgID)
	var u User
	if err := row.Scan(&u.ID, &u.TelegramID, &u.Username, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (r *Repo) UpsertByTelegram(ctx context.Context, tgID int64, defaultRole Role) (*User, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO users (telegram_id, role)
		VALUES ($1,$2)
		ON CONFLICT (telegram_id) DO UPDATE SET
			role       = CASE WHEN users.role = 'admin' THEN users.role ELSE EXCLUDED.role END,
			updated_at = now()
		RETURNING id, telegram_id, COALESCE(username, ''), role, status, created_at, updated_at
	`, tgID, defaultRole)
	var u User
	if err := row.Scan(&u.ID, &u.TelegramID, &u.Username, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *Repo) SetFIO(ctx context.Context, tgID int64, fio string) (*User, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE users
		SET username = $2, updated_at = now()
		WHERE telegram_id = $1
		RETURNING id, telegram_id, COALESCE(username, ''), role, status, created_at, updated_at
	`, tgID, fio)
	var u User
	if err := row.Scan(&u.ID, &u.TelegramID, &u.Username, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *Repo) Approve(ctx context.Context, tgID int64, role Role) (*User, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE users
		SET role = $2, status = 'approved', updated_at = now()
		WHERE telegram_id = $1
		RETURNING id, telegram_id, COALESCE(username, ''), role, status, created_at, updated_at
	`, tgID, role)
	var u User
	if err := row.Scan(&u.ID, &u.TelegramID, &u.Username, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *Repo) Reject(ctx context.Context, tgID int64) (*User, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE users
		SET status = 'rejected', updated_at = now()
		WHERE telegram_id = $1
		RETURNING id, telegram_id, COALESCE(username, ''), role, status, created_at, updated_at
	`, tgID)
	var u User
	if err := row.Scan(&u.ID, &u.TelegramID, &u.Username, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

// ListByRole возвращает всех пользователей с заданной ролью и статусом.
func (r *Repo) ListByRole(ctx context.Context, role Role, status Status) ([]*User, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, telegram_id, username, role, status, created_at, updated_at
		FROM users
		WHERE role = $1 AND status = $2
	`, role, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*User
	for rows.Next() {
		var u User
		if err := rows.Scan(
			&u.ID, &u.TelegramID, &u.Username, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, &u)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return out, nil
}

func (r *Repo) GetByID(ctx context.Context, id int64) (*User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, telegram_id, COALESCE(username, ''), role, status, created_at, updated_at
		FROM users
		WHERE id = $1
	`, id)
	var u User
	if err := row.Scan(&u.ID, &u.TelegramID, &u.Username, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func (r *Repo) ListApprovedTelegramIDs(ctx context.Context) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT telegram_id
		FROM users
		WHERE status = 'approved'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		res = append(res, id)
	}
	return res, rows.Err()
}
