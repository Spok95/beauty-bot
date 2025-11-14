package subscriptions

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct{ db *pgxpool.Pool }

func NewRepo(db *pgxpool.Pool) *Repo { return &Repo{db: db} }

func (r *Repo) GetActive(ctx context.Context, userID int64, place, unit, month string) (*Subscription, error) {
	const q = `SELECT id,user_id,place,unit,month,total_qty,used_qty,created_at,updated_at
	           FROM subscriptions WHERE user_id=$1 AND place=$2 AND unit=$3 AND month=$4`
	row := r.db.QueryRow(ctx, q, userID, place, unit, month)
	var s Subscription
	if err := row.Scan(&s.ID, &s.UserID, &s.Place, &s.Unit, &s.Month, &s.TotalQty, &s.UsedQty, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repo) ListByUserMonth(ctx context.Context, userID int64, month string) ([]Subscription, error) {
	const q = `SELECT id,user_id,place,unit,month,total_qty,used_qty,created_at,updated_at
	           FROM subscriptions WHERE user_id=$1 AND month=$2 ORDER BY place,unit`
	rows, err := r.db.Query(ctx, q, userID, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.UserID, &s.Place, &s.Unit, &s.Month, &s.TotalQty, &s.UsedQty, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (r *Repo) AddUsage(ctx context.Context, id int64, qty int) error {
	const q = `UPDATE subscriptions SET used_qty = used_qty + $2, updated_at = NOW() WHERE id=$1`
	_, err := r.db.Exec(ctx, q, id, qty)
	return err
}

func (r *Repo) CreateOrSetTotal(ctx context.Context, userID int64, place, unit, month string, total int) (int64, error) {
	const q = `
		INSERT INTO subscriptions (user_id, place, unit, month, total_qty)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT ON CONSTRAINT uq_subs
		DO UPDATE SET total_qty = EXCLUDED.total_qty, updated_at = NOW()
		RETURNING id;
	`
	var id int64
	if err := r.db.QueryRow(ctx, q, userID, place, unit, month, total).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// AddOrCreateTotal увеличивает total_qty на qty (или создаёт запись за month)
func (r *Repo) AddOrCreateTotal(ctx context.Context, userID int64, place, unit, month string, qty int) (*Subscription, error) {
	const q = `
INSERT INTO subscriptions(user_id, place, unit, month, total_qty, used_qty)
VALUES($1,$2,$3,$4,$5,0)
ON CONFLICT (user_id, place, unit, month)
DO UPDATE SET total_qty = subscriptions.total_qty + EXCLUDED.total_qty,
              updated_at = NOW()
RETURNING id,user_id,place,unit,month,total_qty,used_qty,created_at,updated_at;`
	row := r.db.QueryRow(ctx, q, userID, place, unit, month, qty)
	var s Subscription
	if err := row.Scan(&s.ID, &s.UserID, &s.Place, &s.Unit, &s.Month, &s.TotalQty, &s.UsedQty, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}
