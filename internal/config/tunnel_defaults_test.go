package config

import (
	"strings"
	"testing"
)

func TestTunnelRejectsInvalidCommandStreamLimits(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		overrides string
		wantError string
	}{
		{name: "zero bytes", overrides: "max_bytes: 0", wantError: "tunnel.command_stream.max_bytes"},
		{name: "negative bytes", overrides: "max_bytes: -1", wantError: "tunnel.command_stream.max_bytes"},
		{name: "zero messages", overrides: "max_msgs: 0", wantError: "tunnel.command_stream.max_msgs"},
		{name: "negative messages", overrides: "max_msgs: -2", wantError: "tunnel.command_stream.max_msgs"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			prepareLoadTest(t)
			_, err := loadConfigTestYAML(t, "tunnel:\n  command_stream:\n    "+testCase.overrides+"\n")
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("Load() error = %v, want %q", err, testCase.wantError)
			}
		})
	}
}

func TestTunnelRejectsRemovedRequestCountConfiguration(t *testing.T) {
	prepareLoadTest(t)
	if _, err := loadConfigTestYAML(t, "tunnel:\n  max_stored_requests: 256\n"); err == nil {
		t.Fatal("accepted removed request-count configuration")
	}
}

func TestTunnelDefaultsAndOverrides(t *testing.T) {
	prepareLoadTest(t)
	cfg, err := loadConfigTestYAML(t, "tunnel:\n  max_body_bytes: 8388608\n  command_stream:\n    max_bytes: 8589934592\n    max_msgs: 256\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tunnel.MaxBodyBytes != 8388608 || cfg.Tunnel.CommandStream != (TunnelCommandStreamConfig{MaxBytes: 8 << 30, MaxMsgs: 256}) {
		t.Fatal("explicit configuration overwritten")
	}
	cfg, err = loadConfigTestYAML(t, "tunnel:\n  command_stream:\n    max_bytes: 8589934592\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tunnel.CommandStream != (TunnelCommandStreamConfig{MaxBytes: 8 << 30, MaxMsgs: -1}) {
		t.Fatal("partial override lost command stream default")
	}
	cfg, err = loadConfigTestYAML(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tunnel.MaxBodyBytes != 16<<20 || cfg.Tunnel.CommandStream != (TunnelCommandStreamConfig{MaxBytes: 513 << 20, MaxMsgs: -1}) {
		t.Fatal("unexpected tunnel defaults")
	}
}
