package portgateway

import (
	"sync"
	"time"
)

type throttle struct {
	mu     sync.Mutex
	window time.Duration
	last   map[string]time.Time
}

func newThrottle(window time.Duration) *throttle {
	return &throttle{window: window, last: make(map[string]time.Time)}
}

func (t *throttle) allow(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if last, ok := t.last[key]; ok && now.Sub(last) < t.window {
		return false
	}
	for candidate, last := range t.last {
		if now.Sub(last) >= t.window {
			delete(t.last, candidate)
		}
	}
	t.last[key] = now
	return true
}

func (t *throttle) forget(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.last, key)
}
