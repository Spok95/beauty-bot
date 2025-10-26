package subscriptions

import "time"

type Subscription struct {
	ID        int64
	UserID    int64
	Place     string // "hall" | "cabinet"
	Unit      string // "hour" | "day"
	Month     string // "YYYY-MM"
	TotalQty  int
	UsedQty   int
	CreatedAt time.Time
	UpdatedAt time.Time
}
