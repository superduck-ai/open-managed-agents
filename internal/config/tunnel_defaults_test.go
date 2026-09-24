package config

import "testing"

func TestTunnelDefaultsAndOverrides(t *testing.T) {
	prepareLoadTest(t)
	cfg, err := loadConfigTestYAML(t, "tunnel:\n  max_body_bytes: 8388608\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tunnel.MaxBodyBytes != 8388608 {
		t.Fatal("explicit configuration overwritten")
	}
	cfg, err = loadConfigTestYAML(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tunnel.MaxBodyBytes != 16<<20 {
		t.Fatal("unexpected tunnel defaults")
	}
}

func TestTunnelRejectsRemovedRequestCountConfiguration(t *testing.T) {
	prepareLoadTest(t)
	if _, err := loadConfigTestYAML(t, "tunnel:\n  max_stored_requests: 256\n"); err == nil {
		t.Fatal("accepted removed request-count configuration")
	}
}
