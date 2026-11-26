package sessions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

const (
	externalEventIdempotencyTTL        = 10 * time.Minute
	externalEventIdempotencyMaxEntries = 4096
)

type idempotencyCacheEntry struct {
	requestHash string
	response    sendEventsResponse
	expiresAt   time.Time
}

type eventIdempotencyCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	maxSize int
	entries map[string]idempotencyCacheEntry
}

func newEventIdempotencyCache(ttl time.Duration, maxSize int) *eventIdempotencyCache {
	if ttl <= 0 {
		ttl = externalEventIdempotencyTTL
	}
	if maxSize <= 0 {
		maxSize = externalEventIdempotencyMaxEntries
	}
	return &eventIdempotencyCache{
		ttl:     ttl,
		maxSize: maxSize,
		entries: make(map[string]idempotencyCacheEntry),
	}
}

func idempotencyCacheKey(workspaceUUID, sessionID, key string) string {
	return workspaceUUID + "\x00" + sessionID + "\x00" + key
}

func hashEventsPayload(raw json.RawMessage) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (c *eventIdempotencyCache) lookup(workspaceUUID, sessionID, key, requestHash string, now time.Time) (sendEventsResponse, bool, bool) {
	if c == nil || key == "" {
		return sendEventsResponse{}, false, false
	}
	cacheKey := idempotencyCacheKey(workspaceUUID, sessionID, key)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[cacheKey]
	if !ok {
		return sendEventsResponse{}, false, false
	}
	if now.After(entry.expiresAt) {
		delete(c.entries, cacheKey)
		return sendEventsResponse{}, false, false
	}
	if entry.requestHash != requestHash {
		return sendEventsResponse{}, false, true
	}
	return entry.response, true, false
}

func (c *eventIdempotencyCache) store(workspaceUUID, sessionID, key, requestHash string, response sendEventsResponse, now time.Time) {
	if c == nil || key == "" {
		return
	}
	cacheKey := idempotencyCacheKey(workspaceUUID, sessionID, key)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.maxSize {
		for k, v := range c.entries {
			if now.After(v.expiresAt) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= c.maxSize {
			clear(c.entries)
		}
	}
	c.entries[cacheKey] = idempotencyCacheEntry{
		requestHash: requestHash,
		response:    response,
		expiresAt:   now.Add(c.ttl),
	}
}
