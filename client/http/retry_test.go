package http

import (
	"fmt"
	gohttp "net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestExponentialBackoff_Calculate(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 5 * time.Second},
		{5, 5 * time.Second},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt %d", tt.attempt), func(t *testing.T) {
			b := exponentialBackoff{
				MinWait: 1 * time.Second,
				MaxWait: 5 * time.Second,
			}
			assert.Equalf(t, tt.want, b.Calculate(tt.attempt), "Calculate(%v)", tt.attempt)
		})
	}
}

func TestRetryAfterBackoff(t *testing.T) {
	bubbleStart := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	fallback := exponentialBackoff{MinWait: 1 * time.Second, MaxWait: 10 * time.Second}

	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"delay-seconds", "30", 30 * time.Second},
		{"zero seconds", "0", 0},
		{"capped at 5 minutes", "600", 5 * time.Minute},
		{"empty header", "", 1 * time.Second},
		{"unparseable header", "not-a-number-or-date", 1 * time.Second},
		{"HTTP-date in the past", bubbleStart.Add(-10 * time.Second).Format(gohttp.TimeFormat), 1 * time.Second},
		{"HTTP-date in the future", bubbleStart.Add(45 * time.Second).Format(gohttp.TimeFormat), 45 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b := retryAfterBackoff{
					Response: &gohttp.Response{Header: gohttp.Header{"Retry-After": {tt.header}}},
					Fallback: fallback,
				}
				assert.Equal(t, tt.want, b.Calculate(1))
			})
		})
	}
}
