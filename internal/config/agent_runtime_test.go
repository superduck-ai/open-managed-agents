package config

import "testing"

func TestLocalSandboxEndpointRejectsInvalidConfig(t *testing.T) {
	for _, cfg := range []E2BConfig{
		{LocalPortLookup: true},
		{LocalPortLookup: true, APIURL: "file:///tmp/api"},
		{LocalPortLookup: true, APIURL: "http://user:secret@localhost"},
		{LocalPortLookup: true, APIURL: "http://localhost?token=x"},
		{LocalServiceHost: "localhost"},
		{LocalPortLookup: true, APIURL: "http://localhost", LocalServiceHost: "http://localhost"},
		{LocalPortLookup: true, APIURL: "http://localhost", LocalServiceHost: "localhost:3001"},
		{LocalPortLookup: true, APIURL: "http://localhost", LocalServiceHost: "-invalid.local"},
	} {
		if err := validateLocalSandboxEndpointConfig(cfg); err == nil {
			t.Fatal("accepted invalid local endpoint configuration")
		}
	}
}

func TestLocalSandboxEndpointAcceptsDefaultsAndHostOverrides(t *testing.T) {
	if err := validateLocalSandboxEndpointConfig(E2BConfig{}); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"", "localhost", "127.0.0.1", "::1"} {
		if err := validateLocalSandboxEndpointConfig(E2BConfig{LocalPortLookup: true, APIURL: "http://localhost:3099", LocalServiceHost: host}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAgentRuntimeConfigRejectsInvalidModeAndEndpoint(t *testing.T) {
	for _, cfg := range []EnvironmentRunnerConfig{
		{AgentMode: "worker"}, {SandboxMCPPort: -1}, {SandboxMCPPort: 65536},
		{SandboxMCPPath: "mcp"}, {SandboxMCPPath: "//example.test/mcp"},
		{SandboxMCPPath: "/mcp?token=x"}, {SandboxMCPPath: "/mcp#x"},
	} {
		if err := validateAgentRuntimeConfig(cfg); err == nil {
			t.Fatalf("accepted invalid runtime config: %#v", cfg)
		}
	}
}

func TestAgentRuntimeConfigAcceptsBothModesAndDefaults(t *testing.T) {
	for _, cfg := range []EnvironmentRunnerConfig{{}, {AgentMode: "sandbox"}, {AgentMode: "host", SandboxMCPPort: 8090, SandboxMCPPath: "/mcp"}} {
		if err := validateAgentRuntimeConfig(cfg); err != nil {
			t.Fatal(err)
		}
	}
}
