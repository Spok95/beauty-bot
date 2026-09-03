package bot

import (
	"testing"
	"time"
)

func TestNextTestPeriodNotice(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.September, 4, 7, 0, 0, 0, loc)
	end := time.Date(2026, time.September, 10, 7, 0, 0, 0, loc)

	tests := []struct {
		name string
		now  time.Time
		want time.Time
		ok   bool
	}{
		{"before window", time.Date(2026, time.September, 3, 20, 0, 0, 0, loc), start, true},
		{"before seven", time.Date(2026, time.September, 5, 6, 30, 0, 0, loc), time.Date(2026, time.September, 5, 7, 0, 0, 0, loc), true},
		{"after seven", time.Date(2026, time.September, 5, 8, 0, 0, 0, loc), time.Date(2026, time.September, 6, 7, 0, 0, 0, loc), true},
		{"after last send", time.Date(2026, time.September, 10, 7, 1, 0, 0, loc), time.Time{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := nextTestPeriodNotice(tt.now, start, end)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && !got.Equal(tt.want) {
				t.Fatalf("next = %v, want %v", got, tt.want)
			}
		})
	}
}
