package tunnels

import (
	"bytes"
	"testing"
)

func TestNewTunnelIDRejectsInsufficientEntropy(t *testing.T) {
	service := &Service{random: bytes.NewReader(make([]byte, 15))}
	if _, err := service.newTunnelID(); err == nil {
		t.Fatal("newTunnelID accepted fewer than 16 random bytes")
	}
}

func TestNewTunnelIDUsesStockTunnelClientFormat(t *testing.T) {
	service := &Service{random: bytes.NewReader([]byte{
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	})}
	got, err := service.newTunnelID()
	if err != nil {
		t.Fatalf("newTunnelID: %v", err)
	}
	const want = "tunnel_000102030405060708090a0b0c0d0e0f"
	if got != want {
		t.Fatalf("newTunnelID = %q, want %q", got, want)
	}
}
