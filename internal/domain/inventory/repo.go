package inventory

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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

func (r *Repo) ReceiveWithCost(
	ctx context.Context,
	actorID, warehouseID, materialID int64,
	qty float64, unitCost float64,
	note string, comment string,
	batchID int64,
) error {
	if qty <= 0 {
		return fmt.Errorf("qty must be > 0")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// balances
	_, err = tx.Exec(ctx, `
		INSERT INTO balances (warehouse_id, material_id, qty)
		VALUES ($1,$2,$3)
		ON CONFLICT (warehouse_id, material_id)
		DO UPDATE SET qty = balances.qty + EXCLUDED.qty
	`, warehouseID, materialID, qty)
	if err != nil {
		return err
	}

	// movements
	_, err = tx.Exec(ctx, `
		INSERT INTO movements (actor_id, warehouse_id, material_id, qty, type, note)
		VALUES ($1,$2,$3,$4,'in',$5)
	`, actorID, warehouseID, materialID, qty, note)
	if err != nil {
		return err
	}

	// supplies (стоимость поставки)
	total := unitCost * qty
	var batchVal any
	if batchID > 0 {
		batchVal = batchID
	} else {
		batchVal = nil
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO supplies (added_by, warehouse_id, material_id, qty, unit_cost, total_cost, comment, batch_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, actorID, warehouseID, materialID, qty, unitCost, total, comment, batchVal)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *Repo) Consume(ctx context.Context, actorID, warehouseID, materialID int64, qty float64, note string) error {
	if qty <= 0 {
		return fmt.Errorf("qty must be > 0")
	}
	// отрицательный дельта = списание; MoveOut должен быть объявлен в этом файле
	return r.apply(ctx, actorID, warehouseID, materialID, -qty, MoveOut, note)
}

// GetBalance возвращает текущий остаток по складу/материалу (0, nil если записи нет).
func (r *Repo) GetBalance(ctx context.Context, warehouseID, materialID int64) (float64, error) {
	var qty float64
	err := r.pool.
		QueryRow(ctx, `
			SELECT qty
			FROM balances
			WHERE warehouse_id = $1 AND material_id = $2
		`, warehouseID, materialID).
		Scan(&qty)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return qty, err
}

// ListSuppliesByPeriod возвращает список «шапок» поставок (batch) за период [from, to).
func (r *Repo) ListSuppliesByPeriod(ctx context.Context, from, to time.Time) ([]SupplyBatch, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, created_at, added_by, warehouse_id, comment
		FROM supply_batches
		WHERE created_at >= $1 AND created_at < $2
		ORDER BY created_at DESC, id DESC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SupplyBatch
	for rows.Next() {
		var s SupplyBatch
		if err := rows.Scan(
			&s.ID,
			&s.CreatedAt,
			&s.ActorID,
			&s.WarehouseID,
			&s.Comment,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSupplyDetails возвращает строки одной поставки (по batch_id) с джойнами.
func (r *Repo) GetSupplyDetails(ctx context.Context, batchID int64) ([]SupplyDetail, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			s.created_at,
			COALESCE(u.username, '') AS actor_name,
			w.name AS warehouse_name,
			c.name AS category_name,
			b.name AS brand_name,
			m.name AS material_name,
			m.unit,
			s.qty,
			s.comment
		FROM supplies s
		JOIN warehouses w ON w.id = s.warehouse_id
		JOIN materials m ON m.id = s.material_id
		JOIN material_categories c ON c.id = m.category_id
		JOIN material_brands b ON b.id = m.brand_id
		LEFT JOIN users u ON u.id = s.added_by
		WHERE s.batch_id = $1
		ORDER BY s.created_at, m.name;
	`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SupplyDetail
	for rows.Next() {
		var d SupplyDetail
		if err := rows.Scan(
			&d.CreatedAt,
			&d.ActorName,
			&d.WarehouseName,
			&d.CategoryName,
			&d.BrandName,
			&d.MaterialName,
			&d.Unit,
			&d.Qty,
			&d.Comment,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repo) CreateSupplyBatch(
	ctx context.Context,
	actorID, warehouseID int64,
	comment string,
) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO supply_batches (added_by, warehouse_id, comment)
		VALUES ($1,$2,$3)
		RETURNING id
	`, actorID, warehouseID, comment).Scan(&id)
	return id, err
}
