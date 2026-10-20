package e2bruntime

import (
	"testing"
	"time"
)

func TestServiceHostCacheExpiresAndPrunes(t *testing.T) {
	var cache serviceHostCache
	now := time.Unix(1_700_000_000, 0)
	if _, ok := cache.get("sbx:3001", now); ok {
		t.Fatal("empty cache returned a host")
	}
	cache.put("sbx:3001", "3001-sbx.e2b.test", now)
	if _, ok := cache.get("sbx:3001", now.Add(serviceHostCacheTTL)); ok {
		t.Fatal("expired host was returned")
	}
	cache.put("other:3001", "3001-other.e2b.test", now.Add(serviceHostCacheTTL))
	if _, ok := cache.hosts["sbx:3001"]; ok {
		t.Fatal("expired host was not pruned")
	}
}

func TestServiceHostCacheReturnsRecentHost(t *testing.T) {
	var cache serviceHostCache
	now := time.Unix(1_700_000_000, 0)
	cache.put("sbx:3001", "3001-sbx.e2b.test", now)
	host, ok := cache.get("sbx:3001", now.Add(serviceHostCacheTTL-time.Second))
	if !ok || host != "3001-sbx.e2b.test" {
		t.Fatalf("host=%q ok=%v", host, ok)
	}
	if _, ok := cache.get("sbx:3002", now); ok {
		t.Fatal("host leaked to another port")
	}
}
