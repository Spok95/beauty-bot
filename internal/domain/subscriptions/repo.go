package subscriptions

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInsufficientLimit = errors.New("subscriptions: insufficient limit")

type Repo struct{ db *pgxpool.Pool }

func NewRepo(db *pgxpool.Pool) *Repo { return &Repo{db: db} }

func (r *Repo) GetActive(ctx context.Context, userID int64, place, unit, month string) (*Subscription, error) {
	const q = `SELECT id,user_id,place,unit,month,plan_limit,total_qty,used_qty,created_at,updated_at
	           FROM subscriptions
	           WHERE user_id=$1 AND place=$2 AND unit=$3 AND month=$4
	             AND total_qty > used_qty
	           ORDER BY created_at
	           LIMIT 1`
	row := r.db.QueryRow(ctx, q, userID, place, unit, month)
	var s Subscription
	if err := row.Scan(
		&s.ID,
		&s.UserID,
		&s.Place,
		&s.Unit,
		&s.Month,
		&s.PlanLimit,
		&s.TotalQty,
		&s.UsedQty,
		&s.CreatedAt,
		&s.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repo) ListByUserMonth(ctx context.Context, userID int64, month string) ([]Subscription, error) {
	const q = `SELECT id,user_id,place,unit,month,plan_limit,total_qty,used_qty,created_at,updated_at
	           FROM subscriptions
	           WHERE user_id=$1 AND month=$2
	           ORDER BY place,unit,plan_limit`
	rows, err := r.db.Query(ctx, q, userID, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(
			&s.ID,
			&s.UserID,
			&s.Place,
			&s.Unit,
			&s.Month,
			&s.PlanLimit,
			&s.TotalQty,
			&s.UsedQty,
			&s.CreatedAt,
			&s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (r *Repo) AddUsage(ctx context.Context, id int64, qty int) error {
	const q = `
UPDATE subscriptions
SET used_qty = used_qty + $2,
    updated_at = NOW()
WHERE id = $1
  AND used_qty + $2 <= total_qty
`
	tag, err := r.db.Exec(ctx, q, id, qty)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// либо не нашли абонемент, либо не хватает лимита
		return ErrInsufficientLimit
	}
	return nil
}

func (r *Repo) CreateOrSetTotal(ctx context.Context, userID int64, place, unit, month string, total int) (int64, error) {
	// Админский режим: считаем, что он задаёт один актуальный абонемент на месяц.
	// Удаляем все существующие записи по этому ключу и создаём одну новую.
	if _, err := r.db.Exec(ctx,
		`DELETE FROM subscriptions WHERE user_id=$1 AND place=$2 AND unit=$3 AND month=$4`,
		userID, place, unit, month,
	); err != nil {
		return 0, err
	}

	const q = `
		INSERT INTO subscriptions (user_id, place, unit, month, plan_limit, total_qty, used_qty)
		VALUES ($1, $2, $3, $4, $5, $6, 0)
		RETURNING id;
	`
	planLimit := total
	var id int64
	if err := r.db.QueryRow(ctx, q, userID, place, unit, month, planLimit, total).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// AddOrCreateTotal увеличивает total_qty на qty (или создаёт запись за month)
// plan_limit фиксируем как номинальный объём плана (qty при покупке).
func (r *Repo) AddOrCreateTotal(ctx context.Context, userID int64, place, unit, month string, qty int) (*Subscription, error) {
	const q = `
INSERT INTO subscriptions(user_id, place, unit, month, plan_limit, total_qty, used_qty)
VALUES($1,$2,$3,$4,$5,$6,0)
ON CONFLICT (user_id, place, unit, month, plan_limit)
DO UPDATE SET total_qty = subscriptions.total_qty + EXCLUDED.total_qty,
              updated_at = NOW()
RETURNING id,user_id,place,unit,month,plan_limit,total_qty,used_qty,created_at,updated_at;`

	planLimit := qty

	row := r.db.QueryRow(ctx, q, userID, place, unit, month, planLimit, qty)
	var s Subscription
	if err := row.Scan(
		&s.ID,
		&s.UserID,
		&s.Place,
		&s.Unit,
		&s.Month,
		&s.PlanLimit,
		&s.TotalQty,
		&s.UsedQty,
		&s.CreatedAt,
		&s.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &s, nil
}
