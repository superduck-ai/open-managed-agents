package porttunnel

import (
	"testing"
	"time"
)

func TestJitteredRevalidationStaysWithinTwentyPercent(t *testing.T) {
	seen := map[time.Duration]bool{}
	for range 1000 {
		got := jittered(30 * time.Second)
		if got < 24*time.Second || got > 36*time.Second {
			t.Fatalf("jittered interval %s outside ±20%%", got)
		}
		seen[got] = true
	}
	if len(seen) < 10 {
		t.Fatalf("jitter produced only %d distinct intervals", len(seen))
	}
	if defaultRevalidateEvery != 30*time.Second {
		t.Fatalf("default revalidation interval = %s", defaultRevalidateEvery)
	}
}
