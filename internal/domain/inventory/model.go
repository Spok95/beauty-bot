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
