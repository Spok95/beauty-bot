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

// GetTier подбирает ступень по qty (для админки, старый интерфейс).
// Работает с новой схемой rent_rates (with_subscription, threshold_materials и т.п.).
func (r *Repo) GetTier(ctx context.Context, place, unit string, withSub bool, qty int) (TierRate, bool, error) {
	const q = `
SELECT id,
       place,
       unit,
       with_subscription,
       min_qty,
       max_qty,
       threshold_materials,
       price_with_materials,
       price_own_materials,
       (active_to IS NULL OR active_to >= CURRENT_DATE) AS active
FROM rent_rates
WHERE place=$1
  AND unit=$2
  AND with_subscription=$3
  AND min_qty <= $4
  AND (max_qty IS NULL OR max_qty >= $4)
ORDER BY min_qty DESC
LIMIT 1`
	var tr TierRate
	var maxSQL sql.NullInt32

	err := r.pool.QueryRow(ctx, q, place, unit, withSub, qty).Scan(
		&tr.ID,
		&tr.Place,
		&tr.Unit,
		&tr.WithSub,
		&tr.MinQty,
		&maxSQL,
		&tr.Threshold,
		&tr.PriceWith,
		&tr.PriceOwn,
		&tr.Active,
	)
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

// ListRates — список ступеней для place/unit/withSub (для экрана "Установка тарифов").
func (r *Repo) ListRates(ctx context.Context, place, unit string, withSub bool) ([]TierRate, error) {
	const q = `
SELECT id,
       place,
       unit,
       with_subscription,
       min_qty,
       max_qty,
       threshold_materials,
       price_with_materials,
       price_own_materials,
       (active_to IS NULL OR active_to >= CURRENT_DATE) AS active
FROM rent_rates
WHERE place=$1
  AND unit=$2
  AND with_subscription=$3
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
		if err := rows.Scan(
			&tr.ID,
			&tr.Place,
			&tr.Unit,
			&tr.WithSub,
			&tr.MinQty,
			&maxSQL,
			&tr.Threshold,
			&tr.PriceWith,
			&tr.PriceOwn,
			&tr.Active,
		); err != nil {
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

// CreateRate — создать новую ступень тарифа (для экрана "Установка тарифов").
// per_unit для таких ступеней считаем всегда TRUE (порог "на единицу").
func (r *Repo) CreateRate(ctx context.Context, place, unit string, withSub bool, minQty int, maxQty *int, threshold, priceWith, priceOwn float64) (int64, error) {
	const q = `
INSERT INTO rent_rates(
    place,
    unit,
    with_subscription,
    min_qty,
    max_qty,
    per_unit,
    threshold_materials,
    price_with_materials,
    price_own_materials,
    active_from,
    active_to
) VALUES ($1,$2,$3,$4,$5,TRUE,$6,$7,$8,CURRENT_DATE,NULL)
RETURNING id`
	var id int64
	var maxAny any
	if maxQty == nil {
		maxAny = nil
	} else {
		maxAny = *maxQty
	}
	err := r.pool.QueryRow(ctx, q,
		place,
		unit,
		withSub,
		minQty,
		maxAny,
		threshold,
		priceWith,
		priceOwn,
	).Scan(&id)
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

// ComputeRentSplit считает аренду по нескольким частям сессии.
// parts идут в порядке использования материалов: сначала старый абонемент, потом новый, потом без абонемента.
// Общая сумма материалов matsSum (до округления) одна на всю сессию:
//   - сначала на первую часть "резервируем" её порог Need1;
//   - из остатка проверяем порог второй части и т.д.
func (r *Repo) ComputeRentSplit(
	ctx context.Context,
	place, unit string,
	matsSum float64,
	parts []RentSplitPartInput,
) (totalRent float64, rounded float64, totalNeed float64, results []RentSplitPartResult, err error) {
	if len(parts) == 0 {
		return 0, 0, 0, nil, nil
	}

	rounded = roundTo10(matsSum)
	remaining := rounded

	results = make([]RentSplitPartResult, 0, len(parts))

	for _, in := range parts {
		if in.Qty <= 0 {
			continue
		}

		qtyForTier := in.Qty
		if in.WithSub {
			qtyForTier = in.SubLimitForPricing
		}

		rate, errPick := r.pickTier(ctx, place, unit, in.WithSub, qtyForTier)
		if errPick != nil || rate == nil {
			return 0, 0, 0, nil, fmt.Errorf(
				"нет активного тарифа для place=%s unit=%s withSub=%v qty=%d",
				place, unit, in.WithSub, qtyForTier,
			)
		}

		var need float64
		if rate.PerUnit {
			need = rate.Threshold * float64(in.Qty)
		} else {
			need = rate.Threshold
		}
		totalNeed += need

		// Проверка порога для этой части
		thrMet := remaining >= need

		// Сколько материалов считаем "пошло" на эту часть
		var used float64
		if remaining > 0 {
			if remaining >= need {
				used = need
				remaining -= need
			} else {
				used = remaining
				remaining = 0
			}
		}

		var price float64
		var tariff string
		if thrMet {
			price = rate.PriceWith
			tariff = "по ставке с материалами"
		} else {
			price = rate.PriceOwn
			tariff = "по ставке со своими материалами"
		}

		var rent float64
		if rate.PerUnit {
			rent = float64(in.Qty) * price
		} else {
			rent = price
		}

		totalRent += rent

		results = append(results, RentSplitPartResult{
			WithSub:            in.WithSub,
			Qty:                in.Qty,
			SubLimitForPricing: in.SubLimitForPricing,
			Rent:               rent,
			Tariff:             tariff,
			Need:               need,
			MaterialsUsed:      used,
			ThresholdMet:       thrMet,
			Rate:               rate,
		})
	}

	return totalRent, rounded, totalNeed, results, nil
}

// ComputeRent — совместимая обёртка над ComputeRentSplit для одной части сессии.
func (r *Repo) ComputeRent(
	ctx context.Context,
	place, unit string,
	withSub bool,
	sessionQty int,
	matsSum float64,
	subLimitForPricing int,
) (rent float64, tariff string, rounded float64, need float64, rate *RentRate, err error) {
	if sessionQty <= 0 {
		return 0, "", 0, 0, nil, fmt.Errorf("sessionQty must be > 0")
	}

	part := RentSplitPartInput{
		WithSub: withSub,
		Qty:     sessionQty,
	}
	if withSub {
		part.SubLimitForPricing = subLimitForPricing
	} else {
		// для расчёта без абонемента тариф выбирается по объёму самой части
		part.SubLimitForPricing = sessionQty
	}

	totalRent, rounded, totalNeed, parts, err := r.ComputeRentSplit(ctx, place, unit, matsSum, []RentSplitPartInput{part})
	if err != nil {
		return 0, "", 0, 0, nil, err
	}
	if len(parts) == 0 || parts[0].Rate == nil {
		return 0, "", rounded, totalNeed, nil, fmt.Errorf("нет результатов расчёта аренды")
	}

	p := parts[0]
	return totalRent, p.Tariff, rounded, totalNeed, p.Rate, nil
}

// ListRentRates возвращает все тарифы аренды из rent_rates.
func (r *Repo) ListRentRates(ctx context.Context) ([]RentRate, error) {
	const q = `
SELECT
    id,
    place,
    unit,
    with_subscription,
    min_qty,
    threshold_materials,
    price_with_materials,
    price_own_materials
FROM rent_rates
ORDER BY id;
`

	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []RentRate
	for rows.Next() {
		var rr RentRate
		if err := rows.Scan(
			&rr.ID,
			&rr.Place,
			&rr.Unit,
			&rr.WithSub,
			&rr.MinQty,
			&rr.Threshold,
			&rr.PriceWith,
			&rr.PriceOwn,
		); err != nil {
			return nil, err
		}
		res = append(res, rr)
	}
	return res, rows.Err()
}

// UpdateRentRatePartial обновляет threshold/price_with/price_own для тарифа.
// Если какое-то значение равно nil, оно не трогается.
func (r *Repo) UpdateRentRatePartial(
	ctx context.Context,
	id int64,
	threshold, priceWith, priceOwn *float64,
) error {
	const q = `
UPDATE rent_rates
SET
    threshold_materials   = COALESCE($2, threshold_materials),
    price_with_materials  = COALESCE($3, price_with_materials),
    price_own_materials   = COALESCE($4, price_own_materials)
WHERE id = $1;
`

	_, err := r.pool.Exec(ctx, q, id, threshold, priceWith, priceOwn)
	return err
}

func roundTo10(x float64) float64 {
	// до ближайшего десятка
	return float64(int((x+5)/10) * 10)
}
