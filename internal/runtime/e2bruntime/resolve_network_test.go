package e2bruntime

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestResolveNetworkInvalidConfigs 验证 resolveNetwork 对各种异常输入的处理。
func TestResolveNetworkInvalidConfigs(t *testing.T) {
	tests := []struct {
		name             string
		raw              string
		wantAllowOut     []string
		wantDenyOut      []string
		wantInternet     bool
		wantErr          bool
		wantNilNetwork   bool // 期望返回 nil Network
	}{
		{
			name:           "nil config returns unrestricted",
			raw:            "",
			wantInternet:   true,
			wantNilNetwork: true,
		},
		{
			name:           "empty object returns unrestricted",
			raw:            "{}",
			wantInternet:   true,
			wantNilNetwork: true,
		},
		{
			name:           "type not cloud returns unrestricted",
			raw:            `{"type":"self_hosted","networking":{"type":"limited","allowed_hosts":["example.com"]}}`,
			wantInternet:   true,
			wantNilNetwork: true,
		},
		{
			name:           "networking is nil returns unrestricted",
			raw:            `{"type":"cloud"}`,
			wantInternet:   true,
			wantNilNetwork: true,
		},
		{
			name:           "unrestricted networking returns unrestricted",
			raw:            `{"type":"cloud","networking":{"type":"unrestricted"}}`,
			wantInternet:   true,
			wantNilNetwork: true,
		},
		{
			name:           "unknown networking type returns no internet but nil network",
			raw:            `{"type":"cloud","networking":{"type":"custom_firewall"}}`,
			wantInternet:   false,
			wantNilNetwork: true,
		},
		{
			name:           "invalid json returns error",
			raw:            `{"type":"cloud","networking":broken}`,
			wantErr:        true,
		},
		{
			name:           "empty networking type treated as unknown → no internet, nil network",
			raw:            `{"type":"cloud","networking":{"type":"","allowed_hosts":["example.com"]}}`,
			wantInternet:   false,
			wantNilNetwork: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw json.RawMessage
			if tt.raw != "" {
				raw = json.RawMessage(tt.raw)
			}
			network, internet, err := resolveNetwork(raw, nil)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if internet != tt.wantInternet {
				t.Fatalf("AllowInternetAccess = %v, want %v", internet, tt.wantInternet)
			}
			if tt.wantNilNetwork {
				if network != nil {
					t.Fatalf("expected nil network, got %#v", network)
				}
				return
			}
			if network == nil {
				t.Fatal("expected non-nil network")
			}
			if tt.wantAllowOut != nil {
				if !reflect.DeepEqual(network.AllowOut, tt.wantAllowOut) {
					t.Fatalf("AllowOut = %#v, want %#v", network.AllowOut, tt.wantAllowOut)
				}
			}
			if tt.wantDenyOut != nil {
				if !reflect.DeepEqual(network.DenyOut, tt.wantDenyOut) {
					t.Fatalf("DenyOut = %#v, want %#v", network.DenyOut, tt.wantDenyOut)
				}
			}
		})
	}
}

// TestResolveNetworkUnrestricted 验证 unrestricted 模式始终返回 nil network + internet=true。
func TestResolveNetworkUnrestricted(t *testing.T) {
	raw := json.RawMessage(`{"type":"cloud","networking":{"type":"unrestricted"}}`)
	network, internet, err := resolveNetwork(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !internet {
		t.Fatal("unrestricted should allow internet")
	}
	if network != nil {
		t.Fatalf("unrestricted should return nil network, got %#v", network)
	}
}

// TestResolveNetworkLimited 验证 limited 模式返回 AllowOut + DenyOut ALL_TRAFFIC。
func TestResolveNetworkLimited(t *testing.T) {
	raw := json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["allowed.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}}`)
	network, internet, err := resolveNetwork(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if internet {
		t.Fatal("limited should disable internet")
	}
	if network == nil {
		t.Fatal("expected non-nil network")
	}
	wantAllow := []string{"allowed.example.com"}
	if !reflect.DeepEqual(network.AllowOut, wantAllow) {
		t.Fatalf("AllowOut = %#v, want %#v", network.AllowOut, wantAllow)
	}
	wantDeny := []string{"0.0.0.0/0"}
	if !reflect.DeepEqual(network.DenyOut, wantDeny) {
		t.Fatalf("DenyOut = %#v, want %#v", network.DenyOut, wantDeny)
	}
}

// TestResolveNetworkLimitedWithPackageManagers 验证 allow_package_managers 追加包管理器 host。
func TestResolveNetworkLimitedWithPackageManagers(t *testing.T) {
	raw := json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_package_managers":true}}`)
	network, internet, err := resolveNetwork(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if internet {
		t.Fatal("limited should disable internet")
	}
	if network == nil {
		t.Fatal("expected non-nil network")
	}

	allowOut, ok := network.AllowOut.([]string)
	if !ok {
		t.Fatalf("AllowOut type = %T, want []string", network.AllowOut)
	}
	pmHosts := packageManagerHosts()
	for _, pm := range pmHosts {
		found := false
		for _, h := range allowOut {
			if h == pm {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("AllowOut missing package manager host %q; got %#v", pm, allowOut)
		}
	}
}
