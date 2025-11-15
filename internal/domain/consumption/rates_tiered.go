package consumption

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RateTier struct {
	ID        int64
	Place     string
	Unit      string
	WithSub   bool
	MinQty    int
	MaxQty    *int
	Threshold float64
	PriceWith float64
	PriceOwn  float64
	Active    bool
}

type RateRepo struct{ db *pgxpool.Pool }

func NewRateRepo(db *pgxpool.Pool) *RateRepo { return &RateRepo{db: db} }

// GetTier возвращает ступень по qty (min_qty <= qty <= max_qty(если не NULL))
func (r *RateRepo) GetTier(ctx context.Context, place, unit string, withSub bool, qty int) (*RateTier, bool, error) {
	const q = `
        SELECT id,place,unit,with_sub,min_qty,max_qty,threshold,price_with,price_own,active
        FROM rent_rates
        WHERE place=$1 AND unit=$2 AND with_sub=$3 AND active=TRUE
          AND min_qty <= $4
          AND (max_qty IS NULL OR $4 <= max_qty)
        ORDER BY min_qty DESC
        LIMIT 1;
    `
	row := r.db.QueryRow(ctx, q, place, unit, withSub, qty)

	var t RateTier
	var mx sql.NullInt32
	if err := row.Scan(&t.ID, &t.Place, &t.Unit, &t.WithSub, &t.MinQty, &mx, &t.Threshold, &t.PriceWith, &t.PriceOwn, &t.Active); err != nil {
		if err == pgx.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}
	if mx.Valid {
		v := int(mx.Int32)
		t.MaxQty = &v
	}
	return &t, true, nil
}

// List Служебные методы для админки (покажу интерфейсы, реализации — короткие SQL):
func (r *RateRepo) List(ctx context.Context, place, unit string, withSub bool) ([]RateTier, error) {
	const q = `
        SELECT id,place,unit,with_sub,min_qty,max_qty,threshold,price_with,price_own,active
        FROM rent_rates
        WHERE place=$1 AND unit=$2 AND with_sub=$3
        ORDER BY min_qty ASC;
    `
	rows, err := r.db.Query(ctx, q, place, unit, withSub)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RateTier{}
	for rows.Next() {
		var t RateTier
		var mx sql.NullInt32 // <-- тут тоже
		if err := rows.Scan(&t.ID, &t.Place, &t.Unit, &t.WithSub, &t.MinQty, &mx, &t.Threshold, &t.PriceWith, &t.PriceOwn, &t.Active); err != nil {
			return nil, err
		}
		if mx.Valid {
			v := int(mx.Int32)
			t.MaxQty = &v
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *RateRepo) Upsert(ctx context.Context, t RateTier) (int64, error) {
	const q = `
        INSERT INTO rent_rates(place,unit,with_sub,min_qty,max_qty,threshold,price_with,price_own,active)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
        ON CONFLICT (place,unit,with_sub,min_qty)
        DO UPDATE SET max_qty=EXCLUDED.max_qty, threshold=EXCLUDED.threshold,
                      price_with=EXCLUDED.price_with, price_own=EXCLUDED.price_own,
                      active=EXCLUDED.active, updated_at=NOW()
        RETURNING id;
    `
	var id int64
	err := r.db.QueryRow(ctx, q, t.Place, t.Unit, t.WithSub, t.MinQty, t.MaxQty, t.Threshold, t.PriceWith, t.PriceOwn, t.Active).Scan(&id)
	return id, err
}

func (r *RateRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.Exec(ctx, `DELETE FROM rent_rates WHERE id=$1`, id)
	return err
}

func (t RateTier) String() string {
	rng := "∞"
	if t.MaxQty != nil {
		rng = fmt.Sprintf("%d", *t.MaxQty)
	}
	return fmt.Sprintf("[%d..%s] thr=%.0f, with=%.2f, own=%.2f, active=%v", t.MinQty, rng, t.Threshold, t.PriceWith, t.PriceOwn, t.Active)
}
