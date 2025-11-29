package inventory

import "time"

type MoveType string

const (
	MoveIn  MoveType = "in"
	MoveOut MoveType = "out"
)

type Movement struct {
	ID          int64
	CreatedAt   time.Time
	ActorID     int64
	WarehouseID int64
	MaterialID  int64
	Qty         float64
	Type        MoveType
	Note        string
}

type Supply struct {
	ID          int64
	CreatedAt   time.Time
	ActorID     int64
	WarehouseID int64
	MaterialID  int64
	Qty         float64
	UnitCost    float64
	TotalCost   float64
	Comment     string
}

type SupplyDetail struct {
	CreatedAt     time.Time
	ActorName     string
	WarehouseName string
	CategoryName  string
	BrandName     string
	MaterialName  string
	Unit          string
	Qty           float64
	Comment       string
}

type SupplyBatch struct {
	ID          int64
	CreatedAt   time.Time
	ActorID     int64
	WarehouseID int64
	Comment     string
}
