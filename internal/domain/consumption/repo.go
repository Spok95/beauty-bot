package consumption

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

func (r *Repo) CreateSession(ctx context.Context, userID int64, place, unit string, qty int, withSub bool,
	mats, rounded, rent, total float64, payload map[string]any) (int64, error) {

	pb, _ := json.Marshal(payload)
	row := r.pool.QueryRow(ctx, `
		INSERT INTO consumption_sessions
		(user_id, place, unit, qty, with_subscription, materials_sum, rounded_materials_sum, rent, total, payload, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'draft')
		RETURNING id
	`, userID, place, unit, qty, withSub, mats, rounded, rent, total, pb)

	var id int64
	return id, row.Scan(&id)
}

func (r *Repo) AddItem(ctx context.Context, sessionID, materialID int64, qty, unitPrice, cost float64) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO consumption_items (session_id, material_id, qty, unit_price, cost)
		VALUES ($1,$2,$3,$4,$5)
	`, sessionID, materialID, qty, unitPrice, cost)
	return err
}

func (r *Repo) CreateInvoice(ctx context.Context, userID, sessionID int64, amount float64) (int64, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO invoices (user_id, session_id, amount, status)
		VALUES ($1,$2,$3,'pending')
		RETURNING id
	`, userID, sessionID, amount)
	var id int64
	return id, row.Scan(&id)
}

/* Ставки аренды (если нет в БД — вернём ok=false) */
type Rate struct {
	Threshold float64
	PriceWith float64
	PriceOwn  float64
}

func (r *Repo) GetRate(ctx context.Context, place, unit string, withSub bool) (Rate, bool, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT threshold_materials, price_with_materials, price_own_materials
		FROM rent_rates
		WHERE place=$1 AND unit=$2 AND with_subscription=$3
		  AND (active_to IS NULL OR active_to >= CURRENT_DATE)
		ORDER BY active_from DESC
		LIMIT 1
	`, place, unit, withSub)
	var rt Rate
	if err := row.Scan(&rt.Threshold, &rt.PriceWith, &rt.PriceOwn); err != nil {
		return Rate{}, false, nil // просто сообщим, что нет ставки
	}
	return rt, true, nil
}
