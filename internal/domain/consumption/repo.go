package consumption

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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

func (r *Repo) pickTier(ctx context.Context, place, unit string, withSub bool, qty int) (*RentRate, error) {
	const q = `
SELECT id, place, unit, with_subscription, min_qty, max_qty, per_unit,
       threshold_materials, price_with_materials, price_own_materials,
       active_from, active_to
FROM rent_rates
WHERE place=$1 AND unit=$2 AND with_subscription=$3
  AND (active_to IS NULL OR active_to >= CURRENT_DATE)
  AND min_qty <= $4
  AND (max_qty IS NULL OR $4 <= max_qty)
ORDER BY min_qty DESC
LIMIT 1`
	var rr RentRate
	var maxQty *int
	var activeTo *time.Time
	err := r.pool.QueryRow(ctx, q, place, unit, withSub, qty).Scan(
		&rr.ID, &rr.Place, &rr.Unit, &rr.WithSub, &rr.MinQty, &maxQty, &rr.PerUnit,
		&rr.Threshold, &rr.PriceWith, &rr.PriceOwn, &rr.ActiveFrom, &activeTo,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	rr.MaxQty = maxQty
	rr.ActiveTo = activeTo
	return &rr, nil
}

// ComputeRent применяет правила:
//   - округление до десятков — только для проверки порога
//   - per_unit=true → rent = qty * price; need = rr.Threshold * qty
//   - per_unit=false → rent = price;     need = rr.Threshold (фикс)
func (r *Repo) ComputeRent(ctx context.Context, place, unit string, withSub bool, sessionQty int, matsSum float64, subLimitForPricing int) (rent float64, tariff string, rounded float64, need float64, rate *RentRate, err error) {
	// qtyForTier: по абонементу выбираем ступень по лимиту абонемента, без абонемента — по количеству в сессии
	qtyForTier := sessionQty
	if withSub {
		qtyForTier = subLimitForPricing
	}
	rate, err = r.pickTier(ctx, place, unit, withSub, qtyForTier)
	if err != nil || rate == nil {
		return 0, "", 0, 0, nil, fmt.Errorf("нет активного тарифа для place=%s unit=%s withSub=%v qty=%d", place, unit, withSub, qtyForTier)
	}

	rounded = roundTo10(matsSum)
	if rate.PerUnit {
		need = rate.Threshold * float64(sessionQty)
	} else {
		need = rate.Threshold
	}

	var price float64
	if rounded >= need {
		price = rate.PriceWith
		tariff = "по ставке с материалами"
	} else {
		price = rate.PriceOwn
		tariff = "по ставке со своими материалами"
	}

	if rate.PerUnit {
		rent = float64(sessionQty) * price
	} else {
		rent = price
	}
	return rent, tariff, rounded, need, rate, nil
}

func roundTo10(x float64) float64 {
	// до ближайшего десятка
	return float64(int((x+5)/10) * 10)
}
