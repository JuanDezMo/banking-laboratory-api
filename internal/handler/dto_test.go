package handler

import (
	"testing"
	"time"
)

func TestFormatTime(t *testing.T) {
	bogota := time.FixedZone("-05:00", -5*60*60)
	tests := []struct {
		name  string
		input time.Time
		want  string
	}{
		{"other zone goes to UTC", time.Date(2026, 11, 9, 9, 30, 0, 123_000_000, bogota), "2026-11-09T14:30:00.123Z"},
		{"zero milliseconds keep 3 decimals", time.Date(2026, 11, 9, 14, 30, 0, 0, time.UTC), "2026-11-09T14:30:00.000Z"},
		{"microseconds are truncated", time.Date(2026, 11, 9, 14, 30, 0, 123_999_999, time.UTC), "2026-11-09T14:30:00.123Z"},
		{"day changes when converting", time.Date(2026, 11, 9, 22, 0, 0, 0, bogota), "2026-11-10T03:00:00.000Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatTime(tc.input); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}
