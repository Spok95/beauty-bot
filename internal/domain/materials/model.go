package materials

import "time"

type Unit string

const (
	UnitPcs Unit = "pcs"
	UnitG   Unit = "g"
)

type Material struct {
	ID           int64
	Name         string
	CategoryID   int64
	Unit         Unit
	Active       bool
	CreatedAt    time.Time
	PricePerUnit float64 // ₽ за g / шт
}

type Balance struct {
	WarehouseID int64
	MaterialID  int64
	Qty         float64
}
