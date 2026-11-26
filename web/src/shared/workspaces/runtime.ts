export type RuntimeProviderKind = 'oma_native' | 'qoder_cloud' | 'claude_managed' | 'openai_agents' | (string & {});

export type RuntimeBudgetMode = 'none' | 'usd' | 'credits';

export type RuntimeUsageMeteringMode = 'token_and_cost' | 'provider_credits';

export type RuntimeArtifactDiscoveryMode = 'filesystem_outputs_dir' | 'explicit_deliver_tool';

export type RuntimeFileUploadConstraints = {
  max_size_bytes: number;
  allowed_mime_prefixes?: string[] | null;
  allow_binary: boolean;
};

export type RuntimeCapabilities = {
  mcp_tunnels: boolean;
  port_preview: boolean;
  otlp_traces: boolean;
  observability_dashboard: boolean;
  byok_llm_providers: boolean;
  deployments: boolean;
  session_webhooks: boolean;
  session_budget: RuntimeBudgetMode;
  session_agent_overrides: boolean;
  channels: boolean;
  evaluations: boolean;
  skills: boolean;
  memory_stores: boolean;
  credential_vaults: boolean;
  mcp_servers: boolean;
  usage_metering: RuntimeUsageMeteringMode;
  artifact_discovery: RuntimeArtifactDiscoveryMode;
  file_constraints: RuntimeFileUploadConstraints;
};

export type BooleanRuntimeCapabilityKey =
  | 'mcp_tunnels'
  | 'port_preview'
  | 'otlp_traces'
  | 'observability_dashboard'
  | 'byok_llm_providers'
  | 'deployments'
  | 'session_webhooks'
  | 'session_agent_overrides'
  | 'channels'
  | 'evaluations'
  | 'skills'
  | 'memory_stores'
  | 'credential_vaults'
  | 'mcp_servers';

export type WorkspaceRuntimeSummary = {
  profile_uuid: string;
  profile_id: string;
  profile_name: string;
  provider_kind: RuntimeProviderKind;
  capabilities: RuntimeCapabilities;
};

export type OrganizationRuntimeProfile = {
  uuid: string;
  id: string;
  name: string;
  provider_kind: RuntimeProviderKind;
  status?: string;
  builtin?: boolean;
  is_default?: boolean;
  effective_capabilities?: RuntimeCapabilities;
  capabilities?: RuntimeCapabilities;
};

export type OrganizationRuntimeProfilesResult = {
  profiles: OrganizationRuntimeProfile[];
  defaultProfileId: string;
  defaultProfileUuid: string;
};

export const DEFAULT_OMA_NATIVE_RUNTIME_PROFILE_UUID = '00000000-0000-4000-8000-000000000001';
export const DEFAULT_OMA_NATIVE_RUNTIME_PROFILE_ID = 'rprof_oma_native_default';

export const defaultNativeCapabilities: RuntimeCapabilities = {
  mcp_tunnels: true,
  port_preview: true,
  otlp_traces: true,
  observability_dashboard: true,
  byok_llm_providers: true,
  deployments: true,
  session_webhooks: true,
  session_budget: 'usd',
  session_agent_overrides: true,
  channels: true,
  evaluations: true,
  skills: true,
  memory_stores: true,
  credential_vaults: true,
  mcp_servers: true,
  usage_metering: 'token_and_cost',
  artifact_discovery: 'filesystem_outputs_dir',
  file_constraints: {
    max_size_bytes: 0,
    allow_binary: true,
  },
};

export const defaultNativeRuntime: WorkspaceRuntimeSummary = {
  profile_uuid: DEFAULT_OMA_NATIVE_RUNTIME_PROFILE_UUID,
  profile_id: DEFAULT_OMA_NATIVE_RUNTIME_PROFILE_ID,
  profile_name: 'OMA Native',
  provider_kind: 'oma_native',
  capabilities: defaultNativeCapabilities,
};

export function isNativeRuntimeProvider(providerKind?: string | null): boolean {
  return !providerKind || providerKind === 'oma_native';
}

export function formatRuntimeProviderKind(providerKind?: string | null): string {
  switch (providerKind) {
    case 'oma_native':
      return 'OMA Native';
    case 'qoder_cloud':
      return 'Qoder Cloud';
    case 'claude_managed':
      return 'Claude Managed';
    case 'openai_agents':
      return 'OpenAI Agents';
    default:
      return providerKind || 'OMA Native';
  }
}

export function resolveWorkspaceRuntime(runtime?: Partial<WorkspaceRuntimeSummary> | null): WorkspaceRuntimeSummary {
  if (!runtime || !runtime.provider_kind) {
    return defaultNativeRuntime;
  }
  const caps = runtime.capabilities;
  return {
    profile_uuid: runtime.profile_uuid || DEFAULT_OMA_NATIVE_RUNTIME_PROFILE_UUID,
    profile_id: runtime.profile_id || DEFAULT_OMA_NATIVE_RUNTIME_PROFILE_ID,
    profile_name: runtime.profile_name || formatRuntimeProviderKind(runtime.provider_kind),
    provider_kind: runtime.provider_kind,
    capabilities: {
      ...defaultNativeCapabilities,
      ...caps,
      file_constraints: {
        ...defaultNativeCapabilities.file_constraints,
        ...(caps?.file_constraints ?? {}),
      },
    },
  };
}

export function workspaceRuntimeBadgeLabel(
  runtime?: Pick<WorkspaceRuntimeSummary, 'profile_name' | 'provider_kind'> | null,
): string | null {
  if (!runtime || isNativeRuntimeProvider(runtime.provider_kind)) {
    return null;
  }
  return runtime.profile_name || formatRuntimeProviderKind(runtime.provider_kind);
}
