package catalog

import "time"

type WarehouseType string

const (
	WHTConsumables WarehouseType = "consumables" // склад расходников
	WHTOther       WarehouseType = "other"       // прочий склад
)

type Warehouse struct {
	ID        int64
	Name      string
	Type      WarehouseType
	Active    bool
	CreatedAt time.Time
}

type Category struct {
	ID        int64
	Name      string
	Active    bool
	CreatedAt time.Time
}

type WarehouseCategory struct {
	WarehouseID  int64
	CategoryID   int64
	CategoryName string
	Active       bool
	Linked       bool
}
