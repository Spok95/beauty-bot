package consumption

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
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

// GetTier подбирает ставку по qty (ступени) с учётом withSub
func (r *Repo) GetTier(ctx context.Context, place, unit string, withSub bool, qty int) (TierRate, bool, error) {
	const q = `
SELECT id, place, unit, with_sub, min_qty, max_qty, threshold, price_with, price_own, active
FROM rent_rates
WHERE place=$1 AND unit=$2 AND with_sub=$3 AND active = TRUE
  AND min_qty <= $4
  AND (max_qty IS NULL OR max_qty >= $4)
ORDER BY min_qty DESC
LIMIT 1`
	var tr TierRate
	var maxSQL sql.NullInt32
	err := r.pool.QueryRow(ctx, q, place, unit, withSub, qty).
		Scan(&tr.ID, &tr.Place, &tr.Unit, &tr.WithSub, &tr.MinQty, &maxSQL, &tr.Threshold, &tr.PriceWith, &tr.PriceOwn, &tr.Active)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TierRate{}, false, nil
		}
		return TierRate{}, false, err
	}
	if maxSQL.Valid {
		m := int(maxSQL.Int32)
		tr.MaxQty = &m
	}
	return tr, true, nil
}

// ListRates List ступеней для place/unit/withSub
func (r *Repo) ListRates(ctx context.Context, place, unit string, withSub bool) ([]TierRate, error) {
	const q = `
SELECT id, place, unit, with_sub, min_qty, max_qty, threshold, price_with, price_own, active
FROM rent_rates
WHERE place=$1 AND unit=$2 AND with_sub=$3
ORDER BY active DESC, min_qty ASC`
	rows, err := r.pool.Query(ctx, q, place, unit, withSub)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TierRate
	for rows.Next() {
		var tr TierRate
		var maxSQL sql.NullInt32
		if err := rows.Scan(&tr.ID, &tr.Place, &tr.Unit, &tr.WithSub, &tr.MinQty, &maxSQL, &tr.Threshold, &tr.PriceWith, &tr.PriceOwn, &tr.Active); err != nil {
			return nil, err
		}
		if maxSQL.Valid {
			m := int(maxSQL.Int32)
			tr.MaxQty = &m
		}
		out = append(out, tr)
	}
	return out, nil
}

// CreateRate — создать новую ступень
func (r *Repo) CreateRate(ctx context.Context, place, unit string, withSub bool, minQty int, maxQty *int, threshold, priceWith, priceOwn float64) (int64, error) {
	const q = `
INSERT INTO rent_rates(place, unit, with_sub, min_qty, max_qty, threshold, price_with, price_own, active)
VALUES($1,$2,$3,$4,$5,$6,$7,$8, TRUE)
RETURNING id`
	var id int64
	var maxAny any
	if maxQty == nil {
		maxAny = nil
	} else {
		maxAny = *maxQty
	}
	err := r.pool.QueryRow(ctx, q, place, unit, withSub, minQty, maxAny, threshold, priceWith, priceOwn).Scan(&id)
	return id, err
}
