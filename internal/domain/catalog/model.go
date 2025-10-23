package catalog

import "time"

type WarehouseType string

const (
	WHTConsumables   WarehouseType = "consumables"    // склад расходников
	WHTClientService WarehouseType = "client_service" // склад клиентского обслуживания
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
