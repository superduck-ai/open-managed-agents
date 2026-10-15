package webhooks

import (
	"math"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestWebhookRetryDelayBounds(t *testing.T) {
	for _, tc := range []struct {
		failures int
		upper    time.Duration
	}{
		{math.MinInt, 10 * time.Second}, {0, 10 * time.Second}, {1, 10 * time.Second}, {2, 20 * time.Second},
		{3, 40 * time.Second}, {4, 80 * time.Second}, {5, 120 * time.Second}, {math.MaxInt, 120 * time.Second},
	} {
		for _, high := range []bool{false, true} {
			got := retryDelay(tc.failures, func(bound int64) int64 {
				if bound != int64(tc.upper-5*time.Second) {
					t.Fatalf("random bound=%v upper=%v", bound, tc.upper)
				}
				if high {
					return bound - 1
				}
				return 0
			})
			want := 5 * time.Second
			if high {
				want = tc.upper - time.Nanosecond
			}
			if got != want {
				t.Fatalf("failures=%d got=%v want=%v", tc.failures, got, want)
			}
		}
	}
}

func TestWebhookMaxAttempts(t *testing.T) {
	for _, tc := range []struct{ configured, want int }{{0, 3}, {-1, 3}, {1, 1}, {3, 3}, {10, 10}} {
		if got := webhookMaxAttempts(config.WebhookConfig{MaxAttempts: tc.configured}); got != tc.want {
			t.Fatalf("configured=%d got=%d", tc.configured, got)
		}
	}
}
