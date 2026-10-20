package codesessions

import (
	"sync"
	"time"
)

type sandboxRenewals struct {
	mu        sync.Mutex
	renewedAt map[string]time.Time
}

func (r *sandboxRenewals) fresh(sandboxID string, now time.Time, window time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	renewedAt, ok := r.renewedAt[sandboxID]
	return ok && now.Sub(renewedAt) < window
}

func (r *sandboxRenewals) record(sandboxID string, now time.Time, window time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.renewedAt == nil {
		r.renewedAt = make(map[string]time.Time)
	}
	for id, renewedAt := range r.renewedAt {
		if now.Sub(renewedAt) >= window {
			delete(r.renewedAt, id)
		}
	}
	r.renewedAt[sandboxID] = now
}

func sandboxRenewalWindow(timeout time.Duration) time.Duration {
	return timeout / 3
}
