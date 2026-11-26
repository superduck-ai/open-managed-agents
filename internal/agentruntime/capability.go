package agentruntime

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	CapabilityMCPTunnels             = "mcp_tunnels"
	CapabilityPortPreview            = "port_preview"
	CapabilityOTLPTraces             = "otlp_traces"
	CapabilityObservabilityDashboard = "observability_dashboard"
	CapabilityBYOKLLMProviders       = "byok_llm_providers"
	CapabilityDeployments            = "deployments"
	CapabilitySessionWebhooks        = "session_webhooks"
	CapabilitySessionAgentOverrides  = "session_agent_overrides"
	CapabilityChannels               = "channels"
	CapabilityEvaluations            = "evaluations"
	CapabilitySkills                 = "skills"
	CapabilityMemoryStores           = "memory_stores"
	CapabilityCredentialVaults       = "credential_vaults"
	CapabilityMCPServers             = "mcp_servers"
)

type CapabilitySet struct {
	MCPTunnels             bool                  `json:"mcp_tunnels"`
	PortPreview            bool                  `json:"port_preview"`
	OTLPTraces             bool                  `json:"otlp_traces"`
	ObservabilityDashboard bool                  `json:"observability_dashboard"`
	BYOKLLMProviders       bool                  `json:"byok_llm_providers"`
	Deployments            bool                  `json:"deployments"`
	SessionWebhooks        bool                  `json:"session_webhooks"`
	SessionBudget          BudgetMode            `json:"session_budget"`
	SessionAgentOverrides  bool                  `json:"session_agent_overrides"`
	Channels               bool                  `json:"channels"`
	Evaluations            bool                  `json:"evaluations"`
	Skills                 bool                  `json:"skills"`
	MemoryStores           bool                  `json:"memory_stores"`
	CredentialVaults       bool                  `json:"credential_vaults"`
	MCPServers             bool                  `json:"mcp_servers"`
	UsageMetering          UsageMeteringMode     `json:"usage_metering"`
	ArtifactDiscovery      ArtifactDiscoveryMode `json:"artifact_discovery"`
	FileConstraints        FileUploadConstraints `json:"file_constraints"`
}

type capabilityOverridesPayload struct {
	MCPTunnels             *bool                  `json:"mcp_tunnels"`
	PortPreview            *bool                  `json:"port_preview"`
	OTLPTraces             *bool                  `json:"otlp_traces"`
	ObservabilityDashboard *bool                  `json:"observability_dashboard"`
	BYOKLLMProviders       *bool                  `json:"byok_llm_providers"`
	Deployments            *bool                  `json:"deployments"`
	SessionWebhooks        *bool                  `json:"session_webhooks"`
	SessionBudget          *BudgetMode            `json:"session_budget"`
	SessionAgentOverrides  *bool                  `json:"session_agent_overrides"`
	Channels               *bool                  `json:"channels"`
	Evaluations            *bool                  `json:"evaluations"`
	Skills                 *bool                  `json:"skills"`
	MemoryStores           *bool                  `json:"memory_stores"`
	CredentialVaults       *bool                  `json:"credential_vaults"`
	MCPServers             *bool                  `json:"mcp_servers"`
	UsageMetering          *UsageMeteringMode     `json:"usage_metering"`
	ArtifactDiscovery      *ArtifactDiscoveryMode `json:"artifact_discovery"`
	FileConstraints        *FileUploadConstraints `json:"file_constraints"`
}

func DefaultCapabilities(kind ProviderKind) CapabilitySet {
	switch kind {
	case ProviderOMANative:
		return CapabilitySet{
			MCPTunnels:             true,
			PortPreview:            true,
			OTLPTraces:             true,
			ObservabilityDashboard: true,
			BYOKLLMProviders:       true,
			Deployments:            true,
			SessionWebhooks:        true,
			SessionBudget:          BudgetModeUSD,
			SessionAgentOverrides:  true,
			Channels:               true,
			Evaluations:            true,
			Skills:                 true,
			MemoryStores:           true,
			CredentialVaults:       true,
			MCPServers:             true,
			UsageMetering:          UsageMeteringTokenAndCost,
			ArtifactDiscovery:      ArtifactDiscoveryFilesystemOutputsDir,
			FileConstraints: FileUploadConstraints{
				AllowBinary: true,
			},
		}
	case ProviderQoderCloud:
		return CapabilitySet{
			MCPTunnels:             false,
			PortPreview:            false,
			OTLPTraces:             false,
			ObservabilityDashboard: false,
			BYOKLLMProviders:       false,
			Deployments:            true,
			SessionWebhooks:        false,
			SessionBudget:          BudgetModeCredits,
			SessionAgentOverrides:  false,
			Channels:               false,
			Evaluations:            false,
			Skills:                 false,
			MemoryStores:           true,
			CredentialVaults:       false,
			MCPServers:             false,
			UsageMetering:          UsageMeteringProviderCredits,
			ArtifactDiscovery:      ArtifactDiscoveryExplicitDeliverTool,
			FileConstraints: FileUploadConstraints{
				MaxSizeBytes:        5 * 1024 * 1024,
				AllowedMIMEPrefixes: []string{"text/", "image/", "application/json", "application/xml"},
				AllowBinary:         false,
			},
		}
	default:
		return CapabilitySet{
			SessionBudget:     BudgetModeNone,
			UsageMetering:     UsageMeteringProviderCredits,
			ArtifactDiscovery: ArtifactDiscoveryExplicitDeliverTool,
		}
	}
}

func MergeCapabilityOverrides(base CapabilitySet, raw json.RawMessage) (CapabilitySet, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("{}")) {
		return base, nil
	}
	var overrides capabilityOverridesPayload
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&overrides); err != nil {
		return CapabilitySet{}, InvalidArgumentError("invalid capability_overrides payload", err)
	}
	merged := base
	applyBoolOverride(&merged.MCPTunnels, overrides.MCPTunnels)
	applyBoolOverride(&merged.PortPreview, overrides.PortPreview)
	applyBoolOverride(&merged.OTLPTraces, overrides.OTLPTraces)
	applyBoolOverride(&merged.ObservabilityDashboard, overrides.ObservabilityDashboard)
	applyBoolOverride(&merged.BYOKLLMProviders, overrides.BYOKLLMProviders)
	applyBoolOverride(&merged.Deployments, overrides.Deployments)
	applyBoolOverride(&merged.SessionWebhooks, overrides.SessionWebhooks)
	applyBoolOverride(&merged.SessionAgentOverrides, overrides.SessionAgentOverrides)
	applyBoolOverride(&merged.Channels, overrides.Channels)
	applyBoolOverride(&merged.Evaluations, overrides.Evaluations)
	applyBoolOverride(&merged.Skills, overrides.Skills)
	applyBoolOverride(&merged.MemoryStores, overrides.MemoryStores)
	applyBoolOverride(&merged.CredentialVaults, overrides.CredentialVaults)
	applyBoolOverride(&merged.MCPServers, overrides.MCPServers)
	if overrides.SessionBudget != nil {
		if !validBudgetMode(*overrides.SessionBudget) {
			return CapabilitySet{}, InvalidArgumentError("invalid session_budget mode", fmt.Errorf("%w: %s", ErrInvalidCapabilityKey, *overrides.SessionBudget))
		}
		merged.SessionBudget = *overrides.SessionBudget
	}
	if overrides.UsageMetering != nil {
		if !validUsageMeteringMode(*overrides.UsageMetering) {
			return CapabilitySet{}, InvalidArgumentError("invalid usage_metering mode", fmt.Errorf("%w: %s", ErrInvalidCapabilityKey, *overrides.UsageMetering))
		}
		merged.UsageMetering = *overrides.UsageMetering
	}
	if overrides.ArtifactDiscovery != nil {
		if !validArtifactDiscoveryMode(*overrides.ArtifactDiscovery) {
			return CapabilitySet{}, InvalidArgumentError("invalid artifact_discovery mode", fmt.Errorf("%w: %s", ErrInvalidCapabilityKey, *overrides.ArtifactDiscovery))
		}
		merged.ArtifactDiscovery = *overrides.ArtifactDiscovery
	}
	if overrides.FileConstraints != nil {
		if overrides.FileConstraints.MaxSizeBytes < 0 {
			return CapabilitySet{}, InvalidArgumentError("file_constraints.max_size_bytes must be non-negative", ErrInvalidCapabilityKey)
		}
		merged.FileConstraints = *overrides.FileConstraints
	}
	return merged, nil
}

func (c CapabilitySet) Supports(key string) (bool, error) {
	switch key {
	case CapabilityMCPTunnels:
		return c.MCPTunnels, nil
	case CapabilityPortPreview:
		return c.PortPreview, nil
	case CapabilityOTLPTraces:
		return c.OTLPTraces, nil
	case CapabilityObservabilityDashboard:
		return c.ObservabilityDashboard, nil
	case CapabilityBYOKLLMProviders:
		return c.BYOKLLMProviders, nil
	case CapabilityDeployments:
		return c.Deployments, nil
	case CapabilitySessionWebhooks:
		return c.SessionWebhooks, nil
	case CapabilitySessionAgentOverrides:
		return c.SessionAgentOverrides, nil
	case CapabilityChannels:
		return c.Channels, nil
	case CapabilityEvaluations:
		return c.Evaluations, nil
	case CapabilitySkills:
		return c.Skills, nil
	case CapabilityMemoryStores:
		return c.MemoryStores, nil
	case CapabilityCredentialVaults:
		return c.CredentialVaults, nil
	case CapabilityMCPServers:
		return c.MCPServers, nil
	default:
		return false, fmt.Errorf("%w: %s", ErrInvalidCapabilityKey, key)
	}
}

func (c CapabilitySet) Require(key string) error {
	supported, err := c.Supports(key)
	if err != nil {
		return InvalidArgumentError("unknown runtime capability", err)
	}
	if !supported {
		return CapabilityUnsupportedError(key)
	}
	return nil
}

func applyBoolOverride(target *bool, override *bool) {
	if override != nil {
		*target = *override
	}
}

func validBudgetMode(mode BudgetMode) bool {
	return mode == BudgetModeNone || mode == BudgetModeUSD || mode == BudgetModeCredits
}

func validUsageMeteringMode(mode UsageMeteringMode) bool {
	return mode == UsageMeteringTokenAndCost || mode == UsageMeteringProviderCredits
}

func validArtifactDiscoveryMode(mode ArtifactDiscoveryMode) bool {
	return mode == ArtifactDiscoveryFilesystemOutputsDir || mode == ArtifactDiscoveryExplicitDeliverTool
}
