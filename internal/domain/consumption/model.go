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
	MaxQty    *int
	Threshold float64
	PriceWith float64
	PriceOwn  float64
	Active    bool
}

type RentRate struct {
	ID         int64
	Place      string
	Unit       string
	WithSub    bool
	MinQty     int
	MaxQty     *int
	PerUnit    bool
	Threshold  float64
	PriceWith  float64
	PriceOwn   float64
	ActiveFrom time.Time
	ActiveTo   *time.Time
}

type RentSplitPartInput struct {
	WithSub            bool // true — часть по абонементу, false — без абонемента
	Qty                int  // сколько часов/дней в этой части
	SubLimitForPricing int  // лимит плана, по которому выбираем тариф (для withSub), иначе можно передавать Qty
}

type RentSplitPartResult struct {
	WithSub            bool
	Qty                int
	SubLimitForPricing int

	Rent          float64   // сумма аренды по этой части
	Tariff        string    // текст: "по ставке с материалами" / "по ставке со своими материалами"
	Need          float64   // порог материалов для этой части
	MaterialsUsed float64   // сколько из общей суммы материалов "ушло" на выполнение порога для этой части
	ThresholdMet  bool      // выполнен ли порог
	Rate          *RentRate // сам тариф
}

type MasterMaterialsReportRow struct {
	UserID    int64
	Username  string
	SessionID int64

	CreatedAt time.Time
	Place     string // hall|cabinet
	Unit      string // hour|day
	Qty       int    // количество часов/дней в сессии
	Comment   string // комментарий из инвойса (дата/примечание сессии)

	BrandName    string // название бренда материала
	MaterialName string
	MaterialUnit string
	MaterialQty  float64
	UnitPrice    float64
	Cost         float64
}
