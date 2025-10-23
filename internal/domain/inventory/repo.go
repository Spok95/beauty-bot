package inventory

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// delta > 0 => приход; delta < 0 => списание (может увести остаток в минус)
func (r *Repo) apply(ctx context.Context, actorID, warehouseID, materialID int64, delta float64, mtype MoveType, note string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Обновляем остаток без проверок (разрешаем отрицательные значения)
	if _, err = tx.Exec(ctx, `
		INSERT INTO balances (warehouse_id, material_id, qty)
		VALUES ($1,$2,$3)
		ON CONFLICT (warehouse_id, material_id)
		DO UPDATE SET qty = balances.qty + EXCLUDED.qty
	`, warehouseID, materialID, delta); err != nil {
		return err
	}

	// Логируем движение
	if _, err = tx.Exec(ctx, `
		INSERT INTO movements (actor_id, warehouse_id, material_id, qty, type, note)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, actorID, warehouseID, materialID, delta, string(mtype), note); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *Repo) Receive(ctx context.Context, actorID, warehouseID, materialID int64, qty float64, note string) error {
	if qty <= 0 {
		return fmt.Errorf("qty must be > 0")
	}
	return r.apply(ctx, actorID, warehouseID, materialID, qty, MoveIn, note)
}

func (r *Repo) WriteOff(ctx context.Context, actorID, warehouseID, materialID int64, qty float64, note string) error {
	if qty <= 0 {
		return fmt.Errorf("qty must be > 0")
	}
	// Списание без проверок — может увести остаток в минус
	return r.apply(ctx, actorID, warehouseID, materialID, -qty, MoveOut, note)
}
