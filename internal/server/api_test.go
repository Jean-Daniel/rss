package server

import (
	"testing"
	"time"

	"fever/internal/feeds"
)

func TestIsStale(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	at := func(ago time.Duration) *time.Time { t := now.Add(-ago); return &t }
	day := 24 * time.Hour
	tests := []struct {
		name                 string
		lastFetched, lastNew *time.Time
		want                 bool
	}{
		{"never fetched", nil, nil, false},
		{"fetched, never brought anything", at(time.Hour), nil, true},
		{"new item yesterday", at(time.Hour), at(day), false},
		{"new item just inside the retention period", at(time.Hour), at(feeds.Retention - day), false},
		{"nothing new for longer than the retention period", at(time.Hour), at(feeds.Retention + day), true},
	}
	for _, tt := range tests {
		if got := isStale(tt.lastFetched, tt.lastNew, now); got != tt.want {
			t.Errorf("%s: isStale = %v, want %v", tt.name, got, tt.want)
		}
	}
}
