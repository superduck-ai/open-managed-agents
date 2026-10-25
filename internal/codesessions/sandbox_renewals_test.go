package codesessions

import (
	"testing"
	"time"
)

func TestSandboxLifetimeUsesSessionMetadata(t *testing.T) {
	configured := 30 * time.Second
	if sandboxLifetime(configured, nil) != configured {
		t.Fatal("missing metadata dropped the platform timeout")
	}
	if sandboxLifetime(configured, []byte(`{"timeout_seconds":60}`)) != time.Minute {
		t.Fatal("session timeout was not used")
	}
}

func TestSandboxRenewalsRequireRenewalOutsideWindow(t *testing.T) {
	var renewals sandboxRenewals
	now := time.Unix(1_700_000_000, 0)
	window := sandboxRenewalWindow(30 * time.Second)
	if renewals.fresh("sbx_a", now, window) {
		t.Fatal("unrenewed sandbox reported fresh")
	}
	renewals.record("sbx_a", now, window)
	if renewals.fresh("sbx_a", now.Add(window), window) {
		t.Fatal("renewal at the window boundary reported fresh")
	}
	if renewals.fresh("sbx_b", now, window) {
		t.Fatal("renewal leaked to another sandbox")
	}
}

func TestSandboxRenewalsPruneExpiredEntries(t *testing.T) {
	var renewals sandboxRenewals
	now := time.Unix(1_700_000_000, 0)
	window := 10 * time.Second
	renewals.record("sbx_old", now, window)
	renewals.record("sbx_new", now.Add(window), window)
	if _, ok := renewals.renewedAt["sbx_old"]; ok {
		t.Fatal("expired renewal was not pruned")
	}
}

func TestSandboxRenewalsSkipRecentRenewal(t *testing.T) {
	var renewals sandboxRenewals
	now := time.Unix(1_700_000_000, 0)
	window := sandboxRenewalWindow(30 * time.Second)
	if window != 10*time.Second {
		t.Fatalf("window=%s", window)
	}
	renewals.record("sbx_a", now, window)
	if !renewals.fresh("sbx_a", now.Add(window-time.Millisecond), window) {
		t.Fatal("recent renewal was not reused")
	}
}
