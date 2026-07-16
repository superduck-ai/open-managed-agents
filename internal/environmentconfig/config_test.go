package environmentconfig

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeStoredRejectsInvalidJSON(t *testing.T) {
	if _, err := DecodeStored(json.RawMessage(`{"type":`)); err == nil {
		t.Fatal("DecodeStored error = nil, want invalid JSON error")
	}
}

func TestDecodeStoredReadsKnownCloudSchema(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"cloud",
		"packages":{"type":"packages","apt":["git"],"cargo":["serde"],"gem":["rails"],"go":["golang.org/x/tools"],"npm":["typescript"],"pip":["numpy"]},
		"networking":{"type":"limited","allowed_hosts":["api.example.com"],"allow_package_managers":true,"allow_mcp_servers":true},
		"init_script":"echo ready",
		"environment":{"MODE":"test"}
	}`)

	got, err := DecodeStored(raw)
	if err != nil {
		t.Fatalf("DecodeStored: %v", err)
	}
	if got.Type != TypeCloud {
		t.Fatalf("Type = %q, want %q", got.Type, TypeCloud)
	}
	if got.Packages == nil || !reflect.DeepEqual(got.Packages.PIP, []string{"numpy"}) || !reflect.DeepEqual(got.Packages.NPM, []string{"typescript"}) {
		t.Fatalf("Packages = %#v, want typed package lists", got.Packages)
	}
	if got.Networking == nil || got.Networking.Type != NetworkTypeLimited {
		t.Fatalf("Networking = %#v, want limited", got.Networking)
	}
	if !reflect.DeepEqual(got.Networking.AllowedHosts, []string{"api.example.com"}) || !got.Networking.AllowPackageManagers || !got.Networking.AllowMCPServers {
		t.Fatalf("Networking = %#v, want allowed hosts and package/MCP flags", got.Networking)
	}
	if got.InitScript != "echo ready" || !reflect.DeepEqual(got.Environment, map[string]any{"MODE": "test"}) {
		t.Fatalf("Config = %#v, want init script and environment", got)
	}
}

func TestNormalizeCreateRejectsInvalidAllowedHost(t *testing.T) {
	_, err := NormalizeCreate(json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["https://example.com"]}}`))
	if err == nil {
		t.Fatal("NormalizeCreate error = nil, want invalid allowed host error")
	}
}

func TestNormalizeUpdatePreservesUnknownStoredFields(t *testing.T) {
	current := json.RawMessage(`{
		"type":"cloud",
		"future_config":{"enabled":true},
		"packages":{"type":"packages","pip":[],"future_package":"keep"},
		"networking":{"type":"limited","allowed_hosts":[],"future_network":"keep"}
	}`)

	config, err := NormalizeUpdate(current, json.RawMessage(`{"type":"cloud"}`))
	if err != nil {
		t.Fatalf("NormalizeUpdate: %v", err)
	}
	got, err := EncodeStored(config)
	if err != nil {
		t.Fatalf("EncodeStored: %v", err)
	}
	for _, want := range []string{`"future_config":{"enabled":true}`, `"future_package":"keep"`, `"future_network":"keep"`} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("EncodeStored = %s, want %s", got, want)
		}
	}
}

func TestResponseFromStoredPreservesUnknownConfigType(t *testing.T) {
	raw := json.RawMessage(`{"type":"future_runtime","future_field":{"enabled":true}}`)
	responseJSON, err := json.Marshal(ResponseFromStored(raw))
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(responseJSON, &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode expected response: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResponseFromStored = %#v, want %#v", got, want)
	}
}

func TestResponseFromStoredIncludesEmptyPackageLists(t *testing.T) {
	responseJSON, err := json.Marshal(ResponseFromStored(json.RawMessage(`{"type":"cloud"}`)))
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var response struct {
		Packages map[string]json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(responseJSON, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, name := range []string{"apt", "cargo", "gem", "go", "npm", "pip"} {
		if got := string(response.Packages[name]); got != "[]" {
			t.Errorf("packages.%s = %s, want []", name, got)
		}
	}
}

func TestNormalizeUpdateKeepsOmittedCloudFields(t *testing.T) {
	current := json.RawMessage(`{
		"type":"cloud",
		"packages":{"type":"packages","apt":["git"],"cargo":[],"gem":[],"go":[],"npm":[],"pip":[]},
		"networking":{"type":"limited","allowed_hosts":["api.example.com"]},
		"init_script":"echo ready",
		"environment":{"MODE":"test"}
	}`)

	got, err := NormalizeUpdate(current, json.RawMessage(`{"type":"cloud","packages":{"pip":["numpy"]}}`))
	if err != nil {
		t.Fatalf("NormalizeUpdate: %v", err)
	}
	if got.Packages == nil || !reflect.DeepEqual(got.Packages.PIP, []string{"numpy"}) {
		t.Fatalf("Packages = %#v, want replacement package config", got.Packages)
	}
	if got.Networking == nil || !reflect.DeepEqual(got.Networking.AllowedHosts, []string{"api.example.com"}) {
		t.Fatalf("Networking = %#v, want current networking", got.Networking)
	}
	if got.InitScript != "echo ready" || !reflect.DeepEqual(got.Environment, map[string]any{"MODE": "test"}) {
		t.Fatalf("Config = %#v, want current compatibility fields", got)
	}
}

func TestDecodeStoredAllowsFutureFields(t *testing.T) {
	got, err := DecodeStored(json.RawMessage(`{"type":"cloud","future_field":{"enabled":true}}`))
	if err != nil {
		t.Fatalf("DecodeStored: %v", err)
	}
	if got.Type != TypeCloud {
		t.Fatalf("Type = %q, want %q", got.Type, TypeCloud)
	}
}

func TestDecodeStoredEmptyUsesZeroValue(t *testing.T) {
	got, err := DecodeStored(nil)
	if err != nil {
		t.Fatalf("DecodeStored: %v", err)
	}
	if !reflect.DeepEqual(got, Config{}) {
		t.Fatalf("Config = %#v, want zero value", got)
	}
}
