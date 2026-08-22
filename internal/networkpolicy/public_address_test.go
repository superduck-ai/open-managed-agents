package networkpolicy

import (
	"net/netip"
	"testing"
)

func TestPublicAddressRejectsSpecialPurposeRanges(t *testing.T) {
	for _, raw := range []string{
		"64:ff9b:1::1",
		"100::1",
		"3fff::1",
		"5f00::1",
	} {
		t.Run(raw, func(t *testing.T) {
			if PublicAddress(netip.MustParseAddr(raw)) {
				t.Fatalf("PublicAddress(%s) = true, want false", raw)
			}
		})
	}
}

func TestPublicAddressAcceptsPublicRanges(t *testing.T) {
	for _, raw := range []string{
		"8.8.8.8",
		"2606:4700:4700::1111",
		"64:ff9b::808:808",
	} {
		t.Run(raw, func(t *testing.T) {
			if !PublicAddress(netip.MustParseAddr(raw)) {
				t.Fatalf("PublicAddress(%s) = false, want true", raw)
			}
		})
	}
}
