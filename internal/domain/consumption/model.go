package consumption

import "time"

type Session struct {
	ID                  int64
	UserID              int64
	Place               string // hall|cabinet
	Unit                string // hour|day
	Qty                 int
	WithSubscription    bool
	MaterialsSum        float64
	RoundedMaterialsSum float64
	Rent                float64
	Total               float64
	Status              string
	CreatedAt           time.Time
}

type Item struct {
	ID         int64
	SessionID  int64
	MaterialID int64
	Qty        float64
	UnitPrice  float64
	Cost       float64
}

// Ступенчатая ставка
type TierRate struct {
	ID        int64
	Place     string
	Unit      string
	WithSub   bool
	MinQty    int
	MaxQty    *int // nil = без верхней границы
	Threshold float64
	PriceWith float64
	PriceOwn  float64
	Active    bool
}
