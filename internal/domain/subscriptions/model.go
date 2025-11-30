package subscriptions

import "time"

type Subscription struct {
	ID     int64
	UserID int64
	Place  string // "hall" | "cabinet"
	Unit   string // "hour" | "day"
	Month  string // "YYYY-MM"

	PlanLimit int // номинальный лимит плана (например, 30 или 50 часов)
	TotalQty  int // всего куплено часов/дней по этому плану за месяц
	UsedQty   int

	ThresholdMaterialsTotal float64 // threshold_materials_total
	MaterialsSumTotal       float64 // materials_sum_total
	ThresholdMet            bool    // threshold_met

	CreatedAt time.Time
	UpdatedAt time.Time
}
