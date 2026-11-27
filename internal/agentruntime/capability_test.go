package agentruntime_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

func TestDefaultCapabilitiesAndSupports(t *testing.T) {
	native := agentruntime.DefaultCapabilities(agentruntime.ProviderOMANative)
	if !native.MCPTunnels || !native.PortPreview || !native.BYOKLLMProviders || !native.Deployments || !native.SessionWebhooks || !native.SessionAgentOverrides {
		t.Fatalf("native capabilities unexpectedly disabled: %+v", native)
	}
	if native.SessionBudget != agentruntime.BudgetModeUSD {
		t.Fatalf("native session budget = %q, want usd", native.SessionBudget)
	}
	if native.UsageMetering != agentruntime.UsageMeteringTokenAndCost {
		t.Fatalf("native usage metering = %q", native.UsageMetering)
	}
	if native.ArtifactDiscovery != agentruntime.ArtifactDiscoveryFilesystemOutputsDir {
		t.Fatalf("native artifact discovery = %q", native.ArtifactDiscovery)
	}
	if !native.FileConstraints.AllowBinary {
		t.Fatalf("native should allow binary files")
	}

	qoder := agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud)
	if !qoder.MemoryStores || !qoder.Deployments || !qoder.Skills {
		t.Fatalf("qoder memory_stores, deployments and skills should be true: %+v", qoder)
	}
	if qoder.MCPTunnels || qoder.PortPreview || qoder.OTLPTraces || qoder.ObservabilityDashboard || qoder.BYOKLLMProviders || qoder.SessionWebhooks || qoder.SessionAgentOverrides || qoder.Channels || qoder.Evaluations || qoder.CredentialVaults || qoder.MCPServers {
		t.Fatalf("qoder unsupported capabilities should be false: %+v", qoder)
	}
	if qoder.SessionBudget != agentruntime.BudgetModeCredits {
		t.Fatalf("qoder session budget = %q, want credits", qoder.SessionBudget)
	}
	if qoder.UsageMetering != agentruntime.UsageMeteringProviderCredits {
		t.Fatalf("qoder usage metering = %q", qoder.UsageMetering)
	}
	if qoder.ArtifactDiscovery != agentruntime.ArtifactDiscoveryExplicitDeliverTool {
		t.Fatalf("qoder artifact discovery = %q", qoder.ArtifactDiscovery)
	}
	if qoder.FileConstraints.MaxSizeBytes != 5*1024*1024 {
		t.Fatalf("qoder max file size = %d", qoder.FileConstraints.MaxSizeBytes)
	}

	unknown := agentruntime.DefaultCapabilities(agentruntime.ProviderKind("custom"))
	if unknown.SessionBudget != agentruntime.BudgetModeNone || unknown.Channels {
		t.Fatalf("unknown default capabilities = %+v", unknown)
	}

	for _, key := range []string{
		agentruntime.CapabilityMCPTunnels,
		agentruntime.CapabilityPortPreview,
		agentruntime.CapabilityOTLPTraces,
		agentruntime.CapabilityObservabilityDashboard,
		agentruntime.CapabilityBYOKLLMProviders,
		agentruntime.CapabilityDeployments,
		agentruntime.CapabilitySessionWebhooks,
		agentruntime.CapabilitySessionAgentOverrides,
		agentruntime.CapabilityChannels,
		agentruntime.CapabilityEvaluations,
		agentruntime.CapabilitySkills,
		agentruntime.CapabilityMemoryStores,
		agentruntime.CapabilityCredentialVaults,
		agentruntime.CapabilityMCPServers,
	} {
		if ok, err := native.Supports(key); err != nil || !ok {
			t.Fatalf("native.Supports(%q) = (%v, %v), want (true, nil)", key, ok, err)
		}
		if err := native.Require(key); err != nil {
			t.Fatalf("native.Require(%q) = %v, want nil", key, err)
		}
	}

	if err := qoder.Require(agentruntime.CapabilityMCPTunnels); !errors.Is(err, agentruntime.ErrCapabilityUnsupported) {
		t.Fatalf("qoder.Require(mcp_tunnels) = %v, want ErrCapabilityUnsupported", err)
	}
	if _, err := qoder.Supports("not_a_real_capability"); !errors.Is(err, agentruntime.ErrInvalidCapabilityKey) {
		t.Fatalf("qoder.Supports(invalid) = %v, want ErrInvalidCapabilityKey", err)
	}
	if err := qoder.Require("not_a_real_capability"); err == nil {
		t.Fatal("qoder.Require(invalid) succeeded, want error")
	}
}

func TestMergeCapabilityOverrides(t *testing.T) {
	base := agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud)

	unchanged, err := agentruntime.MergeCapabilityOverrides(base, nil)
	if err != nil || unchanged.Deployments != base.Deployments {
		t.Fatalf("MergeCapabilityOverrides(nil) = (%+v, %v)", unchanged, err)
	}

	raw := json.RawMessage(`{
		"deployments": true,
		"session_webhooks": true,
		"session_budget": "none",
		"usage_metering": "token_and_cost",
		"artifact_discovery": "filesystem_outputs_dir",
		"file_constraints": {
			"max_size_bytes": 10485760,
			"allowed_mime_prefixes": ["text/", "image/"],
			"allow_binary": true
		}
	}`)
	merged, err := agentruntime.MergeCapabilityOverrides(base, raw)
	if err != nil {
		t.Fatalf("MergeCapabilityOverrides(valid): %v", err)
	}
	if !merged.Deployments || !merged.SessionWebhooks || merged.SessionBudget != agentruntime.BudgetModeNone ||
		merged.UsageMetering != agentruntime.UsageMeteringTokenAndCost ||
		merged.ArtifactDiscovery != agentruntime.ArtifactDiscoveryFilesystemOutputsDir ||
		merged.FileConstraints.MaxSizeBytes != 10485760 || !merged.FileConstraints.AllowBinary {
		t.Fatalf("merged capabilities mismatch: %+v", merged)
	}

	invalidPayloads := []json.RawMessage{
		json.RawMessage(`{"unknown_flag": true}`),
		json.RawMessage(`{"session_budget": "gold"}`),
		json.RawMessage(`{"usage_metering": "minutes"}`),
		json.RawMessage(`{"artifact_discovery": "magic"}`),
		json.RawMessage(`{"file_constraints": {"max_size_bytes": -1}}`),
	}
	for _, payload := range invalidPayloads {
		_, err := agentruntime.MergeCapabilityOverrides(base, payload)
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Kind != apperr.InvalidArgument {
			t.Fatalf("MergeCapabilityOverrides(%s) err = %v, want InvalidArgument apperr", string(payload), err)
		}
	}
}
