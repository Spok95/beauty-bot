package users

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) GetByTelegramID(ctx context.Context, tgID int64) (*User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, telegram_id, username, first_name, last_name, role, created_at, updated_at
		FROM users WHERE telegram_id = $1
	`, tgID)

	var u User
	if err := row.Scan(&u.ID, &u.TelegramID, &u.Username, &u.FirstName, &u.LastName, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// UpsertFromTelegram Upsert по Telegram-профилю. Если пользователь уже admin — не понижаем роль.
func (r *Repo) UpsertFromTelegram(ctx context.Context, tg Telegram, role Role) (*User, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO users (telegram_id, username, first_name, last_name, role)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (telegram_id)
		DO UPDATE SET
			username   = EXCLUDED.username,
			first_name = EXCLUDED.first_name,
			last_name  = EXCLUDED.last_name,
			role       = CASE WHEN users.role = 'admin' THEN users.role ELSE EXCLUDED.role END,
			updated_at = now()
		RETURNING id, telegram_id, username, first_name, last_name, role, created_at, updated_at
	`, tg.ID, tg.Username, tg.FirstName, tg.LastName, role)

	var u User
	if err := row.Scan(&u.ID, &u.TelegramID, &u.Username, &u.FirstName, &u.LastName, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}
