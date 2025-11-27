package brands

import "time"

type Brand struct {
	ID         int64
	CategoryID int64
	Name       string
	Active     bool
	CreatedAt  time.Time
}
